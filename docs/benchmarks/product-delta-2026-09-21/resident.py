"""Sequential candidate benchmark; source edits only in the dedicated Product clone."""
from pathlib import Path
from harness_support import restore_all, wait_process
import argparse,hashlib,json,re,shutil,subprocess,time,threading,os,signal
p=argparse.ArgumentParser();p.add_argument('--binary',required=True);p.add_argument('--resident',required=True);p.add_argument('--initial-hash',default='');p.add_argument('--probe-ignored',action='store_true');p.add_argument('--source-mode',choices=['watch','queue'],default='watch');p.add_argument('--root',required=True);p.add_argument('--source',default='/tmp/enola-product-benchmark-source');p.add_argument('--nats',required=True);p.add_argument('--observer',default='/tmp/enola-product-observer');p.add_argument('--repeat',type=int,default=3);p.add_argument('--profile',choices=['full','ts'],default='full');p.add_argument('--scope-config',help='YAML fragment with graph_inputs/ignore/extractor settings, without repo');a=p.parse_args()
if a.repeat < 1:p.error('--repeat must be positive')
root=Path(a.root).resolve();root.mkdir(parents=True,exist_ok=True);repo=Path(a.source).absolute()
if any(root.glob('state*')) or any((root/n).exists() for n in ['metrics.json','provenance.json','cold-target','cold-original','cold-body','cold-structural']):raise RuntimeError('Output root contains prior run artifacts; use a fresh root')
consumer_file=root/'consumer.jsonl'
if consumer_file.exists() and consumer_file.stat().st_size:raise RuntimeError('Consumer output must be fresh')
def interrupted(signum,frame):raise KeyboardInterrupt('Signal '+str(signum))
signal.signal(signal.SIGTERM,interrupted);signal.signal(signal.SIGINT,interrupted)
def stop_process(child):
 if child is None:return
 try:
  if child.poll() is None:
   try:os.killpg(child.pid,signal.SIGTERM)
   except ProcessLookupError:pass
   try:child.wait(timeout=10)
   except subprocess.TimeoutExpired:
    try:os.killpg(child.pid,signal.SIGKILL)
    except ProcessLookupError:pass
    child.wait()
 finally:
  for channel in [child.stdin,child.stdout,child.stderr]:
   if channel:
    try:channel.close()
    except OSError:pass
if subprocess.check_output(['git','status','--porcelain','--untracked-files=no'],cwd=repo).strip():raise RuntimeError('Product clone has tracked edits; refusing to overwrite')
f=repo/'packages/crypto/src/password.ts';original=f.read_text();body=original.replace('return email.trim().toLowerCase();',"return email.normalize('NFKC').trim().toLowerCase();");assert body!=original
structural=body+'\nexport function enolaBenchmarkEmailKey(email: string) { return normalizeEmail(email); }\n'
# External config parents are watched for replacements. Keep this parent free
# of metrics/log writes so the raw native-event quiet probe measures an idle host.
config_dir=root/'config';config_dir.mkdir()
cfg=config_dir/'config.yaml';cfg.write_text('repo: '+str(repo)+'\n'+('extractors: [typescript]\nexplainers: []\nrenderers: []\n' if a.profile=='ts' else ''))
if a.scope_config:
 scope=Path(a.scope_config).read_text()
 if re.search(r'^repo(?:s)?\s*:',scope,re.M):raise RuntimeError('scope-config must not set repository')
 with cfg.open('a') as stream:stream.write('\n'+scope)
rows=[];checks=[];hashes={};prefix=root.name
observer_digest=hashlib.sha256(Path(a.observer).read_bytes()).hexdigest()
(root/'provenance.json').write_text(json.dumps({'product_sha':subprocess.check_output(['git','rev-parse','HEAD'],cwd=repo).decode().strip(),'observer':a.observer,'observer_sha256':observer_digest,'binary':a.binary,'binary_sha256':hashlib.sha256(Path(a.binary).read_bytes()).hexdigest(),'resident_binary':a.resident,'resident_sha256':hashlib.sha256(Path(a.resident).read_bytes()).hexdigest(),'source_mode':a.source_mode,'profile':a.profile,'config':cfg.read_text(),'started_ns':time.time_ns(),'original_file_sha256':hashlib.sha256(original.encode()).hexdigest()},indent=2))
def save():
 (root/'metrics.json').write_text(json.dumps(rows,indent=2));(root/'checks.json').write_text(json.dumps(checks,indent=2))
