# Stage22 hop profile - instrumentation diff and measurement plan

Scope: /tmp/enola-stage22-hop-profile only. Stage21 trees, binaries and pins are
untouched; the source here is a clone of /tmp/enola-stage21-retain/src (main
6db3ed8 plus the Stage21 freeze plus option C), edited only with scratch marks.
No production optimization, no production edit, nothing pinned.

## 1. What the diff adds (INSTRUMENTATION.diff, 177 lines, two files)

All new marks are `graphprofile.Log` lines prefixed `hop_`, so they are trivially
greppable and cannot be confused with the pinned candidate's marks. No existing
mark, counter, control flow or error path is changed.

internal/extractors/tsextractor/session.go
- `hop_fw_summary_collect files=N of=4086` - summed time in the arm that has this
  hop's source bytes: collectGraphQLContribution, the server/SDL merge and
  grpcFileContribution.
- `hop_fw_summary_merge files=N of=4086 no_prev_record=N` - summed time in the
  cached arm that only re-reads prev[rel].GraphQLServer/.GraphQLSDL/.GRPC and
  re-merges them. This is the collection-versus-merge split you asked for, and it
  is the number that says how much of the per-hop index rebuild is pure re-merge.
- `hop_nuxt_indexes nuxt= pkgs= known_files=` - withNuxtRuntimeAliases plus the
  per-package nuxtAutoComponentIndex loop, previously inside the same span.
- `hop_mapfiles_wall files= cached= needed_uncached= …` - the MapFiles call's wall
  time, captured on return before any logging, with per-arm file counts and summed
  goroutine nanos. `needed_uncached` is everything need() selected, minified and
  unreadable early returns included; it is deliberately not called a parse count.
  The summed nanos exceed wall by design; they are labelled and are not a duration
  of the run.
- `hop_export_scan_total scans= derived= scope=all_index_callers` - summed time
  inside the single flight in namedExportCache.index, which is the one place a
  summary scan actually happens, so it covers callers during extraction as well as
  the surface branch.
- `hop_export_surface_index` and `hop_export_surface_peek`, both labelled
  `scope=post_extraction_surface_branch_only` - the BindsNoImports branch and the
  peek-only branch. These are a subset of the scans above and are not an
  accounting of the 117 SummaryScans; most of those are expected to happen through
  index() during extraction.
- `hop_aggregate_serial files= facts= routers=` - the serial loop after MapFiles.
- `hop_fact_clone facts=` - summed cloneFactSlice time inside that loop.

internal/graphsession/persist.go
- `hop_state_create_write bytes=`, `hop_state_fsync_close bytes=`,
  `hop_state_rename_dirfsync` - the durability syscalls that currently carry no
  mark at all, split from marshal and from the write-side hash. This is what makes
  the unmeasured residual inside write_pending_state measurable instead of inferred.

Both files pass gofmt. Nothing is added to any struct, any persisted type, or any
API; every counter is a local or an atomic local, so nothing lands on the shared
*TSExtractor and no state survives the call.

## 2. Dependencies each stage consumes (for any later reuse)

Read off the source, not assumed. Reuse of any stage would have to prove every
item below unchanged, and record identity proves none of them (r.Records returns
the full map each hop).

1. Framework summary collect: per-file source bytes in `sources` for files this hop
   read. Merge arm: only prev[rel].GraphQLServer, .GraphQLSDL, .GRPC - by value.
   Produces graphqlServer.enabled, graphqlServer.sdlDocuments, grpcIdx.
2. Nuxt indexes: knownFiles, nuxtPkgs, pkgDirSet, pkgNamesEarly, aliasRoots, `sources`
   and - the awkward one - overlayReadFile through the ctx overlay and inputScope,
   so this stage can read from disk under the current capture. Also prev and dirty.
   Any cache here is capture-scoped, not session-scoped.
3. MapFiles per-file parse: need(relFile) i.e. the dirty set, sources[relFile],
   aliases, knownFiles, freshGQL/freshGRPC, statsKind, and the shared mutable
   exportCache. The exportCache is the blocker for naive cross-hop reuse: entries
   are created during the map, so a reused result implies a reused cache or a
   rebuilt one, and hooks.OnFileLocal is a side effect that must still fire.
4. Export surface: readSrc, aliases, knownFiles, plus whatever the session already
   indexed at ts.go - peek deliberately proves only what was already paid for.
5. Serial aggregate and clone: perFile only; cloneFactSlice makes the copies whose
   cost hop_fact_clone reports.
6. Durability: the marshalled bytes and the state dir.

## 2b. Concrete hypothesis under test (root msg_57065ed5dd82, msg_daea253adcc6)

Source confirms the mechanism, not only the log correlation: the minified early
return sets rec.Minified and returns BEFORE rec.NuxtScope is assigned, and need()
returns true whenever rec.NuxtScope is empty. So every prior-minified record is
selected for reread on every hop. fillRecord, which attaches the framework
contribution, runs after that same early return, so a minified record carries no
GraphQL/gRPC contribution - which is exactly why these files cannot be made
"cached" by setting NuxtScope or by treating Minified as clean: the fresh
framework indexes consumed their bytes before the skip, and a later delta would
lose those contributions.

Instrumentation added for it, measurement only:
- hop_need_classification to_read= prior_minified= prior_minified_with_framework=
  no_prior_record= all_dirty= - the reread population counted directly, plus the
  count of minified records that carry any framework metadata, which the source
  says should be zero.
- hop_fw_summary_collect_minified files= bytes= - the share of fresh framework
  collection spent on files that a prior record already marked minified.
- hop_minified_skipped files= bytes= - the skips inside the map, with the SHA256
  each one still pays before the skip.
Root's audit: 52 unique files, 11,113,023 bytes, appearing twice in the body log.
No optimization, no blanket exclusion, no Product-wide skip. Any eventual reuse
must preserve minified consumers and the 11-consumer oracle; a within-call
source-byte-keyed contribution reuse looks simpler than a persisted schema, but
that is a later decision and this stage only measures.

## 3. Measurement plan

Build: one binary in this tree, named enola-hop-instrumented, never pinned, never
an acceptance arm. Labelled in every artifact as an instrumented build.

Perturbation, not a measured overhead. The diff adds order 10^4 time.Now() calls
per run. The uninstrumented counterpart is the existing pinned d3229b4a binary -
no rebuild of unchanged source. Any wall difference between that binary and this
one is diagnostic context only: it is a single comparison across two builds, it is
not a measured causal cost of instrumentation, and I will not subtract it from
anything. Instrumented figures carry a perturbation caveat and produce no timing
claim about the pinned candidate.

Correctness first: focused tests for internal/graphsession and
internal/extractors/tsextractor on this tree, to show the marks changed nothing.
These are the narrowly necessary checks, not a suite.

Then, and only after you coordinate it: one bounded Product profile per scenario
(noop, body, structural), instrumented build, single run each, diagnostic label.
Not a series, not an acceptance arm, no median, no comparison against Stage21
numbers.

## 4. Coordination and what I am not doing

- FSM warm semantic-cache work may overlap stages 1-4 above. I have no visibility
  into its scope; before any reuse design I need to know which of these stages it
  already covers, so we do not build two caches over the same inputs.
- No production optimization, no change to the pinned candidate, no repin, no
  timing claim from an instrumented or shared run.
- Stage21 acceptance directory is shared, not mine: root's timing-window.json and
  timing-series.json are there and stay there.
