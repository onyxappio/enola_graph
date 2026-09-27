# Stage36 guarded timing — computed acceptance, review pending

Six counterbalanced pairs; same Product revision, full test-inclusive policy and Stage33 baseline. Fresh CLI through process exit including producer ACKs; no daemon/watch claim.

| Scenario | Baseline median (range), s | Candidate median (range), s | Change |
|---|---:|---:|---:|
| initial | 11.300 (11.233–11.708) | 11.250 (11.032–11.775) | -0.44% |
| noop | 2.045 (2.009–2.124) | 2.043 (2.040–2.121) | -0.11% |
| body | 4.141 (4.076–4.451) | 3.777 (3.728–3.867) | -8.79% |
| structural | 4.232 (4.167–4.415) | 4.231 (4.164–4.385) | -0.03% |

Body parse count 21 -> 1; candidate body/initial ratio median 0.335. All six body pairs won (7.32–13.11% improvement). All graph equality/no-op checks passed. Computed host/pressure/power gates passed; no median time or RSS gate exceeded. Detailed first batch, broker End, owner scope, RSS and spread are retained in timing-comparison.json.

This meets the preregistered limited Stage36 engineering gate, not the overall near-zero fresh no-op or watch objective. No main publication yet. Independent gate review requested before publication. Raw host/pressure logs remain at /tmp/enola-local-surface-guarded; file digests below preserve evidence identity.
