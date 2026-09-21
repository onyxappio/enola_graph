# Default graph-input ignore audit

Read-only audit, 2026-09-21. No repository edits, builds, tests, benchmarks, or asset-content reads were performed. Source files are a moving shared checkout: resident and manifest workers may change the cited implementation after this observation. The only output is this report. Recommendations require integration and validation by the coordinator; this is not a performance acceptance result.

## Recommendation

Use three explicit categories, not one larger ignore-glob list:

1. **Fully excluded input:** do not enumerate descendants, detect, hash, parse, side-read, register recursive watches, or enqueue events. Dependency lockfiles are mandatory members under the user's contract; distinguish their prohibition from ordinary overridable defaults.
2. **Name-only input:** preserve paths for module/document link resolution and membership changes, but do not read/hash bytes or invalidate on content-only edits when all enabled consumers are audited as content-independent. This is the right default for ordinary opaque media assets.
3. **Semantic input:** retain source and declared configuration dependencies, even if they produce no standalone graph node. Input-only is different from output omission.

The largest safe improvement is consistent enforcement at all boundaries, plus event classification before reconciliation. Existing defaults already exclude most conventional dependency/build trees. Do not introduce blanket JSON, YAML, docs, migrations, generated-source, dot-directory, bin/, or public/ exclusions.

## What exists today

- `internal/config/config.go:234` already excludes any-depth vendor, node_modules, .git, dist, build, tmp, public/assets, testdata, Python environments/caches, Pods, .gradle, target, obj, .NET bin/Debug and bin/Release, Dart caches/generated suffixes, named minified/bundled JS, and convention-scoped test source patterns. `.enola` and configured output-directory exclusions are derived/retained. Several JS framework/cache exclusions are root-only: `.next/**`, `.vercel/**`, `.turbo/**`, `.nuxt/**`, `.svelte-kit/**`, `coverage/**`, `out/**`.
- These are inherited upstream choices, not proof that every such path is dispensable for every project. In particular generated Dart and test omission are existing scope differences. Preserve current scope while documenting overrides; do not expand generic generated exclusions.
- The bundled `mcp-arch.yaml` is **more aggressive than `config.Default()`**: it excludes Markdown/MDX, JSON/YAML, Docker/env/Jenkins files, `**/generated/**`, and additional ecosystem paths. Do not describe these as universally effective graph-input defaults. Config loading unmarshals over Default; a supplied ignore sequence replaces that sequence rather than providing a general append/negation override system.
- `internal/engine/engine.go:1085` collects `AllNames` before ignoring individual files; ignored directories are pruned before descent. Ignored test files can enter `TestFiles` for reference extraction. Thus current `ignore` means omission from normal extraction inventory, not absence from all semantic inputs.
- `internal/graphsession/owner.go:164` fingerprints declared content dependencies and, for `NameSetInput`, inventory names. `filesToHash` selectively hashes content inputs only when all detected/enabled extractors implement `DeltaInputs`; one opaque extractor restores conservative hashing of Files, TestFiles, and AllNames. Previous owner files are also hashed. Adding an asset ignore glob cannot guarantee avoided bytes.
- TS declares sources/templates as content inputs; tsconfig/package and framework configs are separately discovered (`tsextractor/session.go:622`). mdintent declares Markdown content plus name-set dependence: links to an asset may require its name even when its bytes are irrelevant.
- `readRuntimeInputs` (`graphsession/resident.go:47`) inventories, detects, selects hashes, captures independent contexts, and discovers config paths. Fresh process startup reconciles; it does not establish near-zero cold CLI cost.
- The observed resident content fast path accepts existing TS source edits, with conservative checks/fallbacks for other extractors, Angular, unknown paths, deletions and configuration. A content-only image event currently falls back instead of immediately disappearing.
- `graphsession/changes.go:137` watcher ignores are literal path prefixes from state directory/WatchIgnore, not the engine glob policy. Registration walks directories; create/rename/remove signals reconciliation. A correct policy must prune registration and classify irrelevant events **before** setting a name-change/lost flag. Actual overflow, lost coverage and epoch discontinuity must still reconcile.

## Small default-policy change set

### Full exclusion

Retain existing dependency/cache/VCS/output exclusions with explicit profile scope. Add/normalize these exact directory patterns for nested projects:

