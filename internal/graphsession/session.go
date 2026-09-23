package graphsession

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/enola-labs/enola/internal/engine"
	"github.com/enola-labs/enola/internal/extractors/tsextractor"
	"github.com/enola-labs/enola/internal/facts"
	"github.com/enola-labs/enola/internal/filelock"
	"github.com/enola-labs/enola/internal/graphinput"
	"github.com/enola-labs/enola/internal/graphprofile"
	"github.com/enola-labs/enola/internal/graphstream"
	"github.com/enola-labs/enola/pkg/plugin"
)

var ErrInputsChanged = errors.New("inputs changed during transaction")

// Options configure a graph session and its generation transactions.
type Options struct {
	// AuthoritativeFiles selects the v2 frozen file-owner contract.
	AuthoritativeFiles bool
	// ReloadEngine reconstructs registrations and effective config after a config edit.
	ReloadEngine func(context.Context) (*engine.Engine, error)
	// ConfigPaths includes external and missing configuration-selection candidates.
	ConfigPaths []string
	StateDir    string
	ContextID   string
	RepoID      string
	Subject     string
	BatchLimit  int
	Watch       bool
	// WatchEvery is a fixed collection window, not a timer reset on every edit.
	// Non-positive values use DefaultWatchEvery. Baseline analysis starts immediately.
	WatchEvery time.Duration
	// WatchIgnore must contain only non-input output artifacts (for example the event sink).
	WatchIgnore  []string
	ForceInitial bool
	AllowMemory  bool
	SinkID       string
	// MaxBeginBytes is the BeginReplace payload cap. Zero uses 256KiB, or 512KiB
	// with AuthoritativeFiles. Frozen v2 refuses a larger inline manifest; it
	// never splits owner scope into PhaseScope chunks.
	MaxBeginBytes int
	// OnBeforeParse is a test hook invoked before each dirty TypeScript file is parsed.
	OnBeforeParse func(rel string)
	// FreshEngine is the caller stating that it constructed this engine for this
	// session, in this process, and has run nothing against it since. Only a
	// caller can know that; the session cannot infer it from an engine value.
	//
	// It permits one thing: the first non-fast run may prove the graph input
	// policy by re-reading the files that policy declared instead of building a
	// second identical policy. The proof is not this flag. The flag only bounds
	// the window the proof has to cover, and the run still refuses to skip if
	// any declared input moved, if the configuration bracket around the check
	// does not hold, or if anything has already run against the engine.
	FreshEngine bool
}

// Result is the observable outcome of Analyze or Delta.
// InvalidationStats separates observed input changes from actual TS extraction
// operations. Resolution reparses remain whole-file extraction, not pure rebinding.
type InvalidationStats struct {
	RawConfigChanged             bool
	PolicyReconciled             bool
	AddedSources, RemovedSources int
	ContextAffectedSources       int
	ContextReasons               []string
	ParsedByReason               map[string]int
}

// policyReconciles reports whether the graph input policy of this run has to be
// treated as a different policy from the one the stored state was built under.
//
// The comparison is the admission fingerprint, not the raw identity. The raw
// identity hashes every non-hard-excluded tracked name, so it moves on a pure
// `git add` or `git rm --cached` of an already-admitted, unignored, unchanged
// file - an edit that moves no decision this session makes, because the tracked
// set reaches Classify at exactly one place and only for names Git ignores.
// Treating that as a policy change is what turns staging into a whole-domain
// Begin. The raw identity is still stored and still compared, for stale-state
// detection here and as the value the post-run fence revalidates against.
//
// Equal admission fingerprints mean equal decision functions over equivalent
// paths. They say nothing about which files exist, so the caller still has to
// diff the observed membership; this predicate replaces neither the inventory
// comparison nor membershipScope.
//
// An absent admission fingerprint is a state written before admission was
// fingerprinted. Nothing in it proves which rules produced it, so it reconciles,
// and because this predicate is one of the terms of the changed gate itself, the
// first run under this code over such a state always has graph work to do. The
// fallback is therefore taken once per old state rather than conditionally, and
// no run adopts the field without having reconciled under it.
// tsRunDiscovery is the snapshot this run's input read already took. It is
// offered, not imposed: the extractor keeps it only when the repository, the
// policy scope and every configuration byte it read still match what this
// extraction will observe, so a run whose capture disagrees simply reads the
// tree as it did before.
// keepProvenDiscovery records the snapshot an extraction actually proved, for a
// run that had none of its own to offer. Without this a run whose offer was
// refused - or that was never made one - would commit with nothing retained,
// and every content edit after it would rebuild discovery again until some
// reconciling run happened to restore it.
func (s *session) keepProvenDiscovery(disc *tsextractor.Discovery) {
	if s.inputs == nil || disc == nil || s.inputs.tsDiscovery != nil {
		return
	}
	s.inputs.tsDiscovery = disc
}

// adoptRetainedDiscovery offers the resident's retained snapshot to a run that
// is not re-reading the tree, and takes it only if it proves out against the
// capture this run will extract under.
func (s *session) adoptRetainedDiscovery(input *runtimeInputs) {
	if input == nil || s.retained == nil {
		return
	}
	scope := s.eng.GraphScope()
	if scope == nil {
		return
	}
	if s.retainedFor != (retainedDiscoveryIdentity{policy: input.policyIdentity, admission: input.admissionIdentity}) {
		return
	}
	for _, ext := range s.eng.Extractors() {
		ts, ok := ext.(*tsextractor.TSExtractor)
		if !ok {
			continue
		}
		reused, cost := ts.ReuseDiscovery(s.abs, input.config, s.retained)
		s.work.TSDiscoveryRechecks = s.work.TSDiscoveryRechecks.Add(cost)
		if reused != nil {
			input.tsDiscovery = reused
			s.work.TSDiscoveriesReused++
		}
		return
	}
}

func (s *session) tsRunDiscovery() *tsextractor.Discovery {
	if s.inputs == nil {
		return nil
	}
	return s.inputs.tsDiscovery
}

func policyReconciles(st *State, input *runtimeInputs) bool {
	if st == nil {
		return true
	}
	if input.admissionIdentity == "" {
		// No active graph input policy. A state that carries a fingerprint was
		// built with one running, and losing the policy is itself a change.
		return st.PolicyIdentity != "" || st.PolicyAdmissionIdentity != ""
	}
	return st.PolicyIdentity == "" || st.PolicyAdmissionIdentity == "" || st.PolicyAdmissionIdentity != input.admissionIdentity
}

// policyBookkeepingFenced reports whether a run that published nothing may write
// this run's policy fingerprints back into the completed state.
//
// The pure staging case is exactly this one: the index moved, no admission
// decision moved with it, the scan digest and the configuration fingerprint both
// matched, so the run parsed nothing, emitted no event and left the completed
// generation where it was. Leaving the stored identity behind would make every
// later run recompute the same difference forever, so the fingerprints are
// refreshed - and only the fingerprints.
//
// The write is fenced on both sides. Before it, the run has already proven the
// stored graph describes the tree as it stands - the scan digest matched, the
// raw analysis fingerprint was rechecked, and the transaction's own input fence
// re-read every declared policy dependency, the Git index included, refusing the
// run outright if one had moved. This recheck covers what is left of that
// window: the interval between that fence and the save, re-reading the same
// declared inputs and requiring the pair they produce to still be the pair this
// run planned with. It is narrower than it sounds and deliberately so; it is not
// a proof that the policy is fresh, only that these two values still describe
// it. Declining is always safe in that direction: it can only cost a later
// reconciliation, never skip one.
//
// The recheck re-runs Git discovery and the index enumeration and re-reads
// every declared dependency, so it is reached only when the stored pair
// actually differs from this run's. An idle
// resident whose state already carries both fingerprints returns on the first
// comparison, and because the refresh below also advances s.state, a run that
// does pay for it pays once rather than on every later idle request.
func (s *session) policyBookkeepingFenced(input *runtimeInputs) (bool, error) {
	if s.state == nil || s.fast || input.admissionIdentity == "" {
		// A fast run reuses the previous run's inputs rather than rebuilding the
		// policy, so its fingerprints are not an observation of the tree now.
		return false, nil
	}
	if s.state.PolicyIdentity == input.policyIdentity && s.state.PolicyAdmissionIdentity == input.admissionIdentity {
		return false, nil
	}
	scope := s.eng.GraphScope()
	if scope == nil || scope.Policy == nil {
		return false, nil
	}
	identity, admission, ok, err := scope.Policy.RecheckDeclaredInputs()
	if err != nil {
		return false, err
	}
	if !ok || identity != input.policyIdentity || admission != input.admissionIdentity {
		return false, nil
	}
	return true, nil
}

type Result struct {
	Invalidation     InvalidationStats
	RunID            string
	BaseGeneration   int64
	TargetGeneration int64
	Stats            tsextractor.ExtractStats
	Fallbacks        []graphstream.Fallback
	Unreadable       []string
	OwnersPublished  int
	ParsedFiles      int
	EarlyLocal       int
	Facts            []facts.Fact
}

// Run performs initial analysis when no complete state exists, otherwise a delta.
func Run(ctx context.Context, eng *engine.Engine, repoPath string, sink graphstream.Sink, opts Options) (*Result, error) {
	r, err := OpenSession(ctx, eng, repoPath, sink, opts)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return r.reconcile(ctx, true)
}

// OpenSession owns the single-writer lock and recovers durable delivery before use.
func OpenSession(ctx context.Context, eng *engine.Engine, repoPath string, sink graphstream.Sink, opts Options) (*Resident, error) {
	tr := graphprofile.StartNamed("open")
	abs, err := filepath.Abs(repoPath)
	if err != nil {
		return nil, err
	}
	abs = filepath.Clean(abs)
	opts.ConfigPaths = append([]string(nil), opts.ConfigPaths...)
	opts.WatchIgnore = append([]string(nil), opts.WatchIgnore...)
	if opts.ContextID == "" {
		opts.ContextID = "default"
	}
	if opts.RepoID == "" {
		opts.RepoID = abs
	}
	if opts.StateDir == "" {
		opts.StateDir = filepath.Join(abs, eng.Config().Output.Dir, "graphstate")
	}
	opts.StateDir, err = filepath.Abs(opts.StateDir)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(opts.StateDir, 0o755); err != nil {
		return nil, err
	}
	lock, err := filelock.Acquire(filepath.Join(opts.StateDir, "session"))
	if err != nil {
		return nil, fmt.Errorf("graphsession lock: %w", err)
	}
	opened := false
	defer func() {
		if !opened {
			lock.Release()
		}
	}()
	tr.Mark("lock", "")
	if opts.BatchLimit <= 0 {
		opts.BatchLimit = 64
	}
	if err := bindDispatchIdentity(opts.StateDir, opts, abs); err != nil {
		return nil, err
	}
	tr.Mark("bind_identity", "")
	journal, err := graphstream.OpenJournal(filepath.Join(opts.StateDir, "journal.jsonl"))
	if err != nil {
		return nil, err
	}
	defer func() {
		if !opened {
			_ = journal.Close()
		}
	}()
	tr.Mark("open_journal", "")
	if err := bindProtocol(opts.StateDir, opts.AuthoritativeFiles); err != nil {
		return nil, err
	}
	replay := &graphstream.Publisher{Sink: sink, Journal: journal}
	if err := replay.ReplayUnacked(ctx); err != nil {
		return nil, fmt.Errorf("replay journal: %w", err)
	}
	tr.Mark("replay_unacked", "")
	st, err := recoverAcknowledgedPending(opts.StateDir, journal, opts, abs)
	if err != nil {
		return nil, err
	}
	nfiles := 0
	if st != nil {
		nfiles = len(st.Files)
		if err := identityOK(st, opts, abs); err != nil {
			return nil, err
		}
	}
	tr.Mark("load_state", fmt.Sprintf("files=%d", nfiles))
	opened = true
	return &Resident{eng: eng, abs: abs, opts: opts, sink: sink, state: st, journal: journal, lock: lock, engineUnused: opts.FreshEngine}, nil
}

func identityOK(st *State, opts Options, abs string) error {
	if st == nil {
		return nil
	}
	if st.ContextID != "" && opts.ContextID != "" && st.ContextID != opts.ContextID {
		return fmt.Errorf("graphsession: state context %q does not match run context %q (use a separate --state-dir)", st.ContextID, opts.ContextID)
	}
	if st.SinkID != "" && opts.SinkID != "" && st.SinkID != opts.SinkID {
		return fmt.Errorf("graphsession: state sink %q does not match this run %q", st.SinkID, opts.SinkID)
	}
	if st.RepoID != "" && opts.RepoID != "" && st.RepoID != opts.RepoID {
		return fmt.Errorf("graphsession: state repo %q does not match run repo %q", st.RepoID, opts.RepoID)
	}
	if st.Checkout != "" && abs != "" && filepath.Clean(st.Checkout) != filepath.Clean(abs) {
		return fmt.Errorf("graphsession: state checkout %q does not match %q", st.Checkout, abs)
	}
	return nil
}

