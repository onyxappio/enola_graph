# Graph input integration

Implemented in main; no commit, Product source edit, benchmark run, timing acceptance, or Telegram message. The policy/config implementation is owned by the separate policy dispatch and described in `/tmp/enola-graph-input-policy.md`; this report covers consumer/session/CLI integration.

## Contract and public entrypoints

- `bootstrap.NewGraphEngine(bootstrap.GraphOptions{Repo: root, ConfigPath: override, StateDirs: outputs})` builds the graph-only immutable policy, scoped consumers and a reconstruction factory. An empty override discovers root `mcp-arch.yaml`; explicit override must exist. CLI `graph` uses this factory, supports `--config` with a repository argument and a positional single-repository config. External state/output paths are omitted from repository policy options so compatible fork contexts retain cache reuse.
- `graphsession.NewGraphFileChangeSource(eng.Analysis(), root, ignored, limit)` installs the policy before native watcher registration. Production `Watch` uses this constructor. `CoverSessionInputs` covers policy controls and declared dependencies, refreshes policy and registrations after successful reconciliation, prunes excluded registrations, and conservatively reconciles registration gaps without discarding late events.
- `ObservedPath(absPath)` is bounded exact-path processed-native-event diagnostic evidence, including filtered events (1024 cached paths, zero means absent). Observation is recorded after event handling. It is not a backend drain barrier or strict disk equality guarantee. `ObservedEvents` is an aggregate diagnostic count.
- `graph --summary-json` emits a JSON result array with Facts nil; `--json` preserves the existing full-Facts contract and wins if both flags are specified.
- Legacy bootstrap and extractor constructors preserve their original behavior, including manifest lockfile resolution. The graph constructor uses manifest no-lock mode, preserving local manifest/dependency facts without lock-derived versions.

## Enforced graph inputs

Immutable inputscope plumbing guards inventory, names, all direct filesystem reads in the six supported active consumer packages, and independent builtin detector walks/probes. TS fingerprint/config discovery/composition helpers receive the same scope; captured-overlay reads check policy before returning bytes. Excluded inputs are removed before names, source hash targeting, detector selection, fact production and native graph queuing. Root/nested mandatory lock names stay excluded on edit/add/delete, including TS config candidates and manifest discovery/ancestor reads. Cache defaults, explicit Enola exclusions, repo nested Git rules and tracked exceptions use the shared policy; explicit Enola exclusion wins over Git tracking.

Supported active concrete consumers: TypeScript/JavaScript, no-lock manifests, Markdown, HCL, Python, Swift. Other active concrete or custom consumers explicitly fail before graph publication, as approved by the coordinator, rather than emitting a partial graph with unaudited hidden reads. Inactive builtin detectors use scoped filesystem boundaries or policy-filtered name lists. Scope imports were propagated through legacy-compatible optional helper parameters; legacy callers pass no scope.

Audited opaque media remains name-only for Markdown link resolution. Content-only media changes are filtered unless graph_inputs.semantic or a declared content dependency promotes them; explicit Swift includes remain semantic. Exclusions still win over explicit semantic dependencies. Unknown/name/directory changes reconcile. A manually supplied ChangeBatch.Paths represents content notifications; callers must set Reconcile for membership changes. Native watcher integration makes that distinction itself.

New factory-bound sessions reconstruct policy/config on initial bootstrap and every reconciliation; content/idle transactions reuse the committed snapshot. Policy profile identity is hashed into analysis configuration to migrate existing state conservatively. Policy dependency bytes are checked at reconciliation completion, including the no-publication path. Runtime counters now include PolicyBuilds for in-request reconstruction; constructor work is separate, and existing inventory/hash counters do not count policy builder Git IO/additional name traversal.

## Correctness repairs and tests

The independent review's three probes are retained in `internal/graphsession/resident_review_regression_test.go` with unchanged assertions: failed reload plus config revert restores the committed engine; a config change inside ReloadEngine aborts; a declared external dependency under a symlinked ancestor cannot obtain native coverage. Engine replacement is rolled back on failed generation and promoted only with successful state/input caches. Graph factory creation captures config before/after loading, and first reconciliation rebuilds it; a dedicated initial-factory/config-change regression compares to a freshly constructed cold graph. Generic legacy OpenSession has no consumed-config snapshot and still requires its caller to provide a correctly constructed engine at initial entry.

A real watcher regression exposed and fixed a prior session bug: ignoring the last TypeScript source made its detector inactive, but prior extractor retirement explicitly skipped TypeScript and kept stale owners. Retirement now removes the TypeScript contribution and synthetic owners while retaining other consumers' contributions. The watcher scenario now exactly matches an empty cold graph.

New fixtures cover all six supported active consumers, TS content delta/cold equivalence, ignored source/manifest/TS-config mutations, nested Git ignores, tracked exemption and explicit exclusion precedence, lock edit/add/delete in resident and strict runs, media content versus membership, semantic-media override, policy scope removal, real native lock-delivery evidence, real .gitignore scope change, explicit/root config selection, unsupported active rejection before events, and factory-to-initial config changes. Existing cold/delta/noop/overflow/midrun/recovery/fork tests remain in the affected suite.

Validation logs:
- `/tmp/enola-policy-affected-final.log`: affected extractor packages, engine, graphsession, bootstrap and command all pass after retirement fix.
- `/tmp/enola-policy-six.log`: updated graph-policy fixture with all six active consumers passes.
- `/tmp/enola-policy-retirement.log`: real watcher last-TS retirement and initial config-change regressions pass.
- `/tmp/enola-policy-race.log`: affected extractor and engine race checks pass; this earlier aggregate run exposed the subsequently fixed watcher retirement functional failure, not a race.
- `/tmp/enola-policy-race-final.log`: final engine/session/bootstrap/command race rerun all pass on the final implementation.
- `/tmp/enola-policy-explicit-dependency.log`: final additional native-watcher graph-profile external TS extends and Swift YAML-in-.png include regressions pass under race; each changes the raw snapshot, advances the generation and exactly matches cold.
- `git diff --check` passes.

## Remaining costs and boundaries

This is input-policy integration, not package-scoped semantic invalidation. Policy identity currently includes relevant tracked membership and selection configuration; those changes can force broad TS reanalysis. Policy Build performs an additional name walk (including Git-ignored trees) and Git subprocess evaluation; initial graph startup currently constructs policy twice, once in the public factory and again in initial session reconciliation. Resident empty/filtered-content batches do none of this. Hash/state map copies, broad composition, full checkpoint rewrite/fsync, and broad manifest/config history fallback remain; state patches and deeper incremental composition remain separate work. No timing claim is made.

Global Git excludes and .git/info/exclude remain outside the shared policy contract. A symlink checkout root must be passed as its resolved path to the new graph factory, which rejects that otherwise ambiguous root explicitly; legacy entrypoints are unchanged. Native coverage still fails conservatively on unsupported filesystem/topology, watcher loss or uncertain symlinks. Reconciliation/control captures and retained observed watermarks do not provide an atomic whole-disk snapshot or backend event flush. Active languages/spec consumers outside the six audited implementations need a separate side-input audit before graph-policy support.
