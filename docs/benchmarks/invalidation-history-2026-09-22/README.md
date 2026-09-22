# Frozen invalidation history — 2026-09-22

First-parent Product commit chain, oldest to newest. Diagnostic correctness
harness. One pass is not a performance-acceptance run.

The tracked Product checkout `/tmp/enola-product-benchmark-source` is never
written. `mcp-arch.yaml` there is untracked Product graph-input policy
(`apps/mobile/e2e/artifacts/**`, `worker-reports/**`) and is overlaid onto
isolated snapshots after each checkout.

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
3. Isolated snapshot at the oldest SHA. Overlay `mcp-arch.yaml`. One frozen
   **initial** analysis.
4. Persistent `--state-dir` on that live snapshot. For each child SHA, check
   out the child in the live tree and run **delta**.
5. Isolated cold snapshot at the same child SHA. Fresh frozen **analyze**.
   Incremental graph (initial + all deltas so far) must hash-equal cold.
6. Required owners from `git diff --name-status -M` parent→child, minus
   lockfiles. That set must be a subset of Begin. Lock-only transitions must
   no-op with generation equal to the previous completed generation.
7. Raw Begin/End owner and batch digests are recomputed from payloads.

The Enola binary is **built from the current Enola source**. Provenance records
source revision, dirty diff hash, and binary SHA-256.

## Flags

```
python3 docs/benchmarks/invalidation-history-2026-09-22/run.py --self-test

python3 docs/benchmarks/invalidation-history-2026-09-22/run.py \
  --source /tmp/enola-product-benchmark-source \
  --work /tmp/enola-invalidation-history-2026-09-22 \
  --output /tmp/enola-invalidation-history-2026-09-22/results.json \
  --only 0
```

`--only` accepts transition indexes (`0` is oldest→next), SHA prefixes, or
`parent[:12]..child[:12]` ids. `--output` must not be the tracked
`results.json` or the baseline file. Per-case dumps live under `$WORK/cases/`.

## Completed run

The pinned run completed all ten transitions with `graph_hash_equal=true` and no
blockers. Initial analysis at `4d104e600f89` took 25.238 s (4,120 parsed
files, 8,609 owners). Delta rows below include the delta wall time and the
fresh cold oracle time.

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

