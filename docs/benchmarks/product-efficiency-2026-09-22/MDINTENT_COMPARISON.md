# Product Markdown scope comparison

Three final-source candidate runs each replace **18 owners instead of 911** and
send **798604 JSON bytes instead of 9656176**. Exact cold graph equality and
stable 45110-file input fences passed in every baseline and candidate run.
This establishes a scope and publication reduction, not an overall latency win.

## Workload and provenance

Product revision `a609c19f3861971930fae7b33dcb2950598953c5`, full extractor profile,
repository scope configuration, 5-second collection window. Each separate checkout
replays the same five-file invitation-link burst, then remains under observation
for the requested 180-second window and is checked against a cold analysis. Each
run has exactly one initial generation and one replacement generation; all five
edited files were observed. Runs were sequential, not concurrent.

Baseline Enola: `1bcbae0a6e2a409fdaf09e323e75af174d0eef70`.
[Final candidate provenance](mdintent-final-provenance.json) records a private,
unpushed benchmark commit and the exact file hashes reviewed and tested. Its
experimental label does not imply a release in main. The same frozen harness was
used for both series.

## Repeated measurements

Times are median [minimum–maximum] across three samples per build.

| Metric | Baseline | Final candidate |
|---|---:|---:|
| Watch launch → initial consumer applied | 9.646 [9.037–9.783] s | 10.781 [9.418–12.674] s |
| Watch launch → initial frame observed | 10.005 [9.354–10.064] s | 11.105 [9.697–12.978] s |
| Delta Begin → End | 1.351 [1.348–1.357] s | 1.175 [1.115–1.252] s |
| Last burst fsync → consumer applied | 7.963 [7.941–8.096] s | 8.048 [7.926–8.134] s |
| Begin file owners | 911 | 18 |
| TypeScript-session parsed files | 18 | 18 |
| Replacement batches | 237 | 34 |
| JSON payload bytes including Begin/End | 9656176 | 798604 |
| Replacement node records | 10265 | 517 |
| Replacement edge records | 15160 | 2159 |

The scope is 50.61× smaller and payload 12.09× smaller. The median Begin-to-End
interval is 13.0% lower, while full observed delta latency is 1.1% higher with
overlapping ranges. Initial median is 11.8% higher with substantial spread; the
candidate minimum falls inside the baseline range. These observations neither
establish an overall speedup nor isolate the cause of initial variation. The
baseline series predates the candidate series; a fresh baseline control is reported below, separately from the original three
samples.

## What the patch changes

Before Begin, mdintent compiles captured Markdown bytes once, compares per-owner
contributions, and seeds only changed owners plus candidate-name dependents. The
later extraction path consumes that prepared output. Complete facts and cache
state remain available for resolution; the frozen manifest excludes unchanged
Markdown contributions. Changed hashed inputs and capture failures fail closed;
captured bytes are checked again before End. Other extractors retain their own
conservative owner scope.

This removes the 893 unchanged Markdown owners from this burst's replacement.
It does **not** remove their parsing: all required Markdown inputs still compile
once during preview. End parsed_files/cached_files report only TypeScript-session
work. Markdown deletion/rename can still trigger the existing whole-domain
fallback when the prior dependency graph cannot prove a smaller safe scope.

## Validation and limits

[Validation evidence](mdintent-validation.json) records worker-reported tests on
the frozen hashes: graphsession 194.53 s wall, command 5.87 s, mdintent 1.08 s and
vet, all exit 0. Negative controls verify the captured-source fence, fail-closed
input handling and per-extractor scope decision. The failed-run tests assert no
successful End, unchanged completed state and recovery to cold equality.
[Independent gates](mdintent-final-gates.json) cover cache version and golden /
determinism checks (6.561 s, exit 0). Documentation checks also passed.

Initial timing starts at process launch and excludes checkout setup. Consumer
completion and harness frame observation are distinct. Fsync-to-consumer includes
the 5-second collection window, planning and publication; it is a same-host wall
observation, not an instrumented watcher capture timestamp. Begin-to-End alone
omits planning. Payload excludes broker framing; records are replacements, not
new entities. These bursts followed by idle observation do not establish sustained
editing, broker recovery or long-term memory behavior.

Raw baseline reports: [1](mdintent-baseline-1.json), [2](mdintent-baseline-2.json),
[3](mdintent-baseline-3.json). Final candidate reports:
[1](mdintent-candidate-1.json), [2](mdintent-candidate-2.json),
[3](mdintent-candidate-3.json). [Derived data](mdintent-comparison.json) preserves
sample rows and local artifact locations. None of these six runs was excluded.

## Exploratory run excluded from final comparison

The earlier [exploratory run](mdintent-exploratory-1.json) also passed cold equality
and stable input fences, with 18 owners, 34 batches and 798604 bytes. Its
Begin-to-End interval was 1.078 s and fsync-to-consumer 7.901 s. It contained
per-source debug logging and predates final test/profiling refinements, so it is
excluded from the final series. [Exploratory provenance](mdintent-exploratory-provenance.json)
identifies its private source commit. Earlier excluded membership experiments are
listed in [REBOUND_PRODUCT.md](REBOUND_PRODUCT.md).

## Fresh baseline control

The unchanged baseline was rerun after all candidate samples, with cold equality
and stable input fences. Initial consumer completion was 8.869 s, delta
Begin-to-End 1.439 s, and last-fsync-to-consumer 8.028 s. Its scope remained
911 owners and payload 9656176 bytes. See [raw control](mdintent-control-1.json)
and [derived control metrics](mdintent-control-summary.json).

The control reinforces that no complete-cycle latency gain has been established.
The initial variation is unresolved; this checkpoint is accepted for the tested
correctness and publication reduction, not as proof of faster initial or delta.
Further phase profiling and cache reuse remain required.
