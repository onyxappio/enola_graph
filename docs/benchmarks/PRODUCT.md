# Product performance investigation

Status: **the latest repeated Product validation passes initial throughput and
resident incremental latency checks**. All six scenarios and 114 assertions passed.
Initial is 9.119 s versus old 10.189 s; resident idle is 0.136 ms and body delta
0.739 s (medians). Fresh CLI idle still costs 2.078 s; broad history deltas remain
4.37–5.00 s. Memory is higher than old Enola. These limits are explicit in the
[verified comparison, spread and raw evidence](product-delta-2026-09-21/accepted-coalesced/README.md).

The sections below preserve earlier stages and failed comparisons as historical
evidence; they do not override the latest result or describe identical input scopes.

## Integration checkpoint b1820aa

The checkpoint is pushed to `origin/main` for Codata integration. Three isolated
CLI repeats after admission/transport optimizations measured initial **14.870 s**,
no-change **2.064 s**, body delta **2.936 s**, structural delta **3.064 s** (medians).
The old initial median is **10.105 s**, so initial acceptance still fails.
All twelve CLI assertions passed.

Resident body and structural medians were **0.745 s** and **0.800 s**; thirty idle
requests took **0.102–0.146 ms** with zero work. All nine cold graph comparisons
passed. The resident suite nonetheless failed a raw native-event quiet diagnostic:
its own metrics/log writes shared the watched external-config directory. A native
fixture confirmed the cause and root isolated the harness configuration; the
repaired full Product rerun is pending. No native production code change was needed.

[Raw post-push observations](product-delta-2026-09-21/postpush-observations/README.md)
and [native diagnostic investigation](product-delta-2026-09-21/postpush-observations/native-quiet-repair.md).

## Resident stage: preliminary single-run observation

Before repository input-policy integration, a real-fsnotify/NATS smoke on Product
observed initial including watcher setup at **16.394 s**, ten already-running idle
requests at **0.110–0.166 ms**, body edit at **0.961 s**, and structural edit at
**1.136 s**. Each transported graph matched a separate cold analysis. Idle had
zero reported work/events/generation changes. The body parsed one file; the
structural operation performed two parses. Coverage catch-up added no extra parses.

This was one run under concurrent development, not isolated performance acceptance.
The request-driven driver consumes real OS events but does not include production
Watch debounce. Fresh CLI startup is a separate measurement. Independent review
found config-reload/coverage correctness issues and harness acceptance gaps;
repairs and repeated validation remain required before declaring completion.

[Raw smoke evidence](product-delta-2026-09-21/resident-smoke/README.md).

Real-history resident smoke on the same stage measured **5.305 s / 348 parses**
for 27 changed paths and **10.812 s / 4,181 parses** for 100 changed paths.
Both graphs matched the cold target; both used filesystem-membership reconciliation.
The 100-path history also triggered the global TS configuration fallback. These
single, contended runs are not final acceptance and show that resident lifetime
alone does not solve broader invalidation costs. Evidence:
[27 paths](product-delta-2026-09-21/resident-history27-smoke/README.md),
[100 paths](product-delta-2026-09-21/resident-history100-smoke/README.md).

## Second optimization: verified intermediate measurements

Three sequential runs used the same pinned Product revision and full extractor
scope, a real NATS JetStream broker, and an independent protocol consumer. No
concurrent builds, tests or workers ran during timing. Full tests, race/coverage,
vet and independent correctness review passed; all 12 benchmark assertions passed.

| Operation | Old process median, s | Second candidate median (min–max), s | All events in NATS, s | Consumer applied, s |
| --- | ---: | ---: | ---: | ---: |
| Initial | 19.243 | 11.526 (11.424–12.377) | 11.335 | 11.454 |
| No change | 12.309 | 2.881 (2.828–2.978) | 0 events | 0 updates |
| Body edit | 20.220 | 4.395 (4.379–4.408) | 4.277 | 4.279 |
| Add exported function | 21.100 | 4.841 (4.814–4.903) | 4.711 | 4.713 |

Body and structural deltas are **2.62× and 2.38× faster than this candidate’s
initial**, respectively. This is a verified improvement, not final acceptance of
the requested fast incremental workflow. The consumer remains an in-memory
protocol consumer, not a Memgraph writer.

