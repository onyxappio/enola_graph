# Separate clean-host cohort also rejected

Session 49997 exited 1 after baseline-1, control-1 and candidate-1 completed their correctness checks with exit 0. Baseline/control swap counters did not change; candidate-1 had four Swapins (64 KiB) and zero Swapouts. The predeclared absolute-zero-swap rule rejects this incomplete cohort. No refill and no acceptance or comparative performance claim.

The renderer cleanup was real but did not eliminate these small global swap-in events. This observation does not establish which process caused them, nor does it prove a latency effect. It reveals a weakness in the additional host gate: global swap-in alone does not distinguish historical swapped-page retrieval from active pressure. Any revised host criterion must be documented and pinned before a new experiment, retain all paging counters and the original time/RSS gates, and must not relabel this rejected experiment as accepted.
