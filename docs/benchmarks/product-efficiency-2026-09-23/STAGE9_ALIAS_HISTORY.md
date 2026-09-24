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
remain conservative. A subsequent smaller repro also triggers the fallback with
source addition alone, without editing tsconfig; configuration change is not
necessary to reproduce it.

## Integration checkpoint

The alias change was integrated and pushed as `8af2c7a`, including exact supported
metadata-version recognition and conservative reconciliation of unsupported
metadata. The measurement above remains attributed to its earlier experimental
binary; it is not relabelled as a measurement of the integrated binary.

The full suite snapshot has exactly the integrated production hashes.
`graphsession` passed in 588.858 seconds. Its sole failing test was the bootstrap
alias scope test retaining old parsed-file expectations. Both direct and inherited
retargets now reparse the single importer rather than unused descendants; every
cold graph comparison was retained. The entire bootstrap package passed after
that test adaptation (131.556 seconds), as did independent integrated alias,
legacy migration and unsupported-metadata guards. A clean integrated binary also
passed the genuine released-state migration harness. Source and log hashes are
recorded in [the integration receipt](stage9-alias-integration-validation.json).

This remains an intermediate optimization. Empty-domain scope narrowing,
repeated history measurements and final production watch/NATS acceptance remain
open. The separate [released-version watch control](STAGE9_PRODUCT_WATCH_CONTROL.md)
does not establish performance for this alias change.
