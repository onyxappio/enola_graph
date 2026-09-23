# Proven retirement scope: matched Product repetitions

Stage6 consists of three production files over main `05607f7`; seven frozen files
including guards were independently hash-verified before integration. Baseline
is the stage5 binary whose production sources match `f0101f1`. The candidate was
built from current main with the reviewed patch, not the older worker checkout.

Product `599575d0aa398615cde6a4ac12bc0c7734a686d9` →
`ae233c5f56959ce5852c8381edd6cb472c4b9f95`, 60 changed paths. Independent parent
states for both binaries, restored before every delta in the same isolated
checkout. File sink, fresh CLI, baseline/candidate/candidate/baseline order.

| Metric | Stage5 baseline | Stage6 candidate |
|---|---:|---:|
| Delta repetitions, seconds | 21.933 / 22.525 | 7.616 / 7.319 |
| Median seconds | 22.2290 | 7.4675 |
| Begin owners | 8477 | 823 |
| Published owners | 4962 | 822 |
| Parsed TS files | 101 | 101 |
| Delta events, including Begin/End | 2317 | 379 |
| Exact cold graph | both pass | both pass |

Observed median ratio **2.98×**; frozen scope decreases **90.29%**.
All four runs pass full scope/batch count/digest verification and cold equality.
This removes unnecessary replacement/publication work, not the 101 local parses.
The retired owner is still declared in Begin even when it produces no new facts.

Shared host with tests and a separate watch soak active; two samples per build
are not a p95/p99 estimate or NATS/resident acceptance. Both use Go1.27.1 arm64;
baseline has `-trimpath`, candidate uses ordinary `go build`; complete build
metadata is retained. Initial seed times (37.163 / 26.736 s) were not alternated
or repeated and establish **no initial-speedup claim**. Target cold took 27.073 s.

The narrower scope is admitted only if every contributing extractor of a retired
owner actually completed a bounded pre-Begin preview. Unpreviewed/mixed
contributors retain fallback. Removing the last Markdown page still disables
extractor detection and keeps broad fallback. Root tests cover changed link
resolution and a retired manifest with an unchanged global-name consumer; worker
guards additionally cover rename, rebound links, no-op and mixed contributors.
Negative controls fail scope assertions after cold equality succeeds.

[Raw runs, source/binary hashes and restored checkout](v304-retirement-paired.json).
[Earlier single-run diagnostic](V304_RETIREMENT_HISTORY.md).

## Integrated validation

Full actual-main `go test ./...` exited 0 (graphsession 351.015s,
bootstrap 168.790s). [Receipt](v304-retirement-validation.json) and
[full log](v304-retirement-full-tests.log). Existing worker guards and three
independent fixtures are included in this run, not only overlay tests.

A fresh narrowed retirement run followed by three no-op runs on the same Product
child produced zero parses, zero new events, generation 2 unchanged and identical
state JSON hashes each time. Wall times 2.069/2.124/2.057s, median 2.069s; the
near-zero fresh CLI objective remains unfinished. The event field in the helper
originally counted the append-only sink; the report preserves that separately
and reports the verified byte-offset delta as new events.
[No-op evidence](v304-retirement-noop.json).
