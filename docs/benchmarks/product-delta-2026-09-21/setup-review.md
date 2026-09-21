# Delta setup optimization independent review

Verdict: **GO for this bounded correctness review; no actionable blocker found. This is not performance acceptance.**

Reviewed frozen `/tmp/enola-product-fastdelta3-source` against `/tmp/enola-product-fastdelta-source` and read `/tmp/enola-delta-setup-optimization.md`. Work ran only in `/tmp/enola-delta-setup-review-private`; neither main nor frozen production source was edited. No Product measurements, full-suite run, race run, build, vet, agent delegation, commit, push or Telegram occurred in this review. Coordinator owns those remaining checks. The near-zero no-op and body-delta substantially below four seconds goals remain open.

## Code findings

- `internal/engine/engine.go:155–207`: concurrency is an explicit registration-index capability, not a name or interface heuristic. Registry registration appends, so current indices remain stable. Ordinary registrations wait for all earlier independent calls and run synchronously before later launches. Disabled registrations are skipped as before; failed or false results do not activate the extractor; duplicate names retain the former OR-like active-map behavior. Four slots bound concurrently running detector calls. Workers write distinct result slice elements, and the final map is constructed after `Wait`, avoiding shared-map writes. Registration/configuration must finish before analysis, as documented; concurrent mutation of registries/config remains unsupported.
- `pkg/bootstrap/bootstrap.go:426–448`: reviewed all opted-in built-in Detect/DetectFiles paths and content-reading helpers (including Kotlin/Scala build reads and TS root discovery). They inspect names/files and local values without changing the shared input slice, persistent detector state or repository. Existing per-extractor discovery scope is preserved through Engine.detect. The legacy snapshot detection path is unchanged.
- `internal/extractors/asyncapiextractor/asyncapi.go:36–98`: results are only boolean ORs, so completion order cannot lose a positive on stable inputs. Four workers and a 32-candidate window bound outstanding work. Every pending item produces one result; flush consumes all results; jobs are closed and workers joined even after a WalkDir error. No persistent detection cache was added. Nil directory entries on walker errors are safe because the error condition short-circuits before d.Type.
- Directory, walk-error and special-file fences establish the previous prefix's positive result before making the old callback's pruning/error decisions. This deliberately preserves the odd prior behavior in which a positive prevents skipDir from pruning later directories, allowing later directory errors to fail detection. Symlinks/FIFOs are handled synchronously only when no preceding positive exists. The normal regular-file sequence may perform up to 32 candidate reads even when an earlier file is positive; this is an explicit speculative-read cost, not an exact read-trace equivalence claim.
- Candidate selection and parsing are unchanged: arbitrary JSON, hidden/deep candidates, the 4096-byte content prefilter and YAML root validation remain. Nested markers and malformed roots do not newly activate detection. Any pre-existing prefilter limitations remain unchanged rather than being fixed by parallelism.
- `internal/extractors/tsextractor/session.go:430–441` only suppresses the informational SessionResult.ConfigPaths inventory when explicitly requested. Default callers retain it. Graphsession sets the flag for its extraction rounds but still calls independent ConfigInputPaths via analysisFingerprint before extraction and again before successful EndReplace/state promotion (`internal/graphsession/session.go:211,933,1468+`). Config bytes remain captured and compared; discovery is not replaced with the pruned engine inventory. No-change retains its previous single discovery and early-return semantics. Source overlays, extraction facts and records are unaffected.

## Focused evidence

Toolchain: `/tmp/enola-toolchain/go/bin/go`.

First command, exit 0, recorded in `/tmp/enola-delta-setup-review-probes.log`:

```sh
go test ./internal/extractors/asyncapiextractor ./internal/engine ./internal/extractors/tsextractor ./internal/graphsession -run 'TestReviewWalk|TestParallel|TestPositiveCandidate|TestIndependentDetection|TestDetectionHonors|TestDetectorOriginalScope|TestSessionConfig|TestConfigRediscovery|TestConfigChangeRestore|TestConfigCapture|TestSourceHashUsesConsumedBytes|TestIndependentPrunedConfig|TestFullVsDeltaBodyDeleteRenameConfig|TestExtractSession.*Config' -count=1 -timeout=90s
```

AsyncAPI, engine and graphsession selected tests passed. That expression selected no TS-package test, so the exact TS inventory-equivalence test was then explicitly run and passed:

```sh
go test ./internal/extractors/tsextractor ./internal/extractors/asyncapiextractor -run 'TestSessionCanOmitUnusedConfigInventoryWithoutChangingExtraction|TestReviewWalkErrorAndSpecialFences' -v -count=1 -timeout=30s
```

Second command, exit 0, recorded in `/tmp/enola-delta-setup-review-extra.log`; no tests skipped. This checks omitted config inventory against unchanged extraction, and independently added private probe scratch-only file internal/extractors/asyncapiextractor/review_walk_test.go checks:

1. Unreadable `vendor` is pruned before any positive, matching the serial oracle.
2. Unreadable `vendor` after positive `a.json` returns the exact same non-nil WalkDir error as the serial oracle.
3. A real FIFO named `z.json` after positive `a.json` is not opened and detection returns successfully.

Existing focused tests additionally cover detector overlap/custom barriers/disabled and failed detection, original detection scopes, arbitrary and hidden/deep JSON, malformed and nested marker negatives, bounded workers, repeated edit/deletion detection, symlink fences, added engine-pruned configs rejecting promotion, captured config edit/restore, consumed source bytes and full-versus-delta body/delete/rename/config equivalence.

## Limits and remaining ownership

No promise of identical observations under arbitrary concurrent filesystem mutation follows from this change: regular paths can change after enumeration, and detector scheduling can change which version is observed. Existing start/end capture and validation checks are retained, not strengthened into an atomic filesystem snapshot; this review does not certify previously known framework side-read closure gaps. Probes must terminate for draining/joining to finish; there is no new cancellation contract for Detect.

Coordinator reported the archived docs probe was renamed from `.go` to `.go.txt` after its full-suite failure; treat that artifact-only correction as resolved, not an optimization defect. The two fixture cache diffs change build identity only, and do not change fact payloads. Full repository tests/race/build/vet and uncontended broker-backed latency/memory/initial-ratio acceptance remain coordinator-owned and are not replaced by this GO.
