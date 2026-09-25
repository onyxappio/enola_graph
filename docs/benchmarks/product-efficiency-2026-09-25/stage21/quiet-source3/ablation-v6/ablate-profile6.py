"""Stage21 bounded causal diagnostic: frozen candidate vs proof-production ablation.

One paired diagnostic per scenario (body, structural) under declared shared load.
NOT acceptance timing, not a quiet window, not a speed claim.

Seeding, corrected four times. What each correction forced:

  * One protocol context for every seed and every measured arm (root msg_11be6bd6c5d6).
    Arm and scenario live in the local label only. Cold references keep their own
    contexts, which is the one place a different context is correct.

  * The observer keeps the applied graph in an in-memory byContext map, so copying
    consumer.jsonl restores the output file and not the graph (root msg_1165196a84f2).

  * Independently produced seeds can never be byte-identical: State.LastRunID
    (state.go:77) carries the producing run's id (root msg_15f335f9830f).

  * A live consumer cannot be taken backwards. Run 3 re-seeded the second arm from a
    wiped state dir under a live observer and died with
    "begin apply: consumer: base generation 0 != last applied 2" (root msg_321c5cf02f6a).

So the seed is produced ONCE per scenario with the frozen binary. The broker is then
stopped and the full canonical state dir and the JetStream store are snapshotted
together at rest. Each arm restores both to the SAME paths, starts a fresh broker and
a fresh replay observer over an empty output file, and waits for the retained stream
to be replayed back into a consumer graph whose run id, normalized hash and applied
generation equal the seed's - READY alone is not readiness - and only then runs its
one measured delta. Both arms therefore start from the identical seed bytes and an
identical consumer graph, with no generation reset and no weakened guard.

The replay observer is the pinned Stage21 variant
(/tmp/enola-stage21-startup-profile/replayobserver, root-verified
bf5e4ba1...): durable prefix and DeliverNew -> DeliverAll, nothing else. The Enola
binaries are unchanged.

Seed production, broker and observer startup, snapshot, restore and replay all sit
outside every measured delta. Exactly one measured delta per restored seed.
"""
import argparse, hashlib, importlib.util, json, os, pathlib, re, shutil, subprocess, threading, time

FROZEN = '/tmp/enola-stage21-root-review/enola-proof-diagnostic3'
ABLATED = '/tmp/enola-stage21-ablate/enola-ablate-noproof2'
ABLATION_DIFF = '/tmp/enola-stage21-ablate/ablation2.diff'
PRODUCT = '/tmp/enola-stage16-root-review/product-source'
PRODUCT_SHA = 'a609c19f3861971930fae7b33dcb2950598953c5'
OBSERVER = '/tmp/enola-stage21-startup-profile/replayobserver'
OBSERVER_SHA = 'bf5e4ba171d59c80137d63c9f09b0049f4c6d2294c2748ad9a68b7c4e2b4067b'
OBSERVER_DIFF = ('/Users/oleksandr.mykulych/orca/enola_graph/docs/benchmarks/'
                 'product-efficiency-2026-09-25/stage21/replayobs-source.diff')
REPO_ID = 'stage21-ablate-product'
CTX = 'stage21-ablate-seed'

ap = argparse.ArgumentParser(); ap.add_argument('--work', required=True); a = ap.parse_args()
work = pathlib.Path(a.work).resolve(); assert not work.exists(), 'work dir must be fresh'

spec = importlib.util.spec_from_file_location('watch', '/tmp/enola-stage14-watch-pairs/watch.py')
w = importlib.util.module_from_spec(spec); spec.loader.exec_module(w); eff = w.eff

def sha(p): return hashlib.sha256(pathlib.Path(p).read_bytes()).hexdigest()

assert sha(OBSERVER) == OBSERVER_SHA, 'replay observer is not the pinned binary'

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
        runtime = name.startswith('enola') or name in ('benchobserver', 'replayobserver')
        if go or test or tool or runtime: hits.append(f'{pid} {cmd}')
    return hits

