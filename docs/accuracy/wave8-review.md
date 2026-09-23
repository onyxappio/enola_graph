# Wave 8 independent acceptance

Implementation: `7c252626a45900e5b797c661afaca7e915d397d6`, corrected by `9704e347140049c448d6fdadb287a6c3fe22005d` (cache v297). Reviewed integration: `6bfe477496aca5e619118ea0b622bc9de78d3e79`, including main `b4179262ac2327ec555c78f5c23a4331c44d9946`.

Independent binary SHA256: `45afd38a3b70e8c0fc2d14c34b93c05ccb9c7a028436c35aaf751a64f3449733`.

## Accepted behavior

| Candidate | Verified behavior |
| --- | --- |
| 36 | Vue SFC component retains value identity when a local type has the same name; both nodes survive. |
| 37 | File-scope non-function/destructured bindings carry their own file provenance. |
| 38 | JSX file references retain exact import/local target files. |
| 39 | Literal awaited destructured imports bind function calls and file references to imported targets in lexical scope. |
| 40 | Extensionless imports find declaration-only modules after implementation candidates. |

Primary review rejected two first-draft behaviors: unawaited Promise destructuring bound to exports, and nested patterns bound to their container export. Final v297 controls prove those false bindings absent. Nested object/array import patterns remain unsupported; this wave does not claim complete dynamic-import or lexical-scope coverage.

## Validation

- Full `go test ./...`: exit 0 on integrated code.
- Seven independent targeted suites: component lifecycle; cases 37–40 lifecycle; dynamic scope controls; previous resolution controls. All exit 0.
- Five additional regression suites: wave5 negatives, Nitro aliases/shadowing, v1/v2 deltas, wave6 lexical/JSX controls. All exit 0.
- Full Product and Landings, both v1/v2: all five semantic checks pass, including both function and file-reference edges for case 39.
- Real v295 state migrated to v297 equals fresh v297 graph exactly, including owners, provenance and occurrences. Unchanged runs parse zero files, publish zero events and preserve generation.
- v2 owner scope is frozen and file-only; v1 batch-owner semantics are checked separately.

Pinned inputs: Product `a609c19f3861971930fae7b33dcb2950598953c5`; Landings `e263a0942efce56e9cd02abc425c0aa4d0327632`. Product exclusions remain `apps/mobile/e2e/artifacts/**` and `worker-reports/**`; no blanket `state` directory exclusions.

| Protocol | Product nodes / edges | Landings nodes / edges |
| --- | --- | --- |
| v1 | 71,096 / 168,710 | 23,176 / 55,755 |
| v2 | 69,758 / 168,710 | 22,881 / 55,755 |

## Graph and golden review

Product edges decrease from 170,153 to 168,710. All 1,642 reduced multiplicity groups retain the same resolved target: JSX and identifier uses now share exact provenance and the extractor's existing target/file deduplication applies. The 21 removed source/name groups have replacement imported targets: 19 retain the export leaf name, and two `dig` alias records correctly target `imageManifestDigest`. Three old resolved references in the Safari preview script now point to the actual awaited-import exports instead of local destructured bindings. There are 220 new source/name groups. Occurrence changes are expected; complete migration/cold comparisons preserve and check them rather than discarding them.

The extractor golden update adds target-file provenance to two existing calls (`statusUrl`, `admin`); no golden fact additions or removals. No global FactID algorithm change.

## Evidence and limitations

Local receipts: `/tmp/enola-wave8-final-acceptance.json`, `/tmp/enola-wave8-integrated-gates.json`, `/tmp/enola-wave8-integrated-recent-regressions.json`, `/tmp/enola-wave8-final-{v1,v2}-semantics.json`, and `/tmp/enola-wave8-full-review/` descriptors. These are local evidence paths, not portable dependencies.

A worker cleanup deleted earlier temporary evidence. Primary independently restored the fixtures and rebuilt all four real v295 baselines before final migration validation. Private restored-artifact archive exists outside the repository; acceptance does not rely on the deleted evidence.

This is accuracy acceptance, not a new performance benchmark or a claim that every entity kind is covered. Wave9 research is provisional until independently reproduced on accepted v297. Continue in waves under WORKFLOW.md.
