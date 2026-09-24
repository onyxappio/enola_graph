# Stage15 matched Product correctness and work comparison

Historical experiment: the combined Wave14 validation and final integration decision are in [STAGE15_WAVE14_VALIDATION.md](STAGE15_WAVE14_VALIDATION.md).

Transition: `07fb4a41ddafff7f42ebd55af8a23fe5739f4cd7` → `fec1eac346c48dbc68072803d89b2e4709f9bd42`.

Both runs begin from fresh analysis of the parent, use the same pinned policy and advance generation 1→2. Both match their cold target graph and pass the next silent, state-preserving no-op.

| Metric | Baseline | Candidate | Reduction |
|---|---:|---:|---:|
| parsed_files | 591 | 513 | 13.20% |
| files_read | 695 | 617 | 11.22% |
| owners_published | 1168 | 1149 | 1.63% |

All 78 fewer parses belong to resolution (495→417). Source-content 92 and added-source 4 counts are unchanged.

Timing is excluded: these were correctness runs on a contended host. This is one transition and an experimental patch on 1af2b91, not acceptance of combined accuracy changes or the whole performance goal.

Artifacts: matched-history-comparison.json, history-transition-results.json, history-matched-baseline-results.json, candidate-build-receipt.json. Raw state, events and logs retained.


## Source and validation boundary

This report describes the experimental Stage15 patch on `1af2b91`, before wave14 accuracy changes landed as `2ef91a4`. The isolated candidate used provisional cache v319. It must be rebased and revalidated before integration; its counts do not establish performance of the combined version.

Worker validation: full tsextractor 21.833s, full graphsession 418.782s, focused race 1.903s and 48.786s, cachecov 0.386s; all passed. Independent root race validation: tsextractor 1.561s, graphsession 142.907s, cachecov 2.203s; all passed. These are operational test durations, not benchmark measurements.

The independent forwarding regression covers both directions and frozen/legacy runs; the missing-proof state test exercises persisted JSON; merge-unknown and in-flight peek tests have failing mutation controls. Main production was not changed by this experiment.

The compact machine-readable receipt is `stage15-matched-history.json`. It retains binary, patch, source and raw-artifact hashes. Completed Product source clones were removed to recover disk space; state, events and logs remain in the recorded scratch paths.
