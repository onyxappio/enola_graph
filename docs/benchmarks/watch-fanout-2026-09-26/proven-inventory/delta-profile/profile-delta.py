"""Isolated diagnostic phase trace; never acceptance timing."""
import hashlib,importlib.util,json,os,pathlib,subprocess,time
b=pathlib.Path('/tmp/enola-proven-inventory/delta-profile');b.mkdir(exist_ok=False)
pins=json.loads(pathlib.Path('/tmp/enola-proven-inventory/cli-pairs/pins.json').read_text())
binary=pathlib.Path(pins['binaries']['candidate']['path']);assert hashlib.sha256(binary.read_bytes()).hexdigest()==pins['binaries']['candidate']['sha256']
source=pathlib.Path(pins['product_path']);assert subprocess.check_output(['git','rev-parse','HEAD'],cwd=source,text=True).strip()==pins['product_sha'];assert not subprocess.check_output(['git','status','--porcelain'],cwd=source,text=True).strip()
repo=b/'product';subprocess.run(['cp','-cR',str(source),str(repo)],check=True)
spec=importlib.util.spec_from_file_location('watch','/tmp/enola-stage14-watch-pairs/watch.py');w=importlib.util.module_from_spec(spec);spec.loader.exec_module(w);eff=w.eff
port=eff.free_port();conf=eff.write_nats_conf(b,port);conf.write_text(conf.read_text().replace('max_payload: 1048576','max_payload: 8388608'));url=f'nats://127.0.0.1:{port}'
observer=pathlib.Path('/tmp/enola-stage9-product-watch-control/bin/benchobserver');cfg=b/'config.yaml';cfg.write_text('repo: '+str(repo)+'\n'+pathlib.Path('/tmp/enola-proven-inventory/cli-pairs/test-inclusive-scope.yaml').read_text())
server=obs=None;rows=[]
edit=repo/'packages/crypto/src/password.ts'
original=edit.read_text()
body=original.replace('return email.trim().toLowerCase();', "return email.normalize('NFKC').trim().toLowerCase();")
assert body!=original
structural=body+'\nexport function enolaBenchmarkEmailKey(email: string) { return normalizeEmail(email); }\n'

def stream():return json.loads(subprocess.check_output([str(observer),url,'--info'],text=True))['last_seq']
try:
 with (b/'nats.log').open('w') as log:server=subprocess.Popen([str(eff.NATS_SERVER),'-c',str(conf)],stdout=log,stderr=log,start_new_session=True)
 eff.wait_port('127.0.0.1',port)
 env=dict(os.environ);env['OBSERVER_READY_FILE']=str(b/'ready');env['OBSERVER_LIFECYCLE_FILE']=str(b/'lifecycle.jsonl')
 with (b/'observer.log').open('w') as log:obs=subprocess.Popen([str(observer),url,str(b/'consumer.jsonl')],stdout=log,stderr=log,env=env,start_new_session=True)
 eff.wait_observer_ready(b/'ready',b/'observer.log',obs)
 env['ENOLA_GRAPH_PROFILE']='1'
 for name in ['initial','body','structural']:
  if name!='initial':edit.write_text(body if name=='body' else structural)
  state=b/'state/state.json';before=hashlib.sha256(state.read_bytes()).hexdigest() if state.exists() else None;seq=stream()
  argv=[str(binary),'graph','analyze' if name=='initial' else 'delta','--authoritative-scope','--changed-owner-scope','--summary-json','--max-begin-bytes','8000000','--repo-id','stage15-cli-product','--state-dir',str(b/'state'),'--context','fresh-noop-profile','--nats',url,str(cfg)]
  with (b/(name+'.out')).open('w') as out,(b/(name+'.log')).open('w') as err:subprocess.run(argv,cwd=repo,env=env,stdout=out,stderr=err,check=True,timeout=180)
  result=json.loads((b/(name+'.out')).read_text())[0];after=hashlib.sha256(state.read_bytes()).hexdigest();events=stream()-seq
  if name.startswith('noop'):assert result['ParsedFiles']==0 and result['BaseGeneration']==result['TargetGeneration'] and before==after and events==0
  rows.append({'name':name,'result':result,'events':events,'state_before':before,'state_after':after});print(name,'parsed',result['ParsedFiles'],'events',events,flush=True)
finally:
 edit.write_text(original)
 eff.stop_process(obs);eff.stop_process(server)
 (b/'receipt.json').write_text(json.dumps({'purpose':'diagnostic phases only, shared host, no acceptance timing','binary_sha256':pins['binaries']['candidate']['sha256'],'product_revision':pins['product_sha'],'rows':rows},indent=2)+'\n')
