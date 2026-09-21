# Graph streaming example

Graph-only analysis of a small TypeScript tree. This does not write a snapshot
or run explainers. TypeScript is extracted incrementally; other languages, if
present, re-run as a whole extractor and print a fallback reason.

```bash
# A durable sink is required: --events or --nats.
enola graph analyze --events /tmp/enola-events.jsonl --json examples/graph

# File-granularity delta from .enola/graphstate (or --state-dir).
enola graph delta --events /tmp/enola-events.jsonl --json examples/graph

# Branch from an already-analyzed main checkpoint (same checkout).
# fork does not publish; the consumer must clone the main graph first.
enola graph analyze --context main --state-dir /tmp/gs-main --events /tmp/main.jsonl --json examples/graph
enola graph fork --base-state-dir /tmp/gs-main --state-dir /tmp/gs-feature --context feature --json examples/graph
enola graph delta --context feature --state-dir /tmp/gs-feature --events /tmp/feature.jsonl --json examples/graph

# JetStream (Limits retention; one consumer ack cannot drop another
# project's history). Custom streams need a matching --subject wildcard.
enola graph analyze --nats nats://127.0.0.1:4222 --stream ENOLA_GRAPH --subject 'enola.graph.>' examples/graph
```

State is stored under the repository output directory (`.enola/graphstate/` by default).
The default repo identity is the absolute repository path, so two checkouts that
share a basename cannot reuse each other's graphstate. Context, sink, and
checkout identity are checked on every run; use a separate `--state-dir` per
context. A crash after `EndReplace` is acknowledged restores that generation
from `pending-state.json`; a crash before `EndReplace` does not.
