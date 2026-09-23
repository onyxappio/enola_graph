# v295 fresh-CLI no-op baseline and CPU attribution

Current production baseline `b4c9a6a` (v295), Product `9fc7ae5b4c3fc4fc266b24b1df0ef2bfcb1f7030`, full graph profile and pinned repository policy. Three independent CLI processes reuse one completed state on unchanged inputs. File sink, shared host; these are diagnostic observations, not resident-watch measurements or repeated initial performance acceptance.

| Repeat | CLI wall time, seconds | Full parses | New event bytes | Generation |
| --- | ---: | ---: | ---: | --- |
| 1 | 3.112 | 0 | 0 | 1 → 1 |
| 2 | 3.018 | 0 | 0 | 1 → 1 |
| 3 | 2.987 | 0 | 0 | 1 → 1 |

Median is **3.018 seconds**, range **2.987–3.112 seconds**. Correct no-op semantics hold in these runs, but the near-zero startup objective remains unmet. The initial run only establishes the state and is not an initial-speed comparison. Binary hashes and raw summaries are in `v295-fresh-noop.json`.

## Separate CPU diagnostic

A separate instrumented binary wraps the graph command with Go CPU profiling. It completes with zero parses and unchanged generation. The profile lasts 3.15 seconds and samples 3.16 CPU seconds (parallel work can accumulate more CPU time than elapsed wall time).

| Stack | Cumulative sampled CPU, seconds | Fraction of sampled CPU |
| --- | ---: | ---: |
| filepath.WalkDir | 1.07 | 33.86% |
| readRuntimeInputs | 0.93 | 29.43% |
| graphinput.Build / NewGraphEngine | 0.83 | 26.27% |
| Policy.computeIdentities | 0.58 | 18.35% |
| TSExtractor.SessionContext | 0.44 | 13.92% |
| Engine.FileHashes | 0.20 | 6.33% |
| loadCommittedState | 0.18 | 5.70% |

These stacks overlap and **must not be summed or treated as wall-time phases**. This sample points first to repeated filesystem discovery, policy identity construction and context discovery. Deserializing the completed state is a smaller sampled cost in this case. Reuse must still observe source/config/policy changes, tracked admission, nested Git ignore rules and transaction fences.

## Related delta diagnostic

The earlier experimental v293 snapshot2 Product transition used 69 full file parses but also 903 named-export summary scans in its CPU-instrumented run (908 in the prior uninstrumented run). Over 10.30 sampled CPU seconds, filesystem walks account for 2.00 cumulative seconds and named-export index lookup/parsing for 1.51. This is a different version and workload; it cannot be combined numerically with the v295 no-op profile or claimed as current production performance.

The named-export cache parses outside its mutex and may duplicate work on concurrent misses before retaining one entry. Package discovery also repeats through detection, session context, composition signatures and extraction. These are concrete targets for the next reuse task, subject to exact cold equivalence, recorded export bindings, immutable Begin and recovery tests. Reduced parse counters alone have not established a substantial delta latency gain.

Local raw evidence: `/tmp/enola-v295-noop-probe/{noop.pprof,cpu-top.txt,result.json}` and `/tmp/enola-snapshot2-cpu-probe/{delta.pprof,cpu-top.txt,provenance.json}`. Both are profiling-only overlays; no profiling code was added to production.
