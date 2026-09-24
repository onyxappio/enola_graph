package graphsession

import (
	"context"
	"strings"
	"testing"

	"github.com/enola-labs/enola/internal/facts"
)

// An extractor that owns nothing yet has to be told apart from one that owns
// nothing AND declares what it reads: only the second can answer "did anything
// I read move?" without falling back to the scan digest, which moves for every
// source in the repository. Deferring to the declared comparison is what stops
// an empty Python domain from buying a whole-domain Begin with a TypeScript
// file it cannot see, and keeping the fallback for everyone else is what stops
// that deferral from silently skipping an extractor that never said what it
// reads. Both halves are invisible in a suite that only checks graph contents:
// one shows up as work nobody asked for, the other as work nobody did.

type emptyDomainStub struct {
	name   string
	suffix string
	runs   *int
}

func (e emptyDomainStub) Name() string                { return e.name }
func (e emptyDomainStub) Detect(string) (bool, error) { return true, nil }
func (e emptyDomainStub) OwnsFile(rel string) bool    { return strings.HasSuffix(rel, e.suffix) }
func (e emptyDomainStub) Extract(context.Context, string, []string) ([]facts.Fact, error) {
	*e.runs++
	return nil, nil
}

// declaringStub is the same extractor with plugin.DeltaInputs implemented. reads
// widens the declaration past the files it owns, for an extractor that consults a
// manifest it does not itself produce facts for; names declares the production
// inventory name set, as a markdown link checker does.
type declaringStub struct {
	emptyDomainStub
	reads func(rel string) bool
	names bool
}

func (d declaringStub) ContentInput(rel string) bool {
	if d.reads != nil {
		return d.reads(rel)
	}
	return d.OwnsFile(rel)
}
func (d declaringStub) NameSetInput() bool { return d.names }

func emptyDomainRepo(t *testing.T) string {
	t.Helper()
	return setupTSRepo(t, map[string]string{
		"package.json":     `{"name":"root"}`,
		"first.ts":         "export const target=1;\n",
		"consumer.ts":      "import { target } from './first';\nexport const use=target;\n",
		"requirements.txt": "# no active Python sources\n",
	})
}

func TestEmptyDomainWithoutDeclaredInputsKeepsWholeDomainFallback(t *testing.T) {
	root := emptyDomainRepo(t)
	runs := 0
	eng := multiEngine(t, root, emptyDomainStub{name: "undeclared", suffix: ".undeclared", runs: &runs})
	opts := Options{StateDir: t.TempDir(), AuthoritativeFiles: true}
	cons := NewConsumer()
	configScopeRun(t, eng, root, opts, cons)
	before := runs
	writeFile(t, root, "new.ts", "export const fresh=7;\n")
	res, _, _ := configScopeRun(t, eng, root, opts, cons)
	if runs == before {
		t.Fatalf("extractor that never declared its inputs was skipped on a source addition: runs=%d", runs)
	}
	if got := wholeDomainFallback(res); got == "" {
		t.Fatalf("undeclared empty domain re-ran without recording why: fallbacks=%v", res.Fallbacks)
	}
	assertAppliedEqualsCold(t, cons, coldConsumer(t, eng, root))
}

func TestEmptyDomainWithDeclaredInputsIgnoresUnreadSourceAddition(t *testing.T) {
	root := emptyDomainRepo(t)
	runs := 0
	eng := multiEngine(t, root, declaringStub{emptyDomainStub: emptyDomainStub{name: "declared", suffix: ".declared", runs: &runs}})
	opts := Options{StateDir: t.TempDir(), AuthoritativeFiles: true}
	cons := NewConsumer()
	configScopeRun(t, eng, root, opts, cons)
	before := runs
	writeFile(t, root, "new.ts", "export const fresh=7;\n")
	res, scope, ids := configScopeRun(t, eng, root, opts, cons)
	if runs != before {
		t.Fatalf("empty declared domain re-ran for a file it does not read: runs=%d->%d scope=%v", before, runs, ids)
	}
	if scope["consumer.ts"] || scope["first.ts"] {
		t.Fatalf("source addition replaced unrelated owners: scope=%v parsed=%d fallbacks=%v", ids, res.ParsedFiles, res.Fallbacks)
	}
	assertAppliedEqualsCold(t, cons, coldConsumer(t, eng, root))
}

