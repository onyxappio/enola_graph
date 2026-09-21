# Independent review of frozen scoped-delta stage

**NO-GO: confirmed resident cold-versus-delta graph inequality for supported Svelte alias configuration changes.** The validity-domain separation and bounded scoped fixtures otherwise pass, including the prior discovery blockers, v273-to-v274 migration fixture, and native metadata quietness check. This is a correctness review, not Product or performance acceptance.

Reviewed `/tmp/enola-scoped-stage-source` read-only, including AGENTS.md, CONTRIBUTING.md, the streaming contract, and the three assigned reports. Production in main and the frozen snapshot were not edited. All executions used `/tmp/enola-scoped-independent-scratch`; only a new scratch probe and this report were written, with logs outside the repositories.

## Confirmed blocker: actual Svelte alias readers bypass resident configuration invalidation

Severity P1. `internal/extractors/tsextractor/svelte.go:180` (`withSvelteKitAliasFallbacks`) reads `svelte.config.js`, `.ts`, and `.mjs` at effective alias roots. `internal/extractors/tsextractor/session.go:630` (`tsConfigInputs`) captures only root `.js` and `.ts` candidates, and its nested walk discovers package/TS/JS config JSON files rather than these Svelte files. Root `.mjs` and nested Svelte config files therefore become ordinary TS-owned source edits.

`internal/graphsession/resident.go:355` (`contentInputs`) permits these known sources through the content fast path because they are absent from `r.inputs.config`. It retains the committed `tsContext` and `tsFileContext`. Extraction reads the new alias map but reuses unchanged importing file records, so it publishes a successful generation with stale direct import targets. The actual-reader projection in `context.go` can represent the changed aliases during reconciliation, but the resident classification never invokes it for these edits.

### Executed reproduction

Scratch file: [archived scratch probes](scoped-independent-probes.go.txt).

1. Using the five-source scoped fixture, add `svelte.config.mjs` containing `export default {kit:{alias:{'@chosen':'./a.ts'}}}` and `consumer.ts` importing `a` from `@chosen`.
2. Apply the additions and verify exact fresh cold graph equality; this passes.
3. Change only the config alias target to `./unrelated.ts`, then pass `svelte.config.mjs` in a covered `ApplyChanges` batch.
4. The result reports `ParsedFiles=1`, `Reconciled=false`, `RawConfigChanged=false`, zero context-affected sources and no context reasons. Exact consumer canonical graph equality with a new cold engine fails.

`TestIndependentNestedSvelteContext` repeats the same experiment for `packages/database/svelte.config.js`, `.ts`, and `.mjs`, using an actual nested tsconfig alias root and a root `@sveltejs/kit` dependency. All three subtests independently fail cold equality, again parsing only the configuration source. The nested fixtures first verify baseline cold equality before the edit.

Commands:

```
/tmp/enola-toolchain/go/bin/go test ./pkg/bootstrap -run '^TestIndependentSvelteMJSContext$' -count=1 -v
/tmp/enola-toolchain/go/bin/go test ./pkg/bootstrap -run '^TestIndependentNestedSvelteContext$' -count=1 -v
```

Logs: `/tmp/enola-scoped-independent-probes.log` and `/tmp/enola-scoped-independent-nested.log`. All four cases fail as described; this is not a speculative unsupported configuration request. Static kit aliases in these filenames are already read by the extractor.

Sent blocker immediately in `msg_41e7a07cd1a5`, then confirmed nested cases in `msg_cf447d9631a6`. The repair must cover the actual reader paths in configuration capture and resident change classification, including nested candidates, rather than only adding root `.mjs`. Preserve policy filtering and cold equivalence; retain the reproductions. No production repair was attempted in this review.

## Verified boundaries

