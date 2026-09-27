# Current-main Product history correctness

Candidate runtime 3f2883b; published baseline runtime 4859ee3; explicit identical test-inclusive input policy. These are correctness runs, not performance measurements.

## Recent mixed transition — passed

42f589ca → fba38bab (pinned actual GitHub main observed 2026-09-27): nine changed Git paths, including four TypeScript source/test inputs and two manifests. Eight CLI calls passed; at both revisions the accumulated graph equals a fresh candidate graph and fresh baseline graph from the same physical checkout.

Initial parsed 6,749 files. Delta parsed 14 files: four source-content changes and ten resolution dependents. Raw configuration changed, but no TS-context-wide reparse was needed. Both no-op checks parsed zero files, published zero events, retained generation and retained identical checkpoint bytes.

This demonstrates bounded actual parsing for this mixed transition; it does not imply the frozen replacement owner scope is 14 files, nor establish latency. Full receipt: receipt-recent.json.

## Broad transition — passed

The older a609c19f → fba38bab transition has 8,462 changed Git paths and six manifest paths. All eight CLI calls and both revision gates passed: accumulated/cold candidate/baseline graph equality, zero-event/zero-parse no-ops and byte-identical no-op state. Base initial parsed 6,716 files; target cold analysis parsed 6,749. Delta parsed 1,454 files. Detailed invalidation reasons and graph hashes are in receipt-broad.json. These parse counts do not establish a timing result.

Broad delta reasons: 84 added sources, 232 content changes, 1,138 resolution dependents; 51 source removals. The frozen owner scope conservatively broadened to all prior/current owners because Python candidate names could not be bounded before Begin. Cached facts limited actual parsing; this is not a narrow replacement-scope result.