Safe glob prefilters, explicit content-input capabilities and separate name-set
inputs remove unnecessary content reads. The Product run hashes 90,567,325 bytes
across 9,030 paths instead of 1,357,597,803 bytes across 45,105 paths. Manifest and
Swift supplementary contexts preserve hidden inputs and removal detection;
opaque extractors retain conservative hashing. Prior synthetic owners are included
when replacing whole-extractor results. Cache version v273 forces migration.
Original detector and TS configuration traversal semantics remain intact.

Remaining measured no-op costs include detector discovery (~1.09 s), cached state
decode (~0.47 s), inventory (~0.43 s), input hashing/context (~0.52 s) and TS config
discovery (~0.17 s). Reducing these costs is the next optimization step.

Evidence: [raw results and reproduction scripts](product-delta-2026-09-21/verified-stage2/README.md),
[independent review](product-delta-2026-09-21/final-review.md), and
[correctness repairs](product-delta-2026-09-21/correctness-fixes.md).

## First optimization: historical Product measurements

The optimized candidate passed the performance and semantic acceptance gates on
this pinned Product revision. Values below are **medians of three runs (min–max)**,
measured sequentially with no concurrent builds or test workers. Every new initial,
no-change, body-edit and structural-delta process completed faster than its paired
old run. The 16.083 s initial median also beats the earlier 17.757 s old baseline.

| Operation | Old process, s | New process, s | All events in NATS, s | Consumer applied, s | Speedup |
| --- | ---: | ---: | ---: | ---: | ---: |
| Initial | 18.163 (17.440–18.431) | 16.083 (15.824–16.100) | 15.893 (15.639–15.927) | 16.033 (15.766–16.053) | 1.13× |
| No change | 12.045 (12.041–12.049) | 7.757 (7.306–7.961) | 0 events | 0 updates | 1.55× |
| Body edit | 19.111 (18.812–19.421) | 9.133 (9.066–10.231) | 8.986 (8.934–10.100) | 8.988 (8.936–10.102) | 2.09× |
| Add exported function | 19.802 (19.651–19.901) | 9.712 (9.663–10.741) | 9.606 (9.541–10.618) | 9.608 (9.543–10.620) | 2.04× |

Initial first batch arrived at a median **7.407 s**. The authoritative initial graph
contains **82,493 nodes and 188,879 edges**. Its 5,510 protocol messages contain
155.07 MiB of payload, with **zero empty data batches**. NATS End timestamps describe
server receipt after prior-message fences; successful producer process completion
also includes broker acknowledgment and local checkpoint promotion. The consumer
is the independent in-memory protocol consumer, not a Memgraph writer.

The body edit leaves the extracted graph unchanged; it is a real source edit, not
a no-op input. The additional structural edit appends an exported function calling
`normalizeEmail`; it adds one final node and four final edges. It was measured in
all three repetitions. All body and structural deltas exactly match their separate
cold analyses, including properties, source positions, ownership, resolution and
duplicate multiplicities. Each initial exactly matches the independent repaired
direct-IO reference; only the previously documented direct `performs_io` correction
is allowed relative to the original reference. All **12 benchmark assertions pass**.

| Resource / work | Initial | No change | Body edit | Structural edit |
| --- | ---: | ---: | ---: | ---: |
| FilesParsed counter | 4,181 | 0 | 1 | 2 |
| Protocol messages | 5,510 | 0 | 73 | 75 |
| New producer peak RSS, median MiB | 982.84 | 497.91 | 614.19 | 630.81 |
| Old producer peak RSS, median MiB | 553.02 | 465.33 | 604.92 | 583.25 |

Only one source file is edited. FilesParsed counts parsing work across invalidation
rounds, rather than promising distinct file identities. Changed-source runs also
conservatively rerun the five active non-TS extractor scopes; complete output
fingerprints avoid publishing unchanged contributions. The body replacement emits
1,432 nodes / 4,330 edges, structural replacement 1,441 / 4,371, including affected
resolution ownership; this is much smaller than the whole graph.

Completed analysis state is **55.51 MiB**. The maximum 100 ms sampled spool size,
including identity/commit metadata and temporary files, was **36.69 MiB** against
the unchanged 64 MiB configured bound, despite 155.07 MiB total initial output.
Sampling cannot prove the exact transient peak; independent cap and interrupted
compaction tests supplement it. State-file rewrites are separate from the delivery
spool bound.

