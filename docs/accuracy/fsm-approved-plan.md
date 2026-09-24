# Approved finite-state-machine graph work

Status: approved for implementation, not implemented or validated.
Approval: Telegram enola-2, “Схвалити FSM-схему”, 2026-09-24.

## Approved scope

- Five new semantic fact kinds: `fsm_machine`, `fsm_state`, `fsm_event`,
  `fsm_transition`, `fsm_command`.
- A shared model with repository-configured adapters for the backend rule-table
  machines (Effect-tagged types and the local state-machine kernel) and custom
  mobile interpreter. Effect usage alone does not identify a machine.
- Direct transition links to source state, target state, triggering events,
  commands/effects, and code implementing guards and actions. Include nested
  states, entry effects, outcomes, and proven handler bindings.
- One decision branch is one transition. An OR trigger can link one transition
  to multiple events. Separate branches retain distinct identities, guards,
  actions and ordering; never merge solely on matching from/event/to.
- Event construction and actual dispatch are distinct relations. Dispatch needs
  proven binding to the configured machine sink; tag spelling alone is insufficient.
- Resolve possible literal event returns of one resolved callee for forms such as
  `sendUi(intentToEvent(...))`. Unproven values remain explicitly unknown.
- Preserve unknown/partial coverage. Reachability and absent-handler judgments
  belong to graph queries and require sufficient extraction coverage; unknown
  extraction is not dead-code evidence.

## Concrete grounding

Backend: `packages/scan-engine/src/machines/providerJob.ts`, `queued-claim`:
Queued + ClaimRequested -> Running, guarded by payload/due checks, emitting
CallProvider. Mobile: `mobileAppInterpreter.ts`, forgot-password submit/resend
branch guarded by canSubmitResetRequest -> requestingReset; entry emits
requestPasswordReset. Both are at Product revision
`a609c19f3861971930fae7b33dcb2950598953c5`.

## Delivery and acceptance

Implement in isolated Orca accuracy work with GPT Luna xhigh, primary review and
source-grounded tests. Keep workers off Telegram. Stabilize wave14 first, then
implement this approved feature without mixing unfinished ordinary candidates.

Read existing graph/schema and frozen file-owner contracts before coding.
Use structural identities, exact source ownership, registered read dependencies,
and current conservative invalidation. Do not promise one-file parsing or add
transitive graph facts. Validate cold/delta equality, silent no-change, migration,
rename/delete/config changes, source binding negatives and measured performance.
Update the cumulative annotated offline HTML report and publish/synchronize main
only after acceptance. Report partial extraction honestly; an initial subset is
not completion of the approved backend-and-mobile scope.

The research draft proposes details; it is not evidence that the implementation
or coverage exists. Context-encoded sub-runtimes, specification-conformance
analysis and other extensions beyond this boundary still require user approval.
