# Product watch: local-call change and remaining overhead

Single shared-host diagnostic on f71197e production bytes, TypeScript-only profile,
NATS JetStream, and a 5-second fixed collection window. This profile differs from
the all-extractor CLI measurements; the initial times are not directly comparable.
The entire harness took 88.466 seconds including setup, idle checks, and cold analysis.

| Measurement | Result |
|---|---:|
| Watch launch to initial consumer application | 12.265 s |
| Last durable save to matching consumer graph | 6.935 s |
| Delta session through local state promotion | 2.235 s |
| Burst saves / completed replacements | 3 / 1 |
| Delta scope / full parses / summary scans | 11 / 11 / 117 |
| Delta batches / JSON payload bytes | 31 / 784,937 |
| Delta node / edge records sent | 525 / 1,944 |
| Net graph node / edge count change | 0 / +2 |
| Idle / identical-write event counts | 0 / 0 |

Frozen Begin validation, batch count validation, and exact cold equality passed.
The final graph actually changed. Consumer convergence is an observed stable suffix,
not a watcher-watermark causality proof. This is not repeated performance acceptance
or a long-running recovery/backpressure soak.

## What the trace shows

The changed batch takes the resident fast path (`fast=true`), avoiding complete
runtime-input preparation. Frozen preview costs 1.143 seconds: two extraction passes
repeat framework discovery (~0.264 and ~0.272 seconds, plus alias discovery), while
map/aggregation takes ~0.035 and ~0.263 seconds. These are nested timings, not additive
with the preview total. Whole-graph grouping takes 0.170 seconds; pending state writes
0.242 seconds, including marshaling 58.36 MB. State promotion takes 0.163 seconds.
This supports reusing captured discovery and reducing whole-state work as next steps.
A preceding no-publication session still prepares runtime inputs in 0.943 seconds and
returns after 1.091 seconds; zero events alone does not establish negligible CPU cost.

## Benchmark correction and separate defect

The prior `email.normalize()` mutation was graph-neutral under these semantics.
It nevertheless published 11 owners, 31 batches and 784,401 bytes, advancing generation.
Its cold equality was vacuous as a changed-graph correctness check. That result is
retained as evidence of unnecessary neutral publication, not accepted as convergence
performance (its matching suffix even predates the edit).

The harness now inserts calls to two existing local functions in the disposable
Product copy. It rejects graph-neutral changes for Product as well as tiny fixtures.
The observer build fallback now uses a module path inside Enola for Go internal-import
access and permits dependency resolution only in its temporary module. An actual
observer rebuild passed in 1.369 seconds; watch self-tests passed.

Raw measurements, identities and limitations are in v297-watch-local-calls.json;
the complete watch trace is in v297-watch-local-calls.stderr.
