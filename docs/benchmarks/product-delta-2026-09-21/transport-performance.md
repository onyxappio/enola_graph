# Transport publication optimization — 2026-09-21

Scope completed: `internal/graphstream` only. No session preparation, graphinput, Product source, benchmark harness, or repository documentation edits; no commits. Existing uncommitted work preserved. Coordinator requested a bounded freeze for the user-authorized integration push; Product performance acceptance remains open.

## Findings and changes

1. Async acknowledgment gates previously scanned sequence 1 through N for every job while holding the publisher mutex. They also retained two maps proportional to total published history. Replaced these with one map of unfinished jobs; a completed acknowledgment deletes its entry. Gates inspect at most queue capacity plus in-flight jobs, preserving Begin-before-data, Scope-before-resolved, and all-predecessors-before-End semantics. Also fixes reuse across epochs: the previous End cleanup removed the acknowledgments a subsequent End was still checking.
2. Each normal async payload was fully JSON-decoded three times for metadata: classify type/phase on admission, probe End run ID at journal append, then probe envelope type again for non-End batches. Admission now decodes type/phase/run once before taking the queue mutex and shares the small result with the private journal append path. Normal synchronous/reopened journal entries also need only one metadata decode. Invalid/non-protocol inputs retain the previous independent-probe semantics through a conservative fallback.

Payload encoding, batch packing, sequence identity, original payload bytes, journal storage format, checksums, journal-before-send fsync, replay, queue/item limits, acknowledgment handling, and completion barriers are unchanged. Async enqueue still copies caller bytes before inspection. No journal limit increases.

## Evidence

Read frozen Product `/tmp/enola-final-profile/initial.log`: `publish_resolved=2.793s`, whole process 14.55s. Coordinator supplied 5,211 messages / 148,636,036 bytes for the failed acceptance run. No assumption that the entire publication phase is broker time.

Focused synthetic measurements on Apple M1 Pro, Go toolchain `/tmp/enola-toolchain/go/bin/go`:

| Measurement | Before, three runs | Final, three runs |
|---|---:|---:|
| Metadata processing of a 24,957-byte 64-node/64-edge batch | 413.861–416.413 µs/op; 912 B/op; 26 allocs/op | 134.077–139.923 µs/op; 336 B/op; 10 allocs/op |
| Async 4,000-message discard-sink run, 32 queued / 8 workers, one iteration | 63.284–63.811 ms | 11.181–16.885 ms |
| Async 1,000-message discard-sink run, one iteration | 6.802–9.338 ms | 4.937–8.219 ms |

The metadata benchmark measures the actual old three probes versus the new shared single probe, without journal/broker I/O. The async benchmark isolates admission/delivery/gates with no journal and no network. Its final longer 200ms benchmark reports 10.848–10.917 ms per 4,000-message run. Allocation count in this no-journal synthetic case rises from approximately 61k to 68k per 4,000 messages because the common metadata decode now also reads run identity; the real journal path eliminates two decodes instead.

Initial CPU/allocation profile: `/tmp/enola-transport-before.cpu`, `/tmp/enola-transport-before.mem`, binary `/tmp/enola-transport-before.test`. Short CPU sampling was scheduler-dominated (pthread_cond_signal 57%, usleep 21%) and is insufficient to attribute Product's phase precisely. Allocation samples showed enqueue/classification/context churn. Baseline one-iteration benchmark collected that profile whereas final benchmark did not; do not treat the speed ratio as controlled Product acceptance. Other workers were allowed focused builds/tests, so these are engineering microbenchmarks, not isolated end-to-end acceptance timings.

Raw measurements: `/tmp/enola-transport-before.txt`, `/tmp/enola-metadata-before.txt`, `/tmp/enola-transport-after.txt` (gates-only intermediate), `/tmp/enola-transport-final-bench.txt`, `/tmp/enola-transport-final-1x.txt`.

## Validation

- `go test ./internal/graphstream -count=1` passed, final full-suite run 16.714s. Includes real NATS test support via installed `/tmp/enola-toolchain/bin/nats-server`, journal crash/replay, bounded backpressure, conflicts, and acknowledgment gates. Log `/tmp/enola-transport-tests.txt`.
- `go test -race ./internal/graphstream -run 'TestEnvelopeMetadata|TestOutstandingGates|TestAsyncMultipleEpochs|TestAsyncDelayed|TestAsyncFlushSeesAckedEnd' -count=1` passed, 4.149s. Log `/tmp/enola-transport-race.txt`.
- New metadata regression compares shared decoding with independent legacy probes, including duplicate keys, nested fake envelopes, invalid phase/run types, malformed JSON, arbitrary bytes, and all normal phases.
- New gate regression checks every acknowledgment subset across ten jobs/two epochs (1,024 subsets), including out-of-order acknowledgments and future jobs.
- New public-publisher regression completes three epochs through the same publisher, asserts all nine messages delivered, and verifies no completed acknowledgment history remains.
- Existing delayed Begin, delayed Scope, delayed earlier batch/End, and durable End proof tests pass, including race execution.

## Files and remaining work

Modified `internal/graphstream/async.go`, `internal/graphstream/journal.go`; added `internal/graphstream/transport_gates_test.go`, `internal/graphstream/transport_performance_test.go`.

Root owns full repository validation and push. No new Product run, full cold/delta graph equivalence benchmark, absolute Product publication timing, memory measurement, or old-Enola comparison was attempted; those require root's isolated post-push benchmark slot. These bounded changes are ready for integration but do not establish Product performance acceptance.
