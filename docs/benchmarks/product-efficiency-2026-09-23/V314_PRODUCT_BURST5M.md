# Frequent-edit Product watch stress exposes retry and measurement gaps

Current wave10 + stage6 binary, five minutes, scripted edits every 3 s, fixed 5 s
collection window, full profile and NATS. Synthetic module in isolated Product;
not an AI feature implementation. 91 successful edits produced 17 completed
generations. Inputs were stable across cold and final graph equality passed.

**This is not autonomous-watch acceptance.** The harness restarted watch after
seven unplanned exit-code 2 failures caused by files being removed/renamed during
analysis: five `unreadable required input while computing composition context`
and two `source ... unreadable before EndReplace` failures. Thirteen observed
Begins remained without End and were classified superseded. A planned watcher
restart is additional to those seven. Fail-closed correctness is preferable to
publishing a partial graph, but a production watcher must reconcile/retry normal
input-version races without requiring an external test harness to restart it.
Permanent/configuration/transport errors need separate treatment; indiscriminate
retry would hide real faults.

**The raw NATS in-flight verdict is invalid.** The selected open Begin was
155.655 s old (generation 2), superseded by newer attempts; the latest Begin was
generation 11. `open_begins()[0]` confused any unended historical attempt with
current work. The corrected catcher uses the existing `classify_begins` active
head and watcher restart boundary. Self-tests cover superseded/completed heads,
prior process lifetime and a valid active head; replacing it with the old
selection fails the regression tests. This run does not prove interruption of
an active publish or recovery from it, even though its raw report says
`during_publish=true`. Raw data remains unchanged for audit.

The 12.133 s outage measurement excludes the later watcher observation window,
following the preceding timing fix. That measurement is stop request to port
readiness; it does not by itself identify an in-flight transaction.

[Audit verdict](v314-product-burst5m-audit.json) ·
[Unmodified raw report, including error tails](v314-product-burst5m-raw.json).
