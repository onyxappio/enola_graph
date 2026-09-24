# Streaming and incremental graph analysis

Status: design contract. Implemented behavior, verification results and current
limits are recorded in [GRAPH_VALIDATION.md](GRAPH_VALIDATION.md).

## Scope and decisions

Extend this Enola fork and reuse its extraction rules. Optimize graph production for
initial analysis and file-granularity delta analysis. Watch is a lower-priority client
of the same delta engine. External TypeScript compiler enrichment is out of scope.
Codata and other consumers adapt to this contract; Enola does not write to Memgraph.

Emit local facts and direct relationships. Do not propagate transitive attributes such
as indirect IO through callers. Downstream graph queries can calculate reachability.
The streaming profile must distinguish direct IO from legacy transitive semantics.

The unit of reanalysis is a whole file, even for a one-function edit. Track directed
file dependencies without requiring a classification of every reason for an edge.
Do not attempt to prove that a function body change is semantically harmless.

## Analysis state and invalidation

Retain a versioned file manifest, source hashes, analysis configuration/rule versions,
directed dependencies, and the cached context required by existing extraction rules.
The exact persistence implementation is an implementation choice, to be documented
with recovery and performance evidence. No second queryable Memgraph clone is required.
Old exported edges need not be retained solely to compute individual delete events.

For `calls` and `instantiates` relations, extractor-proven `TargetFile` constrains
resolution to symbol facts in that exact file, subject to the source-repository filter
when present. The source fact's file is not a substitute for missing provenance. With no
`TargetFile`, same-name candidates follow normal ambiguity rules; if a proven target
file has no matching symbol, the edge stays unresolved instead of falling back to a
same-name sibling.

An unchanged file is reusable only if all inputs read by its cached computation remain
valid. Framework context, aliases, file sets and global symbol candidate sets count as
inputs. Cached intermediate results must remain pristine: derived passes must not
mutate a cached result and then reuse it as a fresh input.

The affected set must cover changes to direct relationship resolution, including
deletions, new candidates, previously unresolved references, re-exports, ambiguity,
renames and configuration changes. Old dependencies are necessary for deletions.
Neither a fixed one-hop radius nor blind transitive invalidation of all imports is the
universal rule. Audit actual rule inputs; broaden the affected scope where necessary.
Unsupported incremental cases must explicitly report their fallback scope and reason.

The required invariant is equality with fresh full analysis of the same bytes and
configuration under this fork's local-fact contract. This does not imply perfect
understanding of dynamic source-language behavior.

## Producer boundary

All new analysis modes publish through a sink interface, with NATS JetStream as the
first broker adapter. Independent projects have independent durable consumers.
Retention must not remove a message merely because another project acknowledged it.
Retention limits and expired-history recovery must be explicit.

Enola completes publication once the broker has acknowledged every event of the run.
It does not wait for consumer database writes. A bounded publisher separates analysis
from network latency; outstanding batches and disk spool usage are bounded. When both
fill, backpressure or an explicit recoverable failure is required, never silent loss.

Initial analysis must emit stable local results before full extraction finishes.
Relations requiring a complete candidate index wait for that index. An early tentative
resolution must not be advertised as final. Do not attach a sink to the existing
whole-snapshot JSONL writer and call that extraction-time streaming.

## Replacement protocol

The names below describe semantics; implementation names may differ.

1. `BeginReplace` identifies schema version, repository, context, run, exact base and
   target generations, and the replacement ownership scope. `scope_mode=complete`
   (default) requires the full owner set on Begin, or immediately following
   `PhaseScope` chunks, before any resolved write. `scope_mode=incremental` is
   permitted for an initial full-repository epoch: the producer may Begin with a
   partial or inventory-derived owner set so local batches can stream during
   extraction; `PhaseScope` batches grow the set; `EndReplace` carries the
   complete owner-scope length and digest and consumers must not commit without
   that final manifest.
2. Numbered batches supply local (non-authoritative) declarations, owner-scope
   chunks, and the complete new nodes and edges owned by that scope.
3. `EndReplace` declares the exact batch manifest/count/digest, the final owner
   scope length/digest, and successful completion.

