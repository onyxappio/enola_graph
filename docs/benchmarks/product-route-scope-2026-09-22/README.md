# Product route-scope diagnostic — 2026-09-22

This is an experimental checkpoint, not final performance acceptance. The measured
snapshot is base 120285570ea6593caa25f9f753fed4dd80c929e9 plus the archived patch
(SHA-256 eae759aefd0f6ab8d6f92fa1e0996350b724df052ed812a6310b4f16792a0e90).
It predates subsequent Astra fixes for full route payloads, Ember composition and
same-name candidate identity changes. Do not attribute these timings to the final
reviewed production tree.

Product revision: a609c19f3861971930fae7b33dcb2950598953c5. TypeScript-only profile,
repository input policy in the archived provenance, local JetStream, authoritative
frozen scope, 1 MiB Begin cap. The original Product checkout was not modified.

## Observed replacement reduction

| Resident body edit | Previous broad fallback, three runs | Experimental narrow patch, one run |
|---|---:|---:|
| Parsed files | 1 | 1 |
| Published owners | 4,133 | 2 |
| Published events | 2,484 | 4 |
| Request completion | median 2.724444 s | 0.711270 s |

Experimental initial: 10.116992 s, 4,144 parsed files, 4,133 owners, 2,484 events.
Initial broker End arrived at 8.921852 s and reference-consumer apply at 8.998674 s
from driver start. Body broker End arrived at 0.631224 s and consumer apply at
0.631503 s from the edit/request boundary. These are request-driven resident
measurements, not production watch scheduling or downstream database latency.
Ten idle requests took 0.126–0.162 ms with no work/events/generation advance;
same-content duplicate took 0.468 ms, no parsing/publication/checkpoint.

All 14 harness checks passed, including initial/cold and delta/cold equality.
The password normalization body edit does not change extracted graph facts, so
this cold equality alone does not prove graph-changing mutations; dedicated
route/symbol regression tests are necessary. The wrapper's final summary printer
raised KeyError on a cold summary missing Work after the suite had exited zero;
raw metrics/checks are intact and processes were cleaned up. No benchmark rerun
was needed to read the correct metrics.

Prior broad-fallback timings come from the three-run Product evidence described in
[the earlier report](../product-efficiency-2026-09-22/README.md). Comparing its median
with one diagnostic run is not a repeated speedup acceptance result. Other workers
were active; no initial-performance improvement is established here.

## Fresh CLI phase profile

Same experimental binary and restored Product input, separate fresh state and
JetStream. Profiling enabled; no observer attached in this profile run.

| Measurement | Initial | No-change delta |
|---|---:|---:|
| Whole CLI wall time | 7.310462 s | 1.991639 s |
| Inventory | 0.141 s | 0.149 s |
| Content hashing phase | 0.185 s | 0.193 s |
| State load | — | 0.375 s |
| JSON decode within state load | — | 0.364 s |
| TS extraction session | 2.555 s | cache reuse 0.033 s |
| Resolved publication | 2.277 s | skipped |

Phase intervals can be nested and do not sum to process wall time. Initial
checkpoint is about 46 MB. Fresh no-change CLI parses zero files, publishes zero
owners and retains generation 1. Resident near-zero no-op does not establish
near-zero fresh CLI overhead. The remaining work includes avoiding unnecessary
state decoding/inventory work, repeated same-scope measurements, actual watch
latencies, ten contiguous historical transitions and final performance acceptance.

See the diagnostic and profile subdirectories for raw metrics, checks, logs and
binary/source provenance. The archived absolute temporary paths are provenance;
use a new isolated output directory when reproducing runs.
