package graphsession

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/enola-labs/enola/internal/graphinput"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/enola-labs/enola/internal/engine"
	"github.com/enola-labs/enola/internal/extractors/swiftextractor"
	"github.com/enola-labs/enola/internal/extractors/tsextractor"
	"github.com/enola-labs/enola/internal/graphstream"
	"github.com/fsnotify/fsnotify"
)

// ChangeBatch describes observed events, not an instantaneous filesystem snapshot.
// Reconcile is sticky across dropped paths; Through includes all coalesced events.
type ChangeBatch struct {
	Epoch         string
	From, Through uint64
	Covered       bool
	Paths         []string
	Reconcile     string
}

type ChangeSource interface {
	Start(context.Context) error
	Drain() ChangeBatch
	Ready() <-chan struct{}
	Close() error
}

// ChangeQueue is also a deterministic change-source implementation for cooperating
// writers/tests. Producers must register coverage before baseline analysis.
type ChangeQueue struct {
	mu           sync.Mutex
	epoch        string
	seq, drained uint64
	paths        map[string]bool
	reason       string
	limit, bytes int
	covered      bool
	ready        chan struct{}
}

func NewChangeQueue(epoch string, limit int) *ChangeQueue {
	if limit <= 0 {
		limit = 4096
	}
	return &ChangeQueue{epoch: epoch, limit: limit, paths: map[string]bool{}, ready: make(chan struct{}, 1)}
}
func (q *ChangeQueue) Start(context.Context) error {
	q.mu.Lock()
	q.covered = true
	q.mu.Unlock()
	return nil
}
func (q *ChangeQueue) Close() error {
	q.mu.Lock()
	q.covered = false
	q.mu.Unlock()
	q.Lost("change source closed")
	return nil
}
func (q *ChangeQueue) Ready() <-chan struct{} { return q.ready }
func (q *ChangeQueue) signal() {
	select {
	case q.ready <- struct{}{}:
	default:
	}
}
func (q *ChangeQueue) Add(path string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.seq++
	if q.reason == "" && !q.paths[path] {
		if len(q.paths) >= q.limit || q.bytes+len(path) > q.limit*1024 {
			q.reason = "change queue overflow"
			q.paths = map[string]bool{}
			q.bytes = 0
		} else {
			q.paths[path] = true
			q.bytes += len(path)
		}
	}
	q.signal()
}
func (q *ChangeQueue) Lost(reason string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.seq++
	q.reason = reason
	q.paths = map[string]bool{}
	q.bytes = 0
	q.signal()
}
func (q *ChangeQueue) Drain() ChangeBatch {
	q.mu.Lock()
	defer q.mu.Unlock()
	select {
	case <-q.ready:
	default:
	}
	b := ChangeBatch{Epoch: q.epoch, From: q.drained, Through: q.seq, Covered: q.covered, Reconcile: q.reason}
	for p := range q.paths {
		b.Paths = append(b.Paths, p)
	}
	sort.Strings(b.Paths)
	q.drained = q.seq
	q.paths = map[string]bool{}
	q.reason = ""
	q.bytes = 0
	return b
}

// changeToken captures every mutable field a conditional discard depends on.
// Add and Lost both bump seq under q.mu and Close drops covered before its own
// Lost reaches the queue, so an unchanged token proves the source did not move
// rather than merely that no path was added.
type changeToken struct {
	epoch        string
	drained, seq uint64
	covered      bool
	reason       string
}

// Peek builds the batch Drain would return without consuming anything: the
// ready token stays signalled, drained does not advance, and paths and reason
// survive. It exists so a batch can be proven content-identical before its
// collection window opens; the window start a real change is entitled to is
// the caller's to preserve.
func (q *ChangeQueue) Peek() (ChangeBatch, changeToken) {
	q.mu.Lock()
	defer q.mu.Unlock()
	b := ChangeBatch{Epoch: q.epoch, From: q.drained, Through: q.seq, Covered: q.covered, Reconcile: q.reason}
	for p := range q.paths {
		b.Paths = append(b.Paths, p)
	}
	sort.Strings(b.Paths)
	return b, changeToken{epoch: q.epoch, drained: q.drained, seq: q.seq, covered: q.covered, reason: q.reason}
}

