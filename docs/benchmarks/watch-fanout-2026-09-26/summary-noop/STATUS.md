# Experimental summary-only no-op result assembly

`RunSummary` is an explicit one-shot API used only by CLI `--summary-json`
when `--json` is absent. It retains full checkpoint decode, input reads,
planning, fences, delivery and recovery. Default Run and resident Watch keep
their full snapshot behavior. This is not lazy state decoding.

Cached TS contribution cloning is delayed until the planner knows whether the
run publishes. Publication flushes the delayed contribution at its original
append position; a proven no-publication summary does not need those clones.
State facts and cached records remain complete. No schema or index is added.

Focused checks passed: default API full facts on fresh noop, resident snapshot
preservation, summary initial/noop and manifest-only delta (zero TS parses),
consumer graph equality to cold and a subsequent full API result. Focused
command tests also passed. An added explicit zero-event assertion is included
in the in-progress full graphsession/command suite (session71382, log
/tmp/enola-summary-full-tests.log). Full-package outcome not yet established.

Next: complete full suite, real Product cold/delta/noop and replay checks,
then repeated same-scope same-host timing against published main. No performance
claim, watch speed claim, acceptance or publication yet.
