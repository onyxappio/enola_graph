# Product efficiency — 2026-09-22

Frozen v2 efficiency tooling. Three separate measurement kinds, never mixed:

| Tool | `measurement_kind` | What it is |
| --- | --- | --- |
| `resident.py` | `request-driven-resident` | Request-driven `benchresident`. **Not** `graph watch`. |
| `watch.py` | `production-graph-watch` | Bounded production `graph watch` check. |
| `soak.py` | `production-graph-watch-soak` | Long-running `graph watch` under an external editor. |

None of these is performance acceptance until a coordinator-owned Product run is
agreed.

## `graph watch` semantics this tooling depends on

These are properties of the production watcher, not of the harness. Every
completion rule below exists because of them.

- **Fixed collection window, not a debounce.** The window opens on the first
  event and closes `--watch-every` later. Further edits inside the window do not
  extend it. Measured on the 90s tiny soak: median save→Begin **5028 ms** with a
  5s window, and a minimum of **1678 ms** for an edit that landed into an
  already-open window.
- **Edits during analysis queue.** The next window opens only after the
  in-flight analysis finishes, so a burst can span several generations.
- **Broker errors terminate `graph watch`.** A broker failure is not retried
  into a degraded mode. `soak.py` therefore treats a watch exit as expected
  after broker trouble, records it as a `watch-exited` lifecycle event, and
  restarts the watcher. (In the 90s smoke a **149 ms** broker bounce between
  publishes did not kill the watcher, so no `watch-exited` event was recorded —
  a short bounce and a publish-time broker error are not the same case.)
- Frozen v2 guarantees are asserted, not assumed: every recorded Begin must be
  `scope_mode=complete` with a non-empty owner scope and `target=base+1`; idle
  and same-content duplicate notifications must publish **no** events.

### Completion selection (heuristic quiescence, not a drain proof)

The first End after an edit can describe a partial batch with another
generation still queued. After freezing the inputs, `watch.py` and `soak.py`
select the **final** completed generation for the exact watch context once:

1. **stable input** — the whole isolated checkout still hashes to the frozen
   state (any file type, and created/deleted files change the key set);
2. **no active Begin** — no observed Begin is missing its End. Begin/End records
   come from the observer's `OBSERVER_LIFECYCLE_FILE`; `consumer.jsonl` only
   gains a line when a generation *completes*, so without them an in-flight
   analysis is indistinguishable from an idle watcher;
3. **quiet margin** — frame count, JetStream `last_seq` and lifecycle record
   count all unchanged for `max(2*window, window+2s)`.

**This is not a proof that the watcher's queue is drained**, and the reports say
so: `internal_drain_proved=false`,
`completion_selection=heuristic-quiescence + observed cold equality`,
`requires_watcher_watermark_for_proof=true`, plus a
`quiescence_limitations` list. There is no watcher watermark to read, so an
analysis slower than the quiet margin whose Begin is not yet published is
unobservable, and a Begin's broker timestamp records when the Begin was
*published*, not when the watcher captured the input — a Begin after a save does
not prove that run scanned the saved bytes. Acceptance rests on **cold equality
of the final generation after the inputs are frozen**, not on the quiescence
signal.

An open Begin that a later Begin supersedes, or that predates a watcher restart,
is classified as **abandoned** (`abandoned_begins`) rather than waited on —
otherwise a watcher killed mid-publication would block the harness forever.
Abandoned Begins are reported, never silently dropped.

### Timing: what each number is, and is not

`write_durable` returns both the pre-write instant and the post-fsync
completion, and the completion is the reference used. That is a **choice of
reference, not a visibility guarantee**: a reader or filesystem watcher can
observe written bytes before `fsync` returns, so a save-relative interval can be
legitimately negative and is reported, not clamped (`fsync_visibility_note`).

