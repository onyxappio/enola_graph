# Product fresh CLI no-op attribution

A single instrumented no-change run of normal (unstripped) Enola `1cbf12a`, Product `07fb4a41`, completed in **4.207 s**, with **zero parses, zero events and generation 2 → 2**. This uses the existing Product scope configuration, a file sink, and a fork of previously validated state. It is not resident watch latency, broker acceptance, a repeated distribution, or a performance regression/speedup claim.

| Selected stage | Seconds |
| --- | ---: |
| CLI target preparation | 0.665 |
| Session graph-input rebuild | 0.645 |
| State load (63.5 MB JSON) | 0.510 |
| Hash content inputs (5,024 files, 55.8 MB) | 1.037 |
| TypeScript session context | 0.531 |

These selected intervals do not overlap. Other setup, discovery and session checks account for the remainder; nested trace totals must not be added again. The shared-discovery change can address part of the cost, but cannot alone eliminate CLI preparation, state decoding and whole-input hashing. Measure later optimizations separately from the resident path, which already retains state.

The first harness invocation successfully forked state but then attempted to decode empty fork stdout as JSON. The diagnostic resumed directly with delta against that fork; no second fork or mutation was needed. Source files were not changed.

The adjacent JSON records the exact command, binary hash, source revision, summary and trace digest. Full local trace: `/tmp/enola-discovery-noop-profile/delta.stderr`.
