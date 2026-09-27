# Framework summary prepass — experimental, acceptance pending

The session formerly collected fresh GraphQL/gRPC summaries sequentially before
parallel extraction. Collect these pure per-file summaries with the existing
GOMAXPROCS-bounded MapFiles helper, then merge them in the original tsFiles order.
This preserves duplicate gRPC stub precedence and cached summary reuse; it adds
no persistent index, changes no extraction rules, and does not narrow scope.

Diagnostic benchmark on clean Product 01baa6eb0cc2ea74018eafc07e230a29d4efa8f4,
Apple M1 Pro, Go 1.27.1, GOMAXPROCS=10: three repetitions of three iterations.
Serial median 475.231 ms (475.082–475.549), bounded-parallel median 74.438 ms
(73.933–75.196), about 84.3% less time for the prepass alone. Go allocations
remain approximately 1.35 MB/op; this excludes native parser memory and is not
peak RSS evidence. The 6,406 files are tracked TS/JS/SFC/SDL sources, not the configured
graph-profile inventory. Loading source bytes is outside timing. No quiet-window
or end-to-end speed claim is made from this diagnostic.

Reproduce with ENOLA_FRAMEWORK_BENCH_REPO pointing at the pinned clean Product:

```sh
go test ./internal/extractors/tsextractor -run '^$' -bench '^BenchmarkFrameworkSummaryProduct$' -benchtime=3x -count=3
```

Focused GraphQL/gRPC/minified/session tests passed (1.022s). The full TS suite also passed with the race detector (213.092s, exit0).
Product cold/delta correctness passed: seven CLI calls, four gates, and all seven
graph hashes equal the cf71c85 candidate. Full repository suite passed (110 packages, 91 cached, exit0, 447.976s).
Fresh frozen acceptance remains required. Both earlier rejected cohorts remain unchanged. Not published.

Experimental binary: `/tmp/enola-framework-prepass/enola-experimental`, SHA256
`980e849a72e41570af21446d7f69f1624af0ed5967d9db14aaf8f9ae40bd0716`.
Runtime change is the session.go working diff from 9faa0fb; its hash is pinned in
`/tmp/enola-framework-prepass/correctness/cli-pairs/pins.json`.
Product correctness harness and full TS race check were started independently;
their overlapping runtime is explicitly not performance evidence. Both completed exit0. Product checks and all seven cross-build graph hashes were
independently verified; the receipt explicitly marks timing_eligible=false.
Runtime source is now committed as 4859ee3. Full repository suite completed exit0 at
`/tmp/enola-framework-prepass/full-suite` (session38781).

The first 07:25 attempt aborted at the receipt hash preflight, before a Product
clone, broker or Enola invocation. The old full-suite receipt hash had not been
updated after the new suite passed. The failed attempt is preserved in
preflight-abort/. A fresh acceptance-v2 root corrects that one inspected hash;
both pin preflights pass and its complete twelve-arm series is running inside
the same acknowledged 07:25–08:00 UTC interval. No samples were transferred.

Acceptance-v2 completed all12 arms, exit0, within the hold. All frozen gates
passed; see acceptance/RESULT.md. Initial median12.365905→11.500137s (-7.00%),
no-op2.361592→2.125511s (-10.00%). Both deltas improve, but remain approximately
4.3s and37% of initial. This is limited combined-bundle acceptance, not full-goal
completion. New-scope real historical validation has started; no main push yet.
