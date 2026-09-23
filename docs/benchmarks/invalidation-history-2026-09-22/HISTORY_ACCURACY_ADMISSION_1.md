# Integrated accuracy/admission candidate: ten historical transitions

Production candidate `a257dd6fcc720029fcde13beb7a03a2382eb41e9` (v291).
All **10/10** transitions passed exact cold/delta equality, frozen owner manifest,
batch integrity and completed-generation validation. Raw results and build
provenance are in `results.accuracy-admission-1.json`.

The binary was built while the merge and admission patch were uncommitted; root
subsequently byte-compared the compiled production patch with a257dd6. Binary
SHA256: `5e0b3a258ce523aa791ec36087fb524687885ac6bf2f94ccc7dbea63948f791e`.
The untracked difference excluded from that comparison was a regression test,
not production code. Commit 726a163 adds only a remote documentation merge.

File-sink timings below are **diagnostic**, not performance acceptance: the host
also ran tests, snapshot builds and temporary-source cleanup. They measure CLI
wall time and do not measure NATS acknowledgments or Codata GraphHead latency.
The graph semantics differ from the older v274 results; speed ratios against
those builds would conflate accuracy and performance changes.

| Target | Changed paths | Begin owners | Changed-contribution owners | TS parses | Delta s | Cold s |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| 07fb4a41ddaf | 6 | 1401 | 2 | 4 | 15.372 | 31.157 |
| fec1eac346c4 | 9345 | 2543 | 81 | 2426 | 19.914 | 28.749 |
| 9fc7ae5b4c3f | 8125 | 1694 | 17 | 1600 | 16.425 | 26.728 |
| 1c2607479b6d | 70 | 2621 | 32 | 428 | 22.063 | 39.524 |
| 599575d0aa39 | 15 | 149 | 6 | 21 | 9.149 | 31.776 |
| ae233c5f5695 | 60 | 8477 | 27 | 160 | 25.416 | 27.491 |
| 4168360e2e7f | 15 | 8477 | 10 | 3981 | 32.035 | 31.321 |
| a6f1f3a91a36 | 5 | 164 | 4 | 77 | 8.120 | 28.106 |
| 5dfb2c8f276d | 15 | 162 | 5 | 113 | 7.594 | 26.186 |
| a609c19f3861 | 19 | 89 | 7 | 58 | 6.357 | 26.283 |

Scope is still broader than changed contributions. The first transition has
1401 Begin owners for two changed contributions; two later transitions use
almost the whole domain. Passing correctness does not close scope or latency
optimization. No-op, state/index reuse and repeated matched NATS measurements
remain separate acceptance work.

Completed cold-source copies were removed only after each case passed. States,
raw events, summaries, results and the pinned fetch clone remain available under
`/tmp/enola-history-accuracy-admission-1`; the cleanup receipt records each copy.
