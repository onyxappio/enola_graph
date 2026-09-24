package graphsession

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/enola-labs/enola/internal/engine"
	"github.com/enola-labs/enola/internal/extractors/tsextractor"
	"github.com/enola-labs/enola/internal/graphstream"
)

// A refused attempt has already read most of a tree that mostly did not move.
// These guard the retry that follows it: that it reuses what it may, that what
// it reuses is proven rather than assumed, and above all that reusing changes
// the cost and not the graph.

func retryFixture(t *testing.T, n int) (string, map[string]string) {
	t.Helper()
	files := map[string]string{"src/entry.ts": "export const e=0;\n"}
	for i := 0; i < n; i++ {
		files[fmt.Sprintf("src/pkg%02d/m%03d.ts", i%20, i)] = fmt.Sprintf("export const x%d=0;\n", i)
	}
	return setupTSRepo(t, files), files
}

// The retry after a refusal reparses the file that moved, not the tree the
// refused attempt had already read, and the graph it commits is the graph a
// cold run produces.
func TestRetryReusesUnmovedParsesAndStaysColdEqual(t *testing.T) {
	root, _ := retryFixture(t, 300)
	eng := testEngine(t, root)
	ctx := context.Background()
	opts := Options{StateDir: t.TempDir(), AuthoritativeFiles: true}
	sink := &graphstream.MemorySink{}
	res, err := OpenSession(ctx, eng, root, sink, opts)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Close()
	if _, err := res.reconcile(ctx, false); err != nil {
		t.Fatal(err)
	}

	changed := make([]string, 0, 50)
	for i := 0; i < 50; i++ {
		p := fmt.Sprintf("src/pkg%02d/m%03d.ts", i%20, i)
		writeFile(t, root, p, fmt.Sprintf("export const x%d=1;\n", i))
		changed = append(changed, p)
	}
	res.mu.Lock()
	input, reason := res.contentInputs(changed, &WorkCounters{})
	res.mu.Unlock()
	if reason != "" || input == nil {
		t.Fatalf("capture failed: %s", reason)
	}
	// One of the captured files moves again, superseding the attempt.
	writeFile(t, root, changed[0], "export const x0=2;\n")
	res.mu.Lock()
	_, _, err = res.transaction(ctx, input, true)
	res.mu.Unlock()
	if !errors.Is(err, ErrInputsChanged) {
		t.Fatalf("want refusal, got %v", err)
	}
	if len(res.retryRecords) == 0 {
		t.Fatal("the refused attempt left nothing for the retry")
	}

	out, work, err := func() (*Result, WorkCounters, error) {
		res.mu.Lock()
		defer res.mu.Unlock()
		return res.transaction(ctx, nil, false)
	}()
	if err != nil {
		t.Fatal(err)
	}
	if work.RetryParsesReused == 0 {
		t.Fatal("the retry reused none of the refused attempt's parses")
	}
	if work.RetryParsesReused > len(changed) {
		t.Fatalf("retry reused %d parses, more than the %d files the attempt captured", work.RetryParsesReused, len(changed))
	}
	if out.OwnersPublished != len(changed) {
		t.Fatalf("retry published %d owners, want %d - reuse must not hide the delta", out.OwnersPublished, len(changed))
	}
	cons := NewConsumer()
	applyRun(t, cons, sink)
	assertAppliedEqualsCold(t, cons, coldConsumer(t, eng, root))
	if res.retryRecords != nil {
		t.Fatal("a committed run must not leave a retry offer behind")
	}
	t.Logf("retry reused %d of %d captured parses", work.RetryParsesReused, len(changed))
}

