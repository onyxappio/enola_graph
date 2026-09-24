# Checkpoint serialization allocation diagnostic

Both exact-boundary instrumented builds passed all four correctness/no-op gates and all seven cross-arm normalized graph comparisons. This is one diagnostic run per arm, not latency/RSS acceptance. JSON boundary deltas include logging overhead and concurrent goroutine allocations; they are not exclusive CPU attribution.

| Scenario | Baseline allocation bytes | Stage16 allocation bytes |
|---|---:|---:|
| Initial | 381719456 | 381724032 |
| Body delta | 381310424 | 404979328 |
| Structural delta | 381296232 | 381305296 |

Structural checkpoint encoding alone spans roughly381MB of cumulative allocations and653k mallocs for a56.6MB checkpoint. This largely explains the previously unidentified roughly390MB interval. The candidate body sample is higher (~405MB); do not discard this variation or attribute it to config traversal without further evidence. Two collections occur between the structural boundaries in both arms. This does not establish retained heap or explain the repeated uninstrumented RSS regression.

The production path calls json.Marshal(st), writes the bytes to a temporary file, fsyncs, renames, fsyncs the directory and fingerprints the written bytes. No-op does not take this path, so an encoding optimization alone cannot deliver near-zero no-op. Any follow-up must preserve exact checkpoint decoding, atomic pending-state recovery and acknowledged-End promotion. Merely replacing Marshal with json.Encoder is not evidence of streaming allocation reduction; encoding/json may still buffer the value. Profile/validate the actual proposed writer and preserve eager corruption detection.

Evidence: checkpoint-allocation.json, both per-arm raw logs/checks/receipts, build SHA manifests and exact Go overlays. No production changes were made. Main581b13d separately publishes the completed Stage17 three-pair assessment.
