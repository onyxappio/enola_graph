# Enola accuracy workflow

Owner: Telegram `enola-2`; primary terminal `term_b4d33c66-3033-42d6-8870-0852aa948bfd`.

## Standing user instruction — 2026-09-22

The user authorized **auto approval for all future candidates**. This supersedes the earlier per-candidate approve/reject gate and the stale wording in the continuous goal description. Keep the continuous goal active until the user explicitly stops.

- Work in waves of five on real Product and landings-base source.
- First state the expected entities/direct relationship, run the analyzer, and prove the discrepancy with source and controls. Auto approval does not waive evidence or independent review.
- Research: Fable 5.1 through Claude Code. Coding: Grok 4.6 through the configured local CLIProxy launcher. Primary orchestrates and independently verifies.
- Workers must not send Telegram messages. Primary alone sends important updates with `bash ~/.claude/skills/user-send/send.sh -t enola-2 -T term_b4d33c66-3033-42d6-8870-0852aa948bfd`.
- Start workers in the authorized unattended/always-approve mode. Keep all work in this isolated branch. Local commits only; do not push/merge or disturb other sessions.
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
