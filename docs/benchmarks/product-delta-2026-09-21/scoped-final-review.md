# Final scoped-stage independent review

**GO for the bounded repaired scoped stage at `/tmp/enola-final-stage-source`.** The four unchanged independent Svelte reproductions now pass exact cold equality, and the path-contract checks pass. The benchmark-only ignore-overhead correction is independently verified, so the explicitly qualified selected-input comparison design is also GO; actual Product receipts, full validation, and performance acceptance remain coordinator-owned.

## Phase A: Product selected-input legacy comparison

**GO for the explicitly qualified selected-repository-input comparison design**, subject to each real run's exported physical manifest and main-inventory assertions passing. The coordinator-identified redundant ignore-glob overhead correction is now independently verified. This does not certify equal consumed work, old/new graph equality, or performance acceptance. The coordinator explicitly confirmed this claim boundary in message `msg_2a3a5f89a71e`; no Product exclusions should be changed to disguise supported facts or semantic differences.

Reviewed `benchscope.go.txt`, `harness_support.py`, HARNESS.md, relevant CLI invocation wiring, and the additional native-event diagnostic in `resident.py`/`resident-driver.go.txt`. No Product/harness/production edits, extraction runs, or large tests were made for this audit. Read-only detection helpers were compiled in scratch source copies; they do not run analysis or write Product caches.

### Provenance and actual profiles

- Product candidate root: `/tmp/enola-product-benchmark-source`; checked HEAD `a609c19f3861971930fae7b33dcb2950598953c5`.
- Audit: `/tmp/enola-product-selected-audit.json`; 11,127 selected regular files, consisting of 8,645 semantic and 2,482 name-only inputs, plus 670 directories. Candidate inventory has 11,127 Files, 11,127 AllNames, zero TestFiles.
- Pinned old binary `/tmp/enola-product-old` SHA-256: `5e28f6ed20cfaffcafec65a58320ba6fac57077859c72097c83bda07428c5ba7`. Independently compared 47 relevant production Go files in `/tmp/enola-product-bench-old` with `git show d086926aca80975dde34094e99c84cce59fb1cd6`; zero mismatches. This establishes inspected reader source provenance, not a new reproducible-build attestation.
- Actual candidate detection and pinned-old detection over the selected names both yield **hcl, manifests, mdintent, python, swift, typescript**. Candidate helper used the graph factory; old helper used the pinned bootstrap registrations and the engine's FileListDetector preference, with ordinary Detect fallback. The latter used the Product root for read-only detection rather than constructing/running a full mirror; root README and selected source predicates establish the six detections without analysis.
- Logs: `/tmp/enola-product-independent-inputs.log` and `/tmp/enola-product-independent-old-inputs.log`. Source helpers reside at scratch-only cmd/independentaudit/main.go in `/tmp/enola-scoped-independent-scratch` and `/tmp/enola-product-old-input-audit-scratch`.
- Full profile keeps the default registered extractors and default explainers/renderers. TS profile explicitly selects TypeScript and clears explainers/renderers. The supplied audit is full profile; each TS run must retain its own exported config/inventory receipt. Old `--generate` still performs its own output pipeline; candidate graph production has its own broker completion boundary.

### Concrete side-input findings

