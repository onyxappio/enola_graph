from validate_results import validate
import argparse,importlib.util,pathlib,re,subprocess,os,json,time,threading,hashlib,sys
sys.path.insert(0,str(pathlib.Path(__file__).resolve().parent.parent))
import background_gate as bg
from timing_gate import validate_hold,load_observation
PS_FORMAT='pid=,ppid=,%cpu=,rss=,command='
def ps_scan():
    """One bounded ps scan, shared by the competitor check and the host-load log.

    The columns are the FSM ones root named (pid, ppid, %cpu, rss, command), and
    this is the only process either path spawns: the host log is a second reading
    of the same scan, not a second sampler.
    """
    # Bounded so a stuck ps cannot hang the monitor's final join.
    raw=subprocess.check_output(['ps','-axo',PS_FORMAT],text=True,timeout=2)
    rows=[]
    for line in raw.splitlines():
        fields=line.strip().split(None,4)
        if len(fields)!=5: continue
        try: rows.append((int(fields[0]),int(fields[1]),float(fields[2]),int(fields[3]),fields[4]))
        except ValueError: continue
    return raw,rows

def competitors(rows=None):
    """Recognized competing workloads only.

    This list is unchanged: Go builds and tests, toolchain processes, Enola
    runtimes, the observer and the named history scripts. An empty result means
    no recognized competitor was seen, and nothing about the rest of the machine;
    that is what the host-load log is for.
    """
    if rows is None: _,rows=ps_scan()
    owned={os.getpid()}
    while True:
        more={pid for pid,parent,_,_,_ in rows if parent in owned}
        if more <= owned: break
        owned |= more
    hits=[]
    for pid,parent,cpu,rss,command in rows:
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

p=argparse.ArgumentParser();p.add_argument('--arm',choices=['baseline','control','candidate'],required=True);p.add_argument('--repeat',type=int,required=True);p.add_argument('--quiet-message-id');p.add_argument('--background-hold',type=pathlib.Path);p.add_argument('--correctness-only',action='store_true');p.add_argument('--preflight-only',action='store_true');a=p.parse_args()
if not a.preflight_only and not a.correctness_only and not a.quiet_message_id: p.error('timing requires --quiet-message-id')
b=pathlib.Path(__file__).resolve().parent
manifest=json.loads((b/'pins.json').read_text())
for key,expected in manifest.get('runtime_environment',{}).items():
 if os.environ.get(key)!=expected:raise SystemExit('runtime environment mismatch: '+key)
background_hold=None; roster=None; limits=None; background_samples=[]; background_vectors=[]; background_errors=[]
if a.background_hold:
 background_hold=json.loads(a.background_hold.read_text());validate_hold(background_hold)
 if background_hold.get('mode')!='controlled-background':raise SystemExit('wrong background hold mode')
 if background_hold['worker_message']!=a.quiet_message_id:raise SystemExit('hold identity mismatch')
 roster,limits=load_observation(background_hold)
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
# Full-profile frozen Begin exceeds the older TS-only 1 MiB envelope.
conf.write_text(conf.read_text().replace('max_payload: 1048576', 'max_payload: 8388608').replace('# Frozen v2 Product Begin is ~831KiB. Keep JetStream payload at 1MiB.', '# Full-profile Product: 8 MiB payload limit shared by every arm.'))
observer=pathlib.Path('/tmp/enola-stage9-product-watch-control/bin/benchobserver')
stop=threading.Event();monitor_ready=threading.Event();samples=[];host_log=work/'host-load.jsonl';host_written=[];host_errors=[]
def legacy_monitor():
 """Competitor check every second, whole-host ps sample every second tick.

 The host log keeps every process ps reported, not only the recognized ones, so
 unrelated CPU work - a browser, an indexer - is visible to a reader instead of
 being assumed absent. It records the conditions; it does not prove them quiet.
 Both arms run this identical loop.
 """
 tick=0
 with host_log.open('w') as out:
  while not stop.wait(1):
   tick+=1
   try:
    raw,rows=ps_scan()
   except Exception as e:
    # A failed scan is recorded, never silently treated as an idle host.
    err={'time':time.time(),'error':str(e)};host_errors.append(err)
    out.write(json.dumps(err)+'\n');out.flush();continue
   if tick%2==0:
    now=time.time()
    out.write(json.dumps({'time':now,'loadavg':os.getloadavg(),'processes':raw})+'\n');out.flush()
    host_written.append(now)
   hits=competitors(rows)
   if hits:samples.append(hits)
