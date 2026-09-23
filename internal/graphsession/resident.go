package graphsession

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/enola-labs/enola/internal/graphinput"
	"os"
	"path/filepath"
	"sync"

	"github.com/enola-labs/enola/internal/engine"
	"github.com/enola-labs/enola/internal/extractors/swiftextractor"
	"github.com/enola-labs/enola/internal/extractors/tsextractor"
	"github.com/enola-labs/enola/internal/facts"
	"github.com/enola-labs/enola/internal/filelock"
	"github.com/enola-labs/enola/internal/graphprofile"
	"github.com/enola-labs/enola/internal/graphstream"
)

// WorkCounters describe session input and persistence work, not extractor internals.
// Extraction/composition reads remain separately reported by Result.Stats.
type WorkCounters struct {
	PolicyBuilds                                              int
	BoundedContextChecks                                      int
	FactAssemblies, CapturedReads, PublishedEvents            int
	CheckpointBytes                                           int64
	InventoryScans, DetectionScans, ContextScans, ConfigScans int
	HashedFiles, DirtyHashBytes, VerifiedFiles, Checkpoints   int
	// NonTSCaptures counts the fenced snapshot-and-extract passes a run makes
	// over non-TypeScript extractors. A run asks two questions about such an
	// extractor - whether its output moved, and which owners its candidate
	// names retarget - and both are answered from one pass, so this staying at
	// one per needed extractor is what says the second question reused the
	// first answer rather than reading the tree again.
	NonTSCaptures int
	// TSDiscoveries counts the repository-wide TypeScript discovery snapshots a
	// run actually built. One run needs one: the context fingerprint and every
	// planner preview ask the same framework, gate, name and alias questions of
	// the same tree. A rise means some caller observed a capture the snapshot
	// was not taken under and had to read the tree again, and the run reports
	// that rather than hiding it.
	TSDiscoveries int
	// TSDiscoveriesReused counts the runs that answered with a snapshot an
	// earlier run built, after proving it again. It is the other half of
	// TSDiscoveries: together they say how many runs needed a discovery and how
	// many of those had to read the tree for it.
	TSDiscoveriesReused int
	// GraphInputRebuilds counts the graph input policies a run constructed;
	// GraphInputRebuildsProven counts the ones it did not have to construct
	// because the policy it already held re-read identical. A fresh CLI builds
	// one policy resolving its target and then, at the top of its first
	// transaction, a second identical one; these two counters are what say
	// which of the two happened, and they are reported separately so a proven
	// skip can never be read as work that was never needed.
	GraphInputRebuilds, GraphInputRebuildsProven int
	// TSDiscoveryRechecks is what proving a retained snapshot cost: the
	// presences re-observed, the side reads re-read, the directories
	// re-enumerated. This is work this change introduces, not work it avoids,
	// and it is counted separately so it cannot hide inside a win.
	TSDiscoveryRechecks tsextractor.DiscoveryRecheck
}

type runtimeInputs struct {
	engineContextHash string
	tsDiscovery       *tsextractor.Discovery
	tsContext         map[string]string
	tsFileContext     map[string]string
	policyIdentity    string
	admissionIdentity string
	coverageVersion   uint64
	generation        int64
	resolution        *idIndex
	configPaths       []string
	sources           map[string][]byte
	effective         map[string][]byte
	inventory         engine.RepoInventory
	detected          map[string]bool
	hashes, contexts  map[string]string
	configHash        string
	config            map[string][]byte
	angular           bool
}

// retainedDiscoveryIdentity is what a snapshot was proven under, as opposed to
// what it observed. A snapshot only gets as far as being re-proven when the
// policy and admission rules deciding which paths are readable at all are the
// same ones; otherwise the reads behind it answered a different question.
type retainedDiscoveryIdentity struct {
	policy    string
	admission string
}

