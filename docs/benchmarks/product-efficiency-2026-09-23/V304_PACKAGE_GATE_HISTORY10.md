# Package-gate optimization: ten Product history transitions

All ten chronological transitions ending at pinned Product `a609c19f3861971930fae7b33dcb2950598953c5` pass exact cold graph equality and strict frozen Begin/End manifest and batch verification. The harness exited 0. The frozen binary was labelled exploratory when built; all three production source hashes were subsequently matched to pushed `f0101f1`.

| Case | Changed paths | Begin owners | Parsed TS | Actually changed owners | Delta s | Cold s |
|---|---:|---:|---:|---:|---:|---:|
| 00-4d104e600f89..07fb4a41ddaf | 6 | 7 | 6 | 2 | 3.791 | 30.929 |
| 01-07fb4a41ddaf..fec1eac346c4 | 9345 | 1811 | 624 | 83 | 15.084 | 25.381 |
| 02-fec1eac346c4..9fc7ae5b4c3f | 8125 | 2266 | 69 | 17 | 15.029 | 22.201 |
| 03-9fc7ae5b4c3f..1c2607479b6d | 70 | 1119 | 433 | 32 | 9.458 | 22.841 |
| 04-1c2607479b6d..599575d0aa39 | 15 | 9 | 7 | 6 | 3.800 | 21.582 |
| 05-599575d0aa39..ae233c5f5695 | 60 | 8477 | 101 | 25 | 21.917 | 21.579 |
| 06-ae233c5f5695..4168360e2e7f | 15 | 778 | 111 | 10 | 6.799 | 24.548 |
| 07-4168360e2e7f..a6f1f3a91a36 | 5 | 80 | 80 | 4 | 5.664 | 25.333 |
| 08-a6f1f3a91a36..5dfb2c8f276d | 15 | 743 | 93 | 5 | 6.625 | 24.587 |
| 09-5dfb2c8f276d..a609c19f3861 | 19 | 663 | 37 | 7 | 6.097 | 27.471 |

The package-gate transition (case06) drops from 4028 parses / 8477 Begin owners in the earlier baseline to 111 / 778. Every other case retains its baseline parse and scope counts. Case05 still uses the whole 8477-owner domain for a non-TypeScript retirement; case01 still has 528 resolution parses among 624 total. Those are remaining targets, not solved by the package-gate patch.

Actually changed owners are retrospective comparisons with the cold oracle, not proof that a planner could safely announce exactly that set before Begin. Times are single file-sink observations on a shared host with concurrent validation; do not treat historical baseline-vs-candidate timing variation as causal. The separate ABBA case06 experiment supplies repeated timing evidence. This history uses candidate state from its initial parent, so it excludes the one-time v3-to-v4 migration cost.

Disposable cold checkout/state directories were removed only after each case passed cold/protocol checks; event streams, summaries, graph hashes and cleanup receipts remain in `/tmp/enola-stage5-exploratory-history10`. Failed cases would have been preserved.

[Results and provenance](v304-package-gate-history10.json) · [Repeated package-gate benchmark](V304_PACKAGE_GATE_HISTORY.md)
