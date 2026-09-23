# Current production no-op costs

One instrumented fresh CLI run, full Product at `ae233c5f5695`, stage6 with
wave10 production (`1ba06f9`), file sink. Binary hash and exact argv are in
[the receipt](v314-noop-profile.json). A regression suite shared the host.
This is diagnostic attribution, not a repeated speedup comparison.

Elapsed wall time: **3.088 seconds**. Zero parses, zero added event bytes,
generation stays 2, all durable state JSON hashes unchanged.

| Stage | Seconds |
|---|---:|
| CLI graph target / engine setup | 0.814 |
| Durable state load, including JSON decode | 0.287 |
| Policy reuse proof | 0.089 |
| Runtime input construction, inclusive | 1.689 |
| Complete CLI internal trace | 3.069 |

Runtime input construction includes 0.113 s inventory, 0.194 s extractor
detection, 0.830 s hashing 5,044 files / 56.1 MB, 0.341 s discovery and 0.051 s
session context. Nested spans must not be added to the inclusive stage.
State is 61.1 MB; JSON decode is 0.274 s of the 0.287 s load. These timings do
not prove an implementation speed regression versus older shared-host runs.

The no-op performance goal remains open. The candidate work areas are reusable
policy/discovery, delayed full-state decoding, and avoiding repeated input work
with a sound change proof. Skipping input checks or trusting timestamps alone
would weaken correctness. Resident watch and a fresh CLI require separate
measurements; the former can retain knowledge that the latter must establish.

[Full nested trace](v314-noop-profile.stderr).
