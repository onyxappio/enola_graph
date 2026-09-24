# Stage15 on Wave14: validated targeted optimization

Base: `2ef91a45a5e18102849dd6b5f4e2408bfe14a4a3` (cache v320). Experimental Stage15 uses cache v321. Final patch SHA256: `247ed1a5d57ba0ec26df7b3e155c45519fd33cbde87c7471c1395fba42dc1eb3`. This report supports integrating a targeted optimization, not completion of the broader performance goal. Publication is established by the containing main commit, not by the historical experiment receipts.

## Completed validation

- Full combined suite passed (`go test ./... -count=1`); log: [full suite](stage15-combined-fullsuite.log). After the test-only fixture strengthening, the extractor package and cache coverage passed again.
- Independent proof-merge, in-flight peek, and graph-session forwarding regressions passed with the race detector: [log](stage15-independent-race.log).
- Both final direct-session bridge regressions passed independently with the race detector: [log](stage15-bridge-race.log).
- Production hashes remained unchanged during build and final test reconciliation. The [build receipt](stage15-combined-build.json) records the original patch used for building; the final patch differs only in tests and test registration.

## Baseline history: 10 of 10 passed

Pinned first-parent history ending at `a609c19f3861971930fae7b33dcb2950598953c5`. Each delta equals a fresh cold graph; each subsequent no-op parses zero files, publishes zero events, and preserves state and generation. Raw results: [baseline history](stage15-wave14-baseline-history.json).

| Transition | Parsed files | Begin owners | Owners whose graph contribution changed |
|---|---:|---:|---:|
| 4d104e60 → 07fb4a41 | 6 | 7 | 2 |
| 07fb4a41 → fec1eac3 | 591 | 1168 | 83 |
| fec1eac3 → 9fc7ae5b | 62 | 1655 | 17 |
| 9fc7ae5b → 1c260747 | 432 | 554 | 32 |
| 1c260747 → 599575d0 | 7 | 9 | 6 |
| 599575d0 → ae233c5f | 99 | 344 | 25 |
| ae233c5f → 4168360e | 111 | 161 | 10 |
| 4168360e → a6f1f3a9 | 80 | 80 | 4 |
| a6f1f3a9 → 5dfb2c8f | 43 | 126 | 5 |
| 5dfb2c8f → a609c19f | 37 | 46 | 7 |

These are correctness and work counters. Durations in raw logs are ineligible for performance conclusions because other correctness work ran concurrently. Begin scope and parsed-file count measure different work.

## Matched candidate history: 10 of 10 passed

The clean retry completed successfully. All ten incremental graphs equal their fresh cold oracles, all ten post-transition no-ops preserve state/generation with zero parses/events, and the full cold graph hashes match between baseline and candidate for all ten revisions. See [candidate results](stage15-wave14-candidate-history.json), [matched comparison](stage15-wave14-comparison.json), and [totals](stage15-wave14-totals.json).

Across these ten transitions, parsed files decreased **1468 → 1344 (−124; −8.45%)**, input file reads decreased **2768 → 2644 (−4.48%)**, and summed Begin owner counts decreased **4150 → 4139**. Scope increased in two individual cases, by seven and two owners; it is not uniformly narrower. This is work-count evidence, not a wall-time speedup claim.

| Transition | Parsed before → after | Begin scope before → after |
|---|---:|---:|
| 4d104e60 → 07fb4a41 | 6 → 5 | 7 → 6 |
| 07fb4a41 → fec1eac3 | 591 → 513 | 1168 → 1149 |
| fec1eac3 → 9fc7ae5b | 62 → 25 | 1655 → 1662 |
| 9fc7ae5b → 1c260747 | 432 → 431 | 554 → 554 |
| 1c260747 → 599575d0 | 7 → 7 | 9 → 9 |
| 599575d0 → ae233c5f | 99 → 92 | 344 → 346 |
| ae233c5f → 4168360e | 111 → 111 | 161 → 161 |
| 4168360e → a6f1f3a9 | 80 → 80 | 80 → 80 |
| a6f1f3a9 → 5dfb2c8f | 43 → 43 | 126 → 126 |
| 5dfb2c8f → a609c19f | 37 → 37 | 46 → 46 |

