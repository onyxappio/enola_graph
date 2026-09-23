# Retained discovery: two matched Product watch pairs

Experimental stage3 discovery retention merged onto main a857bcf, preserving accuracy v304. Production changes are not yet integrated. The baseline production code is a211e0a; intervening main commits contain documentation and harness changes.

| Build | Initial consumer apply, repeat 1 / 2 | Last save to final cold-equal consumer, repeat 1 / 2 |
| --- | --- | --- |
| Main baseline | 13.330 / 10.962 s | 6.693 / 6.627 s |
| Retention candidate | 13.470 / 10.089 s | 6.272 / 6.198 s |

Order: baseline1, candidate1, candidate2, baseline2. The observed watch reduction repeats at 0.421 and 0.429 s (about 6.3–6.5% of end-to-end latency). All watch numbers include the fixed five-second collection window. Two repeats of one scripted scenario on a shared host are diagnostic evidence, not broad performance acceptance. Initial results vary considerably and do not establish a speedup. Subtracting five seconds would not isolate analysis time: the window starts before the last save in this burst.

All four runs have identical initial and final graph digests, exact cold equality, stable source inputs across the cold comparison, zero idle/duplicate events, and three writes coalesced into one replacement. Each replacement covers 11 owners and parses 11 files, sends 27 batches containing 525 node and 1,696 edge records (725,954 JSON bytes including Begin/End), and changes the resulting graph by two edges. Sent records are not newly added entities.

Product revision: 07fb4a41ddafff7f42ebd55af8a23fe5739f4cd7; isolated copies of the same source, TypeScript-only configuration from the existing watch harness. Both Enola binaries use Go 1.27.1 darwin/arm64, normal -trimpath builds, no stripping. Baseline SHA-256: 593243a102800475686fcd3de4f8a15841ec044d70ba61f7916b35d586775fe8. Candidate: ce63e189fbd1777e0b8a8a3a03b6315b61bf76016f3155f0bf3556648e1cc0e2. The same fixed observer binary and NATS JetStream harness were used throughout. Earlier Go 1.26.8/pre-v304 results are not matched performance comparators.

Candidate provenance is the hashed merged overlay in the adjacent JSON, not its embedded VCS revision alone. Main plus overlay targeted Independent/Discovery/Wave9 tests passed in 54.872 s wall, including four independently reproduced retention invalidation defects after their fixes. Full merged-candidate integration checks, broader historical transitions and long-running watch acceptance remain pending.

The harness has no watcher watermark. Final cold equality proves the observed final result for frozen inputs, not internal queue drain or recovery/backpressure behavior. Initial latency ends at consumer apply; it is not broker-only completion. The adjacent JSON preserves full reports, report hashes, telemetry, limitations and merged-file hashes. Raw artifacts remain in the four /tmp/enola-stage3-{main,candidate}-watch-r{1,2} directories.
