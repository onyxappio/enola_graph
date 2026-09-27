# Alias directory observation candidate — experimental

Base main 9bf667d. Reuse completed directory observations already held by the TS discovery probe for the alias-only walker. No additional retained index, state schema, resolver rule, or protocol change. The configuration inventory callback retains live directory entries. Unknown/unobserved directories retain the live reader; testdata traversal is preserved.

Six alternating Product microdiagnostic pairs with the watch scope configuration: live traversal 65.710–68.322 ms, observed traversal 13.804–14.868 ms; alias outputs and read/directory ledgers match. This is shared-host component timing, not whole-CLI acceptance. The earlier unscoped diagnostic is archived separately and is not comparable to the configured graph profile.

Full TS extractor suite passed in 22.571 s; additional captured-absent-config regression passed. Current Product watch harness exited 0, initial/delta parses 6749/1, owner scopes 11285/1, zero idle/duplicate events and abandoned Begins. Stable inventory across cold: 45467 files. Original and mutated hashes match the prior Stage36 diagnostic as well as candidate cold.

Outstanding: source review of snapshot/fence safety, full-suite result, historical regressions, repeated whole-CLI timings with memory gates. No runtime publication or overall completion claim. Full suite currently session 27828, log /tmp/enola-alias-observation/full-suite.log. Reviewer request msg_69be161130ba.

Prepared additional real source-only first-parent history (585a910d → 3bee95af, two TS files including one test; no manifest/config), alongside prior current-main endpoint scenarios. Harness gate self-tests: 51 passed. Source-only history session 87096 is live; no result claimed. CLI preflight verified pins and clean Product fba38bab without executing timing. Prospective promotion rule is frozen before CLI acceptance samples: no-op median >=2% improvement, all six pair wins, unchanged nonprimary/RSS/host gates. No quiet interval requested.

Source-only history completed exit0: all eight calls and both endpoint chain/cold/baseline equality passed; delta parsed3 for two changed sources, no-ops silent with unchanged state. Two remaining histories now run sequentially under live session36218 (recent then broad). Full suite session27828 remains live.

Full suite session27828 exited0: 110 passing packages,99 cached, graphsession uncached465.383s; exact log/receipt archived. Recent mixed history accepted all eight calls: delta14 parses and endpoint chain/cold/baseline equality, silent no-ops. Wide history remains live in session36218. Whole-CLI correctness arms candidate then baseline are running under session86562. Prospective summary validator passed synthetic cohort tests, rejecting incomplete/out-of-window/missing-power/pressure/swap/graph-drift cases; those are validator tests, not timing evidence.

All three history scenarios completed: source-only/recent/broad delta parses3/14/1453; 24 CLI calls total. Broad initial parsed6716; all endpoints candidate chain/cold and Stage36 cold hashes equal, no-ops silent with unchanged generation/state. Session36218 exited0. These are correctness-only histories, not performance evidence or exhaustive repository history coverage.

CLI correctness candidate completed seven calls/four checks, independently revalidated: initial/noop/body/structural parses6749/0/1/22, exact own cold equality and silent no-op. Baseline arm remains live under session86562; cross-arm validation pending. Shared-host timings are not acceptance samples.

CLI correctness session86562 exited0: both arms completed7 calls/4 checks; all seven graph hashes match cross-arm. Both no-ops prove zero parses/events, unchanged generation and exact state bytes. Source reviewer independently observed actively reading scope.go at this checkpoint; review pending, not unavailable. No heavy primary jobs remain. No timing interval requested.

Build provenance: candidate binary was built before the runtime commit, from dirty base9bf667d (Go embedded vcs.modified=true). Its production ts.go and alias_observation.go SHA256 values match the build-time pins and committed fca10d8 sources; subsequent changes were tests/docs only. fca10d8 names the source snapshot, not the embedded binary VCS revision. Binary SHA remains042a0c6e1580e62389913c7cb646377a05c1942b0c1acf44962c05581f4550eb.

Proposed new quiet window17:10–17:55 UTC requested from FSM/Codata/plugin/reviewer; pending ACKs and source review, no timing authorized or started. Request IDs archived in timing-window-pending.json.

Review msg_bda5197c3be7 found no quiescent-tree output defect and requested cheap policy guard hardening. Implemented as550e1fa: scope.Allowed(dir,true), one-policy-per-probe and first-enumeration comments; new regressions pin stale creation, completed empty/removal and directory refusal. Targeted tests passed0.848s. Earlier fca10d8 results remain preliminary; final guarded revision needs fresh validation and binary pin. All four participants acknowledged availability17:10–17:55 UTC (Codata terminal evidence due dispatch_run_mismatch); timing still conditional on final validation/review.
