# v293 historical baseline before observed-surface narrowing

All ten pinned first-parent Product transitions passed exact cold-versus-delta graph equivalence, frozen Begin/End manifest checks, and batch digest/count validation. This establishes correctness of the published baseline, not acceptance of the pending optimization.

Production baseline: c17be11, published as 9fc78ff with validation documentation. Binary SHA256: `906e4d3c6d1bbb47703c33134072456d9382eff20d511d58303f7dd8223ad45d`. Product history ends at a609c19f3861971930fae7b33dcb2950598953c5.

| Transition | Begin owners | Changed contribution owners | TS parses | Delta seconds | Cold seconds |
|---|---:|---:|---:|---:|---:|
| 4d104e600f89 → 07fb4a41ddaf | 1402 | 2 | 4 | 11.931 | 29.262 |
| 07fb4a41ddaf → fec1eac346c4 | 3166 | 83 | 2442 | 21.600 | 31.128 |
| fec1eac346c4 → 9fc7ae5b4c3f | 2327 | 17 | 1601 | 22.466 | 27.580 |
| 9fc7ae5b4c3f → 1c2607479b6d | 3237 | 32 | 428 | 26.372 | 26.350 |
| 1c2607479b6d → 599575d0aa39 | 150 | 6 | 21 | 8.214 | 26.613 |
| 599575d0aa39 → ae233c5f5695 | 8477 | 27 | 160 | 25.490 | 28.254 |
| ae233c5f5695 → 4168360e2e7f | 8477 | 10 | 4028 | 28.929 | 39.867 |
| 4168360e2e7f → a6f1f3a91a36 | 164 | 4 | 77 | 8.611 | 27.458 |
| a6f1f3a91a36 → 5dfb2c8f276d | 780 | 5 | 114 | 8.585 | 28.378 |
| 5dfb2c8f276d → a609c19f3861 | 706 | 7 | 58 | 7.566 | 27.513 |

Initial at 4d104e600f89 took 32.781 seconds and parsed 4012 files. The entire harness took 1889.031 seconds including repository cloning, checkout, cold oracles and validation. These figures must not be confused.

This is one diagnostic run using a file sink on a shared, contended host. It does not establish NATS acknowledgment latency, watch buffer behavior, Codata GraphHead latency, or an isolated performance improvement. Cold target times use fresh state; delta times include fresh CLI startup. Changed contribution counts are measured after both complete graphs exist and are not themselves a safe pre-Begin planning rule.

## Remaining work exposed by this baseline

- Broad reverse closure: the first case replaces 1402 owners for two changed contributions with four actual parses.
- Publication/assembly cost: the fourth delta takes about as long as its cold target despite only 428 parses.
- Non-TypeScript retirement: deleting the published `infra/pulumi/config/payment-seed-offers/README.md` in the sixth case is sufficient to trigger membershipScope whole-domain fallback. Narrowing ordinary TS preview propagation does not reach this path. Any relaxation must prove Markdown name/link/module retirement coverage.

Raw results: [results.v293-pre-scope.json](results.v293-pre-scope.json). Run receipt: [v293-pre-scope-receipt.json](v293-pre-scope-receipt.json). The harness self-test passed before the run. Raw events and state remain under `/tmp/enola-history-v293-pre-scope`; reproducible cold source clones were removed only after each successful case to bound disk use. Pinned revisions and policy provenance are retained in the results.
