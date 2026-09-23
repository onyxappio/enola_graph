# Main fresh CLI no-op profile

Three sequential, instrumented fresh CLI no-op runs on published production revision `a211e0a`, Go 1.27.1 darwin/arm64, normal `go build -trimpath`. These use the same isolated Product checkout and TypeScript-only configuration as [the main watch baseline](V304_MAIN_WATCH_BASELINE.md), after its real source mutation. State is forked from the completed watch state into a separate context. Output is the file sink, not NATS; worker tests may share the host.

| Metric | Result |
| --- | ---: |
| Run 1 | 2.830 s |
| Run 2 | 2.351 s |
| Run 3 | 2.312 s |
| Median | 2.351 s |
| Parsed files / published events in every run | 0 / 0 |
| Generation / completed state bytes | unchanged |

## Stage medians

| Stage | Seconds |
| --- | ---: |
| CLI target preparation | 0.586 |
| Graph-input rebuild | 0.596 |
| State loading | 0.255 |
| TS discovery | 0.285 |
| TS context projection | 0.041 |
| Content hashing | 0.182 |

JSON decoding (0.244 s) is inside state loading, and `ts_discovery_build` is inside `ts_discovery`; nested spans must not be added again. The two preparation stages total about 1.18 s, but this does not prove all of that time can be removed. Resident retention and fresh CLI startup are separate optimization targets. These numbers do not establish a speedup versus the earlier broader-scope Go 1.26.8 profile.

The first attempted state fork failed with ENOSPC before any measurements. Its logs remain in `/tmp/enola-stage3-main-noop/`; no timings from that failure are included. After disk recovery, the successful runs and profiles are under `/tmp/enola-stage3-main-noop-retry/`. The adjacent JSON records exact timings, binary/config hashes, summaries, state hashes and nested stage spans.

## Artifact storage

To recover disk space, the completed cold cache `/tmp/enola-stage3-main-watch-r1/state-cold` was removed after recording its state SHA-256 in `cold-cache-cleanup.json`; source, binary, consumer/lifecycle logs and cold graph digest remain. The NATS blocks from the four older `/tmp/enola-discovery-watch-{baseline,candidate}-r{1,2}/` runs were gzip-archived only after checking for open handles, verifying stable source files and checking SHA-256 after decompression. Per-run `nats-block-archive-receipt.json` records each hash and the restore instruction. Those stores must be decompressed before opening with NATS.
