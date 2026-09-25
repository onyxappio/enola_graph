"""Audit exactly one pinned transition; preserve original 10-case gate refusal."""
import copy,hashlib,json,pathlib
import gates
HERE=pathlib.Path(__file__).resolve().parent
EXPECTED=['d0fbbf855af5f4a33c364885f85805d71f9fac7c','a2ac71af8a22471c27059a9b318ef4880caa50cb']
def verify(d):
 assert d.get('completed_sequence') is True and not d.get('error') and not d.get('teardown_errors')
 assert d.get('baseline_arm') is True
 assert d['expected_commits']==EXPECTED
 rows=d['series_gates'];keys={r['key'] for r in rows}
 assert set(gates.REQUIRED_SERIES_GATES)|{'baseline-binary-pin'} <= keys
 assert all(r['pass'] is True for r in rows)
 steps=d['steps'];assert len(steps)==2
 assert [s['expected_commit'] for s in steps]==EXPECTED
 assert [s['kind'] for s in steps]==['initial','transition']
 for s in steps:
  assert s['completed'] is True
  actual=gates.step_gates(s)
  assert s['gates']==actual and all(r['pass'] is True for r in actual)
  assert gates.BASELINE_GATE in {r['key'] for r in actual}
  assert gates.step_verdict(s)['accepted']
 return True
p=HERE/'receipt-config-add.json';raw=p.read_bytes();d=json.loads(raw)
assert d['accepted'] is False and d['problems']==['expected exactly 11 pinned commits, got 2','expected 10 first-parent transitions, got 1']
verify(d)
for label,mutate in [('wrong cold hash',lambda x:x['steps'][1].update(candidate_cold_hash='a'*64)),('nonzero no-op parses',lambda x:x['steps'][1]['noop'].update(parsed_files=1)),('incomplete sequence',lambda x:x.update(completed_sequence=False))]:
 x=copy.deepcopy(d);mutate(x)
 try:verify(x)
 except AssertionError:pass
 else:raise RuntimeError('failed refusal: '+label)
out={'accepted_single_transition':True,'not_ten_transition_acceptance':True,'original_harness_exit':1,'original_receipt_sha256':hashlib.sha256(raw).hexdigest(),'original_refusal':d['problems'],'expected_commits':EXPECTED,'checks':'All original source/tool/clean-tree/cold/noop gates rechecked, exact2-revision1-transition shape, three negative audit controls rejected','delta_parsed_files':d['steps'][1]['chain']['result']['ParsedFiles'],'delta_published_owners':d['steps'][1]['chain']['result']['OwnersPublished'],'limitations':'Config addition plus source/manifests, not isolated existing-tsconfig mutation; correctness only, no timing claim'}
(HERE/'config-add-independent-audit.json').write_text(json.dumps(out,indent=2));print(json.dumps(out,indent=2))
