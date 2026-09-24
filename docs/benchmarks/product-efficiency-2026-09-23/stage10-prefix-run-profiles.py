import pathlib,os,subprocess,time,json,re,statistics
b=pathlib.Path(__file__).resolve().parent
def check_host():
 rows=subprocess.check_output(['ps','-axo','pid=,command='],text=True).splitlines()
 conflicts=[r.strip() for r in rows if ('go test ' in r or ('.test ' in r and '-test.' in r)) and '/enola-stage10-prefix-review/' not in r and '/bin/zsh -c ' not in r]
 if conflicts:raise SystemExit('competing tests: '+repr(conflicts))
results=[]
for repeat in range(1,4):
 for arm in (['baseline','candidate'] if repeat%2 else ['candidate','baseline']):
  check_host()
  log=b/f'{arm}-profile-{repeat}.log';cpu=b/f'{arm}-profile-{repeat}.cpu'
  if log.exists() or cpu.exists():raise SystemExit('refusing overwrite')
  env=os.environ.copy();env.update(ENOLA_GRAPH_PROFILE='1',ENOLA_DIAGNOSTIC_REPO='/tmp/enola-stage9-consecutive-retry-review/paired-clean-baseline-1/product-live',ENOLA_DIAGNOSTIC_CONFIG='/tmp/enola-stage9-consecutive-retry-review/paired-clean-baseline-1/config.yaml',ENOLA_DIAGNOSTIC_CPU=str(cpu))
  argv=[str(b/(arm+'.test')),'-test.run=^TestDiagnosticProductPolicyCPU$','-test.count=1','-test.v']
  t=time.monotonic()
  with log.open('w') as f:p=subprocess.run(argv,env=env,stdout=f,stderr=subprocess.STDOUT)
  text=log.read_text();durations=[]
  for value,unit in re.findall(r'construction=\d+ wall=([\d.]+)(ms|µs|ns|s)',text):durations.append(float(value)*{'s':1,'ms':.001,'µs':.000001,'ns':1e-9}[unit])
  identity=[float(x) for x in re.findall(r'compute_identities\s+([\d.]+)s',text)]
  row={'arm':arm,'repeat':repeat,'argv':argv,'exit_code':p.returncode,'wall_s':time.monotonic()-t,'construction_s':durations,'identity_s':identity}
  results.append(row);(b/'profile-results.json').write_text(json.dumps(results,indent=2)+'\n')
  if p.returncode or len(durations)!=20 or len(identity)!=20:raise SystemExit('incomplete diagnostic')
  print(json.dumps({'arm':arm,'repeat':repeat,'median_construction_s':statistics.median(durations),'median_identity_s':statistics.median(identity),'wall_s':row['wall_s']}),flush=True)
