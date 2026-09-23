# Shared discovery candidate: measured checkpoint

The frozen stage2 candidate builds one TypeScript discovery snapshot per run and shares it between context projection and planner previews. On a graph-scoped fixture, initial and config reconciliation build one snapshot instead of three. A local content-only run still builds one; cross-run retention is a later change. At the time of these measurements, production changes were **not integrated in main**. The later merge with accuracy v304 and its validation are recorded in [the integration report](V304_SHARED_DISCOVERY_INTEGRATION.md); the timings below belong to the original candidate binary.

## Correctness

The 14-file production manifest SHA-256 is `d61d4c189983044ade8e4aae7b77873ad13c2c1078b9e6c6b0c2ba8b59c77aac`; binary SHA-256 is `bc11268fd206986f2a34ff68cbead2bcd0af1983869b9778cc1543da5875688a`. Frozen artifacts: `/tmp/enola-stage2-freeze/`. Root independently verified hashes and ran alias, Nuxt, package export membership, failed End rollback and scoped resident config-race guards: PASS in 39.212 s. Worker affected suites also passed; final test-only state-oracle refinement was verified separately. The adjacent JSON identifies the final test manifest.

All four Product watch runs below have the same initial and final graph digests, exact cold equivalence, stable comparison inputs, a real graph-changing edit and zero idle/duplicate events. The delta reparses 11 files and replaces 11 owners, sending 31 batches, 525 node records and 1,944 edge records (net +2 edges).

## Product watch through NATS

Same Product revision `07fb4a41`, TypeScript-only scope, Go 1.26.8 darwin/arm64, normal `go build -trimpath` binaries without stripping. Baseline is `1cbf12a`. Runs were sequential on a shared host. Repeat 2 reused the already-built observer; setup duration is outside the startup timing.

| Build / repeat | Launch to initial consumer | Last save to cold-equal consumer |
| --- | ---: | ---: |
| Baseline 1 | 11.462 s | 6.630 s |
| Candidate 1 | 14.557 s | 6.630 s |
| Baseline 2 | 11.036 s | 6.582 s |
| Candidate 2 | 10.801 s | 6.606 s |

Save latency includes the fixed five-second collection window. The initial spread does not establish a stable speedup or regression; no run is discarded as an outlier. Watch latency is effectively unchanged at this precision. Completion is an observed cold-equal suffix, not a watcher-watermark proof of queue drain.

## Fresh CLI no-op

Three alternating instrumented runs per normal binary, file sink, same Product revision and existing Product scope config (broader than the watch TS-only profile), separate forks of the same completed state. Each run has zero parses/events and unchanged generation; completed state bytes remain identical within each context.

| Build | Median | Range |
| --- | ---: | ---: |
| Baseline | 2.913 s | 2.894–3.614 s |
| Candidate | 2.779 s | 2.737–2.798 s |

The measured median is about 4.6% lower. Baseline TS context projection takes a median 0.495 s; candidate discovery plus context takes about 0.312 s (0.274 + 0.038). Comparing 0.495 with 0.038 alone would hide moved work. CLI target preparation, graph-input rebuild and roughly 0.5 s state decoding remain. These instrumented file-sink no-op timings do not establish resident or broker latency.

## Next steps and limits

Complete reader/probe provenance before retaining discovery across runs, including Svelte/Nuxt config readers, Stat observations and membership transitions. Frozen stage2 depends on run-end input fences for these; its reuse predicate alone does not prove live inputs unchanged. Then reduce repeated CLI preparation, repeat initial/delta comparisons and complete memory, long-watch recovery and Product-history acceptance. This checkpoint does not finish the performance goal.

Raw reports and their SHA-256 values, command arguments, binary identities, no-op stage timings and graph digests are in the adjacent JSON. Local artifacts remain under `/tmp/enola-stage2-root-review/`, `/tmp/enola-stage2-noop-compare/` and the four `/tmp/enola-discovery-watch-*-r*/` directories.
