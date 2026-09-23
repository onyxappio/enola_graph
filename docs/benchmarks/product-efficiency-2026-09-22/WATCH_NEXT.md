# Watch priority checkpoint

The acceptance workload is a long-running production graph watch session while a
separate agent edits an isolated Product checkout. Keep the five-second
collection default, and distinguish collection delay from active analysis,
broker acknowledgments and consumer application.

## Resolved since the previous checkpoint

- **First-End race — FIXED, and the fix is labelled honestly.** The burst check
  used to take the first post-edit completion, so edits spanning a window could
  leave another generation queued. After freezing inputs, both harnesses now
  take the **final** completed generation for the exact watch context, gated on
  stable whole-checkout input, no *active* open Begin, and an unchanged frame
  count / `last_seq` / lifecycle count for `max(2*window, window+2s)`.
  Begin/End records come from the observer's new `OBSERVER_LIFECYCLE_FILE`
  (optional; completed-frame telemetry fields are additive and existing fields
  remain compatible with `resident.py`), because `consumer.jsonl` only gains a line when
  a generation *completes*.

  This is **heuristic quiescence, not an internal drain proof**. Reports carry
  `internal_drain_proved=false`, `requires_watcher_watermark_for_proof=true` and
  a `quiescence_limitations` list: there is no watcher watermark to read, an
  analysis slower than the margin whose Begin is unpublished is unobservable,
  and a Begin timestamp records publication, not input capture. Acceptance rests
  on cold equality of the final generation after inputs are frozen.
- **Aborted Begins no longer hang the wait.** An open Begin superseded by a
  later Begin, or predating a watcher restart, is classified `abandoned` and
  reported rather than waited on; only the head can block. Uncompleted runs are
  never silently dropped.
- **Latency relabelled as non-causal.** Nearest-prior-save pairing is
  correlation, so the columns are `time_since_latest_observed_save_at_begin_ms`
  / `..._at_consumer_ms` with run and generation ids and
  `pairing_is_causal=false`. The strongest measure is `final_convergence_ms`:
  last observed edit → start of the observed final matching suffix (first
  completed generation equal to the final cold graph and never contradicted
  after). It carries `is_causal_claim=false`: a graph-neutral edit makes an
  earlier generation match and shortens it. `fsync` completion is the chosen
  reference, **not** a
  visibility barrier — watchers can read bytes before `fsync` returns, so
  negative offsets are labelled, not clamped.
- **Input scope widened, and checked on both sides of cold.** The old extension
  glob silently ignored `.md`, `.py`, `.tf`, `.swift`, `.mts`, `.cts`, `.vue`,
  `.svelte`, dotfiles and config, so it could not support an all-inputs claim.
  Integrity now hashes the **whole isolated checkout**, frozen before the
  quiescence wait and re-read after cold analyze (`input_stability_across_cold`)
  so equality is never reported across inputs that moved. Out-of-allowlist
  differences are computed against the **initial** baseline, not by rehashing
  the final tree, so a deletion is still caught. The live poll is seeded from
  the declared subtree, never from the full baseline, so the first tick does not
  mass-report every outside path as deleted.
- **Cold equality was unachievable, then vacuous.** Cold analyze ran with
  `--repo-id watch-bench-cold` while the watch ran with `watch-bench`; facts are
  tagged with the repo id (`tagRepo`, `internal/graphsession/session.go`), so the
  ids differed and the hashes could never match. Both now use one `REPO_ID` and
  isolate only via `--context` / `--state-dir`. Separately, the tiny fixture
  mutated a literal (`a = 1` -> `a = 2`), which leaves the normalized graph
  identical, so cold equality would have passed even if the delta were dropped.
  The fixture now changes the exported symbol set and reports
  `mutation_changed_graph`.
- **Provenance no longer equates a commit with dirty source.** Harness packages
  are injected into every snapshot, so `vcs.modified=true` always. Provenance
  splits `product_source_equals_committed_rev` (verified) from
  `snapshot_tree_equals_committed_rev` (always false), and the build fails closed
  if a binary is unstamped or stamped with a different revision.
- **`soak.py` added.** Configurable `>=30m`, real 5s window, scripted editor
  process, mixed edit kinds (add/remove export, edit body, add/remove import,
  create/rename/delete file), per-generation latency, RSS sampling, planned
  watch and broker restarts, and cold equality after quiescence.