type session struct {
	// retained is the snapshot the resident's last committed run proved, and
	// retainedFor the policy identity it was proven under. Both are an offer,
	// never an answer: nothing is used until this run proves it again.
	retained          *tsextractor.Discovery
	retainedFor       retainedDiscoveryIdentity
	plan              *fileInvalidationPlan
	extraFallbacks    []graphstream.Fallback
	priorResolution   *idIndex
	validateEffective func() error
	eng               *engine.Engine
	abs               string
	opts              Options
	sink              graphstream.Sink
	state             *State
	journal           *graphstream.Journal
	pub               *graphstream.Publisher
	seq               int
	replaceScope      []graphstream.OwnerRef
	scopeLimited      bool
	skipPublish       bool
	began             bool
	batchPayloads     [][]byte
	frameworkSig      string
	capturedSources   map[string][]byte
	cfgCaptured       map[string][]byte
	mu                sync.Mutex
	announced         map[string]bool
	scopeMode         string
	localErr          error
	localNodes        []graphstream.Node
	localEdges        []graphstream.Edge
	resolvNodes       []graphstream.Node
	resolvEdges       []graphstream.Edge
	firstLocalSent    bool
	resolvedOwners    int
	inputs            *runtimeInputs
	fast              bool
	work              WorkCounters
	prof              *graphprofile.Trace
	// preparedTS is a dirty-file ExtractSession performed before frozen Begin
	// so name-delta planning uses composed facts. The later TS path reuses it.
	preparedTS    *tsextractor.SessionResult
	preparedDirty map[string]bool
	// preparedParses is one entry per file the preview actually parsed, with
	// the reason it was parsed for. The extraction site replays it when it
	// adopts the preview, so a parse is classified and reported to
	// OnBeforeParse exactly once whichever of the two sites read the file.
	preparedParses []preparedParse
	// preparedMD is the mdintent extraction this run made before Begin to plan
	// the manifest with; see prepareMDScope.
	preparedMD *preparedMD
	// preparedNonTS holds one pre-Begin extraction per non-TypeScript extractor
	// previewed to close its candidate names; see prepareNonTSCandidateScope.
	// The extraction site consumes it instead of reading the tree again.
	preparedNonTS map[string][]facts.Fact
	// nonTSPreviews is that same extraction kept by name for the rest of the
	// run, together with what it proved; see proveNonTSNeutrality.
	nonTSPreviews map[string]*nonTSPreview
	// neutralScan and neutralConfig record that the scan digest and the raw
	// configuration fingerprint moved for reasons this run proved inert, so a
	// no-publication return can still record the values it observed.
	neutralScan   bool
	neutralConfig bool
	// previewFences re-prove, before a successful End, that each previewed
	// extractor's declared context is still the one this run planned from.
	// capturedSources compares bytes it managed to read; these compare the
	// context digest, so a lockfile that appeared, vanished or became
	// unreadable mid-run is caught too.
	previewFences []func() error
}

// preparedParse is a parse the pre-Begin preview performed on the session's behalf.
type preparedParse struct {
	path   string
	reason string
}

func (s *session) analyze(ctx context.Context) (*Result, error) {
	return s.run(ctx, true)
}

func (s *session) delta(ctx context.Context) (*Result, error) {
	return s.run(ctx, false)
}

