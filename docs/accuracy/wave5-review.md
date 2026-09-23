# Accuracy wave 5 — accepted review

Implementation: `4ea3c245fc361b3a66146e866f3fc0fa303fa24d`, extractor **v291**.
Sources: Product `a609c19f3861971930fae7b33dcb2950598953c5` and landings-base
`e263a0942efce56e9cd02abc425c0aa4d0327632`.

The primary built an immutable snapshot before the worker finished, then verified
its complete source patch exactly matched the final commit. Independent binary
SHA256: `fe9b708ea82506b40783eae3ca7c295962671ddd923a86a7e05c68a5592bc38f`.

## Changes and source-level evidence

- **21:** both payment webhook POST routes are Fastify servers, including
  unparenthesized register callbacks; parameter/local receiver shadows stay excluded.
- **22:** test/spec JavaScript module extensions follow default test exclusion;
  the ordinary production `.mjs` control remains present.
- **23:** 33 exact symbol-owned namespace-call pairs resolve to
  `mobileAppMachine.updates.ts`; file references remain present.
- **24:** Nitro file routes and five distinct literal registered paths are emitted.
  Imported `addServerHandler` requires the actual lexical binding; aliases,
  parameter/local/block/catch shadows, and subsequent scope recovery were checked.
  The geo file is development-only and registrations include conditional/mock
  paths: these are static declarations, not deployment claims.
- **25:** five class data fields are variables/constants without function complexity;
  methods and getters retain their classification.
- **F12/F13:** named re-exports resolve to the exact subdirectory leaf; Vue template
  callbacks resolve to their enclosing SFC.

## Validation

Primary: all positive cases, six negative controls, alias/shadow/recovery control,
seven edit/delete/rename scenarios **in each protocol**, 17 previous-wave gates,
and six import-spec replay add/delete scenarios with cache migration passed.
No-change runs parse zero files, append no events, and preserve generation.

Full-repository v288→v291 migrations equal fresh graphs exactly in both v1 and v2;
all new source-level assertions and previously passing assertions remain true.

| Repository | v1 nodes / edges | v2 nodes / edges | Retained checks |
| --- | --- | --- | --- |
| Product | 67,787 / 166,056 | 67,345 / 166,056 | 80 tables, 78 FK, Vue composable, Fastify GraphQL |
| landings-base | 21,558 / 53,968 | 21,273 / 53,968 | 468 resolved metadata calls, Nitro routes, re-export and SFC targets |

The worker's fresh full Go suite and golden/determinism runs passed. The primary's
archived-source full run passed every package except two engine tests that need a
Git checkout; both failed obtaining `--absolute-git-dir`, not on graph behavior.
Repeating those tests plus golden/determinism in the actual checkout passed.

## Protocol correction and limits

The initial primary SFC-deletion test incorrectly enforced v2 frozen-inline-scope
rules on default v1, where scope batches and owner additions are legal. Corrected
v1 consumption and explicit `--authoritative-scope` v2 both passed all seven cases
on the **unchanged v290 baseline**. Speculative planner/session changes were removed;
only a useful true-v2 SFC regression remains. This was a test correction, not an
analyzer fix.

The existing v2 profile filters synthetic directory modules, so Product tracking
and Landings enum import-to-module edges are unresolved already on main's v288
baseline. v1 preserves these edges. We checked each protocol against its own cold
oracle and explicitly retained this inherited limitation; cross-protocol semantic
equality is not claimed. Timing under concurrent work is not a performance benchmark.

The cumulative offline report is `report.html`, now highlighted with Shiki 4.4.3.
Source text is unchanged by highlighting. Wave 6 cards are evidenced candidates,
not completed fixes. Subsequent waves remain automatic after independent reproduction.
