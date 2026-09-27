# Owner contribution digest v2 — experimental cf71c85

Replace escaped JSON-string-array hashing with sorted fixed-width, type-tagged SHA256 record hashes. Every wire field remains in the record JSON; ordering alone is ignored and duplicates remain significant. The existing per-owner digest map remains the only persistent digest state: no relationship index is added.

Focused owner-scope, digest, End-failure/recovery and mode tests passed in 6.047s. New migration test proves obsolete versions cannot suppress replacements, untouched inputs remain silent, migration stays cold-equal and the following no-op stays silent. Invalid JSON properties still fail closed.

Three repetitions of a synthetic microbenchmark: 16-node/32-edge owner median79.522→48.782µs (~38.7% less), allocation115943→29006B (~75.0% less); 256-node/512-edge owner median1.254630→0.808948ms (~35.5% less), allocation1755862→458501B (~73.9% less). This is operation-level evidence only, not end-to-end Enola acceptance or a Product speedup claim. Benchmark includes large string properties and is not a measured distribution of Product owners.

Product cold/delta correctness completed exit0: all seven calls and four gates passed, and all seven normalized graph hashes equal the v1 candidate. It ran using identical test-inclusive scope and binary SHA256bb86a92e9501bc7d370ab5b608aa6943f9b80ada7b3953d779a2e97af2e884fe. Full-suite and repeated performance acceptance remain pending. Prior six-pair cohort remains rejected; no samples are reused.
