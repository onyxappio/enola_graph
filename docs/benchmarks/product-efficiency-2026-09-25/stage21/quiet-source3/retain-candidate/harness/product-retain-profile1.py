"""Stage21 option C retain candidate on Product: correctness, profile and RSS.

Identical protocol, parameters and budget to product-proof-profile3.py - same flags,
same repo-id, same context, same 1 initial + 2 no-op + 1 body-edit - so the rows can be
read beside that run's. The one difference is the binary.

Shared load; NOT acceptance timing. Any comparison with the earlier diagnostic3 rows is
a diagnostic observation between two separately loaded runs. The competing-sample count
in each receipt recognizes Go builds and tests, test binaries, toolchain processes, Enola
runtimes and the observer - and nothing else, so a count of zero means no recognized
competitor was sampled, not that the host was quiet: browsers, indexers and any other CPU
or IO load are invisible to it.

The no-op fields this harness records are observations, not assertions. The correctness
verdict is taken by retain-noop-verdict.py, which fails closed on a missing field or a
missing run."""
import argparse, hashlib, importlib.util, json, os, pathlib, re, subprocess, threading, time

BIN = '/tmp/enola-stage21-root-review/enola-retain-final1'
BIN_SHA = 'd3229b4a5d622d1e9986565782a8c0cab7c078821775d361cb85809927c0aef6'
PRODUCT = '/tmp/enola-stage16-root-review/product-source'
PRODUCT_SHA = 'a609c19f3861971930fae7b33dcb2950598953c5'
OBSERVER = '/tmp/enola-stage9-product-watch-control/bin/benchobserver'

ap = argparse.ArgumentParser(); ap.add_argument('--work', required=True); a = ap.parse_args()
assert hashlib.sha256(pathlib.Path(BIN).read_bytes()).hexdigest() == BIN_SHA, 'not the pinned retain binary'
work = pathlib.Path(a.work); assert not work.exists(), 'work dir must be fresh'

spec = importlib.util.spec_from_file_location('watch', '/tmp/enola-stage14-watch-pairs/watch.py')
w = importlib.util.module_from_spec(spec); spec.loader.exec_module(w); eff = w.eff

def competitors():
    raw = subprocess.check_output(['ps', '-axo', 'pid=,ppid=,command='], text=True).splitlines()
    rows = []
    for line in raw:
        f = line.strip().split(None, 2)
        if len(f) == 3: rows.append((int(f[0]), int(f[1]), f[2]))
    owned = {os.getpid()}
    while True:
        more = {p for p, par, _ in rows if par in owned}
        if more <= owned: break
        owned |= more
    hits = []
    for pid, par, cmd in rows:
        if pid in owned: continue
        name = pathlib.Path(cmd.split(None, 1)[0]).name
        go = (name == 'go' and re.search(r'\s(test|build|install|vet)\s', cmd))
        test = (name.endswith('.test') and '-test.' in cmd)
        tool = ('/pkg/tool/' in cmd and name in ('compile', 'link', 'asm', 'cgo'))
        runtime = name.startswith('enola') or name == 'benchobserver'
        if go or test or tool or runtime: hits.append(f'{pid} {cmd}')
    return hits

assert subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=PRODUCT, text=True).strip() == PRODUCT_SHA
assert not subprocess.check_output(['git', 'status', '--porcelain', '--untracked-files=all'], cwd=PRODUCT, text=True).strip()

work.mkdir(parents=True); repo = work / 'product'
subprocess.run(['cp', '-cR', PRODUCT, str(repo)], check=True)
scope = (w.HERE / 'product-graph-scope.yaml').read_text()
cfg = work / 'config.yaml'
cfg.write_text(f'repo: {repo}\nextractors: [typescript]\nexplainers: []\nrenderers: []\n\n' + scope)

target = repo / 'packages/crypto/src/password.ts'
original = target.read_text()
body = original.replace('return email.trim().toLowerCase();', "return email.normalize('NFKC').trim().toLowerCase();")
assert body != original

port = eff.free_port(); conf = eff.write_nats_conf(work, port); url = f'nats://127.0.0.1:{port}'
stop = threading.Event(); samples = []
def monitor():
    while not stop.wait(1):
        h = competitors()
        if h: samples.append(h)
thread = threading.Thread(target=monitor)

rows = []; statedir = work / 'state1'; consumer = work / 'consumer.jsonl'
def streamseq():
    return json.loads(subprocess.check_output([OBSERVER, url, '--info']))['last_seq']

