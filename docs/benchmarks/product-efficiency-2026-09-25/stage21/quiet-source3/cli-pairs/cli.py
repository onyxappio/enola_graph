"""Sequential candidate benchmark; source edits only in the dedicated Product clone."""
from pathlib import Path
from harness_support import LegacyMirror, wait_process
import argparse,hashlib,json,re,shutil,subprocess,time,threading,os,signal
p=argparse.ArgumentParser();p.add_argument('--binary',required=True);p.add_argument('--old');p.add_argument('--old-config');p.add_argument('--scope-tool',help='candidate benchscope export helper; enables private selected-input legacy mirror');p.add_argument('--root',required=True);p.add_argument('--source',default='/tmp/enola-product-benchmark-source');p.add_argument('--nats',required=True);p.add_argument('--observer',default='/tmp/enola-product-observer');p.add_argument('--repeat',type=int,default=3);p.add_argument('--profile',choices=['full','ts'],default='full');p.add_argument('--scope-config',help='YAML fragment with graph_inputs/ignore/extractor settings, without repo');a=p.parse_args()
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
cfg=root/'config.yaml';cfg.write_text('repo: '+str(repo)+'\n'+('extractors: [typescript]\nexplainers: []\nrenderers: []\n' if a.profile=='ts' else ''))
if a.scope_config:
 scope=Path(a.scope_config).read_text()
 if re.search(r'^repo(?:s)?\s*:',scope,re.M):raise RuntimeError('scope-config must not set repository')
 with cfg.open('a') as stream:stream.write('\n'+scope)
rows=[];checks=[];hashes={};prefix=root.name
observer_digest=hashlib.sha256(Path(a.observer).read_bytes()).hexdigest()
(root/'provenance.json').write_text(json.dumps({'product_sha':subprocess.check_output(['git','rev-parse','HEAD'],cwd=repo).decode().strip(),'observer':a.observer,'observer_sha256':observer_digest,'binary':a.binary,'binary_sha256':hashlib.sha256(Path(a.binary).read_bytes()).hexdigest(),'profile':a.profile,'config':cfg.read_text(),'started_ns':time.time_ns(),'original_file_sha256':hashlib.sha256(original.encode()).hexdigest()},indent=2))
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
 state_before=hashlib.sha256((statepath/'state.json').read_bytes()).hexdigest() if noop else None
 t=time.monotonic();ns=time.time_ns()
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
   after=streaminfo();m['noop_state_before']=state_before;m['noop_state_after']=hashlib.sha256((statepath/'state.json').read_bytes()).hexdigest();m['noop_wire_messages']=after['last_seq']-before['last_seq'];checks.append({'check':label+' no-op','pass':m['result']['ParsedFiles']==0 and m['result']['BaseGeneration']==m['result']['TargetGeneration'] and m['noop_wire_messages']==0 and m['noop_state_before']==m['noop_state_after']})
 else:
  z=re.search(r'Facts:\s+(\d+)',log);m['facts']=int(z[1]) if z else None
 save();print('DONE',label,round(m['seconds'],3),m.get('result'),flush=True);return m
def ng(label,mode,state,ctx,noop=False):return run(label,[a.binary,'graph',mode,'--authoritative-scope','--max-begin-bytes','1048576','--repo-id','stage15-cli-product','--summary-json','--nats',a.nats,'--state-dir',str(root/state),'--context',ctx,str(cfg)],ctx,noop)

if a.old and not (a.old_config or a.scope_tool):raise RuntimeError('--old requires --scope-tool (selected-input mirror) or --old-config (approximate scope)')
old_template=json.loads(Path(a.old_config).read_text()) if a.old and a.old_config else None
mirror=LegacyMirror(repo,root,a.scope_tool,cfg) if a.old and a.scope_tool else None
if a.old:
 if old_template and Path(old_template['repo']).resolve()!=repo.resolve():raise RuntimeError('Legacy config repo differs from candidate')
 provenance=json.loads((root/'provenance.json').read_text());provenance.update({'old_binary':a.old,'old_sha256':hashlib.sha256(Path(a.old).read_bytes()).hexdigest(),'old_config_template':old_template,'legacy_comparison':'selected-repository-input mirror; side reads and semantics not proven equivalent' if mirror else 'approximate scope; ignored tests and side reads can differ','scope_tool':a.scope_tool,'scope_tool_sha256':hashlib.sha256(Path(a.scope_tool).read_bytes()).hexdigest() if a.scope_tool else None});(root/'provenance.json').write_text(json.dumps(provenance,indent=2))
initials=[];bodies=[];structurals=[];body_ref=None;structural_ref=None
try:
 for i in range(1,a.repeat+1):
  f.write_text(original)
  if a.old:
   if mirror:old_path=mirror.sync(f'r{i}-old-initial',i,initial=True)
   else:
    old_cfg=json.loads(json.dumps(old_template));old_cfg.setdefault('output',{})['dir']=f'.enola/{prefix}/old-output-{i}'
    if (repo/old_cfg['output']['dir']).exists():raise RuntimeError('Legacy initial output already exists')
    old_path=root/f'old-config-{i}.json';old_path.write_text(json.dumps(old_cfg,indent=2))
   run(f'r{i}-old-initial',[a.old,'--generate',str(old_path)])
   if mirror:mirror.evidence(f'r{i}-old-initial')
   run(f'r{i}-old-noop',[a.old,'--generate',str(old_path)])
   if mirror:mirror.evidence(f'r{i}-old-noop')
  ctx=prefix+f'-main{i}';state=f'state{i}'
  init=ng(f'r{i}-new-initial','analyze',state,ctx);initials.append(init)
  if init['result']['BaseGeneration']!=0 or init['result']['TargetGeneration']!=1:raise RuntimeError('CLI initial did not start fresh')
  ng(f'r{i}-new-noop','delta',state,ctx,True)
  f.write_text(body)
  if a.old:
   if mirror:old_path=mirror.sync(f'r{i}-old-body',i)
   run(f'r{i}-old-body',[a.old,'--generate',str(old_path)])
   if mirror:mirror.evidence(f'r{i}-old-body')
  bodies.append(ng(f'r{i}-new-body','delta',state,ctx))
  f.write_text(structural)
  if a.old:
   if mirror:old_path=mirror.sync(f'r{i}-old-structural',i)
   run(f'r{i}-old-structural',[a.old,'--generate',str(old_path)])
   if mirror:mirror.evidence(f'r{i}-old-structural')
  structurals.append(ng(f'r{i}-new-structural','delta',state,ctx))
 structural_ref=ng('cold-structural','analyze','cold-structural',prefix+'-cold-structural')['graph_hash']
 f.write_text(body);body_ref=ng('cold-body','analyze','cold-body',prefix+'-cold-body')['graph_hash']
 f.write_text(original);initial_ref=ng('cold-original','analyze','cold-original',prefix+'-cold-original')['graph_hash']
 for group,reference in [(initials,initial_ref),(bodies,body_ref),(structurals,structural_ref)]:
  for r in group:checks.append({'check':r['label']+' cold graph equality','pass':r['graph_hash']==reference})
 save();print('CHECKS',checks,flush=True)
 if any(not c['pass'] for c in checks):raise RuntimeError('CLI acceptance failed')
finally:f.write_text(original)
