# Repository rules for agents

The agreed streaming/delta implementation contract is in
[docs/STREAMING_INCREMENTAL.md](docs/STREAMING_INCREMENTAL.md). Use whole files as
the reanalysis unit and publish file-owned replacement scopes through NATS JetStream;
downstream projects own database integration.

## Enola fork and upstream updates

Continue developing this fork of Enola. Reuse its extraction rules and implementation;
do not replace it with a tree-sitter-only rewrite as the default strategy.
Our priorities are fast initial graph analysis with streaming output, correct incremental
delta analysis, and optionally watch mode. Codata can adapt to this fork and does not
constrain its architecture. External TypeScript compiler enrichment is outside the
current design scope.

## Performance acceptance

Initial graph production and file delta must outperform the pinned old Enola on
the same real project, host, input revision and configured analysis scope. Measure
initial completion through all broker acknowledgments, alongside time to first
batch, delta latency, memory and actual parsed files. Use repeated measurements
and report their spread. The Product baseline and investigation are recorded in
[docs/benchmarks/PRODUCT.md](docs/benchmarks/PRODUCT.md).

Performance acceptance also requires exact cold-versus-delta graph equivalence
under this fork's semantics. Unchanged inputs must cause zero file parses, zero
published events and no generation advancement. Preserve durable replay and
bounded backpressure while optimizing transport. Label experimental builds and
profile changes explicitly; a larger journal limit alone does not satisfy this
requirement.

A single-file delta must also be substantially faster than this fork's own initial
analysis, not merely faster than the old full-run baseline. The measured Product
delta of 9–10 seconds versus a 16-second initial is not accepted as the final
incremental performance target. Profile no-change and changed-file startup costs
separately, remove avoidable whole-project work, and report both the absolute
latency and the ratio to initial. Do not obtain speedups by missing source changes
or weakening cold-versus-delta equivalence.

The subsequent 2.88-second no-change run and 4.40–4.84-second deltas are also
intermediate results, not completion. The user explicitly requires near-zero
no-change latency and much faster single-file edits. Distinguish complete fresh
CLI startup from an already-running session; never imply daemon measurements
establish cold-start performance. Add real multi-file Product history scenarios:
initial at an older main revision, delta to pinned current main, and exact graph
equivalence to a fresh target-revision analysis. Include source-oriented and
manifest/config-changing histories, reporting broader fallback work honestly.

The user also rejects the 100-path history parsing all 4,181 TS files merely
because a package/config fingerprint changed. Narrow invalidation to the actual
configuration and dependency scope, and distinguish reparsing local facts from
recomputing resolution. Lockfile exclusions alone do not close this requirement.
Prove the scope with concrete changed inputs, parsed-file counters and cold graph
equivalence; retain whole-project fallback only for genuinely global or unknown
semantic changes, reporting the reason explicitly.

## Dependency lockfiles in graph analysis

The user explicitly excludes dependency lockfiles from this fork’s graph-analysis
inputs. A lock-only add, edit, rename or removal must not change graph facts,
trigger parsing/publication, or advance generation. Apply this consistently to
inventory/name membership, content fingerprints, TS config input discovery,
manifest side reads and resident change classification; ignore globs alone are
insufficient. Preserve declared dependencies and constraints from manifests such
as package.json, which remain semantic inputs. Do not emit lock-resolved versions
from ignored lock data. Keep any retained legacy lock-aware behavior explicit
and isolated from the graph profile, and version/migrate cached contributions
when this policy changes. Validate cold/delta equivalence under the new policy
rather than silently reusing old lock-aware hash oracles.

## Repository input policy

Graph analysis must support repository-local Enola configuration layered over
safe defaults, including project-specific exclusions. Respect nested `.gitignore`
rules, ordering and negations with Git semantics: tracked files are not excluded
by `.gitignore`, while explicit Enola exclusions apply to tracked files too.
Product excludes `apps/mobile/e2e/artifacts/**` and `worker-reports/**` through
its repository configuration. Do not generalize those project-specific names
into universal exclusions.

Apply policy consistently to inventory, names, fingerprints, side reads and
resident events. Policy/config changes must reconcile the analysis scope and
remove obsolete file-owned contributions. Preserve necessary semantic inputs;
do not blanket-ignore JSON/YAML or all generated source. Opaque media may retain
names for references without hashing content only when all active consumers
support that distinction. Unknown consumers remain conservative.

