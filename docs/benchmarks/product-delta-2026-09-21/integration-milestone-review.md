# Integration milestone independent review

**GO for the integration milestone. Performance remains unaccepted.** No blocking regression found in the new frozen production changes relative to `/tmp/enola-final-stage-source`. This review supplements `/tmp/enola-scoped-final-review.md`; it does not re-certify the entire existing implementation or establish Product performance acceptance. No repository files were edited, no commits or push performed, and no Product benchmarks run by this reviewer.

## Freeze and scope

Coordinator confirmed production freeze in `msg_49246f8e88ee` and the answer to the blocking review question: preparation and transport editors had settled, bounded independent tests were authorized, and root owned concurrent full tests/vet/build. Reviewed precisely the `graphinput/policy.go` admission optimization, `admission_test.go`, `graphstream/async.go`, `journal.go`, and the two new transport test files. Read both worker reports and the current integration, streaming contract, validation, and Product status documentation.

Production SHA-256 receipts (unchanged across review and test run):

- policy.go: `cfa7dbec4ada03f7db2af243b13770fcfdb37bdcc176abad8b764a074cfadfd2`
- async.go: `c2ae4bc05af3cadeb2b07b75cfba660217387fd2333efcdbed13c58db92aca9b`
- journal.go: `eaa4d49ac8c40239764c464ac620db8bcb9cfad86a08f2f7cdb4b31bd80a1861`

## Findings

1. **Admission proof is valid under the existing immutable policy contract.** Build is the sole production writer of `entries` and inserts only after `hard` succeeds. Ancestor short-circuiting occurs after the unknown candidate is checked, while VCS segments, lockfiles and state-directory tests still precede ancestor optimization. Known hard admission does not bypass Git ignore/tracked exemption, caller directory bit, semantic-media override or conservative-media promotion. No disk contents or new membership are cached by this change. The new comparison regression checks known/unknown paths, absolute/relative inputs, both directory bits, tracked ignored paths, nested negation, cache/state/VCS/lock exclusions and media/dependency classification. Existing event classification and immutable promotion remain unchanged.
2. **Acknowledgment fences retain their predecessor rules.** `unfinished` is inserted on accepted queue admission, removed only after successful delivery acknowledgment or durable already-completed retry recognition, and examined under the publisher mutex. Begin precedes dependent data, scope precedes resolved data, and End waits for every preceding unfinished job. Later sequence jobs cannot falsely block earlier jobs. Successful completed epochs no longer retain gate history; the new three-epoch regression also closes the old repeated-End cleanup problem. Error paths retain the existing fatal-error behavior rather than treating failed publication as acknowledged.
3. **Replay identity, durability and backpressure are preserved.** Admission still copies original bytes; shared metadata is derived from that copy and does not reserialize payloads. The commit worker continues history/conflict checking, journal append and Sync before placing jobs in the delivery queue. Queue item/byte accounting, in-flight limits, journal bounds, exact identity tombstones, recovery format, Flush/checkpoint boundary and sink acknowledgment behavior are unchanged. Malformed metadata uses the previous independent probes conservatively; the compatibility test covers duplicate keys, malformed types, nested fake envelopes and invalid JSON. No larger journal limit or weaker completeness rule was introduced.
4. **Integration documentation is suitable after root's clarifications.** I identified three minor ambiguities in the draft: Begin's initial owner scope, Begin/End identity versus batch sequence, and the incomplete validation implemented by the illustrative consumer. Root amended all three and I reread the final text (`msg_facd80aaf835`). Final `docs/CODATA_INTEGRATION.md` SHA-256 is `7fd39490c9b612e3ffc5d201c4d75d093e99f4dbae1c3610f237f3016aa97189`. CLI flags, default stream/filter, schema, original-byte batch digests, durable staging before acknowledgment, owner replacement, independent consumers, Limits retention/default seven days, fork provenance/same-checkout restriction, and broker-versus-database completion match the implementation. Codata must implement the stated production validation contract; the reference consumer is not that adapter.
5. **Status documents do not claim acceptance for these optimizations.** PRODUCT.md and GRAPH_VALIDATION.md lead with the failed isolated comparison, qualify older results, and distinguish fresh CLI latency from resident idle. The workers' microbenchmarks are engineering evidence only. New isolated repeated Product measurements, old-binary comparisons, histories, exact cold/delta equality and latency ratios remain root-owned acceptance work.

## Independent execution

After freeze authorization:

```
PATH=/tmp/enola-toolchain/bin:$PATH /tmp/enola-toolchain/go/bin/go test ./internal/graphinput ./internal/graphstream -count=1
```

Exit 0; both complete bounded package suites passed. Receipt: `/tmp/enola-integration-independent-tests.log` (graphinput 9.998s, graphstream 38.342s). Installed NATS server was on PATH. These suites include policy regressions, transport gates, journal durability/replay/conflicts, bounded queue/backpressure and NATS support. Tests ran concurrently with root validation, so their durations are not performance measurements. `git diff --check` also passed. No tests were added or altered by the reviewer, and no independent full repository/race/Product run is claimed.

Remaining: root owns final full validation results and the authorized integration push; Codata owns its durable production consumer/database adapter; performance acceptance remains open. No new source blocker is outstanding in this bounded review.
