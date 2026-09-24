# Product production-watch control

One production `graph watch` run through a local NATS JetStream broker passed
idle, identical-byte rewrite, burst editing, frozen replacement and final cold
graph checks. This is a control measurement on released `a648c0b`, before the
alias optimization, not final performance or long-duration acceptance.

Product was copied from `a609c19f3861971930fae7b33dcb2950598953c5` into an isolated
checkout. The harness edits `packages/crypto/src/password.ts` three times,
approximately 50 ms apart, adding direct call relationships. The CLI window was
`--watch-every 500ms`. The [full receipt](stage9-product-watch-control.json)
identifies the binaries, telemetry, timings and measurement limitations.

| Observation | Result |
|---|---:|
| Process launch → initial consumer apply | 19.018 s |
| Process launch → harness observes initial | 19.236 s |
| Last durable save → final cold-equal consumer apply | 2.377 s |
| Burst completed replacements | 1 |
| Delta Begin scope / parsed files | 11 / 11 |
| Delta messages / batches | 26 / 24 |
| Delta JSON payload bytes | 688,614 |
| Delta node / edge records sent | 526 / 1,533 |
| Net graph node / edge count change | 0 / +2 |
| Idle / identical-byte rewrite events | 0 / 0 |
| Setup before watch launch | 114.026 s |
| Complete harness wall time, including cold check | 174.175 s |

Replacement traffic is not the number of new entities: unchanged contributions
within the frozen scope are resent. Initial scope contained 8,485 owners and
parsed 4,035 files. The delta reused 4,024 cached files. No incomplete Begin/End
pairs or abandoned Begins were observed at final selection.

Final watch and cold hashes match exactly; all measured checkout inputs remained
stable across the cold comparison. Final-generation selection uses observed
quiescence plus cold equality. It does not prove the internal watcher queue is
drained, because no watcher watermark is exposed. The receipt retains this
limitation and the timing clock definitions.

This single run shared the host with tests. It provides neither a timing spread
nor a before/after optimization comparison. It also exercises a short scripted
burst rather than a long-running coding agent. Enola used Go 1.27.1 with
`-trimpath`; the observer was built by the harness using Go 1.26.8.

```sh
python3 docs/benchmarks/product-efficiency-2026-09-22/watch.py \
  --binary /tmp/enola-stage9-md-timing/enola-a648c0b \
  --source /tmp/enola-stage9-md-timing/product \
  --work /tmp/enola-stage9-product-watch-control \
  --watch-every 500ms
```
