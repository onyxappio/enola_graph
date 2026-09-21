# Frozen invalidation scope on Product — 2026-09-21

Scratch harness, not a performance-acceptance rerun. It measures how wide the
current frozen v2 Begin owner manifest is, compared with the owners whose
contributions actually change between a completed pre-mutation graph and a cold
analysis of the mutated tree.

Production Go is unchanged. The tracked Product checkout is never written.
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
| import-closure | Necessary set includes other files, still small. |
| broad | Necessary set is large, or the delta parsed a large file set *and* the necessary set is not local. |
| global | Necessary set covers most of the published Begin union. |
| missed-invalidation | Cold graph moved and the delta published no events. |
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

## Results (one pass)

Begin owner_scope_count is 8645 on unchanged membership and 8646 when a file is
added or renamed. End owner_scope_len/digest matched Begin on every publishing
delta. Graph hash of initial+delta matched cold in all ten. Synthetic owners
were empty on both sides (v2 file-only manifest). No blockers.

| ID | wall s | parsed | gen | events | Begin owners | necessary | ratio | graph= | owners= | kind |
| --- | ---: | ---: | --- | ---: | ---: | ---: | ---: | --- | --- | --- |
| 01-body | 21.365 | 1 | 1→2 | 2691 | 8645 | 0 | — | yes | yes | stable-facts |
| 02-add-function | 21.217 | 3 | 1→2 | 2691 | 8645 | 1 | 8645 | yes | yes | local |
| 03-rename-symbol | 21.295 | 3 | 1→2 | 2691 | 8645 | 2 | 4323 | yes | yes | local |
| 04-import-target-body | 21.004 | 1 | 1→2 | 2691 | 8645 | 1 | 8645 | yes | yes | local |
| 05-add-file-import | 21.753 | 282 | 1→2 | 2691 | 8646 | 2 | 4323 | yes | yes | local |
| 06-delete-file | 21.686 | 280 | 1→2 | 2691 | 8645 | 3 | 2882 | yes | nonempty yes; all-keys no | import-closure |
| 07-rename-file | 21.462 | 281 | 1→2 | 2691 | 8646 | 4 | 2162 | yes | nonempty yes; all-keys no | local |
| 08-package-config | 20.987 | 0 | 1→2 | 2691 | 8645 | 1 | 8645 | yes | yes | local |
| 09-lock-only | 2.141 | 0 | 1→1 | 0 | 0 | 0 | 1 | yes | yes | noop |
| 10-multi-file | 21.098 | 2 | 1→2 | 2691 | 8645 | 1 | 8645 | yes | yes | local |

owners= is nonempty owner-set equality unless noted. 06 and 07 keep an empty
file owner for the deleted/old path on the delta Consumer; a cold epoch drops
that key. Graph hash ignores empty owners, so both still match.

### Rename (07)

git diff --name-status -M HEAD:

R100 packages/crypto/src/hmac.ts packages/crypto/src/hmacSha.ts

M packages/crypto/src/index.ts

M packages/crypto/src/keys.ts

Begin contained both file:packages/crypto/src/hmac.ts and
file:packages/crypto/src/hmacSha.ts. Both IDs are in the necessary set.

### Lock-only (09)

No events, generation unchanged, necessary empty, cold graph hash matches the
pre-mutation graph. This is the v2 lockfile policy holding on Product.

### Body (01) and multi-file (10)

Changing normalizeEmail's body parsed 1 file and still published the full 8645
owner union, but the cold graph facts were identical: this extractor does not
materialize function-body text as a local fact. 10-multi-file only needed hmac.ts
in the oracle set for the same reason.

### Package/config (08)

Adding a dependency to packages/crypto/package.json parsed 0 TS files and still
published the full union. The oracle necessary set is that one package.json
owner. Not a whole-program TS reparse.

### Parsed vs necessary

05/06/07 parsed 280–282 files (membership/import graph), while necessary owners
stayed at 2–4 files plus, for delete, keys.ts which still imported the removed
module. Current frozen Begin remains the full prior/current file union (~8645),
about 2000× a local necessary set. That is the measured width of today's frozen
producer, not a claim that 8645 files must change facts.

## Limitations

- One repeat, not three.
- In-harness Python Consumer, not a live NATS observer.
- File-sink events, not broker acknowledgments.
- Necessary scope is cold-vs-baseline owner equality under this fork's local-fact
  contract. A resolver that materialized more attributes would widen it.
