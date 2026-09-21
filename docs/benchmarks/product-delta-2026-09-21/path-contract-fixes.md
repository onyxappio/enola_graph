# Path-contract corrections

Changed only the two assigned production files in the main workspace:
- `internal/extractors/tsextractor/context.go`: imported `internal/factpath` and replaced `filepath.ToSlash(filepath.Dir(file))` with `factpath.Dir(file)` for the repo-relative per-file context directory.
- `internal/extractors/mdintent/mdintent.go`: imported `internal/factpath` and normalized the file walker's `filepath.Rel` result immediately with `rel = factpath.Slash(rel)` before content-policy classification.

No tests weakened or added, no host exemptions added, no Product or snapshot edits, and no commits. Existing contract tests cover both precise regressions and failed before the corrections. Context digest fields, version, package/alias selection, and cache semantics remain unchanged; this uses the established fact-path operations for existing repo-relative inputs. Frozen reviewer source was read only to inspect the final two-file diff.

## Commands and results

Toolchain: `/tmp/enola-toolchain/go/bin/go version` => `go version go1.26.8 darwin/arm64`.
An initial bare `go test ./internal/facts -run TestPathContract -count=1` could not start because `go` was absent from PATH (exit 127); the existing local toolchain was then used explicitly.

1. `/tmp/enola-toolchain/go/bin/go test ./internal/facts -run TestPathContract -count=1 > /tmp/enola-path-contract-before.log 2>&1` — exit 1 before edits, reproducing exactly the TS host-builder and mdintent unnormalized-relative-path failures (facts 0.556s).
2. `/tmp/enola-toolchain/go/bin/gofmt -w internal/extractors/tsextractor/context.go internal/extractors/mdintent/mdintent.go` — exit 0.
3. `/tmp/enola-toolchain/go/bin/go test ./internal/facts ./internal/extractors/mdintent -count=1 > /tmp/enola-path-contract-facts-mdintent.log 2>&1` — exit 0; full facts passed (2.876s), full mdintent passed (0.946s).
4. `/tmp/enola-toolchain/go/bin/go test ./internal/extractors/tsextractor ./pkg/bootstrap -run 'Test(Scoped|.*Session|PolicyDiscovery)' -count=1 > /tmp/enola-path-contract-ts-bootstrap.log 2>&1` — exit 0; TS session/scoped selection and bootstrap scoped/session/policy tests passed; exact package timings are in the log below.
5. `/tmp/enola-toolchain/go/bin/gofmt -l internal/extractors/tsextractor/context.go internal/extractors/mdintent/mdintent.go` — exit 0, empty output.
6. `git diff --check -- internal/extractors/tsextractor/context.go internal/extractors/mdintent/mdintent.go` — exit 0.

Bootstrap coverage includes stable-context reuse, semantic changes, nearest-package/alias scoping, and cold-versus-delta graph equality. No full repository rerun or Windows execution was requested/performed; the path-contract source guards validate the portable API convention on this host.

Final scoped test output:
ok  	github.com/enola-labs/enola/internal/extractors/tsextractor	0.389s
ok  	github.com/enola-labs/enola/pkg/bootstrap	34.011s

## Authorized follow-up: Svelte configuration capture

Before settlement the coordinator expanded ownership to `internal/extractors/tsextractor/session.go` and a unique bootstrap regression file, `pkg/bootstrap/svelte_config_capture_test.go`. The original two-file restriction above describes the initial correction only; total final changes are these four files.

`tsConfigInputs` now retains all three Svelte config extensions (JS, TS, MJS) at the actual roots returned by `collectTSAliasRoots`, including missing candidates for additions. Root fallback candidates remain captured. Framework config candidates are also captured at the actual selected TS root because Svelte, Nuxt, and Next detectors inspect that root as well as the repository root. The sibling audit found two further already-supported omissions: `nuxt.config.mjs` in `detectNuxtAt` and `next.config.ts` in `detectNextJSAt`; those candidates are now captured too. No CJS or other speculative extension support was added.

This reuses the existing raw config capture, fingerprint, transaction fence, policy, and resident reconciliation paths. Svelte config bytes remain an unprojected configuration family, so changes explicitly retain conservative whole-TS fallback rather than pretending scoped performance was established. Source-unit support is unchanged. Discovery now additionally invokes the existing selected-root and alias-root readers; this correctness patch does not claim performance improvement, and Product was neither modified nor benchmarked. No cache version change is needed for the two path-only operations; expanded config candidate slots naturally change captured context/fingerprints and reconcile existing state.

The new regression covers resident and strict runs, repository and nested alias roots, JS/TS/MJS, config addition/edit/removal, exact graph equality to fresh analysis at each transition, and no publication on unchanged input. A separate candidate test checks missing slots at the repository root, selected TS root, and nested alias root. Existing tests remain unchanged.

Additional exact commands:

- `/tmp/enola-toolchain/go/bin/gofmt -w pkg/bootstrap/svelte_config_capture_test.go` — exit 0.
- `/tmp/enola-toolchain/go/bin/go test ./pkg/bootstrap -run 'TestScoped(SvelteConfigCapture|FrameworkConfigCandidates)' -count=1 > /tmp/enola-svelte-capture-before.log 2>&1` — exit 1 (27.149s), before the discovery fix; root MJS and all nested extensions failed cold graph equality, and omitted candidate assertions failed. Strict cases already passed via full reconciliation.
- `/tmp/enola-toolchain/go/bin/gofmt -w internal/extractors/tsextractor/session.go` — exit 0.
- `/tmp/enola-toolchain/go/bin/go test ./pkg/bootstrap -run 'TestScoped(SvelteConfigCapture|FrameworkConfigCandidates)' -count=1 > /tmp/enola-svelte-capture-after.log 2>&1` — exit 0 (41.495s).
- `/tmp/enola-toolchain/go/bin/go test ./internal/facts ./internal/extractors/mdintent ./internal/extractors/tsextractor ./internal/graphsession -count=1 > /tmp/enola-path-contract-expanded.log 2>&1` — results below.
- `/tmp/enola-toolchain/go/bin/go test ./pkg/bootstrap -run 'Test(Scoped|.*Session|PolicyDiscovery)' -count=1 > /tmp/enola-path-contract-final-bootstrap.log 2>&1` — results below.
- `/tmp/enola-toolchain/go/bin/gofmt -l internal/extractors/tsextractor/context.go internal/extractors/mdintent/mdintent.go internal/extractors/tsextractor/session.go pkg/bootstrap/svelte_config_capture_test.go` — exit 0, empty output.

Final expanded test results (both commands exited 0):
ok  	github.com/enola-labs/enola/internal/facts	2.747s
ok  	github.com/enola-labs/enola/internal/extractors/mdintent	0.865s
ok  	github.com/enola-labs/enola/internal/extractors/tsextractor	4.896s
ok  	github.com/enola-labs/enola/internal/graphsession	84.241s
ok  	github.com/enola-labs/enola/pkg/bootstrap	90.056s

Final four-file `git diff --check` also exited 0. All requested and added focused checks passed; remaining validation belongs to the coordinator's frozen/full-suite review and Product performance workflow, with no unresolved test failure in this task.