// Discard consumes a peeked batch only if nothing about the source moved since
// the token was taken. Returning false leaves the queue exactly as Peek found
// it, so the ordinary timer and Drain still apply.
func (q *ChangeQueue) Discard(tok changeToken) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	if tok.reason != "" || !tok.covered {
		return false
	}
	if q.epoch != tok.epoch || q.drained != tok.drained || q.seq != tok.seq || q.covered != tok.covered || q.reason != tok.reason {
		return false
	}
	select {
	case <-q.ready:
	default:
	}
	q.drained = q.seq
	q.paths = map[string]bool{}
	q.bytes = 0
	return true
}

// FileChangeSource uses local OS notifications. Empty Drain means observed-event
// completeness only; strict disk equality still requires reconciliation. Symlinks
// and registration failures invalidate coverage rather than claiming a safe idle.
type FileChangeSource struct {
	policy            atomic.Pointer[graphinput.Policy]
	observed          atomic.Uint64
	observedMu        sync.Mutex
	observedPaths     map[string]uint64
	externalTargetsMu sync.RWMutex
	externalTargets   map[string]bool
	coveredResident   *Resident
	coveredVersion    uint64
	coverage          sync.Mutex
	external          map[string]bool
	*ChangeQueue
	root         string
	ignored      []string
	watcher      *fsnotify.Watcher
	done         chan struct{}
	once         sync.Once
	registration sync.Mutex
	uncertain    bool
}

func NewFileChangeSource(root string, ignored []string, limit int) *FileChangeSource {
	if abs, err := filepath.Abs(root); err == nil {
		root = abs
	}
	ignored = append([]string(nil), ignored...)
	for i, p := range ignored {
		if abs, err := filepath.Abs(p); err == nil {
			ignored[i] = abs
		}
	}
	return &FileChangeSource{ChangeQueue: NewChangeQueue(fmt.Sprintf("fs-%d", time.Now().UnixNano()), limit), root: root, ignored: ignored, done: make(chan struct{}), external: map[string]bool{}}
}
func (s *FileChangeSource) ignore(path string) bool {
	for _, p := range s.ignored {
		if path == p || strings.HasPrefix(path, p+string(filepath.Separator)) {
			return true
		}
	}
	return false
}
func (s *FileChangeSource) register(root string) error {
	s.registration.Lock()
	defer s.registration.Unlock()
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if s.ignore(path) || (s.policy.Load() != nil && d != nil && s.policy.Load().Classify(path, d.IsDir()).Kind == graphinput.Excluded) {
			if d != nil && d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if err != nil {
			return err
		}
		if d.Type()&os.ModeSymlink != 0 {
			s.uncertain = true
		}
		if d.IsDir() {
			return s.watcher.Add(path)
		}
		return nil
	})
}
func (s *FileChangeSource) Start(ctx context.Context) error {
	if err := requireLocalWatch(s.root); err != nil {
		return err
	}
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	s.watcher = w
	// Reader starts before traversal; each directory is watched before descent.
	go s.read(ctx)
	if err = s.register(s.root); err != nil {
		s.Close()
		return err
	}
	return s.ChangeQueue.Start(ctx)
}
func (s *FileChangeSource) read(ctx context.Context) {
	defer close(s.done)
	for {
		select {
		case <-ctx.Done():
			s.markUncertain("watch context ended")
			return
		case e, ok := <-s.watcher.Events:
			if !ok {
				s.markUncertain("watch event stream closed")
				return
			}
			s.handleEvent(e)
			s.recordObserved(e.Name)
		case _, ok := <-s.watcher.Errors:
			s.markUncertain("filesystem watcher error or overflow")
			if !ok {
				return
			}
		}
	}
}
func (s *FileChangeSource) Drain() ChangeBatch {
	b := s.ChangeQueue.Drain()
	s.registration.Lock()
	uncertain := s.uncertain
	s.registration.Unlock()
	if uncertain {
		b.Covered = false
		b.Reconcile = "symlink or incomplete watch coverage"
	}
	return b
}

