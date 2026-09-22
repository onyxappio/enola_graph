# Watch priority checkpoint

The next acceptance workload is a long-running production graph watch session
while a separate AI agent edits an isolated Product checkout. Keep the five-second
collection default, and distinguish collection delay from active analysis,
broker acknowledgments and consumer application.

The current watch.py is a short experimental smoke harness, not long-running
acceptance. Its smoke mode overrides the collection window to one second.
The burst check currently takes the first post-edit completion, so edits spanning
a window can leave another generation queued; fix stable-input/drained completion
selection before interpreting its final comparison or latency as authoritative.

Next checks: at least thirty minutes of mixed edits, multiple files, rename/delete,
imports/config changes, edits during analysis, no-op and duplicate events, bounded
memory over time, broker/process interruption and recovery, immutable complete
Begin scope, and exact cold equality after the input and queue settle. Record
per-generation save/Begin/first-batch/End/consumer timestamps and parsed/scope counts.

## Historical diagnostic correction

The f5f4970 Product10 run validated the first five transitions and stopped on the
sixth due to a required-owner assertion. Independent replay of saved generation7
and the corresponding cold output produced equal graph hashes and zero differing
contribution owners. The two reported JSON paths had appeared in prior Begin
inventory scopes but never emitted nodes or edges. The failed assertion is not
proof of lost graph contributions. Keep retirement checks for formerly nonempty
owners and separately decide how never-contributing inventory identities belong
in the protocol contract. Remaining four transitions were not executed.

Raw-config whole-domain invalidation and fresh-CLI state-loading costs remain
open. Prioritize fixes from this live watch workload; do not interpret this
checkpoint as completion of performance acceptance.
