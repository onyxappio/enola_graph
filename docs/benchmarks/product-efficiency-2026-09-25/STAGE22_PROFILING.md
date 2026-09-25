# Stage22: per-hop derived-work profiling

Status: Stage22 candidate passed correctness and three-pair Product speed acceptance; integration review is pending. Stage21 retained candidate remains rejected after repeated changed-file regressions. Stage22 code comes from pinned main, not Stage21.

## Source and recorded-input finding

Root inspected `internal/extractors/tsextractor/session.go` in retained source. `need` returns true for empty `NuxtScope`; the minified early return occurs before assigning that field and before `fillRecord` stores GraphQL/gRPC contributions. Actual `work8/state1/state.json` contains52minified records, all52with empty scope and keys file/hash/minified. The body diagnostic logs104minified skips:52paths in both preview hops, totalling11,113,023source bytes per pass. `to_read=53/62` versus parsed1/10 agrees with those52extra files. Evidence: `stage21/quiet-source3/retain-candidate/cohort8/minified-reread-audit.json`.

This proves repeated selection/processing, not its latency cost. The framework-contribution phase precedes minified skipping, so bypassing those files or merely assigning NuxtScope would lose contributions currently consumed from raw bytes. No blanket exclusion is authorized. Consider only semantically complete reuse after profiling, with existing minified-consumer and cold/delta oracles.

## Authorized next step

Separate scratch instrumentation measures fresh framework contribution collection versus cached merge, prior-minified counts/bytes and cost, MapFiles work versus aggregate cloning, export summary scans, and durable write syscalls separately from input fences/marshal/hash. No cache implementation yet. GraphQL collector inputs are path and source bytes; gRPC collector input is source bytes, but aggregate ambiguity/order and all later consumers must be audited before reuse. All profiled builds must be labelled; instrumented/shared-host numbers are diagnostic only.

FSM coordinator confirmed ownership separation (msg_0518626aaebd): its cb884c5 change is internal/fsm constructor-proof and warm-import provenance; preserve it. New Product timing requires another coordinated interval.

## Instrumentation review checkpoint

Root reviewed the initial scratch diff and required five corrections before Product runs: label all uncached MapFiles paths as needed rather than parsed (minified/unreadable are included); reuse one elapsed measurement for framework total/minified subset; capture MapFiles wall before logging; distinguish post-extraction export-surface timings from all cache-miss scans; and do not treat a single control/profile wall difference as causal instrumentation overhead. Revised diff adds timing inside the actual namedExportCache single-flight parse boundary, exposes summed goroutine time, and retains separate wall/aggregate spans. Source review confirms those corrections are present; final build/tests and binary provenance are still pending.

## Build/test checkpoint

Scratch binary exists, SHA256 `c6d37a34be46d0262f96a422fb13ddbcda978a101301fd372da2b2628c81d299`; revised diff SHA256 `3d7f76bef65cfe2db7dddeb1262fcf435684ce0cc3f891c0235bbb4290a646a9`. Root observed full-package tests rather than the requested focused subset: `go test -count=1 ./internal/extractors/tsextractor/...` passed22.491s exit0; `go test -count=1 ./internal/graphsession/...` remains active (PID53253at10:32UTC). The running suite is preserved, not restarted, and no broader repeats are requested. Product profile has not started.

## Product diagnostic completed

Full TS package passed22.491s and graphsession527.263s, both exit0. Root verified13harness/binary pins and wrapper changes, then executed sequential control/instrumented arms with the unchanged scenario/validator scripts. Both arms passed all checks; seven corresponding normalized graph hashes match across arms. FourteenCLIcalls total, including cold references; silent no-op invariants preserved. No production edits. Source TS session matches mainStage20 byte-for-byte before instrumentation (`cmp` exit0), so the next candidate can start from main instead of depending on rejectedStage21.

|Diagnostic phase|Body|Structural|
|---|---|---|
|Minified framework collection, hop1|0.096s /52files|0.095s /52files|
|Minified framework collection, hop2|0.098s /52files|0.097s /52files|
|Cached framework merge, each hop|0–0.001s|0–0.001s|
|MapFiles wall, hop2|0.263s|0.260s|
|Export scan117calls, summed goroutine time|0.201s|0.206s|
|Fact cloning, each hop|0.023–0.026s|0.023–0.026s|
|Create/write + fsync/close + rename/dirfsync|0.043s|0.042s|

