from pathlib import Path
import subprocess as sp, json, hashlib, os, time, socket, shutil, signal
ROOT=Path('/tmp/enola-stage19-root-review/rss-run1'); ROOT.mkdir(exist_ok=False)
DIAG=Path('/tmp/enola-stage19-rss-diag'); pins=json.loads((DIAG/'pins.json').read_text())
old=json.loads(Path('/tmp/enola-stage19-root-review/cli-pairs/pins.json').read_text())
BASE=old['binaries']['baseline']['path']; OBS='/tmp/enola-stage9-product-watch-control/bin/benchobserver'; NATS='/tmp/enola-toolchain/bin/nats-server'
for path,h in [(BASE,old['binaries']['baseline']['sha256']),(OBS,old['files'][OBS]),(NATS,old['files'][NATS])]+[(a['binary'],a['binary_sha256']) for a in pins['arms'].values()]:
 assert hashlib.sha256(Path(path).read_bytes()).hexdigest()==h,path
P=ROOT/'product'; sp.run(['git','clone','--shared','--no-hardlinks',old['product_path'],str(P)],check=True,stdout=sp.DEVNULL)
assert sp.check_output(['git','rev-parse','HEAD'],cwd=P,text=True).strip()==old['product_sha']
F=P/'packages/crypto/src/password.ts'; original=F.read_bytes(); body=original.replace(b'return email.trim().toLowerCase();',b"return email.normalize('NFKC').trim().toLowerCase();");assert body!=original
CFG=ROOT/'config.yaml'; policy=Path('docs/benchmarks/product-efficiency-2026-09-22/product-graph-scope.yaml');CFG.write_text('repo: '+str(P)+'\n'+policy.read_text())
s=socket.socket();s.bind(('127.0.0.1',0));port=s.getsockname()[1];s.close();url=f'nats://127.0.0.1:{port}'
state=ROOT/'state';ctx='stage19-rss-diagnostic'; rows=[]; states=[]
def save(): (ROOT/'receipt.json').write_text(json.dumps({'timing_eligible':False,'purpose':'experimental single-pair allocator diagnostic','rows':rows},indent=2))
def stop(p):
 if p and p.poll() is None:
  os.killpg(p.pid,signal.SIGTERM)
  try:p.wait(timeout=10)
  except sp.TimeoutExpired:os.killpg(p.pid,signal.SIGKILL);p.wait()
def await_ready(proc,log):
 deadline=time.monotonic()+30
 while time.monotonic()<deadline:
  assert proc.poll() is None,log.read_text()
  if 'READY' in log.read_text():return
  time.sleep(.05)
 raise RuntimeError('observer ready timeout')
def run(binary,mode,st,context,label,out,diag=False):
 cmd=[binary,'graph',mode,'--authoritative-scope','--max-begin-bytes','1048576','--repo-id','stage15-cli-product','--summary-json','--nats',url,'--state-dir',str(st),'--context',context,str(CFG)]
 env=os.environ.copy()
 for k in ['ENOLA_GRAPH_PROFILE','ENOLA_GRAPH_MEMSTATS','GODEBUG']:env.pop(k,None)
 if diag:env.update(ENOLA_GRAPH_PROFILE='1',ENOLA_GRAPH_MEMSTATS='1',GODEBUG='gctrace=1')
 with (out/(label+'.out')).open('w') as a,(out/(label+'.log')).open('w') as b:
  p=sp.run(['/usr/bin/time','-l',*cmd],stdout=a,stderr=b,env=env,timeout=180)
 assert p.returncode==0,(label,(out/(label+'.log')).read_text()[-2000:])
 result=json.loads((out/(label+'.out')).read_text())[0]; deadline=time.monotonic()+30
 while time.monotonic()<deadline:
  lines=(out/'consumer.jsonl').read_text().splitlines()
  frames=[json.loads(l) for l in lines]
  matched=[f for f in frames if f['run_id']==result['RunID'] and f['context']==context]
  if matched:break
  time.sleep(.05)
 else:raise RuntimeError('missing consumer End')
 row={'arm':out.name,'label':label,'result':result,'wire':matched[-1],'cmd':cmd};rows.append(row);save();print(out.name,label,result.get('ParsedFiles'),matched[-1]['normalized_hash'],flush=True);return row
try:
 for arm in ['baseline','candidate']:
  out=ROOT/arm;out.mkdir();F.write_bytes(original)
  if state.exists():shutil.rmtree(state)
  conf=out/'nats.conf';conf.write_text(f'listen: 127.0.0.1:{port}\nmax_payload: 1048576\njetstream {{ store_dir: "{out / "nats-store"}" }}\n')
  n=o=None
  try:
   with (out/'nats.log').open('w') as log:n=sp.Popen([NATS,'-c',str(conf)],stdout=log,stderr=log,start_new_session=True)
   deadline=time.monotonic()+20
   while True:
    try:
     with socket.create_connection(('127.0.0.1',port),timeout=.2):break
    except OSError:
     assert n.poll() is None
     if time.monotonic()>deadline:raise
     time.sleep(.05)
   with (out/'observer.log').open('w') as log:o=sp.Popen([OBS,url,str(out/'consumer.jsonl')],stdout=log,stderr=log,start_new_session=True)
   await_ready(o,out/'observer.log')
   seed=run(BASE,'analyze',state,ctx,'seed',out)
   shutil.copytree(state,out/'pre-state')
   st=json.loads((state/'state.json').read_text());states.append(st)
   if len(states)==2:
    a=dict(states[0]);b=dict(states[1]); ids=[a.pop('last_run_id'),b.pop('last_run_id')]
    differences=[k for k in sorted(set(a)|set(b)) if a.get(k)!=b.get(k)]
    (ROOT/'pre-state-comparison.json').write_text(json.dumps({'allowed_last_run_ids':ids,'other_differing_top_fields':differences,'equal_except_last_run_id':not differences},indent=2))
    assert not differences,('pre-state mismatch',differences)
   F.write_bytes(body)
   delta=run(pins['arms'][arm]['binary'],'delta',state,ctx,'body',out,True)
   cold=run(BASE,'analyze',out/'cold-state',ctx+'-cold','cold-body',out)
   assert delta['wire']['normalized_hash']==cold['wire']['normalized_hash'],'cold delta mismatch'
  finally:stop(o);stop(n)
 assert rows[1]['wire']['normalized_hash']==rows[4]['wire']['normalized_hash']
 (ROOT/'PASS').write_text('State equal except LastRunID; both deltas equal fresh cold and each other. Diagnostic only.\n')
finally:F.write_bytes(original);save()
