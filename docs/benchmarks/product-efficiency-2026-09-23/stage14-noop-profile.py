import pathlib,subprocess,json,time,hashlib,os
b=pathlib.Path(__file__).resolve().parent
w=pathlib.Path('/tmp/enola-stage9-main-history10')
binary=pathlib.Path('/tmp/enola-stage13-watch-pairs/enola-candidate')
state=w/'state-live';events=w/'events-live.jsonl'
def hashes():
 return {str(p.relative_to(state)):hashlib.sha256(p.read_bytes()).hexdigest() for p in sorted(state.rglob('*')) if p.is_file() and p.name!='session.lock'}
if (b/'receipt.json').exists():raise SystemExit('refusing overwrite')
before=hashes(); size=events.stat().st_size
argv=[str(binary),'graph','delta','--authoritative-scope','--max-begin-bytes','1048576','--events',str(events),'--state-dir',str(state),'--context','scope-bench','--repo-id','product-scope','--summary-json',str(w/'live')]
env=os.environ.copy();env['ENOLA_GRAPH_PROFILE']='1'
t=time.monotonic()
with (b/'summary.json').open('w') as out,(b/'profile.stderr').open('w') as err:
 r=subprocess.run(argv,stdout=out,stderr=err,env=env)
wall=time.monotonic()-t
receipt={'argv':argv,'wall_s':wall,'exit_code':r.returncode,'binary_sha256':hashlib.sha256(binary.read_bytes()).hexdigest(),'state_unchanged':before==hashes(),'events_unchanged':size==events.stat().st_size,'kind':'single instrumented fresh CLI diagnostic, file sink, existing completed history state; not paired performance acceptance'}
(b/'receipt.json').write_text(json.dumps(receipt,indent=2)+'\n');print(json.dumps(receipt))
assert r.returncode==0 and receipt['state_unchanged'] and receipt['events_unchanged'],receipt