## History limitations

The first candidate attempt passed one transition, then failed with ENOSPC while writing the second result. Its artifacts remain separate; the complete comparison above uses the successful fresh retry only.

Source review of `authoritativeFilePlan` found a plausible explanation for the two scope increases: preview-reparsed files terminate reverse closure; sparing a reparse can therefore let the conservative closure reach additional importers. All added owners lie beyond intermediate owners already present in both scopes. Per-file preview inclusion/parse reasons were not recorded, so this is a source-backed hypothesis, not definitive causal attribution. Aggregate parse counts and full graph equality are proved; whether each added owner was republished from cache is untraced.

Repeated quiet-host NATS, fresh CLI timing and profile measurements are complete below. The broader near-zero startup objective remains open; this stage reduces resolution work rather than all startup costs.

## Targeted watch measurement design

The ordinary watch control changes only a function body in `packages/crypto/src/password.ts`. A separate import-change scenario adds a private helper import and call inside `apps/mobile/src/stories/figma-catalog/figmaCatalogStoryShared.tsx`, leaving its export declarations unchanged. The exact edit and source hash are pinned in [mutation spec](stage15-import-change.json). The paired runs below establish applicability and saved parses; old-snapshot importer counts were not used as predicted savings.

The optional mutation harness verifies the original source hash, constrains paths to the isolated Product clone, and requires exact unique replacements. Guard checks passed, as did the inherited watch self-test (0.29 seconds, harness validation only). See [harness patch](stage15-watch-mutation-harness.patch) and [verification receipt](stage15-watch-mutation-harness.json). The completed runs are reported below.

## Targeted import-change watch correctness result

Both arms completed the exact pinned edit. Initial graphs match across builds, final watch graphs match their respective fresh cold oracles and each other, idle/duplicate events are zero, inputs stayed stable, and no Begin was abandoned. [Comparison](stage15-import-watch-comparison.json).

| Delta counter | Wave14 baseline | Stage15 |
|---|---:|---:|
| Parsed files | 407 | 1 |
| Begin owners | 407 | 1 |
| Batches | 102 | 2 |
| JSON payload bytes (Begin/batches/End) | 5,012,623 | 34,227 |

This proves reduced work and traffic for one targeted edit in real Product, not a general speedup factor. Runs occurred without a coordinated quiet host; all raw durations in the archived reports are ineligible for performance conclusions. Separate repeated quiet measurements are reported below.

## Fresh CLI harness preflight

The candidate NATS CLI harness completed initial, unchanged no-op, body edit and structural edit. Its four checks passed: silent no-op with zero parses and stable generation, and exact fresh cold graph equality for initial/body/structural. [Checks](stage15-cli-preflight-checks.json). This establishes harness compatibility and scenario correctness, not speed acceptance. The [receipt](stage15-cli-preflight-pair-receipt.json) explicitly sets timing eligibility false and records a concurrent Codata Enola process; that worker was notified of the overlap.

The matched baseline CLI preflight also passed all four checks. Cross-build normalized hashes match at initial, body and structural stages and their cold oracles. Parsed counts are unchanged across builds: initial 4035, no-op 0, body 11, structural 12. The body edit adds string normalization and is graph-neutral under current semantics: its normalized graph equals initial in both builds, despite a completed replacement. It must not be presented as evidence of a changed body-call graph. The structural edit does change the graph. [Matched CLI comparison](stage15-cli-preflight-comparison.json).

## Quiet-window fresh CLI no-op diagnostic

