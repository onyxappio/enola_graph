# Product efficiency — 2026-09-22

Frozen v2 efficiency tooling. **Request-driven resident** measurements
(`resident.py` / `benchresident`) are separate from production **`graph watch`**
(`watch.py`). Neither is performance acceptance until a coordinator-owned
Product run is agreed.

## Pins and snapshots

`--rev candidate` / `--rev parent` are refused. Pass an explicit SHA or `HEAD`.
Prepare always creates a **fresh** directory; existing snapshots under
`/tmp/enola-efficiency-2026-09-22-*` are never deleted.

| Kind | Meaning |
| --- | --- |
| `committed` | Clean checkout of the requested SHA. Snapshot commit equals that SHA. |
| `experimental-patch` | Dirty worktree overlay on `HEAD`. Provenance records the HEAD SHA, diff hash, and `snapshot_contains_uncommitted=true`. This is **not** equal to the committed revision. |

```
python3 docs/benchmarks/product-efficiency-2026-09-22/run.py --self-test
python3 docs/benchmarks/product-efficiency-2026-09-22/watch.py --self-test

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

## Tiny smokes (not Product)

Require `--binary` or `--snapshot`. The harness will not silently build a
hardcoded historical SHA. Observer is built into the work directory via Go
`-overlay` so the snapshot tree is not modified.

```
python3 docs/benchmarks/product-efficiency-2026-09-22/run.py --smoke \
  --snapshot /tmp/enola-efficiency-2026-09-22-35647859c639 \
  --work /tmp/enola-eff-smoke-new

python3 docs/benchmarks/product-efficiency-2026-09-22/watch.py --smoke \
  --snapshot /tmp/enola-efficiency-2026-09-22-35647859c639 \
  --watch-every 1s \
  --work /tmp/enola-watch-smoke-new
```

`--smoke` on watch.py uses a tiny TypeScript fixture and, when `--watch-every`
is left at the CLI default 5s, shortens the window to 1s for a bounded check.
The production default remains **5s**. Product invocation is:

```
python3 docs/benchmarks/product-efficiency-2026-09-22/watch.py \
  --snapshot $SNAP --watch-every 5s \
  --source /tmp/enola-product-benchmark-source \
  --scope-config docs/benchmarks/product-efficiency-2026-09-22/product-graph-scope.yaml \
  --work /tmp/enola-watch-product-new
```

That Product path is isolated (clone + overlay). It is not executed as part of
this tooling change.

Watch smoke records edit timestamps, broker Begin / first-batch / End, consumer
completion, idle and same-content duplicate (no events), burst writes inside the
collection window, completed generations, frozen v2 complete Begin, and cold
graph equality. Child processes are stopped on failure.

## Request-driven resident (not `graph watch`)

```
python3 docs/benchmarks/product-efficiency-2026-09-22/resident.py \
  --binary $SNAP/bin/enola --resident $SNAP/bin/benchresident \
  --observer $SNAP/bin/benchobserver --nats nats://127.0.0.1:$PORT \
  --root $WORK --repeat 1 --source /tmp/enola-product-benchmark-source
```

`resident.py` provenance sets `measurement_kind=request-driven-resident`.
`watch.py` sets `measurement_kind=production-graph-watch`.

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
processes on failure. Messages are pulled and applied in strict JetStream
`Metadata.Sequence.Stream` order.

Toolchain: `/tmp/enola-toolchain/go/bin/go`, `/tmp/enola-toolchain/go/bin/gofmt`,
`/tmp/enola-toolchain/bin/nats-server`.
