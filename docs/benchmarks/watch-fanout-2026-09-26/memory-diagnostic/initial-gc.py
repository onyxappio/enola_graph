import pathlib,subprocess,importlib.util,json,os,time,hashlib
b=pathlib.Path(__file__).resolve().parent
pins=json.loads(pathlib.Path('/tmp/enola-fanout-nats-acceptance/cli-pairs/pins.json').read_text())
spec=importlib.util.spec_from_file_location('watch','/tmp/enola-stage14-watch-pairs/watch.py');w=importlib.util.module_from_spec(spec);spec.loader.exec_module(w);eff=w.eff
source=pathlib.Path(pins['product_path']);assert not subprocess.check_output(['git','status','--porcelain'],cwd=source,text=True).strip()
observer='/tmp/enola-stage9-product-watch-control/bin/benchobserver'
for arm in ['control','candidate']:
 root=b/arm;root.mkdir();binary=pins['binaries'][arm];assert hashlib.sha256(pathlib.Path(binary['path']).read_bytes()).hexdigest()==binary['sha256']
 cfg=root/'config.yaml';cfg.write_text('repo: '+str(source)+'\n')
 port=eff.free_port();conf=eff.write_nats_conf(root,port);conf.write_text(conf.read_text().replace('max_payload: 1048576','max_payload: 8388608'));url=f'nats://127.0.0.1:{port}'
 server=obs=None
 try:
  with (root/'nats.log').open('w') as log:server=subprocess.Popen([str(eff.NATS_SERVER),'-c',str(conf)],stdout=log,stderr=log,start_new_session=True)
  eff.wait_port('127.0.0.1',port)
  env=os.environ.copy();env['OBSERVER_READY_FILE']=str(root/'ready');env['OBSERVER_LIFECYCLE_FILE']=str(root/'lifecycle.jsonl')
  with (root/'observer.log').open('w') as log:obs=subprocess.Popen([observer,url,str(root/'consumer.jsonl')],stdout=log,stderr=log,env=env,start_new_session=True)
  eff.wait_observer_ready(root/'ready',root/'observer.log',obs)
  env['GODEBUG']='gctrace=1';env['ENOLA_GRAPH_PROFILE']='1'
  cmd=['/usr/bin/time','-l',binary['path'],'graph','analyze','--authoritative-scope','--changed-owner-scope','--max-begin-bytes','8000000','--repo-id','stage15-cli-product','--summary-json','--nats',url,'--state-dir',str(root/'state'),'--context','memory-'+arm,str(cfg)]
  start=time.monotonic()
  with (root/'summary.json').open('w') as out,(root/'initial.log').open('w') as log:r=subprocess.run(cmd,stdout=out,stderr=log,env=env,timeout=180)
  receipt=dict(arm=arm,exit=r.returncode,seconds=time.monotonic()-start,binary=binary,diagnostic=True,env={'GODEBUG':env['GODEBUG'],'ENOLA_GRAPH_PROFILE':'1'},argv=cmd)
  (root/'receipt.json').write_text(json.dumps(receipt,indent=2)+'\n');print(arm,r.returncode,flush=True);assert r.returncode==0
 finally:
  eff.stop_process(obs);eff.stop_process(server)
