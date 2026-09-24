# Fresh CLI no-op: where the 1.66 s goes, and the one change worth proposing next

Read-only investigation. No production file was edited, nothing was built, run,
profiled or benchmarked for this note. Every number below is read out of the
archived Stage15 profile decomposition in `profile-results/profile-comparison.json`
(six eligible runs, quiet slot `msg_184bbab78c71`); every source claim was read
out of the tree at `49b486b`.

## 1. What the archived profile actually measured

Candidate arm, no-op delta, three runs, medians (ranges in the comparison file):

| mark | trace | median s | share of 1.662 s wall |
|---|---|---|---|
| `graph_session_run` | cli | 1.288 | container |
| `extractor_need` | session | 0.719 | container |
| `resolve_graph_target` | cli | 0.339 | 20.4% |
| `graphinput_build` | graph_engine | 0.338 | nested in the above |
| `load_state` | open | 0.282 | 17.0% |
| `state_json_unmarshal` | - | 0.245 | 14.7% |
| `ts_discovery` | inputs | 0.210 | 12.6% |
| `engine_hash_files` / `hash_content_inputs` | - / inputs | 0.151 | 9.1% |
| `analysis_fingerprint` | inputs | 0.140 | 8.4% |
| `skip_publish_return` | session | 0.138 | 8.3% |
| `inventory` / `engine_walk_repo` | inputs / - | 0.115 | 6.9% |
| `graph_inputs_proven` | reconcile | 0.098 | 5.9% |

Marks are nested and are **not additive**; the percentages are each mark against
the wall, not a partition. Differences between separate mark medians and wall medians do not precisely attribute startup or teardown; use per-run boundaries for such a decomposition.

These unchanged-input runs emit no state write marks and preserve state bytes. The no-publication code path can still save bookkeeping for neutral input or policy changes; do not generalize these measurements to every no-publication run. The current investigation does not attribute latency to state write IO.

## 2. The finding: the config fingerprint is computed twice, and each computation
enumerates the tree twice

`ts_config_inputs` is the one mark the summarizer classified as *repeated* rather
than differenceable, in all twelve profiled deltas:

| arm | run | occurrence 1 | occurrence 2 | outputs |
|---|---|---|---|---|
| baseline | noop 1/2/3 | 0.141 / 0.137 / 0.141 | 0.137 / 0.134 / 0.141 | n=83 |
| candidate | noop 1/2/3 | 0.137 / 0.138 / 0.142 | 0.134 / 0.138 / 0.137 | n=83 |

Two occurrences per delta, ~0.137 s each, both returning the same 83 paths:
**~0.274 s, 16.5% of the fresh CLI no-op**, and ~0.27 s of the 3.43 s body delta.
For no-op, these correspond to the two `analysisFingerprintInputs` calls:

- `internal/graphsession/resident.go:259` - `analysis_fingerprint` 0.140, inside
  `readRuntimeInputs`.
- `internal/graphsession/session.go:1854` - re-read inside the `skipPublish`
  branch; `skip_publish_return` 0.138 at `session.go:1866` is the mark that closes
  that window.

For a publishing body delta the second call instead comes through `analysisFingerprint` at `session.go:2090`; it is not the `skipPublish` branch.

The second no-op call is the transaction fence: it re-derives the fingerprint and fails
with `ErrInputsChanged` if a raw analysis input moved during a no-publication
transaction. **This note does not propose removing it, weakening it, or answering
it from inventory membership.** Both calls stay.

What is proposed is *inside* one call. `tsConfigInputs`
(`internal/extractors/tsextractor/session.go:1195`) descends from the repository
root twice:

1. `collectTSAliasRoots` -> `walkTSAliasRoots` (`ts.go:3249`, `ts.go:3256`):
   recursive `overlayReadDir`, skipping dot-prefixed names and `tsSkipDirs`,
   parsing `tsconfig.json` / `tsconfig.base.json` at each directory.
2. `overlayWalkDir(ctx, repoPath, ...)` (`session.go:1247`): a second full
   descent, same root, same prune predicate
   (`strings.HasPrefix(name, ".") || tsSkipDirs[name]`, root exempt), collecting
   `tsconfig.json` / `tsconfig.base.json` / `jsconfig.json` / `package.json`.

Neither prunes `testdata`, and both run under the same `inputscope.Scope`, which
is why they can answer for each other and why the package-discovery walk - which
does prune `testdata` - cannot answer for either. Confirmed by reading
`Scope.ReadDir` and `Scope.WalkDir` (`internal/extractors/inputscope/scope.go:45,61`):
both filter each entry with the same `Allowed(path, isDir)` predicate, so the
directory sets they can reach are the same.