```
**/.next/**
**/.nuxt/**
**/.svelte-kit/**
**/.vercel/**
**/.turbo/**
**/.parcel-cache/**
**/.npm/**
**/.pnpm-store/**
**/.yarn/cache/**
**/.yarn/unplugged/**
**/.hg/**
**/.svn/**
```

Also fully exclude the actual graph state/journal directory and configured artifact output path, including a custom in-repository state path. Keep `.yarn/patches`, `.yarn/plugins`, `.yarn/releases`, `.yarnrc.yml`, and `.pnp.cjs` outside this proposed blanket exclusion: executable/config/patch inputs need an explicit dependency decision. Do not add all `.yarn/**`.

These cache/output paths should be overridable if a project deliberately stores source there. Do not automatically broaden root-only `out/**` or `coverage/**` to any depth: ordinary package names can collide. Existing broad `build`, `target`, `tmp`, `vendor` rules similarly deserve documented scope overrides rather than stronger claims of universal safety. No new generic `artifacts/**`, `reports/**`, `.cache/**`, `generated/**`, `bin/**`, `docs/**`, or `migrations/**` default.

### Name-only media, not full exclusion

For the audited graph profile, candidate exact suffix patterns are `**/*.png`, `**/*.jpg`, `**/*.jpeg`, `**/*.gif`, `**/*.webp`, `**/*.avif`, `**/*.ico`, `**/*.bmp`, `**/*.mp4`, `**/*.mov`, `**/*.webm`, `**/*.mp3`, `**/*.wav`, `**/*.ogg`, `**/*.woff`, `**/*.woff2`, `**/*.ttf`, `**/*.otf`. Implement extension matching with explicit case handling, rather than silently missing uppercase filenames.

No content reads/hashes for these unless an enabled extractor explicitly consumes them; custom/opaque consumers must retain conservative handling until their contract is audited. Keep membership for doc links/assets/import resolution. A same-path content edit should be a no-op; add/delete/rename can affect a direct relationship and must update the relevant name-dependent consumer. SVG, HTML, CSS, source maps, archives and arbitrary binary/data suffixes need separate consumer review; do not blanket-classify them from this bounded audit.

Do not implement name-only by appending these patterns to current Ignore: that removes them from `inv.Files`, which is the name set used by mdintent's fingerprint and extraction, and can change link semantics.

### Retain semantic inputs

Keep package.json dependency declarations, tsconfig/jsconfig and extends chains, framework configuration, .graphql/.proto/schema.prisma, OpenAPI/AsyncAPI JSON/YAML and refs, infrastructure YAML/HCL, language manifests/project files, and semantic source-generated declarations/clients/types. Keep SQL migrations/schema and Markdown/intent docs where supported by the configured extractor set. Never identify lockfiles by `*.lock` alone or classify every JSON manifest as lock data. Use the coordinator's exact lock policy across inventory, config discovery, side reads, caches and watcher events.

Proposed override interface (not currently implemented): separate `exclude_inputs`, `include_inputs`, and `content_ignore` policies with documented precedence; include exceptions must keep ancestor directories traversable. Enforce mandatory graph-profile no-lock semantics separately. Reusing current Ignore as all three controls would preserve ambiguities. Include policy identity in cache/state fingerprints and migrate or discard contributions removed by the new policy.

## Hidden reads that defeat glob-only changes

- OpenAPI and AsyncAPI Extract intentionally ignore the supplied file list and walk independently (`openapiextractor/openapi.go:60`, `asyncapiextractor/asyncapi.go:103`). Their hard-coded skip lists differ from engine defaults. AsyncAPI probes **all** JSON/YAML candidates (first 4096 bytes, then full file for matching candidates). Disabling an inactive detection dependency without tracking content can miss a new spec in an existing file.
- Manifest extraction/context uses `detectnames.Walk`; that helper skips dot directories, node_modules/vendor/testdata, but not all engine-pruned output trees. Context reads can therefore see engine-pruned manifests. The concurrent manifest worker owns no-lock mode; that change alone does not unify these other exclusions.
- TS config traversal and Angular package discovery use separate skip tables; explicit config extends may read outside normal inventory, including external paths. TS config input roots still listed lockfiles at observation time. Root coordinator owns that integration.
- gRPC detection, Ansible, Dart pubspec and PHP route configuration have independent traversal; Ruby reads schema/structure SQL and Packwerk declarations, Go reads go.mod, Kotlin/Scala read build configs, Swift follows XcodeGen includes. This is a bounded call-site audit, not proof of every extractor's dependency closure.
- Solve with a shared graph-input policy plus declared/observed side-input dependencies, preserving input-only files and referenced external configs. Output-fact filtering after extraction does not avoid work or prevent ignored inputs from changing other files' facts. A configured full exclusion must be enforced before independent reads, with explicit semantics for a required dependency pointing into excluded scope.