def streaminfo():return json.loads(subprocess.check_output([a.observer,a.nats,'--info']))
def wire(ctx,start,run_id):
 until=time.monotonic()+120
 while time.monotonic()<until:
  for l in (root/'consumer.jsonl').read_text().splitlines():
   try:v=json.loads(l)
   except json.JSONDecodeError:continue
   if v['context']==ctx and v['first_ns']>=start and v['run_id']==run_id:
    if not re.fullmatch(r'[0-9a-f]{64}',v.get('normalized_hash','')):raise RuntimeError('Invalid consumer graph digest')
    return v
  time.sleep(.05)
 raise RuntimeError('No completed consumer frame for '+ctx)
def diskbytes(path):
 total=spool=0
 if path:
  for f in path.rglob('*'):
   try:
    if f.is_file():
     size=f.stat().st_size;total+=size
     if not f.name.startswith(('state.json','pending-state.json','identity.json','session.lock')):spool+=size
   except FileNotFoundError:pass
 return total,spool
def run(label,cmd,ctx=None,noop=False):
 print('START',label,flush=True);before=streaminfo() if noop else None;t=time.monotonic();ns=time.time_ns()
 statepath=Path(cmd[cmd.index('--state-dir')+1]) if '--state-dir' in cmd else None
 peak=[0,0];stop=threading.Event()
 def sample():
  while not stop.is_set():
   sizes=diskbytes(statepath)
   for i,n in enumerate(sizes):peak[i]=max(peak[i],n)
   stop.wait(.1)
 sampler=threading.Thread(target=sample,daemon=True);sampler.start()
 child=None
 try:
  with (root/(label+'.out')).open('w') as out,(root/(label+'.log')).open('w') as err:
   child=subprocess.Popen(['/usr/bin/time','-l',*cmd],cwd=root,stdout=out,stderr=err,start_new_session=True)
   wait_process(child,600)
   completed=time.monotonic()
 finally:
  try:stop_process(child)
  finally:stop.set();sampler.join()
 proc=child
 m={'state_peak_sampled_bytes':peak[0],'spool_peak_sampled_bytes':peak[1],'state_final_bytes':diskbytes(statepath)[0],'label':label,'start_ns':ns,'seconds':completed-t,'exit':proc.returncode};log=(root/(label+'.log')).read_text()
 for k,pat in [('process_real_s',r'([\d.]+)\s+real'),('rss_bytes',r'(\d+)\s+maximum resident set size'),('user_s',r'([\d.]+) user'),('sys_s',r'([\d.]+) sys')]:
  z=re.search(pat,log);m[k]=float(z[1]) if z else None
 rows.append(m);save()
 if proc.returncode:raise RuntimeError(label+' failed: '+log[-1800:])
 if ctx:
  result_rows=json.loads((root/(label+'.out')).read_text())
  if len(result_rows)!=1:raise RuntimeError('Expected one repository summary')
  m['result']=result_rows[0]
  m['result'].pop('Facts',None)
  m['fallbacks']=re.findall(r'\[graph\] fallback ([^\n]+)',log)
  if m['result']['BaseGeneration']!=m['result']['TargetGeneration']:
   w=wire(ctx,ns,m['result']['RunID']);m['wire']=w;hashes[ctx]=w['normalized_hash']
   for k in ['first_ns','first_batch_ns','broker_end_ns','consumer_end_ns']:
    if k in w:m[k.replace('_ns','_s')]=(w[k]-ns)/1e9
  m['graph_hash']=hashes.get(ctx)
  if noop:
   after=streaminfo();m['noop_wire_messages']=after['last_seq']-before['last_seq'];checks.append({'check':label+' no-op','pass':m['result']['ParsedFiles']==0 and m['result']['BaseGeneration']==m['result']['TargetGeneration'] and m['noop_wire_messages']==0})
 else:
  z=re.search(r'Facts:\s+(\d+)',log);m['facts']=int(z[1]) if z else None
 save();print('DONE',label,round(m['seconds'],3),m.get('result'),flush=True);return m
