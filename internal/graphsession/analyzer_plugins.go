package graphsession

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/enola-labs/enola/internal/analyzerplugin"
	"github.com/enola-labs/enola/internal/engine"
	"github.com/enola-labs/enola/internal/facts"
	"github.com/enola-labs/enola/internal/graphinput"
	"github.com/enola-labs/enola/internal/graphstream"
)

const pluginPlanUnit = "@plan"
const pluginResolverInventory = "@host-resolver-all-files"
const pluginResolverContext = "@host-resolver-ts-context"
const pluginContributionPrefix = "plugin:"

func (s *session) hasConfiguredAnalyzerPluginContribution(name string) bool {
	if !strings.HasPrefix(name, pluginContributionPrefix) {
		return false
	}
	want := strings.TrimPrefix(name, pluginContributionPrefix)
	for _, p := range s.analyzerPluginsLocked() {
		if p.Manifest.Name == want {
			return true
		}
	}
	return false
}

type pluginAnchor struct {
	Plugin   string
	Identity string
	Value    analyzerplugin.Anchor
}

type pluginEnrichment struct {
	Plugin   string
	API      string
	Identity string
	Value    analyzerplugin.Enrichment
}

func (s *session) prepareAnalyzerPlugins(ctx context.Context, input *runtimeInputs, files, allNames []string, hashes map[string]string) error {
	return s.prepareAnalyzerPluginsWith(ctx, input, files, allNames, hashes, true)
}

// prepareAnalyzerPluginsWith runs plugin plan/units. When reload is false the
// caller already loaded plugins and froze owner_domain holds from that exact
// identity; a second reload would allow a mid-run manifest race.
func (s *session) prepareAnalyzerPluginsWith(ctx context.Context, input *runtimeInputs, files, allNames []string, hashes map[string]string, reload bool) error {
	if reload {
		if err := s.reloadAnalyzerPlugins(); err != nil {
			return err
		}
	}
	// Isolate plugin captures from the concurrent TS readers of capturedSources.
	s.pluginCapturedSources = map[string][]byte{}
	s.pluginPreviewFences = nil
	s.pluginDeltaOwners = nil
	s.pluginResOwners = nil
	s.pluginEnrichments = nil
	s.pluginScopeIncomplete = false
	loadedPlugins := s.analyzerPluginsLocked()
	configured := map[string]analyzerplugin.Loaded{}
	configuredNames := make([]string, 0, len(loadedPlugins))
	for _, p := range loadedPlugins {
		configured[p.Manifest.Name] = p
		configuredNames = append(configuredNames, p.Manifest.Name)
	}
	sort.Strings(configuredNames)
	s.analyzerPluginStats.Plugins = len(configured)
	previous := map[string]analyzerplugin.PluginRecord{}
	if s.state != nil {
		for name, rec := range s.state.AnalyzerPlugins {
			previous[name] = analyzerplugin.ClonePluginRecord(rec)
		}
	}
	deltaOwners := map[string]bool{}
	nameDelta := map[string]bool{}
	admittedOwners := admittedOwnerSet(files, hashes)
	planFilesDigest := nameSetDigest(pluginVisibleNames(s.eng, allNames))
	verify := s.opts.PluginVerify
	next := make(map[string]analyzerplugin.PluginRecord, len(configured))
	var verifyMismatches []string
	for _, name := range configuredNames {
		p := configured[name]
		old := previous[name]
		if old.Units == nil {
			old.Units = map[string]analyzerplugin.UnitRecord{}
		}
		identityOK := old.Identity == p.Identity && old.RuntimeDigest == p.RuntimeDigest
		planObsValid := old.PlanKnown && observationsValid(old.PlanObservations, s.abs, s.eng, files, hashes, input.tsContext, old.Units)
		planNeedsRefresh := !identityOK || !old.PlanKnown || old.PlanFilesDigest != planFilesDigest || !planObsValid
		if !planNeedsRefresh {
			if err := analyzerplugin.ValidatePlan(old.Plan); err != nil {
				return fmt.Errorf("cached analyzer plugin %q plan: %w", name, err)
			}
		}
		plan := old.Plan
		planObs := old.PlanObservations
		planMetadataChanged := !old.PlanKnown || old.PlanFilesDigest != planFilesDigest
		unitValid := map[string]bool{}
		if identityOK {
			for id, u := range old.Units {
				unitValid[id] = observationsValid(u.Observations, s.abs, s.eng, files, hashes, input.tsContext, old.Units)
			}
		}
		needSpawn := planNeedsRefresh || verify
		for _, u := range plan {
			rec, ok := old.Units[u.ID]
			if !ok || !unitValid[u.ID] || !analyzerplugin.UnitDeclsEqual(rec.Decl, u) {
				unitValid[u.ID] = false
				needSpawn = true
			}
		}
		if !needSpawn {
			s.analyzerPluginStats.UnitsPlanned += len(plan)
			s.analyzerPluginStats.UnitsReused += len(plan)
			next[name] = old
			continue
		}
		client, err := analyzerplugin.Start(ctx, p, nil)
		if err != nil {
			return err
		}
		s.analyzerPluginStats.ProcessesStarted++
		pluginObs := map[string]analyzerplugin.Observation{}
		callback := func(cbctx context.Context, msg map[string]any) (map[string]any, error) {
			return s.pluginCallback(cbctx, p, msg, pluginPlanUnit, pluginObs, input, files, allNames, hashes, nil, nil)
		}
		if planNeedsRefresh {
			units, err := client.Plan(ctx, planFilesDigest, callback)
			if err != nil {
				client.Abort()
				return fmt.Errorf("plugin %q plan: %w", name, err)
			}
			s.analyzerPluginStats.PlansRun++
			plan = units
			planObs = pluginObs[pluginPlanUnit]
			planMetadataChanged = planMetadataChanged || !reflect.DeepEqual(old.PlanObservations, planObs)
			// Keep prior unit records whose observations remain valid, whose
			// refreshed declarations still match, and that still appear in the
			// plan. Inventory membership changes must not discard unrelated caches.
			kept := map[string]analyzerplugin.UnitRecord{}
			planned := map[string]analyzerplugin.UnitDecl{}
			for _, u := range plan {
				planned[u.ID] = u
			}
			for id, rec := range old.Units {
				decl, ok := planned[id]
				if ok && unitValid[id] && analyzerplugin.UnitDeclsEqual(rec.Decl, decl) {
					kept[id] = rec
					continue
				}
				if verify && !ok {
					verifyMismatches = append(verifyMismatches, name+":"+id+" (removed from plan)")
				}
				// Removed or invalidated units contribute their old owners to P_pl
				// and their names to P_res.
				addPluginOwnerKeys(deltaOwners, rec.Owners)
				for n := range pluginOwnerResultNames(rec.Owners) {
					nameDelta[n] = true
				}
			}
			old.Units = kept
			unitValid = map[string]bool{}
			for id, rec := range old.Units {
				unitValid[id] = observationsValid(rec.Observations, s.abs, s.eng, files, hashes, input.tsContext, old.Units)
			}
			old.PlanKnown = true
		}
		ordered, err := analyzerplugin.TopologicalPlan(plan)
		if err != nil {
			client.Abort()
			return fmt.Errorf("plugin %q plan: %w", name, err)
		}
		s.analyzerPluginStats.UnitsPlanned += len(ordered)
		summaries := map[string]any{}
		for id, rec := range old.Units {
			if len(rec.Summary) > 0 {
				var v any
				if json.Unmarshal(rec.Summary, &v) == nil {
					summaries[id] = v
				}
			}
		}
		summaryChanged := map[string]bool{}
		// A plan refresh alone is not a graph change when every unit remains
		// reusable. Touch only when identity, plan membership, or unit output
		// actually moves.
		pluginTouched := !identityOK || planMembershipChanged(old.Plan, plan)
		stagedUnits := map[string]analyzerplugin.UnitRecord{}
		for id, rec := range old.Units {
			stagedUnits[id] = rec
		}
		for _, unit := range ordered {
			rec, hasRec := stagedUnits[unit.ID]
			runUnit := verify || !unitValid[unit.ID] || !hasRec || !analyzerplugin.UnitDeclsEqual(rec.Decl, unit)
			for _, dep := range unit.Consumes {
				if summaryChanged[dep] {
					runUnit = true
				}
			}
			if !runUnit {
				s.analyzerPluginStats.UnitsReused++
				continue
			}
			pluginTouched = true
			// Prefer the original previous record (not the possibly narrowed
			// staged copy) so exclude/re-include can restore owners.
			previousUnit, hadPrevious := previous[name].Units[unit.ID]
			if !hadPrevious {
				previousUnit, hadPrevious = stagedUnits[unit.ID]
			}
			unitObs := map[string]analyzerplugin.Observation{}
			allowedSummaries := make(map[string]bool, len(unit.Consumes))
			for _, dep := range unit.Consumes {
				allowedSummaries[dep] = true
			}
			callback := func(cbctx context.Context, msg map[string]any) (map[string]any, error) {
				return s.pluginCallback(cbctx, p, msg, unit.ID, unitObs, input, files, allNames, hashes, summaries, allowedSummaries)
			}
			declared := analyzerplugin.DeclaredSummaries([]analyzerplugin.UnitDecl{unit}, summaries)
			results, err := client.Run(ctx, []analyzerplugin.UnitDecl{unit}, declared, callback)
			if err != nil {
				client.Abort()
				return fmt.Errorf("plugin %q unit %q: %w", name, unit.ID, err)
			}
			if len(results) != 1 {
				client.Abort()
				return fmt.Errorf("plugin %q unit %q returned %d results", name, unit.ID, len(results))
			}
			var output analyzerplugin.UnitResult
			b, _ := json.Marshal(results[0])
			if err := json.Unmarshal(b, &output); err != nil {
				client.Abort()
				return fmt.Errorf("plugin %q unit %q result: %w", name, unit.ID, err)
			}
			output = analyzerplugin.CanonicalizeResult(output)
			if err := analyzerplugin.ValidateResult(p, unit, output); err != nil {
				client.Abort()
				return err
			}
			s.analyzerPluginStats.UnitsExecuted++
			for owner := range output.Owners {
				if !containsSlashPath(files, owner) {
					client.Abort()
					return fmt.Errorf("plugin %q unit %q wrote owner %q outside the captured graph inventory", name, unit.ID, owner)
				}
			}
			summaryBytes, err := json.Marshal(output.Summary)
			if err != nil {
				client.Abort()
				return err
			}
			canonical, err := analyzerplugin.CanonicalDigest(output)
			if err != nil {
				client.Abort()
				return err
			}
			if verify {
				if !hadPrevious || previousUnit.OutputDigest == "" {
					verifyMismatches = append(verifyMismatches, name+":"+unit.ID+" (no cached digest)")
				} else if previousUnit.OutputDigest != canonical {
					verifyMismatches = append(verifyMismatches, name+":"+unit.ID)
				}
			}
			summaryChanged[unit.ID] = !hadPrevious || string(previousUnit.Summary) != string(summaryBytes)
			if !verify {
				owners := output.Owners
				if owners == nil {
					owners = map[string]analyzerplugin.OwnerResult{}
				}
				// Preserve previously cached owners that are not admitted this
				// run so an exclude/re-include cycle cannot reuse empty output.
				if hadPrevious {
					for owner, result := range previousUnit.Owners {
						key := filepath.ToSlash(owner)
						if admittedOwners[key] {
							continue
						}
						if _, exists := owners[owner]; !exists {
							if owners == nil {
								owners = map[string]analyzerplugin.OwnerResult{}
							}
							owners[owner] = result
						}
					}
				}
				stagedUnits[unit.ID] = analyzerplugin.UnitRecord{Decl: unit, Observations: unitObs[unit.ID], Owners: owners, Summary: summaryBytes, OutputDigest: canonical}
				addPluginOwnerKeys(deltaOwners, previousUnit.Owners)
				addPluginOwnerKeys(deltaOwners, owners)
				for n := range changedResolutionCandidateNames(pluginOwnerResultFacts(name, p.Manifest.API, previousUnit.Owners), pluginOwnerResultFacts(name, p.Manifest.API, owners)) {
					nameDelta[n] = true
				}
			}
			summaries[unit.ID] = output.Summary
		}
		closeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		if err := client.Close(closeCtx); err != nil {
			cancel()
			return fmt.Errorf("plugin %q shutdown: %w", name, err)
		}
		cancel()
		// Keep excluded owners in the persisted unit record so a later
		// re-inclusion can reuse complete contributions. Graph emission filters
		// by current admission separately.
		old.Units = stagedUnits
		old.Identity = p.Identity
		old.RuntimeDigest = p.RuntimeDigest
		old.Plan = plan
		old.PlanKnown = true
		old.PlanFilesDigest = planFilesDigest
		old.PlanObservations = planObs
		next[name] = old
		if planMetadataChanged && !pluginTouched && !verify {
			s.pluginPlanRefresh = true
		}
		if pluginTouched && !verify {
			s.pluginChanged = true
		}
	}
	for name, rec := range previous {
		if _, ok := configured[name]; !ok {
			if verify {
				verifyMismatches = append(verifyMismatches, name+" (removed from config)")
			}
			s.pluginChanged = true
			for _, unit := range rec.Units {
				addPluginOwnerKeys(deltaOwners, unit.Owners)
				for n := range pluginOwnerResultNames(unit.Owners) {
					nameDelta[n] = true
				}
			}
		}
	}
	if len(verifyMismatches) > 0 {
		sort.Strings(verifyMismatches)
		return fmt.Errorf("plugin verification mismatch for units: %s", strings.Join(verifyMismatches, ", "))
	}
	if verify {
		s.pluginVerifyOnly = true
	}
	s.nextAnalyzerPlugins = next
	if err := s.collectPluginContributions(next, admittedOwners); err != nil {
		return err
	}
	s.pluginDeltaOwners = sortedOwnerKeys(deltaOwners)
	if s.state != nil && len(nameDelta) > 0 {
		s.pluginResOwners = ownersForChangedCandidateNames(s.state.Files, nameDelta)
		if pluginResolutionIndexIncomplete(s.state.Files, previous) {
			s.pluginScopeIncomplete = true
		}
	}
	if s.pluginChanged && !verify {
		if s.pluginScopeIncomplete {
			s.extraFallbacks = append(s.extraFallbacks, graphFallback("repository analyzer plugin units changed; resolution name indexes incomplete, using whole domain"))
		} else {
			s.extraFallbacks = append(s.extraFallbacks, graphFallback("repository analyzer plugin units changed; replacing plugin and resolution-affected owners"))
		}
	}
	return nil
}

