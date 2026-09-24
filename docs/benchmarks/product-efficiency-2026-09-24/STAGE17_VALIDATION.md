# Stage17 fact-buffer pre-sizing: incomplete performance checkpoint

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

## Two completed pairs; third not started

All four timing arms passed correctness and competitor checks under explicit
accuracy, Codata and worker holds. The original 200-second pair-reserve guard
refused repeat 3 before launch; the driver exited 1 for that deadline guard, not
an Enola failure. No hold was extended. Full three-pair acceptance is incomplete.
The next pair must use identical pinned binaries, input and configuration.

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

Timing spans process exit through producer acknowledgments. Raw first-batch,
broker/consumer boundaries, RSS, parse counts and wire data are retained in the
per-arm metrics. Original order was baseline/candidate, then candidate/baseline.
Codata disclosed background ClickHouse activity; per-arm container samples remain
in the local run archive. Absence of sampled competitors does not prove an idle
host. No watch performance claim follows from these CLI runs.

See [partial audit](stage17-checkpoint/partial-timing-audit.json), per-arm checks
and receipts, timing-window acknowledgments and the SHA256 manifest. The strict
summarizer rejects incomplete cohorts and correctness-only receipts; its validation
reproduced the existing Stage16 full series. Remaining: third pair, final assessment,
actual-main integration and required hooks. Near-zero cold no-op is not achieved.
