# Preserve proven parses across consecutive refusals

Repeated edits can supersede more than one watch attempt before Begin. Previously,
the second refused attempt offered only its fresh parses to the third attempt:
records it had safely reused from the first attempt were lost. The graph stayed
correct, but the third attempt parsed those files again.

The fix carries fresh parses plus records actually accepted and revalidated in
the current attempt, accumulating accepted paths across all dependency-analysis
waves. It reads their final records from the completed preview and retains the
existing invalid-record filters. It does not union all previous offers or weaken
the source, side-read, membership, configuration, or per-file context checks.
Records adopted from the committed cache are not added to this offer. The
announce-time fence and publication protocol are unchanged.

## Controlled Product comparison

Three runs per arm used the same Product source as the
[earlier comparison](STAGE9_EARLY_SUPERSESSION.md), with a 500 ms collection
window, real CLI and NATS, and isolated checkout copies. The
[harness](consecutive_retry_watch.py) changes three independent files and
password.ts, then supersedes password.ts after each of the first two post-edit
frozen-preview traces. Each run independently confirmed exactly two
`superseded=true` pre-Begin refusals in stderr.

This is deliberate scheduling, not natural editing latency or ordinary delta
acceptance. Runs were sequential, ordered baseline/candidate, candidate/baseline,
baseline/candidate. Both binaries use main `ba5497a`, Go 1.27.1, `-trimpath`, and
profiling; the candidate overlays only the survivor change. The integrated
version differs from the measured candidate only in a documentation comment.

| Measurement | Baseline | Candidate |
| --- | ---: | ---: |
| Final convergence, run 1 | 4.428764 s | 4.331228 s |
| Final convergence, run 2 | 4.679885 s | 4.288756 s |
| Final convergence, run 3 | 4.559917 s | 4.326697 s |
| Median convergence | 4.559917 s | 4.326697 s |
| Successful final-attempt parses, every run | 33 | 11 |
| Parse operations across all post-initial attempts, every run | 78 | 56 |
| Begin owner scope, every run | 34 | 34 |
| Initial median | 9.408190 s | 9.203749 s |

Median convergence improves by **5.11% (0.233220 s)** in this scenario. The
candidate has fewer final-attempt parses in every run, and convergence is lower
in each pair. Initial ranges overlap widely (8.847–10.750 s versus
8.900–10.229 s); this change does not establish an initial speedup. Reducing
parsing alone does not remove the remaining policy, resolution, state, and
publication costs.

The 33-to-11 comparison describes only the successful final attempt. Summing
the extraction-wave counters across both refused attempts and the final attempt
gives 78 versus 56 parse operations, a 28.2% reduction in total post-initial
parsing work. These are operations, not unique files: the changing file and
affected dependents can be parsed again. Wave sizes vary in one candidate run;
the scheduler is controlled by preview signals, not a guarantee that every
earlier filesystem event is captured in the same batch.

All six runs have stable inputs across cold validation, exact cold equality,
the same initial and final graph hashes across arms, zero abandoned Begins,
matching batch counts, and zero observed idle/identical-save events. Finite
quiet windows do not prove an internal watcher drain. Convergence measures the
last observed edit to the first final-cold-equal completion, using the observer's
timestamps; the full clock and selection limitations remain in each receipt.

One earlier paired attempt was stopped during checkout copying after a worker
test suite started concurrently. It produced no accepted watch result and is
excluded entirely. The six measurements above ran after that suite stopped.

## Regression evidence

An independent fixture fails on the old implementation because nine reusable
records disappear after the second refusal. A separate multi-wave fixture
also fails on the first draft, which reset the accepted-path list on each
dependency wave. Both pass with accumulation across waves.

Further tests cover three refusals, alias and resolution-precedence changes,
membership changes, one changed carried file while other files remain reusable,
deletion, cold equality, and zero publication on refusals. Existing per-file
and side-read guards remain unchanged. The independent focused run passed in
13.396 s; the worker's final focused run passed in 11.237 s.

Full graphsession validation from the real Git checkout passed with exit 0 in
516.008 seconds wall time. The worker's interrupted suites are not passing
validation evidence.

Receipts:

- [Full Product results](stage9-consecutive-product-results.json)
- [Computed summary](stage9-consecutive-product-summary.json)
- [All-attempt parse counters and raw marks](stage9-consecutive-parse-work.json)
- [Build commands and binary hashes](stage9-consecutive-builds.json)
- [Validation record](stage9-consecutive-validation.json)
- [Baseline failing regression](stage9-consecutive-baseline-failure.log)
- [Worker focused results](stage9-consecutive-worker-focused.log)

- [Full graphsession output](stage9-consecutive-full-graphsession.log)
