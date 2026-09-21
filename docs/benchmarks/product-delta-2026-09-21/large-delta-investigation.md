# Product large-delta investigation (read-only)

## Scope and conclusion

Inspected frozen extractor/session source at `/tmp/enola-resident-stage-source`, existing smoke evidence at `/tmp/enola-resident-history27-smoke` and `/tmp/enola-resident-history100-smoke`, and Git objects in `/tmp/enola-product-history-source`. No production edits, Product mutations, timing runs, builds or tests were performed. Code references below are relative to the frozen source, not the concurrently changing main checkout.

The 100-path history is **not scripts-only**, but its global TypeScript configuration fallback is broader than the audited built-in readers require for these particular manifest changes. Excluding locks leaves two changed package manifests: one adds a dev dependency, the other adds a dependency and a script. These dependencies must update manifest graph facts; neither changes this snapshot's TS root, package names, alias maps or framework/ORM flags. The 27-path history has no changed TS configuration or package manifest; its costs include full membership reconciliation, real source resolution changes and a whole Markdown extractor rerun. Lock removal alone cannot eliminate the 100-path global fallback while raw package bytes remain in the shared fingerprint.

## Exact histories and changed semantic inputs

Target: `a609c19f3861971930fae7b33dcb2950598953c5`.

| Base | Git name-status rows | Changes |
|---|---:|---|
| `a6f1f3a91a36ea3dead786560412a4944a009694` | 27 | 16 modified, 11 added |
| `599575d0aa398615cde6a4ac12bc0c7734a686d9` | 100 | 64 modified, 33 added, 1 deleted, 2 renamed |

Counts use normal Git rename detection: each renamed pair is one row, not one filesystem event.

**27-path:** None of the named TS config discovery inputs changes: no package.json, tsconfig/jsconfig, framework config, Prisma schema or lock change. Product file docs/analytics/product-core-bronze.md changes, and 11 paths are added. Real TS resolution changes include `packages/clickhouse/src/productCoreBronze/index.ts`, which adds named re-exports from new `./contractDigest` and `./prepareDestination` modules and a `ProductCoreBronzePendingProviderColumn` type export. These changes justify rechecking dependent references even after inventory costs are optimized.

**100-path, non-lock configuration inputs:** Exactly these two tracked package manifests change:

```diff
# packages/clickhouse/package.json, devDependencies
+    "drizzle-orm": "catalog:",

# packages/database/package.json, scripts
+    "db:sync-payment-offers": "tsx src/paymentOfferSyncCli.ts",
# packages/database/package.json, dependencies
+    "@onyx/contracts": "workspace:*",
```

No package name, exports, main, type, workspaces or existing dependency constraint changes. No tracked tsconfig/jsconfig, Deno, Next/Nuxt/Svelte, Angular/Nx configuration, Prisma schema, or local extends target changes. The only discovered `extends` declarations in the 100-path base and target are unchanged `apps/mobile/tsconfig.json -> expo/tsconfig.base` and `apps/safari-extension/tsconfig.json -> ./.wxt/tsconfig.json`; the former is not a relative extends supported by this reader, and neither target is among changed Git paths. This is a Git-history conclusion, not a claim about arbitrary untracked/external filesystem changes.

`pnpm-lock.yaml` also changes, but is excluded from the intended graph profile and from the semantic conclusions here. Names like `services/payment-api/src/config.ts` and `scripts/deploy/stripe-checkout-runtime-config.mjs` are ordinary source inputs, not Enola TS configuration inputs. New `packages/database/catalogs/payment-offers/v1/manifest.json` is application data, not an npm manifest.

Other real resolution changes in the 100-path history include `packages/database/src/index.ts` adding exports from `./paymentOfferSeed`, `./paymentOfferCatalog`, and `./paymentDemoRuntime.mjs`. The new catalog module imports local paymentOfferSeed plus node:crypto/fs/util. Markdown membership changes include deleting `infra/pulumi/config/payment-seed-offers/README.md`, adding `packages/database/catalogs/payment-offers/README.md`, and moving the two provider-ref JSON files into `packages/database/catalogs/payment-offers/v1/`. Existing Markdown links can change resolution even without edits to their source pages.

## Existing measurements, not new performance acceptance