def ng(label,mode,state,ctx,noop=False):return run(label,[a.binary,'graph',mode,'--summary-json','--nats',a.nats,'--state-dir',str(root/state),'--context',ctx,str(cfg)],ctx,noop)

import select
proc=None
ignored_backups={}
if a.probe_ignored:
 lock=repo/'pnpm-lock.yaml'
 if lock.is_file():ignored_backups[lock]=lock.read_bytes()
 media=repo/'apps/mobile/assets/icon.png'
 if not lock.is_file() or not media.is_file():raise RuntimeError('Ignored probes require Product pnpm-lock.yaml and assets/icon.png')
 ignored_backups[media]=media.read_bytes()
def receive(timeout=180):
 if not select.select([proc.stdout],[],[],timeout)[0]:raise RuntimeError('resident response timeout')
 line=proc.stdout.readline()
 if not line:raise RuntimeError('resident exited before response; see driver log')
 return json.loads(line)
def online(label,ctx,request=None,newtext=None,initial=False):
 before=driver_before if initial else streaminfo();ns=time.time_ns();t=time.monotonic()
 if newtext is not None:f.write_text(newtext)
 if request is not None:
  proc.stdin.write(json.dumps(request)+'\n');proc.stdin.flush()
 event=receive();res=event['result'];m={'label':label,'start_ns':ns,'seconds':time.monotonic()-t,'driver':event,'result':res}
 results=[res,*(event.get('coverage_catchup') or [])]
 m['aggregate_parsed']=sum(x['ParsedFiles'] for x in results)
 m['aggregate_work']={k:sum(x['Work'][k] for x in results) for k in res['Work']}
 m['aggregate_stats']={k:sum(x['Stats'][k] for x in results) for k in res['Stats']}
 m['fallback_reasons']=[x['FallbackReason'] for x in results if x.get('FallbackReason')]
 m['reconciliation_count']=sum(bool(x.get('Reconciled')) for x in results)
 if initial:
  if res['BaseGeneration']!=0 or res['TargetGeneration']!=1:raise RuntimeError('Resident initial is not fresh')
  m['seconds']=time.monotonic()-driver_started;m['start_ns']=driver_start_ns;ns=driver_start_ns
 if res['BaseGeneration']!=res['TargetGeneration']:
  w=wire(ctx,ns,m['result']['RunID']);m['wire']=w;hashes[ctx]=w['normalized_hash']
  for k in ['first_ns','first_batch_ns','broker_end_ns','consumer_end_ns']:
   if k in w:m[k.replace('_ns','_s')]=(w[k]-ns)/1e9
 m['graph_hash']=hashes.get(ctx);m['wire_messages']=streaminfo()['last_seq']-before['last_seq']
 if '-noop' in label:
  work=res.get('Work',{})
  checks.append({'check':label+' zero work/no events','pass':bool(work) and {'InventoryScans','HashedFiles','Checkpoints','PublishedEvents','FactAssemblies'}.issubset(work) and m['wire_messages']==0 and all(x['ParsedFiles']==0 and x['BaseGeneration']==res['BaseGeneration']==x['TargetGeneration'] and bool(x['Work']) and all(v==0 for v in x['Work'].values()) for x in results)})
 if request and request.get('observe_after'):
  proof=event['native_observed']
  checks.append({'check':label+' native event delivered','pass':all(proof.get(path,0)>before for path,before in request['observe_after'].items())})
 if request and request.get('paths'):
  batch=event['observed_batch'];expected=set(request['paths']);observed={os.path.relpath(x,repo) if os.path.isabs(x) else x for x in batch['Paths']}
  checks.append({'check':label+' real observed input and fast path','pass':expected.issubset(observed) and batch['Covered'] and not batch['Reconcile'] and not res['Reconciled']})
 rows.append(m);save();print('DONE',label,round(m['seconds'],6),'parsed',m['aggregate_parsed'],'reconciliations',m['reconciliation_count'],'fallbacks',m['fallback_reasons'],flush=True);return m
