# Transport latency investigation and bounded coalescing — 2026-09-21

Status: candidate implemented, graphstream checks passed, and isolated paired Product profiles confirm the scheduling regression and repair. Full acceptance (consumer graph equivalence and repeated legacy-baseline comparison) remains with the coordinator. Production edits confined to `internal/graphstream`; no Product, extraction, scope, journal cap, harness, docs, commit, or push changes by this worker.

## Established mechanism (bounded synthetic workload)

Compare prior `/tmp/enola-final-stage-source/internal/graphstream` against b1820aa transport and candidate, using identical preencoded 1,000 resolved envelopes (24,957,893 total bytes), 32-message / 8 MiB queue, eight delivery workers, real filesystem journal and discard sink. The default 64 MiB journal cap is unchanged. This deliberately measures journal scheduling independently of extraction and NATS; these are engineering microbenchmarks, not Product acceptance. Runs were sequential, but other workers were permitted tests during early synthetic measurements.

With identical opt-in diagnostics on all variants, three runs each:

| Variant | Completion seconds | Commit-pump Sync calls | Mean messages/group | Sync seconds (sum) | Completed queue-wait seconds (sum) |
|---|---:|---:|---:|---:|---:|
| Prior transport | 1.526–1.618 | 63 | 15.87 | 1.080–1.090 | 1.289–1.328 |
| b1820aa transport | 1.500–1.653 | 73–83 | 12.05–13.70 | 1.341–1.494 | 1.080–1.252 |
| Candidate, target 16 / maximum 3 ms gather | 1.207–1.304 | 62–63 | 15.87–16.13 | 1.047–1.137 | 0.856–0.881 |

Baseline current has 3–4 singleton groups/run; prior and final candidate have zero. Faster metadata/gates alter producer/consumer scheduling enough to shrink journal groups; extra sync work offsets some CPU reduction in this workload. The subsequent paired Product diagnostics below confirm the same mechanism at application scale.

Earlier uninstrumented runs: prior 1.556–1.625 seconds / 63 groups, current 1.549–1.938 seconds / 78–96 groups. A rejected 1 ms trial that bypassed gathering whenever the whole queue was full did not reliably improve batching (77–85 groups; 1.497–1.659 seconds). Allowing already-ready delivery to free queue slots while gathering pending work matters; no queue bounds are increased.

Raw outputs: `/tmp/enola-journal-old-diagnostic.txt`, `/tmp/enola-journal-current-diagnostic.txt`, `/tmp/enola-journal-coalesced3ms.txt`; earlier `/tmp/enola-journal-old.txt`, `/tmp/enola-journal-current.txt`, `/tmp/enola-journal-coalesced.txt`.


## Isolated Product paired profiles (completed)

Coordinator authorized an exclusive slot after native worker completion and supplied the exact producer-only command. No tests/builds or other workers ran timings during the slot. Runs were baseline/candidate/candidate/baseline, each with a fresh owned NATS JetStream broker on port 14361, fresh state, the same `/tmp/enola-postpush-isolated-cli/config.yaml`, and Product tracked revision `a609c19f3861971930fae7b33dcb2950598953c5`. Product pre/post status was unchanged (`?? .enola/`, `?? mcp-arch.yaml`, both preexisting); no source changes. Brokers were terminated and reaped after each run.

Command shape: `ENOLA_GRAPH_PROFILE=1 ENOLA_TRANSPORT_DIAGNOSTICS=1 /usr/bin/time -l BINARY graph analyze --summary-json --nats nats://127.0.0.1:14361 --state-dir UNIQUE/state --context UNIQUE /tmp/enola-postpush-isolated-cli/config.yaml`. Raw commands, result JSON, profiles, and broker logs are under `/tmp/enola-transport-product-pairs/{1-baseline,2-coalesced,3-coalesced,4-baseline}/`.

| Metric | Baseline run 1 | Candidate run 2 | Candidate run 3 | Baseline run 4 |
|---|---:|---:|---:|---:|
| Process real seconds | 16.35 | 9.82 | 8.96 | 15.35 |
| Session through End/Flush/promote seconds | 14.603 | 8.130 | 8.064 | 14.474 |
| TS mapfiles aggregate seconds | 5.319 | 3.176 | 3.216 | 5.276 |
| Publish resolved seconds | 6.719 | 2.441 | 2.337 | 6.667 |
| Commit-pump Sync calls | 628 | 271 | 270 | 629 |
| Average messages/group | 8.30 | 19.23 | 19.30 | 8.28 |
| Groups of exactly eight | 578 | 1 | 1 | 580 |
| Sum Sync seconds | 11.091 | 4.571 | 4.517 | 10.972 |
| Maximum Sync milliseconds | 27.3 | 25.4 | 57.5 | 27.5 |
| Completed queue-wait count | 5305 | 2561 | 2559 | 5328 |
| Sum completed queue-wait seconds | 41.146 | 13.250 | 13.737 | 40.560 |
| User CPU seconds | 21.71 | 21.80 | 21.83 | 21.56 |
| System CPU seconds | 2.61 | 2.07 | 2.07 | 2.50 |
| Maximum RSS bytes | 909508608 | 945307648 | 976863232 | 964706304 |
| Voluntary context switches | 10104 | 5105 | 4907 | 10050 |
| Involuntary context switches | 62258 | 59962 | 60839 | 58103 |

