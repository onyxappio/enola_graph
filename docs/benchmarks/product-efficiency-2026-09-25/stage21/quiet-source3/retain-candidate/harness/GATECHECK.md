# Negative controls for the v8 comparator and the correctness verdict

No Product process was run for any of this. Every fixture is built from the real v7
cohort rows (`/tmp/enola-stage21-ablate/work5/ablation-metrics.json`) or from synthetic
rows in the shape the profile harness writes, with exactly one deliberate violation each.
The point is to show each gate can fail, before a real run makes its verdict matter.

## compare8.py — `gatecheck8/`, builder `build-fixture.py`

| fixture | result |
|---|---|
| `positive` | both scenarios interpretable, baseline interpretable, `FULL_COHORT_INTERPRETABLE True`, exit 0 |
| `negative-digest-not-removed` | fails `retain_delta_fingerprinted_twice_one_read_one_write` and `retain_delta_compared_bytes_once` |
| `negative-no-byte-comparison` | fails `retain_delta_compared_bytes_once` |
| `negative-proof-not-produced` | fails `retain_produced_and_promoted_a_proof` |
| `negative-two-decodes` | fails `retain_delta_decoded_the_state_exactly_once` |
| `negative-no-decode` | fails `retain_delta_decoded_the_state_exactly_once` |
| `negative-duplicate-retain-row` | arm refused: scenario reported `incomplete` with `duplicate_measured_rows`, cohort not interpretable |

The retain arm is synthesized from the ablated row, which is the row with a distinct run
id and the same graph. Its `proof_written` / `proof_promoted` observations are supplied by
the fixture rather than measured — the one expectation these fixtures state instead of
reusing evidence, and the reason `negative-proof-not-produced` exists.

### What the real v7 rows already establish

Counted from `work5`, not claimed:

| arm | committed-state reads | fingerprints (total / read / write) | decodes | byte comparisons |
|---|---|---|---|---|
| Stage20 baseline (pre-proof) | 1 | 2 / 1 / 1 | 1 | 0 |
| frozen (proof) | 2 | 3 / 2 / 1 | 1 | 0 |
| retain (expected) | 2 | 2 / 1 / 1 | 1 | 1 |

So option C is expected to put the digest count back where the pre-proof binary had it
and leave the second read in place. The comparator gates that shape; it does not assert
any latency or RSS consequence of it.

## retain-noop-verdict.py — `verdictcheck/`, builder `build.py`

| fixture | result |
|---|---|
| `positive` | `CORRECTNESS PASS` |
| `negative-missing-state-field` | FAIL `o2-noop_state_bytes_unchanged` (absent field, not a false one) |
| `negative-wire-message-published` | FAIL `o3-noop_published_no_wire_messages` |
| `negative-noop-parsed-files` | FAIL `o2-noop_parsed_no_files` |
| `negative-generation-advanced` | FAIL `o3-noop_generation_unchanged` |
| `negative-missing-run` | FAIL `o4-body_present_exactly_once`, `o4-body_exit_zero` |
| `negative-nonzero-exit` | FAIL `o4-body_exit_zero` |
| `negative-duplicate-run` | FAIL all six `o3-noop` checks, including `present_exactly_once` |

Hashes for every file named here are in `HARNESS_MANIFEST.txt`.
