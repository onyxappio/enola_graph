# Stage19 Product history correctness

Candidate `7b03158`, production identical to frozen patch02. All 44 CLI invocations passed across 11 revisions and 10 transitions. Every revision has candidate chain = candidate cold = baseline cold; all no-ops have zero parses/events, unchanged state bytes and generation.

This chain includes 3 manifest transitions and 0 tsconfig transitions. These are correctness runs under concurrent workloads, not performance evidence.

| Revision | Changed paths | Parsed files | Graph equality |
|---|---:|---:|---|
| 4d104e60 | 0 | 4012 | PASS |
| 07fb4a41 | 6 | 5 | PASS |
| fec1eac3 | 9345 | 513 | PASS |
| 9fc7ae5b | 8125 | 25 | PASS |
| 1c260747 | 70 | 431 | PASS |
| 599575d0 | 15 | 7 | PASS |
| ae233c5f | 60 | 92 | PASS |
| 4168360e | 15 | 111 | PASS |
| a6f1f3a9 | 5 | 80 | PASS |
| 5dfb2c8f | 15 | 43 | PASS |
| a609c19f | 19 | 37 | PASS |
