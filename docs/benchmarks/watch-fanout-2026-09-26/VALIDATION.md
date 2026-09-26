# Experimental changed-owner scope and quiet watch

Status: experimental candidate branch; no Product speed acceptance and no publication to main.
Base: 674cf84dadc07e745f502eb0d004a877feb6e02b.

## Correctness evidence

- Existing selective suite for changed-owner filtering, parse-reason accounting and default frozen Begin ordering passed (graphsession 3.406 s).
- `TestChangedOwnerScopeMigratesLegacyAndRenames` passed (package 4.549 s): legacy fingerprints are unknown and publish conservatively; subsequent comment-only edit announces zero owners; identical inputs publish nothing and do not advance generation; rename plus rewritten importer includes removed/new/importer contributions. Applied graph equals fresh analysis after each edit.
- Negative control: same migration test with ChangedOwnersOnly disabled through a Go overlay failed as expected: `neutral edit published 1 owners` (package 2.834 s). Source runtime was unchanged for this control.
- `go test ./internal/graphsession -run '^TestWatchQuiet' -count=1` passed (package 0.751 s): quiet-window reset, fixed maximum wait under continuous saves, cancellation, and collection of repeated/multiple saves during active apply into the following batch with contiguous sequence coverage.
- `git diff --check` passed.

These durations are test package runtimes, not Product latency measurements. The three-package full test command was still live at this checkpoint; new migration and apply-boundary tests were added after its compilation and are covered by the separate commands above.

## Remaining acceptance

Sink failure/replay/state promotion and profile/config transition coverage; Product frozen-scope and exact cold equality replay; actual bytes/owners/parses and repeated latency/memory measurements; delete/rename semantic-context profiling and safe reparse reduction; complete repository validation and cumulative report before publication.

Publication filtering alone does not reduce analysis scope or prove a faster delta. The experimental option defers delta Begin until resolved contributions are known; default authoritative behavior retains Begin before analysis. Graph-neutral changed inputs may emit an empty generation; identical-input no-op must remain completely silent.

## Delivery and package checkpoint

- `TestChangedOwnerScopeEndFailureKeepsConfirmedDigests` passed (package 2.414 s): injected End delivery failure leaves confirmed state bytes unchanged and pending checkpoint present; retry replays the identical End payload/ID; applied graph equals cold; subsequent no-op remains silent.
- Three-package full command completed exit 0: graphsession 541.962 s, graphstream 24.545 s, command 8.110 s. Migration, apply-boundary, and End-failure tests added during that run were validated separately as noted above.
- Experimental binary built successfully at `/tmp/enola-watch-fanout-experimental` for the Product correctness/fanout diagnostic. This is not an acceptance timing run.

## Parse-reason correction and Product diagnostic

The deletion preview labeled every unchanged seed `file semantic context`, including reverse-closed dependencies. The archived Product deletion had ContextAffectedSources=0 but 2,406 parses with that label. This is misleading attribution, not evidence that every file's configuration changed. The candidate now preserves explicit semantic-context and changed-import-resolution seed reasons and labels other preclosed seeds `dependency invalidation`. Planning and parse membership are unchanged by this reporting fix.

`go test ./internal/graphsession -run '^Test(DeleteParseReasons|ChangedOwnerScope|StreamReportsActualParseReason)' -count=1` passed (6.337 s). The new delete fixture verifies no false semantic-context claim in Result or End and exact cold graph equality.

Product diagnostic binary SHA-256: `f0d99f47b80f5d98133763e4f1ee58904fbe0f79fff5ff380ac0eceaa2888d86`. It predates the reporting-only correction above. Source fixture tree `ef25879298f8dfa14e2bc2f70f6a26a5aad46359` was copied cleanly to an isolated candidate-product directory.

First harness attempt: initial full-field cold parity passed; harness then failed with KeyError on omitted empty owner_scope for a graph-neutral literal change. Evidence retained in candidate-results. Corrected harness interprets absent/null scope as empty, consistent with protocol omission, and starts a fresh independent chain in candidate-results-v2. No runtime fix was needed for this harness failure. The replacement harness remains a diagnostic, not a quiet performance acceptance series.

