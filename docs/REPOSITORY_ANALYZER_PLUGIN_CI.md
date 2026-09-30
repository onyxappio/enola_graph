# T-001 CI remediation evidence

Date: 2026-09-30. Reviewed feature baseline: `81b32c1a03312e47e1b88f2ebefa02a7c0043dcc`.
Default branch baseline: `6f06be9ef1d9e9f020489702bfad92fcf4fa6726`.
PR: https://github.com/onyxappio/enola_graph/pull/2.

## Failures and corrections

The original GitHub run `36599623600` passed Windows path checks,
determinism, architecture, documentation, vulnerability and workflow checks;
its full test and lint jobs failed. This is not green CI or delivery evidence.

- V1 plugin identity depended on runtime Go build metadata and readable module
  cache parser sources. Go 1.26.8 test binaries omitted those dependency
  records. Released hosts also cannot require a development module cache.
  Grammar identity is now generated from the pinned parser/scanner sources and
  embedded in the host. Available executable dependency metadata must match
  the generated version and module sum, with replacements rejected. A source
  regression independently verifies the generated digest. Regenerate with
  `go generate ./internal/analyzerplugin` after reviewing a grammar update.
- Changing `Store.BuildGraph` to return an error introduced unchecked calls in
  existing tests. Those callers now fail on errors rather than discarding them.
- Mutable `OnBeforeParse` test callbacks raced because extraction workers call
  the hook concurrently. Test callbacks now synchronize their state; the
  callback concurrency contract is documented. These were test-state races,
  not evidence of a production extraction race.
- The cold manifest-reload regression expected a free owner to publish during
  plugin preparation. The reviewed host contract prepares plugins before
  BeginReplace. The regression now positively observes entry to a blocked
  plan, requires no publication before preparation settles, and verifies both
  the newly covered plugin owner and the unrelated owner in the final graph.
  It retains the manifest-reload assertion.
- The successful Go fixture uses explicit, longer hello/unit limits so a
  concurrent full-suite run does not assert startup speed accidentally.
  Separate strict deadline tests retain their timeout assertions.

## Local checks

Use `GOTOOLCHAIN=go1.26.8`, matching `go.mod` and GitHub CI. Local Go 1.27.1
cannot be used to evaluate the pinned golangci-lint binary's type checking.

Focused grammar, plugin runtime, callback race and graph API migration checks
passed with the race detector. The final combined full-suite run was not green:
the TypeScript decoy test reached its 20-minute package timeout, the native
quiet test failed under broad load, and the Git metadata settle test failed.
The complete `internal/analyzerplugin` race/coverage package passed in 15.485s.
The expanded plugin/grammar/resolver/race selection passed across the host,
graphsession and TypeScript packages; graphsession completed in 105.785s.
Pre-push cache-version, documentation, golden and determinism gates passed.

A separately built Go 1.26.8 CLI ran the V1 plugin fixture with `GOMODCACHE`
pointing to a nonexistent directory. The initial run started one process and
executed one unit; the unchanged fresh CLI reused that unit, started zero
processes, parsed zero files, appended zero event bytes, and kept generation
at one. A second CLI probe used the Go V2 fixture with a runtime PATH that
contains neither Go nor Node. It created namespaced task facts, reused its
unit with zero processes/events on no-change, reran the unit after a Markdown
edit, and exactly matched the fresh target-input cold Facts. These are
portability/correctness probes, not performance benchmarks.
GitHub now has an explicit 45-minute package budget and runs the complete suite
twice: once with race detection, once with atomic coverage. Every test and
assertion remains in both runs. Separating the instrumentations avoids tracing
an atomic coverage-counter write on every hot-loop iteration. This changes
the execution setup, not a correctness assertion or an analysis performance
acceptance threshold.

The complete local command
`GOTOOLCHAIN=go1.26.8 go test -race -count=1 -timeout=45m ./...`
finished with exit zero: 113 tested packages passed. GitHub run
[`36667989812`](https://github.com/onyxappio/enola_graph/actions/runs/36667989812)
on feature commit `23f8348b43033f8b087b049c1d76616ace5fd2f2` finished
both complete race and atomic-coverage steps successfully. Windows,
determinism, architecture, documentation, vulnerability and workflow checks
also passed. The run's overall conclusion remains failure because lint is
still red; this is not green-CI or delivery evidence.

The original CI also timed out in the inherited
`TestScanOpt_LargeDecoyMatchesBaseline` after it had run for 9m23s. A prior
local graphsession run exhausted its overall ten-minute package budget while
its current test had run for only one second. These observations require
separate diagnosis; they are not proof of a deadlock. A live local sample
identified the decoy workload scanning lexical bindings and repeated
`enclosingBlockEnd` searches, while the graphsession sample was executing the
pinned Product FSM delta. Neither sample showed a blocked test. The quadratic
extractor work is inherited and has not been changed by this plugin task.
The exact decoy test passed with race detection alone in 209.887s. The native
quiet test passed all three focused repetitions. The Git metadata settle
failure also reproduced on an unchanged export of default-branch `6f06be9e`
on this macOS host; its source and implementation paths were not changed by
T-001. The original failures remain recorded evidence; the subsequent complete
local race and GitHub race/coverage runs passed without extractor/watch
semantic changes.
The decoy test also passed with atomic coverage alone in 98.034s; the full
TypeScript package passed in the separate race run in 257.488s. The separate
complete local and GitHub results are recorded above.

## Inherited lint blocker

With identical Go 1.26.8, golangci-lint 2.12.2 and whole-tree configuration:

| Source | Findings | errcheck | staticcheck | unused | ineffassign |
|---|---:|---:|---:|---:|---:|
| Unchanged default branch | 289 | 220 | 36 | 27 | 6 |
| T-001 after remediation | 288 | 220 | 35 | 27 | 6 |

Comparing filename, linter, message and source-line multisets gives **zero
introduced findings**, with one inherited simplification resolved. Line
numbers alone are unsuitable because T-001 edits shift existing lines.

The remaining 220 unchecked calls include 153 test calls and 67 production
calls. The lint configuration and whole-tree CI gate remain unchanged.
An ownership decision for separate inherited-debt remediation is pending;
this report does not waive the green-CI requirement.

## Evidence locations and limits

Local diagnostic logs are ephemeral and not required for plugin use:

- `/tmp/t001-original-ci-failures.log`: original GitHub test/lint output.
- `/tmp/t001-latest-focused.log`: latest cold-domain and Go-runtime checks.
- `/tmp/t001-final-full-tests.log`: prior failed combined race/coverage run.
- `/tmp/t001-final-race-alone.log`: complete local race PASS (113 packages).
- `/tmp/t001-pushed-lint.json`: current whole-tree lint findings.
- `/tmp/t001-main-lint.json`: unchanged-main whole-tree baseline findings.
- `/tmp/t001-portable-smoke-result.json`: built V1 CLI portability/no-change probe.
- `/tmp/t001-go-cli-smoke-result.json`: built V2 CLI initial/no-change/delta/cold probe.

This remediation preserves extractor cache version `v322`; plugin protocol
and unit-record schema versions remain separate. Product and tasks-hub
domain adapters remain the responsibility of their linked follow-ups.
