# Stage16 matched-phase memory diagnostic

Both instrumented arms exited 0, all four correctness gates per arm passed, and all seven cross-arm normalized graphs match. No-op preserves zero parses/events and stable state. Both receipts explicitly exclude latency acceptance; concurrent worker tests were observed. No forced GC or heap-profile serialization was used.

At graph_session_run, baseline -> candidate cumulative allocation: no-op 528,409,184 -> 517,456,976 bytes (-2.07%); body 1,884,194,640 -> 1,878,383,936 (-0.31%); structural 1,892,089,392 -> 1,890,190,248 (-0.10%). Structural malloc count 7,658,598 -> 7,569,464; both had 21 collections. Structural current heap 396,133,392 -> 320,086,112 bytes and heap_sys 640,876,544 -> 582,057,984 at this mark.

This sample does not show increased cumulative allocations caused by the fused walk. Current heap is not retained heap, and matching GC counts do not align GC timing. These measurements do not explain or dismiss the repeatedly observed uninstrumented RSS increase. Stage16 memory acceptance remains open. Next investigation should inspect allocation/lifetime changes between matched intermediate phases and verify an explanation with uninstrumented repeated runs; do not use this favorable single sample as clearance.

Full counters: phase-comparison.json. Build/source provenance: builds.json, overlay files, and cli-pairs/pins.json. Cross-arm graph proof: cross-arm-equality.json. Raw per-arm logs and receipts remain under cli-pairs.

## Phase alignment follow-up

Matched trace/phase sequences are asserted in phase-increments.json. Structural runs first differ in observed GC count at assemble_new_files (baseline17/candidate16), remaining different at index_group_owners (19/18), then reconverging by the second ts_config_inputs (19/19). Thus equal final GC counts conceal different intermediate collection schedules. This supports investigating heap scheduling/lifetime as a hypothesis, not proving causation.

The structural interval after the second ts_config_inputs through write_pending_state allocates about390MB in both arms; cumulative total at Run completion is about1.89GB despite12 parsed files. The marks cover more than the named operation, so attribute this to the interval, not exclusively serialization. This is a concrete remaining allocation target separate from config traversal, whose total saving is small. No change to correctness or durability is authorized by these observations.
