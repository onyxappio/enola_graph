from pathlib import Path
import json,subprocess,hashlib,time,os
base=Path('/tmp/enola-fanout-current-20260926');repo=base/'candidate-product';state=base/'profile-3599a8f/state/state.json';events=base/'profile-3599a8f/events.jsonl';out=Path('/tmp/enola-nuxt-noop-profile')
device=repo/'packages/contracts/src/device.ts';original=device.read_bytes();rows=[]
try:
 device.write_bytes(original+b'\nexport const profileFanoutProbe = 1;\n')
 for label,binary in [('candidate','/tmp/enola-nuxt-listing-proof/enola-nuxt-listing-experimental')]:
  before=hashlib.sha256(state.read_bytes()).hexdigest();size=events.stat().st_size
  cmd=['/usr/bin/time','-l',binary,'graph','delta','--authoritative-scope','--changed-owner-scope','--max-begin-bytes','8000000','--events',str(events),'--repo-id','product','--context','profile','--state-dir',str(state.parent),'--summary-json',str(repo)]
  start=time.monotonic()
  with (out/(label+'.stdout')).open('w') as stdout,(out/(label+'.stderr')).open('w') as stderr:
   p=subprocess.run(cmd,stdout=stdout,stderr=stderr,env={**os.environ,'ENOLA_GRAPH_PROFILE':'1'})
  row={'label':label,'wall_s':time.monotonic()-start,'exit':p.returncode,'state_unchanged':before==hashlib.sha256(state.read_bytes()).hexdigest(),'events_added':events.stat().st_size-size,'binary_sha256':hashlib.sha256(Path(binary).read_bytes()).hexdigest()};rows.append(row)
  (out/'noop-receipt.json').write_text(json.dumps({'diagnostic_only':True,'rows':rows},indent=2));print(row,flush=True)
  if p.returncode or not row['state_unchanged'] or row['events_added'] or json.loads((out/(label+'.stdout')).read_text())[0]['ParsedFiles']!=0:raise RuntimeError('not silent no-op')
finally:
 device.write_bytes(original)
