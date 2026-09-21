# Benchmark harness follow-up — 2026-09-21

Completed within benchmark-only ownership. No production source, Product clone, pinned old binary or local suite runner was modified. No large benchmark or full test suite ran.

## Implemented

- `resident.py:192`: cleanup now calls shared `restore_all`; every shutdown/restore is attempted, and aggregated failures raise instead of allowing successful acceptance. Original exceptions remain in exception chaining.
- `resident.py:150` and `resident_history.py:141`: console summaries print aggregate parsed files, reconciliation count and all fallback reasons, including coverage catch-up. Existing native path proofs, catch-up assertions, run-ID matching and cold graph checks remain intact; `resident-driver.go.txt` was not changed.
- All four Python harnesses use blocking process wait plus a separate timeout watchdog for timed CLI operations. Rounded `/usr/bin/time -l` real time is additionally retained as `process_real_s`; parent wall time still includes setup/launch/response observation.
- `benchscope.go.txt` adds `--export-selected`: candidate-policy selected semantic/name-only files and directories, exact file digests/modes, candidate main/test/name inventories, policy identity and dependency receipts. Existing config-only output remains compatible. Export rejects selected symlinks/nonregular files instead of following them.
- New `harness_support.py` implements `LegacyMirror`. It creates an exclusively owned physical mirror outside the original candidate checkout; copies selected bytes without hardlinks; physically omits excluded tests, locks and private/Git-ignored inputs; preserves name-only media bytes and empty directories; verifies selected file hashes and directory names; and synchronizes before each initial/changed legacy invocation outside timing. Sync deletes only previously copied inputs, preserving legacy output/cache. Every repetition has a fresh `.enola/old-output-N`; pre-existing initial output is rejected, not erased.
- `cli.py --old --scope-tool` and `multifile.py --old --scope-tool` now execute the unchanged old binary on that mirror. Candidate invocations still use the original checkout and pay full-tree policy/Git discovery cost. Per-operation selected input receipts include original HEAD, candidate config digest and a physical manifest digest. Post-run physical inventory is checked again; legacy snapshot metadata/receipt are archived when available. If snapshot metadata exists, its main path/hash inventory must exactly match the exported candidate main inventory or the run fails.
- `cli.py --old-config` without a scope helper remains available but is explicitly labeled approximate scope in provenance and documentation. With both flags, generated mirror config is authoritative and old-config is archived only as provenance. Old binaries/helper binaries are never modified; an obsolete scope helper lacking `--export-selected` fails explicitly.
- `HARNESS.md:94-162` removes the source-alignment overclaim and documents evidence, timing, asymmetric full-tree discovery, and remaining limitations.

## Focused validation

1. All six Python files parsed with `ast.parse`: the four harnesses plus `harness_support.py` and `harness-probes.py`.
2. Formatted and built the snapshot-only Go helper using `/tmp/enola-toolchain/go/bin/go`, with a temporary package inside the benchmark directory removed afterward. Output: `/tmp/enola-review-benchscope`, SHA256 `6524a361177ba0331836a54ae18f0460befe4083c9afa670f78663546d5a7b80`.
3. Ran:

   `PYTHONDONTWRITEBYTECODE=1 python3 docs/benchmarks/product-delta-2026-09-21/harness-probes.py --scope-tool /tmp/enola-review-benchscope --old /tmp/enola-product-old`

   All passed:
   - Injected process-shutdown and source-restore failures into the actual resident finally block; confirmed source, lock and media restores are all attempted and acceptance raises.
   - Executed both actual resident `online` functions with a synthetic catch-up result; console reports total 4 parses, 1 reconciliation and the catch-up fallback.
   - Scratch Git fixture proves excluded tests, lockfiles, explicit private files, untracked Git ignores and nested ignore rules/negations; tracked ignored files remain selected.
   - Verified selected byte equality and distinct inodes, name-only media, preserved empty directories, add/delete/content sync, output/cache preservation, reused-initial rejection, and selected symlink rejection.
   - Ran the unchanged pinned old binary for tiny initial and changed scratch inputs. Both legacy main inventories matched candidate main path/hash inventories; no Product inputs were used.
   - Blocking wait completes normally; watchdog timeout kills/reaps the child and raises timeout.

Pinned old binary used only in scratch: `/tmp/enola-product-old`, SHA256 `5e28f6ed20cfaffcafec65a58320ba6fac57077859c72097c83bda07428c5ba7`. The helper build compiled against the current concurrently developed source tree; root should rebuild from its final immutable candidate snapshot for Product measurements.

## Runner coordination and remaining claims

Coordinator confirmed receipt of the new optional CLI `--scope-tool` flag and updated `/tmp/enola-run-suite.py` itself to pass its rebuilt helper to the CLI case. Multifile already passes the flag. Keep new `harness_support.py` alongside scripts if copying/archive-running them.

The delivered mode establishes physically matched **selected repository inputs** and checks actual legacy **main inventory** when metadata is available. It is not universal proof of identical legacy reference parsing, independent/external/ancestor side reads, VCS-derived facts, or graph semantics. The mirror has no Git metadata and avoids traversal of excluded source trees; candidate full-tree policy work stays included. Those qualifications remain explicit, not silently converted into accepted same-workload claims. Active external dependencies and relevant old side-read paths still require root's Product-specific validation/audit before any strict same-scope acceptance claim.

No new Product timing, performance ratio, spread or cold-equivalence result is claimed. Root owns Product validation, final immutable helper/candidate builds, and final benchmark claims.

## Files changed

All paths below are under `docs/benchmarks/product-delta-2026-09-21/`:

- cli.py
- multifile.py
- resident.py
- resident_history.py
- benchscope.go.txt
- HARNESS.md
- harness_support.py (new)
- harness-probes.py (new)
