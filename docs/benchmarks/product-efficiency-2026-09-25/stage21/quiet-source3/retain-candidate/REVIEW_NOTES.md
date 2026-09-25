# Option C: compare the retained bytes instead of digesting the file twice

Isolated candidate at `/tmp/enola-stage21-retain/src`, cloned from frozen source3
(`/tmp/enola-stage21-proof/src`). The freeze and the ablation trees are untouched.

## What it does, labelled honestly

On a deferral the committed state is read and digested once, at open. When the proof
is refused, `materializeDeferred` **reads the file again and compares the bytes** with
the buffer the open retained, then decodes that buffer and reuses the fingerprint
already proven to bind the proof.

- Removed: the second SHA-256 over 54MB.
- **Not removed: the second read.** The file is still read twice. A check that does
  not look at the file cannot see a writer that ignored the lock.
- Rewrite detection is not weakened. `bytes.Equal` catches everything the fingerprint
  comparison caught, including a same-length in-place edit with the timestamps put
  back, and needs no argument about collisions.
- Peak memory during the comparison is the state twice over: both readings are live
  while `bytes.Equal` runs. The comparison buffer is unreferenced at the return, so it
  is not held across the decode. Unreferenced is not reclaimed; RSS is a measured
  observation, not asserted anywhere in these tests.

## Buffer lifetime - every release point

| Where | Why |
|---|---|
| `openSeekValue` | strips `retained` from the accounting copy, so `r.openSeek` is not a second reference for the life of the resident |
| admitted proof, in `resolveDeferred` | an admitted proof never decodes |
| `materializeDeferred`, deferred, both outcomes | decoded bytes are spent; bytes whose file was rewritten are stale |
| `resolveDeferred`, any error return | fence, `validateEffective`, fresh-input and decode failures |
| `Resident.transaction`, **every** exit | covers the exits before `resolveDeferred` is reached - `effectiveConfig`, `RebuildGraphInputs`, the closed check. Stated once so a new exit added later cannot silently start retaining 54MB |
| `Close` | a resident opened and dropped without a transaction |

The proof and its fingerprint always survive a release. With no buffer, `decodeDeferred`
takes the original path - read, digest, compare fingerprints, decode. That is not a
weaker test and not a bypass; it is the comparison the mechanism shipped with, and it
costs one digest. This is the ordinary path for the second transaction on a resident
whose first transaction admitted the proof.

## Review items received during implementation, and their disposition

- **`defer w.fold(work)` folded a zero copy** (`stateReadWork.fold` has a value
  receiver). Root flagged it; found here at the same time via a failing counter
  assertion. Fixed with a closure. Verified: retained path 1 decode / 0 read-side
  digests; fallback path 1 decode / 1 read-side digest.
- **`recheckCommitted` comment claimed the two buffers are never both held.** Wrong,
  and corrected in the source: both are live during the comparison.
- **Transaction-level cleanup** was missing when first reviewed; it is the sixth row
  above now.
- **`HoldsOneBuffer` test name and its one-minute wall threshold**: renamed to
  `TestRecheckCommittedDoesNotTouchTheRetainedBuffer`, threshold removed. It asserts
  non-mutation of the caller's buffer and the three verdicts, and says in its comment
  that it establishes nothing about reachability or reclamation.
- **Same-length rewrite must stay valid JSON**: it does. One character of a hex digest
  is flipped, so the file still parses and still decodes - the test proves the check
  refused it, not that the decoder choked. The transaction-level case asserts the
  rewritten state still decodes before concluding anything.

## Tests

Command, run before the broad suite:

    go test ./internal/graphsession/ -count=1 -v -run 'TestDeferredDecodeRefusesEveryShapeOfRewrite|TestRetryAfterARefusedDecodeStillSeesTheRewrite|TestAdmittedProofReleasesTheBytesAndTheNextRefusalStillDecodes|TestCloseBeforeAnyTransactionReleasesTheRetainedState|TestRefusedDeferralDigestsTheStateOnce|TestTransactionRefusesARewrittenStateWithoutPublishing|TestFailureBeforeTheDecodeReleasesTheBytesAndRetryStillWorks|TestRecheckCommittedDoesNotTouchTheRetainedBuffer'

8 named tests, 11 cases with the subtests, all PASS in 4.172s. Log:
`/tmp/s21-retain-focused.log`.

1. `TestDeferredDecodeRefusesEveryShapeOfRewrite` - same length in place with
   timestamps restored / replaced / truncated / removed. Each asserts
   `ErrInputsChanged`, the right message, no state installed, no stale buffer kept.
2. `TestRetryAfterARefusedDecodeStillSeesTheRewrite` - the retry has no buffer and
   must still refuse; restoring the bytes then makes the same call succeed, at a cost
   of one decode and one digest.
3. `TestAdmittedProofReleasesTheBytesAndTheNextRefusalStillDecodes` - the two
   transaction life: no-op admits and releases, later edit refuses and decodes.
4. `TestCloseBeforeAnyTransactionReleasesTheRetainedState`.
5. `TestRefusedDeferralDigestsTheStateOnce` - the counter evidence.
6. `TestTransactionRefusesARewrittenStateWithoutPublishing` - whole transaction over a
   same-length rewrite: `ErrInputsChanged`, zero published records, generation on disk
   unmoved, state file not written, then restore and prove the retry does the work.
7. `TestFailureBeforeTheDecodeReleasesTheBytesAndRetryStillWorks` - unreadable
   repository, a failure before the decode; skips loudly rather than passing if the
   denial did not actually happen.
8. `TestRecheckCommittedDoesNotTouchTheRetainedBuffer`.

### The counter evidence, and a correction to how it was measured

`TestRefusedDeferralDigestsTheStateOnce` first captured the profile around the
transaction only, which cannot contain the open-time read. The capture now spans open
plus transaction, and read-side and write-side `state_fingerprint` marks are counted
separately - the commit's write digest is a real mark, named and asserted rather than
excluded. Over one refused session:

- read-side `state_fingerprint`: **1** (was 2)
- `state_bytes_compared`: 1
- `state_read_bytes` over `state.json`: **2** - unchanged, and asserted so
- `state_json_unmarshal`: 1
- write-side `state_fingerprint`: 1
- `WorkCounters.StateFingerprints`: 1, `StateDecodes`: 1

One thing is unexplained and is not being papered over: under the **old** capture
boundary the same assertion saw 2 transaction-internal `state_read_bytes` in a filtered
run and 1 in the full package run. The deterministic count for a transaction alone is
1, so the filtered run's 2 is the anomaly. The current boundary counts the whole
session and has been stable, but I have not found the cause of that difference and am
not claiming it is benign.

## Not claimed

No latency or RSS result. Nothing here says the change is worth integrating; that is
the profile that comes next, on the same scenarios plus RSS and no-op.
No proof-fsync change is in this candidate.
