# Cached TypeScript fact materialization allocation

Three controlled samples on one preserved Product state, using production code
from d988437 and a test-only diagnostic. This is not a complete Session.Run,
latency benchmark, peak RSS measurement or optimized implementation.

| Operation | Allocated bytes, sample range |
|---|---:|
| cloneTagged over cached TS facts | 35,265,792–35,265,792 |
| applyLocalIO on cloned facts | 0–16 |
| Append aggregate and synthetic grouping | 43,355,328–43,360,920 |
| Total | 78,621,120–78,626,728 |

There are 4,086 non-nil TS records and 59,661 resulting facts. cloneTagged makes
187,014 allocations per sample. Each sample produces the same fact hash; the
serialized cached State remains unchanged after all samples. The receipt pins
the input and diagnostic source. State decoding, holder allocation and final
hash validation are outside the measured interval; double GC precedes each sample.
Paths are sorted for reproducibility, unlike the production map traversal.

The result establishes a concrete allocation cost, not achievable speed savings.
The prior Stage17 pre-sizing experiment already showed that fewer allocations
need not improve startup latency. A new candidate may avoid materialization for
one-shot summary output only after proving the entire run is a no-op. It must
retain full default API/JSON results, resident snapshots and all contributions
when another extractor publishes. Full --json takes precedence over --summary-json.
Run uses a Resident internally, so the one-shot boundary must be enforced rather
than relying on callers not passing an option to OpenSession. Eager state
validation, recovery and input fences stay in place.

The implementation candidate starts from main5e145dd, independently of unmerged
Stage18. No implementation or performance acceptance is asserted by this report.
