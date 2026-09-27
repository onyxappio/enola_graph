# Diagnostic profile: duplicated traversal removed, speed unproven

Frozen prototype 358f22f9fcb8fa32f64ce1794ea2fa8b6dc9d119f8f90fa6698a12055d8b9961
completed initial and two fresh CLI no-ops. Both no-ops parsed zero files,
published zero events and preserved checkpoint bytes and generation.

The standalone inventory span disappears, but its classification work moves into
the policy proof. Previous diagnostic proof plus inventory: 0.212/0.204 seconds.
Prototype: 0.178/0.189 seconds. These spans are sequential and nonoverlapping;
this comparison does not sum nested session/CLI spans. The apparent difference
is only 15–34 ms, not the entire former 106 ms inventory span.

The earlier diagnostic CLI totals were 1.984/1.978 seconds; prototype totals
2.008/1.966 seconds. These separate shared-host runs are not a paired timing
cohort. They establish neither overall improvement nor regression and must not
be mixed into acceptance measurements. Any promotion still needs repeated,
coordinated measurements against Stage33 with frozen criteria.
