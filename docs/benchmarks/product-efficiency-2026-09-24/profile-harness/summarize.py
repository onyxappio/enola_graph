"""Aggregate the profiled runs into one comparison file.

For the noop and body deltas this reports, per arm, the per-run and median
wall/user/sys and state bytes, and the per-mark candidate-minus-baseline
difference for every mark that occurs exactly once in every eligible run.

What it deliberately does NOT do:
  - it does not sum mark durations. The graph-profile output gives no nesting
    information, several marks carry their own total=, and graph_session_run
    looks like a container, so any sum could double count. Per-mark differences
    are reported individually and never added into one attribution number.
  - it does not collapse a mark that appears more than once in a run. Repeated
    occurrences are preserved raw, with their order, trace and detail, and are
    reported separately instead of being differenced.
  - it does not aggregate anything from an ineligible or missing run.

Marks are instrumented timings under ENOLA_GRAPH_PROFILE=1; they decompose the
gap and are not comparable to the unprofiled timing series.
"""
import json, pathlib, re, statistics as st, sys

HERE = pathlib.Path(__file__).resolve().parent
ARMS = ('baseline', 'candidate')
REPEATS = (1, 2, 3)
LABELS = ('noop', 'body')
REQUIRED_RUNS = tuple(a + '-' + str(i) for a in ARMS for i in REPEATS)
ORACLE_RUNS = tuple(a + '-1' for a in ARMS)
STATE_MARKS = ('state_read_bytes', 'state_fingerprint', 'state_json_unmarshal', 'state_json_marshal')
REQUIRED_MARKS = {
    'noop': ('state_read_bytes', 'state_fingerprint', 'state_json_unmarshal', 'graph_session_run'),
    'body': ('state_read_bytes', 'state_fingerprint', 'state_json_unmarshal', 'state_json_marshal',
             'graph_session_run'),
}
HEX64 = re.compile(r'^[0-9a-f]{64}$')


def med(values):
    return round(st.median(values), 6) if values else None


def spread(values):
    if not values:
        return None
    return {'n': len(values), 'min': min(values), 'max': max(values),
            'median': med(values), 'range': round(max(values) - min(values), 6)}


def reason(r):
    if not r.get('completed_sequence'):
        return 'sequence did not complete: ' + str((r.get('error') or {}).get('message', 'unknown'))
    if r.get('required_checks_missing'):
        return 'required gates absent: ' + ','.join(r['required_checks_missing'])
    failed = [c['key'] for c in r.get('checks', []) if not c.get('pass')]
    if failed:
        return 'gates failed: ' + ','.join(failed)
    if r.get('competing_samples'):
        return 'competing workload in ' + str(len(r['competing_samples'])) + ' samples'
    if not r.get('control_restored'):
        return 'control file not restored to the pinned original'
    return 'receipt not marked timing_eligible'


def occurrences(rows):
    """mark name -> list of occurrences, in file order. Nothing is collapsed."""
    out = {}
    for i, r in enumerate(rows):
        out.setdefault(r['mark'], []).append(
            {'index': i, 'seconds': r['seconds'], 'total_seconds': r.get('total_seconds'),
             'trace': r.get('trace'), 'detail': r.get('detail')})
    return out


def classify(per_run):
    """Split marks into those usable for a difference and those kept raw.

    Usable means: present in every run, exactly once in every run. Everything
    else - repeated in any run, or absent from any run - is preserved raw and
    reported separately rather than summed or silently dropped.
    """
    names = sorted({n for run in per_run for n in run})
    single, repeated, partial = [], {}, {}
    for n in names:
        counts = [len(run.get(n, [])) for run in per_run]
        if all(c == 1 for c in counts):
            single.append(n)
        elif any(c > 1 for c in counts):
            repeated[n] = {'occurrences_per_run': counts,
                           'raw': [run.get(n, []) for run in per_run]}
        else:
            partial[n] = {'occurrences_per_run': counts}
    return single, repeated, partial