Save-relative columns are named `time_since_latest_observed_save_at_begin_ms`
and `..._at_consumer_ms` because pairing a generation with the nearest prior
save is **correlation, not causation** — the collection window is fixed, so a
generation may have been triggered by an earlier save and merely happen to
follow a later one. Run and generation ids are recorded so a reader can re-pair
them (`causality_note`, `pairing_is_causal=false`).

The strongest end-to-end number is `convergence.final_convergence_ms`: last
observed edit → the start of the **observed final matching suffix**, i.e. the
first completed generation that equals the final cold graph *and is never
contradicted afterwards*. Unlike the save-relative columns it does not depend on
a Begin being at or after a save. It is still an observation, not a causal
claim: reports carry `is_causal_claim=false` and a `basis_limitation`, because
an edit that leaves the normalized graph unchanged (a comment, a reformat, a
no-op rename) makes an *earlier* generation match, extending the suffix
backwards and shortening the number without the work being faster. The offset
may be negative for the same reason, and is reported rather than clamped. Read
it as "from this generation onward the graph already matched".

**Cold equality is checked with the whole inventory on both sides of the cold
run.** The inventory is frozen before the quiescence wait and re-read after cold
analyze has finished reading the tree (`input_stability_across_cold`); if
anything moved in between, the two graphs were built from different bytes and
equality proves nothing. Scripted runs fail closed; external-editor runs, where
a real editor may write again after signalling, report
`equality_inconclusive_reason` instead of a verdict.

Two save-observation bases are kept apart and never mixed in one series
(`save_observation_basis`): `fsync-completion` for scripted writes, and
`filesystem-mtime` for an external editor — the latter being when the editor's
write landed, with no durability guarantee and no editor cooperation assumed.

### External-editor mode (`--external-editor`)

For a run driven by a real parallel AI editor rather than the scripted worker.
The harness **does not write the source** in this mode. It prints `READY <path>`
and writes `READY.json` carrying `edit_this_repo` (the isolated checkout to
edit), `signal_finished_by_creating` (the `STOP-EDITOR` file), the artifact
paths, the extractor profile and the effective config. The finish signal is
recorded but **advisory**: the run continues to its full `--duration` /
`--min-duration`, and the report splits `active_editing_s` from `idle_s`.

Change capture uses two deliberately different scopes:

| Scope | What | When |
| --- | --- | --- |
| integrity | whole isolated checkout, hashed | once at start, once after editing |
| live poll | `--editor-allowlist` subtree, metadata only, hashing just files whose mtime/size moved | every `--input-poll` |

Hashing the whole checkout on every tick would compete with the watcher for I/O
and distort the latency being measured. Out-of-allowlist changes are therefore
caught by the **final full inventory** and reported in
`changes_outside_allowlist` — reported, never blocked, and without a timestamp.
`observation_limits` records the detection lag (up to one poll interval), the
fact that a change reverted between ticks is invisible, and that no durable-save
timestamp is inferred from polling. This is complete change detection for the
**declared experiment scope**, not generic coverage of every path a watcher
might observe.

### Extractor profile (`--profile`)

`full` **omits the `extractors` key entirely** so the binary applies its
production default set (`docs/benchmarks/PRODUCT.md:214`); `typescript` pins
`[typescript]`; `auto` picks `typescript` for the tiny fixture and `full` for a
Product checkout. A `full` run fails closed if the effective config still pins
the key. The effective config text is recorded in the report. Explainers and
renderers stay empty — a CLI requirement of this harness, not a narrowing of the
graph.

## Pins and snapshots

`--rev candidate` / `--rev parent` are refused (exit 2). Pass an explicit SHA or
`HEAD`. Prepare always creates a **fresh** directory; existing snapshots under
`/tmp/enola-efficiency-2026-09-22-*` are never deleted.

| Kind | Meaning |
| --- | --- |
| `committed` | Clean checkout of the requested SHA. Verified to have no modified tracked files. |
| `experimental-patch` | Dirty worktree overlay on `HEAD`. Records the HEAD SHA, the diff hash, and `snapshot_contains_uncommitted=true`. **Not** equal to the committed revision. |

