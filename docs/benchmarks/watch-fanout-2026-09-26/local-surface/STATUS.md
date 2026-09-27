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

Resolver validity: tsconfig alias and newly added package.json export alias both transition an unchanged leaf from proven external imports to local repository bindings, then back on removal. Resident proof eligibility changes as expected; every generation equals cold. Expanded test passed in 2.849 s. Recent Product history completed exit 0, eight calls and all gates passed; broad history is running in session 65553.

Both real Product histories passed (16 CLI calls); recent delta parsed 14, broad delta 1453 (published baseline previously 1454), exact delta/cold/baseline graphs at both endpoints and silent no-ops. All primary heavy work is idle. Requested a new 15:10–16:15 UTC window from four participants; no timing until explicit acknowledgments. Source review request msg_d60ec74c5793 remains pending.

Additional membership proof: bare specifier vendor initially external, then vendor.ts appears and is removed without alias/config changes. The existing src/provider.ts already declares value, so this is not a newly introduced global name. Eligibility changes true/false/true and every generation is cold-equal. Three-case resolver regression passed in 5.124 s. Runtime is unchanged; no timing has started.

Source review received (msg_9d394949bf80): core reuse/failure recovery sound, but Router coverage unresolved and framework validity coupled to external fences; three stale comments identified. Before any timing, the requested window was released at 15:28 UTC. Candidate now adds explicit Router refusal and pairwise NuxtScope/ResolutionSpecs/AutoImportDirs/NuxtAliases guards, plus corrected comments. Prior binary/receipts remain evidence for the earlier prototype only; guarded runtime requires fresh build, correctness and performance evidence. Focused body/external-import/router tests passed in 36.474 s. Guarded runtime committed as 7812838, binary f69bae0951bffec1fc9976e7c2fea7548d03f2ae56c62aa934c4a8d4275d08b1. Fresh full suite session 94706 and Product correctness session 78388 are running; follow-up source review msg_8b6e4e7fb012 requested.

Guarded Product run exited 0: all four checks and independently compared seven Stage33 graph hashes passed; parses remain 6752/0/1/22. Follow-up source review closes its three prior issues, with no correctness objection. Five new guard regression cases passed uncached in 0.540 s; pairwise addition/removal and stable-context checks included. The resolver test now explicitly distinguishes its record-local predicate from pairwise reuse eligibility. Full suite 94706 remains live; guarded recent history started as session 33571. No acceptance timing started.

Guarded full suite completed exit 0: 110 passing packages (104 cached), graphsession uncached 462.536 s. Separate guard tests were added after suite compilation and passed independently. Recent current-main history passed all series/step gates, delta 14 parses. Broad-history first attempt stopped before execution because the scenario-limit prose had changed the pinned rule document; the pinned rule was restored byte-for-byte and the prose moved to SCENARIO_LIMIT.md, without threshold changes. Fresh broad run is live as session 53333 at /tmp/enola-local-surface-guarded/history-broad-v2. These are correctness-only runs.

Guarded broad history completed exit 0; all gates passed, delta parsed 1453. Both histories now supply 16 successful CLI calls with exact cold/delta/baseline equality and silent no-ops. Fresh correctness acceptance receipt binds the guarded binary to these results. New quiet interval requested for 15:50–16:55 UTC; waiting for explicit participant acknowledgments.

Four participant holds verified for 15:50–16:55 UTC, including Codata terminal acknowledgment after dispatch_run_mismatch. Timing scheduled with caffeinate in session 10341, waiting for window start. No acceptance samples yet.

Timing session10341 exited0, all12 arms completed; quiet window explicitly released. Computed Stage36 acceptance passed: body4.141125 ->3.777263 s (-8.78655%), six paired wins, no time/RSS gate violation. Independent source-only gate review requested; main still unchanged. Evidence in guarded/timing-20260927.

Pre-publication check: origin/main remains 2b11e7d2; no divergent commits. Pre-push hook completed exit0 (cache coverage, docslint, golden/determinism). Independent timing review pending. Cumulative HTML now records the measured body benefit without claiming publication.

Independent review msg_6bad427439cc confirms every gate and no blocker. Pair6 contention and sensitivity (-8.41% without pair6), cache-enabled suite scope and frozen stale-purpose text documented in timing RESULTS. Stage36 accepted for normal main publication; overall startup/watch goal remains open.
