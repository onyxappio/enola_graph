# Immutable graph input policy worker report

Implemented only `internal/graphinput/{policy.go,policy_test.go}`, `internal/config/graph_inputs.go`, `internal/config/graph_inputs_test.go`, and the GraphInputs field in `internal/config/config.go`. No commits, benchmarks, or engine/session/bootstrap/extractor integration were performed. Coordinator owns architecture declaration: graphinput imports only standard library and internal/facts; config imports graphinput.

## API

- `graphinput.Build(root string, options Options) (*Policy,error)` builds an immutable snapshot. Git and filesystem IO occur only here.
- `cfg.GraphInputOptions(stateDirs ...string) graphinput.Options` copies existing config settings without mutation. Root remains the explicit Build argument, so multi-repo callers can build per repo.
- `Policy.Classify(path string, directory bool) Decision` returns `Kind` (`Excluded`, `NameOnly`, `Semantic`), `Known`, and `Reason`. Paths are root-relative or absolute. Unknown non-excluded paths are provisional: reconcile/rebuild before extracting them. Unknown paths under a known ignored parent or hard exclusion are safely excluded.
- `Policy.ClassifyEvent(path, directory, Content|Membership) Action` returns `Ignore`, `ContentChanged`, `NamesChanged`, or `Reconcile`, without IO or subprocesses. Classify both sides of a rename. Known media content edits are ignored; membership changes retain name-dependency significance. Directory changes, unknown membership, active policy files, any new in-scope `.gitignore`, and index/HEAD/config/gitfile changes reconcile. Genuine watcher overflow/coverage loss remains an unconditional caller reconciliation responsibility.
- `Policy.ClassifyDependency(path) Decision` promotes explicitly declared media content dependencies to semantic, preserves in-root exclusions, and accepts explicit external dependency paths as semantic except recognized locks/VCS metadata. Caller owns external content hashing/watches and required-excluded-input diagnostics. This method does not mutate the policy or record a new dependency.
- `Policy.WithConservativeMedia() *Policy` returns a cheap immutable promotion for unaudited consumers, with changed identity; shares private read-only data and performs no Build IO.
- `Policy.Identity() string` returns a versioned SHA256 policy fingerprint. `Dependencies() []Dependency` returns a defensive copy of absolute control-input paths and their SHA256/missing state. Dependencies are reconciliation signals, not graph contributions or a generation key.
- `graphinput.IsLockfile(path)` centralizes the exact mandatory lock basename list in this package. No `*.lock` wildcard.

## YAML and precedence

Existing YAML convention is unchanged. `ignore` still replaces the legacy default list on Load; graph-only additions live in the same config:

```yaml
graph_inputs:
  exclude:
    - apps/mobile/e2e/artifacts/**
    - worker-reports/**
  # Omit to inherit graph cache defaults. [] explicitly disables that layer.
  cache_exclusions: []
  semantic:
    - '**/*.PNG'
```

All these patterns use existing Enola `facts.CompileGlobs` syntax, not Git syntax. No leading slash or negation convention was invented. `exclude` is additive to effective `Config.Ignore`; those combined patterns are strict graph-input exclusions in this API. The legacy Ignore distinction between output omission and test-reference inputs is not represented here: integrators must explicitly choose supported test-reference scope rather than revive excluded inputs implicitly. `semantic` only promotes name-only media; it never overrides exclusions. To recover source hidden by legacy defaults, replace `ignore` appropriately, and replace `cache_exclusions` if its independent graph cache layer also matches.

Precedence: mandatory lock/VCS exclusions, literal state/output directories, explicit/effective Enola exclusions, overridable graph cache exclusions, `.gitignore` with tracked exemption, semantic/media classification. Tracked files do not override hard exclusions. Tracked descendants keep ignored ancestor directories traversable. Literal directory exclusions cover descendants even without `/**`.

Safe graph cache defaults: node_modules, .next, .nuxt, .svelte-kit, .vercel, .turbo, .parcel-cache, .npm, .pnpm-store, .yarn/cache, .yarn/unplugged, __pycache__, .mypy_cache, .pytest_cache, .ruff_cache, at any depth. VCS segments .git/.hg/.svn are mandatory. No new blanket docs/generated/JSON/YAML/public/bin/coverage/out exclusions. Effective legacy Config.Ignore can still contain its own broader defaults, intentionally preserved.