// busyToken fails an optional discard closed. Registration is held across a
// whole WalkDir, and the discard is an optimisation rather than an obligation,
// so a busy registration is answered by declining immediately and leaving the
// batch to the ordinary timer and Drain rather than by waiting for the walk.
func busyToken() (ChangeBatch, changeToken) {
	b := ChangeBatch{Reconcile: "watch registration busy"}
	return b, changeToken{reason: b.Reconcile}
}

// Peek applies the same coverage gate to a non-destructive read that Drain
// applies to a destructive one: the queue alone reporting Covered is not the
// source reporting covered. Registration is taken before the queue mutex, which
// is the order r.mu already establishes through CoverSessionInputs, and every
// writer of uncertain holds registration, so the flag cannot move underneath
// the snapshot the queue hands back.
func (s *FileChangeSource) Peek() (ChangeBatch, changeToken) {
	if !s.registration.TryLock() {
		return busyToken()
	}
	defer s.registration.Unlock()
	b, tok := s.ChangeQueue.Peek()
	if s.uncertain {
		b.Covered = false
		b.Reconcile = "symlink or incomplete watch coverage"
		// The token carries the gate as well as the batch. A caller that
		// declines on the batch never reaches Discard, but Discard must not
		// depend on the caller having looked.
		tok.covered = false
		tok.reason = b.Reconcile
	}
	return b, tok
}

// Discard reads the uncertainty flag itself rather than a mirror of it, and
// holds registration across the queue commit. Because every writer of uncertain
// holds registration, coverage cannot become uncertain between the check and
// the commit; because the commit also revalidates the queue token, nothing the
// queue saw can have moved either.
func (s *FileChangeSource) Discard(tok changeToken) bool {
	if !s.registration.TryLock() {
		return false
	}
	defer s.registration.Unlock()
	if s.uncertain {
		return false
	}
	return s.ChangeQueue.Discard(tok)
}
func (s *FileChangeSource) Close() error {
	var err error
	s.once.Do(func() {
		if s.watcher != nil {
			err = s.watcher.Close()
			<-s.done
		}
		s.ChangeQueue.Close()
	})
	return err
}

// DefaultWatchEvery is the fixed change-collection window after the first event.
const DefaultWatchEvery = 5 * time.Second

// peekableSource is the optional half of ChangeSource that can be read without
// being consumed. A source that does not implement it keeps the original
// ready-then-wait-then-drain behaviour with nothing added.
type peekableSource interface {
	Peek() (ChangeBatch, changeToken)
	Discard(changeToken) bool
}

// Watch holds one resident writer and runs only in response to events. WatchEvery
// is a debounce ceiling, not a polling interval or a disk-equality barrier.
func Watch(ctx context.Context, eng *engine.Engine, repoPath string, sink graphstream.Sink, opts Options) error {
	r, err := OpenSession(ctx, eng, repoPath, sink, opts)
	if err != nil {
		return err
	}
	defer r.Close()
	ignored := append([]string{r.opts.StateDir}, opts.WatchIgnore...)
	source := NewGraphFileChangeSource(eng, r.abs, ignored, 4096)
	if err = source.Start(ctx); err != nil {
		return err
	}
	defer source.Close()
	// Do not drain after baseline: events arriving during baseline remain pending.
	batch := source.Drain()
	if !batch.Covered {
		return fmt.Errorf("watch unavailable: %s", batch.Reconcile)
	}
	batch.Reconcile = "watch bootstrap"
	apply := func(batch ChangeBatch) error {
		if _, err := r.ApplyChanges(ctx, batch); err != nil {
			if errors.Is(err, ErrInputsChanged) {
				source.Lost("captured inputs changed; reconcile retry")
				return nil
			}
			return err
		}
		return source.CoverSessionInputs(r)
	}
	if err = apply(batch); err != nil {
		return err
	}
	delay := opts.WatchEvery
	if delay <= 0 {
		delay = DefaultWatchEvery
	}
	return watchLoop(ctx, r, source, delay, apply)
}

