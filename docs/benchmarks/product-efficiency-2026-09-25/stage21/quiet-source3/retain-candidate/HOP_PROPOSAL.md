# Per-hop repeated derived work - consolidated source-only proposal

Status: proposal only. Nothing implemented, nothing built, nothing run. The pinned
candidate, `pins.json`, the acceptance tree and all production source are untouched.
Supersedes the body of msg_a3bbc5ddc4ac; incorporates root's corrections in
msg_b3f1d6dd2529 and msg_1cc86bb579e2.

## 0. Corrections folded in

1. **The two-pass shape is arm-independent** (msg_b3f1d6dd2529). All six cells -
   baseline/frozen/retain x body/structural - show two TS index/map/compose passes
   and two `ts_config_inputs` marks. This is a shared optimization boundary. It is
   **not** evidence about option C and cannot explain a candidate-only regression;
   that regression remains separately open and must be found in something that
   differs between arms.
2. **Persistence attribution** (msg_1cc86bb579e2). Inside the 0.392s
   `write_pending_state` interval the 0.025 is the **write-side hash only**
   (`state_fingerprint write bytes=`). The `os.Create`/`Write`/`Sync`/`Close`/
   `os.Rename`/`fsyncDir` durability syscalls carry **no mark**, so their cost here
   is **unmeasured**. The residual between the marked pieces is not assigned to them
   causally, and my earlier phrase "persistence proper is the 0.025 plus part of
   marshal" is withdrawn.
3. **`r.Records` is the full map, not the changed set** (msg_1cc86bb579e2), confirmed
   in source: `session.go:3120` is
   `for path, rec := range r.Records { work[filepath.ToSlash(path)] = rec }`, and it
   overwrites all 4086 entries every hop. Record identity therefore proves nothing
   about which files changed, and nothing about the dependencies a derived index
   consumed.

## 1. Inclusive marks, restated

`graphprofile.Trace.Mark(phase, extra)` reports `now - t.last`, the gap since the
previous mark in that trace, plus a cumulative `total=`. `Since`/`Log` emit standalone
lines. Every `ts_*` figure below is therefore an **inclusive span**, not the cost of
the named step, and no figure here is a causal attribution or a saving.

`write_pending_state` 0.392s opens at the `revalidate_ts_records` mark and covers, in
order: the closing `analysisFingerprint`/`ts_config_inputs` 0.132 (**before** marshal),
`revalidateCapturedInputs`, `validateEffective`, marshal 0.166
(`state_json_marshal bytes= files=`), the unmarked durability syscalls plus the
write-side hash 0.025, then `commitProof` 0.029 (`state_proof_written … build_ns=`),
then `os.Stat` for `CheckpointBytes`. It is not persistence alone.

## 2. What repeats per hop

`prepareFrozenTS` (session.go:2993) is a fixed-point closure loop; each hop calls
`ts.ExtractSession(ctx, s.abs, owned, work, extractDirty, hooks)` at session.go:3104
scoped to the **whole owned set** (`files=4086` in both observed passes). Only
`extractDirty` narrows. Per hop:

- **GraphQL/gRPC index**: iterates every `tsFiles` entry and, for files it did not
  read, re-merges `prev[rel].GRPC` and re-adds `rec.GraphQLSDL`.
  Mark `ts_graphql_grpc_index ts_files=4086`; root's log 0.096 then 0.100.
- **`parallel.MapFiles`** over all `tsFiles`: the `!need(relFile)` branch returns the
  cached record, but the corpus is still walked and a `fileOut` built per file.
- **Router/export aggregate**: `ts_mapfiles_aggregate`, second pass 0.297s with 117
  summary scans; `Stats.SummaryScans`/`DerivedIndexes` are per call.
- **Nuxt alias and auto-component indexes**: `withNuxtRuntimeAliases` plus
  `nuxtAutoComponentIndex` per package over `knownFiles`.
- **`ts_compose_modules`** per hop, while only the last hop's result survives
  (`s.preparedTS = res`); earlier hops contribute `r.Records`, surface verdicts and
  summed stats, and their composed facts are discarded.

Already once, not to be re-solved: **discovery**. `disc.reusableFor` plus
`s.tsRunDiscovery()` gave `shared_discovery=true` on both passes and
`DiscoveryPasses` 0.

Fixed across hops: `files`, `owned`, `ownedSet`, `prevRecs`, seed, `hooks` except
`Sources`. Changing across hops: `work`, `s.capturedSources` (grows), `extractDirty`,
`dirty`, the `rebind` set.

