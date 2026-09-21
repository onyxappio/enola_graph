# Real Git history resident smoke — not performance acceptance

One run using the frozen resident stage before input policy integration, under
concurrent development. Real fsnotify batches; request-triggered ApplyChanges.
Baseline older main, Git checkout to pinned target, delta compared with independent
NATS-consumer graph from a cold target run. All three harness checks passed.

The cold oracle used /tmp/enola-resident-cold-json-shim to translate --summary-json
to the older --json flag. Its full Facts stdout was excluded from metrics and this
archive; cold runtime includes that output cost and is not a performance comparison.
Resident timings do not use this shim. Original checkout restored; observer exited
on deliberate broker shutdown after assertions passed. See provenance.json for SHAs.
General resident config-reload review blockers remain tracked separately.
