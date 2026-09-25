from validate_results import validate
import argparse,importlib.util,pathlib,subprocess,os,json,time,threading,hashlib
def competitors():
    import re
    raw=subprocess.check_output(['ps','-axo','pid=,ppid=,command='],text=True).splitlines()
    rows=[]
    for line in raw:
        fields=line.strip().split(None,2)
        if len(fields)==3: rows.append((int(fields[0]),int(fields[1]),fields[2]))
    owned={os.getpid()}
    while True:
        more={pid for pid,parent,_ in rows if parent in owned}
        if more <= owned: break
        owned |= more
    hits=[]
    for pid,parent,command in rows:
        if pid in owned: continue
        executable=command.split(None,1)[0]
        name=pathlib.Path(executable).name
        go=(name=='go' and re.search(r'\s(test|build|install|vet)\s', command))
        test=(name.endswith('.test') and '-test.' in command)
        tool=('/pkg/tool/' in executable and name in ('compile','link','asm','cgo'))
        runtime=name.startswith('enola') or name=='benchobserver'
        history=(name.startswith('python') and any(x in command for x in ('run-history-bounded.py','hash-history-oracles.py','cleanup-history-live.py')))
        if go or test or tool or runtime or history:
            hits.append(f'{pid} {command}')
    return hits

p=argparse.ArgumentParser();p.add_argument('--arm',choices=['baseline','candidate'],required=True);p.add_argument('--repeat',type=int,required=True);p.add_argument('--quiet-message-id');p.add_argument('--correctness-only',action='store_true');p.add_argument('--preflight-only',action='store_true');a=p.parse_args()
if not a.preflight_only and not a.correctness_only and not a.quiet_message_id: p.error('timing requires --quiet-message-id')
b=pathlib.Path(__file__).resolve().parent
manifest=json.loads((b/'pins.json').read_text())
def require_pin(path, expected):
 if not expected or hashlib.sha256(pathlib.Path(path).read_bytes()).hexdigest()!=expected:
  raise SystemExit('pin mismatch: '+str(path))
for path,expected in manifest['files'].items(): require_pin(path,expected)
binary=pathlib.Path(manifest['binaries'][a.arm]['path'])
require_pin(binary,manifest['binaries'][a.arm]['sha256'])
source=pathlib.Path(manifest['product_path'])
if subprocess.check_output(['git','rev-parse','HEAD'],cwd=source,text=True).strip()!=manifest['product_sha']:
 raise SystemExit('Product revision mismatch')
if subprocess.check_output(['git','status','--porcelain','--untracked-files=all'],cwd=source,text=True).strip():
 raise SystemExit('Product source is dirty')
if a.preflight_only:
 print('pins and clean Product verified; no benchmark executed');raise SystemExit(0)
work=b/f'{"correctness-" if a.correctness_only else ""}{a.arm}-{a.repeat}';assert not work.exists()
if not a.correctness_only and competitors():raise SystemExit('competing workload')
spec=importlib.util.spec_from_file_location('watch','/tmp/enola-stage14-watch-pairs/watch.py');w=importlib.util.module_from_spec(spec);spec.loader.exec_module(w);eff=w.eff
work.mkdir();repo=work/'product'
subprocess.run(['cp','-cR',str(source),str(repo)],check=True)
port=eff.free_port();conf=eff.write_nats_conf(work,port);url=f'nats://127.0.0.1:{port}'
observer=pathlib.Path('/tmp/enola-stage9-product-watch-control/bin/benchobserver')
stop=threading.Event();samples=[]
def monitor():
 while not stop.wait(1):
  hits=competitors()
  if hits:samples.append(hits)
thread=threading.Thread(target=monitor);server=None;obs=None;result=None
try:
 with (work/'nats.log').open('w') as log:server=subprocess.Popen([str(eff.NATS_SERVER),'-c',str(conf)],stdout=log,stderr=subprocess.STDOUT,start_new_session=True)
 eff.wait_port('127.0.0.1',port)
 env=os.environ.copy();env.pop('ENOLA_GRAPH_PROFILE',None);env['OBSERVER_READY_FILE']=str(work/'ready');env['OBSERVER_LIFECYCLE_FILE']=str(work/'lifecycle.jsonl')
 with (work/'observer.stderr').open('w') as log:obs=subprocess.Popen([str(observer),url,str(work/'consumer.jsonl')],stdout=log,stderr=log,env=env,start_new_session=True)
 eff.wait_observer_ready(work/'ready',work/'observer.stderr',obs)
 argv=['python3','-B',str(b/'cli.py'),'--binary',str(binary),'--root',str(work),'--source',str(repo),'--nats',url,'--observer',str(observer),'--repeat','1','--profile','ts','--scope-config',str(w.HERE/'product-graph-scope.yaml')]
 thread.start()
 with (work/'suite.log').open('w') as log:result=subprocess.run(argv,stdout=log,stderr=subprocess.STDOUT,env=env)
finally:
 stop.set()
 if thread.ident:thread.join()
 eff.stop_process(obs);eff.stop_process(server)
 validated=False;validation_error=None
 try:
  validated=validate(json.loads((work/'metrics.json').read_text()),json.loads((work/'checks.json').read_text()))
 except Exception as e: validation_error=str(e)
 receipt={'validation_error':validation_error,'all_checks_validated':validated,'arm':a.arm,'repeat':a.repeat,'quiet_ack_message_id':a.quiet_message_id,'exit':result.returncode if result else None,'competing_samples':samples,'correctness_only':a.correctness_only,'run_succeeded':bool(result and result.returncode==0 and validated),'timing_eligible':bool(not a.correctness_only and result and result.returncode==0 and validated and not samples),'binary_sha256':hashlib.sha256(binary.read_bytes()).hexdigest()}
 (work/'pair-receipt.json').write_text(json.dumps(receipt,indent=2)+'\n');print(json.dumps(receipt),flush=True)
if not receipt['run_succeeded'] or (not a.correctness_only and not receipt['timing_eligible']):raise SystemExit('CLI run not accepted; inspect evidence')