assert subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=PRODUCT, text=True).strip() == PRODUCT_SHA
assert not subprocess.check_output(['git', 'status', '--porcelain', '--untracked-files=all'], cwd=PRODUCT, text=True).strip()

work.mkdir(parents=True)
repo = work / 'product'
subprocess.run(['cp', '-cR', PRODUCT, str(repo)], check=True)
cfg = work / 'config.yaml'
cfg.write_text(f'repo: {repo}\nextractors: [typescript]\nexplainers: []\nrenderers: []\n\n'
               + (w.HERE / 'product-graph-scope.yaml').read_text())

target = repo / 'packages/crypto/src/password.ts'
original = target.read_text()
body = original.replace('return email.trim().toLowerCase();', "return email.normalize('NFKC').trim().toLowerCase();")
assert body != original
structural = body + '\nexport function enolaBenchmarkEmailKey(email: string) { return normalizeEmail(email); }\n'
TEXT = {'original': original, 'body': body, 'structural': structural}

statedir = work / 'state1'
consumer = work / 'consumer.jsonl'
port = eff.free_port(); conf = eff.write_nats_conf(work, port); url = f'nats://127.0.0.1:{port}'
natsstore = work / 'nats-store'

stop = threading.Event(); samples = []
def monitor():
    while not stop.wait(1):
        h = competitors()
        if h: samples.append({'at': time.time(), 'procs': h})
thread = threading.Thread(target=monitor)

rows = []; server = obs = None; phase = [0]
def save(): (work / 'ablation-metrics.json').write_text(json.dumps(rows, indent=2) + '\n')

MARK = re.compile(r'^\[graph-profile\]')
PINS = ['state_proof_bound', 'runtime_inputs_reused', 'runtime_inputs_reusable', 'state_proof_refused',
        'state_proof_unavailable', 'state_proof_written', 'state_proof_promoted',
        'ablation_proof_cleanup_stage', 'ablation_proof_cleanup_promote',
        'state_proof_fallback_decode', 'state_json_unmarshal', 'state_json_marshal',
        'state_read_bytes', 'state_fingerprint', 'load_state', 'select_hash_targets',
        'hash_content_inputs', 'write_pending_state', 'promote_compact_state', 'end_flush_ack',
        'graph_session_run', 'cli_complete']

# ---------------------------------------------------------------- broker/observer

def stream_last_seq():
    return json.loads(subprocess.check_output([OBSERVER, url, '--info']))['last_seq']

def start_broker():
    global server
    assert server is None
    with (work / 'nats.log').open('a') as log:
        server = subprocess.Popen([str(eff.NATS_SERVER), '-c', str(conf)], stdout=log,
                                  stderr=subprocess.STDOUT, start_new_session=True)
    eff.wait_port('127.0.0.1', port)

def start_observer():
    """Fresh observer over a fresh output file. Each start gets its own ready and
    stderr file: wait_observer_ready accepts READY found anywhere in the file it is
    handed, so a shared appended file reports a restarted observer ready before it has
    subscribed."""
    global obs
    assert obs is None
    if consumer.is_file() and consumer.stat().st_size:
        # The completed frames of the phase that just ended are kept under its own
        # name rather than discarded by the truncation (root msg_641e1ec46522). The
        # scenario is unchanged: each observer still starts over an empty file.
        consumer.replace(work / f'consumer-phase-{phase[0]}.jsonl')
    phase[0] += 1
    consumer.write_text('')
    oenv = os.environ.copy(); oenv.pop('ENOLA_GRAPH_PROFILE', None)
    ready = work / f'ready-{phase[0]}'
    errf = work / f'observer-{phase[0]}.stderr'
    oenv['OBSERVER_READY_FILE'] = str(ready)
    oenv['OBSERVER_LIFECYCLE_FILE'] = str(work / f'lifecycle-{phase[0]}.jsonl')
    with errf.open('w') as log:
        obs = subprocess.Popen([OBSERVER, url, str(consumer)], stdout=log, stderr=log, env=oenv,
                               start_new_session=True)
    eff.wait_observer_ready(ready, errf, obs)
    return {'phase': phase[0], 'pid': obs.pid, 'stderr': errf.name}

