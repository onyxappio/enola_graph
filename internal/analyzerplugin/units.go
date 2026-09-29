package analyzerplugin

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

// Observation captures every host-served input for a unit. A plugin record is
// reusable only while each answer remains identical.
type Observation struct {
	Reads     map[string]string `json:"reads,omitempty"`
	Probes    map[string]string `json:"probes,omitempty"`
	Lists     map[string]string `json:"lists,omitempty"`
	Resolves  map[string]string `json:"resolves,omitempty"`
	Summaries map[string]string `json:"summaries,omitempty"`
	Policy    string            `json:"policy,omitempty"`
}

type UnitRecord struct {
	Decl         UnitDecl               `json:"decl"`
	Observations Observation            `json:"observations,omitempty"`
	Owners       map[string]OwnerResult `json:"owners,omitempty"`
	Summary      json.RawMessage        `json:"summary,omitempty"`
	OutputDigest string                 `json:"output_digest,omitempty"`
}

type PluginRecord struct {
	Identity         string                `json:"identity"`
	RuntimeDigest    string                `json:"runtime_digest,omitempty"`
	PlanKnown        bool                  `json:"plan_known,omitempty"`
	PlanFilesDigest  string                `json:"plan_files_digest,omitempty"`
	Plan             []UnitDecl            `json:"plan,omitempty"`
	PlanObservations Observation           `json:"plan_observations,omitempty"`
	Units            map[string]UnitRecord `json:"units,omitempty"`
}

// Node is the plugin's generic, host-validated fact contribution. Owner is
// separate from File so the host cannot infer ownership from plugin spelling.
type Node struct {
	Kind      string         `json:"kind"`
	Name      string         `json:"name"`
	Owner     string         `json:"owner"`
	Line      int            `json:"line,omitempty"`
	EndLine   int            `json:"end_line,omitempty"`
	Props     map[string]any `json:"props,omitempty"`
	Relations []Relation     `json:"relations,omitempty"`
}

type Relation struct {
	Kind       string `json:"kind"`
	Target     string `json:"target"`
	TargetFile string `json:"target_file,omitempty"`
}

type Anchor struct {
	Owner          string         `json:"owner"`
	Symbol         string         `json:"symbol"`
	Line           int            `json:"line"`
	EndLine        int            `json:"end_line,omitempty"`
	SourceIdentity string         `json:"fsm_source_identity,omitempty"`
	Relations      []Relation     `json:"relations,omitempty"`
	Props          map[string]any `json:"props,omitempty"`
}

type UnitResult struct {
	Unit    string                 `json:"unit"`
	Owners  map[string]OwnerResult `json:"owners"`
	Summary any                    `json:"summary,omitempty"`
	Census  map[string]any         `json:"census,omitempty"`
}

type OwnerResult struct {
	Nodes   []Node   `json:"nodes,omitempty"`
	Anchors []Anchor `json:"anchors,omitempty"`
}

// ValidatePlan checks identity and dependency closure, then returns a stable
// topological ordering. Cycles, missing producers and duplicate ids are fatal.
func ValidatePlan(units []UnitDecl) error {
	byID := make(map[string]UnitDecl, len(units))
	for _, u := range units {
		if strings.TrimSpace(u.ID) == "" || strings.TrimSpace(u.Kind) == "" {
			return errors.New("plugin plan contains a unit without id or kind")
		}
		if strings.Contains(u.ID, "\\") || filepath.IsAbs(u.ID) || strings.Contains(u.ID, "../") || u.ID == ".." {
			return fmt.Errorf("plugin unit id %q is not a stable repository-relative identifier", u.ID)
		}
		if _, ok := byID[u.ID]; ok {
			return fmt.Errorf("plugin plan repeats unit %q", u.ID)
		}
		byID[u.ID] = u
	}
	for _, u := range units {
		seen := map[string]bool{}
		for _, dep := range u.Consumes {
			if dep == u.ID {
				return fmt.Errorf("unit %q consumes itself", u.ID)
			}
			if _, ok := byID[dep]; !ok {
				return fmt.Errorf("unit %q consumes missing producer %q", u.ID, dep)
			}
			if seen[dep] {
				return fmt.Errorf("unit %q repeats dependency %q", u.ID, dep)
			}
			seen[dep] = true
		}
	}
	if _, err := TopologicalPlan(units); err != nil {
		return err
	}
	return nil
}

