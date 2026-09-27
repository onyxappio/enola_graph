"""Behavioural tests for every refusal path, on fabricated receipts only.

No Product checkout, no binary, no broker, no state directory is touched. The
point is that the guards are exercised before the long run, so a green series
receipt cannot be the first time the acceptance arithmetic ever ran.
"""
import importlib.util, json, pathlib, sys, tempfile

import gates

HERE = pathlib.Path(__file__).resolve().parent
H = {n: format(n, '064x').replace('0', 'a', 1) if False else (('%064x') % (n * 7919)) for n in range(1, 9)}
PASSED = []
FAILED = []


def check(name, ok):
    (PASSED if ok else FAILED).append(name)


def good_step(index=1, commit='c' * 40, baseline=True, kind='transition'):
    step = {'label': '%02d-x' % index, 'kind': kind, 'index': index, 'completed': True,
            'expected_commit': commit, 'head': commit, 'status_before': '', 'status_after': '',
            'published': True, 'effective_hash': H[1], 'candidate_cold_hash': H[1],
            'noop': {'parsed_files': 0, 'base_generation': 3, 'target_generation': 3,
                     'wire_messages': 0, 'state_before': H[2], 'state_after': H[2]}}
    if baseline:
        step['baseline_cold_hash'] = H[1]
    step['gates'] = gates.step_gates(step)
    step.update(gates.step_verdict(step))
    return step


def verdict_of(step):
    step['gates'] = gates.step_gates(step)
    step.update(gates.step_verdict(step))
    return step


def failed_keys(step):
    return sorted(r['key'] for r in step['gates'] if not r['pass'])


# --- step gates -------------------------------------------------------------
s = good_step()
check('clean step is accepted', s['accepted'] and not failed_keys(s))
check('every required gate ran', not s['gates_missing'])
check('baseline gate present when the arm ran', gates.BASELINE_GATE in {r['key'] for r in s['gates']})

s = verdict_of(dict(good_step(), candidate_cold_hash=H[3]))
check('chain hash differing from cold fails', failed_keys(s) == [gates.BASELINE_GATE, 'delta-equals-cold'])
check('a differing cold hash rejects the step', not s['accepted'])

s = verdict_of(dict(good_step(), effective_hash=None))
check('absent chain hash fails rather than passing vacuously', 'delta-equals-cold' in failed_keys(s))

s = verdict_of(dict(good_step(), candidate_cold_hash='not-a-hash'))
check('malformed cold hash fails', 'delta-equals-cold' in failed_keys(s))

s = verdict_of(dict(good_step(), effective_hash=H[4], candidate_cold_hash=H[4],
                    baseline_cold_hash=H[4], published=False))
check('unpublished step compares the carried-forward hash and passes when equal', s['accepted'])
check('unpublished step is recorded as such',
      [r for r in s['gates'] if r['key'] == 'candidate-published'][0]['detail']['published'] is False)

s = verdict_of(dict(good_step(), head='d' * 40))
check('checkout at the wrong commit fails', failed_keys(s) == ['source-pin'])

s = verdict_of(dict(good_step(), status_before=' M apps/x.ts'))
check('dirty checkout before the runs fails', failed_keys(s) == ['worktree-clean'])

s = verdict_of(dict(good_step(), status_after='?? .enola/'))
check('a run that wrote into the checkout fails', failed_keys(s) == ['worktree-clean-after'])

for field, value, key in [('parsed_files', 1, 'noop-zero-parses'),
                          ('parsed_files', None, 'noop-zero-parses'),
                          ('target_generation', 4, 'noop-stable-generation'),
                          ('base_generation', None, 'noop-stable-generation'),
                          ('wire_messages', 1, 'noop-zero-events'),
                          ('wire_messages', None, 'noop-zero-events'),
                          ('state_after', H[5], 'noop-state-unchanged'),
                          ('state_before', None, 'noop-state-unchanged')]:
    base = good_step()
    noop = dict(base['noop'])
    noop[field] = value
    s = verdict_of(dict(base, noop=noop))
    check('noop %s=%r fails %s' % (field, value, key), failed_keys(s) == [key] and not s['accepted'])

s = verdict_of(dict(good_step(), noop={}))
check('a missing no-op record fails four gates rather than passing',
      failed_keys(s) == ['noop-stable-generation', 'noop-state-unchanged',
                         'noop-zero-events', 'noop-zero-parses'])

s = verdict_of(dict(good_step(), baseline_cold_hash=H[6]))
check('baseline cold hash differing from candidate fails', failed_keys(s) == [gates.BASELINE_GATE])

s = good_step(baseline=False)
check('no baseline arm leaves the step acceptable on its own terms', s['accepted'])
check('no baseline arm omits the baseline gate', gates.BASELINE_GATE not in {r['key'] for r in s['gates']})

s = good_step()
s['gates'] = [r for r in s['gates'] if r['key'] != 'noop-zero-events']
s.update(gates.step_verdict(s))
check('a truncated gate list is refused', s['gates_missing'] == ['noop-zero-events'] and not s['accepted'])

s = good_step()
s['completed'] = False
s.update(gates.step_verdict(s))
check('an incomplete step is refused even with every gate green', not s['accepted'])

s = dict(good_step(), gates=[])
s.update(gates.step_verdict(s))
check('an empty gate list is refused', not s['accepted'] and not s['all_gates_pass'])


