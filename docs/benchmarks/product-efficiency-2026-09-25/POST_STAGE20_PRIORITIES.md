# Remaining startup work after Stage20: evidence and next investigation

Status: Stage20 integrated and pushed in `a1dec99` after three paired CLI measurements and full affected-package tests. The next startup investigation is diagnostic only; no new optimization is implemented by this note.

## Evidence

The prior instrumented Stage15 baseline [no-op trace](../product-efficiency-2026-09-24/stage16-phase-memory/baseline/r1-new-noop.phases.log) reports 1.817 s CLI completion with zero parses. It ran under shared load with memory instrumentation; durations locate work, not a current performance claim. Nested marks must not be summed.

| Observed operation | Trace duration | Work reported |
|---|---:|---|
| Graph input policy build | 0.382 s | Git membership, ignore semantics and tree walk |
| Load complete state | 0.287 s | 56,633,538 bytes; includes 0.248 s JSON decode |
| Inventory projection | 0.125 s | 10,965 files/names |
| Content input hashing | 0.163 s | 4,105 targets; about 45.8 MB hashed |
| TS configuration inputs | 0.168 s then 0.162 s | Two separate observations, 83 inputs each |
| TS discovery build | 0.242 s | 1,965 side reads, 1,977 stat reads, 655 walked directories |
| TS cache reuse | 0.039 s | 4,086 owners and 59,661 facts |

Current source confirms OpenSession reads and decodes full committed state before reconciliation (`session.go`, `persist.go:readStateFileFP`). `writePendingStateFP` marshals and durably replaces the complete state on a changed run. Stage20 changes owner/index construction, not these startup/persistence paths. Removing only JSON decode cannot remove all observed startup work; no numerical projected speedup is claimed.

## Next investigation

1. Profile exact integrated Stage20 source `a1dec99`; do not treat older mark durations as its cost distribution. Its accepted body/structural delta gains did not improve fresh no-op (~1.70 s). The three-pair evidence is in [Stage20 validation](STAGE20_VALIDATION.md).
2. Prefer a compact, separately verifiable input/context summary only if it can prove no-change without materializing all graph facts. It must cover repository membership, policy and Git controls, source content, semantic side inputs and resolver discovery. A path/mtime-only shortcut is insufficient. Unknown or changed evidence must fall back to full reconciliation.
3. Keep fresh CLI and resident-session optimization separate. An existing resident may reuse proven state; a fresh CLI still needs a sound observation of current inputs, so a near-zero fresh no-op target needs measurement and a clearly defined correctness argument, not a daemon benchmark.
4. Evaluate state representation/decode and write costs independently of the no-op proof. The prior roughly 390 MB allocation interval before pending-state completion includes multiple operations and is not evidence that serialization alone allocates that amount.

Any proposal must preserve replay before use, identity/protocol binding, pending-state promotion only after acknowledged End, corruption detection, atomic durable checkpoints, zero unchanged events/parses/generation advancement and exact fresh-versus-delta graph equivalence. Schema/cache migration and interrupted-write tests are required if persistence changes. No production implementation has been authorized by this note alone; the existing worker is preparing a bounded design proposal while the candidate remains frozen.

## First design review

Worker proposal `/tmp/enola-stage20-resolution/DESIGN-startup-noop.md` (first revision SHA256 `b7188a9ed494ae82ae1b11f035edb186572a148cba26e6a9e384298647937029`) is not accepted for implementation. Root requested these corrections:

- Header-only pending-state triage changes corruption behavior even if promotion retains full decoding: today an invalid typed body errors before the incomplete, wrong-identity or unacknowledged-End fallback. Silently ignoring that body would weaken recovery validation.
- Avoiding summary-only fact cloning overlaps the already parked Stage19 experiment and must account for its inconsistent latency gains and RSS signal.
- Earlier task-local restrictions must not be promoted into a claim that the overall near-zero no-op goal is impossible. Any persistence proposal still needs sound fresh content/membership observations, type validation and recovery semantics.
- An arbitrary threefold microbenchmark threshold cannot refute every useful state-decoding change; acceptance is end-to-end evidence under the original correctness contract.

The revised investigation should focus on avoiding duplicate resolver/config computations from freshly validated inputs. No code change, new runtime benchmark or production integration followed this design review.

## Revised design direction

[Worker revision 2](stage20/startup-design-review2.md) withdraws header-only recovery triage and the duplicate Stage19 proposal. It proposes reusing the TS-config walk's own output within one precisely bounded observation, without substituting the engine-pruned inventory or reusing observations across the final independent input-change fence. This is a design lead, not implementation approval or measured speedup. Root requires explicit lifetime/identity rules and tests for newly added alias roots and extends targets, mixed profiles, neutral refresh and scope isolation before accepting the approach.

