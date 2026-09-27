# Test-inclusive Product phase diagnostic — 2026-09-27

Runtime bdd7451, pinned candidate SHA da6ab61d9f54d72d8e623fe14c7839e78d74cb8a5586fd407fbbabf7f198e819. Seven calls and four correctness gates passed. All calls are fresh CLI processes, not resident watch. ENOLA_GRAPH_PROFILE=1 was set only for the CLI driver; observer stayed uninstrumented. Candidate opts into changed-owner scope. This diagnostic is not quiet-window performance acceptance, a repeated comparison, or evidence of a speedup.

| Scenario | Process wall seconds | Parsed files | Peak process RSS MiB |
|---|---:|---:|---:|
| Initial | 12.487 | 6752 | 1230.6 |
| No-op | 2.217 | 0 | 380.1 |
| Body edit | 4.353 | 21 | 896.9 |
| Structural edit | 4.532 | 22 | 817.6 |

The delta/initial ratios in this single diagnostic are approximately 35% and 36%; near-zero fresh no-op is still not achieved. Cold oracles took 12.079–12.159 seconds. These numbers cannot replace prospective repeated acceptance.

No-op outer CLI trace: resolve target 0.357 s, then session run 1.839 s. Nested inside session: open/load state 0.459 s (90,686,087 bytes; JSON decode 0.405 s), reconcile 0.104 s, session 1.268 s. The session includes 1.029 s runtime inputs: inventory 0.124 s, detection 0.148 s, content hashes 0.297 s, discovery 0.190 s, other context/config work. Do not sum nested trace names. State decode and input proof dominate fresh no-op; zero parses does not mean zero whole-repository work.

Body delta: frozen preview 0.648 s, invalidation planning 0.230 s, dirty-scope handling 0.154 s, assembly 0.252 s, grouping 0.155 s, full-record revalidation 0.245 s, pending-state write 0.494 s. Extraction appears as zero after preview because preview already did its work; this is not zero-cost parsing. Graph-neutral body edit publishes an empty replacement generation while structural publishes one owner, preserving source-change versus unchanged-input distinction.

Initial: TS extraction 6.560 s, owner encode/append 2.780 s, pending-state write 0.524 s. Remaining initial cost is not just the file inventory.

Next: freeze and run the same-scope six-pair acceptance cohort before changing this candidate further. Subsequent optimization candidates are reusing already-proven inputs and reducing checkpoint decode/write cost; any shortcut must retain exact content-change detection, conservative frozen scope, durable recovery, and no additional relationship indexes. Full cold-start and resident measurements remain separate. No new implementation is accepted by this diagnostic.