Negative control for reason attribution restored the old seed label via overlay and failed as intended (`dependency work mislabeled: map[file semantic context:2]`, package 2.124 s).

First completed Product delta in v2: literal edit publishes zero owners/batches/facts, 1,197 event bytes (Begin/End); exact full-field cold parity passes. Prior same-fixture baseline published 344 unchanged owner contributions. This proves wire reduction for this scenario, not timing acceptance or reduced parsing.

## Direct-reader delete seed experiment

The delete path previously reverse-closed all dependents before the preview could compare observed surfaces. Both the preview seed and extraction invalidator now seed direct imports/side readers, then use the existing fixed-point surface, name-resolution and side-read checks to grow work. The file-owner replacement planner remains conservative. No new durable relationship index is introduced.

`TestDeleteKeepsUnchangedTransitiveConsumerCached` passed (1.578 s): deleting a source reparses its one direct importer but reuses an unchanged transitive consumer, with exact cold graph equality. Reinstating the previous reverse-closure seed through an overlay makes this test fail (two parses; package 1.293 s). Expanded deletion/rename/membership/frozen/rebind/side-read regression command is running; this experiment is not yet accepted or present in the running Product binary.

Additional Product v2 wire results (binary before reason/delete-seed changes), all exact full-field cold parity passed: new export 1 owner/4,437 bytes; add barrel entry and source 2 owners/19,245 bytes; remove barrel entry 1 owner/17,642 bytes; delete unreferenced source 1 empty replacement/1,272 bytes.

State storage snapshot after these cases: 6,301 digest entries, compact JSON 898,250 bytes (~0.857 MiB); complete state file 70,755,436 bytes. This is serialized storage, not resident or peak-memory measurement. Keys include owner identities; digests do not encode adjacency lists or version history.

## Completed Product wire diagnostic

All 10 rows completed, nine exact cold comparisons passed, final no-op parsed/published zero and retained state bytes/generation. The independently compiled reference consumer replay passed all ten rows too; archived results.json and reference-consumer-audit.json accompany this report.

Same-fixture wire comparison: export 346 -> 1 owner and 10,222,709 -> 4,437 bytes; deletion while still re-exported 2,407 -> 2 owners and 40,404,846 -> 18,371 bytes. Deletion still parsed 2,406 files in this binary: no parse-speed claim.

Expanded direct-reader experiment initially failed two existing regressions (168.147 s): rename conservative scope lost top.ts and Nuxt composition fallback was absent. Fixed by retaining reverse-closed owner scope independently of parse scope, plus conservative reached framework/nonreplayable parse seeds. Both regressions and the one-direct-reader reduction test now pass together (2.413 s). A new complete three-package test run is live; no acceptance yet for this source revision.

## Comparison and digest checks

`wire-comparison.json` records completed baseline/candidate scenario counters using compare.py, which rejects incomplete chains, failed cold parity or non-silent no-op. It deliberately excludes timing acceptance claims.

`TestOwnerDigestPreservesWireDifferences` passed (0.468 s): properties, source location, resolved target/status, occurrence and multiplicity affect the digest; record ordering alone does not. A negative-control overlay stripping properties failed specifically on the property case (0.494 s).

Added an optional `ENOLA_GRAPH_PROFILE` phase marker `compare_owner_contributions` to separate fingerprint comparison from late Begin publication. This instrumentation line was added after the current full package run and v3 binary compilation; their evidence applies to the prior runtime without that marker. No extraction/planning behavior changed with the marker.

## Source review and mode-transition coverage

Retained Astra reviewer performed source-only review (no heavy commands or edits): no confirmed blocker in digest recovery or narrowed deletion planning; requested mode ON/OFF/ON and framework deletion parity coverage. Empty changed-input graph-neutral generations remain an explicitly documented design limitation, not an identical-input no-op claim.

