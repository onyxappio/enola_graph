# Resolver replay: exploratory Product transition

Frozen three-file candidate overlay over `c4e314e`, captured from the worker
before its final deferred-rebind closure changes. Not integrated or release
acceptance. Product `07fb4a41ddaf` → `fec1eac346c4`, independent parent initial,
fresh CLI and file sink. Checkout/cloning time is excluded from Enola run time.

| Metric | Candidate |
|---|---:|
| Changed Git paths, including policy-excluded inputs | 9345 |
| New source parses | 4 |
| Content-change parses | 92 |
| Resolution parses | 495 |
| Total parsed files | 591 |
| Begin owners | 1783 |
| Actually changed graph owners, retrospective | 83 |
| Delta seconds | 15.573 |
| Cold target seconds | 26.396 |

Exact cold graph equality and frozen Begin/End/batch integrity pass. Earlier
pre-wave10 history had 624 parses (528 resolution) and 1811 Begin owners, but it
is **not a same-base timing control**. This candidate does not demonstrate a
large real-history acceleration: 495 resolution parses remain. The next matched
comparison must use current accuracy semantics and the final reviewed source.
The retrospective owner count does not establish a minimal safe Begin scope.

Three independent root fixtures pass on this snapshot: unused export addition
with unchanged importer, global-name add/rename/restore with actual target-ID
transitions, and simultaneous global-name addition/default-reexport-origin
change with resolved-file assertions. These cover correctness and selective
reuse, not the entire remaining resolver work.

[Raw history, source hashes and parse reasons](v314-resolution-exploratory.json).
