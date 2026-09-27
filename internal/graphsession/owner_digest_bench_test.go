package graphsession

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/enola-labs/enola/internal/graphstream"
	"sort"
	"strings"
	"testing"
)

func legacyOwnerDigestBenchmark(nodes []graphstream.Node, edges []graphstream.Edge) (string, error) {
	records := make([]string, 0, len(nodes)+len(edges))
	for _, node := range nodes {
		b, err := json.Marshal(node)
		if err != nil {
			return "", err
		}
		records = append(records, "n:"+string(b))
	}
	for _, edge := range edges {
		b, err := json.Marshal(edge)
		if err != nil {
			return "", err
		}
		records = append(records, "e:"+string(b))
	}
	sort.Strings(records)
	b, err := json.Marshal(records)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

func BenchmarkOwnerContributionDigest(b *testing.B) {
	for _, count := range []int{16, 256} {
		nodes := make([]graphstream.Node, count)
		edges := make([]graphstream.Edge, count*2)
		for i := range nodes {
			nodes[i] = graphstream.Node{Owner: graphstream.OwnerRef{Kind: graphstream.OwnerFile, ID: "source.ts"}, ID: fmt.Sprintf("function:f%d", i), Kind: "function", Name: fmt.Sprintf("f%d", i), Line: i + 1, Props: map[string]any{"io_direct": false, "signature": strings.Repeat("arg: string, ", 12), "detail": "quoted \"text\"\nUnicode: Україна"}}
		}
		for i := range edges {
			edges[i] = graphstream.Edge{Owner: nodes[0].Owner, FromID: nodes[i%count].ID, Kind: "calls", TargetID: nodes[(i+1)%count].ID, Resolution: graphstream.ResResolved, Occurrence: i}
		}
		for _, impl := range []struct {
			name string
			fn   func([]graphstream.Node, []graphstream.Edge) (string, error)
		}{{"legacy", legacyOwnerDigestBenchmark}, {"candidate", resolvedOwnerDigest}} {
			b.Run(fmt.Sprintf("%d/%s", count, impl.name), func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					if _, err := impl.fn(nodes, edges); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}
