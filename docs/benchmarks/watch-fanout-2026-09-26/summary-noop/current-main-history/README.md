# Pinned current-main Product endpoint validation

Target is real GitHub main observed/fetched 2026-09-27: fba38bab93c76a4da8df58cf45fd6847a468c315. The original source checkout remains clean at a609c19f. A separately named Git ref holds the target; benchmark source files were not edited.

Recent interval contains nine Git paths, including four TS source/test files, two package manifests, workflow and changelog changes; it is mixed, not source-only. The broad interval has8,462 Git paths and six manifest paths. Neither count claims all those inputs are admitted by the graph policy.

This harness preserves eight calls, all required pins, clean checkout, no-op, consumer and cold/baseline graph gates. The explicit pinned relation for these intervals is ancestor-endpoints: merge-base --is-ancestor must pass and endpoints must differ. It does not pretend they are adjacent first-parent commits. Older adjacent-history harness/results are unchanged.

Prepared only; no history run or timing acceptance yet. Wait until no quiet timing interval is active before starting this correctness-only work.
