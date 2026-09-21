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
	tr := graphprofile.Start()
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
	return &Resident{eng: eng, abs: abs, opts: opts, sink: sink, state: st, journal: journal, lock: lock}, nil
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
}

func (s *session) analyze(ctx context.Context) (*Result, error) {
	return s.run(ctx, true)
}

func (s *session) delta(ctx context.Context) (*Result, error) {
	return s.run(ctx, false)
}

func (s *session) run(ctx context.Context, initial bool) (*Result, error) {
	tr := s.prof
	var err error
	input := s.inputs
	if !s.fast {
		input, err = readRuntimeInputs(s.eng, s.abs, s.state, &s.work)
		if err != nil {
			return nil, err
		}
	}
	s.inputs = input
	inv, detectedExt, hashes := input.inventory, input.detected, input.hashes
	contextInputs, cfgHash, capturedCfg := input.contexts, input.configHash, input.config
	scanHash := inventoryDigest(inv.AllNames, hashes)
	if s.opts.AuthoritativeFiles {
		scanHash = inventoryDigest(graphSemanticNames(s.eng, inv.AllNames), hashes)
	}
	prevScan := ""
	if s.state != nil {
		prevScan = s.state.ScanHash
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
		invalidation.PolicyReconciled = s.state.PolicyIdentity != input.policyIdentity
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
	if s.state != nil {
		for k, v := range s.state.Files {
			prevFiles[k] = cloneFileState(v)
		}
	}
	tr.Mark("clone_file_state", fmt.Sprintf("n=%d", len(prevFiles)))
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
	tr.Mark("extractor_need", fmt.Sprintf("non_ts_need=%v detected=%d", nonTSNeed, len(detectedExt)))
	if s.opts.AuthoritativeFiles {
		// The name-based graph resolver has no proven isolated domain. Announce
		// the full prior/current file union, while retaining incremental parsing.
		// Empty initial and last-file deletion still Begin before extraction so
		// the frozen manifest is immutable for the run.
		changed := initial || forceAll || nonTSNeed || s.state == nil || s.state.ScanHash != scanHash || s.state.ConfigHash != cfgHash || s.state.PolicyIdentity != input.policyIdentity
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
			seeds := append(append([]string{}, previous...), current...)
			s.plan, err = planFileInvalidation(seeds, previous, current, nil, true, nil)
			if err != nil {
				return nil, err
			}
			s.replaceScope = s.plan.manifest()
			s.scopeLimited = true
			fallbacks = append(fallbacks, graphstream.Fallback{Extractor: "graph", Scope: "all prior/current file owners", Reason: "frozen scope: global name-resolution domain"})
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
				for _, f := range owned {
					h, ok := lookupHash(hashes, f)
					prev := lookupState(prevFiles, f)
					if !ok || prev == nil || prev.Hash != h || prev.Unreadable || recMissing(prev) {
						dirty[f] = true
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
						return nil, fmt.Errorf("unreadable required input while computing composition context: %w", sigErr)
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
			res, err := ts.ExtractSession(ctx, s.abs, owned, prevRecs, dirtyArg, hooks)
			if err != nil {
				return nil, fmt.Errorf("typescript extract: %w", err)
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
			if !forceAll {
				changed := map[string]bool{}
				for path, rec := range res.Records {
					if surfaceChanged(prevRecs[path], rec) {
						changed[filepath.ToSlash(path)] = true
					}
				}
				newNames := map[string]bool{}
				removedNames := map[string]bool{}
				for path, rec := range res.Records {
					old := prevRecs[path]
					if rec == nil {
						continue
					}
					have := map[string]bool{}
					if old != nil {
						for _, n := range old.Declared {
							have[n] = true
						}
					}
					now := map[string]bool{}
					for _, n := range rec.Declared {
						now[n] = true
						if !have[n] {
							newNames[n] = true
						}
					}
					if old != nil {
						for _, n := range old.Declared {
							if !now[n] {
								removedNames[n] = true
							}
						}
					}
				}
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
					for _, n := range old.Declared {
						removedNames[n] = true
					}
				}
				extra := reverseClose(changed, workRecs)
				for path, rec := range prevRecs {
					if rec == nil || extra[filepath.ToSlash(path)] {
						continue
					}
					for _, n := range rec.Referenced {
						if newNames[n] || removedNames[n] {
							extra[filepath.ToSlash(path)] = true
							break
						}
					}
				}
				need := map[string]bool{}
				for p, d := range extra {
					if !d {
						continue
					}
					if dirtyArg[p] || dirtyArg[filepath.ToSlash(p)] {
						continue
					}
					need[p] = true
					dirty[p] = true
				}
				if len(need) > 0 {
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
					workRecs = more.Records
				}
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
			var oldFacts []facts.Fact
			if s.state != nil {
				for _, st := range s.state.Files {
					oldFacts = append(oldFacts, fileFacts(st)...)
					if st != nil && st.Contrib != nil {
						for _, ff := range st.Contrib {
							oldFacts = append(oldFacts, ff...)
						}
					}
				}
			}
			addChangedRouteFiles(scopeFiles, oldFacts, res.Facts)
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
			if err := s.publishScope(ctx, runID, s.replaceScope); err != nil {
				return nil, err
			}
			unreadable = append(unreadable, res.Unreadable...)
			if s.frameworkSig == "" {
				if sig, sigErr := tsextractor.CompositionSignature(s.abs, owned, res.Records, nil, s.capturedSources, s.eng.GraphScope()); sigErr == nil {
					s.frameworkSig = sig
				}
			}
		default:
			need := needByExt[ext.Name()]
			if !need && !s.fast {
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
				continue
			}
			fallbacks = append(fallbacks, graphstream.Fallback{
				Extractor: ext.Name(),
				Scope:     "all files owned by extractor",
				Reason:    "no per-file incremental session; whole-extractor re-run",
			})
			tExt := time.Now()
			extracted, err := ext.Extract(ctx, s.abs, inv.Files)
			graphprofile.Since("non_ts_extract", tExt, fmt.Sprintf("%s facts=%d", ext.Name(), len(extracted)))
			if err != nil {
				var fatal *plugin.FatalError
				if asFatal(err, &fatal) {
					return nil, err
				}
				log.Printf("[graphsession] extractor %s: %v", ext.Name(), err)
				continue
			}
			applyLocalIO(extracted)
			tagRepo(extracted, repoID)
			extractorInput[ext.Name()] = extractorInputDigest(ext, owned, inv.Files, inv.AllNames, hashes, fileSetHash, scanHash)
			tFP := time.Now()
			fp := factsFingerprint(extracted)
			graphprofile.Since("non_ts_fingerprint", tFP, ext.Name())
			if !nonTSForceAll && !nonTSConfigChanged && extractorDigest[ext.Name()] != "" && extractorDigest[ext.Name()] == fp {
				cached := cachedFactsFor(ext.Name(), owned, prevFiles)
				allFacts = append(allFacts, cached...)
				syn := cloneTagged(syntheticFactsFor(s.state, ext.Name()), repoID)
				allFacts = append(allFacts, syn...)
				appendExtractorSynthetic(synByExt, ext.Name(), syn)
				needByExt[ext.Name()] = false
				continue
			}
			// Replacement must retire prior synthetic owners too (for example a
			// Swift target whose include changes its module identity).
			nonTSFileOwners = append(nonTSFileOwners, retireExtractorOwners(s.state, prevFiles, ext.Name())...)
			extractorDigest[ext.Name()] = fp
			allFacts = append(allFacts, extracted...)
			appendExtractorSynthetic(synByExt, ext.Name(), extracted)
			byFile := map[string][]facts.Fact{}
			for _, f := range extracted {
				byFile[filepath.ToSlash(f.File)] = append(byFile[filepath.ToSlash(f.File)], f)
				nonTSFileOwners = append(nonTSFileOwners, ownerOf(f))
			}
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
			if s.plan == nil {
				return nil, fmt.Errorf("missing frozen invalidation plan")
			}
			if err := s.plan.check(o); err != nil {
				return nil, err
			}
			kept = append(kept, f)
		}
		allFacts = kept
	}
	idx := buildIndex(allFacts)
	s.inputs.resolution = idx
	grouped := groupOwners(allFacts)
	if s.state != nil && s.scopeLimited {
		old := s.priorResolution
		if old == nil {
			old = stateResolutionIndex(s.state, repoID)
		}
		s.growScope(changedResolutionOwners(grouped, old, idx))
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
	} else if err := s.publishScope(ctx, runID, s.replaceScope); err != nil {
		return nil, err
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
	next.FrameworkSig = s.frameworkSig
	next.SinkID = s.opts.SinkID
	next.Files = newFiles
	next.Synthetic = syn
	next.ScanHash = scanHash
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
			return nil, fmt.Errorf("source %s unreadable before EndReplace: %w", path, rerr)
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
	for rel, src := range s.capturedSources {
		s.work.CapturedReads++
		disk, rerr := os.ReadFile(filepath.Join(s.abs, rel))
		if rerr != nil || !bytes.Equal(disk, src) {
			return nil, fmt.Errorf("%w: source/config bytes changed during the run (%s); refusing successful EndReplace", ErrInputsChanged, rel)
		}
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
	if err := promotePendingState(s.opts.StateDir); err != nil {
		return nil, err
	}
	if journal != nil {
		_ = journal.CompactAcked()
	}
	_ = os.Remove(filepath.Join(s.opts.StateDir, "pending.json"))
	s.state = next
	tr.Mark("end_flush_promote", fmt.Sprintf("parsed=%d published=%d", stats.FilesParsed, published))

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
		h.Write([]byte("graph-input-profile-v2/" + scope.Policy.Identity()))
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