These instrumented single samples on a shared host attribute work, not accepted speedups; nested/summed times are not additive wall time. Each minified collection processes11,113,023bytes. Root requested a bounded main-based design preserving complete existing framework metadata and valid context for unchanged minified records, plus conservative legacy fallback and minified/normal transition oracles. No implementation accepted yet. Evidence: `stage22/product-diagnostic/`.

## Signature reuse review (10:48 UTC)

Root inspected `compositionSignatureOn`: its cached-summary branch checks only a prior record and an unchanged dirty flag, whereas extraction `need` also rejects an empty NuxtScope. Legacy minified records can therefore supply incomplete empty framework summaries to the pre-Begin signature even though extraction refreshes them. The Stage22 design must prove completeness in both paths and test cold-versus-cached composition signatures for a minified framework producer and a legacy incomplete record. This is a source-level risk, not yet a reproduced Product graph failure. Avoid assigning resolver ImportComplete merely to reuse pure framework metadata. Findings sent to the existing worker for the bounded main-based design; no production changes or accepted performance claim.

Further source review: `grpcFileContribution` is source-only and `mergeFile` constructs a fresh service/method map, so cached GRPCRecord reuse does not itself alias a mutable aggregate. Preserve iteration order and ambiguity behavior in tests. `resultFromRecord` reconstructs facts/router only; minified records must retain their empty local fact contribution. `surfaceRequiresDependentParse` uses ImportComplete and ParseKind as resolver evidence, reinforcing that framework-summary completeness must not silently certify either. Worker liveness was independently confirmed as live/working through Orca worker-list during this review.

Coverage audit: the prior 11-scenario minified consumer oracle seeds `bundle.min.ts` with a long numeric-array export, not a nonempty GraphQL/gRPC producer. Its passing results do not prove preservation of minified framework contributions. Stage22 requires actual framework producers with downstream consumers, ambiguity cases and legacy-record signature equivalence; worker notified.

## Reproduced legacy signature mismatch

On main, root ran a Go overlay test with a genuine generated gRPC producer padded into a minified source. Fixture assertions prove minified classification and nonempty gRPC contribution. Cold CompositionSignature differs from the legacy incomplete cached-record signature: expected regression FAIL, package 0.439 s, complete command 3.01 s. Evidence: `stage22/legacy-signature/`. No production files changed. This proves the signature mismatch, not a stale Product graph; the candidate must make this regression pass.

Added a positive control to the reproducer: the same record with its actual GRPC contribution and NuxtScope populated matches the cold signature (PASS), while legacy incomplete metadata still fails. Full command 2.66 s, package 0.458 s; both subtests are recorded in `result-with-control.log`. This isolates missing metadata in this fixture without claiming candidate acceptance.

## Bounded implementation authorization (10:57 UTC)

Root authorized the existing worker to implement on Stage20 after checking consistency: persist fresh GraphQL/gRPC contributions plus valid NuxtScope before minified return, leaving local facts and resolver proof fields unchanged; reject legacy incomplete minified records in cached composition signatures. Current main has a single production Minified assignment before the only NuxtScope assignment, which supports using empty NuxtScope as the legacy marker without assuming all empty framework summaries are incomplete. The worker must raise any concrete need for explicit schema migration and coordinate reserved versions. A legacy stored FrameworkSig may trigger conservative broader work on the first changed run; tests must expose rather than hide that behavior. Genuine framework consumers, transitions, ambiguity, legacy metadata and session cold/delta/noop are the focused gates. The worker terminal showed an API retry delay; it remains live and has not been replaced.

## Design reviewed and implementation unblocked (10:59 UTC)

Root read the worker draft directly while its API was retrying. Accepted the existing-field design: preserve complete pure summaries and NuxtScope on minified records, refresh legacy empty-scope minified records in the signature collector. No new schema field or version reservation is justified solely to avoid an imprecise first-upgrade fallback diagnostic; validate and disclose conservative broader work instead. Ordinary/unreadable record policy is outside this bounded change.

Corrections required before relying on the worker draft: GraphQLParsed=0 does not exclude SDL contributions; profile hops are extractor passes within one delta invocation, not separate CLI processes; gRPC ambiguity must arise from conflicting merged contributions because mergeFile does not consume AmbiguousClass; reuse proof must include dirty-file planning plus the existing pre-End fence. The preserved worker draft is not an accepted factual report. Implementation GO was sent as msg_f4041a248ef3. No code or speedup accepted yet.

## Planning byte-proof review during accuracy quiet window

