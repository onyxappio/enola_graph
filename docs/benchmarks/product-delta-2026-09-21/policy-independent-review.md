# Independent frozen graph input integration review

**NO-GO for complete policy integration acceptance.** Focused scratch tests prove that policy-allowed inputs can still silently lose manifest and Markdown facts because retained discovery code imposes a second, non-configurable exclusion layer. The earlier reload/capture/symlink regression probes and the selected native policy/explicit-dependency checks pass on this frozen stage.

Reviewed `/tmp/enola-policy-stage-source` read-only after reading the three assigned implementation/review reports and repository rules. Main and frozen source were not edited; no Product mutations, benchmarks, full suite, or broker tests were performed. Scratch source is `/tmp/enola-policy-independent-scratch`.

## Confirmed blockers

### P1: Manifest extraction omits policy-allowed manifests

`internal/extractors/manifestextractor/manifest.go:158` performs an independent `detectnames.Walk`. Although that walk first applies inputscope, `internal/extractors/detectnames/detectnames.go:64` then unconditionally prunes dot directories and the names node_modules/vendor/testdata. A policy grant cannot override these additional exclusions.

The scratch `TestIndependentPolicyAllowedFacts` proves two cases with `ignore: []`, `graph_inputs.cache_exclusions: []`, and only manifests enabled:

- `.app/package.json` declaring `independent-probe: ^1.0.0` is Semantic, is in engine inventory, and activates manifests, but emits zero facts.
- `node_modules/local/package.json` with the same declaration is also Semantic and in inventory after the explicit cache override, activates manifests, and emits zero facts.

The first case needs no node_modules override to expose the extra dot-directory rule. The second directly contradicts the advertised overridable graph cache layer. Detection and extraction disagree about the same input. Cold/delta equality alone cannot detect this omission because cold analysis misses it too.

Graph-mode manifest discovery must use the configured policy consistently, preserving the legacy pruning only for the legacy profile if required. Keep lock exclusions and manifest no-lock semantics intact. Inspect other graph-mode independent walks for the same additional pruning pattern when fixing this specific boundary; this review does not claim to have reproduced every such consumer.

### P1: Markdown detector silently disables allowed documents

`internal/extractors/mdintent/mdintent.go:62` applies unconditional dot-directory and `mdSkipDirs` pruning after inputscope; the list at line 80 includes build/dist/tmp. It also retains an independent depth cutoff at line 66. The engine uses this detector rather than deciding from its already-policy-filtered names.

The third scratch case enables only mdintent, sets `ignore: []`, and puts `# Independent document` in `build/README.md`. Policy says Semantic and inventory contains the document, but detection is empty and the successful graph contains zero facts. This is an ordinary Markdown document the extractor can consume, not an unsupported language or an opaque media ambiguity.

Make graph-mode detection agree with policy-filtered document membership and the extractor's actual supported file predicate. This finding does not require changing the intentional `_archive`/`_views` content semantics. Only the build-directory case was executed here; the other retained detector restrictions are code observations, not extra reproduced findings.

Both blockers were promptly sent to the coordinator in escalation `msg_d8be5be20296`.

## Evidence

New scratch probe: [archived scratch probe](policy-independent-probes.go.txt).

```
/tmp/enola-toolchain/go/bin/go test ./pkg/bootstrap -run '^TestIndependentPolicy' -count=1
```

Log `/tmp/enola-policy-independent-probes.log`: the three allowed-fact subtests fail as described. The independent Python local-IO probe passes: one direct IO symbol, zero indirect `performs_io` symbols. Retained Python/Swift extractors contain upstream propagation internally, but graphsession's `applyLocalIO` normalization removes indirect attributes before graph publication/cache contribution; no published transitive-IO defect is asserted.

```
/tmp/enola-toolchain/go/bin/go test ./internal/graphsession -run '^TestIndependent(FailedReloadRevert|ExternalAncestorSymlink|ReloadConfigCaptureRace)$' -count=1
```

Log `/tmp/enola-policy-independent-repairs.log`: all three earlier independent probes pass (package reported 1.302s).

```
/tmp/enola-toolchain/go/bin/go test ./pkg/bootstrap -run '^Test(GraphRealWatcherPolicyScopeChange|GraphFactoryInitialConfigurationChange|GraphExplicitDependencyNativeEvents|IndependentPolicyLocalIO)$' -count=1 -v
```

Log `/tmp/enola-policy-independent-native.log`: all pass (package reported 3.867s), including last-TypeScript-owner retirement after policy exclusion, initial factory/config change, external TS config native events, and Swift YAML-in-media explicit dependency native events with cold equivalence. These package execution times are test diagnostics, not performance evidence.

## Other reviewed boundaries

- **Six consumers and detectors:** scoped filesystem read boundaries are present in TS/JS, manifests, Markdown, HCL, Python and Swift; manifest graph mode disables lock resolution. Unsupported active concrete consumers are rejected before publication. Name-based detection uses filtered engine inventory, while independent detectors use scoped reads/walks; the secondary pruning failures above remain.
- **Policy semantics and migration:** mandatory lock/VCS exclusions precede overridable exclusions, tracked paths override Git ignores but not explicit Enola exclusions, and immutable policy identity enters `analysisFingerprintInputs` under a versioned graph profile. Manifest no-lock ConfigKey prevents legacy lock-aware contribution reuse. This was code review, not a separately executed old-checkpoint migration test.
- **Names/media:** media membership remains available for Markdown links while ordinary content hash targeting and event classification omit name-only bytes. Declared TS/Swift context paths override media content filtering, with exclusions retained. The focused native tests independently validate the external TS and Swift media paths; no exhaustive media-extension test matrix was repeated.
- **Controls/registrations:** native construction installs policy before recursive registration; reconciliation refreshes policy and watch coverage, queues registration gaps, retains pending events and prunes excluded registrations. Index/HEAD/config dependencies reconcile while ordinary lock events are filtered. Previous external symlink-ancestor rejection now passes the focused gate probe. No cross-platform backend/topology proof is claimed.
- **Reload/retirement:** engine replacement rolls back when a transaction fails, config snapshots bracket reload/rebuild, and graph policy dependencies are validated before commit including the no-publication path. The retirement loop now includes TypeScript and the native policy-removal case matches cold output.

## Exact limits and next step

This review is bounded evidence, not a general correctness certification. It did not rerun the broad affected suite, race suite, old-cache migration matrix, every supported language's source-history delta, Linux/network-filesystem watcher behavior, a native backend flush/atomic capture proof, or real NATS replay/acknowledgment/backpressure checks. No additional stale-owner, exclusion-read bypass, or invalid-no-op blocker was established in the reviewed paths beyond the missing-fact findings above.

The already-known broad policy-identity invalidation cost remains assigned to the next scoped-delta task and is not reported as a new finding. Fix the two discovery boundaries, retain these focused reproductions, and have root run the final full suite plus real NATS/Product equivalence and repeated performance acceptance measurements; fresh CLI and resident measurements must stay distinct. This frozen source does not yet warrant final user acceptance.
