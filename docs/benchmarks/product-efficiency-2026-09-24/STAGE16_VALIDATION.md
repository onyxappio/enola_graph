# Stage16 config-walk: measured candidate, integration pending

Candidate base: `49b486b4a43bc5e6ec1679db0dd947b66bbbf223`, cache v321 (experimental, no integration). Independent baseline binary SHA256 `b7b66f743741f50db5f323b34d632572af4a91ab80f3406104217ba7625a7c83`; candidate `a525f609318c573973b4ff7b32e08083f7273091b4e4c9d659d7a87cf883dc17`.

The candidate fuses the alias and configuration enumeration within each call. Both fresh fingerprint boundaries remain. Symlink/probe cases retain the prior traversal, and any directory-listing error withdraws fusion for the entire call. Differential tests cover policy, external configuration and actual rename transitions. Removing the error fallback makes its regression tests fail; restoring the second walk makes the traversal-work test fail.

Worker validation: full TypeScript extractor package passes (629 tests, four pre-existing corpus skips), 64 focused graphsession tests pass, vet passes. Root verified 17 frozen artifact/source hashes, applied the exact patch in an independent checkout, built it and passed targeted race tests (2.515s operational duration). Root independent full suite completed with exit 0 (`go test -p 2 ./... -count=1`, `GOMAXPROCS=2`); see `stage16-root-fullsuite.log` and its receipt. The graphsession package reported 403.748s, an operational duration under concurrent load, not a benchmark. Historical correctness and repeated timing series have completed. Memory follow-up and integration validation remain pending.


## Clean Product CLI correctness preflight
The source is a clean checkout of Product `a609c19f3861971930fae7b33dcb2950598953c5`, with no untracked source additions from the prior fixture. Both arms use the same explicit Product exclusions and TypeScript analysis profile. The runner checks binary, observer, harness and config hashes and clean Git state before copying the source. State-byte equality is now asserted directly for the unchanged no-op.

Both arms pass all four checks: silent unchanged no-op with zero parses and stable state/generation, and exact cold graph equality for initial, body and structural states. All seven normalized graph hashes match between builds. Initial parses are 4034, unchanged no-op 0, body delta 11, structural delta 12. The body mutation remains graph-neutral; the structural mutation changes the graph.

See [paired comparison](stage16-clean-preflight-comparison.json) and the adjacent per-arm checks, metrics, provenance and receipts. These runs are **correctness-only**: other tests ran concurrently and their durations are not performance evidence. The separate completed timing series is reported below.

## Clean Product watch correctness
Both ordinary-watch arms completed successfully on the clean pinned source. Initial hashes match across arms, and changed-state watch hashes match both each other and their fresh cold oracle. Both report zero idle/duplicate events, no abandoned or incomplete Begin/End pairs, stable cold inputs, and 11 parsed files for the final delta. The mutation changes the graph, so equality is non-vacuous. See `stage16-clean-watch-correctness.json`. These concurrent-load preflights are explicitly timing-ineligible. The harness observes lifecycle quiescence, not an internal watcher watermark; `internal_drain_proved=false` remains a stated limitation.

## Completed history acceptance
All ten transitions (eleven commits) completed successfully in one continuous state chain. Root independently audited all33 completed frames and66 lifecycle records: exact Begin/End pairing, v2 complete scopes, successful completeness, matching batch counts and applied generations. Every revision has chain = candidate cold = baseline cold hashes; every no-op has zero parses/events and unchanged generation/state bytes. Total delta parses:1344. See `stage16-history-root-audit.json`, `stage16-history-receipt.json` and adjacent consumer/lifecycle evidence. This supersedes the partial checkpoints above. No historical performance claim is made.

## Completed repeated timing review
All three CLI pairs and three ordinary-watch pairs completed with exit0, passing correctness gates and no sampled competing tests/builds/Enola processes. The joint window ended at21:00UTC; holds were released. Ambient ClickHouse background activity remained, recorded per pair; this is not proof of an entirely idle host. See `stage16-timing/quiet-comparison.json` and adjacent raw evidence.

| Scenario | Baseline median [min,max], s | Candidate median [min,max], s | Median change |
|---|---:|---:|---:|
| Fresh CLI initial | 8.433 [8.405,8.565] | 8.412 [8.286,8.475] | -0.24% |
| Fresh CLI noop | 1.664 [1.637,1.737] | 1.573 [1.538,1.585] | -5.47% |
| Fresh CLI body | 3.487 [3.423,3.655] | 3.335 [3.334,3.345] | -4.37% |
| Fresh CLI structural | 3.463 [3.431,3.633] | 3.359 [3.352,3.396] | -2.99% |
| Watch initial | 9.051 [8.942,9.081] | 8.974 [8.915,9.275] | -0.85% |
| Watch delta | 2.223 [2.213,2.232] | 2.247 [2.190,2.304] | +1.06% |

