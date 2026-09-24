# Stage15 body-delta profile decomposition - prepared, NOT run

Gates revised twice after root review: binary and observer digests
are now asserted rather than only recorded, a completed-sequence flag plus a
required-gate-presence check replace the old all(checks) test that a partial run
could satisfy, contamination and non-restoration now exit nonzero instead of only
clearing a flag, the quiet-slot grant id is required and recorded, and failed
runs keep every artifact. Second round: state_json_marshal is required on the
body delta only, the summarizer requires the exact six-run set with valid equal
hashes and both cold oracles, repeated mark names are preserved raw instead of
being overwritten by a dict comprehension, and no mark durations or median
differences are summed into an attribution number. gate-tests.py exercises all of
this on synthetic receipts with no workload. Third round: the interpretation
section now states that the state marks omit the write path IO, verified in
persist.go, so a small mark gap can never be reported as excluding state
persistence.

Purpose: decide whether the fresh CLI body regression (+0.071s median, the one
scenario with non-overlapping ranges) sits in state serialization or outside it.
Nothing here changes production source, rebuilds anything, or touches a frozen
tree. Existing binaries only. Not started; waiting on root.

## Inputs, all read-only

| Thing | Path | Digest |
|---|---|---|
| baseline binary | /tmp/enola-stage15-independent/enola-wave14-baseline | 5f5850dab6131aef717bf0f4439efb063201365d8b894858796c0d3913239801 |
| candidate binary | /tmp/enola-stage15-independent/enola-stage15-wave14-candidate | 320d12a90c39eaa34e97fb1edbad6e4d4890746bd9156a557a007f43cc058bb6 |
| Product fixture | /tmp/enola-stage9-md-timing/product | product_sha a609c19f3861971930fae7b33dcb2950598953c5 |
| observer | /tmp/enola-stage9-product-watch-control/bin/benchobserver | 9d4322e7fdeb7068b049ba0ded77aa043ada59396162c9d1286b4e29c3c4bd5f |
| scope overlay | docs/benchmarks/product-efficiency-2026-09-22/product-graph-scope.yaml | same overlay as the measured series |
| nats-server | /tmp/enola-toolchain/bin/nats-server | own port and config per run |

Control file, pinned both ways and verified before any run proceeds:

- packages/crypto/src/password.ts original sha256 b940122ddebf67b8f46073ba98a5bb3e1c82d80a9a28ac1a48cc680b5c0f80c3
- after the one-line body edit sha256 b20431065c89e899cc2a17f1e7290dfe36f0d46224848c7e6f6a00af60cee971
- edit: return email.trim().toLowerCase(); becomes return email.normalize('NFKC').trim().toLowerCase();

The fixture is cloned with cp -cR into each run directory; the edit happens only
in that clone and is restored in a finally block. The fixture itself is never
written.

## What each run does

Body only. The no-op is kept because the measured body delta ran after one, so
dropping it would change the generation the body delta starts from; profiling it
costs 1.7s and doubles as the zero-parse control.

1. initial - graph analyze, generation 0 to 1, NOT profiled
2. noop - graph delta, generation stable, PROFILED
3. body - graph delta after the pinned edit, PROFILED
4. cold-body - graph analyze in a fresh state dir, NOT profiled, only on repeat 1 of each arm

Same initial state per arm means the same procedure and the same pinned input,
not the same bytes: each arm must build its own state, because the two binaries
write different cache versions (v320, v321) and different record fields. State
bytes are recorded per arm rather than assumed equal.

Acceptance is all-or-nothing and every gate is recorded in receipt.json under a
stable key. A run is timing eligible only when the sequence completed, every
required gate ran AND passed, every required mark is present, no competing
workload was seen, and the control file was restored to the pinned original.
Anything else exits nonzero.

Required gate keys, checked for presence as well as verdict, so an exception
partway through can never leave a green receipt:

- quiet-slot: --quiet-message-id is required and must look like a coordinator
  message id; it is recorded in the receipt so every run names the grant it ran under
- binary-pin: the arm binary sha256 equals its published digest
- observer-pin: benchobserver sha256 equals its published digest
- fixture-pin: the clone is at product_sha a609c19f... with no tracked edits
- control-pin: control file matches the pinned original AND the pinned edited revision
- initial-fresh, initial-parses: generation 0 to 1, 4035 files parsed
- noop-silent: zero parses, generation held, zero wire messages
- body-scenario: 11 files parsed and 11 owners published
- marks-present: per label. Both profiled runs must emit state_read_bytes,
  state_fingerprint, state_json_unmarshal and graph_session_run; the body delta
  must also emit state_json_marshal, the main diagnostic there, while the no-op is
  not required to write state. A missing mark fails the run rather than silently
  shrinking the decomposition. Occurrence counts per mark are recorded too
- hash-gate: the run carries --oracle or --expect-hash, plus oracle-match and/or
  expect-hash-match for the verdict itself
- no-competitors: no competing enola/go/test process in the 1s samples

On any failure the run keeps everything it wrote: clone, state dirs, logs, marks,
metrics and the receipt, with the failing gate and the traceback tail recorded.
Failed evidence is the finding. Cleanup of the clone and state dirs happens only
on a fully eligible run, and never touches logs, marks or receipts.