// TopologicalPlan returns producers before consumers, with lexicographic order
// among simultaneously ready units for deterministic runs.
func TopologicalPlan(units []UnitDecl) ([]UnitDecl, error) {
	if err := ValidatePlanShallow(units); err != nil {
		return nil, err
	}
	byID := make(map[string]UnitDecl, len(units))
	indegree := make(map[string]int, len(units))
	consumers := make(map[string][]string, len(units))
	for _, u := range units {
		byID[u.ID] = u
		indegree[u.ID] = len(u.Consumes)
		for _, d := range u.Consumes {
			consumers[d] = append(consumers[d], u.ID)
		}
	}
	ready := make([]string, 0, len(units))
	for id, d := range indegree {
		if d == 0 {
			ready = append(ready, id)
		}
	}
	sort.Strings(ready)
	out := make([]UnitDecl, 0, len(units))
	for len(ready) > 0 {
		id := ready[0]
		ready = ready[1:]
		out = append(out, byID[id])
		for _, next := range consumers[id] {
			indegree[next]--
			if indegree[next] == 0 {
				ready = append(ready, next)
				sort.Strings(ready)
			}
		}
	}
	if len(out) != len(units) {
		return nil, errors.New("plugin summary dependency cycle")
	}
	return out, nil
}

func ValidatePlanShallow(units []UnitDecl) error {
	byID := map[string]bool{}
	for _, u := range units {
		if u.ID == "" || u.Kind == "" {
			return errors.New("plugin plan contains a unit without id or kind")
		}
		if byID[u.ID] {
			return fmt.Errorf("plugin plan repeats unit %q", u.ID)
		}
		byID[u.ID] = true
	}
	for _, u := range units {
		for _, d := range u.Consumes {
			if !byID[d] {
				return fmt.Errorf("unit %q consumes missing producer %q", u.ID, d)
			}
		}
	}
	return nil
}

// UnitKey includes the plugin identity and canonical parameters.
func UnitKey(identity string, unit UnitDecl) (string, error) {
	params, err := json.Marshal(unit.Params)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	_, _ = h.Write([]byte(identity))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(unit.ID))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write(params)
	return hex.EncodeToString(h.Sum(nil)), nil
}

// UnitDeclsEqual reports whether two plan declarations are interchangeable for
// cache reuse, including kind, params and consumes.
func UnitDeclsEqual(a, b UnitDecl) bool {
	if a.ID != b.ID || a.Kind != b.Kind {
		return false
	}
	if len(a.Consumes) != len(b.Consumes) {
		return false
	}
	for i := range a.Consumes {
		if a.Consumes[i] != b.Consumes[i] {
			return false
		}
	}
	left, err := json.Marshal(a.Params)
	if err != nil {
		return false
	}
	right, err := json.Marshal(b.Params)
	if err != nil {
		return false
	}
	return string(left) == string(right)
}

// ClonePluginRecord deep-copies a plugin cache record so a failed transaction
// cannot mutate the committed resident state through shared maps.
func ClonePluginRecord(in PluginRecord) PluginRecord {
	out := PluginRecord{
		Identity:         in.Identity,
		RuntimeDigest:    in.RuntimeDigest,
		PlanKnown:        in.PlanKnown,
		PlanFilesDigest:  in.PlanFilesDigest,
		PlanObservations: cloneObservation(in.PlanObservations),
	}
	if in.Plan != nil {
		out.Plan = make([]UnitDecl, len(in.Plan))
		for i, u := range in.Plan {
			out.Plan[i] = cloneUnitDecl(u)
		}
	}
	if in.Units != nil {
		out.Units = make(map[string]UnitRecord, len(in.Units))
		for id, rec := range in.Units {
			out.Units[id] = cloneUnitRecord(rec)
		}
	}
	return out
}

func cloneUnitDecl(in UnitDecl) UnitDecl {
	out := UnitDecl{ID: in.ID, Kind: in.Kind}
	if in.Consumes != nil {
		out.Consumes = append([]string(nil), in.Consumes...)
	}
	if in.Params != nil {
		b, err := json.Marshal(in.Params)
		if err == nil {
			_ = json.Unmarshal(b, &out.Params)
		}
	}
	return out
}

func cloneUnitRecord(in UnitRecord) UnitRecord {
	out := UnitRecord{
		Decl:         cloneUnitDecl(in.Decl),
		Observations: cloneObservation(in.Observations),
		Summary:      append(json.RawMessage(nil), in.Summary...),
		OutputDigest: in.OutputDigest,
	}
	if in.Owners != nil {
		out.Owners = make(map[string]OwnerResult, len(in.Owners))
		for owner, result := range in.Owners {
			out.Owners[owner] = cloneOwnerResult(result)
		}
	}
	return out
}

func cloneOwnerResult(in OwnerResult) OwnerResult {
	out := OwnerResult{}
	if in.Nodes != nil {
		out.Nodes = append([]Node(nil), in.Nodes...)
		for i := range out.Nodes {
			out.Nodes[i].Props = cloneJSONMap(out.Nodes[i].Props)
			out.Nodes[i].Relations = append([]Relation(nil), out.Nodes[i].Relations...)
		}
	}
	if in.Anchors != nil {
		out.Anchors = append([]Anchor(nil), in.Anchors...)
		for i := range out.Anchors {
			out.Anchors[i].Props = cloneJSONMap(out.Anchors[i].Props)
			out.Anchors[i].Relations = append([]Relation(nil), out.Anchors[i].Relations...)
		}
	}
	return out
}

