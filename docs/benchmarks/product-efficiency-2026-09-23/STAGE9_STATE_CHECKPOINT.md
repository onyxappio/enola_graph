# Reusing decoded committed state after a failed run

A resident retry previously decoded the committed JSON again even when the file
had not changed. The resident now retains the decoded State with a SHA-256
fingerprint of the bytes it read or committed. Recovery still reads the entire
committed file; matching bytes allow it to reuse the decoded object.

Journal replay and all pending-state recovery decisions remain unconditional.
Pending state is decoded and checked for identity and an acknowledged End before
promotion. A changed committed file is decoded, corruption is rejected, and a
missing committed file cannot be replaced with a stale in-memory value. File size,
mtime and journal physical size are not used as substitutes for byte equality.
The retained State must remain immutable; existing failure/recovery regressions
and added serialization/promotion assertions test that pairing.

## Validation

[Receipts](stage9-checkpoint-validation.json) identify the final source hashes and
experimental binary. The complete graphsession package on the combined early
supersession, retry parse cache and checkpoint draft passed with exit 0 in
426.242 seconds. The final production delta adds a profiling mark and comments;
a focused run including the strengthened pairing assertions passed with exit 0
in 8.888 seconds. The earlier full repository run for early supersession plus
retry parsing passed separately; do not describe it as a full repository run
with this checkpoint patch.

The [independent mutation check](stage9-checkpoint-byte-proof-mutation.log)
removes byte comparison from the reuse condition. The same-metadata corruption
guard then fails because the corrupt checkpoint is accepted. The unmutated
combined implementation passes that guard.

## Cost and measurement limits

The change adds hashing to fresh state reads and pending writes. Recovery
WorkCounters count recovery-read digests only; startup and write costs appear in
`state_fingerprint` profile marks, not those counters. No read is eliminated.

Worker microbenchmarks estimate about 23 ms per 55.7 MB digest on this host;
the estimate of roughly 237 ms saved against an earlier retry decode trace is an
extrapolation, not a measured Product speedup. The three-arm Product comparison
below measures the net effect alongside early refusal and parse reuse.

## Product result

The [three-arm report](STAGE9_EARLY_SUPERSESSION.md#three-arm-product-comparison-on-current-accuracy-main)
now measures the combined change on Product. With three repeats per arm, adding
state reuse reduces median controlled retry convergence from 4.490 s to 4.273 s.
All nine runs pass cold equality and protocol/no-op checks, with identical graph
hashes. Each checkpoint run logs a successful decoded-state reuse. Initial
performance did not improve; see the report for timings and limitations.
