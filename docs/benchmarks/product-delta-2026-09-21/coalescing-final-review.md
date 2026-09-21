# Independent coalescing and native-layout review

GO for the reviewed candidate relative to b1820aa20061475137cd08e04b3fbbe4b730101f. No concrete blocking regression found. This is a bounded source/correctness review, not Product performance acceptance or a recertification of all existing transport behavior. No repository code edits, commits, pushes, or Product benchmarks were performed by the reviewer.

## Scope and freeze

Read AGENTS.md, CONTRIBUTING.md, docs/STREAMING_INCREMENTAL.md, the prior integration-milestone-review.md, and /tmp/enola-transport-latency-repair.md. Reviewed the async.go diff, new transport_diagnostics.go, transport_coalescing_test.go and transport_journal_bench_test.go, native_quiet_test.go, and resident.py config-layout diff, with existing transport/journal/watch code and invariant tests as context. Coordinator authorized bounded tests in msg_b78a27b207cc and explicitly confirmed production freeze and coder settlement in the blocking ask reply. Root owns full tests/vet/build and acceptance.

Reviewed file SHA-256 values, identical before and after focused execution where recorded:

- async.go: 712c024a0281eafa2156f79e8d26324e7295f6be368817b849e05fa91ab12f68
- transport_diagnostics.go: 8e0ee8bc72ae68fb65f3d6edebf1e86743d8a9c507d9e2d61201b5190426ddf8
- transport_coalescing_test.go: 7c6d027e8a5cfb49ee741100e8b39d02fd8cbf5167584cdbfdac6d4003044a5d
- native_quiet_test.go: 5fba995abf486d80df820e97390696fa137849b0ef91a252f35d68cf8a5e5827
- resident.py: 12ce3622b88118499641cf94a97da5a333fdc8b4f12b686b632d2ca71ada46fd

## Invariant findings

1. **Queue bounds are preserved.** Coalescing never removes pending work from accounting, appends a separate buffer, or changes admission. Enqueue still checks pending + committing + ready item and byte totals while holding the same mutex; detaching pending into committing is an atomic accounting transfer. Delivery releases queue capacity while gather waits without the mutex. The existing extra in-flight allowance and rejection of a single oversized payload remain unchanged. The target 16 is a gather threshold, not a new maximum group size: concurrent arrivals can produce larger groups within the existing queue bound. No journal cap change is present.

2. **Gather has a fixed timer and explicit escape conditions.** One timer is created per gather, never reset on producer wakeups, and stopped on exit. Sparse production therefore does not require another Publish or Flush to progress. Three milliseconds is the configured gathering interval, not a hard end-to-end real-time bound: scheduling, mutex reacquisition, journal sync and broker work remain additional costs. Begin/scope/End already in pending bypass gathering; newly admitted barriers signal wake and are checked again. Non-journal publishers bypass gathering.

3. **Flush, error and shutdown have valid wake paths.** waitIdle increments the protected flusher count before signaling wake and decrements it on every return, including cancellation. Active Flush therefore bypasses gathering while retaining the preexisting pending/committing/ready/in-flight completion test and final journal acknowledgment sync. Close sets closed under the mutex, cancels stopCtx and signals wake; errors set err under the mutex and signal wake. The coalescer releases the mutex around its select and rechecks these states after reacquisition. An individual admitted job context cancellation does not itself wake the gather; it remains bounded by the fixed timer and is passed to sink delivery as before. No new lock cycle found: diagnostic recording acquires the async mutex only after Journal.Sync returns, and reporting runs after workers finish.

4. **Durability, replay and protocol ordering remain intact.** Coalescing only changes when the existing pending group detaches. Original payload bytes, identity, journal admission, append, successful Sync-before-ready, broker acknowledgment, and replay representation are unchanged. Shutdown still routes pending payloads through journal append/sync before dropping volatile delivery work. A commit-index failure returns through Flush and does not publish the group. Existing unfinished-sequence predecessor gates still hold data behind Begin, resolved batches behind prior scope chunks, and End behind all prior unfinished jobs. Grouping does not replace or bypass these gates.

5. **Diagnostics are appropriately bounded and opt-in.** Disabled diagnostics avoid clock reads/output. Enabled counters and histogram use the async mutex; the histogram key range is bounded by queue capacity, and report output is once-only. They measure attempted commit-pump Sync calls including failures and commit-index work, excluding final acknowledgment-only syncs. Queue stalls count waits ending in a space notification, not canceled waits; the coder report correctly states this limitation. These counters are not total filesystem fsync or complete wall-time measurements.

6. **Native repair fixes measurement layout without weakening observation.** resident.py moves only its generated external config to root/config/config.yaml; report/log/state outputs remain outside that watched parent. The strict native_events before/after equality assertion is unchanged, as are production raw observed counters and filtering. The regression exercises both contaminated and isolated parents, raw per-path observation for shared-parent report writes, empty covered graph batches, zero parses/work/publication/generation advancement, cold equivalence, and real atomic config replacement in both layouts. The config is still watched; isolation does not silently suppress semantic config edits. The benchmark requires a fresh output root, consistent with creating config/ without exist_ok.

## Coverage and execution

Independent commands (both exit 0):

```sh
PATH=/tmp/enola-toolchain/bin:$PATH ENOLA_TRANSPORT_DIAGNOSTICS=1 /tmp/enola-toolchain/go/bin/go test -race ./internal/graphstream -run 'Test(CommitCoalescing|Async|ReviewDetached|OutstandingGates|JournalLostAck|IndependentAsync)' -count=1
PATH=/tmp/enola-toolchain/bin:$PATH /tmp/enola-toolchain/go/bin/go test -race ./internal/graphsession -run '^TestNativeQuietExternalConfigOutputLayouts$' -count=3 -v
```

Receipts: /tmp/enola-coalescing-review-tests.log (graphstream 16.412s) and /tmp/enola-native-quiet-review-tests.log (graphsession 24.482s). Durations are correctness-run receipts, not performance measurements; root validation ran concurrently. git diff --check passed.

The transport selection includes old independent durability, detached-group queue bounds, slow-sync admission, cancellation/shutdown, sink failure/replay, ordered barriers, and unfinished-gate tests, so the evidence is not solely tests mirroring the new coalescer. Diagnostics were enabled during race execution to exercise their synchronization. Three native runs consistently recorded shared-parent report events while isolated-parent raw counters stayed unchanged; both real replacement/cold-equivalence paths passed.

Nonblocking test-depth limitation: the new sparse test asserts eventual progress within one second, not an exact three-millisecond deadline; the direct bypass test checks an already-active Flush, not deterministically injecting Flush/Close/error midway through gather. Fixed non-resetting timer and all wake paths were verified in source, and existing integration/race tests cover their surrounding contracts. A future timer-injection test could strengthen schedule-specific coverage without relying on brittle wall-clock thresholds. No such tests or source changes were made by this reviewer.

## Remaining ownership

Root must finish full validation and fresh same-harness repeated acceptance including broker completion, first batch, memory, parsed-file counters, exact consumer cold/delta equivalence, fresh CLI versus resident distinctions, real source/config histories, and legacy-baseline comparison. The paired diagnostic initial runs around 9 versus 15 seconds are promising engineering evidence only; they lack consumer equality and first-batch measurement and do not establish acceptance. No outstanding code blocker was found within this review scope.
