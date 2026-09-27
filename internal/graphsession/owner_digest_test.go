package graphsession

import (
	"github.com/enola-labs/enola/internal/graphstream"
	"testing"
)

func TestOwnerDigestPreservesWireDifferences(t *testing.T) {
	owner := graphstream.OwnerRef{Kind: graphstream.OwnerFile, ID: "a.ts"}
	n := graphstream.Node{Owner: owner, ID: "function:a", Name: "a", Kind: "function", Occurrence: 0, Props: map[string]any{"io_direct": false}}
	e := graphstream.Edge{Owner: owner, FromID: n.ID, Kind: "calls", TargetName: "b", TargetID: "function:b", Resolution: graphstream.ResResolved}
	base, err := resolvedOwnerDigest([]graphstream.Node{n}, []graphstream.Edge{e})
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name  string
		nodes []graphstream.Node
		edges []graphstream.Edge
	}{
		{"property", []graphstream.Node{func() graphstream.Node { v := n; v.Props = map[string]any{"io_direct": true}; return v }()}, []graphstream.Edge{e}},
		{"location", []graphstream.Node{func() graphstream.Node { v := n; v.Line = 2; return v }()}, []graphstream.Edge{e}},
		{"resolved target", []graphstream.Node{n}, []graphstream.Edge{func() graphstream.Edge { v := e; v.TargetID = "function:c"; return v }()}},
		{"unresolved", []graphstream.Node{n}, []graphstream.Edge{func() graphstream.Edge { v := e; v.TargetID = ""; v.Resolution = graphstream.ResUnresolved; return v }()}},
		{"occurrence", []graphstream.Node{n}, []graphstream.Edge{func() graphstream.Edge { v := e; v.Occurrence = 1; return v }()}},
		{"duplicate", []graphstream.Node{n, n}, []graphstream.Edge{e}},
		{"empty", nil, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolvedOwnerDigest(tc.nodes, tc.edges)
			if err != nil {
				t.Fatal(err)
			}
			if got == base {
				t.Fatal("wire difference suppressed")
			}
		})
	}
	second := n
	second.ID = "function:second"
	a, err := resolvedOwnerDigest([]graphstream.Node{n, second}, []graphstream.Edge{e})
	if err != nil {
		t.Fatal(err)
	}
	b, err := resolvedOwnerDigest([]graphstream.Node{second, n}, []graphstream.Edge{e})
	if err != nil {
		t.Fatal(err)
	}
	if a != b {
		t.Fatal("ordering alone changed digest")
	}
}

func TestOwnerDigestRejectsUnencodableProperties(t *testing.T) {
	_, err := resolvedOwnerDigest([]graphstream.Node{{Props: map[string]any{"invalid": make(chan int)}}}, nil)
	if err == nil {
		t.Fatal("unencodable contribution accepted")
	}
}
