# Stage38 bounded hashing — complete cohort, independently accepted

Runtime 1af548c, clean build cb6a69f, candidate binary e9e48eb0eaeb4e642ff6078ee4a5db0b65eda9d545b61e935ee9f330bb6e1805. Baseline is accepted Stage37 runtime550e1fa / binary15f55895. Same pinned Product fba38bab, test-inclusive configuration and host; six alternating AB/BA pairs, 12 arms, 84 CLI calls and 48 cold/delta correctness checks. Session20509 exited0; all four participants explicitly released.

| Scenario | Baseline median [min,max] s | Candidate median [min,max] s | Change | Median RSS change |
|---|---:|---:|---:|---:|
| initial | 11.235892 [11.025852, 11.699960] | 11.106072 [11.001111, 11.308576] | -1.1554% | -1.5705% |
| noop | 2.006362 [1.995566, 2.044864] | 1.869400 [1.859420, 1.893793] | -6.8264% | +1.0999% |
| body | 3.781358 [3.723113, 3.867897] | 3.636822 [3.602174, 3.719266] | -3.8223% | +4.2933% |
| structural | 4.188238 [4.157293, 4.231200] | 4.046660 [4.035431, 4.125056] | -3.3804% | -2.5200% |

Frozen criteria pass in the validator and independent primary arithmetic: fresh no-op gain6.8264% >=3%, all six pairs win; other scenario median regressions <=2%, every median RSS increase <=5%, mean scenario ratio0.962039 <1 and sum of medians decreases2.6065%. Initial gain is small with overlapping ranges and one losing pair; no meaningful/general initial acceleration claim. RSS body +4.2933% is an increase, within the prospective5% gate, not a memory-reduction claim.

Body changes source but not graph facts: parses1, owner scope0, Begin/End only, generation advances. Structural: parses22, scope1,3 wire messages. All seven normalized graph labels equal across all arms; no-ops parse/publish zero, retain generation and exact state bytes. Initial parses6749. Full details, first batch, broker/consumer boundaries, payload and per-arm delta/initial ratios are in timing-comparison.json. Fresh CLI includes process completion through producer acknowledgments; this is not resident/watch acceptance.

Primary host audit excludes the harness owner PID and its descendants. Mean foreign sampled CPU favors candidate in4/6 pairs; samples are descriptive/coarse ps observations, not calibrated causal corrections. No clean-host, significance, universal storage or cold-page-cache claim. Initial filesystem cache is not reset. Pressure/power gates pass; boundary AC observations do not prove continuous AC. Swapins remain contextual per frozen rule. Independent final review msg_76f385a8252f accepts bounded promotion; foreign-load asymmetry remains disclosed.

Final rule status/build updates were made before timing; numerical thresholds unchanged. Earliest reviewer ACK timestamp18:25:25 UTC precedes actual cohort start; no earlier work is claimed as quiet. Source review msg_f04619eb67a2 found no runtime blocker. Follow-up c3c481e changes comments/tests only; focused hash tests pass and a wrapper-removal mutation is rejected by the new deterministic scratch-buffer guard.

Stage38 independently accepted and normally pushed to main as8c63211; all pre-push guards passed. Near-zero fresh startup, much faster deltas, broad history performance and long-running watch remain open.
