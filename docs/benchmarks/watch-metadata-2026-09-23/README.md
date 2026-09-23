# Watch metadata republication reproduction

Measured builds: clean `d6fe7b0` and that build plus the admission-identity patch
later integrated as `a257dd6`. These probes do not use the merged v291 binary and
do not establish the cause of Codata’s reported run.

An isolated Git fixture had 80 TypeScript sources. Each watch observed 14 seconds
before and 14 seconds after a controlled mutation, using a 300 ms collection
window and a file sink. The 28 seconds are an observation window, not delta
latency. State and events were outside the source tree.

| Mutation | Clean d6fe7b0 generations | Admission-patched generations | Applied graph comparison |
| --- | ---: | ---: | --- |
| None | 1 | 1 | No extra generation |
| Harmless local Git config key | 2 | 2 | Exactly equal graphs across generations |
| Stage a source already present before initial | 2 | 1 | Old build republishes exactly equal graph |

Config mutation: `git config --local probe.marker one`. Staging mutation:
`git add src/extra.ts`, with the source created **before** initial. A separate
probe that created and staged a file together was discarded as a staging test.

The coordinator independently parsed all six logs, validated frozen replacement
manifests and batch digests using the historical harness, applied each complete
generation through its consumer, and compared full graph hashes. Results are in
`root-validation.json`. Hashes are compared within each probe; different fixture
paths use different repository identities.

The admission patch fixes staging of unchanged nonignored sources. Raw Git config
bytes remain in the admission fingerprint, so unrelated local Git configuration
still triggers a full replacement. Startup watch registration alone did not cause
a duplicate in these probes. A follow-up production fix must project actual Git
admission effects while preserving discovery, tracking, ignore rules and mid-run
input fences; removing every Git dependency is not justified.

An owner-scope digest proves owner-set equality, not graph equality. TS parse
counts and total owner counts have different denominators; they cannot establish
a cache-hit percentage or whether initial was cold. Resolved phase alone cannot
rule out a state migration. Codata attribution still needs its exact binary,
commands, input changes and state/Begin/End artifacts.

Local probe artifacts remain under `/tmp/enola-probe/`; the detailed reviewed
worker report is `/tmp/enola-codata-startup-feedback-review.md`. These temporary
paths are provenance, not a portable test installation. The next production
increment is required to add durable regression tests.
