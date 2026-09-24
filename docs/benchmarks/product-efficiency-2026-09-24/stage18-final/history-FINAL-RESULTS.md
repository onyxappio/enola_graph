# Stage18 Product history correctness

Frozen candidate d988437; 10/10 transitions and initial revision accepted. All 11 chain graphs equal candidate and baseline cold graphs. Each no-op had zero parses/events, unchanged state bytes and generation. Correctness-only concurrent run; no latency claim.

| Commit | Parsed files | Published owners | Result |
|---|---:|---:|---|
| 4d104e60 | 4012 | 4942 | PASS |
| 07fb4a41 | 5 | 5 | PASS |
| fec1eac3 | 513 | 1149 | PASS |
| 9fc7ae5b | 25 | 1662 | PASS |
| 1c260747 | 431 | 554 | PASS |
| 599575d0 | 7 | 9 | PASS |
| ae233c5f | 92 | 345 | PASS |
| 4168360e | 111 | 150 | PASS |
| a6f1f3a9 | 80 | 80 | PASS |
| 5dfb2c8f | 43 | 126 | PASS |
| a609c19f | 37 | 46 | PASS |

The chain includes manifest and dependency additions, but no tsconfig-changing transition. Owner scopes can be wider than reparsing; this is intentional under the frozen replacement contract.
