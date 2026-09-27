# Current Product watch correctness diagnostic

Guarded runtime7812838, Product fba38bab93c76a4da8df58cf45fd6847a468c315, full default extractor profile with test-inclusive policy overlay; isolated clean input clone. Shared host, fixed watch collection window500ms, changed-owner-scope enabled. No repeated timing acceptance or long-duration stability claim.

Self-test and tiny smoke passed. Product run exited0: initial6749 parses, burst delta1 parse, actual graph change, exact final watch/cold equality, byte-stable source inventory across cold verification (45467 files), zero idle events, zero duplicate-save events, no abandoned Begin, no telemetry batch-count mismatches. Observed final-matching-suffix latency2033.820ms includes batching and observation; it is not a fresh CLI latency or a baseline comparison, and carries the convergence limitations in watch.json.

First attempt refused its initial frozen Begin (1075423 bytes) against the legacy1MiB harness limit, before successful publication. Both watch and cold now use the existing CLI acceptance limits: max-begin8000000 bytes, broker payload8388608 bytes. No runtime/protocol change or larger journal was introduced. Previous evidence remains in first-attempt-limit.log.

Next: repeated mutation/restoration and long-running watch coverage, then separate coordinated performance comparison. The short current run does not close those requirements.
