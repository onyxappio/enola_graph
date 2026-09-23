# Astra follow-up: frozen route/name-delta gaps (task_0602520ecf73 / ctx_da26da63a66e)

No commit/push. Product benchmarks remain root-owned. Package green is not a release claim.

Root already proved the prior route-identity patch narrows Product body (`owners=2`, parsed=1, events=4). This follow-up addresses Astra REQUEST CHANGES on that patch.

## Fixes

1. `routeDigestByFile` fingerprints the full canonical KindRoute payload via `factsFingerprint` (kind/name/file/line/column/props including method+framework/relations). Repo is cleared so blank cache vs tagged session facts compare equal. Method or line changes grow the owner.

2. `composedRouteFacts` clones record facts plus Express `ComposedMountRoutes`, then `tsextractor.ComposeEngineMounts`. Ember mount rewrites land on child engine route owners before Begin and in the post-Begin check. Cached FileRecord facts stay relative (clone, not mutate).

3. `ownersForNameDelta` diffs per-name candidate identity **sets** (Repo-cleared kind+name+file) for dirty preview vs cache, then extraOwners every cached owner whose facts/Declared/Referenced mention a changed name. No `>100` threshold. Replaced the old whole-domain `resolutionCandidatesChanged` trigger.

4. Mount tests now fail if extraction is empty: initial `/api/orders` must exist, delta must publish `/v2/orders` and drop `/api/orders`; nested `/api/v1/orders` → `/v2/v1/orders`; Ember `/store/cart` → `/v2/cart`. Cold consumer equality on each.

## Tests (`PATH=/tmp/enola-toolchain/go/bin`)

| Run | Duration | Result |
| --- | --- | --- |
| Planner route/name-delta units | 0.556s | ok |
| Frozen mount/nested/ember/unresolved/body/fail-closed + planner | 3.959s | ok |
| Ember extractor `TestEmberEngines` | 0.402s | ok |
| Frozen+invalidation+route suite | 12.113s | ok |
| Full `go test ./internal/graphsession -count=1 -timeout 180s` | 88.865s | ok |

Regressions added:

- `TestRouteDigestByFileHashesPayloadAndNormalizesRepo`
- `TestComposedRouteFactsAppliesEmberEngineMounts` (child flagged; cache not mutated)
- `TestOwnersForNameDeltaSameNameKindChangeIncludesReferencers`
- `TestOwnersForNameDeltaUnresolvedBecomingDeclarationIncludesReferencers`
- `TestFrozenEmberEngineMountPathChangeIncludesChildOwners`
- `TestFrozenUnresolvedNameBecomingDeclarationIncludesReferencers` (`src/b.ts` calls `Foo()` with no import of `src/a.ts`)

## Remaining limits

- Angular `composeAngularRoutes` is not reconstructed from FileRecords (lazy `loadChildren` arrays are not a persisted DTO). A parent-only Angular route-array edit that rewrites other files' composed routes can still miss extraOwners. Express RouterDTO + Ember engine_mount facts are covered.
- Dual-mounted Ember engines skip composition by extractor design (relative routes remain).
- Product Begin-count on this follow-up patch is root-owned; do not treat package green as release.