| Resident history | Apply seconds | Broker end seconds | First batch seconds | TS files parsed | Hashed files | Published events | Checkpoint bytes |
|---|---:|---:|---:|---:|---:|---:|---:|
| 27-path | 5.305389 | 5.138522 | 2.236517 | 348 | 9,030 | 1,268 | 58,153,609 |
| 100-path | 10.811951 | 10.656455 | 2.406571 | 4,181 | 9,031 | 5,510 | 58,153,611 |

Both perform one inventory/detection/context/config scan and verify 4,281 files. The 27-path run reports only mdintent whole-extractor fallback; 100-path reports TS config fallback plus manifests/mdintent/hcl/python/swift fallbacks. `files_parsed` is the TS session counter, **not Markdown parses**: do not attribute the 348 TS parses to mdintent. Their split between actual edits, unresolved imports, reverse dependencies and composition work is not available from these aggregate counters.

Resident initial wall times are 16.412483 and 14.297552 seconds respectively (delta/initial roughly 32.3% and 75.6%). Fresh target CLI runs are 12.063167 and 11.187238 seconds; the 100-path delta is roughly 96.6% of its target cold run. These are single smoke observations, without a repeated-measurement spread or stage timing attribution. Existing checks report cold-target graph equality and resident no-op zero work/events for both histories. Their graph oracle predates the new complete lock-ignore/input-policy integration and must be regenerated under the new profile. Resident results do not establish fresh-process startup performance.

## Reader audit: why scripts-only currently broadens unnecessarily

* `graphsession/session.go:1533` (`analysisFingerprintInputs`) hashes config paths, missing/directory markers, **all raw bytes**, extractor version, configured extractors/ignores and ConfigKey values. `tsextractor/session.go:623` discovers root config slots (including legacy locks) and recursively all package.json / tsconfig.json / tsconfig.base.json / jsconfig.json files plus supported extends. `graphsession/session.go:296` sets shared `forceAll` for any changed config hash, not merely for a changed TS resolution result. The shared flag also broadens other extractors.
* `tsextractor/ts.go:190` (`packageJSONDeclaresPackage`) uses presence of bin/main/exports/type/workspaces, or a nonempty dependencies/devDependencies map. `findTSRoot` (`:97`) first checks the repository root. Product's unchanged root package.json has nonempty devDependencies, so it remains the TS root in both histories.
* `tsextractor/vue.go:117` (`hasPkgDependency`) parses dependencies/devDependencies and tests key presence. Framework/ORM consumers include `storage.go:46`, `ts.go:1483`, `ember.go:76`, `angular.go:67`, `reactnav.go:33`, and Vue/Nuxt/Svelte detectors. Most use tsRoot/repo root; Ember and Angular additionally search nested manifests, React Navigation searches immediate children. Nested additions here are drizzle-orm and @onyx/contracts, not their nested detection keys. Drizzle's gate is root/tsRoot only, so adding it to packages/clickhouse does not change the current repository-wide ORM flags. Do not assume this holds for another repository whose selected TS root changes.
* `tsextractor/ts.go:1427` (`collectPackageNames`) reads package `name` throughout the repository for nearest-package module attribution; neither changed manifest changes name. `ts.go:1874` and `:2031` read alias roots and tsconfig extends/baseUrl/paths; no corresponding input changes here. A compiler's broader semantics are not implied by this fork's reader.
* `manifestextractor/parsers.go:83` reads full declared dependencies/devDependencies maps, including constraint strings and dev classification. Both additions are semantic manifest changes; workspace:* and catalog: must remain declared constraints. This reader does not use scripts. Its `delta.go:18` context still fingerprints raw manifest bytes, so even a scripts-only edit can trigger a manifest fallback after separating TS invalidation.
* A production-source search for package.json reads and `json:"scripts"` found no built-in TS/manifest extraction consumer of scripts. This supports a **narrow scripts/metadata optimization for audited consumers**, not a universal JSON field whitelist. Detection structural keys, malformed JSON behavior, missing-vs-present files, package names and dependency gates must remain represented. Unknown/plugin consumers stay conservative.

Do not change the existing fingerprint in place to a selective digest without separating purposes: captured raw bytes and raw hashes also serve transaction consistency and read overlays. Keep raw snapshots for validation, and add a versioned TS semantic-context key computed by the same actual readers. Also do not mistake `CompositionSignature` (`tsextractor/session.go:729`) for this key: it covers GraphQL/gRPC/Nuxt composition inputs, not all framework flags, aliases, package attribution or configured clients.

