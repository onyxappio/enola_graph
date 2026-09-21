# Scoped delta implementation

Completed the next bounded scoped-delta stage in main after coordinator freeze of `/tmp/enola-policy-stage-source`. No Product source, history checkout, benchmark harness, broker measurements, commit, push or Telegram message was used. Root owns real 100-path history timing and parse-count acceptance; this report contains fixture scope evidence only.

## Validity domains

Graph-profile sessions now persist three distinct validity domains:

1. `ConfigHash`, captured raw config bytes, source hashes and policy-control captures continue to fence transaction consistency. A scripts-only package edit during a source transaction still aborts with `ErrInputsChanged`; the no-publication path also rechecks raw analysis fingerprints. Raw package bytes have not been replaced with a semantic JSON whitelist.
2. `PolicyIdentity` records input-selection reconciliation. Tracked membership and selection policy changes no longer directly set the global TypeScript reparse flag. Existing inventory, tombstones, owner retirement, import invalidation, re-export and reverse dependency closure remain responsible for membership correctness.
3. `EngineContextHash`, `TSContext` and `TSFileContext` determine semantic cache reuse. Global engine keys cover registered extractor configuration keys. TS global contexts use actual selected-root and framework/ORM reader outputs and configured clients. Per-source contexts use effective nearest package attribution and alias maps produced by the existing readers. Maps are immutable after transaction construction and promoted with committed inputs/state.

Legacy non-graph engine paths retain their raw-config fallback behavior. Graph state remains structurally compatible; older state lacking the new contexts is rebuilt conservatively. Cache provenance is bumped to v274, with cachecov entries; graph input/engine-context identities use version 2 and TS effective contexts use version 2. The migration fixture starts from old-discovery contributions and a v273 checkpoint and verifies that now-allowed manifest/Markdown facts are recovered, matching an independently constructed cold graph.

## Reader audit and supported scope

| Reader/domain | Signature and invalidation |
|---|---|
| `findTSRoot`, including `hasTSMarkers`, package structural markers and adaptive depth | Actual selected root/found result; a changed root remains global TS fallback. |
| `detectNextJS`, Vue/Nuxt/SvelteKit, nested Ember/Angular, React Navigation, `detectORMs` | Exact existing detector outputs; changed global extraction flags remain full TS fallback. Dependency key values are not guessed from a hand-maintained generic manifest whitelist. |
| `collectPackageNames` / `nearestPackageName` | Per-source nearest package attribution; a parent name change stops at a descendant package boundary. |
| `collectTSAliasRoots`, supported relative extends, `aliasesForDir`, SvelteKit alias fallbacks | Per-source effective alias map, including replacement, wildcard suffix and exact-match semantics. Existing alias changes reparse only sources whose effective map changes, then run normal dependency/resolution closure. |
| Package/tsconfig validity | JSON/JSONC errors remain explicit conservative context changes; tsconfig validation uses the actual alias reader's decoded type. Healthy missing/present/empty candidates do not independently force all TS when reader outputs are unchanged. |
| Unsupported package-style extends, arbitrary external extends filenames and other unprojected config families | Conservative raw-sensitive global fallback, identified by context reason/path. Configured clients remain covered by the extractor's existing ConfigKey. |
| Manifests and other active extractors | Their own raw hashes/context/fact fingerprints remain authoritative. A manifest dependency change can update manifest facts while TS and unrelated consumers reuse their caches. |

Package scripts, formatting and the investigated `drizzle-orm: catalog:` / `@onyx/contracts: workspace:*` additions cause zero TS reparses when these actual reader outputs remain unchanged. The declared dependency facts still update. This is not a universal claim that dependencies are irrelevant: root ORM dependencies, nested Angular/Ember gates and root selection remain semantic.

Per-file context invalidation still performs whole-file extraction. It is not a syntax/resolve split, and changing an alias map can conservatively reparse files in its effective scope even if a particular alias is unused there. Source membership and changed exported/imported surfaces retain the existing fixed-point/reverse closure; no one-hop cap was introduced. Full-repository composition/signature fallbacks, Angular composition fallback and unresolved-reference membership broadening remain explicit existing behavior.

## Counters for the coordinator's 100-path run

`Result.Invalidation` is available in CLI `--summary-json` and resident results:

- `RawConfigChanged`: observed raw analysis fingerprint changed.
- `PolicyReconciled`: policy identity changed, independent of TS parse validity.
- `AddedSources`, `RemovedSources`: source membership changes.
- `ContextAffectedSources`: existing sources whose effective per-file semantic context changed.
- `ContextReasons`: deterministic global semantic/fallback reasons, including unprojected config paths.
- `ParsedByReason`: actual TS extraction operations classified as `initial`, `added source`, `source content`, `file semantic context`, `semantic context`, `resolution`, `global fallback`, or `specialized extraction`.

