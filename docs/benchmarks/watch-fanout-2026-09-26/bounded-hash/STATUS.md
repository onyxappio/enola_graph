# Bounded file hashing: Stage38 candidate, not accepted or in main

Runtime source1af548c uses at most four readers, each with64KiB reusable scratch and reset SHA256 state; all bytes are read, errors remain omitted as before, and output folding preserves input-order duplicate-key behavior. No persistent index, metadata shortcut, schema or analysis-scope change.

Standalone shared-host diagnostic on7779 Product owner files (~80MB), not the full7798 runtime input set: sequential ReadFile median211.93ms/88.09MB allocated versus four buffered readers105.81ms/2.81MB. These are warm-file component measurements, not whole-CLI/RSS/initial/delta acceptance.

The Go-overlay prototype passed focused tests, focused race checks and full engine tests. Full suite110 packages passed,96 cached,14 uncached including graphsession474.588s. Product NATS seven-call/four-check run matches all seven Stage37 graph hashes; initial/noop/body/structural parses6749/0/1/22. Three histories (24 calls) preserve cold/delta/Stage37 equality and silent no-op/state bytes, parsing3/14/1453 files for their deltas. Broad history retains its conservative owner scope. All history timings are shared-host correctness only.

Integration into source1af548c is byte-identical to the tested overlay source/test files. Final clean build cb6a69f (binary e9e48eb0) independently passed the same CLI and all three historical correctness scenarios; final/ archives provenance and receipts. Source review and a prospective six-pair >=3% fresh-noop acceptance cohort still remain. Stage37 is independently accepted and published; this candidate is separate.

Go diagnostic/overlay sources are archived as .go.txt to avoid turning documentation into extra build packages; original bytes and paths are in archive-manifest.json. Full process argument logs, binaries, Product clones and NATS/state stores stay outside Git. Source-only and mixed/broad scenarios retain explicit pinned revisions; source-only is a historical target, not current main.