def stop_all():
    global server, obs
    eff.stop_process(obs); obs = None
    eff.stop_process(server); server = None

def frames():
    out = []
    for l in consumer.read_text().splitlines():
        try: out.append(json.loads(l))
        except json.JSONDecodeError: continue
    return out

def wire(ctx, start, run_id, timeout=120):
    """The frame a live run produced: matched on run id AND on a first_ns at or after
    the run started, so a replayed frame can never be mistaken for a fresh one."""
    until = time.monotonic() + timeout
    while time.monotonic() < until:
        for v in frames():
            if v.get('context') == ctx and v.get('first_ns', 0) >= start and v.get('run_id') == run_id:
                if not re.fullmatch(r'[0-9a-f]{64}', v.get('normalized_hash', '')):
                    raise RuntimeError('invalid consumer graph digest')
                return v
        time.sleep(0.05)
    raise RuntimeError('no completed consumer frame for ' + ctx)

def await_replay(seed, timeout=180):
    """Readiness for a measured delta: the retained stream has been replayed back into
    a consumer graph identical to the seed's. READY is insufficient - it only says the
    observer subscribed. Matching is on run id, exact normalized hash and applied
    generation together; a partial or reordered replay fails rather than passes."""
    until = time.monotonic() + timeout
    while time.monotonic() < until:
        for v in frames():
            if (v.get('run_id') == seed['run_id'] and v.get('context') == CTX
                    and v.get('normalized_hash') == seed['graph_hash']
                    and v.get('applied_generation') == seed['target_generation']):
                return {'replayed_frames': len(frames()), 'matched': {
                    'run_id': v.get('run_id'), 'normalized_hash': v.get('normalized_hash'),
                    'base_generation': v.get('base_generation'),
                    'applied_generation': v.get('applied_generation')}}
        if obs.poll() is not None:
            raise RuntimeError('replay observer exited before the seed graph was rebuilt:\n'
                               + (work / f'observer-{phase[0]}.stderr').read_text()[-2000:])
        time.sleep(0.05)
    raise RuntimeError('seed graph was never replayed for ' + CTX)

# ---------------------------------------------------------------- runs

