# Delta setup optimization — bounded implementation handoff

2026-09-21; task task_9d09b3f56df1, dispatch ctx_2a5988f9a181. Main uncommitted checkout edited in place. No commit, push, Telegram, protocol change, Product coordinator-copy mutation, or frozen-source/results-directory write.

## Changes

1. Added per-detector `ENOLA_GRAPH_PROFILE` timings to Engine.DetectExtractor. Initial private-copy profile identified AsyncAPI at 1.338s, grpc 0.171s, OpenAPI 0.174s, all detection 1.830s. Warm serial AsyncAPI was materially cheaper (0.561–0.614s), so the initial cold number must not be treated as the before/after baseline.
2. Added explicit Engine.RegisterIndependentExtractor and bounded Engine.DetectExtractors. Bootstrap opts its built-ins into four-way concurrent detection after reviewing their detection functions as read-only. Existing Detect/DetectFiles implementations and their scope are retained. Ordinary RegisterExtractor registrations remain serial barriers: earlier independent calls finish before an opaque/custom call begins, and later independent calls cannot cross it. Disabled and failed detectors retain previous session behavior. No privilege is granted based on extractor name or FileListDetector implementation. This path is used by graphsession; legacy snapshot detection remains otherwise unchanged.
3. AsyncAPI candidate content probes now use four workers and at most 32 outstanding candidates. The original WalkDir, skipDir, absolute candidate paths, arbitrary JSON selection, 4096-byte prefilter, and full YAML root validation remain. Pending probes flush before every directory, error, symlink/special file, and walk completion, preserving original pruning/error behavior, including its unusual continued traversal after finding a match. Only regular files in an uninterrupted file sequence may be read speculatively; symlinks/special files are never speculatively opened. No state survives a Detect call.
4. Added SessionHooks.SkipConfigPaths for callers already discovering/revalidating config inputs independently. Graphsession sets it for its TS extraction and invalidation rounds, eliminating unused informational SessionResult.ConfigPaths walks. Default ExtractSession callers retain original ConfigPaths discovery. The original independent ConfigInputPaths walk runs at fingerprint capture and again at successful changed-run revalidation; it is not replaced by engine AllNames. No-change retains its existing one discovery and early return. No persistent filesystem cache, content-hash shortcut, or revalidation weakening was added.

Graph facts, local IO semantics, invalidation scope, cache format/version (v273), journals, NATS acknowledgments and backpressure are unchanged. These are scheduling and redundant-result-inventory optimizations; no stored-fact semantic change requiring cache invalidation was introduced.

## Private-copy profiling evidence — experimental, not acceptance

Used cloned `/tmp/enola-delta-setup-product`; only this private copy received comment edits. Binaries `/tmp/enola-delta-setup-before` (prior production plus per-detector timing) and `/tmp/enola-delta-setup-after` (new changes). Toolchain `/tmp/enola-toolchain/go/bin/go`. Results use the JSONL debug sink, not NATS, and profile tracing; no broker performance acceptance or initial/delta ratio is claimed.

Three alternating warm no-change measurements per build:

| Phase | Before seconds (three runs) | After seconds (three runs) |
|---|---|---|
| All detectors | 1.094, 1.042, 1.039 | 0.514, 0.508, 0.501 |
| AsyncAPI detector | 0.614, 0.562, 0.561 | 0.514, 0.508, 0.501 |
| Profile total through no-op return | 2.898, 2.735, 2.746 | 2.240, 2.211, 2.210 |

All six noops reported zero parsed files, zero published owners, empty RunID and generation 1→1. The modest AsyncAPI improvement is distinct from the larger benefit of detector overlap. There is no claim that candidate parallelism alone halves AsyncAPI cost.

Three alternating single-file comment edits to `packages/tracking-server/src/publisher.ts`, restored afterward:

| Metric | Before seconds | After seconds |
|---|---|---|
| Profile total through promotion | 5.211, 4.470, 4.411 | 3.839, 3.813, 4.187 |
| End-to-end subprocess wall | 5.470, 4.744, 4.669 | 4.940, 4.073, 4.449 |
| Detectors | 1.604, 1.037, 1.037 | 0.554, 0.538, 0.657 |

Every changed run parsed exactly one file and published 88 owners. TS config discovery count fell from three to two for this single-round edit, saving approximately 0.165s; multi-round deltas save one discovery per extraction round. The first before changed run and first after subprocess wall contain extra noise (vet briefly overlapped the beginning of this exploratory loop); use ranges and phase evidence, not a precise claimed wall-time gain. This private initial used JSONL and took 35.817s to profile completion, so it is unsuitable for comparison with the coordinator's ~11.5s broker initial. Root owns the uncontended same-build initial/delta ratios, repeated NATS acknowledgment timings and memory measurements.

