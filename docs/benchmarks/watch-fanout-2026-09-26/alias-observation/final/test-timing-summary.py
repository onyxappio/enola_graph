import importlib.util,json,tempfile,shutil,hashlib
from pathlib import Path
B=Path(__file__).resolve().parent
spec=importlib.util.spec_from_file_location('summary',B/'summarize-timing.py');m=importlib.util.module_from_spec(spec);spec.loader.exec_module(m)
# Preregistered promotion boundaries: consistency and memory both matter.
import copy
arms={a:{'noop':{'rss_bytes':{'median':100},'seconds':{'median':100 if a=='baseline' else 94}}} for a in ('baseline','candidate')}
comp={'candidate_vs_baseline':{'noop':{'median_percent_change':-6,'paired_percent_changes':[-1,-3,-4]}}}
assert m.stage_decision(arms,comp,'baseline','candidate')['engineering_accepted']
bad=copy.deepcopy(comp);bad['candidate_vs_baseline']['noop']['paired_percent_changes'][0]=1
assert not m.stage_decision(arms,bad,'baseline','candidate')['engineering_accepted']
bad=copy.deepcopy(arms);bad['candidate']['noop']['rss_bytes']['median']=106
assert not m.stage_decision(bad,comp,'baseline','candidate')['engineering_accepted']
# Alias-observation primary is noop, and the preregistered effect floor is 2 percent.
bad=copy.deepcopy(comp);bad['candidate_vs_baseline']['noop']['median_percent_change']=-1.99
assert not m.stage_decision(arms,bad,'baseline','candidate')['engineering_accepted']
edge=copy.deepcopy(comp);edge['candidate_vs_baseline']['noop']['median_percent_change']=-2
assert m.stage_decision(arms,edge,'baseline','candidate')['engineering_accepted']
bad=copy.deepcopy(comp);bad['candidate_vs_baseline']['noop']['median_percent_change']=0
bad['candidate_vs_baseline']['body']={'median_percent_change':-20,'paired_percent_changes':[-20]*6}
assert not m.stage_decision(arms,bad,'baseline','candidate')['engineering_accepted']
with tempfile.TemporaryDirectory(prefix='stage23-24-summary-test-') as tmp:
 b=Path(tmp);c=b/'cli-pairs';c.mkdir()
 v=c/'validate_results.py';shutil.copy2(B/'cli-pairs/validate_results.py',v)
 pins=json.loads((B/'cli-pairs/pins.json').read_text());pins['binaries']['candidate']['sha256']='c'*64;pins['files']={str(v):hashlib.sha256(v.read_bytes()).hexdigest()};(c/'pins.json').write_text(json.dumps(pins))
 h=dict(stage23_authorized=True,stage24_authorized=True,fsm_message='f',codata_message='c',worker_message='w',plugin_message='p',start_utc='2026-09-25T02:00:00+00:00',end_utc='2026-09-25T02:20:00+00:00')
 (b/'timing-window.json').write_text(json.dumps(h));runs=[]
 for i,(a,n) in enumerate((a,n) for n,order in enumerate([('baseline','candidate'),('candidate','baseline')]*3,1) for a in order):
  runs.append(dict(arm=a,repeat=n,exit=0,start_utc=f'2026-09-25T02:{i:02d}:00+00:00',end_utc=f'2026-09-25T02:{i:02d}:30+00:00'))
  runs[-1].update(monotonic_start=.1,monotonic_end=.9)
  label=f'{a}-{n}'
  (b/(label+'-vm-before.txt')).write_text('Swapins: 10.\nSwapouts: 0.\n')
  (b/(label+'-vm-after.txt')).write_text('Swapins: 14.\nSwapouts: 0.\n')
  (b/(label+'-pressure.jsonl')).write_text(''.join(json.dumps(dict(monotonic=t,level=1))+'\n' for t in [0,.5,1]))
  runs[-1]['memory_evidence']=m.require_memory_evidence(b,runs[-1])
  for when,t in [('before',0),('after',1)]:
   (b/(label+'-power-'+when+'.json')).write_text(json.dumps(dict(monotonic=t,wall_time=t,battery_output="Now drawing from 'AC Power'\n -InternalBattery-0 45%; charging",custom_output="AC Power:\n lowpowermode 0\n")))
  runs[-1]['power_evidence']=m.require_power_evidence(b,runs[-1])
  d=c/f'{a}-{n}';d.mkdir()
  for name in ['metrics.json','checks.json']:shutil.copy2(B/'summary-test-fixture'/name,d/name)
  r=dict(timing_eligible=True,run_succeeded=True,all_checks_validated=True,exit=0,correctness_only=False,competing_samples=[],quiet_ack_message_id='w',arm=a,repeat=n,binary_sha256=pins['binaries'][a]['sha256'])
  (d/'pair-receipt.json').write_text(json.dumps(r))
 def run(rs=runs,hold=h):
  (b/'timing-series.json').write_text(json.dumps(dict(hold=hold,runs=rs)));return m.summarize(b)
 assert run()['complete']
 assert not any(d['engineering_accepted'] for d in run()['stage_decisions'].values())
 bad=[dict(r) for r in runs];bad[0]['arm']='control';assert not run(bad)['complete']
 (b/'timing-window.json').write_text(json.dumps(dict(h,stage24_authorized=False)));assert not run(hold=dict(h,stage24_authorized=False))['complete'];(b/'timing-window.json').write_text(json.dumps(h))
 assert not run(runs[:-1])['complete']
 power=b/'candidate-1-power-after.json';saved=power.read_text()
 power.write_text(saved.replace('45%;','19%;'));assert not run()['complete'];power.write_text(saved)
 power.unlink();assert not run()['complete'];power.write_text(saved)
 changed=json.loads(saved);changed['monotonic']=.5;power.write_text(json.dumps(changed));assert not run()['complete'];power.write_text(saved)
 bad=[dict(r) for r in runs];bad[0]['power_evidence']={};assert not run(bad)['complete']
 pressure=b/'candidate-1-pressure.jsonl';saved=pressure.read_text()
 pressure.write_text(saved.replace('"level": 1','"level": 2',1));assert not run()['complete'];pressure.write_text(saved)
 vm=b/'candidate-1-vm-after.txt';saved=vm.read_text()
 vm.write_text('Swapins: 14.\nSwapouts: 1.\n');assert not run()['complete'];vm.write_text(saved)

 bad=[dict(r) for r in runs];bad[-1]['end_utc']='2026-09-25T02:21:00+00:00';assert not run(bad)['complete']
 bad=[dict(r) for r in runs];bad[2]['exit']=1;assert not run(bad)['complete']
 assert not run(hold=dict(h,worker_message='other'))['complete']
 # Keep own-arm cold equivalence while changing both distinct graph hashes.
 p=c/'candidate-3/metrics.json';raw=p.read_text();metrics=json.loads(raw);distinct=sorted({r['graph_hash'] for r in metrics})
 for old,new in zip(distinct,['a'*64,'b'*64]):raw=raw.replace(old,new)
 p.write_text(raw);r=run();assert not r['complete'] and 'cross-arm/repeat' in str(r),r
 print('PASS: synthetic valid cohort accepted; partial, overrun, failed arm, hold mismatch and internally cold-equal graph drift refused. No timing evidence generated.')