func readRuntimeInputs(eng *engine.Engine, abs string, st *State, work *WorkCounters, retained *tsextractor.Discovery, retainedFor retainedDiscoveryIdentity) (*runtimeInputs, error) {
	tr := graphprofile.StartNamed("inputs")
	work.InventoryScans++
	inv, err := eng.Inventory(abs)
	if err != nil {
		return nil, fmt.Errorf("inventory: %w", err)
	}
	tr.Mark("inventory", fmt.Sprintf("files=%d all_names=%d", len(inv.Files), len(inv.AllNames)))
	work.DetectionScans++
	detected := eng.DetectExtractors(abs, inv.AllNames)
	if err := eng.ValidateGraphConsumers(detected); err != nil {
		return nil, err
	}
	tr.Mark("detect_extractors", fmt.Sprintf("detected=%d", len(detected)))
	prev := map[string]*FileState{}
	if st != nil {
		prev = st.Files
	}
	targets := filesToHash(eng, inv, prev, detected)
	tr.Mark("select_hash_targets", fmt.Sprintf("targets=%d", len(targets)))
	hashes := eng.FileHashes(abs, targets)
	work.HashedFiles += len(targets)
	tr.Mark("hash_content_inputs", fmt.Sprintf("targets=%d", len(targets)))
	work.ContextScans++
	// Extractor context capture is its own set of reads, not part of hashing;
	// a delta that hashes a handful of files can still walk for contexts.
	contexts := captureDeltaContexts(eng, abs, detected)
	for k, v := range contexts {
		hashes[k] = v
	}
	tr.Mark("capture_contexts", fmt.Sprintf("contexts=%d", len(contexts)))
	work.ConfigScans++
	cfgHash, cfg, paths, err := analysisFingerprintInputs(abs, eng)
	if err != nil {
		return nil, err
	}
	tsConfigPaths := append([]string(nil), paths...)
	for _, ext := range eng.Extractors() {
		if swift, ok := ext.(*swiftextractor.SwiftExtractor); ok && detected[ext.Name()] {
			paths = append(paths, swift.ObservedContextPaths(abs)...)
		}
	}
	tr.Mark("analysis_fingerprint", fmt.Sprintf("cfg_files=%d", len(cfg)))
	// Hoisted out of the struct literal so it can be timed: RepoUsesAngular is
	// another repository probe, and it ran unattributed inside the composite.
	usesAngular := tsextractor.RepoUsesAngular(abs, eng.GraphScope())
	tr.Mark("angular_probe", fmt.Sprintf("angular=%v", usesAngular))
	result := &runtimeInputs{inventory: inv, detected: detected, hashes: hashes, contexts: contexts, configHash: cfgHash, config: cfg, angular: usesAngular, configPaths: paths}
	if scope := eng.GraphScope(); scope != nil {
		result.policyIdentity = scope.Policy.Identity()
		result.admissionIdentity = scope.Policy.AdmissionIdentity()
		tr.Mark("policy_identity", "")
		result.engineContextHash = engineContextFingerprint(eng)
		tr.Mark("engine_context_fingerprint", "")
		for _, ext := range eng.Extractors() {
			if ts, ok := ext.(*tsextractor.TSExtractor); ok {
				// Built over cfg, the configuration this run has already
				// captured and fenced, because that is what every later reader
				// of this snapshot will observe. Building it over the live tree
				// instead left SessionContext to rebuild locally whenever the
				// capture and the tree disagreed, and that rebuild was thrown
				// away: the previews then ran against a snapshot the projected
				// context keys were never computed from, and the counter never
				// saw the second build.
				offer := retained
				if retainedFor != (retainedDiscoveryIdentity{policy: result.policyIdentity, admission: result.admissionIdentity}) {
					offer = nil
				}
				built, reused, cost := ts.DiscoveryFor(context.Background(), abs, cfg, offer)
				work.TSDiscoveryRechecks = work.TSDiscoveryRechecks.Add(cost)
				if reused {
					work.TSDiscoveriesReused++
					tr.Mark("ts_discovery_retained", cost.String())
				} else {
					work.TSDiscoveries++
					tr.Mark("ts_discovery", cost.String())
				}
				var used *tsextractor.Discovery
				result.tsContext, result.tsFileContext, used = ts.SessionContext(abs, cfg, tsConfigPaths, inv.Files, built)
				if used != built {
					// SessionContext refused the snapshot and built its own.
					// Keep that one - it is the proven one - and count it.
					work.TSDiscoveries++
					tr.Mark("ts_discovery_rebuilt", "")
				}
				result.tsDiscovery = used
			}
		}
		tr.Mark("ts_session_context", fmt.Sprintf("files=%d", len(result.tsFileContext)))
	}
	// Terminal mark: without it the tail after angular_probe - policy identity,
	// engine context fingerprint and the TS SessionContext walk - falls outside
	// every window and is invisible, which is exactly where repeated discovery
	// work would hide. runtime_inputs_complete closes the inputs trace, so the
	// sum of its marks is the whole of readRuntimeInputs.
	tr.Mark("runtime_inputs_complete", "")
	return result, nil
}