func (s *session) run(ctx context.Context, initial bool) (*Result, error) {
	tr := s.prof
	// A run that failed between preview and consumption must not hand its
	// extraction to the next one: the tree has moved on since.
	s.preparedMD = nil
	s.preparedNonTS = nil
	s.nonTSPreviews = nil
	s.neutralScan = false
	s.neutralConfig = false
	s.previewFences = nil
	if s.inputs != nil {
		// A discovery snapshot is one run's observation of the tree, and a run
		// does not inherit one. Whichever path below supplies this run's
		// snapshot supplies it by proof.
		s.inputs.tsDiscovery = nil
	}
	var err error
	input := s.inputs
	if !s.fast {
		input, err = readRuntimeInputs(s.eng, s.abs, s.state, &s.work, s.retained, s.retainedFor)
		if err != nil {
			return nil, err
		}
	}
	s.inputs = input
	if s.fast {
		// A fast run does not re-read the tree, so readRuntimeInputs never runs
		// and there is no fresh capture for it to prove a snapshot against. The
		// capture it is about to extract under is the previous run's, which is
		// exactly the one a retained snapshot has to agree with, so the same
		// proof is asked here - including the presence re-observation, because
		// the tree can have moved since that capture was taken even though this
		// run is not re-reading it.
		//
		// Unprovable leaves this nil and the run behaves as it did before, with
		// whichever reader needs a discovery building its own.
		s.adoptRetainedDiscovery(input)
	}
	inv, detectedExt, hashes := input.inventory, input.detected, input.hashes
	contextInputs, cfgHash, capturedCfg := input.contexts, input.configHash, input.config
	scanHash := inventoryDigest(inv.AllNames, hashes)
	scanHashVersion := "inventory-v1"
	if s.opts.AuthoritativeFiles {
		scanHash = inventoryDigest(graphSemanticNames(s.eng, inv.AllNames), hashes)
		scanHashVersion = authoritativeScanHashVersion
	}
	prevScan := ""
	if s.state != nil {
		prevScan = s.state.ScanHash
		if s.opts.AuthoritativeFiles && scanHashEquivalent(s.state, inv.AllNames, graphSemanticNames(s.eng, inv.AllNames), hashes, scanHash) {
			// Legacy AllNames or unversioned semantic digest of the same tree
			// must not look like an input change to opaque extractors.
			prevScan = scanHash
		}
	}
	repoID := s.opts.RepoID
	if repoID == "" {
		repoID = filepath.Clean(s.abs)
	}
	s.cfgCaptured = capturedCfg
	s.capturedSources = map[string][]byte{}
	for k, v := range capturedCfg {
		s.capturedSources[k] = append([]byte(nil), v...)
	}
	for k, v := range input.sources {
		s.capturedSources[k] = v
	}
	invalidation := InvalidationStats{ParsedByReason: map[string]int{}}
	var invalidationMu sync.Mutex
	if s.state != nil {
		invalidation.RawConfigChanged = s.state.ConfigHash != cfgHash
		invalidation.PolicyReconciled = policyReconciles(s.state, input)
	}
	currentSources := map[string]bool{}
	for _, p := range inv.Files {
		if tsextractor.IsSessionSource(p, input.angular) {
			currentSources[p] = true
			if s.state != nil && (s.state.Files[p] == nil || s.state.Files[p].TS == nil) {
				invalidation.AddedSources++
			}
		}
	}
	if s.state != nil {
		for p, st := range s.state.Files {
			if st.TS != nil && !currentSources[p] {
				invalidation.RemovedSources++
			}
		}
	}
	base := int64(0)
	if s.state != nil {
		base = s.state.Generation
	}
	target := base + 1
	runID := fmt.Sprintf("%s-%s-%d-%d", repoID, s.opts.ContextID, target, time.Now().UnixNano())

	journal := s.journal
	if journal == nil {
		journal, err = graphstream.OpenJournal(filepath.Join(s.opts.StateDir, "journal.jsonl"))
		if err != nil {
			return nil, err
		}
		s.journal = journal
		defer func() { _ = journal.Close() }()
	}
	subject := graphstream.ConcretePublishSubject(s.opts.Subject, repoID, s.opts.ContextID, runID)
	s.pub = &graphstream.Publisher{Sink: s.sink, Journal: journal, Subject: subject}
	s.pub.EnableAsync(64, 16<<20)
	defer s.pub.CloseAsync()

	fallbacks := append([]graphstream.Fallback(nil), s.extraFallbacks...)
	s.work.FactAssemblies++
	var allFacts []facts.Fact
	var unreadable []string
	stats := tsextractor.ExtractStats{}
	earlyLocal := 0
	tsRecords := map[string]*tsextractor.FileRecord{}

	prevFiles := map[string]*FileState{}
	prevFilesStart := time.Now()
	if s.state != nil {
		for k, v := range s.state.Files {
			// FileState records are immutable for the duration of a run. Keep a
			// shallow owner map here and let the existing mutation helpers clone
			// only records they actually replace or retire. Cloning every
			// contribution map on a no-op used to make a 5k-file state pay a
			// repository-sized copy before it could return zero work.
			prevFiles[k] = v
		}
	}
	graphprofile.Since("clone_file_state_copy", prevFilesStart, fmt.Sprintf("n=%d", len(prevFiles)))
	extractorDigest := map[string]string{}
	if s.state != nil && s.state.ExtractorDigest != nil {
		for k, v := range s.state.ExtractorDigest {
			extractorDigest[k] = v
		}
	}
	extractorInput := map[string]string{}
	if s.state != nil && s.state.ExtractorInputHash != nil {
		for k, v := range s.state.ExtractorInputHash {
			extractorInput[k] = v
		}
	}
	fileSetHash := inventoryDigest(inv.Files, hashes)
	synByExt := map[string]map[string][]facts.Fact{}

	haveCache := s.state != nil && s.state.LastComplete && s.state.ExtractorVersion == engine.ExtractorVersion()
	configChanged := haveCache && s.state.ConfigHash != "" && s.state.ConfigHash != cfgHash
	if s.eng.GraphScope() != nil {
		configChanged = haveCache && (s.state.EngineContextHash == "" || s.state.EngineContextHash != input.engineContextHash)
	}
	forceAll := s.opts.ForceInitial || !haveCache
	nonTSForceAll := forceAll || configChanged
	nonTSConfigChanged := configChanged
	if configChanged {
		forceAll = true
		fallbacks = append(fallbacks, graphstream.Fallback{
			Extractor: "typescript",
			Scope:     "all owned files",
			Reason:    "repo analysis inputs (tsconfig/package/clients) changed",
		})
	}
	if s.eng.GraphScope() != nil && haveCache {
		changes := tsextractor.ContextDifference(s.state.TSContext, input.tsContext)
		invalidation.ContextReasons = changes
		if len(changes) > 0 {
			forceAll = true
			fallbacks = append(fallbacks, graphstream.Fallback{Extractor: "typescript", Scope: "all owned files", Reason: strings.Join(changes, "; ")})
		}
	}
	if !forceAll && input.angular {
		forceAll = true
		fallbacks = append(fallbacks, graphstream.Fallback{
			Extractor: "typescript",
			Scope:     "all owned files",
			Reason:    "angular composition extras are not cached; re-extracting TypeScript files for a correct route/template graph",
		})
	}

	tsNoop := false
	hadTS := false
	nonTSNeed := false
	needByExt := map[string]bool{}
	var nonTSFileOwners []graphstream.OwnerRef
	angular := input.angular
	for name := range detectedExt {
		if name == "typescript" {
			continue
		}
		var ext plugin.Extractor
		for _, e := range s.eng.Extractors() {
			if e.Name() == name {
				ext = e
				break
			}
		}
		if ext == nil {
			continue
		}
		if s.fast {
			needByExt[ext.Name()] = false
			continue
		}
		owned := ownedFiles(ext, inv.Files)
		need := nonTSExtractorNeed(owned, prevFiles, hashes, ext.Name(), prevScan, scanHash, nonTSForceAll, nonTSConfigChanged)
		if !need {
			need = ownedExtractorContextNeed(owned, s.state, ext, inv.Files, inv.AllNames, hashes, fileSetHash, prevScan, scanHash)
		}
		needByExt[ext.Name()] = need
		if need {
			nonTSNeed = true
		}
	}
	for _, name := range priorExtractorNames(s.state, prevFiles) {
		if detectedExt[name] {
			continue
		}
		needByExt[name] = true
		nonTSNeed = true
		nonTSFileOwners = append(nonTSFileOwners, retireExtractorOwners(s.state, prevFiles, name)...)
		dropExtractorContribution(prevFiles, name)
		delete(extractorDigest, name)
		delete(extractorInput, name)
	}
	// A need is raised from input hashes, which say an extractor has to run,
	// not that its output moves. Answer the second question here, while the run
	// can still decline to publish at all; see proveNonTSNeutrality.
	if err := s.proveNonTSNeutrality(ctx, needByExt, detectedExt, extractorDigest, inv.Files, inv.AllNames, hashes, fileSetHash, scanHash, repoID, nonTSForceAll || nonTSConfigChanged); err != nil {
		return nil, err
	}
	neutralNonTS := s.neutralNonTSPreviews()
	nonTSNeed = false
	for _, need := range needByExt {
		if need {
			nonTSNeed = true
			break
		}
	}
	tr.Mark("extractor_need", fmt.Sprintf("non_ts_need=%v detected=%d neutral=%d", nonTSNeed, len(detectedExt), len(neutralNonTS)))
	if s.opts.AuthoritativeFiles {
		// The name-based graph resolver has no proven isolated domain. Announce
		// the full prior/current file union, while retaining incremental parsing.
		// Empty initial and last-file deletion still Begin before extraction so
		// the frozen manifest is immutable for the run.
		semantic := graphSemanticNames(s.eng, inv.AllNames)
		scanChanged := s.state != nil && !scanHashEquivalent(s.state, inv.AllNames, semantic, hashes, scanHash)
		rawConfigChanged := s.state != nil && s.state.ConfigHash != cfgHash
		policyChanged := policyReconciles(s.state, input)
		// Two coarse digests would otherwise force a publication on their own.
		// The scan digest is one number over every semantic name and its bytes
		// and the configuration fingerprint one number over every analysis
		// input, so neither can say which byte moved; both are discharged only
		// by showing what did. A proven-neutral extractor does that for the
		// files it owns, and rawConfigScopeBounded states the projection
		// argument for the rest of the configuration. Neither discharge is
		// reached while any other reason to publish stands.
		if !initial && !forceAll && !nonTSNeed && s.state != nil && len(neutralNonTS) > 0 &&
			!s.tsFileContextMoved(graphSemanticNames(s.eng, inv.Files), prevFiles, input.tsFileContext) {
			if scanChanged && scanChangeNeutral(s.state, semantic, hashes, prevFiles, neutralNonTS) {
				scanChanged = false
				s.neutralScan = true
			}
			if rawConfigChanged && !scanChanged && !policyChanged {
				if bounded, _ := s.rawConfigScopeBounded(haveCache, input, detectedExt, prevFiles); bounded {
					rawConfigChanged = false
					s.neutralConfig = true
				}
			}
		}
		changed := initial || forceAll || nonTSNeed || s.state == nil || scanChanged || rawConfigChanged || policyChanged
		tr.Mark("changed_terms", fmt.Sprintf("initial=%v force=%v non_ts=%v scan=%v raw_cfg=%v policy=%v neutral_scan=%v neutral_cfg=%v", initial, forceAll, nonTSNeed, scanChanged, rawConfigChanged, policyChanged, s.neutralScan, s.neutralConfig))
		if changed {
			previous := []string{}
			if s.state != nil {
				for f := range s.state.Files {
					previous = append(previous, f)
				}
			}
			// Keep previously published owners even when the current policy
			// excludes them, so the frozen replacement can clear obsolete facts.
			previous = graphPublishedOwners(previous)
			// Only files that can contribute authoritative graph facts belong in
			// the replacement manifest. AllNames also contains ignored marker and
			// lock files used only for extractor detection; including them would
			// inflate Begin without creating deletable graph owners.
			current := graphSemanticNames(s.eng, inv.Files)
			// Initial/global-context changes still require the complete domain.
			// For an ordinary content delta, derive the manifest from changed
			// files plus reverse file-to-file dependents in the prior state.
			wholeDomain := initial || forceAll || s.state == nil || configChanged || policyReconciles(s.state, input) || incompleteDependencyRecords(prevFiles)
			// A raw configuration byte change is not by itself a reason to
			// replace every owner. It is a reason to do so when no active
			// consumer can prove a smaller boundary for it; rawConfigScopeBounded
			// states that proof. The fingerprint, its stored value and the
			// post-Begin rechecks against it are untouched either way.
			if !wholeDomain && s.state.ConfigHash != cfgHash {
				if bounded, reason := s.rawConfigScopeBounded(haveCache, input, detectedExt, prevFiles); !bounded {
					wholeDomain = true
					fallbacks = append(fallbacks, graphstream.Fallback{Extractor: "graph", Scope: "all prior/current file owners", Reason: "configuration inputs changed; " + reason})
				}
			}
			if !wholeDomain && s.state != nil && s.state.FrameworkSig != "" {
				need, ferr := s.frameworkDirtyRequiresFullScope(inv.Files, prevFiles, hashes, angular)
				if ferr != nil {
					return nil, ferr
				}
				if need {
					wholeDomain = true
					forceAll = true
					fallbacks = append(fallbacks, graphstream.Fallback{Extractor: "typescript", Scope: "all owned files", Reason: "graphql/grpc/nuxt composition context changed; re-extracting affected TypeScript files"})
				}
			}
			var extraOwners []string
			var membership membershipDelta
			var proof *frozenPreview
			if !wholeDomain && s.state != nil {
				// Non-TypeScript extractors without per-file incremental support
				// still have a bounded owner domain. Seed the frozen plan with
				// those files and prior contributions instead of every repository
				// owner; the extractor will replace exactly that owner set below.
				//
				// previewed records which of them actually reached one of the two
				// bounded routes below. It is what lets a deleted page be planned
				// at all: membershipScope cannot tell a retirement whose consumers
				// this loop already enumerated from one whose consumers nothing
				// has looked at.
				previewed := map[string]bool{}
				for name, need := range needByExt {
					if !need || name == "typescript" {
						continue
					}
					for _, ext := range s.eng.Extractors() {
						if ext.Name() != name {
							continue
						}
						if !declaresFileOwnership(ext) {
							// The extractor is about to rerun but declares no owner
							// domain, so the files it will emit cannot be enumerated
							// before Begin. Announce the full domain rather than
							// freeze a manifest its output can escape.
							wholeDomain = true
							fallbacks = append(fallbacks, graphstream.Fallback{Extractor: name, Scope: "all prior/current file owners", Reason: "extractor declares no file-owner domain; its owners cannot be planned before Begin"})
							break
						}
						// One extractor can say more than which files it owns:
						// mdintent's whole output is reproducible from captured
						// bytes, so its unchanged pages stay out of the frozen
						// manifest instead of being seeded with it. Every other
						// owner-declaring extractor keeps the conservative seed.
						// Whether THIS extractor was previewed, not whether some
						// earlier pass through this loop previewed mdintent:
						// s.preparedMD stays set until the extraction site
						// consumes it, so reading it here would let mdintent's
						// narrowing silently swallow the next extractor's seed
						// and leave its owners outside the frozen manifest.
						narrowed, ok, perr := s.prepareMDScope(ctx, ext, inv.Files, prevFiles, hashes, repoID, nonTSForceAll || nonTSConfigChanged)
						if perr != nil {
							return nil, perr
						}
						if ok {
							extraOwners = append(extraOwners, narrowed...)
							previewed[name] = true
							break
						}
						for _, file := range ownedFiles(ext, inv.Files) {
							extraOwners = append(extraOwners, filepath.ToSlash(file))
						}
						for _, owner := range retireExtractorOwners(s.state, prevFiles, name) {
							if owner.Kind == graphstream.OwnerFile {
								extraOwners = append(extraOwners, filepath.ToSlash(owner.ID))
							}
						}
						// The seed says which files this extractor writes. It does
						// not say which owners its output moves: Fact.Name resolves
						// globally, so a new, removed or newly ambiguous candidate
						// retargets references in files that are otherwise untouched
						// and that no import edge connects to the change. Close over
						// them here or the frozen plan cannot carry them.
						closure, closed, cerr := s.prepareNonTSCandidateScope(ctx, ext, inv.Files, prevFiles, hashes, repoID)
						if cerr != nil {
							return nil, cerr
						}
						if !closed {
							wholeDomain = true
							fallbacks = append(fallbacks, graphstream.Fallback{Extractor: name, Scope: "all prior/current file owners", Reason: "extractor contribution cannot be previewed before Begin; its candidate names cannot be bounded"})
							break
						}
						extraOwners = append(extraOwners, closure...)
						// The seed claimed every file this extractor owns and
						// every owner it retires, and the closure took every
						// cached owner naming a candidate it moved. That is the
						// same bound the captured-bytes preview gives, so this
						// extractor's own retirements are planned too.
						previewed[name] = true
						break
					}
				}
				// Membership is settled before the preview, never after it. An
				// importer the new file set rebinds is a reparse like any other:
				// its declared names and route mounts can move, so it has to be
				// previewed before the name and composed route deltas run.
				// inv.Files, not the policy-filtered current list, is the
				// resolution universe the extractor itself will use.
				membership = membershipScopeWithProof(previous, current, inv.Files, prevFiles, provenRetiredOwners(prevFiles, previewed))
				if membership.changed {
					extraOwners = append(extraOwners, directoryModuleSiblings(previous, current, prevFiles)...)
				}
				if membership.changed && !membership.proven {
					wholeDomain = true
					fallbacks = append(fallbacks, graphstream.Fallback{Extractor: "graph", Scope: "all prior/current file owners", Reason: membership.reason})
				}
				if !wholeDomain {
					dirty := map[string]bool{}
					for _, f := range current {
						st := lookupState(prevFiles, f)
						h, ok := lookupHash(hashes, f)
						if tsextractor.IsSessionSource(f, false) && (st == nil || !ok || st.Hash != h || st.Unreadable || recMissing(st)) {
							dirty[filepath.ToSlash(f)] = true
						}
					}
					// A configuration edit can move a byte-unchanged source's own
					// semantic context: the package it belongs to, the alias roots
					// its specifiers resolve against. The extraction site already
					// reparses on exactly this comparison, but it runs after Begin,
					// so the same files have to be in the frozen plan or the run
					// fails closed on a reparse it decided on itself. They are
					// seeded as owners too: their facts are replaced, and
					// planFileInvalidation reverse-closes their dependents.
					if s.eng.GraphScope() != nil && s.state != nil {
						for _, f := range current {
							st := lookupState(prevFiles, f)
							if st == nil || st.TS == nil || s.state.TSFileContext[f] == input.tsFileContext[f] {
								continue
							}
							dirty[filepath.ToSlash(f)] = true
							extraOwners = append(extraOwners, filepath.ToSlash(f))
						}
					}
					for _, f := range membership.rebound {
						dirty[f] = true
					}
					// Retired identities carry an old contribution and no new one.
					// They are absent from the owned set, so the planning extract
					// never reads them, but they must reach the deltas below as
					// old->empty so a global consumer of a deleted candidate - a
					// markdown link, a route mount - is inside Begin. Reverse
					// import edges alone cannot reach those.
					retired := make(map[string]bool, len(membership.retired))
					for _, f := range membership.retired {
						retired[f] = true
						dirty[f] = true
					}
					// Deleting a file changes what its importers see, and that reaches
					// further than one edge: invalidateTS reverse-closes the same seeds
					// at extraction time. Closing here too keeps the previewed parse set
					// equal to the one extraction will ask for, so the planning extract
					// stays reusable instead of being recomputed on every delete.
					for p, d := range reverseClose(retired, tsRecordsFromState(prevFiles)) {
						if d {
							dirty[p] = true
						}
					}
					previewFacts, previewProof, perr := s.prepareFrozenTS(ctx, prevFiles, inv.Files, hashes, dirty, retired, angular)
					// The planning extract is a full TS session of its own; without
					// its own mark its cost hides inside the gap between load_state
					// and ts_extract_session.
					tr.Mark("ts_frozen_preview", fmt.Sprintf("dirty=%d err=%v", dirtyCount(dirty), perr != nil))
					if perr != nil {
						wholeDomain = true
						fallbacks = append(fallbacks, graphstream.Fallback{Extractor: "graph", Scope: "all prior/current file owners", Reason: "planning extract of dirty files failed; using whole domain"})
					} else {
						// An angular session never gets here: it sets forceAll
						// above, and wholeDomain is forceAll or better. broad is
						// carried anyway so the narrowing cannot outlive that
						// coupling if it is ever relaxed.
						proof = previewProof
						extraOwners = append(extraOwners, ownersForNameDelta(prevFiles, dirty, previewFacts)...)
						var previewRecs map[string]*tsextractor.FileRecord
						if s.preparedTS != nil {
							previewRecs = s.preparedTS.Records
						}
						extraOwners = append(extraOwners, composedRouteOwnerDelta(prevFiles, dirty, previewRecs, retired)...)
					}
				}
			}
			var planReason string
			s.plan, planReason, err = authoritativeFilePlan(previous, current, prevFiles, hashes, wholeDomain, extraOwners, membership, proof)
			tr.Mark("invalidation_plan", fmt.Sprintf("reason=%s whole=%v extra=%d", planReason, wholeDomain, len(extraOwners)))
			if err != nil {
				return nil, err
			}
			if !wholeDomain && planReason == frozenScopeMembership {
				wholeDomain = true
			}
			if !wholeDomain && len(s.plan.manifest()) == 0 {
				// A scan/config input changed without a changed semantic file. Keep
				// the frozen contract safe by replacing the complete prior/current
				// domain rather than publishing an empty manifest.
				wholeDomain = true
				s.plan, planReason, err = authoritativeFilePlan(previous, current, prevFiles, hashes, true, nil, membership, nil)
				tr.Mark("invalidation_plan_retry", fmt.Sprintf("reason=%s", planReason))
				if err != nil {
					return nil, err
				}
			}
			s.replaceScope = s.plan.manifest()
			s.scopeLimited = true
			scopeLabel := "changed files and reverse file dependents"
			if planReason == frozenScopeMembershipRe {
				scopeLabel = "changed files, resolution-rebound importers and reverse file dependents"
			}
			if wholeDomain || planReason == frozenScopeMembership || planReason == frozenScopeWholeDomain {
				scopeLabel = "all prior/current file owners"
			}
			fallbacks = append(fallbacks, graphstream.Fallback{Extractor: "graph", Scope: scopeLabel, Reason: planReason})
			phase := graphstream.PhaseResolved
			if initial || forceAll {
				phase = graphstream.PhaseEpoch
			}
			if err := s.begin(ctx, runID, repoID, base, target, phase, graphstream.ScopeModeComplete, s.replaceScope); err != nil {
				return nil, err
			}
			if err := s.pub.Flush(ctx); err != nil {
				return nil, err
			}
			// Begin carries the whole replace scope, so this mark is the encode
			// and transport cost of the frozen scope itself, separate from the
			// resolved-fact publication measured later by publish_resolved.
			tr.Mark("begin_publish", fmt.Sprintf("scope=%d", len(s.replaceScope)))
		}
	}

	for _, ext := range s.eng.Extractors() {
		if !s.eng.Config().IsExtractorEnabled(ext.Name()) {
			continue
		}
		if ext.Name() != "typescript" {
			if !detectedExt[ext.Name()] {
				continue
			}
		} else {
			detected, err := detectedExt[ext.Name()], error(nil)
			if err != nil || !detected {
				continue
			}
		}
		owned := ownedFiles(ext, inv.Files)
		if ext.Name() == "typescript" {
			owned = tsextractor.SessionFiles(owned, angular)
		}

		switch ext.Name() {
		case "typescript":
			hadTS = true
			ts, ok := ext.(*tsextractor.TSExtractor)
			if !ok {
				fallbacks = append(fallbacks, graphstream.Fallback{Extractor: "typescript", Scope: "all files", Reason: "unexpected extractor type"})
				continue
			}
			dirty := map[string]bool{}
			semanticDirty := map[string]bool{}
			prevRecs := map[string]*tsextractor.FileRecord{}
			if !forceAll {
				for path, rec := range prevFiles {
					if rec != nil && rec.TS != nil {
						prevRecs[path] = rec.TS
					}
				}
				disc := s.tsRunDiscovery()
				if disc == nil {
					disc = ts.NewDiscovery(ctx, s.abs, s.capturedSources)
				}
				for _, f := range owned {
					h, ok := lookupHash(hashes, f)
					prev := lookupState(prevFiles, f)
					if !ok || prev == nil || prev.Hash != h || prev.Unreadable || recMissing(prev) {
						dirty[f] = true
						continue
					}
					if prev.TS != nil && prev.TS.NuxtScope != "" {
						if prev.TS.NuxtScope != tsextractor.FileNuxtScope(disc, f) {
							dirty[f] = true
							semanticDirty[f] = true
						}
					}
				}
				if s.eng.GraphScope() != nil {
					for _, f := range owned {
						if prevRecs[f] != nil && s.state.TSFileContext[f] != input.tsFileContext[f] {
							dirty[f] = true
							semanticDirty[f] = true
							invalidation.ContextAffectedSources++
						}
					}
				}
				var broadenAll bool
				var invReason string
				dirty, broadenAll, invReason = invalidateTS(dirty, prevRecs, owned, hashes)
				// The frozen preview already ran this closure and parsed what it
				// found, and the plan was frozen around exactly that set. Adopt it
				// so the two sites agree on one dirty set: otherwise the loop below
				// re-derives the preview's own dependents and parses them a second
				// time, and the manifest and the parse count stop matching.
				if s.preparedTS != nil && coversDirty(s.preparedDirty, dirty) {
					for f, d := range s.preparedDirty {
						if d {
							dirty[f] = true
						}
					}
				}
				if broadenAll {
					forceAll = true
					fallbacks = append(fallbacks, graphstream.Fallback{
						Extractor: "typescript",
						Scope:     "all owned files",
						Reason:    invReason,
					})
				}
				if anyDirty(dirty) {
					captured := map[string][]byte{}
					for f, d := range dirty {
						if !d {
							continue
						}
						if _, ok := s.capturedSources[f]; ok {
							continue
						}
						b, rerr := os.ReadFile(filepath.Join(s.abs, f))
						if rerr != nil {
							continue
						}
						captured[f] = b
					}
					s.mergeCaptured(captured)
					sig, sigErr := tsextractor.CompositionSignature(s.abs, owned, prevRecs, dirty, s.capturedSources, s.eng.GraphScope())
					if sigErr != nil {
						return nil, classifyVanished(sigErr, "composition context input for", "typescript", "refusing to plan from a partial capture")
					}
					s.frameworkSig = sig
					if s.state != nil && s.state.FrameworkSig != "" && s.state.FrameworkSig != sig {
						forceAll = true
						fallbacks = append(fallbacks, graphstream.Fallback{
							Extractor: "typescript",
							Scope:     "all owned files",
							Reason:    "graphql/grpc/nuxt composition context changed; re-extracting affected TypeScript files",
						})
					}
				} else if s.state != nil {
					s.frameworkSig = s.state.FrameworkSig
				}
			}
			var dirtyArg map[string]bool
			if !forceAll {
				dirtyArg = dirty
				if dirtyArg == nil {
					dirtyArg = map[string]bool{}
				}
			}

			scopeFiles := map[string]bool{}
			if forceAll {
				for _, f := range owned {
					scopeFiles[filepath.ToSlash(f)] = true
				}
			} else {
				for f, d := range dirty {
					if d {
						scopeFiles[filepath.ToSlash(f)] = true
					}
				}
				for path, prev := range prevFiles {
					if prev == nil || (prev.Extractor != "typescript" && prev.TS == nil) {
						continue
					}
					if _, still := lookupHash(hashes, path); !still {
						scopeFiles[filepath.ToSlash(path)] = true
					}
				}
			}
			reuseTSCache := func() {
				tsNoop = true
				stats.CachedFiles = len(owned)
				for _, rec := range prevRecs {
					if rec != nil {
						ff := cloneTagged(rec.Facts, repoID)
						applyLocalIO(ff)
						allFacts = append(allFacts, ff...)
					}
				}
				syn := cloneTagged(syntheticFactsFor(s.state, "typescript"), repoID)
				allFacts = append(allFacts, syn...)
				appendExtractorSynthetic(synByExt, "typescript", syn)
				tsRecords = prevRecs
			}
			if !forceAll && !anyDirty(dirty) && len(scopeFiles) == 0 {
				reuseTSCache()
				tr.Mark("ts_reuse_cache", fmt.Sprintf("owned=%d facts=%d", len(owned), len(allFacts)))
				continue
			}
			tr.Mark("ts_dirty_scope", fmt.Sprintf("dirty=%d scope=%d force=%v", dirtyCount(dirty), len(scopeFiles), forceAll))
			if s.opts.AuthoritativeFiles && s.plan != nil && !forceAll {
				for f, d := range dirty {
					if !d {
						continue
					}
					id := filepath.ToSlash(f)
					if tsextractor.IsSessionSource(id, angular) && !s.plan.member[id] {
						return nil, fmt.Errorf("frozen invalidation plan missed dirty file %s", id)
					}
				}
			}
			fileOwners := scopeOwnerRefs(scopeFiles, owned, s.state, forceAll)
			fileOwners = append(fileOwners, nonTSFileOwners...)
			graphstream.SortOwners(fileOwners)
			fileOwners = dedupeOwners(fileOwners)
			s.growScope(fileOwners)
			if s.opts.AuthoritativeFiles {
				if err := s.fileLocalErr(); err != nil {
					return nil, err
				}
				if !s.began {
					return nil, fmt.Errorf("frozen Begin was not published before analysis")
				}
			} else {
				phase := graphstream.PhaseResolved
				if forceAll || initial {
					phase = graphstream.PhaseEpoch
				}
				scopeMode := graphstream.ScopeModeComplete
				if forceAll || initial {
					scopeMode = graphstream.ScopeModeIncremental
				}
				s.scopeMode = scopeMode
				if err := s.begin(ctx, runID, repoID, base, target, phase, scopeMode, s.replaceScope); err != nil {
					return nil, err
				}
				if err := s.pub.Flush(ctx); err != nil {
					return nil, err
				}
			}
			hooks := tsextractor.SessionHooks{
				SkipConfigPaths: true,
				Sources:         s.capturedSources,
				// The run's snapshot, kept only if this capture agrees with
				// what it read; the extractor checks that itself and reports a
				// rebuild in Stats.DiscoveryPasses rather than silently
				// answering from a tree nobody here observed.
				Discovery: s.tsRunDiscovery(),
				OnBeforeParse: func(path string) {
					reason := "resolution"
					prev := s.stateFile(path)
					if initial || !haveCache {
						reason = "initial"
					} else if prev == nil || prev.TS == nil {
						reason = "added source"
					} else if prev.Hash != hashes[path] {
						reason = "source content"
					} else if semanticDirty[path] {
						reason = "file semantic context"
					} else if forceAll {
						if len(invalidation.ContextReasons) > 0 || configChanged {
							reason = "semantic context"
						} else {
							reason = "global fallback"
						}
					}
					invalidationMu.Lock()
					invalidation.ParsedByReason[reason]++
					invalidationMu.Unlock()
					if s.opts.OnBeforeParse != nil {
						s.opts.OnBeforeParse(path)
					}
				},
				OnFileLocal: func(rec *tsextractor.FileRecord) {
					if rec == nil {
						return
					}
					if !forceAll && dirtyArg != nil && !dirtyArg[rec.File] && !dirty[rec.File] && !dirty[filepath.ToSlash(rec.File)] {
						return
					}
					s.publishLocal(ctx, runID, repoID, rec, &earlyLocal)
				},
			}
			var res *tsextractor.SessionResult
			// The planning extract is reusable only when it already parsed everything
			// this extraction asks for. dirtyArg is recomputed from the extraction
			// context and can be wider - a context-dirty source, an owned file the
			// graph policy keeps out of the plan - and the preview carries a cached
			// record for anything it did not parse, so reusing it there would freeze
			// a stale surface into the graph.
			if s.preparedTS != nil && coversDirty(s.preparedDirty, dirtyArg) {
				res = s.preparedTS
				// hooks below never fire for these: the preview read the files.
				// Replay what it recorded so the counters and the test hook
				// still see every parse of this delta, exactly once.
				for _, p := range s.preparedParses {
					invalidationMu.Lock()
					invalidation.ParsedByReason[p.reason]++
					invalidationMu.Unlock()
					if s.opts.OnBeforeParse != nil {
						s.opts.OnBeforeParse(p.path)
					}
				}
			}
			if res == nil && s.preparedTS != nil {
				// The preview still read those files; saying nothing here would
				// report the wider extraction below as the whole cost of the delta.
				tr.Mark("ts_preview_discarded", fmt.Sprintf("parsed=%d", s.preparedTS.Stats.FilesParsed))
			}
			s.preparedTS = nil
			s.preparedDirty = nil
			s.preparedParses = nil
			if res == nil {
				var xerr error
				res, xerr = ts.ExtractSession(ctx, s.abs, owned, prevRecs, dirtyArg, hooks)
				if xerr != nil {
					return nil, fmt.Errorf("typescript extract: %w", xerr)
				}
				s.work.TSDiscoveries += res.Stats.DiscoveryPasses
				s.keepProvenDiscovery(res.Discovery)
			}
			tr.Mark("ts_extract_session", fmt.Sprintf("parsed=%d cached=%d facts=%d records=%d", res.Stats.FilesParsed, res.Stats.CachedFiles, len(res.Facts), len(res.Records)))
			if err := s.fileLocalErr(); err != nil {
				return nil, err
			}
			workRecs := map[string]*tsextractor.FileRecord{}
			for k, v := range prevRecs {
				workRecs[k] = v
			}
			for k, v := range res.Records {
				workRecs[k] = v
			}
			if s.opts.AuthoritativeFiles && s.plan != nil && !forceAll {
				for path, rec := range res.Records {
					if rec == nil {
						continue
					}
					id := filepath.ToSlash(path)
					parsed := dirtyArg[id] || dirtyArg[path] || dirty[id] || dirty[path]
					if !parsed {
						continue
					}
					if tsextractor.IsSessionSource(id, angular) && !s.plan.member[id] {
						return nil, fmt.Errorf("frozen invalidation plan missed extracted file %s", id)
					}
					// Import targets of a dirty file are edges owned by that file.
					// They do not require the target owner to be in the replacement.
				}
			}
			if !forceAll {
				// The same observed-surface closure the frozen preview ran, so
				// extraction never asks for an owner the plan did not carry. It is
				// a loop for the same reason the preview is one: a dependent this
				// round pulls in can publish a changed surface of its own, and
				// only a reparse can show that. seen holds every file already
				// parsed with its final source, so no member is parsed twice.
				prevSlash := make(map[string]*tsextractor.FileRecord, len(prevRecs))
				for path, rec := range prevRecs {
					prevSlash[filepath.ToSlash(path)] = rec
				}
				seen := map[string]bool{}
				for p, d := range dirty {
					if d {
						seen[filepath.ToSlash(p)] = true
					}
				}
				// The last round's verdicts outlive the loop: they are the proof
				// behind every side read it decided not to follow.
				changed := map[string]bool{}
				proven := map[string]bool{}
				for {
					changed = map[string]bool{}
					for path, rec := range res.Records {
						if surfaceChanged(prevRecs[path], rec) {
							changed[filepath.ToSlash(path)] = true
						}
					}
					// A prior record with no new one and no current hash is retired:
					// it takes its declared names out of the index with it.
					retired := map[string]bool{}
					for path, old := range prevRecs {
						if old == nil {
							continue
						}
						if res.Records[path] != nil || res.Records[filepath.ToSlash(path)] != nil {
							continue
						}
						if _, still := lookupHash(hashes, path); still {
							continue
						}
						retired[filepath.ToSlash(path)] = true
					}
					newNames, removedNames := declaredNameDelta(prevSlash, res.Records, retired)
					extra := surfaceDependents(seen, changed, workRecs, prevSlash, angular)
					for f := range nameDependents(prevSlash, newNames, removedNames, seen) {
						extra[f] = true
					}
					proven = provenSideReadSources(seen, prevSlash, workRecs)
					for f := range sideReadDependents(seen, proven, prevSlash, hashes, angular) {
						extra[f] = true
					}
					need := map[string]bool{}
					for p, d := range extra {
						if !d || seen[p] {
							continue
						}
						need[p] = true
						dirty[p] = true
					}
					if len(need) == 0 {
						break
					}
					if s.opts.AuthoritativeFiles && s.plan != nil {
						for p := range need {
							id := filepath.ToSlash(p)
							if !s.plan.member[id] {
								return nil, fmt.Errorf("frozen invalidation plan missed post-parse dependent %s", id)
							}
						}
					}
					if s.capturedSources == nil {
						s.capturedSources = map[string][]byte{}
					}
					for p := range need {
						if _, ok := s.capturedSources[p]; ok {
							continue
						}
						b, rerr := os.ReadFile(filepath.Join(s.abs, p))
						if rerr == nil {
							s.capturedSources[p] = b
						}
					}
					more, xerr := ts.ExtractSession(ctx, s.abs, owned, workRecs, need, hooks)
					if xerr != nil {
						return nil, fmt.Errorf("typescript extract: %w", xerr)
					}
					s.work.TSDiscoveries += more.Stats.DiscoveryPasses
					s.keepProvenDiscovery(more.Discovery)
					if err := s.fileLocalErr(); err != nil {
						return nil, err
					}
					acc := res.Stats
					unread := append(append([]string{}, res.Unreadable...), more.Unreadable...)
					res = more
					res.Unreadable = unread
					res.Stats.FilesRead = acc.FilesRead + more.Stats.FilesRead
					res.Stats.FilesParsed = acc.FilesParsed + more.Stats.FilesParsed
					res.Stats.GraphQLParsed = acc.GraphQLParsed + more.Stats.GraphQLParsed
					res.Stats.SFCParsed = acc.SFCParsed + more.Stats.SFCParsed
					res.Stats.SummaryScans = acc.SummaryScans + more.Stats.SummaryScans
					res.Stats.DerivedIndexes = acc.DerivedIndexes + more.Stats.DerivedIndexes
					workRecs = more.Records
					for p := range need {
						seen[p] = true
					}
				}
				refreshProvenSideReads(res.Records, seen, proven, hashes, angular)
			}
			if err := s.flushPhase(ctx, runID, graphstream.PhaseLocal); err != nil {
				return nil, err
			}
			stats = res.Stats
			tsRecords = res.Records
			applyLocalIO(res.Facts)
			tagRepo(res.Facts, repoID)
			allFacts = append(allFacts, res.Facts...)
			appendExtractorSynthetic(synByExt, "typescript", res.Facts)
			addChangedRouteFiles(scopeFiles, composedRouteFacts(tsRecordsFromState(prevFiles)), composedRouteFacts(res.Records))
			for f, d := range dirty {
				if d {
					scopeFiles[filepath.ToSlash(f)] = true
				}
			}
			fileOwners = scopeOwnerRefs(scopeFiles, owned, s.state, forceAll)
			fileOwners = append(fileOwners, nonTSFileOwners...)
			graphstream.SortOwners(fileOwners)
			fileOwners = dedupeOwners(fileOwners)
			s.growScope(fileOwners)
			if err := s.fileLocalErr(); err != nil {
				return nil, err
			}
			if err := s.publishScope(ctx, runID, s.replaceScope); err != nil {
				return nil, err
			}
			unreadable = append(unreadable, res.Unreadable...)
			// Planning uses dirty source hashes as a conservative pre-Begin
			// signal because newly parsed declarations are not available yet.
			// Persist the canonical post-extraction signature from the complete
			// records instead, so the next clean run compares the same form and
			// does not mistake a settled dirty file for a new framework change.
			if sig, sigErr := tsextractor.CompositionSignature(s.abs, owned, res.Records, map[string]bool{}, s.capturedSources, s.eng.GraphScope()); sigErr == nil {
				s.frameworkSig = sig
			} else {
				return nil, classifyVanished(sigErr, "composition context input for", "typescript", "refusing to commit a partial composition signature")
			}
		default:
			need := needByExt[ext.Name()]
			// A need discharged before Begin was discharged by proof, and this
			// site must not ask the weaker question again. The comparison below
			// is the one that raised the need in the first place - inputs whose
			// hashes moved - and it will raise it again, because the bytes on
			// disk really are not the bytes the stored state records. Re-running
			// the extractor on that answer reads the live tree after the plan is
			// frozen: an input edited since the capture then reaches the graph
			// as an owner the frozen manifest does not carry, and the run fails
			// on the scope audit instead of on the fence that owns this
			// refusal. The proven answer stands, and the captured sources are
			// read back before EndReplace exactly as for any other preview.
			proven := false
			if pv := s.nonTSPreviews[ext.Name()]; pv != nil && pv.neutral {
				proven = true
			}
			if !need && !s.fast && !proven {
				need = nonTSExtractorNeed(owned, prevFiles, hashes, ext.Name(), prevScan, scanHash, nonTSForceAll, nonTSConfigChanged)
				if !need {
					need = ownedExtractorContextNeed(owned, s.state, ext, inv.Files, inv.AllNames, hashes, fileSetHash, prevScan, scanHash)
				}
				needByExt[ext.Name()] = need
			}
			if !need {
				cached := cachedFactsFor(ext.Name(), owned, prevFiles)
				allFacts = append(allFacts, cached...)
				syn := cloneTagged(syntheticFactsFor(s.state, ext.Name()), repoID)
				allFacts = append(allFacts, syn...)
				appendExtractorSynthetic(synByExt, ext.Name(), syn)
				if !s.fast {
					extractorInput[ext.Name()] = extractorInputDigest(ext, owned, inv.Files, inv.AllNames, hashes, fileSetHash, scanHash)
				}
				if pv := s.nonTSPreviews[ext.Name()]; pv != nil && pv.neutral {
					// This need was discharged by proof, not by matching
					// hashes: the inputs on disk differ from the ones the
					// stored state records, and no later site will write down
					// what this run read.
					refreshExtractorObservedInputs(prevFiles, owned, hashes, ext.Name())
				}
				continue
			}
			fallbacks = append(fallbacks, graphstream.Fallback{
				Extractor: ext.Name(),
				Scope:     "all files owned by extractor",
				Reason:    "no per-file incremental session; whole-extractor re-run",
			})
			tExt := time.Now()
			// A preview made before Begin is the extraction, not a rehearsal of
			// one: re-running it here would read a tree that may have moved and
			// could contradict the manifest already frozen from it.
			prepared := s.takePreparedMD(ext.Name())
			var extracted []facts.Fact
			reused, wasPreviewed := s.takePreparedNonTS(ext.Name())
			if prepared != nil {
				extracted = prepared.facts
			} else if wasPreviewed {
				// Planned from this output; re-running would read a tree that may
				// have moved and could contradict the frozen manifest. Its owners
				// were seeded conservatively, so unlike a narrowed preview it still
				// derives replacement owners the ordinary way below.
				extracted = reused
			} else {
				out, err := ext.Extract(ctx, s.abs, inv.Files)
				graphprofile.Since("non_ts_extract", tExt, fmt.Sprintf("%s facts=%d", ext.Name(), len(out)))
				if err != nil {
					var fatal *plugin.FatalError
					if asFatal(err, &fatal) {
						return nil, err
					}
					log.Printf("[graphsession] extractor %s: %v", ext.Name(), err)
					continue
				}
				applyLocalIO(out)
				tagRepo(out, repoID)
				extracted = out
			}
			extractorInput[ext.Name()] = extractorInputDigest(ext, owned, inv.Files, inv.AllNames, hashes, fileSetHash, scanHash)
			tFP := time.Now()
			fp := factsFingerprint(extracted)
			graphprofile.Since("non_ts_fingerprint", tFP, ext.Name())
			// The whole-output short-circuit returns before the contributions are
			// stored, so the per-file hash it consumed stays stale and the next
			// run re-extracts for the same reason. A previewed extractor has a
			// scope narrowed to what changed - nothing, in this case - so it can
			// afford to fall through and refresh those keys instead.
			if prepared == nil && !nonTSForceAll && !nonTSConfigChanged && extractorDigest[ext.Name()] != "" && extractorDigest[ext.Name()] == fp {
				cached := cachedFactsFor(ext.Name(), owned, prevFiles)
				allFacts = append(allFacts, cached...)
				syn := cloneTagged(syntheticFactsFor(s.state, ext.Name()), repoID)
				allFacts = append(allFacts, syn...)
				appendExtractorSynthetic(synByExt, ext.Name(), syn)
				// Record the inputs this proof was made against. Returning here
				// without doing so is what left a version-only manifest edit
				// needing the extractor on every later run: each run re-extracted,
				// proved nothing changed, published a Begin anyway because the
				// plan had already frozen, and advanced a generation on an
				// unchanged repository.
				refreshExtractorObservedInputs(prevFiles, owned, hashes, ext.Name())
				needByExt[ext.Name()] = false
				continue
			}
			// Replacement must retire prior synthetic owners too (for example a
			// Swift target whose include changes its module identity). A
			// previewed extractor announced the owners it changes before Begin;
			// re-deriving the whole domain here would announce owners the frozen
			// plan does not carry, which is the failure the narrowing exists to
			// avoid. Its facts are still published and stored in full below.
			if prepared != nil {
				nonTSFileOwners = append(nonTSFileOwners, prepared.owners...)
			} else {
				nonTSFileOwners = append(nonTSFileOwners, retireExtractorOwners(s.state, prevFiles, ext.Name())...)
			}
			extractorDigest[ext.Name()] = fp
			allFacts = append(allFacts, extracted...)
			appendExtractorSynthetic(synByExt, ext.Name(), extracted)
			byFile := map[string][]facts.Fact{}
			for _, f := range extracted {
				byFile[filepath.ToSlash(f.File)] = append(byFile[filepath.ToSlash(f.File)], f)
				if prepared == nil {
					nonTSFileOwners = append(nonTSFileOwners, ownerOf(f))
				}
			}
			if prepared == nil {
				for _, fpath := range owned {
					nonTSFileOwners = append(nonTSFileOwners, graphstream.OwnerRef{Kind: graphstream.OwnerFile, ID: filepath.ToSlash(fpath)})
				}
				for path, prev := range prevFiles {
					if !extractorOwnsState(prev, ext.Name()) {
						continue
					}
					if _, still := lookupHash(hashes, path); !still {
						nonTSFileOwners = append(nonTSFileOwners, graphstream.OwnerRef{Kind: graphstream.OwnerFile, ID: filepath.ToSlash(path)})
					}
				}
			}
			if len(owned) == 0 {
				for fpath, ff := range byFile {
					if fpath == "" {
						continue
					}
					h, _ := lookupHash(hashes, fpath)
					storeExtractorContribution(prevFiles, fpath, ext.Name(), h, ff)
				}
			} else {
				for _, fpath := range owned {
					h, _ := lookupHash(hashes, fpath)
					storeExtractorContribution(prevFiles, fpath, ext.Name(), h, byFile[filepath.ToSlash(fpath)])
				}
			}
		}
	}
	nonTSNeed = false
	for _, n := range needByExt {
		if n {
			nonTSNeed = true
			break
		}
	}
	s.growScope(nonTSFileOwners)
	if err := s.fileLocalErr(); err != nil {
		return nil, err
	}

	// Deleted TS files: empty replacement later; drop from state.
	newFiles := map[string]*FileState{}
	for path, rec := range tsRecords {
		hash := hashes[path]
		if rec != nil && rec.Hash != "" {
			hash = rec.Hash
		}
		st := &FileState{
			Hash:       hash,
			Extractor:  "typescript",
			Unreadable: rec != nil && rec.Unreadable,
			Minified:   rec != nil && rec.Minified,
			TS:         rec,
		}
		if prev := lookupState(prevFiles, path); prev != nil {
			mergeNonTSContrib(st, prev)
		}
		if rec != nil {
			st.Declared = rec.Declared
			st.Referenced = rec.Referenced
			st.Imports = rec.ResolvedFiles
			st.Reexports = rec.Reexports
			if rec.Unreadable {
				// Preserve previous successful facts; do not treat as deletion.
				if prev := lookupState(prevFiles, path); prev != nil && !prev.Unreadable {
					st.TS = prev.TS
					st.Hash = prev.Hash
					st.Declared = prev.Declared
					st.Referenced = prev.Referenced
					st.Imports = prev.Imports
					st.Reexports = prev.Reexports
				}
			}
		}
		newFiles[path] = st
	}
	for path, st := range prevFiles {
		if st == nil {
			continue
		}
		if _, ok := lookupHash(hashes, path); !ok {
			continue
		}
		if newFiles[path] != nil {
			mergeNonTSContrib(newFiles[path], st)
			continue
		}
		if st.Extractor == "typescript" && st.TS != nil {
			continue
		}
		newFiles[path] = st
	}

	if !initial && (tsNoop || !hadTS) && !nonTSNeed && !s.began {
		s.skipPublish = true
	}
	tr.Mark("assemble_new_files", fmt.Sprintf("new=%d ts_noop=%v non_ts_need=%v skip=%v facts=%d", len(newFiles), tsNoop, nonTSNeed, s.skipPublish, len(allFacts)))
	if s.skipPublish {
		if s.eng.GraphScope() != nil {
			after, _, _, err := analysisFingerprintInputs(s.abs, s.eng)
			if err != nil {
				return nil, err
			}
			if after != cfgHash {
				return nil, fmt.Errorf("%w: raw analysis inputs changed during no-publication transaction", ErrInputsChanged)
			}
		}
		cached := stats.CachedFiles
		if cached == 0 {
			cached = len(prevFiles)
		}
		tr.Mark("skip_publish_return", "")
		if s.validateEffective != nil {
			if err := s.validateEffective(); err != nil {
				return nil, err
			}
		}
		// A run that publishes nothing can still be holding bookkeeping the
		// stored state has not caught up with. Every refresh below rewrites
		// what this run observed and nothing else - the generation and every
		// published fact are carried over untouched - so they are collapsed
		// into one clone and one save rather than racing each other's write,
		// and the result is adopted as this session's state so that a resident
		// holding it in memory sees the same metadata the file now carries
		// without rereading it, and without a second run repeating the work.
		//
		// The proven-neutral refresh is the one that also touches file records.
		// Its extractor read inputs that differ from the ones the stored state
		// describes and proved they produce the same output; if that
		// observation is not written down here, the next run raises the same
		// need for the same reason and proves the same thing again, forever.
		neutralNonTS := s.neutralNonTSPreviews()
		if len(neutralNonTS) > 0 || s.neutralScan || s.neutralConfig {
			// An observation commits under the same fence a publication does.
			if err := s.revalidateCapturedInputs("refusing to record the run's observations"); err != nil {
				return nil, err
			}
		}
		refreshScan := s.opts.AuthoritativeFiles && s.state != nil &&
			(s.state.ScanHashVersion != scanHashVersion || s.state.ScanHash != scanHash) &&
			(s.neutralScan || scanHashEquivalent(s.state, inv.AllNames, graphSemanticNames(s.eng, inv.AllNames), hashes, scanHash))
		refreshPolicy, perr := s.policyBookkeepingFenced(input)
		if perr != nil {
			return nil, perr
		}
		if refreshScan || refreshPolicy || s.neutralConfig || len(neutralNonTS) > 0 {
			st, err := cloneState(s.state)
			if err != nil {
				return nil, err
			}
			if refreshScan {
				st.ScanHash = scanHash
				st.ScanHashVersion = scanHashVersion
			}
			if refreshPolicy {
				st.PolicyIdentity = input.policyIdentity
				st.PolicyAdmissionIdentity = input.admissionIdentity
			}
			if s.neutralConfig {
				st.ConfigHash = cfgHash
			}
			for name, pv := range neutralNonTS {
				refreshExtractorObservedInputs(st.Files, pv.owned, hashes, name)
				if pv.input != "" {
					if st.ExtractorInputHash == nil {
						st.ExtractorInputHash = map[string]string{}
					}
					st.ExtractorInputHash[name] = pv.input
				}
			}
			if err := saveState(s.opts.StateDir, st); err != nil {
				return nil, err
			}
			s.state = st
		}
		return &Result{
			Invalidation:     invalidation,
			BaseGeneration:   base,
			TargetGeneration: base,
			Stats:            tsextractor.ExtractStats{CachedFiles: cached},
			Facts:            allFacts,
			ParsedFiles:      0,
		}, nil
	}

	if s.opts.AuthoritativeFiles {
		kept := make([]facts.Fact, 0, len(allFacts))
		for _, f := range allFacts {
			o := ownerOf(f)
			if o.Kind == graphstream.OwnerSynthetic {
				continue
			}
			kept = append(kept, f)
		}
		allFacts = kept
	}
	idx := buildIndex(allFacts)
	s.inputs.resolution = idx
	grouped := groupOwners(allFacts)
	if s.state != nil && s.scopeLimited {
		old, next := resolutionIndexes(s.state.Files, newFiles, allFacts, repoID)
		resOwners := changedResolutionOwners(grouped, old, next, nil)
		if s.opts.AuthoritativeFiles && s.plan != nil {
			for _, o := range resOwners {
				if o.Kind == graphstream.OwnerFile && !s.plan.member[o.ID] {
					return nil, fmt.Errorf("frozen invalidation plan missed resolution owner %s", o.ID)
				}
			}
		}
		s.growScope(resOwners)
		if err := s.fileLocalErr(); err != nil {
			return nil, err
		}
	}
	tr.Mark("index_group_owners", fmt.Sprintf("facts=%d owners=%d", len(allFacts), len(grouped)))
	owners := make([]graphstream.OwnerRef, 0, len(grouped)+len(s.replaceScope)+8)
	owners = append(owners, s.replaceScope...)
	if !s.scopeLimited || forceAll || initial {
		for _, g := range grouped {
			owners = append(owners, g.Owner)
		}
	}
	if s.state != nil {
		for path := range s.state.Files {
			if _, still := newFiles[path]; !still {
				owners = append(owners, graphstream.OwnerRef{Kind: graphstream.OwnerFile, ID: filepath.ToSlash(path)})
			}
		}
	}
	s.growScope(owners)
	if err := s.fileLocalErr(); err != nil {
		return nil, err
	}
	inScope := map[string]bool{}
	for _, o := range s.replaceScope {
		inScope[o.String()] = true
	}
	if !s.began {
		if s.opts.AuthoritativeFiles {
			return nil, fmt.Errorf("frozen Begin was not published before analysis")
		}
		phase := graphstream.PhaseResolved
		if forceAll || initial {
			phase = graphstream.PhaseEpoch
		}
		if err := s.begin(ctx, runID, repoID, base, target, phase, graphstream.ScopeModeComplete, s.replaceScope); err != nil {
			return nil, err
		}
		if err := s.pub.Flush(ctx); err != nil {
			return nil, err
		}
		tr.Mark("begin_publish_late", fmt.Sprintf("scope=%d", len(s.replaceScope)))
	} else if err := s.publishScope(ctx, runID, s.replaceScope); err != nil {
		return nil, err
	} else {
		tr.Mark("publish_scope", fmt.Sprintf("scope=%d", len(s.replaceScope)))
	}

	if len(unreadable) > 0 {
		return nil, fmt.Errorf("unreadable files: %s (refusing successful EndReplace)", strings.Join(unreadable, ", "))
	}

	published := 0
	for _, g := range grouped {
		if s.scopeLimited && !inScope[g.Owner.String()] {
			continue
		}
		nodes, edges := encodeOwner(g, idx, false)
		if len(nodes) == 0 && len(edges) == 0 {
			continue
		}
		if err := s.appendPhase(ctx, runID, graphstream.PhaseResolved, nodes, edges, false); err != nil {
			return nil, err
		}
		published++
	}
	tr.Mark("encode_append_owners", fmt.Sprintf("published=%d", published))
	if err := s.flushPhase(ctx, runID, graphstream.PhaseResolved); err != nil {
		return nil, err
	}
	if err := s.pub.Flush(ctx); err != nil {
		return nil, err
	}
	tr.Mark("publish_resolved", fmt.Sprintf("owners=%d", published))
	if published == 0 {
		published = s.resolvedOwners
	}

	syn := map[string][]facts.Fact{}
	for _, g := range grouped {
		if g.Owner.Kind == graphstream.OwnerSynthetic {
			syn[g.Owner.ID] = g.Facts
		}
	}
	next := newState(repoID, s.opts.ContextID, s.abs, engine.ExtractorVersion())
	if s.opts.AuthoritativeFiles {
		next.Protocol = graphstream.FrozenSchemaVersion
	}
	next.Generation = target
	next.ConfigHash = cfgHash
	next.TSContext = input.tsContext
	next.TSFileContext = input.tsFileContext
	next.EngineContextHash = input.engineContextHash
	next.PolicyIdentity = input.policyIdentity
	next.PolicyAdmissionIdentity = input.admissionIdentity
	next.FrameworkSig = s.frameworkSig
	next.SinkID = s.opts.SinkID
	next.Files = newFiles
	next.Synthetic = syn
	next.ScanHash = scanHash
	next.ScanHashVersion = scanHashVersion
	next.ExtractorDigest = extractorDigest
	next.ExtractorInputHash = extractorInput
	next.ExtractorSynthetic = synByExt
	next.LastRunID = runID
	next.LastComplete = true
	nRehash := 0
	for path, rec := range tsRecords {
		if rec == nil || rec.Hash == "" || rec.Unreadable {
			continue
		}
		if s.fast {
			if _, captured := s.capturedSources[path]; !captured {
				continue
			}
		}
		s.work.VerifiedFiles++
		disk, rerr := os.ReadFile(filepath.Join(s.abs, path))
		if rerr != nil {
			// A source this run compiled from that is gone by the time the run
			// re-proves it is the ordinary deletion race, not a fault: the
			// attempt is refused without an End and the watch reconciles.
			return nil, classifyVanished(rerr, "source", path, "refusing successful EndReplace")
		}
		sum := sha256.Sum256(disk)
		if hex.EncodeToString(sum[:]) != rec.Hash {
			return nil, fmt.Errorf("%w: source %s changed during the run; refusing successful EndReplace", ErrInputsChanged, path)
		}
		nRehash++
	}
	tr.Mark("revalidate_ts_records", fmt.Sprintf("n=%d", nRehash))
	if !s.fast {
		for k, v := range captureDeltaContexts(s.eng, s.abs, detectedExt) {
			if contextInputs[k] != v {
				return nil, fmt.Errorf("%w: extractor context changed during the run; refusing successful EndReplace", ErrInputsChanged)
			}
		}
		cfgAfter, capturedAfter, fpAfter := analysisFingerprint(s.abs, s.eng)
		if fpAfter != nil {
			return nil, fpAfter
		}
		if cfgAfter != cfgHash {
			return nil, fmt.Errorf("%w: analysis inputs changed during the run; refusing successful EndReplace", ErrInputsChanged)
		}
		for rel, src := range s.cfgCaptured {
			if !bytes.Equal(capturedAfter[rel], src) {
				return nil, fmt.Errorf("%w: analysis inputs changed during the run (%s); refusing successful EndReplace", ErrInputsChanged, rel)
			}
		}
	}
	if err := s.revalidateCapturedInputs("refusing successful EndReplace"); err != nil {
		return nil, err
	}
	if s.validateEffective != nil {
		if err := s.validateEffective(); err != nil {
			return nil, err
		}
	}
	s.work.Checkpoints++
	if err := writePendingState(s.opts.StateDir, next); err != nil {
		return nil, err
	}
	if st, err := os.Stat(pendingStatePath(s.opts.StateDir)); err == nil {
		s.work.CheckpointBytes = st.Size()
	}
	tr.Mark("write_pending_state", "")
	if err := s.end(ctx, runID, s.seq, s.replaceScope, graphstream.Completeness{
		Status:        "success",
		FilesAnalyzed: len(inv.Files),
		ParsedFiles:   stats.FilesParsed,
		CachedFiles:   stats.CachedFiles,
		SummaryScans:  stats.SummaryScans,
		EarlyLocal:    earlyLocal > 0,
		Fallbacks:     fallbacks,
	}); err != nil {
		return nil, err
	}
	if err := s.pub.Flush(ctx); err != nil {
		return nil, err
	}
	if !journalHasAckedEnd(journal, runID) {
		return nil, fmt.Errorf("graphsession: EndReplace for %s was not acknowledged; leaving pending-state in place", runID)
	}
	tr.Mark("end_flush_ack", "")
	if err := promotePendingState(s.opts.StateDir); err != nil {
		return nil, err
	}
	if journal != nil {
		_ = journal.CompactAcked()
	}
	_ = os.Remove(filepath.Join(s.opts.StateDir, "pending.json"))
	s.state = next
	tr.Mark("promote_compact_state", fmt.Sprintf("parsed=%d published=%d", stats.FilesParsed, published))

	classified := 0
	for _, n := range invalidation.ParsedByReason {
		classified += n
	}
	if remainder := stats.FilesParsed - classified; remainder > 0 {
		invalidation.ParsedByReason["specialized extraction"] = remainder
	}
	return &Result{
		Invalidation:     invalidation,
		RunID:            runID,
		BaseGeneration:   base,
		TargetGeneration: target,
		Stats:            stats,
		Fallbacks:        fallbacks,
		Unreadable:       unreadable,
		OwnersPublished:  published,
		ParsedFiles:      stats.FilesParsed,
		EarlyLocal:       earlyLocal,
		Facts:            allFacts,
	}, nil
}