def monitor():
 if background_hold is None:
  return legacy_monitor()
 previous=None;deadline=time.monotonic()
 with (work/'background-load.jsonl').open('x') as out:
  while True:
   try:
    snapshot=bg.sample(roster);background_samples.append(snapshot)
    out.write(json.dumps(snapshot)+'\n');out.flush()
    background_errors.extend(bg.check_processes(snapshot,roster,os.getpid()))
    host_written.append(snapshot['time'])
    if previous is not None:
     vector=bg.rates(previous,snapshot,roster);background_vectors.append(vector)
     background_errors.extend('threshold: '+k for k in bg.violations(vector,limits))
    previous=snapshot
   except Exception as e:
    background_errors.append(repr(e));out.write(json.dumps({'error':repr(e),'time':time.time()})+'\n');out.flush()
   monitor_ready.set()
   if stop.is_set():break
   deadline+=1
   stop.wait(max(0,deadline-time.monotonic()))
thread=threading.Thread(target=monitor);server=None;obs=None;result=None
try:
 with (work/'nats.log').open('w') as log:server=subprocess.Popen([str(eff.NATS_SERVER),'-c',str(conf)],stdout=log,stderr=subprocess.STDOUT,start_new_session=True)
 eff.wait_port('127.0.0.1',port)
 env=os.environ.copy();env.pop('ENOLA_GRAPH_PROFILE',None);env['OBSERVER_READY_FILE']=str(work/'ready');env['OBSERVER_LIFECYCLE_FILE']=str(work/'lifecycle.jsonl')
 with (work/'observer.stderr').open('w') as log:obs=subprocess.Popen([str(observer),url,str(work/'consumer.jsonl')],stdout=log,stderr=log,env=env,start_new_session=True)
 eff.wait_observer_ready(work/'ready',work/'observer.stderr',obs)
 argv=['python3','-B',str(b/'cli.py'),'--binary',str(binary),'--root',str(work),'--source',str(repo),'--nats',url,'--observer',str(observer),'--repeat','1','--profile','full']
 argv += ['--scope-config', str(b/'test-inclusive-scope.yaml')]
 if a.arm != 'baseline': argv.append('--changed-owner-scope')
 thread.start()
 if background_hold and not monitor_ready.wait(10):raise RuntimeError('background monitor startup timeout')
 env['ENOLA_GRAPH_PROFILE']='1'
 with (work/'suite.log').open('w') as log:result=subprocess.run(argv,stdout=log,stderr=subprocess.STDOUT,env=env)
finally:
 stop.set()
 if thread.ident:thread.join()
 eff.stop_process(obs);eff.stop_process(server)
 validated=False;validation_error=None
 try:
  validated=validate(json.loads((work/'metrics.json').read_text()),json.loads((work/'checks.json').read_text()))
 except Exception as e: validation_error=str(e)
 background_ok=bool(background_hold is None or (len(background_vectors)>0 and not background_errors))
 receipt={'background_owner_pid':os.getpid(),'background_mode':background_hold.get('mode') if background_hold else None,'background_errors':background_errors,'background_vectors':background_vectors,'background_evidence':str(work/'background-load.jsonl') if background_hold else None,'validation_error':validation_error,'all_checks_validated':validated,'arm':a.arm,'repeat':a.repeat,'quiet_ack_message_id':a.quiet_message_id,'exit':result.returncode if result else None,'competing_samples':samples,'host_load_log':str(host_log),'host_load_samples':len(host_written),'host_sample_errors':host_errors,'correctness_only':a.correctness_only,'run_succeeded':bool(result and result.returncode==0 and validated),'timing_eligible':bool(not a.correctness_only and result and result.returncode==0 and validated and not samples and not host_errors and len(host_written)>0 and background_ok),'binary_sha256':hashlib.sha256(binary.read_bytes()).hexdigest()}
 (work/'pair-receipt.json').write_text(json.dumps(receipt,indent=2)+'\n');print(json.dumps(receipt),flush=True)
if not receipt['run_succeeded'] or (not a.correctness_only and not receipt['timing_eligible']):raise SystemExit('CLI run not accepted; inspect evidence')
