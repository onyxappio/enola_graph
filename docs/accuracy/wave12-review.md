# Wave 12 — independent accuracy review

Verified integration: `088577e35b06894c38ef249621e5822018c216ab`, including main `b7e2fe5430bbbb7fb604b22628a10a42dfc1ac7f`. Cache **v317**.
Final planner correction: `c7343c75a3aaa28c0246148f52ba9838d242ed4b`.
Independent binary SHA256: `18d146c03a4f24459011d3b58fe112426f4564d8a1d4dde833c0c2cbeb68d251`.

## Behavior

| ID | Source-backed correction |
| --- | --- |
| 56 | Lexical loop locals no longer bind private sibling declarations. Preserve TDZ boundaries, genuine references and single traversal of loop RHS expressions. |
| 57 | Vue default imports reexported through a local barrel retain component bindings, including renamed local imports. |
| 58 | Proven direct Nuxt/Nitro routes receive handled_by edges to their default handler/component. Ambiguous conditional handlers remain unlinked. |
| 59 | Registered target deletion, restoration and retargeting invalidate resolution even without an ordinary import edge. |
| 60 | Plan the composition fallback after dependency closure before frozen Begin; deletion must not publish owners outside the manifest. |

## Final validation

- All 16 primary gate groups passed: 11 focused groups, full Landings lifecycle, prior regressions, bounded source reads, alias race, and full Go.
- Full `GOMAXPROCS=1 go test -p 1 ./... -count=1 -timeout 20m`: **PASS, 847.025 s**; graphsession **585.144 s**.
- All 18 full Landings stage/protocol combinations passed: original, warmup, page delete/restore, handler delete/restore, autoimport delete/restore, warmup restoration for both v1/v2. Every stage preserves exact cold/delta equality and silent no-change.
- All 23 full-corpus acceptance groups passed, including four supported v316→v317 forks, exact cold equality, fork lineage, zero parses/events and stable generation on no-change, binding restoration and prior-wave semantics.
- Final cumulative report has offline Twinkleplop highlighting and annotated source examples. All five new cards pass mobile overflow checks at 390 px; no external network dependencies.

## Source audit

Pinned Product: `a609c19f3861971930fae7b33dcb2950598953c5`; Landings: `e263a0942efce56e9cd02abc425c0aa4d0327632`.
Final normalized semantic diffs equal the previously reviewed runtime exactly in both repositories; full migration equality independently includes occurrences and all record properties.

- Removed 1,593 false loop-local references: 1,258 Product and 335 Landings.
- Preserved identities and nonmetric properties of 189 Product and 23 Landings symbol records. Changes concern loop-reference lists and loop/scaling depths; source AST classification confirms lexical locals or pre-body RHS expressions and callback iteration boundaries.
- Restored four Landings story-to-Vue component bindings and added eight direct handled_by edges.
- Four unresolved references change fallback names: three JSON-default references in Product and one external checkout-vue type in Landings. They remain unresolved; this wave does not claim to resolve them.

The AST locator is review evidence, not a whole-language correctness proof. Graph edges named calls can represent references rather than runtime invocation. Vue components here are symbol/function nodes with web_component=component.

## Rejected intermediate results and limitations

An earlier full v2 page deletion failed with createConfigFiles.ts outside frozen scope despite focused checks passing. The final planner mirrors the extractor composition decision after dependency closure before Begin. Final full-corpus replay proves the covered scope contract.

Broad fallback is intentionally retained. The final v2 page deletion reparsed 1,981 files; this is correctness evidence, not a minimal-scope or performance improvement claim. Missing captured dirty inputs can influence the existing composition signature; this wave does not claim every broad fallback is semantically minimal.

Two golden files formerly expected false src.c/src.id loop-local references. Commit b33aa44 removes those edges using source evidence. Earlier metrics expectations similarly named local callback variables; the retained primary oracle changes only those source-proven expectations. The final full Go run passes outright; earlier failed receipts remain preserved.

## Final main synchronization

Merged main `6090161ea9eee44f782fe721bbe14d9e96c6adb4` into `ac224bf8b1c276642523f8617e129e0dca5a44ba`. The only change relative to the accepted runtime is `independent_retry_alias_context_test.go`; production code is identical. Its `TestIndependentRetryAliasContextRemainsColdEqual` passes independently (1.544 s). Prior complete acceptance remains applicable to unchanged production code.

## Local evidence

- Build and all primary gates: `/tmp/enola-wave12-main4final-independent-review/{build,receipt}.json`.
- Full 18-stage lifecycle: `/tmp/enola-wave12-main4final-full-landings-lifecycle/receipt.json`.
- Fresh fork registry: `/tmp/enola-wave12-main4-full-review/`; full acceptance: `/tmp/enola-wave12-main4-full-acceptance/receipt.json`.
- Source verdict: `/tmp/enola-wave12-main4-primary-full-source-verdict.json`; audit inputs and loop classifications have the same main3 prefix.
- Source-pin integrity: `/tmp/enola-wave12-main1-source-pin-receipt.json` records all 13,650 included Product and 3,293 Landings files matching Git blobs, with only authorized Product exclusions.
- Final HTML QA: `/tmp/enola-wave12-main4-report-final-qa.json` and adjacent card screenshots.

Consumed migration states must not be reused. Local evidence paths are machine-local; the cumulative HTML and this review are the portable summary. The continuous accuracy goal remains active for subsequent waves.
