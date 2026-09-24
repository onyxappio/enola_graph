"""Correctness-only validation of the last 10 first-parent Product transitions.

One isolated checkout is walked forward along the pinned first-parent chain
ending a609c19f3861971930fae7b33dcb2950598953c5. At every commit:

  chain    candidate analyze (first commit) or delta (every later commit),
           into one state directory that carries the whole history
  noop     candidate delta again: zero parses, held generation, zero wire
           messages, byte-identical state.json
  cold-c   candidate analyze into a fresh state directory at this same commit
  cold-b   baseline analyze into a fresh state directory at this same checkout

and the step is accepted only when the chain hash equals the candidate cold
hash, the no-op is silent, the checkout is the pinned commit and the checkout
is clean both before and after the runs.

This is correctness only. Nothing here is timed, no duration is recorded as a
result, the arms are interleaved on a shared host, and no number produced by
this harness may be used as a performance figure.

Both arms run against the SAME physical checkout with the SAME policy overlay
at the SAME commit, so the baseline/candidate cold hash comparison rests on
inputs that are proved identical rather than assumed. If the baseline arm is
skipped (--no-baseline) the series reports candidate self-equivalence only.

Nothing outside this directory and its work tree is written. The pinned
sources, the root review tree and the frozen candidate tree are read only.
"""
import argparse, hashlib, importlib.util, json, os, pathlib, re, shutil, subprocess, sys, time, traceback

import gates

HERE = pathlib.Path(__file__).resolve().parent
SOURCE = pathlib.Path('/tmp/enola-stage16-root-review/product-source')
BINARIES = {}
PIN_BINARIES = {}

# History is a correctness prerequisite, not performance acceptance.
# Pin frozen source here; final timing retains its independent full acceptance gate.
def load_final_pins():
    acceptance = json.loads((HERE.parent / 'product-final-source-equivalence.json').read_text())
    pins = json.loads((HERE.parent / 'cli-pairs/pins.json').read_text())
    if acceptance.get('source_revision') != 'd988437efb25ea13c6ba47e30ab22ca81a14b58d' or acceptance.get('source_mismatches') != [] or acceptance.get('binary_sha256') != pins['binaries']['candidate']['sha256']:
        raise SystemExit('frozen Stage18 source/binary receipt mismatch')
    BINARIES.update({arm: pathlib.Path(v['path']) for arm, v in pins['binaries'].items()})
    PIN_BINARIES.update({arm: v['sha256'] for arm, v in pins['binaries'].items()})

POLICY = pathlib.Path('/Users/oleksandr.mykulych/orca/enola_graph/docs/benchmarks/'
                      'product-efficiency-2026-09-22/product-graph-scope.yaml')
PIN_POLICY = 'b81b19598dd74c63084ae11488cb7f4fa7df783b0887d2c42dcf61dc67890bf7'
OBSERVER = pathlib.Path('/tmp/enola-stage9-product-watch-control/bin/benchobserver')
PIN_OBSERVER = '9d4322e7fdeb7068b049ba0ded77aa043ada59396162c9d1286b4e29c3c4bd5f'
PIN_NATS = '47a95067f10358c7cd7cdf56f5e840c5aa17ed0be91fcf88170cc876d2be02af'
EFF = pathlib.Path('/Users/oleksandr.mykulych/orca/enola_graph/docs/benchmarks/'
                   'product-efficiency-2026-09-22/run.py')
REPO_ID = 'stage18-history-product'

# The 11 first-parent commits, oldest first, giving the 10 transitions. Pinned
# here and asserted against git so a different history can never be validated
# under this name.
COMMITS = (
    '4d104e600f89cfc0d84a93c02a6635191805b7d9',
    '07fb4a41ddafff7f42ebd55af8a23fe5739f4cd7',
    'fec1eac346c48dbc68072803d89b2e4709f9bd42',
    '9fc7ae5b4c3fc4fc266b24b1df0ef2bfcb1f7030',
    '1c2607479b6d0c6a0ae329f72eea20af0ff5d895',
    '599575d0aa398615cde6a4ac12bc0c7734a686d9',
    'ae233c5f56959ce5852c8381edd6cb472c4b9f95',
    '4168360e2e7f5140115ea5af58db62afd4dc9f43',
    'a6f1f3a91a36ea3dead786560412a4944a009694',
    '5dfb2c8f276d99a710d739c11339ee5d9b9a0347',
    'a609c19f3861971930fae7b33dcb2950598953c5',
)
HEAD_PIN = COMMITS[-1]


def sha256_file(path):
    h = hashlib.sha256()
    with open(path, 'rb') as f:
        for chunk in iter(lambda: f.read(1 << 20), b''):
            h.update(chunk)
    return h.hexdigest()


