# Fresh state decode: diagnostic and rejected format shortcut

Runtime remains unchanged. Opt-in benchmarks use the pinned Product checkpoint
in input.json, retain full Facts, and exclude disk IO. CPU/memory profiles and
a prototype test were collected on the shared host; no quiet performance
acceptance or whole-CLI speed claim.

Current decoder: 383.968 ms/op, 179.4 MB allocated, 2,555,572 allocations/op.
The profile attributes substantial CPU to JSON string scanning and memory
management. Allocation leaders are slice growth, map assignments and strings.
These sampled CPU proportions are not wall-time components to sum.

A benchmark-only prototype removes duplicate FileState summaries already
stored in TS records, then reconstructs them. reflect.DeepEqual verifies the
complete restored checkpoint against the original before benchmarking. No
production reader or writer accepts/emits this prototype representation.

| Decode | Median | Range | Allocated bytes/op |
|---|---:|---:|---:|
| Current | 385.805 ms | 383.379–387.203 ms | ~179.4 MB |
| Prototype | 362.396 ms | 361.464–372.437 ms | ~169.0 MB |

The median saving is only 23.409 ms (~6.1% of decode, not CLI), despite
removing about 8 MB of repeated summaries. Do not adopt a format migration
solely for this result: it would add version/recovery/downgrade obligations
without materially addressing near-zero fresh no-op. Existing API Facts
semantics remain unchanged. Next evaluate whether an explicit CLI summary
contract can defer full contribution hydration until graph work is actually
needed, while keeping full API results and all recovery/input proofs intact;
no such bypass is implemented or accepted here.
