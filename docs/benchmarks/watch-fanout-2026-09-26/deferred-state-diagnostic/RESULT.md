# Deferred facts decode — diagnostic, not production-ready

Same Product checkpoint, three repetitions of three iterations, shared host; no CLI or quiet acceptance claim. JSON projection defers payloads for TS/file facts and synthetic/contrib fields into RawMessage. Re-encoding and full decoding reconstructs the exact State (reflect.DeepEqual), checked outside timing.

| Decoder | Median ms/op | Range ms/op | Bytes allocated/op | Allocations/op |
|---|---:|---:|---:|---:|
| Full | 384.556 | 382.498–384.677 | ~180.25 MB | ~2.569 million |
| Projection | 226.423 | 225.903–227.813 | ~114.29 MB | ~0.574 million |

The 158.133 ms reduction is 41.1% of decode only; it is not a whole-CLI speedup and cannot establish near-zero no-op. The input scanning/policy costs remain. Facts bytes are retained, not an added relationship index.

Critical counterexample: valid JSON with facts:42 is rejected by the full State decoder but accepted by RawMessage projection. TestDiagnosticProjectionDefersInvalidPayloadType proves this gap explicitly. This experiment must not replace the production reader. A candidate must retain equivalent payload validation and hydrate before any metadata refresh/save, changed-input analysis, pending promotion requiring full state, or full API return. No schema or runtime change made. Diagnostics used a Go overlay, leaving the Stage36 publication tree untouched except this evidence.

## Typed-payload follow-up: reject the straightforward approach

A separate three-repeat/three-iteration diagnostic additionally unmarshals every retained payload using the real facts.Fact type. Median full decode 383.071 ms, projection 226.934 ms, projection plus typed payload decode 498.118 ms (range495.555–510.955), ~243.25 MB/op. The straightforward validation path is about30% slower than the full decoder, so it is rejected as an optimization. It also does not establish duplicate-field error equivalence; these measurements do not assert production-readiness. Both variants remain overlay-only diagnostics.