# --- series -----------------------------------------------------------------
COMMITS = tuple(('%040x' % i) for i in range(11))


def good_receipt(baseline=True):
    steps = [good_step(index=i, commit=c, baseline=baseline,
                       kind='initial' if i == 0 else 'transition')
             for i, c in enumerate(COMMITS)]
    return {'completed_sequence': True, 'steps': steps,
            'series_gates': [gates.gate(k, True) for k in gates.REQUIRED_SERIES_GATES]}


r = good_receipt()
check('a complete clean series has no problems', gates.series_problems(r, COMMITS) == [])
check('11 commits give 10 transitions',
      len([s for s in r['steps'] if s['kind'] == 'transition']) == 10)

r = good_receipt()
r['series_gates'] = [g for g in r['series_gates'] if g['key'] != 'policy-pin']
check('a missing series gate is a problem',
      'series gate absent: policy-pin' in gates.series_problems(r, COMMITS))

r = good_receipt()
for g in r['series_gates']:
    if g['key'] == 'candidate-binary-pin':
        g['pass'] = False
check('a failed binary pin is a problem',
      'series gate failed: candidate-binary-pin' in gates.series_problems(r, COMMITS))

r = good_receipt()
r['completed_sequence'] = False
r['error'] = {'message': 'boom'}
p = gates.series_problems(r, COMMITS)
check('an unfinished sequence is refused and names the error',
      any(x.startswith('sequence did not complete') and 'boom' in x for x in p))

r = good_receipt()
r['steps'] = r['steps'][:-1]
p = gates.series_problems(r, COMMITS)
check('a short series is refused',
      any('expected 10 first-parent transitions' in x for x in p)
      and any('not the pinned first-parent chain' in x for x in p))

r = good_receipt()
r['steps'][3], r['steps'][4] = r['steps'][4], r['steps'][3]
check('reordered commits are refused',
      any('not the pinned first-parent chain' in x for x in gates.series_problems(r, COMMITS)))

r = good_receipt()
r['steps'][5] = verdict_of(dict(r['steps'][5], candidate_cold_hash=H[7]))
p = gates.series_problems(r, COMMITS)
check('one failing step fails the series',
      any('failed gates: ' in x and 'delta-equals-cold' in x for x in p))

r = good_receipt()
check('eleven pinned commits are required',
      any('expected exactly 11 pinned commits' in x
          for x in gates.series_problems(r, COMMITS[:10])))

r = good_receipt()
r['steps'][2]['gates'] = [g for g in r['steps'][2]['gates'] if g['key'] != 'delta-equals-cold']
r['steps'][2].update(gates.step_verdict(r['steps'][2]))
p = gates.series_problems(r, COMMITS)
check('a step that skipped a gate is refused',
      any('missing gates: delta-equals-cold' in x for x in p))


# --- what may be claimed ----------------------------------------------------
check('full baseline agreement is claimable',
      'equal at every commit' in gates.baseline_claim(good_receipt()))
check('no baseline arm yields a self-equivalence-only claim',
      'self-equivalence only' in gates.baseline_claim(good_receipt(baseline=False)))
r = good_receipt()
del r['steps'][4]['baseline_cold_hash']
r['steps'][4] = verdict_of(r['steps'][4])
check('a partly-run baseline arm never upgrades to an equivalence claim',
      'self-equivalence only' in gates.baseline_claim(r))
r = good_receipt()
r['steps'][6] = verdict_of(dict(r['steps'][6], baseline_cold_hash=H[8]))
check('a disagreeing baseline is reported as a failure, not silence',
      'FAILED' in gates.baseline_claim(r))


# --- deletion guard ---------------------------------------------------------
spec = importlib.util.spec_from_file_location('run_history', HERE / 'run-history.py')
rh = importlib.util.module_from_spec(spec)
spec.loader.exec_module(rh)
with tempfile.TemporaryDirectory() as tmp:
    work = pathlib.Path(tmp) / 'work'
    (work / 'cold-candidate-00').mkdir(parents=True)
    outside = pathlib.Path(tmp) / 'victim'
    outside.mkdir()
    nested = work / 'cold-candidate-00' / 'deeper'
    nested.mkdir()
    for label, target in [('outside the work directory', outside),
                          ('the work directory itself', work),
                          ('a grandchild of the work directory', nested)]:
        try:
            rh.rmtree_exact(target, work)
            check('rmtree_exact refuses ' + label, False)
        except RuntimeError:
            check('rmtree_exact refuses ' + label, target.is_dir())
    victim = work / 'cold-candidate-00'
    rh.rmtree_exact(victim, work)
    check('rmtree_exact removes a state directory it owns', not victim.exists() and work.is_dir())
    try:
        rh.rmtree_exact(victim, work)
        check('rmtree_exact refuses a path that is not a directory', False)
    except RuntimeError:
        check('rmtree_exact refuses a path that is not a directory', True)
check('the commit chain is pinned to 11 entries ending at the task head',
      len(rh.COMMITS) == 11 and rh.COMMITS[-1] == 'a609c19f3861971930fae7b33dcb2950598953c5'
      and len(set(rh.COMMITS)) == 11)

print(json.dumps({'passed': len(PASSED), 'failed': FAILED}, indent=2))
sys.exit(1 if FAILED else 0)
