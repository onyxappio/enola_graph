# Wave 13 — independent accuracy review

Reviewed integration: `9690ebe36c121462496f238bcd1c17acd197f085`, including main `5b0934c`. Cache **v318**.
Final binary SHA256: `dbd8e2df409b6d77f223f5d672568cda572dd076e881d7384b20be4d3492ebec`.

## Behavior

| ID | Source-backed correction |
| --- | --- |
| 61 | ImportType arguments in generic calls no longer corrupt following declarations or become runtime imports. Preserve genuine dynamic imports, regex/string boundaries and escaped template interpolation. |
| 62 | Existing local values explicitly exposed by CommonJS exports receive exported=true without duplicate entities. Preserve provenance and local module/exports shadowing. |
| 63 | Relative Vite query imports resolve underlying source files while retaining import_spec. |
| 64 | Imported object shorthand values create exact references, preserving lexical shadows and avoiding invented bindings. |
| 65 | Nested tsconfig discovery, alias resolution and invalidation follow the relevant config scope. |

## Verification

- Main1 (`85dd66f`) passed 140 focused lifecycle rows plus 12 template rows, lexical safety/performance checks, retained mutation gates, bounded reads and alias race. All 19 additional full-corpus regression groups passed, including 228 prior binding controls and waves8–13 static semantics.
- Original full Go run passed 109 packages. Its only failure was two stale CommonJS exported flags in the Express golden. Source explicitly uses module.exports; only those two booleans changed in `ea4c37c`. The entire engine package then passed.
- The obsolete metrics oracle also failed on published v317. The source-corrected wave12 oracle passed; old failed receipts remain preserved.
- Final integration changed production only in graphsession retry reuse. Its entire package passed again in **376.142 seconds**, including new consecutive-refusal/context tests. A whole Go-suite rerun after the golden and final retry integration is **not claimed**.
- Fresh supported forks of intact v317 cold states validate Product and Landings in v1/v2. Every final migration equals a fresh cold graph exactly, including all properties and occurrences; no-change has zero parses/events and no generation advancement. Fork lineage and frozen v2 scope remain valid.
- All four final graphs equal their independently source-audited main1 graphs exactly. Earlier focused/static results remain applicable to unchanged extractor code and these identical final graphs; graphsession was retested directly.

## Source audit

Pins: Product `a609c19f3861971930fae7b33dcb2950598953c5`; Landings `e263a0942efce56e9cd02abc425c0aa4d0327632`.

- Product: 20 existing symbols change only exported=false→true, matching explicit CommonJS export statements in four files. Eighteen imported shorthand references are added; previous calls are retained after occurrence-order normalization.
- Landings: phantom generic ImportType dependencies and 23 falsely parsed variable declarations disappear. createDomRect becomes its real function declaration. Seven real hosted-paywall declarations and associated references are recovered; function/callback locals no longer become file-level fallback references.
- Six Vite query imports resolve to their real source modules. A deep test tsconfig fixes useDataLayer's @/ target. currentEthnic shorthand gains its reference. Other canceled edge deltas are occurrence-only.
- The known v2 omission of synthetic directory-module nodes persists: package-level declares/imports targets may remain unresolved there, while v1 resolves them. This is documented since wave5, appears in both baseline and current graphs, and is not a wave13 fix. Protocol equality is asserted separately, never by stripping those fields from migration checks.

## Corrected intermediate oracles

The initial interpretation of TypeScript `left < import("./runtime") > (right)` as a runtime comparison was wrong. Both TypeScript whitespace variants are generic calls; JavaScript variants are comparisons. Corrected controls include an unambiguous TypeScript runtime form with await import. Do not reuse the withdrawn oracle.

Independent review found a template-escape double increment; `5767f8d` fixes it and adds controls. Negative tests cover genuine interpolation, escaped interpolation, dangling backslashes and fake import text. Passing these checks does not prove correctness for every possible JavaScript/TypeScript program.

## Evidence

- Final build, graphsession and acceptance: `/tmp/enola-wave13-main2-primary-review/`.
- Final fresh migration registry: `/tmp/enola-wave13-main2-full-review/`. Consumed states must not be reused.
- Source audit: `/tmp/enola-wave13-main1-source-audit.json` and `/tmp/enola-wave13-source-audit.py`.
- Focused, lexical and full-Go coverage receipts: `/tmp/enola-wave13-main1-primary-review/`.
- Prior/full-corpus gates: `/tmp/enola-wave13-main1-full-regressions/receipt.json`.
- Cumulative HTML QA: `/tmp/enola-wave13-report-final-qa.json` with adjacent card screenshots. Five new cards retain source-verified excerpts, graph annotations and offline Twinkleplop; historical code remains unchanged.

The continuous accuracy goal remains active. The next five independently reproduced candidates are #66–70, assigned to GPT Luna xhigh after this wave is published and the accuracy branch is synchronized.
