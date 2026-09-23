# v291 cached-state component probe

Production HEAD: `a15bf9a1bc6096fd3a0ea051fce1bb86697a8326`. Retained Product state SHA256: `74031b71545f6c89e68d7cf3eaa34f2d3a93951ca2907080a0b67ecf98156a75`. State bytes: 62050146.

Component probe with retained cached contributions, not end-to-end pipeline or full assembled/composed domain. Shared host while Product benchmark runs. ResolutionIndexes benchmark includes changedCandidateNames comparison; DecodeState excludes disk read because bytes are read before timing. One iteration per sample, three samples; not a throughput acceptance benchmark.

| Component | Median ms | Range ms | Median allocated MiB |
| --- | ---: | --- | ---: |
| DecodeState | 498.894 | 491.874–509.728 | 150.08 |
| BuildIndexCachedContributions | 10.610 | 9.126–10.756 | 18.07 |
| ResolutionIndexesCachedContributions | 163.360 | 161.309–179.162 | 201.77 |

The index itself costs roughly 10 ms on cached contributions, while old/new reconstruction and comparison cost about 163 ms and allocate about 201 MiB. State JSON decoding costs about 499 ms without filesystem I/O. These measurements support retaining index/state reuse work, but do not explain multi-second delta latency alone; broad reparsing and publication remain higher-priority targets. Full assembled facts include additional composition not represented by this probe.

Total command wall time including compilation: 23.184 s. Go benchmark package time: 3.485 s.

Artifacts: `v291-state-index-components.log`, `v291-state-index-components.json`, and `v291-state-index-components.go.txt`. The snippet was appended to owner_resolution_test.go through a Go overlay with encoding/json and os imports, with ENOLA_PROBE_STATE set to the retained state file. Root production files were not changed.
