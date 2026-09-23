package graphsession

import (
	"path/filepath"
	"sort"

	"github.com/enola-labs/enola/internal/extractors/tsextractor"
	"github.com/enola-labs/enola/internal/facts"
	"github.com/enola-labs/enola/internal/graphstream"
	"github.com/enola-labs/enola/pkg/plugin"
)

const stateSchema = "enola.graphstate.v1"
const authoritativeScanHashVersion = "semantic-v1"

// State is durable analysis state for one (repo, context) pair.
type State struct {
	Protocol          string            `json:"protocol,omitempty"`
	Schema            string            `json:"schema"`
	ExtractorVersion  string            `json:"extractor_version"`
	RepoID            string            `json:"repo_id"`
	ContextID         string            `json:"context_id"`
	Checkout          string            `json:"checkout"`
	SinkID            string            `json:"sink_id,omitempty"`
	Generation        int64             `json:"generation"`
	ConfigHash        string            `json:"config_hash,omitempty"`
	EngineContextHash string            `json:"engine_context_hash,omitempty"`
	TSContext         map[string]string `json:"ts_context,omitempty"`
	TSFileContext     map[string]string `json:"ts_file_context,omitempty"`
	PolicyIdentity    string            `json:"policy_identity,omitempty"`
	// PolicyAdmissionIdentity fingerprints the admission rules the state was
	// built under, as opposed to PolicyIdentity, which also moves when the Git
	// index moves without any decision moving with it. Absent means the state
	// predates the fingerprint: it is not evidence of equal rules, so a run with
	// graph work to do reconciles rather than adopting it.
	PolicyAdmissionIdentity string `json:"policy_admission_identity,omitempty"`
	FrameworkSig            string `json:"framework_sig,omitempty"`
	// ScanHash is a digest of walked names (including ignore-glob files) and
	// their content hashes. Unknown-owner extractors use it as their input set.
	ScanHash        string            `json:"scan_hash,omitempty"`
	ScanHashVersion string            `json:"scan_hash_version,omitempty"`
	ExtractorDigest map[string]string `json:"extractor_digest,omitempty"`
	// ExtractorInputHash is the file-set/context digest each extractor last
	// consumed (inventory names and hashes passed to Extract), independent of
	// which files that extractor owns as output.
	ExtractorInputHash map[string]string       `json:"extractor_input_hash,omitempty"`
	Files              map[string]*FileState   `json:"files"`
	Synthetic          map[string][]facts.Fact `json:"synthetic,omitempty"`
	// ExtractorSynthetic partitions synthetic facts by originating extractor so
	// TypeScript cache reuse does not retain another extractor's coverage.
	ExtractorSynthetic map[string]map[string][]facts.Fact `json:"extractor_synthetic,omitempty"`
	LastRunID          string                             `json:"last_run_id,omitempty"`
	LastComplete       bool                               `json:"last_complete"`
	// ForkBase* record the completed source checkpoint this context was seeded
	// from. They are never written back to the source directory.
	ForkBaseRepoID     string `json:"fork_base_repo_id,omitempty"`
	ForkBaseContextID  string `json:"fork_base_context_id,omitempty"`
	ForkBaseGeneration int64  `json:"fork_base_generation,omitempty"`
	ForkBaseRunID      string `json:"fork_base_run_id,omitempty"`
}

// FileState is one source file's cached contribution.
type FileState struct {
	Hash       string                  `json:"hash,omitempty"`
	Extractor  string                  `json:"extractor,omitempty"`
	Unreadable bool                    `json:"unreadable,omitempty"`
	Minified   bool                    `json:"minified,omitempty"`
	TS         *tsextractor.FileRecord `json:"ts,omitempty"`
	Facts      []facts.Fact            `json:"facts,omitempty"`
	Imports    []string                `json:"imports,omitempty"`
	Declared   []string                `json:"declared,omitempty"`
	Referenced []string                `json:"referenced,omitempty"`
	Reexports  []string                `json:"reexports,omitempty"`
	// Contrib holds additional extractor contributions for this path when more
	// than one extractor owns the file. Primary Extractor+Facts/TS stay intact.
	Contrib map[string][]facts.Fact `json:"contrib,omitempty"`
	// ContribHash is the source hash each extractor last consumed for this path.
	ContribHash map[string]string `json:"contrib_hash,omitempty"`
}

func newState(repoID, contextID, checkout, extractorVersion string) *State {
	return &State{
		Schema:             stateSchema,
		ExtractorVersion:   extractorVersion,
		RepoID:             repoID,
		ContextID:          contextID,
		Checkout:           checkout,
		Files:              map[string]*FileState{},
		Synthetic:          map[string][]facts.Fact{},
		ExtractorInputHash: map[string]string{},
		ExtractorSynthetic: map[string]map[string][]facts.Fact{},
	}
}

func factExtractorName(f facts.Fact) string {
	if f.Props == nil {
		return ""
	}
	s, _ := f.Props["extractor"].(string)
	return s
}

