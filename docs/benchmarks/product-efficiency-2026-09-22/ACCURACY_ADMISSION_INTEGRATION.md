# Accuracy and performance integration — 2026-09-23

Candidate: `a257dd6fcc720029fcde13beb7a03a2382eb41e9`.

This candidate merges accuracy main `ba3ec15` with manifest-scope and measured-report
history through `d6fe7b0`, then applies the admission-identity no-op correction.
The merge preserves extractor cache version **v291**. The preceding v274 timings
in this directory describe their pinned builds, not this merged candidate.

## Behavior

Git staging or untracking an unchanged, nonignored, already-admitted file no
longer changes the semantic policy fingerprint. The raw identity remains tracked
for bookkeeping and declared-input fences. Ignored files becoming tracked still
change admission and reconcile. Real file membership changes still use inventory
and dependency planning. Missing admission fingerprints migrate conservatively
once; this must not be confused with an unchanged same-build startup replacement.

Begin remains frozen before publication. Post-Begin index mutations, including
tracking an ignored file, fail without successful End or completed-state advance.
The configuration fingerprint uses the admission identity; changing only the
publication gates would not have fixed staging-triggered replacements.

## Validation checkpoint

- `go test ./...`: PASS, exit 0, **276.16 s** wall; graphsession 247.111 s,
  bootstrap 160.590 s. Shared host; these are test durations, not graph timings.
- `go build -o /tmp/enola-perf-accuracy-integration-bin ./cmd/enola`: PASS,
  **3.34 s** wall.
- Binary SHA-256: `5e0b3a258ce523aa791ec36087fb524687885ac6bf2f94ccc7dbea63948f791e`.
- Compiled production patch was byte-compared against the committed candidate;
  only the previously untracked regression test is excluded from that comparison.
- Ten-transition Product cold/delta and frozen-Begin validation: **10/10 PASS**.
  See ../invalidation-history-2026-09-22/HISTORY_ACCURACY_ADMISSION_1.md. The file sink measures no Codata or
  JetStream completion latency.

The full suite included the admission migration, no-op, tracked-ignore, rename,
delete, resident and failed-End cases. A separate mutation-proven adversarial directory probe passed: an ignored parent whose only tracked child is hard-excluded.
The existing whole-directory exclusion fixture does not alone prove that case.

## Codata feedback still under investigation

Codata observed a second 22392-owner, 2993-batch generation before its controlled
edit, with zero reported TS parses. Equal owner digest is not graph equality;
zero TS parses do not exclude changes in other inputs. A startup watch reconcile
is allowed, but unchanged inputs must emit no replacement. The exact build,
command, state placement and triggering inputs are needed for attribution.
This report does not claim that the admission patch reproduces or fixes that
specific observation, nor that Codata consumer latency has been measured.

No performance acceptance or main publication is claimed by this checkpoint.

## Early historical finding: scope remains too broad

First transition `4d104e600f89 → 07fb4a41ddaf`: cold equality and frozen manifest
PASS, Begin **1401 owners**, **2 owners with changed contributions**, 4 TS parses.
File-sink wall times were delta 15.372 s and cold 31.157 s on the shared host;
these are diagnostic observations, not matched performance acceptance.

The source changes include a body/constant edit in `tracker.ts` and a new import
in `beacon.ts`. Comparing published node identities shows one added dependency
node and zero removed nodes. A projection using the **target cold** dependency
records gives 1368 reverse-closure owners from tracker, 2 from beacon, and 1369
from both; all 32 manifest seeds close to only 32. This is evidence for examining
unconditional reverse closure of body edits under the richer import graph. It is
not live attribution from the exact prior state and is not proof that dependency
edges are incorrect. The next optimization must distinguish owners requiring
replacement from changes requiring propagation, retaining side-read, reexport,
resolution and failure guarantees.
