# Test-inclusive paired timing — not accepted

All twelve arms and 84 fresh CLI calls completed within the acknowledged 06:00–06:35 UTC window on 2026-09-27. All cold/delta and cross-arm graph checks and silent-no-op checks passed. The cohort is complete, but promotion is rejected because only four of six initial pairs favored the candidate; the prospective rule requires six of six. Do not replace or rerun individual arms.

| Scenario | Baseline median [range], s | Candidate median [range], s | Median change | Baseline/candidate median RSS, MiB |
|---|---:|---:|---:|---:|
| initial | 12.241 [11.900, 12.738] | 11.993 [11.736, 12.359] | -2.03% | 1254.0 / 1288.5 |
| noop | 2.337 [2.284, 2.449] | 2.137 [2.122, 2.211] | -8.53% | 377.4 / 373.8 |
| body | 4.433 [4.360, 4.579] | 4.234 [4.195, 4.295] | -4.49% | 926.4 / 881.9 |
| structural | 4.447 [4.401, 4.512] | 4.276 [4.258, 4.321] | -3.85% | 899.0 / 832.7 |

Initial paired percent changes: -2.250%, -2.935%, -3.628%, +2.293%, +0.895%, -5.822%.

The initial median meets the 2% improvement threshold, all scenario medians improve, and all scenario RSS changes satisfy the 5% ceiling. These do not override the failed initial consistency condition. Candidate initial RSS increased about 2.75%; delta RSS decreased. Fresh no-op remains about 2.14 seconds and delta about 4.2 seconds: this does not satisfy the broader near-zero/no-op goal.

Memory pressure samples were normal and Swapouts increments zero throughout all arms. Swapins occurred and are recorded as context under the preregistered policy, with no latency-bound claim. No recognized competing heavy processes were sampled. Whole-host load was variable (1-minute load 4.65–19.30); WindowServer, Orca, virtualization and VPN activity remained. This is a shared-host descriptive comparison, not an isolated causal estimate or significance claim. We do not attribute the failed pairs to host noise without evidence. Raw host logs remain at paths and SHA256 pins in host-audit.json; per-arm metrics, receipts and memory observations are archived here.

This is the whole experimental bundle against published Stage22 with an identical explicit test-inclusive scope, not an isolated optimization measurement. Only the new candidate enables changed-owner scope. All producer ACKs precede completion; first-batch and consumer timing are separate in timing-comparison.json. The cohort does not establish watch stability, real-history equivalence, old-upstream performance, or overall task completion.

Next action: keep this result and candidate unpublished; investigate additional initial-analysis cost using the completed phase profile, preserving graph semantics and memory bounds. A future changed candidate requires a newly pinned cohort.