### Provenance is precise, not blunt

Every snapshot carries harness-only packages (`cmd/benchresident`,
`cmd/benchobserver`, `bin/`), so Go stamps **`vcs.modified=true` in both kinds**.
The provenance therefore never makes a single "equals the rev" claim. It records:

- `product_source_equals_committed_rev` — true only for a committed snapshot
  whose tracked files are verified unmodified (before *and* after the build);
- `snapshot_tree_equals_committed_rev` — always **false**, because of the
  injected harness packages;
- `snapshot_tree` / `snapshot_tree_after_build` — the classified `git status`;
- per binary: `vcs_revision`, `vcs_modified`, and
  `vcs_modified_explained_by_harness_injection`.

The build **fails closed** if a binary carries no `vcs.revision` or a
`vcs.revision` other than `snapshot_commit`, so a binary can never be presented
as built from a revision it was not built from. Measurement reports propagate
`snapshot_note`, so an `experimental-patch` measurement can never read as a
committed-rev measurement.

```
python3 docs/benchmarks/product-efficiency-2026-09-22/run.py --self-test
python3 docs/benchmarks/product-efficiency-2026-09-22/watch.py --self-test
python3 docs/benchmarks/product-efficiency-2026-09-22/soak.py  --self-test

python3 docs/benchmarks/product-efficiency-2026-09-22/run.py \
  --prepare --build --rev HEAD --kind committed \
  --snapshot-dest /tmp/enola-efficiency-2026-09-22-HEAD-new

python3 docs/benchmarks/product-efficiency-2026-09-22/run.py \
  --prepare --build --kind experimental-patch \
  --snapshot-dest /tmp/enola-efficiency-2026-09-22-experimental-new
```

Historical archived SHAs (not default pins):

- `35647859c639ead615307107f7a956b2ce8a6a9d` — 2026-09-22 candidate dump
- `6430d258694073b90c5c6847da239bdf4407f582` — 2026-09-22 parent dump

Those dumps remain as evidence. Do not rebuild them in place.

## Long watch soak (`soak.py`)

Real 5s collection window, external editor process, `>=30m` configurable.

```
python3 docs/benchmarks/product-efficiency-2026-09-22/soak.py \
  --snapshot $SNAP --duration 30m --watch-every 5s \
  --edit-interval 20s --work /tmp/enola-soak-new
```

The editor runs as a **separate process** (`--editor-worker`), standing in for
another agent editing the checkout. Its seeded workload mixes `add-export`,
`remove-export`, `edit-body`, `add-import`, `remove-import`, `create-file`,
`rename-file` and `delete-file`, and logs every operation with a timestamp to
`edits.jsonl`.

Recorded per generation: the latest observed save at/before the window, broker
Begin, first batch, broker End, consumer completion, owner scope count, scope
mode, run id, plus the derived `write_to_durable_ms`,
`time_since_latest_observed_save_at_begin_ms`, `begin_to_first_batch_ms`,
`begin_to_end_ms`, `end_to_consumer_ms` and
`time_since_latest_observed_save_at_consumer_ms`. Also sampled: watch RSS, and
planned watch/broker restarts with their downtime. The run ends with the
quiescence-selected final generation compared against a cold `graph analyze` of
the same tree, plus the `convergence` measure.

Add `--source /tmp/enola-product-benchmark-source` for Product (isolated clone
only; the source is never written). Use `--no-restart-watch` /
`--no-restart-broker` to drop the restart legs.

## Tiny smokes (not Product)

Require `--binary` or `--snapshot`. The harness will not silently build a
hardcoded historical SHA. Observer is built into the work directory via Go
`-overlay` so the snapshot tree is not modified.

