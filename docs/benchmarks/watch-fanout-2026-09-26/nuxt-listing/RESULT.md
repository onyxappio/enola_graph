# Nuxt config presence from existing completed directory observations

Graph-profile Nuxt detection skips Stat only when its existing discovery ledger
contains a completed listing proving the name absent. It retains the identical
missing-stat observation. Present entries, folded-case matches, missing/failed
listings, policy refusals and the legacy profile use the original stat path.
Captured content still does not imply live filesystem presence. No additional
relationship/name index, persistent state schema or transport change is added.

The prerequisite failed-directory-enumeration fix is ed259ce. This stage also
fixes the existing TS alias-listing optimization to defer case-folded names to
its actual reader. On this case-insensitive host, the mixed-case alias regression
fails against the prior ts.go via Go source overlay and passes after the fix.

Final focused source checks: 19 top-level tests passed, zero skipped, 7.120 s
including Go invocation/build overhead. Nuxt parity covers 13 filesystem/capture
shapes; observation ledgers match the pre-optimization stat/read path. Separate
checks prove appearing configs refuse retained discovery and an unobserved
directory is never treated as empty. Source hashes and raw receipts accompany
this record. These are correctness results, not performance acceptance.

An earlier full TS + graphsession command was launched before the folded-case
adjustment; both packages passed (406.834 s including build/invocation overhead). That command must not be described as full-suite
validation of the final source. A full final-source run, Product cold/delta
checks, measurements and publication are still pending.

The pending six-pair harness still pins 9c7359f. A benchmark of this new candidate
requires a new source/binary pin and correctness receipt before timing; do not
silently attribute the older candidate's results to this change.

Product NATS correctness on 7c4651d passed all four gates (silent no-op and three cold graph comparisons). All seven scenario hashes also match the pinned Stage22 reference. This correctness-only run is explicitly ineligible for timing acceptance. The final-source full repository suite and repeated timing remain pending.
