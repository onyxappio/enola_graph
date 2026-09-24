# Literal-directory matching: policy cost and Product watch

A positive-only fast path in `GlobSet.MatchAny` avoids scanning the full pattern
list when a literal anchored `prefix/**` already proves a match. `Match` remains
unchanged, including which pattern wins. Every fast-path miss falls back to it;
paths are not normalized and the directory/sibling boundary is preserved.

This improves one input-policy stage. The ordinary Product watch measurements
below do **not** establish a general end-to-end speedup, and do not complete the
initial/delta/watch performance objective.

## Provenance and correctness

Both measured builds use Enola `5b0934c`, Go 1.27.1 and `-trimpath`. The candidate
is an overlay of one production file, SHA-256
`8e117aec68efaeee8413d347e343f0399ae31e8510e2850bb730c78643b46c95`.
[Build receipts](stage10-prefix-builds.json) identify both CLI and diagnostic
binaries; candidate VCS metadata alone does not identify the overlay. Subsequent
accuracy main `012dd29` is not the measured base.

The independent frozen-old-matcher oracle passes, including malformed globs,
raw dot segments, escapes, repeated separators and Unicode. A deliberate mutant
that accepts sibling `foobar` for `foo/**` fails this oracle. The Product policy
snapshot is byte-identical for 50,925 names classified both as files and as
directories, including identity and admission digests. Root package tests pass:
`facts` 2.873 s, `graphinput` 10.045 s, `engine` 25.964 s, `bootstrap` 76.732 s.
These are correctness runs, not performance comparisons.

A separate counter diagnostic resets immediately after `Build`, then recomputes
identities once. Full `Match` calls drop from 34,137 to 2,682, avoiding 31,455
calls (92.1%). Identities remain unchanged. This is an operation count, not a
92.1% speedup claim. The rejected ancestor-memo draft is absent from both arms.

## Policy construction

Three alternating pairs each perform 20 constructions on the same isolated
Product checkout with actual bootstrap options. CPU profiling and phase tracing
are enabled in both arms. The values below are medians within each 20-call run.

| Stage | Baseline run medians, seconds | Candidate run medians, seconds |
| --- | --- | --- |
| Compute identities | 0.1520, 0.1550, 0.1545 | 0.0385, 0.0385, 0.0370 |
| Entire policy/engine construction | 0.4587, 0.5385, 0.6128 | 0.4737, 0.4820, 0.5027 |

The identity-stage median falls 75.1%. Construction medians overlap and drift;
the overall median falls 10.5%, but the first pair is slower. These nested stages
must not be added together. No competing Go tests were detected by the per-run
checks and additional process snapshots; this does not establish an idle host.
See [all per-call samples](stage10-prefix-profile-results.json).

## Ordinary production watch

The existing `2026-09-22/watch.py` harness uses real CLI/NATS delivery, a 500 ms
fixed collection window and three closely spaced body edits to Product
`packages/crypto/src/password.ts`. Product is pinned to `a609c19f3861` and copied
into isolated trees. No production phase profiling is enabled. The same prebuilt
observer is used throughout. Setup/cloning and the subsequent cold comparison
are excluded from initial and delta timings, but included in harness wall time.

| Phase | Baseline seconds (three runs) | Candidate seconds (three runs) |
| --- | --- | --- |
| Initial to consumer apply | 17.478, 9.118, 8.761 | 10.667, 8.835, 8.724 |
| Last fsync-observed save to cold-equal consumer apply | 2.876, 2.625, 2.711 | 5.074, 2.609, 2.687 |

Initial medians are 9.118 and 8.835 s; delta medians are 2.711 and 2.687 s.
The candidate's 5.074 s delta is retained. In that first pair, save-to-Begin was
3.860 s versus 1.800 s for baseline; that localizes most extra elapsed time
before Begin but does not establish its cause. Do not attribute the outlier to
the optimization or dismiss it as noise without a phase trace.

All six accepted runs parse 11 files for delta, publish one 11-owner replacement,
match cold exactly, keep the checkout stable during cold validation, and observe
zero idle/identical-save events. Initial hashes agree across all arms, as do
final hashes. Batch counts match and no abandoned Begins are observed.
Finite quiet windows and cold equality do not prove the internal watcher queue
is drained, nor establish long-running coding-agent acceptance.

A seventh run (baseline repetition 3, first attempt) is retained but excluded
from timing: two-second process monitoring detected unrelated cachecov,
docslint and golden/determinism pre-push tests. It passed correctness. Both arms
of the final pair were then run again, with no detected competing tests.
Monitoring detects test processes, not all host contention. The full
[seven-run receipt](stage10-prefix-watch-pairs.json) preserves the excluded run,
raw telemetry, clock definitions, hashes and contention samples.

## Integration validation

The implementation was applied to accuracy main `012dd29`. Its executable text
is identical to the measured overlay; comments were shortened. Three permanent
regressions cover boolean equivalence, first-pattern order and directory/sibling
boundaries across a 2,592-case corpus. No other production file changed.

The full `/opt/homebrew/bin/go test ./... -count=1 -timeout=20m` run passed:
110 packages, exit 0, 422.513 s wall. Root independently checked the exit receipt,
log and unchanged production/test source hashes. See
[the integration receipt](stage10-prefix-integration.json) and
[full log](stage10-prefix-full-suite.log). This proves regression coverage on the
combined accuracy main; it does not relabel the earlier timings as measurements
of that newer base.

Diagnostic sources and drivers are archived beside this report with the
`stage10-prefix-` name prefix (`.go.txt` for Go overlays). Their original
working directory was `/tmp/enola-stage10-prefix-review`; restore original
names there or adapt the paths before reproducing. The frozen candidate source
and Product options/snapshots are included. Raw stdout/stderr and CPU profiles
remain in that working directory.
