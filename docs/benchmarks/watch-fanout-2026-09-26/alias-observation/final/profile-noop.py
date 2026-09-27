from pathlib import Path
import subprocess,json,os,hashlib,socket,time
r=Path(__file__).resolve().parent;p=r/'cli-pairs/correctness-candidate-1';d=r/'noop-profile';d.mkdir(exist_ok=False)
ident=json.loads((p/'cold-original/identity.json').read_text());url=ident['sink_id'].split('|')[1];port=int(url.rsplit(':',1)[1])
with socket.socket() as s:s.bind(('127.0.0.1',port))
for src,dst in [(p/'cold-original',d/'state'),(p/'nats-store',d/'nats-store')]:subprocess.run(['cp','-cR',str(src),str(dst)],check=True)
conf=(p/'nats.conf').read_text().replace(str((p/'nats-store').resolve()),str((d/'nats-store').resolve())).replace(str(p/'nats-store'),str(d/'nats-store'));(d/'nats.conf').write_text(conf)
server=None
try:
 with (d/'nats.log').open('w') as log:server=subprocess.Popen(['/tmp/enola-toolchain/bin/nats-server','-c',str(d/'nats.conf')],stdout=log,stderr=log,start_new_session=True)
 for _ in range(300):
  if server.poll() is not None:raise RuntimeError('broker exited')
  try:
   with socket.create_connection(('127.0.0.1',port),timeout=.2):break
  except OSError:time.sleep(.05)
 else:raise RuntimeError('broker not ready')
 observer='/tmp/enola-stage9-product-watch-control/bin/benchobserver'
 before=json.loads(subprocess.check_output([observer,url,'--info']));digest=lambda:hashlib.sha256((d/'state/state.json').read_bytes()).hexdigest();state_before=digest()
 argv=[str(r/'enola-experimental'),'graph','delta','--authoritative-scope','--changed-owner-scope','--max-begin-bytes','8000000','--repo-id',ident['repo_id'],'--summary-json','--nats',url,'--state-dir',str(d/'state'),'--context',ident['context_id'],str(p/'config.yaml')]
 env=dict(os.environ,ENOLA_GRAPH_PROFILE='1')
 with (d/'stdout.json').open('w') as out,(d/'trace.log').open('w') as err:proc=subprocess.run(argv,stdout=out,stderr=err,env=env)
 assert proc.returncode==0
 after=json.loads(subprocess.check_output([observer,url,'--info']));trace=(d/'trace.log').read_text();assert 'ts_disc_alias_roots' in trace
 result=json.loads((d/'stdout.json').read_text());assert before==after and state_before==digest()
 (d/'receipt.json').write_text(json.dumps({'exit':proc.returncode,'purpose':'shared-host traced fresh no-op path proof; not timing acceptance','argv':argv,'state_unchanged':True,'broker_info_unchanged':True,'alias_phase_observed':True,'result':result,'binary_sha256':hashlib.sha256((r/'enola-experimental').read_bytes()).hexdigest()},indent=2)+'\n')
 print('PASS: fresh no-op observes ts_disc_alias_roots, unchanged state and broker info')
finally:
 if server is not None:
  server.terminate()
  try:server.wait(timeout=10)
  except subprocess.TimeoutExpired:server.kill();server.wait()
