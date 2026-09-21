# Codata integration checkpoint

The streaming producer is available for integration. Performance optimization is
ongoing; the current benchmark acceptance status is in
[Product measurements](benchmarks/PRODUCT.md). Enola publishes graph replacements;
Codata owns its durable consumer and Memgraph writes.

## Build and run

Use Go 1.26+ and a C compiler for tree-sitter bindings. Start a NATS server with
JetStream enabled, then build and run:

```bash
go build -o enola ./cmd/enola
./enola graph analyze --nats nats://127.0.0.1:4222 \
  --repo-id product --context main --state-dir /tmp/product-enola-state \
  --summary-json /path/to/product/mcp-arch.yaml
# After editing source, reuse the same repository, context, sink and state:
./enola graph delta --nats nats://127.0.0.1:4222 \
  --repo-id product --context main --state-dir /tmp/product-enola-state \
  --summary-json /path/to/product/mcp-arch.yaml
```

The default stream is `ENOLA_GRAPH`, subject filter `enola.graph.>`. Use a separate
durable consumer for each independent downstream application; replicas of one
application may share its consumer. Messages are JSON and can be consumed from
Node.js using a JetStream client. There is no required Node process in Enola's
analysis pipeline.

## Consumer contract

The envelope schema is `enola.graph.v1`. Exact JSON fields and digest algorithms
are defined in [protocol.go](../internal/graphstream/protocol.go), with the
replacement semantics in [Streaming and incremental analysis](STREAMING_INCREMENTAL.md).
The [reference consumer](../internal/graphsession/consumer.go) demonstrates
validation and owned replacement; it is not a production database adapter or a
complete production validator. For example, it does not enforce `schema_version`;
Codata must implement the full stated contract rather than copy it unchanged.

| Event | Consumer action |
| --- | --- |
| `begin_replace` | Validate schema, repository/context and exact base generation; open staging and seed the scope from `owner_scope`. |
| `batch`, phase `scope` | Collect the replacement owner manifest. |
| `batch`, phase `local` | Treat as provisional declarations; retain its original bytes for completeness checks. |
| `batch`, phase `resolved` | Stage authoritative owned nodes and edges. |
| `end_replace` | Verify every sequence, batch count/digest, final owner manifest and successful completeness; atomically commit the replacement. |

Hash the original batch bytes in sequence order, not JSON reserialized by Node.
Delivery may repeat or arrive out of sequence. Deduplicate envelopes by run and
type, and batches additionally by sequence (equivalently the producer's
`Nats-Msg-Id`); Begin and End have no batch sequence. Reject conflicting bytes for
the same identity. A received End alone does not authorize commit.
Persist received data before acknowledging it, or acknowledge after the database
transaction commits. Broker acknowledgment is distinct from database completion.

Replace only contributions owned by the declared scope. Preserve duplicate
occurrences, other owners' contributions and incoming edges owned elsewhere.
An owner with no new authoritative results is removed only by a complete successful
replacement. Interrupted runs leave the last completed graph usable. Unresolved,
ambiguous and pending references are not resolved edges with guessed targets.

The adapter creates missing streams with Limits retention and a default seven-day
maximum age. Existing stream configuration is not rewritten. Size retention and
consumer lag for your deployment; expired required history needs a verified base
or a new initial analysis. The producer finishes after broker acknowledgments,
without waiting for Memgraph.

## Contexts and watch

Use separate state directories for independent contexts. Forking seeds an exact
completed producer checkpoint; the consumer must separately seed the matching
base graph and validate its fork provenance. The current fork implementation
requires the same checkout path; independent worktrees are not supported.

`graph watch` uses the same replacement protocol through a resident session.
Its filesystem coverage and observed-watermark guarantees are documented in
[Resident sessions](RESIDENT_SESSIONS.md). Fresh CLI startup and resident idle
latency are different measurements.