def run(label, binary, mode, ctx, sdir, measured=False, arm=None, scenario=None, seed=None):
    env = os.environ.copy(); env['ENOLA_GRAPH_PROFILE'] = '1'
    cmd = [binary, 'graph', mode, '--authoritative-scope', '--max-begin-bytes', '1048576',
           '--repo-id', REPO_ID, '--summary-json', '--nats', url,
           '--state-dir', str(sdir), '--context', ctx, str(cfg)]
    ns = time.time_ns(); t = time.monotonic()
    with (work / (label + '.out')).open('w') as out, (work / (label + '.log')).open('w') as err:
        p = subprocess.Popen(['/usr/bin/time', '-l', *cmd], cwd=work, stdout=out, stderr=err,
                             env=env, start_new_session=True)
        p.wait()
    wall = time.monotonic() - t
    log = (work / (label + '.log')).read_text()
    assert p.returncode == 0, label + ' failed: ' + log[-2000:]
    res = json.loads((work / (label + '.out')).read_text()); assert len(res) == 1
    r0 = dict(res[0]); r0.pop('Facts', None)
    phases = [l for l in log.splitlines() if MARK.match(l)]
    pins = {n: [l for l in phases if re.search(r'\b' + n + r'\b', l)] for n in PINS}
    pins = {k: v for k, v in pins.items() if v}
    m = {'label': label, 'arm': arm, 'scenario': scenario, 'measured': measured, 'mode': mode,
         'binary': binary, 'binary_sha256': sha(binary), 'context': ctx, 'wall_seconds': wall,
         'exit': p.returncode, 'result': r0, 'phases': phases, 'pinned': pins, 'seed': seed,
         'fallbacks': re.findall(r'\[graph\] fallback ([^\n]+)', log)}
    for k, pat in [('process_real_s', r'([\d.]+)\s+real'), ('rss_bytes', r'(\d+)\s+maximum resident set size'),
                   ('user_s', r'([\d.]+) user'), ('sys_s', r'([\d.]+) sys')]:
        z = re.search(pat, log); m[k] = float(z[1]) if z else None
    if r0.get('BaseGeneration') != r0.get('TargetGeneration'):
        f = wire(ctx, ns, r0['RunID'])
        m['graph_hash'] = f['normalized_hash']
        m['wire_run_id'] = f.get('run_id')
        m['wire_applied_generation'] = f.get('applied_generation')
    m['gate_observations'] = {
        'exit_zero': p.returncode == 0,
        'generation_advanced': r0.get('BaseGeneration') != r0.get('TargetGeneration'),
        'proof_bound': bool(pins.get('state_proof_bound')),
        'capture_reused': bool(pins.get('runtime_inputs_reused')),
        'proof_written': bool(pins.get('state_proof_written')),
        'proof_promoted': bool(pins.get('state_proof_promoted')),
        'proof_unavailable': bool(pins.get('state_proof_unavailable')),
        'parsed_files': r0.get('ParsedFiles'),
        'owners_published': r0.get('OwnersPublished'),
        'graph_hash_present': 'graph_hash' in m,
        'wire_run_id_matches_result': m.get('wire_run_id') == r0.get('RunID'),
    }
    rows.append(m); save()
    print('DONE', label, round(wall, 3), r0.get('ParsedFiles'), m.get('graph_hash'), flush=True)
    return m

# ---------------------------------------------------------------- snapshot/restore

def manifest(d):
    """sha256 and size of every regular file under d. The state dir's journals are part
    of what a measured delta starts from, so the manifest is the whole tree and not
    just state.json and the proof."""
    out = {}
    for f in sorted(d.rglob('*')):
        if f.is_file():
            out[str(f.relative_to(d))] = {'sha256': sha(f), 'bytes': f.stat().st_size}
    return out

def snapshot(name):
    """Taken with the broker stopped, so the JetStream store is at rest and the copy is
    a consistent set of retained messages rather than a torn one."""
    assert server is None and obs is None, 'snapshot must be taken with the broker stopped'
    assert (statedir / 'state-proof.json').is_file(), 'seed has no committed proof'
    dst = work / f'snapshot-{name}'
    if dst.exists(): shutil.rmtree(dst)
    dst.mkdir()
    for src, sub in [(statedir, 'state'), (natsstore, 'nats')]:
        subprocess.run(['cp', '-cR', str(src), str(dst / sub)], check=True)
    return {'state': manifest(dst / 'state'), 'nats': manifest(dst / 'nats')}

def restore(name, snap):
    """Both halves go back to the SAME paths they were taken from: a differently named
    state root can be refused by the proof and confounds startup."""
    assert server is None and obs is None, 'restore must happen with the broker stopped'
    src = work / f'snapshot-{name}'
    for dst, sub in [(statedir, 'state'), (natsstore, 'nats')]:
        assert work in dst.parents, dst
        if dst.exists(): shutil.rmtree(dst)
        subprocess.run(['cp', '-cR', str(src / sub), str(dst)], check=True)
    got = {'state': manifest(statedir), 'nats': manifest(natsstore)}
    assert got['state'] == snap['state'], 'restored state dir differs from the snapshot byte manifest'
    assert got['nats'] == snap['nats'], 'restored JetStream store differs from the snapshot byte manifest'
    return got['state']

# ---------------------------------------------------------------- experiment

