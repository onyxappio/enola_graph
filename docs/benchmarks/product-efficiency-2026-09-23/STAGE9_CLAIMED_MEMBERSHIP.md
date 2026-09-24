# Neutral unclaimed file membership

Adding, renaming or deleting an unclaimed JSON file previously triggered a whole
graph replacement even when no facts changed: three events, zero parses and a
new generation in the regression fixture. The candidate records a versioned
digest of names claimed by active extractors and uses it to prove such membership
changes neutral. It passes both the TypeScript-only and TypeScript-plus-Markdown
lifecycle checks, with zero events and no generation advancement.

Ownership alone is not a dependency proof. The existing declared-input and
TypeScript-context checks stay authoritative: a Markdown link gaining or losing
its target still updates the graph, as do source and relevant configuration
changes. Unknown ownership and unknown digest versions remain conservative.
Nothing changes the frozen Begin/End protocol or the final input fences.

The persisted fields are ScanClaimedHash and ScanClaimedMeta=claimed-v1.
Legacy state without that proof remains byte-identical on unchanged input.
Its first real membership change can conservatively publish and record the new
proof; the next neutral membership change is silent. Neutral observations can
refresh bookkeeping without advancing generation, and the following unchanged
no-op must not write state.

The first candidate still required a neutral non-TS preview. An independent
TS-only test exposed this: add/rename/delete each created a generation. Version 2
moves that requirement onto the two existing discharges that actually need the
preview. The new claimed-name comparison accepts an empty restoration while
retaining the declared-input, TS-context, marker and bounded-ownership guards.
Disabling the new proof or restoring the old outer gate makes the corresponding
regression tests fail.

[Validation receipts](stage9-claimed-membership-validation.json) identify the
experimental binary and source hashes. Independent focused tests pass in 16.250 s;
worker focused v2 tests pass in 15.578 s. The full v1 affected-package suite passed,
but does not substitute for v2 validation. The full v2 repository suite passed (graphsession: 595.351 s). All 10 Product transitions passed exact cold equality, frozen protocol checks and
per-transition persisted no-op verification. The production source matches the
full-suite snapshot and experimental binary overlay.

This removes unnecessary replacements, not the remaining fresh CLI startup
cost. Near-zero CLI no-op and faster watch retries remain open.

## Product history validation

[All 10 transition receipts](stage9-claimed-membership-history10.json) passed.
These are diagnostic file-sink runs on a shared host, not repeated NATS performance
acceptance. Scope counts are file owners; changed-fact owners are an after-the-fact
lower bound, not a proven safe planning scope.

| Transition | Changed paths | Begin owners | Changed-fact owners | Parsed | Delta s | Cold s | No-op s |
|---|---:|---:|---:|---:|---:|---:|---:|
| 00-4d104e600f89..07fb4a41ddaf | 6 | 7 | 2 | 6 | 3.977 | 25.924 | 1.88 |
| 01-07fb4a41ddaf..fec1eac346c4 | 9345 | 1168 | 83 | 591 | 13.929 | 24.201 | 1.839 |
| 02-fec1eac346c4..9fc7ae5b4c3f | 8125 | 1655 | 17 | 62 | 13.222 | 24.956 | 1.84 |
| 03-9fc7ae5b4c3f..1c2607479b6d | 70 | 554 | 32 | 432 | 10.001 | 23.122 | 1.864 |
| 04-1c2607479b6d..599575d0aa39 | 15 | 9 | 6 | 7 | 3.896 | 21.687 | 1.864 |
| 05-599575d0aa39..ae233c5f5695 | 60 | 344 | 25 | 99 | 6.415 | 23.977 | 1.853 |
| 06-ae233c5f5695..4168360e2e7f | 15 | 161 | 10 | 111 | 5.641 | 26.102 | 1.771 |
| 07-4168360e2e7f..a6f1f3a91a36 | 5 | 80 | 4 | 80 | 4.787 | 24.862 | 1.994 |
| 08-a6f1f3a91a36..5dfb2c8f276d | 15 | 126 | 5 | 43 | 5.914 | 25.157 | 2.94 |
| 09-5dfb2c8f276d..a609c19f3861 | 19 | 46 | 7 | 37 | 4.829 | 33.949 | 3.399 |

Broad membership/name/dependency scope remains open: transition 02 publishes
1,634 JS/TS owners and 21 Markdown owners, despite parsing only 62 files. Its
summary reports no raw config change and no context-affected sources. This rules
out the Markdown owner domain alone as an explanation; attributing the TS
expansion requires further planner profiling.
