# Main watch baseline for retained-discovery work

One diagnostic Product run of published main `a211e0a`, before cross-run discovery retention. Product revision `07fb4a41ddafff7f42ebd55af8a23fe5739f4cd7`, TypeScript-only scope from the existing watch harness, isolated checkout, NATS JetStream, fixed five-second collection window.

| Metric | Result |
| --- | ---: |
| Process launch to initial consumer apply | 13.330 s |
| Last burst save to final cold-equal consumer apply | 6.693 s |
| Burst writes / completed replacements | 3 / 1 |
| Delta owners / parsed files | 11 / 11 |
| Delta batches | 27 |
| Delta node / edge records sent | 525 / 1,696 |
| Delta JSON payload bytes, including Begin and End | 725,954 |
| Net graph change | +2 edges |
| Idle / duplicate events | 0 / 0 |

Watch and cold graph digests match exactly after a graph-changing edit; inputs remain stable across the cold comparison. This does not prove internal watcher queue drain, long-running recovery, or a performance improvement. One run has no repeat spread.

The baseline binary was built with Go **1.27.1**, darwin/arm64, normal `go build -trimpath`, without stripping, in 22.735 s. The complete harness took 83.844 s, including setup, idle/duplicate windows, mutation and cold verification; that is not initial latency. The observer is the same fixed Go 1.26.8 binary used previously. Future stage3 comparisons must use the same baseline/candidate compiler, flags, Product revision, scope and observer.

Earlier stage2 timings used Go 1.26.8 and pre-wave-9 semantics. This new run also has different graph hashes and edge counts after accuracy v304; do not attribute differences from those older runs solely to performance code. The adjacent JSON preserves both build identity and the complete harness output, limitations and telemetry. Raw artifacts remain at `/tmp/enola-stage3-main-watch-r1/`.
