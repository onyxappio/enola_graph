# Fresh no-op startup: avoiding redundant resolver/config work

Revision 2. Read-only design note. Stage20 stays frozen at `d39c43e`
(`owner.go` sha256 `2673574ae559a20a2817c9a54fdf1ca66acb21a12495cb6ea38e8022270a97ed`).
No builds, tests, benchmarks or production edits were performed for this note.

Revision 1 was reviewed in `msg_e2810f9df6d1`. This revision retracts what that
review found wrong, re-scopes what it found over-generalized, and re-centres the
note on the question root actually asked: safely avoiding redundant resolver and
configuration work from inputs this same run has already validated.

---

## 1. Corrections to revision 1

**C1 — P2 does not preserve corruption detection. Retracted.**
Revision 1 §4 claimed "P1 and P2 preserve the current detection point exactly."
That is false, and the source says so plainly. `recoverAcknowledgedPendingFP`
(`internal/graphsession/persist.go`) calls `readStateFileFP(pendingStatePath(dir), w)`
as its *first* statement, before all three reject branches — no acknowledgement,
identity mismatch, and `LastComplete == false` are each tested only after the
pending file has been fully unmarshalled. `decodeStateBytes` is the only load-time
type validation in the package. So a pending state with a type-corrupt body
errors today *even when it is about to be discarded*. A header-only fast path
would silently drop that body instead. Deferring the full decode to the promotion
branch does not recover the behaviour either, because the rejected-pending case is
exactly the case that would stop being checked. P2 as written is a behaviour
change to corruption reporting, not a transparent optimisation, and it is
withdrawn in that form.

**C2 — P4 restates the parked Stage19 candidate. Reconciled, not re-proposed.**
`docs/benchmarks/product-efficiency-2026-09-25/STAGE19_VALIDATION.md` is titled
"deferred cached facts for summary-only CLI calls". That is P4. It was measured
over three alternating pairs and parked at `7b03158`:

| Mode | latency change | peak RSS change |
|---|---:|---:|
| initial | -0.64% | -2.18% |
| noop | -1.90% | -8.41% |
| body | +0.53% | **+12.77%** |
| structural | +0.47% | +1.97% |

All four latency ranges overlap and pair 2 was slower for the candidate in every
mode, so no latency gain is established. Body-delta RSS rose in all three pairs
(10.35%, 2.65%, 18.08%). `STAGE19_RSS_DIAGNOSTIC.md` records one instrumented
comparison that did not reproduce the RSS difference and explicitly states this
does not disprove it. Root's standing instruction on that series is "Do not repeat
the same experiment solely to obtain a favorable result; investigate allocation
and object lifetime first." Proposing another deferred-facts variant now would be
that repeat. P4 is therefore **withdrawn from this note's recommendations** and
recorded only as prior art: the no-op RSS direction it showed (-8.41%) is the one
datum worth carrying into the object-lifetime investigation, and the body-delta
regression is the open question that gates it. I am not asking for a variant.

**C3 — three constraints were scoped tasks, not global rules. Provenance below.**
Revision 1 treated "no persistent inventory reuse", "keep exported API unchanged"
and "lazy facts deferred" as a standing impossibility bound on the startup goal.
That was wrong, and it also violates my own standing rule against universal
impossibility claims. Exact provenance and scope:

| Constraint as I stated it | Message | Actual scope |
|---|---|---|
| No persistent inventory reuse | `msg_5b64ab8c5601` — "Next implementation priority after Stage17 freeze: fuse fresh proof and inventory walk" | A bounded task: fuse `Policy.ReusableOver`'s fresh traversal with the engine Inventory traversal in a new scratch from `c6332bf`. The same message adds "Eager state decode unchanged" and "First send concise API design and risks before editing". It restricts that task; it does not forbid persisted reuse generally. |
| Keep exported API unchanged | `msg_36faf46f8124` (Stage13) | "Proceed now with step 1 only in /tmp/enola-stage13-discovery-thread: call-local walk cache … no cross-call Discovery reuse". Scoped to Stage13 step 1. |
| Do not implement lazy facts/accessor refactor | `msg_fcc53b0e6f0d` — "Audit reviewed; retain eager validation for now" | Defers the *accessor refactor* and mandates preserving load-time type validation, but then states: "Prefer a separate deferred clone at reuseTSCache, materialized before real consumers if publication is needed, without changing FileRecord or decoding." That is a preference for a smaller change, not a prohibition. I mis-scored it as blocking. |

Per root: the user authorises state/index reuse optimisation, a verified persisted
input summary is not ruled out by current AGENTS, and the two hard carve-outs are
**type validation** and **replay**. This note respects those two and drops the
impossibility framing.

**C4 — the falsification criterion was too binary. Rescoped.**
Revision 1 §5 said a sub-3x decode microbenchmark would refute P2 and P3 and close
the state-decode track. A microbenchmark result can only refute the premise of the
variant it measures. Rewritten in §6 as per-proposal predicates.

