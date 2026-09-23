# Retained-state projection of the 1,577 resolution reparses

Product transition 07fb4a41ddaf to fec1eac346c4 previously parsed 90 content-changed sources, 4 added sources and 1,577 sources labelled resolution. This probe calls the current rebound and reverse-closure helpers over retained parent and child cold states from experimental snapshot 41e530e9. The probe itself runs through a temporary Go overlay on final snapshot 1e77fa7d, whose production tree matches 9ca81a4. It does not alter production files.

## Results

| Projection | Count |
| --- | ---: |
| Prior / target TS records | 4,220 / 4,224 |
| Added / removed known source paths | 4 / 0 |
| Existing source hashes changed | 92 |
| Per-file semantic context changes | 0 |
| Cached import rebound (M2) | 0 |
| Projected first-pass records | 96 |
| Surface-changed seeds | 45 |
| Transitive reverse closure, including seeds | 1,657 |
| Reverse closure beyond first pass | 1,577 |
| Additional bare-name wave owners beyond closure | 0 |
| Projected second-pass owners with identical prior/target fact slices | 1,577 |
| Projected second-pass owners with identical complete FileRecord | 1,577 |

The combined projected second pass exactly matches the observed 1,577 resolution parses. This contradicts the earlier hypothesis that the global bare-name wave dominates this transition: reverse importer traversal already covers every projected second-pass file. Zero additional name-wave owners does not imply zero overlapping name matches.

Every one of the 1,577 projected second-pass records is deeply equal between the retained parent and target cold states, including its facts. This is concrete evidence of unchanged extraction output for this case; it is not a general proof that dependency reparsing can be removed. Publication/resolution can still change even where file-local facts are identical.

This is an offline projection, not direct instrumentation of the running branch. It substitutes target cold record keys for the new known-file universe and target cold records for first-pass re-extraction. A faithful live attribution still needs to verify those substitutions. In particular, 92 changed hashes are not the same as 90 content parse callbacks; skipped/minified records must not be counted as parsed without evidence.

The result identifies the next investigation: determine which import/export surface changes require transitive source re-extraction, and which can update direct facts and resolution without reparsing the entire reverse closure. It does not prove all 1,577 parses are unnecessary, nor authorize a universal one-hop cutoff. Cold equality, re-exports, candidate ambiguity, deletion and rename correctness remain required.

## Reproduction and evidence

The attached `resolution-attribution-probe.go.txt` is loaded as an additional test in the graphsession package through a temporary Go overlay. It reads the retained state paths named in its source and writes the JSON projection. Use canonical real paths in the overlay: on this host, a first attempt using the /var spelling instead of /private/var ran no tests and was discarded. The corrected probe created its JSON and passed.

Final test: TestRootRetainedHistoryReboundAttribution, wall 3.397 s. These are diagnostic test durations under concurrent work, not Enola delta timings. Raw results and the actual PASS output are in `resolution-attribution-projection.json` and `resolution-attribution-projection.log`.
