# Frozen invalidation scope on Product — 2026-09-21

Scratch harness for frozen v2 scope and replacement-contract checks. It is
diagnostic correctness evidence. One pass of wall-clock numbers is not a
performance-acceptance run and is not an SLA.

It measures how wide the current frozen v2 Begin owner manifest is, compared
with the owners whose contributions actually change between a completed
pre-mutation graph and a cold analysis of the mutated tree.

The tracked Product checkout is never written. Clones, event logs, and state
directories live under the `--work` directory (default
`/tmp/enola-invalidation-scope-2026-09-21`). This docs folder holds the harness
script and a committed numeric snapshot. The harness writes `results.json` next
to `--work` and does not overwrite the tracked snapshot.

## Method

Every Enola invocation used frozen v2 with an explicit 1 MiB Begin cap:

graph analyze|delta --authoritative-scope --max-begin-bytes 1048576
--events FILE --state-dir STATE --context scope-bench --repo-id product-scope
--summary-json CLONE

Initial and delta share one events file so SinkID matches. Cold uses a fresh
state directory on the mutated clone.

1. APFS-clone Product to a gold tree, then clone that gold independently for
   each of the ten scenarios.
2. Initial frozen analysis. Apply events to an in-harness Consumer.
3. Mutate only that clone.
4. Frozen delta on the same state directory. Persist Begin
   owner_scope_count/digest and End batch_count, batch_digest,
   owner_scope_len/digest. Recompute owner and batch digests from the
   published payloads; do not trust the envelope fields alone.
5. Cold frozen analysis of the mutated tree.
6. Apply initial+delta to one Consumer and cold events to another.
7. Compare **graph hash** (non-empty owned nodes/edges, Canonical-style) and
   **owner sets** separately: nonempty keys, and all keys including empty
   replacements. Deleted owners that remain as empty keys on the delta path
   but are dropped by a cold epoch are reported, not hidden.
8. **Necessary scope** = owners whose canonical contribution differs between
   the pre-mutation graph and the cold graph. List order is sorted before
   comparison so two equivalent batches are not treated as a change.

Rename (07) uses `git mv` then `git diff --name-status -M HEAD`, and asserts
that Begin and the necessary set contain both the old and new file owner IDs.

Every publishing run (initial, delta, cold) must satisfy the frozen v2
complete immutable contract or the harness exits nonzero:

- `schema_version=enola.graph.v2` and `scope_mode=complete` on Begin.
- Begin owner scope is unique nonempty `file` owners. Count and SHA-256
  owner digest are recomputed from the sorted kind/id pairs.
- `target_generation == base_generation + 1`.
- Batch `seq` values are unique contiguous `1..N`. File-sink line order may
  interleave under async publish; the digest uses seq order.
- No `phase=scope` batches and no batch `owners` additions after Begin.
- Resolved batches only. Every node/edge owner is inside the Begin scope.
- End `owner_scope_len` / `owner_scope_digest` match Begin. End
  `batch_count` matches the number of batches. End `batch_digest` is the
  SHA-256 of the raw batch payloads in seq order.
- End completeness `status=success` with no unreadable files.
- Necessary owners are a subset of the delta Begin scope.
- `09-lock-only` is a strict no-op: zero Begin/End/batch events, zero parses,
  zero published owners, generation unchanged, empty necessary set.

Any blocker, graph-hash mismatch, missed required owner, rename missing an
old/new owner id, lock-only leak, or ordinary TS edit (01–04, 10) that
announces a whole-domain Begin exits with status 1. `growScope` is
fail-closed: an owner required after Begin that is outside the frozen
manifest aborts with no successful End. Resolver-domain widening uses
sorted per-name `Fact.Identity()` multisets; unchanged candidate
fingerprints do not expand scope.

One diagnostic pass of all ten clones. Three repeats are skipped. Wall times
from that pass are observations, not a performance-acceptance result.

## Kinds

| Kind | Meaning |
| --- | --- |
| noop | Necessary set empty and the delta published no events. |
| stable-facts | Delta parsed/published, but cold graph facts match the pre-mutation graph (body text is not a local fact). |
| local | Necessary owners are the mutated file owners. |
| whole-domain-membership | Add/delete/rename announced the full prior/current file union; old import edges cannot prove a safe subset. |
| whole-domain-resolution | Exported declaration set changed; resolver dependencies fall back to the complete domain. |
| whole-domain-config | Config/manifest change uses the conservative whole-domain fallback. |
| import-closure | Necessary set includes other files, still small. |
| broad | Necessary set is large, or the delta parsed a large file set *and* the necessary set is not local. |
| global | Necessary set covers most of the published Begin union. |
| missed-invalidation | Cold graph moved and the delta published no events, with graph-hash mismatch. |
| oracle-instability | Oracle owner lists disagreed while graph hash and owner sets matched. Not an Enola miss. |
| equality-failed | Initial-plus-delta graph hash does not match cold. |
| rename-missing-owner-ids | Rename Begin omitted old or new file owner. |
| blocker | Harness or Enola failed before a measurement. |

