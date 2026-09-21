# Product CLI delta/noop phase profile and optimizations

> Historical worker report for a rejected intermediate candidate. Independent
> probes found input-cache, configuration-discovery, glob and detector regressions.
> Its timings are exploratory, use a file sink, and cannot establish speedup
> versus a NATS initial from a different build. See independent-probes.md and
> input-contract-review.md. Subsequent corrected measurements supersede this report.

Date: 2026-09-21. Worker: graphsession/engine/tsextractor delta path.
Product fixture `/tmp/enola-product-benchmark-source` was never written.
Profiling used APFS clone `/tmp/enola-delta-profile/source` and `--events` (no NATS).

## Baseline (instrumented, before opts, true noop vs reverse-delta)

True noop against matching state (gen 4→4, parsed=0, events+0): **8.20s**

| Phase | Seconds |
| --- | ---: |
| walkRepo (45,105 names) | 3.50 |
| SHA-256 all files (1.36 GiB; 780 MiB JSON + 453 MiB PNG) | 2.88 |
| DetectExtractor (OpenAPI/AsyncAPI/mdintent re-walks) | 1.09 |
| state.json unmarshal (56 MiB, 6476 files) | 0.47 |
| tsConfig WalkDir | 0.20 |
| cloneFileState | 0.002 |
| reuseTSCache fact clone | 0.03 |

Body edit (password.ts NFKC): **9.37s**, parsed=1, 5 non-TS fallbacks because `fileSetHash` of all `inv.Files` changed.

cloneFileState and reuseTSCache were not the floor.

## After optimizations (same clone, events sink)

| Run | Process complete | parsed | events | generation |
| --- | ---: | ---: | ---: | --- |
| Initial (file sink, not NATS) | 37.24s (publish 18s to 164 MiB JSONL) | 4181 | n/a | 0→1 |
| True noop | **1.51s in-run / 2.19s real** | 0 | +0 | 2→2 |
| Body edit | **3.26s** (before last detect trim; in-run prefix ~1.8s + extract 0.52s + publish 0.36s) | 1 | +1.8 MiB, 88 owners | 1→2 |
| True noop after detect trim | 1.51s in-run, 0 events, fallbacks=0 | 0 | +0 | 2→2 |

Body delta fallbacks=0: mdintent/manifests/hcl/python/swift did not re-run on a TS body edit.

Vs Product accepted 16.08s initial / 7.76s noop / 9.13s body: noop ~4×, body ~3×, and ~8× / ~5× vs initial. Still a fresh CLI (walk + content hash + state JSON). Not a resident watcher.

## What changed

1. Compiled ignore globs (`facts.GlobSet`) with skip-dir and extension filters. Walk 3.50s → ~0.43s.
2. Hash only extractor-owned content plus config basenames and prior state paths, after name-only detection. Conservative full-file hashing remains for detected extractors with no FileOwner/factFileOwner. 1.36 GiB / 45,105 files → 90 MiB / 9,058 files (0.28–0.39s).
3. FileListDetector for mdintent, grpc, openapi, asyncapi. AsyncAPI detection probes YAML always and JSON only when the filename contains `asyncapi`.
4. mdintent input digest = inventory **names** + markdown **content**. FileOwner extractors use owned-content digests. TS body edits no longer re-run markdown. v1 all-files `ExtractorInputHash` still matches an unchanged checkout.
5. Config fingerprint uses walked names instead of a second WalkDir.
6. Env-gated `ENOLA_GRAPH_PROFILE=1` phase timers (`internal/graphprofile`).

Content SHA-256 remains the dirty detector. Same-size edits with restored mtime still reparse (`TestSameSizeRestoredMtimeStillDirty`). Size/mtime are not used as the change oracle.

## Tests

- `go test ./internal/graphsession` (includes new TS-body/markdown and mtime tests)
- `go test ./internal/facts ./internal/extractors/{mdintent,grpc,openapi,asyncapi,tsextractor}`

## Limitations

- Fresh CLI still walks 45k names (~0.4s) and unmarshals 56 MiB state (~0.47s). Subsecond noop needs a committed compact scan index or a resident process.
- Body delta still reconstructs ~60k facts in ExtractSession, re-hashes 4281 TS records before End, and rewrites 56 MiB pending state (~0.5s+0.16s+0.16s).
- AsyncAPI JSON specs whose filename does not contain `asyncapi` are not used for detection (YAML and `asyncapi.json` still are). Extract still reads all JSON/YAML candidates if the extractor runs.
- First run against a v1 Product state may re-run non-TS extractors once if stored digests used all-file hashes; subsequent noops are stable. Root Product timings should use this binary for initial+delta.
- File-sink initial publish is not comparable to NATS Product initial (18s of JSONL write).
- Root owns final NATS Product benchmarks and docs.
