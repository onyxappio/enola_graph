#!/usr/bin/env python3
"""Independent CLI probe: inert manifest edits, reverts and real dependency edits.
Writes only a new output directory. Exits nonzero on the known no-op defect.
"""
from pathlib import Path
import subprocess,json,time,argparse,importlib.util,hashlib
p=argparse.ArgumentParser();p.add_argument("--binary",required=True);p.add_argument("--out",required=True);a=p.parse_args()
out=Path(a.out).resolve();out.mkdir(exist_ok=False);repo=out/'repo';repo.mkdir();repo=repo.resolve();events=(out/'events.jsonl').resolve();state=(out/'state').resolve()
(repo/'index.ts').write_text('export function work(){return 1}\n')
manifest={'name':'demo','version':'1.0.0','dependencies':{'some-lib':'^1.0.0'}}
(repo/'package.json').write_text(json.dumps(manifest))
module_path=Path(__file__).resolve().parent.parent/'invalidation-history-2026-09-22/run.py'
spec=importlib.util.spec_from_file_location('history_probe',module_path)
history=importlib.util.module_from_spec(spec);spec.loader.exec_module(history)
scope=history.scope
consumer=scope.Consumer()
rows=[]
(out/'provenance.json').write_text(json.dumps({'binary':str(Path(a.binary).resolve()),'binary_sha256':hashlib.sha256(Path(a.binary).read_bytes()).hexdigest(),'label':'small independent correctness probe, file sink'},indent=2))
for i in range(8):
 if i==1:manifest['version']='1.0.1'
 if i==3:manifest['version']='1.0.0'
 if i==5:manifest['dependencies']['some-lib']='^2.0.0'
 (repo/'package.json').write_text(json.dumps(manifest))
 before=events.stat().st_size if events.exists() else 0
 cmd=[str(Path(a.binary).resolve()),'graph','analyze' if i==0 else 'delta','--authoritative-scope','--events',str(events),'--state-dir',str(state),'--summary-json',str(repo)]
 t=time.perf_counter();p=subprocess.run(cmd,capture_output=True,text=True);elapsed=time.perf_counter()-t
 (out/f'{i}.stdout').write_text(p.stdout);(out/f'{i}.stderr').write_text(p.stderr)
 if p.returncode:raise RuntimeError(p.stderr)
 s=json.loads(p.stdout)[0];rows.append({'step':i,'wall_s':elapsed,'generation':[s['BaseGeneration'],s['TargetGeneration']],'parsed':s['ParsedFiles'],'owners':s['OwnersPublished'],'event_bytes':events.stat().st_size-before})
 pairs=scope.parse_event_records(events,before)
 records=[r for r,_ in pairs]
 if records:
  scope.validate_replacement(records,pairs)
  consumer.apply(records)
 cold_events=out/f'cold-{i}.jsonl'
 cold_cmd=cmd.copy();cold_cmd[2]='analyze'
 cold_cmd[cold_cmd.index('--events')+1]=str(cold_events)
 cold_cmd[cold_cmd.index('--state-dir')+1]=str(out/f'cold-state-{i}')
 cold=subprocess.run(cold_cmd,capture_output=True,text=True)
 (out/f'{i}.cold.stdout').write_text(cold.stdout);(out/f'{i}.cold.stderr').write_text(cold.stderr)
 if cold.returncode:raise RuntimeError(cold.stderr)
 cpairs=scope.parse_event_records(cold_events,0);crecords=[r for r,_ in cpairs]
 scope.validate_replacement(crecords,cpairs)
 expected=scope.Consumer();expected.apply(crecords)
 rows[-1].update(exact_cold=consumer.graph_hash()==expected.graph_hash(),graph_hash=consumer.graph_hash(),cold_hash=expected.graph_hash(),protocol='pass')
 (out/'results.json').write_text(json.dumps(rows,indent=2));print(rows[-1],flush=True)

expected_changes={0,5}
failures=[]
for row in rows:
 if not row['exact_cold']:failures.append({'step':row['step'],'reason':'cold graph mismatch'})
 changed=row['step'] in expected_changes
 if changed and (row['event_bytes']==0 or row['generation'][1]!=row['generation'][0]+1):failures.append({'step':row['step'],'reason':'real graph change missing'})
 if not changed and (row['event_bytes']!=0 or row['generation'][0]!=row['generation'][1]):failures.append({'step':row['step'],'reason':'unchanged graph published or generation advanced'})
(out/'acceptance.json').write_text(json.dumps({'pass':not failures,'failures':failures},indent=2))
raise SystemExit(1 if failures else 0)