type ownerChunk struct {
	nodes []graphstream.Node
	edges []graphstream.Edge
}

func chunkOwner(nodes []graphstream.Node, edges []graphstream.Edge, limit int) []ownerChunk {
	if limit <= 0 {
		return []ownerChunk{{nodes, edges}}
	}
	var out []ownerChunk
	for len(nodes) > 0 || len(edges) > 0 {
		c := ownerChunk{}
		n := limit
		if len(nodes) < n {
			n = len(nodes)
		}
		c.nodes = nodes[:n]
		nodes = nodes[n:]
		e := limit
		if len(edges) < e {
			e = len(edges)
		}
		c.edges = edges[:e]
		edges = edges[e:]
		out = append(out, c)
	}
	return out
}

func (s *session) begin(ctx context.Context, runID, repoID string, base, target int64, phase, scopeMode string, owners []graphstream.OwnerRef) error {
	if s.opts.AuthoritativeFiles && s.began {
		return s.fileLocalErr()
	}
	limit := s.opts.MaxBeginBytes
	if limit <= 0 {
		limit = 256 * 1024
		if s.opts.AuthoritativeFiles {
			limit = 512 * 1024
		}
	}
	if scopeMode == "" {
		scopeMode = graphstream.ScopeModeComplete
	}
	s.scopeMode = scopeMode
	msg := graphstream.BeginReplace{
		Type:             graphstream.TypeBeginReplace,
		SchemaVersion:    graphstream.SchemaVersion,
		RepoID:           repoID,
		ContextID:        s.opts.ContextID,
		RunID:            runID,
		BaseGeneration:   base,
		TargetGeneration: target,
		Phase:            phase,
		ScopeMode:        scopeMode,
		OwnerScope:       owners,
		OwnerScopeCount:  len(owners),
	}
	if s.opts.AuthoritativeFiles {
		msg.SchemaVersion = graphstream.FrozenSchemaVersion
		msg.ScopeMode = graphstream.ScopeModeComplete
		msg.OwnerScopeDigest = graphstream.DigestOwners(owners)
	}
	if s.state != nil && s.state.ForkBaseRunID != "" && s.state.LastRunID == s.state.ForkBaseRunID {
		msg.ForkBaseRepoID = s.state.ForkBaseRepoID
		msg.ForkBaseContextID = s.state.ForkBaseContextID
		msg.ForkBaseGeneration = s.state.ForkBaseGeneration
		msg.ForkBaseRunID = s.state.ForkBaseRunID
	}
	payload, err := graphstream.Marshal(msg)
	if err != nil {
		return err
	}
	if s.opts.AuthoritativeFiles && len(payload) > limit {
		return fmt.Errorf("frozen Begin manifest is %d bytes, exceeds limit %d; no scope chunks allowed", len(payload), limit)
	}
	if len(payload) > limit && len(owners) > 0 {
		msg.OwnerScope = nil
		payload, err = graphstream.Marshal(msg)
		if err != nil {
			return err
		}
		if err := s.pub.Publish(ctx, graphstream.MessageID(msg.RunID, graphstream.TypeBeginReplace, 0), payload); err != nil {
			return err
		}
		s.began = true
		s.noteAnnounced(nil)
		const chunk = 200
		for i := 0; i < len(owners); i += chunk {
			j := i + chunk
			if j > len(owners) {
				j = len(owners)
			}
			seq := s.nextSeq()
			b := graphstream.Batch{Type: graphstream.TypeBatch, RunID: runID, Seq: seq, Phase: graphstream.PhaseScope, Owners: owners[i:j]}
			bp, err := graphstream.Marshal(b)
			if err != nil {
				return err
			}
			s.storePayload(seq, bp)
			if err := s.pub.Publish(ctx, graphstream.MessageID(runID, graphstream.TypeBatch, seq), bp); err != nil {
				return err
			}
			s.noteAnnounced(owners[i:j])
		}
		return nil
	}
	if err := s.pub.Publish(ctx, graphstream.MessageID(msg.RunID, graphstream.TypeBeginReplace, 0), payload); err != nil {
		return err
	}
	s.began = true
	s.noteAnnounced(owners)
	return nil
}

