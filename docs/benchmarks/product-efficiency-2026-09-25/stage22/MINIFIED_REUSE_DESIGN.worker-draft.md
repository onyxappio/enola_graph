# Stage22 bounded design: persist minified framework contributions

Target: MAIN `6db3ed834daab2adf8083ac46dc15ac21b670eb2` (Stage20 line).
NOT the rejected Stage21 retain candidate. Design only; nothing implemented, built
or tested for this document.

All line numbers below are main at that SHA.

## 1. What is being paid for

Instrumented Stage22 arm, delta hops (diagnostic, shared host, no timing claim):

| hop | to_read | prior_minified | fw collect | minified subset of that collect |
|---|---|---|---|---|
| body 1 | 53 | 52 | 0.096s / 11,119,913 B | 0.096s / 52 files / 11,113,023 B |
| body 2 | 62 | 52 | 0.103s / 11,616,558 B | 0.098s |
| structural 1 | 53 | 52 | 0.095s | 0.095s |
| structural 2 | 63 | 52 | 0.102s | 0.097s |

`prior_minified_with_framework = 0` in every hop. Read precisely: the prior
FileRecords carry no `GraphQLServer` / `GraphQLSDL` / `GRPC`. It does NOT say the
bytes have no contribution - that question is separate and answered in 6.

So on every delta hop the same 52 files / 11,113,023 bytes are re-read and
re-scanned, and that subset is essentially the whole framework-collection cost of
the hop.

## 2. Why they are re-read

Three facts compose:

1. `need()` (`session.go:261-312`) ends with
   `if rec.NuxtScope == "" || rec.NuxtScope != nuxtScopeKey(pkg, inNuxt) { return true }`.
2. The minified early return (`session.go:512-519`) happens BEFORE
   `rec.NuxtScope = nuxtScopeKey(...)` at `:526` and before
   `fillRecord(...)` at `:576`. A minified record therefore always has
   `NuxtScope == ""`, so `need()` is unconditionally true for it, forever.
3. The framework collection loop (`session.go:443-478`) branches on
   `sources[rel]` presence, not on minified-ness. Anything `need()` selected is in
   `sources`, so it takes the fresh branch and runs
   `collectGraphQLContribution(rel, src)` + `grpcFileContribution(src)` over its
   bytes. Only the `prev[rel]` else-branch is cheap, and minified records never
   reach it.

A naive skip is wrong for exactly the reason (2) creates: the cached branch reads
`rec.GraphQLServer`, `rec.GraphQLSDL`, `rec.GRPC`, and a minified record has none
of them. Skipping the read without persisting those fields silently drops a
minified bundle's framework contribution.

## 3. Pre-existing correctness defect this exposes (root reproduced it)

`compositionSignatureOn` (`session.go:1342`) gates cache reuse on
`usePrev := !allDirty && !dirty[rel] && prev[rel] != nil` with NO completeness or
`NuxtScope` gate. A not-dirty minified record is therefore consumed as an EMPTY
contribution pre-Begin, while a cold run reads the same bytes and collects the
real one.

This is not only a legacy-migration concern: on main, every minified record has
that empty shape, so for any repo with a minified GraphQL/gRPC producer the
cached CompositionSignature already diverges from the cold one on every delta hop.

Root reproduced it on main: `/tmp/enola-stage22-root-signature/signature_test.go`
(`genUsersClient` + 1601-element numeric array; asserts `isMinifiedSource` and a
non-empty `grpcFileContribution`), expected FAIL,
`cold=e12d5ec3... cached=df2b1412...`, package 0.439s, wall 3.01s. I treat that as
the regression test for this design and preserve it in the matrix. Per root, the
failure establishes the signature mismatch only, not a stale Product graph.

For THIS repo the 52 minified files provably contribute no GraphQL
(`graphql_parsed = 0` on both delta hops, and that counter is gated on
`possibleGraphQLServerSignal(src)` over exactly the read set). gRPC contribution
from them is unmeasured - there is no counter for `grpcFileContribution`
returning non-nil - so I do not claim this repo is unaffected on the gRPC side.