def series_problems(runs, eligible):
    problems = []
    for name in REQUIRED_RUNS:
        if name not in runs:
            problems.append('missing receipt: ' + name)
        elif name not in eligible:
            problems.append('ineligible run: ' + name + ' (' + reason(runs[name]) + ')')
    extra = sorted(set(runs) - set(REQUIRED_RUNS))
    if extra:
        problems.append('unexpected run dirs present: ' + ','.join(extra))
    for name in ORACLE_RUNS:
        r = runs.get(name) or {}
        h = r.get('oracle_hash')
        if not (isinstance(h, str) and HEX64.match(h)):
            problems.append('missing or malformed cold oracle hash in ' + name + ': ' + repr(h))
        keys = {c['key'] for c in r.get('checks', []) if c.get('pass')}
        if 'oracle-match' not in keys:
            problems.append('oracle-match gate did not pass in ' + name)
    body = {}
    for name in REQUIRED_RUNS:
        h = (runs.get(name) or {}).get('body_hash')
        if not (isinstance(h, str) and HEX64.match(h)):
            problems.append('missing or malformed body hash in ' + name + ': ' + repr(h))
        else:
            body[name] = h
    distinct = sorted(set(body.values()))
    if len(distinct) > 1:
        problems.append('body hashes differ across runs: ' + json.dumps(body))
    for name in ORACLE_RUNS:
        r = runs.get(name) or {}
        if r.get('oracle_hash') and r.get('body_hash') and r['oracle_hash'] != r['body_hash']:
            problems.append('body hash does not equal its own cold oracle in ' + name)
    quiet = {name: (runs.get(name) or {}).get('quiet_message_id') for name in REQUIRED_RUNS}
    if any(not (isinstance(q, str) and re.fullmatch(r'msg_[0-9a-f]{12}', q)) for q in quiet.values()):
        problems.append('missing or malformed quiet_message_id: ' + json.dumps(quiet))
    return problems, distinct[0] if len(distinct) == 1 else None