// watchLoop waits for the source to become ready, collects for the fixed window
// and applies what it drained. A source that can be read without being consumed
// is asked first whether the batch already waiting changes anything; if it
// provably does not, the batch is acknowledged without opening a window and
// without a transaction, and if it might, the window it gets is still the one
// that started when the source became ready.
func watchLoop(ctx context.Context, r *Resident, source ChangeSource, delay time.Duration, apply func(ChangeBatch) error) error {
	prober, peekable := source.(peekableSource)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-source.Ready():
		}
		// The window belongs to the event that made the source ready, so its
		// deadline is fixed here and not after the probe below. A probe that
		// declines spends its own time inside the window rather than on top of
		// it, and a probe that succeeds opens no window at all.
		deadline := time.Now().Add(delay)
		if peekable {
			discarded, err := r.discardUnchanged(ctx, prober)
			if err != nil {
				return err
			}
			if discarded {
				continue
			}
		}
		timer := time.NewTimer(time.Until(deadline))
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
		batch := source.Drain()
		if !batch.Covered {
			return fmt.Errorf("watch coverage lost: %s; reopen to reconcile and rebuild watches", batch.Reconcile)
		}
		if err := apply(batch); err != nil {
			return err
		}
	}
}

// Newly discovered external/negative config dependencies are registered before
// another reconciliation. Events from the first baseline are never discarded.
// CoverSessionInputs validates the native coverage profile and registers known
// external and negative config/include dependencies after a successful bootstrap
// or reconciliation. Call it after each ApplyChanges before treating Drain as a
// session-covered batch. Newly registered dependencies enqueue reconciliation;
// drain/apply that batch before measuring idle. It never discards pending events.
func (s *FileChangeSource) CoverSessionInputs(r *Resident) error {
	s.coverage.Lock()
	defer s.coverage.Unlock()
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed || r.inputs == nil {
		return fmt.Errorf("bootstrap the resident before covering its inputs")
	}
	if s.root != r.abs {
		return fmt.Errorf("watch root does not match resident checkout")
	}
	s.ChangeQueue.mu.Lock()
	covered := s.ChangeQueue.covered
	s.ChangeQueue.mu.Unlock()
	if !covered || s.watcher == nil {
		return fmt.Errorf("start the change source before covering session inputs")
	}

	if s.coveredResident == r && s.coveredVersion == r.inputs.coverageVersion {
		return nil
	}
	for _, ext := range r.eng.Extractors() {
		if !r.eng.Config().IsExtractorEnabled(ext.Name()) {
			continue
		}
		switch ext.(type) {
		case *tsextractor.TSExtractor, *swiftextractor.SwiftExtractor:
			continue
		}
		if !r.eng.ObservedRepositoryCoverage(ext, r.inputs.detected[ext.Name()]) {
			return fmt.Errorf("watch dependency coverage is unaudited for %s; use strict Run reconciliation or a caller-supplied covered ChangeSource", ext.Name())
		}
	}

	policyChanged := false
	if scope := r.eng.GraphScope(); scope != nil {
		old := s.policy.Load()
		// Watch coverage follows the admitted tree, so it is the admission
		// fingerprint that has to move before the registration is rebuilt and
		// the queue is declared lost. A pure index edit produces a different
		// raw identity and the same admitted tree, and dropping coverage for it
		// would turn `git add` of an already-watched file into a reconciliation.
		policyChanged = old == nil || old.AdmissionIdentity() != scope.Policy.AdmissionIdentity()
		s.policy.Store(scope.Policy)
		if policyChanged {
			if err := s.register(s.root); err != nil {
				return err
			}
			s.Lost("graph policy watch coverage refreshed")
		}
	}
	watched := map[string]bool{}
	for _, p := range s.watcher.WatchList() {
		watched[p] = true
	}
	paths := append([]string{}, r.inputs.configPaths...)
	if scope := r.eng.GraphScope(); scope != nil {
		for _, dep := range scope.Policy.Dependencies() {
			paths = append(paths, dep.Path)
		}
	}
	for p := range r.inputs.effective {
		paths = append(paths, p)
	}
	targets := map[string]bool{}
	for _, p := range paths {
		if !filepath.IsAbs(p) {
			p = filepath.Join(r.abs, p)
		}
		targets[strings.ToLower(filepath.Clean(p))] = true
	}
	s.externalTargetsMu.Lock()
	s.externalTargets = targets
	s.externalTargetsMu.Unlock()
	for _, p := range paths {
		if !filepath.IsAbs(p) {
			p = filepath.Join(r.abs, p)
		}
		if watched[filepath.Dir(p)] {
			continue
		}
		common := s.root
		for common != filepath.Dir(common) && p != common && !strings.HasPrefix(p, common+string(filepath.Separator)) {
			common = filepath.Dir(common)
		}
		for ancestor := p; ancestor != common; ancestor = filepath.Dir(ancestor) {
			if st, err := os.Lstat(ancestor); err == nil && st.Mode()&os.ModeSymlink != 0 {
				return fmt.Errorf("config ancestor symlink has uncertain coverage: %s", ancestor)
			}
			if filepath.Dir(ancestor) == ancestor {
				break
			}
		}
		parent := filepath.Dir(p)
		for {
			st, err := os.Stat(parent)
			if err == nil && st.IsDir() {
				break
			}
			next := filepath.Dir(parent)
			if next == parent {
				return fmt.Errorf("cannot cover external config %s", p)
			}
			parent = next
		}
		if st, err := os.Lstat(p); err == nil && st.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("external config symlink has uncertain coverage: %s", p)
		}
		if err := requireLocalWatch(parent); err != nil {
			return err
		}
		if s.external[parent] && watched[parent] {
			continue
		}
		if err := s.watcher.Add(parent); err != nil {
			return fmt.Errorf("cover external config: %w", err)
		}
		s.external[parent] = true
		s.Lost("external config watch registered; reconcile registration gap")
	}
	if policyChanged {
		needed := map[string]bool{}
		for p := range targets {
			needed[filepath.Dir(p)] = true
		}
		for _, dir := range s.watcher.WatchList() {
			if (dir == s.root || strings.HasPrefix(dir, s.root+string(filepath.Separator))) && s.policy.Load().Classify(dir, true).Kind == graphinput.Excluded && !needed[strings.ToLower(dir)] {
				if err := s.watcher.Remove(dir); err != nil && !errors.Is(err, fsnotify.ErrNonExistentWatch) {
					return err
				}
			}
		}
	}
	s.coveredResident = r
	s.coveredVersion = r.inputs.coverageVersion
	return nil
}

