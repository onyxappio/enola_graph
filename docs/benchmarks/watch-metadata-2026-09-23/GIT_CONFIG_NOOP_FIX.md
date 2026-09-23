# Git configuration no-op correction

A harmless local Git configuration write used to trigger a complete replacement of an unchanged graph. This replacement could parse zero TypeScript files while still republishing every owner. The fix hashes effective Git discovery and tracked admission decisions instead of config bytes. Raw config dependencies remain watched and fenced during a transaction. `config.worktree` is also declared, including when absent, so linked-worktree redirects cannot bypass that fence.

## Real CLI watch check

An isolated seven-owner fixture ran the initial analysis, eight harmless local configuration edits, then forced tracking of a previously ignored TypeScript file. The same sequence was run with an independently built pre-fix binary.

| Phase | Fixed | Before fix |
| --- | --- | --- |
| Initial | 1 completed replacement; 6 TS parses | same |
| Eight config edits | 0 records; 0 attempts; 0 parses | 7 records: 1 completed replacement with 0 parses, plus 2 interrupted attempts |
| Track ignored source | 1 completed replacement; 1 TS parse | same |
| Final completed generation | 2 | 3 |

The fixed watch probe took about 20 seconds including explicit observation waits. This is a correctness probe, not a latency benchmark. All completed replacements passed the frozen owner manifest, owner count/digest, raw batch digest/count and completeness checks. Applying every replacement through the test consumer produced exact equality with a cold analysis of the same fixture, preserving all node/edge identities and repository fields. Interrupted attempts were not counted as completed generations. Root independently verified both streams; see `git-config-fixed-root-validation.json`.

## Regression coverage and limits

Tests cover unrelated config keys, real worktree redirects, repository disappearance, initial then idle, new and resident sessions, tracked-ignored admission, ignore changes, schema migration and mid-Begin failure/recovery. Mutation tests fail when the defect is restored, when discovery is removed from identity, and when `config.worktree` is dropped from declared control files. The ignored-parent/exact-hard-excluded-child admission guard is also retained.

After the worktree correction, graphinput passed in 3.634 seconds and graphsession in 216.007 seconds. These are package test runtimes under shared-host load, not analysis latency. Earlier bootstrap coverage passed in 211.033 seconds. The final publication snapshot passed `go test ./...` in 223.499 seconds wall time; receipt and source hashes are archived in `git-config-full-suite.json` and `git-config-source-hashes.json`.

The policy identity format changes to v2, so an existing workspace conservatively reconciles once after upgrading. The Git configuration fix itself does not change extraction semantics. The measurements above used v291; publication also merges the concurrent v293 accuracy changes, whose integrated checks are recorded separately. External Git include/global/system files and process environment are not all watched; their effects are projected when policy discovery rebuilds. This patch does not establish immediate notification for every external Git configuration source. Measured include-file discovery behavior is specific to Git 2.50.1 and is not a universal guarantee.

## Integrated v293 publication check

Before publication, origin/main advanced to `56b072d` with v293 accuracy fixes. They were merged without conflicts. The merged binary SHA256 is `906e4d3c6d1bbb47703c33134072456d9382eff20d511d58303f7dd8223ad45d`. A fresh isolated CLI watch probe repeated the eight config edits and real tracked-source admission: 0 extra records after harmless config edits, then exactly one replacement for admission. Full consumer replay and frozen manifest/batch checks pass against the independent cold target (`git-config-v293-root-validation.json`). Build took 3.425 seconds; build plus the observation-window probe took 23.663 seconds. This is not Product performance acceptance for v293; the repeated Product timing report remains explicitly v291.

The merged production snapshot `c17be11` passed `go test ./...` in **251.127 seconds** wall time. Receipt: `git-config-v293-full-suite.json`.
