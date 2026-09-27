# Delta attribution after Stage38 — diagnostic only

Overlay binary cfbb3a1, source checkout7877cef; adds a nested two-phase timer only. Session1837 exited0. Seven CLI graph hashes equal the uninstrumented Stage38 candidate; four cold/noop checks passed. Single shared-host instrumented run, no acceptance or speedup claim.

| Phase | Body delta | Structural preview | Structural reanalysis |
|---|---:|---:|---:|
| MapFiles | 12 ms | 12 ms | 304 ms |
| Aggregation (including fact cloning) | 42 ms | 45 ms | 33 ms |
| Parsed files | 1 | 1 | 21 |

The two child phases partition the existing ts_mapfiles_aggregate parent; do not add them to that parent. Aggregation includes operations other than cloning, so this does not isolate clone-only cost. These are separate structural passes, not duplicate timestamps.

For body/structural respectively, the full-record source fence rereads6801 records and takes250/242ms; pending-state writes take466/512ms. Initial MapFiles5.558s dominates its36ms aggregation. The earlier composite delta span did not justify prioritizing aggregation: current evidence points first to source-fence/state overhead for body edits and dirty parsing/resolution for structural edits. Do not skip fences or materialize transitive attributes to obtain gains. Next experiment must attribute state encode versus durable-write costs and buffer/allocate costs in the fence, retaining exact content proof. No runtime optimization is implemented by this diagnostic.

README and provenance preserve preparation history; provenance is updated to completed. The overlay is archived as .go.txt, so it is not a repository build input. Raw host process arguments and Product clones are excluded.
