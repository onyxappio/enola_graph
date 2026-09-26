# Final validation checkpoint

The full repository run on d77ec47 (runtime 7c4651d) caught one source-policy failure: the Nuxt listing parent is a host filesystem path but lacked `//factpath:host`. Commit f0e158c adds only that comment; runtime code is unchanged. The facts package passed after that correction. The full run completed in 651.384 seconds: 109 packages passed and only internal/facts failed. Its original exit1 is preserved; the affected package passed after the annotation fix. This is a full run plus a targeted fix verification, not a claim of an all-green original invocation.

The required cache-version coverage, documentation and engine golden/determinism pre-push checks pass on f0e158c in 11.524 seconds. Installer files are unchanged in this bundle.

The frozen experimental benchmark binary remains built from 7c4651d, SHA256 `354df3445b03dc105d9033c2a8ef212085105132762bcefb88b1188b18678efe`; it does not contain the comment-only annotation commit. No timing arm has started.

The proposed19:15–19:50 UTC timing interval was deferred: Codata confirmed only through19:35 and declined the extension. All holds were released. No timing sample was collected; the six-pair protocol and thresholds are unchanged.
