# Product no-op after package-gate integration

Integrated `f0101f1` behavior, fresh CLI, full Product at `4168360e2e7f`, file sink.
Three sequential unchanged runs: 3.054 / 2.236 / 2.287 s (median 2.287 s).
All three parse zero files, add zero event bytes, retain generation and leave all
state JSON hashes unchanged. This preserves no-op correctness but does not meet
the remaining near-zero latency goal. Shared-host measurements with profiling
enabled, not a quiet-host acceptance or a resident/watch measurement.

The second run explains the remaining cost:

| Step | Seconds |
|---|---:|
| Resolve CLI graph target | 0.613 |
| Read and decode durable state | 0.291 |
| Prove reusable graph input policy | 0.094 |
| Runtime input construction (inclusive) | 1.022 |
| CLI total from internal trace | 2.220 |

Within runtime inputs, inventory is 0.121 s, extractor detection 0.151 s, content
hashing 0.196 s (5045 files / 56.1 MB), discovery construction 0.340 s and session
context 0.049 s. These are nested timings, not additional costs on top of the
inclusive row. Durable state is 61.0 MB and JSON decoding alone is 0.278 s.
The session `extractor_need` mark includes the preceding runtime input work;
it must not be added as another independent second.

Next optimization investigation should separate delayed full-state materialization
from reusable discovery/configuration and input scans. Skipping parsing does not
remove these startup costs. Any fast no-change path must still detect source,
config, Git policy and membership changes; metadata-only assumptions cannot
silently weaken that requirement.

[Results](v304-package-gate-noop.json) · [Representative full trace](v304-package-gate-noop-profile.stderr)
