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
}

type runtimeInputs struct {
	engineContextHash string
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

func readRuntimeInputs(eng *engine.Engine, abs string, st *State, work *WorkCounters) (*runtimeInputs, error) {
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
				result.tsContext, result.tsFileContext = ts.SessionContext(abs, cfg, tsConfigPaths, inv.Files)
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
	snapshot        []facts.Fact
	epoch           string
	watermark       uint64
	failed, closed  bool
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
		fresh, err := r.eng.RebuildGraphInputs()
		if err != nil {
			return nil, WorkCounters{}, err
		}
		r.eng = fresh
		rtr.Mark("rebuild_graph_inputs", "")
	}
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
	if !fast && r.eng.GraphScope() != nil {
		s.work.PolicyBuilds = 1
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
