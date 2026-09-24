# Product input-policy rebuild attribution

One instrumented Product watch diagnostic attributes the previously observed
roughly half-second input-policy rebuild. This is a diagnostic, not a repeated
performance comparison or acceptance result.

The binary is based on `ad7bfb0`, with trace-only additions in
`internal/graphinput/policy.go` and `pkg/bootstrap/graph.go`; main `4224bfe`
has the same production implementation. It was built with Go 1.27.1 and
`-trimpath`. Binary identity and complete observer evidence are in
[the watch receipt](stage9-product-policy-watch.json).

The run uses the same Product source and controlled disjoint-edit/retry harness
as [the nine-run comparison](STAGE9_EARLY_SUPERSESSION.md), with a 500 ms fixed
collection window. It is scripted supersession, not natural agent editing.

| Stage, seconds | Initial construction | First rebuild | Second rebuild |
| --- | ---: | ---: | ---: |
| Discover Git | 0.039 | 0.069 | 0.047 |
| Declare Git controls | 0.012 | 0.010 | 0.010 |
| List tracked paths | 0.053 | 0.072 | 0.067 |
| Walk tree | 0.115 | 0.113 | 0.112 |
| Ignore helper setup | 0.015 | 0.020 | 0.018 |
| Run ignore check | 0.066 | 0.058 | 0.058 |
| Compute identities | 0.148 | 0.148 | 0.147 |
| Outer policy construction span | 0.448 | 0.493 | 0.461 |

Each construction sees 45,105 tracked paths, 5,818 tracked directories,
11,637 walked names, four ignore files, and 13 declared dependencies.
Engine allocation and plugin registration each round to 0.000 seconds in this
trace; they do not explain the policy rebuild cost. Values are rounded to
milliseconds, so zero does not establish absence of work.

The outer span contains the inner stages, options preparation, and deferred
temporary-repository cleanup. Do not add inner and outer durations or equate
their totals. Raw marks are preserved in
[the trace log](stage9-product-policy-trace.log), with a parsed copy in
[stage data](stage9-product-policy-stages.json).

Final convergence was 4.253177 seconds. The completed delta published 34 owners
and parsed 12 files; its graph hash exactly matched the cold result. Idle and
identical-save observation windows each recorded zero events. These finite
observations do not prove an internal watcher drain, and this single run does
not establish a new speedup.

Next candidates are identity computation, tree traversal, and Git subprocess
costs. Any reuse must retain freshness checks for policy, configuration, and
membership changes. Elapsed time since a previous attempt is not evidence that
those inputs remained unchanged. Consecutive-refusal parse retention is a
separate optimization and needs its own multi-refusal validation.
