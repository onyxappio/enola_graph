"""Behavioral tests for the acceptance gates. Synthetic receipts only.

Runs no workload: it never touches a binary, a broker, the fixture or a state
dir. It drives the pure acceptance functions of run-profile-arm.py directly, and
drives summarize.py as a subprocess over fabricated run directories in a fresh
temp tree, asserting what it accepts and what it refuses.

    python3 -B gate-tests.py
"""
import copy, importlib.util, json, pathlib, shutil, subprocess, sys, tempfile

HERE = pathlib.Path(__file__).resolve().parent
FAILURES = []
SCRATCH = []
KEEP = '--keep' in sys.argv


def load(name):
    spec = importlib.util.spec_from_file_location(name.replace('-', '_'), HERE / (name + '.py'))
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod


def check(desc, got, want):
    ok = got == want
    print(('PASS  ' if ok else 'FAIL  ') + desc + ('' if ok else '\n        got  ' + repr(got) + '\n        want ' + repr(want)))
    if not ok:
        FAILURES.append(desc)


HASH = 'a' * 64
OTHER = 'b' * 64
MARKS = ('state_read_bytes', 'state_fingerprint', 'state_json_unmarshal', 'state_json_marshal',
         'graph_session_run', 'extractor_need')


def mark_rows(base, drop=(), repeat=()):
    rows = []
    for i, name in enumerate(MARKS):
        if name in drop:
            continue
        rows.append({'mark': name, 'seconds': round(base + 0.01 * i, 6), 'total_seconds': None,
                     'trace': 'session', 'detail': ''})
        if name in repeat:
            rows.append({'mark': name, 'seconds': round(base + 0.01 * i + 0.002, 6), 'total_seconds': None,
                         'trace': 'reconcile', 'detail': 'second occurrence'})
    return rows


def checks_all_pass(keys):
    return [{'key': k, 'check': k, 'pass': True} for k in keys]


def build_run(root, arm, repeat, *, eligible=True, body_hash=HASH, oracle=True,
              drop_body=(), repeat_body=(), quiet='msg_5a77572fcaee', overrides=None):
    arm_mod = load('run-profile-arm')
    d = root / (arm + '-' + str(repeat))
    d.mkdir(parents=True)
    keys = list(arm_mod.REQUIRED_CHECKS)
    if oracle:
        keys.append('oracle-match')
    receipt = {'arm': arm, 'repeat': repeat, 'quiet_message_id': quiet, 'completed_sequence': True,
               'control_restored': True, 'competing_samples': [], 'checks': checks_all_pass(keys),
               'body_hash': body_hash, 'binary_sha256': 'deadbeef',
               'required_checks_missing': [], 'required_checks_present': True, 'all_checks_pass': True,
               'timing_eligible': eligible}
    if oracle:
        receipt['oracle_hash'] = body_hash
    if overrides:
        receipt.update(copy.deepcopy(overrides))
    (d / 'receipt.json').write_text(json.dumps(receipt, indent=2))
    scale = 1.0 if arm == 'baseline' else 1.03
    metrics = []
    for label, wall in (('initial', 8.4), ('noop', 1.7), ('body', 3.5)):
        metrics.append({'label': label, 'profiled': label != 'initial', 'seconds': round(wall * scale + repeat * 0.01, 6),
                        'exit': 0, 'state_final_bytes': 55_000_000 + (700_000 if arm == 'candidate' else 0) + repeat,
                        'process_real_s': wall, 'rss_bytes': 1_000_000, 'user_s': round(wall * 1.1, 6),
                        'sys_s': round(wall * 0.2, 6), 'result': {}})
    (d / 'metrics.json').write_text(json.dumps(metrics, indent=2))
    (d / 'marks-noop.json').write_text(json.dumps(mark_rows(0.02 * scale, drop=('state_json_marshal',)), indent=2))
    (d / 'marks-body.json').write_text(json.dumps(mark_rows(0.03 * scale, drop=drop_body, repeat=repeat_body), indent=2))
    return d


def build_series(root, **per_run):
    for arm in ('baseline', 'candidate'):
        for repeat in (1, 2, 3):
            kwargs = dict(per_run.get(arm + '-' + str(repeat), {}))
            build_run(root, arm, repeat, oracle=(repeat == 1), **kwargs)
    return root


