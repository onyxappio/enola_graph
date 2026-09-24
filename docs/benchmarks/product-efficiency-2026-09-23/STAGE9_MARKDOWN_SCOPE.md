# Directory module scope regression

An unrelated TypeScript addition in Product included 617 unchanged Markdown owners
in the frozen Begin scope. The planner compared published owner keys with the
current semantic inventory. Existing files without contributions therefore looked
like new module owners on every membership change.

The planner now bounds additions by active extractor ownership and, where declared,
its module-candidate domain. TypeScript HTML templates affect extraction inputs but
cannot independently introduce a directory module. Prior published-owner departures
remain conservative, as do extractors without an ownership declaration. Non-TypeScript
module candidates retain the same invalidation path.

This changes planning only: local extracted facts and their cache format are unchanged.
The patch preserves the existing extraction cache version.

## Diagnostic Product evidence

The pinned Product fixture uses revision `a609c19f3861971930fae7b33dcb2950598953c5`.
These arms use Enola base `0260153`, including experimental overlays; they do not
establish performance acceptance for the current merged main build.

| Arm | Delta Begin owners | Delta records | Delta bytes |
|---|---:|---:|---:|
| Control | 618 | 140 | 6,078,481 |
| Ownership bound | 358 | 79 | 3,407,124 |
| Module-candidate bound | 1 | 3 | 2,559 |

Every arm has its own initial state and event stream. Independent strict parsing,
Begin/End manifest and batch digest validation, and replay produced the same final
graph as the separate cold stream. See `stage9-markdown-product-audit.json`.
The final candidate parsed one file. Scope and payload reductions do not imply an
equivalent latency speedup; repeated matched timings remain required.

## Current-main verification

Independent tests on base `8704d61` with the final patch cover TS/Python module
addition, rename, deletion, resident event handling and final no-op; targeted tests
passed in 19.389 seconds. An Angular sequence covers template addition/removal,
an unrelated TS addition and removal of the Angular manifest dependency, with
cold equality after every step; it passed in 4.843 seconds.

The final experimental old-base full graphsession suite failed after 518.608 seconds
in `TestFileChangeSourceRealWritesAtomicSaveAndNewDirectory`: the final nested-file
write was not observed within the test deadline. Twenty isolated repetitions on
the integrated current-main source passed in 1.367 seconds. This does not turn the
failed full run into a pass; a test synchronization race is under investigation.
The full integrated `go test -count=1 -timeout=20m ./...` passed (exit 0),
including graphsession at 600.785 seconds. The watcher test passed in this full run.

Current-main Product streams also passed independent protocol and cold-equivalence
checks: control scope 618, candidate scope 1, one parsed file. All three streams
share graph hash `0ac6ae9f73f5f53fc4c173eeee2059a69ee539e1471f32a5fa95b16ea7cb01e1`.
The experimental candidate binary has base `8704d61`, Go 1.27.1 with trimpath,
and SHA-256 `8270c2316015ac8b199f8587c92aae625899b2fbf293a1a855364a173382d497`.
Its four changed production files match the integrated source byte-for-byte.
These are shared-host correctness diagnostics, not repeated performance acceptance.

## Remaining work

An unused package addition still dirties unrelated TS contexts. A separate regression
also demonstrates that adding an unclaimed JSON file can publish an unnecessary
whole-domain replacement despite zero parses and unchanged graph facts. These
remain open, alongside fresh CLI no-op overhead and final matched performance gates.

## Repeated fresh CLI timing

Three runs per build, alternating control/candidate/candidate/control/control/candidate,
on a separate copy of the same Product fixture. Control is `50b64fd`; candidate is
clean `a648c0b`. Both binaries use Go 1.27.1 with trimpath and the same file sink
configuration. Values below are whole CLI wall time, median (minimum–maximum).

| Phase | Control seconds | Candidate seconds |
|---|---:|---:|
| initial | 25.934 (24.342–26.224) | 23.995 (20.389–25.441) |
| delta | 4.776 (4.119–4.957) | 3.443 (3.430–3.803) |
| noop | 1.813 (1.738–1.826) | 1.780 (1.694–1.879) |

The observed median delta fell about 28%, with one parse in both builds and scope
618 versus 1. Initial and no-op ranges overlap; this run does not establish an
improvement for those phases. Every successful arm has the same initial/final
graph, verified manifests and batch digests, and a no-op with zero parses/events,
no generation advancement and unchanged persisted state. A separate final cold
analysis matches all six delta graphs.

One additional candidate initial was rejected because a coordinator `git status`
refreshed the fixture index during analysis. The failed arm is excluded and
preserved; the successful sequence resumed without further Git commands on the
fixture. Alias tests overlapped parts of the measurement on the shared host.
These file-sink diagnostics do not establish final NATS acknowledgment performance.
Raw timing, binary hashes and interruption details are in
`stage9-markdown-repeated-timing.json`.
