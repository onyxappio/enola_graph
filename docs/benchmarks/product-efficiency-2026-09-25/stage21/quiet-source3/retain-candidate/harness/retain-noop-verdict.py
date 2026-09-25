"""Fail-closed correctness verdict over product-retain-profile1.py's phase-metrics.json.

The harness records what each run did; it does not judge it. The no-op fields in
particular are observations, and an observation that is missing - because a field was
never written, or a run never happened - must not read as a satisfied condition. So every
condition here is required to be present AND true, and anything else is a FAIL
(root msg_1afa31342ada).

Correctness PASS requires, with no exceptions and no partial credit:
  * all four authorized runs present (o1-initial, o2-noop, o3-noop, o4-body), each exit 0;
  * both no-op runs: ParsedFiles == 0, BaseGeneration == TargetGeneration, zero new wire
    messages, and the committed state byte-identical before and after.

Timing and RSS are printed as diagnostic observations only. They are not part of the
verdict, they were taken under shared load, and nothing here is a speed claim.
"""
import json, pathlib, sys

work = pathlib.Path(sys.argv[1])
rows = json.loads((work / 'phase-metrics.json').read_text())
by = {}
for r in rows: by.setdefault(r['label'], []).append(r)

EXPECTED = ['o1-initial', 'o2-noop', 'o3-noop', 'o4-body']
checks, notes = {}, []

for label in EXPECTED:
    got = by.get(label, [])
    checks[f'{label}_present_exactly_once'] = len(got) == 1
    checks[f'{label}_exit_zero'] = len(got) == 1 and got[0].get('exit') == 0

def true(label, field):
    """Present and exactly True/0 - a missing field fails."""
    got = by.get(label, [])
    if len(got) != 1: return False
    return got[0].get(field) is True

def zero(label, field):
    got = by.get(label, [])
    if len(got) != 1: return False
    return got[0].get(field) == 0

for label in ['o2-noop', 'o3-noop']:
    checks[f'{label}_parsed_no_files'] = zero(label, 'noop_parsed_files')
    checks[f'{label}_generation_unchanged'] = true(label, 'noop_generation_unchanged')
    checks[f'{label}_published_no_wire_messages'] = zero(label, 'noop_wire_messages')
    checks[f'{label}_state_bytes_unchanged'] = true(label, 'noop_state_unchanged')

failed = sorted(k for k, v in checks.items() if not v)
verdict = {
    'correctness': 'PASS' if not failed else 'FAIL',
    'checks': checks,
    'failed_checks': failed,
    'runs_seen': {k: len(v) for k, v in sorted(by.items())},
    'diagnostic_observations_not_part_of_the_verdict': {
        r['label']: {'wall_seconds': round(r['wall_seconds'], 4),
                     'process_real_s': r.get('process_real_s'),
                     'rss_bytes': r.get('rss_bytes'),
                     'parsed_files': r['result'].get('ParsedFiles')}
        for r in rows},
    'load': 'shared host; see competing_samples in diagnostic-receipt.json, which counts '
            'recognized Go/test/Enola/observer processes only and is not a quiet-host claim',
}
(work / 'correctness-verdict.json').write_text(json.dumps(verdict, indent=2) + '\n')
print('CORRECTNESS', verdict['correctness'])
for k in failed: print('  FAILED', k)
print('written', work / 'correctness-verdict.json')
sys.exit(0 if not failed else 1)
