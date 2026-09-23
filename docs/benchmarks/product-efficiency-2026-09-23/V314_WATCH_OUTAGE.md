# Product watch: broker outage recovery

The stage8 production binary recovered to an exactly cold-equivalent graph after
a 12-second NATS outage interrupted a replacement. Recovery required the harness
to restart the watcher after its transport timeout; the watcher did not survive
in the same process. This is functional evidence, not a speedup comparison.

## Run

- Full Product extractor profile, isolated copy, scripted edits every 3 seconds.
- Fixed watch collection window: 5 seconds; 300-second requested soak.
- Total harness time including setup and cold comparison: 332.494 seconds.
- 87 edits, zero editor errors; 22 completed generations (1 initial, 21 delta).
- Binary SHA256: `4095e9c7653f3da1b05b32a974b75a7b6781c79ee5ba97bd0201c57ea198e36a`.
- Go 1.27.1; stage8 production sources, as recorded in
  [the integration report](V314_WATCH_RETRY_INTEGRATION.md).

## Interrupted transaction

The observer received generation 14's Begin with base generation 13 and 619
owners. The broker stop request followed that Begin by 19.723 ms. NATS logs
confirm shutdown and restart. Publishing batch 8 of that exact run failed with
`context deadline exceeded`; the watcher exited with code 2. No successful End
for that run appears in the observer lifecycle.

After the harness restart, the next Begin still used base generation 13 and
target generation 14, with a new run ID. It completed successfully. Nine
generations completed after the outage. Thus the interrupted attempt did not
advance the completed generation. This proves restart/reconciliation recovery,
not successful replay of the abandoned run itself.

The stop-request-to-ready interval was 12,146.284 ms. Exact stop confirmation and
ready timestamps are retained in the raw report and independent audit.

## Final checks and limits

The final consumer graph hash equals the fresh cold graph hash:
`c924df9be98ab2ae0431c61659212051465c4785328ada8353993f928d351ecc`.
All 45,110 checkout inputs remained stable across the cold comparison. The
observer/harness protocol checks passed; completed-frame batch-count mismatches
were zero. Four Begin attempts were superseded without End, including the
outage attempt; these must not be counted as completed replacements.

The `--no-restart-watch` flag disabled planned restarts only. The harness still
restarted the watcher after its unexpected transport exit, as recorded in
`lifecycle_events`. This run used a synthetic scripted module inside Product;
the real AI-editor prolonged watch experiment remains separate and unfinished.
The raw report deliberately retains its conservative generic acceptance labels;
the timestamp and error-log audit here supplies the run-specific evidence.

Evidence: [raw report](v314-watch-outage5m.json),
[independent audit](v314-watch-outage-audit.json), and
[observer lifecycle](v314-watch-outage-lifecycle.jsonl).
