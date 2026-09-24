# Stage16 diagnostic evidence

One sequential instrumented baseline/candidate pair on pinned Product, with concurrent worker tests. Both runs are correctness-only and timing-ineligible. The production candidate remains unmerged and its RSS acceptance remains open.

FINDINGS.md records interpretation. Metrics and checks are raw harness receipts; phase logs are filtered trace/memory lines, with original log paths and hashes in log-provenance.json. phase-comparison.json contains every selected memory row. The overlay and summarizer are archived as instrumentation provenance, not a portable launcher; their temporary source paths remain in pins.json/builds.json. No binaries or Product source are committed.

The diagnostic instrumented both Stage15 baseline and the frozen Stage16 patch. runtime.ReadMemStats perturbs execution. Cumulative allocation, current heap, peak RSS and retained heap are different measurements. Do not use these wall/RSS values as performance acceptance or interpret the single pair as clearing the previous repeated RSS signal.