## Prioritized minimal next changes and correctness constraints

1. **Separate global input validity from TS reparse validity.** After the other owner's lock/policy integration, introduce a versioned TS context signature using actual detector outputs, selected TS root, alias-root maps, package-name attribution, client configuration and all other audited TS side inputs. Continue raw-byte observation and transaction checks. On unchanged TS context, allow manifest changes to recompute manifest facts without forcing every TS file or unrelated extractor. Start conservatively: retain existing full fallback for unsupported config families/consumers and errors. A metadata-only package edit is a useful first regression; a deps/devDeps-map-only signature will still invalidate both Product manifests and thus will not solve this history by itself. The strongest bounded opportunity here is recognizing unchanged **effective TS context** while preserving two real dependency facts. General per-package resolution invalidation is a later step, not required to avoid this particular global reparse.

2. **Give Markdown whole-file cached contributions.** `mdintent/mdintent.go:90` currently rereads all Markdown whenever one content input or the inventory name set changes (`graphsession/owner.go:164`, NameSetInput at :189). Separate a per-page extraction operation from the full inventory context; content-only edits can replace one page plus aggregated `mdintent:links` coverage. Retain whole-md fallback for membership changes as the first safe step. Next retain candidate dependencies for each page's links, including unresolved links, relative-first/root-second candidates (`mdintent/document.go:221`), and directory existence counts. Reevaluate pages affected by changed candidate names and ancestor directories; storing only resolved targets misses a newly added higher-priority candidate or previously missing target. Preserve fatal invalid-intent behavior and exact synthetic coverage counts. This materially reduces fallback work for 27-path only once membership handling is covered too.

3. **Add a bounded membership path before relaxing reconciliation generally.** `graphsession/changes.go:218` converts every Create/Rename/Remove to Lost/reconcile; `resident.go:282` accepts only existing regular TS content edits. Start with known file additions/removals in already watched directories, preserving complete event-path sets, policy decisions and tombstones. Keep reconciliation for directory moves/creation registration gaps, symlinks, watcher loss/overflow, policy/config changes, unknown detectors and unsupported side readers. Apply names to inventory and all interested consumers, then run existing resolution invalidation. Git checkout replacement events are not inherently trustworthy content-only edits. Simply suppressing Lost while leaving contentInputs unchanged will not yield the intended fast path.

4. **Narrow resolution work only after measuring why it broadens.** `graphsession/invalidate.go:70` marks unresolved-reference files dirty on any TS membership change; `session.go:540` onward reverse-closes changed declared/re-export/import surfaces and checks referenced names. Both histories add real exports, so an arbitrary one-hop limit or blanket reuse is incorrect. Retained raw import-candidate indexes could target new/shadowed candidates, but must cover ambiguity, deletion and re-exports before replacing this fallback. Add reason counters before claiming all 348 parses are avoidable.

5. **Profile residual whole-state work after those changes.** `session.go:964` verifies inputs; `:1007` records checkpointing; `persist.go:157` serializes pending state. Both deltas still write about 58 MB. Persistent file-owned state segments / bounded journal checkpoints may help, but require replay/crash consistency review and are not the smallest first fix. No evidence here quantifies their independent elapsed cost; larger journal limits alone do not address it.

Future validation should cover scripts-only and formatting-only package edits; real declared dependency updates without TS gate changes; framework-gate/name/alias changes that must invalidate; malformed/missing config transitions; Markdown heading/intent edits and newly resolved/shadowed/deleted link targets; source rename/deletion/re-export ambiguity; and both pinned histories versus fresh target analyses under the new profile. Run repeated broker-acknowledged timings with first batch, actual parses, memory and spread only in the coordinator's designated benchmark phase. Preserve zero-event/no-generation behavior for unchanged inputs and all lock-only operations, durable replay, bounded backpressure and file-owned replacement scopes.

## Coordinator follow-up: actionable scope API and new policy trigger

