# Wave 7 accuracy review — accepted v295

Candidates #31–35. Product `a609c19f3861971930fae7b33dcb2950598953c5`; landings-base `e263a0942efce56e9cd02abc425c0aa4d0327632`.

Implementation `23a2096` plus correction `d64466f52e7819384b34778de30cc8a556fa0c22`. Integrated with main `9fc78fff9c95dae737659cbf9319c9095994a343` in `8aafe562135de041ecaeca3ebfd9ec52e9475ea2`. All final checks below target that combined checkout, including the concurrent Git-configuration no-op changes.

Independent binary `/tmp/enola-wave7-integrated-independent`, SHA256 `eb3ca6975cd3e8e8ab869c698daabc273999390e0faf24f26d4f186f44d54063`.

## Verified changes

| Candidate | Source-proven behavior |
| --- | --- |
| #31 | Vue template calls through default-as barrels reach Stepper/StepperGroup definitions; aliases and chains preserve leaf provenance. Missing defaults, cycles and ambiguous stars do not create resolved guesses. |
| #32 | Top-level destructured bindings emit variable symbols with correct exports and consumer calls. Nested patterns, rest/defaults/holes are covered; object keys, initializer identifiers and function-local bindings do not become module symbols. |
| #33 | Parsed TS/JS/Vue files have a file_ref even without file-scope references. Markdown names reaches the exact health.ts/scans.ts file, with empty-file, rename, delete and exclusion controls. |
| #34 | Explicit JS runtime imports prefer source substitution, then implementation, then declaration fallback. .ts/.mts substitution remains valid; barrels reach the actual .js/.mjs implementation. |
| #35 | Class/type fields have direct declares edges to existing same-file enclosing types. All 18 formerly isolated Product constants retain their identities and have class connections, including Swift OnyxGlassView.effectView. |

#35 required a Swift correction after the initial TS-only patch. The actual screenshot's live Codata database was not queried. No calls/uses were invented to make unused fields appear connected. Swift top-level and extension properties do not acquire false class membership. The existing directory declares relationship remains; v2 can leave that directory relationship unresolved while retaining the resolved field-to-type relationship.

## Full graphs and migration

Fresh, separate states for v1 and v2; published v293 baseline → integrated v295, then independent cold v295 and no-change. Full canonical fact/event records retain owners, provenance and occurrences; file_ref facts are not collapsed solely by ID. v2 Begin scope remains frozen and file-owned.

| Repository | Protocol | Nodes | Edges | Migration = cold | No-change |
| --- | --- | ---: | ---: | --- | --- |
| Product | v1 | 71,096 | 170,153 | exact | silent |
| Product | v2 | 69,758 | 170,153 | exact | silent |
| Landings | v1 | 23,175 | 55,739 | exact | silent |
| Landings | v2 | 22,880 | 55,739 | exact | silent |

Silent means zero parses, unchanged journal bytes/hash and unchanged generation. All wave5/6/7 full-source semantic assertions pass. Protocol node counts intentionally differ. Product keeps the established e2e-artifact and worker-report exclusions; pinned inputs and analysis scope are unchanged.

## Independent checks

- Full `go test ./...` passed in the real integration git checkout, including Git-hook environment regressions.
- Eight focused suites passed: 18 exact-source/control cases; 14 TS constant lifecycle stages; eight destructuring stages; 14 implementation-resolution cases; 16 Vue barrel cases; 16 Markdown-file lifecycle stages; 14 Swift lifecycle stages; two Swift scope fixtures (nested classes, sibling classes, top-level/local negatives).
- All 17 prior-wave gates passed on v295. Wave5 negatives, Nitro alias/shadow controls, seven v1 deltas, seven authoritative v2 deltas and wave6 lexical controls also passed.
- Golden changes independently reviewed: 25 empty file_ref facts added and 25 fact records gaining structural declares relations; no fact/relation removals or changes to other properties. Conditional duplicate symbols retained. Golden/determinism passed in the coder checkout; publication hook rechecks the integrated checkout.

Local evidence: `/tmp/enola-wave7-final-acceptance.json`; `/tmp/enola-wave7-integrated-go.log`; `/tmp/enola-wave7-integrated-gates.json`; `/tmp/enola-wave7-integrated-recent-regressions.json`; `/tmp/enola-wave4-independent-review/v295-gates-1790152472/report.json`. Full-run descriptors: `/tmp/enola-wave7-final-full{,-v2}-review/{product,landings}-integration.json`. The local evidence paths are machine-local, not durable repository artifacts; committed tests and this review preserve the contract.

## Report and next wave

The cumulative HTML retains previous cards and embeds Shiki offline. Candidate35 now shows both original TS and Swift excerpts, with labelled explanatory comments identifying node kinds and field → declares → enclosing-class direction. Its status reflects the verified final result.

Fable5.1 is researching #36–40 against the integrated v295 binary. Those findings require primary reproduction before auto approval. The continuous objective remains active; this acceptance completes one wave only.
