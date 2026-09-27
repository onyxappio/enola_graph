# Next candidate: bounded file hashing with reusable buffers

Exploratory only; production tree unchanged. Product fba38bab; 7779 existing checkpoint owner files /79,991,530 bytes. This is a subset of the7798 runtime hash targets, not whole-CLI evidence. Shared host, six alternating forward/reverse mode sweeps. All exact digest/result vectors match the initial sequential oracle.

Second sweep medians: current-like ReadFile sequential211.93ms/88.09MB allocated; buffer sequential198.87ms/2.61MB; ReadFile-four120.76ms/88.29MB; buffer-four105.81ms/2.81MB; buffer-two132.17ms/2.68MB; buffer-eight86.81ms/3.08MB. Warm filesystem data; no cold-storage/general-host speed claim. Initial baseline/map assembly differs from production harness, so diagnostic effect cannot establish a CLI gain. First three-mode exploration retained separately.

Potential implementation: Engine.computeFileHashes, bounded worker pool with one64KiB buffer and SHA256 state per worker. Read every file to EOF, omit failures as current ReadFile path does; retain relative keys and apply results in input order to preserve duplicate-key resolution. Do not add a metadata shortcut, persistent index, new profile or schema. Limit workers to a modest fixed bound, benchmark whole CLI before selecting final default. Current comment reports earlier Airflow regressions from concurrency; real Product diagnostic is grounds for investigation, not proof those regressions disappeared.

Required evidence before any promotion: byte/hash parity with old reader over empty/large/binary files, missing/unreadable/directory reads and symlinks; bounded worker behavior; same-size/restored-mtime source edits still seen; existing initial/delta/no-op/frozen scope/recovery suite; real Product cold/delta graph equality and histories; preregistered fresh-process comparison including RSS/allocations and initial, changed-fact delta, no-op. Do not infer global speed from this component test. Stage37 must be reviewed/published first; do not mix unfinished Stage38 runtime into it.

Stage37 reviewer ctx_6b66f230a801 is live but was compacting for several minutes; queued review request msg_0db5f8ccd298 and followups. Do not restart or replace on silence.

## Completed overlay validation

Go overlay changes only computeFileHashes and adds buffered_hash.go/tests; repository runtime remains unchanged. Focused tests passed0.775s; full engine package passed21.733s; focused race tests passed1.863s. Experimental binary9d6b9da20d13aff45d1730e0cc6f05a4b9b4c6f791049e956bd3efaf1cfbae2d. CLI correctness session3794 exited0:7 calls/4 checks, all seven normalized graphs equal the exact guarded Stage37 candidate; parses6749/0/1/22. No acceptance timing. Full suite/histories/source review and clean-source integration still required. Stage37 first; its reviewer remains live.
