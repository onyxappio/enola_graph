package tsextractor

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// compositionWalkFixture extends the shared-walk fixture with the source files a
// composition signature actually reads, so the signature has Nuxt, GraphQL and
// gRPC content to hash rather than hashing an empty tree.
//
// packages/ui is deliberately NOT a Nuxt package to begin with, and already
// holds a component the file list carries. That is what lets the late-mutation
// test below move the signature without moving the file list: the component is
// known from the first call, and only the package metadata changes.
func compositionWalkFixture(t *testing.T) (root string, files []string) {
	t.Helper()
	root = discoveryWalkFixture(t)
	write := func(rel, body string) {
		t.Helper()
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	write("apps/landings/components/Widget.vue", "<template><div/></template>\n")
	write("apps/landings/pages/index.vue", "<template><Widget/></template>\n")
	write("packages/ui/components/Card.vue", "<template><span/></template>\n")
	write("packages/api/src/index.ts", "export const api = 1\n")
	write("packages/api/src/schema.ts", "export const typeDefs = gql`type Query { a: String }`\n")

	return root, []string{
		"apps/landings/components/Widget.vue",
		"apps/landings/pages/index.vue",
		"packages/ui/components/Card.vue",
		"packages/api/src/index.ts",
		"packages/api/src/schema.ts",
	}
}

func uncachedCompositionSignature(t *testing.T, root string, files []string) string {
	t.Helper()
	ctx := withFileOverlay(context.Background(), newFileOverlay(root, nil))
	sig, err := compositionSignatureOn(ctx, root, files, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("uncached composition signature: %v", err)
	}
	return sig
}

func cachedCompositionSignature(t *testing.T, root string, files []string) string {
	t.Helper()
	sig, err := CompositionSignature(root, files, nil, nil, nil)
	if err != nil {
		t.Fatalf("composition signature: %v", err)
	}
	return sig
}

// The collectors behind the signature are proven equivalent cached and uncached
// by TestSharedDiscoveryWalkMatchesPerCollectorWalks. This is the signature-level
// statement of the same thing: sharing one enumeration across the four collectors
// CompositionSignature reaches must not move the hash it returns, over a tree
// that exercises every shape the prune rule has an opinion about - nested
// workspace packages, a duplicate specifier whose winner depends on walk order,
// a nested Nuxt app, an invalid package.json, and pruned node_modules, dot and
// testdata directories each holding a package.json that must stay unseen.
func TestCompositionSignatureSharedWalkMatchesSeparateWalks(t *testing.T) {
	root, files := compositionWalkFixture(t)

	want := uncachedCompositionSignature(t, root, files)
	got := cachedCompositionSignature(t, root, files)
	if want != got {
		t.Fatalf("shared walk moved the composition signature:\n separate=%s\n shared  =%s", want, got)
	}
	if want == "" {
		t.Fatal("fixture produced an empty signature")
	}
}

// The cache is scoped to one call and borrows nothing from any other, so a
// package.json that appears or changes between two signatures is still seen by
// the second one. This is the property the cross-call reuse candidate would NOT
// have, and the reason this layer is separable from it: nothing here observes
// the tree less often than the unshared code did, only less redundantly.
//
// The file list is identical across both calls. packages/ui/components/Card.vue
// is known from the start and contributes nothing while packages/ui is an
// ordinary package; adding a Nuxt dependency to that package.json alone brings
// it into the auto-component index. So a moved signature can only have come from
// re-reading the package metadata.
func TestCompositionSignatureSeesLatePackageMutation(t *testing.T) {
	root, files := compositionWalkFixture(t)

	before := cachedCompositionSignature(t, root, files)
	if before != uncachedCompositionSignature(t, root, files) {
		t.Fatal("shared and separate walks disagreed before the mutation")
	}

	pkg := filepath.Join(root, "packages", "ui", "package.json")
	if err := os.WriteFile(pkg, []byte(`{"name":"@acme/ui","types":"dist/index.d.ts","dependencies":{"vue":"^3","nuxt":"^3"}}`), 0o600); err != nil {
		t.Fatal(err)
	}

	after := cachedCompositionSignature(t, root, files)
	if after == before {
		t.Fatal("a package.json that became a Nuxt package between two calls did not move the signature; the shared walk outlived its call")
	}
	if after != uncachedCompositionSignature(t, root, files) {
		t.Fatal("shared and separate walks disagreed after the mutation")
	}
}

// A second signature over an unchanged tree must return the same hash. Without
// this, a cache that leaked across calls could pass the mutation test above by
// being wrong in both directions.
func TestCompositionSignatureStableAcrossCallsOnUnchangedTree(t *testing.T) {
	root, files := compositionWalkFixture(t)

	first := cachedCompositionSignature(t, root, files)
	second := cachedCompositionSignature(t, root, files)
	if first != second {
		t.Fatalf("composition signature is not stable over an unchanged tree:\n first =%s\n second=%s", first, second)
	}
}
