import pathlib,subprocess,json,datetime
b=pathlib.Path(__file__).resolve().parent
results=[]
for tag,scenario in [('source','source-only'),('recent','recent-to-current'),('broad','history-to-current')]:
 row=dict(tag=tag,scenario=scenario,start_utc=datetime.datetime.now(datetime.timezone.utc).isoformat())
 with (b/('history-'+tag+'.log')).open('x') as out:
  r=subprocess.run(['python3','-B',str(b/'history-harness/run-history.py'),'--scenario',scenario,'--tag',tag,'--output',str(b/('history-'+tag)),'--pins',str(b/'history-pins.json')],stdout=out,stderr=subprocess.STDOUT)
 row.update(exit=r.returncode,end_utc=datetime.datetime.now(datetime.timezone.utc).isoformat());results.append(row);(b/'history-series.json').write_text(json.dumps(results,indent=2)+'\n');print(row,flush=True)
 if r.returncode:raise SystemExit(r.returncode)
