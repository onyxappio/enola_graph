# Candidate 35 — isolated constants in Codata's v2 graph

**Wave 7 · approved by the user on 2026-09-23 · not implemented.**
Reserved from user feedback after wave 5; wave 6 already covers candidates 26–30.

## Source and expected relationship

Product `a609c19f3861971930fae7b33dcb2950598953c5`,
`apps/mobile/src/visual-diff/storybookArtifactAttestation.ts:85`:

```ts
export class RecaptureRequiredError extends Error {
  readonly code = 'recapture-required' as const;
  // constructor omitted
}
```

The field belongs to a real class/file/module. Preserve a source-proven structural
connection in the stored graph; do not invent calls or uses for an unused field.

## Reproduction

Same source, Enola v291 (`4ea3c24`), different protocol only:

| Scope | v1 | v2 (`--authoritative-scope`) |
| --- | --- | --- |
| Exact-file fixture | constant has one resolved `declares` edge | same constant has zero resolved edges |
| Full Product | 103 constants, zero isolated | 103 constants, 18 isolated |

In v2, the outgoing `declares` relation remains unresolved because its synthetic
directory-module target is excluded. The class and constant themselves exist.
This is distinct from candidate 30: Markdown's containing module is absent even
in v1, while this TypeScript connection exists in v1 and is lost in v2.

Codata `483c9c9` explicitly selects v2 in
`packages/enola/src/graph-command.ts:88`. Its stream assembler keeps unresolved
edges separate (`graph-stream.ts:627`), and the graph adapter materializes only
resolved relationships. No constant-specific edge filter was found. The exact
live database/version behind screenshot 1389 has not been queried; do not claim
that every pictured constant is one of these 18.

Primary evidence: `/tmp/enola-codata-constant-review/report.json`,
`minimal-evidence.json`, and `reproduce.py`. Exact-file source bytes are unchanged.

## Acceptance

- A meaningful structural edge for the reproduced constant survives Codata's
  resolved-edge projection in v2; its class/file/module target exists.
- Check the other isolated Product constants and broader structural consequences.
- Preserve v1 behavior, exact source provenance, stable identity where possible,
  file-only frozen v2 owners, and complete replacements.
- Test class/field/file renames, deletion and last-owner removal; delta equals cold.
- No-change performs zero parses/publication/generation advancement.
- Do not solve this by allowing synthetic owners in v2 Begin, fabricating use
  relationships, or adding transitive attributes. Review the structural ownership
  contract before choosing the implementation.
