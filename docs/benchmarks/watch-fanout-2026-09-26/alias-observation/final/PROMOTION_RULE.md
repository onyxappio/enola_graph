# Prospective alias-observation promotion rule

Candidate runtime fca10d8; baseline published Stage36 runtime 7812838. Full test-inclusive Product fba38bab93c76a4da8df58cf45fd6847a468c315, identical summary-json/changed-owner-scope options. Frozen before any whole-CLI acceptance samples.

Primary: fresh-process no-op. Require median candidate/baseline ratio <=0.98 and all six paired ratios <1 over six counterbalanced pairs (AB, BA repeated three times). No re-selection or pooling. Initial/body/structural median regression <=2%, every median RSS regression <=5%, mean scenario median ratio <1 and sum of scenario medians nonincreasing. Two percent is a limited engineering target justified by the ~52 ms component opportunity against ~2 s startup; it is not near-zero no-op or completion of the broader goal.

All twelve arms require one explicitly acknowledged quiet interval, frozen pins, full suite and historical correctness, broker completion, cold/delta and cross-binary equality and silent no-op. Report all samples/spread, first batch, broker End, parses, owner scopes, RSS, and delta/initial ratio. Preserve Stage36 host/pressure/power gates: complete host observations, normal pressure every250 ms with gaps <=1s, zero new Swapouts, no counter resets, AC/charge>=20%/low-power off at boundaries; Swapins context only; caffeinate -i. Boundary power is not continuous power proof.

No interval requested or granted. No acceptance timing started. Component diagnostics and correctness watch timings are excluded. Source review remains required. A failed gate rejects this runtime candidate; do not weaken a frozen gate after timing.

Final guarded source550e1fa supersedes prototype fca10d8 before any acceptance samples. Only directory admission guard/comments/tests changed; primary, thresholds, repetitions and host gates stay unchanged. Clean build checkout02df91a.