def git(repo, *args):
    return subprocess.check_output(['git', *args], cwd=str(repo), text=True).strip()


def load_eff():
    spec = importlib.util.spec_from_file_location('efficiency_run', EFF)
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod


class Runner:
    def __init__(self, work, url, observer, consumer, logdir):
        self.work, self.url, self.observer = work, url, observer
        self.consumer, self.logdir = consumer, logdir
        self.seq = 0

    def streaminfo(self):
        return json.loads(subprocess.check_output([str(self.observer), self.url, '--info']))

    def frame(self, ctx, start_ns, run_id):
        """The completed consumer frame for one published run, with its digest."""
        until = time.monotonic() + 180
        while time.monotonic() < until:
            for line in self.consumer.read_text().splitlines():
                try:
                    v = json.loads(line)
                except json.JSONDecodeError:
                    continue
                if v.get('context') == ctx and v.get('first_ns', 0) >= start_ns and v.get('run_id') == run_id:
                    if not gates.hash_ok(v.get('normalized_hash', '')):
                        raise RuntimeError('invalid consumer graph digest for ' + ctx)
                    return v
            time.sleep(0.05)
        raise RuntimeError('no completed consumer frame for ' + ctx)

    def graph(self, label, binary, mode, statedir, ctx, config, noop=False):
        """One CLI invocation. Unique log files per invocation, exit code checked."""
        self.seq += 1
        tag = '%03d-%s' % (self.seq, label)
        cmd = [str(binary), 'graph', mode, '--authoritative-scope',
               '--max-begin-bytes', '1048576', '--repo-id', REPO_ID, '--summary-json',
               '--nats', self.url, '--state-dir', str(statedir), '--context', ctx, str(config)]
        statefile = statedir / 'state.json'
        before_seq = self.streaminfo()['last_seq'] if noop else None
        before_state = sha256_file(statefile) if noop else None
        out_path, err_path = self.logdir / (tag + '.out'), self.logdir / (tag + '.log')
        start_ns = time.time_ns()
        with out_path.open('w') as out, err_path.open('w') as err:
            child = subprocess.Popen(cmd, cwd=str(self.work), stdout=out, stderr=err,
                                     start_new_session=True)
            try:
                code = child.wait(timeout=1800)
            except subprocess.TimeoutExpired:
                os.killpg(child.pid, 9)
                child.wait()
                raise RuntimeError(tag + ' exceeded its 1800s bound')
        log = err_path.read_text()
        if code != 0:
            raise RuntimeError(tag + ' exited ' + str(code) + ': ' + log[-1800:])
        rows = json.loads(out_path.read_text())
        if len(rows) != 1:
            raise RuntimeError(tag + ' did not print exactly one repository summary')
        result = rows[0]
        result.pop('Facts', None)
        record = {'label': tag, 'cmd': cmd, 'exit': code, 'result': result,
                  'fallbacks': re.findall(r'\[graph\] fallback ([^\n]+)', log)}
        published = result['BaseGeneration'] != result['TargetGeneration']
        record['published'] = published
        if published:
            record['graph_hash'] = self.frame(ctx, start_ns, result['RunID'])['normalized_hash']
        if noop:
            record['wire_messages'] = self.streaminfo()['last_seq'] - before_seq
            record['state_before'] = before_state
            record['state_after'] = sha256_file(statefile)
        print('RAN', tag, result['BaseGeneration'], '->', result['TargetGeneration'],
              'parsed', result.get('ParsedFiles'), flush=True)
        return record


def write_config(path, repo):
    overlay = POLICY.read_text()
    if re.search(r'^repo(?:s)?\s*:', overlay, re.M):
        raise RuntimeError('policy overlay must not set the repository')
    path.write_text('repo: ' + str(repo) + '\n\n' + overlay)
    return path