Five alternating pairs completed during the jointly confirmed 15:30–15:35 UTC window on September 24. FSM acknowledgment: `msg_1b0fd0b92610`; Codata acknowledgment was verified from its coordinator terminal because cross-run reply routing failed. The captured transcript is archived beside the results. All ten runs preserve state bytes, event-file length and generation11, with zero parses/owners and no sampled competing processes.

| Fresh CLI file-sink no-op | Wave14 | Stage15 |
|---|---:|---:|
| Median seconds | 1.933 | 1.977 |
| Min–max seconds | 1.892–2.846 | 1.909–2.903 |

Candidate median is 2.31% higher; these results do not demonstrate a no-op speedup. This is repeated process startup on a warm filesystem, not cold OS-cache latency or NATS performance acceptance. See `stage15-noop-results.json` and `stage15-noop-summary.json`. Near-zero fresh no-op remains unresolved.

## First two quiet targeted watch pairs

Both pairs finished by15:34:22 UTC inside the confirmed slot, with reversed arm order on pair2. Every run passed cold equality, silent idle/duplicate checks, stable inputs and complete lifecycle checks; no sampled competing processes.

| Metric | Wave14 pair1 / pair2 | Stage15 pair1 / pair2 |
|---|---:|---:|
| Delta convergence ms | 2282.931 / 2357.595 | 1910.996 / 1932.737 |
| Initial consumer-applied ms | 9682.442 / 9408.805 | 9567.450 / 9339.076 |
| Delta parses | 407 / 407 | 1 / 1 |

The two-run median Delta decreases by 17.17%. These measure the targeted private-import change only; they do not establish general speedups. The final three-pair summary, ordinary controls and fresh CLI NATS results follow below. Raw paired reports include the timing and correctness evidence.

## Observed watch boundaries

Cross-build initial/final hashes were rechecked for both measured pairs, and both edits actually change the graph. In pair2 the candidate takes1161.783ms from observed save to first broker event,495.401ms from that event to first batch,275.294ms from first batch to broker End and0.259ms from broker End to consumer completion. These sum to1932.737ms. This identifies substantial cost outside parsing but does not attribute it: the first interval mixes scheduling, detection and preparation. Broker observation is not publisher acknowledgment timing. See `stage15-watch-observed-boundaries.json` for all four runs.

Observer-source inspection: the retained observer overlay obtains event timestamps from `msg.Metadata().Timestamp` (`brokerTimestampNS`), not from Python frame polling or the time Fetch returns. Its consumer loop uses `Fetch(1, FetchMaxWait(time.Second))`; that timeout alone is not evidence of a fixed one-second delay. Therefore the observed first-event-to-first-batch interval cannot be explained simply by Python polling timestamps. This is source inspection of the retained overlay, not a new binary provenance check or a causal profile. The measured broker-End-to-consumer gap is only0.232–0.259ms for the candidate in these two runs.

## Completed repeated timing series

All scheduled series completed within the confirmed slots: three targeted import-watch pairs, three ordinary watch pairs, three fresh CLI NATS pairs and five file-sink no-op pairs. Every paired CLI graph hash matches across builds, all24 CLI correctness checks pass, all measured watch runs pass their cold/lifecycle checks, and no competitor samples were recorded. Sampling does not exclude all possible host contention.

| Scenario (process seconds unless stated) | Wave14 median [min–max] | Stage15 median [min–max] | Change |
|---|---:|---:|---:|
| r1-new-initial | 8.408 [8.392–8.565] | 8.424 [8.311–8.643] | +0.19% |
| r1-new-noop | 1.663 [1.660–1.664] | 1.654 [1.651–1.669] | -0.57% |
| r1-new-body | 3.412 [3.363–3.428] | 3.483 [3.442–3.502] | +2.10% |
| r1-new-structural | 3.453 [3.428–3.491] | 3.564 [3.424–3.606] | +3.22% |
| Targeted watch convergence | 2.309 [2.283–2.358] | 1.915 [1.911–1.933] | -17.09% |
| Ordinary watch convergence | 2.218 [2.188–2.238] | 2.218 [2.205–2.273] | -0.04% |

