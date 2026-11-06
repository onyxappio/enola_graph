package analyzerplugin

import (
	"fmt"
	"math"
	"path"
	"sort"
	"strings"

	"github.com/enola-labs/enola/internal/facts"
)

// ValidateResult enforces the host-registered vocabulary, machine claims,
// owner domain, and per-owner identity uniqueness before a result can enter
// durable state or a frozen replacement plan.
func ValidateResult(p Loaded, unit UnitDecl, result UnitResult) error {
	_, err := ValidatedCensusParsedFiles(p, unit, result)
	return err
}

// ValidatedCensusParsedFiles validates a result and returns its V2 parsed-files
// census exactly once for host accounting. Census is not part of the V1
// protocol, so V1 results retain their historical behavior and return zero.
func ValidatedCensusParsedFiles(p Loaded, unit UnitDecl, result UnitResult) (int, error) {
	parsedFiles := 0
	if p.Manifest.API == GoAPIVersion {
		var err error
		parsedFiles, err = CensusParsedFiles(result.Census)
		if err != nil {
			return 0, fmt.Errorf("plugin %q unit %q census: %w", p.Manifest.Name, unit.ID, err)
		}
	}
	if err := validateResult(p, unit, result); err != nil {
		return 0, err
	}
	return parsedFiles, nil
}

func validateResult(p Loaded, unit UnitDecl, result UnitResult) error {
	if result.Unit != unit.ID {
		return fmt.Errorf("plugin %q returned unit %q while running %q", p.Manifest.Name, result.Unit, unit.ID)
	}
	if p.Manifest.API == GoAPIVersion {
		return validateGenericResult(p, unit, result)
	}
	claims := map[string]bool{}
	for _, id := range p.Manifest.Claims.Machines {
		claims[id] = true
	}
	for owner, output := range result.Owners {
		if !validOwner(owner) || !ownerAllowed(p.Manifest.OwnerDomain, owner) {
			return fmt.Errorf("plugin %q unit %q wrote outside owner_domain: %q", p.Manifest.Name, unit.ID, owner)
		}
		seen := map[string]map[string]bool{}
		for _, node := range output.Nodes {
			if node.Owner != "" && node.Owner != owner {
				return fmt.Errorf("plugin %q unit %q node %q claims owner %q, enclosing owner is %q", p.Manifest.Name, unit.ID, node.Name, node.Owner, owner)
			}
			// V1 remains deliberately unchanged: its historical FSM-only relation
			// table does not include the legacy containment edge. V2 plugins which
			// explicitly opt into the FSM vocabulary get that one legacy shape.
			if err := validateNode(p.Manifest.Name, claims, owner, node, seen, false); err != nil {
				return fmt.Errorf("plugin %q unit %q: %w", p.Manifest.Name, unit.ID, err)
			}
		}
		if len(output.Enrichments) > 0 && p.Manifest.API != GoAPIVersion {
			return fmt.Errorf("plugin %q unit %q used generic enrichments outside enola.plugin/v2", p.Manifest.Name, unit.ID)
		}
		for _, anchor := range output.Anchors {
			anchorOwner := anchor.Owner
			if anchorOwner == "" {
				anchorOwner = owner
			}
			if anchorOwner != owner {
				return fmt.Errorf("plugin %q unit %q anchor %q claims owner %q, enclosing owner is %q", p.Manifest.Name, unit.ID, anchor.Symbol, anchor.Owner, owner)
			}
			if anchor.Symbol == "" || anchor.Line < 1 || (anchor.EndLine > 0 && anchor.EndLine < anchor.Line) {
				return fmt.Errorf("invalid anchor in owner %q", owner)
			}
			for _, rel := range anchor.Relations {
				want, ok := facts.FSMRelationTargetKind(rel.Kind)
				if !ok {
					return fmt.Errorf("anchor %q uses unsupported relation %q", anchor.Symbol, rel.Kind)
				}
				if rel.Target == "" {
					return fmt.Errorf("anchor %q has an empty relation target", anchor.Symbol)
				}
				if strings.HasPrefix(want, "fsm_") {
					if !nameHasKind(rel.Target, want) {
						return fmt.Errorf("anchor relation %s target %q is not a %s identity", rel.Kind, rel.Target, want)
					}
					targetMachine := machineOf(rel.Target)
					if targetMachine == "" {
						targetMachine = rel.Target
					}
					if !claims[targetMachine] {
						return fmt.Errorf("anchor relation %s targets unclaimed machine %q", rel.Kind, targetMachine)
					}
				}
			}
		}
	}
	return nil
}

