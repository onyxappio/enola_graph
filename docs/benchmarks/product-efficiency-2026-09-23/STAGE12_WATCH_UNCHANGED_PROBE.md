# Stage 12: discard proven unchanged watch batches before collection

Status: integrated locally after paired Product measurements, focused race checks and the full repository test suite passed.

## Problem and change

An identical-content notification can open the fixed collection window. A later real edit burst can then overlap analysis at that window boundary, causing a pre-Begin refusal and reconciliation retry. Stage 11 reproduced this timing shape; it did not identify the sole native event that triggered Ready.

The candidate checks a non-destructive queue snapshot before opening the window. Only a covered, continuous, content-identical batch may be conditionally discarded. Its queue token must still match under the queue mutex, and the source uncertainty flag is checked under the registration mutex through commit. Busy registration declines immediately. The resident watermark advances with the successful discard under the resident mutex. Real or uncertain changes retain the Ready-time deadline, including time already spent probing. Changed-content probes stop at the first differing hash. Failure recovery, frozen Begin, End validation and completed generation rules are unchanged.

## Measurement identity

- Base: `ec43523f8e3536578b3271524f5948aa5602a3de`.
- Candidate: only `internal/graphsession/changes.go` and `resident.go` production changes from that base; frozen source and SHA receipts accompany the report.
- Go 1.27.1 darwin/arm64, both binaries built with `-trimpath`.
- Pinned Product source: `/tmp/enola-stage9-md-timing/product`; isolated harness checkouts.
- CLI watch collection window: **500 ms**, explicitly configured. This does not change the CLI default of 5 s.
- Three alternating pairs per scenario. Initial: process launch to consumer-applied initial graph. Delta: last observed fsync save to final cold-equal consumer completion. Broker acknowledgments and consumer application are included.
- No profiling instrumentation enabled. Every run samples competing test processes every 2 seconds; this detects test workloads, not all host contention.
- Ordinary schedule uses the existing harness. Near-expiry changes only the duplicate-to-burst pause to 0.4 s. This controls save scheduling, not exact native Ready arrival. All tails are retained.

## Ordinary watch pairs

| Arm | Initial, seconds | Delta, seconds | Delta median |
|---|---|---|---:|
| Base | 10.124889, 9.039886, 10.738384 | 5.338602, 5.246756, 2.553307 | 5.246756 |
| Candidate | 10.208276, 9.699482, 9.290177 | 2.686089, 2.649172, 2.726846 | 2.686089 |

All six runs pass exact cold equality, stable inputs across cold verification, silent idle/duplicate checks, matching batch counts, and no abandoned Begins. Initial hashes match across all arms, as do final hashes. Every delta publishes one generation with 11 owners and 11 parsed files.

The spread suggests fewer slow attempts in this small sample, not uniform doubling of delta speed. One base run is faster than all candidate runs. Initial distributions overlap; no initial speedup is claimed. A 2.7-second watch delta remains an intermediate result in the larger performance goal.

## Duplicate-to-burst pause of 0.4 seconds

| Arm | Initial, seconds | Delta, seconds | Delta median |
|---|---|---|---:|
| baseline | 9.028524, 9.002559, 9.120045 | 3.101049, 2.919620, 2.866311 | 2.919620 |
| candidate | 9.252084, 9.211330, 9.112814 | 2.896310, 2.892977, 2.897902 | 2.896310 |

All six runs pass the same correctness gates. All 12 runs across both scenarios share identical initial and final graph hashes. The second scenario is effectively neutral in this small sample: medians 2.919620 versus 2.896310 seconds, with overlapping ranges. Save timing does not prove when the native event was consumed, so this is not a deterministic recreation of the Stage 11 trace.

## Independent correctness checks

Five root tests pass under the race detector: a new save at the proof/commit boundary; source uncertainty while the raw queue remains covered; silent duplicate followed by a cold-equal real edit; busy registration declines without consuming work; probe time does not restart the collection window. Package time 4.809 s, command wall time 11.173 s including build. Source hashes were unchanged during this run.

Removing the queue sequence comparison via a scratch Go overlay makes the new-save test fail: it wrongly discards the racing save. The artificial uncertainty-flag test initially failed with the duplicated counter design; this showed dependence on keeping two states synchronized, not a production writer missing an increment. The final candidate uses the actual flag.

Worker focused regressions passed in 90.119 s; combined probe/watch/root tests passed under the race detector in 9.001 s. The integrated full repository suite (`go test ./... -count=1 -timeout=20m`, Go 1.27.1) passed in 430.834 s wall time with unchanged production/test source hashes; raw log and receipt are included.

## Separate probe diagnostic

A separately instrumented overlay run, with phase profiling enabled, remained cold-equal and silent for duplicates. It recorded three probes: a conservative decline with zero reads (1,833 ns); an unchanged batch discarded after one 6,872-byte read (53,792 ns); and a changed batch declined after one 6,899-byte read (560,208 ns). These are one-run diagnostic observations including timer/log measurement effects, not performance distribution estimates or clean-binary latency comparisons. Raw stderr and the diagnostic source overlay are included. The clean paired runs were not instrumented, so this proves the discard executed in the diagnostic, not how often it executed in each timed run. Single-path observations do not establish the multi-path early-stop bound; the dedicated multi-path test covers that.

## Remaining ordinary-delta costs

In the same single instrumented diagnostic, the ordinary content delta uses `fast=true`; effective config and reconciliation report approximately zero, and runtime input/discovery snapshots are reused. Earlier input construction spans in the log belong to registration-gap reconciliation and must not be attributed to the ordinary delta.

Within the ordinary session (2.313 s total), sequential session spans include frozen preview 0.788 s, invalidation planning 0.284 s, post-Begin dirty-scope work 0.218 s, assembling file state 0.276 s, grouping owners 0.162 s, and pending-state writing 0.242 s. Frozen preview contains two TS passes and their nested index/aggregation work; do not sum those children into these parent spans again. These observations direct the next audit toward repeated project-wide composition/planning/state work, rather than rebuilding policy on a path that already reuses it. They are diagnostic phase observations, not paired speed estimates.

## Outstanding

Fresh CLI/no-op and broader historical/long-running watch acceptance remain outside this individual optimization result. The remaining ordinary-delta work above is the next profiling target; this change does not complete the larger performance objective.

## Evidence layout and reproduction

The `stage12-*` files archive the exact invoked harness, driver, build receipts, paired results and diagnostic overlay. The original filenames and absolute scratch paths are preserved inside receipts. To rerun, restore the harness as `watch.py` and driver as `run-watch-pairs.py` in a fresh work directory, supply the pinned baseline/candidate binaries, and adapt the recorded helper/source/observer paths. The archived diagnostic overlay maps a scratch `resident.go` to the diagnostic source; recreate that mapping for the chosen scratch root. Do not apply it to the clean timing binaries.