Root traced current main independently: `readRuntimeInputs` builds inventory then `filesToHash` and `eng.FileHashes`; `filesToHash` includes prior semantic owners and active extractor ContentInput paths, with no Minified exception. Both frozen planning (session.go around1034) and extraction dirty selection (around1209) compare FileState.Hash with current input hash before reuse. Resident content events read regular source bytes and SHA256 them in `contentInputsStopping`, updating the input hash on change; unknown/config/deleted/symlink/unreadable paths fall back rather than silently reuse. No Minified exception exists there either. Thus the proposed summary reuse preserves existing dirty-selection mechanisms as well as the pre-End revalidation fence; focused mutation/race tests remain required.

Accuracy quiet GO sent11:04:34UTC (msg_cbc26b865735) after worker explicit hold ACK msg_2a692dd908f3. Root and Stage22 run no heavy jobs through11:20UTC unless released earlier; only light source edits/review. This is a coordination window, not evidence that timing has started or succeeded.

First candidate source review: `/tmp/enola-stage22-candidate/src/internal/extractors/tsextractor/session.go` now writes pure framework metadata and NuxtScope in the minified branch and rejects legacy incomplete minified records in signature reuse. The diff preserves ordinary/unreadable handling and does not change schema version. Root found no behavioral concern in this initial diff; it is still a mutable, untested candidate. Accuracy reported timing START at11:05:07 UTC; no Stage22 tests/builds run during the hold.

Focused test draft reviewed during hold: legacy signature and read-count tests are present. Root requested stronger GraphQL fixture assertions (both server and SDL), serialized-record reuse rather than memory-only reuse, genuine downstream consumer facts with cold equality, and an actual config change rather than only corrupting a stored NuxtScope. Existing draft is not accepted coverage until these gaps and session/frozen/noop tests are addressed. No tests executed in the quiet window.

Accuracy released the quiet window via msg_4d9859e28432 (11:15:08UTC), reporting all nine arms completed; metrics acceptance belongs to that coordinator and remains under audit. Root relayed RELEASE to Stage22. Root Stage22 summary-validator test initially failed because copied script referenced absent historical fixture data; replaced it with explicitly labelled historical baseline data under summary-test-fixture (not Stage22 evidence). Synthetic summary test then passed in0.05s: valid cohort accepted; partial/out-of-window/failed-arm/hold-mismatch/cross-arm graph drift rejected. Added rejection of malformed/empty binary pins. No candidate performance run executed.

Root candidate check after RELEASE: independent legacy signature reproducer and complete-metadata control both PASS on candidate, package0.374s, command2.12s. Candidate session.go SHA256 f06556a0e96ca71dc07e318f119130f0e80f5ff031b14dccadead2be9fb57797. First overlay attempt used the /tmp alias and discovered zero tests; it is explicitly discarded. Canonical /private/tmp overlay logs confirm both named subtests executed. This proves the narrow regression fixed, not full candidate correctness or performance.

Root preliminary extractor tests: first run passed5 then panicked in ambiguity fixture because its helper intentionally returns nil when all names collide (wall0.847s, package0.383s). Remaining6 tests passed separately (wall0.845s, package0.400s). Total11pass/1fixturepanic; no claim of final-source coverage. Worker notified of the fixture fix and outstanding persistence/consumer/session assertions. Evidence `stage22/focused-draft/`. Worker message accidentally quoted remainder package0.365s; authoritative log is0.400s.

Two existing graphsession regressions passed on current candidate: TestFrozenBeginPrecedesParsingAndNeverGrows (0.60s; frozen manifest plus rename/cold consumer equality) and TestReviewMinifiedNoop (0.18s; zero parses/events/generation advance). Package1.344s, complete command5.48s; verbose log has no skips. These do not replace the new genuine-framework consumer/persistence/fence tests still being written.

Independent persisted gRPC consumer oracle PASS: genuine minified generated client, expected CreateUser route asserted in cold output, JSON record roundtrip, only app.ts marked dirty with planner-owned invalidation, one file read and exact canonical full-fact equality to cold. Package0.494s, command2.20s. Test is root-owned overlay, no worker source edits; archived as text with result under focused-draft. GraphQL and session migration/fence checks still outstanding.

Independent persisted GraphQL consumer oracle PASS: minified server imports schema.graphql and calls buildSchema; asserts true GraphQLServer plus exact imported GraphQLSDL path, actual Query.real route in schema output, JSON records roundtrip, one consumer-only read and full canonical fact equality. Package0.399s, command2.08s. GraphQLSDL denotes imported paths, so a pure inline template is not sufficient evidence for that field; worker notified with reusable test.

### 11:36 UTC — independent harness dependency audit

