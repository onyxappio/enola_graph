# Thirty-minute scripted watch soak on Product

Stage5 binary (production sources match `f0101f1`), full graph profile, NATS
JetStream, isolated Product copy with a synthetic three-file editing module.
This is scripted traffic in Product, not the requested independent AI feature
implementation and not final Product acceptance. Changes are confined to the
isolated copy. Earlier broken-editor attempt is excluded separately.

88 successful editing operations over 29m 1s; 75 completed generations: one initial
and 74 delta. Zero editor errors, batch-count mismatches, incomplete Begin/End
pairs at selection or abandoned Begins. Final graph hash equals cold exactly;
45,099 scoped input files unchanged across cold comparison. No internal watcher
watermark exists: final correctness is observed cold equality, not proof the
watch queue was drained by a watermark.

| Delta-only metric | Median | Mean | Range |
|---|---:|---:|---:|
| Begin → End, ms | 971.186 | 1154.444 | 616.230–2181.789 |
| Begin owners | 2 | 293.257 | 1–620 |
| Parsed files | 1 | 1.135 | 0–2 |
| Batches | 1 | 65.797 | 1–138 |
| Node records sent | 12 | 3450.635 | 2–7294 |
| Edge records sent | 14 | 4131.838 | 1–8735 |
| JSON payload bytes | 8500 | 2868708 | 1971–6063507 |

74 delta completions in 30 minutes is about 2.47/minute under an editor configured
for 20-second intervals; no-op/coalescing means edits and generations are not a
one-to-one series. Delta traffic totals 212,284,409 bytes including Begin/End JSON,
excluding broker framing. Records are retransmitted owner contributions, not
new graph entities. Final graph: 69,719 node records and 147,967 edge records.

Nearest-prior-save → consumer median 6.504 s (range 3.624–9.637 s), including the
fixed 5 s collection window. This is correlation, not per-edit causality. The
last edit was graph-neutral enough that the final matching suffix began before
it; the resulting negative convergence offset is **not** a negative processing
latency or proof of watcher responsiveness.

Watch spawn → initial consumer apply 13.634 s; harness-observed initial 13.937 s.
Setup before watch 43.235 s is separate. RSS sampled 170 times: median 988.844 MiB,
maximum 1134.453 MiB; these samples alone establish no long-term leak verdict.

Planned watch restart observed 40.52 ms process downtime. NATS bounce requested 3 s,
observed 8.181 s; it missed the publication window, watch stayed alive and 24 later
generations completed. **Recovery during publication remains untested.** A
future fault run must interrupt an observed open transaction, not infer success
from a broker bounce between generations.

[Full report and all generation rows](v304-product-soak30m.json) ·
[Delta-only statistics](v304-product-soak30m-deltas.json).
