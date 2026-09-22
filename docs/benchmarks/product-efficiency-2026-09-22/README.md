# Product resident efficiency — 2026-09-22

Frozen v2 request-driven resident comparison of Enola **3564785** (`Defer
resident input hash map copies`) versus parent **6430d25** (`Reuse unchanged
graph state records`). The current candidate additionally includes the frozen
scope completeness fix in the working tree. Unit tests are not a speedup claim.

## Product evidence

Three isolated candidate runs and one parent run used fresh NATS JetStream
servers, an independent ordered observer, and the same Product source/config.
The candidate runs include the scope fix; the parent run is retained as a
contract-failure baseline.

| Scenario | Candidate | Parent | Result |
| --- | ---: | ---: | --- |
| Resident initial, all consumer frames | 8.459 s median (8.390–8.608) | 9.288 s | candidate 8.9% faster; parent is a single run |
| Ten covered idle requests | 0.138 ms median (0.129–0.177) | 0.126–0.174 ms | zero parses/events; generation unchanged |
| Same-content duplicate | 0.464 ms median (0.460–0.505) | 0.651 ms | zero parse/publish/checkpoint; one-file verification hash |
| One-file body delta | 2.724 s median (2.718–2.735); 1 parsed | fail-closed | parent omitted `apps/architect-console/src/server/app.ts` from frozen Begin |
| Candidate body vs candidate initial | 32.2% of median initial | — | cold graph equality passed in all 3 repeats |

The parent body result is intentionally not treated as a timing result: it
failed closed when post-parse dependency discovery found an owner outside Begin.
The candidate succeeds with the broader frozen owner manifest, reparses only the
dirty/dependent files, and matches the cold body graph. Candidate evidence:
`/tmp/enola-product-resident-candidate-repeat-1790082291`. Parent evidence:
`/tmp/enola-product-resident-parent-1790081525`.

## Contract

- `AuthoritativeFiles: true` and `MaxBeginBytes: 1MiB` in the resident driver.
- Actual NATS JetStream (`/tmp/enola-toolchain/bin/nats-server`) with
  `max_payload: 1048576`.
- Independent consumer built from the **same Enola revision** as the producer
  (the private snapshot's benchobserver binary). Do not reuse
  `/tmp/enola-product-observer` or other v1 binaries.
- Record: initial, ten covered idle requests, same-content duplicate
  notification, one-file body edit of `packages/crypto/src/password.ts`,
  cold graph equality, first-batch / broker End / consumer End timings.
- `--repeat` bounds sequential resident lifetimes (default 1 until agreed).

Same-content duplicate notifications may hash the one notified file
(`HashedFiles=1`, `DirtyHashBytes>0`) to verify the bytes are unchanged.
Acceptance requires zero parses, zero `PublishedEvents` / `Checkpoints` /
`FactAssemblies`, `wire_messages=0`, and unchanged generation. Covered idle
requests still require every Work counter to be zero.

## Observer lifecycle

Start **one** `benchobserver` per NATS server **before** the first `analyze`/`resident`
process, keep it for the whole work directory, and stop it after the last cold
run. Do not share the failed-run broker on port **14232**. `resident.py
--observer-pid` fails as soon as that process exits.

Batches have `run_id` only. The observer registers each Begin on a persistent
`repo_id+context_id` Consumer and routes later batches/End by `run_id`, so
deltas keep `LastGeneration`. Messages are pulled and applied in strict
JetStream `Metadata.Sequence.Stream` order (buffered until the next sequence
is present); Consume callbacks are not used. `first_ns` / `first_batch_ns` /
`broker_end_ns` are JetStream broker timestamps
(`time_base=broker_metadata_timestamp`); `consumer_end_ns` is local apply
completion.

## Isolation

Private snapshots under `/tmp/enola-efficiency-2026-09-22-<rev12>/`. The
benchresident/benchobserver packages are copied there so they never enter the
tracked tree. Product mutations use `$WORK/product-live` cloned from
`/tmp/enola-product-benchmark-source`; that original tree is not written.
Dirty `mcp-arch.yaml` is overlaid onto the live clone. Historical result dumps
are not overwritten.

## Commands (not executed until agreed)

```
python3 docs/benchmarks/product-efficiency-2026-09-22/run.py --self-test
python3 docs/benchmarks/product-efficiency-2026-09-22/run.py --smoke   # tiny NATS fixture, not Product

# expensive: snapshot + go build of enola/driver/observer
python3 docs/benchmarks/product-efficiency-2026-09-22/run.py --prepare --build --rev candidate
python3 docs/benchmarks/product-efficiency-2026-09-22/run.py --prepare --build --rev parent

NATS=/tmp/enola-toolchain/bin/nats-server
# write nats.conf from nats.conf.template (PORT + STORE_DIR)
$NATS -c $WORK/nats.conf
$SNAP/bin/benchobserver nats://127.0.0.1:$PORT $WORK/consumer.jsonl
python3 docs/benchmarks/product-efficiency-2026-09-22/resident.py \
  --binary $SNAP/bin/enola --resident $SNAP/bin/benchresident \
  --observer $SNAP/bin/benchobserver --nats nats://127.0.0.1:$PORT \
  --root $WORK --repeat 1 --rev 3564785 \
  --source /tmp/enola-product-benchmark-source
```

Toolchain: `/tmp/enola-toolchain/go/bin/go`, `/tmp/enola-toolchain/go/bin/gofmt`,
`/tmp/enola-toolchain/bin/nats-server`.
