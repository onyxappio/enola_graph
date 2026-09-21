"""Sequential candidate benchmark; source edits only in the dedicated Product clone."""
from pathlib import Path
import argparse,hashlib,json,re,shutil,subprocess,time,threading,os,signal
p=argparse.ArgumentParser();p.add_argument('--binary',required=True);p.add_argument('--old');p.add_argument('--root',required=True);p.add_argument('--source',default='/tmp/enola-product-benchmark-source');p.add_argument('--nats',required=True);p.add_argument('--observer',default='/tmp/enola-product-observer');p.add_argument('--repeat',type=int,default=3);p.add_argument('--profile',choices=['full','ts'],default='full');p.add_argument('--cold',action='store_true');p.add_argument('--initial-only',action='store_true');a=p.parse_args()
root=Path(a.root).resolve();root.mkdir(parents=True,exist_ok=True);repo=Path(a.source).absolute()
if subprocess.check_output(['git','status','--porcelain','--untracked-files=no'],cwd=repo).strip():raise RuntimeError('Product clone has tracked edits; refusing to overwrite')
f=repo/'packages/crypto/src/password.ts';original=f.read_text();body=original.replace('return email.trim().toLowerCase();',"return email.normalize('NFKC').trim().toLowerCase();");assert body!=original
structural=body+'\nexport function enolaBenchmarkEmailKey(email: string) { return normalizeEmail(email); }\n'
cfg=root/'config.yaml';cfg.write_text('repo: '+str(repo)+'\n'+('extractors: [typescript]\nexplainers: []\nrenderers: []\n' if a.profile=='ts' else ''))
rows=[];checks=[];hashes={};prefix=root.name
(root/'provenance.json').write_text(json.dumps({'product_sha':subprocess.check_output(['git','rev-parse','HEAD'],cwd=repo).decode().strip(),'binary':a.binary,'binary_sha256':hashlib.sha256(Path(a.binary).read_bytes()).hexdigest(),'profile':a.profile,'config':cfg.read_text(),'started_ns':time.time_ns(),'original_file_sha256':hashlib.sha256(original.encode()).hexdigest()},indent=2))
def save():
 (root/'metrics.json').write_text(json.dumps(rows,indent=2));(root/'checks.json').write_text(json.dumps(checks,indent=2))
def streaminfo():return json.loads(subprocess.check_output([a.observer,a.nats,'--info']))
def wire(ctx,start):
 until=time.monotonic()+120
 while time.monotonic()<until:
  for l in (root/'consumer.jsonl').read_text().splitlines():
   try:v=json.loads(l)
   except json.JSONDecodeError:continue
   if v['context']==ctx and v['first_ns']>=start:return v
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
 with (root/(label+'.out')).open('w') as out,(root/(label+'.log')).open('w') as err:
  proc=subprocess.Popen(['/usr/bin/time','-l',*cmd],cwd=root,stdout=out,stderr=err,start_new_session=True)
  try:proc.wait(timeout=600)
  except subprocess.TimeoutExpired:
   os.killpg(proc.pid,signal.SIGKILL);proc.wait()
   rows.append({'label':label,'start_ns':ns,'seconds':time.monotonic()-t,'timeout':True});save();raise
  finally:
   if proc.poll() is None:os.killpg(proc.pid,signal.SIGKILL);proc.wait()
   stop.set();sampler.join()
 m={'state_peak_sampled_bytes':peak[0],'spool_peak_sampled_bytes':peak[1],'state_final_bytes':diskbytes(statepath)[0],'label':label,'start_ns':ns,'seconds':time.monotonic()-t,'exit':proc.returncode};log=(root/(label+'.log')).read_text()
 for k,pat in [('rss_bytes',r'(\d+)\s+maximum resident set size'),('user_s',r'([\d.]+) user'),('sys_s',r'([\d.]+) sys')]:
  z=re.search(pat,log);m[k]=float(z[1]) if z else None
 rows.append(m);save()
 if proc.returncode:raise RuntimeError(label+' failed: '+log[-1800:])
 if ctx:
  z=re.search(r'generation (\d+)→(\d+) parsed=(\d+) cached=(\d+) early_local=(\d+) fallbacks=(\d+)',log)
  if not z:raise RuntimeError('missing graph summary '+label)
  m['result']=dict(zip(['BaseGeneration','TargetGeneration','ParsedFiles','CachedFiles','EarlyLocal','FallbackCount'],map(int,z.groups())))
  m['fallbacks']=re.findall(r'\[graph\] fallback ([^\n]+)',log)
  if m['result']['BaseGeneration']!=m['result']['TargetGeneration']:
   w=wire(ctx,ns);m['wire']=w;hashes[ctx]=w['normalized_hash']
   for k in ['first_ns','first_batch_ns','broker_end_ns','consumer_end_ns']:
    if k in w:m[k.replace('_ns','_s')]=(w[k]-ns)/1e9
  m['graph_hash']=hashes.get(ctx)
  if noop:
   after=streaminfo();m['noop_wire_messages']=after['last_seq']-before['last_seq'];checks.append({'check':label+' no-op','pass':m['result']['ParsedFiles']==0 and m['result']['BaseGeneration']==m['result']['TargetGeneration'] and m['noop_wire_messages']==0})
 else:
  z=re.search(r'Facts:\s+(\d+)',log);m['facts']=int(z[1]) if z else None
 save();print('DONE',label,round(m['seconds'],3),m.get('result'),flush=True);return m
