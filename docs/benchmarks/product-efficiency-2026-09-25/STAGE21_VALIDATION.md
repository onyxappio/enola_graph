# Stage 21 compact startup proof and same-transaction capture reuse

Status: experimental, not integrated, not performance accepted. Main remains 6db3ed834daab2adf8083ac46dc15ac21b670eb2 (runtime a1dec99). The full active goal is not complete.

## Change and contract

An optional save-time sidecar binds summary/plain one-shot no-op to exact committed state bytes and current semantic inputs, avoiding typed state decoding only when eligibility is proven. Full JSON and public resident sessions retain facts. Missing, corrupt, stale or unsupported proof falls back to normal materialization; pending recovery retains typed validation. The file remains read and SHA256-hashed: this does not eliminate startup I/O.

On proof refusal, reuse this transaction's captured inputs only when the hash target set computed with decoded prior records equals the captured set. Retained resident inputs/discovery refuse reuse. Removals/renames that widen targets keep two passes. Fast/resident semantics and closing source/config/policy fences remain unchanged.

Minified records with no stored Nuxt scope carry an explicit absence flag; records with a nonempty scope retain scope comparison. Exact owner membership/content and sidecar integrity remain checked.

## Independent evidence

- `capture-consumer-oracle.log`: 11 mutation cases, legacy versus candidate and candidate versus cold consumer graph, PASS29.407s on source3.
- `capture-race-root.log`: source/config/policy mutations during reused-capture run refuse without successful End or committed-state change; source/config retry succeeds, PASS3.672s without skips.
- `capture-reuse-root.log`: one-pass body edit, conservative widened-target refusal, no check on admitted proof, target-set comparison; PASS1.947s.
- `cli-output-check2/receipt.json`: real CLI plain, summary, full JSON and both flags; full data matches no-sidecar legacy delta. Initial Result.Facts has a pre-existing synthetic-module difference from no-op Result.Facts, so initial output was not used as an invalid delta oracle.
- `cli-lock-check/receipt.json`: package-lock add/edit, rename to yarn.lock and remove, all silent and proof-admitted. This fixture is not exhaustive lock-name coverage.
- `cli-pairs/correctness-candidate-2/pair-receipt.json`: seven actual Product NATS runs, all four checks pass, initial/body/structural graph equal cold; no-op zero parses/events/state or generation advancement. Correctness-only with concurrent tests.

## Diagnostic, not acceptance

Frozen source3 binary SHA2566553bf7c8c847b9eddb22ce4a63c65c96b4d1cf383c89b2d330c5c6bd51fc705; Product a609c19f3861971930fae7b33dcb2950598953c5 with TS and Product scope overlay. `product-proof-run3/root-review.json`:

| Operation | Wall seconds | Evidence |
|---|---:|---|
| Initial | 9.748 | 4034 parsed files |
| Fresh no-op A | 1.358 | proof admitted, silent |
| Fresh no-op B | 1.349 | proof admitted, silent |
| Body delta | 3.551 | 11 parses, one inventory/hash pass, captured inputs reused |

One competing workload sample; not a quiet paired comparison. Sidecar10966 bytes; build approximately19.7–19.8ms nested inside28–31ms production, promotion4–5ms. Nested times must not be added twice. No claim of near-zero fresh startup or completed performance target.

## Full package verification

Worker full graphsession package run passed490.348s: /tmp/s21-full-graphsession-1.log. This is operational test duration under shared load, not performance evidence. Full repository go test ./... passed with exit0: 110 passing packages and13 packages with no tests, independently matched to123 go-list packages (full-repo-output-audit.json). Graphsession re-executed in553.780s; no blanket zero-skip claim is made. Build and focused vet also passed.


## Completed history series

44 actual CLI calls across11 pinned revisions/10 transitions; all chain/cold and baseline/candidate cold hashes equal, all follow-up no-ops silent and state-preserving. Independent gates recomputed successfully. Three manifest-changing transitions; no tsconfig-changing transitions. This is correctness evidence, no historical timing claim.

