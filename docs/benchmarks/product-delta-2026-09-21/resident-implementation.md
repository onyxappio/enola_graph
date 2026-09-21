# Resident session implementation report

Completed the first resident execution/event-source stage in the shared checkout. No commits, pushes, Telegram, Product source changes, frozen-source changes, or performance measurements were made. The coordinator owns Product timing and Git-history acceptance. Existing unrelated work was preserved, including the separately owned manifest no-lock mode; that new policy is not wired into graph execution by this task.

## Delivered

- `OpenSession` holds identity binding, writer lock, delivery journal, replay/recovery and loaded committed state for a lifetime. Strict one-shot `Run` is a wrapper that still reconciles and retains full `Result.Facts`.
- `Resident.ApplyChanges` serializes fresh generation transaction objects. Committed state/input contributions, detection, inventory, config capture, hash map and resolution index are reused immutably. Failed transactions do not promote runtime caches; the next apply replays/recovers and reconciles. The prior resolution index is only reused when its generation matches recovered state.
- Covered empty contiguous batches return before disk/input work, fact assembly, publisher setup or checkpoint writing. Summary results avoid whole-graph response copies. `Snapshot()` explicitly deep-copies the graph after successful bootstrap/apply.
- Bounded `ChangeSource`/`ChangeBatch` boundary, deterministic `ChangeQueue`, and fsnotify `FileChangeSource`. Path-count/path-byte overflow collapses to a sticky reconciliation reason. Events arriving during parsing or broker acknowledgment remain for the next captured watermark. Events consumed by a failed transaction remain logically dirty through the resident's mandatory reconciliation state.
- Real `graph watch` holds one resident writer, registers the event reader/recursive watches before baseline analysis, and debounces events rather than polling the old Run path. New directories are registered before their reconciliation signal. Captured-input mutation retries reconciliation. Native coverage failure and other analysis/broker errors are explicit; restart replays/reconciles.
- Narrow existing TS/JS content batches hash dirty source bytes, provide those exact bytes to extraction, retain existing whole-file invalidation/composition fallback, and verify captured inputs before successful EndReplace. Unchanged bytes do not parse, publish or advance a generation. No size/mtime equality shortcut was introduced.
- A full-default-profile test exposed a pre-existing direct-resolution gap: adding TS `file_ref` facts left a cached Markdown link unresolved. The committed candidate index is reused, and owners whose reference resolution changes are refreshed without re-extracting their local facts. This applies to strict one-shot delta too. No transitive attribute propagation was introduced.
- Effective Enola config candidates are separately captured. `Options.ReloadEngine` reconstructs configuration/registrations; absent callback fails closed on changed effective config. CLI watch supplies it and rejects a changed repository selection. Reload uses full reanalysis and revalidates captured configuration before commit.
- Added precise work counters, updated CLI/watch documentation, and durable repo documentation in `docs/RESIDENT_SESSIONS.md`.

## Supported contract and exclusions

The online guarantee is completeness through the **observed event watermark and captured input versions**. Empty userspace queues are not strict instantaneous disk equality. Strict callers request reconciliation; new processes always reconcile. No durable OS cursor, backend flush/barrier, quiet-period equality proof, IPC, or local daemon attachment was added.

Bootstrap registers two distinct facts: existing-TS-content independence and repository event coverage. Neither a name nor a content-input predicate silently grants full coverage. All registered builtin detector discovery is audited as repository-tree bounded. Native active extraction coverage is explicit for TS, manifests, markdown, HCL, Python and Swift—the six active profiles reported for Product. Other active profiles and custom/opaque input sources fail native watch coverage closed; caller-supplied genuinely complete batches can still use ApplyChanges with conservative reconciliation where needed.

TS coverage includes the entire repository tree and discovered TS/package/framework/extended configuration paths. Manifests cover hidden manifests and ancestor lock candidates within the repository; Markdown uses local source and inventory links; HCL reads owned files; Python uses sources and root framework/dependency manifests. Swift exposes the same root XcodeGen include/missing/case-folded paths used by its existing DeltaContext plus shallow repository iOS markers. Active Swift recaptures that existing bounded context on each nonempty TS batch, reports `BoundedContextChecks`, and reconciles if it differs. Active OpenAPI/AsyncAPI arbitrary reference discovery remains unsupported for native coverage.

All repository directories are watched rather than copying the engine's source ignore policy. Only explicit non-input output artifacts are ignored (state directory and CLI event sink). Existing/new symlink uncertainty, failed registration, closed channels and kernel errors/overflow invalidate native coverage persistently and fail watch closed; draining cannot clear the failure. Path-queue overflow can safely reconcile without rebuilding because directory registration is maintained independently of dirty-path storage. A new process rebuilds watches and reconciles after a coverage failure.

Native coverage is supported only on local Darwin filesystems and an explicit Linux local-filesystem whitelist. No claim is made for unsupported/network filesystem delivery or unobserved mount/topology operations. Darwin fsnotify uses kqueue and internally opens file watches: descriptor use and bootstrap cost scale with the watched repository and external parent directories. Product-scale descriptor/resource feasibility must still be checked by the coordinator. External parent notifications are filtered to exact config/include targets and relevant ancestor changes, avoiding log/checkpoint sibling feedback while preserving atomic target replacement.

## Exact benchmark/production setup

The public API is stable for the coordinator's private benchmark driver:

1. Create `NewFileChangeSource(repo, ignoredOutputPaths, limit)` and call `Start(ctx)` **before baseline analysis**. `OpenSession(ctx, eng, repo, sink, opts)` must use the same checkout and appropriate frozen config/registrations.
2. Capture `batch := source.Drain()`, set `batch.Reconcile = "bootstrap"`, and call `resident.ApplyChanges(ctx, batch)`.
3. Call **`source.CoverSessionInputs(resident)`** after each successful apply. This is the exact production Watch gate. It validates supported session coverage and adds external/missing config/include parent watches. If registrations change, it queues a reconciliation reason covering the registration gap.
4. Drain/apply pending registration-gap/event batches and repeat the gate before warm measurements; do not throw away events arriving during bootstrap. The gate caches unchanged coverage across covered idle and content-only batches, so it does no hidden disk work on warm idle.
5. `ApplyChanges` accepts `ChangeBatch{Epoch, From, Through, Covered, Paths, Reconcile}`; `From` must equal the last accepted watermark. `OnlineResult` embeds summary `Result`, plus `Work`, `Epoch`, `Watermark`, `Reconciled`, `FallbackReason`. Consume actual filesystem batches for real watcher measurements. Explicit cooperating-writer batches are a separate workload, not evidence of native watcher coverage.
6. `Snapshot()` is optional and copies the graph; account for it separately from summary-only apply. Close the source and resident when done. The lifetime writer lock prevents concurrent fork/state writers; there is no live-session fork/IPC shortcut.

`WatchEvery` now means debounce ceiling (25 ms default), not periodic polling. For raw FileChangeSource usage, `Drain.Covered` describes source-tree observation; use `CoverSessionInputs` to establish the supported session dependency scope.

## Counters and remaining costs

`Work` reports `InventoryScans`, `DetectionScans`, `ContextScans`, `ConfigScans`, `HashedFiles`, `DirtyHashBytes`, `BoundedContextChecks`, `VerifiedFiles`, `CapturedReads`, `FactAssemblies`, `PublishedEvents`, `Checkpoints`, and `CheckpointBytes`. All are zero on covered empty idle. `HashedFiles` counts the explicit session input hash targets; `DirtyHashBytes` only measures narrow dirty-source capture, not unknown total bytes consumed by full hashing/context routines. Extractor reads/parses and TS composition remain in `Result.Stats`/profiling. These categories should not be mislabeled as a total filesystem-read counter.

This stage still copies whole hash/state maps, aggregates and composes much of the TS graph, performs extractor-internal framework/package setup, groups owners, and compares whole-graph direct resolution subscriptions. It rewrites/fsyncs the full pending analysis checkpoint for changed generations. Captured config verification and bounded Swift context checks remain. State patches, incremental composition/index updates, broader dependency manifests and local IPC remain subsequent bounded tasks. No subsecond delta or near-zero cold CLI claim is made. No contended execution was presented as performance acceptance.

## Validation

Passed affected tests:

```
/tmp/enola-toolchain/go/bin/go test ./internal/graphsession ./internal/graphstream ./internal/extractors/tsextractor ./internal/extractors/swiftextractor ./internal/engine ./pkg/bootstrap ./pkg/command
```

Log: `/tmp/enola-resident-affected.log`.

Passed broad race tests:

```
/tmp/enola-toolchain/go/bin/go test -race ./internal/graphsession ./internal/graphstream ./internal/extractors/swiftextractor ./internal/engine ./pkg/bootstrap
```

Log: `/tmp/enola-resident-race.log`.

Passed final focused race after coverage filtering, cached public coverage gate, config reload, and watch retry changes:

```
/tmp/enola-toolchain/go/bin/go test -race ./internal/graphsession ./pkg/bootstrap -run 'TestResident|TestFileChangeSource|TestChangeQueue|TestWatch|TestDefaultBuiltinResident|TestExternalCoverage' -count=1
```

Log: `/tmp/enola-resident-final-race.log`.

Passed `go vet` for graphsession, engine, Swift, bootstrap and command packages; CLI build at `/tmp/enola-resident-worker-bin`; `git diff --check`. Logs: `/tmp/enola-resident-vet.log`, `/tmp/enola-resident-build.log`. No full `go test ./...` or Product benchmark was run by this worker.

New tests exercise repeated zero-work idle and checkpoint stability; snapshot/cache immutability; restored-mtime content edits; body/structural/revert/add/delete/config cold equivalence; queue overflow and epoch/restart gaps; late same/other-file events at End acknowledgment; failed parse and broker recovery; effective config reload; unknown inactive custom detector fallback; active Swift includes; real in-place write/atomic save/new directory notifications; external extended configuration; external sibling output suppression; native resident watch; captured-input retry; and six-active-builtin cold equivalence including cross-extractor resolved/unresolved edges.

The initial full-profile equivalence failure was fixed rather than weakening the comparison. The first new Swift test exposed a missing merge of the bounded-context counter; it was fixed. All final listed checks passed. Real OS notification assertions were exercised on this Darwin development host; Linux/other-host behavior and Product descriptor limits are not claimed by that result.

## Files owned by this task

`go.mod`, `go.sum`, `internal/engine/engine.go`, `internal/extractors/swiftextractor/delta.go`, `internal/graphsession/session.go`, `internal/graphsession/owner.go`, `internal/graphsession/resident.go`, `internal/graphsession/changes.go`, `internal/graphsession/watch_local_darwin.go`, `internal/graphsession/watch_local_linux.go`, `internal/graphsession/watch_local_other.go`, `internal/graphsession/resident_test.go`, `pkg/bootstrap/bootstrap.go`, `pkg/bootstrap/resident_test.go`, `pkg/command/graph.go`, `docs/CLI.md`, `docs/GRAPH_VALIDATION.md`, `docs/RESIDENT_SESSIONS.md`.

The requested lockfile-ignore graph policy remains the coordinator's next integration task. This pass preserves existing lock behavior and does not claim the new profile is active.

Latest coordinator steering also reserves nested real `.gitignore`/tracked-exemption and graph input-policy integration for a separate task; those policies were not introduced by this resident pass.