## 3. What actually identifies change, in source

Not `r.Records`. The loop keeps, per hop:

- `pending` - the files this hop asked to parse - with `parsed[f] = true`;
- `changed[f]` from `surfaceChanged(prevRecs[f], work[f])`, a **value** comparison,
  and only over `pending`;
- `next` from `surfaceDependents`, `declaredNameDelta`/`nameDependents`,
  `sideReadDependents`, with `rebindableConsumer` diverting to `rebind`;
- `reuse` entries that set `work[f]` and `delete(extractDirty, f)`
  (`RetryParsesReused`).

Any invalidation design must be built from these, plus the inputs a derived index
actually consumed, and must not treat "the record object came back" as "the record is
unchanged" or as "its dependencies are unchanged".

## 4. Instrumentation that already isolates this (no new marks)

- Hop count: one `ts_detect_frameworks` line per `ExtractSession` call.
  `ts_extract_session parsed=… 0.000` is the proof the post-Begin extraction reused
  `preparedTS`; `ts_preview_discarded parsed=…` (session.go:1442) marks the case where
  the preview was thrown away instead.
- Per-hop whole-corpus stages: `ts_read_dirty`, `ts_graphql_grpc_index ts_files=`,
  `ts_mapfiles_aggregate parsed= facts= routers= scans= derived=`,
  `ts_compose_modules facts= records= unread=`.
- Accumulation: `prepareFrozenTS` sums `FilesRead`/`FilesParsed`/`SummaryScans`/
  `DerivedIndexes` across hops and then corrects `CachedFiles`, so summed minus
  last-hop values give the earlier hops' share without adding counters.
- Counters: `TSDiscoveries`, `RetryParsesReused`, `Checkpoints`, `CheckpointBytes`,
  `VerifiedFiles`.
- Write span interior: `ts_config_inputs`, `state_json_marshal`,
  write-side `state_fingerprint`, `state_proof_written … build_ns=`,
  and `ProofProduce`/`ProofBuild`/`ProofWrites`/`ProofWriteBytes`.

Named gaps: nothing separates the `MapFiles` corpus walk from the aggregate, and
nothing covers the durability syscalls. Closing either needs scratch-only sub-marks in
a throwaway build - never in the pinned candidate, and not in this proposal.

## 5. What to establish before any code

1. Hop distribution per scenario, from **existing logs only**: how many deltas need
   more than one hop, and what share of preview time hops 2+ are. Both observed
   scenarios settled in two passes; I have no evidence about other shapes.
2. Per stage, by reading source: exactly which inputs each whole-corpus stage consumes
   (records, captured sources, config/context, resolver state), so invalidation can be
   keyed on inputs rather than on record identity.
3. Only then, whether per-call reuse is worth its proof cost. No claim is made that
   removing a repeat saves the interval it sits in.

## 6. Bounded cache proof and ownership constraints

- **Ownership**: per-call only - on the `prepareFrozenTS` call or threaded through
  `SessionHooks`. Never on `*TSExtractor`, which is shared across repos and captures;
  a cache outliving one overlay would be read under another.
- **Validity by derived inputs, not identity**: an entry is usable only if every input
  it consumed is proven unchanged - the changed identities derived from that hop's
  `pending`/`parsed` set and `surfaceChanged` value comparison, the captured-source
  bytes in `s.capturedSources` (which grow between hops), and the config/context and
  resolver inputs the stage read. Record identity is explicitly insufficient.
- **Fail closed**: an entry that cannot prove its inputs unchanged is recomputed. No
  partial or approximate reuse, no merge of a stale entry.
- **No persisted schema**: in-memory for one call; nothing new in `state.json`.
- **Equality proof, not digests**: an oracle comparing full `res.Facts` and
  `res.Records` **values**, including ordering and `Repo` fill, cached versus uncached,
  over cold/noop/edit/add/remove/rename/side-read plus a fixture that genuinely needs
  more than one hop.
- **Preserve unchanged**: discovery sharing, `ts_preview_discarded` semantics,
  unknown/global fallback, strict `sideReadProven`, the pre-End byte fence over
  captured sources, the `DeltaInputs` precondition, and the existing retirement loop.

## 7. Not claimed

No timing claim, no saving claim, no statement that the durability syscalls are cheap
or expensive, no explanation of the candidate-only regression, no explanation of the
body-cell outlier. Scope unchanged; all pins preserved.