## Local facts, direct dependencies, and fast deltas

The target analysis architecture prioritizes performance, fast delta processing, and
independent updates to unaffected subgraphs. Emit locally established facts and direct
relationships. Do not materialize transitive attributes or propagate them through the
dependency graph as part of extraction or incremental graph maintenance.

For example, record an explicitly detected IO operation at its source. Do not mark all
of its transitive callers as `performs_io`. Indirect IO involvement belongs in graph
reachability queries or a separate downstream analysis layer. The TypeScript profile
retains `io_direct` and `performs_io`; both describe direct IO under this fork's
contract. Preserve this intentional semantic difference when adapting upstream tests.

A local attribute change must not by itself trigger a transitive cascade of file
reanalysis or writes to otherwise unaffected subgraphs. Preserve the correctness of
direct relationships: symbol deletion, renaming, re-exports, resolution changes, and
configuration changes can still require updating references in other files. Removing
attribute propagation does not justify stale edges or a universal one-hop invalidation
limit. Recompute the affected resolution scope, conservatively broadening it when
required for correctness, without reintroducing transitive attribute materialization.

This is an intentional difference from upstream semantics. During upstream updates,
document and adapt tests that specifically require transitive attribute propagation
to this fork's local-fact contract; preserve unrelated assertions and regression
coverage. Validate delta results against a full analysis under this same contract.

These rules govern new graph extraction and incremental maintenance. Audit retained
extractors against them when expanding the supported incremental profile.

## Authoritative invalidation planning: simplicity first

For the upcoming Codata replacement contract, the user prioritizes a simple,
maintainable file-dependency planner over a minimal replacement scope. Compute
a conservative, complete owner scope before BeginReplace and freeze it for that
run. Prefer file-level dependencies and conservative fallback rules; do not add
fine-grained symbol/reference indexes merely to reduce the number of invalidated
files at this stage. Broader scopes are acceptable. This updates earlier scope
narrowing priorities; it does not permit missed invalidations or weaker delivery
and recovery guarantees.

The prior file graph alone cannot represent dependencies that did not previously
exist. Additions, deletions, renames, configuration changes and resolver behavior
that can affect references without an old file edge require conservative fallback.
Use a wider proven analysis domain, including the whole repository when no smaller
safe boundary is established. Do not assume every ordinary source edit is local
if the active resolver can introduce nonlocal name-resolution changes.

Every owner included in the replacement scope must receive its complete current
contribution, possibly reused from valid caches, or an empty replacement when it
no longer contributes facts. Scope size and reparsed-file count are distinct.
Do not silently grow the scope after Begin; an out-of-scope contribution must
fail the run without a successful End or completed-generation advancement.
Dependency traversal for invalidation must not introduce transitive attribute
propagation. Measure scope size, actual parses and latency before considering
further narrowing complexity.

This section records the agreed target; it does not assert that the current
streaming protocol already implements a frozen full scope on Begin.

## Upstream update procedure

Use upstream tests and implementation changes to maintain compatibility with
[enola-labs/enola](https://github.com/enola-labs/enola):

1. Record the previously integrated upstream revision and the target revision for each
   update. Review changes to code, tests, fixtures, schemas, and public contracts.
2. Bring in the target revision's upstream tests, including their fixtures and golden
   files. Preserve their behavioral assertions. Adapt test wiring where our internal
   architecture differs; do not weaken assertions just to make the fork pass.
3. Use AI agents to adapt our implementation based on the upstream code and tests,
   while retaining this fork's streaming and incremental architecture. A wholesale
   merge of upstream internals is not required.
4. Retain our own regression tests alongside the upstream suite, including tests for
   initial analysis, delta correctness, streaming, and watch as these are implemented.
   Check incremental results against a fresh full analysis of the same inputs and
   analysis configuration. Check performance for affected analysis paths.
5. Report the integrated revision, checks run, failures or unavailable checks, and any
   intentional compatibility differences. For upstream features outside our supported
   scope, document the exclusion explicitly rather than silently skipping failures.

Passing tests is evidence of compatibility for covered behavior, not a guarantee of
complete equivalence. Review changed contracts and add coverage for relevant gaps.
This policy describes future updates; it does not itself request an upstream sync.

See [CONTRIBUTING.md](CONTRIBUTING.md) for build and validation instructions.
