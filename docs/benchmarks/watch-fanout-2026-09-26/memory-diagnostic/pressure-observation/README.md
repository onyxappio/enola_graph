# Continuous pressure observation — preparation, not acceptance

The two zero-swap cohorts remain rejected under their original frozen rules. No
results are relabeled. This observer is not yet wired into a new acceptance run.
No new timing window has been requested.

## Source check

Apple XNU `sysctl_memorystatus_vm_pressure_level` converts the internal level to
its dispatch value before returning it; the normal dispatch value is 1 in
`bsd/sys/event_private.h`. Source URLs, content hashes and relevant excerpts are
in `kernel-sources.json`. These are upstream source references, not a claim that
this machine runs a byte-identical kernel. The live read returned 1.

## Observation behavior

The read-only monitor uses libc `sysctlbyname` directly every 250 ms, avoiding a
new shell process for each sample. It records monotonic and wall clocks and raw
levels; starts with a synchronous sample before the timed interval and ends with
one after it. Missing values, read failures, a non-normal sample, an observation
gap over one second, unordered samples or missing interval coverage fail pressure
validation. Sampling can still miss shorter pressure transients; it is not a
continuous kernel event trace. The same observer must run for both treatments.

Four unit tests pass, including a warning in the middle of otherwise normal
samples, malformed evidence, missing coverage, read failures and exclusive output
creation. A read-only 1.1-second live smoke check recorded six normal samples with
a maximum gap of 0.2602 seconds. This checks instrumentation only and measures no
Enola performance. The unit suite took 0.002 seconds.

## Unresolved measurement decision

Global Swapins cannot attribute faults or their cost to Enola. Normal pressure
and zero new Swapouts do not prove Swapins have no latency impact. Neither an
allowance derived from the observed 64 KiB nor an assumed per-page service time
is justified by present evidence. A new protocol must explicitly distinguish its
pressure-validity gate from paging context, disclose any departure from the
previous absolute-zero rule, and retain all rejected cohorts. This observer alone
does not settle that decision or establish publication eligibility.

The original time and RSS acceptance thresholds remain unchanged: at least 2%
initial median improvement, every paired initial faster, no more than 2% median
regression in each other scenario, and no more than 5% scenario median RSS growth,
plus the aggregate and correctness gates. No candidate may be published from the
incomplete cohorts. Sparse cross-session baseline values are descriptive only;
they cannot establish independent paired noise or an acceptance-rule power claim.
