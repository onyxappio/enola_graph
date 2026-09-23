# Tracked membership broadens frozen replacement scope

Diagnostic performed 2026-09-23 against main 665f3c9 plus the uncommitted manifest/config candidate whose six production source hashes match private benchmark snapshot 1e77fa7d. This is not accepted production and is not a performance benchmark.

## Reproduction

The attached `policy-membership-probe.go.txt` is a Go test over the candidate fixture helpers. It is loaded with a Go overlay rather than written into production sources. Each subcase initializes a separate Git repository and stages all initial files, runs an authoritative initial session, and creates the same independent TypeScript source. The second subcase also stages that new file before delta. Both compare the resulting complete consumer graph with a cold session.

| New source | Begin owners | TS parses | PolicyReconciled | Cold equality |
| --- | ---: | ---: | --- | --- |
| Untracked | 1 | 1 | false | exact |
| Staged | 6 (whole fixture) | 1 | true | exact |

Full test command: `/tmp/enola-toolchain/go/bin/go test -overlay /tmp/enola-root-policy-membership-overlay.json ./internal/graphsession -run '^TestRootProbeTrackedAdditionPolicyScope$' -count=1 -v`. The overlay maps the virtual root_policy_membership_probe_test.go file in the graphsession package to the attached test, both as absolute paths. Wall time 4.60 s, package 3.035 s; these are test-run durations, not delta latency. Output is retained in `policy-membership-probe.log`.

## Cause and next requirement

`graphinput.Policy.Identity` hashes tracked names, excluding hard-excluded paths, together with options and policy dependencies. `graphsession.session.run` forces wholeDomain when stored PolicyIdentity differs. A tracked membership change therefore reconciles policy and replaces the entire domain even when ordinary candidate planning can bound the new independent source.

Do not remove tracked membership from validation without replacing its role: tracked files bypass Git ignore rules; explicit Enola exclusions still apply. The follow-up must distinguish unchanged rules plus reconciled input membership from genuinely changed/unknown policy semantics, preserve removed owners, freeze the complete scope before Begin, and keep failure/recovery fences. Pure staging of an already analyzed ordinary file should be tested for zero publication/generation advancement where input semantics do not change.

Historical diagnostic 07fb4a41ddaf -> fec1eac346c4 on experimental 41e530e9 also reports PolicyReconciled=true and whole-domain Begin 8,613 despite no package.json, tsconfig.json or .gitignore edits. Actual changed contribution owners 81; parsed 1,671 (90 source content, 4 additions, 1,577 resolution). Delta 27.281 s versus cold 24.990 s, using file sink with other work on the host; this is not a clean repeated latency comparison. Tracked-membership fallback explains broad Begin, but does not by itself establish the cause or removability of all 1,577 resolution reparses.

## Pure staging without source changes

A second diagnostic first analyzes the new source while it is untracked, then only stages that same file. The delta reparses zero files but still publishes the whole six-owner fixture and advances generation 1 to 2. Cold equality passes. This is a reproducible no-op performance defect, not stale graph data. TestRootProbePureStagingAlreadyAnalyzedSource is included in the attached overlay source; output is in policy-pure-staging-probe.log. Command uses the same overlay and test-name selection. Wall 3.46 s / package 2.127 s are test durations.
