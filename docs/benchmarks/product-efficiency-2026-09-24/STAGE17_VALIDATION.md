# Stage17 fact-buffer pre-sizing: three-pair performance assessment

Experimental candidate based on main c6332bf, not published. It pre-sizes the
TypeScript cached-fact output buffer without skipping eager decode, cloning,
normalization, result facts or snapshot retention. Production session.go SHA256:
`80469ff97b30c583d2dcb21d1684f7702e2e0faee883a7de70c6008eb9457756`.

## Correctness

On clean Product `a609c19f3861971930fae7b33dcb2950598953c5`, all seven normalized
graph states match baseline and cold oracles. Initial/no-op/body/structural parse
counts remain 4034/0/11/12. No-op emits zero events and preserves generation and
state bytes. The body edit is graph-neutral; the structural edit changes facts.
See [independent comparison](stage17-checkpoint/correctness-comparison.json).
Those earlier correctness runs carried competing test load and are not timings.
Root's focused checks also preserve exported no-op facts and resident snapshots.
The final test rename/comment change leaves production and assertions unchanged;
provenance is in [reconciliation](stage17-checkpoint/reconciled-candidate.json).

## Original window: two pairs completed

All four timing arms passed correctness and competitor checks under explicit
accuracy, Codata and worker holds. The original 200-second pair-reserve guard
refused repeat 3 before launch; the driver exited 1 for that deadline guard, not
an Enola failure. No hold was extended. The third pair was subsequently completed in a new confirmed window, as recorded
below, with identical pinned binaries, input and configuration.

| Scenario | Pair1 baseline → candidate, s | Pair2 baseline → candidate, s |
|---|---:|---:|
| Initial | 8.432 → 8.373 | 8.410 → 8.302 |
| Fresh CLI no-op | 1.650 → 1.665 | 1.698 → 1.697 |
| Body delta | 3.449 → 3.432 | 3.452 → 3.460 |
| Structural delta | 3.434 → 3.443 | 3.479 → 3.522 |

Fresh CLI no-op speed has not improved in these two pairs. Its RSS is lower:
292,241,408→273,580,032bytes in pair 1 and 293,797,888→272,318,464 in pair 2.
These are incomplete descriptive results, not a completed performance conclusion.
RSS is distinct from the worker's 500-file allocation diagnostic (12.5% fewer
allocated bytes per no-op), which does not establish wall-time savings.

Timing spans fresh CLI startup through process exit, including producer acknowledgments. Raw first-batch,
broker/consumer boundaries, RSS, parse counts and wire data are retained in the
per-arm metrics. Original order was baseline/candidate, then candidate/baseline.
Codata disclosed background ClickHouse activity; per-arm container samples remain
in the local run archive. Absence of sampled competitors does not prove an idle
host. No watch performance claim follows from these CLI runs.

See [partial audit](stage17-checkpoint/partial-timing-audit.json), per-arm checks
and receipts, timing-window acknowledgments and the SHA256 manifest. The strict
summarizer rejects incomplete cohorts and correctness-only receipts; its validation
reproduced the existing Stage16 full series. Production integration remains deferred. Near-zero cold no-op is not achieved.


## Completed third pair and conclusion

Repeat 3 ran baseline then candidate in a separately confirmed 22:53–22:57 UTC
window on September 24, with the original 200-second pair-reserve guard. Both
arms passed every correctness gate, sampled no competing workloads and exited 0.
The runner exited 0 and END released all holds at 22:55 UTC. All 21 cross-arm
graph comparisons across three pairs match. Earlier deadline refusal remains
recorded; it was not overridden or retroactively reclassified.

| Scenario | Baseline median [min, max], s | Candidate median [min, max], s | Median change |
|---|---:|---:|---:|
| Initial | 8.410 [8.252, 8.432] | 8.338 [8.302, 8.373] | -0.86% |
| Fresh CLI no-op | 1.650 [1.649, 1.698] | 1.665 [1.645, 1.697] | +0.86% |
| Body delta | 3.449 [3.424, 3.452] | 3.432 [3.418, 3.460] | -0.49% |
| Structural delta | 3.456 [3.434, 3.479] | 3.451 [3.443, 3.522] | -0.15% |

All wall-time ranges overlap. Three descriptive pairs do not establish a latency
improvement or statistical significance. Candidate deltas still cost about 41%
of its own initial run; no-op remains about 1.66 seconds. This does not meet the
requested fast-startup goal. No further repetition is planned solely to seek a
favorable timing result for this frozen candidate.

No-op peak RSS median decreases from 293,797,888 to 272,318,464 bytes (-7.31%),
with nonoverlapping ranges: baseline [292,241,408, 297,435,136], candidate
[272,203,776, 273,580,032]. Initial RSS median rises from 911,278,080 to
918,831,104 bytes; the candidate maximum is 970,817,536 bytes. Body and structural
RSS ranges overlap. Thus the observed memory benefit is specifically no-op, not
a demonstrated universal memory reduction. It is distinct from total allocation.

See [full summary](stage17-final/timing-comparison.json),
[cross-arm graph checks](stage17-final/three-pair-graph-equality.json),
[third-pair receipt](stage17-final/repeat3-series.json), adjacent raw metrics,
participant acknowledgments and container samples. The implementation remains
unmerged; the next latency work focuses on repeated inventory and state costs.
