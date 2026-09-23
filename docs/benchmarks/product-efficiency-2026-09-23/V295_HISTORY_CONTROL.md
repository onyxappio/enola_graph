# v295 Product history control — 2026-09-23

This is an unmodified-main control, not an accepted optimization result. The
experimental scope patch remains unpublished after independent export-visibility
regressions. The control establishes version-matched correctness and diagnostic
costs for its eventual replacement.

## Inputs and measurement

- Enola commit: `340fb9e3e91d8707fa0757f142f9b8546094f86d`, extractor semantics v295.
- Binary SHA-256: `ea733b898ad42b9763623bd1fb2324f307693044d5a8f711ca98f1c17c60715b`.
- Product parent: `fec1eac346c48dbc68072803d89b2e4709f9bd42`.
- Product target: `9fc7ae5b4c3fc4fc266b24b1df0ef2bfcb1f7030`.
- Fresh CLI processes, persistent state from parent initial to target delta;
  independent empty state for the target cold oracle. Authoritative file scope,
  file sink, `scope-bench` context and `product-scope` repository identity.
- The pinned Product policy is applied by the existing history harness. Its hash
  is recorded in [the result](v295-history-control.json).
- One sequential run on a shared host. A worker disk scan and cleanup of completed
  benchmark source copies overlapped the measurement. These numbers are not an
  isolated performance comparison or broker-acknowledged completion acceptance.

## Results

| Phase | CLI seconds | Full file parses | Begin owners | Events | Event-file bytes |
|---|---:|---:|---:|---:|---:|
| Parent initial | 31.386 | 4,016 | 8,455 | 2,654 | 80,743,444 |
| Target delta | 17.574 | 1,601 | 2,295 | 1,379 | 43,125,563 |
| Target cold oracle | 31.424 | 4,018 | 8,458 | 2,656 | 80,789,020 |

The delta took 56.0% of the parent initial time. This remains an intermediate
result, not the requested fast-delta endpoint. Full file parses exclude export
summary scans. Event bytes include the file-sink envelope, not just broker payload.

All three replacement manifests passed the existing protocol validator. Applying
initial plus delta produced exactly the cold target graph under the harness's
complete graph comparison. Both graph hashes are:

```text
1142e5831b18d354066fec2b174dc95bf488f562d58f9303131a1f907c9dc695
```

CLI timing comes from the subprocess timer inside the existing scope harness;
it excludes Python event decoding and graph comparison. Event counts are scoped
to each phase, rather than cumulative initial-plus-delta counts.

## Evidence and limits

Machine-readable data: [v295-history-control.json](v295-history-control.json).
Local raw events, state, phase summaries and checkpoints:
`/tmp/enola-v295-control-checkpoint`. The wrapper used the existing history and
scope harnesses and saved each successful phase before validating later phases.

An earlier attempt ran out of disk while cloning the target cold source. Its
completed initial/delta graph was separately verified against cold, but its CLI
timings were not saved; those timings are not reconstructed from file timestamps.
The table above comes from a subsequent complete run.

The v293 historical baseline and the old experimental snapshot-2 figures have
different extractor semantics and are context only, not controls for v295
optimization acceptance. Acceptance still requires a corrected candidate,
matched repeated measurements, historical regression coverage and broker/watch
measurements, alongside preserved frozen Begin and failed-run recovery behavior.
