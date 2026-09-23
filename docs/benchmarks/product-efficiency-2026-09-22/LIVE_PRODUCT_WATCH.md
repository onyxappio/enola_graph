# Product watch with a live AI editor — 2026-09-22

The real Product watch experiment completed with final cold equality. It exposed
an overly broad replacement scope on TypeScript file additions; the proposed
membership optimization was not part of this baseline and remains under review.

## Setup and evidence

- Enola production source: `b611e22e446c57a419339c401ec767d7cdedeca0`.
- Product: `a609c19f3861971930fae7b33dcb2950598953c5`, isolated disposable checkout.
- Production default extractors (no `extractors` override), explainers/renderers off,
  existing Product exclusions; real `graph watch`, fixed 5-second collection window.
- Separate Claude Code Opus 5 editor: invitation deep-link parser and shared helpers,
  only `apps/mobile/src/deeplinks/**`; no production Product commit or push.
- 5 files changed, 11 filesystem observations (4 additions, 7 modifications).
  Observed active editing spanned 230.896 seconds, followed by idle observation.
  This was not 30 minutes of sustained editing; no deletion or rename was exercised.
- Harness elapsed: 1838.760 seconds including setup and final verification.
  Requested observation duration: 1800 seconds. The timer begins before setup;
  do not treat this as a separately instrumented 1800-second active workload.
- Editor focused isolated tests: 22/22 passed; not the full mobile suite, no tsc check.
- No watcher/broker restart events; no deliberate outage or recovery acceptance.

## Completed generations

Times below are broker Begin to broker End, not complete initial wall time or
causal save-to-graph latency. Consumer delay is the local test consumer, not Codata.
Owners are the announced replacement scope, not parsed-file counts.

| Generation | Begin owners | Begin → first batch (s) | Begin → End (s) | End → consumer (ms) |
|---|---:|---:|---:|---:|
| 1 | 8645 | 3.261 | 6.103 | 88.425 |
| 2 | 8646 | 0.679 | 3.500 | 81.031 |
| 3 | 8647 | 0.554 | 3.624 | 106.086 |
| 4 | 18 | 0.518 | 1.016 | 1.000 |
| 5 | 1 | 0.339 | 0.859 | 0.396 |
| 6 | 18 | 0.355 | 0.842 | 1.071 |

The two completed file-addition generations replaced 8646–8647 owners. Later
edits replaced 1–18 owners. The current planner's unconditional membership fallback
explains the broad scope; it does not prove every owner was reparsed.
One earlier attempt at generation 2 published Begin but no End. A later attempt
completed from base generation 1. This abandoned attempt did not produce a
completed consumer generation; its precise failure cause was not logged.

## Correctness and stability

- Final selected watch generation 6 exactly matched cold normalized graph hash:
  `17d786c72a1db2b92893bae783c022125fe7e0d052014bd3d7143dafcd7dd3d8`.
- Consumer protocol validation accepted every completed generation with complete
  frozen scope. Final cold parsed 4146 files and reported 5070 owners published;
  these counters are distinct from the initial Begin inventory count of 8645.
- Manual inventory fence: 45110-file inventories identical at editor completion
  and after cold (two checkpoints, not continuous no-write proof). Final whole-inventory diff had no changes outside the allowlist.
  Inventory excludes the harness's listed metadata/build/dependency directories;
  this is not a proof about files excluded from that inventory.
- No new completed watch generations after the last edit's generation 6.
- 176 RSS samples: minimum 660.3 MiB, median 747.8 MiB, maximum 917.4 MiB.
  Last 60 samples ranged 742.6–747.8 MiB; final RSS 742.6 MiB. No upward idle
  trend in this observation, not a long-term memory-leak proof.

## Measurement limits and corrections to the frozen raw report

The frozen harness predates its final wording/fence fixes. Keep raw evidence
unchanged and apply these corrections when reading it:

- `save_ns_basis` incorrectly says fsync; this external editor run uses filesystem
  mtime and a 1-second metadata poll, with no durability guarantee.
- Nearest preceding save pairing is correlation, not causation. The raw
  `convergence.final_convergence_ms = -42940.856` means generation 5 already matched
  the final graph before later graph-neutral edits. It is not negative processing
  time or causal convergence latency. Generation 6 also matched cold.
- Quiescence is inferred from stable observable traffic; there is no internal
  watcher watermark proving its queue drained. Final frozen-input cold equality
  supplies the correctness evidence for the selected result.
- RSS samples are finite and editor load was brief. Recovery, sustained edits,
  rename/delete churn and real Codata application latency remain untested here.

## Follow-up

Bound source-membership invalidation using cached import resolution changes and
candidate/route dependencies, with complete planning before Begin. Review found
preview-ordering and prior-inventory-versus-owner-cache gaps in the first draft;
fix and prove those with cold-equality regressions before claiming a speedup.
Replay the same Product change set on the candidate and compare scope and timing.

## Traffic and update frequency (broker audit)

A separate read-only logical audit of a copied JetStream store read all 13533
messages in 5.231 seconds, with zero missing-sequence errors. Original store was
not reopened. Audit server used a fresh port and was stopped afterwards.
Full per-run records are in `live-product-watch-traffic.json`.

During 230.896 seconds between first and last observed edits, 11 filesystem
observations give a mean inter-observation interval of 23.09 seconds. Five
completed deltas have a mean inter-completion interval of 53.50 seconds. These
frequencies reflect the editor workload, not a watcher scheduling SLA. Six delta
attempts published Begin: five completed and one abandoned. Four of the five
completed deltas changed canonical graph content; generation 6 equaled generation 5.

| Delta | Owners | Parsed files | Batches | Node records | Edge records | JSON payload MB |
|---|---:|---:|---:|---:|---:|---:|
| 1 | 8646 | 14 | 2690 | 69317 | 171967 | 78.228 |
| 2 | 8647 | 1 | 2690 | 69327 | 171992 | 78.238 |
| 3 | 18 | 18 | 34 | 517 | 2160 | 0.799 |
| 4 | 1 | 1 | 1 | 10 | 24 | 0.011 |
| 5 | 18 | 1 | 34 | 517 | 2159 | 0.798 |

Successful deltas totaled 5449 batches / 5459 messages including Begin and End,
158.075 MB JSON payload, 139688 node records and 348302 edge records. These are
transmitted replacement records, not counts of newly created graph entities;
wide scopes retransmit large amounts of unchanged content. Per successful delta,
mean Begin-to-End was 1.968 seconds and median 1.016 seconds. The abandoned attempt
added 2691 messages / 78.229 MB without advancing completed generation. Initial
and final cold traffic are excluded from these delta totals. MB means decimal
payload bytes and excludes NATS headers/protocol overhead.