// Resident serializes generations and owns immutable committed input/state caches.
// Callers must not mutate the Engine after opening the session. A new process
// always reconciles; an empty userspace event queue is not strict disk equality.
type Resident struct {
	coverageVersion uint64
	mu              sync.Mutex
	eng             *engine.Engine
	abs             string
	opts            Options
	sink            graphstream.Sink
	journal         *graphstream.Journal
	lock            *filelock.Lock
	state           *State
	inputs          *runtimeInputs
	// tsDiscovery is the snapshot the last committed run proved, kept for the
	// next one to try to prove again, together with the policy identity it was
	// taken under. It is dropped on any failure: a run that did not commit
	// leaves no observation worth carrying.
	tsDiscovery    *tsextractor.Discovery
	tsDiscoveryFor retainedDiscoveryIdentity
	snapshot       []facts.Fact
	epoch          string
	watermark      uint64
	failed, closed bool
	// engineUnused is the caller's FreshEngine claim, still true. It survives
	// exactly until the first transaction, because after that the engine is one
	// this session has been running against rather than one just constructed,
	// and the window a declared-input recheck would have to cover is no longer
	// bounded by anything.
	engineUnused bool
}

type OnlineResult struct {
	Result
	Work           WorkCounters
	Epoch          string
	Watermark      uint64
	Reconciled     bool
	FallbackReason string
}

func (r *Resident) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil
	}
	r.closed = true
	err := r.journal.Close()
	r.lock.Release()
	return err
}

// Snapshot explicitly copies the committed graph; ApplyChanges returns a summary.
func (r *Resident) Snapshot() []facts.Fact {
	r.mu.Lock()
	defer r.mu.Unlock()
	b, _ := json.Marshal(r.snapshot)
	var out []facts.Fact
	_ = json.Unmarshal(b, &out)
	return out
}

func (r *Resident) reconcile(ctx context.Context, fullFacts bool) (*Result, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	res, _, err := r.transaction(ctx, nil, false)
	if res != nil && !fullFacts {
		res.Facts = nil
	}
	return res, err
}