// The offer is proven, not trusted: a record whose file moved again between the
// refusal and the retry is not reused, and one left by a failure that is not a
// moved input is not kept at all.
func TestRetryOfferIsProvenPerFile(t *testing.T) {
	root, _ := retryFixture(t, 40)
	eng := testEngine(t, root)
	ctx := context.Background()
	opts := Options{StateDir: t.TempDir(), AuthoritativeFiles: true}
	res, err := OpenSession(ctx, eng, root, &graphstream.MemorySink{}, opts)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Close()
	if _, err := res.reconcile(ctx, false); err != nil {
		t.Fatal(err)
	}
	changed := []string{"src/pkg00/m000.ts", "src/pkg01/m001.ts", "src/pkg02/m002.ts"}
	for i, p := range changed {
		writeFile(t, root, p, fmt.Sprintf("export const x%d=1;\n", i))
	}
	res.mu.Lock()
	input, reason := res.contentInputs(changed, &WorkCounters{})
	res.mu.Unlock()
	if reason != "" {
		t.Fatalf("capture failed: %s", reason)
	}
	writeFile(t, root, changed[0], "export const x0=9;\n")
	res.mu.Lock()
	_, _, err = res.transaction(ctx, input, true)
	res.mu.Unlock()
	if !errors.Is(err, ErrInputsChanged) {
		t.Fatalf("want refusal, got %v", err)
	}
	offered := res.retryRecords
	if len(offered) == 0 {
		t.Fatal("no offer to prove")
	}

	// Every offered file moves again before the retry: none may be reused.
	for i, p := range changed {
		writeFile(t, root, p, fmt.Sprintf("export const x%d=7;\n", i))
	}
	here := res.retryFor
	ctxMap, baseMap := res.retryFileContext, res.retryFileBase
	matching := &runtimeInputs{policyIdentity: here.policy, admissionIdentity: here.admission,
		engineContextHash: here.engine, configHash: here.config, tsFileContext: ctxMap, tsFileBase: baseMap}

	// Every offered file moved again before the retry: none may be reused.
	res.mu.Lock()
	sameRun := &session{retryRecords: offered, retryFor: here, retryFileContext: ctxMap, retryFileBase: baseMap}
	reusable := sameRun.reusableRetryRecords(matching, currentHashes(t, root, changed), pendingSet(changed))
	res.mu.Unlock()
	if len(reusable) != 0 {
		t.Fatalf("reused %d record(s) whose bytes had moved again", len(reusable))
	}

	// The remaining legs are asked with the bytes deliberately in agreement, so
	// that the only thing left to decline a record is the leg under test. A real
	// tree cannot hold two of these apart: by the time an alias declaration has
	// moved, the files it renamed have usually moved too, and a record declined
	// twice proves neither guard.
	const path = "src/dep.ts"
	rec := func() *tsextractor.FileRecord {
		return &tsextractor.FileRecord{Hash: "h", SideReads: []string{"src/side.ts"},
			SideReadHashes: map[string]string{"src/side.ts": "old"}}
	}
	agree := map[string]string{path: "h", "src/side.ts": "old"}
	pend := map[string]bool{path: true}
	offerWith := func(id retryIdentity, fileCtx, fileBase map[string]string, in *runtimeInputs, hashes map[string]string) map[string]*tsextractor.FileRecord {
		s := &session{retryRecords: map[string]*tsextractor.FileRecord{path: rec()},
			retryFor: id, retryFileContext: fileCtx, retryFileBase: fileBase}
		return s.reusableRetryRecords(in, hashes, pend)
	}
	live := &runtimeInputs{policyIdentity: "p", admissionIdentity: "a", engineContextHash: "e", configHash: "c",
		tsContext:     map[string]string{"paths:@x/*": "src/a/*"},
		inventory:     engine.RepoInventory{AllNames: []string{path, "src/side.ts"}},
		tsFileContext: map[string]string{path: "fc"}, tsFileBase: map[string]string{path: "fb"}}
	fileCtx, fileBase := map[string]string{path: "fc"}, map[string]string{path: "fb"}
	base := retryIdentityFor(live)
	// The control: everything agrees and the record is taken.
	if got := offerWith(base, fileCtx, fileBase, live, agree); len(got) != 1 {
		t.Fatalf("declined a record with every leg in agreement: %d", len(got))
	}

	// Each leg of the run-wide identity withdraws the whole offer on its own.
	for name, broken := range map[string]retryIdentity{
		"policy":    {policy: "other", admission: "a", engine: "e", config: "c", tsContext: base.tsContext, names: base.names},
		"admission": {policy: "p", admission: "other", engine: "e", config: "c", tsContext: base.tsContext, names: base.names},
		"engine":    {policy: "p", admission: "a", engine: "other", config: "c", tsContext: base.tsContext, names: base.names},
		"config":    {policy: "p", admission: "a", engine: "e", config: "other", tsContext: base.tsContext, names: base.names},
		"tsContext": {policy: "p", admission: "a", engine: "e", config: "c", tsContext: "other", names: base.names},
		"names":     {policy: "p", admission: "a", engine: "e", config: "c", tsContext: base.tsContext, names: "other"},
	} {
		if got := offerWith(broken, fileCtx, fileBase, live, agree); len(got) != 0 {
			t.Fatalf("reused %d record(s) across a different %s identity", len(got), name)
		}
	}

	// A file this run projects differently is not the file the refused attempt
	// parsed, whatever its bytes say. Per file, because an alias root moving
	// under one file is not an argument about the next.
	if got := offerWith(base, map[string]string{path: "moved"}, fileBase, live, agree); len(got) != 0 {
		t.Fatal("reused a record whose per-file context key moved")
	}
	if got := offerWith(base, fileCtx, map[string]string{path: "moved"}, live, agree); len(got) != 0 {
		t.Fatal("reused a record whose per-file alias root moved")
	}

	// A side read that moved withdraws the record, and so does one this run
	// cannot see. The extractor's own reuse test asks whether a side read still
	// exists, not whether it still says the same thing, so this leg is the only
	// thing standing between a moved re-export chain and a stale contribution.
	if got := offerWith(base, fileCtx, fileBase, live, map[string]string{path: "h", "src/side.ts": "moved"}); len(got) != 0 {
		t.Fatal("reused a record whose side read had moved")
	}
	if got := offerWith(base, fileCtx, fileBase, live, map[string]string{path: "h"}); len(got) != 0 {
		t.Fatal("reused a record whose side read this run cannot see")
	}
}