func (s *session) nextSeq() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seq++
	return s.seq
}

func (s *session) storePayload(seq int, payload []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if seq > len(s.batchPayloads) {
		if seq <= cap(s.batchPayloads) {
			s.batchPayloads = s.batchPayloads[:seq]
		} else {
			ncap := cap(s.batchPayloads) * 2
			if ncap < seq {
				ncap = seq
			}
			if ncap < 16 {
				ncap = 16
			}
			n := make([][]byte, seq, ncap)
			copy(n, s.batchPayloads)
			s.batchPayloads = n
		}
	}
	s.batchPayloads[seq-1] = payload
}

func (s *session) noteAnnounced(owners []graphstream.OwnerRef) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.announced == nil {
		s.announced = map[string]bool{}
	}
	for _, o := range owners {
		s.announced[o.String()] = true
	}
}

// revalidateCapturedInputs re-proves that every snapshot this run compiled from
// is still the tree. The captured bytes are compared directly; the previewed
// context digests are re-derived rather than kept, so an input that appeared,
// vanished or became unreadable mid-run is caught along with one that was
// edited. Both a publication and a run that records observations without
// publishing have to clear it before committing anything.
func (s *session) revalidateCapturedInputs(refusing string) error {
	for rel, src := range s.capturedSources {
		s.work.CapturedReads++
		disk, rerr := os.ReadFile(filepath.Join(s.abs, rel))
		if rerr != nil {
			// A captured input that vanished changed; one that is still there
			// but unreadable is a fault the operator has to see, so it must not
			// enter the watch retry path.
			return classifyVanished(rerr, "captured source", rel, refusing)
		}
		if !bytes.Equal(disk, src) {
			return fmt.Errorf("%w: source/config bytes changed during the run (%s); %s", ErrInputsChanged, rel, refusing)
		}
	}
	for _, fence := range s.previewFences {
		if err := fence(); err != nil {
			return err
		}
	}
	return nil
}

