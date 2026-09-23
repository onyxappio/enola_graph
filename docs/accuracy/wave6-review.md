# Wave 6 accuracy review — accepted v293

Candidates: #26–30. Sources: Product `a609c19f3861971930fae7b33dcb2950598953c5` and landings-base `e263a0942efce56e9cd02abc425c0aa4d0327632`.

Implementation: `133cb89ce8a1f751d3c56d09bc2866ff192e18f6` (initial implementation `8540b1a`, corrections v293). Integrated with main `a15bf9a1bc6096fd3a0ea051fce1bb86697a8326` in merge `daa197b` before acceptance. The final production source matched the independently reviewed immutable snapshot; the final additional test expands sibling Markdown replacement coverage.

The integration includes concurrent policy/admission/manifest invalidation changes, so acceptance was repeated against that combined checkout. Integrated binary `/tmp/enola-wave6-main-integration-independent`, SHA256 `72ef610e76bbb7ff387f0833e139583d249a7fb7af8a917efc10a593665310a3`. Full `go test ./...` passed; log `/tmp/enola-wave6-main-integration-go.log`. All 31 lexical/module/mixed-consumer checks passed on the integrated binary; log `/tmp/enola-wave6-main-targeted.log`.

## Verified behavior

| Candidate | Exact-source result |
| --- | --- |
| #26 | FigmaIcon is parsed despite a long comment/string; references from SemanticStatusIcon resolve. |
| #27 | DetailsGroup owns a direct calls edge to DetailField; imported aliases/namespaces and lexical shadows are checked. |
| #28 | Product Confetti import-then-export resolves to confettiPieces; renamed imports/exports resolve to the implementation. |
| #29 | Nuxt explicit registrations and page alias are emitted; module runtime filenames do not fabricate app routes. |
| #30 | Markdown declares resolves to an existing directory-module representation in both v1 and v2, including deletion/rename/last-owner changes. |

Comments in report code examples identify actual node kinds and direct relation direction. They are labelled report annotations, not original source comments. Shiki is embedded for offline viewing.

## Full-project migration and graph checks

v291 → v293, same inputs and configuration, each protocol with its own state and event sink. Every result equals an independent clean analysis byte-for-byte after canonicalizing full fact/event records, including owner/provenance/occurrence. Repeated nochange has zero parsed files, zero event changes, unchanged generation. All wave5 and wave6 semantic assertions pass.

| Repository | Protocol | Nodes | Edges | Migration = cold | Nochange silent |
| --- | --- | ---: | ---: | --- | --- |
| Product | v1 | 70,354 | 169,570 | yes | yes |
| Product | v2 | 69,017 | 169,570 | yes | yes |
| Landings | v1 | 21,569 | 54,000 | yes | yes |
| Landings | v2 | 21,284 | 54,000 | yes | yes |

Evidence descriptors: `/tmp/enola-wave6-integrated-full-review/{product,landings}-integration.json`, `/tmp/enola-wave6-integrated-full-v2-review/{product,landings}-integration.json`. Each points to all raw journals, run hashes, summaries and `verify-structural-verification.json`. The verifier also requires semantic assertions, despite that historical filename.

## Targeted and prior-wave checks

- 7 lexical positive/negative controls: imported JSX alias, namespace, parameter/namespace shadows, block-scope recovery, renamed bridge, actual local exported binding.
- 12 module lifecycle stages across v1/v2, plus 12 mixed TS/Markdown consumer stages: rename, deletion, last owner, restore, existing resolved endpoints and nochange.
- All four original real-source primary runners (#26, #27, #28, #29–30) pass.
- Previous wave5 six negative controls, Nitro lexical alias/shadow checks, seven v1 deltas and seven authoritative v2 deltas pass.
- All 17 prior-wave gates pass: `/tmp/enola-wave4-independent-review/v293-gates-1790148318/report.json`.
- Targeted records: `/tmp/enola-wave6-correction-acceptance.log`, `/tmp/enola-wave6-correction-draft-{lexical-controls,module-lifecycle,module-consumers}.log`.

## Semantics and limits

v2 retains frozen FILE-owned Begin scope. Markdown emits a source-owned module representation; sibling Markdown owners may require complete replacement when module membership changes. Broader proven dependency scope is allowed; unrelated-domain fallback and equality remain checked. v1 synthetic module behavior is preserved. Do not describe both protocols as having identical node sets.

Candidate #35 (Codata isolated TS class-field constants) remains a separate wave7 requirement. Rechecked on the final v293 production snapshot: the minimal fixture still has v1 degree 1 / v2 degree 0. Evidence: `/tmp/enola-codata-constant-review/v293-evidence.json`. No live Codata database was queried. JSX calls represent direct source render dependencies, not React runtime equivalence. Nuxt routes are static declarations, not proof of deployment.

## Next wave

Candidates #31–35 independently reproduce on the v293 production snapshot, with controls and pinned source integrity. They remain pending in the cumulative report: Vue default-as barrel export, destructured declarations, Markdown references to parsed TS files lacking file_ref, declaration-file resolution precedence, and isolated v2 constants. Current wave acceptance does not claim those are fixed.

Local `/tmp` evidence paths describe this review machine and are not durable repository artifacts. Committed regression tests and this review preserve the behavioral contract; the cumulative HTML preserves concise source examples and outcomes.
