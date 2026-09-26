# Next fresh CLI no-op target — source review, not a measured optimization

The pinned combined profile (`stage24-combined/combined.stderr`) reports 6.395 s
inside CLI instrumentation; its outer diagnostic receipt reports 7.300 s. These
are different boundaries from one diagnostic invocation, not accepted medians.
The currently pending balanced cohort still measures the frozen 9c7359f runtime.
No new runtime source change has been made during this review.

| Profile span | Seconds | Concrete work |
| --- | ---: | --- |
| TS discovery build | 1.443 | 17,442 side reads, 17,454 stat observations, 5,837 walked directories |
| Extractor detection | 0.779 | Concurrent detectors; do not sum their individual durations |
| Inventory | 0.558 | Existing whole-repository input inventory |
| Capture contexts | 0.385 | Separate from content hashing |
| Initial config fingerprint | 0.362 | Config scope discovery and hashing |
| Final no-publication fence | 0.374 | Includes a second 0.369 s config-input scan |
| Content hashing | 0.235 | 6,393 input targets, about 64 MB |

## First concrete candidate

`collectNuxtPackages` already enumerates through `sharedDiscoveryEntries` and then
calls `detectNuxtAt` for every directory. That function tries three config stats
before testing the manifest. The completed listings already reside in the
existing discovery observation ledger. Roughly three probes per directory match
the observed stat-count scale; that is a source-based hypothesis, not measured
attribution of the whole discovery duration.

A narrow candidate can use a completed listing to establish missing config
names, retain the same missing-stat observation, and keep actual stat behavior
for present names, symlinks, failed/unavailable listings and policy refusals. It
must use the existing per-discovery ledger; no new file/symbol relationship index
or persistent cache is needed. Crucially, `overlayStat` currently uses live
filesystem presence even when captured content exists: do not import the alias
reader’s captured-byte semantics into this presence probe.

Before adoption, prove equal Nuxt package selection and observation ledgers with
no configs, nested manifests, each supported config name, directories named like
configs, broken/present symlinks, policy exclusion, captured/live disagreement,
failed listings, and a config appearing after enumeration. Retained discovery
must reject changed inputs and cold/delta graphs must remain equal. A listing
optimization must not silently treat an unobserved directory as empty.

## Other candidates, after the narrow probe

OpenAPI, AsyncAPI and gRPC detection currently perform their own scoped walks.
The engine already supplies `AllNames` to `FileListDetector` implementations, but
these three do not implement it. Reuse is plausible, yet requires proving their
individual directory-pruning, name membership, symlink and walk-error behavior;
the inventory list is not automatically an equivalent detector input. No such
change has been implemented or accepted.

Do not remove the final config-input fence merely because a run publishes no
facts. It guards mutation during the transaction. Any reuse there must carry a
proof that covers the same inputs and failure behavior.

## Timing status

The 18:30–19:05 UTC proposed window was released without starting an arm after
preflight found codata-memgraph-17689 at 100.92% CPU. Codata’s coordinator confirmed
a live focused integration test (message msg_21d3518c2751), not an orphan process;
its completion ETA was unknown. Prior container boundary snapshots were near
idle (0.03–0.46%); the separate audit preserves exact excerpts and source hashes.
Those snapshots cannot rule out intervening bursts or explain prior swap events.

Topology follow-up: local Docker context is `colima`, using the local Unix socket,
with no DOCKER_HOST/DOCKER_CONTEXT override. Codata reports a GCP tunnel for its
integration test. Until the coordinator resolves that discrepancy, do not infer
that its test causes the local container CPU load merely from a matching name.
Local VM CPU and container CPU are observed; precise workload ownership is pending.
