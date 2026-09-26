# Full-profile Product fanout timing — not accepted for promotion

Nine cyclic baseline/control/candidate arms completed exit 0, 17:07:48–17:25 UTC on 2026-09-26, inside the confirmed 17:05–17:30 window. All seven-call cold/delta correctness checks and normalized graph hashes match across arms and repetitions; no-op stays silent. Combined source is 63c037d (binary built from the identical frozen overlay); baseline is published Stage22, not old upstream. Full repository suite passed 110 packages.

## Descriptive measurements

Seconds are fresh CLI through exit including producer broker acknowledgments. Each cell gives median [minimum–maximum], n=3; RSS is median process peak MiB.

| Scenario | Baseline seconds | Combined seconds | Time change | Peak RSS MiB |
|---|---:|---:|---:|---:|
| initial | 17.106 [16.105–17.217] | 14.842 [14.810–15.139] | -13.23% | 1138.9 → 1226.2 |
| noop | 7.866 [7.224–7.983] | 6.149 [6.050–6.266] | -21.83% | 379.4 → 378.6 |
| body | 11.689 [11.508–12.147] | 10.473 [10.443–10.970] | -10.40% | 809.0 → 721.4 |
| structural | 11.531 [11.515–12.522] | 10.647 [10.381–11.161] | -7.67% | 843.5 → 773.9 |

The preregistered promotion decision is **FAIL**: initial median peak RSS grew 7.66% (about 87.3 MiB), exceeding the 5% limit. Initial improved in all three paired comparisons; timing-only gates passed. Fanout-only control also fails its diagnostic comparison: inconsistent initial improvements and structural median +3.27%. Neither comparison justifies pushing this change to main.

Candidate delta/initial remains approximately 0.71; fresh no-op remains 6.15 seconds. These are intermediate full-profile results, not fulfillment of near-zero no-op or fast resident watch. Do not compare them directly with older TS-only measurements.

Initial first batch median: 12.807 → 11.285 seconds; broker End boundary 16.990 → 14.737 seconds. In graph-neutral body changes the candidate emits Begin/End without any batch; raw first_batch_s contains an absent-timestamp sentinel converted to a negative value. Treat that field as **N/A**, never a speed measurement. Raw observations are preserved; elapsed CLI and End metrics are unaffected. A future harness correction must represent absent first batch as null.

## Host conditions and limits

No recognized competing Enola/build/test job or failed host sample was detected. Agent holds were explicit. Whole-host logs nevertheless show variable OS, desktop, virtualization and agent CPU activity (per-arm median summed foreign ps %CPU roughly 72–156; instantaneous summed peaks 214–483). ps CPU values are decayed observations, not interval utilization. This is a coordinated shared-desktop series, not an unloaded-host proof; causal timing precision is limited. Old Chrome processes were left untouched. See host-audit.json. Do not use these data as unconditional performance acceptance even apart from the failed RSS gate.

Raw host command-line logs remain local under /tmp/enola-fanout-nats-acceptance to avoid publishing unrelated process arguments. Numerical audit and exact arm/check/binary receipts are archived here. The harness scripts retain their original absolute paths/pins as provenance; relocate deliberately for reproduction.

## Next work

Investigate initial heap/RSS behavior of shared configuration discovery versus the fanout-only control without assuming the 0.86 MiB owner-digest state explains the much larger process peak. Preserve the failed cohort and thresholds; any corrected candidate needs a new frozen build, correctness checks and separately coordinated repeated timing. Main has not been updated with these changes.