func validateGenericResult(p Loaded, unit UnitDecl, result UnitResult) error {
	seen := map[string]map[string]bool{}
	claims := map[string]bool{}
	for _, id := range p.Manifest.Claims.Machines {
		claims[id] = true
	}
	fsmVocabulary := UsesFSMVocabulary(p.Manifest)
	for owner, output := range result.Owners {
		if !validOwner(owner) || !ownerAllowed(p.Manifest.OwnerDomain, owner) {
			return fmt.Errorf("plugin %q unit %q wrote outside owner_domain: %q", p.Manifest.Name, unit.ID, owner)
		}
		for _, node := range output.Nodes {
			if node.Owner != "" && node.Owner != owner {
				return fmt.Errorf("plugin %q unit %q node %q claims owner %q, enclosing owner is %q", p.Manifest.Name, unit.ID, node.Name, node.Owner, owner)
			}
			if fsmVocabulary && IsFSMVocabularyNodeKind(node.Kind) {
				if err := validateNode(p.Manifest.Name, claims, owner, node, seen, true); err != nil {
					return fmt.Errorf("plugin %q unit %q: %w", p.Manifest.Name, unit.ID, err)
				}
				continue
			}
			if fsmVocabulary && node.Kind == facts.KindSymbol {
				if err := validateFSMNestedDeclarationSymbol(owner, node, seen); err != nil {
					return fmt.Errorf("plugin %q unit %q: %w", p.Manifest.Name, unit.ID, err)
				}
				continue
			}
			if node.Kind == "" || !validLocalKind(node.Kind) || node.Name == "" {
				return fmt.Errorf("plugin %q unit %q node must have a stable local kind and non-empty name", p.Manifest.Name, unit.ID)
			}
			if node.Line < 0 || node.EndLine < 0 || (node.EndLine > 0 && node.Line > 0 && node.EndLine < node.Line) {
				return fmt.Errorf("plugin %q unit %q node %q has invalid source span", p.Manifest.Name, unit.ID, node.Name)
			}
			key := node.Kind + "\x00" + node.Name + "\x00" + owner
			if seen[key] == nil {
				seen[key] = map[string]bool{}
			}
			occ := occurrence(node)
			if seen[key][occ] || (occ == "" && len(seen[key]) > 0) || seen[key][""] {
				return fmt.Errorf("plugin %q unit %q repeats node identity %s/%s in owner %s without distinct occurrence", p.Manifest.Name, unit.ID, node.Kind, node.Name, owner)
			}
			seen[key][occ] = true
			for _, relation := range node.Relations {
				if err := validateGenericRelation(relation); err != nil {
					return fmt.Errorf("plugin %q unit %q node %q: %w", p.Manifest.Name, unit.ID, node.Name, err)
				}
			}
		}
		seenEnrichments := map[string]bool{}
		for _, enrichment := range output.Enrichments {
			enrichmentOwner := enrichment.Owner
			if enrichmentOwner == "" {
				enrichmentOwner = owner
			}
			if enrichmentOwner != owner || !validOwner(enrichmentOwner) {
				return fmt.Errorf("plugin %q unit %q enrichment %s/%s targets owner %q outside enclosing owner %q", p.Manifest.Name, unit.ID, enrichment.Kind, enrichment.Name, enrichmentOwner, owner)
			}
			if enrichment.Name == "" || (!isHostKind(enrichment.Kind) && !validLocalKind(enrichment.Kind) && !validNamespacedKind(enrichment.Kind)) {
				return fmt.Errorf("plugin %q unit %q enrichment requires a valid target kind and non-empty name", p.Manifest.Name, unit.ID)
			}
			key := enrichment.Kind + "\x00" + enrichment.Name + "\x00" + enrichmentOwner
			if seenEnrichments[key] {
				return fmt.Errorf("plugin %q unit %q repeats enrichment target %s/%s in owner %s", p.Manifest.Name, unit.ID, enrichment.Kind, enrichment.Name, owner)
			}
			seenEnrichments[key] = true
			for _, relation := range enrichment.Relations {
				if err := validateGenericRelation(relation); err != nil {
					return fmt.Errorf("plugin %q unit %q enrichment %s/%s: %w", p.Manifest.Name, unit.ID, enrichment.Kind, enrichment.Name, err)
				}
			}
		}
		if len(output.Anchors) > 0 && !containsString(p.Manifest.Vocabularies, FSMVocabularyV1) {
			return fmt.Errorf("plugin %q unit %q used FSM anchors without declaring %s", p.Manifest.Name, unit.ID, FSMVocabularyV1)
		}
		for _, anchor := range output.Anchors {
			if err := validateGenericAnchor(p, owner, anchor); err != nil {
				return fmt.Errorf("plugin %q unit %q: %w", p.Manifest.Name, unit.ID, err)
			}
		}
	}
	return nil
}

