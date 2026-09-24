import pathlib,json,re,hashlib
root=pathlib.Path(__file__).resolve().parent
out={'diagnostic_only':True,'timing_acceptance':False,'caveat':'Single instrumented run per arm under concurrent load; phase counters are not retained heap or a causal explanation of RSS. No forced GC.','arms':{}}
for arm in ('baseline','candidate'):
 d=root/'cli-pairs'/f'correctness-{arm}-1'
 receipt=json.loads((d/'pair-receipt.json').read_text()); assert receipt['run_succeeded'] and receipt['all_checks_validated'] and not receipt['timing_eligible']
 rows={}
 for logfile in sorted(d.glob('*.log')):
  phases=[]
  for line in logfile.read_text().splitlines():
   if line.startswith('[memory-phase] '):
    fields=dict(re.findall(r'(\w+)=([^ ]+)',line)); phases.append({k:int(v) if v.isdigit() else v for k,v in fields.items()})
  if phases:
   assert any(p['phase']=='graph_session_run' for p in phases),logfile
   rows[logfile.name]=phases
 assert len(rows)==7,(arm,len(rows))
 out['arms'][arm]={'binary_sha256':receipt['binary_sha256'],'competing_sample_count':len(receipt['competing_samples']),'logs':rows}
(root/'phase-comparison.json').write_text(json.dumps(out,indent=2)+'\n')
for name in ('r1-new-noop.log','r1-new-body.log','r1-new-structural.log'):
 print(name)
 for arm in ('baseline','candidate'):
  row=next(p for p in out['arms'][arm]['logs'][name] if p['phase']=='graph_session_run');print(arm,row)
