// Package graphstream is the producer-side replacement protocol for streaming
// graph analysis. Consumers apply replacements; this package does not write a
// database.
package graphstream

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
)

// SchemaVersion identifies the envelope contract.
const SchemaVersion = "enola.graph.v1"

// Envelope types.
const (
	TypeBeginReplace = "begin_replace"
	TypeBatch        = "batch"
	TypeEndReplace   = "end_replace"
)

// Replacement phases. Local batches may emit declarations before the candidate
// index is complete; resolved batches are the authoritative owner replacement.
const (
	PhaseLocal    = "local"
	PhaseResolved = "resolved"
	PhaseEpoch    = "epoch"
	PhaseScope    = "scope"
)

// Owner-scope modes. Complete requires the full owner set on BeginReplace
// (or immediately following PhaseScope chunks). Incremental is for an initial
// full-repository epoch: the owner manifest may be collected via PhaseScope
// while local batches stream; EndReplace carries the complete count/digest.
const (
	ScopeModeComplete    = "complete"
	ScopeModeIncremental = "incremental"
)

// Owner kinds. File owners are source paths. Synthetic owners hold shared
// contributions (directory modules, extractor coverage, composed aggregates).
const (
	OwnerFile      = "file"
	OwnerSynthetic = "synthetic"
)

// Resolution states for an edge. Absent TargetID must not be guessed into an edge.
const (
	ResResolved   = "resolved"
	ResUnresolved = "unresolved"
	ResAmbiguous  = "ambiguous"
	ResPending    = "pending"
)

// OwnerRef is one replacement-ownership identity.
type OwnerRef struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

func (o OwnerRef) String() string { return o.Kind + ":" + o.ID }

// BeginReplace opens a replacement. In complete mode, OwnerScope is the full
// set this delimited replacement may write; growing it requires a new
// replacement or PhaseScope chunks that finish before resolved writes. In
// incremental mode the set may grow through PhaseScope until EndReplace.
type BeginReplace struct {
	Type             string     `json:"type"`
	SchemaVersion    string     `json:"schema_version"`
	RepoID           string     `json:"repo_id"`
	ContextID        string     `json:"context_id"`
	RunID            string     `json:"run_id"`
	BaseGeneration   int64      `json:"base_generation"`
	TargetGeneration int64      `json:"target_generation"`
	Phase            string     `json:"phase"`
	ScopeMode        string     `json:"scope_mode,omitempty"`
	OwnerScope       []OwnerRef `json:"owner_scope,omitempty"`
	OwnerScopeCount  int        `json:"owner_scope_count"`
	// ForkBase* identify the completed source checkpoint the consumer must
	// already hold before applying this context's first replacement. Empty on
	// ordinary (non-fork) runs.
	ForkBaseRepoID     string `json:"fork_base_repo_id,omitempty"`
	ForkBaseContextID  string `json:"fork_base_context_id,omitempty"`
	ForkBaseGeneration int64  `json:"fork_base_generation,omitempty"`
	ForkBaseRunID      string `json:"fork_base_run_id,omitempty"`
}

// Node is one owned fact occurrence. ID is FactID; Occurrence distinguishes
// duplicate facts that share an identity within the same owner.
type Node struct {
	Owner      OwnerRef       `json:"owner"`
	ID         string         `json:"id"`
	Kind       string         `json:"kind"`
	Name       string         `json:"name"`
	File       string         `json:"file,omitempty"`
	Line       int            `json:"line,omitempty"`
	EndLine    int            `json:"end_line,omitempty"`
	Column     int            `json:"column,omitempty"`
	EndColumn  int            `json:"end_column,omitempty"`
	Repo       string         `json:"repo,omitempty"`
	Props      map[string]any `json:"props,omitempty"`
	Occurrence int            `json:"occurrence"`
}

// Edge is one owned relation. TargetID is omitted when resolution is not unique.
type Edge struct {
	Owner      OwnerRef `json:"owner"`
	FromID     string   `json:"from_id"`
	Kind       string   `json:"kind"`
	TargetName string   `json:"target_name"`
	TargetID   string   `json:"target_id,omitempty"`
	Resolution string   `json:"resolution"`
	Occurrence int      `json:"occurrence"`
}

// Batch is one numbered chunk of a replacement's owned output.
// Phase distinguishes early local declarations from authoritative resolved output.
// Consumers apply only resolved batches, and only after EndReplace.
type Batch struct {
	Type   string     `json:"type"`
	RunID  string     `json:"run_id"`
	Seq    int        `json:"seq"`
	Phase  string     `json:"phase,omitempty"`
	Nodes  []Node     `json:"nodes,omitempty"`
	Edges  []Edge     `json:"edges,omitempty"`
	Owners []OwnerRef `json:"owners,omitempty"`
}

// Completeness is declared on EndReplace. Seeing an end marker without this
// metadata does not prove the replacement finished successfully.
type Completeness struct {
	Status          string     `json:"status"`
	FilesAnalyzed   int        `json:"files_analyzed"`
	FilesUnreadable []string   `json:"files_unreadable,omitempty"`
	ParsedFiles     int        `json:"parsed_files"`
	CachedFiles     int        `json:"cached_files"`
	SummaryScans    int        `json:"summary_scans"`
	EarlyLocal      bool       `json:"early_local"`
	Fallbacks       []Fallback `json:"fallbacks,omitempty"`
}

// Fallback records an extractor or feature that could not do file-granularity
// incremental work.
type Fallback struct {
	Extractor string `json:"extractor"`
	Scope     string `json:"scope"`
	Reason    string `json:"reason"`
}

// EndReplace closes a replacement. BatchDigest is the hex SHA-256 of the
// concatenation of every batch payload in seq order. OwnerScopeDigest is the
// hex SHA-256 of the sorted final owner manifest; required for incremental
// scope so consumers can verify the collected set before commit.
type EndReplace struct {
	Type             string       `json:"type"`
	RunID            string       `json:"run_id"`
	BatchCount       int          `json:"batch_count"`
	BatchDigest      string       `json:"batch_digest"`
	Completeness     Completeness `json:"completeness"`
	OwnerScopeLen    int          `json:"owner_scope_len"`
	OwnerScopeDigest string       `json:"owner_scope_digest,omitempty"`
}

// MessageID is the deterministic JetStream Msg-Id for an envelope.
func MessageID(runID, typ string, seq int) string {
	return fmt.Sprintf("%s:%s:%d", runID, typ, seq)
}

// Marshal canonical JSON for a protocol value. Replay of the same identity must
// produce identical bytes.
func Marshal(v any) ([]byte, error) {
	return json.Marshal(v)
}

// DigestBatches hashes the already-marshaled batch payloads in seq order.
func DigestBatches(payloads [][]byte) string {
	h := sha256.New()
	for _, p := range payloads {
		h.Write(p)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// SortOwners is a stable owner-scope order for replay identity.
func SortOwners(owners []OwnerRef) {
	sort.Slice(owners, func(i, j int) bool {
		if owners[i].Kind != owners[j].Kind {
			return owners[i].Kind < owners[j].Kind
		}
		return owners[i].ID < owners[j].ID
	})
}

// DigestOwners hashes the sorted owner manifest. Replay of the same set is
// byte-stable because the kind/id pairs are ordered first.
func DigestOwners(owners []OwnerRef) string {
	cp := append([]OwnerRef(nil), owners...)
	SortOwners(cp)
	h := sha256.New()
	for _, o := range cp {
		h.Write([]byte(o.Kind))
		h.Write([]byte{0})
		h.Write([]byte(o.ID))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}
