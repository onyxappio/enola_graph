# Current-main Product history correctness

Candidate runtime 3f2883b; published baseline runtime 4859ee3; explicit identical test-inclusive input policy. These are correctness runs, not performance measurements.

## Recent mixed transition — passed

42f589ca → fba38bab (pinned actual GitHub main observed 2026-09-27): nine changed Git paths, including four TypeScript source/test inputs and two manifests. Eight CLI calls passed; at both revisions the accumulated graph equals a fresh candidate graph and fresh baseline graph from the same physical checkout.

Initial parsed 6,749 files. Delta parsed 14 files: four source-content changes and ten resolution dependents. Raw configuration changed, but no TS-context-wide reparse was needed. Both no-op checks parsed zero files, published zero events, retained generation and retained identical checkpoint bytes.

This demonstrates bounded actual parsing for this mixed transition; it does not imply the frozen replacement owner scope is 14 files, nor establish latency. Full receipt: receipt-recent.json.

## Broad transition — running

The older a609c19f → fba38bab transition has 8,462 changed Git paths and six manifest paths. Its independent correctness run is in progress; no result claimed yet.
