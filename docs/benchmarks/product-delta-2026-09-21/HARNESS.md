# Incremental acceptance harnesses

These harnesses are development artifacts. The resident driver is prepared for
the in-progress resident API; it is not evidence of a passed benchmark until
its compiled source, logs and assertions are archived with a measured candidate.

## Real main history

`multifile.py` accepts explicit `--base` and `--target` commits, requires a clean
tracked checkout, checks ancestry, and restores its original symbolic branch or detached revision
in `finally`. Use a dedicated benchmark clone. Each iteration completes baseline
initial, no-op and target delta before the observer switches contexts. The final
target graph is compared with a separate cold target analysis.

Pinned Product scenarios:

| Scenario | Base | Target | Changed paths |
| --- | --- | --- | ---: |
| Two main merges | a6f1f3a91a36ea3dead786560412a4944a009694 | a609c19f3861971930fae7b33dcb2950598953c5 | 27 |
| Five main merges | 599575d0aa398615cde6a4ac12bc0c7734a686d9 | a609c19f3861971930fae7b33dcb2950598953c5 | 100 |

Both passed single-run correctness checks on the setup-optimized candidate. Those
checks ran concurrently with development and are not performance measurements.
The 100-path history includes manifest/config changes; its broad fallback is a
separate result, not a representative local source delta.

## Resident and real watcher

Copy `resident-driver.go.txt` into the snapshot-only cmd/benchresident/main.go in a private source
snapshot and build that command. `resident.py` accepts the CLI with `--binary`,
the helper with `--resident`, and `--source-mode watch` or `queue`. The latter is
an explicitly cooperating-writer change feed; it does not prove OS watcher
coverage. Real watcher mode waits for observed paths or a reconciliation signal.

For each of three iterations, the harness starts a new resident lifetime and
measures bootstrap, ten idle requests, a body edit and a structural edit. File
mutation precedes the request timestamp recorded inside the driver; the Python
wall time starts before the actual write and includes notification/request and
response overhead. Each changed generation is matched to the independent NATS
observer; broker End, consumer completion and producer completion stay separate.
Lifetime peak RSS is recorded by `/usr/bin/time -l`, not attributed to individual
requests. Idle assertions require all Work counters to be zero, no parsing,
no broker messages and no generation change.

Initial and changed graphs are compared with separate strict cold CLI analyses
under the same profile. An optional `--initial-hash` additionally checks a pinned
reference for that exact profile. Historical lock-aware references must not be
silently reused after the user-requested ignore-lock policy changes graph facts.
Record intentional semantic changes and corresponding regression coverage.

The existing observer retains one active context. Finish a context's complete
sequence before starting another; returning to an evicted context without
reconstructing its baseline is invalid. Start and stop owned broker/observer
processes explicitly. Do not mix diagnostic post-run profiles with acceptance
metrics, and run final timing with no concurrent workers, builds or tests.


## Review hardening in progress

The resident smoke predates the harness review fixes. Acceptance scripts now reject
reused analysis state/output, require a positive repeat count, match consumer run IDs,
require the actual expected filesystem event without fallback for local-edit probes,
and aggregate coverage catch-up work. SIGTERM/SIGINT enter cleanup; subprocess groups
are reaped and original contents/branch restored. Broker/observer lifetime remains
owned by the launcher, which must stop/reap both after the script exits.

Use `--scope-config` for a YAML fragment without `repo`, keeping identical policy
across resident, strict cold comparisons and history scenarios. The CLI candidate
must support `graph --summary-json` (single-repository JSON array without Facts).
Legacy `--old`, `--cold`, and `--initial-only` flags are not accepted by resident;
the CLI and multifile harnesses implement the legacy comparison below.

Native resident measurements call ApplyChanges in a request-driven harness using
real fsnotify batches; they do not include production Watch debounce latency.
They prove observed-watermark processing, not instantaneous filesystem equality.
The external configuration resides in a dedicated `config/` directory, separate
from metrics and logs. Its parent is watched for config replacement; writing
benchmark artifacts there would invalidate a raw native-event quiet interval.

Ignored-event delivery must be explicitly observed; a sleep followed by an empty
queue is insufficient. Excluded subtrees with no registered watches require separate
policy/registration tests, not a claim that their absent events were observed.


`resident_history.py` keeps one resident alive across a Git checkout from `--base`
to `--target`; it reports apply time and checkout time separately, accepts explicitly
reported membership/config reconciliation and compares the transported target graph
with a fresh target analysis. It restores the original symbolic branch or detached
HEAD. This is separate from `multifile.py`, which measures strict CLI invocations.

