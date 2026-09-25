# Remaining startup work after Stage20: evidence and next investigation

Status: read-only assessment, not an implemented optimization or latency acceptance. Stage20 remains frozen and unmerged pending paired measurements.

## Evidence

The prior instrumented Stage15 baseline [no-op trace](../product-efficiency-2026-09-24/stage16-phase-memory/baseline/r1-new-noop.phases.log) reports 1.817 s CLI completion with zero parses. It ran under shared load with memory instrumentation; durations locate work, not a current performance claim. Nested marks must not be summed.

| Observed operation | Trace duration | Work reported |
|---|---:|---|
| Graph input policy build | 0.382 s | Git membership, ignore semantics and tree walk |
| Load complete state | 0.287 s | 56,633,538 bytes; includes 0.248 s JSON decode |
| Inventory projection | 0.125 s | 10,965 files/names |
| Content input hashing | 0.163 s | 4,105 targets; about 45.8 MB hashed |
| TS configuration inputs | 0.168 s then 0.162 s | Two separate observations, 83 inputs each |
| TS discovery build | 0.242 s | 1,965 side reads, 1,977 stat reads, 655 walked directories |
| TS cache reuse | 0.039 s | 4,086 owners and 59,661 facts |

Current source confirms OpenSession reads and decodes full committed state before reconciliation (`session.go`, `persist.go:readStateFileFP`). `writePendingStateFP` marshals and durably replaces the complete state on a changed run. Stage20 changes owner/index construction, not these startup/persistence paths. Removing only JSON decode cannot remove all observed startup work; no numerical projected speedup is claimed.

## Next investigation

1. Obtain the pending Stage20 uninstrumented timing/RSS decision before changing its source. If accepted, profile that exact integrated source; do not treat older mark durations as its cost distribution.
2. Prefer a compact, separately verifiable input/context summary only if it can prove no-change without materializing all graph facts. It must cover repository membership, policy and Git controls, source content, semantic side inputs and resolver discovery. A path/mtime-only shortcut is insufficient. Unknown or changed evidence must fall back to full reconciliation.
3. Keep fresh CLI and resident-session optimization separate. An existing resident may reuse proven state; a fresh CLI still needs a sound observation of current inputs, so a near-zero fresh no-op target needs measurement and a clearly defined correctness argument, not a daemon benchmark.
4. Evaluate state representation/decode and write costs independently of the no-op proof. The prior roughly 390 MB allocation interval before pending-state completion includes multiple operations and is not evidence that serialization alone allocates that amount.

Any proposal must preserve replay before use, identity/protocol binding, pending-state promotion only after acknowledged End, corruption detection, atomic durable checkpoints, zero unchanged events/parses/generation advancement and exact fresh-versus-delta graph equivalence. Schema/cache migration and interrupted-write tests are required if persistence changes. No production implementation has been authorized by this note alone; the existing worker is preparing a bounded design proposal while the candidate remains frozen.

## First design review

Worker proposal `/tmp/enola-stage20-resolution/DESIGN-startup-noop.md` (first revision SHA256 `b7188a9ed494ae82ae1b11f035edb186572a148cba26e6a9e384298647937029`) is not accepted for implementation. Root requested these corrections:

- Header-only pending-state triage changes corruption behavior even if promotion retains full decoding: today an invalid typed body errors before the incomplete, wrong-identity or unacknowledged-End fallback. Silently ignoring that body would weaken recovery validation.
- Avoiding summary-only fact cloning overlaps the already parked Stage19 experiment and must account for its inconsistent latency gains and RSS signal.
- Earlier task-local restrictions must not be promoted into a claim that the overall near-zero no-op goal is impossible. Any persistence proposal still needs sound fresh content/membership observations, type validation and recovery semantics.
- An arbitrary threefold microbenchmark threshold cannot refute every useful state-decoding change; acceptance is end-to-end evidence under the original correctness contract.

The revised investigation should focus on avoiding duplicate resolver/config computations from freshly validated inputs. No code change, new runtime benchmark or production integration followed this design review.

## Revised design direction

[Worker revision 2](stage20/startup-design-review2.md) withdraws header-only recovery triage and the duplicate Stage19 proposal. It proposes reusing the TS-config walk's own output within one precisely bounded observation, without substituting the engine-pruned inventory or reusing observations across the final independent input-change fence. This is a design lead, not implementation approval or measured speedup. Root requires explicit lifetime/identity rules and tests for newly added alias roots and extends targets, mixed profiles, neutral refresh and scope isolation before accepting the approach.

## Independent experiment boundary

After the second timing cancellation, the existing worker was assigned an independent scratch prototype from production-equivalent main, without modifying Stage20. Start inside one `tsConfigInputs` invocation; keep the later fence entirely fresh and defer cross-phase Discovery sharing. Root source review found that `overlayWalkDir` delegates to `inputScope.WalkDir`, while alias-root recursion uses `overlayReadDir`; caching only the latter would not remove the second filesystem traversal. The experiment must measure underlying enumeration calls and preserve differing pruning/probe semantics, not report memo hits as eliminated work.