body_runs=[];structural_runs=[];initial_runs=[];body_ref=None;structural_ref=None
try:
 for i in range(1,a.repeat+1):
  f.write_text(original)
  for p,b in ignored_backups.items():p.write_bytes(b)
  ctx=prefix+f'-main{i}';state=root/f'state{i}'
  log=(root/f'r{i}-resident.log').open('w')
  driver_before=streaminfo();driver_started=time.monotonic();driver_start_ns=time.time_ns()
  proc=subprocess.Popen(['/usr/bin/time','-l',a.resident,'--repo',str(repo),'--config',str(cfg),'--state',str(state),'--context',ctx,'--nats',a.nats,'--source',a.source_mode],stdin=subprocess.PIPE,stdout=subprocess.PIPE,stderr=log,text=True,bufsize=1,start_new_session=True)
  init=online(f'r{i}-resident-initial',ctx,initial=True)
  initial_runs.append(init)
  if a.initial_hash:checks.append({'check':f'r{i} pinned initial reference equality','pass':init['graph_hash']==a.initial_hash})
  for j in range(10):online(f'r{i}-resident-noop{j}',ctx,{'label':f'noop{j}'})
  if a.source_mode=='watch' and a.probe_ignored:
   time.sleep(.2)
   quiet_before=online(f'r{i}-resident-noop-native-quiet-before',ctx,{'label':'native-quiet-before'})
   time.sleep(.3)
   quiet_after=online(f'r{i}-resident-noop-native-quiet-after',ctx,{'label':'native-quiet-after'})
   checks.append({'check':f'r{i} native backend quiet over 300ms','pass':'native_events' in quiet_before['driver'] and quiet_before['driver']['native_events']==quiet_after['driver'].get('native_events')})
   save()
  for probe_path,probe_bytes in ignored_backups.items():
   rel=str(probe_path.relative_to(repo))
   if a.source_mode=='watch':
    proc.stdin.write(json.dumps({'label':'arm-ignored','arm_paths':[rel]})+'\n');proc.stdin.flush();baseline=receive()['observed']
    request={'label':'ignored-input','observe_after':baseline}
   else:request={'label':'ignored-input','paths':[rel]}
   probe_path.write_bytes(probe_bytes+b'\n# enola ignored-input probe\n')
   online(f'r{i}-resident-noop-ignored-{probe_path.suffix}',ctx,request)
  body_runs.append(online(f'r{i}-resident-body',ctx,{'label':'body','paths':['packages/crypto/src/password.ts']},body))
  structural_runs.append(online(f'r{i}-resident-structural',ctx,{'label':'structural','paths':['packages/crypto/src/password.ts']},structural))
  proc.stdin.close();proc.wait(timeout=20)
  if proc.returncode:raise RuntimeError('resident exit '+str(proc.returncode))
  proc=None;log.close()
  rss_match=re.search(r'(\d+)\s+maximum resident set size',(root/f'r{i}-resident.log').read_text())
  init['lifetime_rss_bytes']=int(rss_match[1]) if rss_match else None
  save()
  if i==a.repeat:structural_ref=ng('cold-structural','analyze','cold-structural',prefix+'-cold-structural')['graph_hash']
  if i==1 and i!=a.repeat:
   f.write_text(body);body_ref=ng('cold-body','analyze','cold-body',prefix+'-cold-body')['graph_hash']
 if body_ref is None:
  f.write_text(body);body_ref=ng('cold-body','analyze','cold-body',prefix+'-cold-body')['graph_hash']
 f.write_text(original)
 initial_ref=ng('cold-original','analyze','cold-original',prefix+'-cold-original')['graph_hash']
 for op,ref in [(initial_runs,initial_ref),(body_runs,body_ref),(structural_runs,structural_ref)]:
  for d in op:checks.append({'check':d['label']+' cold equality','pass':d['graph_hash']==ref})
 save();print('CHECKS',checks,flush=True)
 if any(not c['pass'] for c in checks):raise RuntimeError('resident acceptance failed')
finally:
 restore_all([('stop resident',lambda:stop_process(proc)),
              *[(str(path),lambda path=path,content=content:path.write_bytes(content))
                for path,content in [(f,original.encode()),*ignored_backups.items()]]])
