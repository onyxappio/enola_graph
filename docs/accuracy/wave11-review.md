# Wave 11 — independent accuracy review

Implementation revision 6: `777e2c83fce41f46cb89141fafff601e27419534`, cache **v316**.
Verified integration: `2652a14d9cfcb0e552d2089da4644e01d5a256bd`, including main `0260153` and its shared TypeScript discovery enumeration.
Independent binary SHA256: `850c6c7f0f872e48e3df58af4a604cbf771adfbdb986415dab7cf16ea709a51c`.

## Behavior

| ID | Source-backed correction |
| --- | --- |
| 51 | Constructor parameter properties produce class-field symbols and direct ownership/reference edges. |
| 52 | Proven arrow-valued defineEndpoint URLs produce HTTP client routes. |
| 53 | Lexically proven fetch aliases retain HTTP client routes; unrelated or shadowed receivers do not invent routes. |
| 54 | Registered Nuxt composable reexports resolve to their original declaration, including actual virtual imports and configured runtime aliases. |
| 55 | External imports no longer bind private same-name declarations in unrelated sibling files. Same-file CommonJS declarations remain valid reference targets. |

Alias proof requires an actual unshadowed `@nuxt/kit` createResolver based on `import.meta.url`; renamed kit imports work. Alias/config changes invalidate resolution. Context signatures preserve cold/delta equality and bounded unrelated source reads.

## Final validation

- All 23 independent gate groups passed, including previous-wave regression suites and source-backed positive/negative lifecycle controls.
- Bounded Nuxt source reads and concurrent fetch-alias race checks passed.
- Full `GOMAXPROCS=1 go test -p 1 ./... -count=1 -timeout 20m`: **PASS, 494.515 s**.
- Four supported forks of published v314 states migrated to v316; exact cold equality, fork lineage, zero parses/events and stable generation on no-change passed.
- All 228 binding-restoration assertions and full previous-wave semantics passed.

| Protocol | Repository | Node records | Edge records | Cold = migration | Silent no-change |
| --- | --- | ---: | ---: | --- | --- |
| v1 | Product | 71,132 | 139,947 | PASS | PASS |
| v1 | Landings | 23,187 | 44,777 | PASS | PASS |
| v2 | Product | 69,794 | 139,947 | PASS | PASS |
| v2 | Landings | 22,892 | 44,777 | PASS | PASS |

Counts are protocol records, not unique logical entities. These fresh states use separate local state/event paths; their context labels retain the earlier final2 test prefix. No shared transport or previously consumed state was reused.

The final2 build already passed all 23 independent gate groups, bounded-read and race checks, full Go, and four supported v314→v316 graph migrations with exact cold equality and silent no-change. It restored 97 legitimate references and removed 17 invalid sibling relationships per protocol (228 assertions). These historical results are retained; they are not a substitute for final main3 validation.

## Source audit

Pinned Product `a609c19f3861971930fae7b33dcb2950598953c5`; Landings `e263a0942efce56e9cd02abc425c0aa4d0327632`.
All 36 introduced source entities were reviewed: 26 constructor fields and 10 routes. All other changed node/property/metric rows match the source-reviewed main1 records, except 18 dependency transformations per Landings protocol. Those actual `#landings-runtime` imports now point to existing module runtime files with verified Nuxt registration. All 18 import occurrences remain; 14 unique external edges become 12 unique internal module edges because directory targets coalesce. No source occurrence is dropped.

Resolved-reference review covers 401 composable-origin references and 30 additional references in Landings, plus the 21 Product field references. It preserves 77 formerly lost valid Nuxt references and 20 valid Product CommonJS references. Five Product pixelmatch and twelve Landings edges to unrelated private declarations are correctly absent. The whole-file `file_ref` pass deliberately represents imported type annotations and argument-value uses as references: not every edge named `calls` is a runtime function invocation. Existing scalar metrics remain unchanged; false local qualification of external loop calls was removed under a source-checked exact-removal oracle.

Final main3 graphs exactly equal final2 in all four combinations, including every occurrence, owner and property. The audited source verdicts therefore carry forward unchanged through the shared-discovery integration.

## Evidence and limitations

- Acceptance receipt: `/tmp/enola-wave11-main3-acceptance.json`.
- Main3 build and tests: `/tmp/enola-wave11-main3-independent-review/`.
- Fresh final3 published-state fork registry: `/tmp/enola-wave11-final3-full-review/`; full driver: `/tmp/enola-wave11-final3-full-acceptance/receipt.json`. Consumed migration states must never be rerun in place.
- Final2 node audit: `/tmp/enola-wave11-final2-node-audit-carryforward.json`; dependencies: `/tmp/enola-wave11-final2-dependency-source-verdict.json`.
- Exact main3/final2 comparison: `/tmp/enola-wave11-main3-final2-equivalence.json`.
- Prior whole-source pin integrity: `/tmp/enola-wave11-main3-source-pin-receipt.json`: all 13,650 included Product files and 3,293 Landings files match their Git blobs; no unexpected missing or changed files. Product omissions are the authorized artifact/report exclusions.

Known preexisting failure: v2 deletion of a Nuxt autoimport source can request an owner outside the frozen replacement scope. Reproduced on published v314 and main2, and reconfirmed on main3; the original broad alias-lifecycle harness therefore did not pass in full. This is the independently reproduced wave12 scope-planning candidate, not claimed fixed by wave11. Preserve fail-closed publication. Protocol v2 still omits synthetic directory module nodes under its existing contract.

An optional older broad HTTP corpus oracle also failed its unchanged Fastify serverBindings assertion; it is not reported as passed. Independent gated/ungated HTTP extraction over all 6,787 Product TS/JS files was equal on main1; later corrections were confined to Nuxt/CJS resolution. Final focused HTTP gates and full Go remain required.

This review proves the listed corrections on pinned sources and controls, not universal graph completeness or a new performance benchmark. The cumulative offline Twinkleplop report preserves annotated source examples. Continuous auto-approved waves continue; the next five evidenced cases cover loop lexical bindings, Vue default reexport barrels, Nuxt route-handler links, target-membership invalidation, and frozen-scope planning. Coding changes to GPT Luna xhigh; research remains Opus.
