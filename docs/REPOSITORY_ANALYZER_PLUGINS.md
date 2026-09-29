# Repository-owned analyzer plugins

Repository analyzer plugins add graph facts for conventions that belong to a
repository. Enola remains responsible for graph vocabulary, fact validation,
file ownership, cached observations, replacement scope, and publication. V1
does not add Product-specific recognizers to Enola.

## Registration and trust

Register a plugin in the repository's `enola.yaml`:

```yaml
analyzer_plugins:
  - path: tools/analyzers/example
    config:
      profile: repository-example
```

Each registered directory contains an `enola-plugin.yaml` manifest and a
self-contained Node bundle. The manifest declares an exact Node version, the
entry bundle, identity files, registered vocabularies, claimed machine ids,
and allowed owner paths:

```yaml
api: enola.plugin/v1
name: example
runtime:
  kind: node
  version: 22.14.0
  entry: dist/plugin.mjs
identity_files:
  - dist/plugin.mjs
vocabularies:
  - enola.fsm@1
claims:
  machines:
    - example-machine
owner_domain:
  - src/**
```

The operator must allow configured code explicitly for each graph command:

```sh
enola graph analyze . --events .enola/graph-events.jsonl --allow-repo-plugins example
enola graph analyze . --events .enola/graph-events.jsonl --allow-repo-plugins example --plugin-verify
```

Unknown manifest fields, unsupported vocabularies, duplicate claims, missing
bundle or identity files, path escapes, and Node version mismatches fail before
graph replacement begins. Dependency lockfiles cannot be plugin identity
inputs and are excluded from plugin file queries and reads.

## Process and protocol

For an invalid plugin plan or unit, Enola launches one Node process for that
plugin in a temporary working directory and exchanges newline-delimited JSON
messages over stdin/stdout. A fully unchanged run validates stored observations
without starting Node. The process receives a minimal environment; repository
code is not run unless its plugin name is in the operator allow-list.

The protocol has four host requests:

- `hello` negotiates the API, runtime, vocabulary, config, and concurrency.
- `plan` returns stable unit declarations and their `consumes` summary
  dependencies.
- `run` evaluates one invalid unit at a time in deterministic dependency
  order. It returns owner contributions and an optional summary.
- `shutdown` ends the process.

Plugins request repository inputs through host callbacks: `read`, `probe`,
`list`, `resolve_module`, `resolve_export`, and `summary`. Enola records each
answer. Reads and resolver candidates are rechecked before successful
publication; a changed observation invalidates its unit. A summary read is
accepted only when the consuming unit declared that dependency in `consumes`.
Consumers rerun when a producer's canonical summary changes.

Each unit result maps repo-relative owners to nodes or anchors. Nodes may use
the host-registered `enola.fsm@1` vocabulary and plugin-scoped `extraction`
coverage facts. Enola validates machine claims, relation kinds, node identity,
owner domain, and current inventory membership, then canonicalizes result
ordering. The host stamps plugin provenance. Anchors bind to TypeScript symbols
using Enola's source-span rules; unbound anchors produce partial-coverage facts.

Malformed protocol, undeclared summary reads, invalid output, a process failure,
or an out-of-inventory owner aborts the run before `BeginReplace`. The last
committed graph and generation remain in place. Removing a plugin from config
is explicit and replaces its previous contributions with empty output.

## Cache and migration boundaries

Plugin observations and unit outputs live in graphsession state, separately
from the TypeScript extractor cache version. This worktree is rebased on
`origin/main` at `6f06be9e`, whose extractor cache version is `v322`; this
plugin work leaves that version unchanged. Accuracy owns the `v322` semantics;
do not change them here. Recheck the accepted main revision and cache version
before landing. Reserve `v323` only if plugin
invalidation demonstrably requires a global extractor cache bump and the
accuracy/performance owners coordinate that change. A plugin cache schema
change does not by itself allocate a new extractor version.

Product migration is tracked separately in `product/T-606`. It owns the
repository adapter, differential parity against the pinned Product oracle, and
the Product performance matrix. T-001 supplies a generic Node fixture in
`internal/graphsession/analyzer_plugins_test.go`; it does not claim Product
parity. Numeric overhead budgets remain deferred by the owner; before delivery,
record initial, changed-file, and no-change overhead with parsed-file counts,
replacement owners, unit/process counts, latency, and memory.

