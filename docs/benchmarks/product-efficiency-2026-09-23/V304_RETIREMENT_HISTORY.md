# Non-TypeScript retirement scope: exploratory Product evidence

This records the earlier frozen three-file exploratory overlay over `f0101f1`.
Final integration and matched repeats are recorded in [the paired report](V304_RETIREMENT_PAIRED.md).
Product `599575d0aa39` -> `ae233c5f5695`, an independently initialized parent.
The transition changes 60 paths and includes a retired Markdown owner.

| Metric | Current package-gate history case05 | Exploratory retirement candidate |
|---|---:|---:|
| Begin owners | 8477 | 823 |
| Parsed TypeScript files | 101 | 101 |
| Actually changed owners | 25 | 25 |
| Delta seconds | 21.917 | 8.055 |

Candidate publishes 822 owners in 377 batches. Exact cold graph equality and
frozen Begin/End scope and batch digests pass; process exited 0. Cold target
analysis took 26.391 s. Scope/parse counters establish the intended reduction;
wall times are single shared-host diagnostics with distinct seed histories and
must not be reported as repeated acceptance. The change reduces replacement
work, not this transition's TS parse count.

Root independent guards cover Markdown link retirement, an unchanged global-name
consumer, and manifest retirement affecting a TS dependency reference. Each
matches cold first, fails on unrelated-owner inclusion on current main, and
passes on the exploratory overlay. The candidate admits a narrower retirement
only when every contributor to that prior owner had a bounded pre-Begin preview;
unknown/unpreviewed contributors retain the broad fallback. Deleting the last
Markdown file currently loses extractor detection and therefore keeps fallback;
this is an explicit remaining boundary.

[Candidate result and source/binary hashes](v304-retirement-history-exploratory.json).
Worker affected suites passed, the final seven-file freeze was verified and
integrated locally, and four matched repeats passed. Full-main tests are tracked
with the final integration receipt.
