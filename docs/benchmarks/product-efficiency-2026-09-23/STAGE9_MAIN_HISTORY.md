# Stage 9 merged-main history verification

Build `50b64fd9890a642e7a3c72fad6f928ab1076fd86`, Go 1.27.1, trimpath. Ten chronological first-parent Product transitions ending at pinned `a609c19f3861971930fae7b33dcb2950598953c5`; this is a local pinned cohort, not a refreshed remote tip.

All ten transitions pass exact cold-versus-delta graph equality and frozen Begin/End manifest and batch validation. This verifies correctness for the cohort; it does not meet the final performance target. Each timing is one fresh CLI/file-sink diagnostic on a shared host, not resident watch or broker-acknowledged performance.

| Target | Changed paths | Parsed | Begin owners | Owners with changed facts | Delta s | Cold s |
|---|---:|---:|---:|---:|---:|---:|
| `07fb4a41ddaf` | 6 | 6 | 7 | 2 | 3.793 | 27.029 |
| `fec1eac346c4` | 9345 | 591 | 1783 | 83 | 11.680 | 20.499 |
| `9fc7ae5b4c3f` | 8125 | 62 | 2267 | 17 | 15.277 | 21.035 |
| `1c2607479b6d` | 70 | 432 | 1118 | 32 | 9.529 | 20.926 |
| `599575d0aa39` | 15 | 7 | 9 | 6 | 3.565 | 22.568 |
| `ae233c5f5695` | 60 | 99 | 823 | 25 | 8.221 | 23.736 |
| `4168360e2e7f` | 15 | 111 | 778 | 10 | 5.901 | 22.068 |
| `a6f1f3a91a36` | 5 | 80 | 80 | 4 | 4.279 | 21.201 |
| `5dfb2c8f276d` | 15 | 43 | 743 | 5 | 5.906 | 21.055 |
| `a609c19f3861` | 19 | 37 | 663 | 7 | 5.189 | 20.750 |

Initial at the first parent: 23.660 s, 4,012 parses. Final no-op: 1.673 s, zero parses/reads/events/owners, generation 11→11, persistent file content hashes unchanged (excluding the session lock). This no-op check covers the final state only, not each intermediate commit.

The complete per-owner JSON audit identifies repeated unchanged Markdown contributions. Unchanged final facts establish excess publication, but do not by themselves prove that pre-Begin planning can safely omit an owner. A conservative proof and regression coverage are still required.

Remaining: package-export alias fallback narrowing, Markdown scope diagnosis/fix, paired repeated performance measurements and current-build NATS/watch verification. Fresh CLI no-op remains above the requested near-zero target.

Evidence: [history](stage9-main-history10.json), [final no-op](stage9-main-history10-noop.json), [scope audit](stage9-main-history10-scope-audit.json). Raw event stream and per-case summaries remain in `/tmp/enola-stage9-main-history10`. Disposable successful cold checkouts/state were removed after validation; per-case cleanup receipts preserve state hashes.

## Supplemental manifest/config case

Real Product transition `d0fbbf855af5` → `a2ac71af8a22` adds the state-machine-telemetry package, including tsconfig, manifest and source, and modifies the root manifest. Fourteen changed paths include an ignored lockfile. Exact graph and frozen protocol validation pass, but performance acceptance fails: delta 22.192 s, cold 22.224 s, all 3,940 files parsed, zero cache hits, Begin 8,178 owners, only seven owners with changed facts.

The summary reports `TS package export aliases changed`, followed by `frozen scope: global name-resolution domain`. This is an outstanding resolver/config narrowing case, not evidence the goal is complete.

Final no-op: 2.431 s, zero parses/events, generation 2→2, persistent state content unchanged. See [config result](stage9-main-config-history.json) and [config no-op](stage9-main-config-noop.json).
