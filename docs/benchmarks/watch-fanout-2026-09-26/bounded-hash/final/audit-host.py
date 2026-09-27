"""Describe sampled foreign CPU load; no new timing acceptance thresholds."""
import collections, json, pathlib, statistics, sys
base=pathlib.Path(sys.argv[1]); out=[]
series=json.loads((base/'timing-series.json').read_text())
for run in series['runs']:
 d=base/'cli-pairs'/f"{run['arm']}-{run['repeat']}"
 receipt=json.loads((d/'pair-receipt.json').read_text())
 owner=receipt['background_owner_pid']; samples=[]; errors=[]; names=set(); times=[]
 for line in (d/'host-load.jsonl').read_text().splitlines():
  record=json.loads(line)
  if 'error' in record: errors.append(record); continue
  rows=[]
  for raw in record['processes'].splitlines():
   fields=raw.strip().split(None,4)
   if len(fields)!=5: continue
   try: rows.append((int(fields[0]),int(fields[1]),float(fields[2]),fields[4]))
   except ValueError:continue
  owned={owner}
  while True:
   children={p for p,parent,_,_ in rows if parent in owned}
   if children<=owned:break
   owned|=children
  sample=collections.defaultdict(float)
  for pid,_,cpu,cmd in rows:
   if pid not in owned:sample[pathlib.Path(cmd.split(None,1)[0]).name]+=cpu
  samples.append(sample);names.update(sample);times.append(record['time'])
 stats=[]
 for name in names:
  values=[sample.get(name,0) for sample in samples]
  ordered=sorted(values)
  stats.append({'executable':name,'mean_sampled_cpu_percent':statistics.mean(values),'p95_sampled_cpu_percent':ordered[min(len(ordered)-1,int(.95*len(ordered)))],'max_sampled_cpu_percent':max(values)})
 stats.sort(key=lambda r:r['mean_sampled_cpu_percent'],reverse=True)
 out.append({'arm':run['arm'],'repeat':run['repeat'],'samples':len(samples),'errors':errors,'max_sample_gap_s':max([b-a for a,b in zip(times,times[1:])],default=None),'top_foreign_executables':stats[:12]})
print(json.dumps({'note':'Descriptive ps CPU samples, not instantaneous utilization or a new acceptance threshold. Owned runner descendants excluded; command arguments omitted.','arms':out},indent=2))
