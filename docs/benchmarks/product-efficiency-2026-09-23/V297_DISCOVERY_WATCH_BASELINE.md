# Product watch baseline before shared discovery

This is one completed baseline run, not a measurement of the pending shared-discovery optimization or a speedup claim. Enola `1cbf12aee4e53a0a568da12ce522008575ba4d7b` used Product `07fb4a41ddafff7f42ebd55af8a23fe5739f4cd7`, a TypeScript-only profile, real NATS, and the production watch command with a fixed five-second collection window. The binary was built with Go 1.26.8, `go build -trimpath`, without stripping, on darwin/arm64. Compare a candidate with the same build flags and inputs.

| Measurement | Result |
| --- | ---: |
| Watch launch to initial consumer apply | 11.462 s |
| Last durable save to observed cold-equal consumer | 6.630 s |
| Burst writes / completed replacements | 3 / 1 |
| Delta owners / parsed files / summary scans | 11 / 11 / 117 |
| Delta batches | 31 |
| Delta node / edge records transmitted | 525 / 1,944 |
| Delta JSON payload bytes, including Begin and End | 784,937 |
| Net graph change | +2 edges |
| Idle / identical-write events | 0 / 0 |
| Whole harness, including setup, idle checks and cold run | 83.644 s |

The save-to-consumer interval includes the collection window; it is not analysis-only duration. The mutation changes actual graph facts. The final watch graph exactly matches a cold graph, inputs were stable across the cold comparison, observed batch counts match End, and no incomplete Begin/End pair remained at selection. The harness uses an isolated clone.

This single shared-host run establishes a baseline, not a distribution, memory result, long-run recovery result, or full Product acceptance. Completion selection is an observed matching suffix; no watcher watermark proves causal queue drain. Startup consumer timing compares clocks in two processes on the same host. JSON payload sizes exclude broker framing.

The adjacent JSON preserves revisions, binary and raw-report SHA-256, graph digests and detailed delta telemetry. Raw local evidence is `/tmp/enola-discovery-watch-baseline-r1/`.

Reproduce with:

```sh
python3 docs/benchmarks/product-efficiency-2026-09-22/watch.py \
  --binary /tmp/enola-discovery-comparison-baseline/enola \
  --source /private/tmp/enola-scope-matched-v295-compare/baseline/live \
  --work /tmp/enola-discovery-watch-baseline-repeat
```
