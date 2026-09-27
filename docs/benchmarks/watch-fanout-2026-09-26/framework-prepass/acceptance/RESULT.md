# Stage32 combined candidate — limited engineering acceptance passed

Runtime 4859ee3 versus published Stage22 c75e33e, same pinned Product tree and
explicit test-inclusive full-profile scope. Twelve counterbalanced arms completed
2026-09-27 07:26–07:42 UTC inside the explicitly held 07:25–08:00 window.
All 84 CLI calls and cold/no-op gates passed; graph hashes match across arms.

| Scenario | Baseline median seconds (range) | Candidate median seconds (range) | Change | Median peak RSS MiB |
|---|---:|---:|---:|---:|
| initial | 12.365905 (12.141920–12.698190) | 11.500137 (11.443453–11.998135) | -7.00% | 1264.02 → 1247.78 |
| noop | 2.361592 (2.314254–2.401788) | 2.125511 (2.122477–2.136243) | -10.00% | 372.87 → 386.24 |
| body | 4.373035 (4.358916–4.452902) | 4.258745 (4.220997–4.377984) | -2.61% | 880.49 → 844.86 |
| structural | 4.411744 (4.378336–4.509794) | 4.312226 (4.273672–4.338174) | -2.26% | 940.82 → 829.17 |

Initial improved in all six pairs: -1.18%, -6.28%, -6.34%, -8.15%, -5.15%, -9.85%.
The ratio of initial medians is 0.929987, exceeding the required 2% gain. All
other time/RSS gates pass, mean scenario median ratio is 0.945332 and the sum of
scenario medians falls 5.60%. One body-edit pair is 0.22% slower; its median still
improves and the frozen gate is a scenario-median regression limit, not six-of-six
for body edits. Initial peak RSS falls 1.28%; no-op RSS grows 3.59%, below 5%.

Initial first-batch median is 8.288395 → 7.857174s; producer broker-End boundary
12.218674 → 11.393220s. CLI completion includes all producer acknowledgments;
consumer completion is separately retained in timing-comparison.json. Actual
initial parses remain 6,752; body edits parse 21 files and structural edits 22.
Candidate owner scope is 0 for the graph-neutral body edit (Begin/End only) and
1 for structural edits; that is not a claim of zero/one file parse. True unchanged
no-op remains zero parses/events with stable state and generation.

No recognized competing heavy jobs or host-sample errors. Pressure coverage
passed throughout, with zero new Swapouts. There were 6,991 Swapins (context
only under the rule frozen before timing); no claim of zero or bounded paging
impact. Host load1 ranged 5.60–14.84 and background CPU was variable, led by
WindowServer. This is a shared-host engineering comparison, not statistical
significance or isolated attribution of the full 7% to the prepass change.
Raw host logs remain at the paths/hash pins in host-audit.json.

The initial receipt-pin preflight abort remains archived separately with zero
Enola invocations; no samples were transferred or replaced. Both prior complete
rejected cohorts retain their verdicts. This passes limited bundle acceptance,
not the full goal: fresh no-op is still 2.13s, deltas about 4.3s / 37% of initial,
new-scope real histories and long watch still need verification. Main publication
has not happened at this checkpoint.
