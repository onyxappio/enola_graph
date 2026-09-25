# Stage19 body-delta allocation diagnostic

Stage19 remains unmerged. The earlier three-pair timing assessment showed no consistent latency gain and higher body-delta peak RSS in all three pairs. One subsequent instrumented comparison did not reproduce that RSS difference; it does not disprove the earlier observation. No performance acceptance follows from this diagnostic.

## Setup and validity

Baseline `5e145dd` and candidate `7b03158` received an identical optional `runtime.ReadMemStats` overlay at existing trace boundaries, plus trace start. Both were built with Go 1.27.1 and `-trimpath`; root verified revisions, changed-file lists, overlay bytes and binary hashes. Runs enable `ENOLA_GRAPH_PROFILE`, `ENOLA_GRAPH_MEMSTATS` and `GODEBUG=gctrace=1`, without forced GC. Sampling perturbs execution. This was a concurrent-work-eligible diagnostic, not a reserved timing window.

Product input was `a609c19f3861971930fae7b33dcb2950598953c5`, with the existing Product graph scope. Both body edits change `packages/crypto/src/password.ts` identically. Each arm uses a fresh NATS server and observer, and an initial seed from the same uninstrumented baseline binary. The observer uses DeliverNew, so restoring a broker store alone would not restore its graph.

The first attempt stopped before candidate delta: independently produced seed states differed beyond LastRunID and fact ordering. Nine fact lists were exact full-JSON multiset permutations. One Vue record, `apps/behavior-visualizer/src/components/InspectorDrawer.vue`, had `export_surface=[default:InspectorDrawer]`, `export_surface_recorded=true`, and `export_surface_context_free=true` in one seed, with all three absent in the other. Graph hashes were equal. These cache fields were not normalized away.

The source explains a scheduling-sensitive cache observation: `tsextractor/session.go` records importing-file export surfaces only when `exportCache.peek` already has a context-free index. Vue extraction returns before the ordinary TS AST index-adoption path, so a concurrent consumer can populate that index before or after the file's own peek. Missing proof is treated conservatively and can affect subsequent invalidation work. This baseline behavior is not introduced by Stage19, and its contribution to the earlier RSS difference is unmeasured.

The second attempt preserves the independent seed artifacts but restores the **complete original baseline state directory** before candidate delta. All nine files are verified byte-identical, without editing fields. The snapshot has no unacknowledged payload backlog. Same physical checkout, state path, configuration, context, repository ID and NATS URL are used across arms. Each fresh observer independently receives an initial graph verified equal at generation 1; ordinary delta Begin validates predecessor generation, not predecessor RunID. This is a controlled diagnostic setup, not a claim that arbitrary broker/state restoration is safe.

Both deltas parsed 11 files and matched fresh baseline cold analyses of the edited repository, as well as each other. Initial and cold runs parsed 4,034 files. The harness exited successfully and stopped its NATS/observer children.

## Observations

| Metric | Baseline | Stage19 |
| --- | ---: | ---: |
| Cumulative TotalAlloc at CLI completion, bytes | 2,071,670,784 | 2,060,483,328 |
| HeapAlloc at final sample, bytes | 382,528,648 | 382,534,328 |
| GC cycles | 23 | 23 |
| Instrumented process max RSS, bytes | 679,510,016 | 678,330,368 |

Cumulative allocation is neither live memory nor peak RSS. Point samples do not locate a continuous peak. These process-global intervals include any concurrent work and instrumentation overhead. Similar allocation totals and GC counts do not prove identical object lifetimes, residency, fragmentation or scavenger behavior. The instrumented pair cannot be compared directly with the uninstrumented timing series.

The first attempt's successful baseline delta identifies substantial allocation intervals: approximately 423 MB through pending-state writing, 308 MB through index/owner grouping, 191 MB through assembling new file state, and 186 MB through state loading. These are phase-boundary differences, not allocation-stack attribution or proof of time spent in each function. They justify investigating redundant whole-graph state/index copies before pursuing further marginal summary-only optimization.

## Decision and evidence

Park Stage19 production integration; retain the unresolved RSS concern and make no speedup claim. Next implementation investigation targets redundant resolution-fact copies and indexes, preserving resolution semantics, frozen scope, cold/delta equality and recovery. The baseline opportunistic cache behavior remains recorded rather than silently excluded from future benchmark controls.

[Evidence and hashes](stage19/rss/sha256.json), [source/binary verification](stage19/rss/overlay-verification.json), [state equality](stage19/rss/run2-state-equality.json), [run receipt](stage19/rss/run2-receipt.json), [memory comparison](stage19/rss/run2-memory-comparison.json), and [first seed differences](stage19/rss/run1-pre-state-diff.json). Harness scripts retain absolute original scratch paths and require their pinned local artifacts; they are not portable benchmark commands. Large state snapshots remain local and are not committed.