**C5 — there is no double policy build on the measured no-op.**
Revision 1 implied `graphinput_build` was paid twice. The trace refutes this:
`graph_inputs_proven 0.104s trace=reconcile` shows `Policy.ReusableOver` already
succeeds, so reconcile does *not* rebuild. The 0.382s `graphinput_build` is the
CLI's own `resolve_graph_target`, paid once. The reuse machinery is already doing
its job here; the redundancy is elsewhere.

---

## 2. Evidence base

`docs/benchmarks/product-efficiency-2026-09-24/stage16-phase-memory/baseline/r1-new-noop.phases.log`,
71 lines, one fresh CLI no-op, `cli_complete total=1.817s`. Root-owned, read-only.
Marks are nested and are phase-boundary deltas; they are not additive and they are
not stack attribution. Quoted exactly:

```
git_ls_files        0.061s  trace=graphinput_build  tracked=45105 dirs=5818
walk_tree           0.131s  trace=graphinput_build  names=11635 ignorefiles=4
check_ignore_setup  0.036s  trace=graphinput_build
check_ignore_run    0.067s  trace=graphinput_build  names=11635 ignored=0
compute_identities  0.034s  trace=graphinput_build  entries=11635
graphinput_build    0.382s  trace=graph_engine
resolve_graph_target 0.382s trace=cli
graph_inputs_proven 0.104s  trace=reconcile
ts_config_inputs    0.168s  n=83                        <- first
analysis_fingerprint 0.170s trace=inputs  cfg_files=59
ts_disc_nuxt_packages 0.135s pkgs=0
ts_disc_alias_roots   0.093s roots=1
ts_config_inputs    0.162s  n=83                        <- second
skip_publish_return 0.165s  total= 1.027s  trace=session
state_json_unmarshal 0.248s files=4086
```

---

## 3. Findings

**F1 — the TypeScript configuration enumeration is a full tree walk, and it runs
twice in one no-op.** The mark `ts_config_inputs` wraps `tsConfigInputs`
(`internal/extractors/tsextractor/session.go:1275`). That function performs
**two** full recursive traversals of the repository: `collectTSAliasRoots` at
`session.go:1242`, which recurses every non-skipped directory via `overlayReadDir`
and attempts `tsconfig.json` then `tsconfig.base.json` in each one
(`ts.go:3249-3290`), followed by `overlayWalkDir` at `session.go:1247` over the
same tree. It is called from two places in a single no-op:
`analysisFingerprintInputs` at `resident.go:249` (first observation) and again at
`session.go:1854` (the no-publication fence). 0.168s and 0.162s, `n=83` both times.
The second is 0.162s of the 1.027s session trace.

**F2 — the read half of the fingerprint is negligible; the enumeration is the
cost.** `analysis_fingerprint` totals 0.170s with the nested `ts_config_inputs` at
0.168s, so reading and hashing all 59 captured config bodies costs roughly 2 ms.
Any proposal that targets the *reads* or the *hash* at the fence is chasing
milliseconds. The walk is the item.

**F3 — the captured config bytes are already retained in memory.**
`analysisFingerprintInputs` returns `captured map[string][]byte`; `session.go:653-656`
copies every entry into `s.capturedSources`, and `s.cfgCaptured` holds the same
map. The fence at `session.go:1854` discards the second `captured` map it builds
and compares only the hex hash. This is an allocation observation, not a latency
one, and given F2 it is not a latency lever.

**F4 — `collectTSAliasRoots` is recomputed at least three times per no-op.**
Call sites: `discovery.go:113` (visible as `ts_disc_alias_roots 0.093s roots=1`),
`session.go:1242` inside each of the two `tsConfigInputs` calls, plus `ts.go:241`
and `ts.go:4802`. For `roots=1` on this repository the walk is pure overhead
repeated per caller.

**F5 — substituting the engine inventory for this walk has already been tried and
rejected, and that rejection is specific.** `ConfigInputPathsFromNames`
(`session.go:1161`) accepts `names`, discards them with `_ = names`, and carries
the comment "Engine inventory pruning (ignore globs) is not equivalent to this
walk." This matches the standing instruction "DO NOT use sharedDiscoveryEntries
unchanged: it prunes testdata". The rejection is of *inventory substitution*. It is
not a rejection of reusing the walk's own result, which is a different object with
different semantics.

**F6 — `check_ignore_run` costs 0.067s over 11,635 names and excludes nothing
here (`ignored=0`), while `git_ls_files` has already listed 45,105 tracked
paths.** Git does not apply ignore rules to tracked files, so a large majority of
those 11,635 queries are answerable from a result the same run already has. I have
not read the ignore-resolution implementation closely enough to assert this is
safe, and "Lockfile exclusion is intentional graph policy per AGENTS" plus "do not
weaken engine-pruned manifest semantics" both apply. Recorded as a lead requiring
its own reading, not as a proposal.

**F7 — `ts_disc_nuxt_packages` costs 0.135s and reports `pkgs=0`.** Inside
`ts_discovery_build 0.242s`. Recorded; not analysed.

