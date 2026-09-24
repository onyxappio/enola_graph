# Alias scope: Product history diagnostic

The experimental alias projection reduces TypeScript reparsing from 3,940 files
to the six added source files on this Product transition. It does **not** yet
reduce the authoritative replacement scope or establish acceptable delta latency.

Product input is pinned to `d0fbbf855af5f4a33c364885f85805d71f9fac7c` →
`a2ac71af8a22471c27059a9b318ef4880caa50cb` (state-machine-telemetry addition).
There are 14 changed paths, including an ignored lockfile. The control binary is
clean Enola `a648c0b`; the candidate is an experimental Go overlay on `f7e1ed5`.
The [machine-readable receipt](stage9-alias-history.json) records binary and
production-source hashes. Candidate Go VCS metadata identifies only the base
checkout and must not be used to identify the overlay implementation.

| Metric | Released control | Experimental candidate |
|---|---:|---:|
| Delta wall seconds | 24.785 | 20.341 |
| Fresh target analysis seconds | 24.011 | 21.527 |
| Parsed files | 3,940 | 6 |
| Cached files | 0 | 3,934 |
| Reported file reads | 3,992 | 58 |
| Begin owners | 8,178 | 8,178 |
| Owners published | 4,773 | 4,773 |
| Published events | 2,077 | 2,077 |
| Owners whose facts actually changed | 7 | 7 |
| Final fresh CLI no-op seconds | 1.732 | 1.783 |

These are single diagnostic file-sink measurements on a shared host, not repeated
NATS/JetStream acceptance measurements. Reported file reads are engine counters,
not all operating-system reads. Candidate delta still takes 94.5% of its own
fresh target analysis time. No stable speedup percentage or initial improvement
is established by these runs.

Both arms pass strict Begin/End owner count and digest, batch count and digest,
and exact incremental-versus-cold graph validation. Final no-ops parse zero files,
publish zero events, retain generation 2 and leave persisted state hashes unchanged.
Seven changed-fact owners are an observed output difference, not proof that a
safe pre-analysis planner could use precisely seven owners.

## Remaining scope cause

A reduced independent fixture reproduces the remaining global fallback. Python
is detected through a manifest but owns no Python source files. Adding an unrelated
TypeScript source changes the repository name set. `nonTSExtractorNeed` then
requests Python work before an unchanged declared-input digest can prove its
empty domain reusable. Because Python cannot preview captured extraction, the
planner conservatively includes all prior/current file owners before Begin.

This is not a claim that every TypeScript configuration change invalidates Python:
a config-only retarget without the new source did not reproduce the broadening.
The failing fixture combines retargeting and source addition. A separate retirement
guard passes and requires deletion of the last Python source to clear its previous
contribution even when a manifest keeps the extractor detected.

The next production change must preserve that retirement behavior while reusing
a proven unchanged empty domain. Unknown ownership or unsupported metadata must
remain conservative. Exact alias metadata-version handling and the full suite
are still integration gates for this experimental candidate. Repeated history
and production watch/NATS measurements remain required afterward.