// reloadAnalyzerPlugins revalidates manifest, bundle, runtime identity and trust
// for every transaction so resident watches cannot reuse stale plugin outputs.
func (s *session) reloadAnalyzerPlugins() error {
	if s == nil {
		return nil
	}
	entries := s.boundAnalyzerPluginConfigs()
	if len(entries) == 0 {
		s.setAnalyzerPlugins(nil, nil)
		return nil
	}
	cache := s.runtimeFingerprints
	if cache == nil {
		cache = analyzerplugin.RuntimeCache{}
		s.runtimeFingerprints = cache
	}
	loaded, err := analyzerplugin.Load(s.abs, entries, cache)
	if err != nil {
		return err
	}
	for _, p := range loaded {
		if !analyzerplugin.Trusted(s.opts.AllowRepoPlugins, p.Manifest.Name) {
			return fmt.Errorf("repository analyzer plugin %q is not trusted; rerun with --allow-repo-plugins %s", p.Manifest.Name, p.Manifest.Name)
		}
	}
	s.setAnalyzerPlugins(append([]analyzerplugin.Config(nil), entries...), loaded)
	return nil
}

// boundAnalyzerPluginConfigs uses the live engine registration list whenever the
// engine config exists, including an explicit empty list that retires plugins.
// Fallback to open-time registrations applies only when the engine is unset.
func (s *session) boundAnalyzerPluginConfigs() []analyzerplugin.Config {
	if s.eng != nil && s.eng.Config() != nil {
		return append([]analyzerplugin.Config(nil), s.eng.Config().AnalyzerPlugins...)
	}
	if len(s.opts.analyzerPluginConfigs) > 0 {
		return append([]analyzerplugin.Config(nil), s.opts.analyzerPluginConfigs...)
	}
	out := make([]analyzerplugin.Config, 0, len(s.opts.analyzerPlugins))
	for _, p := range s.opts.analyzerPlugins {
		out = append(out, p.Config)
	}
	return out
}

