# Second candidate: verified, latency target still open

These three sequential full-scope Product runs used the frozen source manifest and
binary in provenance.json, a local NATS JetStream file store and the independent
in-memory protocol observer. Product revision and binary hash are recorded in
provenance.json. Cold means empty Enola state, not flushed operating-system caches.

All 12 checks passed. Full tests, race/coverage and vet passed after adding the
profiling utility to the architecture declaration. Earlier failed test logs are
retained; their only failure was that missing declaration, corrected in the final logs.
The independent review is ../final-review.md. No concurrent workers/builds/tests
ran during timing. A subsequent optimization worker started only after timing ended.

Medians: initial 11.526 s, no-op 2.881 s, body delta 4.395 s, structural delta
4.841 s. Deltas are only 2.62× / 2.38× faster than this same build’s initial.
This candidate improves the previous 9–10 s delta but is not the final incremental
latency acceptance. See summary.txt for spread and broker/consumer times.

The subsequent profile-revert and profile-noop logs are separate diagnostics and
are not included in benchmark metrics. Revert reused main3 after the observer had
switched to the cold-structural context. That single-context observer consequently
exited on its base-generation guard during the diagnostic revert (base 3 versus 0).
This occurred after all benchmark runs and all 12 graph assertions had completed;
those diagnostics establish producer phase times only, not consumer validation.
The owned broker was stopped after profiling. Product tracked source was restored.

The phase profile shows the remaining no-op work: detector discovery ~1.09 s,
state decode ~0.47 s, inventory ~0.43 s, input hashing/context ~0.52 s, TS config
discovery ~0.17 s. It hashes 90,567,325 bytes across 9,030 paths versus the original
1,357,597,803 bytes across 45,105 paths. The source manifest records the exact Go
files, modules and architecture declaration of this candidate.
