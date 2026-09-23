# Manifest/config candidate: full historical diagnostic

All ten first-parent Product transitions pass exact cold graph equality and frozen replacement validation. Build is private experimental 41e530e9 on main 665f3c9, not a pushed or accepted production revision. The later candidate 1e77fa7d removes the unfenced plan-only fallback tier; these results are not an exact-build run of that later candidate.

File sink, single samples, with concurrent correctness tests and temporary checkout cleanup during parts of the run. Timings are diagnostic only and cannot establish a speedup/regression or be compared directly with NATS watch. Initial at 4d104e600f89: 26.747 s, 4,120 TS parses.

| # | Target | Changed paths | Begin owners | Changed contribution owners | TS parses | Delta s | Cold s |
| --- | --- | ---: | ---: | ---: | ---: | ---: | ---: |
| 1 | 07fb4a41ddaf | 6 | 38 | 2 | 3 | 3.595 | 29.126 |
| 2 | fec1eac346c4 | 9345 | 8613 | 81 | 1671 | 27.281 | 24.990 |
| 3 | 9fc7ae5b4c3f | 8125 | 8616 | 17 | 1599 | 25.845 | 24.466 |
| 4 | 1c2607479b6d | 70 | 8622 | 35 | 52 | 25.905 | 29.510 |
| 5 | 599575d0aa39 | 15 | 202 | 6 | 22 | 8.423 | 36.614 |
| 6 | ae233c5f5695 | 60 | 8636 | 29 | 111 | 32.892 | 27.659 |
| 7 | 4168360e2e7f | 15 | 8636 | 10 | 13 | 23.490 | 24.797 |
| 8 | a6f1f3a91a36 | 5 | 33 | 4 | 4 | 3.808 | 26.362 |
| 9 | 5dfb2c8f276d | 15 | 8641 | 31 | 83 | 25.264 | 25.061 |
| 10 | a609c19f3861 | 19 | 8645 | 9 | 13 | 23.935 | 23.770 |

Compared with the production 5627150 history baseline, the first manifest/source transition drops from 8,609 to 38 Begin owners. The other nine scopes remain unchanged. Seven transitions still replace the whole graph, and all seven report PolicyReconciled=true. A separate staged-versus-unstaged probe shows that tracked-name identity changes force whole-domain Begin even for a new independent source; this is the next optimization target.

The final candidate full repository test run completed in 325.49 s: code packages passed; the sole docslint failure was a virtual overlay path formatted as a real file in a diagnostic report. That report was corrected and docslint passed separately (0.647 s package / 4.67 s wall). A strengthened edit-and-restore test passes with captured facts and fails under an intentional live-reread mutant because transient facts reach publication.

Pending acceptance: final-candidate history evidence and repeated matched NATS/watch timing, including initial overhead. Whole-domain fallback for uncapturable non-TS consumers is a known performance limitation; no global goal completion is claimed.
