# Frozen invalidation history — 2026-09-22

First-parent Product commit chain, oldest to newest. Diagnostic correctness
harness. One pass is not a performance-acceptance run. Wall times are
file-sink `--events` JSONL, not NATS JetStream.

The dedicated Product checkout `/tmp/enola-product-history-source` is never
written. Analysis uses a pinned overlay from
`docs/benchmarks/product-delta-2026-09-21/product-mcp-arch.yaml`
(`apps/mobile/e2e/artifacts/**`, `worker-reports/**`). The overlay is
hash-recorded in provenance and applied only when a SHA has no git-tracked
`mcp-arch.yaml`. Tracked historical config is left in place.

## What was wrong with the previous dump

[results.json](results.json) and
[results.baseline-invalid-chronology.json](results.baseline-invalid-chronology.json)
are preserved raw dumps of an earlier diagnostic. That run used ordinary
`git log` (not `--first-parent`), a fresh initial per transition, and a stale
binary path. Do not treat those numbers as the history contract.

## Method

1. Dedicated clone of Product. `git fetch origin main`. Source worktree stays
   untouched, including dirty/untracked config.
2. Pin the latest **ten first-parent** commits of `origin/main`, oldest first
   (`main~10` … `main` on that parent chain).
3. Isolated snapshot at the oldest SHA. Apply the pinned overlay when the SHA
   has no tracked `mcp-arch.yaml`. One frozen **initial** analysis on a fresh
   work directory (existing work dirs are refused).
4. Persistent `--state-dir` on that live snapshot. For each child SHA, check
   out the child in the live tree and run **delta**. Stop after the first
   failed transition.
5. Isolated cold snapshot at the same child SHA. Fresh frozen **analyze**.
   Incremental graph (initial + all deltas so far) must hash-equal cold.
6. Required owners from `git diff --name-status -M` parent→child, minus
   lockfiles, kept when the owner is in the **prior semantic owner set or the
   target cold scope**. Deleted and old-rename owners stay required.
   Lock-only transitions must no-op with generation equal to the previous
   completed generation.
7. `necessary_owner_count` is the prior-versus-cold owner canonical
   contribution diff (overinvalidation = Begin owners minus that set).
   Changed-path counts are not a minimal invalidation proof.
8. Raw Begin/End owner and batch digests are recomputed from payloads.
   The imported validator requires Begin-first/End-last ordering, rejects
   malformed records and foreign-run batches, and hashes complete node/edge
   JSON (order-independent, duplicates preserved).

The Enola binary is **built from the current Enola source**. Provenance records
source revision, dirty/untracked identifiers, binary SHA-256, and
`go version -m`.

## Flags

```
python3 docs/benchmarks/invalidation-history-2026-09-22/run.py --self-test

python3 docs/benchmarks/invalidation-history-2026-09-22/run.py \
  --source /tmp/enola-product-history-source \
  --ref a609c19f3861971930fae7b33dcb2950598953c5 \
  --count 10 \
  --work /tmp/enola-invalidation-history-2026-09-22-a609c19f \
  --output /tmp/enola-invalidation-history-2026-09-22-a609c19f/results.json
```

`--work` must not already exist. `--only` accepts a **contiguous prefix** of
first-parent transitions starting at index 0 (`0` is oldest→next), or matching
SHA prefixes / `parent[:12]..child[:12]` ids of that prefix. Gaps and later-only
selections are rejected because the persistent delta parent would not be
initialized. `--output` must not be the tracked `results.json` or the baseline
file. Per-case dumps live under `$WORK/cases/`.

## Completed run (file-sink, earlier dump)

The earlier dump completed all ten transitions with `graph_hash_equal=true` and
no blockers. Those walls are file-sink `--events` JSONL, not JetStream.
`necessary_owner_count` in that dump followed changed paths; the harness now
records prior-versus-cold contribution diffs. Initial analysis at `4d104e600f89`
took 25.238 s (4,120 parsed files, 8,609 owners). Delta rows below include the
delta wall time and the fresh cold oracle time.

| # | transition | changed files | Begin owners | delta parsed | delta s | cold s | graph equal |
|---:|---|---:|---:|---:|---:|---:|:---:|
| 0 | `4d104e600f89..07fb4a41ddaf` | 6 | 8,609 | 3 | 22.521 | 24.684 | ✅ |
| 1 | `07fb4a41ddaf..fec1eac346c4` | 9,345 | 8,613 | 1,923 | 23.256 | 24.580 | ✅ |
| 2 | `fec1eac346c4..9fc7ae5b4c3f` | 8,125 | 8,616 | 1,858 | 23.068 | 25.054 | ✅ |
| 3 | `9fc7ae5b4c3f..1c2607479b6d` | 70 | 8,622 | 318 | 22.450 | 24.538 | ✅ |
| 4 | `1c2607479b6d..599575d0aa39` | 15 | 8,622 | 8 | 22.091 | 24.757 | ✅ |
| 5 | `599575d0aa39..ae233c5f5695` | 60 | 8,636 | 342 | 22.157 | 24.672 | ✅ |
| 6 | `ae233c5f5695..4168360e2e7f` | 15 | 8,636 | 289 | 23.235 | 24.719 | ✅ |
| 7 | `4168360e2e7f..a6f1f3a91a36` | 5 | 8,636 | 4 | 22.627 | 25.028 | ✅ |
| 8 | `a6f1f3a91a36..5dfb2c8f276d` | 15 | 8,641 | 340 | 23.065 | 25.005 | ✅ |
| 9 | `5dfb2c8f276d..a609c19f3861` | 19 | 8,645 | 287 | 22.812 | 24.729 | ✅ |

The Product snapshots have an active framework-composition resolver domain. To
keep Begin authoritative and immutable, these deltas use the conservative
prior/current file fallback (about 8.6k owners). Narrow file-level scopes remain
available when the resolver domain is proven isolated; otherwise the run fails
closed before End rather than expanding scope after Begin.

## Limitations

- The ten first-parent Product transitions at
  `a609c19f3861971930fae7b33dcb2950598953c5` have not been re-run after these
  harness fixes. Launch them with the invocation above into a fresh `--work`
  directory after production graphsession edits settle.
- Timings are file-sink `--events` JSONL. They are not NATS JetStream broker
  ACK times and are not performance acceptance.
- Whole-domain Begin (~8.6k owners) remains the observed Product history
  fallback while the framework-composition resolver domain is active.
- `--only` is a contiguous prefix from the initial SHA. It does not jump into
  the middle of the chain.
- Pinned overlay bytes are the tracked `product-mcp-arch.yaml`. A live dirty
  `mcp-arch.yaml` on the Product clone is hash-compared and is not copied.

