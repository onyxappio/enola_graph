# Independent final Product candidate review

Date: 2026-09-21. Task `task_1529acb0ad69`, dispatch `ctx_2378187614d5`.

**Overall candidate: NOT ACCEPTED yet — Product performance and repaired-oracle acceptance remain unverified here. Final bounded correctness review: PASS, with no outstanding reproduced failure in the reviewed final source.** This review execution is complete; it is not a claim that matched Product performance acceptance has passed.

## Source and isolation

Private snapshot `/tmp/enola-accepted-review/source`; final binary `/tmp/enola-accepted-review/enola-final`. Final production SHA-256 inventory: `/tmp/enola-accepted-review/final-production-sha256.txt`; transport inventory: `transport-final-sha256.txt`; initial analysis inventory: `analysis-sha256.txt`, all under the same review directory. Final production graphsession, graphstream and tsextractor files were byte-compared with the coordinator's stable checkout and match.

Analysis/session/state/fingerprint sources stayed unchanged throughout this wave. The only initially inventoried analysis production change was the coordinator-authorized `httpclient.go` boundary repair, with old copy retained as `httpclient-before-boundary.go` and new hash in `scanner-final-sha256.txt`. Earlier transport source is preserved separately in `transport-stage`; the initial mid-edit dependency did not compile and was used for no transport acceptance. Analysis initially used the earlier frozen recheck transport solely as a dependency, then all final checks used the final transport.

No production checkout edits, commits, Product fixture writes/runs, root binary/result changes, or root broker access. Private brokers used ephemeral ports and were killed and reaped by test cleanup. All reviewer test/build subprocesses completed before settlement.

## Observed final passes

- **Complete final race suites, without exclusions:** `go test -race ./internal/graphstream ./internal/graphsession -count=1 -timeout 240s` PASS, 70.600s / 80.590s. Includes original independent probes, final admission/recovery probes, durability/error/cancellation/Close/restart regressions and real-NATS tests. Evidence: `final-race.log`.
- **Complete final TypeScript suite:** `go test ./internal/extractors/tsextractor -count=1 -timeout 120s` PASS, 13.686s. Final build succeeds. Evidence: `scanner-final-full.log`.
- **Focused independent final transport:** `go test ./internal/graphstream -run 'TestIndependent|TestRootFreeQueue|TestReview' -count=1 -v -timeout 120s` PASS. Evidence: `transport-final-independent.log`.
- **Free-capacity parser admission:** original root `TestRootFreeQueueDoesNotWaitOnJournal` preserved verbatim and passes while journal mutex is held. Candidate deterministic delayed-Sync hook test also passes in the complete suite. Async Publish now performs bounded volatile admission without journal calls; nil is not durable acceptance. The commit worker validates durable identity, appends/fsyncs before network, and Flush waits for completion and propagates errors.
- **Detached committing group:** original `TestReviewDetachedGroupFlushAndBound` preserved verbatim and passes; detached item/byte counts remain charged, Flush does not escape blocked durability, and a full queue cannot admit another job before cancellation.
- **Begin/scope/End fences:** delayed Begin blocks subsequent data; delayed scope blocks resolved writes; delayed earlier batches block End. Those existing assertions pass under race. Reviewed worker code retains those fences while permitting data delivery concurrency.
- **Durable before network and broker acknowledgement:** disk-before-sink probes, failed sink replay, identical broker replay IDs/payloads, Flush errors/cancellation, and Close tests pass. Independent retry of a durable existing unacked identity now actually delivers and acks before successful Flush.
- **Healthy output beyond 64 MiB:** original independent real-broker harness preserved verbatim delivers 83,886,080 payload bytes, 321 broker messages including End, zero unacked records and durable End proof with a 1 MiB cap. Final focused run: 31.47s, sampled post-Flush spool peak 39,791B. This is a transport progression test, not Product timing or a valid 80 MiB graph; the sample is not a peak during compaction. It also passes in the final race suite.
- **Bounded exact identity history:** only the original tiny-cap many-message probe was adapted to the authorized recoverable-failure policy. It reaches 270 accepted identities then returns a cap error, with 32,622B actual disk under the 32,768B cap. After close/reopen, every accepted ID permits identical replay and rejects changed payload. No silent forgetting or truncated identity hashes. Retained tombstones store exact message ID, subject and full SHA-256; completed protocol-run fences explicitly reject retired IDs, including conflicting payloads, rather than treating them as successful duplicate delivery. Historical conflict and retired-run async errors propagate through Flush with no network emission.
- **Committed final newline corruption:** original test preserved verbatim and passes by rejecting `have 61 want 62`; no silently dropped durable unacked row.
- **Every compaction data rename:** independent reclaim stages 0/1/2 and CompactAcked stages 0/1/2/3 pass. Tests preserve durable unacked bytes, require read-only Open for CompactAcked recovery, reject retired conflict, then append a fresh entry and reopen again. Recovery validates each live or temporary file against the next manifest; first mutation installs the remaining files. Exact tests: `internal/graphstream/independent_crash_test.go`.
- **No phantom completed generation after volatile loss:** independent subprocess admits End while an earlier journal Sync is deterministically stalled, exits without Flush, then reopens. Pending generation 2 does not replace completed generation 1 and no acknowledged End is invented. Test passes alone and in final race: `internal/graphsession/independent_volatile_test.go`, `volatile-recovery.log` (stage standalone evidence).
- **End before checkpoint promotion:** final source Flushes batches, fsyncs pending state and its parent directory, publishes End, Flushes, verifies matching acknowledged End, then renames/promotes state with parent fsync before journal compaction. Pending/restart tests pass. Deferred Close errors are ignored, but successful session completion has already passed the explicit Flush/End barriers.