Parsed-file count is reported even when the necessary owner set is local. A
membership change can reparse hundreds of TS files while only a handful of
owners actually change facts.

## Scenarios

Mutations are under packages/crypto except lock-only (pnpm-lock.yaml).

| ID | Mutation |
| --- | --- |
| 01-body | Body of normalizeEmail in password.ts. |
| 02-add-function | New exported function on hmac.ts. |
| 03-rename-symbol | hmacEquals renamed; package index re-export updated. |
| 04-import-target-body | Body of computeHmac, which other files import. |
| 05-add-file-import | New scopeProbe.ts imported from hmac.ts. |
| 06-delete-file | Delete hmac.ts and drop its index re-exports. |
| 07-rename-file | git mv hmac.ts to hmacSha.ts, with import updates. |
| 08-package-config | Add a dependency on packages/crypto/package.json. |
| 09-lock-only | Append a comment to pnpm-lock.yaml. |
| 10-multi-file | Body edits to both password.ts and hmac.ts. |

## How to run

```
python3 docs/benchmarks/invalidation-scope-2026-09-21/run.py \
  --source /tmp/enola-product-benchmark-source \
  --binary /tmp/enola-product-frozen-v2-enola \
  --work /tmp/enola-invalidation-scope-2026-09-21
```

The harness writes `$WORK/results.json`. The tracked
[results.json](results.json) is the committed snapshot from the earlier
diagnostic pass and is not updated by later `--work` runs.

## Results (provisional, not performance acceptance)

Provisional dump from the completed process:
`/tmp/enola-invalidation-scope-narrow2-2026-09-21/results.json`.
Zero blockers. Graph hash matched cold in all ten. These numbers are one-pass
scope/correctness evidence. They are not a performance-acceptance result, and
another harness run is not queued.

| ID | wall s | parsed | gen | events | Begin | necessary | kind |
| --- | ---: | ---: | --- | ---: | ---: | ---: | --- |
| 01-body | 2.876 | 1 | 1→2 | 4 | 2 | 0 | stable-facts |
| 02-add-function | 2.990 | 3 | 1→2 | 4 | 3 | 1 | local |
| 03-rename-symbol | 2.960 | 3 | 1→2 | 4 | 3 | 2 | local |
| 04-import-target-body | 2.908 | 1 | 1→2 | 4 | 3 | 1 | local |
| 05-add-file-import | 21.339 | 282 | 1→2 | 2691 | 8646 | 2 | whole-domain-membership |
| 06-delete-file | 24.571 | 280 | 1→2 | 2691 | 8645 | 3 | whole-domain-membership |
| 07-rename-file | 23.480 | 281 | 1→2 | 2691 | 8646 | 4 | whole-domain-membership |
| 08-package-config | 21.030 | 0 | 1→2 | 2691 | 8645 | 1 | whole-domain-config |
| 09-lock-only | 2.125 | 0 | 1→1 | 0 | 0 | 0 | noop |
| 10-multi-file | 2.906 | 2 | 1→2 | 5 | 4 | 1 | local |

Graph hash matched cold in all ten. End digest matched Begin on every publishing
delta. 07 rename: `git diff --name-status -M` is R100 hmac.ts→hmacSha.ts; Begin
has both owner IDs. 06/07: nonempty owner sets match; all-keys differ because
delta keeps an empty deleted/old owner and a cold epoch drops it.

**09-lock-only.** Enola published 0 events and did not advance generation.
Required owners under the lock-only contract are **0**. An unsorted oracle
compare had labeled this `missed-invalidation` with 9 package.json owners while
`graph_hash_equal` and `all_owner_set_equal` were already true. Canonical-sorted
compare is empty. That is **oracle instability**, not a missed Enola
invalidation.

Provisional narrow2 Begin/parsed: 01=2/1, 02=3/3, 03=3/3, 04=3/1, 10=4/2.
Membership 05/06/07 and config 08 remain the full prior/current union
(Begin 8645–8646). Lock-only is a no-op. Graph equality holds on all ten.

## Limitations

- One diagnostic pass of all 10 clones. Three repeats were not run. Wall
  times in the table are observations from that pass. They are not a
  performance-acceptance result and are not an SLA.
- Python in-harness Consumer and file-sink events, not a NATS observer.
- Necessary scope uses sorted owner equality. Unsorted list compare is unstable
  (see 09) and must not be read as Enola missing a lockfile change.
- Delete/rename retain empty old-owner keys on the incremental path; graph hash
  and non-empty owner sets still match cold exactly.