def run(label, mode, ctx, noop=False):
    env = os.environ.copy(); env['ENOLA_GRAPH_PROFILE'] = '1'
    cmd = [BIN, 'graph', mode, '--authoritative-scope', '--max-begin-bytes', '1048576',
           '--repo-id', 'stage21-cli-product', '--summary-json', '--nats', url,
           '--state-dir', str(statedir), '--context', ctx, str(cfg)]
    sfile = statedir / 'state.json'
    before_seq = streamseq() if noop else None
    before_state = hashlib.sha256(sfile.read_bytes()).hexdigest() if noop else None
    t = time.monotonic()
    with (work / (label + '.out')).open('w') as out, (work / (label + '.log')).open('w') as err:
        p = subprocess.Popen(['/usr/bin/time', '-l', *cmd], cwd=work, stdout=out, stderr=err,
                             env=env, start_new_session=True)
        p.wait()
    wall = time.monotonic() - t
    log = (work / (label + '.log')).read_text()
    m = {'label': label, 'mode': mode, 'wall_seconds': wall, 'exit': p.returncode}
    for k, pat in [('process_real_s', r'([\d.]+)\s+real'), ('rss_bytes', r'(\d+)\s+maximum resident set size'),
                   ('user_s', r'([\d.]+) user'), ('sys_s', r'([\d.]+) sys')]:
        z = re.search(pat, log); m[k] = float(z[1]) if z else None
    assert p.returncode == 0, label + ' failed: ' + log[-2000:]
    res = json.loads((work / (label + '.out')).read_text())
    assert len(res) == 1
    r0 = dict(res[0]); r0.pop('Facts', None); m['result'] = r0
    m['phases'] = [l for l in log.splitlines() if l.startswith('[graph-profile]')]
    if noop:
        m['noop_parsed_files'] = r0.get('ParsedFiles')
        m['noop_generation_unchanged'] = r0.get('BaseGeneration') == r0.get('TargetGeneration')
        m['noop_wire_messages'] = streamseq() - before_seq
        m['noop_state_unchanged'] = hashlib.sha256(sfile.read_bytes()).hexdigest() == before_state
    rows.append(m)
    (work / 'phase-metrics.json').write_text(json.dumps(rows, indent=2))
    print('DONE', label, round(wall, 3), r0, flush=True)
    return m

server = obs = None
try:
    with (work / 'nats.log').open('w') as log:
        server = subprocess.Popen([str(eff.NATS_SERVER), '-c', str(conf)], stdout=log,
                                  stderr=subprocess.STDOUT, start_new_session=True)
    eff.wait_port('127.0.0.1', port)
    oenv = os.environ.copy(); oenv.pop('ENOLA_GRAPH_PROFILE', None)
    oenv['OBSERVER_READY_FILE'] = str(work / 'ready'); oenv['OBSERVER_LIFECYCLE_FILE'] = str(work / 'lifecycle.jsonl')
    with (work / 'observer.stderr').open('w') as log:
        obs = subprocess.Popen([OBSERVER, url, str(consumer)], stdout=log, stderr=log, env=oenv,
                               start_new_session=True)
    eff.wait_observer_ready(work / 'ready', work / 'observer.stderr', obs)
    thread.start()
    run('o1-initial', 'analyze', 'stage21-proof-diagnostic')
    run('o2-noop', 'delta', 'stage21-proof-diagnostic', noop=True)
    run('o3-noop', 'delta', 'stage21-proof-diagnostic', noop=True)
    target.write_text(body)
    run('o4-body', 'delta', 'stage21-proof-diagnostic')
finally:
    target.write_text(original)
    stop.set()
    if thread.ident: thread.join()
    eff.stop_process(obs); eff.stop_process(server)
    (work / 'diagnostic-receipt.json').write_text(json.dumps({
        'label': 'diagnostic only; shared load; not a timing measurement',
        'enola_source': 'option C retain candidate /tmp/enola-stage21-retain/src; see REVIEW_NOTES.md and optionC.diff',
        'comparable_with': 'product-proof-profile3.py rows, same flags and budget, different load',
        'binary_sha256': hashlib.sha256(pathlib.Path(BIN).read_bytes()).hexdigest(),
        'product_sha': PRODUCT_SHA, 'profile': 'ts + product-graph-scope.yaml overlay',
        'authorized_runs': '1 initial + 2 no-op + 1 body-edit', 'runs_executed': len(rows),
        'competing_samples': samples,
    }, indent=2) + '\n')
    print('COMPETING_SAMPLES', len(samples), flush=True)