`TestChangedOwnerModeToggleDiscardsStaleDigests` passed (2.192 s): enabled initial -> disabled publication of an added export -> enabled restoration of original source. The disabled publication clears fingerprint proof, so the restored graph is not incorrectly suppressed against an obsolete digest; cold equality and subsequent silent no-op pass.

Profile-marker smoke check with ENOLA_GRAPH_PROFILE=1 and TestChangedOwnerScopeSkipsIdenticalDependents passed (5.065 s); compare_owner_contributions is emitted for delta comparison. The rounded 0.000 s fixture values are not a Product cost bound.

New Product direct-reader binary SHA-256: f11cf44bb97caf2f0c7a910d588baf61fda2a48f1fc8ac4df1e4844142eed26e. It excludes the later profiling marker and added tests; functional extraction/delivery source corresponds to the full package run currently in progress.

## Direct-reader Product checkpoint

The v3 re-exported deletion passed full-field cold equality with 346 parses instead of v2's 2,406 (85.6% fewer). Reasons: 1 changed import resolution, 343 dependency invalidation, 2 subsequent resolution. Published scope stays two owners. Observed single-run times (17.17 s vs 15.04 s) are under concurrent correctness workloads, not accepted speed evidence.

The revised complete package run passed: graphsession 496.484 s, graphstream 25.208 s, command 7.772 s. Later tests/trace marker have their separate results above. Full `go test ./...` is now running against current source; v3 awaits final no-op and independent consumer replay before completion.

## V3 complete and profiling preparation

V3 completed all ten scenarios; independent reference consumer replay passed (15.53 s), including nine cold graph comparisons and final unchanged-input no-op. Complete results and comparison are archived in direct-readers/.

Implementation checkpoint committed locally as 3599a8f on experimental branch; no main publication. Full repository suite remains live. Built /tmp/enola-watch-fanout-3599a8f from the committed source and started a separate initial/no-op/export profile with ENOLA_GRAPH_PROFILE=1 plus /usr/bin/time -l (peak RSS). This run is diagnostic under shared host load, not a performance acceptance cohort. It uses the same pinned isolated Product fixture, a fresh state, and restores source in finally.

## Profile of committed source (diagnostic only)

Archived profile-3599a8f includes traces, summaries and binary-pinned receipt for initial/no-op/export. No-op preserved state bytes, event size and zero parses. Fresh CLI no-op 8.259 s (shared load): input discovery 1.612 s, extractor detection 1.183 s, configuration input scans about 0.95 s twice, graph-input policy build 1.041 s; trace groups nest and must not be summed indiscriminately. This is not resident no-op latency and is far from the target.

Export comparison of encoded owner contributions took 0.078 s; frozen preview 2.187 s; pending-state write 1.671 s. Peak process RSS: no-op 397,426,688 bytes, export 931,790,848 bytes. These are single-run absolute RSS values for the whole process, not attributed fingerprint memory or a baseline/candidate regression comparison. Prior Stage23/24 configuration/discovery experiments remain relevant; they are not incorporated into this branch or performance-accepted yet.

## Stage23/24 combined overlay diagnostic

Verified retained Stage23 session.go and Stage24 ts.go hashes match prior acceptance preparation. Remapped a frozen copy onto 3599a8f with three focused test files. The previously unrerun visitor fixture failed because expected root directories were absolute while the reference contract uses relative paths (root is empty string); corrected only that expectation, retaining exact callback/alias/ledger comparisons and real alias-root count checks. Focused suite then passed (1.379 s). Archived runtime patch and test sources under stage24-combined/; implementation remains overlay-only.

Same target/state sequential diagnostic no-op pair: baseline 7.672 s, combined 7.300 s, both zero parsed/events and byte-identical state. Two config enumeration phases: 0.909/0.852 s -> 0.348/0.369 s; discovery 1.666 -> 1.443 s. RSS 393,969,664 -> 395,575,296 bytes. This one pair under concurrent tests does not establish performance acceptance; phase savings do not translate into an equal wall-time reduction under this load. Full repository test session still pertains to 3599a8f, not the overlay.