## 4. Proposed change (bounded, two edits plus one field)

### 4a. Populate the pure contributions at the minified early return

Inside `parallel.MapFiles`, at `session.go:512-519`, after `rec.Hash` is set and
`rec.Minified = true`:

    rec.Minified = true
    if g, ok := freshGQL[relFile]; ok {
        rec.GraphQLServer = g.Server
        rec.GraphQLSDL = g.SDL
    }
    rec.GRPC = freshGRPC[relFile]
    minNuxt, minInNuxt := nuxtPackageForFile(nuxtPkgs, relFile, pkgDirSet)
    rec.NuxtScope = nuxtScopeKey(minNuxt, minInNuxt)
    rec.FrameworkSummary = true
    return fileOut{rec: rec}

Notes:

* `freshGQL` / `freshGRPC` are already computed for this file by the loop at
  `:443-478`, which ran before `MapFiles`. This adds NO scanning work on the hop
  that reads the file; it only stops later hops from redoing it.
* `nuxtPackageForFile` must be called inside the minified branch: the existing
  `fileNuxt, inNuxt := nuxtPackageForFile(...)` at `:524` is declared AFTER the
  early return, so those two locals are not in scope there. `nuxtPkgs` and
  `pkgDirSet` are closure-captured (used at `:495` and `:524`) and are available.
  This is the one piece of added work per minified file, and it is a map lookup
  over the already-collected Nuxt package set, not a read or a scan.
* `fillRecord` is deliberately NOT called. Per `msg_326256ed1b36` I do not want
  `ImportComplete` set for a file whose imports were never resolved: its resolver
  meaning for a skipped minified file is unproved, and several conservative gates
  read it (see 5c). Only the four pure fields plus the explicit marker are set.

`NuxtScope` is what ends the re-read: `need()`'s last clause stops returning true,
so the file leaves `to_read` entirely. That removes BOTH hops' cost, not just the
second, which is why this is preferred over a within-call memo (7).

### 4b. Gate `usePrev` in the signature collector on a completeness proof

At `session.go:1342`:

    usePrev := !allDirty && !dirty[rel] && prev[rel] != nil && frameworkSummaryTrusted(prev[rel])

    // frameworkSummaryTrusted reports whether a cached record's GraphQL/gRPC
    // summary was written by a version that records one. A record written before
    // the summary existed must be re-read, not consumed as empty.
    func frameworkSummaryTrusted(rec *FileRecord) bool {
        if rec.Unreadable {
            return true // no contribution is the correct contribution
        }
        if rec.Minified {
            return rec.FrameworkSummary
        }
        return rec.NuxtScope != ""
    }

The three branches:

* `Unreadable`: must stay `usePrev`, otherwise the gate would force
  `overlayReadFile` on a file known to be unreadable and turn a signature
  computation into an error. Today's behaviour (empty contribution) is correct and
  is preserved exactly.
* `Minified`: needs the new explicit marker, because `NuxtScope` alone cannot
  distinguish "written by new code" from "never set" for these records.
* everything else: `NuxtScope != ""` is already the established
  "written before this field existed" marker - the field's own doc comment at
  `session.go:116-119` says so - and `:526` sets it unconditionally for every
  non-minified parsed file, so no current non-minified record is disturbed and
  there is no mass re-read on upgrade. Only pre-`NuxtScope` legacy records are
  re-read, which the extraction path already does via `need()`.

### 4c. The new field

    // FrameworkSummary records that GraphQLServer, GraphQLSDL and GRPC were
    // written for this file even though it was not parsed. Empty means a record
    // written before minified files carried a framework summary.
    FrameworkSummary bool `json:"framework_summary,omitempty"`