The implementation still keeps analysis caches, intermediate results and a global
resolution index. Initial memory is higher than old Enola, and a no-change CLI
run still takes 7.757 s despite zero parsing/events. This establishes a Product
speedup, not subsecond online operation or a memory-free streaming analyzer.

### What removed the regression

- Parser publication performs bounded volatile queue admission, with no journal
  calls or disk-related locks. One worker validates durable identity and journals
  groups before pipelined broker delivery; Flush propagates failures before state
  advancement.
- Empty batches are omitted and small results are packed. Acknowledged payloads
  are reclaimed within the unchanged journal capacity; exact identity metadata
  is bounded and fails explicitly if exhausted.
- Necessary-condition scanner prefilters avoid expensive regex work while
  preserving the frozen extraction rules, including their boundary semantics.
- Per-extractor input hashes/contributions and synthetic provenance support
  correct reuse; complete content fingerprints cover properties and positions.

### Verification and artifacts

Build, full `go test ./...`, full race/coverage suite, and `go vet ./...` passed on
an immutable candidate source copy. Two copied test files were subsequently
formatted without semantic changes; two additional independent regressions
(existing-unacked retry and volatile End loss) were retained in the checkout
verbatim apart from gofmt and passed a focused race run. Production Go files
remain byte-identical to the measured build. Astra independently passed the full focused
race suites, original-Consumer multiset comparisons, true no-ops, every compaction
rename recovery, nonblocking admission, durable unacked retry, committed-tail
corruption rejection and real-broker output beyond 64 MiB. These are coverage
claims for the exercised scenarios, not exhaustive power-loss or language proofs.

- [Final paired measurements](product-2026-09-21/final-metrics.json),
  [checks](product-2026-09-21/final-checks.json),
  [wire inspection](product-2026-09-21/final-wire-inspection.jsonl)
- [Input and binary provenance](product-2026-09-21/final-provenance.json),
  [tested source hashes](product-2026-09-21/final-source-manifest.json)
- [Full tests](product-2026-09-21/final-tests.log),
  [full race/coverage tests](product-2026-09-21/final-race-tests.log),
  [vet](product-2026-09-21/final-vet.log),
  [independent review before Product timings](product-2026-09-21/independent-final-review.md)
- [Harness](product-2026-09-21/candidate.py),
  [summarizer](product-2026-09-21/summarize-candidate.py),
  [run log](product-2026-09-21/final-benchmark.log)

Final local run: `/tmp/enola-product-candidate3-results`; binary
`/tmp/enola-product-candidate2`, source `/tmp/enola-product-candidate2-source`.
The earlier candidate2 timing attempt stopped because a cold check reset the
single-context observer before a later structural delta. The harness now finishes
all mutations of a context before cold checks. No measurements from that partial
attempt enter the final medians. Summary selection uses exact scenario boundaries,
so `cold-body` and `cold-structural` never enter old-Enola groups. Product tracked
contents were restored, and owned brokers/consumers were stopped after measurement.

## Input and method

- Repository: `onyxappio/product`, commit `a609c19f3861971930fae7b33dcb2950598953c5`.
- 45,105 tracked files, including 5,295 `.ts` and 939 `.tsx`; inventory under the
  shared configuration: 42,583 non-test files and 2,522 test files. Tracked build
  and visual artifacts were not removed to improve the result.
- Host: Apple M1 Pro, 10 logical CPUs, 32 GiB RAM, macOS 26.3; Go 1.26.8 and
  NATS Server 2.11.9. No dependencies installed and no remote code execution.
- Old Enola: pre-change revision `d086926aca80975dde34094e99c84cce59fb1cd6`,
  built with the same Go toolchain. New baseline: working implementation binary
  whose SHA256 is recorded in `environment.json`.
- Full profile configuration: only `repo: /tmp/enola-product-benchmark-source`;
  both binaries inherit their configured extractor defaults. TypeScript profile:
  additionally `extractors: [typescript]`, `explainers: []`, `renderers: []`.
- Timed separate CLI processes sequentially. Initial means empty Enola cache/state,
  **not** flushed OS disk caches. Clone/build/setup time is excluded. Maximum RSS
  comes from macOS `/usr/bin/time -l` and describes the producer only.
- Old command: `enola --generate CONFIG`; new command:
  `enola graph analyze|delta --nats URL --state-dir STATE --context CONTEXT CONFIG`.
  `--json` was intentionally omitted because it additionally dumps every fact.
