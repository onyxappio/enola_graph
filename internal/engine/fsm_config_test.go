package engine

import (
	"testing"

	"github.com/enola-labs/enola/internal/config"
	"github.com/enola-labs/enola/internal/fsm"
)

func TestConfigHashFoldsConfiguredStateMachines(t *testing.T) {
	base := computeConfigHash(config.Default())
	empty := config.Default()
	empty.StateMachines = []fsm.Spec{}
	if computeConfigHash(empty) != base {
		t.Fatal("an empty FSM declaration changed the configuration hash")
	}
	configured := config.Default()
	configured.StateMachines = []fsm.Spec{{
		ID: "jobs", Adapter: fsm.AdapterRuleTable, File: "src/jobs.ts",
		Factory: fsm.SymbolRef{Module: "src/kernel.ts", Export: "defineMachine"}, Registration: "createRegistration",
	}}
	if err := configured.Normalize(); err != nil {
		t.Fatal(err)
	}
	if computeConfigHash(configured) == base {
		t.Fatal("configured machine adapters do not change the graph configuration hash")
	}
	changedSink := config.Default()
	changedSink.StateMachines = []fsm.Spec{{
		ID: "jobs", Adapter: fsm.AdapterRuleTable, File: "src/jobs.ts",
		Factory: fsm.SymbolRef{Module: "src/kernel.ts", Export: "defineMachine"}, Registration: "createRegistration",
		DispatchFiles: []string{"src/worker.ts"},
	}}
	if err := changedSink.Normalize(); err != nil {
		t.Fatal(err)
	}
	if computeConfigHash(changedSink) == computeConfigHash(configured) {
		t.Fatal("changing a configured dispatch scope did not change the graph configuration hash")
	}
}
