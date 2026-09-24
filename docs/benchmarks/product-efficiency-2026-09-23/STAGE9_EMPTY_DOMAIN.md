# Empty declared-input domain: Product history result

Deferring an empty extractor domain to its declared-input fingerprint removes
the unrelated Python fallback on the Product package-addition transition. The
candidate passes exact incremental/cold equality and strict replacement protocol
validation. This is an experimental two-file patch on `a970a03`; the full
graphsession suite passed in 812.115 seconds, and all added integrated guards
passed in 17.787 seconds. The [receipt](stage9-empty-domain-history.json)
identifies its actual production files and binary independently of Go VCS metadata.

The input transition and policy match [the alias history diagnostic](STAGE9_ALIAS_HISTORY.md):
`d0fbbf855af5f4a33c364885f85805d71f9fac7c` →
`a2ac71af8a22471c27059a9b318ef4880caa50cb`.

| Metric | Released a648c0b | Alias candidate | Alias + empty domain |
|---|---:|---:|---:|
| Delta wall seconds | 24.785 | 20.341 | 4.108 |
| Target cold seconds | 24.011 | 21.527 | 29.270 |
| Parsed files | 3,940 | 6 | 6 |
| Begin scope owners | 8,178 | 8,178 | 60 |
| Owners published | 4,773 | 4,773 | 8 |
| Published events | 2,077 | 2,077 | 4 |
| JSON payload bytes | 69,503,864 | 69,503,935 | 70,500 |
| Final CLI no-op seconds | 1.732 | 1.783 | 2.026 |

All timings are individual file-sink runs on a shared host, not repeated NATS
acceptance measurements. Setup/cloning is excluded. The cold timings demonstrate
the host variability; do not infer an initial-analysis regression or a stable
speedup ratio from these samples. The observed new delta/cold ratio is 14.0%.
Near-zero fresh CLI no-op remains unresolved.

The new Begin/End both describe 60 owners with the same deterministic digest;
two batches and their digest validate. Only seven owners have changed final facts.
That observed difference does not establish a safe seven-owner planning algorithm.
The new scope still conservatively includes configuration-affected owners.

The final no-op parses and publishes nothing, retains generation 2 and preserves
all persistent-state file hashes. Separately, the same candidate reads genuine
released Product state without publishing or changing state (4.217 seconds during
concurrent Git cloning, recorded only as a compatibility check).

Independent regression checks cover TS addition alone, TS alias retarget with
addition, deletion of the last Python source, and subsequent first-source
reappearance. Additional checks cover an extractor without declared ownership
whose declared content or inventory-name input changes: both update the graph,
equal cold, and return to a silent no-op. The production rule preserves the scan
fallback for extractors without `DeltaInputs` and still checks retirements.

## Repeated file-sink comparison

[Three alternating repeats per arm](stage9-empty-domain-repeated.json) compare
released alias optimization `8af2c7a` with the experimental empty-domain patch.
Both use the same Product transition and policy above, fresh state for each arm,
and a fresh CLI for initial, delta and no-op. Git checkout/setup is excluded.

| Phase | Alias median seconds (range) | Empty-domain median seconds (range) |
|---|---:|---:|
| Initial | 28.686 (27.340–28.941) | 28.613 (28.378–29.091) |
| Delta | 24.667 (24.404–27.397) | 4.319 (4.167–4.451) |
| No-op | 1.822 (1.805–1.848) | 1.819 (1.799–2.011) |

Median delta is 5.71 times faster (82.5% less elapsed time); its ratio to initial
falls from 86.0% to 15.1%. Initial and no-op show no meaningful improvement.
All six arms validate replacement digests and match independently cold-validated
initial and target graph hashes. Every final no-op preserves state hashes and
publishes zero events without advancing generation. Candidate scope is 60 in
all three repeats, compared with 8,178 for alias alone.

These runs share the host with tests and use a file sink. They do not establish
broker acknowledgment latency or production NATS acceptance. The broader
historical corpus and long-running production watch/NATS acceptance remain open.
The worker full-repository suite exited 0: 110 packages passed, including
graphsession in 698.518 seconds and bootstrap in 193.601 seconds. The measured
production files match this snapshot; the integrated owner file only adds an
explanatory comment.
