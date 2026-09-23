# Stage4 candidate: Product changed-file delta

Appended a unique exported async function calling fetch to packages/clickhouse/src/readQueryPolicy.ts in the disposable Product checkout at a609c19f3861. The mutation changes the graph. Forked the completed history state into a separate benchmark context, restored the identical seed before each delta, and restored source bytes in finally (verified SHA-256).

| Build | Run 1 s | Run 2 s | Median s |
|---|---:|---:|---:|
| Main 17226ed | 4.020 | 4.425 | 4.222 |
| Stage4 candidate | 3.462 | 3.701 | 3.581 |

Order baseline/candidate/candidate/baseline. Candidate median is 15.2% lower. All four runs parse 32 files, publish 32 owners and 580,584 event bytes. Every replacement passes the frozen scope/batch manifest validator and reproduces the complete cold graph exactly. The target cold run took 21.890 s; candidate delta median is 0.164 of this single cold run.

Fresh CLI, file sink, instrumentation enabled, shared host. Two repetitions of one source edit are diagnostic evidence, not complete watch/NATS/long-run acceptance. This patch removes duplicate policy construction; it does not narrow the 32-file parse set. [Raw results and provenance](v304-stage4-cli-delta.json).
