# Post-push observations for b1820aa

These isolated measurements use committed integration checkpoint b1820aa and
snapshot-only benchmark helpers copied from its archived source. All three CLI
repeats passed their twelve assertions. Resident completed three repeats with
all nine cold graph checks and every zero-work/real-edit check passing, but all
three native-backend quiet diagnostics failed. The runner therefore stopped
before either real-history scenario. No full performance acceptance is claimed.

| Case | Old CLI median (s) | New CLI median (s) | New resident median (s) |
| --- | ---: | ---: | ---: |
| Initial | 10.105 | 14.870 | 16.498 |
| No change | 4.516 | 2.064 | 0.000132 (0.132 ms) |
| Body edit | 10.621 | 2.936 | 0.745 |
| Structural edit | 11.544 | 3.064 | 0.800 |

Thirty resident idle requests took 0.102–0.146 ms and performed zero semantic work.

The native diagnostic failure is additional observed filesystem events, with
no parsing, published events or generation advancement. Its cause is under
investigation. Resident changed-graph equality is separately verified; the
failed diagnostic is not concealed by those passing checks.

The follow-up producer-only profile is 15.72 s total: preparation 0.687 s,
TS parse/aggregate 5.336 s, resolved publication 6.699 s. Earlier profile was
14.55 s total with 2.735 s preparation, 3.087 s TS parse/aggregate and 2.793 s
resolved publication. Faster preparation has not improved total initial time;
transport batching and backpressure are being investigated. Profiles are
engineering observations, not repeated acceptance timings. The paired legacy
input-mirror qualifications in HARNESS.md still apply.