Additive, `omitempty`, so old state files unmarshal to `false` and take the
refresh path. Zero-schema alternative if root prefers no new field: overload
`rec.NuxtScope != ""` as the minified marker too, since 4a sets it. That works
mechanically but conflates a Nuxt scope key with a framework-summary completeness
proof, and root asked for an explicit proof, so I recommend the field. Root's
call.

I am NOT reusing `FileRecord.GraphQLParsed` (`session.go:107`, declared but never
set or read anywhere) as the marker - its name promises a parse claim I am not
making. See 8 on the stats counter.

## 5. Correctness argument

### 5a. Context inputs: there are none to capture

`collectGraphQLContribution(relFile, src)` (`graphql.go:571`) consults only
`facts.IsTestPath(relFile)`, `isGraphQLDocFile(relFile)`, `isHasuraSDLPath(relFile)`,
`possibleGraphQLServerSignal(src)` and `graphQLServerASTSignals(src, relFile)` - a
pure function of `(rel, src)`. `grpcFileContribution(src)` (`grpcclient.go:177`) is
pure in `src` alone. No alias map, `knownFiles`, config, package gate or side read
feeds either. So a persisted result is valid for exactly as long as `(rel, src)`
holds, and nothing else can invalidate it. This is the whole reason the reuse is
bounded: it is NOT a general "trust the cache" step.

`GRPCRecord` (`grpcclient.go:169`) is fully json-tagged (`FQ`, `Methods`,
`Classes`, `ServiceConsts`, `AmbiguousClass`), so the contribution round-trips
through state without loss. `GraphQLSDL []string` and `GraphQLServer bool`
likewise.

### 5b. Byte proof: already there, unchanged

`rel` is the map key, so it cannot drift. `src` is proven by
`revalidateRecordHashes` (`graphsession/session.go:2374`), called at `:2079` under
`tr.Mark("revalidate_ts_records", ...)`: for every record it re-reads
`filepath.Join(s.abs, path)`, re-hashes, and returns `ErrInputsChanged` on
mismatch, refusing the End. It skips only `rec == nil`, `rec.Hash == ""` and
`rec.Unreadable` - it does NOT skip `Minified`, and `rec.Hash` is set at
`session.go:514-515` BEFORE the early return, so every minified record is in the
proof set today and stays in it. No new fence, no new invalidation index.

Honest caveat, stated because it bounds the claim: under `s.fast` the function
revalidates only `s.capturedSources`. So in fast mode the byte proof for a
minified record covers it only if it was captured. That is exactly the same
strength the existing `Minified`-record `Hash` and the existing cached
`prev[rel]` framework branch already rely on - this design does not weaken it -
but I am not claiming a stronger proof than fast mode gives.

