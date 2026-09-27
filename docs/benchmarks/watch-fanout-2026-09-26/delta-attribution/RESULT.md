# TS preview attribution on restored Stage33

Diagnostic binary: accepted Stage33 runtime plus three env-gated phase marks;
see instrumentation.patch and receipt.json. No behavior optimization is present.
Initial and body/structural diagnostic calls exited0; parsed6752/21/22 files and
published3091/2/3 events. These are shared-host diagnostic observations, not
acceptance timings. The three added marks are removed from the working source
after collecting this evidence.

For body edit, first-hop MapFiles/aggregation took0.012/0.044s; second-hop
MapFiles/aggregation took0.297/0.040s. Structural:0.012/0.045 and0.296/0.035s.
The earlier combined span therefore mostly reflects second-hop file processing,
not cloning the entire fact collection. Its child spans must not be added to
parent preview/CLI spans.

Inspection of independently cold-built Product original/body states shows the
changed password.ts record differs only in its content hash outside facts.
Declared/referenced/import/export surface fields agree. It imports node:crypto,
records a context-free local export surface, and has no resolved repository file
imports. Exactly20 owners record it as a side read. sideReadProven currently
requires BindsNoImports, which rejects this record solely because ImportSpecs
is nonempty. This is a concrete conservative-proof candidate, not permission
to ignore changed exports, defaults, resolution or framework dependencies.

Next: reproduce with a small builtin-import/barrel fixture and assess whether
existing context-free surface evidence proves stable consumers across body edits.
No new retained relationship index is proposed; frozen owner planning remains.