## Full graph multisets and no-ops

All final CLI comparisons use unchanged `/tmp/enola-independent-acceptance/normalize`, the original Consumer binary. Its complete owned node/edge JSON rows include properties, positions, resolution and occurrence data; `compare.py` compares Counters, preserving multiplicity. This is not counts-only comparison and does not use the candidate Consumer.

Final logs/scripts are under `/tmp/enola-accepted-review/final-cli`:

| Original scenario | Final fixture | Result |
| --- | --- | --- |
| Property/heading-only README edit | `/tmp/enola-review-openapi-7elrqhfg` | exact cold equality, 3 rows |
| mdintent missing target-file addition | `/tmp/enola-review-openapi-k4zokahm` | exact cold equality, 5 rows |
| Last README deletion, TypeScript unchanged | `/tmp/enola-review-openapi-su_fz_mf` | exact cold equality, 3 rows |
| README change, stale synthetic provenance | `/tmp/enola-review-openapi-nofijmd1` | exact cold equality, 8 rows |
| Original OpenAPI edit/add | `/tmp/enola-review-openapi-fg6wtaaf` | exact cold equality, 7 rows |
| Original resolved fixture TS/full × plain/HTML | `/tmp/enola-independent-fixture-tn2iz2h3` | all four cold-equal and true no-ops |
| Original unresolved-only TS/full × plain/HTML | `/tmp/enola-independent-fixture-2gvjdfnu` | all four cold-equal and true no-ops |

Each small CLI scenario's unchanged delta has zero events and parses and retains generation 1. Intentional edits publish and advance. The generic comparator's `noop_published:true` includes intentional edit streams; it does not describe the separately verified empty noop stream.

Original graphsession probes were copied verbatim from the prior independent recheck, preserving assertions: overlapping content-sensitive extractors; unknown-owner edit/add/delete; cached synthetic module resolution; ambiguous candidate removal/rename; direct IO contract; actual minified bundle no-op. Property/position/multiplicity fingerprint regressions also pass in the full final suite.

## Findings observed during this review, now fixed

1. **Scanner semantic difference:** `$url:` lost a route accepted by the frozen regex baseline because its prefilter used JS identifier boundaries rather than regexp word boundaries. Coordinator explicitly rejected an exception. Original counterexample, baseline boundary matrix and edge syntax now pass with the two predicate replacements. Prior failure: `scanner-boundary.log`; final pass: `scanner-final.log`. Other prefilters were inspected for necessary-condition alignment; full scanner regression suite passes. Product helper fixture sweep was not rerun here (environment-gated test skips); root owns the Product semantic oracle.
2. **P1 mixed compaction rename recovery:** first payload rename made neither complete live manifest match; durable unacked data became unreadable on restart. Confirmed pre-fix reclaim and CompactAcked images/logs: `compaction-crash.log`, `compact-crash.log`; preserved images include `/tmp/enola-review-crash-4081419068` and `/tmp/enola-review-compact-2415910202`. Final per-file validated live/tmp recovery passes the same assertions.
3. **Existing unacked retry falsely completed Flush:** async identity short-circuit returned success without network/ack. Confirmed pre-fix `async-existing-unacked.log`; exact independent retry test now passes after worker-side distinction between acked and unacked identities.
4. **Known free-capacity mutex stall:** original root assertion independently failed against the staged source and passes final source after all producer journal access was removed. Prior evidence: `transport-original.log`.

All findings were sent promptly to the coordinator; none was silently converted into an accepted exception.

## Remaining acceptance limits

No final matched host/input/scope repeated Product broker-ack measurements, spread, RSS/actual-parse results, or final repaired Product oracle/delta-cold hashes were supplied to this reviewer. Root owns these. Expected repaired oracle hashes remain full `d727bfe7f954069a6b16c636c8ad6bd87b288bf11a5e214c71b88b3b75eae2d2` and TS `a52bd38e666922e1db871e766bd43b5495483c9154fd5b1f84a9a54f13120f3b`; they were not independently confirmed against final Product output in this wave.

Crash checks reconstruct valid rename-boundary images and include a real subprocess exit for volatile loss. They are not exhaustive filesystem/power-loss fault injection. Post-Flush disk samples do not establish maximum temporary-disk use at every syscall. Full repository validation is coordinator-owned; this worker ran complete focused packages and race suites, not `go test ./...`. Unknown-owner input walks under engine-pruned directories remain an existing coverage limit, not a newly verified supported profile.

**Disposition:** correctness GO for the final bounded review and preserved acceptance probes; overall Product candidate NOT ACCEPTED until root's performance and full Product graph-oracle requirements are demonstrated.

Coordinator follow-up received immediately before settlement: root reports frozen candidate2 build, full `go test ./...`, full race/coverage suite and `go vet` PASS, with logs `/tmp/enola-product-candidate2-results`. This is coordinator-reported evidence, not checks independently executed by this reviewer. Root timing window is next; reviewer is quiet with all test/build subprocesses finished and will start no further tests.
