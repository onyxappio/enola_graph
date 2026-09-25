import importlib.util,json,tempfile,shutil,hashlib
from pathlib import Path
B=Path(__file__).resolve().parent
spec=importlib.util.spec_from_file_location('summary',B/'summarize-timing.py');m=importlib.util.module_from_spec(spec);spec.loader.exec_module(m)
with tempfile.TemporaryDirectory(prefix='stage21-summary-test-') as tmp:
 b=Path(tmp);c=b/'cli-pairs';c.mkdir()
 v=c/'validate_results.py';shutil.copy2(B/'cli-pairs/validate_results.py',v)
 pins=json.loads((B/'cli-pairs/pins.json').read_text());pins['files']={str(v):hashlib.sha256(v.read_bytes()).hexdigest()};(c/'pins.json').write_text(json.dumps(pins))
 h=dict(stage21_authorized=True,fsm_message='f',codata_message='c',worker_message='w',start_utc='2026-09-25T02:00:00+00:00',end_utc='2026-09-25T02:10:00+00:00')
 (b/'timing-window.json').write_text(json.dumps(h));runs=[]
 for i,(a,n) in enumerate((a,n) for n in range(1,4) for a in (('baseline','candidate') if n%2 else ('candidate','baseline'))):
  runs.append(dict(arm=a,repeat=n,exit=0,start_utc=f'2026-09-25T02:0{i}:00+00:00',end_utc=f'2026-09-25T02:0{i}:30+00:00'))
  d=c/f'{a}-{n}';d.mkdir()
  for name in ['metrics.json','checks.json']:shutil.copy2(B/'cli-pairs/correctness-candidate-2'/name,d/name)
  r=dict(timing_eligible=True,run_succeeded=True,all_checks_validated=True,exit=0,correctness_only=False,competing_samples=[],quiet_ack_message_id='w',arm=a,repeat=n,binary_sha256=pins['binaries'][a]['sha256'])
  (d/'pair-receipt.json').write_text(json.dumps(r))
 def run(rs=runs,hold=h):
  (b/'timing-series.json').write_text(json.dumps(dict(hold=hold,runs=rs)));return m.summarize(b)
 assert run()['complete']
 assert not run(runs[:-1])['complete']
 bad=[dict(r) for r in runs];bad[-1]['end_utc']='2026-09-25T02:11:00+00:00';assert not run(bad)['complete']
 bad=[dict(r) for r in runs];bad[2]['exit']=1;assert not run(bad)['complete']
 assert not run(hold=dict(h,worker_message='other'))['complete']
 # Keep own-arm cold equivalence while changing both distinct graph hashes.
 p=c/'candidate-3/metrics.json';raw=p.read_text();metrics=json.loads(raw);distinct=sorted({r['graph_hash'] for r in metrics})
 for old,new in zip(distinct,['a'*64,'b'*64]):raw=raw.replace(old,new)
 p.write_text(raw);r=run();assert not r['complete'] and 'cross-arm/repeat' in str(r),r
 print('PASS: synthetic valid cohort accepted; partial, overrun, failed arm, hold mismatch and internally cold-equal graph drift refused. No timing evidence generated.')