Fresh CLI timings include process startup and completion through all producer ACKs. First-batch, broker-End, consumer-End, RSS, parsed-file, scope and traffic spreads are retained in the comparison JSON. Initial parses4034, no-op0, body11, structural12 are unchanged; scopes8483/none/11/12 are unchanged. Candidate body/structural wall ratios to its initial are39.64%/39.93%. Watch includes a500ms collection window and uses observed quiescence, not a watcher watermark.

No initial or watch speedup is established: ranges overlap, and watch delta median is1.06% higher with a wider candidate range. CLI no-op/body/structural improve in all paired wall comparisons. Structural RSS median rises604,864,512→648,331,264bytes; other medians fall. Memory ranges and allocator variability must be considered before interpreting this as retained-memory growth. This candidate removes redundant traversal but does not achieve near-zero fresh no-op.

Integration remains pending FSM/cache322, reconciliation, cache323 coverage and final checks. These measurements apply only to the frozen49b486b-based candidate, not the eventual rebased integration build.

Structural RSS ranges do not overlap in this three-pair series: baseline583,532,544–618,348,544bytes, candidate642,826,240–696,909,824bytes. Treat this as an unresolved memory regression signal, not dismissible noise. Before accepting integration, investigate allocation/peak-live-heap behavior and repeat an appropriately controlled structural case. Darwin RSS does not establish retained Go heap growth, but it remains a measured cost.

## Memory investigation
One instrumented structural run per arm passed all correctness gates. RSS653,770,752→661,651,456bytes (+1.21%); peak footprint633,719,936→612,535,488bytes (−3.34%). GC cycles23→24. These differently sampled heap snapshots cannot establish retained-memory growth or compare total end-of-process allocations. Profile timing is ineligible.

A diagnostic Go test overlay compared the frozen baseline oracle and fused config traversal on clean Product under explicit exclusions and asserted equal outputs. Three repeats of5iterations each: oracle10,903,046–10,907,675B/op and112,914–112,935allocs/op; fused7,894,400–7,896,462B/op and76,654–76,688allocs/op. Thus the modified traversal allocates about27.6% fewer bytes and32.1% fewer objects per call. This does not prove the cause of the end-to-end RSS increase; it narrows investigation away from a direct large new traversal allocation. The overlay replaces a test file only in the diagnostic build; frozen source and production binaries are unchanged. Initial attempts with an unresolved symlink overlay key ran no benchmark and are not evidence. See `stage16-memory/` artifacts. An uninstrumented controlled repeat remains needed for the RSS signal.

## Integration and remaining work

The agreed order is FSM/cache322 first, then Stage16/cache323 with its own cache coverage. Reconcile the frozen patch against the actual FSM landing, repeat affected correctness/performance checks, and use normal commit/push hooks. Stage16 production changes have not been published.

Before integration, repeat the uninstrumented structural case under a confirmed quiet hold to resolve the RSS signal. The broader goal remains incomplete: fresh no-op is still about1.57s, initial is unchanged and watch has no demonstrated improvement. Follow-up state/indices reuse work is described in `NEXT_STARTUP_INVESTIGATION.md`; it is a candidate plan, not an implemented optimization.

Historical inputs include three manifest transitions, two adding declared dependencies; there are no tsconfig changes in this chain. Tsconfig mutations are covered by differential tests rather than this historical sample. `stage16-history-input-audit.json` records the actual changed inputs.


## Additional uninstrumented RSS series (partial)

The follow-up window yielded five accepted CLI arms, each with all correctness
checks passing and no sampled competing tests/builds/Enola processes. Repeats 4
and 5 are complete alternating pairs; candidate repeat 6 is unpaired. The driver
retained its original 21:20 UTC cutoff and refused baseline 6 when less than its
100-second reserve remained. It exited nonzero for that deadline guard, not for
a correctness failure. Both coordinators received END at 21:19 UTC. No unconfirmed
extension was used. Raw metrics, receipts, hold evidence and ambient container
samples are in `stage16-rss-followup/`.

| Structural delta | Baseline RSS, bytes | Candidate RSS, bytes | Candidate change | Baseline wall, s | Candidate wall, s |
|---|---:|---:|---:|---:|---:|
| Repeat 4 | 639877120 | 640385024 | +0.08% | 3.498 | 3.370 |
| Repeat 5 | 607862784 | 593657856 | -2.34% | 3.455 | 3.355 |
| Repeat 6 (unpaired) | unavailable | 677920768 | unavailable | unavailable | 3.536 |

The two complete follow-up pairs do not reproduce the original nonoverlapping
RSS increase. This weakens a claim of a consistent regression, but does not erase
the original three-pair signal or establish a cause. The unpaired high-RSS
candidate run is retained, not discarded. Do not pool this incomplete cohort
into the original medians or claim that memory acceptance is closed. Complete
paired follow-up and validation of the eventual integrated build remain pending.
