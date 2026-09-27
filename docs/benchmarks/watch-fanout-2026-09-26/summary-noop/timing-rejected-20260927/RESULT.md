# Rejected incomplete timing cohort — 2026-09-27

Frozen runtime 3f2883b vs published runtime 4859ee3. Only baseline-1 and candidate-1 ran; session90749 exited 1. No speed comparison or promotion.

Both arm correctness checks passed. Baseline pressure checks passed, with zero new Swapouts. Candidate timing eligibility failed because host process sampling timed out and pressure sampling had a gap. Its recorded wall interval was 08:40:22–13:01:50 UTC, while monotonic elapsed time was about 234 seconds. The macOS power log confirms Low Power Sleep at 11:43:06 +0300 (1% charge), followed by wake at 16:01:16 +0300. Do not interpret this sleep interval as graph-analysis duration.

The original hold ended 09:05 UTC; it was not extended. The cohort is incomplete and cannot be pooled with a future run. The fixed summarizer independently rejected it as incomplete or nonalternating. All four participants received explicit release after process exit was verified. Even before the clock discontinuity was observed, baseline arm duration indicated the original interval was too short for twelve arms.

Next: audit host sleep evidence and earlier harness composition; complete real current-main history correctness independently, then schedule a fresh sufficiently long cohort. Runtime remains experimental and unpublished.
