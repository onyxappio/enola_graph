# Preparation performance repair

Status: bounded implementation and affected validation complete. Real Product acceptance remains coordinator-owned.

## Audit and measured plan

Read AGENTS.md, CONTRIBUTING.md, docs/STREAMING_INCREMENTAL.md, /tmp/enola-scoped-delta-implementation.md, /tmp/enola-path-contract-fixes.md, and /tmp/enola-scoped-final-review.md. The first stage was read-only: no builds, tests, Product commands or production edits were made until the coordinator explicitly answered the profiling-isolation question with authorization.

Coordinator baseline profile receipts are `/tmp/enola-final-profile/initial.log` and `/tmp/enola-final-profile/noop.log`. Initial was 14.55 seconds real, with 0.462s inventory, 0.442s parallel detection, 0.940s hash/context preparation (only 0.171s actual hashing), 0.409s config discovery, and another 0.410s config discovery at verification. No-change was 6.00 seconds real; 0.428s load, 0.468s inventory, 0.446s detection, 1.117s hash/context preparation (0.178s actual hashing), 0.409s config discovery and 0.410s repeated verification config discovery. The `clone_file_state` timing includes all preceding runtime preparation and must not be interpreted as time spent cloning alone. The coordinator reported about 2.15s factory work before the no-change session.

Code audit traced repeated live walks through inventory, inactive schema detectors, manifest DeltaContext, TS config discovery, TS package collection, and extraction; those repeatedly call immutable graph policy classification. TS config discovery intentionally also calls the actual selected-root and alias-root readers to preserve Svelte/Nuxt/Next capture. Config fingerprints are recomputed for strict transaction verification and no-publication verification; semantic TS context is built during preparation, not again by the raw-fingerprint verifier. Preparation also independently probes Angular and repeats Svelte/Angular detection inside context construction. The bounded measured intervention targets repeated hard-exclusion matching shared by these readers, avoiding new filesystem or semantic caches.

## Implementation

Changed `internal/graphinput/policy.go` only in `Policy.hard`:

* Reuse the existing `entries` map as proof of hard admission. Build inserts a path only after it passes the immutable VCS, lockfile, output, Enola-exclusion and cache-exclusion predicates.
* During unknown-path/Build ancestor checking, stop once an ancestor already has that admission proof. The candidate itself is checked before moving to ancestors; unknown matching children still fail normally.
* Continue evaluating Git ignore/tracked exemption, caller directory bit, media classification, semantic override and conservative-media promotion on every Classify call. Entries may include Git-ignored paths: hard admission never implies graph admission.

This adds no mutable lookup cache, no directory snapshot, no reused file bytes, and no new policy identity. Live inventory/discovery, raw config captures, source reads/hashes, transaction verification, native coverage and replay/backpressure are unchanged. Svelte/Nuxt/Next candidate discovery is untouched. There is no extractor fact/schema change, so existing v274/profile/cache migration remains valid and no new cache version is needed.

## Regressions and diagnostic measurements

New `internal/graphinput/admission_test.go` compares cached policy decisions with a copied policy whose entry-admission proofs are absent. It covers actual Build inventories; relative/absolute paths; both directory-bit values; unknown children; literal and glob exclusions; lockfiles/VCS/output/cache exclusions; tracked exemptions and nested ignore negation; name-only media, semantic overrides and immutable conservative promotion. Existing policy and bootstrap coverage remains unchanged.

Synthetic `BenchmarkPolicyAdmission`, three samples per version, 500ms each:

| Version | ns/op spread | B/op | allocations/op |
|---|---:|---:|---:|
| Before | 9,223–9,242 | 432 | 7 |
| After | 384.3–385.3 | 0 | 0 |

This is a bounded classification microbenchmark, **not Product startup, delta, broker or acceptance evidence**. Logs: `/tmp/enola-preparation-policy-before.log`, `/tmp/enola-preparation-policy-after.log`. The after command also ran the complete policy suite three times successfully.

Failure sensitivity used a Go overlay pointing only to `/tmp/enola-preparation-policy-mutant.go`. The deliberate wrong fast path returned Semantic for every admitted path, bypassing Git/media distinctions; the new comparison test failed as expected (exit 1), including nested ignored TypeScript paths. Main source was never replaced with the mutant. Receipt: `/tmp/enola-preparation-policy-mutant.log` and `/tmp/enola-preparation-mutant-overlay.json`.

## Validation and limits

Affected package command completed successfully (exit 0):

```
/tmp/enola-toolchain/go/bin/go test ./internal/graphinput ./internal/engine ./internal/extractors/detectnames ./internal/extractors/tsextractor ./internal/extractors/manifestextractor ./internal/extractors/mdintent ./internal/graphsession ./pkg/bootstrap ./internal/cachecov ./internal/facts -count=1
```

Log: `/tmp/enola-preparation-affected.log`. All ten packages passed: graphinput, engine, detectnames, tsextractor, manifestextractor, mdintent, graphsession, bootstrap, cachecov and facts. The complete bootstrap suite includes scoped invalidation/cold equality, Svelte capture, policy discovery, migration and native-watch fixtures; no test was weakened or removed.

Additional checks:

- `/tmp/enola-toolchain/go/bin/go test -race ./internal/graphinput -count=1` — exit 0, `/tmp/enola-preparation-policy-race.log`; includes the final expanded unknown-child candidates.
- `/tmp/enola-toolchain/go/bin/go vet ./internal/graphinput` — exit 0.
- Formatting and `git diff --check` for both changed files — pass.

Only two repository files were edited in this worker task: `internal/graphinput/policy.go` and `internal/graphinput/admission_test.go`. No session/publication helper overlap occurred. All test/build processes have finished; the coordinator can take an exclusive Product profile slot now, after coordinating the separate transport worker. No further speculative reader/cache refactoring was made because the first measured shared bottleneck should be reprofiled before expanding scope.

No Product inputs, benchmark harness or pinned snapshots changed; no commits. No real Product analysis/benchmark was run by this worker. Coordinator owns transport optimization, final freeze, full validation, isolated repeated Product timings, history equivalence and acceptance. Strict fresh CLI still performs actual policy/inventory/content/config reconciliation; already-running covered resident idle follows its existing no-work path. This change does not claim near-zero fresh CLI startup or completion of the broader performance target.
