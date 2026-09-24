# Product watch slow tail: an interrupted attempt and retry

All three roughly 5.3-second convergence samples in the six-run NATS comparison
have one abandoned Begin. All three roughly two-second samples have none. A
separate [instrumented timeline](stage9-watch-retry-timeline.json) reproduces the
slow case at 5.384 seconds with exact final cold equality. The extra latency is
failed work followed by reconciliation, not one unusually slow successful delta.

Times below are relative to the first observed profile line. An external reader
polls stderr every 10 ms; these are approximate observation times, not internal
watcher timestamps. Save times come from the harness after fsync returns.

| Event | Seconds |
|---|---:|
| Identical-byte save | 13.217 |
| First burst save | 14.245 |
| Fast transaction enters | 14.291 |
| Second / last burst saves | 14.304 / 14.368 |
| First Begin | 15.351 |
| First attempt finishes publishing resolved contributions | 15.897 |
| Read / decode prior committed state for retry | 16.421 / 16.654 |
| Reconciliation enters with failed=true | 16.657 |
| Successful retry Begin | 18.731 |
| Successful retry finishes promoting state | 19.921 |

The first transaction starts between burst saves. It stops after publication and
before completing TS-record revalidation, without a successful End. Watch only
retries this way on ErrInputsChanged. Later saves therefore correctly invalidate
the captured input. The final completed generation matches cold; the abandoned
attempt does not advance the completed generation.

A fixed collection window can already be active when the burst begins. This
reproduction includes a preceding identical-byte save and bootstrap dependency
registration; it does not identify which exact event started that window. The
causal finding is the overlap of analysis and later saves, followed by a retry.
Do not interpret the earlier save-to-final-Begin metric as time with no prior
publication. Completed-generation telemetry also excludes abandoned-attempt
traffic, so it cannot measure all retry bytes.

Two prior profiles stayed fast (2.179 and 2.007 seconds). The first profile is
retained in [its phase receipt](stage9-watch-profile.json). It shows an extra
post-initial reconciliation (engine rebuild 0.518 s plus a 0.714 s session,
with input collection nested inside that session), followed by a fast delta:
frozen preview 0.813 s, owner grouping 0.163 s over 58,910 facts/4,035 owners,
and marshaling about 55.7 MB of state in 0.185 s. That reconciliation is currently
part of dependency-coverage safety; these numbers do not justify deleting it.

Next work is earlier detection of superseded captured inputs and reducing safe
retry work, while retaining the final input fences, frozen Begin, and failure
recovery. Fresh CLI no-op and initial optimization remain separate open goals.