Also unchanged: the `extractDirty` commentary at `graphsession/session.go:3085-3089`
("the fence before End re-proves its bytes like any other record, so nothing here
is trusted that is not proven later") and `consumedInputsStillCurrent` at `:2433`
("The fence before End stays exactly where it is and stays authoritative").

### 5c. Consumers of minified records: all preserved

* `session.go:903-904` (`surfaceChanged`) and `session.go:996-999`
  (`surfaceRequiresDependentParse`) both test
  `!rec.ImportComplete || ... || rec.GraphQLServer || len(rec.GraphQLSDL) > 0 || rec.GRPC != nil || ...`.
  A minified record has `ImportComplete == false` (unchanged - 4a does not set
  it), so `!rec.ImportComplete` already makes both conditions true. Populating the
  framework fields cannot change either outcome. Verified by reading both.
* `session.go:633`: `if need(rel) && !out.rec.Unreadable && !out.rec.Minified` -
  the `parsed++` accounting already excludes minified, so `ParsedFiles` is
  unaffected by whether the file was in `to_read`.
* `graphsession/session.go:2602-2612`: `FileState{Minified: rec != nil && rec.Minified, TS: rec}` -
  `Minified` still true; the `TS` record now simply carries four more fields.
* `publishLocal` (`graphsession/session.go:2602`) skips
  `rec.Unreadable || rec.Minified` - unchanged, so no facts are published from a
  minified file and `resultFromRecord` (`session.go:1016`, returns `rec.Facts` and
  `routerFromDTO(rec.Router)`) still returns the empty facts a minified record has.
* The framework collection loop's `prev[rel]` branch (`:470-477`) reads exactly
  `rec.GraphQLServer`, `rec.GraphQLSDL`, `rec.GRPC`. Those are now populated, so
  the cached branch produces the same `graphqlServer` / `grpcIdx` state the fresh
  branch produced. That equality IS the correctness condition for skipping the
  read, and it holds by 5a plus 5b.

### 5d. Transitions

* minified -> changed bytes: the hash changes, the resolver dirties the file, it
  is read, the early return recomputes all four fields from the new bytes. Same
  path as today.
* minified -> no longer minified: bytes changed, so it is read;
  `isMinifiedSource(src)` is now false, the early return is not taken, the file is
  parsed and `fillRecord` runs as today. No stale `FrameworkSummary` survives
  because the record is rewritten from scratch.
* unminified -> minified: read, early return taken, facts dropped, framework
  fields written. Same as today plus the new fields.
* `NuxtScope` recomputation: `need()`'s clause is
  `rec.NuxtScope == "" || rec.NuxtScope != nuxtScopeKey(pkg, inNuxt)`. A Nuxt
  package layout change still changes `nuxtScopeKey` and still forces the re-read,
  so config/name/side-read sensitivity is preserved rather than bypassed. No
  blanket exclusion.

### 5e. Legacy refresh proof

Two independent paths, and both must be covered because they gate differently:

* extraction: a legacy minified record has `NuxtScope == ""`, so `need()` is true,
  so it is read once and rewritten complete. Self-healing in one hop, no
  migration code.
* signature: `frameworkSummaryTrusted` returns false for
  `Minified && !FrameworkSummary`, so the collector re-reads the file and collects
  the real contribution, matching cold. This is the part that does NOT self-heal
  on its own, which is why 4b is required and not optional.

## 6. Framework signature behaviour when absent contributions become persisted

This is the one behaviour change that is visible outside the read counts, and it
must be handled deliberately.

For a repo with a minified GraphQL/gRPC producer, the CompositionSignature VALUE
changes: it starts including contributions it previously omitted from the cached
path. `next.FrameworkSig = s.frameworkSig` (`graphsession/session.go:2066`)
persists it, and at `:1272-1281` a stored-vs-computed difference sets
`forceAll = true` and records a
`graphstream.Fallback{Reason: "graphql/grpc/nuxt composition context changed; re-extracting affected TypeScript files"}`.
The same comparison exists in `frameworkSignatureChangedForDirty` (`:2931-2942`).

So without a version change, the first hop after upgrade on an affected repo would
do a full re-extract AND report a composition-context change that did not happen.
The graph would be correct, the fallback reason would be a lie.

With a cacheVersion bump the state is discarded and the first run is a cold start,
which is honest about what happened. That is my recommendation, and it is the
reason for the bump - not the record format, which 4c handles without one.

## 7. Cross-delta reuse vs a within-call memo

A within-call memo over `(rel, hash)` inside one extract call would deduplicate
nothing on a single hop: the loop at `:443-478` and `MapFiles` at `:502` each
touch a given file once, so there is no repeat WITHIN a call to remove. Across
calls a memo keyed in memory would only help a long-lived process on its second
hop, and the Product scenario starts a fresh process per hop - the measured hops
above are separate invocations. So a memo saves nothing measurable here.

Cross-delta reuse via the persisted record is what removes both hops, and it is
safe for the bounded reason in 5a/5b: the contribution is pure in `(rel, src)` and
`src` is byte-proven before End. That is why I propose 4a/4b and not a memo.

## 8. Observable consequences to disclose

* body hop `files_read` falls from 115 toward ~63 and `cached_files` rises from
  4023 toward ~4075. These are correct, not regressions, and I will assert them
  rather than let them read as drift.
* `stats.GraphQLParsed` (`session.go:452`) increments per fresh read with a
  signal; not reading the file means not incrementing. I propose leaving the
  counter as an honest count of work done. It is already 0 on both delta hops for
  this repo, but the increment is signal-gated so another repo could see it drop.
  I do NOT propose persisting it into `FileRecord.GraphQLParsed` to fake the old
  number.
* `summary_scans` / `derived_indexes` are untouched by this change.

## 9. cacheVersion

Current `cacheVersion = "v321"` at `internal/engine/cache.go:2621`. v322 is
claimed by accuracy work and v323 by plugin-conditional work; I am reserving
neither. My recommendation is a bump for the reason in 6, with the next free
version allocated by root after coordinating with those two owners - I will not
write a version number until root names one.

Whatever version is allocated needs a `// vN:` line in `cache.go` naming covering
tests, because `internal/cachecov/coverage_test.go` (`TestCacheVersionCoverage`)
requires every `// vN:` line to name an existing test. Existing minified entry is
`65: {"TestExtract_SkipsMinifiedBundle", "TestIsMinifiedSource"}`; the new line
would name the tests in 10.

## 10. Narrow test matrix

Root's coverage clarification is honoured: the existing 11-case
minified-consumer oracle uses a bundle of numeric array exports with NO
GraphQL/gRPC producer, so it proves graph behaviour with minified inventory and
NOT preservation of non-empty framework contributions. It is preserved unchanged
and is NOT counted as this coverage.

New focused tests, all in `internal/extractors/tsextractor` unless noted:

1. `TestRootStage22LegacyMinifiedSignature` - root's existing reproduction,
   adopted verbatim as the regression. Currently FAILs; must pass after 4b.
2. Cold vs cached `CompositionSignature` equality with a minified GraphQL server
   + SDL producer (not only gRPC), new-format record.
3. Same, with a genuine minified gRPC producer consumed by another file, so the
   consumer's resolution is asserted and not just the signature hash.
4. Same, with a legacy record (`{File, Minified: true}`, no `FrameworkSummary`) -
   proves the 4b refresh path.
5. Ambiguity case: minified gRPC producer that sets `AmbiguousClass`, asserting
   the ambiguity survives the persist/reuse round trip rather than collapsing.
6. Unreadable cached record: signature must stay `usePrev` and must NOT attempt a
   read (asserted via a repo where the file is absent, which would error).
7. Extraction equality: cold extract vs delta extract over a minified framework
   producer - identical `graphqlServer` effect and identical gRPC-derived facts -
   plus `to_read` no longer containing the minified file on the second hop.
8. Legacy minified record in the extraction path: re-read exactly once, record
   rewritten with the four fields, second hop skips it.
9. Transitions: minified bytes changed; minified -> unminified; unminified ->
   minified (5d), asserting facts appear/disappear as today.
10. End fence: mutate a minified file's bytes after the plan freezes and assert
    `revalidateRecordHashes` refuses the End with `ErrInputsChanged`
    (`internal/graphsession`). This is the byte proof made explicit.
11. Unchanged: `TestExtract_SkipsMinifiedBundle`, `TestIsMinifiedSource`, and the
    preserved 11-case oracle must pass without edits.
12. `internal/cachecov` `TestCacheVersionCoverage` after the `// vN:` line is
    added.

Product level, only after the focused matrix is green and only if root asks: the
existing `cli-pairs` validators already gate cold equality for
initial/body/structural and the silent noop, so no new harness is needed and none
is proposed.

## 11. Scope boundaries

* No blanket minified exclusion anywhere.
* No new fine-grained invalidation index.
* No change to the End fence, the announce fence, `sideReadProven`, hashing
  semantics, or `ImportComplete` for minified files.
* No production-only test hooks; no scratch instrumentation in the candidate.
* Implementation happens in an isolated Stage22 candidate tree only after root
  reviews this design.
