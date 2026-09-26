"""Run three balanced three-arm CLI rounds only within an explicitly confirmed hold."""
import datetime, json, pathlib, subprocess, time
from timing_gate import validate_hold
b = pathlib.Path(__file__).resolve().parent
acceptance = json.loads((b / 'final-candidate-acceptance.json').read_text())
pins = json.loads((b / 'cli-pairs/pins.json').read_text())
if not acceptance.get('correctness_accepted') or not acceptance.get('source_revision'):
    raise SystemExit('final candidate correctness not accepted')
if acceptance.get('binary_sha256') != pins['binaries']['candidate']['sha256']:
    raise SystemExit('candidate acceptance/pin mismatch')
hold = json.loads((b / 'timing-window.json').read_text())
start = datetime.datetime.fromisoformat(hold['start_utc']).timestamp()
end = datetime.datetime.fromisoformat(hold['end_utc']).timestamp()
validate_hold(hold)
if not start <= time.time() < end:
    raise SystemExit('outside confirmed window')
receipt = b / 'timing-series.json'
if receipt.exists():
    raise SystemExit('refusing overwrite')
control = json.loads((b / 'control-correctness-acceptance.json').read_text())
if not control.get('correctness_accepted') or control.get('binary_sha256') != pins['binaries']['control']['sha256']:
    raise SystemExit('fanout control correctness/pin mismatch')
orders = [('baseline','control','candidate'), ('control','candidate','baseline'), ('candidate','baseline','control')]
rows = []
for repeat in range(1, 4):
    # Reserve three 150-second arms before starting a new round; partial evidence
    # remains visible if a run unexpectedly takes longer or fails.
    if time.time() + 450 > end:
        raise SystemExit('insufficient remaining pair reserve')
    for arm in orders[repeat - 1]:
        if time.time() + 150 > end:
            raise SystemExit('insufficient arm reserve; retain incomplete pair')
        label = f'{arm}-{repeat}'
        argv = ['python3', '-B', str(b / 'cli-pairs/run-arm.py'),
                '--arm', arm, '--repeat', str(repeat),
                '--quiet-message-id', hold['worker_message']]
        if hold.get('mode') == 'controlled-background':
            argv += ['--background-hold', str(b / 'timing-window.json')]
        row = {'arm': arm, 'repeat': repeat, 'argv': argv,
               'start_utc': datetime.datetime.now(datetime.timezone.utc).isoformat()}
        with (b / (label + '-containers.log')).open('x') as log:
            subprocess.run(['docker', 'stats', '--no-stream', '--format',
                            '{{.Name}} {{.CPUPerc}} {{.MemUsage}}'],
                           stdout=log, stderr=log, timeout=15, check=True)
        with (b / (label + '.log')).open('x') as log:
            result = subprocess.run(argv, stdout=log, stderr=subprocess.STDOUT)
        row.update(exit=result.returncode,
                   end_utc=datetime.datetime.now(datetime.timezone.utc).isoformat())
        rows.append(row)
        receipt.write_text(json.dumps({'hold': hold, 'runs': rows}, indent=2) + '\n')
        print(label, result.returncode, flush=True)
        if result.returncode:
            raise SystemExit('arm failed; inspect receipt before continuing')
