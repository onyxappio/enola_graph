# Final fast-delta review — GO

2026-09-21. Independent read-only review of frozen `/tmp/enola-product-fastdelta-source` against untouched `/tmp/enola-product-candidate2-source`; task `task_f687d2c46418`, dispatch `ctx_cb0e9a3b3b66`.

**GO for the coordinator's final validation and isolated Product comparison. No actionable blocking regression found in the reviewed optimization scope.** This is bounded correctness evidence, not performance acceptance or a claim that every historical extractor side read is closed.

## Reviewed changes

- Read both `/tmp/enola-final-correctness-pass.md` and `/tmp/enola-delta-input-contract-review.md`, then compared production diffs independently.
- GlobSet retains original matcher fallback and first-match ordering, uses literal-segment and safe extension prefilters, and is local to each walk. No shared compiled state remains on Engine. Existing non-walk helpers retain original semantics.
- Selective hashing is capability-based, not extractor-name or FileOwner-based. Opaque/custom extractors retain AllNames hashing, and prior tracked files remain hashing candidates in the selective path. Built-in predicates cover the intended inventory-driven source inputs; markdown separately observes production name membership.
- Manifest context rediscovers using its original walk, includes manifest bytes and ancestor lock candidates, and records missing/error states. The five formerly omitted lock names are present. Each extractor consumes its own context in its v3 digest rather than depending on an unrelated extractor's shared hashes.
- Swift context mirrors root project.yml and enabled root includes, including case-insensitive fallback after read/parse failure, plus the exact shallow iOS-marker boolean. It deliberately does not recurse into nested include lists. Removal/absence is reflected in context values.
- grpc, OpenAPI and AsyncAPI production files are byte-identical to candidate2. Markdown detection is restored to its independent original walk. TS ConfigInputPaths and SessionResult.ConfigPaths retain the independent config walk; changes to session extraction itself are profiling only.
- Whole-extractor output changes include prior synthetic owners in replacement scope. Checked removal behavior and retained synthetic-resolution probes.
- Cache version is v273; old extractor versions force full analysis. State schema remains v1 and added maps have nil-safe load handling. Legacy digest compatibility branches were inspected; actual old-version migration with absent per-extractor input/synthetic maps was exercised, rather than treating missing digests as proof of current context equality.
- Context recapture precedes successful EndReplace/state advancement. Existing consumed-source and captured-config protections remain. Profiling trace mutation is mutex protected, and local glob compilation removes the identified shared-engine race.

## Independent execution

All Go commands used `/tmp/enola-toolchain/go/bin/go` in the private copy `/tmp/enola-fastdelta-final-private`. No builds/tests ran in frozen source or main, and no Product benchmark or NATS timing ran.

1. Focused retained probes across graphsession, facts, engine and Swift passed: independent glob equivalence, opaque/custom hash dependencies, arbitrary AsyncAPI JSON, pruned TS config, hidden manifest/Swift inputs, all five lock inputs and ancestor precedence, detector original scopes, concurrent inventory, same-size/restored-mtime edits, review owner/resolution/local-IO cases and Swift include/marker semantics. Exact selection and results are in `/tmp/enola-fastdelta-final-focused.log` (all four packages passed).
2. `go test -race ./internal/engine ./internal/graphsession -run 'TestConcurrentInventory|TestIndependentOpaqueInputs|TestBuiltinHiddenInputDelta|TestFinalContextRemoval' -count=1` passed; `/tmp/enola-fastdelta-final-race.log`.
3. New private probes in `/tmp/enola-fastdelta-final-private/internal/graphsession/final_removal_probe_test.go` passed:
   - Hidden manifest deletion while root package.json keeps detection enabled: applied delta equals cold.
   - Enabled Swift include removal and root project.yml removal: applied delta equals cold, including another subsequent run.
   - v272 state with missing input-digest and extractor-synthetic maps, followed by Swift target rename: rebuild equals cold; the next unchanged run has zero events, zero parses and unchanged generation.
   - Swift include mutated deliberately after Extract returns: Run rejects changed context and does not write committed state.
4. Additional existing source-consistency and closure probes passed: TestConfigChangeRestoreUsesCapturedBytes, TestSourceHashUsesConsumedBytes, TestConsumedHashMatchesBytes, TestDeepReexportClosureParsesChainAndPreservesIndependents, and TestDeleteAndRenameAppliedEqualsCold. These and all new private probes are logged in `/tmp/enola-fastdelta-final-extra.log`.

## Limits

No new loss of old scan/detection signals was identified. Previously documented TS framework side reads and opaque extractor reads outside their old AllNames universe remain historical limitations, not newly introduced regressions established by this review. Context revalidation is not a transactional filesystem snapshot and does not promise protection against arbitrary edit-and-restore schedules. Missing-digest compatibility was reviewed in code and exercised through an actual old-version migration, not every artificially corrupted current-version state permutation.

The coordinator owns the full build/test/race/vet outcome and isolated repeated broker-acknowledged Product comparisons, including initial/delta ratio, spread and cold/delta equivalence. No production files were modified, and no commit, push or Telegram message was made.