CLI process completion includes publisher completion; broker metadata arrival and consumer-applied times are separately recorded, not interchangeable with publisher acknowledgment timing. Watch convergence is observed save-to-final-cold-equal consumer completion, not an internal watcher-drain proof. The ordinary/body edit is graph-neutral and remains a control, not evidence of a changed graph.

| Initial telemetry | Wave14 median | Stage15 median |
|---|---:|---:|
| first_batch_s | 5.934 | 5.933 |
| broker_end_s | 8.300 | 8.325 |
| consumer_end_s | 8.379 | 8.410 |
| rss_bytes | 957956096.000 | 917372928.000 |

Stage15 proves a targeted import-change improvement, not a universal speedup. Initial and ordinary watch are effectively unchanged in this sample; fresh body/structural deltas have higher medians. The subsequent diagnostic below did not reproduce the body gap and does not erase these unprofiled results. Near-zero fresh no-op is not achieved. Timing against this Wave14 baseline does not replace the repository-wide pinned-old-baseline requirement. The integration decision accepts this limited tradeoff for the targeted improvement; it does not claim a general CLI speedup.

## Profiling interpretation constraint

Source review of `writePendingStateFP` shows `state_json_marshal` stops before file creation, Write, Sync, Close, Rename and directory fsync; the write-side `state_fingerprint` starts after these operations. Consequently, a small sum of read/decode/marshal/digest mark differences cannot rule out larger-state persistence IO as a cause of the CLI delta overhead. Any residual must be described as outside the measured marks, not outside persistence altogether. The completed diagnostic below retains this limitation.

## Profile decomposition completed (six runs)

The validated harness ran all six alternating baseline/candidate arms in the jointly confirmed19:45–19:50UTC slot. All required checks passed, both cold oracles and all body hashes match, and no competing processes were sampled. Raw profiles and receipts are preserved in `profile-results/`; this is an instrumented diagnostic, not a replacement for unprofiled acceptance timing.

| Instrumented wall seconds | Wave14 median [min–max] | Stage15 median [min–max] |
|---|---:|---:|
| Body delta | 3.440 [3.390–3.472] | 3.434 [3.414–3.445] |
| No-op | 1.661 [1.644–1.699] | 1.662 [1.647–1.672] |

The previous+71ms median body gap did not reproduce under instrumentation: this series gives−6ms with overlapping ranges. State decode median differs by+4ms; read and marshal medians are unchanged at the trace precision. Repeated fingerprint occurrences are preserved raw rather than overwritten or summed. Preview and file assembly marks differ by+12ms/+11ms, but whole session differs−3ms and pending-state write−4ms; these overlapping/instrumented marks establish no single cause. The larger persisted state remains a measured format cost (~775KB), not a proved latency explanation. No production correction is justified by this diagnostic alone. Earlier unprofiled results remain reported, including their small CLI slowdowns.

## Integration verification

The production and regression files applied to main match the independently tested combined clone byte-for-byte. The full-suite and race logs above therefore apply to the integrated source. A fresh trimpath CLI build and targeted package vet also passed in the integration checkout. The final Stage15/Independent/cache-coverage selection passed: tsextractor 0.667s, graphsession 77.816s, cachecov 0.828s (operational durations on a shared host). Raw NATS logs and the archived patch retain original whitespace for provenance; the source/document diff whitespace check passes when these raw artifacts are excluded. These checks are correctness evidence, not new benchmark measurements. The normal pre-push cache coverage, documentation and golden/determinism guards remain enabled.

The next performance work is fresh CLI startup: separate policy/inventory work, state loading and persistence costs before selecting another change. Resident watch results do not establish near-zero fresh CLI startup.