---

## 4. Proposal

**P-A (primary): memoise the TypeScript configuration enumeration within a single
observation.** One cached result of `collectTSAliasRoots` and of the
`tsConfigInputs` traversal per observation instant, keyed by (repoPath, scope
identity), held in a value that does not outlive the observation that created it.

Boundaries that make it safe:

- **It does not span the fence.** The fence at `session.go:1854` exists precisely
  to re-answer "did the analysis inputs move during this transaction", and it must
  continue to perform a genuine fresh traversal. The memo is invalidated, or
  simply not shared, across the two observations. A short window is not proof
  against ABA, and this proposal does not rely on the window.
- **It does not substitute the engine inventory.** It reuses the walk's own
  output, so F5's recorded rejection does not transfer. `overlayWalkDir` keeps its
  current semantics bug-for-bug, including the enumerated-before-listing record
  behaviour, which stays parked.
- **No persisted format, no exported API signature change, no change to
  `FileRecord`, decoding, or `Policy` identity.**
- **Type validation and replay untouched.**

Expected effect: removes the duplicated `collectTSAliasRoots` traversals within
each `tsConfigInputs` call and between `tsConfigInputs` and `ts_disc_alias_roots`
in the same observation. It does **not** remove the second `ts_config_inputs`
(0.162s) — that is the fence and it stays.

Size: unknown. The marks are nested phase boundaries, so I cannot attribute a
figure to `collectTSAliasRoots` alone from this trace, and I will not derive one
by subtracting nested marks. It needs a focused measurement, which I am not
running under the current read-only instruction.

**Withdrawn or demoted from revision 1**

| | Status |
|---|---|
| P1 (skip redundant identity-binding decodes) | Retained as a candidate, unchanged in substance, but demoted: `load_state 0.287s` with `state_json_unmarshal 0.248s` is a real cost, yet it is not "resolver/config work" and root deferred P1/P2 explicitly. Not recommended for implementation now. |
| P2 (header-only pending triage) | **Withdrawn.** See C1. Any future form must state the corruption-reporting change as a deliberate decision for root, not as an invariant-preserving optimisation. |
| P3 (fingerprint-gated decode reuse) | Held. Independent of P2's defect, but unmeasured and outside the redirect. |
| P4 (deferred cached facts) | **Withdrawn.** See C2. Prior art only. |
| P5 | Folded into P-A. |

---

## 5. Invariants

P-A preserves: the exact path set and ordering `tsConfigInputs` produces today
(`sort.Strings(out)` unchanged); `followTSConfigExtends` behaviour; scope
filtering and `graphinput.IsLockfile` exclusion in `analysisFingerprintInputs`;
the admission-fingerprint choice documented at `session.go:3262-3267`; the
`ErrInputsChanged` fence and its error text; `revalidateCapturedInputs` and every
preview fence; load-time type validation; journal replay ordering in
`OpenSession`.

P-A changes: nothing observable, if the memo is correctly bounded to one
observation. The whole risk is in that bound, which is why the fence must be
tested explicitly rather than assumed.

---

## 6. Falsification, per proposal

Each predicate refutes only its own proposal.

1. **P-A is not worth building** if a focused benchmark of `tsConfigInputs` on the
   pinned Product tree shows the memoised form saving less than a few tens of
   milliseconds per observation. That result says nothing about F6, F7, or the
   state-decode track.
2. **P-A is unsafe** if a test that mutates a `tsconfig.json` between the first
   observation and the fence fails to raise `ErrInputsChanged`, or if a test that
   *adds* a new `tsconfig.json` in that window is not detected. Either outcome
   kills P-A in this form regardless of its speed.
3. Neither predicate bears on P1 or P3. A sub-3x decode microbenchmark would
   refute only the specific header-skip premise it measures, not "all useful
   decode changes" — the revision-1 claim to the contrary is withdrawn.

Correctness cases P-A needs before any timing: mutation during the fence window
(both directions above); mixed-extractor and neutral-refresh no-ops, since
`neutralConfig` drives `revalidateCapturedInputs`; a monorepo with more than one
alias root, since this repository yields `roots=1` and does not exercise the
multi-root path; and the overlay-scope cases, so the memo cannot leak across two
different scopes in one process.

---

## 7. What I do not claim

- No timing claim of any kind. Nothing was built, run, or measured for this note.
- No attribution of the 0.382s, 0.168s or 0.162s marks to any function below the
  mark, and no sum of nested marks.
- No claim that the four allocation intervals in `STAGE19_RSS_DIAGNOSTIC.md`
  (~423 MB pending-state writing, ~308 MB index/owner grouping, ~191 MB new file
  state, ~186 MB state loading) attribute to a stack; they are phase boundaries.
- No claim that F6 is safe. It is a lead, unread.
- No claim that removing the duplicated enumeration reaches the near-zero startup
  goal. The measured no-op is 1.817s and P-A addresses a part of one 0.168s mark.
