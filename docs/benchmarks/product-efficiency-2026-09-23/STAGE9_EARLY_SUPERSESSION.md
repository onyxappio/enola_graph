# Early refusal of superseded watch inputs

A resident transaction can capture a source, then start after that source has
already changed or disappeared. The existing final fence correctly refuses End
and preserves the completed generation, but only after publishing Begin and a
batch. The experimental patch adds a consumed-input check before Begin; it does
not remove or weaken the final checks.

Independent hook-free tests capture bytes through resident.contentInputs, then
overwrite or remove the source before transaction entry. Both fail on the claimed-v2
baseline solely because two records were published. With the frozen candidate,
both pass: zero records, retryable error, unchanged generation and a retry whose
graph equals cold analysis. Existing late-mutation and delivery/recovery guards
also pass (focused package run: 14.038 s). Full repository tests failed: TestConfigChangeRestoreUsesCapturedBytes now
refuses before Begin when a custom extractor temporarily changes configuration.
The graphsession package ran for 622.912 s; other packages passed. This existing
behavior requires resolution before integration.

The candidate deduplicates captured-source and preview-record checks. On the
worker-owned 1,201-file fixture, initial checks read 2 inputs, no-op checks read 0,
a one-file edit reads 3 and a 200-file edit reads 202. These counts bound reads;
they do not establish wall-clock overhead on the shared, loaded host. Repeated
timing comparisons are inconclusive. A superseded write reverted before the
final fence can now cause an additional safe retry; this conservative behavioral
difference is covered by the worker ABA regression.

## First Product NATS watch run

| Measurement | Candidate |
|---|---:|
| Launch to initial consumer application | 12.896 s |
| Last observed save to final cold-equal graph | 5.234 s |
| Abandoned Begins | 0 |
| Completed burst replacements | 1 |
| Burst owner scope | 11 |
| Idle / identical-save events | 0 / 0 |

Exact final cold equality and input stability passed. This is one short scripted
burst at a 500 ms collection window, not long-running concurrent-agent acceptance
or evidence of a speedup. Zero abandoned Begins does not mean zero silent retries.
The 5.234 s convergence still requires investigation and retry-cost reduction.

Experimental provenance: /tmp/enola-stage9-supersession-review/provenance.json.
Frozen candidate: /tmp/enola-stage9-supersession-patch. Production is not yet
integrated or pushed. The claimed-v2 history validation is a separate run and
must not be represented as coverage of this additional patch.

## Instrumented Product follow-up

[Profile receipt](stage9-early-supersession-profile.json): exact cold equality and
input stability passed. The early check read 59 inputs on initial (2 ms), and
70 inputs on the 11-file delta (3 ms). Thus the tiny-fixture counts do not predict
Product read counts. This single instrumented sample recorded no superseded
attempt and no failed transaction, so it does not explain the earlier slow tail.

Delta session time was 2.379 s, including a frozen preview of 0.887 s, dirty-scope
work of 0.228 s, grouping of 0.156 s and pending-state write of 0.291 s. The latter
includes 0.198 s to serialize 55.7 MB; nested timings must not be added twice.
The observed last-save-to-final-graph time was 2.166 s.

## Corrected candidate: validation in progress

The v2 candidate restricts the early fence to `AuthoritativeFiles`, preserving
the legacy captured-config behavior without changing the final fence. Independent
focused tests passed in 9.302 s, including the unchanged config regression and
the captured-source overwrite/deletion guards. Its final source hashes match
the worker freeze in `/tmp/enola-stage9-supersession-v2-patch`.

A separate retry-cache candidate preserves parsed records from an input-change
refusal only while source, side-read, configuration, policy, per-file resolution
context and inventory-name identity remain valid. Independent focused tests
passed in 10.250 s. The alias-retarget and newly shadowing module regressions
both passed; a 50-file fixture reused 49 records and retained exact cold equality.
Those parse counts are fixture evidence, not a Product speedup measurement.

The combined candidate completed full repository tests in
`/tmp/enola-stage9-retry-v3-root-suite`. Graphsession passed in 592.035 s.
The command exited 1 solely because two engine hook-environment tests require
Git metadata absent from the archive (`git rev-parse --absolute-git-dir` failed).
The entire engine package then passed from the Git worktree with the same
source overlay in 27.483 s (exit 0). All packages passed across the two runs;
the original full invocation remains recorded as exit 1. Product performance
validation and integration remain pending.
Its experimental binary SHA-256 is
`61cd08cf9ddf7482a13e288f941134ceb076af26914d2cb15bc36a60cb4442fe`.
The Product measurements above describe the earlier candidate and must not be
attributed to this combined build. Both delivered patch files use `-p0` from
the repository root; the retry manifest's `-p1` instruction for its base patch
is a receipt typo, confirmed against the actual patch headers.

## Combined candidate: repeated Product watch comparison

Three alternating runs per arm used local NATS and a 500 ms collection window.
[Full receipts](stage9-retry-v3-watch-results.json) and
[summary](stage9-retry-v3-watch-summary.json) preserve all samples.

| Seconds | Main 6090161 | Combined candidate |
|---|---:|---:|
| Initial median | 11.534 | 9.401 |
| Initial min–max | 8.818–16.806 | 8.973–18.454 |
| Last-save convergence median | 2.184 | 2.339 |
| Last-save convergence min–max | 2.183–2.197 | 2.168–4.769 |

