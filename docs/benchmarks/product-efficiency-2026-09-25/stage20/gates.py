"""Pure acceptance arithmetic for the Stage16 history validation.

Everything in this module is a pure function of receipt data, so gate-tests.py
can exercise every refusal path on fabricated rows without a Product checkout,
a broker or a binary. run-history.py imports these and nothing else decides
whether a step or the series passes.
"""
import re

HEX64 = re.compile(r'^[0-9a-f]{64}$')

# Gates that must have RUN for a step, whatever their verdict. A step that
# raised partway through cannot satisfy this set, so an exception can never
# leave a green step behind.
REQUIRED_STEP_GATES = (
    'source-pin',          # checkout is at the pinned commit
    'worktree-clean',      # no tracked, untracked or ignored file at the commit
    'worktree-clean-after',# the runs wrote nothing into the checkout
    'candidate-published', # whether the chain step published, recorded either way
    'delta-equals-cold',   # the chain hash equals a cold analyze at the same commit
    'noop-zero-parses',
    'noop-stable-generation',
    'noop-zero-events',
    'noop-state-unchanged',
)

# Baseline comparison is conditional: it is only gated when the arm actually
# ran at the identical checkout. Absent, the series reports candidate
# self-equivalence only.
BASELINE_GATE = 'baseline-cold-equals-candidate-cold'

REQUIRED_SERIES_GATES = (
    'candidate-binary-pin',
    'policy-pin',
    'observer-pin',
    'nats-pin',
    'source-head-pin',
    'commit-chain-pin',
    'chain-initial-fresh',
)


def gate(key, ok, detail=None):
    return {'key': key, 'pass': bool(ok), 'detail': detail}


def hash_ok(value):
    return isinstance(value, str) and bool(HEX64.match(value))


def step_gates(step):
    """Gate rows for one history step.

    delta-equals-cold uses the effective hash: a chain step that published
    nothing (generation held) leaves the graph at the previously published
    hash, and that is what must equal the fresh cold analyze. Treating an
    absent hash as a pass, or as a failure, would both be wrong.
    """
    rows = [
        gate('source-pin', step.get('head') == step.get('expected_commit'),
             {'head': step.get('head'), 'expected': step.get('expected_commit')}),
        gate('worktree-clean', step.get('status_before') == '',
             {'porcelain': step.get('status_before')}),
        gate('worktree-clean-after', step.get('status_after') == '',
             {'porcelain': step.get('status_after')}),
        gate('candidate-published', True, {'published': step.get('published')}),
    ]
    effective = step.get('effective_hash')
    cold = step.get('candidate_cold_hash')
    rows.append(gate('delta-equals-cold',
                     hash_ok(effective) and hash_ok(cold) and effective == cold,
                     {'effective_chain_hash': effective, 'candidate_cold_hash': cold,
                      'published': step.get('published')}))
    noop = step.get('noop') or {}
    rows.append(gate('noop-zero-parses', noop.get('parsed_files') == 0,
                     {'parsed_files': noop.get('parsed_files')}))
    rows.append(gate('noop-stable-generation',
                     noop.get('base_generation') is not None
                     and noop.get('base_generation') == noop.get('target_generation'),
                     {'base': noop.get('base_generation'), 'target': noop.get('target_generation')}))
    rows.append(gate('noop-zero-events', noop.get('wire_messages') == 0,
                     {'wire_messages': noop.get('wire_messages')}))
    rows.append(gate('noop-state-unchanged',
                     hash_ok(noop.get('state_before')) and noop.get('state_before') == noop.get('state_after'),
                     {'before': noop.get('state_before'), 'after': noop.get('state_after')}))
    base = step.get('baseline_cold_hash')
    if base is not None:
        rows.append(gate(BASELINE_GATE, hash_ok(base) and hash_ok(cold) and base == cold,
                         {'baseline_cold_hash': base, 'candidate_cold_hash': cold,
                          'inputs': 'same physical checkout, same policy overlay, same commit'}))
    return rows


def step_verdict(step):
    rows = step.get('gates') or []
    ran = {r['key'] for r in rows}
    missing = [k for k in REQUIRED_STEP_GATES if k not in ran]
    return {
        'gates_missing': missing,
        'gates_present': not missing,
        'all_gates_pass': bool(rows) and all(r['pass'] for r in rows),
        'accepted': bool(step.get('completed')) and not missing
                    and bool(rows) and all(r['pass'] for r in rows),
    }


def series_problems(receipt, expected_commits):
    """Reasons the series may not be reported as a pass. Empty list = accepted."""
    problems = []
    rows = receipt.get('series_gates') or []
    ran = {r['key'] for r in rows}
    for key in REQUIRED_SERIES_GATES:
        if key not in ran:
            problems.append('series gate absent: ' + key)
    for r in rows:
        if not r['pass']:
            problems.append('series gate failed: ' + r['key'])
    if not receipt.get('completed_sequence'):
        problems.append('sequence did not complete: '
                        + str((receipt.get('error') or {}).get('message', 'unknown')))
    steps = receipt.get('steps') or []
    transitions = [s for s in steps if s.get('kind') == 'transition']
    if len(expected_commits) != 11:
        problems.append('expected exactly 11 pinned commits, got ' + str(len(expected_commits)))
    if len(transitions) != 10:
        problems.append('expected 10 first-parent transitions, got ' + str(len(transitions)))
    seen = [s.get('expected_commit') for s in steps]
    if seen != list(expected_commits):
        problems.append('step commits are not the pinned first-parent chain in order')
    for s in steps:
        v = step_verdict(s)
        if v['gates_missing']:
            problems.append('step ' + str(s.get('label')) + ' missing gates: '
                            + ','.join(v['gates_missing']))
        failed = [r['key'] for r in (s.get('gates') or []) if not r['pass']]
        if failed:
            problems.append('step ' + str(s.get('label')) + ' failed gates: ' + ','.join(failed))
        if not v['accepted']:
            problems.append('step ' + str(s.get('label')) + ' not accepted')
    return problems


def baseline_claim(receipt):
    """What may be claimed about the baseline arm, in words, from the receipt.

    Never upgrades to an equivalence claim unless every step actually carried a
    passing baseline gate at the identical checkout.
    """
    steps = receipt.get('steps') or []
    if not steps:
        return 'no steps'
    gated = [s for s in steps
             if any(r['key'] == BASELINE_GATE for r in (s.get('gates') or []))]
    if len(gated) != len(steps):
        return ('candidate self-equivalence only: the baseline arm did not run at every '
                'checkout, so identical clean inputs are not proved for the whole series')
    if not all(r['pass'] for s in gated for r in s['gates'] if r['key'] == BASELINE_GATE):
        return 'baseline comparison ran and FAILED at one or more commits'
    return ('baseline and candidate cold graph hashes are equal at every commit, from the '
            'same physical checkout, policy overlay and commit')