## Full repository and combined input-fence checkpoint

Full `go test ./...` completed exit 0 on 3599a8f runtime (graphsession 446.668 s, TypeScript extractor 39.110 s); complete log archived. Combined Stage23/24 overlay passed retained mutation-fence tests (7.204 s), including uncaptured config changes, added pruned config, scoped resident mutation fences and discovery retention.

Prepared a new independent NATS harness under /tmp/enola-fanout-nats-acceptance: pinned full-profile Product 6042744 archive reconstruction, Stage22 baseline, 3599a8f control, combined overlay candidate. Unlike earlier Stage23/24 harness it uses full extraction profile rather than TS scope; baseline keeps old flag behavior, both new arms use changed-owner scope. Pin/clean-source preflight passed. Candidate correctness-only seven-call NATS suite is running; no quiet timing window or performance acceptance is claimed.

## NATS harness payload correction

First full-profile NATS initial failed before extraction publication with `nats: maximum payload exceeded`. The inherited TS-only harness capped broker payload at 1 MiB; full-profile file-owner Begin exceeds it. Failed evidence is retained. Set the dedicated test broker's max_payload to 8 MiB for every arm, matching the explicit 8,000,000-byte producer Begin ceiling; no runtime correctness or performance claim follows from this environment correction. Started fresh correctness-candidate-2 rather than overwriting attempt 1. Production consumers/brokers must support the chosen initial manifest size; this configuration requirement is explicit.

## NATS candidate passed

Combined candidate correctness-candidate-2 completed exit 0: all seven CLI calls and four correctness gates validated, no missing metrics, graph equality/no-op proofs passed, broker/observer cleaned by harness finally. Timing eligible=false by design. Receipt, checks, complete metrics and provenance archived under nats-candidate/. Baseline full-profile correctness run started separately; control remains pending. A fresh quiet-window availability request was sent to accuracy coordinator run_05b12be88d78 as msg_3b29e3bc16b8; no acknowledgment or hold is assumed.

A separate full-profile promotion rule was fixed before timing: combined-versus-published initial median gain >=2%, all three paired initial ratios below 1, secondary scenario regression <=2%, peak-process median RSS growth <=5%, plus aggregate ratios/seconds nonincrease and exact correctness/no-op. Control is an ablation comparison. This does not alter the standalone Stage23/24 rule or claim the larger goal is complete.

## NATS baseline parity

Published Stage22 full-profile correctness-baseline-1 completed exit 0, all seven calls/four gates validated. Every normalized graph hash matches the combined candidate's corresponding scenario. Baseline receipts and cross-arm parity assertion archived under nats-baseline/. Started control (3599a8f, no Stage23/24 overlay) correctness-control-1. Timing coordination inbox still has no reply; worker fleet reports a stale/unverifiable active accuracy dispatch, so no team-idle acknowledgment is inferred from that projection.

## All-arm correctness and source integration checkpoint

Control NATS suite completed exit 0; all seven normalized graph hashes match both published baseline and combined candidate, with all four gates in each arm passing. Archived nats-control/ evidence. No timing acceptance yet.

The prior accuracy coordinator terminal was positively observed exited; its stale run route cannot establish a fresh quiet hold. Plugin peer terminal is live. One direct coordination prompt was accepted (request 8984c4c3-b6c7-4335-a93e-1c6dac48884c), but replayed observation still reports no turn start; no acknowledgment is inferred and no duplicate prompt sent.

Copied the exact frozen Stage23/24 runtime and corrected focused fixtures into the working branch (original worktree untouched). Final combined-source `go test ./...` is running; the previously passing full suite covered the fanout-only source, so it is not substituted for this final combination.

## Combined source full repository validation

The Stage23/24 discovery changes integrated with 3599a8f passed `go test ./...` (exit 0). See `combined-full-repository.log`. No repeated acceptance timing or main publication is claimed by this correctness result.
