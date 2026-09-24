# Product checkpoint allocation investigation

Exact pre/post json.Marshal counters isolate about381MB of cumulative allocations per56.6MB structural checkpoint in both baseline and Stage16. The independent encoding diagnostic compares standard Marshal versus Encoder against the same decoded real Product state, with three controlled samples each and doubleGC before each sample. Encoder median324.65MB versus Marshal381.29MB is a14.85% reduction. Canonical bytes match after removing Encoder’s final newline; Encoder performs one complete-buffer write, not bounded streaming.

These are diagnostic allocations, not peak memory or latency acceptance. The encoding diagnostic excludes filesystem writes/fsync/rename. It does not implement a production checkpoint writer or prove end-to-end speed. No-op never serializes state, so this change alone cannot accelerate no-op. Preserve pending-file atomicity, acknowledged-End promotion, deterministic bytes/fingerprints and eager corruption validation in any follow-up.

FINDINGS.md describes full-run boundary samples, including the higher405MB candidate body sample. The controlled encoding experiment is distinct; do not pool them. Build manifests, overlays archived as text, raw check receipts and filtered trace lines preserve provenance. Temporary paths refer to local evidence; scripts are archived diagnostics, not portable launchers.
