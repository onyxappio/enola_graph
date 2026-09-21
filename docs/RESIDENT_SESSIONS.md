# Resident graph sessions

This is the first resident execution stage, not completion of the Product edit-latency target. `Run` remains strict fresh-process reconciliation and returns full `Result.Facts`. `OpenSession` holds the checkout/context/sink identity, single-writer lock, replay/recovery journal, committed state and runtime input cache. Each generation uses a fresh transaction object. `ApplyChanges` returns `OnlineResult` with summary statistics; `Snapshot()` explicitly copies the graph after a successful bootstrap/apply.

An empty **covered** batch with matching epoch and continuous watermark returns before scanning, hashing, detection, context discovery, fact assembly, publisher setup or checkpoint writing. It does not advance the generation. This guarantee is complete through the supplied observed event watermark and captured input versions. An unobserved empty userspace queue does not establish instantaneous disk equality. Set `ChangeBatch.Reconcile` for strict reconciliation. Restart always reconciles; the in-memory watcher sequence is not a durable filesystem journal.

## API and change sources

`OpenSession(ctx, engine, repo, sink, options)` returns a `*Resident`; close it to release the writer lock. Do not mutate the engine/configuration after opening. `ApplyChanges(ctx, ChangeBatch)` serializes transactions. The batch contains `Epoch`, `From`, `Through`, `Covered`, `Paths`, and a sticky `Reconcile` reason. `From` must equal the last accepted watermark. A failed transaction forces replay/recovery and reconciliation before any idle shortcut. Late events remain in the source queue for the next transaction.

`NewChangeQueue(epoch, limit)` is a bounded deterministic source for cooperating writers and tests. `Start`, `Add`, `Lost`, `Drain`, `Ready` and `Close` implement event observation without disk polling. Its caller supplies the coverage guarantee. Overflow discards individual paths but preserves a reconciliation reason; paths and total path bytes are bounded.

`NewFileChangeSource(root, ignoredOutputs, limit)` supplies local filesystem notifications. Start it **before baseline analysis**. After bootstrap and every successful apply, call `source.CoverSessionInputs(resident)` before relying on native session coverage. The method validates the supported profile and registers external/missing config/include dependencies. New registrations queue reconciliation of the registration gap: drain/apply these batches before measuring warm idle. The coverage gate is cached while the committed input coverage remains unchanged, so calling it after covered idle does no disk work. External-parent watches filter unrelated sibling logs/checkpoints while retaining target replacement and ancestor coverage events. Do not discard baseline events. Production `graph watch` uses this same public gate and retains the session for its lifetime.

The default watch collection window is **5 seconds** after the first pending event, configurable with `graph watch --watch-every 10s` (any positive Go duration). Later events join the batch without resetting its timer. Baseline analysis starts immediately; edits during an active analysis accumulate for the next sequential batch and its collection window. This is not a disk barrier. There is no polling-triggered analysis. Directory creation/move-in is recursively registered before publishing its reconciliation signal. Unknown paths, names, deletions, config changes, continuity gaps and path-queue overflow reconcile. Kernel errors, closed event channels, uncertain symlink coverage and failed registrations fail native watch closed; reopen to rebuild coverage and reconcile. They cannot become covered merely by draining an error batch. Captured-input changes during a transaction enqueue a reconciliation retry; other analysis/broker errors remain visible and recover on retry/reopen.

## Audited paths and conservative limits

Existing regular TypeScript/JavaScript session source content edits reuse committed inventory, detected extractors, content/context hashes and configuration capture. Dirty source bytes are always read and SHA-256 hashed, including same-size/restored-mtime writes; those captured bytes are supplied to extraction and verified before successful EndReplace. Existing TS invalidation, composition-signature fallback, resolution closure, local facts and initial streaming are retained. A context-changing TS edit may still re-extract a broader scope. Direct candidate changes refresh referencing owners across extractors without re-extracting their unchanged local facts.

Bootstrap explicitly registers **separate** existing-TS-content independence and repository-input-coverage contracts. An extractor name is never authorization. Inactive builtin detectors are audited as repository-tree bounded. Native active extraction coverage is currently supported for:

- TypeScript: repository tree plus discovered TS/package/framework/extended configuration inputs.
- Manifests: repository discovery, hidden manifests and ancestor-lock candidates inside the root.
- Markdown: markdown source bytes and inventory-based link resolution.
- HCL: owned HCL source bytes.
- Python: owned Python sources and root framework/dependency manifests.
- Swift: repository sources/shallow iOS directory markers plus exact root XcodeGen and enabled include candidates, including missing/case-folded/external paths.