func (s *session) setAnalyzerPlugins(configs []analyzerplugin.Config, loaded []analyzerplugin.Loaded) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.opts.analyzerPluginConfigs = configs
	s.opts.analyzerPlugins = loaded
}

func (s *session) analyzerPluginsLocked() []analyzerplugin.Loaded {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]analyzerplugin.Loaded(nil), s.opts.analyzerPlugins...)
}

// freezePluginOwnerHoldSnapshot captures the configured plugins used for
// owner_domain holds for the rest of this run so concurrent reload cannot race
// hold decisions during cold streaming.
func (s *session) freezePluginOwnerHoldSnapshot(plugins []analyzerplugin.Loaded) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pluginHoldSnapshot = append([]analyzerplugin.Loaded(nil), plugins...)
	s.pluginHoldOwners = pluginOwnerDomainHold(plugins)
}

// admittedOwnerSet is the authoritative admitted graph-input owner domain for
// plugin contribution emission. It unions inventory-admitted names (including
// NameOnly media that retains membership without content hashes) with the
// ordinary content-hash map. Deletions and policy-excluded paths stay absent.
func admittedOwnerSet(files []string, hashes map[string]string) map[string]bool {
	out := map[string]bool{}
	for _, f := range files {
		out[filepath.ToSlash(f)] = true
	}
	for f := range hashes {
		out[filepath.ToSlash(f)] = true
	}
	return out
}

func planMembershipChanged(previous, next []analyzerplugin.UnitDecl) bool {
	if len(previous) != len(next) {
		return true
	}
	prevIDs := make([]string, 0, len(previous))
	nextIDs := make([]string, 0, len(next))
	for _, u := range previous {
		prevIDs = append(prevIDs, u.ID)
	}
	for _, u := range next {
		nextIDs = append(nextIDs, u.ID)
	}
	sort.Strings(prevIDs)
	sort.Strings(nextIDs)
	for i := range prevIDs {
		if prevIDs[i] != nextIDs[i] {
			return true
		}
	}
	return false
}

func addPluginOwnerKeys(dst map[string]bool, owners map[string]analyzerplugin.OwnerResult) {
	for owner, result := range owners {
		owner = filepath.ToSlash(owner)
		if owner != "" {
			dst[owner] = true
		}
		for _, anchor := range result.Anchors {
			anchorOwner := filepath.ToSlash(anchor.Owner)
			if anchorOwner == "" {
				anchorOwner = owner
			}
			if anchorOwner != "" {
				dst[anchorOwner] = true
			}
		}
		for _, enrichment := range result.Enrichments {
			enrichmentOwner := filepath.ToSlash(enrichment.Owner)
			if enrichmentOwner == "" {
				enrichmentOwner = owner
			}
			if enrichmentOwner != "" {
				dst[enrichmentOwner] = true
			}
		}
	}
}

func sortedOwnerKeys(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for owner := range set {
		out = append(out, owner)
	}
	sort.Strings(out)
	return out
}

func pluginOwnerResultNames(owners map[string]analyzerplugin.OwnerResult) map[string]bool {
	names := map[string]bool{}
	for _, result := range owners {
		for _, node := range result.Nodes {
			if node.Name != "" {
				names[node.Name] = true
			}
			for _, rel := range node.Relations {
				if rel.Target != "" {
					names[rel.Target] = true
				}
			}
		}
		for _, anchor := range result.Anchors {
			for _, rel := range anchor.Relations {
				if rel.Target != "" {
					names[rel.Target] = true
				}
			}
		}
		for _, enrichment := range result.Enrichments {
			if enrichment.Name != "" {
				names[enrichment.Name] = true
			}
			for _, rel := range enrichment.Relations {
				if rel.Target != "" {
					names[rel.Target] = true
				}
			}
		}
	}
	return names
}

func pluginOwnerResultFacts(pluginName, api string, owners map[string]analyzerplugin.OwnerResult) []facts.Fact {
	var out []facts.Fact
	for owner, result := range owners {
		owner = filepath.ToSlash(owner)
		for _, node := range result.Nodes {
			rels := pluginFactsRelations(pluginName, api, node.Relations)
			out = append(out, facts.Fact{Kind: pluginFactKind(pluginName, api, node.Kind), Name: node.Name, File: owner, Line: node.Line, EndLine: node.EndLine, Relations: rels})
		}
		for _, anchor := range result.Anchors {
			anchorOwner := filepath.ToSlash(anchor.Owner)
			if anchorOwner == "" {
				anchorOwner = owner
			}
			for _, rel := range anchor.Relations {
				out = append(out, facts.Fact{
					Kind: facts.KindSymbol, Name: anchor.Symbol, File: anchorOwner, Line: anchor.Line, EndLine: anchor.EndLine,
					Relations: pluginAnchorRelations(pluginName, api, []analyzerplugin.Relation{rel}),
				})
			}
		}
		for _, enrichment := range result.Enrichments {
			enrichmentOwner := filepath.ToSlash(enrichment.Owner)
			if enrichmentOwner == "" {
				enrichmentOwner = owner
			}
			out = append(out, facts.Fact{
				Kind: pluginEnrichmentTargetKind(pluginName, api, enrichment.Kind), Name: enrichment.Name, File: enrichmentOwner,
				Relations: pluginFactsRelations(pluginName, api, enrichment.Relations),
			})
		}
	}
	return out
}

func ownerRefsForPaths(paths []string) []graphstream.OwnerRef {
	out := make([]graphstream.OwnerRef, 0, len(paths))
	seen := map[string]bool{}
	for _, path := range paths {
		path = filepath.ToSlash(path)
		if path == "" || seen[path] {
			continue
		}
		seen[path] = true
		out = append(out, graphstream.OwnerRef{Kind: graphstream.OwnerFile, ID: path})
	}
	return out
}

func (s *session) analyzerPluginReplacementOwners(currentFiles []string) []graphstream.OwnerRef {
	if s == nil {
		return nil
	}
	// Always keep proven P_pl ∪ P_res seeds. Incomplete-index whole-domain
	// fallback widens with semantic + prior owners without dropping those seeds,
	// so newly admitted NameOnly/unhashed plugin owners remain in scope.
	paths := append([]string(nil), s.pluginDeltaOwners...)
	paths = append(paths, s.pluginResOwners...)
	if s.pluginScopeIncomplete {
		paths = append(paths, graphSemanticNames(s.eng, currentFiles)...)
		if s.state != nil {
			for path := range s.state.Files {
				paths = append(paths, filepath.ToSlash(path))
			}
		}
	}
	return ownerRefsForPaths(paths)
}

func pluginResolutionIndexIncomplete(files map[string]*FileState, previous map[string]analyzerplugin.PluginRecord) bool {
	if len(previous) == 0 {
		return false
	}
	// Retain plugin identity: every prior (owner, plugin) pair must present the
	// exact contribution key plugin:<name>. Another plugin's index on the same
	// owner must not make P_res appear complete.
	expected := map[string]map[string]bool{}
	for pluginName, rec := range previous {
		for _, unit := range rec.Units {
			owners := map[string]bool{}
			addPluginOwnerKeys(owners, unit.Owners)
			for owner := range owners {
				if expected[owner] == nil {
					expected[owner] = map[string]bool{}
				}
				expected[owner][pluginName] = true
			}
		}
	}
	if len(expected) == 0 {
		return false
	}
	for owner, plugins := range expected {
		st := files[owner]
		if st == nil {
			return true
		}
		for pluginName := range plugins {
			if _, ok := st.Contrib["plugin:"+pluginName]; !ok {
				return true
			}
		}
	}
	return false
}

func filterUnitOwnersToAdmitted(units map[string]analyzerplugin.UnitRecord, admitted map[string]bool) {
	for id, rec := range units {
		if len(rec.Owners) == 0 {
			continue
		}
		filtered := map[string]analyzerplugin.OwnerResult{}
		for owner, result := range rec.Owners {
			if admitted[filepath.ToSlash(owner)] {
				filtered[owner] = result
			}
		}
		rec.Owners = filtered
		units[id] = rec
	}
}

func pIdentity(p analyzerplugin.Loaded) string { return p.Identity }

