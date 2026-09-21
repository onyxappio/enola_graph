package graphsession

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/enola-labs/enola/internal/facts"
	"github.com/enola-labs/enola/internal/graphstream"
)

// Consumer is an in-memory reference consumer. It stages per-replacement and
// swaps an owner's contribution only after EndReplace with matching batch
// count, digest, base generation, repo/context identity, and owner scope.
// Local-phase batches are ignored; only resolved batches become authoritative.
type Consumer struct {
	Owners         map[string][]graphstream.Node
	Edges          map[string][]graphstream.Edge
	RepoID         string
	ContextID      string
	LastGeneration int64
	LastRunID      string
	open           map[string]*openReplace
	applied        map[string]bool
	appliedMsg     map[string]bool
	seen           map[string][]byte
	seed           seedBase
}

type seedBase struct {
	RepoID     string
	ContextID  string
	Generation int64
	RunID      string
}

type openReplace struct {
	begin     graphstream.BeginReplace
	seq       map[int]graphstream.Batch
	raw       map[int][]byte
	scope     map[string]graphstream.OwnerRef
	maxSeq    int
	epoch     bool
	growing   bool
	scopeMode string
}

func NewConsumer() *Consumer {
	return &Consumer{
		Owners:     map[string][]graphstream.Node{},
		Edges:      map[string][]graphstream.Edge{},
		open:       map[string]*openReplace{},
		applied:    map[string]bool{},
		appliedMsg: map[string]bool{},
		seen:       map[string][]byte{},
	}
}

func (c *Consumer) ApplyRecords(records []graphstream.Recorded) error {
	for _, r := range records {
		if err := c.Apply(r); err != nil {
			return err
		}
	}
	return nil
}

// Apply consumes one recorded protocol payload.
func (c *Consumer) Apply(r graphstream.Recorded) error {
	if r.MsgID != "" {
		if prev, ok := c.seen[r.MsgID]; ok {
			if !bytes.Equal(prev, r.Payload) {
				return fmt.Errorf("consumer: msg id %s reused with different payload", r.MsgID)
			}
			if c.appliedMsg[r.MsgID] {
				return nil
			}
			// Same bytes were seen but not successfully applied; revalidate.
		} else {
			c.seen[r.MsgID] = append([]byte(nil), r.Payload...)
		}
	}
	var probe struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(r.Payload, &probe); err != nil {
		return err
	}
	var err error
	switch probe.Type {
	case graphstream.TypeBeginReplace:
		var b graphstream.BeginReplace
		if err = json.Unmarshal(r.Payload, &b); err != nil {
			return err
		}
		err = c.applyBegin(b)
	case graphstream.TypeBatch:
		var b graphstream.Batch
		if err = json.Unmarshal(r.Payload, &b); err != nil {
			return err
		}
		err = c.applyBatch(b, r.Payload)
	case graphstream.TypeEndReplace:
		var e graphstream.EndReplace
		if err = json.Unmarshal(r.Payload, &e); err != nil {
			return err
		}
		err = c.applyEnd(e, r.Payload)
	default:
		return nil
	}
	if err != nil {
		return err
	}
	if r.MsgID != "" {
		c.appliedMsg[r.MsgID] = true
	}
	return nil
}

func (c *Consumer) applyBegin(b graphstream.BeginReplace) error {
	if b.RunID == "" {
		return fmt.Errorf("consumer: begin missing run_id")
	}
	if c.applied[b.RunID] {
		return nil
	}
	if _, open := c.open[b.RunID]; open {
		return fmt.Errorf("consumer: duplicate begin for %s", b.RunID)
	}
	if c.RepoID != "" && b.RepoID != "" && c.RepoID != b.RepoID {
		return fmt.Errorf("consumer: repo %q does not match %q", b.RepoID, c.RepoID)
	}
	if b.ForkBaseRunID != "" || b.ForkBaseContextID != "" {
		if err := c.checkForkBase(b); err != nil {
			return err
		}
	} else if c.ContextID != "" && b.ContextID != "" && c.ContextID != b.ContextID {
		return fmt.Errorf("consumer: context %q does not match %q", b.ContextID, c.ContextID)
	}
	if b.BaseGeneration != c.LastGeneration {
		return fmt.Errorf("consumer: base generation %d != last applied %d", b.BaseGeneration, c.LastGeneration)
	}
	st := &openReplace{
		begin:     b,
		seq:       map[int]graphstream.Batch{},
		raw:       map[int][]byte{},
		scope:     map[string]graphstream.OwnerRef{},
		epoch:     b.Phase == graphstream.PhaseEpoch,
		scopeMode: b.ScopeMode,
		growing:   b.ScopeMode == graphstream.ScopeModeIncremental,
	}
	for _, o := range b.OwnerScope {
		st.scope[o.String()] = o
	}
	c.open[b.RunID] = st
	if c.RepoID == "" {
		c.RepoID = b.RepoID
	}
	if b.ForkBaseRunID != "" {
		c.ContextID = b.ContextID
	} else if c.ContextID == "" {
		c.ContextID = b.ContextID
	}
	return nil
}