All six runs passed exact final cold equality, frozen protocol checks, idle
silence and identical-save silence. Initial and final graph hashes agree across
all runs. Each completed delta parsed 11 files and published scope 11. None
announced an abandoned Begin, which does not rule out silent refused attempts.
The candidate slow sample has no diagnostic trace, so its cause remains unknown.
This series does not establish a speedup; candidate integration remains pending
that investigation. Startup spread overlaps heavily on the shared host.

These are short scripted bursts, not long-duration concurrent-agent acceptance.
The common source is Product a609c19f plus the prior untracked diagnostic fixture
and scope configuration listed in the receipts; these timings must not be
compared directly with the clean-commit history series.

## Slow retry reproduced with profiling

The [instrumented follow-up](stage9-retry-v3-profile.json) stopped after its second
run reproduced a slow retry. The first run converged in 2.173 s without refusal;
the second converged in 4.220 s and remained exactly cold-equal. The latter
refused before Begin at about 0.243 s after the last observed save. The next
transaction entered at about 1.010 s after the configured 500 ms window and
state read/decode (about 0.269 s). It rebuilt policy (0.491 s), collected runtime
inputs (0.619 s), and ran the frozen preview (0.835 s). These explain substantial
retry overhead; the preview still parsed 1 + 10 files. The exact cache rejection
reason requires further diagnosis, so side-read changes are not asserted as
the cause.

The successful early fence cost 2 ms. Final record verification read 4,087 files
in 0.159 s; pending state write cost 0.325 s including a 0.202 s marshal of
55.7 MB. Nested timings must not be added twice. Wall-clock observation was
sampled every 10 ms and is approximate. This identifies a retry mechanism;
it does not prove a speedup against an equally timed control retry.

## Disjoint multi-file retry: measured benefit

The [controlled harness](controlled_retry_watch.py) changes three independent
Product sources plus password.ts, then supersedes password.ts once after the
first post-edit frozen-preview trace. Both arms use the production CLI and NATS,
with profiling enabled and a 500 ms collection window. Timing is deliberate;
this is mechanism validation rather than natural editor latency.

[All six receipts](stage9-disjoint-retry-watch.json) retain injection timestamps
and binary provenance. Three alternating repeats per arm gave:

| Measurement | Main 6090161 | Combined candidate |
|---|---:|---:|
| Retry convergence median | 4.380 s | 3.774 s |
| Min–max | 4.369–4.392 s | 3.757–3.787 s |
| Successful retry parsed files, every run | 34 | 12 |
| Frozen owner scope, every run | 34 | 34 |
| Abandoned Begins per run | 1 | 0 |

Median latency fell 13.83% for this scenario. All runs passed exact cold equality,
protocol checks and idle/identical-save silence; initial and final graph hashes
also match across arms. The candidate combines early refusal and parse reuse, so
the wall-time saving cannot be attributed entirely to either mechanism.

The original one-file burst cannot preserve these independent parses: its changed
password.ts invalidates its own record and the side-read hashes of all ten
dependents. This distinction explains why the earlier series did not show the
same benefit.

These binaries predate concurrent accuracy main 96e4416. Production integration
requires composing the patches with those accuracy changes and validating the
new combination; these receipts do not establish that acceptance.

## Integration with accuracy main

The frozen early-v2 and retry-v3 patches were composed incrementally onto
`6a8581f`, retaining the intervening accuracy changes. The targeted framework,
retry-context and checkpoint-corruption tests passed in 20.366 seconds. The full
repository suite then passed with exit 0 in 488.741 seconds, using a Go overlay
from the real Git checkout. The production files subsequently copied into the
working tree match the tested source hashes exactly.

[Validation receipt](stage9-retry-current-main-validation.json) and
[full suite output](stage9-retry-current-main-full-suite.log) preserve that evidence.
The completed Product comparisons on this base are reported below. The separate
committed-state decode-reuse change is not part of this full-suite result or
these two binaries; its validation is reported separately.

## Three-arm Product comparison on current accuracy main

[All nine receipts](stage9-current-main-product-results.json) and
[summary with state trace marks](stage9-current-main-product-summary.json) compare
the same Product input and base `6a8581f`, with three alternating repeats per arm,
profiling enabled and a 500 ms watch window. This is the controlled disjoint retry
scenario described above, not a natural editing or fresh CLI benchmark.

| Measurement | Control | Early refusal + parse reuse | Plus state decode reuse |
|---|---:|---:|---:|
| Retry median, s | 5.328 | 4.490 | 4.273 |
| Retry minimum, s | 5.230 | 4.377 | 4.253 |
| Retry maximum, s | 5.335 | 4.531 | 4.290 |
| Initial consumer-applied median, s | 8.884 | 8.988 | 9.149 |
| Successful retry parses, every run | 34 | 12 | 12 |
| Frozen Begin owners, every run | 34 | 34 | 34 |
| Abandoned Begins, every run | 1 | 0 | 0 |

The complete candidate reduces median retry convergence by 19.81%. Adding
state decode reuse to early refusal and parse reuse saves a further 0.217 s
in this scenario. Every run passes exact cold equality, protocol checks, input
stability, idle silence and identical-save silence. Initial and final graph
hashes match across all arms. The checkpoint trace confirms actual decode reuse
in all three checkpoint runs.

Initial medians increased from 8.884 s to 8.988 s and 9.149 s; ranges overlap
(8.782–9.926, 8.960–10.293, 8.926–10.390 s). This series establishes no initial
speedup. State hashing adds real work on reads and writes, and this small series
does not isolate its contribution to the initial difference. The host is shared.
The measured improvement is scoped to one controlled refusal with independent
parses available for reuse; repeated consecutive refusals and general fresh CLI
no-op latency remain separate work.
