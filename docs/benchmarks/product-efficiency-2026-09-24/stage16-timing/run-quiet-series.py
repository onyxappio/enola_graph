import pathlib,subprocess,json,datetime,time
b=pathlib.Path(__file__).resolve().parent
hold=json.loads((b/'quiet-window.json').read_text())
start=datetime.datetime.fromisoformat(hold['start_utc']).timestamp()
end=datetime.datetime.fromisoformat(hold['end_utc']).timestamp()
if not start<=time.time()<end:raise SystemExit('outside confirmed window')
receipt=b/'quiet-series.json'
if receipt.exists():raise SystemExit('refusing overwrite')
rows=[]
def run(label,args,reserve):
 if time.time()+reserve>end:raise SystemExit('insufficient confirmed window; retain completed evidence')
 row={'label':label,'argv':args,'started_utc':datetime.datetime.now(datetime.timezone.utc).isoformat()}
 with (b/(label+'-containers.log')).open('w') as f:
  subprocess.run(['docker','stats','--no-stream','--format','{{.Name}} {{.CPUPerc}} {{.MemUsage}}'],stdout=f,stderr=f,timeout=15)
 with (b/(label+'.log')).open('x') as f:r=subprocess.run(args,stdout=f,stderr=subprocess.STDOUT)
 row.update(exit=r.returncode,finished_utc=datetime.datetime.now(datetime.timezone.utc).isoformat())
 rows.append(row);receipt.write_text(json.dumps({'hold':hold,'runs':rows},indent=2)+'\n');print(label,r.returncode,flush=True)
 if r.returncode:raise SystemExit('run failed; inspect log')
for n in range(1,4):
 for arm in (['baseline','candidate'] if n%2 else ['candidate','baseline']):
  run(f'cli-{arm}-{n}',['python3','-B',str(b/'cli-pairs/run-arm.py'),'--arm',arm,'--repeat',str(n),'--quiet-message-id','joint-transcript-20260924T2048'],100)
for n in range(1,4):
 run(f'watch-pair-{n}',['python3','-B',str(b/'watch-pairs/run-pair.py'),'--scenario','ordinary','--repeat',str(n),'--quiet-message-id','joint-transcript-20260924T2048'],160)
print('series completed',flush=True)
