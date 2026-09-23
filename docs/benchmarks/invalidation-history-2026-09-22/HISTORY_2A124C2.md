# Product first-parent history at Enola 2a124c2

All ten transitions passed exact cold graph equality and frozen replacement validation. One persistent initial-plus-delta chain, pinned Product tip `a609c19f3861971930fae7b33dcb2950598953c5`. Enola was built from a clean `2a124c2` tree; production sources match `5627150`.

File-sink correctness diagnostic, one measurement per transition; **not NATS/watch performance acceptance**. The debug sink fsyncs each message. Parsed counts below are TypeScript counters, not all extractor work. Required contribution owners are measured from complete prior-versus-cold node/edge differences, including retiring owners. Empty inventory identities do not count as contributions.

Initial: **25.697 s**, 4120 TS parses, 8609 Begin owners.

| # | Target | Changed paths | Begin owners | Changed contribution owners | Extra Begin owners | TS parses | Delta s | Cold s |
|---:|---|---:|---:|---:|---:|---:|---:|---:|
| 1 | `07fb4a41ddaf` | 6 | 8609 | 2 | 8607 | 3 | 21.889 | 24.763 |
| 2 | `fec1eac346c4` | 9345 | 8613 | 81 | 8532 | 1671 | 24.669 | 25.245 |
| 3 | `9fc7ae5b4c3f` | 8125 | 8616 | 17 | 8599 | 1599 | 25.235 | 25.571 |
| 4 | `1c2607479b6d` | 70 | 8622 | 35 | 8587 | 52 | 25.325 | 28.729 |
| 5 | `599575d0aa39` | 15 | 202 | 6 | 196 | 22 | 5.979 | 27.830 |
| 6 | `ae233c5f5695` | 60 | 8636 | 29 | 8607 | 111 | 24.492 | 26.462 |
| 7 | `4168360e2e7f` | 15 | 8636 | 10 | 8626 | 13 | 33.612 | 26.572 |
| 8 | `a6f1f3a91a36` | 5 | 33 | 4 | 29 | 4 | 3.906 | 25.853 |
| 9 | `5dfb2c8f276d` | 15 | 8641 | 31 | 8610 | 83 | 24.043 | 24.918 |
| 10 | `a609c19f3861` | 19 | 8645 | 9 | 8636 | 13 | 24.525 | 26.462 |

## Interpretation

The first transition includes a package.json version-only edit (0.7.1 to 0.7.2) alongside source edits. It changes only two owner contributions but invalidates the whole domain. This motivates the next manifest/config fallback investigation; the run does not establish that every broader owner is safely removable without inspecting active consumers.

The sixth transition now passes after fixing the harness to distinguish empty manifest identities from actual node/edge contributions. The assertion still requires retired nonempty owners, and complete cold equality remains mandatory.

No initial/delta/watch performance goal is declared complete by this diagnostic. Repeated same-sink measurements and the next optimized-build comparison remain separate work.

[Full results and provenance](results.2a124c2.json). Raw case logs and event streams: `/tmp/enola-history-9472142-nonempty-20260923`.
