# Intermediate policy-stage validation

Frozen production source: `/tmp/enola-policy-stage-source`.

- Full `go test ./...` completed: every package except docslint passed. Three archived report references incorrectly appeared to be local repository paths; these documentation-only references were corrected in main and the snapshot. Focused docslint then passed (0.699s).
- Independent review is NO-GO pending allowed-input manifest and Markdown discovery fixes. See [review](policy-independent-review.md).
- Product native smoke failed during initial catch-up: watcher coverage did not settle within 16 iterations. No delta or performance acceptance is claimed. Root evidence is in `/tmp/enola-policy-product-smoke`.
- Production source remains frozen. A separate diagnostic helper build logs drained native batches to investigate catch-up; it is not a performance candidate.

Scoped invalidation changes and fixes are under development in main. This intermediate stage is not final acceptance.

Diagnostic reproduction identifies repeated Git index CHMOD notifications on every reconciliation, producing a feedback loop. Evidence: `/tmp/enola-policy-watch-diag.log`. The scoped-delta worker owns the production fix and regression; controls must remain correct for real index changes and atomic replacement.

The main documentation suite also exposed a snapshot-only helper path in HARNESS; its qualification was fixed and main docslint passed (0.488s). The legacy scope translator compiled and successfully generated a configuration for Product; pinned old Enola completed with that configuration, preserving existing output directories. This was a contended harness smoke, not an accepted timing baseline.