| Target | Parsed files | Published owners |
|---|---:|---:|
| 07fb4a41 | 5 | 5 |
| fec1eac3 | 513 | 1149 |
| 9fc7ae5b | 25 | 1662 |
| 1c260747 | 431 | 554 |
| 599575d0 | 7 | 9 |
| ae233c5f | 92 | 345 |
| 4168360e | 111 | 150 |
| a6f1f3a9 | 80 | 80 |
| 5dfb2c8f | 43 | 126 |
| a609c19f | 37 | 46 |

Source: history/receipt-source3.json and history/root-summary.json. Broader scope does not mean all owners were reparsed.

## Supplemental evidence and remaining gates

- Supplemental config-add history passed: d0fbbf8→a2ac71a,6parses8published owners, cold/noop/source gates all pass. Preserve original ten-case-driver exit1 and use exact single-transition audit (history/config-add-independent-audit.json), with three negative audit controls rejected. This does not cover isolated existing-tsconfig mutation.
- Full final repository tests passed; integrated checks and normal push hooks remain.
- Watch ordinary Product regression passed both arms: watch-pairs/root-watch-review.json; 500ms window, cold equality, no idle/duplicate events or abandoned Begins,11parses each. Correctness only; not long-duration stress or timing acceptance.
- Freeze reviewed independently:16-file patch SHA2569029305036bfbd4152641e545af02381a60898bdd9b2ac95403cc34fab186b37; all10production files match tested source3 exactly;6test files included; scratch probes excluded. Pristine baseline apply check passed (freeze-root-review.json).
- Joint quiet measurements completed: six valid arms, three alternating pairs; results and integration decision below.
- Integration remains deferred because changed-file latency did not improve. Investigate overhead, preserve this cohort, then validate any revised candidate before integration and normal push hooks. Broader near-zero no-op and much faster delta target remains open.

## Complete quiet timing series: integration deferred

Three alternating pairs completed within September25 07:23–07:33UTC; all correctness, binary/input pins, no-op and cross-arm graph gates passed. No competing process samples. Time includes fresh CLI exit and producer acknowledgments; three pairs are descriptive, not a significance test.

| Scenario | Stage20 median (range), s | Stage21 median (range), s | Median change |
|---|---:|---:|---:|
| initial | 8.442 (8.308–8.565) | 8.477 (8.384–8.537) | +0.42% |
| noop | 1.665 (1.638–1.692) | 1.349 (1.348–1.360) | -18.96% |
| body | 3.449 (3.414–3.480) | 3.496 (3.475–3.499) | +1.36% |
| structural | 3.414 (3.412–3.427) | 3.509 (3.488–3.556) | +2.80% |

No-op median peak RSS290.19→161.22MB; initial RSS894.06→914.28MB, body634.22→632.78MB, structural611.88→571.05MB. RSS is not heap/allocation. Candidate body delta remains~41% of initial. All three body and structural paired comparisons are slower; structural ranges do not overlap. No-op win does not establish delta improvement. Integration deferred pending changed-file overhead investigation. Quiet holds released at07:29:59UTC. At the timing checkpoint the full allocation boundary on4105actual inputs was pending (now closed below); the earlier4086-target build+stage measurement excludes selections/promotion and must not be presented as total overhead.

## Archived evidence

The complete quiet cohort summary, six arm metrics/checks/receipts, pinned harness and checksums are under `stage21/quiet-source3/`. Paths inside original receipts retain their measured locations; copied scripts are evidence, not a portable rerun package. Large Product clones/state files remain in the original scratch tree. This archive does not include code integration or a main push.

