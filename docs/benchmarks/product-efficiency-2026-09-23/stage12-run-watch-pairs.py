import pathlib, subprocess, time, json, os, hashlib, threading
b=pathlib.Path(__file__).resolve().parent
root=pathlib.Path('/Users/oleksandr.mykulych/orca/enola_graph')
harness=b/'watch.py'
def competitors():
    rows=subprocess.check_output(['ps','-axo','pid=,command='],text=True).splitlines()
    return [r.strip() for r in rows if ('go test ' in r or ('.test ' in r and '-test.' in r)) and '/enola-stage12-watch-probe-review/' not in r and '/bin/zsh -c ' not in r]
import argparse
p=argparse.ArgumentParser()
p.add_argument('--scenario',choices=['ordinary','near-expiry'],required=True)
a=p.parse_args()
scenario=a.scenario
receipt=b/f'{scenario}-watch-pairs.json'
results=[]
if receipt.exists(): raise SystemExit('refusing overwrite')
for repeat in range(1,4):
    for arm in (['baseline','candidate'] if repeat%2 else ['candidate','baseline']):
        work=b/f'{scenario}-watch-{arm}-{repeat}'
        if work.exists(): raise SystemExit(f'refusing overwrite {work}')
        conflict=competitors()
        if conflict: raise SystemExit('wait for competing tests: '+repr(conflict))
        binary=b/f'enola-{arm}'
        argv=['python3',str(harness),'--binary',str(binary),'--observer','/tmp/enola-stage9-product-watch-control/bin/benchobserver','--source','/tmp/enola-stage9-md-timing/product','--work',str(work),'--watch-every','500ms']
        if scenario=='near-expiry': argv += ['--duplicate-pause','0.4']
        env=os.environ.copy();env.pop('ENOLA_GRAPH_PROFILE',None)
        stop=threading.Event();samples=[]
        def monitor():
            while not stop.wait(2):
                hits=competitors()
                if hits:samples.append({'elapsed_s':time.monotonic()-start,'processes':hits})
        start=time.monotonic();monitor_thread=threading.Thread(target=monitor);monitor_thread.start()
        try:
            with (b/f'{scenario}-watch-{arm}-{repeat}.log').open('x') as f:p=subprocess.run(argv,cwd=root,env=env,stdout=f,stderr=subprocess.STDOUT)
        finally:
            stop.set();monitor_thread.join()
        row={'scenario':scenario,'arm':arm,'repeat':repeat,'argv':argv,'exit_code':p.returncode,'elapsed_s':time.monotonic()-start,'binary_sha256':hashlib.sha256(binary.read_bytes()).hexdigest(),'harness_sha256':hashlib.sha256(harness.read_bytes()).hexdigest(),'competing_test_samples':samples,'monitor_limit':'2-second process sampling detects test workloads, not all host contention'}
        if (work/'watch.json').exists():
            d=json.loads((work/'watch.json').read_text());row['watch']=d
            row['valid_correctness']=p.returncode==0 and d['equal'] and d['input_stability_across_cold']['stable'] and d['idle_events']==0 and d['duplicate_events']==0 and not d['abandoned_begins'] and d['telemetry_totals']['batch_count_mismatches']==0
            row['valid_timing']=row['valid_correctness'] and not samples
            row['initial_ms']=d['watch_launch_to_initial_consumer_applied_ms']
            row['delta_ms']=d['convergence']['final_convergence_ms']
            row['parsed_files']=d['telemetry_generations'][-1]['completeness_parsed_files']
        results.append(row);receipt.write_text(json.dumps(results,indent=2)+'\n')
        print(json.dumps({k:v for k,v in row.items() if k not in ['watch','argv','competing_test_samples']}),flush=True)
        if not row.get('valid_timing'): raise SystemExit('run unsuitable for paired acceptance; inspect receipt')