Raw evidence: `/tmp/enola-delta-setup-before.log`, `/tmp/enola-delta-setup-{before,after}-noop-{0,1,2}.{log,json}`, `/tmp/enola-delta-setup-{before,after}-body-{0,1,2}.{log,json}`, `/tmp/enola-delta-setup-body-timings.json`.

## Tests and checks

Passed with the toolchain above:

- Full affected suites: `go test ./internal/engine ./internal/extractors/asyncapiextractor ./internal/extractors/tsextractor ./internal/graphsession ./internal/cachecov ./pkg/bootstrap`; `/tmp/enola-delta-setup-tests.log`. Graphsession includes retained cold/delta, config/consumed-source, no-op, replay and incremental regressions. This completed before the final conservative special-file probe barrier; that last change then passed the focused race suite and build below.
- Focused new and retained correctness checks; `/tmp/enola-delta-setup-focused.log`.
- Race checks for new detection/concurrency/config tests, original detector scopes, captured-config restore, ignored config discovery and concurrent inventory; `/tmp/enola-delta-setup-race.log`.
- Final AsyncAPI and engine race checks including positive-candidate/symlink barrier: `go test -race ./internal/extractors/asyncapiextractor ./internal/engine -run 'TestParallel|TestPositiveCandidate|TestIndependentDetection|TestDetectionHonors|TestDetectorOriginalScope' -count=1`; `/tmp/enola-delta-setup-final-race.log`.
- `go build -o /tmp/enola-delta-setup-after ./cmd/enola` and `go vet ./internal/engine ./internal/extractors/asyncapiextractor ./internal/extractors/tsextractor ./internal/graphsession ./pkg/bootstrap`; vet log `/tmp/enola-delta-setup-vet.log` (empty, success).
- Config-result equivalence test repeated ten times after sorting unordered facts to remove a test-only ordering assumption; `/tmp/enola-delta-setup-config-repeat.log`.
- `git diff --check`.

New regression coverage proves independent calls overlap, custom barriers preserve ordering, disabled/error behavior persists, detector original scopes still match, AsyncAPI retains arbitrary/hidden/deep JSON/YAML and rejects nested/malformed root markers, probes stay bounded to four workers and rediscover edits/deletions, a positive file prevents later speculative symlink reads, omitted informational config paths do not alter facts/records, and newly added engine-pruned configs during extraction reject state promotion.

Guard verification used Go overlays only: disabling independent detection, reducing AsyncAPI to one worker, and ignoring SkipConfigPaths each caused its corresponding new regression test to fail. `/tmp/enola-delta-setup-guard-failures.log`; intentionally failing mutant output, not an unresolved production failure. Initial test wiring mistakes (custom test names not enabled by default config, unsorted fact order) were corrected without relaxing semantic assertions.

## Remaining costs and limitations

Warm no-op still spends roughly 0.5s loading state, 0.4s walking inventory, 0.5s detecting (dominated by actual AsyncAPI content reads), 0.5s hashing/context capture, 0.165s independent config discovery, plus assembly/digest work. Changed runs retain TS composition and correctness revalidation, state encoding and delivery. These changes cannot alone establish the desired many-times-faster delta: expected savings are roughly half a second of detection plus 0.165s per avoided TS-result inventory under this private profile.

Concurrency preserves stable-input detection results and original walker/error scope; it cannot promise identical observation schedules for an arbitrarily mutating filesystem. Up to 32 regular candidate reads may be speculative after a match within a file sequence. The existing framework side-read limitations described by the prior correctness review remain; this pass does not claim comprehensive new input closure. Four detector calls may overlap, with AsyncAPI itself using four content workers; no unbounded worker creation or persistent stale cache exists.

Coordinator still owns final frozen-source review, full-repository checks and repeated broker-backed acceptance. No final speed acceptance is claimed.

## Files changed by this task

- internal/engine/engine.go
- internal/engine/detector_scope_test.go
- internal/engine/detection_parallel_test.go (new)
- internal/extractors/asyncapiextractor/asyncapi.go
- internal/extractors/asyncapiextractor/detection_parallel_test.go (new)
- internal/extractors/tsextractor/session.go
- internal/extractors/tsextractor/session_config_inventory_test.go (new)
- internal/graphsession/session.go
- internal/graphsession/config_rediscovery_test.go (new)
- pkg/bootstrap/bootstrap.go
