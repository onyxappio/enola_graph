# Manifest/config scope: repeated Product watch comparison

Candidate production code is local commit 9ca81a4, measured through private snapshot 1e77fa7d; production trees under internal, cmd, go.mod and go.sum match, excluding tests. Baseline snapshot bb89663 has production code equivalent to 5627150. This compares one isolated optimization, not completion of the overall efficiency goal.

All runs use pinned Product a609c19f3861971930fae7b33dcb2950598953c5, the same full extractor profile and repository scope, an isolated NATS broker, and a 5-second watch buffer. A script changes tracking-client package version 0.7.2 to 0.7.3 and appends one exported constant to its source. This is a scripted two-file burst, not an AI-editor soak. Each run observes for 180 seconds and compares the applied graph to a cold target analysis, with identical input hashes before and after cold verification.

## Individual samples

| Build | Repeat | Initial to consumer s | Delta Begin–End s | Fsync to consumer s | Begin owners | Batches | Payload bytes | TS parses | Go/test samples before delta applied |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| baseline | 1 | 9.344 | 17.343 | 24.254 | 8645 | 2689 | 78222886 | 1 | 0 |
| candidate | 1 | 10.507 | 0.923 | 7.608 | 33 | 3 | 73238 | 1 | 0 |
| candidate | 2 | 9.083 | 0.935 | 7.605 | 33 | 3 | 73238 | 1 | 6 |
| baseline | 2 | 9.530 | 3.744 | 10.263 | 8645 | 2689 | 78222886 | 1 | 0 |
| baseline | 3 | 9.215 | 3.629 | 10.112 | 8645 | 2689 | 78222886 | 1 | 0 |
| candidate | 3 | 9.460 | 1.024 | 7.765 | 33 | 3 | 73238 | 1 | 4 |

## Median and full range

All six samples are retained, including the slow baseline and samples with concurrent Go/test processes. No outlier was discarded. These are observations on a shared host, not uncontended causal estimates.

| Metric | Baseline median [min, max] | Candidate median [min, max] |
| --- | --- | --- |
| Initial to consumer, s | 9.344 [9.215, 9.530] | 9.460 [9.083, 10.507] |
| Delta Begin–End, s | 3.744 [3.629, 17.343] | 0.935 [0.923, 1.024] |
| Fsync to consumer, s | 10.263 [10.112, 24.254] | 7.608 [7.605, 7.765] |

## Startup and sampled memory

Broker-End timing is an observation of the durable End frame, not a direct instrument of the publisher returning from its final acknowledgment. Cross-component timestamps use the same host clock but have unquantified skew. RSS is sampled across the watch run; it is not a captured instantaneous peak.

| Build | Repeat | Launch to initial first batch s | Launch to initial broker End s | Sampled maximum RSS MiB |
| --- | ---: | ---: | ---: | ---: |
| baseline | 1 | 6.445 | 9.256 | 926.7 |
| candidate | 1 | 7.426 | 10.423 | 839.5 |
| candidate | 2 | 6.027 | 8.999 | 901.7 |
| baseline | 2 | 6.224 | 9.447 | 953.1 |
| baseline | 3 | 6.074 | 9.128 | 930.9 |
| candidate | 3 | 6.301 | 9.372 | 896.5 |

Completion selection uses heuristic quiescence plus observed cold equality; the harness does not prove an internal watcher-drain watermark.

## Correctness and interpretation

- All six runs pass exact cold graph equality with 45,106 stable input files and one delta generation per scripted burst.
- Candidate replaces 33 owners in 3 batches / 73,238 payload bytes; baseline replaces 8,645 owners in 2,689 batches / 78,222,886 bytes. Both parse one TS file and produce a net increase of one node and one edge. Scope and transport work shrink; this does not demonstrate fewer TS parses.
- Initial means watch launch to the harness consumer applying initial End. Begin–End covers only the observed replacement interval, excluding pre-Begin planning. Fsync-to-consumer includes the configured 5-second buffer, planning, publication and consumer application. These figures do not measure completion in Codata.
- Process sampling runs every 5 seconds and records Go/test processes only. Zero samples does not prove an idle machine; activity after delta completion may affect cold validation but not the already-completed delta interval. The archived samples preserve timestamps and observed CPU use.
- This short scripted case does not replace prolonged AI-editor, failure/recovery, no-op, fresh-CLI or multi-commit validation. Unsupported non-TS consumers can still require conservative whole-domain replacement.

Raw data: `config-watch-matched-final-1.json`. Production equivalence is included in that archive.