func (s *session) waitAnalyzerPlugins() error {
	if s == nil {
		return nil
	}
	if s.pluginPrepareDone != nil {
		err := <-s.pluginPrepareDone
		s.pluginPrepareDone = nil
		if err != nil {
			return err
		}
	}
	if err := s.mergePluginCapturedSources(); err != nil {
		return err
	}
	return s.revalidateCapturedInputs("refusing analyzer plugin output built from superseded inputs")
}

// mergePluginCapturedSources publishes plugin-isolated captures into the shared
// session map after the concurrent prepare goroutine has finished.
func (s *session) mergePluginCapturedSources() error {
	if s == nil || len(s.pluginCapturedSources) == 0 && len(s.pluginPreviewFences) == 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.capturedSources == nil {
		s.capturedSources = map[string][]byte{}
	}
	for path, data := range s.pluginCapturedSources {
		if prior, exists := s.capturedSources[path]; exists && !stringEqual(prior, data) {
			return fmt.Errorf("plugin read %q after another captured version", path)
		}
		s.capturedSources[path] = append([]byte(nil), data...)
	}
	if len(s.pluginPreviewFences) > 0 {
		s.previewFences = append(s.previewFences, s.pluginPreviewFences...)
		s.pluginPreviewFences = nil
	}
	s.pluginCapturedSources = nil
	return nil
}

// inputSource returns a byte capture from the immutable runtime-input snapshot.
// The session-level capturedSources map is intentionally excluded because TS
// dependency expansion may mutate it while cold plugin preparation runs.
func inputSource(input *runtimeInputs, path string) ([]byte, bool) {
	if input == nil {
		return nil, false
	}
	source, ok := input.sources[path]
	return source, ok
}

func pluginOwnerDomainHold(plugins []analyzerplugin.Loaded) map[string]bool {
	if len(plugins) == 0 {
		return nil
	}
	// Non-nil map enables lazy per-path owner_domain matching in pluginOwnerHeld.
	return map[string]bool{}
}

func (s *session) pluginOwnerHeld(path string) bool {
	if s == nil {
		return false
	}
	path = filepath.ToSlash(path)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pluginHoldOwners == nil {
		return false
	}
	if held, ok := s.pluginHoldOwners[path]; ok {
		return held
	}
	held := false
	for _, p := range s.pluginHoldSnapshot {
		for _, glob := range p.Manifest.OwnerDomain {
			if analyzerplugin.MatchRepositoryGlob(glob, path) {
				held = true
				break
			}
		}
		if held {
			break
		}
	}
	s.pluginHoldOwners[path] = held
	return held
}

func graphFallback(reason string) (fallback graphstream.Fallback) {
	return graphstream.Fallback{Extractor: "analyzer_plugins", Scope: "all prior/current file owners", Reason: reason}
}

func graphPolicy(eng *engine.Engine) *graphinput.Policy {
	if eng == nil || eng.GraphScope() == nil {
		return nil
	}
	return eng.GraphScope().Policy
}

func pathNowExcluded(policy *graphinput.Policy, rel string) bool {
	if graphinput.IsLockfile(rel) {
		return true
	}
	if policy == nil {
		return false
	}
	return policy.ClassifyDependency(rel).Kind == graphinput.Excluded
}

func observationsValid(obs analyzerplugin.Observation, root string, eng *engine.Engine, files []string, hashes, tsContext map[string]string, records map[string]analyzerplugin.UnitRecord) bool {
	policy := graphPolicy(eng)
	if obs.Policy != "" && (policy == nil || obs.Policy != policy.Identity()) {
		return false
	}
	for rel, want := range obs.Reads {
		if want == "NOT_ADMITTED" {
			if policy == nil || obs.Policy != policy.Identity() {
				return false
			}
			continue
		}
		if want == "MISSING" {
			if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel))); !os.IsNotExist(err) {
				return false
			}
			continue
		}
		// Admitted reads must remain admitted under the current policy.
		if pathNowExcluded(policy, rel) {
			return false
		}
		if got, ok := hashes[rel]; ok {
			if got != want {
				return false
			}
			continue
		}
		b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			return false
		}
		sum := sha256.Sum256(b)
		if hex.EncodeToString(sum[:]) != want {
			return false
		}
	}
	for rel, want := range obs.Probes {
		if want == "NOT_ADMITTED" {
			if policy == nil || obs.Policy != policy.Identity() {
				return false
			}
			continue
		}
		if pathNowExcluded(policy, rel) {
			return false
		}
		_, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel)))
		got := "exists"
		if os.IsNotExist(err) {
			got = "absent"
		} else if err != nil {
			return false
		}
		if got != want {
			return false
		}
	}
	for glob, want := range obs.Lists {
		if glob == pluginResolverInventory {
			if nameSetDigest(pluginVisibleNames(eng, files)) != want {
				return false
			}
			continue
		}
		var matched []string
		for _, name := range files {
			if !graphinput.IsLockfile(name) && pluginGlob(glob, name) {
				matched = append(matched, name)
			}
		}
		if nameSetDigest(matched) != want {
			return false
		}
	}
	if want, ok := obs.Resolves[pluginResolverContext]; ok && stringMapDigest(tsContext) != want {
		return false
	}
	for dep, want := range obs.Summaries {
		record, ok := records[dep]
		if !ok || len(record.Summary) == 0 {
			return false
		}
		var value any
		if json.Unmarshal(record.Summary, &value) != nil {
			return false
		}
		digest, err := analyzerplugin.CanonicalDigest(value)
		if err != nil || digest != want {
			return false
		}
	}
	return true
}

