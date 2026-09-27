# Test-inclusive Product historical correctness

All three pinned first-parent transitions passed: 24 CLI calls, six revision checks with exact evolving/cold candidate/cold baseline graph hash equality. All six unchanged calls had zero parses, zero wire messages, unchanged generation and byte-identical state. Runtime: 4859ee3.

| Scenario | Delta parsed files | Correctness |
|---|---:|---|
| source | 51 | PASS |
| manifest | 178 | PASS |
| tsconfig-addition | 27 | PASS |

These are correctness-only historical pairs, not timing evidence or a history ending at current remote main. Source, manifest and tsconfig-addition change 19, 15 and 24 paths respectively; scope is distinct from parses. Whole-extractor fallbacks are preserved in receipts.

The copied validator was audited: chain length and transition count now derive from the pinned scenario; revision ordering, initial-kind, required gates, clean worktrees, binary/policy/observer/broker pins, no-op invariants and cold equality remain required. No other Stage19-specific cardinality remains. The original downstream validator failure is retained under harness-correction; the fresh reruns do not replace that evidence.
