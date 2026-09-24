# Empty-domain optimization: Product watch check

The empty-domain candidate passes a production `graph watch` check through local
NATS JetStream on the same Product fixture as [the control](STAGE9_PRODUCT_WATCH_CONTROL.md).
The [receipt](stage9-empty-domain-watch.json) pins the experimental binary, observer
and integrated commit `887b50f`. This is one short scripted burst, not repeated
or long-duration performance acceptance.

| Observation | Result |
|---|---:|
| Process launch → initial consumer apply | 9.836 s |
| Process launch → harness observes initial | 10.090 s |
| Last durable save → final cold-equal consumer apply | 2.171 s |
| Three burst saves → completed replacements | 1 |
| Delta scope / parsed files | 11 / 11 |
| Delta messages / batches | 26 / 24 |
| Delta JSON payload bytes | 688,614 |
| Delta node / edge records resent | 526 / 1,533 |
| Net graph node / edge count change | 0 / +2 |
| Idle / identical-byte rewrite events | 0 / 0 |
| Setup before watch launch | 25.376 s |
| Total harness including setup and cold check | 65.224 s |

The collection window is 500 ms. Initial parses 4,035 files; delta reuses 4,024.
The initial and final graph hashes match the released control exactly. Final
watch/cold equality passes, all 45,107 checked inputs remain stable across cold
analysis, and no unfinished or abandoned replacement is observed at selection.
Selection uses observed quiescence; it does not prove internal watcher queue drain.

The earlier control measured 19.018 seconds initial and 2.377 seconds convergence.
These separately scheduled shared-host samples do not prove an initial speedup.
The fixture edits an existing TS file, so it does not exercise the package-addition
scope reduction measured in [the history benchmark](STAGE9_EMPTY_DOMAIN.md).
Repeated alternating measurements are reported below.

```sh
python3 docs/benchmarks/product-efficiency-2026-09-22/watch.py \
  --binary /tmp/enola-stage9-empty-domain-review/enola \
  --observer /tmp/enola-stage9-product-watch-control/bin/benchobserver \
  --source /tmp/enola-stage9-md-timing/product \
  --work /tmp/enola-stage9-empty-domain-watch \
  --watch-every 500ms
```

## Repeated comparison

[Six fresh watch runs](stage9-empty-domain-watch-repeated.json), alternating three
per binary on the same fixture, all pass final cold equality, stable-input checks,
frozen replacement validation, idle silence and identical-byte rewrite silence.
All six initial hashes agree and all six final hashes agree. Each burst produces
one replacement with 11 owners. Both binaries and the observer are pinned.

| Phase | Released a648c0b median seconds (range) | Candidate median seconds (range) |
|---|---:|---:|
| Launch → initial consumer apply | 11.619 (10.487–12.144) | 10.286 (8.655–11.603) |
| Last save → final cold-equal apply | 5.322 (1.975–5.367) | 2.249 (2.154–5.310) |

The distributions overlap substantially: both arms have roughly two-second and
five-second convergence samples. These three-repeat shared-host results do not
establish a stable watch speedup from the optimization. In the slow samples,
save → the **final successful** Begin is about 4.3 seconds, compared with about
one second from that Begin to End. Subsequent timeline profiling showed an earlier
Begin whose run was interrupted: the slow tail includes failed work and a retry,
not 4.3 seconds without publication. See [the retry timeline](STAGE9_WATCH_RETRY_PROFILE.md). Consumer completion is also distinct from watcher readiness
and local durable-state completion. No internal watcher watermark is available.

This remains a short scripted burst check, not a long-running concurrent coding
agent acceptance test. The measured initial boundary includes NATS delivery and
consumer apply; full CLI teardown and setup/copy time are reported separately.