The reason counts sum to `ParsedFiles` in the fixture assertions. `resolution` is a resolution-induced whole-file reparse, not parser-free rebinding. These counters allow the root harness to distinguish real history source edits, membership/resolution work and context-driven reparses. This worker did not execute either real Product history and cannot report its final actual parsed count or latency.

## Fixture evidence

The scope fixture has five TS sources plus active manifests, Markdown, HCL, Python and Swift.

| Scenario | Asserted TS parse behavior |
|---|---|
| Package scripts-only or formatting-only edit | 0 parses, no events, unchanged generation. |
| Both investigated dependency additions with stable TS reader outputs | 0 TS parses; manifest graph advances and matches cold. |
| Root framework/ORM or nested Angular/Ember gate changes | Conservative semantic reparse and exact cold equality. |
| Parent package rename | 1 owned source reparse; descendant package boundary unaffected. |
| Nested alias map change with child directory | 2 affected descendant sources; unrelated root/package sources reuse. |
| Inherited alias change at package parent | 3 affected package sources; root sources reuse. |
| Add tracked TS source with unchanged context | 1 new-source parse; no global context reason despite changed policy identity. |
| Add/remove healthy empty package.json or tsconfig candidates | 0 TS parses when effective readers remain stable. |
| Add named package around an existing source | 1 descendant attribution reparse. |
| Re-export, rename, removal, explicit exclusion | Normal resolution/owner invalidation and independent cold equality. |
| Malformed syntax and typed alias-config errors, then recovery | Conservative invalidation; raw captures and cold equality preserved. |
| Raw package edit during extraction | Transaction rejected; recovery equals cold. |
| Configured TS client change | Reparse under changed client key; cold equality. |

## Native Git metadata loop repair

Root reproduced repeated index CHMOD events during policy reconciliation. A pure CHMOD notification is now discarded only for an explicitly captured index/HEAD/config path whose readable bytes have the same SHA-256 as the policy snapshot. There is no size/mtime shortcut. Changed bytes, write/rename/create/remove or combined operations, missing controls and read failures still reconcile.

The deterministic unit test checks identical versus changed/missing control contents and all relevant operation classes. The real native fixture repeatedly changes index metadata, proves native path delivery, verifies zero graph work/generation, then checks aggregate observed-event count stays quiet during an idle observation window. It subsequently makes a real Git tracked-exemption/index change and verifies cold equality. This checks both graph-queue quiescence and absence of a test-local notification/hash loop; Product's large index and backend behavior remain root's verification responsibility.

## Validation

- `/tmp/enola-scoped-affected.log`: affected TS extractor, graphsession, engine, cachecov, bootstrap and command full suites pass.
- `/tmp/enola-scoped-race.log`: those same full affected packages pass under race.
- `/tmp/enola-scoped-membership.log`: complete scoped fixture matrix passes after healthy-membership narrowing.
- `/tmp/enola-scoped-race-final.log`: graphsession and bootstrap full race suites pass after the membership change.
- `/tmp/enola-scoped-validity-final.log`: final typed-config-error and healthy-membership race regressions pass.
- `/tmp/enola-scoped-context-final.log`: final stable-dependency, conservative-context and scoped alias/package race matrix passes.
- `/tmp/enola-scoped-migration-watch.log`: cachecov, migration and native metadata fixture checks pass.
- `/tmp/enola-scoped-finalcases.log`: configured clients, config recovery and native event-quiet checks pass.
- `git diff --check` passes.

The separate discovery worker owns detectnames/Markdown changes and their tests; see `/tmp/enola-policy-discovery-fixes.md`. This stage owns their cache/profile migration only.

## Remaining costs and limits

Reconciliation still rebuilds policy and inventory and recomputes reader contexts; context construction adds package/alias discovery and repeated existing detector calls. Initial factory/session policy duplication remains. Non-TS manifests can re-extract before identical output is recognized, while Markdown membership still has whole-extractor fallback. TS graph composition, resolution index comparison, map/state copies, captured-byte verification, and full checkpoint serialization/fsync remain. A persistent local syntax/binding split, more selective import-candidate indexes, per-page Markdown membership dependencies and segmented state persistence are separate work.

No measured near-zero startup, Product history throughput or performance acceptance is claimed. The coordinator should freeze this stage, run the real history with the new reason counters, compare exact graph output against fresh target engines, and measure repeated broker-acknowledged isolated timings before deciding the next optimization.
