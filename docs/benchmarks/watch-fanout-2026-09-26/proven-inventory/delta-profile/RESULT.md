# Diagnostic changed-file profile

Experimental177c0c5, binary358f22f9, Product01baa6eb, identical test-inclusive
scope and body/structural mutations as the correctness harness. Initial and both
deltas completed; source was restored in the isolated clone. These are diagnostic
shared-host runs, not promotion measurements. Existing seven-call correctness
checks cover these same mutations.

Body parsed21 files and sent2 events (Begin/End only). Structural parsed22 files
and sent3 events (one replacement batch). Their CLI traces were4.083/4.080s.
Those are observations, not a baseline comparison. Tiny publication volume does
not explain the remaining latency.

Selected sequential session spans (body / structural seconds):

| Span | Body | Structural |
| --- | ---: | ---: |
| TS frozen preview |0.620|0.614|
| Invalidation planning |0.206|0.238|
| TS dirty scope |0.144|0.146|
| Assemble current file contributions |0.221|0.248|
| Group/index owners |0.157|0.151|
| Revalidate TS records (6804) |0.233|0.201|
| Pending state write |0.473|0.468|

The preview contains the TS MapFiles-plus-aggregation span0.353/0.330s; do not
add that nested span to preview. Source inspection shows that span includes
parallel dirty parsing, reconstruction of cached results and cloning/aggregation
of all returned fact slices; it does not isolate any one of these costs.
Likewise state JSON encode/write0.321/0.320s is inside pending-state write.
Fresh state load0.450/0.444s and fresh policy/input work precede these session
spans. Never sum parent CLI/session spans with their children.

Next concrete investigation: attribute the MapFiles/aggregation span before
changing it, especially copying unchanged facts before frozen preview and again
when assembling current contributions. Preserve immutable cached records and
all framework/resolution mutations; do not replace this with another persistent
relationship index. State durability and source fences are substantial but
cannot simply be skipped. No additional optimization is implemented by this
profile, and the frozen177c0c5 candidate is unchanged.
