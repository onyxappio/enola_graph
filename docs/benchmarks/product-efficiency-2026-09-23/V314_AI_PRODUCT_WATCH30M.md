# Real AI editor: Product watch, 30 minutes

The stage 8 watch completed without an observed exit or restart. Its final completed graph exactly matched a fresh cold analysis of stable final inputs. This validates this experiment, not general long-running acceptance or the newer stage 9 optimizations.

The external Claude editor implemented disposable opt-in deep-link diagnostics in six files under `apps/mobile/src/deeplinks`. It made two additions and four net modifications. The harness observed ten save changes over 86.939 seconds; the remaining watch period was mostly idle. This is **not** thirty minutes of continuous AI editing. The editor reported 60 passing tests using a separate Vitest mirror; Product typechecking and lint were not run. No disposable feature changes were merged into Product or Enola.

## Configuration and provenance

- Isolated Product checkout; full production extractor profile; NATS JetStream.
- Binary SHA256: `4095e9c7653f3da1b05b32a974b75a7b6781c79ee5ba97bd0201c57ea198e36a` (stage 8, Go 1.27.1).
- Fixed collection window: 5 s, not a debounce. Changes during analysis queue for a subsequent window.
- Requested watch observation: 1,800 s. Entire harness, including setup and final cold comparison: 1,852.34 s.
- No requested broker outage or planned watch restart. The harness can restart unexpected exits, but none was observed in this run.
- Shared host with concurrent functional tests; this is a robustness experiment, not a quiet paired performance comparison.

## Traffic and latency

Initial watch launch to consumer application was 10.738 s; initial Begin to End was 8.028 s. These intervals are different and must not both be called full initial time.

| Delta generation | Begin owners | Parsed files | Begin → End, s | JSON payload bytes | Node records sent | Edge records sent |
|---|---:|---:|---:|---:|---:|---:|
| 2 | 618 | 1 | 1.310 | 6,061,528 | 7292 | 8730 |
| 3 | 2 | 2 | 0.767 | 151,066 | 112 | 403 |
| 4 | 3 | 2 | 0.719 | 162,784 | 125 | 426 |
| 5 | 2 | 2 | 0.715 | 16,455 | 19 | 29 |

Across four deltas, Begin-to-End median was 0.743 s and mean 0.878 s. This excludes the five-second collection window and pre-Begin planning. Total delta JSON traffic was 6,391,833 bytes, with 7,548 node and 9,588 edge records. Replacement records are not newly created graph entities.

Time from the latest observed filesystem save before a Begin to consumer application ranged from 6.017 to 6.775 s. The pairing is observational, not causal; filesystem mtime is not an fsync/durable-save timestamp, and the one-second poll can miss intermediate saves. Do not label this a proven per-edit latency distribution.

## Scope finding

The first delta included the new TS file plus 617 Markdown owners. An independent read-only broker audit compared every per-owner node/edge JSON record (including IDs, occurrence and properties, excluding only run/batch envelopes) with the initial contribution. All 617 Markdown contributions were identical: 16,011 unchanged records were resent. [Audit](v314-ai-watch30m-md-audit.json).

The current small TS+Markdown resident fixture does narrow correctly, so this is a Product/full-profile investigation. A proposed missing-hash policy explanation was retracted after code inspection; the specific cause is still unproven. Any narrowing must preserve captured-input fences and cold equivalence.

## Correctness and limitations

- Five completed generations: one initial and four deltas. Zero abandoned or open Begin/End pairs, zero reported batch-count mismatches.
- No published generations after the editor finish signal.
- Final graph hash on both watch and cold: `9c664ab8ca15c08b6c25ba20dfe00096ca35fc921fadb37391efde40b433f47f`.
- Whole isolated checkout stability: 45,108 files hashed before and after cold; zero changes.
- 176 RSS samples: 956,256–1,082,288 KiB (about 934–1,057 MiB). A single mostly idle run does not establish absence of long-term memory growth.
- No watcher watermark exists. Traffic quiescence cannot prove the internal queue drained; acceptance here rests on exact cold equivalence after input stabilization.
- No broker outage was attempted in this experiment; see the separate outage report for that coverage.

[Raw report](v314-ai-watch30m.json) · [Begin/End lifecycle](v314-ai-watch30m-lifecycle.jsonl)
