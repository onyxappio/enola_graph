package fsm

import (
	"github.com/enola-labs/enola/internal/facts"
	"testing"
)

func TestInterpreterGuardsBindDeclarationsAndIgnoreTextAndMembers(t *testing.T) {
	sources := map[string][]byte{
		"machine.ts": []byte(`import { ready as canProceed } from './guards'; function decoy() {} function local() {}`),
		"guards.ts":  []byte(`export function ready() { return true; }`),
	}
	a, _ := analyzerForSources(nil, sources)
	m := &machineModel{file: "machine.ts", reads: map[string]bool{}, coverage: map[string]any{}}
	var rels []facts.Relation
	a.addInterpreterGuardRelations(m, &rels, []string{`canProceed() && local() && obj.decoy() && "decoy()" && missing()`})
	if len(rels) != 2 {
		t.Fatalf("expected only two proven direct calls: %#v", rels)
	}
	want := map[string]string{moduleName("guards.ts") + ".ready": "guards.ts", moduleName("machine.ts") + ".local": "machine.ts"}
	for _, rel := range rels {
		if rel.Kind != facts.RelFSMGuardCalls || want[rel.Target] != rel.TargetFile || rel.TargetFile == "" {
			t.Fatalf("unproven guard endpoint: %#v", rel)
		}
		delete(want, rel.Target)
	}
	if len(want) != 0 || !m.reads["guards.ts"] {
		t.Fatalf("missing endpoint/read: %v, %v", want, m.reads)
	}
	if !m.partial || m.coverage["unresolved_guard_calls"] != 1 {
		t.Fatalf("missing call must remain explicit coverage: %#v", m.coverage)
	}
}