func (s *session) mergeCaptured(extra map[string][]byte) {
	if s.capturedSources == nil {
		s.capturedSources = map[string][]byte{}
	}
	for k, v := range extra {
		if _, ok := s.capturedSources[k]; ok {
			continue
		}
		s.capturedSources[k] = v
	}
}

func (s *session) fileLocalErr() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.localErr
}

func (s *session) growScope(owners []graphstream.OwnerRef) {
	if s.opts.AuthoritativeFiles {
		if s.localErr != nil {
			return
		}
		if s.plan == nil {
			for _, o := range owners {
				if o.Kind == graphstream.OwnerSynthetic || o.Kind == "" {
					continue
				}
				s.localErr = fmt.Errorf("missing frozen invalidation plan")
				return
			}
			return
		}
		for _, o := range owners {
			if o.Kind == graphstream.OwnerSynthetic {
				continue
			}
			if err := s.plan.check(o); err != nil {
				s.localErr = err
				return
			}
		}
		return
	}
	if len(owners) == 0 {
		return
	}
	s.replaceScope = append(s.replaceScope, owners...)
	graphstream.SortOwners(s.replaceScope)
	s.replaceScope = dedupeOwners(s.replaceScope)
	s.scopeLimited = true
}

func scopeOwnerRefs(scopeFiles map[string]bool, owned []string, st *State, forceAll bool) []graphstream.OwnerRef {
	fileOwners := make([]graphstream.OwnerRef, 0, len(scopeFiles)+8)
	seenDir := map[string]bool{}
	for f := range scopeFiles {
		fileOwners = append(fileOwners, graphstream.OwnerRef{Kind: graphstream.OwnerFile, ID: f})
		dir := filepath.ToSlash(filepath.Dir(f))
		if !seenDir[dir] {
			seenDir[dir] = true
			fileOwners = append(fileOwners, graphstream.OwnerRef{Kind: graphstream.OwnerSynthetic, ID: "module:" + dir})
		}
	}
	if st != nil {
		ownedSet := map[string]bool{}
		for _, f := range owned {
			ownedSet[filepath.ToSlash(f)] = true
		}
		for id := range st.Synthetic {
			if !strings.HasPrefix(id, "module:") {
				continue
			}
			dir := strings.TrimPrefix(id, "module:")
			still := false
			for f := range ownedSet {
				if filepath.ToSlash(filepath.Dir(f)) == dir {
					still = true
					break
				}
			}
			if !still {
				fileOwners = append(fileOwners, graphstream.OwnerRef{Kind: graphstream.OwnerSynthetic, ID: id})
			}
		}
		if forceAll {
			for path, prev := range st.Files {
				if prev == nil {
					continue
				}
				if prev.Extractor != "typescript" && prev.TS == nil {
					continue
				}
				fileOwners = append(fileOwners, graphstream.OwnerRef{Kind: graphstream.OwnerFile, ID: filepath.ToSlash(path)})
			}
			for id := range st.Synthetic {
				fileOwners = append(fileOwners, graphstream.OwnerRef{Kind: graphstream.OwnerSynthetic, ID: id})
			}
		}
	}
	if forceAll {
		fileOwners = append(fileOwners, graphstream.OwnerRef{Kind: graphstream.OwnerSynthetic, ID: "aggregate:typescript"})
	}
	return fileOwners
}