func (s *session) pluginCallback(ctx context.Context, p analyzerplugin.Loaded, msg map[string]any, defaultUnit string, obs map[string]analyzerplugin.Observation, input *runtimeInputs, files, allNames []string, hashes map[string]string, summaries map[string]any, allowedSummaries map[string]bool) (map[string]any, error) {
	// Host owns the active unit. A callback that claims another unit would
	// otherwise park observations outside the record that is persisted.
	if claimed, ok := msg["unit"].(string); ok && claimed != "" && claimed != defaultUnit {
		return nil, fmt.Errorf("plugin callback unit %q does not match active unit %q", claimed, defaultUnit)
	}
	unit := defaultUnit
	current := obs[unit]
	if current.Reads == nil {
		current.Reads = map[string]string{}
	}
	if current.Probes == nil {
		current.Probes = map[string]string{}
	}
	if current.Lists == nil {
		current.Lists = map[string]string{}
	}
	if current.Resolves == nil {
		current.Resolves = map[string]string{}
	}
	if current.Summaries == nil {
		current.Summaries = map[string]string{}
	}
	if msg["op"] == "list" {
		glob, ok := msg["glob"].(string)
		if !ok || glob == "" {
			return nil, errors.New("list callback requires glob")
		}
		if err := analyzerplugin.ValidateRepositoryGlob(glob); err != nil {
			return nil, fmt.Errorf("list callback glob: %w", err)
		}
		names := make([]string, 0)
		for _, f := range files {
			if !graphinput.IsLockfile(f) && pluginGlob(glob, f) {
				names = append(names, f)
			}
		}
		sort.Strings(names)
		digest := nameSetDigest(names)
		current.Lists[glob] = digest
		obs[unit] = current
		return map[string]any{"paths": names}, nil
	}
	pathArg := ""
	if msg["op"] == "read" || msg["op"] == "probe" {
		pathArg, _ = msg["path"].(string)
		pathArg = filepath.ToSlash(filepath.Clean(pathArg))
		if pathArg == "." || pathArg == ".." || strings.HasPrefix(pathArg, "../") || filepath.IsAbs(pathArg) || strings.Contains(pathArg, "\\") {
			return nil, fmt.Errorf("plugin callback path %q escapes repository", pathArg)
		}
		decision := graphinput.Decision{Kind: graphinput.Excluded, Reason: "no graph input policy"}
		if graphinput.IsLockfile(pathArg) {
			decision = graphinput.Decision{Kind: graphinput.Excluded, Known: true, Reason: "dependency lockfile is not a graph input"}
		} else if scope := s.eng.GraphScope(); scope != nil && scope.Policy != nil {
			decision = scope.Policy.ClassifyDependency(pathArg)
		} else {
			decision = graphinput.Decision{Kind: graphinput.Semantic, Known: true}
		}
		// Bind every classified path observation to the policy identity so a
		// later exclusion invalidates the unit even when bytes are unchanged.
		if current.Policy == "" && input != nil {
			current.Policy = input.policyIdentity
		}
		if decision.Kind == graphinput.Excluded {
			switch msg["op"] {
			case "read":
				current.Reads[pathArg] = "NOT_ADMITTED"
				obs[unit] = current
				return map[string]any{"missing": true, "not_admitted": true}, nil
			default:
				current.Probes[pathArg] = "NOT_ADMITTED"
				obs[unit] = current
				return map[string]any{"exists": false, "not_admitted": true}, nil
			}
		}
	}
	full, err := confinedRepositoryInputPath(s.abs, pathArg)
	if err != nil {
		return nil, fmt.Errorf("plugin callback path %q: %w", pathArg, err)
	}
	switch msg["op"] {
	case "read":
		var data []byte
		// prepareAnalyzerPlugins may run concurrently with TypeScript dependency
		// expansion, which grows capturedSources. Read the immutable runtime
		// input/config snapshots here instead of that shared, growing map.
		if prior, ok := inputSource(input, pathArg); ok {
			data = append([]byte(nil), prior...)
		} else if prior, ok := s.cfgCaptured[pathArg]; ok {
			data = append([]byte(nil), prior...)
		} else if prior, ok := s.pluginCapturedSources[pathArg]; ok {
			data = append([]byte(nil), prior...)
		} else {
			var err error
			data, err = os.ReadFile(full)
			if os.IsNotExist(err) {
				current.Reads[pathArg] = "MISSING"
				obs[unit] = current
				s.pluginPreviewFences = append(s.pluginPreviewFences, func() error {
					_, later := os.Stat(full)
					if !os.IsNotExist(later) {
						return fmt.Errorf("%w: plugin input %s appeared during analysis", ErrInputsChanged, pathArg)
					}
					return nil
				})
				return map[string]any{"missing": true}, nil
			}
			if err != nil {
				return nil, err
			}
		}
		h := sha256.Sum256(data)
		digest := hex.EncodeToString(h[:])
		// Inventory-captured paths must match the run's hash snapshot so plugin
		// facts cannot commit beside stale reused TypeScript facts.
		if want, ok := hashes[pathArg]; ok && want != "" && want != digest {
			return nil, fmt.Errorf("%w: plugin read %s diverged from inventory snapshot", ErrInputsChanged, pathArg)
		}
		current.Reads[pathArg] = digest
		obs[unit] = current
		if s.pluginCapturedSources == nil {
			s.pluginCapturedSources = map[string][]byte{}
		}
		if prior, exists := s.pluginCapturedSources[pathArg]; exists && !stringEqual(prior, data) {
			return nil, fmt.Errorf("plugin read %q after another captured version", pathArg)
		}
		s.pluginCapturedSources[pathArg] = append([]byte(nil), data...)
		return map[string]any{"text": string(data)}, nil
	case "probe":
		_, err := os.Stat(full)
		exists := err == nil
		if err != nil && !os.IsNotExist(err) {
			return nil, err
		}
		answer := "absent"
		if exists {
			answer = "exists"
		}
		current.Probes[pathArg] = answer
		obs[unit] = current
		expected := exists
		s.pluginPreviewFences = append(s.pluginPreviewFences, func() error {
			_, later := os.Stat(full)
			same := later == nil
			if os.IsNotExist(later) {
				same = false
			} else if later != nil {
				return later
			}
			if same != expected {
				return fmt.Errorf("%w: plugin probe %s changed during analysis", ErrInputsChanged, pathArg)
			}
			return nil
		})
		return map[string]any{"exists": exists}, nil
	case "list":
		return nil, errors.New("list callback must not reach path handler")
	case "summary":
		id, _ := msg["target"].(string)
		if id == "" {
			id, _ = msg["id"].(string)
		}
		if !allowedSummaries[id] {
			return nil, fmt.Errorf("unit %q accessed undeclared summary %q", unit, id)
		}
		if summaries == nil {
			return nil, fmt.Errorf("summary %q is unavailable", id)
		}
		value, exists := summaries[id]
		if !exists {
			return nil, fmt.Errorf("summary %q is not a settled declared dependency", id)
		}
		digest, err := analyzerplugin.CanonicalDigest(value)
		if err != nil {
			return nil, err
		}
		current.Summaries[id] = digest
		obs[unit] = current
		return map[string]any{"value": value}, nil
	case "resolve_module", "resolve_export":
		if input == nil || input.tsDiscovery == nil {
			return nil, errors.New("TypeScript host resolver is unavailable")
		}
		current.Lists[pluginResolverInventory] = nameSetDigest(pluginVisibleNames(s.eng, files))
		current.Resolves[pluginResolverContext] = stringMapDigest(input.tsContext)
		switch msg["op"] {
		case "resolve_module":
			from, ok := msg["from"].(string)
			if !ok || from == "" {
				return nil, errors.New("resolve_module callback requires from")
			}
			spec, ok := msg["spec"].(string)
			if !ok || spec == "" {
				return nil, errors.New("resolve_module callback requires spec")
			}
			from = filepath.ToSlash(filepath.Clean(from))
			if from == "." || from == ".." || strings.HasPrefix(from, "../") || filepath.IsAbs(from) || strings.Contains(from, "\\") {
				return nil, fmt.Errorf("resolve_module source %q escapes repository", from)
			}
			resolved := input.tsDiscovery.ResolvePluginModule(from, spec, files)
			obs[unit] = current
			for _, candidate := range resolved.Candidates {
				if _, err := s.pluginCallback(ctx, p, map[string]any{"op": "probe", "unit": unit, "path": candidate}, defaultUnit, obs, input, files, allNames, hashes, summaries, allowedSummaries); err != nil {
					return nil, err
				}
			}
			current = obs[unit]
			answer := map[string]any{"resolved": resolved.Resolved, "file": resolved.File, "module_dir": resolved.ModuleDir, "replay_spec": resolved.ReplaySpec, "external": resolved.External, "candidates": resolved.Candidates, "found": resolved.File != ""}
			key, _ := analyzerplugin.CanonicalDigest(map[string]any{"op": "resolve_module", "from": from, "spec": spec})
			current.Resolves[key], _ = analyzerplugin.CanonicalDigest(answer)
			obs[unit] = current
			return answer, nil
		case "resolve_export":
			file, ok := msg["file"].(string)
			if !ok || file == "" {
				return nil, errors.New("resolve_export callback requires file")
			}
			name, ok := msg["name"].(string)
			if !ok || name == "" {
				return nil, errors.New("resolve_export callback requires name")
			}
			file = filepath.ToSlash(filepath.Clean(file))
			if file == "." || file == ".." || strings.HasPrefix(file, "../") || filepath.IsAbs(file) || strings.Contains(file, "\\") {
				return nil, fmt.Errorf("resolve_export file %q escapes repository", file)
			}
			var readErr error
			readSource := func(rel string) []byte {
				if readErr != nil {
					return nil
				}
				value, err := s.pluginCallback(ctx, p, map[string]any{"op": "read", "unit": unit, "path": rel}, defaultUnit, obs, input, files, allNames, hashes, summaries, allowedSummaries)
				if err != nil {
					readErr = err
					return nil
				}
				if text, ok := value["text"].(string); ok {
					return []byte(text)
				}
				return nil
			}
			target, targetFile := input.tsDiscovery.ResolvePluginExport(file, name, files, readSource, nil)
			if readErr != nil {
				return nil, readErr
			}
			answer := map[string]any{"target": target, "file": targetFile, "found": targetFile != ""}
			key, _ := analyzerplugin.CanonicalDigest(map[string]any{"op": "resolve_export", "file": file, "name": name})
			current = obs[unit]
			current.Lists[pluginResolverInventory] = nameSetDigest(pluginVisibleNames(s.eng, files))
			current.Resolves[pluginResolverContext] = stringMapDigest(input.tsContext)
			current.Resolves[key], _ = analyzerplugin.CanonicalDigest(answer)
			obs[unit] = current
			return answer, nil
		default:
			return nil, fmt.Errorf("unknown resolver callback %q", msg["op"])
		}
	case "log":
		return map[string]any{}, nil
	default:
		return nil, fmt.Errorf("unknown plugin callback %q", msg["op"])
	}
}

