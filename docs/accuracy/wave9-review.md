# Wave 9 independent acceptance

Verified implementation/integration: `0c638ee158c2d4468f3b59aadbbff52d7f912c22`, cache v304. Independent binary SHA256: `b6f935ab6cad99b531183a4728de56064264732e36e821fcbbf106f1b440250e`.

Publication integration `56ed3329e3777dcbc10092dbe247730019bafa96` additionally incorporates main `365d150d6885a98d30e4a5ccc801b8d97d92272a`: documentation and resident configuration-race tests only, no production changes. The new `TestIndependentScopedResidentConfigMutationFence` passes on this integration; full-suite and full-repository evidence below belongs to the verified implementation commit above.

## Accepted behavior

| Candidate | Verified result |
| --- | --- |
| 41 | Re-export references preserve the original target file. |
| 42 | Aliased re-exports resolve to the original exported name. |
| 43 | Lexical parameter/local bindings suppress false file-reference fallback to sibling modules. |
| 44 | Chained re-exports retain source provenance without inventing targets for cycles or absent exports. |
| 45 | Destructured parameters and forward local declarations suppress false sibling calls. |

Independent review found and corrected regressions in catch complexity, type/value namespaces, scoped literal require bindings, closure captures, module require shadowing and CommonJS dual value/namespace bindings. Final implementation is `6d26b730bd48cfc34b70847f8c0d24f1cb838d84`; `61ce1d5f79c32321abc45bf7291e6891344c497c` registers its missing cache-version test coverage.

## Validation

- Full Go suite: exit 0, 376.6 seconds on verified integration.
- Literal require: 34 controls, 12 scope boundaries, 24 lifecycle assertions and eight real Product call checks pass. Both function and file-reference targets are preserved for the typed destructured require in `apps/mobile/src/visual-diff/gateRunner.ts` (`readScreenStructureFileKey`, `shouldRunInstanceCropCompare`).
- CommonJS plain-value fixture passes in v1/v2; cached v303-to-v304 migration passes in both protocols. Existing Express golden is unchanged.
- Prior 114 assertions: 26 scope, 12 type references, 22 metrics, 40 lifecycle, 14 chained-reference checks; all pass. Twelve previous-wave regression scripts also pass.
- Full Product and Landings in both protocols: v297-to-v304 migration/replay exactly equals fresh analysis, preserving file owners, duplicate occurrences and fork provenance. No-change runs parse zero files, publish zero events and preserve generation. Replacement-scope checks and prior wave5/6/7 semantics pass.
- Full wave8 and wave9 semantic checks pass in both protocols. Wave9 includes 12 assertions, including real require preservation.
- Source review covers all 37 Product and 10 Landings changed node-property records. Changes remove false lexical/module loop-call metrics (and Landings false recursion); identities and cyclomatic complexity remain preserved. Property changes match across v1/v2.

Pinned sources: Product `a609c19f3861971930fae7b33dcb2950598953c5`; Landings `e263a0942efce56e9cd02abc425c0aa4d0327632`. Source policy and exclusions remain unchanged.

## Evidence and limits

Local evidence: `/tmp/enola-wave9-final-acceptance.json`, `/tmp/enola-wave9-main4-full-go-receipt.json`, `/tmp/enola-wave9-main4-require-run-receipt.json`, `/tmp/enola-wave9-main4-prior114-receipt.json`, `/tmp/enola-wave9-main4-prior12-receipt.json`, `/tmp/enola-wave9-main4-commonjs-migration-receipt.json`, `/tmp/enola-wave9-main4-props-audit-receipt.json`, `/tmp/enola-wave9-require-final-full-review/`, and `/tmp/enola-wave9-publication-main-tests.log`. These are machine-local receipts, not portable repository dependencies. Superseded bulky evidence was archived with member SHA verification; failed tests and corrected harness assumptions remain documented.

- Nested local functions still lack their own graph nodes. This wave removes wrong module-target edges; it does not establish complete local-call coverage.
- CommonJS value imports retain legacy importer-local target spellings (for example `routes.webhookRoutes`), which may remain unresolved for `module.exports = router`. Proper CommonJS default-export resolution is not claimed.
- Computed require expressions remain unbound; no TypeScript compiler or global type inference is added.
- Cold/delta equality is not sufficient proof of semantic correctness; independent controls and source inspection exposed the regressions above.
- This is accuracy acceptance, not a new performance benchmark or universal entity-coverage claim.

Cumulative HTML retains historical cards, embeds Shiki and annotated code, and has no external resource dependencies. Playwright/Chrome visual review at 500×1000 confirms readable header/code and no horizontal document overflow or JavaScript errors. The initial CLI screenshot was blank; acceptance uses the subsequent inspected Playwright screenshots, not that failed render.

Wave10 candidates 46–50 have independent reproduction evidence and are automatically approved under WORKFLOW.md. Continue after this publication boundary.