func (c *Consumer) checkForkBase(b graphstream.BeginReplace) error {
	if c.seed.RunID == "" {
		return fmt.Errorf("consumer: fork begin requires SeedFrom the exact completed base (missing baseline)")
	}
	if c.seed.RepoID != b.ForkBaseRepoID {
		return fmt.Errorf("consumer: fork base repo %q does not match seeded %q", b.ForkBaseRepoID, c.seed.RepoID)
	}
	if c.seed.ContextID != b.ForkBaseContextID {
		return fmt.Errorf("consumer: fork base context %q does not match seeded %q", b.ForkBaseContextID, c.seed.ContextID)
	}
	if c.seed.Generation != b.ForkBaseGeneration {
		return fmt.Errorf("consumer: fork base generation %d does not match seeded %d", b.ForkBaseGeneration, c.seed.Generation)
	}
	if c.seed.RunID != b.ForkBaseRunID {
		return fmt.Errorf("consumer: fork base run %q does not match seeded %q", b.ForkBaseRunID, c.seed.RunID)
	}
	if b.ForkBaseGeneration != c.LastGeneration {
		return fmt.Errorf("consumer: fork base generation %d != last applied %d", b.ForkBaseGeneration, c.LastGeneration)
	}
	return nil
}

// SeedFrom copies the committed graph of a completed base consumer so a
// branch replacement can be applied. The caller must already hold that exact
// baseline; Enola does not copy a database.
func (c *Consumer) SeedFrom(src *Consumer) error {
	if c == nil {
		return fmt.Errorf("consumer: nil destination")
	}
	if src == nil || src.LastRunID == "" || !src.complete() {
		return fmt.Errorf("consumer: missing completed base")
	}
	cloned := src.cloneGraph()
	cloned.seed = seedBase{
		RepoID:     src.RepoID,
		ContextID:  src.ContextID,
		Generation: src.LastGeneration,
		RunID:      src.LastRunID,
	}
	*c = *cloned
	return nil
}

func (c *Consumer) complete() bool {
	return c.LastGeneration > 0 && c.LastRunID != "" && len(c.open) == 0
}

func (c *Consumer) cloneGraph() *Consumer {
	out := NewConsumer()
	out.RepoID = c.RepoID
	out.ContextID = c.ContextID
	out.LastGeneration = c.LastGeneration
	out.LastRunID = c.LastRunID
	out.Owners = cloneNodes(c.Owners)
	out.Edges = cloneEdges(c.Edges)
	return out
}

func cloneNodes(in map[string][]graphstream.Node) map[string][]graphstream.Node {
	out := make(map[string][]graphstream.Node, len(in))
	for k, v := range in {
		cp := make([]graphstream.Node, len(v))
		for i := range v {
			cp[i] = v[i]
			cp[i].Props = cloneAnyMap(v[i].Props)
		}
		out[k] = cp
	}
	return out
}

func cloneEdges(in map[string][]graphstream.Edge) map[string][]graphstream.Edge {
	out := make(map[string][]graphstream.Edge, len(in))
	for k, v := range in {
		cp := make([]graphstream.Edge, len(v))
		copy(cp, v)
		out[k] = cp
	}
	return out
}

func cloneAnyMap(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = cloneAny(v)
	}
	return out
}

