# Per-run shared discovery integration

This checkpoint shares TypeScript discovery within one run: context projection and planner previews consume the same captured repository metadata when their inputs agree. A changed capture rebuilds discovery, and the actual rebuild is counted. Discovery is discarded before the next resident fast run; cross-run retention is not part of this change.

The integration preserves accuracy wave 9 through v304 from main `3013ec9`. A three-way merge against `365d150` preserved the newer lexical/CommonJS code in `ts.go`; the combined `lexical_call_test.go` is byte-identical to current main. The other candidate files match the frozen stage2 manifests. No production files outside that manifest are replaced.

## Validation

Targeted wave 9, independent cold-equivalence and discovery tests passed: TypeScript extractor 0.554 s, graphsession 48.714 s. Full TypeScript extractor, graphsession, engine and CLI entrypoint suites passed in 230.634 s wall time. The command implementation suite passed separately in 5.384 s. The complete `go test ./...` run also passed in 298.615 s wall time. Package-level timings are preserved in the adjacent integration receipt.

## Performance scope

The original candidate measurements are in [the candidate report](V297_SHARED_DISCOVERY_CANDIDATE.md). Fresh CLI no-op median improved from 2.913 s to 2.779 s in three alternating runs. Watch save-to-consumer latency remained about 6.6 s including the five-second window; initial measurements were inconclusive. Those measurements used the pre-wave-9 candidate binary and must not be presented as measurements of this merged build.

This is an intermediate integration, not final performance acceptance. Remaining work includes proven cross-run retention, reduced CLI preparation/state overhead, new matched measurements, Product history regressions and long-running watch recovery/memory acceptance.
