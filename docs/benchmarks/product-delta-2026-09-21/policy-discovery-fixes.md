# Graph policy discovery fixes

Completed the two discovery NO-GO findings within the assigned boundaries, without commits, resets, Product mutations, or edits to other workers' files.

## Changes

- `internal/extractors/detectnames/detectnames.go`: retain dot-directory/node_modules/vendor/testdata pruning only when no graph policy is attached. Scoped graph walks use authoritative policy membership, including explicit cache overrides; mandatory VCS/lockfile and configured exclusions remain enforced by inputscope.
- `internal/extractors/mdintent/mdintent.go`: graph detection no longer adds directory-name or depth exclusions after policy classification. It uses the same ContentInput predicate as extraction; intentional `_archive/` and `_views/` semantics remain unchanged. Legacy detection retains its pruning and depth bound. Extraction now calls the existing equivalent ContentInput predicate rather than duplicating it.
- `internal/extractors/detectnames/policy_discovery_test.go`: new graph-policy and nil/empty-scope legacy walk regression.
- `internal/extractors/mdintent/policy_discovery_test.go`: new graph/legacy detection and graph fact tests covering allowed paths, depth, explicit exclusions, and archive/view semantics.
- `pkg/bootstrap/policy_discovery_test.go`: eight end-to-end graph allowed-fact cases adapted from the independent scratch reproduction, verifying semantic classification, inventory membership, and actual file-owned facts.

The two production files already had uncommitted scope wiring before this task; that work was preserved. Only the discovery restrictions and predicate deduplication described above were changed by this task.

## Exact validation commands and results

Before changing production code:

```
/tmp/enola-toolchain/go/bin/go test ./pkg/bootstrap -run '^TestPolicyDiscoveryAllowedFacts$' -count=1 > /tmp/enola-policy-discovery-before.log 2>&1
```

Exit 1: all eight subtests failed with policy-allowed inputs present in inventory but no facts. Cases: dot manifest, cache-override node_modules manifest, dot Markdown, deep Markdown, cache-override node_modules Markdown, vendor manifest, testdata manifest, build README. This reproduces all three independent review findings and five additional directory/depth cases.

After fixes:

```
/tmp/enola-toolchain/go/bin/gofmt -w internal/extractors/detectnames/detectnames.go internal/extractors/detectnames/policy_discovery_test.go internal/extractors/mdintent/mdintent.go internal/extractors/mdintent/policy_discovery_test.go pkg/bootstrap/policy_discovery_test.go
/tmp/enola-toolchain/go/bin/go test ./internal/extractors/detectnames ./internal/extractors/mdintent ./internal/extractors/manifestextractor -count=1 > /tmp/enola-policy-discovery-extractors.log 2>&1
/tmp/enola-toolchain/go/bin/go test ./pkg/bootstrap -run '^TestPolicyDiscoveryAllowedFacts$' -count=1 -v > /tmp/enola-policy-discovery-after.log 2>&1
```

All exited 0. Full affected extractor packages passed: detectnames 0.353s, mdintent 1.510s, manifestextractor 0.478s. All eight bootstrap allowed-fact cases passed (package 3.166s). These timings are test diagnostics, not performance acceptance evidence.

## Coordination and limits

Coordinator acknowledged that graph profile/cache migration and its regression belong to scoped-delta owner `ctx_dc0183d7ace9` (message `msg_47c46a127e8d`); no engine/profile/cache files were edited here. Full suite, Product performance/cold-delta acceptance, broker tests and migration validation remain coordinator-owned and were not claimed by this focused task. Graph-mode directory policy is authoritative; legacy pruning and document content semantics are covered by tests.
