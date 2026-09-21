# Rejected isolated performance stage

This candidate passed correctness review but **fails performance acceptance**.
The requested three-repeat CLI suite was stopped after the first complete repeat
and a second initial. These observations are not three-run medians.

| Operation | Old, first repeat (s) | Candidate, first repeat (s) |
| --- | ---: | ---: |
| Initial | 9.906 | 15.270 |
| No change | 4.612 | 5.982 |
| Body edit | 10.650 | 7.634 |
| Structural edit | 11.692 | 8.067 |

Second initial also completed: old 10.132 s, candidate 14.430 s. Second old
no-change completed in 4.551 s; no paired candidate no-change was recorded.
All six available compressed legacy inventory receipts (`*-inventory-evidence.json.gz`) confirm actual old main inputs
match candidate selection. The legacy binary uses a physically filtered mirror;
Git/policy discovery costs and graph semantics still differ as described in
[the harness contract](../HARNESS.md). Earlier stage timings used different
scope/comparison preparation and are not a controlled version regression test.

The independent follow-up profile measured initial 14.55 s and unchanged CLI
6.00 s. Noop performs zero parses/publications but loads 51.34 MB of state
(0.428 s), spends 3.420 s in session preparation/revalidation, and has roughly
2.15 s outside those measured phases. Of the 1.117 s hash-input phase, actual
file hashing is only 0.178 s. TS configuration scans recur at about 0.410 s.
Initial resolved publication costs 2.793 s; that phase combines graph encoding,
queueing and acknowledgment waiting, so it is not a pure broker measurement.
Phase traces have nested/cumulative timers: do not sum every reported row.

Optimization is ongoing. No accepted latency result is claimed for this stage.