```
python3 docs/benchmarks/product-efficiency-2026-09-22/run.py --smoke \
  --snapshot $SNAP --work /tmp/enola-eff-smoke-new

python3 docs/benchmarks/product-efficiency-2026-09-22/watch.py --smoke \
  --snapshot $SNAP --watch-every 1s --work /tmp/enola-watch-smoke-new

python3 docs/benchmarks/product-efficiency-2026-09-22/soak.py --smoke \
  --snapshot $SNAP --work /tmp/enola-soak-smoke-new
```

`--smoke` on `watch.py` uses a tiny TypeScript fixture and, when `--watch-every`
is left at the CLI default 5s, shortens the window to 1s for a bounded check.
`soak.py --smoke` shortens only the **duration** (90s) and keeps the real 5s
window. The production default remains **5s**.

The tiny fixture mutates the **exported symbol set**, not a literal. A
literal-only edit (`a = 1` → `a = 2`) leaves the normalized graph identical, and
cold equality would then pass even if the delta were dropped. Reports carry
`mutation_changed_graph`, and the tiny fixture fails if it is false.

Cold comparison runs use the **same `--repo-id`** as the watch run and differ
only by `--context` / `--state-dir`: facts are tagged with the repo id
(`tagRepo` in `internal/graphsession/session.go`), so a different `--repo-id`
changes every node id and cold equality can never hold.

### Product invocation check (does not execute a Product run)

```
python3 docs/benchmarks/product-efficiency-2026-09-22/watch.py --check-product \
  --snapshot $SNAP --watch-every 5s
```

Validates the Product source, the edit anchor, the scope overlay (which must not
repoint `repo:`), and the pins — then prints `executed_product_benchmark=false`.

Product watch invocation is:

```
python3 docs/benchmarks/product-efficiency-2026-09-22/watch.py \
  --snapshot $SNAP --watch-every 5s \
  --source /tmp/enola-product-benchmark-source \
  --scope-config docs/benchmarks/product-efficiency-2026-09-22/product-graph-scope.yaml \
  --work /tmp/enola-watch-product-new
```

That Product path is isolated (clone + overlay). It is **not** executed as part
of this tooling change.

## Measured durations (2026-09-22, snapshot `b611e22`, committed)

Tooling validation only — not performance acceptance.

| Check | Duration | Result |
| --- | ---: | --- |
| `run.py --self-test` | 0.14 s | ok |
| `watch.py --self-test` | 0.05 s | ok |
| `soak.py --self-test` | 0.05 s | ok |
| `--prepare --build` committed HEAD | 11.2 s | binaries stamped `b611e22` |
| `--prepare --build` experimental-patch | 11.2 s | `product_source_equals_committed_rev=false` |
| `run.py --smoke` | 3.5 s | cold equality equal |
| `watch.py --smoke` (1s window) | 11.4 s | gen 2 selected, cold equality equal, inventory stable across cold (4 files), idle/dup 0 events, convergence 1014.7 ms |
| `soak.py --smoke` (90s, real 5s window) | 100.6 s | 11 edits, 10 generations, cold equality equal, inventory stable across cold (8 files), convergence 8279.8 ms (gen 9, 2 cold-equal at tail) |
| `external_editor_smoke.py` (tiny) | 55.6 s | READY/STOP protocol, 3 changes from a separate process observed, harness wrote no source, out-of-allowlist `DESIGN.md` absent from the live poll and caught by the final inventory, run outlasted the finish signal by 31.5 s |

Convergence varies run to run with where the last edit falls inside the fixed
collection window (1005.8 ms and 8279.8 ms are both from passing soak smokes);
it is a single observed interval, not a bound.

90s soak latency medians: latest-observed-save→Begin 5030 ms (min 1788 ms for an
edit landing inside an open window), Begin→first batch 39.5 ms, Begin→End
83.0 ms, End→consumer 0.29 ms, write→durable 1.07 ms.

The broker outage was taken **during publication** (an in-flight Begin was
waited for before killing the broker) and the watcher **survived** it: the
documented terminate-on-broker-error path was not reproduced, so this is
recorded as an observation, not as recovery acceptance
(`broker_outage_verdict`, `broker_recovery_acceptance="untested until observed
in a Product run"`).

