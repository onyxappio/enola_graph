# Stage20 resolution allocation: correctness validated, timing pending

Frozen candidate `d39c43e793919ac5d0beffe91cfccb9ad3ab9676` changes resolution-array construction and internal candidate identity keys; it is **not integrated** and makes no CLI latency claim yet.

The Product binary was built from candidate01, SHA256 `787f36ded2fcd0f26fb2153358a5001912e00e59fd267442dd366515d87d7685`. Final production source differs only in an explanatory comment block, verified against both frozen files; final tests were independently run on the committed test file. External FactID bytes and cached fact payloads are unchanged. Internal tuples can distinguish identities that the former truncated hash or NUL delimiter merged; this intentional conservative difference is documented and tested.

## Allocation diagnostic

Same corrected benchmark harness and pre/post Product states; exactly one source content hash changes. Three repetitions, three iterations each; fixture reconstruction omits composition extras, so this measures isolated operations rather than a captured whole phase. Host contention excludes timing interpretation.

| Operation | Baseline median B/op [min,max] | Candidate median B/op [min,max] | Reduction |
|---|---:|---:|---:|
| ResolutionIndexes | 174175477 [173504778,178212178] | 68691312 [68691312,68724485] | 60.56% |
| ChangedCandidateNames | 64159176 [64159176,64159176] | 7443064 [7443064,7443064] | 88.40% |

These are allocated bytes per operation, not peak RSS or CLI latency. See [comparison](stage20/allocation-comparison.json).

## Correctness

Independent candidate01 checks passed eight frozen/recovery/resolution regressions. Four affected worker suites passed (`graphsession` 483.074s, `engine` 61.024s, `graphinput` 28.362s, `bootstrap` 143.684s); durations are operational test times under shared load. Root then reviewed corrected synthetic-owner, cold-input, path/repo and same-name delimiter-collision tests: 19 selected tests passed on the final committed source in 0.467s. Tests changed after the full suite, executable code did not.

Product initial, no-op, body and structural scenarios pass cold equality; unchanged no-op has zero parses/events, stable generation and byte-identical state. The body edit is graph-neutral; the structural edit changes the graph. All measurements here are correctness-only.

One continuous 11-revision chain completed 44 CLI invocations: all 10 transitions equal both candidate cold and baseline cold, and every follow-up no-op is silent and state-preserving. The chain includes three manifest transitions and no tsconfig transitions; a separate config-addition transition is evaluated below.

| Target | Parsed files | Published owners | Cold/no-op checks |
|---|---:|---:|---|
| 07fb4a41 | 5 | 5 | PASS |
| fec1eac3 | 513 | 1149 | PASS |
| 9fc7ae5b | 25 | 1662 | PASS |
| 1c260747 | 431 | 554 | PASS |
| 599575d0 | 7 | 9 | PASS |
| ae233c5f | 92 | 345 | PASS |
| 4168360e | 111 | 150 | PASS |
| a6f1f3a9 | 80 | 80 | PASS |
| 5dfb2c8f | 43 | 126 | PASS |
| a609c19f | 37 | 46 | PASS |

Replacement scope, published owners and parsed files are distinct. Broader replacement is not evidence that every owner was reparsed. See [history receipt](stage20/history-receipt.json) and [changed inputs](stage20/history-input-changes.json).

## Pending

Repeated quiet CLI timing, memory assessment, any required integration reconciliation, normal hooks and production push remain. The proposed 02:05–02:15 UTC slot was cancelled before launch because the accuracy matrix remained live; no measurements from an unconfirmed window are accepted. This stage does not complete the near-zero fresh no-op or overall performance goal.

## Supplemental config-addition transition

Product `d0fbbf8` → `a2ac71a` adds state-machine-telemetry source, manifests and `tsconfig.json`: delta parses 6 files and publishes 8 owners, matching candidate and baseline cold graphs; both no-ops are silent and state-preserving. This covers package/config addition, not isolated mutation of an existing tsconfig resolver option.

The inherited history driver exits 1 because its series validator requires 11 commits/10 transitions; its original `accepted=false` receipt is preserved. A separate exact two-revision/one-transition audit recomputes all source/tool/clean-tree/cold/no-op gates and passes, with three negative controls rejected. This is accepted only as the single transition, never relabeled as a successful ten-case run. See [original receipt](stage20/receipt-config-add.json) and [independent audit](stage20/config-add-independent-audit.json). No performance timing claim follows.

## Live watch correctness

One baseline/candidate Product pair passed with a 500 ms watch collection window: the scripted burst changes the graph, final watch output equals a fresh cold analysis, inputs remain stable across cold, idle and identical-save publication are both zero, there are no abandoned Begins and batch counts match. Both arms parse 11 files for the final delta. The harness observes quiescence; it does not prove an internal watcher queue drain.

This pair is correctness-only under concurrent accuracy load, explicitly ineligible for latency acceptance. End-to-end harness durations were 48.77 s baseline and 49.13 s candidate, including setup, waits and cold verification; these are not delta latency. See [watch receipt](stage20/watch-correctness-pair.json) and [pins](stage20/watch-pins.json).

Timing summary preparation additionally rejects missing joint holds, incomplete/nonalternating cohorts, mismatched acknowledgment receipts, failed arms and arms ending outside the agreed window. Negative-control checks passed; no timing comparison exists yet.

The later proposed 02:33–02:43 UTC window was also cancelled before launch: accuracy had naturally completed its 26-case matrix and confirmed GO, and the own worker confirmed GO, but Codata never confirmed by the start. Both holds were released; no timing-window authorization or timing evidence was created. Next scheduling requires Codata availability first.
