# Stage21: fresh-start diagnostic checkpoint

This is an instrumented, shared-load diagnostic of the Stage20 runtime, not a new implementation or a performance acceptance series. The next optimization design remains under review. No new speedup is established.

## Scope and provenance

The binary records source revision `a1dec99086fc1ace834e5616bd31c39ddce0b39b` and **vcs.modified=true**; do not call this a clean-tree build. Its SHA256 is `e9964ea1e7a99afee568b8f6d8aa7d9d9e66f03063d9dbfafe75f4a0f53b4a0b`. The observed root changes were documentation, but that observation alone is not a retrospective source attestation. Build metadata is retained in [build-info](stage21/build-info.txt).

Product revision `a609c19f3861971930fae7b33dcb2950598953c5`, TypeScript profile, and the existing Product scope were used for one initial, two fresh CLI no-ops, one body edit and one fresh cold verification. The source edit changes email normalization in `packages/crypto/src/password.ts`; its exact hashes are retained in the cold receipt. Competing workloads were observed, so durations only locate work.

## Observed no-op costs

Seconds for the two observations; rows can nest and must not be summed.

| Phase | Observation 1 | Observation 2 |
|---|---:|---:|
| Complete process | 1.660 | 1.636 |
| Graph input build | 0.329 | 0.319 |
| State JSON decode | 0.249 | 0.246 |
| Content hashing | 0.166 | 0.155 |
| First TS config input observation | 0.135 | 0.135 |
| Final TS config input observation | 0.134 | 0.132 |
| TS discovery | 0.207 | 0.211 |

Both no-ops parsed zero files, published zero messages, retained generation 1 and left committed state bytes unchanged. State decode materialized 4,086 records from 56,633,692 bytes; content hashing checked 4,105 inputs. Removing repeated decoding is a design lead, not a measured saving. The final independent input fence remains required.

## Published graph verification

Root independently verified replay of the retained initial plus delta stream through applied generation 2. Delta sent 24 batches for 11 owners and parsed 11 files. Its final normalized hash equals the fresh cold graph: `4412eeb573c637e0baaf9eb58195200d593d2b25ce4abde9e6ad3d421b10002d`, with 59,224 nodes and 124,878 edges. This proves the one observed body-edit case, not a wider history suite or a future optimization.

The first replay observer used DeliverNewPolicy and therefore did not consume historical messages. Its wait was stopped and a separate observer changed only the durable-name prefix and delivery policy to DeliverAllPolicy; validation and normalization were preserved. Replay consumer timestamps are later replay times and must not be reported as original delivery latency.

The state comparison also found nine differences in opportunistic export-surface proof metadata on three Vue files. Whole-state byte equality is not asserted. Published graph equality was established independently through the applied stream, rather than by discarding those fields from a state comparison.

## Next acceptance boundary

A compact committed-state proof must specify how it is produced, validated and bound to exact state bytes, identities and versions. The no-change decision must precede full record consumers; full-result calls, changed inputs and neutral bookkeeping refresh need compatible materialization. Pending typed validation, replay and acknowledged-End promotion must remain intact. No production implementation is approved by this diagnostic report.

## Evidence

[Root review](stage21/root-review.json), [phase metrics](stage21/run1/phase-metrics.json), [run receipt](stage21/run1/diagnostic-receipt.json), [cold receipt](stage21/run2/cold-receipt.json), [applied replay frames](stage21/replay2/consumer.jsonl), [cold frame](stage21/run2/consumer.jsonl), [observer diff](stage21/replayobs-source.diff), and [artifact hashes](stage21/artifact-sha256.json). Raw artifacts retain original local paths and require their original environment to replay. The root review and this report supersede any draft claim of a clean-tree build or whole-state equivalence.
