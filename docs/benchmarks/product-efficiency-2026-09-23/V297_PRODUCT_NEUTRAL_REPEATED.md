# Product neutral manifest: repeated fresh CLI diagnostic

Enola checkpoint `a74cd4f`; Product `07fb4a41ddafff7f42ebd55af8a23fe5739f4cd7`. Three sequential cycles on the same disposable checkout and host, using the stripped correctness binary recorded in the accompanying JSON. This records current overhead, not a matched before/after speedup or broker acceptance result.

| Operation | Runs | Median, s | Range, s |
|---|---:|---:|---:|
| baseline | 3 | 2.923 | 2.901–2.940 |
| version_edit | 3 | 4.005 | 3.993–4.043 |
| unchanged | 3 | 2.932 | 2.926–2.946 |
| revert | 3 | 4.016 | 3.992–4.088 |

All 12 deltas parsed zero source files, published zero event bytes and kept generation unchanged. The version mutation and exact-byte revert target `packages/tracking-client/package.json`. The original manifest was restored and its SHA-256 verified.

The baseline was forked from the previously validated Product state. This diagnostic did not repeat Product cold analysis: exact cold and frozen-protocol checks were independently repeated on the eight-step small CLI fixture for this checkpoint. The file sink produced no messages, so these times do not establish broker acknowledgment latency, resident watch latency or initial performance.

The approximately 2.9-second unchanged fresh CLI remains far from the required near-zero no-op target. Version edits still cost approximately 4.0 seconds despite publishing nothing. The next change targets repeated framework/package/alias discovery; future comparisons must use the same revision, configuration, build flags and measurement boundary.