Active Swift performs one bounded recapture of its existing `DeltaContext` per nonempty content batch; a difference reconciles. Idle performs none. Active OpenAPI/AsyncAPI arbitrary external references and other unaudited active profiles fail native coverage closed; unknown/custom extractors also fail that gate. A caller-supplied complete change source may use `ApplyChanges`; unaudited content independence selects reconciliation.

Native event coverage is currently limited to local Darwin filesystems and explicitly recognized Linux local filesystem types. Other platforms/filesystems fail explicitly. Darwin fsnotify uses kqueue and internally watches files, so descriptors and registration cost scale with files; resource-limit failures are errors, not successful coverage. Real in-place writes, atomic saves, new directories, external extended config and resident watch are tested on the development host. Product-scale watcher resource feasibility still requires the coordinator's real harness. No backend flush/barrier, periodic audit service, IPC or cross-process attach is implemented.

Effective Enola configuration candidates are captured separately. `Options.ReloadEngine` reconstructs config and registrations when their bytes change; without it the resident fails closed. CLI watch supplies this callback and rejects a changed repository selection. The new engine is used for full reanalysis, and config capture is revalidated before commit. Context/sink/checkpoint identities remain bound for the lifetime.

## Costs and counters

`OnlineResult.Work` exposes session inventory/detection/context/config scans, direct input `HashedFiles`, narrow `DirtyHashBytes`, `BoundedContextChecks`, source-hash verification, captured-byte verification reads, fact assemblies, publication events and checkpoint count/bytes. Extractor internal reads/parses/composition remain in `Result.Stats` and profiler traces. `DirtyHashBytes` is deliberately not a claim to total bytes hashed by full reconciliation or extractor context code. Covered idle has zero work counters, zero parses/events and unchanged generation.

Small deltas still copy input hash/state maps, rebuild/compose much of the TS graph, discover TS framework/package context inside extraction, compare direct resolution subscriptions across the graph, group owners and rewrite/fsync the full pending checkpoint. The committed resolution index is reused, but affected-index maintenance and state-patch persistence are not implemented. The durable bounded publication journal, broker acknowledgment barrier, pending-state promotion and fork semantics remain in force. No performance acceptance is claimed from fixture or contended test execution.

## Graph input policy profile

`enola graph analyze|delta|watch` now uses a distinct immutable input profile.
`bootstrap.NewGraphEngine(bootstrap.GraphOptions{Repo: root, ConfigPath: override,
StateDirs: outputs})` exposes the same factory to resident callers. An empty
`ConfigPath` discovers `mcp-arch.yaml` in `Repo`, or uses built-in defaults when
absent; an explicit path must load successfully. The graph CLI also accepts
`--config path` with a repository argument, or a positional config selecting one
repository. Legacy `NewEngine`, `NewEngineFromConfig`, and extractor `New()`
constructors retain their earlier behavior, including manifest lock resolution.
`--summary-json` emits the result array with `Facts: null`; existing `--json`
retains full facts and takes precedence if both are supplied.

The graph profile excludes exact ecosystem lockfile names, VCS storage, configured
state/output paths, cache defaults, effective `ignore`, additive
`graph_inputs.exclude`, and nested Git ignores before graph inventory, independent
extractor walks and side reads. A tracked Git path overrides Git ignore rules;
explicit Enola exclusions still win. `graph_inputs.cache_exclusions: []` replaces
cache defaults with an empty list. These configuration patterns use Enola glob
syntax; `.gitignore` uses Git syntax. Global Git ignores and `.git/info/exclude`
are deliberately outside this profile. The policy builder inventories names and
runs Git batch evaluation, including an additional name walk of Git-ignored trees;
it does not read ordinary source or media bytes. This remains reconciliation work.

Supported active consumers are concrete TypeScript, manifests without lockfile
resolution, Markdown, HCL, Python and Swift extractors. Other active builtin or
custom consumers fail before publication with an explicit unaudited-input error.
Their inactive builtin detectors remain policy scoped. This restriction is specific
to the new graph profile; it is not an upstream extraction removal. Arbitrary new
consumer support requires auditing hidden reads and detector inputs first.

