package config

import (
	"strings"
	"testing"
)

func TestLoad_StateMachineAdapters(t *testing.T) {
	cfg, err := Load(writeConfig(t, `
state_machines:
  - id: jobs
    adapter: rule_table
    file: src/jobs.ts
    factory:
      module: src/kernel.ts
      export: defineMachine
    registration: createRegistration
    state_type: JobState
    event_type: JobEvent
    command_type: JobCommand
  - id: app
    adapter: reducer_interpreter
    file: src/interpreter.ts
    dispatcher: sendEvent
    enter: enterState
    state_type: AppState
    event_type: AppEvent
    dispatch_files: [src/App.tsx]
    dispatch_sinks:
      - factory:
          module: src/runtime.ts
          export: createRuntime
        method: send
`))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.StateMachines) != 2 || cfg.StateMachines[0].ID != "app" || cfg.StateMachines[1].ID != "jobs" {
		t.Fatalf("normalized FSM specs = %#v", cfg.StateMachines)
	}
	if cfg.StateMachines[0].DispatchSinks[0].Factory.Export != "createRuntime" {
		t.Fatalf("dispatch sink was not loaded: %#v", cfg.StateMachines[0].DispatchSinks)
	}
}

func TestLoad_InvalidStateMachineNamesConfig(t *testing.T) {
	_, err := Load(writeConfig(t, `
state_machines:
  - id: jobs
    adapter: rule_table
    file: ../outside.ts
    registration: createRegistration
    factory:
      module: src/kernel.ts
      export: defineMachine
`))
	if err == nil || !strings.Contains(err.Error(), "repository-relative path") {
		t.Fatalf("unsafe FSM source path accepted: %v", err)
	}
}
