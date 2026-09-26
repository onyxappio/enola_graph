# Failed directory enumeration must not become proof of absence

`filepath.WalkDir` visits a directory before listing its children and calls back
again on read failure. When discovery ignored the second error, the first visit
remained in the set of completed enumerations. The observation ledger therefore
claimed the directory was empty even though enumeration failed.

The fix drops that completion entry on any walk error, independently of the
callback returning nil. Successful parent membership and truly empty-directory
observations remain intact. No new cache/index or protocol change is involved.

The new regression failed on pre-change `observe.go` supplied through Go's
`-overlay` option: `failed enumeration was recorded as a complete empty directory`.
The same regression passed with the fix. It ran without skipping on this host
(uid 501); it skips only on hosts able to read mode-000 directories.

Focused discovery/alias checks and shared-discovery/retention checks passed;
receipts record exact commands and elapsed times. These are correctness tests,
not performance measurements. The pinned benchmark candidate remains 9c7359f;
this subsequent fix is not silently included in its pending acceptance cohort.
It must be accounted for explicitly in final source validation before main.

Nuxt stat elimination remains unimplemented: this fixes a prerequisite rather
than claiming the proposed optimization is already safe or faster.
