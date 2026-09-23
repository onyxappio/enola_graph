# Resolver replay integration and matched Product result

The stage7 patch skips unchanged plain-TS importers when only unused declared
names move, and republishes proven global-name consumers from cached local
facts. Replay candidates are reconsidered until the dependency closure settles.
Re-exports, changed import provenance, side reads, Nuxt, Angular and route
composition retain conservative analysis when the replay proof is unavailable.
Frozen Begin, direct facts and generation semantics are unchanged.

## Matched real-history benchmark

Product `07fb4a41ddaf` → `fec1eac346c4`; baseline stage6 + wave10 `1ba06f9`;
candidate reviewed patch over `742ff51`. Same isolated checkout, full pinned
profile, Go 1.27.1 arm64, `-trimpath`, fresh CLI and file sink. Independent parent
states per binary restored for each repetition; order baseline/candidate/
candidate/baseline. This is not NATS or resident-watch acceptance.

| Metric | Baseline | Candidate |
|---|---:|---:|
| Delta seconds, repetitions | 12.441 / 12.461 | 12.38 / 12.262 |
| Delta median seconds | 12.451 | 12.321 |
| Parsed files | 624 | 591 |
| Resolution parses | 528 | 495 |
| Frozen Begin owners | 1811 | 1783 |
| No-op seconds, repetitions | 2.066 / 2.092 | 2.155 / 2.076 |

Parsing decreases by 33 files (5.3%), but median elapsed time decreases only
1.04% on this shared host. **This does not demonstrate a material latency improvement.**
The remaining 495 resolution parses and startup/publication costs still need
work. Targeted fixtures demonstrate fewer parses for unused names and cached
rebinding; they do not establish the desired large Product speedup.

All four deltas exactly equal the same cold target graph and pass frozen
Begin/End manifest and batch integrity validation. All four following no-ops
parse zero files, publish zero events, retain generation 2 and leave state JSON
hashes unchanged. The checkout was restored to the child revision.

## Validation

Full actual-main `go test ./...` passes, exit 0, 334.014 s (Go 1.26.8). Worker
full affected suites also pass: graphsession 263.930 s, bootstrap 82.574 s and
TypeScript extractor 29.006 s. Nineteen focused guards pass; four negative
controls fail as intended when individual replay protections are reverted.
Independent root tests cover unused export additions, global target-ID
add/rename/restore, and simultaneous default-reexport-origin/global-name changes.

The initial Go 1.26.8 candidate build was preserved but never benchmarked.
The measured candidate was rebuilt with baseline-matching Go 1.27.1; the harness
asserts matching toolchains and trimpath. Exact source hashes, build receipts,
raw results and restore evidence are in [the paired receipt](v314-resolution-paired.json).

Watch deletion-race retries, recovery under broker outage, prolonged real agent
editing, and near-zero fresh CLI no-op are still incomplete overall-goal work.
