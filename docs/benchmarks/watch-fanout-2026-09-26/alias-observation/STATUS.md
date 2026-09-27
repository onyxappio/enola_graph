# Alias directory observation candidate — experimental

Base main 9bf667d. Reuse completed directory observations already held by the TS discovery probe for the alias-only walker. No additional retained index, state schema, resolver rule, or protocol change. The configuration inventory callback retains live directory entries. Unknown/unobserved directories retain the live reader; testdata traversal is preserved.

Six alternating Product microdiagnostic pairs with the watch scope configuration: live traversal 65.710–68.322 ms, observed traversal 13.804–14.868 ms; alias outputs and read/directory ledgers match. This is shared-host component timing, not whole-CLI acceptance. The earlier unscoped diagnostic is archived separately and is not comparable to the configured graph profile.

Full TS extractor suite passed in 22.571 s; additional captured-absent-config regression passed. Current Product watch harness exited 0, initial/delta parses 6749/1, owner scopes 11285/1, zero idle/duplicate events and abandoned Begins. Stable inventory across cold: 45467 files. Original and mutated hashes match the prior Stage36 diagnostic as well as candidate cold.

Outstanding: source review of snapshot/fence safety, full-suite result, historical regressions, repeated whole-CLI timings with memory gates. No runtime publication or overall completion claim. Full suite currently session 27828, log /tmp/enola-alias-observation/full-suite.log. Reviewer request msg_69be161130ba.

Prepared additional real source-only first-parent history (585a910d → 3bee95af, two TS files including one test; no manifest/config), alongside prior current-main endpoint scenarios. Harness gate self-tests: 51 passed. Source-only history session 87096 is live; no result claimed. CLI preflight verified pins and clean Product fba38bab without executing timing. Prospective promotion rule is frozen before CLI acceptance samples: no-op median >=2% improvement, all six pair wins, unchanged nonprimary/RSS/host gates. No quiet interval requested.

Source-only history completed exit0: all eight calls and both endpoint chain/cold/baseline equality passed; delta parsed3 for two changed sources, no-ops silent with unchanged state. Two remaining histories now run sequentially under live session36218 (recent then broad). Full suite session27828 remains live.
