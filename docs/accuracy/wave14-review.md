# Wave 14 — independent accuracy review

Reviewed integration: `765a67d043d9ef3d7a68ce9d3e11f0f8e8ce2101`, including main `1af2b91`. Cache **v320**.
Tested runtime: `16c07f71e56e1ceea4d4ebd60062e1e35ca152af`; later changes are reviewed golden artifacts and tests only.
Binary SHA256: `e6fbadd5444c4f133e394f2855ce23011cd60ac4ac01fd1519cf7c68dfff783c`.

## Corrections

| ID | Behavior |
| --- | --- |
| 66 | Unbound module references cannot bind arbitrary sibling modules; explicit imports, local bindings and script globals retain their controls. |
| 67 | Vue props/emits preserve newline-, comment-, digit- and Unicode-containing member declarations. |
| 68 | Module control-flow blocks emit their lexical declarations and exact calls. Structural block identities ignore trivia; inserting/reordering sibling blocks can still renumber identities. |
| 69 | Wrapped default values retain entities. Default selection changes invalidate consumers even when the exported-name set is unchanged. |
| 70 | Own-file constructor uses emit file references; body instantiates relations retain exact target-file resolution and shadow controls. |

## Verification

- 190 focused CLI lifecycle rows passed for v1/v2: explicit semantics, exact cold/delta equality and silent no-change.
- Independent standalone ExtractSession tests verify repeated default A/B swaps, restoration, exact target references and zero no-change parses. An independent test initially used dirty=nil for no-change; the documented API means reparse-all, so the harness was corrected to an empty map. This was not a product regression.
- Full `go test ./...` passed in the real integration Git checkout at the reviewed commit in 515.354 seconds. Earlier archive-only failures and old golden failures are retained as evidence, not counted as passes.
- Ten goldens were source-reviewed; no nodes or proven resolved relationships were removed. Two mixed-replay tests were adapted to reject unimported sibling binding while retaining explicit-import positives, default-origin side reads, add/remove/restore and cold equality.
- Four fresh supported v318 forks (Product/Landings, v1/v2) migrated to v320 and exactly matched fresh cold graphs. No-change retained byte-identical events and generation, with zero file parses. Frozen file ownership and fork lineage passed. These migration states are consumed; never reuse them for another migration claim.
- Prior-wave 5–13 checks, callable/binding restoration, and wave14 full-corpus semantic checks passed. Scope/count oracles do not substitute for source review.

## Source review

Pins: Product `a609c19f3861971930fae7b33dcb2950598953c5`; Landings `e263a0942efce56e9cd02abc425c0aa4d0327632`.

- No previous node identity was removed. Independent TypeScript AST checks verified all 391 new symbols: 316 Product and 64 Landings module-block declarations, plus 11 Landings wrapped default objects.
- 110 Vue component records changed 123 props/emits lists; all lists matched independent AST type literals or local interfaces. One file_ref owner marker changes line 2 to 1 without changing identity.
- After occurrence normalization, Product removes 1,426 unresolved and eight ambiguous call rows; no resolved call is removed. Landings removes 405 unresolved rows and exactly the two proven false sibling __dirname bindings.
- Added relations comprise 391 declares, 378 references to new entities, 28 calls from new entities, 85 own-class references, five references to locally declared require-bound constructor values, and two remaining unresolved calls. One further unresolved call is within the new-source calls. JSDOM and SourceMapConsumer source declarations/usages were inspected directly.
- Node/call changes match between v1/v2. Directory declares/imports resolution differs because v2 still omits synthetic directory modules. Normalization is used only for semantic classification, never migration equality.

## Limits and next work

This does not resolve the queued Codata R1/R2 directory-module/occurrence issues or all global/external-name modeling. Unbound body calls may retain an own-file constraint while unresolved; it is not proof of a local declaration. Passing this suite does not prove all JavaScript/TypeScript syntax correct.

Next: implement the explicitly approved backend and mobile FSM semantic schema; then ordinary wave15 and continued research waves. Ordinary evidenced candidates remain autoapproved; additional FSM scope still needs approval. The continuous goal remains active.

## Evidence

- `/tmp/enola-wave14-primary-acceptance/run-main2/receipt.json` — 190 focused rows.
- `/tmp/enola-wave14-main2-primary-review/` — immutable runtime and standalone proof.
- `/tmp/enola-wave14-main3-full-go/result.json` — final full Go pass.
- `/tmp/enola-wave14-main3-corpus-pipeline/receipt.json` and `/tmp/enola-wave14-candidate-full-review/` — four migrations and preserved receipts.
- `/tmp/enola-wave14-main3-full-regressions/receipt.json` — prior/new semantic gates.
- `/tmp/enola-wave14-main3-source-audit.json`, `/tmp/enola-wave14-new-node-source-oracle.json`, `/tmp/enola-wave14-vue-source-oracle.json` — source review.
- `/tmp/enola-wave14-report-final-qa.json` — five cards at390px, offline Twinkleplop, filters/search and unchanged historical snippets.