Across a fresh no-op that is **four** full config-tree descents (two calls x two
walks), on top of `walk_tree` 0.116 in `graphinput_build` and
`inventory`/`engine_walk_repo` 0.115.

### What is NOT measured

There is no mark inside `tsConfigInputs`. The 0.137 s covers, undistinguished:
up to 22 root `overlayStat`s, `findTSRoot` (`ts.go:101`, bounded depth 2 or 8),
the alias descent *and its tsconfig parsing*, the second descent, and every
`followTSConfigExtends` read. Fusion removes the `ReadDir`/`lstat` syscalls of
one descent and **nothing else** - not the alias parsing, not the extends reads,
not `findTSRoot`.

So **the saving is currently unquantified.** Bounding it needs two marks around
the two descents (instrumentation-only, no behaviour change) and one profiled
no-op pair. No number should be attached to this proposal before that.

## 3. Reuse the Stage14 patch, do not write a fresh implementation

`/tmp/enola-stage14-config-walk/stage14-config-walk.patch`
(sha256 `54f431a0c05b4fbf0b78b72d53c0417d41953ba115dc631131ad75e21b005403`, 703 lines)
already implements exactly this fusion. Assessed read-only:

- **It still applies to `49b486b`.** `git apply --check --verbose` succeeds; both
  existing-file hunks land on context with offsets only (session.go +283,
  ts.go +233). This is textual applicability - it has **not** been compiled or
  tested here.
- **Its oracle is still faithful to production.** `configwalk_oracle_test.go`
  carries a verbatim pre-patch copy lifted at `eb73b66`. Diffed against today's
  `tsConfigInputs` + `collectTSAliasRoots` + `walkTSAliasRoots`, comment- and
  whitespace-normalised: the only differences are the three renamed helpers and
  the dropped `graphprofile.Since` call. Nothing has drifted since, so the
  differential test still compares the new answer against the shipped one rather
  than against a restatement of it.
- **Every symbol it needs exists**: `probeFrom` (`overlay.go:193`),
  `overlayReadDir` / `overlayStat` (`observe.go:54,27`), `tsAlias` /
  `tsAliasRoot` / `aliasesAtDir` (`ts.go:3231,3241,3294`).
- **It already declines fusion where the two enumerations genuinely differ**, and
  falls back to the walk that has always answered:
  - `walkDirDescendsInto(repoPath)` - `filepath.WalkDir` lstats its root and
    stops at a symlink, while `overlayReadDir` follows one; a symlinked
    repository root would otherwise gain config candidates it never had.
  - `probeFrom(ctx) == nil` - under a probe, `overlayWalkDir` marks a directory
    enumerated before it tries to list it, so the two enumerations record
    different observations for a failing listing. Left bug-for-bug, per the
    standing constraint that `overlayWalkDir` is fixed separately. In production
    `probeFrom(ctx)` is nil and `overlayWalkDir` is a plain
    `inputScope.WalkDir` with no recording (`observe.go:80-83`), so the fused
    path is the one production takes.
- **It carries seven equivalence tests** against the oracle: separate-walk
  equality, scope exclusions, hidden root, symlinked root, config add/remove,
  unreadable directory, and a fuse-actually-taken assertion.

A fresh per-call shared traversal would have to rediscover the symlinked-root and
probe divergences, and would lose the differential oracle unless it recreated one.
There is no advantage over the existing patch and a real loss of evidence.
**Recommendation: rebase the Stage14 patch onto `49b486b` as its own change.**

## 4. Exact correctness risks to re-verify before it lands

Ordered by how much they would cost if wrong.

1. **Policy scope equivalence.** Fusion moves the config-candidate enumeration
   from `Scope.WalkDir` to `Scope.ReadDir`. Both filter with the same
   `Allowed(path, isDir)`, but they differ in *shape*: `ReadDir` refuses the
   directory itself up front and returns an error; `WalkDir` returns `SkipDir`
   per visited path. `TestConfigWalkMatchesUnderScopeExclusions` must be re-run
   against current policy, including an excluded repository root.
2. **Alias-root order.** `aliasesForDir` resolves longest-prefix with first-wins
   on ties, so the order `w.roots` is appended in is semantic. The fused walk
   must keep the pre-order, parent-before-child sequence `walkTSAliasRoots`
   produced.
3. **Read order.** Today every alias read precedes every `followTSConfigExtends`
   read. The patch preserves this by collecting candidate *paths* during the
   descent and reading them after it. If that is ever changed to read inline, a
   probe capture of the same session would see a different read sequence.
4. **Symlinked root.** `walkDirDescendsInto` is the whole guard. If it is
   dropped or inverted, a symlinked repo root silently gains config inputs, which
   changes the fingerprint and therefore the fence verdict.