Every run parsed 4,144 files, published 5,510 owners, and emitted 5,211 messages. Raw payload bytes are baseline 148,714,006–148,714,126 versus candidate 148,719,298–148,719,308; context/run identities differ across isolated runs (candidate context name is one character longer), so raw bytes must not be used as a cross-run equality oracle. There is no serialization change in this patch.

This confirms the mechanism: b1820aa's faster transport drains delivery in eight-worker bursts, then immediately detaches small pending groups; allowing bounded gathering largely eliminates these eight-message commits. Total Sync time drops ~59%, voluntary context switches roughly halve, and initial wall time drops ~41% by pair medians (15.85 to 9.39 seconds). Summed queue waits exceed wall time because independent concurrent producers can stall together. CPU user time stays essentially unchanged, consistent with a durability/scheduling repair rather than less extraction.

This producer-only diagnostic has no consumer observer, cold graph equality assertion, or time-to-first-batch measurement. It is not full performance acceptance. No old Enola binary was rerun in this slot; the supplied prior old baseline range of 9.845–10.335 seconds is context only, and candidate 8.96–9.82 is encouraging but does not replace the coordinator's repeated same-harness comparison. RSS spread overlaps and does not establish a memory improvement. No new single-file delta/no-change measurements were made; Begin and Flush bypass coalescing, and no-change performs no admission, but fresh CLI/resident performance must still be measured separately.

## Candidate behavior and invariants

`async.go` gathers pending work until 16 messages, its existing pending item/byte bound, or a fixed 3 ms deadline. Ready/in-flight delivery can proceed during the gather; all admission bounds still count pending + committing + ready as before. Begin, scope, and End entries bypass gathering, as do active Flush waiters, failure, and shutdown. Sparse producers have a fixed deadline; Flush wakes the gather and returns only through existing completion/error semantics. Non-journal publishers do not gather.

All payload bytes, sequence identity, journal representation, checksums, fsync-before-ready transition, replay, broker acknowledgments, and predecessor fences remain unchanged. No weakening of durability or larger spool caps. The added wait can delay a sparse ordinary batch by up to 3 ms of configured gather time, plus ordinary scheduling; it does not apply to Begin or active Flush.

`ENOLA_TRANSPORT_DIAGNOSTICS=1` opts into `graphstream_transport {JSON}` stderr output once at CloseAsync. Fields include groups, messages, raw bytes, group-size histogram, total/max Sync nanoseconds, queue stall count/time. Sync measures the entire commit-pump `Journal.Sync()` call (including commit-index durability), not just an individual `fsync` syscall; final acknowledgment-only Flush/Close syncs are excluded. Groups count attempted Sync calls, including a failed call. Queue counters cover waits that return on a space notification, not canceled waits. No clock reads or reporting occur on the ordinary path when disabled. Counters/maps are bounded by queue group sizes, not total history.

## Validation

- `PATH=/tmp/enola-toolchain/bin:/tmp/enola-toolchain/go/bin:$PATH go test ./internal/graphstream -count=1`: PASS, 15.630 seconds, `/tmp/enola-latency-repair-tests.txt`. Installed nats-server included in PATH.
- `go test -race ./internal/graphstream -run 'TestCommitCoalescing|TestAsync|TestReviewDetachedGroup' -count=1`: PASS, 6.836 seconds, `/tmp/enola-latency-repair-race.txt`.
- New regressions cover bypass for Begin/scope/End/Flush, progress for a sparse producer without Flush, and Flush surfacing a commit-index durability failure without delivering to the sink.
- Existing suite covers journal-before-send, crash/replay, spool/item/byte bounds, slow sync, cancellation, broker failure, identity conflict, Begin/scope/End barriers, multi-epoch use, and durable acknowledgment fences.
- Initial new test wrongly assumed a failed payload commit implied zero total Journal.SyncCount; journal initialization can independently sync. Corrected the test to assert the actual contract: Flush returns the durability error and no sink delivery occurs.

## Isolated diagnostic binaries

Both built from the same current repository extraction code, using an overlay only for baseline async transport. Baseline contains the same diagnostics; candidate additionally invokes bounded gathering. The extra Flush wake bookkeeping in baseline has no coalescer to wake.

- `/tmp/enola-transport-baseline-diag`: SHA256 `a9f0881f70571245a9097c8185c9b947fde847fef80e7fc664e0cc1441b42441`.
- `/tmp/enola-transport-coalesced-diag`: SHA256 `c392ab95a7ccffcb2d1e07868dee7da3dc425298c12fd6168e9fc3f2acd2c6fc`.
- Overlay: `/tmp/enola-transport-baseline-overlay.json`; diagnostic-only package copies `/tmp/enola-transport-latency-old` and `/tmp/enola-transport-latency-current`.

Modified `internal/graphstream/async.go`; added `internal/graphstream/transport_diagnostics.go`, `internal/graphstream/transport_coalescing_test.go`, `internal/graphstream/transport_journal_bench_test.go`.

## Handoff and freeze

Coordinator requested production freeze after inspecting the paired logs. All owned test/build/profile processes finished and all four owned brokers were reaped; no process remains intentionally running. No further scope expansion or tuning performed. Root owns independent coalescing/Flush/backpressure review, a frozen integration build, and the six full acceptance cases with three repeats, repaired resident harness and histories, consumer equality, and legacy comparison. Candidate production is ready for that review; this report does not claim final acceptance.
