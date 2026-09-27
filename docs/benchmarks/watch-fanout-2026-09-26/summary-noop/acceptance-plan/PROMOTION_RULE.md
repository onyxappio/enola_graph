# Prospective summary-noop promotion rule

Written before any acceptance timing for this candidate. Earlier Stage32 and
other cohort verdicts remain unchanged; none of their samples enter this cohort.

Target runtime: 3f2883b (index ancestor deduplication plus one-shot summary
assembly), binary ec5143e824760312920a1760d7674ec4a010f329dce44b7abbc11b648cf19fef.
Baseline: published main 3b99244, runtime 4859ee3, frozen binary
980e849a72e41570af21446d7f69f1624af0ed5967d9db14aaf8f9ae40bd0716.
Both use --summary-json, --changed-owner-scope and identical explicit test-inclusive
scope on the same pinned Product tree. This comparison measures the combined
new changes, not isolated lazy assembly. State is fully decoded in both arms.

This stage targets fresh-process unchanged CLI latency. Unlike the earlier
initial-focused stage, the primary scenario is noop. Promotion requires:

- Six counterbalanced pairs (AB, BA repeated three times), all twelve arms in
  one explicitly acknowledged quiet interval; no refill or cross-window pooling.
- Noop ratio of medians <=0.98, with all six paired noop ratios <1.
- Initial, body and structural median time regressions <=2%; every scenario's
  median RSS increase <=5%; mean scenario median ratio <1 and the sum of median
  scenario times does not increase.
- Full candidate repository suite and same-scope baseline/candidate correctness
  checks pass on the pinned source/binaries. Every arm independently verifies
  full/cold equality, broker completion and silent zero-parse/event no-op with
  unchanged generation and checkpoint bytes. Cross-arm graph hashes must match.
- Same host controls as Stage32: complete competitor/host samples, normal pressure
  at 250ms cadence with <=1s gaps, zero new Swapouts, no counter resets, valid
  boundaries and pins. Swapins are context, not evidence of zero paging impact.
- Report all observations, spreads, first batch and broker End, parsed counts,
  memory, absolute latency and delta/initial ratios, including rejected results.

These are limited engineering promotion gates, not statistical significance,
near-zero fresh-noop completion, resident-watch latency, old-upstream comparison
or complete current-main historical coverage. Those obligations remain open.
No quiet window has been requested or granted for this cohort yet.