1. **No demonstrated active external/ancestor input loss at this Product revision.** Read the selected TS configs. `apps/mobile/tsconfig.json` extends `expo/tsconfig.base`; both reader implementations decline package-style extends, so this is not an installed dependency read lost by mirroring. `apps/safari-extension/tsconfig.json` extends `./.wxt/tsconfig.json`; that target is absent in the candidate and selected list. Other selected TS configs have no external extends; the tracking-client alias points to selected repository source. No Svelte config files are selected. This is a revision-specific finding, not a claim about every future history.
2. **Manifest ancestor lock lookup stops at repository root.** Pinned `manifestextractor` uses relative ancestry ending at dot/root; it does not climb to `/tmp` or mirror-container locks. Export physically excludes lock files, so old lock-resolution reads miss them too. The helper supplies the real bytes for name-only media; no old-only dummy or enlarged media is introduced.
3. **Other active extractors show no active external side input in this revision.** Selected Swift sources are two local module files; no selected project.yml/Package.swift/Podfile/Info.plist supplies an external XcodeGen include chain. No root project.yml exists. Root Python requirements/pyproject/setup/manage detector candidates are absent; the selected nested `analytics/dbt/pyproject.toml` remains available. HCL reads the selected `docker-bake.hcl`. Markdown uses selected inventory for link membership. Repository `enola-intent.yaml` is absent. No providers are configured.
4. **Confirmed version/discovery semantic difference:** `.deepsec/package.json` is selected and declares `deepsec: ^2.0.8`. Candidate graph manifest discovery honors policy and includes it; pinned old manifest extraction independently calls detectnames.Walk, which unconditionally prunes dot directories. Physical selected bytes are equal, but old does not consume this selected manifest. This is a legacy omission, not additional input artificially inflating old timing. The comparison must not claim equal consumer scope or equal facts.
5. **VCS and identity are explicit differences:** the mirror has no `.git`; old `gitInfo` consequently returns nil and falls back to a directory-derived repository label, while candidate graph input policy reads Git controls/tracked membership. Old avoids Git status/remote/provenance work; candidate pays policy discovery. Physical mirroring also avoids traversing excluded trees. These asymmetries favor old's input-discovery workload and belong beside timings. A mirror nested under an unrelated Git worktree could discover that ancestor: the inspected `/tmp`, `/private/tmp`, `/private`, and `/` have no `.git`, but final output placement must retain this condition. The helper does not explicitly disable ancestor Git discovery.
6. **Intentional graph semantics stay separate:** direct IO versus legacy transitive propagation, missing legacy dot-manifest facts, changed graph ownership/streaming, and full output pipeline differences are version semantics. They are not repaired by making the selected physical manifest equal. Candidate correctness must continue using a fresh candidate under the same profile, not the old graph as oracle.

### Mirror implementation and evidence limits

Export classifies directories and files with candidate policy, rejects selected nonregular files/symlinks, records byte hashes, and preserves actual name-only bytes. Sync re-reads and hashes every copied file, removes obsolete previously selected entries, retains only its own generated `.enola` output/cache aside from the selected tree, and verifies the complete physical file/directory manifest. Paths reject absolute/traversal/reserved `.enola` entries. The unchanged old detectnames walk prunes `.enola`, and engine config ignores it; retained legacy outputs are expected cache state, not extra extraction sources.

Post-run evidence again checks physical bytes and compares legacy snapshot main hashes with candidate inventory when snapshot metadata exists. **The helper does not fail merely because snapshot metadata is absent**: it records `legacy_main_inventory_available=false`. Therefore final claims of exact main-inventory equality require inspecting the actual archived evidence and a true `main_inventory_matches_candidate`, not just harness exit success. No real Product legacy run was executed by this reviewer, so none is certified here. No concrete extra old-mirror source input that would artificially inflate old parsing was found in the implementation or inspected Product dependencies.

### Fairness repair identified by coordinator

The initial export appends excluded-path ignores even in physical-mirror mode. Product audit config therefore contains thousands of redundant match patterns for files that are already absent, creating avoidable old-walker overhead. Coordinator reported this in `msg_6e29e59d16a2` and assigned a benchmark-only repair. This is an artificial timing cost, distinct from additional source inputs or intentional version semantics; do not accept timings from that original exporter as final. The corrected main-workspace benchmark helper now gates only generated `cfg.Ignore` additions behind `!export`; excluded-directory pruning and exported selection remain unchanged. Config-only compatibility still appends escaped exclusions. Independently reviewed the exact two-file diff from the production snapshot and reran:

```
PYTHONDONTWRITEBYTECODE=1 python3 docs/benchmarks/product-delta-2026-09-21/harness-probes.py --scope-tool /tmp/enola-mirror-ignore-benchscope --old /tmp/enola-product-old
```

Exit 0; all four bounded probe groups pass. Log `/tmp/enola-final-independent-mirror.log` confirms original-only export ignores, config-only escaped exclusions, selected physical bytes, tracked exemption/nested negation, unique output, symlink rejection, initial/delta pinned-old main-inventory equality, cleanup and timeout behavior. The production owner's report `/tmp/enola-mirror-ignore-fix.md` additionally records failure of this exact ignore regression against the prior helper; I did not rerun that negative control.

Reviewed helper source SHA-256 `cb973147f1864db73d57741a31ea955704887f7a2b9786e3d0d634b65c9359e2`, probe source `c385b077d57c95cefc631e64620d7e7e71693465fdd301e16fc15b1878d1df04`. These two files were updated separately after `/tmp/enola-final-stage-source` was frozen. The helper binary tested here was built by the bounded repair owner against the working source; coordinator must copy these benchmark-only artifacts into the final measurement snapshot and rebuild the helper there. This review does not describe the older frozen benchmark helper as repaired.

### Native idle diagnostic