func (s *session) collectPluginContributions(records map[string]analyzerplugin.PluginRecord, admitted map[string]bool) error {
	s.pluginFacts = nil
	s.pluginAnchors = nil
	s.pluginEnrichments = nil
	s.pluginContribs = map[string][]facts.Fact{}
	type seenPluginIdentity struct {
		owner       string
		occurrences map[string]string
	}
	seenIdentities := map[string]*seenPluginIdentity{}
	ownerFiles := map[string]bool{}
	apiVersions := map[string]string{}
	for _, plugin := range s.analyzerPluginsLocked() {
		apiVersions[plugin.Manifest.Name] = plugin.Manifest.API
	}
	repoID := ""
	if s != nil {
		repoID = s.opts.RepoID
		if repoID == "" {
			repoID = filepath.Clean(s.abs)
		}
	}
	pluginNames := make([]string, 0, len(records))
	for pluginName := range records {
		pluginNames = append(pluginNames, pluginName)
	}
	sort.Strings(pluginNames)
	for _, pluginName := range pluginNames {
		record := records[pluginName]
		api := apiVersions[pluginName]
		unitIDs := make([]string, 0, len(record.Units))
		for id := range record.Units {
			unitIDs = append(unitIDs, id)
		}
		sort.Strings(unitIDs)
		for _, id := range unitIDs {
			unit := record.Units[id]
			owners := make([]string, 0, len(unit.Owners))
			for owner := range unit.Owners {
				owners = append(owners, owner)
			}
			sort.Strings(owners)
			for _, owner := range owners {
				owner = filepath.ToSlash(owner)
				// Emit only currently admitted owners; excluded owners remain in
				// the persisted unit record for a later re-inclusion.
				if admitted != nil && !admitted[owner] {
					continue
				}
				ownerFiles[owner] = true
				output := unit.Owners[owner]
				key := "plugin:" + pluginName
				for _, node := range output.Nodes {
					factKind := pluginFactKind(pluginName, api, node.Kind)
					identity := factKind + "\x00" + node.Name
					if api != analyzerplugin.GoAPIVersion {
						// Legacy Node plugins keep their owner-path-qualified fact identity.
						identity += "\x00" + filepath.ToSlash(owner)
					}
					occurrence := ""
					if node.Props != nil {
						if value, ok := node.Props["occurrence"]; ok && value != nil {
							occurrence = strings.TrimSpace(fmt.Sprint(value))
						}
					}
					seen := seenIdentities[identity]
					if seen == nil {
						seen = &seenPluginIdentity{owner: owner, occurrences: map[string]string{}}
						seenIdentities[identity] = seen
					} else if api == analyzerplugin.GoAPIVersion && seen.owner != owner {
						return fmt.Errorf("Go analyzer plugin %q emitted stable fact identity %s/%s for multiple owners %q and %q", pluginName, node.Kind, node.Name, seen.owner, owner)
					}
					if len(seen.occurrences) > 0 && (occurrence == "" || seen.occurrences[""] != "" || seen.occurrences[occurrence] != "") {
						return fmt.Errorf("analyzer plugins emitted duplicate fact identity %s/%s for owner %s without distinct occurrences", node.Kind, node.Name, owner)
					}
					seen.occurrences[occurrence] = id
					props := pluginOwnedProperties(api, pluginName, node.Props)
					props["plugin"] = pluginName
					props["plugin_identity"] = record.Identity
					rels := make([]facts.Relation, 0, len(node.Relations))
					rels = pluginFactsRelations(pluginName, api, node.Relations)
					fact := facts.Fact{Kind: factKind, Name: node.Name, File: owner, Line: node.Line, EndLine: node.EndLine, Repo: repoID, Props: props, Relations: rels}
					s.pluginFacts = append(s.pluginFacts, fact)
					s.pluginContribs[key] = append(s.pluginContribs[key], fact)
				}
				anchors := append([]analyzerplugin.Anchor(nil), output.Anchors...)
				sort.SliceStable(anchors, func(i, j int) bool {
					return analyzerplugin.AnchorSortKey(anchors[i]) < analyzerplugin.AnchorSortKey(anchors[j])
				})
				for _, anchor := range anchors {
					anchorOwner := filepath.ToSlash(anchor.Owner)
					if anchorOwner == "" {
						anchorOwner = filepath.ToSlash(owner)
						anchor.Owner = anchorOwner
					}
					ownerFiles[anchorOwner] = true
					props := pluginOwnedProperties(api, pluginName, anchor.Props)
					props["plugin"] = pluginName
					props["plugin_identity"] = record.Identity
					props["plugin_anchor"] = true
					if anchor.SourceIdentity != "" {
						props["fsm_source_identity"] = anchor.SourceIdentity
					}
					rels := make([]facts.Relation, 0, len(anchor.Relations))
					rels = pluginAnchorRelations(pluginName, api, anchor.Relations)
					// Persist anchors into Contrib so frozen pre-Begin name
					// planning and resolution indexes see old and new owners.
					s.pluginContribs[key] = append(s.pluginContribs[key], facts.Fact{
						Kind: facts.KindSymbol, Name: anchor.Symbol, File: anchorOwner,
						Line: anchor.Line, EndLine: anchor.EndLine, Repo: repoID, Props: props, Relations: rels,
					})
					s.pluginAnchors = append(s.pluginAnchors, pluginAnchor{Plugin: pluginName, Identity: record.Identity, Value: anchor})
				}
				for _, enrichment := range output.Enrichments {
					if enrichment.Owner == "" {
						enrichment.Owner = owner
					}
					s.pluginEnrichments = append(s.pluginEnrichments, pluginEnrichment{Plugin: pluginName, API: api, Identity: record.Identity, Value: enrichment})
				}
			}
		}
	}
	s.analyzerPluginStats.OwnerFiles = len(ownerFiles)
	return nil
}

