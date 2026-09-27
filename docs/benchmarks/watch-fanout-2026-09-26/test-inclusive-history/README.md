# Test-inclusive history preparation — not executed

Three real first-parent Product transitions are pinned in SCENARIOS.json:
19 changed paths without manifest/config changes, 15 paths including a manifest,
and 24 paths including a new package tsconfig. These are historical revisions,
not proof of a transition to the current remote main or the reconstructed 6042744
benchmark tree. That newer ancestry remains a separate requirement.

The runner adapts the existing Stage19 correctness procedure: initial, unchanged
no-op, candidate cold and optional baseline cold at the base; then delta, no-op
and both cold oracles at the target. All graph equality and silent no-op gates
remain. Cold state directories are deleted only after collecting their hashes;
the evolving chain state is retained. Both builds get the same explicit
test-inclusive policy and 8 MiB broker/Begin limit. Candidate uses changed-owner
scope, baseline retains its published behavior. No history timing is claimed.

After the Stage32 timing window has been explicitly released:

```sh
python3 run-history.py --scenario source --tag source \
  --pins /tmp/enola-framework-prepass/acceptance/cli-pairs/pins.json \
  --output /tmp/enola-framework-prepass/history-source
```

Repeat with manifest and tsconfig-addition using distinct fresh output roots.
The source checkout is read-only; each run creates its own copy. Forty-seven
existing gate tests passed; the new runner parses successfully but has not yet
executed a Product scenario. Pins, output graphs and fallback/parse counts must
be inspected before claiming validation.
