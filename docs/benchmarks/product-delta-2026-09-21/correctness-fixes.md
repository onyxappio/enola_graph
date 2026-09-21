# Final bounded delta correctness pass

2026-09-21 — task task_9a3327ba3401 / dispatch ctx_ab7c14e7896b.
Sole editor pass on the existing uncommitted candidate; no commits, pushes, Product mutations or performance measurements.

## Disposition of requested items

1. **Engine shared glob state: fixed.** Removed shared compiled set fields and constructor assignments. Each walk compiles immutable local ignore/test sets captured only by its walk closure. Non-walk helpers retain original MatchGlob/MatchAnyGlob behavior, including configuration changes between calls. `TestConcurrentInventory` performs concurrent inventories and helper checks on one Engine; focused race passes.
2. **Manifest missing lock inputs: fixed.** Added uv.lock, poetry.lock, Pipfile.lock, bun.lockb and pnpm-lock.yaml to lockNames, hence both ownership and selective content hashing. Added repository-aware `DeltaContext`: rediscover manifests through the extractor's original detectnames walk, hash their bytes plus every ancestor lock candidate, and retain missing/read-error states. This covers engine-pruned manifests and nearest-lock addition/removal precedence, not just the engine AllNames universe. Own per-extractor digest consumes this context as well as shared hashes.
3. **Swift side reads: fixed.** Repository-aware context hashes root project.yml and only its enabled root includes. It follows the implementation's case-sensitive read/parse attempt followed by case-insensitive lookup on read or parse failure. It deliberately does not follow nested include lists. Context also calls the exact shallow detectiOSProject probe, capturing empty Assets.xcassets and Info.plist existence despite engine pruning. Tests cover hidden include cold/delta changes, empty marker changes, case-insensitive lookup, disabled includes, nonrecursive semantics and marker removal.
4. **New detector scope narrowing: reverted.** Removed the newly added FileListDetector methods from mdintent, grpc, OpenAPI and AsyncAPI. Engine now invokes their unchanged original Detect walks. Original pruning/depth/absolute-path semantics are preserved. Scope regressions cover archive/deep markdown, custom-pruned markdown/proto/specs, relaxed-ignore testdata proto, absolute OpenAPI directory candidates and arbitrary AsyncAPI JSON.
5. **TS config traversal: verified, not narrowed.** Compared current session.go to untouched candidate2 source: tsConfigInputs traversal is unchanged apart from profiling. Both ConfigInputPaths/fingerprint discovery and SessionResult.ConfigPaths use the original independent walk. Existing TestIndependentPrunedConfig checks fingerprint; added TestSessionConfigPathsRetainPrunedConfig checks result discovery explicitly.
6. **Opaque/custom fallback: preserved.** No name-based privileges added. Custom FileOwner and custom mdintent continue to use conservative AllNames hashing/scan digest. Retained independent probes pass normally and under race.

## Additional correction exposed by the new test

The first Swift include cold/delta test failed: a renamed module left its prior synthetic owner in the applied graph. Whole-extractor replacements now include prior owners (including synthetic modules) in the replacement scope when output changes. The same test now passes exact consumer-versus-cold comparison. This remains whole-extractor fallback scope and introduces no transitive attribute propagation.

## Contract/versioning

Added optional plugin.DeltaContext supplement to DeltaInputs. The context is discovered before planning, included in each capable extractor's v3 input digest, and revalidated before successful EndReplace/local state advancement. Manifest and Swift context formats are independently tagged v1. Bumped extractor cache version to v273 with registered regression coverage, forcing migration from earlier incomplete dependency semantics. NATS publisher, journal, backpressure and local IO semantics were not changed.

## Validation

Toolchain: /tmp/enola-toolchain/go/bin/go; NATS path supplied via /tmp/enola-toolchain/bin.

Passed:

- `go test ./internal/graphsession ./internal/facts ./internal/cachecov ./internal/extractors/manifestextractor ./internal/extractors/swiftextractor ./internal/extractors/mdintent ./internal/extractors/grpcextractor ./internal/extractors/openapiextractor ./internal/extractors/asyncapiextractor ./internal/extractors/tsextractor ./internal/extractors/pythonextractor ./internal/extractors/hclextractor` — log `/tmp/enola-final-correctness-tests.log` (graphsession 59.861s).
- `go test -race ./internal/engine -run 'TestConcurrentInventory|TestDetectorOriginalScope'` — `/tmp/enola-final-correctness-race.log` (1.844s).
- `go test -race ./internal/graphsession -run 'TestIndependent|TestBuiltinHiddenInputDelta|TestManifestLockInputsAndPrecedence|TestSessionConfigPathsRetainPrunedConfig'` — `/tmp/enola-final-graphsession-race.log` (18.949s).
- Final Swift context refinement and new semantics regression: `go test ./internal/extractors/swiftextractor ./internal/facts ./internal/cachecov` — `/tmp/enola-final-context-tests.log`.
- `git diff --check`.

Meaningful initial failures were resolved: stale Swift synthetic module owner; factpath guard caught filepath.Dir on a repo-relative path (now path.Dir); absolute OpenAPI fixture needed a literal directory segment named openapi (corrected fixture).

Unchanged-input regressions require zero published events, zero parsed files and unchanged generation. Lock tests verify all five missing names enter ownership and shared hash targets, exercise graph-changing lock appearance, and check nearer-lock insertion/removal against cold consumers.

## Limits / what remains coordinator-owned

No performance acceptance is claimed. No same-build NATS initial/delta timing or Product benchmark state was touched. Root owns immutable build, full-repository tests, independent review, and repeated NATS benchmark acceptance.

This is not a claim of complete input closure for every retained extractor. The audit's older TS framework side reads (nested Svelte alias configs, selected-root Prisma schemas, marker variants/root-selection context and reads outside captured overlays) remain a separate audit/implementation scope. Their historical config traversal was preserved, not weakened by inventory reuse. OpenAPI/AsyncAPI and other opaque extractors retain baseline AllNames fallback; independent side reads outside that pre-existing universe, including pruned AsyncAPI reference targets, are not solved by this pass. The new manifest/Swift contexts specifically close their identified side-input gaps; context revalidation is not a transactional filesystem snapshot.

## Files edited in this dispatch

- internal/engine/engine.go
- internal/engine/cache.go
- internal/engine/inventory_race_test.go (new)
- internal/engine/detector_scope_test.go (new)
- internal/cachecov/coverage_test.go
- pkg/plugin/plugin.go
- internal/graphsession/owner.go
- internal/graphsession/session.go
- internal/graphsession/hidden_input_test.go (new)
- internal/extractors/manifestextractor/parsers.go
- internal/extractors/manifestextractor/delta.go (new)
- internal/extractors/swiftextractor/delta.go (new)
- internal/extractors/swiftextractor/delta_test.go (new)
- internal/extractors/mdintent/mdintent.go
- internal/extractors/grpcextractor/grpc.go
- internal/extractors/openapiextractor/openapi.go
- internal/extractors/asyncapiextractor/asyncapi.go
