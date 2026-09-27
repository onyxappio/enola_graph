# Fresh CLI no-op: next investigation, not an implemented optimization

Read-only review during Stage32 timing. Evidence is the earlier test-inclusive
profile `../test-source/profile-20260927/r1-new-noop.log`, not a new Stage32
measurement. Its CLI total was 2.197s with zero parses. Nested timings must not
be summed twice.

- Initial graph-input policy construction: 0.356s.
- Committed state: 90,686,087 bytes; read 0.014s, fingerprint 0.040s,
  JSON decode 0.405s (load total 0.459s).
- Runtime inputs: 1.029s, including inventory 0.124s, extractor detection
  0.148s, hashing 7,791 inputs 0.297s, context capture 0.080s,
  configuration fingerprint 0.073s, TS discovery 0.190s and TS context 0.088s.
- Cached TS fact assembly alone still costs 0.083s.

Investigate whether already-proven unchanged semantic inputs can bypass derived
TS discovery/context recomputation before considering a new persisted cache.
The proof must include scope membership, configuration and missing/external
side-input dependencies, extractor/version identity and recovery state; matching
only changed source paths or size/mtime is insufficient. Unknown consumers must
retain conservative behavior. No new relationship index is proposed.

State decoding is a separate opportunity, but TestFreshNoopRetainsResultFacts
requires the normal API to return exact cached facts on a fresh no-op. It cannot
be optimized by silently dropping those facts. Any summary-specific path would
need an explicit contract and evidence covering recovery, migration, stale state,
file additions/deletions/renames, policy/config edits and cold graph equality.

These are investigation targets, not measured savings or accepted designs.
Resident-session reuse does not prove fresh-CLI performance. Do not change the
frozen Stage32 binary while its cohort is running.

## Source audit after publication (2026-09-27)

A shortcut based only on State.ConfigHash, file hashes and inventory membership
is not yet justified. `TSExtractor.Discovery.reobserve` independently proves
three input classes: stat presence/type, uncaptured live side-read bytes (including
external extends), and walked directory membership/type. The retained Discovery
contains those observations in memory; State's TSContext/TSFileContext/TSFileBase
store derived projections, not the full observation proof. A fresh process cannot
recover the former from the latter. Skipping DiscoveryFor/SessionContext on these
existing durable hashes alone would drop an existing correctness boundary.

Do not implement that shortcut or call its estimated 0.278s a saving. Next inspect
whether discovery and configuration capture can share already-performed reads
within the same fresh run, retaining side-input observation and the existing
post-run fence. This avoids requiring a new durable dependency structure. State
decode and graph-input-policy construction remain independent larger targets.
