# Product performance profile — 2026-09-22

This profile compares the current `graphsession` tree with the same build made
before the shallow `prevFiles` owner-map reuse. Both runs used the same Product
snapshot (`/tmp/enola-product-benchmark-source`), configuration, Go toolchain,
and fresh state directories.

| Scenario | Current | Previous | Interpretation |
| --- | ---: | ---: | --- |
| Fresh initial, file-event sink | 36.37 s | 38.61 s | Single cold runs; not sufficient evidence of a speedup. |
| Fresh no-op, 5,209 hashed files | 2.07 s | 2.08 s | No material change. |

The no-op profile identifies the reusable work that still dominates a fresh
process: 0.178 s hashing, 0.230 s extractor detection, and about 0.4 s state
load/unmarshal. The explicit `clone_file_state_copy` phase is below 1 ms after
shallow owner-map reuse. The old cumulative profile marker was removed because
it was not measuring that copy phase.

The current change preserves transactional isolation: mutation helpers clone a
record before replacing or retiring it, while unchanged records are reused by
the run-local owner map. The `graphsession` regression subset passed in 23.63 s;
the full package passed earlier in this worktree in 89.74 s.

Next optimization target: a correctness-preserving reuse path for input
discovery and hashing in a resident session. A cold process must continue to
verify content rather than trusting only mtime/size metadata.
