# Frozen-scope candidate: preliminary Product comparison

This experimental snapshot is **not accepted or published production code**. A real Product transition passes exact cold graph equality, but the wider worker suite exposes an existing re-export rename regression. Fewer parses alone do not satisfy the efficiency goal.

## Comparable inputs

Both binaries initialize fresh state at Product `fec1eac346c48dbc68072803d89b2e4709f9bd42`, then apply `9fc7ae5b4c3fc4fc266b24b1df0ef2bfcb1f7030`. Same repository path, input policy and full profile; file sink, fresh CLI processes. The candidate ran first, baseline second, on a shared host. These are single sequential samples, not repeated performance acceptance or NATS/watch/Codata latency measurements.

Baseline binary SHA-256: `906e4d3c6d1bbb47703c33134072456d9382eff20d511d58303f7dd8223ad45d` (v293).
Candidate binary SHA-256: `69c72b84ce18853b73b037a86fd17c5ba88a4599c862f155aca44181ddf221c9` (captured worker WIP over production-equivalent root `a1319d4`). The source hashes and overlay receipt are in the companion JSON; the embedded VCS revision alone does not identify this experimental binary.

| Metric | Baseline | Candidate |
| --- | ---: | ---: |
| Fresh initial at parent, seconds | 27.517 | 34.365 |
| Delta, seconds | 16.830 | 16.387 |
| Parsed TS files | 1601 | 69 |
| Begin owners | 2295 | 2266 |
| Delta batches | 1370 | 1328 |
| Delta records including Begin/End | 1372 | 1330 |
| Changed contribution owners | 17 | 17 |
| Exact graph equality to target cold | PASS | PASS |
| Begin/End scope and batch manifests | PASS | PASS |

The shared target cold oracle took 28.155 seconds. The baseline and candidate both match its complete graph under the same semantics. Source staging/cloning time is outside the Enola timings. The older cumulative-history baseline measured 22.466 seconds and Begin 2327 for this transition; it is **not** the same initialized state and must not be used as a causal before/after speedup claim.

## Interpretation and remaining work

Parsing falls by 95.7%, but observed delta wall time falls only 2.6%, too small to establish a speedup from one shared-host pair. Initial has substantial variation and no improvement is established. The graph scope remains broad: Begin contains 633 Markdown owners and 1633 TS/TSX/JS owners, while only 17 contribution owners actually change. The summary reports an mdintent full-extractor rerun, but that alone does not explain the TS owners; membership, module/name-dependent and reverse-closure planning still need attribution. Repeated graph composition, preparation, serialization and publication need phase attribution before choosing the next optimization.

Independent snapshot tests matching `TestBodyScope|TestRawConfigVersionBumpWithSourceEditKeepsBeginBounded` passed with actual exit 0 in 20.989 seconds (Go package 17.792 seconds). This is limited coverage. The wider worker run failed `TestPublishedBridgeExportRenameDeltaEqualsCold`: changing `export { round }` to `export { round as renamed }` can leave the cached surface summary unchanged and incorrectly retain a consumer. The worker is correcting this; the cold assertion must remain intact. The earlier side-read rollback fixture fails first Publish/Begin via `FailAt(1)`, so it does not yet prove rollback specifically after a failed End.

Before release: fix those safety gaps, freeze a new candidate, run the full suites and all 10 historical transitions, then repeat matched performance and resident watch/no-op/recovery checks. This checkpoint does not complete scope optimization, state/index reuse, fresh-CLI no-op reduction or the overall goal.

## Evidence

- `results.scope-snapshot2-preliminary.json`: counts, manifests, graph equality, binary/source hashes and targeted-test receipt.
- Local candidate raw history: `/tmp/enola-scope-snapshot-2-history-probe`.
- Local matched baseline raw events/state: `/tmp/enola-scope-snapshot-2-matched-baseline`.
- Candidate source snapshot and overlay: `/tmp/enola-scope-review-snapshot-2`.
