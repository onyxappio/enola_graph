# Summary-only no-op assembly: engineering gates passed

Runtime 3f2883b adds the explicit one-shot RunSummary API, selected only by CLI --summary-json when --json is absent. Full checkpoint decoding, input reads, planning, fences, delivery and recovery remain intact. Default Run and resident Watch preserve complete facts. No state schema or persistent relationship index was added.

Cached TS contribution cloning is delayed until publication is known; publishing flushes it at the original append position, while a proven no-publication summary skips cloning.

Validation passed: full graphsession/command packages, full repository suite, default full-fact API and resident contracts, summary initial/no-op/zero-parse manifest delta, Product cold/delta/baseline equality, silent byte-stable no-op, and two real current-main Product histories (16 CLI calls;14/1454 delta parses).

The original timing attempt was rejected after host Low Power Sleep interrupted observation. None of its samples entered the fresh cohort. The new twelve-arm cohort passed the preregistered engineering gates: fresh summary no-op median2.117442→2.065174s (-2.4685%, all six pairs improved), no-op RSS382.406→355.164 MiB. Initial median+0.4443%, body-1.3810%, structural-0.2872%, all under the preset regression bounds. See timing-accepted-20260927/RESULTS.md for spread, producer boundaries, caveats and evidence.

Arithmetic review and main publication are pending. This limited stage does not complete the broader near-zero fresh-start, faster delta or long-running watch goal; current fresh no-op remains about2.065s and deltas about4.2s. State decoding and fresh input capture remain substantial costs.