Revalidated all 12 pinned harness/configuration/tool files plus the Stage20 baseline binary: every SHA-256 matches. Product checkout remains clean at `a609c19f3861971930fae7b33dcb2950598953c5`. Audit elapsed 0.487 s; evidence: `stage22/harness-preparation/dependency-audit.json`. This is a dependency check, not a Product performance measurement. Candidate remains unbuilt and acceptance execution remains disabled.

Worker adopted imported GraphQL document fixtures and dirty-consumer cache tests; review caught a leftover old function fragment from a raw-string-sensitive replacement. Worker acknowledged and is rewriting its test file; no final test pass is claimed for this intermediate file. Accuracy quiet hold was explicitly released at 11:15 UTC; a fresh window is required for future Stage22 timing.

### 11:38 UTC — corrected extractor tests independently verified

Pinned source and test snapshots in `stage22/focused-final/`: all 13 new extractor regressions pass with the candidate production overlay (package 0.524 s, wall 2.749 s). Main negative control fails all four selected guards as expected (wall 2.224 s): missing minified metadata, legacy signature mismatch and missing producer metadata in the two consumer fixtures. The latter fail at prerequisite assertions; they do not establish that old main publishes an incorrect consumer graph. Candidate consumer tests force consumer reanalysis with persisted producer metadata, assert one file read and compare all fact fields.

Graph-session legacy upgrade/configuration/frozen-mutation coverage and Product correctness/performance remain pending. No speed claim or publication.

### 11:40 UTC — experimental Product correctness passed

Pinned experimental binary `a3c19dc902f80248cc14d0e7669bf8c0067d779bd6709741ab756ac7146986d1`, built in 4.851 s from main plus the reviewed session.go overlay. Seven CLI runs exited successfully; initial, body delta and structural delta each match their fresh cold graph, and no-op preserves state/generation with zero wire messages and zero parses. Actual files_read: initial 4086, no-op 0, body 11, structural 12; files_parsed: 4034, 0, 11, 12 respectively. This confirms the intended removal of repeated minified reads for these Product scenarios.

Evidence in `stage22/product-correctness/`. This is correctness-only with competing worker/FSM tests observed: timing_eligible=false, no latency acceptance claim. Full session legacy upgrade/configuration/mutation tests and paired quiet speed acceptance remain required before integration.

### 11:42 UTC — full TypeScript suite passed; session review gaps

Full `internal/extractors/tsextractor` package passed on the pinned overlay, package 20.760 s / wall 21.257 s, exit 0 (`stage22/focused-final/full-ts.*`). Session-test source review found the new fixtures did not set `AuthoritativeFiles`, so their frozen-plan claim was not established; worker instructed to enable the actual protocol and prove Begin preceded mutation, End was absent and generation unchanged. The manually downgraded legacy record retained a new-version signature; authentic old-writer signature provenance is also required before claiming migration coverage. These are test evidence gaps, not observed production regressions.

### 11:43 UTC — real old-binary checkpoint migration

A separate-process frozen CLI fixture with a minified GraphQL server imported schema.graphql. Stage20 baseline produced initial then changed state; candidate reopened that exact state. Candidate no-op retained generation 2 with zero parses, then first changed run reported the explicit TypeScript composition fallback and global frozen owner domain, parsing 2 files; subsequent no-op retained generation 3 with zero parses. This confirms the expected one-time broader changed run with authentic old-writer state. Consumer/cold equality for this migration fixture is not yet asserted. Evidence in `stage22/legacy-cli/`, live fixture and state snapshots `/tmp/enola-stage22-legacy-cli`. The first attempted delta used a different file-sink path and was correctly rejected; the corrected sequence used the original sink path throughout.

### 11:46 UTC — actual legacy CLI replay equals cold

Reference Consumer.Apply accepted every event from the real old-initial + old-delta + new-delta sequence, completed generation 3 and exactly matched Canonical() from a fresh candidate generation 1 of the target tree. An explicit route assertion verified Query.real remains present. Replay test PASS, package 0.563 s / wall 2.173 s; source, raw event streams and receipts archived in `stage22/legacy-cli/`. Initial replay assertion incorrectly used FactNames(), which returns symbols only; corrected to inspect route nodes in Owners, preserving the first failed fixture log. No production failure or graph difference observed.

### 11:52 UTC — authoritative session regressions independently passed

Reviewed and copied the corrected four-test suite: every Options enables AuthoritativeFiles; the mutation hook asserts exactly one Begin and zero End before modifying the minified producer, then requires ErrInputsChanged, no End and unchanged generation. All four pass independently on the pinned production overlay, package 3.795 s / wall 5.780 s. Nuxt layout change and delta graphs equal cold; legacy test uses old-behavior signature derivation backed by actual old-binary evidence and unconditionally asserts the composition fallback reason. Test source SHA c83a6635fbbc8fe961f874867c53c530fd43881427d37d511385df973b5b8b43. Evidence: `stage22/focused-final/final-session.*`.

