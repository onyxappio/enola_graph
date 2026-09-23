# Frozen scope and reparse checkpoint on v297

The planner now computes a fixed point of observed file-level changes before Begin,
reuses the prepared extraction after Begin, and preserves conservative fallback
when a dependency or export surface cannot prove a smaller scope. Export membership
is compared using the binder index, including default and named exports; older
records without that proof remain unknown. Concurrent requests share one counted
export-index scan per file. Only eligible files collect the extra surface.

This is a correctness and reduced-work checkpoint, not completion of the performance
goal. The previous accuracy changes through v297 are preserved. A file body edit
must not suppress reanalysis when export membership, resolution or dependencies move.

## Real Product history

All 10 sequential transitions passed exact delta-versus-cold graph equality and
frozen Begin/End owner and batch manifests. The same delta state persisted across
transitions. Owners whose contributions changed are a hindsight lower bound, not a
proven safe minimum pre-analysis scope.

| Target commit | Delta s | Full parses | Summary scans | Begin owners | Changed contribution owners |
|---|---:|---:|---:|---:|---:|
| 07fb4a41ddaf | 5.534 | 4 | 15 | 36 | 2 |
| fec1eac346c4 | 18.962 | 622 | 1944 | 1841 | 83 |
| 9fc7ae5b4c3f | 18.908 | 69 | 827 | 2298 | 17 |
| 1c2607479b6d | 14.345 | 431 | 1327 | 1149 | 32 |
| 599575d0aa39 | 6.939 | 7 | 114 | 41 | 6 |
| ae233c5f5695 | 37.679 | 101 | 441 | 8477 | 25 |
| 4168360e2e7f | 37.865 | 4028 | 3019 | 8477 | 10 |
| a6f1f3a91a36 | 7.363 | 80 | 309 | 80 | 4 |
| 5dfb2c8f276d | 9.911 | 93 | 245 | 743 | 5 |
| a609c19f3861 | 8.493 | 37 | 232 | 663 | 7 |

These are shared-host file-sink measurements with concurrent tests and disk cleanup;
they do not establish isolated latency or broker acknowledgment performance. Raw cold
artifacts were losslessly compressed after validation with SHA-256 verification.

## Version-matched control

Both arms use v297 base `ef6f962` and the same Product transition
`fec1eac346c4..9fc7ae5b4c3f` and input policy. One sequential sample per arm:

| Arm | Initial s | Delta s | Cold target s |
|---|---:|---:|---:|
| Control | 30.837 | 17.207 | 28.279 |
| Scope candidate | 28.658 | 14.966 | 30.341 |

Exact cold equality and cross-arm cold hashes passed. The candidate delta parses 69
files; this does not make a 15-second delta an accepted final result. Repeated,
isolated measurements through broker acknowledgments remain outstanding.

## Validation and limits

Independent export visibility/add/remove/default probes, resident failed-End rollback,
published graph and Wave8 cases passed. The full repository test command completed
with six environmental ENOSPC failures: five TempDir failures in tsextractor and one
commit-file write in a graphsession regression. After space recovery, the complete
tsextractor package and the affected graphsession test (both modes) passed. Other
packages completed successfully; do not describe the initial full command as exit 0.

Two broad cases remain: retiring a published non-TS owner selects all 8477 owners;
changing TS owning-package gates reparses 4028 files. Stage timings, shared discovery
and index reuse, near-zero freshCLI no-op, and long-running watch/broker acceptance
remain work in the active performance goal.

Machine-readable provenance and counts: [v297-scope-checkpoint.json](v297-scope-checkpoint.json).
