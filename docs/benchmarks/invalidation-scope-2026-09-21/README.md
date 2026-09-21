# Frozen invalidation scope on Product — 2026-09-21

Scratch harness, not a performance-acceptance rerun. It measures how wide the
current frozen v2 Begin owner manifest is, compared with the owners whose
contributions actually change between a completed pre-mutation graph and a cold
analysis of the mutated tree.

The tracked Product checkout is never written.
Clones, event logs, and state directories live under
/tmp/enola-invalidation-scope-2026-09-21. This docs folder holds only the
harness script and the numeric summary.

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
   owner_scope_len/digest.
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

One pass of all ten clones. Three repeats were skipped: the measured pass took
about 15 minutes of Product initial+delta+cold work.

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

Numeric dump: [results.json](results.json).

## Results (one diagnostic repeat)

| ID | wall s | parsed | gen | events | Begin | necessary | kind |
| --- | ---: | ---: | --- | ---: | ---: | ---: | --- |
| 01-body | 3.331 | 1 | 1→2 | 4 | 2 | 0 | stable-facts |
| 02-add-function | 33.004 | 3 | 1→2 | 2691 | 8645 | 1 | whole-domain-resolution |
| 03-rename-symbol | 28.146 | 3 | 1→2 | 2691 | 8645 | 2 | whole-domain-resolution |
| 04-import-target-body | 3.016 | 1 | 1→2 | 4 | 3 | 1 | local |
| 05-add-file-import | 22.893 | 282 | 1→2 | 2691 | 8646 | 2 | whole-domain-membership |
| 06-delete-file | 22.450 | 280 | 1→2 | 2691 | 8645 | 3 | whole-domain-membership |
| 07-rename-file | 27.193 | 281 | 1→2 | 2691 | 8646 | 4 | whole-domain-membership |
| 08-package-config | 24.966 | 0 | 1→2 | 2691 | 8645 | 1 | whole-domain-config |
| 09-lock-only | 2.273 | 0 | 1→1 | 0 | 0 | 0 | noop |
| 10-multi-file | 3.298 | 2 | 1→2 | 5 | 4 | 1 | local |

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

Body-only and import-target content deltas use reverse-closure Begin (2–4
owners, 4–5 events). Adding or renaming an exported declaration uses the
`whole-domain-resolution` fallback because the old file graph cannot prove all
name-resolution dependents. Add/delete/rename use `whole-domain-membership`,
and config uses `whole-domain-config`; all preserve graph equality. Membership
cases still parse about 280 TS files.

## Limitations

- One diagnostic repeat of all 10 clones (~15 min). Three repeats were not run;
  these numbers are scope/correctness evidence, not a performance SLA.
- Python in-harness Consumer and file-sink events, not a NATS observer.
- Necessary scope uses sorted owner equality. Unsorted list compare is unstable
  (see 09) and must not be read as Enola missing a lockfile change.
- Delete/rename retain empty old-owner keys on the incremental path; graph hash
  and non-empty owner sets still match cold exactly.