- A live durable NATS consumer decoded and applied the replacement protocol to
  the reference in-memory Consumer and acknowledged messages. This is not a
  Memgraph write benchmark. Broker End timestamp, first received batch and
  completed consumer application are measured from producer process launch.
- The controlled body edit changed `normalizeEmail` in
  `packages/crypto/src/password.ts`: `email.trim().toLowerCase()` became
  `email.normalize('NFKC').trim().toLowerCase()`. Only the isolated clone was edited;
  its tracked contents were restored afterward.

## Capacity failure in the standard implementation

The unmodified CLI failed initial analysis after **42.08 s** with
`graphstream journal: spool exceeds 67108864 bytes`. Acknowledged payloads remain
in the journal until the generation commits, making this a total-run size cap.

All successful new-baseline rows below therefore use an **experimental build**
with exactly one code change in an isolated source copy: journal `maxBytes`
increased from `64 << 20` to `1024 << 20`. No fsync, acknowledgment, extraction,
protocol, or correctness check was disabled. This change is **not** applied to
this repository and is **not** the proposed fix. Producer queue bounds stayed
unchanged. Old and new native output formats differ; these are end-to-end flow
measurements, not a pure parser comparison.

## Baseline timings

Seconds. The full-profile old initial/no-op and new no-op values are medians of
three completed runs; all other rows are single measurements. Expensive failed
and exploratory new runs are recorded separately, not mixed into medians.

| Profile / operation | Old: snapshot complete | New: process complete | New: first batch received | New: all events in NATS | New: consumer applied |
| --- | ---: | ---: | ---: | ---: | ---: |
| Full / initial | 17.76 | 469.03 | 8.61 | 467.98 | 468.16 |
| Full / no change | 12.47 | 21.59 | 10.03 | 21.39 | 21.49 |
| Full / one-file body delta | 19.91 | 454.07 | 9.92 | 453.38 | 453.53 |
| TypeScript / initial | 17.80 | 95.28 | 6.66 | 94.44 | 94.55 |
| TypeScript / no change | 12.70 | 17.24 | 8.49 | 17.13 | 17.14 |
| TypeScript / one-file body delta | 18.45 | 17.19 | 8.43 | 17.06 | 17.07 |

Old full initial range: 17.59–18.09 s; old no-op: 12.34–12.87 s;
new full no-op: 21.54–21.93 s. A 1.26 s advantage in the single TypeScript
body measurement is not evidence of a stable performance win.

The first batch contains provisional local data. A usable complete replacement
requires End and all declared batches, so time to first batch is not time to a
complete graph. "All events in NATS" is the server timestamp of End; successful
producer completion additionally waits for broker acknowledgment and checkpoint
promotion. No native queue stage exists in the old command; no old-to-NATS
adapter or Codata processor was included.

## Work and output

Counts below are emitted authoritative nodes/edges for each replacement, not
necessarily the final retained graph size. Message bytes include provisional,
scope, resolved and End messages.

| New baseline operation | FilesParsed counter | Messages | Empty data batches | Nodes emitted | Edges emitted | Payload MiB | Producer peak RSS MiB |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| Full initial | 4,181 | 48,663 | 36,254 | 82,493 | 188,879 | 164.92 | 1,745 |
| Full body delta | 281 | 44,389 | 36,288 | 81,763 | 188,462 | 105.19 | 1,578 |
| TypeScript initial | 4,181 | 10,162 | 165 | 60,626 | 160,675 | 135.24 | 1,386 |
| TypeScript no-op | 280 | 913 | 37 | 6,513 | 19,882 | 13.36 | 1,018 |
| TypeScript body delta | 281 | 917 | 37 | 6,538 | 19,948 | 13.40 | 976 |

Old initial emitted 85,516 facts with full profile and 63,649 with TypeScript-only;
these are old snapshot facts, not a promise of identical entity semantics to the
new owned protocol. The old full initial producer peak RSS median was 540 MiB.
New completed state files were approximately 143 MiB (full) and 119 MiB (TS-only).

## Correctness findings and optimization requirements

1. Full initial emits 36,254 empty data batches. Each goes through journal and
   broker. The ownership scope already describes empty replacements.
2. One TS body edit triggers `mdintent` whole-extractor fallback and broadens
   replacement ownership across the inventory. Only `mdintent` fallback was
   reported for that delta. This defeats the intended file-granularity benefit.
