# Product membership fix: frozen scope reduced to 911 owners

The five-file invitation-link burst now replaces **911 owners instead of 8647**
and reports **18 parsed files instead of 293**. The accepted run exactly matches
cold analysis, with identical 45110-file inventory fences across cold verification.
This is a measured scope reduction; timings below are one accepted sample per
candidate, not repeated performance acceptance.

## Same input and profile

Product revision: `a609c19f3861971930fae7b33dcb2950598953c5`. Full default extractor
profile, existing Product exclusions, isolated broker and checkout, 5-second watch
collection window. The exact same final five-file patch was replayed as one burst.
The source snapshot is based on `fffc0b7` plus the three files and patch hashes
recorded in [rebound-product-result.json](rebound-product-result.json).

| Metric | Previous checkpoint | Fixed candidate |
|---|---:|---:|
| Begin owners | 8647 | 911 |
| Parsed files reported by End | 293 | 18 |
| Batches | 2690 | 237 |
| Delta Begin → End | 3.646 s | 1.378 s |
| Initial Begin → End | 6.070 s | 6.405 s |
| Exact final cold equality | yes | yes |

Scope is 9.49× smaller; the parsed-file counter is 16.28× smaller. The single-run
Begin → End interval is 2.65× lower, but host contention and unequal harness
observation windows preclude treating that as a stable speedup estimate. Initial
completion was not optimized here; Begin → End is not complete initial wall time.

The fixed delta sent 239 messages (Begin + 237 batches + End), 10265 node records,
15160 edge records and 9656176 bytes of JSON payload including Begin/End, excluding
broker framing and headers. These are replacement records, not newly created
entities. A read-only live JetStream audit checked every sequence in that run and
matched the End batch count.

## Why it changed

Previously, unchanged unresolved slash-bearing imports were considered rebound.
That unnecessarily reparsed unrelated owners; their old declarations could then
trigger a whole-domain name-resolution fallback. The fix compares actual old/new
resolution outcomes and shares the same replay predicate between planning and
extraction. Required global-name consumers become seeds in the frozen pre-Begin
plan; the dependency closure and conservative fallback remain.

A cache record carrying resolution outcomes but no import specifiers takes the
conservative fallback. New targets, removed targets, shadowed modules, renamed
owners and new declarations retain their regression coverage.

## Remaining work

Of the 911 owners, **893 are Markdown documents and 18 are TS/TSX files**.
The mdintent extractor still reruns its entire owner domain. Its optimization is
separate follow-up work and must preserve file/link and global-name dependencies.

The last observed filesystem mtime to consumer completion was 7.952 seconds,
including the collection window and planning. This is correlated observation,
not instrumented causal latency. The result is not a claim of near-instant watch
updates or complete initial startup below two seconds.

## Validation and limits

- Full graphsession package: exit 0, 175.025 s; command package: exit 0, 5.211 s.
- 20 focused planner tests passed; build and vet passed. Timings are test durations.
- Five observed input changes, no out-of-allowlist net changes, stable input fences.
- Final canonical hash: `17d786c72a1db2b92893bae783c022125fe7e0d052014bd3d7143dafcd7dd3d8`.
- Requested observation window 180 s; harness elapsed 215.2 s including setup and
  cold verification. This is a single burst followed by idle time, not sustained editing.
- First exploratory replay also produced 911 owners and cold equality but its old
  harness missed edit polling after slow setup. Excluded from accepted measurements.
- A subsequent setup failed before broker startup due to a missing copied harness
  template. The accepted run used the complete committed harness and fresh paths.
- No broker interruption/recovery or long-term memory acceptance in this burst.

The separate telemetry change adds automatic per-generation traffic and startup
metrics. Its Python self-tests and tiny watch/external-editor smokes passed (10 s
and 55 s for the final smoke runs). It was not used to derive these Product traffic
counts, which came from the independent broker audit.
