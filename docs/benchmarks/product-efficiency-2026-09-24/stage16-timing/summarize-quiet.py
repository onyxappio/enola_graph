import json,pathlib,statistics
b=pathlib.Path(__file__).resolve().parent
out={};missing=[]
def spread(v):
 return {'n':len(v),'median':statistics.median(v),'min':min(v),'max':max(v),'values':v}
for arm in ['baseline','candidate']:
 series={}
 for repeat in range(1,4):
  p=b/'cli-pairs'/f'{arm}-{repeat}'
  if not (p/'pair-receipt.json').exists():missing.append(str(p));continue
  receipt=json.loads((p/'pair-receipt.json').read_text())
  assert receipt['timing_eligible'] and receipt['all_checks_validated'] and not receipt['competing_samples']
  for row in json.loads((p/'metrics.json').read_text()):
   if not row['label'].startswith('r1-new-'):continue
   label=row['label'].removeprefix('r1-new-')
   values=series.setdefault(label,{})
   for key in ['seconds','first_batch_s','broker_end_s','consumer_end_s','rss_bytes']:
    if row.get(key) is not None:values.setdefault(key,[]).append(row[key])
   values.setdefault('parsed_files',[]).append(row['result']['ParsedFiles'])
   if row.get('wire'):
    for key in ['owner_scope_count','payload_bytes_total','messages_observed']:
     values.setdefault(key,[]).append(row['wire'][key])
 out[arm]={k:{m:spread(v) for m,v in fields.items()} for k,fields in series.items()}
watch={arm:[] for arm in ['baseline','candidate']}
for repeat in range(1,4):
 p=b/'watch-pairs'/f'ordinary-pair-{repeat}.json'
 if not p.exists():missing.append(str(p));continue
 rows=json.loads(p.read_text())
 if len(rows)!=2:missing.append(str(p)+' incomplete pair');continue
 for row in rows:
  assert row['valid_timing'] and row['valid_correctness'] and not row['competing_test_samples']
  watch[row['arm']].append(row)
if missing:
 print(json.dumps({'complete':False,'missing':missing},indent=2));raise SystemExit(2)
for arm,scenarios in out.items():
 assert set(scenarios)=={'initial','noop','body','structural'}
 for scenario in scenarios.values():assert scenario['seconds']['n']==3
comparison={k:{'baseline_s':out['baseline'][k]['seconds'],'candidate_s':out['candidate'][k]['seconds'],'median_percent_change':100*(out['candidate'][k]['seconds']['median']/out['baseline'][k]['seconds']['median']-1)} for k in out['baseline']}
watch_summary={arm:{k:spread([row[k] for row in rows]) for k in ['initial_ms','delta_ms','parsed_files']} for arm,rows in watch.items()}
result={'complete':True,'cli':out,'cli_comparison':comparison,'watch':watch_summary,'note':'Fresh CLI through process exit (all producer ACKs); broker and consumer boundaries separately. Watch includes500ms collection window. Ambient ClickHouse load recorded in per-arm container logs; sampled competitor absence does not prove zero host contention.'}
(b/'quiet-comparison.json').write_text(json.dumps(result,indent=2)+'\n');print(json.dumps({'cli_comparison':comparison,'watch':watch_summary},indent=2))
