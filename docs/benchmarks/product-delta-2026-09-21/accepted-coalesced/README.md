# Verified Product initial and delta measurements

Date: 2026-09-21. Candidate: b1820aa plus reviewed bounded commit coalescing,
identified by [the frozen source manifest](../coalesced-source-manifest.json).
All six scenarios completed sequentially with three repeats, no concurrent
build/test workers, NATS JetStream and an independent in-memory protocol consumer.
All **114 assertions passed**, including cold graph equality, no-change zero work,
real watcher events, ignored lock/media events, native quiet and history transitions.

## Comparison

Seconds, median (minimum–maximum). Resident means an already-running session;
its initial includes watcher setup and coverage reconciliation.

| Scenario | Old fresh CLI | New fresh CLI | New resident | New TS parses |
| --- | ---: | ---: | ---: | ---: |
| Initial at target | 10.189 (9.877–10.192) | 9.119 (9.071–9.806) | 10.889 (10.762–11.474) | 4144 |
| No change | 4.529 (4.523–4.576) | 2.078 (2.057–2.082) | 0.000136 (0.000123–0.000158) | 0 |
| Body edit | 10.705 (10.644–10.825) | 2.927 (2.916–2.955) | 0.739 (0.724–0.748) | 1 |
| Structural edit | 11.476 (11.408–11.658) | 3.039 (3.002–3.086) | 0.798 (0.795–0.826) | 2 |
| 27-path history delta | 11.557 (11.499–11.672) | 4.374 (4.307–4.398) | 4.724 (4.708–4.804) | 346 |
| 100-path history delta | 11.566 (11.470–11.572) | 4.635 (4.598–4.635) | 5.005 (4.993–5.023) | 414 |

New initial is 10.5% shorter than old initial by median, and faster in all three
paired repeats. Resident body/structural edits are 14.7×/13.6× faster than their
own resident initial. Fresh CLI body/structural are only 3.12×/3.00× faster than
fresh CLI initial: process startup/state loading/reconciliation still cost time.
Near-zero no-change latency is established for the resident API, not fresh CLI.
Thirty resident idle requests caused zero work, messages and generation changes.

For history baselines, initial at the 27-path base took old 10.239 s / new 8.973 s;
at the 100-path base old 10.202 s / new 9.104 s (medians). Pinned revisions and
reproduction boundaries are in [the harness contract](../HARNESS.md).
The 100-path delta performs 16 added-source parses, 34 changed-source parses and
364 resolution-induced parses; no global TS configuration fallback. Membership
changes require policy/watch coverage reconciliation. Resident history times
include all apply/catch-up work but exclude Git checkout itself; checkout timing
is separately retained in raw metrics. These broad deltas remain materially more
expensive than single-file edits and are not described as near-online latency.

## Streaming completion and resources

| Operation | First batch | Broker End receipt | Consumer applied/acked | Producer/request complete |
| --- | ---: | ---: | ---: | ---: |
| Initial fresh CLI | 1.848 | 8.874 | 8.984 | 9.119 |
| Body fresh CLI | 2.119 | 2.828 | 2.830 | 2.927 |
| Body resident | 0.169 | 0.650 | 0.652 | 0.739 |
| 27 paths fresh CLI | 2.324 | 4.259 | 4.282 | 4.374 |
| 100 paths fresh CLI | 2.366 | 4.490 | 4.515 | 4.635 |
| 100 paths resident | 1.503 | 3.615 | 3.640 | 5.005 |

Producer completion waits for broker acknowledgments and local checkpoint
promotion; a broker receipt timestamp alone does not prove producer acknowledgment.
The consumer is a protocol validator and in-memory graph, not a Memgraph writer.
Its timestamp excludes artifact serialization. Production Watch debounce is not
included in the request-driven resident harness.

Initial fresh-process peak RSS median is 868.3 MiB versus old 555.5 MiB; resident
lifetime peak is 986.2 MiB. These are OS RSS observations, not retained Go heap.
The completed state is about 51.34 MB. Speed acceptance does not imply reduced
memory. Full min/max resources, event bytes/counts, work counters, hashes and
per-run transport timestamps are retained in metrics.json and summary.json.

## Input comparability and evidence

All **24 legacy receipts** were independently rechecked: exact path→hash maps
from the actual old main inventory equal the selected candidate physical inputs,
with no duplicate legacy paths. The unchanged old binary uses a physically
filtered mirror. Its input discovery does not pay the candidate's Git/policy walk;
legacy extraction/propagation/output semantics differ from this fork's direct
streamed graph, including selected hidden manifests. This is same selected file
bytes, not a claim of identical consumed work or output semantics. See HARNESS.md.

The archive deduplicates five canonical input hash maps under input-manifests/.
legacy-inventory-checks.json maps all 24 original receipts to those maps and records
original receipt SHA-256. File mtimes and temporary mirror locations are omitted
from these normalized maps; both original path/hash maps were checked before
normalization. Raw logs, result summaries, assertions, commands and provenance
for all six scenarios are in their respective subdirectories. Source and binary
provenance, full tests, vet and independent review are archived alongside this folder.

The transport fix gathers data for up to a configured 3 ms, targeting 16 pending
messages within unchanged queue limits. Begin/scope/End, Flush and shutdown bypass
gathering. It preserves journal-before-delivery durability and acknowledgment
barriers. [Diagnostic paired profiles](../transport-latency-repair.md) establish
why smaller commit groups caused the earlier regression. The native quiet harness
repair isolates config from its own report writes without changing watcher semantics.