func (r *Resident) transaction(ctx context.Context, input *runtimeInputs, fast bool) (*Result, WorkCounters, error) {
	if r.closed {
		return nil, WorkCounters{}, fmt.Errorf("graphsession: session closed")
	}
	if r.failed {
		p := &graphstream.Publisher{Sink: r.sink, Journal: r.journal}
		if err := p.ReplayUnacked(ctx); err != nil {
			return nil, WorkCounters{}, err
		}
		st, err := recoverAcknowledgedPending(r.opts.StateDir, r.journal, r.opts, r.abs)
		if err != nil {
			return nil, WorkCounters{}, err
		}
		r.state = st
	}
	// The work between entering a transaction and starting the session trace was
	// outside every window: root measured it only as an untraced CLI residual.
	rtr := graphprofile.StartNamed("reconcile")
	rtr.Mark("transaction_enter", fmt.Sprintf("fast=%v failed=%v", fast, r.failed))
	committedEngine := r.eng
	promoteEngine := false
	defer func() {
		if !promoteEngine {
			r.eng = committedEngine
		}
	}()
	// provenInputs records that this run answered the policy question by
	// re-reading what the policy declared rather than by building a second one.
	provenInputs := false
	var beforeRebuild map[string][]byte
	if !fast {
		var captureErr error
		beforeRebuild, captureErr = r.effectiveConfig()
		if captureErr != nil {
			return nil, WorkCounters{}, captureErr
		}
		rtr.Mark("effective_config_before", fmt.Sprintf("files=%d", len(beforeRebuild)))
	}
	if !fast {
		// The rebuild is here to make the policy an observation of this run's
		// tree rather than of whenever the engine happened to be built. A fresh
		// CLI pays for it twice: it constructs one policy resolving its target
		// and an identical one here, microseconds later, and the second is a
		// repository walk, a git index read and a check-ignore pass over every
		// name.
		//
		// It can be skipped only with the same property proven a cheaper way:
		// that building one now would read the same things. That is what
		// ReusableOver asks - the declared reads, a rerun of the Git discovery
		// and the walk, without the index read, the check-ignore pass or the
		// identity computation, which are functions of them - inside the
		// configuration bracket this function already holds, so a configuration
		// edit racing the check still fails below. Every other case rebuilds,
		// and the reason it rebuilt is recorded rather than inferred.
		reason := ""
		switch {
		case !r.engineUnused:
			reason = "engine already used"
		case r.eng.GraphScope() == nil:
			reason = "no graph scope"
		default:
			if why, ok := r.eng.GraphScope().Policy.ReusableOver(); !ok {
				reason = why
			}
		}
		if reason == "" {
			provenInputs = true
			rtr.Mark("graph_inputs_proven", "")
		} else {
			fresh, err := r.eng.RebuildGraphInputs()
			if err != nil {
				return nil, WorkCounters{}, err
			}
			r.eng = fresh
			rtr.Mark("rebuild_graph_inputs", reason)
		}
	}
	// Whatever happened above, the engine has now been used by a transaction.
	r.engineUnused = false
	effective := map[string][]byte(nil)
	var err error
	if fast {
		effective = r.inputs.effective
	} else {
		effective, err = r.effectiveConfig()
	}
	if err != nil {
		return nil, WorkCounters{}, err
	}
	rtr.Mark("effective_config", fmt.Sprintf("files=%d fast=%v", len(effective), fast))
	if !fast && !sameConfig(beforeRebuild, effective) {
		return nil, WorkCounters{}, fmt.Errorf("%w: config changed during graph policy construction", ErrInputsChanged)
	}
	reloaded := false
	if !fast && r.inputs != nil && !sameConfig(effective, r.inputs.effective) && r.eng.GraphScope() == nil {
		if r.opts.ReloadEngine == nil {
			return nil, WorkCounters{}, fmt.Errorf("effective Enola config changed: provide Options.ReloadEngine or reopen with freshly resolved configuration")
		}
		beforeReload := effective
		eng, err := r.opts.ReloadEngine(ctx)
		if err != nil {
			return nil, WorkCounters{}, err
		}
		if eng == nil {
			return nil, WorkCounters{}, fmt.Errorf("ReloadEngine returned nil")
		}
		reloaded = true
		r.eng = eng
		effective, err = r.effectiveConfig()
		if err != nil {
			return nil, WorkCounters{}, err
		}
		if !sameConfig(beforeReload, effective) {
			return nil, WorkCounters{}, fmt.Errorf("%w: config changed inside ReloadEngine", ErrInputsChanged)
		}
		rtr.Mark("reload_engine", "")
	}
	rtr.Mark("reconcile_complete", fmt.Sprintf("reloaded=%v", reloaded))
	s := &session{eng: r.eng, abs: r.abs, opts: r.opts, sink: r.sink, state: r.state, journal: r.journal, prof: graphprofile.StartNamed("session"), inputs: input, fast: fast}
	s.retained, s.retainedFor = r.tsDiscovery, r.tsDiscoveryFor
	if !fast && r.eng.GraphScope() != nil {
		if provenInputs {
			s.work.GraphInputRebuildsProven = 1
		} else {
			// PolicyBuilds stays what it has always been: the policies this
			// transaction built. A proven run built none, and says so here
			// rather than reporting one it did not do.
			s.work.PolicyBuilds = 1
			s.work.GraphInputRebuilds = 1
		}
	}
	if reloaded {
		s.opts.ForceInitial = true
		s.extraFallbacks = []graphstream.Fallback{{Extractor: "all", Scope: "all owned files", Reason: "effective Enola configuration reloaded"}}
	}
	s.validateEffective = func() error {
		after, err := r.effectiveConfig()
		if err != nil {
			return err
		}
		if !sameConfig(effective, after) {
			return fmt.Errorf("%w: effective Enola config changed during transaction", ErrInputsChanged)
		}
		if !fast {
			return validateGraphPolicy(r.eng)
		}
		return nil
	}
	if r.inputs != nil && r.state != nil && r.inputs.generation == r.state.Generation {
		s.priorResolution = r.inputs.resolution
	}
	have := r.state != nil && r.state.LastComplete && r.state.ExtractorVersion == engine.ExtractorVersion() && r.state.Schema == stateSchema
	res, err := s.run(ctx, r.opts.ForceInitial || !have)
	if err == nil && s.began {
		s.work.PublishedEvents = s.seq + 2
	}
	if err != nil {
		r.failed = true
		// A run that did not commit proved nothing that outlives it. Its
		// snapshot may have been built against a tree that moved underneath it,
		// which is often why it failed, so the next run starts from no offer.
		r.tsDiscovery, r.tsDiscoveryFor = nil, retainedDiscoveryIdentity{}
		return nil, s.work, err
	}
	promoteEngine = true
	r.failed = false
	r.state = s.state
	if !fast {
		r.coverageVersion++
		s.inputs.coverageVersion = r.coverageVersion
	}
	s.inputs.effective = effective
	if s.state != nil {
		s.inputs.generation = s.state.Generation
	}
	r.inputs = s.inputs
	// Retain what this run proved, not what it was offered: on a run that had
	// to rebuild, this is the new snapshot, and on one that reused, it is the
	// same value carried one run further.
	r.tsDiscovery = s.inputs.tsDiscovery
	r.tsDiscoveryFor = retainedDiscoveryIdentity{policy: s.inputs.policyIdentity, admission: s.inputs.admissionIdentity}
	r.snapshot = res.Facts
	r.opts.ForceInitial = false
	return res, s.work, nil
}

