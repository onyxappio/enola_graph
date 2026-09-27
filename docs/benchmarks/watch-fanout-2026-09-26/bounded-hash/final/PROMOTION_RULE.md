# Prospective bounded-hash candidate rule

Prepared before any whole-CLI acceptance timing for this candidate. Standalone file-read diagnostics and the first NATS correctness-only arm are excluded from acceptance.

Baseline must be the accepted guarded Stage37 runtime550e1fa (exact binary and source pinned before timing), not an older slower version. Same Product fba38bab revision, full test-inclusive configuration, summary-json and changed-owner-scope profile, same host and identical harness. Four workers with64KiB scratch each is the prospective candidate default: component diagnostics showed useful headroom with bounded memory/concurrency; eight-worker results do not authorize selecting a different default from CLI outcomes. Any runtime refinement must be frozen/reviewed before the cohort.

Primary fresh-process no-op: require median candidate/baseline<=0.97 and all six paired ratios<1, with six counterbalanced AB/BA pairs. No subsets, pooling or selecting a winning repetition. Non-primary initial/body/structural median regressions<=2%; every scenario median RSS regression<=5%; mean scenario median ratio<1; sum of scenario medians nonincreasing. Preserve full reported values and spreads, first-batch/broker-ACK completion, parsed files, owner scope, bytes and delta/initial ratios. Body is source-changed/fact-unchanged; structural tests changed graph payload. Do not conflate them.

Prerequisites: source review; complete suite; reader failure and same-size/restored-mtime controls; race checks for the new parallel path; exact cold/delta and cross-binary normalized graph equality; three real Product histories with source/config scope reporting; unchanged runs must parse/publish nothing, not advance generation or rewrite state. No filesystem metadata shortcut or additional relationship index.

One explicitly acknowledged quiet interval covers all12 arms. Retain Stage37 pressure/power gates: normal pressure every250ms, gap<=1s, zero new Swapouts, no counter resets; Swapins contextual; AC/battery>=20%/lowpower off at boundaries; caffeinate-i; complete raw host/competitor observation. Boundary power is not continuous-power proof. Foreign host load must be disclosed, not hidden by team-idle ACKs.

Any failed gate rejects the candidate; do not adjust thresholds after results. Component/warm-filesystem results do not prove full CLI, cold-storage, remote-filesystem or general-host gains. This is a limited engineering stage, not completion of near-zero startup or the wider delta/watch goal. No acceptance window has been requested or granted; no acceptance timing has started. Prototype is Go-overlay-only and not in main.

Source integration update before acceptance: source1af548c, clean buildcb6a69f, binarye9e48eb0 supersede the overlay-only status above; identical source bytes are proved in source-equivalence.json. All thresholds remain unchanged. Final-binary correctness is in progress.

Pre-timing prerequisite checkpoint: final clean binary CLI and all three historical receipts independently passed. Window requested for 2026-09-27 18:15–19:00 UTC; FSM/plugin/Codata acknowledged, reviewer acknowledgment and source verdict remain pending. No acceptance timing exists. This supersedes the earlier status sentences only; numerical gates are unchanged.
