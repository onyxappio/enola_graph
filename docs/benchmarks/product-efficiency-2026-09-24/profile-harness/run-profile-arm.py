"""Body-only profiled delta pair runner for Stage15 overhead attribution.

One invocation = one arm, one repeat. Isolated work dir, own NATS + observer,
own APFS clone of the read-only Product fixture. Existing binaries only; no
build, no edit of any frozen tree. The only file written inside the clone is
the pinned control file, restored in finally.

Sequence per run, mirroring the r1-new sequence of the measured CLI series so
the body delta sits at the same generation and state as the run that regressed:
  initial   analyze, generation 0->1, NOT profiled
  noop      delta, generation stable, PROFILED (state read with zero parses)
  body      delta after the pinned edit, PROFILED (the measurement)
  cold-body analyze in a fresh state dir, NOT profiled, only with --oracle

ENOLA_GRAPH_PROFILE=1 instruments both arms equally, so these timings are a
decomposition diagnostic only: they are not comparable to the unprofiled
series and must not be republished as the regression figure.

Acceptance is all-or-nothing. A run is timing eligible only when the whole
sequence completed, every required gate ran and passed, every required profile
mark is present, and no competing workload was seen at start or in the 1s
samples. Anything else exits nonzero and keeps every file it wrote: failed
evidence is the finding, not garbage.
"""
import argparse, hashlib, importlib.util, json, os, pathlib, re, subprocess, threading, time, traceback

PIN_ORIGINAL = 'b940122ddebf67b8f46073ba98a5bb3e1c82d80a9a28ac1a48cc680b5c0f80c3'
PIN_BODY = 'b20431065c89e899cc2a17f1e7290dfe36f0d46224848c7e6f6a00af60cee971'
CONTROL = 'packages/crypto/src/password.ts'
OLD_LINE = 'return email.trim().toLowerCase();'
NEW_LINE = "return email.normalize('NFKC').trim().toLowerCase();"
SOURCE = pathlib.Path('/tmp/enola-stage9-md-timing/product')
# product_sha of the read-only fixture, as recorded in the wave14 pair receipts.
PIN_FIXTURE_HEAD = 'a609c19f3861971930fae7b33dcb2950598953c5'
BINARIES = {
    'baseline': pathlib.Path('/tmp/enola-stage15-independent/enola-wave14-baseline'),
    'candidate': pathlib.Path('/tmp/enola-stage15-independent/enola-stage15-wave14-candidate'),
}
# Digests as published in docs/benchmarks/product-efficiency-2026-09-24 provenance
# receipts, not read back from these files. A mismatch means the artifact under
# measurement is not the artifact that produced the reported series.
PIN_BINARIES = {
    'baseline': '5f5850dab6131aef717bf0f4439efb063201365d8b894858796c0d3913239801',
    'candidate': '320d12a90c39eaa34e97fb1edbad6e4d4890746bd9156a557a007f43cc058bb6',
}
OBSERVER = pathlib.Path('/tmp/enola-stage9-product-watch-control/bin/benchobserver')
PIN_OBSERVER = '9d4322e7fdeb7068b049ba0ded77aa043ada59396162c9d1286b4e29c3c4bd5f'
WATCH = '/tmp/enola-stage14-watch-pairs/watch.py'
HERE = pathlib.Path(__file__).resolve().parent
MARK = re.compile(r'^\[graph-profile\] (\S+)\s+([\d.]+)s(.*)$')
# Marks observed in the stage14 steady no-op profile; absence is a real finding
# about the instrumentation, so it fails the run rather than being tolerated.
# state_json_marshal is required on the body delta only: it is the main
# diagnostic there, and a no-op that writes no state need not emit it.
REQUIRED_MARKS = {
    'noop': ('state_read_bytes', 'state_fingerprint', 'state_json_unmarshal', 'graph_session_run'),
    'body': ('state_read_bytes', 'state_fingerprint', 'state_json_unmarshal', 'state_json_marshal',
             'graph_session_run'),
}
# Gates that must have RUN, whatever their verdict. A partial sequence cannot
# satisfy this set, so an exception mid-run can never leave the receipt green.
REQUIRED_CHECKS = ('quiet-slot', 'binary-pin', 'observer-pin', 'fixture-pin', 'control-pin',
                   'initial-fresh', 'initial-parses', 'noop-silent', 'body-scenario',
                   'marks-present', 'hash-gate', 'no-competitors')