func cloneAny(v any) any {
	switch x := v.(type) {
	case map[string]any:
		return cloneAnyMap(x)
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = cloneAny(e)
		}
		return out
	case []map[string]any:
		out := make([]map[string]any, len(x))
		for i, e := range x {
			out[i] = cloneAnyMap(e)
		}
		return out
	case []string:
		return append([]string(nil), x...)
	case []byte:
		return append([]byte(nil), x...)
	default:
		return v
	}
}

func (c *Consumer) applyBatch(b graphstream.Batch, raw []byte) error {
	st := c.open[b.RunID]
	if st == nil {
		if c.applied[b.RunID] {
			return nil
		}
		return fmt.Errorf("batch seq %d for unknown run %s", b.Seq, b.RunID)
	}
	if _, dup := st.seq[b.Seq]; dup {
		return fmt.Errorf("duplicate batch seq %d in %s", b.Seq, b.RunID)
	}
	if b.Phase == graphstream.PhaseScope {
		for _, o := range b.Owners {
			st.scope[o.String()] = o
		}
	} else if b.Phase != graphstream.PhaseLocal {
		for _, n := range b.Nodes {
			if err := c.inScope(st, n.Owner); err != nil {
				return err
			}
		}
		for _, e := range b.Edges {
			if err := c.inScope(st, e.Owner); err != nil {
				return err
			}
		}
	}
	st.seq[b.Seq] = b
	st.raw[b.Seq] = append([]byte(nil), raw...)
	if b.Seq > st.maxSeq {
		st.maxSeq = b.Seq
	}
	return nil
}

func (c *Consumer) inScope(st *openReplace, o graphstream.OwnerRef) error {
	if st.growing {
		// Incremental epoch: membership is proven at EndReplace against the
		// collected owner manifest. Authoritative writes still must not survive
		// commit if they are outside that final set.
		return nil
	}
	if len(st.scope) == 0 && st.begin.OwnerScopeCount == 0 && len(st.begin.OwnerScope) == 0 {
		return fmt.Errorf("consumer: write to %s with empty owner scope", o.String())
	}
	if len(st.scope) == 0 {
		// Scope is still arriving via PhaseScope; cannot prove membership yet.
		return nil
	}
	if _, ok := st.scope[o.String()]; !ok {
		return fmt.Errorf("consumer: owner %s is outside replacement scope", o.String())
	}
	return nil
}

func jsonHasField(raw []byte, key string) bool {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return false
	}
	_, ok := m[key]
	return ok
}

func (c *Consumer) applyEnd(e graphstream.EndReplace, raw []byte) error {
	if c.applied[e.RunID] {
		return nil
	}
	st := c.open[e.RunID]
	if st == nil {
		return fmt.Errorf("end for unknown run %s", e.RunID)
	}
	if e.Completeness.Status != "success" {
		delete(c.open, e.RunID)
		return nil
	}
	if e.BatchCount != len(st.seq) {
		return fmt.Errorf("end batch count %d != %d", e.BatchCount, len(st.seq))
	}
	payloads := make([][]byte, 0, e.BatchCount)
	for i := 1; i <= e.BatchCount; i++ {
		if _, ok := st.seq[i]; !ok {
			return fmt.Errorf("missing batch seq %d in %s", i, e.RunID)
		}
		payloads = append(payloads, st.raw[i])
	}
	if e.BatchDigest != "" {
		got := graphstream.DigestBatches(payloads)
		if got != e.BatchDigest {
			return fmt.Errorf("consumer: batch digest mismatch")
		}
	}
	if st.growing {
		if e.OwnerScopeDigest == "" {
			return fmt.Errorf("consumer: incremental replacement missing owner_scope_digest")
		}
		if !jsonHasField(raw, "owner_scope_len") {
			return fmt.Errorf("consumer: incremental replacement missing owner_scope_len")
		}
		if e.OwnerScopeLen != len(st.scope) {
			return fmt.Errorf("consumer: owner_scope_len %d != %d", e.OwnerScopeLen, len(st.scope))
		}
	} else if e.OwnerScopeLen != 0 && e.OwnerScopeLen != len(st.scope) {
		return fmt.Errorf("consumer: owner_scope_len %d != %d", e.OwnerScopeLen, len(st.scope))
	}
	if e.OwnerScopeDigest != "" {
		got := graphstream.DigestOwners(scopeList(st.scope))
		if got != e.OwnerScopeDigest {
			return fmt.Errorf("consumer: owner_scope_digest mismatch")
		}
	}
	// Re-check resolved writes now that PhaseScope is complete.
	for i := 1; i <= st.maxSeq; i++ {
		b := st.seq[i]
		if b.Phase == graphstream.PhaseLocal || b.Phase == graphstream.PhaseScope {
			continue
		}
		for _, n := range b.Nodes {
			if _, ok := st.scope[n.Owner.String()]; !ok {
				return fmt.Errorf("consumer: owner %s is outside replacement scope", n.Owner.String())
			}
		}
		for _, ed := range b.Edges {
			if _, ok := st.scope[ed.Owner.String()]; !ok {
				return fmt.Errorf("consumer: owner %s is outside replacement scope", ed.Owner.String())
			}
		}
	}
	c.commit(st)
	c.applied[e.RunID] = true
	c.LastGeneration = st.begin.TargetGeneration
	c.LastRunID = st.begin.RunID
	delete(c.open, e.RunID)
	return nil
}