func TestEmptyDomainDeclaringTheNameSetStillSeesSourceAddition(t *testing.T) {
	root := emptyDomainRepo(t)
	runs := 0
	eng := multiEngine(t, root, declaringStub{
		emptyDomainStub: emptyDomainStub{name: "namesensitive", suffix: ".names", runs: &runs},
		names:           true,
	})
	opts := Options{StateDir: t.TempDir(), AuthoritativeFiles: true}
	cons := NewConsumer()
	configScopeRun(t, eng, root, opts, cons)
	before := runs
	writeFile(t, root, "new.ts", "export const fresh=7;\n")
	configScopeRun(t, eng, root, opts, cons)
	if runs == before {
		t.Fatalf("extractor that declared the inventory name set missed a new name: runs=%d", runs)
	}
	assertAppliedEqualsCold(t, cons, coldConsumer(t, eng, root))
}

func TestEmptyDomainSeesItsDeclaredNonOwnedInputChange(t *testing.T) {
	root := emptyDomainRepo(t)
	runs := 0
	eng := multiEngine(t, root, declaringStub{
		emptyDomainStub: emptyDomainStub{name: "manifestreader", suffix: ".manifest", runs: &runs},
		reads:           func(rel string) bool { return rel == "requirements.txt" },
	})
	opts := Options{StateDir: t.TempDir(), AuthoritativeFiles: true}
	cons := NewConsumer()
	configScopeRun(t, eng, root, opts, cons)
	quiet := runs
	writeFile(t, root, "new.ts", "export const fresh=7;\n")
	configScopeRun(t, eng, root, opts, cons)
	if runs != quiet {
		t.Fatalf("declared reader re-ran for a source it does not read: runs=%d->%d", quiet, runs)
	}
	writeFile(t, root, "requirements.txt", "# one active dependency\nrequests\n")
	configScopeRun(t, eng, root, opts, cons)
	if runs == quiet {
		t.Fatalf("declared reader missed a change to the only file it reads: runs=%d", runs)
	}
	assertAppliedEqualsCold(t, cons, coldConsumer(t, eng, root))
}

func TestEmptyDomainWithoutStoredInputDigestReRuns(t *testing.T) {
	root := emptyDomainRepo(t)
	runs := 0
	eng := multiEngine(t, root, declaringStub{emptyDomainStub: emptyDomainStub{name: "predigest", suffix: ".predigest", runs: &runs}})
	opts := Options{StateDir: t.TempDir(), AuthoritativeFiles: true}
	cons := NewConsumer()
	configScopeRun(t, eng, root, opts, cons)
	st, err := readStateFile(statePath(opts.StateDir))
	if err != nil {
		t.Fatal(err)
	}
	// A state written before the extractor declared anything carries no digest to
	// compare, which is not the same fact as a digest that matches.
	delete(st.ExtractorInputHash, "predigest")
	if err := saveState(opts.StateDir, st); err != nil {
		t.Fatal(err)
	}
	before := runs
	writeFile(t, root, "new.ts", "export const fresh=7;\n")
	configScopeRun(t, eng, root, opts, cons)
	if runs == before {
		t.Fatalf("empty domain with no stored input digest was skipped: runs=%d", runs)
	}
	assertAppliedEqualsCold(t, cons, coldConsumer(t, eng, root))
}

// Deferring an empty domain to the declared comparison stays conservative for an
// extractor that declares nothing only because its digest IS the scan digest, so
// the deferred comparison asks exactly what the removed short-circuit asked. That
// is a property of extractorInputDigest rather than of the branch, so pin it: if
// the fallback ever stops being the scan digest, the explicit DeltaInputs guard in
// the empty-domain branch becomes the only thing keeping an opaque extractor from
// being skipped on evidence it never produced.
func TestOpaqueExtractorInputDigestIsTheScanDigest(t *testing.T) {
	runs := 0
	opaque := emptyDomainStub{name: "opaque", suffix: ".opaque", runs: &runs}
	declaring := declaringStub{emptyDomainStub: emptyDomainStub{name: "declaring", suffix: ".declaring", runs: &runs}}
	inv := []string{"first.ts", "consumer.ts"}
	hashes := map[string]string{"first.ts": "h1", "consumer.ts": "h2"}
	got := extractorInputDigest(opaque, nil, inv, nil, hashes, "fileset", "scan")
	if got != "scan" {
		t.Fatalf("opaque extractor digest is no longer the scan digest: %q", got)
	}
	if moved := extractorInputDigest(opaque, nil, inv, nil, hashes, "fileset", "scan2"); moved == got {
		t.Fatal("opaque extractor digest did not move with the scan digest")
	}
	if d := extractorInputDigest(declaring, nil, inv, nil, hashes, "fileset", "scan"); d == "scan" {
		t.Fatal("declaring extractor fell back to the scan digest")
	}
}
