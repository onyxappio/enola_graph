# Product efficiency follow-up — 2026-09-21

Two optimizations on top of `492b1e8` remove redundant work without changing
protocol, graph facts, invalidation or checkpoint durability:

- Check EndReplace acknowledgment using the journal metadata already decoded on
  append/reopen, instead of copying and decoding every retained payload.
- Borrow cached immutable TS facts until the aggregation boundary; clone once
  before composition instead of cloning twice. The remaining clone isolates
  mutable output properties and relations from committed file records.

## Repeated Product measurements

Seconds, median (minimum–maximum), three sequential repeats per build. These
are request-driven resident measurements with actual filesystem events, NATS
JetStream and an independent in-memory protocol consumer. They exclude the
production Watch collection window and do not include Memgraph. No builds or
tests ran concurrently with these Product timing runs.

| Scenario | Before | Both optimizations | Interpretation |
| --- | ---: | ---: | --- |
| Resident initial | 10.699 (10.545–10.785) | 10.523 (10.429–11.463) | Overlapping spread; no stable initial speedup established |
| One-file body edit | 0.738 (0.730–0.738) | 0.711 (0.702–0.719) | 3.7% shorter median |
| Structural edit | 0.810 (0.783–0.815) | 0.761 (0.747–0.767) | 6.1% shorter median |
| 100-path history | 5.005 (4.993–5.023), previous accepted run | 5.002 (4.862–5.010) | Effectively unchanged; reference is historical, not a fresh paired run |

The intermediate journal-only build took 0.738 s for body edits and 0.772 s for
structural edits. That isolated change does not establish a material body-edit
speedup. Builds ran in sequential groups, not randomized order, so these small
improvements are local observations rather than a universal latency guarantee.

Producer completion includes acknowledgment/checkpoint work. Body-edit median
time to the first batch is now 0.150 s (before 0.161), broker End receipt 0.636 s
(before 0.645), and in-memory consumer apply/ack 0.638 s (before 0.647).
Structural consumer apply/ack is 0.684 s (before 0.726). The 100-path consumer
applies in 3.668 s; full request completion includes coverage reconciliation.

The full resident process peak RSS medians were 979.7 MiB before and 976.8 MiB
for the final candidate. That is effectively unchanged; the intermediate build
was noisier. These small changes do not solve the overall memory footprint.

## Correctness and evidence

- Final candidate: **75/75** harness checks passed (66 normal/ignored/idle and
  9 history checks). All three initial, body and structural normalized graph
  hashes match their fresh cold oracle and match the prior build exactly.
- One body file and two structural files parsed, as before. The 100-path history
  still parses 414 TS files with exact cold equality. No invalidation was removed.
- Idle and ignored changes still publish nothing, parse nothing and advance no
  generation. The dedicated Product clones were restored without tracked edits.
- Focused extractor/session/transport tests and the race-enabled ExtractSession
  tests passed, including output mutation isolation from cached records.
- Full suite and vet results are retained in `full-tests.log` and `vet.log`.
  The first full-suite attempt rejected the temporary in-repository benchmark
  driver as an undeclared architecture package. It was removed before the final
  run; the initial failure log is retained as `temporary-driver-failure.log`.

`summary.json` contains medians/spreads and delivery timings. Each stage retains
metrics, checks, commands, provenance and resident logs. The source manifest and driver source identify the measured candidate; the
production changes are committed with this report. The before resident
binary is the prior coalesced driver; its extraction/session behavior matches
`492b1e8` (the intervening watch interval change is unused by this request driver).
The base CLI is `492b1e8`; final CLI and driver use both optimizations.

The isolated journal microbenchmark uses 32 acknowledged 256 KiB batches plus
EndReplace. The former check takes a median **24.48 ms and 8.66 MB allocations**;
metadata lookup takes **277 ns and zero allocations**. This isolates one
checkpoint check, not total analysis throughput; it must not be advertised as
an equivalent end-to-end speedup. See `end-proof-benchmark.log`.

## Next larger opportunities

The profile still shows about 51.34 MB serialized for a one-file checkpoint,
and repository-wide fact assembly/indexing for about 69,754 facts. Those are the
next larger targets. [The follow-up design notes](NEXT.md) describe incremental
checkpoint storage and owner/index updates, with their recovery and correctness
constraints. Neither architectural change is implemented in this patch.
