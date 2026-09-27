from pathlib import Path
import json,hashlib,collections
b=Path('/tmp/enola-framework-prepass/acceptance-v2');audit=[]
for repeat in range(1,7):
 for arm in ['baseline','candidate']:
  d=b/'cli-pairs'/f'{arm}-{repeat}';receipt=json.loads((d/'pair-receipt.json').read_text());p=d/'host-load.jsonl';samples=[json.loads(x) for x in p.read_text().splitlines()]
  assert not receipt['host_sample_errors'] and not receipt['competing_samples'] and len(samples)==receipt['host_load_samples'] and samples
  sums=collections.defaultdict(float);peaks=collections.defaultdict(float)
  for sample in samples:
   rows=[]
   for line in sample['processes'].splitlines():
    parts=line.split(None,4)
    if len(parts)==5:rows.append((int(parts[0]),int(parts[1]),float(parts[2]),parts[4]))
   owned={receipt['background_owner_pid']}
   while True:
    more={pid for pid,ppid,cpu,cmd in rows if ppid in owned}
    if more<=owned:break
    owned|=more
   for pid,ppid,cpu,cmd in rows:
    if pid in owned:continue
    name=Path(cmd.split()[0]).name;sums[name]+=cpu;peaks[name]=max(peaks[name],cpu)
  top=sorted(sums,key=sums.get,reverse=True)[:8]
  audit.append({'arm':arm,'repeat':repeat,'samples':len(samples),'host_errors':receipt['host_sample_errors'],'recognized_competitor_samples':len(receipt['competing_samples']),'load1_min':min(x['loadavg'][0] for x in samples),'load1_max':max(x['loadavg'][0] for x in samples),'top_background':[{'name':k,'mean_cpu_per_sample':sums[k]/len(samples),'max_process_cpu':peaks[k]} for k in top],'raw_path':str(p),'sha256':hashlib.sha256(p.read_bytes()).hexdigest()})
(b/'host-audit.json').write_text(json.dumps(audit,indent=2)+'\n')
print('12 host logs validated, no recognized competitor samples or host-sample errors')
print('load1 range',min(x['load1_min'] for x in audit),max(x['load1_max'] for x in audit))
print('leading background names',sorted({x['top_background'][0]['name'] for x in audit}))
