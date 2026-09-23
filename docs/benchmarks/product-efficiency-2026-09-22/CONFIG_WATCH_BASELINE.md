# Manifest/source watch baseline — 2026-09-23

This is a baseline diagnostic, not acceptance of the pending raw-config
optimization. Production files in the frozen private snapshot match `5627150`;
Product is pinned to `a609c19f3861971930fae7b33dcb2950598953c5`.
Raw metrics, exact write hashes, snapshot and binary provenance are in
[config-watch-baseline.json](config-watch-baseline.json).

## Scenario and method

Use the full graph profile and the existing pinned Product scope overlay.
Start watch with a 5-second window and an isolated NATS broker. After initial
completion, change `packages/tracking-client/package.json` version from 0.7.2
to 0.7.3 and append `export const enolaConfigBenchmarkRevision = 1;` to
`packages/tracking-client/src/index.ts`. Record each write completion after
`fsync`. Observe for 180 seconds, then compare the final applied graph exactly
with a cold run and hash the whole input tree before and after cold.

Both runs passed exact cold equality and input stability over 45,106 files.
Each produced exactly one delta after initial. These were scripted edits, not
an AI-editor exercise. The tests used isolated disposable copies of Product.

| Run | Initial launch → consumer | Last fsync → consumer | Begin → End | Timing qualification |
| --- | ---: | ---: | ---: | --- |
| 1 | 12.367 s | 27.053 s | 19.618 s | Concurrent full Go suites; exclude from performance comparison |
| 2 | 9.400 s | 9.996 s | 3.520 s | No sampled Go/test processes before delta applied; one appeared 125.246 s later |

Run 2 sampled Go/test processes and host load every five seconds. Sampling
cannot establish absence of all host contention. A single usable baseline
sample is not a repeat-spread result. Run 1 and run 2 must not be pooled into
an acceptance series. The 5-second buffer is included in fsync-to-consumer
latency. Initial latency is measured separately from Begin-to-End.

## Scope and publication volume

Both runs parsed one TypeScript file for the delta but replaced 8,645 owners,
sending 2,689 batches and 78,222,886 JSON payload bytes, including Begin/End
and excluding broker framing. The net graph change was one node and one edge.
Parsed TypeScript count does not count Markdown compilation or other work.
This confirms that raw-config fallback republishes a much larger graph than
this scenario changes, despite successful local parse reuse.

## Pending candidate review

The initial optimization candidate failed an independent focused regression:
adding a manifest dependency `left-pad` while an unchanged source imports
`pkg:npm/left-pad` fails with `frozen invalidation plan missed resolution owner
src/use.ts`. The same fixture passes exact cold equality when `session.go`
is restored to main through a Go test overlay. The new owner-plan optimization
therefore needs pre-Begin candidate-resolution coverage before acceptance.
Neither the baseline timings nor the broad green suite establish correctness
of that candidate. The production patch remains uncommitted during this review.