- **External-editor mode added** (`--external-editor`) for a real parallel AI
  editor: the harness does not write the source, publishes `READY.json` with the
  isolated repo path, artifact paths and the `STOP-EDITOR` finish trigger, and
  keeps watching for the full duration after the finish signal while reporting
  `active_editing_s` vs `idle_s`. Live change capture polls only the declared
  `--editor-allowlist` subtree by metadata (hashing just files whose mtime/size
  moved) so it does not distort the latency being measured; out-of-allowlist
  changes are caught by the full final inventory and reported.
- **`--profile full|typescript` added.** `full` omits the `extractors` key so the
  binary uses its production default set (`PRODUCT.md:214`) and fails closed if
  the effective config still pins it. A Product run must not silently measure a
  narrower graph than production builds.

## Watcher facts this tooling encodes

- The collection window is **fixed, not a debounce**: it opens on the first event
  and closes `--watch-every` later; later edits do not extend it.
- Edits arriving during analysis **queue**, and the next window opens only after
  that analysis finishes.
- Broker errors **terminate** the watch process; there is no degraded mode.

## Validation so far (tiny fixture only)

90s soak smoke, real 5s window, snapshot `b611e22`: 11 edits, 10 generations,
quiescence selection, cold equality equal, frozen complete Begin on every
generation, idle and duplicate notifications produced no events, whole inventory
stable across the cold run, convergence 8279.8 ms (2128.5 ms in an earlier
smoke: it moves with where the last edit lands inside the fixed window, so it is
an observed interval, not a bound). Medians: latest-observed-save->Begin 5030 ms (min 1788 ms for an edit
landing inside an open window), Begin->first batch 39.5 ms, Begin->End 83.0 ms,
End->consumer 0.29 ms, write->durable 1.07 ms.

**Broker recovery still not demonstrated.** The outage is now taken *during
publication* — the harness waits for an in-flight Begin before killing the
broker — and in the last smoke the watcher **survived** it: no `watch-exited`
event, so the documented terminate-on-broker-error path was not reproduced. The
run records that as an observation (`broker_outage_verdict`), never as recovery
acceptance; `broker_recovery_acceptance` stays "untested until observed in a
Product run". A 149 ms bounce between publishes proves even less and is no
longer treated as evidence.

## Still open

The live AI editor Product experiment has now completed: see
[LIVE_PRODUCT_WATCH.md](LIVE_PRODUCT_WATCH.md). Final cold equality passed after
11 observed changes across 5 files, with roughly 3.85 minutes of active editing
and a requested 30-minute observation window. No restart or deliberate outage
was exercised. The follow-up five-file burst now replaces 911 owners instead
of 8647 and reports 18 TypeScript-session parses instead of 293; exact cold equality and stable input
fences passed. See [REBOUND_PRODUCT.md](REBOUND_PRODUCT.md). Three subsequent
baseline replays at the pushed checkpoint reproduce the 911-owner scope; their
timing spread is in [MDINTENT_COMPARISON.md](MDINTENT_COMPARISON.md). Of those
911 owners, 893 are Markdown documents. The final Markdown-scope candidate has
now passed three Product cold-equality replays with 18 owners and 12.09× less JSON
payload. Complete delta latency has not improved; Markdown still compiles once
per required preview. The repeated timing comparison and fresh baseline control
are recorded in the same report.

Automatic per-generation messages, batches, node/edge records, payload bytes and
End completeness counters are available. Startup-to-frame observation,
startup-to-consumer-apply and Begin-to-End are separate fields; zero change in
graph cardinality does not imply graph equality. Tiny watch and external-editor
smokes passed; Product use of this new telemetry is being measured separately.

Remaining checks: repeated baseline/candidate measurements, sustained mixed edits,
rename/delete scenarios, an interruption that actually kills the watcher during
publication, raw-config scope and fresh-CLI state-loading costs.

## Historical diagnostic correction

The f5f4970 Product10 run validated the first five transitions and stopped on the
sixth due to a required-owner assertion. Independent replay of saved generation7
and the corresponding cold output produced equal graph hashes and zero differing
contribution owners. The two reported JSON paths had appeared in prior Begin
inventory scopes but never emitted nodes or edges. The failed assertion is not
proof of lost graph contributions. Keep retirement checks for formerly nonempty
owners and separately decide how never-contributing inventory identities belong
in the protocol contract. Remaining four transitions were not executed.

Do not interpret this checkpoint as completion of performance acceptance.
