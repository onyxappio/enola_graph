# Pre-Begin neutral manifest checkpoint

Base: `ae8aa8097398875755c520e7d16adf2edb80c949`. This is a correctness and redundant-publication checkpoint, not a demonstrated Product latency improvement.

A fenced non-TypeScript extraction now proves unchanged output before Begin. Version-only manifest edits and reverts refresh observed input hashes without publishing a replacement or advancing generation. Genuine dependency changes still publish; per-file TypeScript context changes remain a reason to reanalyze. The same captured extraction feeds candidate planning and publication. Proven neutrality cannot be reversed by a later live-tree need calculation. Input races refuse observation commits as well as successful End.

## Verification

- Worker full affected suites: graphsession 188.729 s, command 6.052 s, exit 0.
- Independent race-enabled tests on frozen sources: 11.510 s, exit 0. Covers actual capture-time dependency edits with neutral and changed-dependency retries; mixed neutral manifest plus source edit with failed End, byte-identical completed state and exact-cold recovery; resident alias retarget sequence.
- The capture-race probe failed on the withdrawn candidate with the wrong error. Both retry modes pass with the fix. Its manifest capture hook is test-only overlay instrumentation and is not part of production.
- Strengthened fresh-CLI manifest probe: 8/8 exact-cold and frozen-protocol checks. Version edit, revert and unchanged runs have zero events, zero parsed source files and unchanged generation. The dependency edit changes the graph and advances exactly once.
- The Markdown whole-domain regression now adds a third note, producing an actual new fact, and still requires all three note owners. The old content-only change emitted identical facts and correctly became neutral. Uncapturable extractors retain whole-domain coverage.

The accompanying JSON contains source hashes, binary build receipt and diagnostic CLI times. The binary is stripped (`-s -w -trimpath`) because of local disk pressure; these tiny-fixture, file-sink times are not matched Product/broker performance acceptance.

## Still open

Captured discovery reuse, redundant fresh-CLI preparation, near-zero no-op latency on Product, repeated initial/delta measurements through broker acknowledgments, historical config/source cases and long-running watch acceptance remain open. Deleting a factless manifest still conservatively publishes once; graph-neutral TypeScript edits are not suppressed by this checkpoint. No whole-goal completion claim is made.