def missing_marks(marks):
    """Required marks absent from each profiled label. Pure, so it is testable
    against synthetic mark rows without running a workload."""
    return {label: [m for m in want if m not in {r['mark'] for r in marks.get(label, [])}]
            for label, want in REQUIRED_MARKS.items()}


def eligibility(receipt, checks, samples):
    """Acceptance arithmetic, kept pure for the same reason. A run is eligible
    only if the sequence completed, every required gate ran and passed, nothing
    competed, and the control file went back to the pinned original."""
    ran = {c['key'] for c in checks}
    out = {'required_checks_missing': [k for k in REQUIRED_CHECKS if k not in ran]}
    out['required_checks_present'] = not out['required_checks_missing']
    out['all_checks_pass'] = bool(checks) and all(c['pass'] for c in checks)
    out['timing_eligible'] = bool(receipt.get('completed_sequence') and out['required_checks_present']
                                  and out['all_checks_pass'] and not samples
                                  and receipt.get('control_restored'))
    return out


def competitors():
    raw = subprocess.check_output(['ps', '-axo', 'pid=,ppid=,command='], text=True).splitlines()
    rows = []
    for line in raw:
        fields = line.strip().split(None, 2)
        if len(fields) == 3:
            rows.append((int(fields[0]), int(fields[1]), fields[2]))
    owned = {os.getpid()}
    while True:
        more = {pid for pid, parent, _ in rows if parent in owned}
        if more <= owned:
            break
        owned |= more
    hits = []
    for pid, parent, command in rows:
        if pid in owned:
            continue
        executable = command.split(None, 1)[0]
        name = pathlib.Path(executable).name
        go = (name == 'go' and re.search(r'\s(test|build|install|vet)\s', command))
        test = (name.endswith('.test') and '-test.' in command)
        tool = ('/pkg/tool/' in executable and name in ('compile', 'link', 'asm', 'cgo'))
        runtime = name.startswith('enola') or name == 'benchobserver'
        if go or test or tool or runtime:
            hits.append(str(pid) + ' ' + command)
    return hits