// ApplyChanges acknowledges only the supplied observed watermark, never events
// arriving later. Covered means the source covers all semantic inputs, including
// external config, without a loss since From. Strict callers set Reconcile.
func (r *Resident) ApplyChanges(ctx context.Context, batch ChangeBatch) (*OnlineResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil, fmt.Errorf("graphsession: session closed")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if scope := r.eng.GraphScope(); scope != nil {
		var paths []string
		for _, p := range batch.Paths {
			switch scope.Policy.ClassifyEvent(p, false, graphinput.Content) {
			case graphinput.Ignore:
				if !r.explicitContentPath(p) {
					continue
				}
			case graphinput.Reconcile:
				if batch.Reconcile == "" {
					batch.Reconcile = "graph input policy or membership changed"
				}
			}
			paths = append(paths, p)
		}
		batch.Paths = paths
	}
	reason := batch.Reconcile
	if reason == "" && (!batch.Covered || batch.Epoch == "") {
		reason = "uncovered change source"
	}
	if reason == "" && (r.inputs == nil || r.epoch != batch.Epoch || batch.From != r.watermark || batch.Through < batch.From) {
		reason = "bootstrap or change-source continuity gap"
	}
	if r.failed {
		reason = "failed transaction requires replay and reconciliation"
	}
	var input *runtimeInputs
	work := WorkCounters{}
	if reason == "" && len(batch.Paths) > 0 {
		input, reason = r.contentInputs(batch.Paths, &work)
	}
	if reason == "" && (len(batch.Paths) == 0 || input == r.inputs) {
		r.epoch, r.watermark = batch.Epoch, batch.Through
		generation := r.state.Generation
		return &OnlineResult{Result: Result{BaseGeneration: generation, TargetGeneration: generation}, Work: work, Epoch: r.epoch, Watermark: r.watermark}, nil
	}
	res, txWork, err := r.transaction(ctx, input, reason == "")
	if err != nil {
		r.failed = true
		return nil, err
	}
	txWork.BoundedContextChecks += work.BoundedContextChecks
	txWork.HashedFiles += work.HashedFiles
	txWork.DirtyHashBytes += work.DirtyHashBytes
	// The snapshot readRuntimeInputs built is work this run did, and it was
	// being dropped here: only the extractions' own rebuilds survived into the
	// reported counters, so a run that built one snapshot and a run that built
	// one and then rebuilt it reported the same number.
	txWork.TSDiscoveries += work.TSDiscoveries
	txWork.TSDiscoveriesReused += work.TSDiscoveriesReused
	txWork.TSDiscoveryRechecks = txWork.TSDiscoveryRechecks.Add(work.TSDiscoveryRechecks)
	txWork.GraphInputRebuilds += work.GraphInputRebuilds
	txWork.GraphInputRebuildsProven += work.GraphInputRebuildsProven
	r.epoch, r.watermark = batch.Epoch, batch.Through
	res.Facts = nil
	out := &OnlineResult{Result: *res, Work: txWork, Epoch: r.epoch, Watermark: r.watermark, Reconciled: reason != "", FallbackReason: reason}
	return out, nil
}

