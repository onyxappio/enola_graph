# Local export surface reuse — experimental

Candidate based on Stage33 runtime restored at b029f16; not published or accepted.

The diagnostic trace in ../delta-attribution showed ~0.30 s in second-hop MapFiles for 20 Product dependents. The edited password.ts imports node:crypto but has an unchanged, recorded context-free export surface. The old no-import predicate discarded that existing proof.

The candidate reuses that proof only for supported complete records with no resolved repository files, unresolved specs, reexports, side reads or side-read hashes. It adds no relationship index. Surface, declaration and reference equality remain required for both records; frozen Begin and delivery code are unchanged.

## Evidence

- Before implementation, the external-import body-edit regression failed: 2 parses instead of 1.
- Initial candidate plus existing TestBodyScope cases passed in 26.734 s.
- Expanded external-import regressions passed in 4.597 s: default-export switch, export removal/restoration and repository-binding retarget/restoration all compare applied deltas with cold graphs. Repeated unrelated edits verify refreshed side-read proofs.
- Full graphsession suite passed uncached in 449.211 s; the remaining repository packages passed separately with cache enabled (see receipt and log). External-import recovery test was added after graphsession started and passed separately.
- Product correctness-only harness exited 0: seven CLI calls, four checks passed; all seven graphs also equal published Stage33. Initial/noop/body/structural parses: 6752/0/1/22 (body previously 21). Noop has unchanged state, zero events and no generation advancement. This shared-host run is not performance acceptance.
- External-import resident failed-End recovery passed in 1.705 s: committed and resident side-read hashes remain unchanged on failure, then recover and match cold graph after another edit.

## Remaining gates

Full suite passed; review conservative unknown/framework/resolver handling; run required historical regressions and repeated coordinated timing with prospective criteria before publication. No speedup claim yet.

Acceptance plan is frozen before timing: >=5% body median gain and all six paired wins, with the prior time/RSS/host/power bounds. Gate tests passed. Historical current-main checks are now running (recent logs: /tmp/enola-local-surface/history-recent.log). The copied historical gate test had a stale three-scenario assertion; it now explicitly requires the two named ancestor-endpoint scenarios actually supplied by that harness, retaining all other 50 checks.
