# Index reuse and observed manifest inputs: intermediate checkpoint

This checkpoint removes repeat manifest extraction after graph-neutral edits and
reuses context-free export indexes from existing ASTs. It also adds opt-in stage
timing. It does not complete the initial/delta/watch performance objective.

The semantic changes to extraction are intended to be zero: index adoption uses
the same bytes and grammar, rejects resolution-dependent forwarding, excludes
transformed Ember input and early-return SFC formats, preserves empty-file markers,
and fills the existing per-file single-flight cache without replacing prior entries.
Derived indexes are counted separately from actual summary scans.

The manifest output-fingerprint shortcut now records the input hashes it proved
unchanged, including newly added factless inputs and retired inputs. This prevents
repeated no-op runs from perpetually publishing the same contributions. The first
graph-neutral edit and its revert still publish a replacement; suppressing that
publication before Begin remains an explicit open requirement.

## Product evidence

Same Product transition and configuration as V297_STAGE_NOOP_DIAGNOSIS.md.
Both arms are timing-instrumented. These are sequential shared-host diagnostics,
with concurrent worker tests; they establish no stable latency improvement.

| Measurement | Control | Candidate |
|---|---:|---:|
| Initial fresh CLI | 36.220 s | 36.767 s |
| Delta fresh CLI | 5.589 s | 6.284 s |
| Cold target fresh CLI | 37.577 s | 39.621 s |
| Initial full parses | 4,012 | 4,012 |
| Initial summary scans | 3,004 | 1,648 |
| Initial derived indexes | unavailable | 2,318 |
| Delta full parses | 4 | 4 |
| Delta summary scans | 15 | 15 |

Candidate cold summary scans were 1,635 with 2,331 derived indexes; which caller
wins the single-flight entry can vary with scheduling, so these counters describe
actual work in that run, not a deterministic guarantee. Initial reduced summary
scans do not establish reduced elapsed time. Both arms produced the exact same
target graph, and every candidate replacement passed frozen protocol validation.

A separate Product stale-state recovery run still published once, then two unchanged
fresh CLI invocations emitted zero events and kept generation 6 unchanged. They took
3.203 and 3.106 seconds, which remains far from the requested near-zero no-op target.

## Independent correctness evidence

- Eight-step CLI probe: all exact-cold and frozen-protocol comparisons passed.
  Assertions for the graph-neutral edit and revert still failed (steps 1 and 3);
  subsequent no-ops passed, including step 2 which failed on the published baseline.
- Concurrent adopt/index access and side-read provenance passed the race detector.
- Empty-byte index mismatch was detected independently, corrected and rechecked.
- Resident failed-End tests for version edits and added factless manifests passed
  with the race detector in 7.587 seconds. They check byte-identical committed
  memory/disk state after failure, replay recovery, exact cold equality using the
  same authoritative mode and a silent following no-op.

The first failure-recovery harness incorrectly compared authoritative mode with a
non-authoritative cold helper. That test-only mismatch was corrected; it is not
claimed as a production defect.

## Package validation status

The earlier package run passed tsextractor, graphsession, engine and pkg/command.
After the added-factless-input correction, a subsequent run returned failure in
pkg/command, but its error text was truncated by the worker log pipeline. Its cause
is unknown; low disk space does not prove an environmental cause. An isolated
pkg/command rerun passed. Final-byte reruns with complete logs and zero exit codes
passed graphsession (186.281 s), pkg/command (7.529 s), tsextractor (30.665 s)
and engine (26.776 s). The earlier truncated failure remains unexplained.

## Remaining work

Detect graph-neutral changes before Begin; share captured discovery/context results
across planning and extraction; remove redundant whole-repository setup from ordinary
resident delta/watch; optimize fresh CLI startup separately; narrow genuinely affected
framework/package scopes; finish repeated broker-acknowledged performance and long
watch recovery/backpressure acceptance. This checkpoint is not a completion claim.

Raw identities and embedded measurements are in v297-index-noop-checkpoint.json.