## Product metadata evidence

Snapshot: `/tmp/enola-product-benchmark-source`, stat/name enumeration only, no file content opened. Pruned .git/.enola; traversal also configured conventional dependency/cache pruning, but no additional matching directories were encountered. Counts are this checkout's filesystem inventory, not exact engine inventory or a tracked-only baseline; excluded directory sizes were not measured. No node_modules descent, mutation, build, test, or timing was performed. The history checkout was available but not needed for this bounded census.

| Selection | Files | Logical bytes |
|---|---:|---:|
| Enumerated total | 45,105 | 1,357,597,803 |
| JSON | 14,828 | 779,981,571 |
| PNG | 20,110 | 453,353,465 |
| TS | 5,295 | 47,163,983 |
| TSX | 939 | 5,392,091 |
| Markdown | 2,133 | 17,021,486 |
| MP4 + MOV | 11 | 15,539,603 |
| apps/mobile/e2e/artifacts | 30,037 | 759,081,168 |
| worker-reports | 1,418 | 175,937,050 |
| docs | 1,052 | 32,768,962 |
| package.json | 31 | 48,429 |
| tsconfig*.json | 28 | 8,217 |
| generated path segment | 15 | 1,425,099 |
| migration-containing directory segment | 13 | 18,793 |

The two report/artifact trees together contain 31,455 files / 935,018,218 bytes (about 69% of enumerated bytes). Candidate **Product-specific opt-in** exclusions are `/apps/mobile/e2e/artifacts/**` and `/worker-reports/**` (repo-relative patterns without the initial slash in this engine). They are not certified safe default exclusions: those trees contain respectively 22 and 24 files with source extensions, and may have deliberate document/intent evidence. Verify required graph scope before applying. Do not exclude all e2e or docs. Three largest JSON files are gate-result manifests of 81,658,264, 62,681,329 and 48,721,306 bytes; there is strong motivation to audit content consumers instead of reading all JSON, but their size alone cannot prove semantic irrelevance. Byte totals are potential avoided I/O ceilings, not measured current hashing or speedups.

## Integration regression matrix

| Scenario | Required result |
|---|---|
| Lock add/edit/delete/rename at root/nested/ignored-file locations | Zero parsing/publication/generation advance; no names/config/manifest side-input change; fresh graph identical |
| Cache/output subtree events, nested monorepo caches, custom state/output directory | No descendant inventory/detection/hashing/watch registration; no feedback loop or resident reconcile |
| Opaque-media byte edit | Zero content hashes/parses/events/generation change under audited profile |
| Linked media add/delete/rename | Correct direct doc/name relationships; delta equals fresh target graph |
| package.json dependency or nested tsconfig/extends edit | Semantic graph change preserved; needed scope reanalysis; cold/delta equality |
| Previously ordinary JSON becomes AsyncAPI, spec ref changes | Correct activation/ref invalidation even if no source extension changed |
| Markdown intent/doc, migration/schema, generated imported TS/.d.ts changes | Retained supported facts and direct edges; no blanket exclusions |
| Excluded path through independent walk/side read; explicit dependency crossing boundary | Policy enforced consistently or explicit dependency diagnostic; never silent stale graph |
| Include override below excluded parent, custom extractor consuming a nominal asset | Override traversable and consumer content tracked; conservative unknown-consumer fallback |
| Rename across excluded/included boundary; directory replacement; overflow/gap | Correct deletions/additions; genuine coverage loss still reconciles |
| Policy/cache version transition | Removed owners replaced/deleted once; subsequent unchanged run publishes nothing |
| No-change resident versus fresh CLI | Separate counters/measurements; no resident latency claim used as cold-start evidence |

Coordinator validation should measure repeated initial-to-all-acks, first batch, no-change fresh startup, resident idle/event handling, changed-file absolute latency and ratio, memory, bytes hashed and actual parses. Do not claim acceptance from this read-only audit or from larger ignore counts alone.
