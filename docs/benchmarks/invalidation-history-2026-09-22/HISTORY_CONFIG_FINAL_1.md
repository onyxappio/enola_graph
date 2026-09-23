# Final manifest/config candidate: Product history validation

All ten first-parent transitions pass exact cold graph equality and frozen Begin manifest checks on final private snapshot 1e77fa7d. Comparing every tracked file with production commit 9ca81a4 finds only regression-test differences; production code matches. This closes the exact-build history evidence gap left by experimental 41e530e9, which included an unfenced plan-only tier later removed.

These are single file-sink diagnostic samples under concurrent focused tests and retained-state probes, not uncontended performance estimates. Product starts at 4d104e600f89 and advances to pinned a609c19f3861. Clone/setup time is separate from the per-run timings below.

| # | Target | Changed paths | Begin owners | Changed contribution owners | TS parses | Delta s | Cold s |
| --- | --- | ---: | ---: | ---: | ---: | ---: | ---: |
| 1 | 07fb4a41ddaf | 6 | 38 | 2 | 3 | 3.319 | 26.161 |
| 2 | fec1eac346c4 | 9345 | 8613 | 81 | 1671 | 22.969 | 25.742 |
| 3 | 9fc7ae5b4c3f | 8125 | 8616 | 17 | 1599 | 23.464 | 27.612 |
| 4 | 1c2607479b6d | 70 | 8622 | 35 | 52 | 24.605 | 25.373 |
| 5 | 599575d0aa39 | 15 | 202 | 6 | 22 | 6.305 | 27.429 |
| 6 | ae233c5f5695 | 60 | 8636 | 29 | 111 | 26.814 | 27.111 |
| 7 | 4168360e2e7f | 15 | 8636 | 10 | 13 | 24.874 | 34.388 |
| 8 | a6f1f3a91a36 | 5 | 33 | 4 | 4 | 6.378 | 25.739 |
| 9 | 5dfb2c8f276d | 15 | 8641 | 31 | 83 | 26.111 | 26.412 |
| 10 | a609c19f3861 | 19 | 8645 | 9 | 13 | 25.642 | 24.906 |

All seven remaining whole-domain replacements remain visible in these results; this patch does not resolve tracked-membership invalidation or broad dependent reparsing. The next policy optimization is separate and is not present in this binary.

Repeated NATS/watch measurements for this exact production code are recorded in the sibling product-efficiency report CONFIG_WATCH_MATCHED_FINAL_1.md: three runs per build, all cold-equal, with all samples and host-load caveats retained. That case reduces Begin from 8,645 to 33 owners; its median Begin–End interval is 3.744 s before and 0.935 s after. It does not establish overall goal completion.

Raw history, initial result, source/binary provenance and exact-production equivalence are retained in `results.config-final-1.json`.