def ng(label,mode,state,ctx,noop=False):return run(label,[a.binary,'graph',mode,'--nats',a.nats,'--state-dir',str(root/state),'--context',ctx,str(cfg)],ctx,noop)
body_ref=None;body_runs=[];structural_runs=[];structural_ref=None
expected_initial={'full':'d727bfe7f954069a6b16c636c8ad6bd87b288bf11a5e214c71b88b3b75eae2d2','ts':'a52bd38e666922e1db871e766bd43b5495483c9154fd5b1f84a9a54f13120f3b'}
try:
 for i in range(1,a.repeat+1):
  f.write_text(original)
  if a.old:
   shutil.rmtree(repo/'.enola',ignore_errors=True);run(f'r{i}-old-initial',[a.old,'--generate',str(cfg)]);run(f'r{i}-old-noop',[a.old,'--generate',str(cfg)])
  ctx=prefix+f'-main{i}';state=f'state{i}'
  init=ng(f'r{i}-new-initial','analyze',state,ctx)
  checks.append({'check':f'r{i} initial equals independent repaired baseline','pass':init['graph_hash']==expected_initial[a.profile],'expected':expected_initial[a.profile],'actual':init['graph_hash']});save()
  if a.initial_only:continue
  ng(f'r{i}-new-noop','delta',state,ctx,True)
  f.write_text(body)
  if a.old:run(f'r{i}-old-body',[a.old,'--generate',str(cfg)])
  b=ng(f'r{i}-new-body','delta',state,ctx);body_runs.append(b)
  f.write_text(structural)
  if a.old:run(f'r{i}-old-structural',[a.old,'--generate',str(cfg)])
  d=ng(f'r{i}-new-structural','delta',state,ctx);structural_runs.append(d)
  if a.cold and i==a.repeat:
   structural_ref=ng('cold-structural','analyze','cold-structural',prefix+'-cold-structural')['graph_hash']
  if a.cold and i==1 and i!=a.repeat:
   f.write_text(body)
   body_ref=ng('cold-body','analyze','cold-body',prefix+'-cold-body')['graph_hash']
 if a.cold and body_ref is None:
  f.write_text(body);body_ref=ng('cold-body','analyze','cold-body',prefix+'-cold-body')['graph_hash']
 if structural_ref:
  for d in structural_runs:checks.append({'check':d['label']+' equals cold','pass':d['graph_hash']==structural_ref})
 if body_ref:
  for b in body_runs:checks.append({'check':b['label']+' equals cold','pass':b['graph_hash']==body_ref})
 save();print('CHECKS',checks,flush=True)
 if any(not c['pass'] for c in checks):raise RuntimeError('Independent graph/no-op acceptance failed; see checks.json')
finally:f.write_text(original)
