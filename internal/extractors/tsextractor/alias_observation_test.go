package tsextractor

import (
	"context"
	"gopkg.in/yaml.v3"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/enola-labs/enola/internal/extractors/inputscope"
	"github.com/enola-labs/enola/internal/graphinput"
)

func aliasObservedContext(root string, scope *inputscope.Scope) (context.Context, *overlayProbe) {
	p := newOverlayProbe()
	ctx := withDiscoveryWalkCache(withOverlayProbe(context.Background(), p))
	sharedDiscoveryEntries(ctx, root, scope)
	return ctx, p
}

func TestAliasObservedListingMatchesLiveWalker(t *testing.T) {
	root := retentionRepo(t, map[string]string{
		"tsconfig.json":          `{"compilerOptions":{"paths":{"@root/*":["src/*"]}}}`,
		"a/tsconfig.base.json":   `{"compilerOptions":{"paths":{"@a/*":["lib/*"]}}}`,
		"testdata/tsconfig.json": `{"compilerOptions":{"paths":{"@fixture/*":["fixture/*"]}}}`,
		"excluded/tsconfig.json": `{"compilerOptions":{"paths":{"@bad/*":["bad/*"]}}}`,
		"z/TsCoNfIg.JsOn":        `{"compilerOptions":{"paths":{"@case/*":["case/*"]}}}`,
	})
	if err := os.Symlink(filepath.Join(root, "a"), filepath.Join(root, "linked")); err != nil {
		t.Fatal(err)
	}
	policy, err := graphinput.Build(root, graphinput.Options{Exclude: []string{"excluded/**"}})
	if err != nil {
		t.Fatal(err)
	}
	scope := &inputscope.Scope{Root: root, Policy: policy}
	oldCtx, oldProbe := aliasObservedContext(root, scope)
	newCtx, newProbe := aliasObservedContext(root, scope)
	var want []tsAliasRoot
	// Non-nil visit forces the original real-listing path at every directory.
	walkTSAliasRootsVisit(oldCtx, root, root, &want, func(string, []fs.DirEntry) {}, scope)
	got := collectTSAliasRoots(newCtx, root, scope)
	if !reflect.DeepEqual(want, got) {
		t.Fatalf("alias mismatch: %v != %v", want, got)
	}
	if !reflect.DeepEqual(oldProbe.snapshot(), newProbe.snapshot()) || !reflect.DeepEqual(oldProbe.dirSnapshot(), newProbe.dirSnapshot()) || !reflect.DeepEqual(oldProbe.statSnapshot(), newProbe.statSnapshot()) {
		t.Fatal("discovery observation ledger differs")
	}
	if len(got) < 3 {
		t.Fatalf("fixture lost alias roots: %v", got)
	}
}

func TestAliasObservedListingStillRefusesMembershipChange(t *testing.T) {
	root := retentionRepo(t, map[string]string{"package.json": "{}", "src/a.ts": "export const a = 1"})
	policy, err := graphinput.Build(root, graphinput.Options{})
	if err != nil {
		t.Fatal(err)
	}
	scope := &inputscope.Scope{Root: root, Policy: policy}
	e := NewGraph(scope)
	d := e.NewDiscovery(context.Background(), root, nil)
	writeRetentionFile(t, root, "tsconfig.json", `{"compilerOptions":{"paths":{"@x/*":["src/*"]}}}`)
	if _, reused, _ := e.DiscoveryFor(context.Background(), root, nil, d); reused {
		t.Fatal("changed directory membership reused stale discovery")
	}
}

func TestAliasObservedListingKeepsCapturedAbsentConfig(t *testing.T) {
	root := retentionRepo(t, map[string]string{"package.json": "{}"})
	policy, err := graphinput.Build(root, graphinput.Options{})
	if err != nil {
		t.Fatal(err)
	}
	scope := &inputscope.Scope{Root: root, Policy: policy}
	ctx, _ := aliasObservedContext(root, scope)
	ctx = withFileOverlay(ctx, newFileOverlay(root, map[string][]byte{"tsconfig.json": []byte(`{"compilerOptions":{"paths":{"@captured/*":["src/*"]}}}`)}))
	roots := collectTSAliasRoots(ctx, root, scope)
	if len(roots) != 1 || roots[0].aliases["@captured/"].replacement != "src/" {
		t.Fatalf("captured config lost: %#v", roots)
	}
}

func TestDiagnosticAliasObservedProduct(t *testing.T) {
	root := os.Getenv("ENOLA_ALIAS_DIAGNOSTIC_ROOT")
	if root == "" {
		t.Skip("optional shared-host Product microdiagnostic")
	}
	opts := graphinput.Options{}
	if cfg := os.Getenv("ENOLA_ALIAS_DIAGNOSTIC_SCOPE"); cfg != "" {
		var config struct {
			Ignore      []string `yaml:"ignore"`
			GraphInputs struct {
				Exclude []string `yaml:"exclude"`
			} `yaml:"graph_inputs"`
		}
		data, err := os.ReadFile(cfg)
		if err != nil {
			t.Fatal(err)
		}
		if err := yaml.Unmarshal(data, &config); err != nil {
			t.Fatal(err)
		}
		opts.Exclude = append(config.Ignore, config.GraphInputs.Exclude...)
	}
	policy, err := graphinput.Build(root, opts)
	if err != nil {
		t.Fatal(err)
	}
	scope := &inputscope.Scope{Root: root, Policy: policy}
	for i := 0; i < 6; i++ {
		oldCtx, oldProbe := aliasObservedContext(root, scope)
		newCtx, newProbe := aliasObservedContext(root, scope)
		var want, got []tsAliasRoot
		var oldTime, newTime time.Duration
		old := func() {
			start := time.Now()
			walkTSAliasRootsVisit(oldCtx, root, root, &want, func(string, []fs.DirEntry) {}, scope)
			oldTime = time.Since(start)
		}
		candidate := func() {
			start := time.Now()
			got = collectTSAliasRoots(newCtx, root, scope)
			newTime = time.Since(start)
		}
		if i%2 == 0 {
			old()
			candidate()
		} else {
			candidate()
			old()
		}
		if !reflect.DeepEqual(want, got) || !reflect.DeepEqual(oldProbe.snapshot(), newProbe.snapshot()) || !reflect.DeepEqual(oldProbe.dirSnapshot(), newProbe.dirSnapshot()) {
			t.Fatal("Product aliases or ledger differ")
		}
		t.Logf("pair=%d live_ms=%.3f observed_ms=%.3f roots=%d dirs=%d", i, float64(oldTime)/float64(time.Millisecond), float64(newTime)/float64(time.Millisecond), len(got), len(newProbe.dirSnapshot()))
	}
}