Independent paired-work-counter audit: body, structural and no-op Stats, ParsedFiles, OwnersPublished, Invalidation and Fallbacks match exactly in all three pairs. Initial parses/reads/owner counts also match; summary_scans and derived_indexes vary between repeats/arms, while their combined count is3993 in every run. Thus the observed delta regression does not accompany broader reported extraction/invalidation work; its cause still needs profiling. Evidence: quiet-source3/paired-work-counter-review.json.

## Full proof allocation boundary closed

Root reviewed the scratch probe and logs under `stage21/quiet-source3/allocation/`: real inventory10965 names,4086owner records,4105hash targets, including19HTML paths absent from state. Setup policy/inventory/hashing and state decode are outside the timer. Whole commitProof includes both target selections, proof construction, staged write and promotion; 5iterations measured2,719,539B/op and29,062allocs/op (repeat2,719,507B/op). Selections allocate676,392B/op each; build1,340,972B/op, stage24,270B/op, promotion1,345B/op. Components are separate benchmarks and need not sum exactly. Whole uses the committed disposition; real published runs stage and promote later. These small shared-load runs characterize allocations, not accepted latency or the complete CLI overhead. Original partial4086-target numbers are superseded for this boundary. Frozen production is unchanged; causal delta profiling remains pending.

## Proof-production ablation: diagnostic only

Completed two frozen/ablated scenario pairs on byte-identical restored state and replayed broker seeds. Root independently checked successful exits, full results except RunID, wire/generation correlation, exact restored state manifests, binary hashes and equality with cold graphs: body11parses/11owners; structural12parses/12owners. Raw metrics, harness, comparator, diff and checksums are archived in `stage21/quiet-source3/ablation-v6/`.

Proof production/promotion marks disappear as expected; timed proof cleanup remains. Both arms still read state twice and decode it after rejecting the proof for changed inventory. This identifies work to compare against Stage20, not proven candidate-only overhead. Under44competing workload samples, phase-sum differences of23–24ms are not causal net wall savings and cannot rule out explanations for the quiet regression. No new performance acceptance claim; Stage20 comparison and a revised production proposal remain pending.

## Stage20 control establishes the additional read

The v7 cohort completed11CLI calls including Stage20, frozen and ablated deltas for both scenarios. Root independently verified all three arms started from identical state/store manifests and replay frames, had matching complete results except unique RunIDs, valid wire/generation correlation, pinned binaries and exact cold graph equality. All comparisons pass. The comparator now requires the entire three-arm/two-scenario cohort and exits nonzero for incomplete or failed cohorts; the missing-baseline negative control is retained. Evidence: `stage21/quiet-source3/ablation-v7/`.

In both scenarios, Stage20 reads state once and fingerprints twice (including the write), whereas frozen/ablated Stage21 read twice and fingerprint three times; every arm decodes once. This confirms additional changed-input startup work in Stage21. This single fixed-order cohort observed zero competing samples, but had no reserved quiet window or repeated alternating pairs; its durations do not establish accepted savings or fully explain the prior quiet regression. Next: bounded production fix proposal preserving state integrity/recovery and capture fences, then correctness validation and repeated quiet measurements.

## Revised candidate authorized: exact byte comparison

Decision msg_9d1aa5074280 authorizes an isolated source3-based implementation in `/tmp/enola-stage21-retain/src`: retain the first state buffer, reread committed state at materialization and compare exact bytes, then decode with the already-bound fingerprint. This removes a second SHA256, not the rewrite-detection read. Stat-only/conditional rewrite checks and reverting the no-op decode benefit were rejected. Original freeze remains intact.

Acceptance remains pending: same-size/same-mtime rewrite, removal/truncation/replacement, retry/Close buffer lifetime, cold/capture/recovery regressions, memory and repeated timing. Both buffers coexist during comparison; dropping references does not imply immediate garbage collection. Proof-fsync changes are outside this candidate.

## Retained-byte candidate review and focused checks

