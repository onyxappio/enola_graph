import importlib.util,json,tempfile,shutil,hashlib
from pathlib import Path
B=Path(__file__).resolve().parent
spec=importlib.util.spec_from_file_location('summary',B/'summarize-timing.py');m=importlib.util.module_from_spec(spec);spec.loader.exec_module(m)
# Preregistered promotion boundaries: consistency and memory both matter.
import copy
arms={a:{'initial':{'rss_bytes':{'median':100},'seconds':{'median':100 if a=='baseline' else 97}}} for a in ('baseline','candidate')}
comp={'candidate_vs_baseline':{'initial':{'median_percent_change':-3,'paired_percent_changes':[-1,-3,-4]}}}
assert m.stage_decision(arms,comp,'baseline','candidate')['engineering_accepted']
bad=copy.deepcopy(comp);bad['candidate_vs_baseline']['initial']['paired_percent_changes'][0]=1
assert not m.stage_decision(arms,bad,'baseline','candidate')['engineering_accepted']
bad=copy.deepcopy(arms);bad['candidate']['initial']['rss_bytes']['median']=106
assert not m.stage_decision(bad,comp,'baseline','candidate')['engineering_accepted']
with tempfile.TemporaryDirectory(prefix='stage23-24-summary-test-') as tmp:
 b=Path(tmp);c=b/'cli-pairs';c.mkdir()
 v=c/'validate_results.py';shutil.copy2(B/'cli-pairs/validate_results.py',v)
 pins=json.loads((B/'cli-pairs/pins.json').read_text());pins['binaries']['candidate']['sha256']='c'*64;pins['files']={str(v):hashlib.sha256(v.read_bytes()).hexdigest()};(c/'pins.json').write_text(json.dumps(pins))
 h=dict(stage23_authorized=True,stage24_authorized=True,fsm_message='f',codata_message='c',worker_message='w',plugin_message='p',start_utc='2026-09-25T02:00:00+00:00',end_utc='2026-09-25T02:10:00+00:00')
 (b/'timing-window.json').write_text(json.dumps(h));runs=[]
 for i,(a,n) in enumerate((a,n) for n,order in enumerate([('baseline','control','candidate'),('control','candidate','baseline'),('candidate','baseline','control')],1) for a in order):
  runs.append(dict(arm=a,repeat=n,exit=0,start_utc=f'2026-09-25T02:0{i}:00+00:00',end_utc=f'2026-09-25T02:0{i}:30+00:00'))
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
 bad=[dict(r) for r in runs];bad[-1]['end_utc']='2026-09-25T02:11:00+00:00';assert not run(bad)['complete']
 bad=[dict(r) for r in runs];bad[2]['exit']=1;assert not run(bad)['complete']
 assert not run(hold=dict(h,worker_message='other'))['complete']
 # Explicit observational mode: never substitute a fabricated FSM ACK.
 import background_gate as bg
 pre=b/'preflight';pre.mkdir()
 samples=[dict(duration=.05,monotonic=float(i),parked={},paging=dict(Pageins=0,Compressions=0,Swapins=0),processes=[]) for i in range(60)]
 (pre/'roster.json').write_text('{}')
 (pre/'samples.jsonl').write_text(''.join(json.dumps(s)+'\n' for s in samples))
 limits=bg.calibrate(samples,{})
 (pre/'receipt.json').write_text(json.dumps(dict(valid=True,failures=[],samples=60,limits=limits,release_message='p',source_sha256=hashlib.sha256(Path(bg.__file__).read_bytes()).hexdigest())))
 obs=dict(h,mode='controlled-background',fsm_message=None,observation_review_message='review',preflight_path=str(pre),preflight_sha256={n:hashlib.sha256((pre/n).read_bytes()).hexdigest() for n in ['receipt.json','roster.json','samples.jsonl']})
 for a,n in [(a,n) for n in range(1,4) for a in ('baseline','control','candidate')]:
  folder=c/f'{a}-{n}';rp=folder/'pair-receipt.json';receipt=json.loads(rp.read_text())
  raw=samples[:3];vec=[bg.rates(x,y,{}) for x,y in zip(raw,raw[1:])]
  receipt.update(background_mode='controlled-background',background_owner_pid=100,background_errors=[],background_vectors=vec)
  rp.write_text(json.dumps(receipt));(folder/'background-load.jsonl').write_text(''.join(json.dumps(s)+'\n' for s in raw))
 (b/'timing-window.json').write_text(json.dumps(obs))
 assert run(hold=obs)['complete']
 evidence=c/'candidate-2/background-load.jsonl';original=evidence.read_text()
 evidence.write_text(original+json.dumps({'error':'permission failure'})+'\n')
 assert not run(hold=obs)['complete'];evidence.write_text(original)
 rp=c/'candidate-2/pair-receipt.json';original_receipt=rp.read_text();r=json.loads(original_receipt);r['background_vectors'][0]['parked.user']=1;rp.write_text(json.dumps(r))
 assert not run(hold=obs)['complete'];rp.write_text(original_receipt)
 badobs=dict(obs,observation_review_message=None);(b/'timing-window.json').write_text(json.dumps(badobs));assert not run(hold=badobs)['complete']
 (b/'timing-window.json').write_text(json.dumps(h))
 # Keep own-arm cold equivalence while changing both distinct graph hashes.
 p=c/'candidate-3/metrics.json';raw=p.read_text();metrics=json.loads(raw);distinct=sorted({r['graph_hash'] for r in metrics})
 for old,new in zip(distinct,['a'*64,'b'*64]):raw=raw.replace(old,new)
 p.write_text(raw);r=run();assert not r['complete'] and 'cross-arm/repeat' in str(r),r
 print('PASS: synthetic valid cohort accepted; partial, overrun, failed arm, hold mismatch and internally cold-equal graph drift refused. No timing evidence generated.')