// Root's review case, end to end: a refused attempt parses a named re-export
// chain, then the middle of that chain moves its bytes before the retry while
// the importer's own bytes and every file in the chain stay exactly where they
// were. The extractor's reuse test cannot see this - it asks whether a side read
// exists - and the hop that pulls the importer back into pending reasons from
// the committed record's hashes, not from the offer's. Only the offer's own
// side-read comparison declines it, and the graph is the proof.
func TestRetryDeclinesRecordWhoseSideReadMoved(t *testing.T) {
	root := setupTSRepo(t, map[string]string{
		"src/base1.ts": "export const n=1;\n",
		"src/base2.ts": "export const q=1;\n",
		"src/mid.ts":   "export * from './base1';\nexport * from './base2';\n",
		"src/use.ts":   "import {n} from './mid'; export const u=n;\n",
		"src/other.ts": "export const o=1;\n",
	})
	eng := testEngine(t, root)
	ctx := context.Background()
	opts := Options{StateDir: t.TempDir(), AuthoritativeFiles: true}
	sink := &graphstream.MemorySink{}
	res, err := OpenSession(ctx, eng, root, sink, opts)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Close()
	if _, err := res.reconcile(ctx, false); err != nil {
		t.Fatal(err)
	}

	writeFile(t, root, "src/use.ts", "import {n} from './mid'; export const u=n+1;\n")
	writeFile(t, root, "src/other.ts", "export const o=2;\n")
	changed := []string{"src/use.ts", "src/other.ts"}
	res.mu.Lock()
	input, reason := res.contentInputs(changed, &WorkCounters{})
	res.mu.Unlock()
	if reason != "" || input == nil {
		t.Fatalf("capture failed: %s", reason)
	}
	writeFile(t, root, "src/other.ts", "export const o=3;\n")
	res.mu.Lock()
	_, _, err = res.transaction(ctx, input, true)
	res.mu.Unlock()
	if !errors.Is(err, ErrInputsChanged) {
		t.Fatalf("want refusal, got %v", err)
	}
	if res.retryRecords["src/use.ts"] == nil {
		t.Fatal("the refused attempt did not offer the importer; the case is not set up")
	}
	if len(res.retryRecords["src/use.ts"].SideReadHashes) == 0 {
		t.Fatal("the importer recorded no side read; the re-export chain did not bind through one")
	}

	// The name the importer binds is declared by the other leg of the chain now.
	// Every file that was there is still there, the middle of the chain is
	// untouched, and so is the importer - only the two files it read through the
	// chain moved, so only their hashes can say that its record is stale.
	writeFile(t, root, "src/base1.ts", "export const z=1;\n")
	writeFile(t, root, "src/base2.ts", "export const q=1;\nexport const n=1;\n")
	res.mu.Lock()
	_, _, err = res.transaction(ctx, nil, false)
	res.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	cons := NewConsumer()
	applyRun(t, cons, sink)
	assertAppliedEqualsCold(t, cons, coldConsumer(t, eng, root))
}

