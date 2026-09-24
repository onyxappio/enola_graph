# Stage 13: Product history verification

All ten pinned first-parent transitions through Product `a609c19f3861971930fae7b33dcb2950598953c5` passed exact cold-versus-delta graph equality, frozen Begin/End manifest and batch checks, and a fresh persisted no-op after each transition. No-op checks require zero parses, zero events, unchanged generation and unchanged persistent state bytes.

Measured binary is the stage13 candidate, production-equivalent to main `eb73b66` (only explanatory comments changed during integration). This is a single chronological file-sink diagnostic; timings are not repeated NATS/watch performance acceptance. Changed paths include excluded artifacts and test files. Changed-contribution owners are determined retrospectively by cold graph comparison; this is not a proof of the smallest scope that can safely be planned before analysis.

| Target commit | Changed paths | Begin owners | Changed-contribution owners | Parsed | Delta s | Cold s | No-op s |
|---|---:|---:|---:|---:|---:|---:|---:|
| 07fb4a41 | 6 | 7 | 2 | 6 | 3.725 | 26.550 | 3.773 |
| fec1eac3 | 9345 | 1168 | 83 | 591 | 14.332 | 25.109 | 1.985 |
| 9fc7ae5b | 8125 | 1655 | 17 | 62 | 12.645 | 26.176 | 2.537 |
| 1c260747 | 70 | 554 | 32 | 432 | 11.305 | 23.055 | 1.929 |
| 599575d0 | 15 | 9 | 6 | 7 | 3.854 | 22.172 | 1.990 |
| ae233c5f | 60 | 344 | 25 | 99 | 6.325 | 23.485 | 2.137 |
| 4168360e | 15 | 161 | 10 | 111 | 5.445 | 22.029 | 2.010 |
| a6f1f3a9 | 5 | 80 | 4 | 80 | 4.553 | 21.726 | 2.696 |
| 5dfb2c8f | 15 | 126 | 5 | 43 | 4.994 | 22.326 | 1.981 |
| a609c19f | 19 | 46 | 7 | 37 | 4.695 | 22.477 | 1.962 |

Scope and parsing are distinct: the 1,655-owner transition parsed 62 files; the 1,168-owner transition parsed 591. Both remain cold-equal. Broad resolution updates remain a performance candidate, not an accepted optimal result.

Raw receipts: [stage13-main-history10.json](stage13-main-history10.json). Original per-case summaries and event streams are under `/tmp/enola-stage13-main-history10`; the checked-in harness is `docs/benchmarks/invalidation-history-2026-09-22/run.py`, invoked with `--skip-build --verify-noop` and the binary/source/work paths recorded in provenance.
