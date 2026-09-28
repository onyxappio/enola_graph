package graphsession

import (
	"testing"

	"github.com/enola-labs/enola/internal/facts"
	"github.com/enola-labs/enola/internal/graphstream"
)

func TestFSMResolverUsesTypedEndpointsAndProvenFiles(t *testing.T) {
	relations := map[string]string{
		facts.RelFSMParent: facts.KindFSMState, facts.RelFSMInitial: facts.KindFSMState,
		facts.RelFSMFrom: facts.KindFSMState, facts.RelFSMTo: facts.KindFSMState,
		facts.RelFSMOn: facts.KindFSMEvent, facts.RelFSMEmits: facts.KindFSMCommand,
		facts.RelFSMEntryEmits: facts.KindFSMCommand, facts.RelFSMOutcome: facts.KindFSMEvent,
		facts.RelFSMGuardRef: facts.KindSymbol, facts.RelFSMReducerRef: facts.KindSymbol,
		facts.RelFSMGuardCalls: facts.KindSymbol, facts.RelFSMActionCalls: facts.KindSymbol,
		facts.RelFSMDeclaredIn: facts.KindSymbol, facts.RelFSMTypedBy: facts.KindSymbol,
		facts.RelFSMConstructsEvent: facts.KindFSMEvent, facts.RelFSMDispatches: facts.KindFSMEvent,
		facts.RelFSMDispatchesUnknownEvent: facts.KindFSMMachine, facts.RelFSMHandlesCommand: facts.KindFSMCommand,
	}
	for relation, targetKind := range relations {
		t.Run(relation, func(t *testing.T) {
			want := facts.Fact{Kind: targetKind, Name: "same.name", File: "src/one.ts", Repo: "product"}
			wrongKind := want
			wrongKind.Kind = facts.KindModule
			idx := buildIndex([]facts.Fact{want, wrongKind})
			id, status := idx.resolveRelConstrained("product", facts.KindFSMTransition, relation, want.Name, false, "")
			if status != "resolved" || id != want.Identity() {
				t.Fatalf("typed target = (%s, %s), want (%s, resolved)", id, status, want.Identity())
			}

			idx = buildIndex([]facts.Fact{wrongKind})
			if _, status := idx.resolveRelConstrained("product", facts.KindFSMTransition, relation, want.Name, false, ""); status != "unresolved" {
				t.Fatalf("wrong-kind-only target status = %s, want unresolved", status)
			}

			sibling := want
			sibling.File = "src/two.ts"
			idx = buildIndex([]facts.Fact{want, sibling})
			id, status = idx.resolveRelConstrained("product", facts.KindFSMTransition, relation, want.Name, false, want.File)
			if status != "resolved" || id != want.Identity() {
				t.Fatalf("proven target file = (%s, %s), want (%s, resolved)", id, status, want.Identity())
			}

			idx = buildIndex([]facts.Fact{sibling})
			if _, status := idx.resolveRelConstrained("product", facts.KindFSMTransition, relation, want.Name, false, want.File); status != "unresolved" {
				t.Fatalf("missing proven target rebound: status = %s", status)
			}
		})
	}
}

func TestFSMExtractionCoverageUsesFileOwnership(t *testing.T) {
	fsmCoverage := ownerOf(facts.Fact{Kind: facts.KindExtraction, Name: "typescript:fsm:jobs", File: "src/machine.ts",
		Props: map[string]any{"extractor": "typescript:fsm"}})
	if fsmCoverage.Kind != graphstream.OwnerFile || fsmCoverage.ID != "src/machine.ts" {
		t.Fatalf("FSM coverage owner = %+v, want machine file", fsmCoverage)
	}
	legacy := ownerOf(facts.Fact{Kind: facts.KindExtraction, Name: "typescript:legacy", File: "src/other.ts"})
	if legacy.Kind != graphstream.OwnerSynthetic || legacy.ID != "aggregate:typescript" {
		t.Fatalf("legacy extraction owner = %+v, want preserved aggregate owner", legacy)
	}
}