## Focused checks

```sh
go test ./internal/analyzerplugin ./internal/graphsession -run '^Test(RepositoryAnalyzerPlugin|PluginInputPaths|RepositoryGlob|CanonicalizeResult|ValidateResultRequiresDistinct|RuntimeDigest)' -count=1
go test ./internal/extractors/tsextractor -run '^TestPlugin(Module|Export)Resolution' -count=1
```

## Generic host measurements — 2026-09-26

These measurements use a two-TypeScript-file fixture and the generic Node
plugin above. The control copy contains the same plugin files but does not
register the plugin; only `analyzer_plugins` differs. Three histories were run
in alternating control/candidate order. Each history used three fresh CLI
processes with `--authoritative-scope`: initial analysis, a body-only edit to
`src/a.ts`, then an unchanged run. Wall time is the complete CLI invocation;
RSS is the median OS-reported maximum resident set from `/usr/bin/time -l`.
The host was macOS 26.3 arm64, Go 1.27.1, Node 24.20.0, with this worktree's
binary built from base `6db3ed8` plus the uncommitted T-001 implementation.

| Scenario | Control wall time, median [min–max] | Plugin wall time, median [min–max] | Plugin / control | Parsed files | Replacement owners, control → plugin | Plugin work | Max RSS, control → plugin |
|---|---:|---:|---:|---:|---:|---|---:|
| Initial | 0.2878 s [0.2760–0.2913] | 0.3663 s [0.3556–0.3712] | 1.273× | 3 | 7 → 7 | 1 process, 1 plan, 2/2 units executed | 37,076,992 → 45,891,584 bytes |
| Single-file delta | 0.2695 s [0.2574–0.2699] | 0.3446 s [0.3408–0.3554] | 1.279× | 1 | 1 → 7 | 1 process, no replan, 1/2 units executed and 1 reused | 33,308,672 → 45,645,824 bytes |
| No change | 0.0836 s [0.0834–0.0863] | 0.0828 s [0.0818–0.0846] | 0.990× | 0 | 0 → 0 | 0 processes, 0 plans, 0/2 executed and 2 reused | 25,690,112 → 26,001,408 bytes |

The no-change run published zero owners, held generation at 2, and started no
plugin process. A changed file ran one of the two cached plugin units and
conservatively froze all seven admitted owners; the no-plugin control replaced
one owner. This confirms the intended V1 broad-scope behavior on the fixture.

The fresh-CLI single-file delta is 0.94× the plugin initial time on this tiny
fixture, where process startup dominates both runs. Plugin registration adds
about 76 ms to initial analysis and 75 ms to the edit run here; no-change is
within the control spread. The fixture is too small to decide whether the
Product workload needs optimization, and it does not meet the repository's
real-project performance acceptance by itself.

The same-process resident API was measured separately by
`TestRepositoryAnalyzerPluginResidentRunMetrics` (`go test ... -count=3`). It
keeps one `graphsession.Resident` open and records per-run Go heap allocations;
these heap-allocation deltas are not OS peak RSS and are not compared directly
with fresh CLI measurements.

| Resident scenario | Wall time, median [min–max] | Go heap total-alloc delta, median [min–max] | Parsed files | Replacement owners | Plugin work |
|---|---:|---:|---:|---:|---|
| Initial | 300.773 ms [299.379–337.479] | 3,596,600 bytes [3,592,168–3,834,024] | 3 | 6 | 1 process, 1 plan, 2/2 units executed |
| Single-file delta | 299.324 ms [298.129–301.513] | 2,307,008 bytes [2,303,024–2,392,024] | 1 | 6 | 1 process, no replan, 1 executed and 1 reused |
| No change | 51.559 ms [50.200–64.807] | 494,176 bytes [493,376–508,152] | 0 | 0 | 0 processes, 0 plans, 2 units reused |

The resident delta also preserves exact graph equality with a fresh analysis.
These fixture results record overhead and scope without setting numeric
acceptance budgets. `product/T-606` remains responsible for the pinned Product
adapter, real-project cold/delta/no-change history, and the decision whether
the observed plugin startup and broader replacement scope warrant optimization.
