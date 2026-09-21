# Remaining performance work

The next substantial gains require avoiding work proportional to the entire
repository during a small file delta. The profile in `profile-resident.log` is
an exploratory single run, not the repeated acceptance timing.

## 1. Incremental checkpoint storage

A one-file change still serializes about 51.34 MB of state: about 128 ms of JSON
encoding and 146 ms including durable pending-state creation in the sampled body
edit. Store immutable per-file contributions with a small generation manifest;
write changed contributions only. Keep referenced blobs until a complete,
acknowledged EndReplace permits promotion of the manifest. Garbage collection
must retain both committed and pending generations and support recovery/fork.
A versioned migration must keep existing state.json checkpoints readable. This
is the strongest next bounded storage investigation, not implemented here.

## 2. Incremental fact assembly and resolution indexes

The body edit parsed one source but composed roughly 69,754 facts and grouped
5,510 owners. TS extraction/aggregation, general assembly and owner indexing
consume roughly 377 ms after dirty-scope selection in the exploratory profile.
Retain immutable owner contributions and update indexes for changed files plus
actually affected reference owners. Symbol deletion, re-exports, configuration,
negative resolution and file membership require conservative invalidation.
Do not remove the remaining clone before composition: composition can mutate
fact properties/relations, and failed transactions must not alter committed caches.
Use exact fresh-analysis equality, failed-run recovery and rename/delete/config
fixtures as the acceptance condition.

## 3. Initial delivery and large history deltas

Initial still spends substantial time in extraction and acknowledged delivery;
the sampled resolved-publication phase alone is about 2.43 seconds, including
queue drain. Measure producer CPU, broker acknowledgment and local journal sync
separately before changing batching. Keep bounded buffers and journal-before-send.
The 100-path history still needs broad membership/configuration reconciliation;
it is a separate workload from the one-file fast path. Reuse prepared scope and
configuration proofs only where their invalidation is established.

## Measurement boundary

A 5-second production Watch collection window is deliberate product behavior,
not analyzer execution time. Continue reporting both save-to-consumer latency
and active analysis/publish time separately. These resident benchmarks use a
request-driven harness with actual filesystem events, NATS and an independent
in-memory consumer; they do not measure production Watch scheduling or Memgraph.
