# Stage4 candidate: fresh CLI no-op

Three runs per build on the same completed Product state and checkout, ordered baseline/candidate/candidate/baseline/baseline/candidate. The baseline is main 17226ed; the candidate applies the graph-input reuse proof to main, preserving accuracy work. File sink, ENOLA_GRAPH_PROFILE enabled, shared host. These measurements do not establish resident-watch latency or NATS delivery performance.

| Build | Median s | Range s |
|---|---:|---:|
| Baseline | 2.549 | 2.508–3.082 |
| Candidate | 2.038 | 1.959–2.836 |

Median decreased 20.1%. All six runs parsed zero files, emitted zero event bytes and preserved state JSON hashes and generation. Baseline second policy construction took 0.562–0.580 s; candidate proof took 0.087–0.094 s. These timings include the new Git-discovery recheck required by the independent inherited-gitfile and linked-worktree common-directory regressions.

This remains far from the requested near-zero no-op goal. First policy construction, inventory/content work, state loading and discovery remain. Initial attempt rejected a /tmp versus /private/tmp checkout spelling mismatch before transaction; it is excluded, its log retained, and the harness was corrected without changing the graph state.

[Raw measurements and binary/source provenance](v304-stage4-cli-noop.json). Changed-file validation is recorded in V304_STAGE4_CLI_DELTA.md. Integration evidence is in v304-stage4-integration.json; the full original suite had only two missing host-path annotations, corrected without semantic edits, followed by a passing full facts-package rerun.
