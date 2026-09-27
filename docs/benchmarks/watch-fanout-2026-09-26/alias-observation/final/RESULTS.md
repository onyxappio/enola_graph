# Stage37: limited fresh CLI no-op improvement

Full test-inclusive Product `fba38bab93c76a4da8df58cf45fd6847a468c315`; published Stage36 runtime `7812838` versus guarded candidate `550e1fa`, clean build checkout `02df91a`. Twelve arms / six counterbalanced AB/BA pairs completed in the acknowledged 17:10–17:55 UTC window on 2026-09-27. Session31471 exited0; hold released at17:31 UTC.

## Measurements

| Scenario | Baseline median (range), s | Candidate median (range), s | Median change |
|---|---:|---:|---:|
| initial | 11.341549 (11.233719–11.738684) | 11.335263 (11.200056–11.540592) | -0.055% |
| noop | 2.085532 (2.042627–2.167941) | 2.025774 (1.992064–2.037388) | -2.865% |
| body | 3.814083 (3.787510–3.961449) | 3.789999 (3.732021–3.906647) | -0.631% |
| structural | 4.325383 (4.203257–4.453091) | 4.232445 (4.153627–4.337578) | -2.149% |

No-op wins in all six pairs: 2.399%, 0.256%, 5.964%, 6.747%, 2.797%, 3.212%. The primary median reduction is about60 ms; frozen ≥2% gate passes. No median time/RSS regression gate failed. Mean scenario median ratio0.985748; sum medians−0.849%. Exact numbers, samples, first batch, broker End, consumer End, owner scope, parses and delta/initial ratios are in `timing-comparison.json`.

Candidate parsed initial/noop/body/structural:6749/0/1/22. The body edit changes source but not graph facts: its frozen replacement scope is empty, Begin/End are published with zero fact batches and generation advances; it is not a test of a changed fact payload. Structural scope is one owner; initial scope11284 owners. No-op is distinct and publishes nothing. All84 CLI calls and48 checks passed, including cold/delta and cross-arm graph equality and silent no-op. Fresh CLI time includes process exit and producer ACKs; this is not a resident/watch measurement.

## Status and limitations

Frozen engineering gates pass; independent complete-cohort review msg_a2041212da30 reproduced the raw figures, pins and every criterion with no blockers and recommends bounded acceptance. Runtime publication follows normal hooks. The broader near-zero no-op / substantially faster delta objective remains open. Initial and body ranges overlap and show no meaningful gain claimed here; no significance claim.

Foreign CPU is variable, including siriactionsd, WindowServer and virtual machine work. `host-audit.json` reports argument-free per-arm summaries; raw ps logs remain local and their SHA256 identities are archived, avoiding publication of full process arguments. There were no host sampling errors; host cadence is distinct from the stricter pressure gate. Do not describe the host as universally clean. AC/charge/low-power checks are at arm boundaries only; caffeinate-i was used, not continuous-power proof. All3606 pressure samples were normal, max gap0.279919s; zero new Swapouts, Swapins+1288 are contextual only. Details are in the series and comparison receipts.

Final full suite:110 passing packages,99 cached; graphsession uncached512.044s. Three real histories passed24 CLI calls with exact endpoint equality; delta parses3/14/1453. These shared-host history runs establish correctness, not history performance acceptance or exhaustive coverage.

Prototype outer-directory files are superseded by this final guarded candidate. The first final source-history preflight failed before workload due to /tmp versus /private/tmp pin relocation; corrected v2 logs, original CLI pins and independent byte-equivalence audit are retained. Pin repair preceded acceptance timing. `PROMOTION_RULE.md` intentionally retains its pre-timing status text; timing-window and these results supersede that status, without changing thresholds.

No new persistent relationship index, cache schema, protocol or resolver semantics. Completed discovery directory observations are reused only inside the same build; missing observations fall back to live reads.

## Raw-host arithmetic follow-up

Independent primary recomputation from84 metrics reproduces every numerical gate; primary-audit.json also verifies host sample counts and reports summed foreign CPU. Mean foreign CPU baseline/candidate pairs:184.75/176.52,90.68/94.86,214.67/194.98,242.86/220.98,169.43/160.05,89.68/89.50 percent (sum across processes, may exceed100%). Several pairs favor the candidate in background load. This is a limitation of shared-host inference, not grounds for discarding selected pairs or claiming clean-host causality; the preregistered six-pair decision is unchanged.

## Independent acceptance and publication caveat

The complete review is retained in cohort-review.json. Foreign CPU favored the candidate in five of six pairs, so the full2.87% must not be attributed cleanly to this mechanism alone; attribution and repeatability remain limited to this shared-host cohort. Sensitivity calculations in the review are exploratory, not causal estimates or alternative gates, and no pairs were removed from acceptance. Initial/body changes are within noise; structural and no-op have consistent signs. The traced alias phase localizes saved work (73→16ms) but two single instrumented total CLI times do not establish a speedup.

Wire payload byte differences are fully explained by candidate run identifiers being one character longer than baseline identifiers, not changed graph content. Raw acceptance pins and VALIDATION-STATE.json intentionally retain historical pre-timing text; STATUS.md and this report give the current verdict. The full-suite receipt explicitly reports99 cached packages and11 uncached package executions; no all-uncached claim or rerun is needed. The outer prototype042a0c6e is superseded by final guarded15f55895.