## Independent experiment boundary

After the second timing cancellation, the existing worker was assigned an independent scratch prototype from production-equivalent main, without modifying Stage20. Start inside one `tsConfigInputs` invocation; keep the later fence entirely fresh and defer cross-phase Discovery sharing. Root source review found that `overlayWalkDir` delegates to `inputScope.WalkDir`, while alias-root recursion uses `overlayReadDir`; caching only the latter would not remove the second filesystem traversal. The experiment must measure underlying enumeration calls and preserve differing pruning/probe semantics, not report memo hits as eliminated work.

## September 25 review correction: overlap with Stage16

The prototype at `0b05b9b91bc0550282993075df2f984d4eb0bbe9` removes the same duplicate enumeration already targeted by the unmerged Stage16 candidate. It is not a new independent optimization stage. Root identified this overlap while comparing the actual frozen Stage16 source with the prototype and requested a differential assessment before further implementation or performance runs. The earlier assignment failed to account for this existing work.

There are implementation differences requiring review: Stage16 collects configuration candidates during alias recursion and performs extends reads afterwards, preserving the original read order; the prototype invokes alias reads during WalkDir and interleaves them with configuration reads. Stage16 also retains explicit probe/error fallbacks. Passing static fixtures does not establish equivalence for changing inputs or replace those safeguards.

Root independently passed all 13 characterization fixtures, without skips, on the prototype, the prototype with the production Scope implementation overlaid to remove diagnostic counters, and the original baseline source. The checked outputs include candidate names, alias roots and recorded reads/stats/directory membership. Logs and source pins are in [config-enum-review](config-enum-review/oracle-review-receipt.json). These are shared-load correctness checks, not CLI performance measurements or full graph acceptance. The Go package durations were 0.605 s, 0.521 s and 0.523 s respectively; consult the logs for the authoritative duration of each run.

Retain useful additional fixtures, but do not count the duplicate prototype as extra speedup or discard Stage16's unresolved RSS evidence. The worker must identify a distinct justified improvement or consolidate the coverage into the existing Stage16 work. Stage20 remains frozen and awaits its own measurements.


### Overlay correction and direct Stage16 coverage

The first source-replacement overlays used `/tmp` keys, whereas Go resolved the package under `/private/tmp`; those attempts did not prove replacement-source execution. They are retained as superseded evidence. Root corrected the keys to canonical paths and repeated the baseline and counter-free candidate checks: each explicitly ran and passed all 13 fixtures, without skips. The receipt and logs above now reference those corrected runs.

Applying the oracle to frozen Stage16 first discovered no tests for the same path reason; those logs are retained as invalid attempts. The corrected overlay explicitly ran 26 successful subtests: 13 with probe observations and 13 with no probe, checking production-path inputs against the same baseline goldens. The distinction matters because Stage16 deliberately declines fusion when a probe is present. The package reported 0.753 s under shared load; this is not a speed comparison. These cases confirm useful coverage for the existing Stage16 implementation, rather than a demonstrated new capability of the duplicate prototype.


## Final disposition of duplicate prototype

The prototype is retired; the [final comparison](config-enum-review/CONFIG_ENUM_COMPARISON.md) records the read-order difference and preserves the limits of its measurements. Carry forward only the [permanent Stage16 coverage patch](config-enum-review/patch02-stage16-permanent-coverage.patch), SHA256 `75ab2f134ec1602960b2718a720b2de3f4746c7c10533135142f7085130a1e16`: 13 unchanged baseline goldens, probe/no-probe assertions and a corrected Lstat cost comment. It is archived here, not applied to production.

Root independently verified clean `git apply --check` against frozen Stage16, six unchanged existing source/test files, comment-only changes in the seventh, and all 13 unchanged goldens. The focused log contains 17 passing top-level tests and 26 passing subtests with no skips or failures. Root also ran the original `oracle-refresh.py --check` in the coverage tree, exit 0: the generated oracle matches a verbatim lift of `49b486b`. The final comment-only revision does not change assertions; the worker additionally reports build and vet passing. Stage16 integration and its RSS review remain outstanding. Stage20 timing and integration subsequently completed; see STAGE20_VALIDATION.md.

## Post-integration no-op review boundaries