The final resident helper uses `NewGraphEngine` and `NewGraphFileChangeSource`,
matching production input policy before watcher registration. For filtered lock/media
probes it records `ObservedPath` before mutation, waits for a strictly newer native
path sequence, then verifies zero semantic work/events. This proves delivery of that
specific event; the bounded diagnostic cache is not a filesystem-wide barrier.


## Legacy comparison and selected-input mirror

`cli.py` measures fresh CLI initial/no-change/body/structural operations. With
`--old PINNED_BINARY --scope-tool BENCHSCOPE`, it runs the unchanged legacy binary
on a private physical mirror outside the candidate checkout. `multifile.py --old`
also requires `--scope-tool` and uses that mirror. Build `benchscope.go.txt` as a
snapshot-only command in the same source snapshot as the candidate. Its new
`--export-selected` mode exports the candidate policy's selected repository files,
name-only inputs and directories, byte hashes, and candidate main/name inventories.
Selected symlinks and nonregular files fail explicitly; they are not followed.

Before every legacy initial or changed operation, the harness exports the current
candidate configuration/revision, copies selected bytes into the mirror, removes
obsolete previously selected inputs, and verifies its physical file/hash manifest.
This work is outside the measured interval. Excluded tests, lockfiles and other
excluded side-input files are physically absent, rather than merely ignored by
legacy extraction. Name-only media retain their actual bytes. Copies are independent
files, not hardlinks. The candidate continues to analyze the original checkout and
pays its full-tree policy/Git discovery cost; the legacy mirror avoids that excluded
tree traversal and has no Git metadata. This asymmetry is explicit and must be
reported with timings, not described as identical total workload.

Each repetition uses a fresh `.enola/old-output-N` directory inside the owned mirror.
Previously created output is preserved, and an existing initial output is rejected.
The mirror itself is retained under the run output as evidence; no original Product
cache or source is removed by the mirror helper. Per-operation `*-selected-inputs.json`
records selected classes, byte digests, physical manifest digest and candidate
inventory. `*-inventory-evidence.json` records the post-run physical inventory and,
when available, legacy snapshot file hashes. Legacy `snapshot.meta.json` and
`receipt.json` are archived before the next operation overwrites them. When snapshot
metadata exists, its main file/hash inventory must equal the candidate main inventory.
This does not instrument every legacy reference parse or independent/external side
read: metadata covers the main walked files only.

These are **selected repository-input mirror comparisons**, not a claim that every
legacy consumer reads exactly the same inputs or emits equivalent facts. Ancestor or
external references, legacy detector/side-read behavior, VCS-dependent facts and
transitive propagation remain qualifications; manifests keep declared dependencies,
while excluded lock bytes are unavailable in the mirror. Resolve any active external
input dependency and audit old consumers before claiming strict same-scope acceptance.
Candidate correctness always uses the separate same-profile cold candidate oracle.

For compatibility, `cli.py --old-config CONFIG_JSON` without `--scope-tool` still
runs the old binary on the original checkout using translated ignores. Its provenance
explicitly labels **approximate scope**: ignored reference tests and independent side
reads can differ, and these results do not satisfy same-scope acceptance. When both
flags are provided, the generated mirror configuration is authoritative; old-config
is retained only as provenance. The local suite runner must pass `--scope-tool` to
both CLI cases for mirror mode. Old helper binaries without `--export-selected` fail
rather than silently reverting to ignore translation.

## Timing, cleanup and bounded validation

Timed subprocesses use a blocking wait and an independent timeout watchdog, avoiding
POSIX timeout-polling quantization. `seconds` still includes parent setup, process
launch and response observation; `process_real_s` additionally preserves the rounded
`/usr/bin/time -l` process real time. Broker/consumer observation times remain distinct.
Resident console summaries use aggregate parses, reconciliations and fallback reasons
including coverage catch-up, matching the retained JSON. Resident cleanup attempts
process shutdown and every content restore; any failure raises and fails acceptance.

Run `harness-probes.py --scope-tool BUILT_BENCHSCOPE --old PINNED_BINARY` for bounded
scratch fixtures only. It injects cleanup failures into the actual resident finally
block, checks selected scope including excluded tests and tracked Git exemptions,
checks copying/history/cache preservation and unsupported symlink rejection, checks
legacy main-inventory evidence, and exercises blocking wait/timeout cleanup. No Product
clone is touched. Report medians/ranges and delta-to-initial ratios separately; these
scripts retain measurements and correctness checks but do not enforce a performance
threshold by themselves.

Native single-file runs with --probe-ignored also sample total delivered native events over a 300 ms idle window after a 200 ms settling allowance. Equality checks for a metadata-event feedback loop independently of graph-queue zero work. These sleeps are outside timed ApplyChanges requests and do not establish a universal filesystem barrier.
