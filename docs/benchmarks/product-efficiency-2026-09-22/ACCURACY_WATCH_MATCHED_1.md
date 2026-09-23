# Integrated accuracy/admission: repeated Product watch comparison

Baseline: ba3ec15. Candidate: a257dd6. Both contain v291 extraction semantics. This comparison must not be combined with earlier v274 timings. Binary hashes and snapshot build provenance are archived alongside this report.

Pinned Product a609c19f3861971930fae7b33dcb2950598953c5; full extractor profile; same repository scope; isolated NATS; 5-second watch collection window. Scripted package version 0.7.2 to 0.7.3 plus one exported constant in tracking-client. This is a two-file scripted burst, not an AI-editor soak. Each run observes for 180 seconds and checks exact cold graph equality.

## Samples

| Build | Repeat | Initial launch to consumer s | Save to Begin s | Delta Begin to End s | Save to consumer s | Owners | Batches | Payload bytes | TS parses | Contention samples before delta applied |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| baseline | 1 | 35.982 | 7.560 | 16.052 | 23.690 | 8484 | 2597 | 78186647 | 1597 | 37 |
| candidate | 1 | 14.952 | 9.172 | 5.507 | 14.717 | 1629 | 1132 | 33866220 | 1597 | 14 |
| candidate | 2 | 14.519 | 8.752 | 6.108 | 14.891 | 1629 | 1132 | 33866220 | 1597 | 39 |
| baseline | 2 | 11.788 | 8.062 | 5.668 | 13.809 | 8484 | 2597 | 78186647 | 1597 | 1 |
| baseline | 3 | 17.491 | 7.495 | 10.533 | 18.109 | 8484 | 2597 | 78186647 | 1597 | 32 |
| candidate | 3 | 15.884 | 8.158 | 5.976 | 14.172 | 1629 | 1132 | 33866220 | 1597 | 12 |

## Median and full range

All samples are retained. Concurrent Go/tests are recorded. Shared-host observations do not establish uncontended causal speedups.

| Metric | Baseline median [min, max] | Candidate median [min, max] |
| --- | --- | --- |
| initial_launch_to_consumer (s) | 17.491 [11.788, 35.982] | 14.952 [14.519, 15.884] |
| fsync_to_begin (s) | 7.560 [7.495, 8.062] | 8.752 [8.158, 9.172] |
| begin_to_end (s) | 10.533 [5.668, 16.052] | 5.976 [5.507, 6.108] |
| fsync_to_consumer (s) | 18.109 [13.809, 23.690] | 14.717 [14.172, 14.891] |

## Interpretation

- All six runs pass exact cold graph equality with stable inputs. Each burst produces one completed delta.
- Save means completion of the scripted fsync. Save to Begin includes detection, the collection window, scheduling and preparation; it is not pure analysis time.
- Initial includes watch startup through application by the test consumer. Delta Begin to End excludes pre-Begin work. Test consumer application is not actual Codata completion.
- Counts measure replaced file owners, TS parses and emitted payload separately. Unchanged owners inside a conservative scope can still require publication.
- Five-second process samples only track Go/test processes. Absence of samples does not establish an idle host.
- Remaining work includes body-edit scope narrowing, redundant parsing, resident index reuse, fresh-CLI no-op overhead and prolonged watch/recovery validation. This checkpoint does not complete the efficiency goal.

## Before/after state attribution

The third candidate repeat archived its completed initial state before the controlled edit. This archival copy happened after initial completion and before the measured write timestamps; no graph inputs changed. Earlier repeats did not perform this copy.

Comparison of the actual committed generation 1 and generation 2 records finds one content-dirty TypeScript source, zero configuration-context-dirty sources, and one owner whose side-read hash changed. One source changes the surface used by the current propagation rule. Its reverse dependency closure contains 1,597 owners; 1,595 lie outside the first-pass seed union and retain identical complete FileRecord values after the run. The two other records change source facts/hash/declarations and a side-read hash, respectively.

This reproduces the closure from the actual before/after state; it is not live OnBeforeParse tracing. Identical output alone is not a correctness proof for skipping those parses. A narrower dependency rule must retain side reads, reexports, resolution/candidate changes and framework composition. Raw attribution and script: `v291-watch-parse-attribution.json`, `v291-watch-parse-attribution.py.txt`.
