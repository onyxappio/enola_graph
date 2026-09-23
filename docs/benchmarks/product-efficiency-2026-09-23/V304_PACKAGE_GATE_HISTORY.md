# Package-gate historical regression: baseline and exploratory candidate

Clean main 3957b50, Product ae233c5f5695 -> 4168360e2e7f, initialized independently at the parent. This is the same transition as history10 case06, with a fresh parent checkpoint rather than accumulated history state. The commit adds drizzle-orm to packages/clickhouse/package.json devDependencies and changes source files.

| Metric | Baseline |
|---|---:|
| Raw changed paths | 15 |
| Parsed TS files | 4,028 |
| Cached files | 0 |
| Begin owners | 8,477 |
| Owners whose contributions changed | 10 |
| Delta wall time | 26.732 s |
| Target cold wall time | 25.521 s |

Exact cold graph equality and frozen replacement validation passed. Single shared-host file-sink run; do not compare wall times directly with the earlier 23.864 s history result or with resident/NATS runs. The exploratory comparison is below. Each candidate must initialize the parent with its own context version so a one-time migration is not confused with steady-state package invalidation.

Independent small tests reproduce both nested-package and root-package over-invalidation. The initial per-file gate candidate passes the nested case but fails the root case because the old global framework/ORM context projection remains. Removing that projection requires auditing actual consumers, retaining truly global Prisma/schema handling, and testing package-shadow boundaries and cold equality.

[Baseline results and provenance](v304-package-gate-history-baseline.json).

## Exploratory candidate

A frozen three-file overlay over main3957b50 passes exact cold equivalence and the protocol validator on the same parent/child transition. Its build/source hashes are recorded in [the exploratory result](v304-package-gate-history-exploratory.json).

| Metric | Baseline | Exploratory candidate |
|---|---:|---:|
| Parses | 4,028 | 111 |
| Cached files | 0 | 3,969 |
| Begin owners | 8,477 | 778 |
| Published owners | 4,963 | 767 |
| Batches | 2,316 | 254 |
| Delta s | 26.732 | 6.121 |
| Target cold s | 25.521 | 26.048 |

Both graphs match their full cold oracles exactly; 10 owners actually change contributions. The candidate has 26 parses for moved file context, 7 content changes, 1 added source and 77 resolution parses. Remaining scope/reparse excess is not resolved by this change. Single shared-host observations are not repeat performance acceptance. The earlier small root-package failure is fixed in this snapshot, and both independent tests pass.

A separate real-binary v3-to-v4 state probe preserves the graph and settles: the first candidate run rebuilds and advances generation once, followed by two zero-parse/zero-event runs with unchanged generation. The one-time upgrade rebuild must be disclosed; fresh candidate parent initialization is used here to measure steady-state invalidation rather than migration.

## Review and acceptance status

The candidate projects the actual extraction inputs: nearest-package Vue,
TypeORM and Drizzle flags are per-file; the six existing repository-wide
framework flags and any-package Prisma remain global. Both the initial and
session extraction paths consume `packageGates.forFile` and `anyPrisma`.
The projection explicitly hashes the three exported boolean values rather than
serializing the internal ORM struct with unexported fields.

Independent local-package and root-shadow guards passed on the corrected
candidate and failed against the prior projection. Worker regression tests also
check emitted Drizzle table facts, package-manifest appearance/removal, malformed
manifest fallback and context migration against cold graphs. The worker affected suites passed: tsextractor 28.684 s, graphsession 269.830 s,
bootstrap 109.303 s (all exit 0). The six reviewed files are integrated locally
on main `3957b50`; the full repository suite passed with exit 0. The affected integration package
times were tsextractor 43.255 s, graphsession 553.797 s and bootstrap 230.359 s;
these are test durations under shared load, not analysis benchmarks. All three production hashes match the exploratory
snapshot exactly.

A ten-transition Product history run has started against the frozen exploratory
binary, pinned to `a609c19f3861971930fae7b33dcb2950598953c5`. Its running state is
not evidence of completion. The final patch matches the frozen snapshot, has been reviewed, and passed the
full integration suite. This report supports the scoped package-gate change; the
remaining history results and broader performance goal are still in progress.

## Repeated matched transition on integrated candidate

ABBA order, same checkout and pinned parent/child, independent parent states per context version restored before each delta. All four frozen manifests validate and all four resulting graphs equal the target cold oracle.

| Build | Delta runs (s) | Median (s) | Parses | Begin owners |
|---|---|---:|---:|---:|
| Main 3957b50 | 30.705, 35.103 | 32.904 | 4028 | 8477 |
| Integrated candidate | 6.527, 7.621 | 7.074 | 111 | 778 |

Observed median speedup: 4.65x. Shared host with concurrent validation; two runs per build describe this observation, not a stable tail-latency guarantee. Initial parent runs were 34.536 s and 34.324 s respectively (4027 parses each); this change targets incremental scope. File sink only, with no 5-second watch collection window included.

The first harness attempt changed the file sink path after initialization and was rejected before a transaction. It is excluded. The corrected run resumed the valid parent seeds and cold oracle while preserving their original sink paths; each delta segment is independently validated from its event-file offset.

[Repeated results, source/binary provenance and excluded-attempt receipt](v304-package-gate-paired.json).

Engine extraction semantics and cached local facts are unchanged: the package selection function is refactored without changing its selection rule. The changed durable session projection migrates through context version v4. The first upgraded run rebuilds once and advances generation even if the graph is identical; subsequent unchanged runs settle to zero parses/events, as the real-binary migration probe established.

[Full integration-test receipt and source hashes](v304-package-gate-integration.json).

## Completed ten-transition follow-up

All ten Product history transitions subsequently passed cold equivalence and frozen manifest/batch validation (process exit 0). See [the full history table](V304_PACKAGE_GATE_HISTORY10.md).
