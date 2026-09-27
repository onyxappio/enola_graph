# Requested window cancelled before timing

2026-09-27 08:25–09:00 UTC was requested but never jointly agreed.
Codata T-003 explicitly rejected it while fixes/tests were active. No timing
arm started and no acceptance samples exist. All participants were sent an
explicit cancellation/release (msg_894077aa872e, msg_39ad1182358b,
msg_fab77e076df4, msg_98a021f6b384, msg_fa24e8fed90c). Reviewer release
acknowledged in msg_9bd996873f83. FSM also received direct terminal release.
A future series requires a newly confirmed interval; this request grants none.
The prospective rule and pins remain unchanged.

Baseline correctness exited0 and all seven graph hashes equal candidate.
Harness arithmetic and current-roster failure tests passed. The first local
harness test attempt found a missing copied synthetic fixture; a correctness
receipt was used as the synthetic test fixture before pins were finalized.
No benchmark data or acceptance condition was changed.
