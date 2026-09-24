# Stage 13: share discovery walks within each composition signature

Status: integrated locally after paired Product watch, focused race and graphsession regression checks passed; full suite passed; ready for normal push checks.

CompositionSignature previously enumerated the same repository separately for Nuxt packages, package gates and package export sources, plus package-name collectors on Nuxt branches. Each call now shares one discovery walk. The cache is created inside the call; the next call observes the tree afresh. Source contributions, captured input handling, alias resolution, signature hashing and Begin/End decisions are unchanged. This does not claim identical observation timing on a concurrently mutating tree.

## Product watch measurements

Base main `48fbc69b00828a1c351b0ee386edd8aa78219aa9`; candidate changes only `internal/extractors/tsextractor/session.go`. Go 1.27.1, both binaries built with `-trimpath`. Source Product a609c19f3861971930fae7b33dcb2950598953c5 in `/tmp/enola-stage9-md-timing/product`, isolated per-run checkouts. Three alternating ordinary pairs, explicit 500ms collection window, no instrumentation. Default CLI window remains 5s.

| Arm | Initial seconds | Delta seconds | Delta median |
|---|---|---|---:|
| Main | 10.570340, 9.428658, 9.677659 | 2.633600, 2.694355, 2.679174 | 2.679174 |
| Candidate | 9.965425, 9.799508, 9.207668 | 2.229474, 2.187367, 2.216523 | 2.216523 |

Delta median decreased 0.462651s (17.27%). All three candidate deltas beat every baseline in this sample. Initial ranges overlap and candidate initial median is higher; no initial speedup is claimed. Initial measures launch to consumer-applied graph; delta measures last fsync save to final cold-equal consumer completion, including broker acknowledgments and consumer application. The harness samples competing test processes every two seconds; none were found, which does not exclude all host contention.

All six runs pass cold equality, input stability across cold verification, silent duplicate/idle checks, matching batch counts, and no abandoned Begins. Every delta parses 11 files. The consumer observation and quiet margin are not proof of an internal watcher watermark; retain the harness limitations in the raw receipts.

## Collector diagnostic and reuse decision

A separate five-pair diagnostic over the same source, graph input policy with explicit Product exclusions, no captured inputs, compares the three collectors together. Their outputs match by DeepEqual in every pair.

| Work | Median | Range |
|---|---:|---:|
| Separate walks | 156.097ms | 154.329–157.981ms |
| Shared call-local walk | 63.719ms | 63.513–65.151ms |
| Reobserve retained Discovery | 67.822ms | 66.901–69.764ms |

Reobservation checks 1,977 names, 1,965 side reads and 655 directories. These are isolated diagnostic measurements, not additive estimates of end-to-end delta latency. The test completes in 2.08s. Full reobservation per signature is not cheaper than the cached collectors here, so cross-call Discovery reuse is deferred. Capture compatibility alone does not prove uncaptured live metadata unchanged and cannot replace that fence.

## Correctness evidence

Worker signature/shared-walk focused tests pass (package 0.576s), including ordering-sensitive collector equality and metadata changes between calls. An independent root test passes (package 0.497s): previously absent nested package addition, deletion, captured manifest overriding contradictory live bytes, and captured package absent on disk all agree with the uncached signature. Combined focused race checks pass (package 1.664s). Frozen/framework/package/Discovery graphsession regressions pass (package 74.397s). Raw logs accompany this report. The integrated source differs from the measured candidate only in its explanatory comment.

## Outstanding

This optimization does not complete the larger performance goal. Fresh CLI no-op, remaining watch overhead, final historical coverage and broader performance acceptance remain open. The full repository suite passed in 498.543s (`go test ./... -count=1 -timeout=20m`, Go 1.27.1); production and test source hashes were unchanged. Normal push checks follow. Raw paired receipts, exact harness/driver, build hashes and diagnostic source/logs are archived as `stage13-*` alongside this report. Original scratch paths are preserved; adapt those paths to reproduce.