// pluginOwnerSeeds returns every file owner referenced by plugin unit outputs,
// including anchor owners, for frozen Begin seeding.
func pluginOwnerSeeds(records map[string]analyzerplugin.PluginRecord) []string {
	if len(records) == 0 {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	pluginNames := make([]string, 0, len(records))
	for name := range records {
		pluginNames = append(pluginNames, name)
	}
	sort.Strings(pluginNames)
	for _, name := range pluginNames {
		record := records[name]
		unitIDs := make([]string, 0, len(record.Units))
		for id := range record.Units {
			unitIDs = append(unitIDs, id)
		}
		sort.Strings(unitIDs)
		for _, id := range unitIDs {
			owners := make([]string, 0, len(record.Units[id].Owners))
			for owner := range record.Units[id].Owners {
				owners = append(owners, owner)
			}
			sort.Strings(owners)
			for _, owner := range owners {
				owner = filepath.ToSlash(owner)
				if owner != "" && !seen[owner] {
					seen[owner] = true
					out = append(out, owner)
				}
				for _, anchor := range record.Units[id].Owners[owner].Anchors {
					anchorOwner := filepath.ToSlash(anchor.Owner)
					if anchorOwner == "" {
						anchorOwner = owner
					}
					if anchorOwner != "" && !seen[anchorOwner] {
						seen[anchorOwner] = true
						out = append(out, anchorOwner)
					}
				}
				for _, enrichment := range record.Units[id].Owners[owner].Enrichments {
					enrichmentOwner := filepath.ToSlash(enrichment.Owner)
					if enrichmentOwner == "" {
						enrichmentOwner = owner
					}
					if enrichmentOwner != "" && !seen[enrichmentOwner] {
						seen[enrichmentOwner] = true
						out = append(out, enrichmentOwner)
					}
				}
			}
		}
	}
	return out
}

func replacePluginContributions(files map[string]*FileState, contributions map[string][]facts.Fact, hashes map[string]string, admitted map[string]bool) {
	// FileState pointers may still belong to the committed resident snapshot.
	// Copy only owners whose plugin contribution maps this replacement will
	// mutate; cloning every retained owner here would add repository-wide work
	// to ordinary plugin no-change runs.
	newPluginOwners := map[string]bool{}
	for _, ff := range contributions {
		for _, f := range ff {
			if owner := filepath.ToSlash(f.File); owner != "" {
				newPluginOwners[owner] = true
			}
		}
	}
	for owner, st := range files {
		if st == nil {
			continue
		}
		mutatesPluginContribution := newPluginOwners[filepath.ToSlash(owner)]
		if !mutatesPluginContribution {
			for name := range st.Contrib {
				if strings.HasPrefix(name, "plugin:") {
					mutatesPluginContribution = true
					break
				}
			}
		}
		if !mutatesPluginContribution {
			for name := range st.ContribHash {
				if strings.HasPrefix(name, "plugin:") {
					mutatesPluginContribution = true
					break
				}
			}
		}
		if mutatesPluginContribution {
			st = cloneFileState(st)
			files[owner] = st
		}
		for name := range st.Contrib {
			if strings.HasPrefix(name, "plugin:") {
				delete(st.Contrib, name)
			}
		}
		for name := range st.ContribHash {
			if strings.HasPrefix(name, "plugin:") {
				delete(st.ContribHash, name)
			}
		}
		if st.Extractor == "" && st.TS == nil && len(st.Facts) == 0 && len(st.Contrib) == 0 {
			delete(files, owner)
		}
	}
	if admitted == nil {
		admitted = admittedOwnerSet(nil, hashes)
	}
	for name, ff := range contributions {
		byOwner := map[string][]facts.Fact{}
		for _, f := range ff {
			byOwner[filepath.ToSlash(f.File)] = append(byOwner[filepath.ToSlash(f.File)], f)
		}
		for owner, ownedFacts := range byOwner {
			if !admitted[owner] {
				// Deleted or otherwise non-admitted owners receive an empty
				// replacement; cached plugin facts must not resurrect them.
				continue
			}
			st := files[owner]
			if st == nil {
				st = &FileState{Hash: hashes[owner]}
				files[owner] = st
			}
			if st.Contrib == nil {
				st.Contrib = map[string][]facts.Fact{}
			}
			if st.ContribHash == nil {
				st.ContribHash = map[string]string{}
			}
			st.Contrib[name] = ownedFacts
			// NameOnly / unhashed admitted owners keep an empty contrib hash;
			// hashed owners bind the content digest when present.
			st.ContribHash[name] = hashes[owner]
		}
	}
}

func mergeAnalyzerPluginAnchors(base []facts.Fact, anchors []pluginAnchor) []facts.Fact {
	if len(anchors) == 0 {
		return base
	}
	ordered := append([]pluginAnchor(nil), anchors...)
	sort.SliceStable(ordered, func(i, j int) bool {
		left, right := ordered[i], ordered[j]
		if left.Plugin != right.Plugin {
			return left.Plugin < right.Plugin
		}
		leftKey := analyzerplugin.AnchorSortKey(left.Value)
		rightKey := analyzerplugin.AnchorSortKey(right.Value)
		if leftKey != rightKey {
			return leftKey < rightKey
		}
		return left.Identity < right.Identity
	})
	out := append([]facts.Fact(nil), base...)
	for _, wrapped := range ordered {
		a := wrapped.Value
		ownerPath := filepath.ToSlash(a.Owner)
		overlay := facts.Fact{Kind: facts.KindSymbol, Name: a.Symbol, File: ownerPath, Line: a.Line, EndLine: a.EndLine, Props: cloneAnyMap(a.Props)}
		if overlay.Props == nil {
			overlay.Props = map[string]any{}
		}
		overlay.Props["plugin"] = wrapped.Plugin
		overlay.Props["plugin_identity"] = wrapped.Identity
		for _, r := range a.Relations {
			overlay.Relations = append(overlay.Relations, facts.Relation{Kind: r.Kind, Target: r.Target, TargetKind: r.TargetKind, TargetFile: r.TargetFile})
		}
		owner := pluginAnchorOwner(out, overlay, a.SourceIdentity)
		if owner < 0 {
			props := cloneAnyMap(overlay.Props)
			props["coverage_status"] = "partial"
			props["extractor"] = "plugin:" + wrapped.Plugin
			props["unresolved_source_binding"] = a.Symbol
			props["unresolved_source_bindings"] = 1
			props["unbound_fsm_relations"] = relationEvidence(overlay.Relations)
			out = append(out, facts.Fact{Kind: facts.KindExtraction, Name: "plugin:" + wrapped.Plugin + ":source-binding:" + ownerPath + ":" + a.Symbol, File: ownerPath, Line: a.Line, EndLine: a.EndLine, Repo: overlay.Repo, Props: props})
			continue
		}
		for _, rel := range overlay.Relations {
			found := false
			for _, old := range out[owner].Relations {
				if old.Kind == rel.Kind && old.Target == rel.Target && old.TargetKind == rel.TargetKind && old.TargetFile == rel.TargetFile {
					found = true
					break
				}
			}
			if !found {
				out[owner].Relations = append(out[owner].Relations, rel)
			}
		}
		if out[owner].Props == nil {
			out[owner].Props = map[string]any{}
		}
		sites, _ := out[owner].Props["fsm_evidence_sites"].([]map[string]any)
		sites = append(sites, map[string]any{"line": overlay.Line, "end_line": overlay.EndLine, "props": overlay.Props, "relations": relationEvidence(overlay.Relations)})
		sort.SliceStable(sites, func(i, j int) bool {
			return evidenceSiteKey(sites[i]) < evidenceSiteKey(sites[j])
		})
		out[owner].Props["fsm_evidence_sites"] = sites
	}
	return out
}

// mergeAnalyzerPluginEnrichments applies plugin-owned overlays only after all
// base and plugin-created facts are available. The target is deliberately
// resolved by the full stable identity (owner, kind, name); absent or ambiguous
// targets fail the run instead of silently dropping or misapplying data.
func mergeAnalyzerPluginEnrichments(base []facts.Fact, enrichments []pluginEnrichment) ([]facts.Fact, error) {
	if len(enrichments) == 0 {
		return base, nil
	}
	out := append([]facts.Fact(nil), base...)
	ordered := append([]pluginEnrichment(nil), enrichments...)
	sort.SliceStable(ordered, func(i, j int) bool {
		left, right := ordered[i], ordered[j]
		leftOwner, rightOwner := filepath.ToSlash(left.Value.Owner), filepath.ToSlash(right.Value.Owner)
		leftKey := leftOwner + "\x00" + pluginEnrichmentTargetKind(left.Plugin, left.API, left.Value.Kind) + "\x00" + left.Value.Name
		rightKey := rightOwner + "\x00" + pluginEnrichmentTargetKind(right.Plugin, right.API, right.Value.Kind) + "\x00" + right.Value.Name
		if left.Plugin != right.Plugin {
			return left.Plugin < right.Plugin
		}
		if leftKey != rightKey {
			return leftKey < rightKey
		}
		return left.Identity < right.Identity
	})

	for _, wrapped := range ordered {
		enrichment := wrapped.Value
		owner := filepath.ToSlash(enrichment.Owner)
		kind := pluginEnrichmentTargetKind(wrapped.Plugin, wrapped.API, enrichment.Kind)
		match, matches := -1, 0
		for i := range out {
			if out[i].Kind == kind && out[i].Name == enrichment.Name && filepath.ToSlash(out[i].File) == owner {
				match = i
				matches++
			}
		}
		if matches == 0 {
			return nil, fmt.Errorf("analyzer plugin %q enrichment target %s/%s is missing in owner %s", wrapped.Plugin, kind, enrichment.Name, owner)
		}
		if matches != 1 {
			return nil, fmt.Errorf("analyzer plugin %q enrichment target %s/%s is ambiguous in owner %s (%d facts)", wrapped.Plugin, kind, enrichment.Name, owner, matches)
		}

		target := out[match]
		if len(enrichment.Props) > 0 {
			properties := pluginOwnedProperties(wrapped.API, wrapped.Plugin, enrichment.Props)
			incoming, _ := properties["plugin_properties"].(map[string]any)
			existing, ok := target.Props["plugin_properties"].(map[string]any)
			if value, present := target.Props["plugin_properties"]; present && !ok {
				return nil, fmt.Errorf("analyzer plugin %q cannot enrich %s/%s in owner %s: existing plugin_properties has type %T", wrapped.Plugin, kind, enrichment.Name, owner, value)
			}
			merged := cloneAnyMap(existing)
			if merged == nil {
				merged = map[string]any{}
			}
			plugins := make([]string, 0, len(incoming))
			for plugin := range incoming {
				plugins = append(plugins, plugin)
			}
			sort.Strings(plugins)
			for _, plugin := range plugins {
				fields, _ := incoming[plugin].(map[string]any)
				prior, ok := merged[plugin].(map[string]any)
				if value, present := merged[plugin]; present && !ok {
					return nil, fmt.Errorf("analyzer plugin %q cannot enrich %s/%s in owner %s: namespace %q has type %T", wrapped.Plugin, kind, enrichment.Name, owner, plugin, value)
				}
				prior = cloneAnyMap(prior)
				if prior == nil {
					prior = map[string]any{}
				}
				fieldNames := make([]string, 0, len(fields))
				for field := range fields {
					fieldNames = append(fieldNames, field)
				}
				sort.Strings(fieldNames)
				for _, field := range fieldNames {
					if _, exists := prior[field]; exists {
						return nil, fmt.Errorf("analyzer plugin %q enrichment would overwrite its property %s on %s/%s in owner %s", wrapped.Plugin, field, kind, enrichment.Name, owner)
					}
					prior[field] = cloneAny(fields[field])
				}
				merged[plugin] = prior
			}
			if target.Props == nil {
				target.Props = map[string]any{}
			} else {
				target.Props = cloneAnyMap(target.Props)
			}
			target.Props["plugin_properties"] = merged
		}

		relationsCopied := false
		for _, relation := range pluginFactsRelations(wrapped.Plugin, wrapped.API, enrichment.Relations) {
			found := false
			for _, previous := range target.Relations {
				if previous == relation {
					found = true
					break
				}
			}
			if !found {
				if !relationsCopied {
					target.Relations = append([]facts.Relation(nil), target.Relations...)
					relationsCopied = true
				}
				target.Relations = append(target.Relations, relation)
			}
		}
		out[match] = target
	}
	return out, nil
}

func pluginFactKind(pluginName, api, kind string) string {
	if api == analyzerplugin.GoAPIVersion {
		return analyzerplugin.GenericFactKind(pluginName, kind)
	}
	return kind
}

func pluginEnrichmentTargetKind(pluginName, api, kind string) string {
	if api == analyzerplugin.GoAPIVersion {
		return analyzerplugin.GenericTargetKind(pluginName, kind)
	}
	return kind
}

func pluginFactsRelations(pluginName, api string, relations []analyzerplugin.Relation) []facts.Relation {
	out := make([]facts.Relation, 0, len(relations))
	for _, relation := range relations {
		kind, targetKind := relation.Kind, relation.TargetKind
		if api == analyzerplugin.GoAPIVersion {
			kind = analyzerplugin.GenericRelationKind(pluginName, kind)
			targetKind = analyzerplugin.GenericTargetKind(pluginName, targetKind)
		}
		out = append(out, facts.Relation{Kind: kind, Target: relation.Target, TargetKind: targetKind, TargetFile: relation.TargetFile})
	}
	return out
}

func pluginAnchorRelations(pluginName, api string, relations []analyzerplugin.Relation) []facts.Relation {
	if api != analyzerplugin.GoAPIVersion {
		return pluginFactsRelations(pluginName, api, relations)
	}
	out := make([]facts.Relation, 0, len(relations))
	for _, relation := range relations {
		targetKind := relation.TargetKind
		if targetKind == "" {
			targetKind, _ = facts.FSMRelationTargetKind(relation.Kind)
		}
		out = append(out, facts.Relation{Kind: relation.Kind, Target: relation.Target, TargetKind: targetKind, TargetFile: relation.TargetFile})
	}
	return out
}

func pluginOwnedProperties(api, pluginName string, properties map[string]any) map[string]any {
	if api != analyzerplugin.GoAPIVersion {
		out := cloneAnyMap(properties)
		if out == nil {
			out = map[string]any{}
		}
		return out
	}
	owned := cloneAnyMap(properties)
	if owned == nil {
		owned = map[string]any{}
	}
	return map[string]any{"plugin_properties": map[string]any{pluginName: owned}}
}

func evidenceSiteKey(site map[string]any) string {
	plugin := ""
	props, _ := site["props"].(map[string]any)
	if props != nil {
		plugin, _ = props["plugin"].(string)
	}
	propsKey := ""
	if b, err := json.Marshal(props); err == nil {
		propsKey = string(b)
	}
	relationsKey := ""
	if b, err := json.Marshal(site["relations"]); err == nil {
		relationsKey = string(b)
	}
	return fmt.Sprintf("%v\x00%v\x00%s\x00%s\x00%s", site["line"], site["end_line"], plugin, propsKey, relationsKey)
}

func pluginAnchorOwner(base []facts.Fact, overlay facts.Fact, sourceIdentity string) int {
	var candidates, exact []int
	canonical := filepath.ToSlash(filepath.Dir(overlay.File)) + "." + overlay.Name
	for i, candidate := range base {
		if candidate.Kind != facts.KindSymbol || filepath.ToSlash(candidate.File) != filepath.ToSlash(overlay.File) {
			continue
		}
		if sourceIdentity != "" {
			if candidate.Name != sourceIdentity || candidate.Props["fsm_source_binding"] != "tree_sitter_nested_declaration" {
				continue
			}
		} else if candidate.Name != overlay.Name && candidate.Name != canonical {
			continue
		}
		if overlay.Line > 0 && candidate.Line > 0 && candidate.EndLine > 0 && (overlay.Line < candidate.Line || overlay.Line > candidate.EndLine) {
			continue
		}
		candidates = append(candidates, i)
		if overlay.Line > 0 && candidate.Line == overlay.Line {
			exact = append(exact, i)
		}
	}
	if len(exact) == 1 {
		return exact[0]
	}
	if len(candidates) == 1 {
		return candidates[0]
	}
	return -1
}
func relationEvidence(relations []facts.Relation) []map[string]string {
	out := make([]map[string]string, 0, len(relations))
	for _, r := range relations {
		out = append(out, map[string]string{"kind": r.Kind, "target": r.Target, "target_kind": r.TargetKind, "target_file": r.TargetFile})
	}
	return out
}

func confinedRepositoryInputPath(root, rel string) (string, error) {
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", fmt.Errorf("resolve repository root: %w", err)
	}
	candidate := filepath.Join(resolvedRoot, filepath.FromSlash(rel))
	lexicalRel, err := filepath.Rel(resolvedRoot, candidate)
	if err != nil || lexicalRel == ".." || strings.HasPrefix(lexicalRel, ".."+string(filepath.Separator)) {
		return "", errors.New("path escapes repository")
	}
	// Resolve the deepest existing path prefix before read/stat. This rejects
	// symlinks that leave the repository even when the requested leaf is absent
	// and avoids reading an outside target before checking its canonical path.
	ancestor := candidate
	for {
		resolved, resolveErr := filepath.EvalSymlinks(ancestor)
		if resolveErr == nil {
			resolvedRel, relErr := filepath.Rel(resolvedRoot, resolved)
			if relErr != nil || resolvedRel == ".." || strings.HasPrefix(resolvedRel, ".."+string(filepath.Separator)) {
				return "", errors.New("path resolves outside repository")
			}
			suffix, relErr := filepath.Rel(ancestor, candidate)
			if relErr != nil {
				return "", relErr
			}
			return filepath.Join(resolved, suffix), nil
		}
		if !os.IsNotExist(resolveErr) {
			return "", resolveErr
		}
		parent := filepath.Dir(ancestor)
		if parent == ancestor {
			return "", resolveErr
		}
		ancestor = parent
	}
}

func pluginGlob(glob, name string) bool {
	return analyzerplugin.MatchRepositoryGlob(glob, name)
}

// pluginVisibleNames returns graph-visible admitted names for plugin plan and
// list digests: lockfiles and policy-excluded inventory entries are omitted.
func pluginVisibleNames(eng *engine.Engine, names []string) []string {
	visible := graphSemanticNames(eng, names)
	sort.Strings(visible)
	return visible
}

func containsSlashPath(paths []string, want string) bool {
	for _, p := range paths {
		if filepath.ToSlash(p) == want {
			return true
		}
	}
	return false
}
func stringEqual(a, b []byte) bool { return string(a) == string(b) }
