# Graph streaming implementation and validation

Validation date: 2026-09-21. The scoped-invalidation candidate passed full
repository tests, vet, focused race checks and independent correctness review.
Its Product 100-path history parsed 414 TS files instead of 4,181 and matched
the cold target graph in a contended correctness run. That run does not establish
latency acceptance.

**Performance acceptance remains failed.** The latest isolated initial comparison
was 15.270 s for the candidate versus 9.906 s for the pinned old binary;
fresh CLI no-change was 5.982 s versus 4.612 s. The incomplete repeat series and
phase profiles are archived in [the rejected stage report](benchmarks/product-delta-2026-09-21/failed-isolated-stage/README.md).
Further preparation and publication optimizations are in progress and require
new correctness checks and isolated timing. Historical results below do not
validate those ongoing edits. This is validation of the fork, not an upstream release.

## Integration checkpoint

The integration checkpoint includes the bounded input-policy admission and transport
optimizations described in the [preparation report](benchmarks/product-delta-2026-09-21/preparation-performance.md)
and [transport report](benchmarks/product-delta-2026-09-21/transport-performance.md).
The assembled source passed `go test ./...`, `go vet ./...`, and CLI build.
Affected-package and focused race checks are recorded in those reports.
Full-suite results are [archived here](benchmarks/product-delta-2026-09-21/enola-integration-full-test.log);
the [source manifest](benchmarks/product-delta-2026-09-21/integration-source-manifest.json)
identifies the built candidate. These checks validate the integration checkpoint;
new isolated Product timings remain outstanding. Start with the
[Codata integration guide](CODATA_INTEGRATION.md).

The earlier synthetic observations below are retained as historical evidence.
Product exposed additional capacity, no-op, ownership and recovery defects;
these were fixed and independently rechecked before the final measurements.
Scope and remaining limits are documented below and in [AGENTS.md](../AGENTS.md).

## Implemented flow

`enola graph analyze` streams local TypeScript results while extraction is still
running, then publishes resolved contributions. `graph delta` reuses persisted
file analysis and replaces affected owners. `graph fork` seeds a separate context
from an exact completed checkpoint. `graph watch` now holds one resident writer
and applies filesystem-event batches; [resident sessions](RESIDENT_SESSIONS.md)
describes its observed-watermark guarantee and supported coverage. See [the example](../examples/graph/README.md) for commands
and [the protocol specification](STREAMING_INCREMENTAL.md) for consumer obligations.

NATS JetStream is the broker adapter. Publication uses a durable journal, broker
acknowledgments, and a bounded asynchronous queue (64 pending, committing or ready
items / 16 MiB of queued payload, plus at most eight in-flight messages). Async
Publish accepts volatile memory admission; the commit worker validates history,
journals and fsyncs before delivery. Flush waits for all acknowledgments and
propagates errors before checkpoint promotion. Begin and scope fences precede
authoritative data; End waits for all prior batches. Consumers stage replacements
and commit only a complete, validated EndReplace. Enola does not write to Memgraph
or clone databases.

The 64 MiB journal bound includes identity metadata; total output may exceed it
through acknowledged-payload reclamation. Temporary compaction files can approach
twice the configured bound. Exact identity history exhausting the cap fails
explicitly and preserves recovery data. A completed-run fence rejects later
publication under the retired run ID. OpenJournal validates committed data and
reads interrupted compaction from matching live or temporary files; first mutation
finishes the installation.

## Earlier synthetic acceptance results

Separate CLI processes against a real NATS JetStream server analyzed a fixture
with 103 TypeScript files: a three-file dependency chain and 100 independent files.

| Operation | Files parsed | Replacement owners |
| --- | ---: | ---: |
| Initial | 103 | 106 |
| No change | 0 | 0 |
| Function-body edit | 1 | 2 |
| File deletion | 2 | 4 |
| File addition | 2 | 3 |

The no-change run published nothing and retained its generation. For body edits,
deletion and addition, applying the actual broker events produced exactly the
same owned nodes, edges and properties as a fresh analysis. The wire verifier
checked exact batch bytes/digests, contiguous sequence, final owner manifest,
scope membership and repository/context/base identity. A custom stream and
subject also completed successfully.

A branch seeded from main performed two successive one-file deltas, each parsing
one file and replacing two owners. Both applied graphs equaled their cold runs;
hashes of the main state directory stayed unchanged. Exact baseline provenance
was checked. Watch observed an edit and completed generation 2 in approximately
1.6 seconds in this fixture, then left that generation unchanged during idle polls.
These timings are observations, not standardized benchmarks.

Independent review also exercised deeper deletion/rename/retarget cases, framework
configuration changes, concurrent same-root and different-root extraction, and
comparison against cold analysis. A real broker received a local batch while a
later source file remained blocked before parsing, establishing extraction-time
delivery. Recovery tests covered a broker-stored End with a lost acknowledgment,
replay, and checkpoint promotion.

Final review identified three defects: occupied fork targets could admit stale
journal replay, seeded branches shared mutable node properties, and an empty
incremental End could omit its mandatory owner count. All three were repaired.
The coordinator reran the reviewer's original reproductions against a separate
copy of the repaired source under the race detector; all passed. The full
repository suite also passed after these repairs. Dirty completed/pending fork
retries and nested-property isolation have repository regression coverage.

## Validation commands

The implementation was tested with Go 1.26.8 and NATS Server 2.11.9:

```bash
go test ./...
go test -race -count=1 ./internal/graphsession ./internal/graphstream ./internal/extractors/tsextractor
```

The CLI/broker acceptance harnesses and independent review reproductions ran
outside the checkout. Repository regression tests cover the maintained contract;
the manual broker observations above supplement those tests.

## Current limits

- Forks require the same checkout path. Independent Git worktrees are rejected.
  Consumers must explicitly clone/seed the exact completed baseline themselves.
- Angular uses a declared all-TypeScript fallback. Other language extractors use
  whole-extractor fallback; small TypeScript deltas do not establish equivalent
  performance for every framework or language.
- Watch now uses filesystem notifications with explicit coverage checks. Earlier
  polling measurements below/above are historical and do not measure this resident
  implementation. Product performance evidence is recorded in [the benchmark
  report](benchmarks/PRODUCT.md); this change makes no new latency claim.
- Local state caches extraction context and results, including per-extractor
  contributions and synthetic-fact provenance. It is not a queryable graph
  database. Owned extractors conservatively rerun when their input inventory
  digest changes; full output fingerprints avoid publishing unchanged results.
- External TypeScript compiler enrichment and legacy test-reference extraction
  are outside this incremental profile. IO attributes describe direct operations;
  transitive reachability belongs downstream.

Upstream adaptation and the intentional local-fact semantics are recorded in
[AGENTS.md](../AGENTS.md).
