# Independent resident implementation review

**NO-GO for general native-watch/config-reload correctness acceptance.** Three P1 issues reproduced in bounded scratch tests. This does not invalidate the coordinator's successful Product body/structural smoke; that scenario does not exercise these failures. Frozen source `/tmp/enola-resident-stage-source` and main were read-only. No Product mutation, benchmark, or broad suite run was performed.

Read the implementation report, AGENTS, resident/session/owner/change-source code, bootstrap registrations, CLI reload wiring, filesystem eligibility gates, and existing resident tests. All code references below are relative to the frozen source.

## Blocking findings

### P1 — Failed reload promotes the engine before commit, leaving stale configuration after revert

`internal/graphsession/resident.go:173-185` assigns `r.eng = eng` before `s.run`; failure at `214-216` preserves old committed inputs but does not restore the engine. Recovery reload comparison at `173` compares disk config with **old committed** `r.inputs.effective`, not the configuration actually represented by the now-replaced engine.

Reproduction: bootstrap with a.ts enabled; change config to ignore a.ts; inject broker failure during reload; revert config to its original absent state; clear broker failure and apply again. Recovery succeeds while `r.eng.Config().Ignore` still contains a.ts. This can publish/cache the wrong graph and then declare subsequent empty batches idle.

**Fix:** keep the reloaded engine transaction-local, including effective-config capture/validation and extraction; promote engine with committed state/inputs only on successful transaction. Preserve recovery semantics for an acknowledged End whose state promotion failed: recovered disk generation and engine/config snapshot must be reconciled together. Add failed reload + revert tests for pre-End broker failure, captured-input failure, and post-End state-promotion failure. Compare to a newly bootstrapped engine from disk, not `residentCold`'s current `r.eng` (`resident_test.go:45-53`), which can hide the wrong-engine defect.

### P1 — Config bytes loaded by ReloadEngine are not the bytes validated for commit

`resident.go:167` captures pre-reload effective config; `177` invokes the loader; `185-186` replaces the engine and **overwrites** the captured effective config with a new disk read. `196-203` only compares against that post-load read. A writer changing config after the loader consumed it but before it returned is therefore accepted as a consistent transaction.

Reproduction: config says ignore a.ts; callback constructs that engine, writes `ignore: []` before returning; ApplyChanges succeeds with a.ts still excluded and captures the new include-all bytes. The next reconciliation sees equal config bytes and does not fix the engine.

**Fix:** bracket engine loading with config snapshots and reject/retry if inputs differ; ideally loader returns the precise consumed config snapshot with the engine, covering discovered configuration dependencies. Never replace the pre-load snapshot without checking it. Apply the same discipline at initial bootstrap: `r.inputs == nil` bypasses reload (`173`) even if the engine was built before a config change during watcher registration (`changes.go:265-282`). Add a bootstrap-change regression and callback-time race regression; initial race is code-inspected, not separately executed here.

### P1 — External coverage accepts symlinked ancestors without covering their replacement

`changes.go:391-402` uses `os.Stat` on the nearest existing parent, following ancestor symlinks. `403-405` only Lstats the final config path, then `412` watches the parent inode. For `/outside/alias/base.json`, where alias is a symlink to another directory, final Lstat reports a regular file and coverage succeeds. Retargeting alias changes the effective input while the watch can remain on the old target directory; alias's containing directory is not registered by this path. This contradicts the implementation's fail-closed symlink coverage contract.

A focused gate test explicitly supplies that external config path and proves `CoverSessionInputs` accepts it. It does **not** claim a platform-specific missed-event end-to-end test was run.

**Fix:** inspect the full ancestor chain for external inputs and missing candidates; reject uncertain symlink components, or explicitly track symlink and target topology with replacement watches. Account for ordinary ancestor rename/replacement too: filtering ancestor events (`430-448`) cannot receive events from ancestors never watched. Validate per backend. Add existing ancestor symlink, retarget, missing config under symlink, and ancestor-directory rename tests; each must either fail coverage closed or trigger a correct reconciliation. Avoid simply rejecting Darwin's normal `/tmp` -> `/private/tmp` alias without canonicalizing a stable root and documenting the supported topology.

## Bounded independent evidence

Scratch: `/tmp/enola-resident-independent-scratch` (copy of frozen source).
Probe source: [archived scratch probe](resident-independent-probes.go.txt) in that scratch.
Final log: `/tmp/enola-resident-independent-tests.log`.