// UsesFSMVocabulary reports whether a V2 plugin opted into the host's FSM
// vocabulary compatibility contract.
func UsesFSMVocabulary(m Manifest) bool {
	return m.API == GoAPIVersion && containsString(m.Vocabularies, FSMVocabularyV1)
}

// IsFSMVocabularyNodeKind is a node kind that the FSM vocabulary owns. These
// remain host kinds when a V2 plugin has explicitly declared enola.fsm@1.
func IsFSMVocabularyNodeKind(kind string) bool {
	return kind == facts.KindExtraction || isFSMKind(kind)
}

// IsFSMNestedDeclarationSymbol identifies the single ordinary host symbol
// shape admitted by enola.fsm@1. It is the synthetic nested-callable helper
// emitted by the legacy TypeScript FSM binder; accepting it lets V2 anchors
// resolve with exactly the same source identity without opening the host
// symbol vocabulary to arbitrary plugin output.
func IsFSMNestedDeclarationSymbol(owner string, n Node) bool {
	if n.Kind != facts.KindSymbol || n.Line < 1 || n.EndLine < n.Line || len(n.Relations) != 0 || len(n.Props) != 3 {
		return false
	}
	module := path.Dir(owner)
	if !strings.HasPrefix(n.Name, module+".") || strings.TrimPrefix(n.Name, module+".") == "" {
		return false
	}
	return n.Props["language"] == "typescript" &&
		n.Props["symbol_kind"] == facts.SymbolFunc &&
		n.Props["fsm_source_binding"] == "tree_sitter_nested_declaration"
}

func validateFSMNestedDeclarationSymbol(owner string, n Node, seen map[string]map[string]bool) error {
	// IsFSMNestedDeclarationSymbol includes the exact prop, source-span, owner
	// module and relation restrictions. Keep an explicit diagnostic here so a
	// near miss cannot silently become a generic namespaced node.
	if !IsFSMNestedDeclarationSymbol(owner, n) {
		return fmt.Errorf("symbol %q is not the permitted nested-declaration FSM helper shape", n.Name)
	}
	key := n.Kind + "\x00" + n.Name + "\x00" + owner
	if seen[key] == nil {
		seen[key] = map[string]bool{}
	}
	occ := occurrence(n)
	if seen[key][occ] || (occ == "" && len(seen[key]) > 0) || seen[key][""] {
		return fmt.Errorf("duplicate node identity %s/%s in owner %s without distinct occurrence", n.Kind, n.Name, owner)
	}
	seen[key][occ] = true
	return nil
}

// CensusParsedFiles returns the optional parsed-files census supplied by a
// unit. Protocol JSON numbers arrive as float64; accepting integral Go values
// as well keeps direct SDK and host tests honest.
func CensusParsedFiles(census map[string]any) (int, error) {
	if len(census) == 0 {
		return 0, nil
	}
	raw, ok := census["parsed_files"]
	if !ok || raw == nil {
		return 0, nil
	}
	var value int64
	switch n := raw.(type) {
	case int:
		value = int64(n)
	case int64:
		value = n
	case float64:
		if math.Trunc(n) != n || n > float64(math.MaxInt) || n < 0 {
			return 0, fmt.Errorf("parsed_files must be a non-negative integer")
		}
		value = int64(n)
	default:
		return 0, fmt.Errorf("parsed_files must be a non-negative integer")
	}
	if value < 0 || value > int64(math.MaxInt) {
		return 0, fmt.Errorf("parsed_files must be a non-negative integer")
	}
	return int(value), nil
}