func cloneObservation(in Observation) Observation {
	return Observation{
		Reads:     cloneStringMap(in.Reads),
		Probes:    cloneStringMap(in.Probes),
		Lists:     cloneStringMap(in.Lists),
		Resolves:  cloneStringMap(in.Resolves),
		Summaries: cloneStringMap(in.Summaries),
		Policy:    in.Policy,
	}
}

func cloneStringMap(in map[string]string) map[string]string {
	if in == nil {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func cloneJSONMap(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	b, err := json.Marshal(in)
	if err != nil {
		return nil
	}
	var out map[string]any
	if json.Unmarshal(b, &out) != nil {
		return nil
	}
	return out
}

// CanonicalDigest returns a stable digest of JSON-compatible output.
func CanonicalDigest(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

// CanonicalizeResult removes plugin array ordering from the persisted graph
// contribution and its digest while preserving distinct occurrences. Empty
// anchor owners inherit their enclosing output owner before sorting.
func CanonicalizeResult(result UnitResult) UnitResult {
	for owner, contribution := range result.Owners {
		for i := range contribution.Anchors {
			if contribution.Anchors[i].Owner == "" {
				contribution.Anchors[i].Owner = owner
			}
		}
		for i := range contribution.Nodes {
			sort.SliceStable(contribution.Nodes[i].Relations, func(a, b int) bool {
				return relationKey(contribution.Nodes[i].Relations[a]) < relationKey(contribution.Nodes[i].Relations[b])
			})
		}
		sort.SliceStable(contribution.Nodes, func(a, b int) bool {
			left, right := contribution.Nodes[a], contribution.Nodes[b]
			if left.Kind != right.Kind {
				return left.Kind < right.Kind
			}
			if left.Name != right.Name {
				return left.Name < right.Name
			}
			if left.Line != right.Line {
				return left.Line < right.Line
			}
			if left.EndLine != right.EndLine {
				return left.EndLine < right.EndLine
			}
			return occurrence(left) < occurrence(right)
		})
		for i := range contribution.Anchors {
			sort.SliceStable(contribution.Anchors[i].Relations, func(a, b int) bool {
				return relationKey(contribution.Anchors[i].Relations[a]) < relationKey(contribution.Anchors[i].Relations[b])
			})
		}
		sort.SliceStable(contribution.Anchors, func(a, b int) bool {
			return anchorLess(contribution.Anchors[a], contribution.Anchors[b])
		})
		result.Owners[owner] = contribution
	}
	return result
}

func anchorLess(left, right Anchor) bool {
	if left.Symbol != right.Symbol {
		return left.Symbol < right.Symbol
	}
	if left.Line != right.Line {
		return left.Line < right.Line
	}
	if left.EndLine != right.EndLine {
		return left.EndLine < right.EndLine
	}
	if left.SourceIdentity != right.SourceIdentity {
		return left.SourceIdentity < right.SourceIdentity
	}
	if left.Owner != right.Owner {
		return left.Owner < right.Owner
	}
	leftProps := canonicalJSONKey(left.Props)
	rightProps := canonicalJSONKey(right.Props)
	if leftProps != rightProps {
		return leftProps < rightProps
	}
	return relationsKey(left.Relations) < relationsKey(right.Relations)
}

// AnchorSortKey is a total order over canonical anchor content for host-side
// multi-plugin evidence ordering.
func AnchorSortKey(a Anchor) string {
	return strings.Join([]string{
		a.Symbol,
		fmt.Sprintf("%d", a.Line),
		fmt.Sprintf("%d", a.EndLine),
		a.SourceIdentity,
		a.Owner,
		canonicalJSONKey(a.Props),
		relationsKey(a.Relations),
	}, "\x00")
}

func relationKey(relation Relation) string {
	return relation.Kind + "\x00" + relation.Target + "\x00" + relation.TargetFile
}

func relationsKey(relations []Relation) string {
	if len(relations) == 0 {
		return ""
	}
	parts := make([]string, len(relations))
	for i, relation := range relations {
		parts[i] = relationKey(relation)
	}
	return strings.Join(parts, "\x01")
}

func canonicalJSONKey(v any) string {
	if v == nil {
		return ""
	}
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprint(v)
	}
	return string(b)
}

func occurrence(node Node) string {
	if node.Props == nil {
		return ""
	}
	value, ok := node.Props["occurrence"]
	if !ok || value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}