// Only the concrete TS extractor has an audited content-only detector/context
// boundary here. Other enabled extractors (including inactive custom detectors)
// deliberately reconcile until they publish an audited dependency contract.
func (r *Resident) contentInputs(paths []string, work *WorkCounters) (*runtimeInputs, string) {
	for _, ext := range r.eng.Extractors() {
		if !r.eng.Config().IsExtractorEnabled(ext.Name()) {
			continue
		}
		if swift, ok := ext.(*swiftextractor.SwiftExtractor); ok && r.inputs.detected[ext.Name()] {
			work.BoundedContextChecks++
			if swift.DeltaContext(r.abs) != r.inputs.contexts[deltaContextKey(ext)] {
				return nil, "Swift include/context changed"
			}
			continue
		}
		if _, ok := ext.(*tsextractor.TSExtractor); !ok && !r.eng.ObservedTSContentIndependent(ext, r.inputs.detected[ext.Name()]) {
			return nil, "unaudited extractor or detector: " + ext.Name()
		}
	}
	if r.inputs.angular {
		return nil, "angular composition requires reconciliation"
	}
	next := *r.inputs
	next.sources = map[string][]byte{}
	// Keep the committed hash map shared until a captured path proves that its
	// content changed. Most watcher notifications are duplicate or stale
	// same-content events; copying every captured hash for those events adds a
	// repository-sized allocation before the idle shortcut can return.
	next.hashes = r.inputs.hashes
	changed := false
	seen := map[string]bool{}
	for _, p := range paths {
		if filepath.IsAbs(p) {
			var err error
			p, err = filepath.Rel(r.abs, p)
			if err != nil {
				return nil, "external input"
			}
		}
		p = filepath.ToSlash(filepath.Clean(p))
		if seen[p] {
			continue
		}
		seen[p] = true
		prev := r.state.Files[p]
		if prev == nil || prev.TS == nil || !tsextractor.IsSessionSource(p, false) {
			return nil, "unknown path, name, or non-source input: " + p
		}
		if _, config := r.inputs.config[p]; config {
			return nil, "configuration content changed: " + p
		}
		st, err := os.Lstat(filepath.Join(r.abs, p))
		if err != nil || !st.Mode().IsRegular() {
			return nil, "source deletion, replacement, or symlink: " + p
		}
		b, err := os.ReadFile(filepath.Join(r.abs, p))
		if err != nil {
			return nil, "unreadable source: " + p
		}
		next.sources[p] = b
		work.HashedFiles++
		work.DirtyHashBytes += len(b)
		sum := sha256.Sum256(b)
		h := hex.EncodeToString(sum[:])
		if next.hashes[p] != h {
			if !changed {
				next.hashes = make(map[string]string, len(r.inputs.hashes))
				for k, v := range r.inputs.hashes {
					next.hashes[k] = v
				}
			}
			changed = true
		}
		next.hashes[p] = h
	}
	if !changed {
		return r.inputs, ""
	}
	return &next, ""
}

func (r *Resident) effectiveConfig() (map[string][]byte, error) {
	out := map[string][]byte{}
	paths := append([]string{filepath.Join(r.abs, "mcp-arch.yaml"), r.eng.Config().SourcePath}, r.opts.ConfigPaths...)
	for _, p := range paths {
		if p == "" {
			continue
		}
		if !filepath.IsAbs(p) {
			var err error
			p, err = filepath.Abs(p)
			if err != nil {
				return nil, err
			}
		}
		b, err := os.ReadFile(p)
		if os.IsNotExist(err) {
			out[p] = nil
			continue
		}
		if err != nil {
			return nil, err
		}
		out[p] = append([]byte{1}, b...)
	}
	return out, nil
}
func sameConfig(a, b map[string][]byte) bool {
	if len(a) != len(b) {
		return false
	}
	for p, v := range a {
		w, ok := b[p]
		if !ok || !bytes.Equal(v, w) {
			return false
		}
	}
	return true
}

func (r *Resident) explicitContentPath(p string) bool {
	if r.inputs == nil {
		return false
	}
	if !filepath.IsAbs(p) {
		p = filepath.Join(r.abs, p)
	}
	if !r.eng.GraphScope().Allowed(p, false) {
		return false
	}
	for _, candidate := range r.inputs.configPaths {
		if !filepath.IsAbs(candidate) {
			candidate = filepath.Join(r.abs, candidate)
		}
		if filepath.Clean(candidate) == filepath.Clean(p) {
			return true
		}
	}
	return false
}

// Reconciliation validates the policy controls captured by Build. Online content
// transactions rely on the retained watcher watermark for later policy events.
func validateGraphPolicy(eng *engine.Engine) error {
	scope := eng.GraphScope()
	if scope == nil {
		return nil
	}
	for _, dep := range scope.Policy.Dependencies() {
		b, err := os.ReadFile(dep.Path)
		digest := "missing"
		if err == nil {
			sum := sha256.Sum256(b)
			digest = hex.EncodeToString(sum[:])
		} else if !os.IsNotExist(err) {
			return err
		}
		if digest != dep.Digest {
			return fmt.Errorf("%w: graph policy input changed: %s", ErrInputsChanged, dep.Path)
		}
	}
	return nil
}
