# Real main transition: correctness-only check

Single run with concurrent development activity; timings are diagnostic and must not be used for performance acceptance. All baseline/noop/target cold-equivalence checks passed. Exact revisions and changed paths are in provenance.json. Use ../multifile.py with repeat 3 under isolated load for final timing. The 100-file history includes package/lock changes and currently triggers full TypeScript reanalysis; the 27-file history parses 348 file operations through dependent invalidation rounds.
