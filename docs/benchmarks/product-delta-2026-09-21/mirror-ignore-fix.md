# Legacy mirror ignore fairness correction

Changed only the benchmark helper snapshot `docs/benchmarks/product-delta-2026-09-21/benchscope.go.txt` and its focused `harness-probes.py` regression. No production or Product files were edited, no commits were created, and no large benchmarks were run.

The helper now appends generated excluded-path patterns only in backward-compatible config-only mode. Export mode preserves the effective candidate/legacy ignore list unchanged because the selected-input manifest already physically filters the mirror. Excluded directory pruning and selected-input export remain intact.

## Validation

- Read repository AGENTS.md, CONTRIBUTING.md and streaming contract.
- Built the helper with `/tmp/enola-toolchain/go/bin/go build` from a temporary package inside the benchmark directory, removed automatically after the build. Verified exact gofmt output. Binary: `/tmp/enola-mirror-ignore-benchscope`.
- Ran `PYTHONDONTWRITEBYTECODE=1 python3 docs/benchmarks/product-delta-2026-09-21/harness-probes.py --scope-tool /tmp/enola-mirror-ignore-benchscope --old /tmp/enola-product-old`: all four probe groups passed.
- The same updated probe against the prior `/tmp/enola-review-benchscope` failed at the intended export-ignore assertion, showing seven redundant patterns appended after the three effective original ignores. This establishes regression sensitivity.
- Export initial and delta configurations retain exactly `**/*.test.ts`, `.enola/**`, and the engine-added `**/.enola/**`; no generated exclusions remain. Config-only mode retains the original prefix and still translates excluded files, directories (`.git/**`) and escaped metacharacter paths (`excluded\[dir]/hidden.ts`).
- Scratch-only physical mirror file digests and directory names match selected inputs before and after old runs. Pinned old snapshot main inventories match candidate selections for initial and delta runs. Existing probes also pass tracked Git exemption, nested negation, excluded tests/lock/private inputs, copied bytes, history synchronization, cache preservation, unique initial output, symlink rejection, cleanup failure handling, catch-up summaries, and process timeout reaping.

## SHA256

- Pinned old `/tmp/enola-product-old`: `5e28f6ed20cfaffcafec65a58320ba6fac57077859c72097c83bda07428c5ba7`
- New helper: `d71770dfdfc0a4773d562201cce88215eaf86de8fbb166e3c778641e90944238`
- Helper source snapshot: `cb973147f1864db73d57741a31ea955704887f7a2b9786e3d0d634b65c9359e2`
- Probe source: `c385b077d57c95cefc631e64620d7e7e71693465fdd301e16fc15b1878d1df04`

The helper compiled against the concurrent working tree; rebuild from the final immutable candidate for actual measurements. These scratch checks establish inventory fairness, not identical legacy extraction semantics or external side-read behavior. No remaining work within this bounded correction.