Consumers finalize only after every declared batch is applied. Seeing an end marker
alone does not prove completeness or processing order. Duplicate delivery is expected;
run and batch identity must support idempotent handling. Replayed payloads for the same
identity must be identical. A missing end marker leaves an incomplete replacement.

If the affected set grows during analysis, either finalize it before publishing
Begin, start a separately delimited replacement, or (initial epoch only) announce
`scope_mode=incremental` and emit `PhaseScope` so the collected set is complete
before End. Delta replacements use complete mode: the affected owner set is
precomputed (and grown only with reverse-closure/composition owners via
PhaseScope) before resolved writes. Do not silently write resolved facts outside
the announced-or-collected scope. Large scopes must respect broker payload limits.
Initial analysis must overlap extraction with bounded async broker delivery.

### Frozen file-owner protocol (v2)

`enola graph analyze|delta|watch --authoritative-scope` selects envelope
`schema_version` `enola.graph.v2`. Default graph runs stay on `enola.graph.v1`.
The flag is opt-in and binds the state directory; a forked checkpoint keeps the
source protocol and must be reopened with the same flag.

BeginReplace publishes a complete file-owner manifest (`scope_mode=complete`)
before any file parse. That owner set is frozen for the run: later dirty-file
discovery or export/resolution expansion that needs an owner outside Begin
fails the run instead of growing the manifest. Batches are `resolved`
file-owned replacements only. EndReplace repeats the same owner-scope count
and digest.

Ordinary content edits of existing files use the prior file-to-file reverse
closure plus every cached owner (any extractor) whose **published** facts
mention an added or removed `Fact.Name` or relation target. Those names come
from a bounded TypeScript session on the dirty files with the full owned set
(so GraphQL/route composition matches the later stream), `applyLocalIO`, then
Begin. The same `SessionResult` is reused; dirty files are not parsed twice.
Source regex over `export` identifiers is not a sound bound for generated
facts. The coarse alternative of every owner with published edges is only
~1.7× on Product (5047/8645) and is not used. Add, delete, and rename still
fall back to the full prior/current file domain. Late fail-closed checks
remain a guard. Empty initial analysis still publishes an epoch. Deleting
the last contributing file publishes an empty replacement of that file owner.

An oversized Begin fails closed with zero published events: v2 never splits the
file-owner manifest into `PhaseScope` chunks. `--max-begin-bytes` sets that cap
(default 512KiB with `--authoritative-scope`) and must fit the broker
`max_payload` (NATS default 1MiB). Raise both together; a larger Begin limit
does not change the frozen contract.

A lockfile-only add, edit, rename, or removal publishes no events and does not
advance generation. Frozen scan hashing uses semantic inventory names and omits
lockfiles.

A change in stored `PolicyIdentity`, including repository `.gitignore` that
alters graph admission, starts a frozen replacement. Previously published file
owners remain in the Begin manifest so exclusion can clear obsolete
contributions.

Each result carries explicit ownership. A call from B into C is owned by B; replacing
C must not erase an unchanged incoming call owned by B. If C disappears or resolution
changes, affected owners must be refreshed. Shared identities require contributions
or a deterministic ownership scheme; replacing one owner must preserve contributions
from other owners. Existing FactID is not an occurrence-unique identifier, so duplicate
facts and relations must not be silently collapsed without defined merge semantics.

For deleted files, successful replacement supplies no new owned results. Absence in a
completed replacement removes old owned results; absence in a partial run does not.
Consumers may stage generations or use database transactions. They must preserve the
last completed state after producer interruption. Physical node deletion must preserve
incoming relationships and other owners' contributions until reconciliation finishes.

Local state advancement and the durable delivery journal must be coordinated. A crash
must not leave an advanced base with no way to publish its delta. Broker acknowledgments
may be lost; recovery may replay identical batches. Broker deduplication windows alone
are insufficient to guarantee consumer correctness.

## Branches and watch

Each context updates sequentially from its exact prior generation. Branch names are
labels, not immutable versions. A delta from M1 to F1 cannot be applied to M2. Contexts
may share immutable cached results when all analysis inputs match. A new consumer
context needs its declared base graph or an initial analysis, not only a delta.