5. **Unreadable / partially-readable directory.** `filepath.WalkDir` hands the
   callback an error and continues; the fused walk returns on `ReadDir` error.
   Both stop descending that subtree, but the equivalence must be re-asserted,
   not assumed - `TestConfigWalkMatchesWithUnreadableDirectory`.
6. **Fingerprint stability.** `n=83` is the observed output count on this
   fixture. The gate for landing is that the fused and oracle outputs are equal
   as sorted sets on every fixture tested, not that the count matches.
7. **Not touched by this proposal, explicitly:** the second
   `analysisFingerprintInputs` call, inventory membership as a substitute for
   either walk, the `testdata`-pruning package walk, `overlayWalkDir`'s
   enumerated-before-listing record, and the unmarked state write path.

## 5. What was considered and not recommended

- **`state_json_unmarshal` 0.245 s** (decode of a ~56 MB state, 5032 files) is
  the second-largest single mark. Deferring the heavy per-file payload behind an
  accessor would not change the persisted format. It is not recommended *first*:
  it touches state load, which every consumer reads, and the risk that a consumer
  silently reads a zero value out of an undecoded field is much harder to fence
  than a traversal equivalence. It is also worth noting the no-op reads all 5032
  entries shallowly in `filesToHash`, so the win depends entirely on how much of
  each entry is nested payload - unmeasured.
- **Sharing enumeration across `graphinput_build`'s `walk_tree` and the engine
  inventory** has roughly 0.23 s of observed traversal windows, but potential savings are unmeasured. It crosses a prune-semantics boundary and needs a separate proof; it has not been ruled out permanently.

## 6. Authorized next step

Root authorized an isolated adaptation based on `49b486b` via `msg_f51aad46d8c3`, including refreshed oracle tests and actual traversal counts. Both fresh fingerprint boundaries and probe/symlink fallbacks must remain. Experimental marks are separate from the production comparison. No shared-main edit or benchmark run is authorized by this note; timing requires a coordinated quiet slot.

## 7. Independent review: partial directory-read errors

Root review found a source-level equivalence gap in the retained patch. `Scope.ReadDir` discards entries when `os.ReadDir` returns both entries and an error. Go 1.27.1 `filepath.walkDir` instead reports the error to the callback and, when the callback returns nil, traverses the partial entries. The config callback returns nil on errors. Thus the two traversals are not interchangeable on this error path; an unreadable directory yielding no entries does not cover it.

Requested correction (`msg_878247915667`): if any alias enumeration fails, discard fused candidates and use the original config traversal, preserving prior alias behavior. Add deterministic error-injection coverage and a failing mutation control. This finding is based on current source and the Go implementation, not a reproduced live filesystem failure; validation remains pending.

## 8. Baseline prepared for candidate comparison

Root built the published `49b486b4a43bc5e6ec1679db0dd947b66bbbf223` source with Go 1.27.1 and `-trimpath`. Tracked source diff was empty. Baseline binary: `/tmp/enola-stage16-root-review/enola-baseline`, SHA256 `b7b66f743741f50db5f323b34d632572af4a91ab80f3406104217ba7625a7c83`. The adjacent `baseline-build.json` records build command and all tracked Go/module source hashes. This is build provenance, not a benchmark result.


## Follow-up after config-walk: fresh-process state decoding

Root static inspection on clean Product `a609c19` found a 56,634,042-byte committed state with 4,086 file entries. Compact JSON values under `files` occupy 55,263,696 bytes, including 48,682,055 bytes of `ts` values. See `stage16-state-payload-audit.json` for the exact source/hash and measurement definition. These are serialized payload sizes, not memory usage or measured savings.

`OpenSession` replays the journal and calls `recoverAcknowledgedPendingFP` with an empty checkpoint. `readStateFileFP` reads and fingerprints all bytes, then `decodeStateBytes` unmarshals every file contribution. The resident checkpoint optimization cannot remove this first-process decode because no decoded state exists yet. The archived no-op profile attributes roughly 245ms to JSON decoding; Stage16 does not address it.

A follow-up candidate can investigate separating admission/planning metadata from heavyweight cached TS contributions, loading contributions only when required. This is a design candidate, not an implementation or accepted speed claim. First audit every no-op decision's state reads: `Files` hashes/owners, extractor digests, policy/config/context identities, unreadable markers and planner relationships must remain available. A lightweight return must not skip identity checks, journal replay, acknowledged pending promotion, cache/schema migration or detection of corrupt committed state. Preserve old-format loading and atomic publication of any index/contribution pair. A mere sidecar keyed by file size/mtime is not adequate proof. Compare fresh CLI and resident behavior separately, and exercise failed publication/restart, equal-length corruption, input/config changes and exact cold/delta equivalence before adopting it.