Command:

```
/tmp/enola-toolchain/go/bin/go test ./internal/graphsession -run '^TestIndependent(FailedReloadRevert|ExternalAncestorSymlink|ReloadConfigCaptureRace)$' -count=1
```

All three probes fail on the frozen implementation, with explicit assertions for the above defects; package execution reported 1.546s. These are reproductions, not passing validation. Original test wiring was intact for the final run. An earlier scratch filename collision temporarily replaced an existing test helper file and caused a compile failure; both original files were restored, the probes renamed, and the coordinator correction sent. There is **no frozen-source compile defect** claimed from that attempt.

## Reviewed behavior with no additional blocking defect established

- **Lifetime/recovery:** OpenSession binds identity, acquires the writer lock, opens/replays the journal and recovers acknowledged pending state (`session.go:78-150`). ApplyChanges serializes operations, forces reconciliation after failures, and accepts watermarks only after successful transactions (`resident.go:237-279`). Generation guards protect reuse of the resolution index (`206-207`). Engine promotion is the exception identified above.
- **Committed facts:** file-state maps are copied (`session.go:274-279`, `owner.go:106-124`); TS extraction clones retained facts (`tsextractor/session.go:301,463,484`); cloneTagged copies top-level props/relations, and Snapshot deep-copies via JSON. No additional mutation of retained fact structures was established in the inspected paths. This was code review, not a comprehensive alias/race proof.
- **Watcher startup/new directories:** event reader starts before recursive registration; parent directory watches precede descent (`changes.go:162-199`). Create/rename/remove events register discovered directories and enqueue reconciliation (`216-228`). Events during baseline remain pending. CoverSessionInputs registers external/missing inputs and queues registration-gap reconciliation (`412-416`), so startup catch-up must remain in total startup work/time.
- **Overflow/loss/symlinks:** queue overflow keeps a reconciliation signal; kernel errors and registration uncertainty become persistent uncovered status (`changes.go:77-119,232-248`). Real Watch rejects lost coverage and requires restart (`313-315`). Repository symlinks and final external-file symlinks fail closed; ancestor coverage is the gap above. No Linux or network-filesystem runtime checks were executed.
- **External sibling filtering:** exact target matching and ancestor-only topology filtering (`430-448`) prevent ordinary sibling logs/checkpoints from feeding back into analysis. Lowercasing on case-sensitive filesystems can admit extra sibling events (conservative extra work); not an established stale-graph bug. Watch directories can accumulate as configuration targets move; consider pruning for long-lived resource usage, but no acceptance blocker demonstrated from that alone.
- **Real Watch retries:** ErrInputsChanged queues Lost and re-enters the event loop (`283-287`); resident failed state then enforces replay/reconciliation. Other parse/broker errors exit explicitly. The test suite includes a captured-input retry test; not rerun here. Some disappearance/read errors are intentionally fatal rather than retried, so do not claim all concurrent atomic-save errors recover transparently (`session.go:967-970`).
- **Cross-extractor direct resolution:** new index is compared against committed candidates and changed reference owners added to scope (`session.go:861-870`; `owner.go:672-723`). This handles unresolved/resolved/ambiguous direct relationships without transitive attribute propagation. No extra blocking defect found in these paths; full-profile Product cold equivalence remains coordinator validation.
- **No-op/fast path:** covered contiguous empty batches return before input work/publication/checkpoint (`resident.go:257-266`), with zero initialized counters. Nonempty TS edits use captured bytes, hashes, TS invalidation/composition, and source/config verification. Unchanged-content events can still hash/check bounded context; that is different from empty idle. Full-map copying, composition and checkpoint serialization remain substantial work; no performance conclusion follows from code review.

## Acceptance limitations and next step

Lockfile-ignore and repository input-policy integration are separately owned and not present in this frozen stage; this review does not accept the stage as meeting those AGENTS requirements. Do not reuse historical lock-aware graph hashes as final policy oracles. Native watcher availability/descriptor scaling, Linux behavior, immutable-cache behavior across every extractor, repeated Product timing/spread, and fresh-CLI versus resident latency remain unverified here.

Fix the three reproduced issues, preserve the probes as regressions, and run config-aware cold equivalence with an independently reloaded cold engine. Then rerun affected recovery/watcher tests and the coordinator's isolated acceptance scenarios. The architecture is suitable for continued validation, but this frozen implementation is not yet a general correctness GO.