## Request-driven resident (not `graph watch`)

```
python3 docs/benchmarks/product-efficiency-2026-09-22/resident.py \
  --binary $SNAP/bin/enola --resident $SNAP/bin/benchresident \
  --observer $SNAP/bin/benchobserver --nats nats://127.0.0.1:$PORT \
  --root $WORK --repeat 1 --source /tmp/enola-product-benchmark-source
```

## Archived resident comparison

Three isolated candidate runs and one parent run used fresh NATS JetStream
servers, an independent ordered observer, and the same Product source/config.
Those numbers used the archived SHAs above and a dirty worktree that is **not**
those commits. They are historical evidence, not a pin of current HEAD.

| Scenario | Candidate | Parent | Result |
| --- | ---: | ---: | --- |
| Resident initial, all consumer frames | 8.459 s median (8.390–8.608) | 9.288 s | candidate 8.9% faster; parent is a single run |
| Ten covered idle requests | 0.138 ms median (0.129–0.177) | 0.126–0.174 ms | zero parses/events; generation unchanged |
| Same-content duplicate | 0.464 ms median (0.460–0.505) | 0.651 ms | zero parse/publish/checkpoint; one-file verification hash |
| One-file body delta | 2.724 s median (2.718–2.735); 1 parsed | fail-closed | parent omitted `apps/architect-console/src/server/app.ts` from frozen Begin |
| Candidate body vs candidate initial | 32.2% of median initial | — | cold graph equality passed in all 3 repeats |

Candidate evidence: `/tmp/enola-product-resident-candidate-repeat-1790082291`.
Parent evidence: `/tmp/enola-product-resident-parent-1790081525`.

## Experimental Product body (unreviewed)

Root diagnostic `/var/folders/69/n9gknnz11gq1gwnvwt1rlt200000gn/T/enola-route-product-diagnostic-mzh5dtso`
used request-driven resident (`measurement` in that dump is benchresident, not
`graph watch`) on Product `a609c19f3861971930fae7b33dcb2950598953c5` with
binary provenance `experimental-1202855+eae759aefd0f`. Body:
**2 owners, 4 events, 0.711 s**, parsed 1, generation 1→2. This is a **single
unreviewed experimental** observation. It is not release or performance
acceptance.

## Contract

- `AuthoritativeFiles: true` and `MaxBeginBytes: 1MiB`.
- Actual NATS JetStream (`/tmp/enola-toolchain/bin/nats-server`) with
  `max_payload: 1048576`. Never reuse port **14232**.
- Independent consumer from the same Enola module as the producer (overlay
  build into the work dir). Do not reuse `/tmp/enola-product-observer`.
- Observer frames use JetStream broker timestamps
  (`time_base=broker_metadata_timestamp`) plus `consumer_end_ns`.
  Frozen Begin fields (`schema_version`, `scope_mode`, `owner_scope_count`,
  generations) are recorded on End frames.

Same-content duplicate notifications may hash the one notified file
(`HashedFiles=1`, `DirtyHashBytes>0`) to verify the bytes are unchanged.
Acceptance requires zero parses, zero `PublishedEvents` / `Checkpoints` /
`FactAssemblies`, `wire_messages=0`, and unchanged generation. Covered idle
requests still require every Work counter to be zero.

## Observer lifecycle

Start **one** `benchobserver` per NATS server **before** the first
`analyze`/`watch`/`resident` process. Wait for the READY file. Stop all child
processes on failure — including the editor process in `soak.py`. Messages are
pulled and applied in strict JetStream `Metadata.Sequence.Stream` order.

Toolchain: `/tmp/enola-toolchain/go/bin/go`, `/tmp/enola-toolchain/go/bin/gofmt`,
`/tmp/enola-toolchain/bin/nats-server`.
