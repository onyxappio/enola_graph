# Product phase profile after Markdown scope narrowing

The instrumented run used the exact frozen source of main `5627150` with
`ENOLA_GRAPH_PROFILE=1`. It passed cold graph equality and stable 45110-file
input fences. It is separate from the uninstrumented timing series.

The five-file membership delta published 18 owners, 34 batches and 798604 bytes.
Its session trace ended at 2.448 s, excluding collection delay and work outside
that trace. The observed last-save-to-consumer interval was 7.814 s including
the 5-second collection window. These are different timing boundaries.

## Delta phase attribution

| Sequential session trace phase | Seconds |
|---|---:|
| Input preparation through extractor_need | 0.830 |
| Planning / preview through ts_dirty_scope | 0.540 |
| Prepared TS extraction handoff | 0.000 |
| Assemble new file state | 0.233 |
| Index and group owners, resolution checks | 0.210 |
| Publish resolved owners | 0.056 |
| Revalidate TS records | 0.135 |
| Remaining validation and write pending state | 0.339 |
| End, flush and promote | 0.104 |

Rounded deltas sum to 2.447 s. Nested timers must not be added to that sum:
input inventory/detection/hashing/fingerprinting took 0.693 s inside preparation;
Markdown preview took 0.168 s; JSON marshaling took 0.130 s inside the state-write
interval. The pending state contained 50965544 JSON bytes for 5192 file records.
The run still hashed 5211 inputs and revalidated 4246 TS records.

A startup reconciliation with unchanged inputs took 1.073 s and published no
replacement. That is a strict bootstrap scan, not the existing covered-event
idle shortcut, and must not be represented as normal idle polling cost. Watch
registers its source after OpenSession and reconciles the resulting coverage gap.

## Next investigation

Publication is now a small part of this delta. The next bounded candidate is
avoiding redundant resolution-index construction: the session builds an index,
then resolutionIndexes builds two more; Resident passes priorResolution but the
current session does not consume it. Reuse is only valid if index semantics
match, including stable published overlays, relation-only facts, retired owners
and failed-run rollback. A read-only worker is reviewing those invariants before
implementation. The profile does not yet establish how much reuse will save.

Checkpoint serialization and input preparation remain further candidates. Do not
remove captured-source fences, trust timestamps alone, or equate an empty event
queue with strict disk equality to obtain a lower time.

[Raw profile trace](mdintent-profile-trace.txt),
[run and cold verification](mdintent-profile-result.json),
[uninstrumented comparison](MDINTENT_COMPARISON.md).
