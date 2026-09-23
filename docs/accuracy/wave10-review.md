# Wave 10 — independent accuracy review

Verified implementation: `8e19a6fb650f40464658ea403d2064da656bfe6b`, cache **v314**.
Includes main `3957b50cf721a04262395589b97d4ac4d1587d03`, retaining discovery reuse and the fresh input-policy proof.
Independent binary SHA256: `0a26750c71e90e62521f6770e23b7336828415aa42c5df0e5a85f194739102f1`.

## Behavior

| ID | Source-backed correction |
| --- | --- |
| 46 | Nx Tree filesystem deletion does not create an HTTP client route. Legitimate HTTP receivers retain routes. |
| 47 | Nuxt page routes have direct `handled_by` links to their unique same-file component. |
| 48 | TypeScript methods and constructors have direct `declares` links to their enclosing class. |
| 49 | Ordinary default-call results are variables; proven callable factories remain functions. |
| 50 | Default objects/arrays create value nodes; imports/reexports bind the actual exported identity. |

Primary review additionally protected Ember gts/gjs test references, Svelte default imports, Ember buildRoutes, Nuxt factory provenance and H3 lazy handlers. Explicit nested Nuxt configs establish applications under plain ancestor packages; a more-specific plain package blocks inherited implicit Nuxt behavior. Config/manifest changes invalidate the affected cached scope. Default reexport changes no longer retain stale source reads.

## Validation

- Seven final gate groups passed, including 13 broader groups with 22 previous regression scripts, provenance controls and cold/delta lifecycle checks.
- Full `GOMAXPROCS=1 go test -p 1 ./...`: **PASS, 721.798 s**.
- Four supported forks of published v304 checkpoints migrated to v314. Exact applied-versus-cold record equality, fork identity, zero-parse/event no-change and stable generation passed for both protocols and repositories. No raw cache copying.
- Full wave 5–10 semantic assertions passed, plus all nine real Nuxt plugin defaults and the H3 lazy handler callable guard.
- Earlier v310/v312/v313 correction migrations passed on the pre-main2 v314 binary. These are historical receipts, not rerun claims. Final full-repository migrations used the exact binary above.

| Protocol | Repository | Node records | Edge records | Cold = delta | Silent no-change |
| --- | --- | ---: | ---: | --- | --- |
| v1 | Product | 71,103 | 148,052 | PASS | PASS |
| v2 | Product | 69,765 | 148,052 | PASS | PASS |
| v1 | Landings | 23,180 | 48,802 | PASS | PASS |
| v2 | Landings | 22,885 | 48,802 | PASS | PASS |

These are protocol record counts, not unique logical-entity counts.

## Source and graph audit

Product inputs: `a609c19f3861971930fae7b33dcb2950598953c5`; Landings: `e263a0942efce56e9cd02abc425c0aa4d0327632`. All 13,650 included Product files and 3,293 Landings files match their Git blobs. Product's 31,455 omitted tracked files are all in the authorized `apps/mobile/e2e/artifacts/**` or `worker-reports/**` exclusions; no unexpected missing or changed files.

Complete Product graphs equal the previously source-reviewed v310 graphs in both protocols. Landings differs only in ten symbol records: nine Nuxt plugins and one H3 handler. Each restored record, including function metrics, exactly equals its correct published v304 record; no edges changed from v310. This exact comparison carries forward the prior source review without treating expected-looking differences as sufficient evidence.

Against published v304, all 390 removed resolved Product edge records retain their direct relationship in the full final graph. Landings retains 177/178 in v1; the sole disappearance is ownership of the false Nx `DELETE /.gitkeep` route. In v2 that obsolete ownership edge was already unresolved; all 177 removed resolved records retain their relationship. Occurrence records remain intact in replay comparison; this audit does not deduplicate them.

Prior source checks, retained by exact graph comparison, cover 193 method/class ownership links, 12 default-value/module links and 93 route/same-file-component links. The complete node/property/edge diff was reviewed, not only candidate-specific checks.

## Reproduction evidence

Local acceptance receipt: `/tmp/enola-wave10-final-acceptance.json`.
Final build, gates, full Go and full-repository job receipts use `/tmp/enola-wave10-v314-main2-*`.
Four registry entries: `/tmp/enola-wave10-final-full-review/{v1,v2}-{product,landings}.json`; each points to immutable run outputs, exact graph comparison, source samples, relation-survival review and v310 comparison. Their migration states are consumed; preserve them instead of rerunning migrations in place.

The full Go failure before main2 was two missing host-path markers. Commit `6e96ca5` changed comments only, retained an identical executable hash and passed the full suite. Main2 integrated the equivalent upstream annotations; the final full suite above passed. Original failure receipts are retained.

## Report and limits

`report.html` retains the cumulative cards and annotated source examples, rendered offline with Twinkleplop. Code text is checked unchanged during rendering; mobile overflow and external resources are checked. Two Swift examples remain plain text.

This establishes the listed fixes on the pinned repositories and controls, not universal analysis completeness. No new performance benchmark is claimed. Candidates 51–55 remain for the next coding wave; continuous research and independent verification continue under the user's auto-approval policy.
