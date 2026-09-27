"""Independent receipt arithmetic; no replacement for the frozen validator."""
import pathlib,json,statistics,hashlib
b=pathlib.Path(__file__).resolve().parent
series=json.loads((b/'timing-series.json').read_text())
assert len(series['runs'])==12
samples={a:{} for a in ['baseline','candidate']}
hashes={};host=[]
for r in series['runs']:
 a=r['arm'];i=r['repeat'];d=b/'cli-pairs'/f'{a}-{i}'
 receipt=json.loads((d/'pair-receipt.json').read_text());metrics=json.loads((d/'metrics.json').read_text())
 assert r['exit']==0 and r['error'] is None and receipt['run_succeeded'] and not receipt['correctness_only']
 assert not receipt['host_sample_errors'] and not receipt['competing_samples']
 records=[json.loads(l) for l in (d/'host-load.jsonl').read_text().splitlines()]
 assert len(records)==receipt['host_load_samples'] and all('error' not in x for x in records)
 foreign=[];pid=receipt['background_owner_pid']
 for record in records:
  rows=[]
  for line in record['processes'].splitlines():
   fields=line.split(None,4)
   if len(fields)!=5:continue
   try:rows.append((int(fields[0]),int(fields[1]),float(fields[2])))
   except ValueError:continue
  owned={pid}
  while True:
   expanded=owned|{p for p,parent,cpu in rows if parent in owned}
   if expanded==owned:break
   owned=expanded
  foreign.append(sum(cpu for p,parent,cpu in rows if p not in owned))
 host.append(dict(arm=a,repeat=i,samples=len(records),mean_foreign_cpu_percent=statistics.mean(foreign),max_foreign_cpu_percent=max(foreign)))
 assert len(metrics)==7 and len({x['label'] for x in metrics})==7
 for x in metrics:
  assert x['exit']==0
  label=x['label'];digest=x['graph_hash']
  assert label not in hashes or hashes[label]==digest
  hashes[label]=digest
  if label.startswith('r1-new-'):
   scenario=label[len('r1-new-'):];samples[a].setdefault(scenario,[]).append(x)
   if scenario=='noop':assert x['result']['ParsedFiles']==0 and x['result']['BaseGeneration']==x['result']['TargetGeneration']
comparison={}
for scenario in ['initial','noop','body','structural']:
 before=samples['baseline'][scenario];after=samples['candidate'][scenario]
 assert len(before)==len(after)==6
 med=statistics.median
 comparison[scenario]=dict(time_ratio=med(x['seconds'] for x in after)/med(x['seconds'] for x in before),rss_ratio=med(x['rss_bytes'] for x in after)/med(x['rss_bytes'] for x in before),paired_ratios=[y['seconds']/x['seconds'] for x,y in zip(before,after)])
assert comparison['noop']['time_ratio']<=.98 and all(r<1 for r in comparison['noop']['paired_ratios'])
assert all(x['time_ratio']<=1.02 and x['rss_ratio']<=1.05 for x in comparison.values())
assert statistics.mean(x['time_ratio'] for x in comparison.values())<1
assert sum(statistics.median(x['seconds'] for x in rows) for rows in samples['candidate'].values())<=sum(statistics.median(x['seconds'] for x in rows) for rows in samples['baseline'].values())
out=dict(primary_arithmetic_passed=True,cli_calls=84,graph_labels_equal_across_all_arms=7,comparisons=comparison,host_context=host,note='Foreign CPU is descriptive, not a new acceptance threshold. Frozen host/power/pressure checks remain separately validated by summarize-timing.py. No claim of a clean host or statistical significance.')
(b/'primary-audit.json').write_text(json.dumps(out,indent=2)+'\n')
print(json.dumps(out,indent=2))