3. The journal synchronizes each payload and each ack to disk, and retains all
   acknowledged payloads until checkpoint. Bounded group commit and safe
   reclamation need proper crash/replay tests; simply raising the cap is inadequate.
4. No-op is broken: full-profile runs reparse 280 files, emit 1,201–1,207 messages
   and advance generations. More seriously, a fresh independent replay normalized
   the node/edge multisets and found **26,303 added and 6 missing items** after the
   first no-change run. Final counts changed from 82,493 nodes / 188,879 edges to
   88,908 / 208,761. Subsequent no-ops stabilize at that altered graph. This is not
   a JSON ordering artifact. TS-only no-op preserves the graph but still reparses
   and publishes unnecessarily.
5. Protocol sequence/digest/scope checks passed for these completed streams.
   That does not establish semantic correctness. A cold-equivalence check of the
   modified full Product graph has not yet been completed. Optimized acceptance
   must compare delta results with a fresh analysis and make no-op truly empty.

The user requires new initial broker-confirmed completion and delta to beat old
Enola on Product. Preserve facts and configured scope, make no-op zero-work at
extraction/publication level, repair cache ownership, remove redundant messages,
and optimize durable transport before claiming that requirement is met.

## Evidence

- [Raw baseline metrics](product-2026-09-21/baseline-metrics.json)
- [Independent broker replay](product-2026-09-21/baseline-wire-inspection.jsonl)
- [Environment and binary hashes](product-2026-09-21/environment.json)

Local detailed logs and exploratory harnesses: `/tmp/enola-product-bench/`.

## Optimization diagnostic — not accepted

A frozen intermediate build with batch packing, empty-batch removal and grouped
journal publication completed one full initial run in **46.275 s** (broker End
46.093 s, first batch 11.677 s). It emitted 5,510 messages with zero empty batches,
82,493 nodes and 188,879 edges; peak RSS was 1.14 GB. This remains slower than the
old 17.757 s median. No optimized delta result is accepted yet.

This single diagnostic used the same physical Product checkout but its harness
resolved `/tmp` to `/private/tmp`, changing repository identities in the graph.
The harness now preserves the original `/tmp` spelling. A separate instrumented
run with the corrected path reproduced the exact original cold graph hash
`79e537d738832cd975a4d8b679bd16b9a5c06d7c88c0f41c92a4088d50e8054f`.
Neither run establishes acceptance of the known remaining delta and durability
defects identified by independent review.

CPU profiling found 21.29 s in regular-expression matching (63.13 s total CPU),
including server-binding, router and HTTP-client scans. These cumulative values
overlap and must not be added. The process allocated approximately 5.98 GB over
the run, including 1.42 GB in file reads and 1.02 GB in tree-sitter node wrappers.
The instrumented run is for hotspot diagnosis, not benchmark timing.

The old implementation's separate CPU profile performs approximately the same
CPU work (62.30 s, including 22.28 s of regex matching). Those scans are therefore
shared optimization targets, rather than the cause of the streaming regression.
A blocking profile isolates journal contention: `Journal.Sync` accounts for
78% of attributed mutex wait time; producers wait inside `appendUnsynced` while
the commit worker holds that lock. These aggregate waits overlap across workers
and are not elapsed-time measurements. The next transport change must decouple
bounded enqueueing from the disk-sync lock while retaining durable-before-send
ordering.

- [Diagnostic initial metrics](product-2026-09-21/diagnostic-pilot-metrics.json)
- [CPU profile summary](product-2026-09-21/cpu-cumulative.txt)
- [Allocation profile summary](product-2026-09-21/allocations.txt)
- [Old CPU profile summary](product-2026-09-21/old-cpu-cumulative.txt)
- [Blocking profile](product-2026-09-21/block-profile.txt)
- [Mutex contention profile](product-2026-09-21/mutex-profile.txt)

Independent review also found that streaming removed `performs_io` even when
`io_direct=true`, contrary to the agreed direct-IO contract. The original broker
graphs were replayed and an explicit expected reference was derived by adding
only that direct synonym to 139 nodes in each profile. The
[reference manifest](product-2026-09-21/direct-io-reference.jsonl) records original
and expected hashes. Future cold comparisons must explain any additional
difference; self-consistent fact suppression is not a valid optimization.
