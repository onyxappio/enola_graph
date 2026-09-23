# Watch retries after captured inputs disappear

A source removed during analysis previously returned a plain filesystem error,
which terminated watch. ENOENT/ENOTDIR on captured-source revalidation or
composition-context reads now signal input change: the attempt fails without a
successful End and watch reconciles again. Permission, I/O and other hard
failures remain errors. No synthetic-owner behavior changed.

## Full Product scripted stress

Five-minute requested editing window, full profile, NATS, fixed watch window
5 seconds and scripted edits every 3 seconds in an isolated Product synthetic
module. This is not a real AI feature-editing run or broker-outage acceptance.
The test binary uses Go 1.27.1, trimpath, stage7 main `9785cf6` plus the exact two
production files later integrated; their SHA256s match the immutable final patch.
Worker tests shared the host, so these timings are diagnostic.

| Result | Value |
|---|---:|
| Successful scripted operations | 82 |
| Completed initial / delta generations | 1 / 20 |
| Unexpected watch exits / restarts | 0 / 0 |
| Abandoned Begin attempts, superseded by retries | 5 |
| Exact final cold graph | PASS |
| Inputs stable across cold comparison | 45,109 files |
| Delta Begin→End median / maximum | 1.234 / 1.451 s |
| Mean interval between completed Delta frames | 11.998 s |
| Mean parsed files per completed Delta | 1.85 |
| Median / maximum Begin owners per Delta | 619.0 / 622 |
| Total Delta JSON payload bytes | 97,013,111 |
| Mean node / edge records sent per Delta | 5835.9 / 6986.7 |

Records sent include replacements of existing facts, not just new graph entities.
Completed-generation telemetry excludes abandoned attempts. Nearest-save latency
is correlation under overlapping edits, not a causal response time. The final
matching suffix was observed 8.313 s after the last save; this includes
collection time. No speedup ratio is claimed against the older failure run.

## Validation and harness corrections

Worker full graphsession suite passes in 275.654 s and bootstrap in 67.041 s
(Go 1.26.8). Root integrated Go 1.27.1 build passes and focused tests pass in
20.736 s. Independent authoritative watch test fails baseline on ENOENT and
passes the patch, checking recovery during Delta, generation preservation,
subsequent edits and exact cold. Deletion/rename shapes and strict no-op guards
cover both owner contracts. Four negative controls verify individual retry and
hard-error protections.

An earlier synthetic-module discrepancy was a test oracle error: it compared
legacy watch with authoritative cold. Same-mode comparisons pass; the claim
of a synthetic retirement defect is withdrawn and no such production fix exists.

The first stress attempt ended at the benchmark's quiescence gate before cold:
91 operations, 23 completed generations, no active Begin. The editor can rewrite
identical bytes, so requiring a post-save generation wrongly rejects correct
no-op. The gate now selects a stable, quiet, completed graph for mandatory cold
comparison even without a post-save frame. It explicitly reports absence of
processing evidence; active Begin, unstable inputs and quiet-margin checks still
block selection. Exact cold equality is still required for acceptance. Both
harness self-tests pass; reinstating the old gate fails the new regression.
The first attempt remains failed evidence; this second run independently passes.

[Raw second run](v314-watch-retry-burst5m.json) · [Audit, source hashes and test receipts](v314-watch-retry-audit.json).

Long real-agent editing, actual in-flight NATS outage recovery, fresh-CLI near-zero
no-op and substantial remaining delta acceleration remain open goal work.