Final candidate changes three production files plus one test file relative to frozen source3. Exact byte comparison and missing-buffer fingerprint fallback remain; transaction-wide exit and Close release the retained buffer. Root verified all four manifest hashes before and after building `enola-retain-final1` (Go1.27.1, SHA256d3229b4a5d622d1e9986565782a8c0cab7c078821775d361cb85809927c0aef6).

Eight named tests (11leaf cases) passed4.172s with no skips. Coverage includes valid same-length/restored-mtime rewrite refusal, no publication or generation advancement, restored-state retry, no-op admission followed by edit, early failure/retry, Close, exact counters and comparison buffer non-mutation. Memory reclamation and RSS are not proven by these tests.

First broad run failed477.805s in the new profile-count test: openSession was outside the capture window, so its first read was absent. The corrected test captures opening and distinguishes read-side from write-side fingerprints. Earlier23.123s selected-test coverage remains unresolved and is superseded by named final results. The failure log is retained. Final-source graphsession suite passed500.393s; root reverified all four source-manifest hashes after completion. This is regression-test duration, not Product analysis latency. Product correctness/profile/RSS and repeated quiet acceptance remain pending. Evidence: `stage21/quiet-source3/retain-candidate/`.


## Remaining performance boundary after the retained-byte fix

Removing a duplicate fingerprint is a bounded repair to Stage21 overhead, not proof of the broader near-zero no-op and much faster delta objective. The existing v7 diagnostic still shows larger work boundaries on both changed-file scenarios: frozen preview 0.691/0.745s, pending-state write 0.502/0.449s, graph-input build 0.316/0.342s, and typed-state decode 0.241/0.251s (body/structural). These are single-cohort instrumented observations, not accepted timing or additive costs; several marks contain or overlap others. Source: `stage21/quiet-source3/ablation-v7/ablation-comparison.json` (original `/tmp/enola-stage21-ablate/work5/ablation-comparison.json`).

First finish the retained-candidate cohort and fresh no-op RSS/silence checks, then repeat quiet end-to-end comparisons before deciding integration. Subsequent profiling should select the largest remaining avoidable boundary from that final candidate, compare it with prior stages, and distinguish fresh CLI from resident sessions. Any persistence optimization must preserve replay/recovery and acknowledgment completion; any preview/resolution reuse must preserve frozen owner scope and exact cold equality. This records investigation priorities, not an implementation authorization or a claimed saving.


## Retained-byte Product diagnostic completed

The three-arm cohort completed all11CLI calls and both scenarios passed every comparator gate: exact cold graph equality, byte-identical restored seeds, replay and wire correlation, full result equality except RunID, proof production/promotion, and complete unique measured arms. Retain reads twice, fingerprints twice (one read and one write), compares bytes once and decodes once; frozen reads twice and fingerprints three times. Root inspected the comparator output and source. Raw evidence is archived under `retain-candidate/cohort8/`; final independent raw-evidence audit remains pending.

Single diagnostic wall observations (frozen / retain / Stage20), seconds: body3.4023 /4.2254 /3.3255; structural3.4373 /3.3447 /3.3538. Retain peak RSS exceeds frozen by43.43MB and43.83MB respectively. Recognized competing-process samples were zero, but unrelated headless browsers were present and the detector does not cover all host load. This is not quiet acceptance. The body outlier cannot be dismissed as host noise: process real4.22s versus CLI traced total3.310s leaves about0.91s outside that trace, whose cause is unresolved. The removed fingerprint (~24ms) and added byte comparison (~2ms) are local phase observations, not proven net end-to-end savings.

The separate four-call profile passed all16verdict checks. Both fresh no-ops have zero parses/events and unchanged generation/state. Diagnostic walls: initial8.7769s, no-op1.2917/1.2817s, body3.3677s; RSS833.86MB,186.55/186.14MB,596.59MB. These are neither paired quiet timings nor a whole-goal acceptance. Evidence: `retain-candidate/profile1/`. Production integration remains deferred pending attribution, memory assessment and repeated acceptance measurements.

