# Native quiet investigation after b1820aa

## Outcome

No production native source or control-metadata change is warranted by this reproduction. The failed raw quiet diagnostic included real benchmark output writes in the directory watched for the external config. The coordinator repaired the harness by moving config.yaml into a dedicated config/ parent; native raw counters retain their existing, honest semantics.

Added only `internal/graphsession/native_quiet_test.go`, covering shared versus isolated external-config/output parents. No Product file was read for profiling, edited, or analyzed by this worker. No transport files changed, no commits or pushes performed; other workers' uncommitted files preserved.

## Cause and evidence

The original benchmark external config was `/tmp/enola-postpush-isolated-resident/config.yaml`. CoverSessionInputs registers its parent; fsnotify/kqueue observes the parent's children, including files which relevantEvent correctly filters from the graph queue. `ObservedEvents` intentionally counts those filtered deliveries too.

`resident.py` online() receives the driver's raw-counter response, then invokes save(), writing `/tmp/enola-postpush-isolated-resident/metrics.json` and `/tmp/enola-postpush-isolated-resident/checks.json`, and prints progress to its caller's stdout. Therefore quiet-before itself performs writes after sampling the counter and before quiet-after. If stdout is redirected to another file in that parent it contributes further events. The saved original run has no per-event trace, so attributing every historical extra event individually is impossible; the exact output operations and reproducible mechanism establish the cause without pretending the old log contains missing evidence.

Original quiet counter pairs were 49→54, 53→58, and 52→56. Across idle requests counters grow by approximately 4–5 each, while the graph queue remains From=Through=10, with no paths/reconciliation and zero graph work. This is finite activity coupled to request output, not evidence of an unbounded read/atime feedback loop.

Native fsnotify reproduction on this Darwin host (`/tmp/enola-native-quiet-trace.go`, output `/tmp/enola-native-quiet-trace.log`) watched a temporary external config parent and rewrote pre-existing metrics.json, checks.json and suite.log siblings. Exact deliveries:

```
CHMOD       /var/folders/69/n9gknnz11gq1gwnvwt1rlt200000gn/T/enola-native-quiet-trace-3521188221/metrics.json
WRITE       /var/folders/69/n9gknnz11gq1gwnvwt1rlt200000gn/T/enola-native-quiet-trace-3521188221/metrics.json
WRITE|CHMOD /var/folders/69/n9gknnz11gq1gwnvwt1rlt200000gn/T/enola-native-quiet-trace-3521188221/checks.json
WRITE|CHMOD /var/folders/69/n9gknnz11gq1gwnvwt1rlt200000gn/T/enola-native-quiet-trace-3521188221/suite.log
```

Four deliveries during the 300ms write window; zero during the next 300ms without writes. The same operation with only a dedicated config/ subdirectory watched produced zero deliveries in both windows. Temporary fixture directories were removed afterward. Event coalescing can vary; correctness does not rely on an exact delivery count.

The new regression uses production FileChangeSource plus CoverSessionInputs and a resident with a real external TS config dependency. It proves exact-path delivery for both report siblings in the shared layout, unchanged raw count in the isolated layout, and stable raw counts for a bounded 300ms after writers stop. Both layouts additionally require covered unchanged queue watermarks, all-zero WorkCounters, zero parses/publication, unchanged generation, real atomic external-config replacement detection/reconciliation, and exact resident-versus-cold graph equality before and after replacement. It intentionally requires shared-layout output writes to remain visible to raw diagnostics; moving the observation counter behind filtering would break the test.

## Validation

Toolchain: `/tmp/enola-toolchain/go/bin/go` (go was not on this terminal's PATH).

- Existing controls/external coverage regressions, repeated 3 times:
  `/tmp/enola-toolchain/go/bin/go test ./internal/graphsession ./pkg/bootstrap -run 'TestGitControlMetadataRequiresIdenticalCapturedBytes|TestScopedNativeGitMetadataSettles|TestExternalCoverageIgnoresSiblingLogsAndDetectsAtomicConfig' -count=3`
  PASS: graphsession 3.186s; bootstrap 6.520s.
- New native layout regression, repeated 3 times:
  `/tmp/enola-toolchain/go/bin/go test ./internal/graphsession -run TestNativeQuietExternalConfigOutputLayouts -count=3 -v`
  PASS, 13.655s; `/tmp/enola-native-quiet-tests.log`. Shared layout grew by 2–3 events for two report writes; isolated layout stayed 0→0 in all three runs.
- Race checks:
  `/tmp/enola-toolchain/go/bin/go test ./internal/graphsession -run 'TestNativeQuietExternalConfigOutputLayouts|TestGitControlMetadataRequiresIdenticalCapturedBytes|TestExternalCoverageIgnoresSiblingLogsAndDetectsAtomicConfig' -race -count=1`
  PASS, 6.860s; `/tmp/enola-native-quiet-race.log`.
- Native metadata regression includes real Git index CHMOD and a quiet raw-counter window. Passing repeatedly gives no evidence of the suspected control byte-read feedback on this host.

## Remaining work

Coordinator owns the isolated-config Product resident rerun after other profiling permits, including full Product cold equality and raw quiet diagnostics. Fixture evidence establishes the repaired layout and production filtering behavior, not Product performance acceptance or an instantaneous backend barrier. No native semantics, observation-counter filtering, or meaningful-event suppression was changed.
