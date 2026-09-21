# Independent bounded hash/digest regression probes

Status: task completed; candidate correctness failures demonstrated. This is NOT final implementation approval, a full regression suite, or performance acceptance. Main checkout and original Product source were not edited; all snapshots, fixture tests and reports are below this directory. Root granted a focused-test window; no benchmarks were run.

## Provenance and oracle

Two private full source copies were made because the checkout was actively changing. `snapshot-sha256.json` records the first captured Go sources; `snapshot-b-sha256.json` records the later copy used only for the AsyncAPI detection follow-up. Neither represents an atomic Git commit or a claim about the final implementation. Added probe test files are distinguished by the `independent_*_probe_test.go` suffix. Production code and Consumer were not modified in either copy.

Graph probes use the existing original `Consumer.ApplyRecords` and `Consumer.Canonical` to compare initial-plus-delta output against a fresh state directory cold run on the same inputs. Canonical equality covers complete owned node/edge content, properties and occurrence multiplicities, rather than counts. The custom extractor supplies an independently predictable content property. The config probe compares original recursive `ConfigInputPaths` against optimized `ConfigInputPathsFromNames` and checks fingerprint sensitivity. Glob probes use unchanged `MatchGlob` as the oracle, checking both match boolean and first matching pattern.

## Results

| Probe | Result | Evidence |
|---|---|---|
| Opaque FileOwner reads normal unowned `payload.dat` | FAIL | `old` -> `new` returns generation 1 -> 1, zero events, one total extractor call; applied Consumer still has `value: old`, cold has `value: new`. |
| Opaque FileOwner reads file-ignored `payload.dat` retained in AllNames | FAIL | Same stale property; fixture verifies it exists in AllNames and not production Files. |
| Custom extractor named `mdintent`, normal/ignored unowned payload | FAIL (both) | Same failures, showing extractor naming cannot authorize built-in input assumptions. |
| Custom `mdintent` reads `src/a.ts` while built-in TS hashes/parses it | FAIL | TS parses one file and emits four events, custom extractor stays at one call and its owned `note.md` property is stale versus cold. This isolates digest invalidation from missing content hashing. |
| Built-in mdintent: target body edit | PASS | Exact cold Consumer equality, zero events, generation 1 -> 1. |
| Built-in mdintent: target rename | PASS | Exact cold Consumer equality; link graph actually changes, three events, generation 1 -> 2. |
| Config under engine-pruned `private/**` | FAIL discovery/fingerprint | Original recursive discovery includes `private/tsconfig.json`; FromNames omits it and the analysis fingerprint does not change after its contents change. |
| Same pruned config fixture cold graph | PASS, limited | This strict-option-only config is intentionally inert for active `src/a.ts`; graph remains cold-equivalent. This probe proves discovery/fingerprint regression, NOT an observed graph mismatch for that config fixture. |
| Compiled GlobSet: escaped extension | FAIL | `a.ts` with raw Go pattern `**/*.t\s` matches original but fails compiled; adding fallback `**/*` changes the winning pattern. Directory-scoped escaped extension fails too. |
| Compiled GlobSet: empty path/pattern | FAIL | Original matches empty path with empty pattern, `*`, `**/`, or `/**`; compiled returns false. |
| GlobSet malformed and combined patterns | FAIL combinations | Malformed entries combined with empty/fallback patterns expose the same empty-path mismatch; no separate malformed-pattern defect was observed on nonempty inputs in this matrix. |
| Arbitrarily named AsyncAPI `api.json` | PASS in A; FAIL in B | Initial snapshot detects and streams it; later snapshot Detect and engine FileListDetector both return false while direct Extract returns one fact, cold stream emits zero nodes. |

## Exact reproduction

Toolchain: `/tmp/enola-toolchain/go/bin/go`.

From `/tmp/enola-delta-independent-probes/snapshot`:

```sh
/tmp/enola-toolchain/go/bin/go test ./internal/graphsession -run '^TestIndependent(OpaqueInputs|BuiltinMarkdownTarget|PrunedConfig|CustomMarkdownHashedDependency)$' -count=1 -v
/tmp/enola-toolchain/go/bin/go test ./internal/facts -run '^TestIndependentGlobEquivalence$' -count=1 -v
/tmp/enola-toolchain/go/bin/go test ./internal/graphsession -run '^TestIndependentArbitraryJSONAsyncAPI$' -count=1 -v
```

From `/tmp/enola-delta-independent-probes/snapshot-b`:

```sh
/tmp/enola-toolchain/go/bin/go test ./internal/graphsession -run '^TestIndependentArbitraryJSONAsyncAPI$' -count=1 -v
```

Tests:
- `snapshot/internal/graphsession/independent_hash_probe_test.go`
- `snapshot/internal/facts/independent_glob_probe_test.go`
- `snapshot/internal/graphsession/independent_asyncapi_probe_test.go` (same AsyncAPI probe in snapshot-b)

Recorded logs:
- `graph-output.txt`: four opaque cases FAIL; built-in markdown cases PASS; config discovery/fingerprint FAIL. The original run selector also included an existing `TestIndependentUnsyncedAdmissionCannotPromote`, which passed; it is unrelated and omitted from the narrower replay selector above.
- `name-collision-output.txt`: definitely-hashed TS dependency/custom mdintent mismatch.
- `test-output.txt`: complete glob mismatch matrix. This first invocation also records a graph test compile setup error caused by my initial wrong plugin import; corrected to `pkg/plugin` before graph tests ran, with subsequent results in graph-output.txt.
- `asyncapi-a-output.txt`: PASS using corrected assertion counting all Consumer nodes (AsyncAPI facts are not symbol facts).
- `asyncapi-b-output.txt`: detection and empty-stream failures.

Minimal opaque fixture: `note.md = # Note`, `payload.dat = old`; custom FileOwner only owns note.md but reads payload.dat and emits a note.md symbol with property `value` equal to payload contents. Change only payload bytes to `new`. For ignored case append exact file glob `payload.dat`; inventory assertion verifies AllNames still contains it. Name-only mdintent variant changes only Name(). Hashed-dependency variant registers TS too and reads src/a.ts instead.

Minimal pruned config fixture: active `src/a.ts`, `private/tsconfig.json`, and configured ignore `private/**`; change strict true to strict false without altering names. The original TS config walk descends private while engine inventory prunes it.

AsyncAPI fixture is JSON with root asyncapi `2.6.0`, info title/version, and channels.orders.created.publish.operationId `emitOrder` plus an object message payload. Filename is `api.json`; direct extraction emits one fact. Cold-versus-delta alone cannot catch this regression because both may omit the same feature, so the probe separately asserts detection and direct-extraction facts reaching Consumer.

## Follow-up

Root and Grok received exact paths and failures promptly. The original Grok dispatch had settled; its rejected send was rerouted through root and the new correction dispatch `ctx_b58d6a132740`. Corrections, fresh final-snapshot verification, and all performance acceptance remain coordinator-owned; do not treat these captured-source results as evaluation of later edits. Hash input closure must preserve opaque plugin fallback (including ignored AllNames content), audited built-in semantics must not be granted by Name alone, config traversal must retain the old discovery set, and compiled glob filtering must preserve escaped and empty-input behavior.

Capture metadata: A `2026-09-21T06:33:50.890916+00:00`; B `2026-09-21T06:37:44.693809+00:00`.
