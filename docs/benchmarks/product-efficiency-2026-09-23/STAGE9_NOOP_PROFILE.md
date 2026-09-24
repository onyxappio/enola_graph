# Fresh CLI no-op profile after Markdown scope fix

A single instrumented Product no-op on clean `a648c0b` took 2.162 seconds.
The preceding uninstrumented three-repeat median was 1.780 seconds; these are
separate measurements, not an instrumentation overhead estimate. Both use a
file sink. The profile reports generation 2 to 2 and zero parses.

| Phase | Seconds | Observation |
|---|---:|---|
| Resolve repo/config target | 0.528 | Before opening graph session |
| Read and decode state | 0.296 | 60,294,501 bytes; JSON decode 0.274 s |
| Prove captured graph inputs | 0.197 | Reconciliation input fence |
| Runtime input collection | 0.883 | Parent of inventory, detection, hashing, discovery and context work |
| Reuse TS facts | 0.073 | Copies 71,115 facts |
| Assemble file state | 0.009 | 5,033 records, 71,134 total facts |
| Final no-publication fence | 0.101 | Includes config input revalidation |

Within runtime input collection: inventory 0.130 s, extractor detection 0.164 s,
content hashing 0.215 s over 5,052 files and 56,129,371 bytes, TS discovery 0.148 s,
and TS session context 0.057 s. These nested durations must not be summed with
their parent phase. Detection timings also include concurrent work.

Omitting unused returned facts alone would address only about 0.08 s here.
The larger remaining opportunities are repeated target/policy setup, discovery,
input collection and state loading. Preserve input-change fences and full
state corruption validation when optimizing them. A resident session is a
separate performance mode; its numbers must not be presented as fresh CLI time.

At the time of this profile, the package-alias candidate was not accepted: an independent test found
`missing frozen invalidation plan` on the immediate fresh no-op after adding
a referenced package. Baseline main passed that same sequence. Subsequent alias integration resolved
this failure and was released as `8af2c7a`; see
[the integration validation](stage9-alias-integration-validation.json). The required invariant
remains zero-parse, zero-publication and stable-generation no-op behavior without
a warm-up transaction.