- **Validity separation:** `State` persists raw `ConfigHash`, semantic `EngineContextHash`/TS maps, and selection `PolicyIdentity` separately. Graph sessions no longer use raw package bytes or policy identity alone as universal TS parse invalidators. Framework/root/client outputs are global; nearest package and effective aliases are per source. Cached-state version mismatch remains a conservative rebuild.
- **Raw fences:** enumerated configuration bytes remain captured and compared before successful completion, including source-only transactions using captured context bytes. No-publication reconciliation recomputes the raw fingerprint and validates effective config/policy. The raw-package-mid-parse rejection/recovery fixture passes. This positive finding applies to enumerated inputs; the Svelte coverage omission above prevents a completeness claim.
- **Reader projection:** the global detector calls match ExtractSession's framework/ORM gates; per-file alias maps include replacement, suffix and exact semantics. Scoped package and alias fixture checks pass with independent cold engines. Prisma/Angular and other enumerated unprojected config retain raw-sensitive fallback; malformed JSON/JSONC, typed tsconfig errors, unsupported package extends, and recovery checks pass. No generic manifest dependency whitelist substitutes for the actual detectors.
- **Selection and retirement:** tracked source addition parses one new source without a global TS-context reason. Re-export/rename/removal/exclusion and last-TS-owner native retirement fixtures pass cold equality. Fixed-point/resolution broadening remains present; this change does not replace it with a one-hop cutoff.
- **Migration:** cache provenance is v274 and engine/input/TS identities are versioned. The fixture seeds legacy discovery contributions with a checkpoint marked v273, rebuilds under graph mode, recovers the previously missing facts, and matches fresh cold. This is a migration regression fixture, not execution of an archived v273 binary.
- **Discovery repair:** graph detectnames uses policy without legacy dot/node_modules/vendor/testdata pruning; graph Markdown detection uses ContentInput without the legacy depth/directory cutoff. All eight end-to-end allowed-fact cases pass, confirming actual facts rather than merely matching two empty graphs. Legacy detection and archive/view behavior remain scoped separately in the focused extractor tests.
- **Git metadata:** the CHMOD guard checks only a captured matching index/HEAD/config path and compares readable SHA-256 bytes. Pure identical CHMOD is dropped; changed/missing bytes and write/create/rename/remove/combined operations reconcile. The deterministic control test passes. The native fixture observes actual index deliveries, checks zero reconciliation/generation changes for identical metadata, then checks aggregate event-count quietness over its idle window and a true tracked-exemption change against cold. The native true-change step edits both the index and gitignore; the deterministic test independently isolates changed captured control bytes.

## Executed checks

All commands below ran only in the scratch copy:

```
/tmp/enola-toolchain/go/bin/go test ./pkg/bootstrap -run '^(TestScoped|TestPolicyDiscoveryAllowedFacts|TestGraphRealWatcherPolicyScopeChange)' -count=1 -v
/tmp/enola-toolchain/go/bin/go test ./internal/graphsession -run '^TestGitControlMetadataRequiresIdenticalCapturedBytes$' -count=1 -v
/tmp/enola-toolchain/go/bin/go test ./internal/extractors/detectnames ./internal/extractors/mdintent -run '^TestPolicyDiscovery' -count=1 -v
```

Logs are `/tmp/enola-scoped-independent-focused.log`, `/tmp/enola-scoped-independent-controls.log`, and `/tmp/enola-scoped-independent-discovery.log`. The selected bootstrap matrix and control check pass; extractor discovery results are recorded in the final log. Reported test durations are diagnostics under concurrent load, not benchmark results.

## Caveats and acceptance

The coordinator separately reported full-suite fact-path contract failures in `/tmp/enola-scoped-stage-full-test.log:64-74`, assigned for repair elsewhere; this review neither modifies nor independently adjudicates that repair. Previous resident reload/capture/symlink issues were not reported again as new findings.

No full suite, race/vet, Product access, broker benchmark, replay/backpressure experiment, cross-platform watcher test, or fresh-CLI latency benchmark was run here. The bounded native idle window does not establish Product-scale backend behavior. Scoped reparsing fixtures demonstrate useful invalidation narrowing but do not establish near-zero no-change latency or accepted edit latency; whole-project inventory/context construction and other documented costs remain.

The requested independent review is complete. Final stage acceptance remains NO-GO until the confirmed Svelte config invalidation gap is repaired and its regression probes pass, followed by coordinator-owned full validation and real Product/broker equivalence and performance evidence.
