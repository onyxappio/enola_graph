import pathlib,json,re
r=pathlib.Path(__file__).resolve().parent
out={'diagnostic_only':True,'timing_acceptance':False,'method':'Difference in runtime TotalAlloc at directly adjacent pre/post json.Marshal trace boundaries; includes tiny trace overhead and concurrent goroutine allocation, not exclusive CPU attribution.','arms':{}}
metrics={}
for arm in ('baseline','candidate'):
 d=r/'cli-pairs'/f'correctness-{arm}-1';receipt=json.loads((d/'pair-receipt.json').read_text());assert receipt['run_succeeded'] and receipt['all_checks_validated'] and not receipt['timing_eligible']
 metrics[arm]=json.loads((d/'metrics.json').read_text());rows={}
 for name in ('r1-new-initial','r1-new-body','r1-new-structural'):
  lines=(d/(name+'.log')).read_text().splitlines();phases=[]
  for line in lines:
   if line.startswith('[memory-phase] '):phases.append(dict(re.findall(r'(\w+)=([^ ]+)',line)))
  starts=[p for p in phases if p['phase']=='state_json_marshal_start'];ends=[p for p in phases if p['phase']=='state_json_marshal'];assert len(starts)==len(ends)==1
  before,after=starts[0],ends[0];rows[name]={'allocated_bytes':int(after['total_alloc'])-int(before['total_alloc']),'mallocs':int(after['mallocs'])-int(before['mallocs']),'gc_before':int(before['num_gc']),'gc_after':int(after['num_gc']),'before':before,'after':after}
 out['arms'][arm]=rows
assert len(metrics['baseline'])==len(metrics['candidate'])==7
assert all(x['graph_hash']==y['graph_hash'] for x,y in zip(metrics['baseline'],metrics['candidate']))
out['seven_cross_arm_graphs_equal']=True
(r/'checkpoint-allocation.json').write_text(json.dumps(out,indent=2)+'\n')
for arm,rows in out['arms'].items():
 for name,row in rows.items():print(arm,name,row['allocated_bytes'],row['mallocs'],row['gc_before'],row['gc_after'])