The helper samples `ObservedEvents()` after ApplyChanges and any coverage catch-up. The watch/ignored-probe branch waits 200 ms to settle, captures a no-op response, waits 300 ms, captures another, and requires the native count to be present and unchanged. Sleeps are outside measured ApplyChanges. Existing online no-op checks separately enforce no graph work/events/generation advancement. This checks backend notification/hash feedback even if queue filtering hides graph work, but only over a bounded interval; it is neither a global filesystem barrier nor proof for unobserved excluded subtrees. AST parsing of changed Python files passed; no Product watcher run was repeated here.

## Phase B: final frozen production repair

**GO for the reviewed repair, with the limitations below.** Coordinator supplied `/tmp/enola-final-stage-source` in `msg_aeb4a2b144b1`. Copied it to `/tmp/enola-scoped-final-independent-scratch`, copied the previous independent probe file unchanged, and ran only bounded tests there. Main and frozen production files were not edited.

Reviewed the repair report `/tmp/enola-path-contract-fixes.md` and the precise production diff from the previous frozen stage:

- `context.go` computes the repository-relative directory through `factpath.Dir` instead of the host-path builder; package and alias context payloads remain unchanged.
- Markdown detection normalizes the `filepath.Rel` result immediately through `factpath.Slash`; no exemption or test weakening was introduced.
- `tsConfigInputs` now includes repository `svelte.config.mjs`, `nuxt.config.mjs`, and `next.config.ts` candidates, matching already-supported readers. It adds all supported JS/TS/MJS framework candidates at the actual selected TS root and Svelte candidates at actual alias roots returned by `collectTSAliasRoots`, including inherited alias roots and missing candidate slots. This follows the same roots as `withSvelteKitAliasFallbacks`; root fallback candidates remain covered.
- The existing graph-policy filter remains authoritative after enumeration. Existing raw capture, configuration fingerprint, explicit-path classification, and commit fencing receive these paths. Missing candidate additions trigger reconciliation; existing Svelte config sources are now recognized as configuration rather than ordinary source-only changes.
- These files remain unprojected raw-sensitive contexts, so Svelte config edits intentionally force whole-TS fallback. The fix does not claim selective reparse performance for Svelte or a parser-free resolution split. Extra reader walks add startup work that coordinator benchmarks must measure.
- The new captured context entries invalidate older in-memory/checkpoint semantics through the existing TS context/fingerprint comparison; the path-only fixes do not require changing fact payload semantics. Cache v274 remains the stage provenance.

### Independent execution

```
/tmp/enola-toolchain/go/bin/go test ./pkg/bootstrap -run '^TestIndependent(SvelteMJSContext|NestedSvelteContext)$' -count=1 -v
/tmp/enola-toolchain/go/bin/go test ./internal/facts -run '^TestPathContract' -count=1
/tmp/enola-toolchain/go/bin/go test ./pkg/bootstrap -run '^TestScopedFrameworkConfigCandidates$' -count=1 -v
```

All exit 0. The exact four previous failures—root MJS and nested JS/TS/MJS Svelte alias edits—now each report `Reconciled=true`, `RawConfigChanged=true`, an explicit `TS unprojected config <path> changed` reason, and 7 parses (6 semantic-context and 1 source-content) rather than stale source-only 1 parse. Each fixture independently compares baseline and changed transported graph to a new cold engine. Logs:

- `/tmp/enola-scoped-final-independent-probes.log`
- `/tmp/enola-scoped-final-independent-paths.log`
- `/tmp/enola-scoped-final-independent-candidates.log`

Reviewed the owner's additional regression matrix for resident/strict root/nested JS/TS/MJS add/edit/remove and unchanged publication; did not redundantly rerun that full matrix. The prior independent scoped/discovery/migration/native checks remain recorded in `/tmp/enola-scoped-independent-review.md`; this follow-up verifies the bounded delta rather than repeating the broad prior review.

Reviewed production SHA-256 values:

```
context.go: 71bff6071a874aad54bf8c501a149ca80a0aff5a5c5e5c97736446347b337d18
session.go: 1d5b81dc127949264314fede6ce7cef216e02f93d5f1b669c6517d154664bc1f
mdintent.go: 500b6e56aa732d14bd3bda3287d4a749acfe4a00a44209669dc84d4694d56f07
```

No Windows/backend portability execution, full suite, race/vet, broker run, Product extraction/history, repeated timing, or fresh CLI startup benchmark was performed by this worker. GO means the confirmed stale-graph blocker and bounded path-contract violations are closed in the tested snapshot. It does not mean performance acceptance or exhaustive correctness certification.
