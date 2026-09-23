# Retained discovery integration

Integrates the reviewed stage3 snapshot into main after f95a065, preserving accuracy v304. The TypeScript discovery snapshot is retained only after a committed run and reused after rechecking the inputs it observed: captured bytes, live side reads, file presence, and directory membership including entry kind. Failure drops the retained snapshot. A rebuilt input policy remains a conservative reason to rebuild discovery. Stage4 CLI construction-proof changes are not included.

Independent guards first reproduced stale live package bytes, a newly added package directory, changed external tsconfig extends, and a file-to-directory transition with unchanged entry name. All four now pass. Their original failure evidence remains in /tmp/enola-stage3-reuse-review and /tmp/enola-stage3-reuse-fixed-review. The current main accuracy changes were merged into ts.go rather than overwritten by the older worker checkout.

Validation:

- Main plus candidate Independent/Discovery/Wave9 targeted tests: PASS, 54.872 s wall.
- Full `go test -overlay /tmp/enola-stage3-main-review/overlay.json ./...`: 255.432 s wall; all packages passed except pkg/command, whose linker failed with ENOSPC before its tests could run. Original exit 1 is preserved.
- Unchanged pkg/command overlay retry after space became available: PASS, 5.686 s wall.
- After copying the hashed candidate files onto main, `go test ./internal/graphsession ./internal/extractors/tsextractor -run 'Test(CLI|Independent)' -count=1`: PASS, 47.510 s wall. This reruns all three separate-process CLI tests against actual integrated code because their nested go build does not inherit a command-line overlay. It also verifies the independently authored guards after gofmt.

The companion watch report records two matched Product pairs: last-save latency including the five-second collection window is 6.693/6.627 s baseline versus 6.272/6.198 s candidate, with identical final cold graphs. This is one short scenario, not final goal acceptance. Initial speedup, the final ten-transition history, prolonged watch/recovery, and near-zero fresh-CLI no-op remain unproven or incomplete.

The adjacent receipt records integrated-file and test-log hashes. Tests changed only by gofmt after overlay validation; production bytes are the reviewed frozen merge. Raw logs remain under /tmp/enola-stage3-main-review. Completed NATS stores were losslessly gzip-archived with per-run decompression hash receipts; restore blocks before reopening those stores.
