# Stage35 rejected; experimental runtime withdrawn

Prototype177c0c5 collected inventory during the fresh policy proof, without a
persistent index. Full four-package tests, full repository suite (123 package
rows,77 cached passing packages), seven-call Product correctness and diagnostic
profiles passed. These establish correctness, not performance acceptance.

The complete12-arm cohort failed its frozen primary performance conditions:
no-op median2.055889→2.035283s (-1.0023%, required at least2%) and one of six
paired no-ops regressed (+1.2319%). No time/RSS regression bound was exceeded;
all correctness and host gates passed. Five improving pairs and faster body
medians do not override the preregistered no-op criteria.

The runtime and its added tests have been restored exactly to published
Stage33 (main2b11e7d, runtime3f2883b). Experimental source remains in commit
177c0c5 for audit; all evidence is retained. It is not being promoted to main.
See timing-rejected-20260927/RESULTS.md for the complete cohort and limits.

All four quiet participants were released after session75829 exited0 at
14:36:04 UTC. Independent arithmetic review requested via msg_31af590d035f.
Next work is deeper attribution of delta preview/assembly and durable-state
costs, preserving frozen Begin, exact graphs and immutable cached facts.
