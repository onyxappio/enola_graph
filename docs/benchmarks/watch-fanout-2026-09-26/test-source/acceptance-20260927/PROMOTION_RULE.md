# Full-profile fanout acceptance rule — fixed before timing

Six pairs ordered baseline/candidate, candidate/baseline, repeated three times. There are twelve arms with three of each order. Baseline is pinned published Stage22. Control is the 63c037d combined fanout/discovery binary from the failed prior cohort. Candidate7c4651d includes no-copy checkpoint JSON encoding, completed-directory error handling, missing Nuxt-config probe reuse and folded-case config-name fallback. Checkpoint bytes/fingerprints and graph/delivery contracts must remain exact. Same pinned full-profile Product tree, analysis configuration, broker settings and host; retain every attempted arm. No automatic replacement of rejected runs.

Correctness is mandatory: seven calls/four explicit checks per arm, exact normalized graph equality with cold and across arms, no-op zero parses/events/state change/generation advancement. Report actual parsed files, owner scopes and bytes independently. All broker acknowledgments must precede producer completion; first-batch and consumer commit times reported separately. Scope and delivery configuration differences are explicit: only new arms opt into changed-owner scope; every broker uses 8 MiB max payload.

For promoting the combined candidate versus published baseline: initial median improves at least 2%, each paired initial ratio is below 1; no-op/body/structural median regressions at most 2%; each scenario median peak-process RSS growth at most 5%; mean of four scenario median-time ratios below 1 and sum of those median seconds nonincreasing. Report medians, full ranges and all within-round ratios. Control is retained in binary provenance but is not measured in this new cohort; earlier control results cannot establish a new accepted ablation. Prior Stage23/24-only acceptance rules remain unchanged for those standalone experiments.

Timing requires a fresh explicit coordination window, no recognized competing heavy workloads, and whole-host process observations. An unverified or contaminated cohort cannot support acceptance. This small repeated engineering comparison does not establish statistical significance, resident-watch latency, old-upstream performance, history correctness or completion of near-zero no-op. Those remain separate requirements.

The prior failed cohort remains archived unchanged. This is a new candidate after a code change, not a replacement sample or relaxed RSS gate. Missing first-batch timestamp is now represented as null for graph-neutral Begin/End-only deltas; primary timings and thresholds are unchanged.

## Prospective measurement-policy change

All prior cohorts retain their original rejected verdicts. No samples are reused
or replaced. This protocol is frozen before acquiring new timing data.

The previous assistant-added absolute-zero global Swapins gate is removed for this
new experiment. Swapins are recorded before/after every arm and reported with
explicitly NO disqualifying effect, regardless of amount. This does not assert
that paging has zero or bounded latency impact: global counters cannot attribute
faults to Enola. Results are shared-host engineering observations, not isolated
causal estimates or statistical significance claims. The rule is fixed now, not
chosen after inspecting the next paging count.

Require zero new global Swapouts and normal memory pressure (Darwin dispatch
level 1) sampled every 250 ms throughout every arm, including correctness oracles.
Reject missing/error/non-normal observations, counter resets, uncovered interval
boundaries, and observation gaps greater than one second. Shorter transients
between samples may be missed. This pressure gate does not replace host workload
observations, participant holds, correctness checks or time/RSS thresholds.

Every one of SIX paired initial comparisons must favor the candidate, in addition
to the original >=2% initial median gain and all original scenario/aggregate/RSS
gates. Six-of-six is stricter than the prior three-of-three consistency condition.
No numerical type-I error or power claim is made. Report every individual arm,
full spread, ratios, paging context and failures; no automatic cohort refill.

Median thresholds use the ratio of medians, explicitly median(candidate)/median(baseline), not median(within-pair ratios). Every within-pair initial ratio must separately be less than one. All ratios are reported. This preserves the previous harness definition.

This new root pins7c4651d before any data. The earlier balanced9c7359f root never started a timing arm (preflight found local Codata workload); it remains unchanged. No samples are transferred. The complete bundle is compared to Stage22; no isolated Nuxt speedup is inferred.

All twelve arms must complete inside one contiguous explicitly acknowledged quiet window. Do not split, resume, or combine a cohort across separate windows; retain an interrupted cohort as incomplete evidence.

## Test-inclusive cohort 2026-09-27

This new cohort supersedes no prior verdict and reuses no timing samples. Candidate runtime is bdd7451; baseline remains published Stage22 c75e33e. Both receive the identical explicit test-inclusive scope YAML. All existing numerical time/RSS, correctness, pressure, spread and contiguous-hold gates remain unchanged. No control arm is timed; the startup correctness gate checks the actual same-scope baseline and candidate receipts instead of the historical, unmeasured control. Full110 candidate tests passed. Traced diagnostic is excluded.