def parse_marks(text):
    out = []
    for line in text.splitlines():
        m = MARK.match(line.strip())
        if not m:
            continue
        rest = m.group(3)
        trace = re.search(r'trace=(\S+)', rest)
        total = re.search(r'total=\s*([\d.]+)s', rest)
        out.append({
            'mark': m.group(1),
            'seconds': float(m.group(2)),
            'trace': trace.group(1) if trace else None,
            'total_seconds': float(total.group(1)) if total else None,
            'detail': re.sub(r'total=\s*[\d.]+s|trace=\S+', '', rest).strip(),
        })
    return out


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument('--arm', choices=sorted(BINARIES), required=True)
    ap.add_argument('--repeat', type=int, required=True)
    ap.add_argument('--quiet-message-id', required=True,
                    help='coordinator message id granting the quiet slot, recorded in the receipt')
    ap.add_argument('--oracle', action='store_true', help='also compute this arm cold-body oracle hash')
    ap.add_argument('--expect-hash', help='body graph hash the run must reproduce (other arm oracle)')
    ap.add_argument('--keep', action='store_true', help='keep clone and state dirs even on a clean run')
    a = ap.parse_args()

    work = HERE / (a.arm + '-' + str(a.repeat))
    if work.exists():
        raise SystemExit('work dir already exists: ' + str(work))
    hits = competitors()
    if hits:
        raise SystemExit('competing workload present before start; refusing to measure:\n' + '\n'.join(hits))

    spec = importlib.util.spec_from_file_location('watch', WATCH)
    w = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(w)
    eff = w.eff
    scope_config = w.HERE / 'product-graph-scope.yaml'

    work.mkdir(parents=True)
    rows, checks, marks, samples = [], [], {}, []
    stop = threading.Event()
    receipt = {'arm': a.arm, 'repeat': a.repeat, 'binary': str(binary_path := BINARIES[a.arm]),
               'quiet_message_id': a.quiet_message_id,
               'pinned_original_sha256': PIN_ORIGINAL, 'pinned_body_sha256': PIN_BODY,
               'profile_env': 'ENOLA_GRAPH_PROFILE=1 on noop and body only',
               'completed_sequence': False,
               'started_utc': time.strftime('%Y-%m-%dT%H:%M:%SZ', time.gmtime())}

    def gate(key, desc, ok, observed=None, expected=None):
        entry = {'key': key, 'check': desc, 'pass': bool(ok)}
        if observed is not None:
            entry['observed'] = observed
        if expected is not None:
            entry['expected'] = expected
        checks.append(entry)
        if not ok:
            raise SystemExit('gate failed: ' + key + ' (' + desc + ')')
        return ok

    server = obs = None
    control = original = body = None
    repo = work / 'product'

    def sample():
        while not stop.wait(1):
            found = competitors()
            if found:
                samples.append(found)

    thread = threading.Thread(target=sample)
    try:
        gate('quiet-slot', 'quiet slot granted by a coordinator message id',
             bool(re.fullmatch(r'msg_[0-9a-f]{12}', a.quiet_message_id)), a.quiet_message_id)
        gate('hash-gate', 'run carries a graph hash gate (--oracle or --expect-hash)',
             bool(a.oracle or a.expect_hash))
        if a.expect_hash:
            gate('expect-hash-well-formed', 'expected hash is a 64 hex digest',
                 bool(re.fullmatch(r'[0-9a-f]{64}', a.expect_hash)), a.expect_hash)

        binary = binary_path
        digest = hashlib.sha256(binary.read_bytes()).hexdigest()
        receipt['binary_sha256'] = digest
        gate('binary-pin', 'arm binary matches its published digest',
             digest == PIN_BINARIES[a.arm], digest, PIN_BINARIES[a.arm])
        obs_digest = hashlib.sha256(OBSERVER.read_bytes()).hexdigest()
        receipt['observer_sha256'] = obs_digest
        gate('observer-pin', 'observer matches its published digest',
             obs_digest == PIN_OBSERVER, obs_digest, PIN_OBSERVER)

        subprocess.run(['cp', '-cR', str(SOURCE), str(repo)], check=True)
        head = subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=repo, text=True).strip()
        dirty = subprocess.check_output(['git', 'status', '--porcelain', '--untracked-files=no'], cwd=repo, text=True).strip()
        receipt['fixture_head'] = head
        gate('fixture-pin', 'clone is the pinned fixture revision with no tracked edits',
             head == PIN_FIXTURE_HEAD and not dirty, {'head': head, 'dirty': dirty}, PIN_FIXTURE_HEAD)

        control = repo / CONTROL
        original = control.read_text()
        original_digest = hashlib.sha256(original.encode()).hexdigest()
        body = original.replace(OLD_LINE, NEW_LINE)
        body_digest = hashlib.sha256(body.encode()).hexdigest()
        gate('control-pin', 'control file and its edited revision match both pins',
             original_digest == PIN_ORIGINAL and body != original and body_digest == PIN_BODY,
             {'original': original_digest, 'edited': body_digest},
             {'original': PIN_ORIGINAL, 'edited': PIN_BODY})

        cfg = work / 'config.yaml'
        cfg.write_text('repo: ' + str(repo) + '\nextractors: [typescript]\nexplainers: []\nrenderers: []\n\n' + scope_config.read_text())
        receipt['scope_sha256'] = hashlib.sha256(scope_config.read_bytes()).hexdigest()
        receipt['config_sha256'] = hashlib.sha256(cfg.read_bytes()).hexdigest()
        prefix = work.name

        def wire(ctx, start, run_id):
            until = time.monotonic() + 120
            while time.monotonic() < until:
                try:
                    lines = (work / 'consumer.jsonl').read_text().splitlines()
                except FileNotFoundError:
                    lines = []
                for line in lines:
                    try:
                        v = json.loads(line)
                    except json.JSONDecodeError:
                        continue
                    if v['context'] == ctx and v['first_ns'] >= start and v['run_id'] == run_id:
                        if not re.fullmatch(r'[0-9a-f]{64}', v.get('normalized_hash', '')):
                            raise SystemExit('invalid consumer graph digest')
                        return v
                time.sleep(.05)
            raise SystemExit('no completed consumer frame for ' + ctx)

        def state_bytes(path):
            total = 0
            for f in path.rglob('*'):
                try:
                    if f.is_file():
                        total += f.stat().st_size
                except FileNotFoundError:
                    pass
            return total

        def run(label, mode, state, ctx, profile, noop=False):
            statedir = work / state
            cmd = [str(binary), 'graph', mode, '--authoritative-scope', '--max-begin-bytes', '1048576',
                   '--repo-id', 'stage15-cli-product', '--summary-json', '--nats', url,
                   '--state-dir', str(statedir), '--context', ctx, str(cfg)]
            env = os.environ.copy()
            env.pop('ENOLA_GRAPH_PROFILE', None)
            if profile:
                env['ENOLA_GRAPH_PROFILE'] = '1'
            before = json.loads(subprocess.check_output([str(OBSERVER), url, '--info'])) if noop else None
            ns = time.time_ns()
            t = time.monotonic()
            with (work / (label + '.out')).open('w') as out, (work / (label + '.log')).open('w') as err:
                child = subprocess.Popen(['/usr/bin/time', '-l', *cmd], cwd=work, env=env,
                                         stdout=out, stderr=err, start_new_session=True)
                code = child.wait()
            seconds = time.monotonic() - t
            log = (work / (label + '.log')).read_text()
            if code:
                raise SystemExit(label + ' failed: ' + log[-1800:])
            row = {'label': label, 'profiled': profile, 'seconds': seconds, 'exit': code,
                   'state_final_bytes': state_bytes(statedir)}
            for key, pat in [('process_real_s', r'([\d.]+)\s+real'), ('rss_bytes', r'(\d+)\s+maximum resident set size'),
                             ('user_s', r'([\d.]+) user'), ('sys_s', r'([\d.]+) sys')]:
                z = re.search(pat, log)
                row[key] = float(z[1]) if z else None
            result_rows = json.loads((work / (label + '.out')).read_text())
            if len(result_rows) != 1:
                raise SystemExit('expected one repository summary for ' + label)
            row['result'] = result_rows[0]
            row['result'].pop('Facts', None)
            if row['result']['BaseGeneration'] != row['result']['TargetGeneration']:
                frame = wire(ctx, ns, row['result']['RunID'])
                row['graph_hash'] = frame['normalized_hash']
            if noop:
                after = json.loads(subprocess.check_output([str(OBSERVER), url, '--info']))
                row['noop_wire_messages'] = after['last_seq'] - before['last_seq']
            if profile:
                marks[label] = parse_marks(log)
                (work / ('marks-' + label + '.json')).write_text(json.dumps(marks[label], indent=2) + '\n')
            rows.append(row)
            (work / 'metrics.json').write_text(json.dumps(rows, indent=2) + '\n')
            print('DONE', label, round(seconds, 3), flush=True)
            return row

        port = eff.free_port()
        conf = eff.write_nats_conf(work, port)
        url = 'nats://127.0.0.1:' + str(port)
        with (work / 'nats.log').open('w') as log:
            server = subprocess.Popen([str(eff.NATS_SERVER), '-c', str(conf)], stdout=log, stderr=subprocess.STDOUT, start_new_session=True)
        eff.wait_port('127.0.0.1', port)
        oenv = os.environ.copy()
        oenv.pop('ENOLA_GRAPH_PROFILE', None)
        oenv['OBSERVER_READY_FILE'] = str(work / 'ready')
        oenv['OBSERVER_LIFECYCLE_FILE'] = str(work / 'lifecycle.jsonl')
        with (work / 'observer.stderr').open('w') as log:
            obs = subprocess.Popen([str(OBSERVER), url, str(work / 'consumer.jsonl')], stdout=log, stderr=log, env=oenv, start_new_session=True)
        eff.wait_observer_ready(work / 'ready', work / 'observer.stderr', obs)
        thread.start()

        ctx = prefix + '-main'
        control.write_text(original)
        init = run('initial', 'analyze', 'state1', ctx, profile=False)
        gate('initial-fresh', 'initial starts from an empty state',
             init['result']['BaseGeneration'] == 0 and init['result']['TargetGeneration'] == 1,
             {'base': init['result']['BaseGeneration'], 'target': init['result']['TargetGeneration']})
        gate('initial-parses', 'initial parses the whole fixture',
             init['result']['ParsedFiles'] == 4035, init['result']['ParsedFiles'], 4035)

        noop = run('noop', 'delta', 'state1', ctx, profile=True, noop=True)
        gate('noop-silent', 'noop parses nothing, holds generation and publishes nothing',
             noop['result']['ParsedFiles'] == 0
             and noop['result']['BaseGeneration'] == noop['result']['TargetGeneration']
             and noop['noop_wire_messages'] == 0,
             {'parsed': noop['result']['ParsedFiles'], 'base': noop['result']['BaseGeneration'],
              'target': noop['result']['TargetGeneration'], 'wire': noop['noop_wire_messages']})

        control.write_text(body)
        bodyrow = run('body', 'delta', 'state1', ctx, profile=True)
        gate('body-scenario', 'body delta is the same scenario as the measured series',
             bodyrow['result']['ParsedFiles'] == 11 and bodyrow['result']['OwnersPublished'] == 11,
             {'ParsedFiles': bodyrow['result']['ParsedFiles'], 'OwnersPublished': bodyrow['result']['OwnersPublished']},
             {'ParsedFiles': 11, 'OwnersPublished': 11})

        missing = missing_marks(marks)
        gate('marks-present', 'both profiled runs emitted every mark required for their label',
             not any(missing.values()),
             {'missing': missing, 'counts': {k: len(v) for k, v in marks.items()}},
             {k: list(v) for k, v in REQUIRED_MARKS.items()})
        # Repeated mark names are kept as occurrence counts here and as raw rows in
        # marks-<label>.json; nothing collapses them into one number.
        receipt['mark_names'] = {k: sorted({r['mark'] for r in v}) for k, v in marks.items()}
        receipt['mark_occurrences'] = {k: {n: sum(1 for r in v if r['mark'] == n)
                                           for n in sorted({r['mark'] for r in v})} for k, v in marks.items()}

        receipt['body_hash'] = bodyrow.get('graph_hash')
        if a.oracle:
            oracle = run('cold-body', 'analyze', 'cold-body', prefix + '-cold-body', profile=False)
            receipt['oracle_hash'] = oracle.get('graph_hash')
            gate('oracle-match', 'body delta equals this arm cold oracle',
                 bodyrow.get('graph_hash') is not None and bodyrow.get('graph_hash') == oracle.get('graph_hash'),
                 {'body': bodyrow.get('graph_hash'), 'oracle': oracle.get('graph_hash')})
        if a.expect_hash:
            gate('expect-hash-match', 'body delta equals the expected cross-build hash',
                 bodyrow.get('graph_hash') == a.expect_hash, bodyrow.get('graph_hash'), a.expect_hash)
        receipt['completed_sequence'] = True
    except BaseException as exc:
        receipt['error'] = {'type': type(exc).__name__, 'message': str(exc),
                            'traceback': traceback.format_exc().splitlines()[-12:]}
        raise
    finally:
        stop.set()
        if thread.ident:
            thread.join()
        if control is not None and original is not None:
            try:
                control.write_text(original)
                receipt['control_restored'] = hashlib.sha256(control.read_text().encode()).hexdigest() == PIN_ORIGINAL
            except Exception as exc:
                receipt['control_restored'] = False
                receipt['control_restore_error'] = str(exc)
        eff.stop_process(obs)
        eff.stop_process(server)
        receipt['competing_samples'] = samples
        checks.append({'key': 'no-competitors', 'pass': not samples,
                       'check': 'no competing workload in the 1s samples',
                       'observed': {'sample_count': len(samples)}})
        receipt['checks'] = checks
        receipt.update(eligibility(receipt, checks, samples))
        (work / 'receipt.json').write_text(json.dumps(receipt, indent=2) + '\n')
        print(json.dumps({k: receipt[k] for k in ('arm', 'repeat', 'completed_sequence',
                                                  'required_checks_present', 'all_checks_pass',
                                                  'timing_eligible')}), flush=True)
        if receipt['timing_eligible'] and not a.keep:
            for victim in [repo, work / 'state1', work / 'cold-body']:
                if victim.exists():
                    subprocess.run(['rm', '-rf', str(victim)], check=False)
            print('CLEANED clone and state dirs; logs, marks, metrics and receipt kept in ' + str(work), flush=True)
        else:
            print('KEPT everything in ' + str(work) + ' (not timing eligible, or --keep)', flush=True)
    if not receipt['timing_eligible']:
        raise SystemExit('run not timing eligible; see ' + str(work / 'receipt.json'))


if __name__ == '__main__':
    main()
