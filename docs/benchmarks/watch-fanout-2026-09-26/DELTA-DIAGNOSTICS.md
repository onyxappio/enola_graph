# Delta causality diagnostics — proposal, not implemented

User question: can production telemetry explain two edited files expanding to
half the graph? Current code provides partial evidence, not a complete causal log.

## Existing evidence

- BeginReplace: RunID, generations, scope mode, owner scope/count/digest.
- EndReplace.Completeness: actual parses/cache/summary counts, parsed-by-reason,
  fallback extractor/scope/reason.
- graphsession.Result.Invalidation: config/policy changes, added/removed source
  counts, affected-context count/reasons, parse reasons.
- No durable per-run changed-path-to-expanded-owner explanation is represented
  as product graph facts. Retaining NATS events is a consumer/retention concern;
  their availability cannot be inferred merely from current graph state.

## Proposed audit record

Join with protocol events through repo/context/run and base/target generation.
Record observed changed/added/removed input paths, config/policy changes, planner
mode and expansion reasons, frozen owner scope count/digest, actual parse count
and reasons, cached contribution reuse, emitted node/edge counts and stage time.
Differentiate observed content changes from Git diff and from changed functions;
function-diff analysis is not currently provided. Also record aborted attempts
without presenting them as completed generations.

Keep these operational records outside product graph facts, with bounded
retention and size limits; attach large path details to a bounded artifact rather
than growing Begin payloads without limit. No new persistent relationship index.
Exact per-owner causal chains are not currently available and must not be claimed
from aggregate fallback reasons. Instrument existing planning decisions first.

Scope replacement, reparsing, and emitted record volume are separate metrics.
A large cache-served replacement can be cheap to parse yet costly to transport
and consume. Ratios need explicit zero-input handling for recovery/config-only
runs and a denominator defined against the same owner/file domain.

This proposal does not modify the frozen Begin contract or the main graph schema.
Implementation and overhead measurements remain outstanding.
