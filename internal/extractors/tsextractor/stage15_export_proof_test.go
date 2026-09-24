package tsextractor

import "testing"

// TestStage15MergeSeparatesUnknownFromProvenEmpty guards the distinction the
// merged proof rests on. An SFC index starts context-free and every block
// narrows it; a block that is empty because its source was empty proved that,
// while a block that is empty because nobody could read it proved nothing and
// may still name a module. Both look alike on the index - empty is set either
// way - so only the block's own proof separates them.
func TestStage15MergeSeparatesUnknownFromProvenEmpty(t *testing.T) {
	newDst := func() *namedExportIndex {
		return &namedExportIndex{
			contextFree: true,
			local:       map[string]bool{"seen": true},
			named:       map[string][][2]string{},
			unresolved:  map[string]bool{},
		}
	}
	for _, tc := range []struct {
		name  string
		src   *namedExportIndex
		want  bool
		keeps bool
	}{
		{"proven-empty-keeps-proof", &namedExportIndex{empty: true, contextFree: true}, true, true},
		{"unknown-empty-clears-proof", &namedExportIndex{empty: true}, false, true},
		{"nil-block-clears-proof", nil, false, true},
		{"proven-block-keeps-proof", &namedExportIndex{contextFree: true, local: map[string]bool{"added": true}}, true, true},
		{"forwarding-block-clears-proof", &namedExportIndex{local: map[string]bool{"added": true}}, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dst := newDst()
			mergeNamedExportIndex(dst, tc.src)
			if dst.contextFree != tc.want {
				t.Fatalf("contextFree = %v, want %v", dst.contextFree, tc.want)
			}
			if tc.keeps && !dst.local["seen"] {
				t.Fatal("narrowing the proof dropped a fact the merge already held")
			}
		})
	}
}

// TestStage15MergeOrderDoesNotRecoverProof pins the narrowing as one-way: a
// block nobody could read must not be washed out by a later readable one.
func TestStage15MergeOrderDoesNotRecoverProof(t *testing.T) {
	dst := &namedExportIndex{contextFree: true, local: map[string]bool{}, named: map[string][][2]string{}, unresolved: map[string]bool{}}
	mergeNamedExportIndex(dst, &namedExportIndex{empty: true})
	mergeNamedExportIndex(dst, &namedExportIndex{contextFree: true, local: map[string]bool{"late": true}})
	if dst.contextFree {
		t.Fatal("a readable block after an unreadable one recovered a proof that was never made")
	}
	if !dst.local["late"] {
		t.Fatal("facts from the readable block were dropped")
	}
}

// TestStage15SFCProofRunsThroughTheRealParser is the end-to-end half: the
// indexes here come from parseNamedExportIndex over real sources, not from
// structs, so the proof is whatever the block-merging path actually computes.
func TestStage15SFCProofRunsThroughTheRealParser(t *testing.T) {
	files := map[string]string{
		"local.vue":     "<script>\nexport function one() { return 1; }\n</script>\n",
		"forwarded.vue": "<script>\nexport { one } from './local';\n</script>\n",
		"noscript.vue":  "<template><div/></template>\n",
		"local.ts":      "export function one() { return 1; }\n",
		"forwarded.ts":  "export { one } from './local';\n",
		"empty.ts":      "",
	}
	readSrc := func(f string) []byte { return []byte(files[f]) }
	known := map[string]bool{"local.ts": true, "local.vue": true}
	for _, tc := range []struct {
		file string
		want bool
	}{
		{"local.vue", true},
		{"forwarded.vue", false},
		{"noscript.vue", true},
		{"local.ts", true},
		{"forwarded.ts", false},
		{"empty.ts", true},
	} {
		t.Run(tc.file, func(t *testing.T) {
			idx := parseNamedExportIndex(tc.file, readSrc, nil, known)
			if idx.isContextFree() != tc.want {
				t.Fatalf("contextFree = %v, want %v", idx.isContextFree(), tc.want)
			}
		})
	}
}

// TestStage15PeekNeverBuilds pins the cost side of the design: a peek at a file
// no one indexed must return nothing rather than parse it, and must not leave
// an entry behind that a later peek would read as an answer.
func TestStage15PeekNeverBuilds(t *testing.T) {
	c := newNamedExportCache()
	if idx := c.peek("never-indexed.ts"); idx != nil {
		t.Fatalf("peek built an index for a file nobody asked for: %+v", idx)
	}
	if c.peek("never-indexed.ts").isContextFree() {
		t.Fatal("a file with no index must not read as proven")
	}
	if got := c.summaryScans(); got != 0 {
		t.Fatalf("peek cost %d scans, want 0", got)
	}
	var nilCache *namedExportCache
	if nilCache.peek("anything").isContextFree() {
		t.Fatal("a nil cache must not read as proven")
	}
}