def rmtree_exact(path, work):
    """Remove one directory this harness created, by exact path, never a glob.

    Only a direct child of this run's own work directory can be removed, and
    only when it really is a directory. Every call names a state directory this
    process created moments earlier.
    """
    path = pathlib.Path(path).resolve()
    if path.parent != pathlib.Path(work).resolve() or not path.is_dir():
        raise RuntimeError('refusing to remove ' + str(path))
    shutil.rmtree(path)


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument('--no-baseline', action='store_true',
                    help='skip the baseline arm; the series then reports candidate '
                         'self-equivalence only')
    ap.add_argument('--limit', type=int, default=len(COMMITS),
                    help='validate only the first N commits of the chain (smoke use)')
    ap.add_argument('--tag', default='main',
                    help='names this run: work-<tag>/, logs-<tag>/, receipt-<tag>.json')
    ap.add_argument('--keep-states', action='store_true',
                    help='keep every state directory instead of dropping cold states '
                         'once their hash is captured')
    args = ap.parse_args()
    load_final_pins()
    commits = COMMITS[:max(2, min(args.limit, len(COMMITS)))]

    if not re.fullmatch(r'[a-z0-9-]{1,32}', args.tag):
        raise SystemExit('--tag must be a short lowercase slug')
    work = HERE / ('work-' + args.tag)
    receipt_path = HERE / ('receipt-' + args.tag + '.json')
    if work.exists() or receipt_path.exists():
        raise SystemExit(str(work) + ' or its receipt already exists; move it aside rather '
                         'than overwriting evidence')
    logdir = HERE / ('logs-' + args.tag)
    logdir.mkdir(exist_ok=True)
    work.mkdir(parents=True)
    receipt = {'purpose': 'correctness-only history validation; no timing claim of any kind',
               'started_utc': time.strftime('%Y-%m-%dT%H:%M:%SZ', time.gmtime()),
               'source': str(SOURCE), 'expected_commits': list(commits),
               'baseline_arm': not args.no_baseline,
               'series_gates': [], 'steps': [], 'completed_sequence': False}
    checks = receipt['series_gates']

    receipt['tag'] = args.tag

    def save():
        receipt_path.write_text(json.dumps(receipt, indent=2) + '\n')

    eff = load_eff()
    server = obs = None
    try:
        digests = {name: sha256_file(p) for name, p in BINARIES.items()}
        checks.append(gates.gate('candidate-binary-pin',
                                 digests['candidate'] == PIN_BINARIES['candidate'],
                                 {'measured': digests['candidate'], 'pinned': PIN_BINARIES['candidate']}))
        if not args.no_baseline:
            checks.append(gates.gate('baseline-binary-pin',
                                     digests['baseline'] == PIN_BINARIES['baseline'],
                                     {'measured': digests['baseline'], 'pinned': PIN_BINARIES['baseline']}))
        policy_sha = sha256_file(POLICY)
        checks.append(gates.gate('policy-pin', policy_sha == PIN_POLICY,
                                 {'measured': policy_sha, 'pinned': PIN_POLICY, 'path': str(POLICY)}))
        observer_sha = sha256_file(OBSERVER)
        checks.append(gates.gate('observer-pin', observer_sha == PIN_OBSERVER,
                                 {'measured': observer_sha, 'pinned': PIN_OBSERVER}))
        nats_sha = sha256_file(eff.NATS_SERVER)
        checks.append(gates.gate('nats-pin', nats_sha == PIN_NATS,
                                 {'measured': nats_sha, 'pinned': PIN_NATS}))
        receipt['binary_sha256'] = digests
        receipt['policy_sha256'] = policy_sha

        head = git(SOURCE, 'rev-parse', 'HEAD')
        dirty = git(SOURCE, 'status', '--porcelain', '--ignored=traditional')
        checks.append(gates.gate('source-head-pin', head == HEAD_PIN and dirty == '',
                                 {'head': head, 'pinned': HEAD_PIN, 'porcelain': dirty}))
        actual = git(SOURCE, 'log', '--first-parent', '-n', str(len(COMMITS)),
                     '--format=%H', HEAD_PIN).splitlines()
        checks.append(gates.gate('commit-chain-pin', tuple(reversed(actual)) == COMMITS,
                                 {'git_first_parent_oldest_first': list(reversed(actual))}))
        save()
        if any(not c['pass'] for c in checks):
            raise RuntimeError('pin gate failed before any run; see ' + str(receipt_path))

        repo = work / 'product'
        print('CLONE', repo, flush=True)
        subprocess.check_call(['cp', '-Rc', str(SOURCE), str(repo)])
        config = write_config(work / 'graph-config.yaml', repo)
        receipt['config'] = config.read_text()
        receipt['checkout'] = str(repo)

        port = eff.free_port()
        conf = eff.write_nats_conf(work, port)
        url = 'nats://127.0.0.1:' + str(port)
        with (work / 'nats.log').open('w') as log:
            server = subprocess.Popen([str(eff.NATS_SERVER), '-c', str(conf)],
                                      stdout=log, stderr=subprocess.STDOUT, start_new_session=True)
        eff.wait_port('127.0.0.1', port)
        consumer = work / 'consumer.jsonl'
        oenv = dict(os.environ)
        oenv.pop('ENOLA_GRAPH_PROFILE', None)
        oenv['OBSERVER_READY_FILE'] = str(work / 'ready')
        oenv['OBSERVER_LIFECYCLE_FILE'] = str(work / 'lifecycle.jsonl')
        with (work / 'observer.stderr').open('w') as log:
            obs = subprocess.Popen([str(OBSERVER), url, str(consumer)], stdout=log, stderr=log,
                                   env=oenv, start_new_session=True)
        eff.wait_observer_ready(work / 'ready', work / 'observer.stderr', obs)

        runner = Runner(work, url, OBSERVER, consumer, logdir)
        chain_state = work / 'chain-state'
        chain_ctx = 'stage18-history-chain'
        last_hash = None

        for index, commit in enumerate(commits):
            short = commit[:8]
            kind = 'initial' if index == 0 else 'transition'
            label = '%02d-%s-%s' % (index, kind, short)
            print('STEP', label, flush=True)
            step = {'label': label, 'kind': kind, 'index': index, 'expected_commit': commit,
                    'completed': False, 'gates': []}
            receipt['steps'].append(step)
            subprocess.check_call(['git', 'checkout', '-q', '--detach', commit], cwd=str(repo))
            step['head'] = git(repo, 'rev-parse', 'HEAD')
            step['status_before'] = git(repo, 'status', '--porcelain', '--ignored=traditional')
            step['subject'] = git(repo, 'log', '-1', '--format=%s', commit)

            mode = 'analyze' if index == 0 else 'delta'
            chain = runner.graph(label + '-chain', BINARIES['candidate'], mode,
                                 chain_state, chain_ctx, config)
            step['chain'] = {k: chain[k] for k in ('label', 'result', 'published', 'fallbacks')}
            step['chain']['graph_hash'] = chain.get('graph_hash')
            step['published'] = chain['published']
            if index == 0:
                checks.append(gates.gate('chain-initial-fresh',
                                         chain['result']['BaseGeneration'] == 0
                                         and chain['result']['TargetGeneration'] == 1,
                                         {'base': chain['result']['BaseGeneration'],
                                          'target': chain['result']['TargetGeneration']}))
            if chain['published']:
                last_hash = chain['graph_hash']
            step['effective_hash'] = last_hash

            noop = runner.graph(label + '-noop', BINARIES['candidate'], 'delta',
                                chain_state, chain_ctx, config, noop=True)
            step['noop'] = {'parsed_files': noop['result'].get('ParsedFiles'),
                            'base_generation': noop['result']['BaseGeneration'],
                            'target_generation': noop['result']['TargetGeneration'],
                            'wire_messages': noop['wire_messages'],
                            'state_before': noop['state_before'],
                            'state_after': noop['state_after']}

            cold_state = work / ('cold-candidate-%02d' % index)
            cold = runner.graph(label + '-cold-candidate', BINARIES['candidate'], 'analyze',
                                cold_state, 'stage18-history-cold-candidate-%02d' % index, config)
            if not cold['published']:
                raise RuntimeError('cold candidate analyze published nothing at ' + short)
            step['candidate_cold_hash'] = cold['graph_hash']

            if not args.no_baseline:
                base_state = work / ('cold-baseline-%02d' % index)
                base = runner.graph(label + '-cold-baseline', BINARIES['baseline'], 'analyze',
                                    base_state, 'stage18-history-cold-baseline-%02d' % index, config)
                if not base['published']:
                    raise RuntimeError('cold baseline analyze published nothing at ' + short)
                step['baseline_cold_hash'] = base['graph_hash']
                if not args.keep_states:
                    rmtree_exact(base_state, work)
            if not args.keep_states:
                rmtree_exact(cold_state, work)

            step['status_after'] = git(repo, 'status', '--porcelain', '--ignored=traditional')
            step['gates'] = gates.step_gates(step)
            step['completed'] = True
            step.update(gates.step_verdict(step))
            save()
            print('STEP-DONE', label, 'accepted' if step['accepted'] else 'REJECTED', flush=True)

        receipt['completed_sequence'] = True
    except BaseException as error:
        receipt['error'] = {'message': str(error), 'traceback': traceback.format_exc()[-4000:]}
        raise
    finally:
        for child in (obs, server):
            if child is not None:
                try:
                    eff.stop_process(child)
                except Exception as stop_error:
                    receipt.setdefault('teardown_errors', []).append(str(stop_error))
        receipt['problems'] = gates.series_problems(receipt, commits)
        receipt['accepted'] = not receipt['problems']
        receipt['baseline_claim'] = gates.baseline_claim(receipt)
        receipt['finished_utc'] = time.strftime('%Y-%m-%dT%H:%M:%SZ', time.gmtime())
        save()
        print(json.dumps({'accepted': receipt['accepted'], 'problems': receipt['problems'][:10],
                          'baseline_claim': receipt['baseline_claim'],
                          'receipt': str(receipt_path)}, indent=2), flush=True)
    return 0 if receipt['accepted'] else 1


if __name__ == '__main__':
    sys.exit(main())