Read-only inspection of the live main checkout at `/Users/oleksandr.mykulych/orca/enola_graph` confirms a new intermediate broadening: `internal/graphsession/session.go:1539` hashes `graph-input-profile-v1/` plus `scope.Policy.Identity()` into cfgHash. `internal/graphinput/policy.go:385` collects sorted non-lock tracked names and includes them in Identity alongside options and semantic dependency hashes. Thus tracked source addition/deletion can now change cfgHash and forceAll even with identical package/config bytes. The smoke timings above do not measure this new integration. This must be separated as part of the first optimization, otherwise the 27-path and 100-path histories retain an all-TS fallback even after the package-byte trigger is fixed.

Proposed minimal API and staged implementation (design only):

```go
// Separate validity domains; rawInputs remain captured bytes for consistency.
type InputSnapshot struct {
    PolicyIdentity string       // triggers scope reconciliation, not forceAll
    Inventory      Inventory    // selected names + content/name-only classification
    TS             TSContext    // actual reader outputs, versioned
    RawInputs      map[string][]byte
}
type TSContext struct {
    Root string
    GlobalParseFlags FrameworkFlags
    AliasesByRoot map[string]AliasMap
    PackageNamesByDir map[string]string
    ClientKey string
    // Explicit keys for supported side readers; unknown family => fallback.
}
type ChangePlan struct {
    ParseFiles, ResolveFiles, RemovedFiles map[string]bool
    ManifestFiles map[string]bool
    Fallbacks []Fallback
}
```

1. Keep rebuilding policy/inventory initially. Compare prior/next **selected names and classification**, removing file-owned contributions outside new scope and adding newly admitted files. Policy identity changes cause this reconciliation, but do not directly imply TS parser context changes. Conservatively handle uncertain discovery/readers; tracked exemptions and nested ignore negations must remain correct. Retain Identity for cache validity rather than weakening its existing tracked-membership tests.
2. Produce TSContext via shared detection/config reader helpers, not a duplicate hand-maintained JSON allowlist. Start with exact equality: when TSContext is equal, eliminate config-driven forceAll, retaining existing source dirty/invalidation logic and independent manifest extraction. This directly addresses both manifest additions here while preserving dependency facts, and avoids the newly introduced tracked-name forceAll. Unknown config families or consumer contracts retain explicit full fallback.
3. Narrow changed contexts by consumer: package-name changes affect module attribution within nearest-package ownership (stop at descendant package boundaries) and relevant resolution indexes, not syntax parsing globally; alias changes affect files whose effective nearest alias map changes, plus importers/re-exports whose binding surface changes; configured framework/ORM flags currently apply repository-wide, so an actual flag change retains all-file fallback until its per-file consumer dependencies are modeled. Do not assume package.json directory bounds the present global framework semantics.
4. Initially, a ResolveFiles entry may conservatively run current whole-file extraction. The existing FileRecord stores already-derived Facts, normalized ImportSpecs/ResolvedFiles, declarations and framework DTOs (`tsextractor/session.go:35`); it is not a complete raw syntax cache that can be rebound to arbitrary new aliases. A genuine parse/bind split needs a versioned local contribution retaining raw import specifiers, raw reference/call sites, declaration identities and sufficient composition DTOs. Add `ExtractLocal(file, bytes, localFlags)` and `ResolveLocal(localRecord, context, candidateIndex)` behind the existing whole-file extractor; retain full extraction for unsupported rules. Rebinding must replace all that file's resolved contribution and update reverse/candidate indexes, then iterate affected binding surfaces to a fixed point. This is a separate implementation increment, not a one-line safe optimization to FileRecord reuse.

Focused acceptance tests for the first increment: (a) add a tracked TS file with unchanged TSContext and assert existing unrelated files are not parsed, while cold/delta graph equality holds; (b) alter tracked membership so a .gitignore exemption changes inclusion and assert obsolete owners are removed without stale edges; (c) scripts-only package edit has zero TS parses, while raw input consistency still detects concurrent edits; (d) both exact Product manifest dependency additions update manifest facts but cause no config-driven TS full parse; (e) changing nested Angular/Ember gate dependencies still activates the correct global fallback; (f) package rename/nearest-package boundary and inherited alias changes update appropriate module/reference facts; (g) raw config malformed-to-valid and missing-to-present transitions invalidate rather than collide with an empty projection. Assert separate counters/reasons for content parses, context reparses, rebindings and scope reconciliation. These tests are proposed, not run.
