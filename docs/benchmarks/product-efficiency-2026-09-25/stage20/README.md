# Stage20 provisional evidence

Correctness and allocation diagnostics only; no accepted CLI timing series exists. Production candidate is not integrated. See ../STAGE20_VALIDATION.md. Absolute paths in receipts pin the original scratch artifacts; binaries and Product source are not committed.

`python3 -B test-timing-summary.py` runs synthetic acceptance-gate controls using the archived correctness metrics as fixture data. It does not run Enola or create real performance evidence. `summarize-timing.py` requires a complete authorized timing window and six actual timing arms; it refuses the current incomplete archive.

Watch and history receipts retain operational durations under shared load. Do not use them as performance acceptance. The supplemental config-add history receipt retains its original series-cardinality refusal; its separate two-revision audit covers only that one transition.
