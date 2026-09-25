# Stage19: deferred cached facts for summary-only CLI calls

Candidate `7b03158a2ca8da1858030acdca4d0a3cedefbc1f` remains unmerged. Correctness passed, but three paired measurements do not establish a consistent latency gain. No-op peak process RSS decreased by 8.41%; body-delta peak process RSS increased by 12.77%, with an increase in every pair. The memory concern remains unresolved. This checkpoint publishes evidence, not the candidate implementation.

## Scope and provenance

The candidate skips cloning cached TypeScript facts on proven non-publishing one-shot summary runs. Public full-result Run, full JSON and retained resident snapshots preserve their facts. Publishing summary runs reconstruct deferred facts before aggregation. The benchmark compares production-equivalent main against this isolated candidate; it does not combine unmerged Stage16–18 changes.

- Base: `5e145dd82fa2420b96f1c9ca78e4815d38180d15`; current main `ecd72b0` differs only in documentation.
- Product: `a609c19f3861971930fae7b33dcb2950598953c5`, same host and pinned TypeScript analysis policy.
- Candidate binary SHA-256: `50baa1a9816631899b724b2d8a32f36a11b58a8407f9fb86ff6c5436cdaa8477`, built with `-trimpath`.
- Three alternating pairs, six eligible arms, inside September 25 00:37–00:47 UTC. All arms completed before END at 00:44:21 UTC.
- Accuracy, Codata and the implementation worker explicitly confirmed the window. Codata paused all seven containers; root independently verified every paused state. VM preflight was 0.2–0.4% CPU, after earlier pre-window contention. Every arm recorded zero competing workload samples. Normal system activity is not claimed absent.

## Fresh CLI latency

Seconds below are median [minimum, maximum] across three runs. CLI completion includes producer acknowledgments and process exit; broker and consumer boundaries remain separate in the raw metrics. These are descriptive samples, not a statistical significance test.

| Mode | Baseline, s | Candidate, s | Median change |
|---|---:|---:|---:|
| initial | 8.334 [8.230, 8.413] | 8.281 [8.157, 8.489] | -0.64% |
| noop | 1.685 [1.628, 1.693] | 1.653 [1.605, 1.720] | -1.90% |
| body | 3.433 [3.423, 3.459] | 3.451 [3.397, 3.596] | +0.53% |
| structural | 3.416 [3.410, 3.425] | 3.432 [3.402, 3.639] | +0.47% |

All four latency ranges overlap; pair 2 is slower for the candidate in every mode. Candidate deltas still take about 42% of its initial run. A 1.653-second fresh no-op does not satisfy the near-zero startup goal. No resident/watch latency improvement is established by this CLI experiment.

Initial median broker End was 8.226 → 8.181 seconds; first batch was 5.810 → 5.847 seconds. See the raw comparison for spreads and consumer completion.

## Peak process RSS

MiB, median [minimum, maximum], measured consistently for each CLI process. RSS is not retained Go heap or allocation; the measurements do not establish the cause of the change.

| Mode | Baseline, MiB | Candidate, MiB | Median change |
|---|---:|---:|---:|
| initial | 898.62 [857.86, 908.92] | 879.06 [861.86, 906.52] | -2.18% |
| noop | 274.94 [266.80, 284.19] | 251.81 [249.56, 251.83] | -8.41% |
| body | 568.95 [543.39, 630.77] | 641.62 [627.81, 647.47] | +12.77% |
| structural | 595.48 [542.42, 631.61] | 607.23 [602.30, 607.89] | +1.97% |

Body-delta RSS increased by 10.35%, 2.65% and 18.08% in the paired runs. Do not repeat the same experiment solely to obtain a favorable result; investigate allocation and object lifetime first.

## Correctness evidence

- Both affected packages passed: graphsession 399.952 seconds and command 7.237 seconds on the strengthened tests. These test durations are not performance comparisons.
- Root independently passed nine graphsession regressions and the CLI routing test. Four actual CLI output modes preserved the expected full/summary result behavior and silent no-op state.
- Product initial/no-op/body/structural parsed-file counts remained 4034/0/11/12; file-owner scopes remained 8483/0/11/12 (no-op publishes no replacement).
- All 21 paired cross-arm graph comparisons matched. Every arm also validated delta against a fresh cold graph and silent no-op invariants.
- All 44 history CLI runs passed: 11 pinned revisions, 10 transitions, including three manifest transitions and no tsconfig transition. Each revision requires candidate chain = candidate cold = baseline cold, and zero no-op parses/events with unchanged state/generation. This does not claim coverage of a tsconfig-changing history.
- Mutation controls prove reconstruction and complete-fact assertions detect removed materialization and dropped fields; source was restored to the pinned patch.

## Next action

Keep the candidate separate while investigating the body-delta memory increase and larger fresh-start costs. The overall initial/delta/watch performance goal is incomplete. No production change is authorized for integration by this report.

## Artifacts

[Comparison](stage19/timing-comparison.json), [assessment](stage19/stage19-assessment.json), [history](stage19/history/FINAL-RESULTS.md), [source identity](stage19/product-final-source-equivalence.json), [quiet preflight](stage19/quiet-preflight.json), and [artifact hashes](stage19/artifact-sha256.json). Harness scripts and per-arm metrics, checks and receipts are retained under `stage19/`; their absolute paths preserve the original local setup and require that setup to replay.

Follow-up: [body-delta allocation diagnostic](STAGE19_RSS_DIAGNOSTIC.md) records a controlled shared-snapshot pair, a baseline cache nondeterminism finding, and the decision to park Stage19 integration. It does not supersede this timing assessment or close the RSS concern.
