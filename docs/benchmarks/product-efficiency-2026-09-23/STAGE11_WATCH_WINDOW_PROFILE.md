# Watch slow tail: collection window crosses a burst

A trace-only Product watch run reproduces a 5.208-second save-to-cold-equal
consumer interval with one pre-Begin refusal followed by a successful retry.
The final graph equals cold, inputs remain stable during the comparison, and
there are no abandoned Begins or batch-count mismatches. This is a diagnostic,
not a paired speed comparison or proof of every earlier outlier's cause.

The binary uses accuracy main `012dd29` plus the prefix optimization subsequently
pushed as `d661367`. Its production executable text matches that integration;
additional diagnostics mark Watch ready, collection-window expiry, ApplyChanges,
and coverage-registration boundaries. Go 1.27.1, trimpath, with phase tracing on.
[Build provenance](stage11-watch-window-build.json) identifies the actual overlay
and binary; do not identify it from embedded VCS revision alone.

## Observed sequence

| Event | Relative to final fsync-observed save, seconds |
| --- | ---: |
| Initial ApplyChanges returns | -2.401 |
| Start registration-gap reconciliation after a collection window | -1.898 |
| Identical-byte save | -1.135 |
| Registration-gap reconciliation returns without advancing generation | -0.602 |
| Next ready notification consumed; 500 ms window starts | -0.602 |
| First graph-changing burst save | -0.103 |
| Collection window expires and ApplyChanges starts | -0.100 |
| Last burst save | 0.000 |
| Superseded-input refusal returns, before Begin | 1.149 |
| Retry ApplyChanges starts after another collection window | 1.652 |
| Cold-equal graph applied by consumer | 5.208 |
| Retry ApplyChanges returns after commit housekeeping | 5.291 |

The refused attempt runs 1.249 s. The retry runs 3.639 s; its consumer application
is observed before ApplyChanges returns. The configured window is fixed after a
ready notification, not extended by each later save. Here the first burst save
arrives roughly 3 ms before expiry, so analysis overlaps subsequent writes.
The refusal preserves the frozen contract and completed generation.

The preceding 1.296 s reconciliation is explicitly attributed to registering
external configuration watches and closing their coverage gap. Its result is
1→1, without graph publication. Registration itself takes only about 2 ms;
do not mistake its subsequent safety reconciliation for watcher registration
CPU cost. The trace proves that a window was already active before the burst,
and that an identical save preceded it, but does not timestamp every native
filesystem event or prove which one alone caused Ready.

## Retry costs

Selected sequential session marks: frozen preview 0.823 s, invalidation planning
0.279 s, post-Begin dirty-scope work 0.221 s, file-state assembly 0.280 s, grouping
facts by owner 0.146 s and record revalidation 0.114 s. The pending-state span is
0.420 s and includes a configuration check and JSON marshaling. Policy rebuilding
outside that session costs 0.346 s. Runtime input collection is 0.698 s nested
inside the session; its inventory, hashing and discovery marks are not additional
peer costs. Recovered state bytes are reread and fingerprinted; decoded state is
reused, as expected.

See [full phase log](stage11-watch-window-trace.log),
[timeline and watch receipt](stage11-watch-window-timeline.json), and
[harness execution receipt](stage11-watch-window-receipt.json). The archived log
only trims trailing whitespace; original stderr remains in
`/tmp/enola-stage10-watch-attribution/product-diagnostic-1/watch.stderr`.

Next investigation: safely discard a proven unchanged covered content batch
before opening its collection window, without losing newer queued events or
weakening watermark, context, configuration or membership checks. Compare that
option with reducing pre-Begin retry work. No production change to watch
scheduling is implemented or accepted by this report.
