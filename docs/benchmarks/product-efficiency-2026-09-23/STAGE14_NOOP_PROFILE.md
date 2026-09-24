# Stage 14: current fresh CLI no-op costs

Single instrumented file-sink diagnostic with the stage13 candidate binary (production-equivalent to eb73b66; final integration changes only comments). Product live checkout and persistent state from the completed stage9 history harness. This is not a repeated performance comparison or a resident-watch timing.

The first attempt using the old benchmark state was NOT a no-op: session profiling reported initial=true, force=true and raw_cfg=true, rebuilt the graph, and advanced generation. Its 27.482s is excluded from no-op evidence, and the assertion rejecting it is preserved by the receipt's false state/events-unchanged flags. A second fresh process against the newly completed state took **1.825441s**, parsed zero files, published zero owners/events, held generation **12→12**, and preserved all persistent state bytes (excluding the session lock) and event-stream length.

| Phase | Seconds |
|---|---:|
| Resolve graph target / build policy | 0.303 |
| Load state, including JSON decode | 0.308 |
| JSON decode alone (inside load) | 0.269 |
| Prove fresh engine inputs | 0.087 |
| Runtime input collection, total | 0.853 |
| Inventory (inside runtime inputs) | 0.105 |
| Detect extractors (inside runtime inputs) | 0.118 |
| Hash input content (inside runtime inputs) | 0.171 |
| Capture extractor contexts (inside runtime inputs) | 0.062 |
| Analysis fingerprint (inside runtime inputs) | 0.132 |
| TS discovery (inside runtime inputs) | 0.199 |
| TS session contexts (inside runtime inputs) | 0.048 |
| Reuse TS facts | 0.060 |
| Final no-publication fence | 0.124 |

Parent/child rows must not be added together. Zero extractor file reads in the public summary does not mean zero filesystem reads: input proof hashes 5,051 files, and policy/discovery/context checks also read the tree.

Two proposed shortcuts were rejected during review: borrowing policy-proof membership for a later inventory moves the observation earlier without a final ordinary-source membership fence; substituting engine AllNames for manifest DeltaContext omits descendants of engine-ignored directories that the manifest extractor deliberately observes. Any future fusion must preserve those inputs and observation guarantees. No stage14 production optimization has been implemented yet.

Raw diagnostic, summary, script and receipts are adjacent `stage14-*` files. The script uses the recorded absolute scratch paths and refuses to overwrite its output. Its first-run precondition matters: the saved state must already belong to the measured build/config, otherwise it correctly rejects a rebuild as a no-op.

## Separate config subphase diagnostic

An additional timer-only overlay on eb73b66 split each tsConfigInputs call: alias-root recursion 0.094/0.101s; config-candidate walk 0.068/0.071s; complete calls 0.164/0.174s. The full run was 4.486s with zero parses/publication and unchanged state/events. This differs substantially from the earlier single run and is not a paired regression or speedup measurement. It identifies work to prototype: call-local directory enumeration sharing while preserving separate reads and independent final revalidation. Raw timings, source hash and exact instrumented file are archived as stage14-config-*; no production optimization is implied.
