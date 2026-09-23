# Retirement optimization with accuracy wave10

Accuracy wave10 landed in remote main while the preceding retirement change was
being validated. The retirement commit was rebased without conflict onto
`c648dd7`, producing `1ba06f9`. All accuracy changes, including Nuxt scope and
default-reexport side-read tracking, are retained. Earlier V304 reports remain
measurements of their stated older source base; they are not current-build timings.

## Matched Product benchmark

Same Product parent/child `599575d0aa39` → `ae233c5f5695`, separate parent seeds,
restored per repetition. Baseline is an exact archive of `c648dd7`; candidate
is main `1ba06f9`. Both built with Go1.27.1 `go build -trimpath`, same isolated
checkout and fresh CLI/file sink. Order baseline/candidate/candidate/baseline.
The raw provenance records binary hashes and source identities.

| Metric | Accuracy wave10 baseline | With retirement proof |
|---|---:|---:|
| Delta seconds | 29.629 / 28.246 | 9.180 / 9.021 |
| Median seconds | 28.9375 | 9.1005 |
| Begin owners | 8477 | 823 |
| Published owners | 4962 | 822 |
| TS parses | 101 | 101 |
| File reads | 205 | 205 |
| Events | 2317 | 379 |
| Exact cold graph | 2/2 pass | 2/2 pass |

Observed median ratio **3.18×**. All four runs verify the frozen scope,
End count/digest and batch integrity. Both initial parent graphs also agree.
Shared host: independent tests and watch soak running, only two samples per
build, no NATS or p95/p99 acceptance claim. New cold hash reflects accuracy
wave10 semantics, so no comparison against the older graph oracle is used.
A mistakenly built candidate at the initial baseline output name was never
measured; the corrected baseline was built from the verified archive under a
distinct filename, as recorded in the build receipt.

[Raw evidence](v314-retirement-paired.json).

Full actual-main `go test ./...` exited 0. [Validation receipt](v314-retirement-validation.json)
and [full log](v314-retirement-full-tests.log).

After a new narrowed retirement delta, three no-op runs took 2.477 / 2.646 /
2.376 s. All parsed zero files, published zero new events, retained generation 2
and left every state JSON hash unchanged. Median 2.477 s remains above the
near-zero fresh CLI goal. [No-op evidence](v314-retirement-noop.json).