Case-insensitive media extensions: PNG/JPG/JPEG/GIF/WEBP/AVIF/ICO/BMP, MP4/MOV/WEBM, MP3/WAV/OGG, WOFF/WOFF2/TTF/OTF. SVG, archives, maps, HTML, CSS and arbitrary data remain semantic. `Options.ConservativeMedia` or `WithConservativeMedia()` restores content semantics for opaque consumers; explicit semantic globs are case-sensitive under the existing Enola matcher.

Mandatory exact names: package-lock.json, npm-shrinkwrap.json, yarn.lock, pnpm-lock.yaml, bun.lock, bun.lockb, go.sum, Cargo.lock, Gemfile.lock, composer.lock, Pipfile.lock, poetry.lock, uv.lock, pdm.lock, pubspec.lock, packages.lock.json, Package.resolved. This list is broader than manifestextractor's ten legacy lock side-read names; integration should converge recognition without enabling lock reads. package.json/go.mod and ordinary schema.lock remain semantic.

## Git semantics and identity

Build performs one batched `git check-ignore --no-index -z --stdin` against an isolated temporary bare Git directory with the analysis root as work-tree. This preserves nested ordering, negation, anchored/directory patterns, ignored-parent constraints, escaping, and Git wildcard semantics. Existing paths are submitted without synthetic trailing slashes (adding one falsely makes `open/` match `open/*`). Temporary Git initialization disables templates. Real index tracking comes from one `git ls-files -z --cached -- .` call; git-dir/common-dir discovery adds linked-worktree control dependencies. No user global excludes or real `.git/info/exclude` are applied: only repository `.gitignore` files within the analysis root are policy inputs. Git matching is explicitly case-sensitive (`core.ignoreCase=false`). Non-Git folders also support nested `.gitignore`; Git executable remains required.

Identity includes options, active `.gitignore` content/missing root, supplied config contents, Git config/gitfile dependencies, and sorted non-hard-excluded tracked names. Ordinary source/media content or untracked membership does not change policy identity: extraction/name inventory tracks those separately. Index/HEAD bytes are control dependencies but excluded from identity; lock-only staging does not change identity. Inactive `.gitignore` beneath an ignored parent is not hashed. Adding/removing an active nested `.gitignore` changes identity. Config paths may be absent or external. Mandatory locks supplied as config paths are not read or used as dependency content.

## Validation

`/tmp/enola-toolchain/go/bin/go test -race ./internal/graphinput ./internal/config` passed, including legacy config tests. Focused cases cover nested pattern ordering/negation/escaping/anchoring/directory-only rules, parent barriers, tracked ignored files and traversable ancestors, explicit exclusions winning, non-Git roots and global-ignore isolation, cache/media defaults and overrides, retained semantic inputs, exact locks and lock-only staging identity, unknown and directory events, config and external dependencies, immutable copies/promotion, and linked-worktree external index/gitfile controls. Tests caught and then verified correction of a synthetic directory-trailing-slash bug during implementation. No Product or performance measurements were run.

## Precise limitations and integration obligations

This is a bounded reusable policy implementation, not end-to-end graph completion. Build is a fresh filesystem name walk plus a fixed set of Git subprocesses and one batch. Hard-excluded trees are pruned; Git-ignored trees are still enumerated for exact classification/tracked descendants, although their ordinary content is not read. This is not evidence of near-zero fresh CLI startup and may need build/inventory fusion or incremental snapshot maintenance after profiling. Permission errors during enumeration fail the build.

Build is not an atomic filesystem/index snapshot. The integration must detect watcher epochs/overflow or use a reconciliation fence/retry when changes overlap construction. No claim of race-free cross-file capture is made. Symlink paths are lexical; the walk does not follow symlink directories, Git does not follow symlink `.gitignore` files, and downstream side-read resolution needs its own explicit dependency/exclusion checks. Analysis rooted in a subdirectory deliberately excludes ancestor `.gitignore` rules outside that analysis root. Unknown in-root dependency names require reconciliation, as signaled by Known=false.

Consumers must include policy identity in cache/state migration, enforce exclusions before independent reads/detection/hash/name membership, preserve name-only membership for direct links, apply media promotion for content-consuming extractors, and rebuild on control changes without treating every index stat refresh as graph generation advancement. Dependencies outside the root require watcher coverage/polling. Removing owners after a policy transition, cache migration, fresh/delta graph equivalence, and no-publication/no-generation lock/media regressions belong to integration validation. Global ignore/info-exclude support, fully dynamic in-memory gitignore matching, Git-ignored-tree traversal optimization, and benchmark acceptance remain outside this implementation.
