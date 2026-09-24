package tsextractor

import "testing"

func TestIndependentExportProofMergeRejectsUnknownBlock(t *testing.T) {
	for _, tc := range []struct {
		name string
		src  *namedExportIndex
		want bool
	}{
		{"missing-index", nil, false},
		{"unknown-empty-index", &namedExportIndex{empty: true}, false},
		{"proven-empty-index", &namedExportIndex{empty: true, contextFree: true}, true},
		{"forwarding-index", &namedExportIndex{local: map[string]bool{}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dst := &namedExportIndex{contextFree: true, local: map[string]bool{"kept": true}, named: map[string][][2]string{}, unresolved: map[string]bool{}}
			mergeNamedExportIndex(dst, tc.src)
			if dst.contextFree != tc.want {
				t.Fatalf("contextFree=%v, want %v", dst.contextFree, tc.want)
			}
			if !dst.local["kept"] {
				t.Fatal("existing facts lost while combining proof")
			}
		})
	}
}
