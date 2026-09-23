# Product stage profile and manifest no-op defect

Base: `ee8f1b7` (v297 accuracy plus frozen observed-surface scope checkpoint).
This is diagnostic evidence, not a performance acceptance claim. Runs used a file
sink on a shared host; concurrent worker tests affected timings. Broker latency,
isolated repeated initial/delta, resident/watch and final speed targets remain open.

## Detailed Product transition

Product `4d104e600f89cfc0d84a93c02a6635191805b7d9` →
`07fb4a41ddafff7f42ebd55af8a23fe5739f4cd7`.
Root rebuilt the reviewed timing-only overlay on clean `ee8f1b7`.
It contains no derived export-index adoption. Binary SHA256:
`48a4bb7845ac2d4d8a241574a97bddc7f2a159a8d40b5b92064b82b7decf4f21`.

| Run | Complete fresh CLI seconds | Frozen protocol | Exact target graph |
|---|---:|---|---|
| Initial parent | 36.220 | pass | parent |
| Delta | 5.589 | pass | equal to cold |
| Cold target | 37.577 | pass | comparison oracle |

Delta performed 4 full parses and 15 summary scans, with 36 Begin owners and
25 published owners. Its outer CLI trace was 5.531 seconds; process startup and
termination account for the difference from the subprocess wall measurement.

| Non-overlapping top-level stage | Seconds |
|---|---:|
| CLI target/config/engine preparation | 0.678 |
| Session open and state load | 0.513 |
| Pre-session input rebuild | 0.653 |
| Graph session | 3.682 |

Within graph session, runtime inputs took 1.128 seconds, including 0.536 seconds
for TS SessionContext. Two nested TS previews took 0.371 and 0.333 seconds.
Their map/aggregate windows totaled only 0.107 seconds. Each preview repeated
Nuxt package discovery (~0.100s), package gates (~0.061–0.064s), package names
(~0.058–0.061s) and package aliases (~0.056–0.058s). These nested values must NOT
be added to graph-session time. Repeated engine preparation and discovery are
concrete optimization targets; fewer AST scans alone cannot remove these costs.

## Confirmed no-op defect

Using the published-semantics binary after the target transition, three fresh
CLI deltas with unchanged sources took 3.825, 3.893 and 3.932 seconds. All parsed
zero files but each published 21 owners and 60,066 event bytes, advancing
completed generation 2 → 3 → 4 → 5. Replaying every replacement produced the
same graph hash after generations 2 through 5:
`7f29a20304ec5db22d9ab5752231919e8449f9165a83412980968a77290e5a18`.
This violates zero-event/no-generation no-op behavior. Its introduction commit
has not been isolated; do not attribute it to the latest scope checkpoint.

The changed Product manifest was `packages/tracking-client/package.json`:
only its own version changed from 0.7.1 to 0.7.2. Its cached manifests input
hash remained the old hash even after three successful runs. The unchanged-fact
fingerprint shortcut skips refreshing observed per-file hashes. Planning seeds
manifest owners and emits Begin before the later fingerprint comparison learns
that contributions are unchanged; late no-op suppression requires no prior Begin.

A two-file CLI reproduction uses `index.ts` containing an exported function and
`package.json` declaring a dependency. Changing only the package own version
reproduces repeated empty-effect replacements. A discriminator matrix confirms:

| Edit | Subsequent unchanged delta |
|---|---|
| Own version 1.0.0 → 1.0.1 | incorrectly publishes again |
| Revert own version to 1.0.0 | zero events, stable generation after revert run |
| Dependency constraint ^1.0.0 → ^2.0.0 | zero events, stable generation after changed-fact run |

Fix acceptance must cover observed-hash refresh, no-op detection before Begin,
real dependency changes, cold graph equality, frozen scope and failed-run recovery.
No production fix is included in this report.

## Evidence

`v297-stage-noop-diagnosis.json` embeds measured results, trace instances,
graph hashes and manifest mismatch, with paths and SHA256 of original evidence.
Full stderr resides in `/tmp/enola-small-delta-profile-detailed/`.
The initial no-op harness attempt used a /tmp versus /private/tmp sink identity
and failed before mutation; it is excluded from successful-run measurements.

The runnable `manifest_noop_probe.py` accepts `--binary` and a new `--out`
directory. It validates frozen protocol and independently compares applied output
against a fresh cold graph after every one of eight steps. Negative control on
the published-semantics binary passed all eight graph comparisons and failed
exactly the neutral-edit, repeated-no-op and revert publication assertions.
The regression is therefore unnecessary replacement, not a cold graph mismatch.