func validateGenericAnchor(p Loaded, owner string, anchor Anchor) error {
	anchorOwner := anchor.Owner
	if anchorOwner == "" {
		anchorOwner = owner
	}
	if anchorOwner != owner || anchor.Symbol == "" || anchor.Line < 1 || (anchor.EndLine > 0 && anchor.EndLine < anchor.Line) {
		return fmt.Errorf("invalid FSM anchor %q in owner %q", anchor.Symbol, owner)
	}
	claims := map[string]bool{}
	for _, machine := range p.Manifest.Claims.Machines {
		claims[machine] = true
	}
	for _, relation := range anchor.Relations {
		want, ok := facts.FSMRelationTargetKind(relation.Kind)
		if !ok || relation.Target == "" {
			return fmt.Errorf("anchor %q uses unsupported relation %q", anchor.Symbol, relation.Kind)
		}
		if relation.TargetKind != "" && relation.TargetKind != want {
			return fmt.Errorf("anchor %q relation %s targets %q, not %q", anchor.Symbol, relation.Kind, relation.TargetKind, want)
		}
		if strings.HasPrefix(want, "fsm_") {
			if !nameHasKind(relation.Target, want) {
				return fmt.Errorf("anchor relation %s target %q is not a %s identity", relation.Kind, relation.Target, want)
			}
			machine := machineOf(relation.Target)
			if machine == "" {
				machine = relation.Target
			}
			if !claims[machine] {
				return fmt.Errorf("anchor relation %s targets unclaimed machine %q", relation.Kind, machine)
			}
		}
	}
	return nil
}

func validateGenericRelation(relation Relation) error {
	if !validLocalKind(relation.Kind) || relation.Target == "" {
		return fmt.Errorf("relation requires a stable local kind and non-empty target")
	}
	if relation.TargetKind == "" || (!isHostKind(relation.TargetKind) && !validLocalKind(relation.TargetKind) && !validNamespacedKind(relation.TargetKind)) {
		return fmt.Errorf("relation %q requires a valid target_kind", relation.Kind)
	}
	if relation.TargetFile != "" && !validOwner(relation.TargetFile) {
		return fmt.Errorf("relation %q has invalid target_file %q", relation.Kind, relation.TargetFile)
	}
	return nil
}

func validNamespacedKind(kind string) bool {
	pluginLocal := strings.TrimPrefix(kind, "plugin:")
	if pluginLocal == kind {
		return false
	}
	plugin, local, ok := strings.Cut(pluginLocal, ":")
	return ok && validID(plugin) && validLocalKind(local)
}

func validLocalKind(kind string) bool {
	if kind == "" {
		return false
	}
	for _, r := range kind {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '_' && r != '-' && r != '.' {
			return false
		}
	}
	return true
}

func isHostKind(kind string) bool {
	switch kind {
	case facts.KindModule, facts.KindSymbol, facts.KindRoute, facts.KindStorage, facts.KindDependency,
		facts.KindService, facts.KindIntent, facts.KindExtraction, facts.KindAssociation,
		facts.KindTestRef, facts.KindFileRef, facts.KindLint, facts.KindFSMMachine,
		facts.KindFSMState, facts.KindFSMEvent, facts.KindFSMTransition, facts.KindFSMCommand:
		return true
	default:
		return false
	}
}

// GenericFactKind qualifies a plugin-local fact kind so two plugins can use the
// same short domain name without colliding in the shared graph.
func GenericFactKind(plugin, local string) string { return "plugin:" + plugin + ":" + local }

// GenericRelationKind qualifies a plugin-local relationship name.
func GenericRelationKind(plugin, local string) string { return "plugin:" + plugin + ":" + local }

