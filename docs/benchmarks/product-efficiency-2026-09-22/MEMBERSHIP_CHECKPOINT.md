# Membership planning checkpoint — 2026-09-22

Commit `fffc0b7` is a correctness/planning checkpoint, not a demonstrated Product
performance improvement. It plans membership before preview extraction, removes
retired records from the overlay and avoids reusing a preview for a different
set of dirty files. Focused cold-equivalence regressions cover helper additions,
module shadowing and renamed owners plus their dependents.

## Product replay

Both binaries used pinned Product `a609c19f3861971930fae7b33dcb2950598953c5`,
the full default extractor profile and the same final five-file invitation-link
patch from the live editor experiment. This replay applies all five files in one
burst; it does not reproduce the editor chronology. Each sample uses an isolated
checkout, a fresh broker and a 5-second collection window.

| Build | Initial Begin → End (s) | Delta Begin owners | Delta Begin → End (s) | Final cold equality |
|---|---:|---:|---:|---|
| Baseline b611e22 | 6.393 | 8647 | 3.854 | exact |
| Checkpoint fffc0b7 production patch | 6.070 | 8647 | 3.646 | exact |

These are one sample per build, not repeated performance acceptance. Begin → End
excludes startup, pre-Begin planning and the collection window. Total harness
wall times (99.183 and 97.897 seconds) include setup, the requested observation
window and final cold validation; they are not analysis latency.

Both runs had identical 45110-file inventory fences across cold verification.
The inventory uses the harness exclusions; this is not continuous input capture.
The candidate emitted 2690 batches, reported 293 parsed files and 4234 cached
files. Its graph fallback remained `frozen scope: global name-resolution domain`;
mdintent also reported its existing whole-extractor rerun. **The scope objective
was not achieved.** Lower historical baseline parse counts are not a correctness
oracle because the checkpoint also fixes prepared-preview reuse.

## Follow-up under development

Unchanged unresolved imports are incorrectly treated as resolution changes.
That introduces unchanged declaration owners into the dirty set; a global-name
dependent guard then falls back to the whole graph. The follow-up will compare
actual old/new resolution targets and include required name consumers in the
pre-Begin scope. Missing dependency evidence must retain conservative fallback.

Acceptance requires focused regressions, exact cold equality and another replay
of this same Product burst, followed by repeated timings if scope improves.

Raw comparison and candidate End counters are in
[membership-checkpoint-burst.json](membership-checkpoint-burst.json).

The subsequent fix and accepted Product replay are recorded in
[REBOUND_PRODUCT.md](REBOUND_PRODUCT.md).