func extractorContextHash(owned []string, fileSetHash, scanHash string) string {
	if len(owned) == 0 {
		return scanHash
	}
	return fileSetHash
}

func ownedExtractorContextNeed(owned []string, st *State, ext plugin.Extractor, invFiles, allNames []string, hashes map[string]string, fileSetHash, prevScan, scanHash string) bool {
	if ext == nil {
		return false
	}
	extName := ext.Name()
	if extName == "" {
		return false
	}
	prevInput := ""
	if st != nil && st.ExtractorInputHash != nil {
		prevInput = st.ExtractorInputHash[extName]
	}
	newInput := extractorInputDigest(ext, owned, invFiles, allNames, hashes, fileSetHash, scanHash)
	if prevInput == "" {
		if prevScan != "" && prevScan != scanHash {
			return true
		}
		return false
	}
	if prevInput == newInput {
		return false
	}
	if prevInput == fileSetHash || prevInput == scanHash {
		return false
	}
	return true
}

func priorExtractorNames(st *State, files map[string]*FileState) []string {
	seen := map[string]bool{}
	add := func(n string) {
		if n != "" {
			seen[n] = true
		}
	}
	if st != nil {
		for n := range st.ExtractorDigest {
			add(n)
		}
		for n := range st.ExtractorInputHash {
			add(n)
		}
		for n := range st.ExtractorSynthetic {
			add(n)
		}
	}
	for _, fs := range files {
		if fs == nil {
			continue
		}
		add(fs.Extractor)
		for n := range fs.Contrib {
			add(n)
		}
		for n := range fs.ContribHash {
			add(n)
		}
	}
	out := make([]string, 0, len(seen))
	for n := range seen {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

func dropExtractorContribution(files map[string]*FileState, name string) {
	if files == nil || name == "" {
		return
	}
	for path, st := range files {
		if st == nil || !extractorOwnsState(st, name) {
			continue
		}
		st = cloneFileState(st)
		if name == "typescript" {
			st.TS = nil
			st.Declared = nil
			st.Referenced = nil
			st.Imports = nil
			st.Reexports = nil
			if st.Extractor == name {
				st.Extractor = ""
			}
			if len(st.Contrib) == 0 {
				delete(files, path)
				continue
			}
		}
		if st.Contrib != nil {
			delete(st.Contrib, name)
		}
		if st.ContribHash != nil {
			delete(st.ContribHash, name)
		}
		if st.Extractor == name && st.TS == nil {
			delete(files, path)
			continue
		}
		files[path] = st
	}
}

func syntheticFactsFor(st *State, name string) []facts.Fact {
	if st == nil || name == "" {
		return nil
	}
	if len(st.ExtractorSynthetic) > 0 {
		byID, ok := st.ExtractorSynthetic[name]
		if !ok {
			return nil
		}
		var out []facts.Fact
		for _, ff := range byID {
			out = append(out, ff...)
		}
		return out
	}
	var out []facts.Fact
	for _, ff := range st.Synthetic {
		for _, f := range ff {
			ext := factExtractorName(f)
			if name == "typescript" {
				if ext != "" && ext != "typescript" {
					continue
				}
				out = append(out, f)
				continue
			}
			if ext == name {
				out = append(out, f)
			}
		}
	}
	return out
}

func extractorSyntheticIDs(st *State, name string) []string {
	if st == nil || name == "" {
		return nil
	}
	seen := map[string]bool{}
	if len(st.ExtractorSynthetic) > 0 {
		for id := range st.ExtractorSynthetic[name] {
			if id != "" {
				seen[id] = true
			}
		}
	} else {
		for id, ff := range st.Synthetic {
			for _, f := range ff {
				if factExtractorName(f) == name {
					seen[id] = true
					break
				}
			}
		}
	}
	out := make([]string, 0, len(seen))
	for id := range seen {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

func appendExtractorSynthetic(dst map[string]map[string][]facts.Fact, ext string, ff []facts.Fact) {
	if dst == nil || ext == "" || len(ff) == 0 {
		return
	}
	for _, f := range ff {
		o := ownerOf(f)
		if o.Kind != graphstream.OwnerSynthetic {
			continue
		}
		if dst[ext] == nil {
			dst[ext] = map[string][]facts.Fact{}
		}
		dst[ext][o.ID] = append(dst[ext][o.ID], f)
	}
}

func retireExtractorOwners(st *State, files map[string]*FileState, name string) []graphstream.OwnerRef {
	var out []graphstream.OwnerRef
	seen := map[string]bool{}
	add := func(o graphstream.OwnerRef) {
		k := o.String()
		if seen[k] {
			return
		}
		seen[k] = true
		out = append(out, o)
	}
	for path, prev := range files {
		if !extractorOwnsState(prev, name) {
			continue
		}
		add(graphstream.OwnerRef{Kind: graphstream.OwnerFile, ID: filepath.ToSlash(path)})
	}
	for _, id := range extractorSyntheticIDs(st, name) {
		add(graphstream.OwnerRef{Kind: graphstream.OwnerSynthetic, ID: id})
	}
	return out
}