Current `session.go` reaches the no-publication return only after input observation and extraction decisions. The return path separately checks analysis configuration, effective inputs and policy bookkeeping. Proven-neutral configuration, membership or non-TS changes may update stored observations while preserving generation and published facts; a fast no-op must not suppress those necessary refreshes. Unsupported alias projections, forced initial runs, Angular composition and unknown extractors retain conservative handling. Existing callers may consume returned facts, so avoiding their materialization needs an explicit compatible API boundary and must not silently repeat the parked Stage19 experiment.

Current `persist.go` fully reads and typed-decodes pending state before testing completeness, identity or acknowledged End. A header-only fallback would weaken corruption handling. A potential compact input proof must bind the exact validated committed bytes, schema/extractor/profile and dispatch/protocol identities; absent, stale or invalid proof must take the existing safe path. Fresh content, membership, side-input and discovery observations remain necessary, including inputs absent from the previous inventory. This is a feasibility constraint, not approval of a particular persistence design.

The existing worker has a bounded diagnostic assignment on `a1dec99`: one initial setup, two no-op observations and one body-edit/cold check on pinned Product and the Stage20 scope. Shared-load phase durations locate work only and must not be reported as performance acceptance. Implementation follows evidence review; frozen benchmark inputs and prior candidates remain unchanged.

### Existing regression anchors for a future startup fast path

| Contract | Existing regression anchor | Implication for any new proof |
|---|---|---|
| Fresh no-op returns facts and leaves state bytes unchanged | `TestFreshNoopRetainsResultFacts` in `noop_result_contract_test.go` | Preserve the default full-result API; a summary-only optimization cannot silently replace it. |
| Summary reconcile preserves resident snapshot | `TestReconciledNoopRetainsResidentSnapshot` | A nil summary result does not authorize discarding resident facts. |
| Equal-length corrupt committed state is rejected | `TestRecoveryRejectsCorruptCommittedStateAtEqualLength` | Metadata equality cannot establish valid state. |
| Missing committed state wins over cached knowledge | `TestRecoveryReportsMissingCommittedStateOverCheckpoint` | A detached proof cannot invent a missing generation. |
| Fresh process without a checkpoint decodes | `TestRecoveryWithoutCheckpointDecodes` | Keep the existing no-proof fallback; a new proof needs independent validation and negative controls. |
| Promotion requires acknowledged End | `TestRecoverPendingRequiresAckedEnd`, `TestRecoveryNeverServesCheckpointOverPromotedPending` | Replay and promotion ordering remain authoritative. |
| Neutral observations commit under an input fence | `TestNeutralObservationCommitsOnlyUnderTheCapturedInputFence` | Silence is not permission to persist stale bookkeeping. |
| Unknown alias metadata is not interpreted as no-change | `TestAliasStateUnknownMetadataIsRefusedNotGuessed` | Version mismatch must trigger reconciliation, not bypass it. |

These anchors identify assertions to preserve, not evidence that a future fast path already passes them. New proof corruption/staleness and missed-input cases must be selected from the eventual implementation.


### Stage21 design and harness review checkpoint

The diagnostic draft targets committed-state JSON materialization, which is distinct from Stage19's deferred cloning after eager decode. This distinction keeps the design open, but does not establish feasibility: a summary derived by decoding the complete state on every startup cannot remove that same decode. A proposed persisted proof must describe its production, exact-byte and identity/version binding, atomic lifecycle and fallback, plus the compact observations that permit a no-change decision without accessing full file records. Changed inputs, neutral bookkeeping refresh and full-result consumers still need explicitly correct materialization paths. No implementation is approved at this checkpoint.

The first retained-stream replay attempt used the existing observer's `DeliverNewPolicy`, so its empty output cannot establish whether historical delta publication succeeded. The worker has been directed to use a separate, pinned replay observer with `DeliverAllPolicy`, preserving validation and normalization, and to consume through generation 2 End before comparing the applied graph with the fresh cold result. Cache metadata differences and whole-state byte comparisons must remain separate from this published-graph check. The corrected replay subsequently applied generations 1 and 2 and matched the cold graph; see STAGE21_DIAGNOSTIC.md. This closes the single observed body-edit oracle gap, not wider optimization acceptance.


Concrete access boundary from current source: `readRuntimeInputs` passes `st.Files` to `filesToHash`, but that helper uses only the previous file keys, so a validated compact key set could serve this particular dependency. Later `session.run` directly inspects `FileState.TS` while counting added/removed sources, then assembles complete `newFiles` before its current no-publication return. A lazy decoder inserted only at that return would already be too late. The design must place an independently proven unchanged-input decision before those record consumers, or document their compact equivalents; it must not supply incomplete fake `FileState` values to the existing path. This is a source-level feasibility finding, not implementation acceptance.
