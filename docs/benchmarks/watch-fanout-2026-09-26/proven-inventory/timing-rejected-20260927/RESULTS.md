# Stage35: complete cohort, performance promotion rejected

All12 counterbalanced arms completed exit0 during14:20:00–14:36:04 UTC on
2026-09-27 inside the independently acknowledged14:20–15:25 window. All four
participants were explicitly released after completion. No sample was replaced.

Candidate177c0c5 versus published Stage33 runtime3f2883b, Product01baa6eb,
identical explicit test-inclusive scope, fresh CLI --summary-json. This is an
internally valid complete cohort that fails the frozen performance target,
not an interrupted or contaminated run.

No-op median improved1.0023%, below the required2%; the first paired no-op
regressed1.2319%, failing the all-six-improve condition. Five pairs improved.
All time/RSS regression bounds, aggregate bounds and correctness gates passed,
but they do not override the primary scenario. No threshold was relaxed.

| Scenario | Baseline median (range), seconds | Candidate median (range), seconds | Change |
| --- | --- | --- | ---: |
| initial | 11.575311 (11.170117–13.147226) | 11.494334 (11.306514–11.752673) | -0.6996% |
| noop | 2.055889 (2.013730–2.114955) | 2.035283 (2.021752–2.057982) | -1.0023% |
| body | 4.342838 (4.184829–4.419918) | 4.199799 (4.120436–4.261433) | -3.2937% |
| structural | 4.307031 (4.252865–4.475475) | 4.272206 (4.198829–4.470482) | -0.8086% |

| Scenario | Baseline median RSS, MiB | Candidate median RSS, MiB |
| --- | ---: | ---: |
| initial | 1241.258 | 1246.125 |
| noop | 352.266 | 361.711 |
| body | 849.883 | 854.789 |
| structural | 874.578 | 863.531 |

Actual parses remain6752/0/21/22 for initial/no-op/body/structural; cold and
incremental graph hashes agree within and across arms. All no-ops preserve
checkpoint bytes and generation and emit no events. Complete first-batch,
broker End, parsed counts, owner scopes, payloads and delta/initial ratios are
in timing-comparison.json and the per-arm metrics.

Host: normal pressure, no new Swapouts, valid pressure cadence and power
boundaries (AC100%, Low Power Mode0). Swapins occurred and are reported as
context. Shared desktop/VM/container activity persisted; host-audit.json is
descriptive and does not establish a dedicated idle host. Full raw host command
lines remain local, with hashes/paths in raw-host-log-manifest.json. Caffeinate
was launched with -i; boundary samples do not establish continuous AC power.

This rejection leaves Stage33 as the accepted runtime. Stage35 correctness and
profiling evidence remain useful, but its runtime is withdrawn from the working
branch before any main push. Independent arithmetic review was requested as
msg_31af590d035f; no further benchmark or sample selection is requested.
