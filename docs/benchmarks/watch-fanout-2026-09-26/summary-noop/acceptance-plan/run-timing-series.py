"""Run six counterbalanced two-arm CLI pairs only within an explicitly confirmed hold."""
import datetime, json, pathlib, subprocess, time
from timing_gate import validate_hold
from memory_gate import require_memory_evidence
from pressure_monitor import Monitor
b = pathlib.Path(__file__).resolve().parent
acceptance = json.loads((b / 'final-candidate-acceptance.json').read_text())
pins = json.loads((b / 'cli-pairs/pins.json').read_text())
if not acceptance.get('correctness_accepted') or not acceptance.get('source_revision'):
    raise SystemExit('final candidate correctness not accepted')
if acceptance.get('binary_sha256') != pins['binaries']['candidate']['sha256']:
    raise SystemExit('candidate acceptance/pin mismatch')
suite = json.loads((b / 'full-suite-receipt.json').read_text())
if suite.get('exit') != 0 or suite.get('runtime_revision') != pins['combined_source_revision']:
    raise SystemExit('candidate full-suite receipt missing or mismatched')
hold = json.loads((b / 'timing-window.json').read_text())
start = datetime.datetime.fromisoformat(hold['start_utc']).timestamp()
end = datetime.datetime.fromisoformat(hold['end_utc']).timestamp()
validate_hold(hold)
if hold.get("mode") != "current-roster":
    raise SystemExit("new cohort requires current-roster holds")
if not start <= time.time() < end:
    raise SystemExit('outside confirmed window')
receipt = b / 'timing-series.json'
if receipt.exists():
    raise SystemExit('refusing overwrite')
baseline = json.loads((b / 'baseline-correctness-acceptance.json').read_text())
if not baseline.get('correctness_accepted') or baseline.get('binary_sha256') != pins['binaries']['baseline']['sha256']:
    raise SystemExit('same-scope baseline correctness/pin mismatch')
orders = [('baseline','candidate'), ('candidate','baseline')] * 3
rows = []
for repeat in range(1, 7):
    # Reserve two 150-second arms before starting a new round; partial evidence
    # remains visible if a run unexpectedly takes longer or fails.
    if time.time() + 300 > end:
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
        before_vm=subprocess.check_output(['vm_stat'], text=True, timeout=5)
        (b/(label+'-vm-before.txt')).write_text(before_vm)
        monitor = Monitor(b/(label+'-pressure.jsonl'))
        monitor.start()
        row['monotonic_start'] = time.monotonic()
        failure = None
        result = None
        try:
            with (b / (label + '.log')).open('x') as log:
                result = subprocess.run(argv, stdout=log, stderr=subprocess.STDOUT)
        except BaseException as error:
            failure = repr(error)
        finally:
            row['monotonic_end'] = time.monotonic()
            try:
                monitor.finish(row['monotonic_start'], row['monotonic_end'])
                after_vm = subprocess.check_output(['vm_stat'], text=True, timeout=5)
                (b/(label+'-vm-after.txt')).write_text(after_vm)
                row['memory_evidence'] = require_memory_evidence(b, row)
            except Exception as error:
                failure = repr(error)
            row.update(exit=result.returncode if result else None, error=failure,
                       end_utc=datetime.datetime.now(datetime.timezone.utc).isoformat())
        rows.append(row)
        receipt.write_text(json.dumps({'hold': hold, 'runs': rows}, indent=2) + '\n')
        print(label, row, flush=True)
        if failure or result is None or result.returncode:
            raise SystemExit('arm failed; inspect receipt before continuing')
