# Main 17226ed: Product history10 baseline

All 10 chronological transitions passed exact cold graph equivalence and frozen replacement protocol checks. This is a diagnostic baseline, not completion of incremental performance acceptance.

One run per transition, shared host, fresh CLI with a file sink (not resident watch or NATS acknowledgments). Product is the pinned historical cohort ending at a609c19f3861, not a newly fetched latest main. The binary and input-policy provenance are in [the JSON report](v304-main-history10.json).

| Case | Target | Raw changed paths | Begin owners | Owners with changed contributions | Parsed files | Delta s | Cold s |
|---|---|---:|---:|---:|---:|---:|---:|
| 0 | 07fb4a41ddaf | 6 | 7 | 2 | 6 | 4.066 | 24.501 |
| 1 | fec1eac346c4 | 9345 | 1811 | 83 | 624 | 12.639 | 23.353 |
| 2 | 9fc7ae5b4c3f | 8125 | 2266 | 17 | 69 | 12.250 | 22.658 |
| 3 | 1c2607479b6d | 70 | 1119 | 32 | 433 | 9.751 | 21.703 |
| 4 | 599575d0aa39 | 15 | 9 | 6 | 7 | 4.378 | 22.381 |
| 5 | ae233c5f5695 | 60 | 8477 | 25 | 101 | 19.714 | 22.915 |
| 6 | 4168360e2e7f | 15 | 8477 | 10 | 4028 | 23.864 | 25.233 |
| 7 | a6f1f3a91a36 | 5 | 80 | 4 | 80 | 5.051 | 26.051 |
| 8 | 5dfb2c8f276d | 15 | 743 | 5 | 93 | 6.706 | 25.378 |
| 9 | a609c19f3861 | 19 | 663 | 7 | 37 | 5.738 | 23.117 |

Changed contributions are measured retrospectively by the cold oracle. They are not proof that an equally small pre-Begin scope can safely be computed. Raw path changes include excluded/nonsemantic inputs. Scope size and parse count measure different costs.

## Follow-up findings

- Case 05 deletes a published Markdown owner. membershipScope treats a retired non-TypeScript owner as an unproved boundary and expands to all owners: 8,477 versus 25 changed contributions, with 101 parses. Narrowing needs proof for removed candidates and their consumers, not deletion of the guard.
- Case 06 adds drizzle-orm to one package devDependencies. The global owning-package-gates context hash changes and forces 4,028 parses. A separate small regression reproduces 5 parses instead of the affected package file; cold equivalence still passes. Per-file effective gate context is a candidate fix, with truly global readers retained.
- Case 01 reparses 528 files for resolution in addition to 96 added/content-changed files. It needs separate diagnosis of resolution work versus local-fact parsing.

## Artifact retention

The bounded-artifact wrapper deletes successful cold source/state directories after saving results and state digests. Failed cases are retained. Raw event output and per-case summaries remain under /tmp/enola-stage4-main-history10; the committed JSON report preserves results and binary provenance. Original runner exit: 0.