def main():
    runs = {}
    for d in sorted(HERE.glob('*-[0-9]')):
        receipt = d / 'receipt.json'
        if receipt.exists():
            runs[d.name] = json.loads(receipt.read_text())
    if not runs:
        raise SystemExit('no receipts under ' + str(HERE))

    eligible = {n: r for n, r in runs.items() if r.get('timing_eligible')}
    out = {'runs': {n: {k: r.get(k) for k in ('arm', 'repeat', 'quiet_message_id', 'completed_sequence',
                                              'required_checks_present', 'all_checks_pass', 'timing_eligible',
                                              'body_hash', 'oracle_hash', 'binary_sha256', 'competing_samples')}
                    for n, r in sorted(runs.items())},
           'ineligible_runs': {n: reason(r) for n, r in sorted(runs.items()) if n not in eligible},
           'expected_runs': list(REQUIRED_RUNS),
           'note': 'instrumented timings; per-mark differences only, never summed into an attribution'}

    problems, shared_hash = series_problems(runs, eligible)
    out['series_problems'] = problems
    out['series_complete'] = not problems
    out['body_hash_shared'] = shared_hash
    if problems:
        out['refused'] = 'no aggregate written: ' + str(len(problems)) + ' series problem(s)'
        path = HERE / 'profile-comparison.json'
        path.write_text(json.dumps(out, indent=2) + '\n')
        print(json.dumps({'written': str(path), 'refused': out['refused'],
                          'series_problems': problems}, indent=2))
        return 1

    for label in LABELS:
        per_arm = {}
        for arm in ARMS:
            names = [n for n in REQUIRED_RUNS if runs[n]['arm'] == arm]
            metrics, per_run_marks, rows = [], [], {}
            for n in names:
                d = HERE / n
                m = [x for x in json.loads((d / 'metrics.json').read_text()) if x['label'] == label]
                if len(m) != 1:
                    raise SystemExit('expected exactly one ' + label + ' row in ' + str(d / 'metrics.json'))
                metrics.append(m[0])
                f = d / ('marks-' + label + '.json')
                if not f.exists():
                    raise SystemExit('eligible run without marks: ' + str(f))
                occ = occurrences(json.loads(f.read_text()))
                absent = [x for x in REQUIRED_MARKS[label] if x not in occ]
                if absent:
                    raise SystemExit('run ' + n + ' ' + label + ' missing required marks: ' + ','.join(absent))
                per_run_marks.append(occ)
                rows[n] = {'wall_s': m[0]['seconds'], 'user_s': m[0].get('user_s'), 'sys_s': m[0].get('sys_s'),
                           'state_final_bytes': m[0]['state_final_bytes'], 'rss_bytes': m[0].get('rss_bytes')}
            single, repeated, partial = classify(per_run_marks)
            per_arm[arm] = {
                'per_run': rows,
                'wall_s': spread([r['wall_s'] for r in rows.values()]),
                'user_s': spread([r['user_s'] for r in rows.values() if r['user_s'] is not None]),
                'sys_s': spread([r['sys_s'] for r in rows.values() if r['sys_s'] is not None]),
                'state_final_bytes': spread([r['state_final_bytes'] for r in rows.values()]),
                'marks_differenceable': single,
                'marks_repeated_raw': repeated,
                'marks_partial_raw': partial,
                'mark_spread': {n: spread([run[n][0]['seconds'] for run in per_run_marks]) for n in single},
                'mark_traces': {n: sorted({run[n][0]['trace'] for run in per_run_marks if run[n][0]['trace']})
                                for n in single},
            }
        both = [n for n in per_arm['baseline']['marks_differenceable'] if n in per_arm['candidate']['marks_differenceable']]
        delta = {n: round(per_arm['candidate']['mark_spread'][n]['median']
                          - per_arm['baseline']['mark_spread'][n]['median'], 6) for n in both}
        not_differenced = sorted(set(per_arm['baseline']['marks_differenceable'])
                                 ^ set(per_arm['candidate']['marks_differenceable'])
                                 | set(per_arm['baseline']['marks_repeated_raw'])
                                 | set(per_arm['candidate']['marks_repeated_raw'])
                                 | set(per_arm['baseline']['marks_partial_raw'])
                                 | set(per_arm['candidate']['marks_partial_raw']))
        wall_gap = round(per_arm['candidate']['wall_s']['median'] - per_arm['baseline']['wall_s']['median'], 6)
        state_deltas = {n: delta[n] for n in STATE_MARKS if n in delta}
        out[label] = {
            'arms': per_arm,
            'wall_gap_s': wall_gap,
            'wall_ranges_overlap': not (per_arm['baseline']['wall_s']['max'] < per_arm['candidate']['wall_s']['min']
                                        or per_arm['candidate']['wall_s']['max'] < per_arm['baseline']['wall_s']['min']),
            'state_bytes_gap': round(per_arm['candidate']['state_final_bytes']['median']
                                     - per_arm['baseline']['state_final_bytes']['median'], 6),
            'mark_delta_candidate_minus_baseline': delta,
            'state_mark_deltas': state_deltas,
            'state_marks_absent_from_delta': [n for n in STATE_MARKS if n not in delta],
            'marks_not_differenced': not_differenced,
            'largest_mark_gaps': sorted(delta.items(), key=lambda kv: -abs(kv[1]))[:12],
            'summing_note': ('per-mark medians are differenced individually; nesting between marks is not '
                             'reported by graph-profile, so these differences are not summed and no single '
                             'number here attributes the wall gap'),
        }

    path = HERE / 'profile-comparison.json'
    path.write_text(json.dumps(out, indent=2) + '\n')
    print(json.dumps({'written': str(path), 'series_complete': True,
                      'body_wall_gap_s': out['body']['wall_gap_s'],
                      'body_state_mark_deltas': out['body']['state_mark_deltas'],
                      'body_marks_not_differenced': out['body']['marks_not_differenced'],
                      'noop_wall_gap_s': out['noop']['wall_gap_s'],
                      'body_state_bytes_gap': out['body']['state_bytes_gap']}, indent=2))
    return 0


if __name__ == '__main__':
    sys.exit(main())