Audited opaque media retains name membership for Markdown link resolution, but
content-only edits cause no graph work. `graph_inputs.semantic` promotes matching
media bytes to semantic inputs; explicit declared Swift includes also retain their
content dependency. These promotions never revive excluded inputs. Native media
membership changes reconcile, whereas lock add/edit/delete and cache events are
filtered. A caller constructing `ChangeBatch` itself must set `Reconcile` for name
changes: `Paths` alone describes content notifications, not a complete file-system
operation log.

Use `graphsession.NewGraphFileChangeSource(eng.Analysis(), root, ignored, limit)`
before `Start` to exclude directories during initial registration. `Watch` uses
this factory automatically. `CoverSessionInputs` updates policy snapshots, covers
Git index/config/ignore controls and declared external dependencies, registers newly
included directories, prunes excluded registrations, and retains late events.
Its registration-gap reconciliation remains mandatory. `ObservedPath(absolutePath)`
returns the last processed native event sequence in a bounded diagnostic cache,
including filtered events, enabling exact-path tests of lock event delivery. Zero
means absent from that cache; neither this diagnostic nor `ObservedEvents` is a
backend flush or strict disk-equality barrier.

Each reconciliation rebuilds the engine and immutable policy; content/idle requests
reuse the committed versions. Policy identity and raw config validity are tracked
separately from TypeScript semantic contexts. A tracked source membership or source
selection change uses source/resolution invalidation and owner retirement; policy
identity alone no longer forces every TS source to reparse. External state/output directories do not affect repository policy identity,
so an otherwise compatible branch fork retains source cache reuse. Strict startup
currently builds policy in both the factory and session reconciliation. No cold-start
or history-delta performance acceptance follows from these changes.

A failed transaction restores the committed engine, and changes during config reload
are rejected. Reconciliation checks policy-control captures before completion;
observed online transactions retain later control events for subsequent processing.
Native coverage rejects external dependencies through symlinked ancestors below the
checkout/dependency common ancestor. These changes retain the existing journal,
acknowledgment, generation and pending-state durability protocol.

`Work.PolicyBuilds` counts policy reconstructions inside a generation request; the
factory's initial policy build occurs before that request and is separate. Existing
inventory/hash counters do not count the policy builder's additional name walk or
Git process IO. Legacy `OpenSession` callers must supply an engine built from the
configuration they intend to analyze; the generic legacy constructor has no consumed
configuration snapshot to validate a change before the first request. The new graph
factory rebuilds from disk at that first request and has a dedicated regression.


## Scoped TypeScript contexts

Graph-profile state records raw analysis/config captures separately from effective
TS contexts. Shared extractor readers produce the selected root, framework/ORM
gates and configured-client key. Per-source contexts contain effective nearest
package attribution and aliases (including supported relative extends and SvelteKit
fallbacks). Package scripts/formatting and dependency additions with unchanged
reader outputs cause zero TS parses; manifest consumers still update their declared
dependency facts. Package attribution and existing tsconfig alias changes dirty only
sources whose effective context changes, then retain the existing transitive
resolution/re-export invalidation. This remains whole-file extraction, not a pure
parse/bind split. A descendant package boundary stops ancestor-name attribution.

True global framework/ORM/root/client changes retain full TS fallback. Malformed package and tsconfig candidates retain conservative validity boundaries;
valid membership changes rely on the actual root, gate and per-file reader outputs. Other config families, including arbitrary external extends filenames,
remain raw-byte-sensitive global fallbacks with their paths in ContextReasons.
Metadata-only package changes can still rerun manifest extraction before its graph
fingerprint proves no publication is needed. Policy building, context discovery,
composition and whole-state persistence retain their previously documented costs.

`Result.Invalidation` reports RawConfigChanged, PolicyReconciled, AddedSources,
RemovedSources, ContextAffectedSources, ContextReasons and ParsedByReason.
ParsedByReason counts actual TS extraction operations, including initial, added
source, source content, file semantic context, semantic context, resolution, global
fallback and specialized extraction. Resolution counts are reparses caused by
binding changes; they are not a claim of parser-free rebinding. The map sums to
ParsedFiles. Fixture counters establish scope, not Product timings.

Pure CHMOD events on captured Git index/HEAD/config controls are suppressed only
when their readable SHA-256 content exactly matches the policy snapshot. Writes,
atomic replacements, removals, combined operations, changed bytes and read failures
still reconcile. This avoids repeated Git metadata notifications creating a graph
reconciliation loop; native regression additionally checks that observed event
counts become quiet. There is no size/mtime shortcut. Cache version v274 and graph
profile/context version 2 rebuild earlier discovery/context checkpoints safely.
