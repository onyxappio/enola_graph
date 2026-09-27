# State write attribution — diagnostic, not acceptance

Overlay binary c98ef094, current source b35477d; session29443 exited0. Seven CLI graph hashes independently equal the uninstrumented Stage38 candidate; four cold/noop checks pass. Timers wrap the existing output/hash writers and durable operations without changing ordering. These are single shared-host instrumented measurements, not speedup evidence.

| Component | Initial ms | Body ms | Structural ms |
|---|---:|---:|---:|
| Encode residual (includes diagnostic overhead) |318|276|261|
| Output SHA256 |38|38|38|
| File writes |26|13|13|
| File sync |9|13|13|
| Close and pending rename (rounded) |0|0|0|
| Directory sync |4|6|4|
| Parent write_pending_state span |547|490|469|

Correction to earlier shorthand: write_pending_state is a sequential session mark covering context/config/effective-input fences as well as actual persistence. The difference between its490ms body span and the327ms encode/write plus19ms durable trace is about144ms of surrounding work; it must not be labeled slow disk, fsync or JSON without further attribution. Source: session.go between revalidate_ts_records and write_pending_state. Rounded child timings need not sum exactly.

JSON encoding dominates the measured persistence work, not fsync. Fresh body startup also paid391ms state JSON decoding, which resident sessions can avoid through their existing proven checkpoint. Do not imply this fresh CLI profile establishes resident timings. Next optimizations should retain all before-End fences, state content proof, sync/rename recovery and immutable cached records. No persistent relationship index or runtime optimization is introduced here.