// GenericTargetKind resolves a plugin-local target kind while preserving
// references to Enola-owned fact kinds.
func GenericTargetKind(plugin, local string) string {
	if isHostKind(local) || strings.HasPrefix(local, "plugin:") {
		return local
	}
	return GenericFactKind(plugin, local)
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func validateNode(plugin string, claims map[string]bool, owner string, n Node, seen map[string]map[string]bool, allowLegacyDeclares bool) error {
	if n.Name == "" || n.Kind == "" {
		return fmt.Errorf("node in %q has empty kind or name", owner)
	}
	if n.Line < 0 || n.EndLine < 0 || (n.EndLine > 0 && n.Line > 0 && n.EndLine < n.Line) {
		return fmt.Errorf("node %q has invalid source span", n.Name)
	}
	if n.Kind == facts.KindExtraction {
		if !strings.HasPrefix(n.Name, "plugin:"+plugin+":") {
			return fmt.Errorf("coverage node %q is outside plugin namespace", n.Name)
		}
	} else if !isFSMKind(n.Kind) {
		return fmt.Errorf("unregistered node kind %q", n.Kind)
	} else if err := validateFSMName(claims, n.Kind, n.Name); err != nil {
		return err
	}
	key := n.Kind + "\x00" + n.Name + "\x00" + owner
	if seen[key] == nil {
		seen[key] = map[string]bool{}
	}
	occ := occurrence(n)
	if seen[key][occ] || (occ == "" && len(seen[key]) > 0) || seen[key][""] {
		return fmt.Errorf("duplicate node identity %s/%s in owner %s without distinct occurrence", n.Kind, n.Name, owner)
	}
	seen[key][occ] = true
	for _, rel := range n.Relations {
		if allowLegacyDeclares && rel.Kind == facts.RelDeclares {
			if err := validateV2FSMLegacyDeclares(claims, owner, n, rel); err != nil {
				return err
			}
			continue
		}
		want, ok := facts.FSMRelationTargetKind(rel.Kind)
		if !ok {
			return fmt.Errorf("node %q uses unsupported relation %q", n.Name, rel.Kind)
		}
		if rel.Target == "" {
			return fmt.Errorf("node %q has empty target for %s", n.Name, rel.Kind)
		}
		if strings.HasPrefix(want, "fsm_") {
			if !nameHasKind(rel.Target, want) {
				return fmt.Errorf("relation %s target %q is not a %s identity", rel.Kind, rel.Target, want)
			}
			targetMachine := machineOf(rel.Target)
			if targetMachine == "" {
				targetMachine = rel.Target
			}
			if !claims[targetMachine] {
				return fmt.Errorf("relation %s targets unclaimed machine %q", rel.Kind, targetMachine)
			}
			if machine := machineOf(n.Name); machine != "" && machineOf(rel.Target) != "" && machineOf(rel.Target) != machine {
				return fmt.Errorf("relation %s crosses claimed machines %q and %q", rel.Kind, machine, machineOf(rel.Target))
			}
		}
	}
	return nil
}

// validateV2FSMLegacyDeclares accepts the containment relations emitted by the
// legacy FSM extractor, but only for a V2 plugin which explicitly adopted the
// FSM vocabulary. They are intentionally not added to FSMRelationTargetKind:
// that table describes typed fsm_* relations, while declares keeps its
// repository-wide, un-namespaced graph meaning.
func validateV2FSMLegacyDeclares(claims map[string]bool, owner string, n Node, rel Relation) error {
	if rel.Target == "" {
		return fmt.Errorf("node %q has empty target for %s", n.Name, rel.Kind)
	}
	if rel.TargetFile != "" {
		return fmt.Errorf("node %q relation %s must not set target_file", n.Name, rel.Kind)
	}
	switch n.Kind {
	case facts.KindFSMState, facts.KindFSMEvent, facts.KindFSMCommand:
		// rule_table.go emitted this untyped relation from each member to the
		// configured machine. A target kind would change that legacy shape.
		if rel.TargetKind != "" {
			return fmt.Errorf("node %q relation %s must not set target_kind", n.Name, rel.Kind)
		}
		if !claims[rel.Target] {
			return fmt.Errorf("relation %s targets unclaimed machine %q", rel.Kind, rel.Target)
		}
		return nil
	case facts.KindFSMMachine:
		// A machine declares the module containing its definition. Legacy facts
		// omit target_kind; V2 producers may make the module type explicit.
		if rel.TargetKind != "" && rel.TargetKind != facts.KindModule {
			return fmt.Errorf("node %q relation %s targets %q, not %q", n.Name, rel.Kind, rel.TargetKind, facts.KindModule)
		}
		if rel.Target != path.Dir(owner) {
			return fmt.Errorf("node %q relation %s targets module %q, want owner module %q", n.Name, rel.Kind, rel.Target, path.Dir(owner))
		}
		return nil
	default:
		return fmt.Errorf("node %q kind %q cannot use relation %q", n.Name, n.Kind, rel.Kind)
	}
}

func isFSMKind(kind string) bool {
	switch kind {
	case facts.KindFSMMachine, facts.KindFSMState, facts.KindFSMEvent, facts.KindFSMTransition, facts.KindFSMCommand:
		return true
	}
	return false
}

func validateFSMName(claims map[string]bool, kind, name string) error {
	if kind == facts.KindFSMMachine {
		if !claims[name] {
			return fmt.Errorf("plugin emitted unclaimed machine %q", name)
		}
		return nil
	}
	prefix, member, ok := strings.Cut(name, "/")
	if !ok || !claims[prefix] {
		return fmt.Errorf("FSM node %q does not belong to a claimed machine", name)
	}
	memberKind, memberValue, ok := strings.Cut(member, ":")
	if !ok || memberValue == "" {
		return fmt.Errorf("FSM member %q must be machine/<kind>:<value>", name)
	}
	want := map[string]string{"state": facts.KindFSMState, "event": facts.KindFSMEvent, "transition": facts.KindFSMTransition, "command": facts.KindFSMCommand}[memberKind]
	if want == "" || want != kind {
		return fmt.Errorf("FSM member %q is incompatible with fact kind %q", name, kind)
	}
	return nil
}

func nameHasKind(name, kind string) bool {
	if kind == facts.KindFSMMachine {
		return !strings.Contains(name, "/")
	}
	_, member, ok := strings.Cut(name, "/")
	if !ok {
		return false
	}
	want := strings.TrimPrefix(kind, "fsm_") + ":"
	return strings.HasPrefix(member, want)
}

func machineOf(name string) string {
	id, _, _ := strings.Cut(name, "/")
	if id == name {
		return ""
	}
	return id
}

func validOwner(owner string) bool {
	if owner == "" || strings.Contains(owner, "\\") || path.IsAbs(owner) {
		return false
	}
	clean := path.Clean(owner)
	return clean == owner && clean != "." && clean != ".." && !strings.HasPrefix(clean, "../")
}

func ownerAllowed(globs []string, owner string) bool {
	for _, g := range globs {
		if MatchRepositoryGlob(g, owner) {
			return true
		}
	}
	return false
}

// ValidateRepositoryGlob checks a repository-relative path pattern used by a
// plugin callback or manifest.
func ValidateRepositoryGlob(glob string) error { return validateRepoGlob(glob) }

// MatchRepositoryGlob uses the same ** and brace semantics for callback name
// queries and owner-domain validation.
func MatchRepositoryGlob(glob, owner string) bool { return matchRepoGlob(glob, owner) }

func matchRepoGlob(glob, owner string) bool {
	for _, expanded := range expandBraces(glob) {
		if matchGlobSegments(strings.Split(expanded, "/"), strings.Split(owner, "/")) {
			return true
		}
	}
	return false
}

func matchGlobSegments(pattern, value []string) bool {
	if len(pattern) == 0 {
		return len(value) == 0
	}
	if pattern[0] == "**" {
		if matchGlobSegments(pattern[1:], value) {
			return true
		}
		return len(value) > 0 && matchGlobSegments(pattern, value[1:])
	}
	if len(value) == 0 {
		return false
	}
	matched, err := path.Match(pattern[0], value[0])
	return err == nil && matched && matchGlobSegments(pattern[1:], value[1:])
}

func expandBraces(pattern string) []string {
	start := strings.IndexByte(pattern, '{')
	if start < 0 {
		return []string{pattern}
	}
	end := strings.IndexByte(pattern[start:], '}')
	if end < 0 {
		return []string{pattern}
	}
	end += start
	var out []string
	for _, choice := range strings.Split(pattern[start+1:end], ",") {
		out = append(out, expandBraces(pattern[:start]+choice+pattern[end+1:])...)
	}
	return out
}

// CanonicalOwners returns stable owner names for deterministic state writes.
func CanonicalOwners(m map[string]OwnerResult) []string {
	out := make([]string, 0, len(m))
	for owner := range m {
		out = append(out, owner)
	}
	sort.Strings(out)
	return out
}
