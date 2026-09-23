# Enola accuracy workflow

Owner: Telegram `enola-2`; primary terminal `term_b4d33c66-3033-42d6-8870-0852aa948bfd`.

## Standing user instruction — 2026-09-22

The user authorized **auto approval for all future candidates**. This supersedes the earlier per-candidate approve/reject gate and the stale wording in the continuous goal description. Keep the continuous goal active until the user explicitly stops.

- Work in waves of five on real Product and landings-base source.
- First state the expected entities/direct relationship, run the analyzer, and prove the discrepancy with source and controls. Auto approval does not waive evidence or independent review.
- Research: Fable 5.1 through Claude Code. Coding: Grok 4.6 through the configured local CLIProxy launcher. Primary orchestrates and independently verifies.
- Workers must not send Telegram messages. Primary alone sends important updates with `bash ~/.claude/skills/user-send/send.sh -t enola-2 -T term_b4d33c66-3033-42d6-8870-0852aa948bfd`.
- Start workers in the authorized unattended/always-approve mode. Keep all work in this isolated branch. Workers make local commits only and must not push/merge or disturb other sessions. The primary integrates completed waves under the synchronization policy below.
- Preserve exact cold/delta equality, silent no-change runs, cache migration, source evidence, and negative controls.

## One cumulative report

Canonical user artifact: `docs/accuracy/report.html`. Keep it standalone, readable on mobile, without external dependencies or secrets. Update this same file after **every completed wave**, retain older entries, then send it as an HTML document attachment to Telegram:

```bash
bash ~/.claude/skills/user-send/send.sh -t enola-2 \
  -T term_b4d33c66-3033-42d6-8870-0852aa948bfd \
  -f docs/accuracy/report.html 'Enola — updated cumulative wave report'
```

Each concise card: candidate ID/title, repository-relative source path, actual short code excerpt (mark omissions), why the relationship exists, observed behavior before fix, required behavior, verified result/status. Include wave/commit and explicit limitations. Never label implementation in progress as verified. No more candidate approval buttons unless the user changes the policy.

Current state at policy adoption: waves 1–2 implemented; wave2 reviewed at `b38d16a` (v278), 5/5 acceptance, 6/6 GraphQL controls, old-state migration and full Go suite passed. Wave3 #11–15 already individually approved and dispatched. Its original brief may still mention manual approval; this instruction governs subsequent waves.

## Code highlighting — 2026-09-23

The user requests Shiki syntax highlighting in the next and subsequent HTML reports.
Generate highlighting at report build time and embed the resulting markup/styles in
`docs/accuracy/report.html`; preserve standalone offline viewing with no CDN dependency.

## Main synchronization — 2026-09-23

The user now authorizes integrating and pushing each verified completed wave to
`origin/main`, then bringing current main back into the accuracy branch. This
supersedes the earlier local-only policy for the primary orchestrator. Fetch main
first, preserve concurrent upstream changes, resolve and validate integration, and
push without force. Keep unfinished wave changes out of main. Do not merge into
a checkout while its coder is writing; use a separate integration checkout and
synchronize the accuracy checkout at a clean worker boundary. Workers still do
not push or merge themselves.

## Annotated code examples — 2026-09-23

In the next and subsequent HTML report updates, add concise explanatory comments
inside code examples, adjacent to the relevant source lines. Identify the code
entity, its actual graph node kind (and symbol_kind where useful), and the direct
relationship with its direction: source node → relation → target node. Use the
analyzer's real names; distinguish expected relationships from currently emitted
ones. Do not invent edges for candidates concerning only properties or scope.

Label inserted comments as report annotations (for example `// Report: …`) and
state that they are explanations, not original repository comments. Keep source
code otherwise faithful, mark omissions, use syntax-appropriate comments, and
include annotations in the embedded Shiki highlighting. Keep each explanation
short enough to read on a phone.
