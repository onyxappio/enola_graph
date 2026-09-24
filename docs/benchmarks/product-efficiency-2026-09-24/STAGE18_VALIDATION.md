# Stage18 inventory fusion: final correctness and timing

Experimental candidate d988437 fuses the policy reuse proof walk with inventory projection. It remains unmerged: the measured startup improvement is small and RSS remains a concern. Main production is unchanged; baseline c6332bf is production-equivalent to main dba40ae (intervening changes are documentation only).

## Correctness

Four affected packages passed on the frozen source (435 seconds total wall time). Independent TestFused passed, including nonempty graph/body-change assertions and source/config observation boundaries. The Product history completed 10 transitions and 11 revisions: chain, candidate cold and baseline cold graph hashes matched at every revision. Every no-op had zero parses/events and unchanged state bytes and generation. The history includes manifest/dependency changes, but no tsconfig-changing transition. These concurrent-load checks make no timing claim.

All six timing arms passed their correctness checks, and all 21 cross-arm graph comparisons match. Initial/no-op/body/structural parse counts are 4034/0/11/12. The timed body edit is graph-neutral; the separate focused body test changes direct IO facts.

## Three paired measurements

Three alternating baseline/candidate pairs ran in the confirmed September 24 23:33–23:43 UTC window and finished before 23:40:14. All three participants explicitly confirmed quiet work; END released the holds. No competing workloads were sampled. Codata background services remained running, including ClickHouse; container samples are retained. This is descriptive evidence from three pairs, not a significance test.

| Scenario | Baseline median [min, max], s | Candidate median [min, max], s | Median change |
|---|---:|---:|---:|
| initial | 8.384 [8.305, 8.497] | 8.460 [8.416, 8.470] | +0.92% |
| noop | 1.664 [1.657, 1.664] | 1.616 [1.612, 1.619] | -2.86% |
| body | 3.469 [3.456, 3.475] | 3.410 [3.404, 3.449] | -1.71% |
| structural | 3.532 [3.465, 3.532] | 3.465 [3.438, 3.552] | -1.90% |

No-op and body time ranges do not overlap. Initial and structural ranges overlap; structural pair 3 is slower. Candidate delta remains about 40–41% of its own initial time, and fresh no-op is still 1.616 seconds. This does not achieve near-zero no-op or substantially faster single-file delta. The series measures fresh CLI through process exit including producer ACKs; first batch and broker/consumer completion are reported separately in the raw metrics. It makes no watch or daemon claim.

## Memory and decision

Body peak RSS median increases from 581,615,616 to 660,815,872 bytes (+13.62%). Baseline range is [571,457,536, 677,363,712], candidate [658,636,800, 664,813,568]; pair 1 is lower, pairs 2 and 3 are higher. Overlap and GC variation do not establish that this concern is harmless. No-op and initial RSS medians also rise, with overlapping ranges. No universal memory improvement is claimed.

Do not merge this candidate based on these modest timing gains. Retain the frozen evidence and investigate remaining state loading/index costs and memory behavior. No further repetitions solely to obtain a favorable result. Earlier Stage16 RSS and FSM integration gates remain independent and unresolved.

## Evidence

See [timing summary](stage18-final/timing-comparison.json), [21 graph comparisons](stage18-final/three-pair-graph-equality.json), [history](stage18-final/history-receipt-final-d988437.json), and adjacent raw per-arm receipts, package logs, source reconciliation and container samples. The pre-timing acceptance receipt means correctness acceptance for this candidate only, not performance acceptance or completion of the overall goal.