func (s *FileChangeSource) markUncertain(reason string) {
	s.registration.Lock()
	s.uncertain = true
	s.registration.Unlock()
	s.Lost(reason)
}

func (s *FileChangeSource) relevantEvent(e fsnotify.Event) bool {
	if e.Name == s.root || strings.HasPrefix(e.Name, s.root+string(filepath.Separator)) {
		return true
	}
	p := strings.ToLower(filepath.Clean(e.Name))
	s.externalTargetsMu.RLock()
	defer s.externalTargetsMu.RUnlock()
	if s.externalTargets[p] {
		return true
	}
	if e.Op&(fsnotify.Create|fsnotify.Remove|fsnotify.Rename|fsnotify.Chmod) == 0 {
		return false
	}
	for target := range s.externalTargets {
		if strings.HasPrefix(target, p+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

func (s *FileChangeSource) explicitTarget(path string) bool {
	s.externalTargetsMu.RLock()
	defer s.externalTargetsMu.RUnlock()
	return s.externalTargets[strings.ToLower(filepath.Clean(path))]
}

// ObservedEvents counts native deliveries, including events filtered before the graph queue.
func (s *FileChangeSource) ObservedEvents() uint64 { return s.observed.Load() }

// ObservedPath returns the most recent native event sequence for a path, or zero
// when absent from the bounded diagnostic cache. It is not a coverage barrier.
func (s *FileChangeSource) ObservedPath(path string) uint64 {
	s.observedMu.Lock()
	defer s.observedMu.Unlock()
	return s.observedPaths[filepath.Clean(path)]
}
func (s *FileChangeSource) recordObserved(path string) {
	n := s.observed.Add(1)
	s.observedMu.Lock()
	defer s.observedMu.Unlock()
	if len(s.observedPaths) >= 1024 {
		s.observedPaths = nil
	}
	if s.observedPaths == nil {
		s.observedPaths = map[string]uint64{}
	}
	s.observedPaths[filepath.Clean(path)] = n
}

// NewGraphFileChangeSource installs the engine policy before registration, so
// excluded directories are never recursively watched during bootstrap.
func NewGraphFileChangeSource(eng *engine.Engine, root string, ignored []string, limit int) *FileChangeSource {
	s := NewFileChangeSource(root, ignored, limit)
	if scope := eng.GraphScope(); scope != nil {
		s.policy.Store(scope.Policy)
	}
	return s
}

func (s *FileChangeSource) handleEvent(e fsnotify.Event) {
	if s.ignore(e.Name) || !s.relevantEvent(e) {
		return
	}
	if policy := s.policy.Load(); policy != nil {
		event := graphinput.Content
		if e.Op&(fsnotify.Create|fsnotify.Rename|fsnotify.Remove) != 0 {
			event = graphinput.Membership
		}
		action := policy.ClassifyEvent(e.Name, false, event)
		if action == graphinput.Reconcile && e.Op == fsnotify.Chmod && unchangedControlBytes(policy, e.Name) {
			return
		}
		// Explicit external extraction inputs are covered separately.
		if action == graphinput.Ignore && !s.explicitTarget(e.Name) {
			return
		}
		if action == graphinput.Reconcile {
			s.Lost("graph input policy or membership changed")
		}
	}
	if e.Op&(fsnotify.Create|fsnotify.Rename|fsnotify.Remove) != 0 {
		if st, err := os.Lstat(e.Name); err == nil && st.Mode()&os.ModeSymlink != 0 {
			s.markUncertain("new symlink has uncertain target coverage")
		}
		if st, err := os.Stat(e.Name); err == nil && st.IsDir() {
			if err = s.register(e.Name); err != nil {
				s.registration.Lock()
				s.uncertain = true
				s.registration.Unlock()
				s.Lost("watch registration failed")
			}
		}
		s.Lost("filesystem name change requires reconciliation")
	} else {
		s.Add(e.Name)
	}
}

// Pure metadata notifications can be produced by Git itself during reconciliation.
// Only a captured control with identical readable bytes can be discarded; writes,
// replacements, removals and failed reads retain conservative reconciliation.
func unchangedControlBytes(policy *graphinput.Policy, path string) bool {
	name := filepath.Base(path)
	if name != "index" && name != "HEAD" && name != "config" {
		return false
	}
	for _, dep := range policy.Dependencies() {
		if filepath.Clean(dep.Path) != filepath.Clean(path) {
			continue
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return false
		}
		sum := sha256.Sum256(b)
		return hex.EncodeToString(sum[:]) == dep.Digest
	}
	return false
}