func scopeList(m map[string]graphstream.OwnerRef) []graphstream.OwnerRef {
	out := make([]graphstream.OwnerRef, 0, len(m))
	for _, o := range m {
		out = append(out, o)
	}
	return out
}

func (c *Consumer) commit(st *openReplace) {
	for _, o := range st.scope {
		c.Owners[o.String()] = nil
		c.Edges[o.String()] = nil
	}
	for i := 1; i <= st.maxSeq; i++ {
		b := st.seq[i]
		if b.Phase == graphstream.PhaseLocal {
			continue
		}
		if b.Phase == graphstream.PhaseScope {
			for _, o := range b.Owners {
				key := o.String()
				c.Owners[key] = nil
				c.Edges[key] = nil
			}
			continue
		}
		for _, n := range b.Nodes {
			key := n.Owner.String()
			c.Owners[key] = append(c.Owners[key], n)
		}
		for _, e := range b.Edges {
			key := e.Owner.String()
			c.Edges[key] = append(c.Edges[key], e)
		}
	}
	if st.epoch {
		keep := map[string]bool{}
		for k := range st.scope {
			keep[k] = true
		}
		for k := range c.Owners {
			if !keep[k] {
				delete(c.Owners, k)
				delete(c.Edges, k)
			}
		}
	}
}

// Canonical is a stable encoding of committed owners, nodes, and edges.
func (c *Consumer) Canonical() string {
	keys := make([]string, 0, len(c.Owners))
	seen := map[string]bool{}
	for k := range c.Owners {
		keys = append(keys, k)
		seen[k] = true
	}
	for k := range c.Edges {
		if !seen[k] {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	type row struct {
		Owner string             `json:"owner"`
		Nodes []graphstream.Node `json:"nodes"`
		Edges []graphstream.Edge `json:"edges"`
	}
	out := make([]row, 0, len(keys))
	for _, k := range keys {
		ns := append([]graphstream.Node{}, c.Owners[k]...)
		es := append([]graphstream.Edge{}, c.Edges[k]...)
		sort.Slice(ns, func(i, j int) bool {
			if ns[i].ID != ns[j].ID {
				return ns[i].ID < ns[j].ID
			}
			if ns[i].Occurrence != ns[j].Occurrence {
				return ns[i].Occurrence < ns[j].Occurrence
			}
			return ns[i].Kind < ns[j].Kind
		})
		sort.Slice(es, func(i, j int) bool {
			if es[i].FromID != es[j].FromID {
				return es[i].FromID < es[j].FromID
			}
			if es[i].Kind != es[j].Kind {
				return es[i].Kind < es[j].Kind
			}
			if es[i].TargetName != es[j].TargetName {
				return es[i].TargetName < es[j].TargetName
			}
			return es[i].Occurrence < es[j].Occurrence
		})
		if len(ns) == 0 && len(es) == 0 {
			continue
		}
		out = append(out, row{Owner: k, Nodes: ns, Edges: es})
	}
	b, err := json.Marshal(out)
	if err != nil {
		return err.Error()
	}
	return string(b)
}

// FactNames is a test helper: names of symbol nodes in the last committed graph.
func (c *Consumer) FactNames() []string {
	var names []string
	for _, ns := range c.Owners {
		for _, n := range ns {
			if n.Kind == facts.KindSymbol {
				names = append(names, n.Name)
			}
		}
	}
	return names
}