## Acceptance harness ready; timing deferred at 09:59 UTC

Root independently verified all16 pinned files/binaries and ran both arms in preflight-only mode (exit0, clean pinned Product). The copied scenario runner, validators, alternating order and summary reducer are unchanged; only shared bounded host sampling was added to the arm runner. Sampling errors or no host samples make an arm ineligible. The harness and review are archived in `retain-candidate/acceptance-harness/`. The 09:59–10:14UTC proposal was deferred because the accuracy coordinator explicitly withheld GO pending its live worker idle acknowledgment; no timing run was started.

Lightweight source review identifies two concrete later profiling boundaries, not proven speedups: `prepareFrozenTS` calls `ExtractSession` inside its dependency-closure loop and can repeat derived work as the closure grows; `write_pending_state` starts at the preceding `revalidate_ts_records` mark and includes the closing config/input fences, full-state JSON marshaling/durable write and `commitProof`; its interval is not merely persistence or disk latency. Any next optimization must measure those substeps separately, reuse only semantically valid records, freeze complete owners before Begin, and retain the pending-state/acknowledged-End promotion barrier. Whole-state persistence redesign is not justified by the aggregate timing alone.

Root checked the actual retained-candidate body trace (`work8/body-retain.log`): preview0.700s contains two TS sessions (1then10parses), each running the GraphQL/gRPC index interval over4086files (0.096/0.100s); the second map/aggregate interval is0.297s with117summary scans. The prepared result is reused after Begin (`ts_extract_session` rounds to0.000s). The0.392s `write_pending_state` interval includes0.132s of closing `ts_config_inputs` before0.166s JSON marshaling, plus write/fingerprint/proof work. These are diagnostic attribution boundaries, not accepted wall-time savings. Repeated derived work warrants profiling before any cache change; semantics of side reads and cross-file resolution must remain proven.

The same two TS index/map/compose passes occur in all six measured diagnostic cells (Stage20, frozen Stage21, retained Stage21 × body/structural), and every cell runs `ts_config_inputs` twice. Raw matching trace lines are retained in `retain-candidate/cohort8/root-phase-attribution.json`. Thus this is a shared optimization boundary, not evidence explaining candidate-only regression.

## Retained candidate: three alternating pairs completed, integration deferred

All six arms completed exit0 within the coordinated10:12–10:27UTC window (actual series10:14–10:21); 42CLI calls including cold references passed complete-cohort graph, replay and no-op gates. Zero recognized competing samples and zero sampling errors across145whole-host samples. The machine was not fully isolated: OS/app/VM background activity remained, including mds_stores peak131.1%CPU in baseline1; its presence does not establish a causal timing effect.

|Fresh CLI scenario|Stage20 median(range), seconds|Retained Stage21 median(range), seconds|Median change|
|---|---|---|---|
|Initial|8.589(8.586–8.811)|8.636(8.538–8.832)|+0.5%|
|No-op|1.682(1.676–1.760)|1.367(1.363–1.371)|−18.7%|
|Body delta|3.401(3.384–3.401)|3.456(3.430–3.463)|+1.6%|
|Structural delta|3.427(3.392–3.474)|3.463(3.447–3.474)|+1.0%|

Body delta is slower in every pair with nonoverlapping ranges; structural slower in every pair but ranges overlap. Candidate delta remains about40%of its own initial. No-op RSS median285.57→177.68MB; body596.57→617.07MB and structural631.57→654.44MB, with wide RSS spread (full series retained). The earlier body outlier did not recur; its cause remains unresolved.

Decision: no performance acceptance or main integration. Preserve the no-op result as evidence, but broader delta/no-op targets remain open. Next investigation is repeated per-hop derived work, with scratch instrumentation and correctness before any new production candidate. Evidence, raw metrics, host samples and checksums: `retain-candidate/paired-timing/`.
