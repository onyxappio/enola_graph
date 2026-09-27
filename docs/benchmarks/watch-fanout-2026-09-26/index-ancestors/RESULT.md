# Experimental index ancestor deduplication

The fresh Git index is still read on every invocation. While decoding names,
stop walking parents when the current read has already inserted a directory
and all its ancestors. No additional persistent index or cross-run cache.

Diagnostic command: `ENOLA_INDEX_BENCH_REPO=/tmp/enola-stage16-root-review/product-source go test ./internal/graphinput -run ^$ -bench BenchmarkIndexNamesProduct -benchtime=5x -count=3`.

Mac Apple M1 Pro; shared host, not a quiet acceptance cohort. The benchmark
checks exact equality of both maps against the previous loop before timing.
Git execution, filesystem inventory and complete CLI runtime are excluded.

| Decoder | Three ns/op observations | Median |
|---|---|---:|
| Previous repeated ancestors | 30942033, 31336717, 30591733 | 30.942 ms |
| Unique ancestor visits | 13415792, 13368325, 14360375 | 13.416 ms |

About 17.5 ms saved in this diagnostic; allocations remain about 9.14 MB.
Do not translate this into a whole-CLI improvement. Broader fresh-noop work
remains open, particularly state decode and repeated discovery.

Full graphinput suite passed (7.789s) before the helper extraction. Focused
graphinput policy/admission tests passed after extraction (3.721s). Focused graphsession policy/admission/fresh-noop/lock regressions passed
(24.570s, exit0). Whole-CLI performance acceptance and publication are pending.