try:
    thread.start()
    receipt_extra = {}
    for scenario in ['body', 'structural']:
        # --- seed, produced once, with the frozen binary, outside every measurement
        assert work in statedir.parents, statedir
        if statedir.exists(): shutil.rmtree(statedir)
        if natsstore.exists(): shutil.rmtree(natsstore)
        natsstore.mkdir(parents=True)
        start_broker(); start_observer()
        target.write_text(original)
        seeds = [run(f'seed-{scenario}-analyze', FROZEN, 'analyze', CTX, statedir, scenario=scenario)]
        if scenario == 'structural':
            target.write_text(body)
            seeds.append(run(f'seed-{scenario}-body', FROZEN, 'delta', CTX, statedir, scenario=scenario))
        last = seeds[-1]
        seed = {'run_id': last['result']['RunID'], 'graph_hash': last['graph_hash'],
                'base_generation': last['result']['BaseGeneration'],
                'target_generation': last['result']['TargetGeneration']}
        stop_all()
        snap = snapshot(scenario)
        seed['state_files'] = len(snap['state'])
        seed['state_sha256'] = snap['state']['state.json']['sha256']
        seed['proof_sha256'] = snap['state']['state-proof.json']['sha256']
        receipt_extra[f'seed-{scenario}'] = seed

        # --- the two arms, each from the identical restored seed and consumer graph
        for arm, binary in [('frozen', FROZEN), ('ablated', ABLATED)]:
            tag = f'{scenario}-{arm}'
            ident = restore(scenario, snap)
            start_broker()
            seq = stream_last_seq()
            observer = start_observer()
            replay = await_replay(seed)
            target.write_text(TEXT[scenario])
            m = run(tag, binary, 'delta', CTX, statedir, measured=True, arm=arm, scenario=scenario,
                    seed=seed)
            m['restored_state_manifest'] = ident
            m['observer'] = observer
            m['replay'] = replay
            m['stream_last_seq_before_delta'] = seq
            save()
            stop_all()

    # --- cold references, each in its own context and state dir, on a fresh store
    if natsstore.exists(): shutil.rmtree(natsstore)
    natsstore.mkdir(parents=True)
    start_broker(); start_observer()
    for scenario in ['body', 'structural']:
        target.write_text(TEXT[scenario])
        run(f'cold-{scenario}', FROZEN, 'analyze', f'stage21-ablate-cold-{scenario}',
            work / f'state-cold-{scenario}', scenario=scenario)
finally:
    target.write_text(original)
    stop.set()
    if thread.ident: thread.join()
    stop_all()
    save()
    (work / 'ablation-receipt.json').write_text(json.dumps({
        'label': 'bounded causal diagnostic; shared load; NOT acceptance timing and NOT a speed claim',
        'protocol': 'one canonical Product path/config/state-dir/context; seed produced once per '
                    'scenario with the frozen binary; broker stopped, full state dir and JetStream '
                    'store snapshotted together at rest; each arm restores both to the same paths, '
                    'starts a fresh broker and a fresh pinned replay observer over an empty output '
                    'file, and waits for the retained stream to be replayed into a consumer graph '
                    'whose run id, normalized hash and applied generation equal the seed before its '
                    'one measured delta',
        'frozen_binary': FROZEN, 'frozen_sha256': sha(FROZEN),
        'ablated_binary': ABLATED, 'ablated_sha256': sha(ABLATED),
        'ablation_diff': ABLATION_DIFF, 'ablation_diff_sha256': sha(ABLATION_DIFF),
        'product_sha': PRODUCT_SHA, 'profile': 'ts + product-graph-scope.yaml overlay',
        'observer': OBSERVER, 'observer_sha256': sha(OBSERVER), 'observer_diff': OBSERVER_DIFF,
        'seeds': receipt_extra,
        'runs_executed': len(rows), 'competing_samples': samples,
    }, indent=2) + '\n')
    print('COMPETING_SAMPLES', len(samples), flush=True)
