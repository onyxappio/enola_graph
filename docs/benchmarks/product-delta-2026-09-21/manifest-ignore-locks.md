# Manifest-only extraction

Implemented `manifestextractor.NewWithoutLockfiles() *Extractor`. Its private constructor option has no setter; `New()` and the zero value retain legacy lock-aware behavior. No caller wiring was changed.

## API and semantics

- `Name()` remains `manifests`.
- `ConfigKey()` is `manifests-without-lockfiles-v1` in the new mode and empty for legacy construction.
- `DeltaContext()` uses the `manifests-without-lockfiles-v1:` prefix and hashes only discovered manifest paths/content/read results in the new mode; legacy retains `manifests-v1:` and ancestor lock candidates.
- `OwnsFile` and `ContentInput` exclude all lockfiles in the new mode. `NameSetInput` stays false.
- Parser read context short-circuits both lock resolution and unsupported-lock existence reporting before ancestor search, cache lookup, reads or parsing. Its raw read boundary additionally rejects known lock basenames.
- No resolved versions or unresolved-lock attributes derive from locks. Dependency identities, declared constraints, dev classification and manifests are retained. Ranges remain unpinned regardless of locks; exact manifest constraints remain pinned. Existing Go manifest versions and Python exact manifest pins still populate resolved_version because those values are declared in manifests, not lock-resolved.
- Discovery still uses the existing repository walk; encountered lock names are not selected, hashed, read, or made into facts. The change does not implement broader engine inventory optimizations.

## Changed files in this task

- internal/extractors/manifestextractor/manifest.go
- internal/extractors/manifestextractor/delta.go (pre-existing untracked file updated)
- internal/extractors/manifestextractor/without_lockfiles_test.go (new)

Existing unrelated workspace changes, including parsers.go, were preserved.

## Validation

- `/tmp/enola-toolchain/go/bin/go test ./internal/extractors/manifestextractor` PASS, including all existing legacy lock-aware tests.
- `/tmp/enola-toolchain/go/bin/go test -race ./internal/extractors/manifestextractor` PASS.
- New tests cover all manifest parser families, all ten recognized lock names, root/intermediate/sibling lock additions, edits, deletions, nearest-ancestor precedence changes, dangling symlink read errors, cache/input identity, parser read boundaries, unchanged complete facts/context, changed manifest constraints/context, and retention of manifest-declared exact versions.
- Negative control: temporarily returning legacy construction from NewWithoutLockfiles made the input/identity, history, and manifest-pin regression tests fail; restored implementation then passed the complete race-enabled package suite.
- Initial bare go/gofmt commands were unavailable on PATH; discovered and used the existing /tmp/enola-toolchain/go/bin toolchain.

## Remaining integration (coordinator-owned)

Wire the graph-only profile to NewWithoutLockfiles, bump global cache coverage/version, intentionally update the cold oracle, and validate graph-level behavior. No engine/bootstrap/profile/cache/docs/go.mod edits, Product runs, commits, or pushes were performed by this worker.
