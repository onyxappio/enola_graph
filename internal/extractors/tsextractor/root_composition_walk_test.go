package tsextractor

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestRootCompositionWalkNewPackageAndCapture(t *testing.T) {
	root := retentionRepo(t, map[string]string{
		"package.json":                    `{"name":"root","dependencies":{"vue":"^3"}}`,
		"packages/ui/components/Card.vue": "<template><span/></template>",
		"src/a.ts":                        "export const a = 1",
	})
	files := []string{"packages/ui/components/Card.vue", "src/a.ts"}
	check := func(sources map[string][]byte) string {
		t.Helper()
		got, err := CompositionSignature(root, files, nil, nil, sources)
		if err != nil {
			t.Fatal(err)
		}
		ctx := withFileOverlay(context.Background(), newFileOverlay(root, sources))
		want, err := compositionSignatureOn(ctx, root, files, nil, nil, sources, nil)
		if err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Fatal("cached and uncached signatures differ")
		}
		return got
	}
	before := check(nil)
	manifest := `{"name":"ui","dependencies":{"nuxt":"^3"}}`
	writeRetentionFile(t, root, "packages/ui/package.json", manifest)
	after := check(nil)
	if after == before {
		t.Fatal("new uncaptured nested package not observed")
	}
	// A captured package must override contradictory live bytes on every call.
	captured := map[string][]byte{"packages/ui/package.json": []byte(`{"name":"ui","dependencies":{"vue":"^3"}}`)}
	if got := check(captured); got != before {
		t.Fatal("live package overrode capture")
	}
	if err := os.Remove(filepath.Join(root, "packages/ui/package.json")); err != nil {
		t.Fatal(err)
	}
	if got := check(nil); got != before {
		t.Fatal("removed package persisted across calls")
	}
	if got := check(map[string][]byte{"packages/ui/package.json": []byte(manifest)}); got != after {
		t.Fatal("captured new package absent from walk")
	}
}
