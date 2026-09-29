package analyzerplugin

import (
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/enola-labs/enola/internal/facts"
)

// ValidateResult enforces the host-registered vocabulary, machine claims,
// owner domain, and per-owner identity uniqueness before a result can enter
// durable state or a frozen replacement plan.
func ValidateResult(p Loaded, unit UnitDecl, result UnitResult) error {
	if result.Unit != unit.ID {
		return fmt.Errorf("plugin %q returned unit %q while running %q", p.Manifest.Name, result.Unit, unit.ID)
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
			if err := validateNode(p.Manifest.Name, claims, owner, node, seen); err != nil {
				return fmt.Errorf("plugin %q unit %q: %w", p.Manifest.Name, unit.ID, err)
			}
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

func validateNode(plugin string, claims map[string]bool, owner string, n Node, seen map[string]map[string]bool) error {
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
		for _, suffix := range expandBraces(pattern[:start] + choice + pattern[end+1:]) {
			out = append(out, suffix)
		}
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