To start a branch from an already-analyzed main checkpoint without a full reanalysis,
seed a **new empty** `--state-dir` and `--context` from that checkpoint:

```bash
enola graph analyze --context main --state-dir /tmp/gs-main --events /tmp/main.jsonl --json .
# edit a few files in the same checkout
enola graph fork --base-state-dir /tmp/gs-main --state-dir /tmp/gs-feature \
  --context feature --json .
enola graph delta --context feature --state-dir /tmp/gs-feature \
  --events /tmp/feature.jsonl --json .
```

`delta --base-state-dir /tmp/gs-main --state-dir /tmp/gs-feature --context feature`
forks if the target is empty, then runs the delta. The source checkpoint is locked
during the copy and is never modified. Independent worktrees are not supported: the
checkout path must match. `--repo-id` may set a stable repository identity; it does
not lift the same-checkout requirement.

The first `BeginReplace` on the branch carries `fork_base_repo_id`,
`fork_base_context_id`, `fork_base_generation`, and `fork_base_run_id`. Downstream
consumers must already hold that exact completed baseline (clone/seed it explicitly).
Enola does not copy a database and does not write Memgraph. A missing or wrong base
(including the same generation with a different context or run) is rejected. Dirty or
non-empty targets and unresolved source pending/unacked journals are rejected before
writes. A completed or interrupted matching fork may be retried safely.

Watch collects filesystem events into bounded-delay batches and invokes the same file
delta pipeline. Reconcile after missed events or restart. Version dirty working trees
separately from Git HEAD. Capture stable per-run input bytes and track edits occurring
during analysis for a subsequent generation; do not claim an atomic Git snapshot for
an ordinary mutable working directory. Handle checkout as a base/context transition.

## Validation and delivery

Compare initial-plus-delta through a reference consumer with a cold full run, including
properties, ownership, resolved and unresolved edges. Exercise add/delete/rename,
cycles, re-export, nested functions/objects, ambiguous and newly resolved symbols,
configuration changes, delivery failure/replay and restart.

Measure time to first batch, time to broker-confirmed completion, actual files parsed,
context/resolution work, peak memory, persistent state size and event bytes. Small
output alone does not establish incremental execution. Keep upstream tests for retained
behavior and document tests adapted for intentional local-fact semantics.

Implement a complete small initial/delta/protocol slice first, then expand retained
framework coverage, tune performance and add watch. Report any unsupported profile
or fallback explicitly. Do not represent a protocol-only scaffold as a working engine.

### Required regression scenarios

Validate the public CLI across separate processes, using its actual configured
extractor set. A source file's cached contribution must not be overwritten by
another extractor that discovers no owned files. A no-change run publishes nothing
and keeps the completed generation unchanged.

Apply initial events and subsequent deltas to the same consumer before comparing
all owned nodes and edges with a fresh run. Cover removal of the final file in a
directory, file restoration, and shared contributions. Testing a deletion against
an empty consumer cannot establish removal of old results.

Framework and configuration regression cases include enabling GraphQL in another
file, introducing ambiguous gRPC metadata, changing an extended or nested tsconfig,
and losing access to a required template. Either reuse correctly or declare a
conservative fallback; successful completion with missing required input is invalid.
Bind cached hashes to the bytes actually consumed, including edits during a run.

Verify the exact bytes stored by a real broker: scope membership, contiguous batch
sequence, checksum, and repository/context/base identity. Test both inline and
chunked ownership scopes. Identical retries must be harmless and conflicting retries
must fail. A custom subject must route every event into the configured stream.

Exercise interruption before End is journaled, after End is journaled but before its
acknowledgment, and after acknowledgment but before local state promotion. Absence of
unacknowledged messages alone does not prove that End was ever published. Recover a
pending completed generation before deriving a subsequent delta from it.

Use a deliberately slow sink to prove that extraction and delivery overlap within
bounded memory/disk limits. The completion barrier must still wait for all broker
acknowledgments and propagate delivery errors.
