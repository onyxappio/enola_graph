# Stage33: accepted limited fresh-summary no-op improvement

Runtime 3f2883b versus published runtime 4859ee3, same pinned Product 01baa6e and explicit test-inclusive scope. Twelve alternating arms, six observations per scenario per binary, all inside 13:20–14:25 UTC on 2026-09-27. Actual series ended 13:35:48 UTC. Previous interrupted cohort is excluded.

| Scenario | Baseline median (min–max), s | Candidate median (min–max), s | Median change | Candidate parses |
|---|---:|---:|---:|---:|
| initial | 11.488168 (11.329061–11.670056) | 11.539213 (11.272957–11.774639) | +0.4443% | 6752 |
| noop | 2.117442 (2.095744–2.150929) | 2.065174 (2.060355–2.092331) | -2.4685% | 0 |
| body | 4.254876 (4.183117–4.323725) | 4.196115 (4.182472–4.288203) | -1.3810% | 21 |
| structural | 4.297374 (4.189327–4.571976) | 4.285032 (4.222292–4.323212) | -0.2872% | 22 |

Noop improved in all six pairs (0.163% to 4.211%), ratio of medians 0.975315; its RSS median fell 382.406→355.164 MiB (about27.24 MiB). Initial median +0.4443%, body -1.3810%, structural -0.2872%; all under the <=2% median regression gate. All scenario RSS changes are under +5%. Mean scenario median ratio0.990769 and sum of scenario medians -0.3264% satisfy the aggregate gates.

All own-arm cold graph checks and cross-arm/repeat hashes match; each unchanged no-op has zero parses/events and unchanged generation/state. Full source suite passed before timing; current-main historical correctness independently passed16 CLI calls. No runtime code changed after binary pinning.

Initial first-batch median7.811584→7.820523s; producer broker-end11.372534→11.435494s. Body broker-end4.158479→4.104528s; structural4.206304→4.189143s. Fresh CLI totals include process exit and all producer acknowledgments. Candidate body/initial median paired ratio0.369532, structural/initial0.373798. These approximately4.2-second deltas and2.065-second no-ops remain intermediate, not the requested final latency.

Normal pressure in all arms, max sample gap below0.27s, no new Swapouts. Swapins occurred (284–668 pages per arm) and are context; this is not a zero-paging claim. AC boundary charge63–81%, Low Power Mode off. Boundary checks do not prove continuous AC during an arm. No recognized competing jobs or host sampling errors. Whole-host descriptive audit shows normal desktop/background activity, including WindowServer; not a dedicated idle host. Compare within this cohort only; the previous unexplained pre-sleep baseline slowdown is not explained by these data.

This is a small engineering acceptance under a preregistered rule, not statistical significance, a watch measurement, old-upstream comparison, or completion of the broader goal. It combines Git-index ancestor deduplication and summary-only deferred TS fact assembly; it does not isolate their effects. Durable state is still fully decoded and no persistent relationship index or schema was added.

Arithmetic review and publication are pending.
