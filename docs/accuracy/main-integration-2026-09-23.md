# Accuracy waves 1–4 integration

Integrated completed accuracy work through `efbc37c` with main `665f3c9`.
Unfinished wave5 changes are excluded. Extractor cache version: **v288**.

The combined membership planner exposed a lost-import-spec regression: adding
`foo.ts` beside `foo/index.ts` failed to rebind an existing `./foo` import.
Dependency facts now retain `import_spec` for replay, separately from exact
`target_file` provenance. The cache version invalidates older metadata.

Validation on the integrated binary SHA256
`4c99029b07f85fce1aa0cad1ef4eb33e4e1a6ef84410097aae628602ae82e5ca`:

- Grok integration worker: affected packages and full `go test ./...` passed.
- Primary: all 17 accuracy regression gates passed, including prior waves and cache migration.
- Primary: relative, alias and explicit-index imports passed addition/deletion,
  v287 migration, exact cold/delta equality and silent nochange checks. Unrelated
  owners remained excluded; explicit index imports did not rebind.
- Full pinned Product (`a609c19f`): 69,934 nodes / 174,158 edges; 80 tables,
  78 foreign keys; exact source-target, Vue and Fastify assertions passed.
- Full pinned landings-base (`e263a094`): 21,552 nodes / 53,960 edges;
  468 resolved metadata calls; exact source-target assertions passed.
- Both repositories: v287→v288 migration equals a clean graph exactly;
  subsequent runs parse zero files, publish zero bytes/events, and keep generation.

Timing was measured under concurrent work and is not a performance benchmark.
The report remains `report.html`. Followups F12/F13 and candidates21–25 remain
in progress; integration does not claim they are fixed.
