# Stage55 — End transaction statistics

EndReplace adds optional `statistics.transaction_duration_ns`: a monotonic elapsed
transaction interval sampled before End serialization, not full CLI/acknowledged
latency. It retains the original bytes on journal replay and adds no graph walk,
index, extraction change, or duplicate work counters.

## Boundaries

The interval includes transaction recovery and in-transaction policy/input
preparation, planning, extraction, preceding publication/backpressure, pending
checkpoint preparation and End digests. It excludes CLI/OpenSession, lock wait,
Watch buffering/classification, pre-transaction changed-input hashing/probes,
End serialization/publication, final outstanding ACK drain, final checkpoint
promotion and consumer application. Old absent statistics means unknown, not zero.
Existing completeness/envelope counters already describe parses, cached files,
scans, fallback reasons, batches and owner scopes. Strict external JSON decoders
must allow this optional field before rollout.

## Combined-source validation

Source-only main integration: `11edf1ee149ea4b6c3610e3cd727986ad2f690ba`
on `d9ae5c6eed3a33c9e203ca2f90344b0b7d241809`; seven telemetry/test/docs
files, with main's resident architecture preserved through conflict resolution.
The clean binary stamps this source revision and `vcs.modified=false`.
All 1,323 tracked Go/module pins in the root module were verified. This main
checkout contains no third_party tree or local replacement; binary dependency
metadata stamps upstream Segmentio. Earlier parent evidence had a separate
nested-module scope limitation; it is not used to prove this main runtime.

Main focused race passed for initial and resident deltas in legacy/frozen modes,
no-op silence, End timing boundaries, frozen Begin and byte-identical failed-End
recovery. One real Product/NATS correctness repetition completed successfully:
10 frames, 20 lifecycle records, matching positive End durations, six single-file
edits/restorations with one parse and one owner each, exact fresh cold graphs,
and generation 1→7. Two bounded 0.7-second identical-write checks proved wire
and generation-state silence; these are not an independent zero-parse proof.
102 host samples recorded no errors. Watch exit 2 is intentional context
cancellation, observer -15 and broker 0; all owned processes exited.

This is telemetry correctness evidence on a shared host, not performance
acceptance or a new speed table. The parent candidate separately passed its
complete suite and a negative guard omitting statistics. The first integrated
main suite exited 1: 112 packages passed, 13 skipped, one pre-existing native
Git-metadata test failed. The failure reproduces on untouched main. It reports
RENAME|CHMOD after chmod; warm-up, quiet waits and fixture aging did not eliminate
it. Production correctly reconciles that possible membership change.

The test correction explicitly separates guarantees: actual native unchanged
controls must preserve zero parses, zero publication and unchanged generation;
reconciliation-bearing batches require conservative work with a recognized
reason. The no-reconciliation assertion remains for non-reconcile batches.
Exact pure-CHMOD byte equality and composite membership behavior are covered
by the deterministic classifier test. This does not claim native pure-flag
delivery. All three native pulses, idle feedback and true-index cold checks are
retained, with no production watcher change or new index. Both tests passed ten
race repetitions; controlled faults independently fail pure-metadata and exact
RENAME|CHMOD guards. The previous failure is preserved. The final complete suite ran next; graphsession exceeded Go's default ten-minute
package timeout without an assertion failure. Its running test was only starting
when the package-wide deadline fired, so this does not establish a stuck test.
A fresh unfiltered graphsession run with a twenty-minute budget passed in
544.260 seconds, with 659 root tests and no assertion failures.
Combined coverage is 126 packages: 113 PASS and 13 skipped, across the
repository-wide run and the separate graphsession retry. This is not a claim
that one repository-wide command exited zero. Final independent Astra medium acceptance passed at clean `e0c56537`.
The acceptance JSON is retained here. The telemetry package was published
non-force to main at `ead307de4958b910ff09fe30dba9a5bd14b9aecc`;
cachecov, docslint, golden and determinism pre-push guards passed. Remote main
and unchanged source pins were rechecked. The publication receipt is retained.
No performance acceptance is claimed.

Product evidence remains tied to clean11edf1ee. The transfer receipt proves
only those two tests differ in root-module Go/module pins; all production Go
and module bytes are identical. It does not transfer performance acceptance.

## Evidence

Small review/receipt files are retained here. Raw results, source/binary pins,
runner, independent audit, observer and suite logs remain at
`~/.local/share/enola-performance/stage55-end-statistics-20261003/`.
The Product receipt records SHA-256 for raw results, consumer/lifecycle events,
host observations, runner and source manifest. Reviews identify their source and
validation boundaries. Stage54's encoder remains outside this integration.
