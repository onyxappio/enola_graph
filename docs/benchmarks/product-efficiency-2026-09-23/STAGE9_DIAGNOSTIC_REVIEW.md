# Stage 9 diagnostic review

Initial review at 2026-09-23 19:10 UTC preceded integration. Patch 1 is now integrated with its discovery and same-metadata regression tests; the other candidates remain unintegrated. These are diagnostic findings, not final performance acceptance. The ongoing AI-editor watch uses the previously validated stage 8 binary, not this candidate.

## Shared TypeScript discovery enumeration

Patch 1 shares one directory enumeration across four collectors within a single discovery build. Package manifests are still read and decoded by each consumer. Different session and Angular traversal predicates are unchanged.

Matched fresh CLI no-op runs on Product `ae233c5f56959ce5852c8381edd6cb472c4b9f95`, baseline `8c30274`, Go 1.27.1, file sink, shared host:

| Measure | Baseline | Candidate |
|---|---:|---:|
| First no-op, s | 2.001 | 1.839 |
| Three subsequent runs, s | 2.010 / 2.038 / 1.997 | 1.813 / 1.843 / 1.827 |
| Subsequent median, s | 2.010 | 1.827 |
| TS discovery median, s | 0.303 | 0.122 |

The within-batch wall reduction is 0.183 s (9.1%); discovery accounts for 0.181 s. This does not establish quiet-host acceptance, resident latency, or completion through NATS acknowledgments. Do not compare against the earlier 2.211 s control from a busier host interval. Profiling was enabled on both builds. Raw filenames incorrectly say `instrumented` for the clean candidate; binary provenance identifies the actual build.

Independent checks found identical normalized Product initial graphs and zero parses, events, generation advancement, or durable state changes in all eight no-op runs. The driver did not itself enforce the parse count and seed file count; the coordinator checked actual outputs independently. Both seeds contained 5,025 files. See [audit](stage9-patch1-diagnostic-audit.json).

The same discovery oracle ran against pristine baseline and candidate, covering hidden roots, duplicate export ordering, pruning, overlays and membership changes; normalized outputs and read probes matched. This is separate from candidate-local cache consistency tests. Test suites and same-size/restored-mtime source-change guards passed. Further matched performance and final history checks remain pending.

## Deferred fact decode: prototype withdrawn

A test-only microbenchmark used a 61,110,865-byte state with 5,025 file records. Best of five in-process measurements on the shared host, not complete CLI times:

| Decode approach | Seconds | Allocated MiB |
|---|---:|---:|
| Existing eager decode | 0.259 | 118.0 |
| Deferred payloads, syntax checks only | 0.149 | 72.6 |
| Deferred plus full fact decode validation | 0.346 | 160.5 |
| Deferred plus partial scalar-shape validation | 0.260 | 130.7 |

Partial validation leaves Props and Relations undecoded and does not preserve all eager corruption checks. The full-validation variant regressed. We withdrew this prototype without production changes or weakened validation. This is evidence about tested approaches, not a proof that every possible deferred representation must fail.

Additionally, current no-op execution assembles cached facts after loading, so merely delaying decoding would not remove that work. Skipping assembly for callers that discard facts remains a separate, unimplemented candidate; it need not weaken eager validation.

Next candidate: memoize repeated pure policy evaluations while preserving exact decision arrays, serialized identity inputs and digests. An independent oracle freezes the pre-change functions for comparison.

## Remaining acceptance

The final candidate still needs matched initial, delta and no-op measurements, exact cold equivalence, NATS acknowledgment timing, and the real history cohort. [The pinned plan](stage9-history-acceptance-plan.json) preserves ten chronological transitions and adds one real config-changing transition because those ten contain manifest changes but no tsconfig changes. The plan records selected cases, not completed runs. Fresh CLI, resident processing and the five-second watch collection window must remain distinct in reports.
