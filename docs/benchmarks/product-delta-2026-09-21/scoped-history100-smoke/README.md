# Scoped-delta Product history smoke

Intermediate frozen production source: `/tmp/enola-scoped-stage-source`.
Single real native-watcher/NATS run under simultaneous full tests and review; these times are not isolated performance acceptance.

- Initial: 32.123249 s, 4,128 TS parses.
- Already-running idle: 0.000130 s, zero work/events.
- 100-path history: 17.500154 s, 414 TS parses: 16 new sources, 34 changed source bodies, 364 resolution-induced whole-file reparses. No global context fallback. Coverage catch-up added no parses.
- All three checks passed, including transported target graph equality with a separate cold analysis.

Full-stage suite found two path-contract violations, assigned for correction. Independent review also reproduced stale Svelte alias edges from omitted root/nested config inputs, assigned for correction. Therefore this stage is not accepted as final. The reduction from the earlier 4,181 parses is scope evidence; do not claim the contended time is a speedup.
