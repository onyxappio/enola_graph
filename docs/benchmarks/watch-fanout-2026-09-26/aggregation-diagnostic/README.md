# Next delta attribution diagnostic (not an accepted candidate)

Overlay adds a nested trace splitting existing ts_mapfiles_aggregate into MapFiles and sequential aggregation, including fact cloning. No extraction, mutation, caching, scope or publication behavior changes are intended. The nested durations must not be added to their parent. Instrumentation is diagnostic overhead, never acceptance timing.

Prepared during the Stage38 hold; not built or executed. After Stage38 settles and heavy work is coordinated: rebuild the overlay from the accepted runtime, validate focused extraction tests, run the same Product initial/body/structural/cold-oracle harness with ENOLA_GRAPH_PROFILE=1, and compare every normalized graph hash plus parsed-file and owner counts to its uninstrumented source binary. Then use the larger child phase to choose further attribution. Existing broad profiles are older source and do not establish current component cost.

No persistent index or metadata shortcut is introduced. Cached record immutability, composition mutation isolation, before-Begin and before-End source fences remain mandatory. Any later optimization needs its own independent source review, full correctness evidence and prospective performance gates.
