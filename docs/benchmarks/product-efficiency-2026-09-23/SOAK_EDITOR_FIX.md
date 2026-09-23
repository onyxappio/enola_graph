# Product scripted-editor harness correction

The first post-package-gate Product soak was interrupted (exit 130) and excluded:
6 of 11 attempted editor operations failed because the scripted worker assumed
`src/core.ts`, `src/util.ts` and `src/index.ts` existed. A Product checkout does
not necessarily contain that tiny fixture. Successful watch generations from
that run are diagnostic only, not a completed soak acceptance.

For scripted Product runs, the harness now initializes a new
`enola-watch-fixture/src` module within the isolated Product copy before watch
starts. A pre-existing fixture directory is refused. The application files are
untouched. External-editor mode still creates no fixture and writes no source.
Reports explicitly name the scripted root and label the synthetic module.
Failed editor operations are excluded from save-latency correlation and make
the run fail instead of allowing an apparently successful soak.

Validation: `python3 docs/benchmarks/product-efficiency-2026-09-22/soak.py --self-test`
passes. The new behavioral case exercises all eight edit kinds on a Product-like
checkout lacking src/, verifies application bytes stay unchanged, and checks
collision refusal. The corrected live Product run has also executed its first
five edits without editor errors; its 30-minute recovery/cold check is pending.
This is scripted synthetic-module traffic on a real Product graph, not an AI
agent implementing a feature in existing application code.

Excluded artifacts: `/tmp/enola-stage5-product-soak/excluded-run.json`.
Corrected active run: `/tmp/enola-stage5-product-soak-v2`.