def run_summarize(root):
    shutil.copy(HERE / 'summarize.py', root / 'summarize.py')
    p = subprocess.run([sys.executable, '-B', 'summarize.py'], cwd=root, capture_output=True, text=True)
    out = root / 'profile-comparison.json'
    return p.returncode, (json.loads(out.read_text()) if out.exists() else None), p.stdout + p.stderr


def scenario(name, **per_run):
    root = pathlib.Path(tempfile.mkdtemp(prefix='gate-tests-' + name + '-'))
    SCRATCH.append(root)
    build_series(root, **per_run)
    return root


def main():
    arm = load('run-profile-arm')

    print('== run-profile-arm.py required marks, per label')
    full_body = {'noop': mark_rows(0.02, drop=('state_json_marshal',)), 'body': mark_rows(0.03)}
    check('complete labels have no missing marks', arm.missing_marks(full_body), {'noop': [], 'body': []})
    check('noop does not require state_json_marshal',
          arm.missing_marks({'noop': mark_rows(0.02, drop=('state_json_marshal',)), 'body': mark_rows(0.03)})['noop'], [])
    check('body requires state_json_marshal',
          arm.missing_marks({'noop': full_body['noop'], 'body': mark_rows(0.03, drop=('state_json_marshal',))})['body'],
          ['state_json_marshal'])
    check('a missing state mark on body is reported',
          arm.missing_marks({'noop': full_body['noop'], 'body': mark_rows(0.03, drop=('state_read_bytes',))})['body'],
          ['state_read_bytes'])
    check('empty marks report every required mark for the label',
          arm.missing_marks({})['body'], list(arm.REQUIRED_MARKS['body']))

    print('== run-profile-arm.py eligibility arithmetic')
    green_checks = checks_all_pass(arm.REQUIRED_CHECKS)
    green = {'completed_sequence': True, 'control_restored': True}
    check('complete green run is eligible', arm.eligibility(green, green_checks, [])['timing_eligible'], True)
    check('truncated gate list is not eligible',
          arm.eligibility(green, checks_all_pass(('quiet-slot', 'initial-fresh')), [])['timing_eligible'], False)
    check('truncated gate list names the absent gates',
          'body-scenario' in arm.eligibility(green, checks_all_pass(('quiet-slot',)), [])['required_checks_missing'], True)
    check('sequence not completed is not eligible',
          arm.eligibility({'completed_sequence': False, 'control_restored': True}, green_checks, [])['timing_eligible'], False)
    check('competing samples are not eligible',
          arm.eligibility(green, green_checks, [['123 enola-something']])['timing_eligible'], False)
    check('unrestored control file is not eligible',
          arm.eligibility({'completed_sequence': True, 'control_restored': False}, green_checks, [])['timing_eligible'], False)
    failing = green_checks[:-1] + [{'key': green_checks[-1]['key'], 'check': 'x', 'pass': False}]
    check('one failing gate is not eligible', arm.eligibility(green, failing, [])['timing_eligible'], False)
    check('no gates at all is not eligible', arm.eligibility(green, [], [])['timing_eligible'], False)

    print('== summarize.py accepts a complete synthetic series')
    root = scenario('complete')
    code, out, log = run_summarize(root)
    check('exit 0', code, 0)
    check('no series problems', out['series_problems'], [])
    check('per-run state bytes are reported for every run',
          sorted(out['body']['arms']['candidate']['per_run']), ['candidate-1', 'candidate-2', 'candidate-3'])
    check('state byte spread is reported',
          set(out['body']['arms']['baseline']['state_final_bytes']), {'n', 'min', 'max', 'median', 'range'})
    check('state marks are differenced individually',
          sorted(out['body']['state_mark_deltas']), sorted(arm.REQUIRED_MARKS['body'][:4]))
    check('no summed state gap field exists', 'state_mark_gap_s' in out['body'], False)
    check('noop delta omits state_json_marshal', 'state_json_marshal' in out['noop']['state_mark_deltas'], False)
    check('noop reports the absent state mark',
          out['noop']['state_marks_absent_from_delta'], ['state_json_marshal'])

    print('== summarize.py refuses an incomplete or contaminated series')
    for name, kwargs, needle in [
        ('missing', {}, 'missing receipt: candidate-3'),
        ('ineligible', {'candidate-2': {'eligible': False, 'overrides': {'completed_sequence': False,
                                                                        'error': {'message': 'noop failed'}}}},
         'ineligible run: candidate-2'),
        ('nullhash', {'baseline-2': {'body_hash': None}}, 'missing or malformed body hash in baseline-2'),
        ('shorthash', {'baseline-2': {'body_hash': 'abc123'}}, 'missing or malformed body hash in baseline-2'),
        ('mismatch', {'candidate-2': {'body_hash': OTHER}}, 'body hashes differ across runs'),
        ('quiet', {'candidate-3': {'quiet': 'nope'}}, 'missing or malformed quiet_message_id'),
    ]:
        r = scenario(name, **kwargs)
        if name == 'missing':
            shutil.rmtree(r / 'candidate-3')
        code, out, log = run_summarize(r)
        check(name + ': exit 1', code, 1)
        check(name + ': problem reported', any(needle in p for p in out['series_problems']), True)
        check(name + ': no aggregate written', 'body' in out, False)

    r = scenario('extra')
    build_run(r, 'baseline', 4, oracle=False)
    code, out, log = run_summarize(r)
    check('extra run dir: exit 1', code, 1)
    check('extra run dir: reported',
          any('unexpected run dirs present: baseline-4' in p for p in out['series_problems']), True)

    r = scenario('nooracle')
    d = r / 'candidate-1'
    rec = json.loads((d / 'receipt.json').read_text())
    rec.pop('oracle_hash')
    rec['checks'] = [c for c in rec['checks'] if c['key'] != 'oracle-match']
    (d / 'receipt.json').write_text(json.dumps(rec, indent=2))
    code, out, log = run_summarize(r)
    check('missing cold oracle: exit 1', code, 1)
    check('missing cold oracle: hash reported',
          any('missing or malformed cold oracle hash in candidate-1' in p for p in out['series_problems']), True)
    check('missing cold oracle: gate reported',
          any('oracle-match gate did not pass in candidate-1' in p for p in out['series_problems']), True)

    print('== summarize.py preserves repeated marks instead of collapsing them')
    r = scenario('repeated', **{'candidate-2': {'repeat_body': ('state_json_unmarshal',)}})
    code, out, log = run_summarize(r)
    check('repeated mark: exit 0', code, 0)
    check('repeated mark is excluded from the difference',
          'state_json_unmarshal' in out['body']['mark_delta_candidate_minus_baseline'], False)
    check('repeated mark is listed as not differenced',
          'state_json_unmarshal' in out['body']['marks_not_differenced'], True)
    check('repeated mark is listed as an absent state delta',
          out['body']['state_marks_absent_from_delta'], ['state_json_unmarshal'])
    raw = out['body']['arms']['candidate']['marks_repeated_raw']['state_json_unmarshal']
    check('repeated mark keeps its occurrence counts', raw['occurrences_per_run'], [1, 2, 1])
    check('repeated mark keeps both durations raw', len(raw['raw'][1]), 2)
    check('repeated mark keeps distinct traces',
          sorted({o['trace'] for o in raw['raw'][1]}), ['reconcile', 'session'])
    check('other marks are still differenced',
          'graph_session_run' in out['body']['mark_delta_candidate_minus_baseline'], True)

    print('== summarize.py refuses a run whose body lost a required mark')
    r = scenario('dropmark', **{'baseline-3': {'drop_body': ('state_json_marshal',)}})
    code, out, log = run_summarize(r)
    check('dropped required mark: nonzero exit', code != 0, True)
    check('dropped required mark: named in the error',
          'missing required marks: state_json_marshal' in log, True)

    print()
    if KEEP:
        print('kept ' + str(len(SCRATCH)) + ' synthetic tree(s): ' + ', '.join(str(d) for d in SCRATCH))
    else:
        for d in SCRATCH:
            if d.is_dir() and d.name.startswith('gate-tests-') and d.parent == pathlib.Path(tempfile.gettempdir()):
                shutil.rmtree(d, ignore_errors=True)
        print('removed ' + str(len(SCRATCH)) + ' synthetic tree(s); pass --keep to inspect them')
    if FAILURES:
        print(str(len(FAILURES)) + ' FAILURE(S): ' + '; '.join(FAILURES))
        return 1
    print('all gate tests passed; no workload was run')
    return 0


if __name__ == '__main__':
    sys.exit(main())
