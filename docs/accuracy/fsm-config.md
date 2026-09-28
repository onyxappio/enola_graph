# Repository-configured FSM extraction

`state_machines:` in `enola.yaml` enables the TypeScript FSM extractor for
specific source declarations. There are no built-in Product names or
auto-detection by `Effect`, `send`, `MobileAppEvent`, or other familiar spelling.
The configured factory, interpreter and sink must resolve through repository
imports and re-exports before they can produce machine or dispatch facts.

Each entry has a stable repository-local `id`, an adapter, and repository-relative
source paths. Changes to this configuration participate in the extractor cache
key. The current profile supports two adapters.

## Rule-table adapter

The local state-machine kernel's `defineMachine` adapter reads literal
`stateTags`, `eventTags`, rule entries, initial state and command tags from the
configured registration. `commandTags` are read from the registration object,
not from `defineMachine` options. Registration command outcome mappings are
kept separate from code that handles a command.

```yaml
state_machines:
  - id: scan-provider-job
    adapter: rule_table
    file: packages/scan-engine/src/machines/providerJob.ts
    factory:
      module: packages/state-machine-kernel/src/defineMachine.ts
      export: defineMachine
    instance_export: providerJobMachine
    registration: createProviderJobRegistration
    state_type: ProviderJobState
    event_type: ProviderJobEvent
    command_type: ProviderJobCommand
    handler_files:
      - packages/scan-engine/src/providerJobRepository.ts
    dispatch_files:
      - services/workers/src/application/providerJobClaimStep.ts
    dispatch_sinks:
      - factory:
          module: packages/state-machine-runtime/src/step.ts
          export: step
        method: step
        machine_tag:
          module: services/workers/src/application/providerJobMachineRuntime.ts
          export: ProviderJobRuntime
        machine_argument: 0
        event_argument: 2
        event_path: event
```

`rules` may be a literal array or a configured helper call whose body exposes a
literal array/return array. A per-rule helper is expanded only when its local
template and every string argument can be established. A direct literal
`.push({...})` within the builder is conditional when guarded by source control
flow and declared when unconditional. Unsupported array entries and dynamic
push arguments are counted as unresolved; spreads, variables and dynamic rule
values are never guessed. One rule entry is one transition, and repeated rule
IDs retain separate nodes with an ordinal suffix.

## Reducer/interpreter adapter

The reducer adapter starts at a configured dispatcher and follows local calls
that pass its event parameter into another locally declared function. It reads
the configured state and event types, initial-state constructor, entry function,
transition helpers and optional settlement functions. Caller state and event
conditions constrain a called handler when the binding is direct.

```yaml
  - id: mobile-app
    adapter: reducer_interpreter
    file: apps/mobile/src/behavior/mobileAppInterpreter.ts
    dispatcher: sendMobileAppEventTransition
    start: startMobileAppInterpreter
    enter: enterState
    stay: stay
    reject: rejected
    entry: enterState
    state_type: MobileAppStateValue
    event_type: MobileAppEvent
    effect_type: MobileAppEffectRequest
    command_discriminant: type
    settlements:
      - settleMobileAppEffectTransition
      - settleMobileAppEffectError
    effect_runner:
      module: apps/mobile/src/effects/mobileAppServices.ts
      export: runMobileAppEffect
    handler_files:
      - apps/mobile/src/effects/mobileAppServices.ts
    dispatch_files:
      - apps/mobile/src/App.tsx
    dispatch_sinks:
      - factory:
          module: apps/mobile/src/effects/mobileAppRuntime.ts
          export: createMobileAppRuntime
        method: send
```

The mobile entry adapter follows `entryResult(snapshot, effects)` calls inside
the configured `entry` function. A command handler is proven only when the
configured runner resolves to that file, a runner parameter resolves to the
configured command type, and a switch/condition tests that typed parameter's
configured discriminant. Handler coverage is partial if commands are missing or
a default arm prevents an exhaustive source-level claim.

Dispatch sinks are restricted to configured files. The extractor follows the
configured runtime factory into a local `send` binding and typed forwarding
wrappers, or a configured machine tag and argument path for function sinks.
Event construction and sink dispatch are distinct. A call like
`sendUi(intentToEvent(intent))` is resolved only when the one imported callee's
literal event returns are all understood. A dynamic or otherwise unresolved
event value at a proven configured sink produces unknown-dispatch evidence. An
unrelated or unproven sink does not establish machine dispatch and does not
produce an unknown-event edge. A local reducer with a `send` method is not the
configured runtime and does not become a main-machine dispatch.

Construction facts belong to the source callee that creates the event. An
exported typed converter can contribute its literal returned event constructions
even when no current caller remains, when its return annotation resolves to the
configured machine event declaration. Imported type aliases are resolved to
their declaring module and export; a same-spelling local type is not sufficient.
An untyped converter is admitted only when a configured sink use proves it.

## Coverage and identity limits

Coverage facts report modeled and unresolved rule/interpreter branches, entry
sites, dispatches and handlers. An unresolved target or event remains unknown.
Consumers must require adequate coverage before asking whether a state is
unreachable, a command lacks a handler, or an event is undispatched.

Transition identities use the handler or rule identity, source state, trigger
set and destination; source byte offsets are excluded. Trivia changes do not
change identities. If two interpreter branches have the same structural key,
they get an ordinal suffix. Inserting or reordering indistinguishable branches
can move those suffixes; distinct source conditions and actions remain separate
facts with their own locations and evidence. Rule-table entries use the literal
rule id, with a suffix for duplicate ids. These structural limits are reported
rather than hidden behind byte-based IDs.

Type declarations and resolved callees are direct read dependencies of the
configured machine/dispatch contributions. Import, type, handler, return-value
and config changes therefore invalidate the relevant contributions; when
resolver evidence is incomplete, incremental planning may broaden the affected
file scope. FSM extraction adds no transitive state properties or caller
attributes. Cache records use extractor version v322 so v320 and v321 contributions
are recomputed under the revised return/type dependencies, file-owned coverage, source
binding and typed relation resolution.
