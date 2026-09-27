# Fresh summary no-op phase diagnosis after Stage33

Isolated new Product01baa6e clone, unchanged runtime3f2883b binary; initial plus two fresh-process no-ops completed exit0. Each no-op had zero parses/events, unchanged generation and byte-identical durable state. Profiling enabled explicitly; shared host, no acceptance timing or new speed claim.

First no-op trace: CLI1.984s; policy construction0.335s; committed state90,686,134 bytes, decode0.395s (load0.448s); graph-input reuse proof0.106s; input inventory0.106s; parallel detector group0.120s; hash7,791 files /82,572,444 bytes0.280s; context capture0.066s; configuration fingerprint0.059s; TS discovery0.157s; TS session context0.089s. These are nested trace segments, not additive across trace names. Use both raw logs for variation.

Next concrete source investigation: Policy.ReusableOver already walks every non-hard-excluded name and kind to prove the policy unchanged. Immediately afterward Engine.Inventory performs another walk. Consider carrying the just-proven ordered entries into the first transaction inventory, not retaining an inventory across transactions and not introducing a relationship index. This is a candidate design, not an implemented or measured saving.

The proof walk is broader than admitted inventory. Any reuse must preserve graph policy classification, legacy ignore/test globs, directory-pruning semantics, exact WalkDir order, symlink/root behavior, errors, configuration brackets, recovery and existing post-read fences. The proof cannot prune extra directories merely because inventory ignores them; additions/removals/type changes must still invalidate its reuse. Unknown/stale observations fall back to the existing walk. Full API and resident behavior remain intact.