Full repository suite remains live (session97804). Quiet timing proposal12:00–12:15UTC has plugin ACK msg_e1317b79a8a9 and Stage22 worker ACK msg_68f62c6b20db; accuracy primary ACK is partial pending its worker. No timing authorization yet.

### 11:54 UTC — full repository suite passed

Pinned-production `go test -overlay ... -count=1 ./...` completed exit0, 110 packages with tests passed, wall453.218s. This is suite duration, not Product analysis latency. The four final session tests were separately verified against the identical production snapshot. Candidate correctness is accepted for paired timing; performance and publication remain pending. Logs/receipt archived in stage22/focused-final.

## 12:06 UTC — repeated quiet Product acceptance

The 12:00–12:15 UTC window was explicitly acknowledged by the accuracy/FSM stream, Codata standing GO, plugin coordinator and Stage22 worker. Six alternating Stage20/Stage22 NATS arms completed by 12:06:35 UTC. All six exited0, passed four graph/no-op checks per arm, had complete host-load samples and zero recognized competing workloads; normalized graphs match across every arm and repeat. Each arm used the same pinned clean Product revision `a609c19f3861971930fae7b33dcb2950598953c5` and scope. Candidate binary SHA256 `a3c19dc902f80248cc14d0e7669bf8c0067d779bd6709741ab756ac7146986d1`; baseline SHA256 `99162b12e148c995c0a6e91ff73ffea05f2fb978ec004887f89e35a337006f60`. Full receipts, host-load logs, timing scripts and hashes: `stage22/quiet-product3/`.

| Fresh CLI scenario | Stage20 median (range), s | Stage22 median (range), s | Change | Files read Stage20→Stage22 | Parses |
|---|---:|---:|---:|---:|---:|
| Initial to all broker acknowledgments | 8.448 (8.408–8.654) | 8.332 (8.314–8.353) | −1.37% | 4086→4086 | 4034 |
| No-op | 1.665 (1.657–1.666) | 1.665 (1.652–1.719) | +0.01% | 0→0 | 0 |
| Body delta | 3.373 (3.362–3.395) | 3.192 (3.183–3.195) | −5.36% | 115→11 | 11 |
| Structural delta | 3.392 (3.366–3.449) | 3.211 (3.204–3.216) | −5.35% | 116→12 | 12 |

All three paired body and structural comparisons improved. Delta/initial median ratio moved from ~0.40 to ~0.38, still far from the intended much faster delta. No-op remains ~1.7s, far from near-zero. Time to first batch, broker End, and consumer End are separately present in the JSON comparison; no consumer processing time is being mislabelled as producer completion. Three pairs describe this host and scope; they are not a significance test.

Median measured RSS (MB, decimal): initial 891.5→946.7, no-op 289.9→290.1, body 644.9→627.0, structural 567.7→594.7. Initial RSS increased in all three pairs; this is a material limitation to investigate during integration, although no memory ceiling is defined for this change. No watch performance claim follows from these CLI arms.

The Codata note `2984-enola-fanout-note.md` describes a distinct, large deletion fanout on Enola 6090161 and Product 6042744: 2407 owners and 2405 identical by its digest script, 40.6MB of events. Stage22 does not address that case. Under the current authoritative v2 contract the complete owner set is frozen before parsing; eliminating an owner after Begin is not a compatible local optimization. Their proposed two-phase pre-parse or an unchanged-owner marker needs a separate design that preserves early initial streaming, durable replay and exact graph equivalence. Their digest omits some node/edge fields, so the 2405 count is a strong lead rather than an independently verified exact semantic equality claim.

## Product watch regression, 18:06 UTC

Production `graph watch` on an isolated pinned Product copy completed the existing scripted smoke with default5s collection window, final cold graph equality and idle/duplicate-save checks, exit0 / total harness90.483s. Evidence: `stage22/product-watch/`. It observed initial plus one coalesced delta, not a sustained long-running soak. This shared-host correctness run is not paired watch performance acceptance.

Integration uses the exact tested production source SHA f06556a0 and exact test snapshots. The change only populates existing graph-session record fields and lazily refreshes recognizable legacy records; it changes neither cold extracted facts nor engine cache payload/schema, so no engine cacheVersion bump is introduced. Legacy no-op retains its checkpoint; first changed run can broaden once as recorded above.