func (s *session) publishLocal(ctx context.Context, runID, repoID string, rec *tsextractor.FileRecord, earlyLocal *int) {
	if s.opts.AuthoritativeFiles {
		return
	}
	if rec == nil || rec.Unreadable || rec.Minified {
		return
	}
	s.mu.Lock()
	if s.localErr != nil {
		s.mu.Unlock()
		return
	}
	s.mu.Unlock()
	ff := cloneTagged(rec.Facts, repoID)
	applyLocalIO(ff)
	nodes, edges := encodeOwner(ownerOutput{
		Owner: graphstream.OwnerRef{Kind: graphstream.OwnerFile, ID: filepath.ToSlash(rec.File)},
		Facts: ff,
	}, &idIndex{byName: map[string][]facts.Fact{}}, true)
	if err := s.appendPhase(ctx, runID, graphstream.PhaseLocal, nodes, edges, true); err != nil {
		s.mu.Lock()
		if s.localErr == nil {
			s.localErr = err
		}
		s.mu.Unlock()
		return
	}
	s.mu.Lock()
	*earlyLocal++
	s.mu.Unlock()
}

// Session-side aggregation: mixed-owner batches packed to BatchLimit.
// Empty resolved batches are omitted; deletion is owner-scope membership at End.
func (s *session) appendPhase(ctx context.Context, runID, phase string, nodes []graphstream.Node, edges []graphstream.Edge, earlyLocal bool) error {
	if len(nodes) == 0 && len(edges) == 0 {
		return nil
	}
	limit := s.opts.BatchLimit
	if limit <= 0 {
		limit = 64
	}
	s.mu.Lock()
	if s.localErr != nil {
		err := s.localErr
		s.mu.Unlock()
		return err
	}
	if earlyLocal && phase == graphstream.PhaseLocal && !s.firstLocalSent {
		firstNodes := append([]graphstream.Node(nil), s.localNodes...)
		firstEdges := append([]graphstream.Edge(nil), s.localEdges...)
		s.localNodes = nil
		s.localEdges = nil
		s.firstLocalSent = true
		s.mu.Unlock()
		if len(firstNodes) > 0 || len(firstEdges) > 0 {
			if err := s.emitPacked(ctx, runID, phase, firstNodes, firstEdges, limit); err != nil {
				return err
			}
		}
		return s.emitPacked(ctx, runID, phase, nodes, edges, limit)
	}
	if phase == graphstream.PhaseLocal {
		s.localNodes = append(s.localNodes, nodes...)
		s.localEdges = append(s.localEdges, edges...)
		for len(s.localNodes) >= limit || len(s.localEdges) >= limit {
			chunk := takePacked(&s.localNodes, &s.localEdges, limit)
			s.mu.Unlock()
			if err := s.emitBatch(ctx, runID, phase, chunk.nodes, chunk.edges); err != nil {
				return err
			}
			s.mu.Lock()
			if s.localErr != nil {
				err := s.localErr
				s.mu.Unlock()
				return err
			}
		}
		s.mu.Unlock()
		return nil
	}
	s.resolvNodes = append(s.resolvNodes, nodes...)
	s.resolvEdges = append(s.resolvEdges, edges...)
	s.resolvedOwners++
	for len(s.resolvNodes) >= limit || len(s.resolvEdges) >= limit {
		chunk := takePacked(&s.resolvNodes, &s.resolvEdges, limit)
		s.mu.Unlock()
		if err := s.emitBatch(ctx, runID, phase, chunk.nodes, chunk.edges); err != nil {
			return err
		}
		s.mu.Lock()
	}
	s.mu.Unlock()
	return nil
}

func (s *session) flushPhase(ctx context.Context, runID, phase string) error {
	limit := s.opts.BatchLimit
	if limit <= 0 {
		limit = 64
	}
	s.mu.Lock()
	var nodes []graphstream.Node
	var edges []graphstream.Edge
	if phase == graphstream.PhaseLocal {
		nodes, s.localNodes = s.localNodes, nil
		edges, s.localEdges = s.localEdges, nil
	} else {
		nodes, s.resolvNodes = s.resolvNodes, nil
		edges, s.resolvEdges = s.resolvEdges, nil
	}
	s.mu.Unlock()
	if len(nodes) == 0 && len(edges) == 0 {
		return nil
	}
	return s.emitPacked(ctx, runID, phase, nodes, edges, limit)
}

func (s *session) emitPacked(ctx context.Context, runID, phase string, nodes []graphstream.Node, edges []graphstream.Edge, limit int) error {
	for len(nodes) > 0 || len(edges) > 0 {
		n := limit
		if len(nodes) < n {
			n = len(nodes)
		}
		e := limit
		if len(edges) < e {
			e = len(edges)
		}
		if err := s.emitBatch(ctx, runID, phase, nodes[:n], edges[:e]); err != nil {
			return err
		}
		nodes = nodes[n:]
		edges = edges[e:]
	}
	return nil
}

func takePacked(nodes *[]graphstream.Node, edges *[]graphstream.Edge, limit int) ownerChunk {
	n := limit
	if len(*nodes) < n {
		n = len(*nodes)
	}
	e := limit
	if len(*edges) < e {
		e = len(*edges)
	}
	c := ownerChunk{
		nodes: append([]graphstream.Node(nil), (*nodes)[:n]...),
		edges: append([]graphstream.Edge(nil), (*edges)[:e]...),
	}
	*nodes = (*nodes)[n:]
	*edges = (*edges)[e:]
	return c
}

func (s *session) emitBatch(ctx context.Context, runID, phase string, nodes []graphstream.Node, edges []graphstream.Edge) error {
	if len(nodes) == 0 && len(edges) == 0 {
		return nil
	}
	seq := s.nextSeq()
	return s.batch(ctx, runID, seq, phase, nodes, edges)
}

func (s *session) publishScope(ctx context.Context, runID string, owners []graphstream.OwnerRef) error {
	if s.opts.AuthoritativeFiles {
		return s.fileLocalErr()
	}
	var extra []graphstream.OwnerRef
	s.mu.Lock()
	if s.announced == nil {
		s.announced = map[string]bool{}
	}
	for _, o := range owners {
		if s.announced[o.String()] {
			continue
		}
		s.announced[o.String()] = true
		extra = append(extra, o)
	}
	s.mu.Unlock()
	if len(extra) == 0 {
		return nil
	}
	graphstream.SortOwners(extra)
	const chunk = 200
	for i := 0; i < len(extra); i += chunk {
		j := i + chunk
		if j > len(extra) {
			j = len(extra)
		}
		seq := s.nextSeq()
		b := graphstream.Batch{Type: graphstream.TypeBatch, RunID: runID, Seq: seq, Phase: graphstream.PhaseScope, Owners: extra[i:j]}
		bp, err := graphstream.Marshal(b)
		if err != nil {
			return err
		}
		s.storePayload(seq, bp)
		if err := s.pub.Publish(ctx, graphstream.MessageID(runID, graphstream.TypeBatch, seq), bp); err != nil {
			return err
		}
	}
	return nil
}

func dedupeOwners(owners []graphstream.OwnerRef) []graphstream.OwnerRef {
	seen := map[string]bool{}
	out := owners[:0]
	for _, o := range owners {
		k := o.String()
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, o)
	}
	return out
}

func (s *session) batch(ctx context.Context, runID string, seq int, phase string, nodes []graphstream.Node, edges []graphstream.Edge) error {
	if s.opts.AuthoritativeFiles {
		if !s.began || s.plan == nil || phase != graphstream.PhaseResolved {
			return fmt.Errorf("invalid frozen batch phase or missing Begin")
		}
		for _, n := range nodes {
			if err := s.plan.check(n.Owner); err != nil {
				return err
			}
		}
		for _, e := range edges {
			if err := s.plan.check(e.Owner); err != nil {
				return err
			}
		}
	}
	msg := graphstream.Batch{Type: graphstream.TypeBatch, RunID: runID, Seq: seq, Phase: phase, Nodes: nodes, Edges: edges}
	payload, err := graphstream.Marshal(msg)
	if err != nil {
		return err
	}
	s.storePayload(seq, payload)
	return s.pub.Publish(ctx, graphstream.MessageID(runID, graphstream.TypeBatch, seq), payload)
}

func (s *session) end(ctx context.Context, runID string, batchCount int, owners []graphstream.OwnerRef, c graphstream.Completeness) error {
	if s.opts.AuthoritativeFiles {
		if err := s.fileLocalErr(); err != nil {
			return err
		}
		if s.plan == nil || graphstream.DigestOwners(owners) != s.plan.digest {
			return fmt.Errorf("frozen manifest changed before End")
		}
	}
	msg := graphstream.EndReplace{
		Type:             graphstream.TypeEndReplace,
		RunID:            runID,
		BatchCount:       batchCount,
		BatchDigest:      graphstream.DigestBatches(s.batchPayloads),
		OwnerScopeLen:    len(owners),
		OwnerScopeDigest: graphstream.DigestOwners(owners),
		Completeness:     c,
	}
	payload, err := graphstream.Marshal(msg)
	if err != nil {
		return err
	}
	return s.pub.Publish(ctx, graphstream.MessageID(runID, graphstream.TypeEndReplace, batchCount+1), payload)
}

func recMissing(st *FileState) bool {
	return st == nil || (st.Extractor == "typescript" && st.TS == nil)
}

func (s *session) frameworkDirtyRequiresFullScope(files []string, prevFiles map[string]*FileState, hashes map[string]string, angular bool) (bool, error) {
	if s.state == nil || s.state.FrameworkSig == "" {
		return false, nil
	}
	owned := tsextractor.SessionFiles(files, angular)
	dirty := map[string]bool{}
	prevRecs := map[string]*tsextractor.FileRecord{}
	for path, rec := range prevFiles {
		if rec != nil && rec.TS != nil {
			prevRecs[path] = rec.TS
		}
	}
	for _, f := range owned {
		st := lookupState(prevFiles, f)
		h, ok := lookupHash(hashes, f)
		if !ok || st == nil || st.Hash != h || st.Unreadable {
			dirty[filepath.ToSlash(f)] = true
		}
	}
	// CompositionSignature observes the current file universe. Deleted TS
	// sources are not in `owned`, but their removal can withdraw Nuxt auto-import
	// names just as surely as an edit can change one. Include those prior owners
	// in the pre-Begin dirty set so the planner sees the same framework-context
	// change the later invalidation pass sees.
	for path, st := range prevFiles {
		if st == nil || st.TS == nil {
			continue
		}
		rel := filepath.ToSlash(path)
		if !tsextractor.IsSessionSource(rel, angular) {
			continue
		}
		if _, present := lookupHash(hashes, rel); !present {
			dirty[rel] = true
		}
	}
	if !anyDirty(dirty) {
		return false, nil
	}
	captured := s.capturedSources
	if captured == nil {
		captured = map[string][]byte{}
	}
	for f, d := range dirty {
		if !d {
			continue
		}
		if _, ok := captured[f]; ok {
			continue
		}
		b, rerr := os.ReadFile(filepath.Join(s.abs, f))
		if rerr != nil {
			continue
		}
		captured[f] = b
	}
	sig, err := tsextractor.CompositionSignature(s.abs, owned, prevRecs, dirty, captured, s.eng.GraphScope())
	if err != nil {
		return false, classifyVanished(err, "composition context input for", "typescript", "refusing to plan from a partial capture")
	}
	s.frameworkSig = sig
	return s.state.FrameworkSig != sig, nil
}

// coversDirty reports whether a planning extract that parsed prepared satisfies
// a later request for want. A nil want means every owned file, which no
// dirty-file preview covers.
func coversDirty(prepared, want map[string]bool) bool {
	if want == nil {
		return false
	}
	for f, d := range want {
		if !d {
			continue
		}
		if !prepared[f] && !prepared[filepath.ToSlash(f)] {
			return false
		}
	}
	return true
}

// frozenPreview is what the pre-Begin planning extract proved. closed lists the
// files whose dependents it already resolved under the observed-surface rule, so
// the frozen plan carries them as plain members instead of reverse-closing them a
// second time. declaredAdded and declaredRemoved are the name-surface delta the
// preview observed; the cached records alone cannot show an added name. broad is
// set when the session keeps the old reachability rule and the plan must too.
type frozenPreview struct {
	closed          map[string]bool
	declaredAdded   map[string]bool
	declaredRemoved map[string]bool
	// recorded is the subset of closed the preview actually produced a record
	// for, so a file it could not read falls back to its whole cached name
	// surface instead of to a delta computed from nothing.
	recorded map[string]bool
	broad    bool
}