summarize.py refuses to write an aggregate unless the series is exactly right:
receipts for baseline-1..3 and candidate-1..3 all present and all eligible, no
unexpected run dirs, both repeat-1 cold oracle hashes present as 64 hex with a
passing oracle-match gate, every body hash 64 hex and identical across all six
runs and equal to its own arm oracle, and a well formed quiet_message_id on every
run. Otherwise it writes the problem list, prints it and exits 1. Ineligible runs
never feed an aggregate.

Repeated mark names are preserved, not collapsed. A mark that occurs more than
once in any run is reported raw, with each occurrence's duration, order, trace and
detail, and is excluded from the difference rather than summed: graph-profile does
not report nesting, so summing occurrences could double count. Marks absent from
some runs are likewise kept raw and reported separately.

## Behavioral gate tests

    python3 -B gate-tests.py      # add --keep to inspect the synthetic trees

54 assertions over fabricated receipts, metrics and mark files in temp trees: no
binary, broker, fixture or state dir is touched. They cover the per-label required
marks, all eight eligibility outcomes including truncated gate lists and
unrestored control files, acceptance of a complete series, refusal of a missing,
ineligible, extra, null-hash, short-hash, mismatched-hash, bad-quiet-id or
oracle-less series, preservation of a repeated mark with both durations and traces
intact, and refusal of a run whose body lost a required mark. Last run: 54 passed,
0 failed, exit 0.

## Commands, in this order

    cd /tmp/enola-stage15-profile-plan
    Q=<coordinator message id granting the slot>
    B=<baseline-1 body_hash, from its receipt>
    python3 -B run-profile-arm.py --arm baseline  --repeat 1 --quiet-message-id $Q --oracle
    python3 -B run-profile-arm.py --arm candidate --repeat 1 --quiet-message-id $Q --oracle --expect-hash $B
    python3 -B run-profile-arm.py --arm candidate --repeat 2 --quiet-message-id $Q --expect-hash $B
    python3 -B run-profile-arm.py --arm baseline  --repeat 2 --quiet-message-id $Q --expect-hash $B
    python3 -B run-profile-arm.py --arm baseline  --repeat 3 --quiet-message-id $Q --expect-hash $B
    python3 -B run-profile-arm.py --arm candidate --repeat 3 --quiet-message-id $Q --expect-hash $B
    python3 -B summarize.py

Order alternates and reverses in the middle pair so a monotonic host drift
cannot favour one arm. Each invocation is independent: a failed run can be
deleted and repeated without disturbing the others.

## Cost

- per run without oracle: about 18s (clone 2, brokers 1, initial 8.4, noop 1.7, body 3.5, teardown 1)
- per run with oracle: about 27s
- six runs plus summarize: about 3 minutes of host time
- disk: each run holds one state dir of about 56MB, plus 56MB more on the two
  oracle runs; clone is APFS copy-on-write. The clone and state dirs are deleted
  at the end of a fully eligible run (unless --keep), leaving a few MB of logs,
  marks and receipts; a failed run keeps its clone and states as evidence. Peak
  under 200MB with sequential runs, or about 320MB if one run fails and is kept.

## Output and how it is read

summarize.py writes profile-comparison.json with, for noop and for body: per-run
wall/user/sys/state bytes for all six runs plus min, max, median and range per
arm; per-mark medians with their own spread; the candidate-minus-baseline
difference for each mark that occurs exactly once in every run; the four state
marks called out individually; whether the wall ranges overlap; and the marks that
were not differenced, with their raw occurrences.

No number in that file adds mark durations or adds median differences together.
graph-profile reports no nesting and graph_session_run looks like a container, so
a sum could double count; each state mark is compared to the wall gap on its own,
and attribution is a separate question from this decomposition.

- If one or more state marks carry most of the body wall gap, the cost is the larger
  persisted state, and the recording arm in session.go is what to look at.
- If the state marks are a few ms while the gap remains tens of ms, the cost is
  outside the MEASURED STATE MARKS. That is not the same as outside state
  serialization or persistence, and must never be written as such: on the write
  path the marks do not cover the file IO at all (see below), so a larger state can
  still be paying write and fsync cost that no mark reports. graphsession also has
  no mark inside the invalidation pass. That case narrows the location to a set that
  still contains state write IO, so it names no function; the unconditional
  sideReaders build is one candidate among those and would need its own instrumented
  evidence before any code change.
- If neither holds and the gap does not reproduce under instrumentation, that is
  also an answer: the unprofiled gap would then need re-establishing before it
  justifies work.

### What the four state marks cover, and what they do not

Verified by reading internal/graphsession/persist.go writePendingStateFP, not
assumed from mark names:

- line 14 to 19: tJSON to state_json_marshal covers json.Marshal only.
- lines 20 to 45: os.Create, f.Write(b), f.Sync, f.Close, os.Rename and fsyncDir
  are covered by NO mark. On this fixture b is about 56MB and about 775KB larger in
  the candidate, so this unmarked window is exactly where a bigger state would pay
  write and fsync cost.
- line 49 to 51: tFP to state_fingerprint covers fingerprintStateBytes(b) only,
  starting after fsyncDir. It scales with bytes but is not the IO.

So the four marks cover read, decode, marshal and the write-side digest. They do
not cover persisting the bytes. A small four-mark gap therefore does not exclude
state IO, and no conclusion here may be phrased as excluding state serialization or
persistence as a whole. The raw marks are enough to say this correctly; no extra
harness feature is needed.

Instrumentation inflates both arms. These numbers decompose the gap; they do not
restate it, and they must not be published as the regression figure.
