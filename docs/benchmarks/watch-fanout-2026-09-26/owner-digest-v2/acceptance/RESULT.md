# Owner digest v2 paired timing — not accepted

All twelve arms / 84 fresh CLI calls completed inside the confirmed 06:40–07:15 UTC window. All graph equality, no-op, workload, pressure and memory gates passed. Six of six initial pairs favor candidate, but initial ratio of medians improves only1.313%, below the preregistered2% minimum. No promotion; no samples replaced.

| Scenario | Baseline median [range], s | Candidate median [range], s | Median change | Median RSS baseline/candidate, MiB |
|---|---:|---:|---:|---:|
| initial | 12.163 [12.111, 12.630] | 12.004 [11.742, 12.090] | -1.313% | 1291.9 / 1214.8 |
| noop | 2.312 [2.295, 2.333] | 2.114 [2.100, 2.127] | -8.569% | 366.5 / 378.1 |
| body | 4.397 [4.346, 4.495] | 4.231 [4.175, 4.311] | -3.784% | 895.1 / 876.9 |
| structural | 4.391 [4.375, 4.485] | 4.281 [4.220, 4.290] | -2.511% | 904.7 / 838.1 |

Initial paired changes: -4.839%, -2.067%, -0.240%, -0.710%, -3.135%, -1.771%.

The mean of four scenario median ratios is0.95956 and sum of medians decreases2.728%; neither overrides the primary magnitude gate. Initial median RSS falls about5.97% versus published baseline. No isolated digest-v2 end-to-end gain can be inferred from this whole-bundle comparison or by subtracting the prior cohort.

Memory validity: every pressure sample normal, zero new Swapouts, no counter resets, no uncovered boundaries and no sample gap over1s. Swapins remain recorded context, not evidence of bounded paging latency. No recognized competing heavy process was sampled. Whole-host background load remains in host-audit.json and raw log hashes; this is a shared-host engineering comparison, not a significance claim.

Near-zero fresh CLI no-op, real Product history scenarios with the new test-inclusive scope, long watch stability and publication remain open. Next action: target a larger initial cost (the existing phase trace places5.783s in TS mapfiles and0.544s in GraphQL/gRPC index preparation) rather than repeat this unchanged candidate. Prior cohorts retain their original verdicts.