// prepareFrozenTS parses the dirty files before Begin and closes over the files
// their reparse proved are affected, growing dirty in place.
//
// The closure is a fixed point over observed surfaces, not over reachability: a
// hop takes every dependent of a file whose reparse published a changed
// import/export surface, every dependent of a dirty file that cannot prove its
// own cross-file reads, and every cached owner whose Referenced surface mentions
// a name the delta added or removed. Each hop passes the records it already has
// as the cached input and only the newly added files as dirty, so an unchanged
// member is parsed once for the whole preview however many hops run. Parsing
// reads source bytes and the fixed session filename context, never another
// file's record, so a file parsed in an early hop needs no reparse when a cycle
// partner changes surface later.
func (s *session) prepareFrozenTS(ctx context.Context, prevFiles map[string]*FileState, files []string, hashes map[string]string, dirty, retired map[string]bool, angular bool) (map[string][]facts.Fact, *frozenPreview, error) {
	out := map[string][]facts.Fact{}
	if len(dirty) == 0 {
		return out, nil, nil
	}
	var ts *tsextractor.TSExtractor
	for _, ext := range s.eng.Extractors() {
		if t, ok := ext.(*tsextractor.TSExtractor); ok {
			ts = t
			break
		}
	}
	if ts == nil {
		return nil, nil, fmt.Errorf("no typescript extractor for planning extract")
	}
	owned := tsextractor.SessionFiles(files, angular)
	ownedSet := make(map[string]bool, len(owned))
	for _, f := range owned {
		ownedSet[filepath.ToSlash(f)] = true
	}
	prevRecs := map[string]*tsextractor.FileRecord{}
	for path, st := range prevFiles {
		if st != nil && st.TS != nil {
			prevRecs[filepath.ToSlash(path)] = st.TS
		}
	}
	// The seed is the dirt the invalidator found; everything the hops add on top
	// of it was reached through a dependency, which is the extraction site's
	// "resolution". Snapshot it before the loop starts widening dirty.
	seed := make(map[string]bool, len(dirty))
	for f, d := range dirty {
		if d {
			seed[filepath.ToSlash(f)] = true
		}
	}
	parseReason := func(path string) string {
		id := filepath.ToSlash(path)
		prev := lookupState(prevFiles, id)
		if prev == nil || prev.TS == nil {
			return "added source"
		}
		if h, _ := lookupHash(hashes, id); prev.Hash != h {
			return "source content"
		}
		if seed[id] {
			return "file semantic context"
		}
		return "resolution"
	}
	var parseMu sync.Mutex
	var parses []preparedParse
	hooks := tsextractor.SessionHooks{
		SkipConfigPaths: true,
		Sources:         s.capturedSources,
		Discovery:       s.tsRunDiscovery(),
		OnBeforeParse: func(path string) {
			parseMu.Lock()
			parses = append(parses, preparedParse{path: path, reason: parseReason(path)})
			parseMu.Unlock()
		},
	}
	work := make(map[string]*tsextractor.FileRecord, len(prevRecs))
	for k, v := range prevRecs {
		work[k] = v
	}
	pending := make(map[string]bool, len(dirty))
	for f, d := range dirty {
		if d {
			pending[filepath.ToSlash(f)] = true
		}
	}
	var res *tsextractor.SessionResult
	var stats tsextractor.ExtractStats
	parsed := map[string]bool{}
	changed := map[string]bool{}
	parsedBefore := 0
	unreadable := map[string]bool{}
	// Owners the name delta took that have so far proven they only need their
	// cached facts republished. Held across hops rather than committed on the
	// spot: see the reconsideration below.
	rebind := map[string]bool{}
	for {
		r, err := ts.ExtractSession(ctx, s.abs, owned, work, pending, hooks)
		if err != nil {
			return nil, nil, err
		}
		s.work.TSDiscoveries += r.Stats.DiscoveryPasses
		s.keepProvenDiscovery(r.Discovery)
		stats.FilesRead += r.Stats.FilesRead
		stats.FilesParsed += r.Stats.FilesParsed
		stats.GraphQLParsed += r.Stats.GraphQLParsed
		stats.SFCParsed += r.Stats.SFCParsed
		stats.SummaryScans += r.Stats.SummaryScans
		stats.DerivedIndexes += r.Stats.DerivedIndexes
		for _, u := range r.Unreadable {
			unreadable[u] = true
		}
		res = r
		for path, rec := range r.Records {
			work[filepath.ToSlash(path)] = rec
		}
		// Only a file this hop actually reparsed can have published a new surface.
		// The verdict is kept across hops: a side-read owner reached later still
		// has to be judged against what an earlier hop observed.
		for f := range pending {
			parsed[f] = true
			if surfaceChanged(prevRecs[f], work[f]) {
				changed[f] = true
			}
		}
		next := surfaceDependents(dirty, changed, work, prevRecs, angular)
		added, removed := declaredNameDelta(prevRecs, work, retired)
		for f := range nameDependents(prevRecs, added, removed, dirty) {
			if !angular && !next[f] && rebindableConsumer(prevRecs[f], dirty) {
				rebind[f] = true
				continue
			}
			next[f] = true
		}
		for f := range sideReadDependents(dirty, provenSideReadSources(parsed, prevRecs, work), prevRecs, hashes, angular) {
			next[f] = true
		}
		// A candidate is provisional until the closure settles. Dirt a later hop
		// adds can reach an edge it held, and a later hop can take it for a
		// reason of its own, so every candidate is judged again against the dirt
		// as it now stands and is parsed like any other dependent the moment it
		// stops proving itself. Nothing has read it yet, so promoting it is just
		// an ordinary pending file.
		for f := range rebind {
			if next[f] || !rebindableConsumer(prevRecs[f], dirty) {
				delete(rebind, f)
				next[f] = true
			}
		}
		parsedBefore += r.Stats.FilesParsed
		pending = map[string]bool{}
		for f := range next {
			// Only a session source can be reparsed. A non-TS or retired dependent
			// still has to reach the plan; extraOwners and the deltas carry it.
			if !dirty[f] && ownedSet[f] {
				pending[f] = true
			}
		}
		if len(pending) == 0 {
			// Nothing further will be read, so a candidate that survived every
			// hop is proven: republished, not reparsed. The scope has to carry
			// it so the owner is rewritten, and pending never did, so nothing
			// read its bytes.
			for f := range rebind {
				dirty[f] = true
			}
			break
		}
		// The next hop is inside the same transaction as the first one. Bytes it
		// reads for the first time have to be captured, or the pre-End fence
		// cannot prove the file the plan was frozen around is the file that was
		// published.
		if s.capturedSources == nil {
			s.capturedSources = map[string][]byte{}
			hooks.Sources = s.capturedSources
		}
		for f := range pending {
			if _, ok := s.capturedSources[f]; ok {
				continue
			}
			if b, rerr := os.ReadFile(filepath.Join(s.abs, f)); rerr == nil {
				s.capturedSources[f] = b
			}
		}
		for f := range pending {
			dirty[f] = true
		}
	}
	// CachedFiles on the last hop counts every file it did not parse, including
	// the ones earlier hops parsed. Subtract those so the figure still means
	// "reused without reading source" for the preview as a whole.
	stats.CachedFiles = res.Stats.CachedFiles - (parsedBefore - res.Stats.FilesParsed)
	if stats.CachedFiles < 0 {
		stats.CachedFiles = 0
	}
	res.Stats = stats
	unread := make([]string, 0, len(unreadable))
	for u := range unreadable {
		unread = append(unread, u)
	}
	sort.Strings(unread)
	res.Unreadable = unread
	s.preparedTS = res
	s.preparedDirty = dirty
	s.preparedParses = parses
	added, removed := declaredNameDelta(prevRecs, work, retired)
	proof := &frozenPreview{
		closed:          make(map[string]bool, len(dirty)),
		declaredAdded:   added,
		declaredRemoved: removed,
		recorded:        make(map[string]bool, len(dirty)),
		broad:           angular,
	}
	for f, d := range dirty {
		if d {
			id := filepath.ToSlash(f)
			proof.closed[id] = true
			if retired[id] || work[id] != nil {
				proof.recorded[id] = true
			}
		}
	}
	for path, rec := range res.Records {
		id := filepath.ToSlash(path)
		if rec == nil || !dirty[id] && !dirty[path] {
			continue
		}
		ff := cloneTagged(rec.Facts, "")
		applyLocalIO(ff)
		out[id] = ff
	}
	return out, proof, nil
}

func tagRepo(ff []facts.Fact, repo string) {
	for i := range ff {
		if ff[i].Repo == "" {
			ff[i].Repo = repo
		}
	}
}

func analysisFingerprint(abs string, eng *engine.Engine, _ ...[]string) (string, map[string][]byte, error) {
	hash, captured, _, err := analysisFingerprintInputs(abs, eng)
	return hash, captured, err
}

func analysisFingerprintInputs(abs string, eng *engine.Engine) (string, map[string][]byte, []string, error) {
	h := sha256.New()
	h.Write([]byte(engine.ExtractorVersion()))
	h.Write([]byte{0})
	captured := map[string][]byte{}
	paths := tsextractor.ConfigInputPaths(abs, eng.GraphScope())
	if scope := eng.GraphScope(); scope != nil {
		// The admission fingerprint, not the raw identity: the configuration
		// fingerprint answers "did an analysis input move", and the tracked
		// names the raw identity also hashes are repository state that moves on
		// a pure `git add`. Hashing those here would make every index edit a
		// configuration change and defeat the no-publication path below.
		h.Write([]byte("graph-input-profile-v3/" + scope.Policy.AdmissionIdentity()))
		filtered := paths[:0]
		for _, p := range paths {
			full := p
			if !filepath.IsAbs(full) {
				full = filepath.Join(abs, p)
			}
			if scope.Allowed(full, false) {
				filtered = append(filtered, p)
			}
		}
		paths = filtered
	}
	sort.Strings(paths)
	for _, p := range paths {
		if graphinput.IsLockfile(p) {
			continue
		}
		rel := filepath.ToSlash(p)
		h.Write([]byte(rel))
		h.Write([]byte{0})
		full := p
		if !filepath.IsAbs(full) {
			full = filepath.Join(abs, p)
		}
		st, statErr := os.Stat(full)
		if statErr != nil {
			h.Write([]byte("MISSING"))
			h.Write([]byte{0})
			continue
		}
		if st.IsDir() {
			h.Write([]byte("DIR"))
			h.Write([]byte{0})
			continue
		}
		b, err := os.ReadFile(full)
		if err != nil {
			return "", captured, paths, fmt.Errorf("unreadable required config %s: %w", p, err)
		}
		captured[rel] = b
		h.Write(b)
		h.Write([]byte{0})
	}
	if eng != nil && eng.Config() != nil {
		cfg := eng.Config()
		exts := append([]string{}, cfg.Extractors...)
		sort.Strings(exts)
		h.Write([]byte(strings.Join(exts, ",")))
		h.Write([]byte{0})
		ign := append([]string{}, cfg.Ignore...)
		sort.Strings(ign)
		h.Write([]byte(strings.Join(ign, ",")))
		h.Write([]byte{0})
	}
	if eng != nil {
		var keys []string
		for _, ext := range eng.Extractors() {
			if ck, ok := ext.(plugin.ConfigKeyed); ok {
				keys = append(keys, ext.Name()+"="+ck.ConfigKey())
			}
		}
		sort.Strings(keys)
		h.Write([]byte(strings.Join(keys, ";")))
	}
	return hex.EncodeToString(h.Sum(nil)), captured, paths, nil
}

func asFatal(err error, dest **plugin.FatalError) bool {
	return errors.As(err, dest)
}

func writePending(dir, runID string, base, target int64) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	b, err := json.Marshal(map[string]any{"run_id": runID, "base": base, "target": target})
	if err != nil {
		return err
	}
	tmp := filepath.Join(dir, "pending.json.tmp")
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(dir, "pending.json"))
}

func cloneTagged(ff []facts.Fact, repo string) []facts.Fact {
	out := make([]facts.Fact, len(ff))
	for i, f := range ff {
		out[i] = f
		if f.Props != nil {
			out[i].Props = make(map[string]any, len(f.Props))
			for k, v := range f.Props {
				out[i].Props[k] = v
			}
		}
		out[i].Relations = append([]facts.Relation(nil), f.Relations...)
		if out[i].Repo == "" {
			out[i].Repo = repo
		}
	}
	return out
}

// DecodeRun walks recorded sink payloads into typed protocol values.
func DecodeRun(records []graphstream.Recorded) (begins []graphstream.BeginReplace, batches []graphstream.Batch, ends []graphstream.EndReplace, err error) {
	for _, r := range records {
		var probe struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(r.Payload, &probe); err != nil {
			return nil, nil, nil, err
		}
		switch probe.Type {
		case graphstream.TypeBeginReplace:
			var b graphstream.BeginReplace
			if err := json.Unmarshal(r.Payload, &b); err != nil {
				return nil, nil, nil, err
			}
			begins = append(begins, b)
		case graphstream.TypeBatch:
			var b graphstream.Batch
			if err := json.Unmarshal(r.Payload, &b); err != nil {
				return nil, nil, nil, err
			}
			batches = append(batches, b)
		case graphstream.TypeEndReplace:
			var e graphstream.EndReplace
			if err := json.Unmarshal(r.Payload, &e); err != nil {
				return nil, nil, nil, err
			}
			ends = append(ends, e)
		}
	}
	return begins, batches, ends, nil
}

// Engine context is separate from input selection and observed raw contents.
// Extractor registrations supply their semantic configuration keys; scope changes
// are handled by inventory/ownership reconciliation rather than a TS-wide flag.
func engineContextFingerprint(eng *engine.Engine) string {
	var keys []string
	for _, ext := range eng.Extractors() {
		if ck, ok := ext.(plugin.ConfigKeyed); ok {
			keys = append(keys, ext.Name()+"="+ck.ConfigKey())
		}
	}
	sort.Strings(keys)
	sum := sha256.Sum256([]byte("graph-engine-context-v2/" + engine.ExtractorVersion() + strings.Join(keys, ";")))
	return hex.EncodeToString(sum[:])
}

func (s *session) stateFile(path string) *FileState {
	if s.state == nil {
		return nil
	}
	return s.state.Files[path]
}