// The same shape for resolution rather than content: the alias the importer
// resolves through points at a different existing file by the time the retry
// runs. Nothing the importer reads has moved, so only the run-wide identity
// withdraws the offer.
func TestRetryDeclinesOfferWhenAliasTargetMoved(t *testing.T) {
	root := setupTSRepo(t, map[string]string{
		"tsconfig.json": `{"compilerOptions":{"baseUrl":".","paths":{"@x/*":["src/a/*"]}}}`,
		"src/a/t.ts":    "export const t=1;\n",
		"src/b/t.ts":    "export const t=2;\n",
		"src/use.ts":    "import {t} from '@x/t'; export const u=t;\n",
		"src/other.ts":  "export const o=1;\n",
	})
	eng := testEngine(t, root)
	ctx := context.Background()
	opts := Options{StateDir: t.TempDir(), AuthoritativeFiles: true}
	sink := &graphstream.MemorySink{}
	res, err := OpenSession(ctx, eng, root, sink, opts)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Close()
	if _, err := res.reconcile(ctx, false); err != nil {
		t.Fatal(err)
	}

	writeFile(t, root, "src/use.ts", "import {t} from '@x/t'; export const u=t+1;\n")
	writeFile(t, root, "src/other.ts", "export const o=2;\n")
	res.mu.Lock()
	input, reason := res.contentInputs([]string{"src/use.ts", "src/other.ts"}, &WorkCounters{})
	res.mu.Unlock()
	if reason != "" || input == nil {
		t.Fatalf("capture failed: %s", reason)
	}
	writeFile(t, root, "src/other.ts", "export const o=3;\n")
	res.mu.Lock()
	_, _, err = res.transaction(ctx, input, true)
	res.mu.Unlock()
	if !errors.Is(err, ErrInputsChanged) {
		t.Fatalf("want refusal, got %v", err)
	}
	if res.retryRecords["src/use.ts"] == nil {
		t.Fatal("the refused attempt did not offer the importer; the case is not set up")
	}

	writeFile(t, root, "tsconfig.json", `{"compilerOptions":{"baseUrl":".","paths":{"@x/*":["src/b/*"]}}}`)
	res.mu.Lock()
	_, work, err := res.transaction(ctx, nil, false)
	res.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if work.RetryParsesReused != 0 {
		t.Fatalf("reused %d parse(s) after the alias declarations moved", work.RetryParsesReused)
	}
	cons := NewConsumer()
	applyRun(t, cons, sink)
	assertAppliedEqualsCold(t, cons, coldConsumer(t, eng, root))
}

func currentHashes(t *testing.T, root string, paths []string) map[string]string {
	t.Helper()
	out := make(map[string]string, len(paths))
	for _, p := range paths {
		b, err := os.ReadFile(filepath.Join(root, p))
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(b)
		out[p] = hex.EncodeToString(sum[:])
	}
	return out
}

func pendingSet(paths []string) map[string]bool {
	out := make(map[string]bool, len(paths))
	for _, p := range paths {
		out[p] = true
	}
	return out
}
