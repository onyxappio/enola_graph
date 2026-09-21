"""Bounded scratch-only probes: --scope-tool BUILT_HELPER [--old PINNED_BINARY]."""
import argparse
import ast
import json
import contextlib
import io
import time
import subprocess
import tempfile
from pathlib import Path
from harness_support import LegacyMirror, restore_all, wait_process


def cleanup_probe():
    called = []

    class FakePath:
        def __init__(self, name, fail=False):
            self.name, self.fail = name, fail

        def __str__(self):
            return self.name

        def write_bytes(self, data):
            called.append(self.name)
            if self.fail:
                raise OSError('injected restore failure')

    def fail_stop(proc):
        called.append('stop')
        raise OSError('injected process cleanup failure')

    resident = ast.parse(Path(__file__).with_name('resident.py').read_text())
    cleanup = resident.body[-1].finalbody
    env = dict(restore_all=restore_all, stop_process=fail_stop, proc=None,
               f=FakePath('source', True), original='original',
               ignored_backups={FakePath('lock'): b'lock', FakePath('media'): b'media'})
    try:
        exec(compile(ast.Module(body=cleanup, type_ignores=[]), '<resident cleanup>', 'exec'), env)
    except RuntimeError as error:
        assert 'injected restore failure' in str(error)
        assert 'injected process cleanup failure' in str(error)
    else:
        raise AssertionError('cleanup failure did not fail acceptance')
    assert called == ['stop', 'source', 'lock', 'media'], called
    print('PASS actual resident finally attempts every restore and fails acceptance')


def mirror_probe(scope_tool, old):
    with tempfile.TemporaryDirectory(prefix='enola-harness-probe-') as scratch:
        root = Path(scratch)
        repo = root / 'source'
        output = root / 'evidence'
        repo.mkdir(); output.mkdir()
        subprocess.run(['git', 'init', '-q', str(repo)], check=True)
        files = {
            'src/main.ts': 'export const x = 1;\n',
            'src/main.test.ts': 'import { x } from "./main"; console.log(x);\n',
            'private.ts': 'export const privateValue = 3;\n',
            'tracked.ts': 'export const tracked = 4;\n',
            'ignored.ts': 'export const ignored = 5;\n',
            '.gitignore': 'ignored.ts\ntracked.ts\n',
            'nested/.gitignore': '*.ts\n!keep.ts\n',
            'nested/drop.ts': 'export const drop = 1;\n',
            'nested/keep.ts': 'export const keep = 1;\n',
            'package.json': '{"name":"scratch","dependencies":{"example":"^1.0.0"}}',
            'pnpm-lock.yaml': 'excluded lock bytes',
            'excluded[dir]/hidden.ts': 'export const hidden = 1;\n',
            'assets/icon.png': 'opaque exact bytes\x00',
        }
        for path, data in files.items():
            target = repo / path
            target.parent.mkdir(parents=True, exist_ok=True)
            target.write_bytes(data.encode())
        (repo / 'empty').mkdir()
        subprocess.run(['git', 'add', '-f', 'tracked.ts', 'private.ts'], cwd=repo, check=True)
        config = root / 'candidate.yaml'
        original_ignores = ['**/*.test.ts', '.enola/**']
        effective_ignores = original_ignores + ['**/.enola/**']
        config.write_text('repo: ' + str(repo) + '\nextractors: [typescript]\nexplainers: []\nrenderers: []\nignore: ' + json.dumps(original_ignores) + '\ngraph_inputs:\n  exclude: ["private.ts", "excluded*/**"]\n')
        mirror = LegacyMirror(repo, output, scope_tool, config)
        old_config = mirror.sync('initial', 1, initial=True)
        first = json.loads((output / 'initial-selected-inputs.json').read_text())
        assert first['config']['ignore'] == effective_ignores, first['config']['ignore']
        approximate = json.loads(subprocess.check_output([
            scope_tool, '--repo', str(repo), '--config', str(config)]))
        assert approximate['ignore'][:len(effective_ignores)] == effective_ignores
        assert {'private.ts', 'ignored.ts', 'nested/drop.ts', 'pnpm-lock.yaml',
                'src/main.test.ts', '.git/**', 'excluded\\[dir]/hidden.ts'} <= set(approximate['ignore'])
        selected = first['physical_file_sha256']
        assert {'src/main.ts', 'tracked.ts', 'nested/keep.ts', 'package.json', 'assets/icon.png'} <= selected.keys()
        assert not {'src/main.test.ts', 'private.ts', 'ignored.ts', 'nested/drop.ts', 'pnpm-lock.yaml', 'excluded[dir]/hidden.ts'} & selected.keys()
        assert not (mirror.root / '.git').exists()
        assert (mirror.root / 'empty').is_dir()
        for name in selected:
            assert (mirror.root / name).read_bytes() == (repo / name).read_bytes()
            assert (mirror.root / name).stat().st_ino != (repo / name).stat().st_ino
        if old:
            result = subprocess.run([old, '--generate', str(old_config)], capture_output=True, text=True, timeout=45)
            if result.returncode:
                raise RuntimeError(result.stderr[-4000:])
            mirror.evidence('initial')
            evidence = json.loads((output / 'initial-inventory-evidence.json').read_text())
            assert evidence['main_inventory_matches_candidate']
        else:
            out = mirror.root / '.enola/old-output-1'
            out.mkdir(parents=True)
        sentinel = mirror.root / '.enola/old-output-1/cache-sentinel'
        sentinel.write_text('keep cache')
        (repo / 'src/main.ts').write_text('export const renamed = 2;\n')
        (repo / 'nested/keep.ts').unlink()
        (repo / 'new.ts').write_text('export const added = 3;\n')
        delta_config = mirror.sync('delta', 1)
        assert json.loads(delta_config.read_text())['ignore'] == effective_ignores
        if old:
            result = subprocess.run([old, '--generate', str(delta_config)], capture_output=True, text=True, timeout=45)
            if result.returncode:
                raise RuntimeError(result.stderr[-4000:])
            mirror.evidence('delta')
        assert not (mirror.root / 'nested/keep.ts').exists()
        assert (mirror.root / 'new.ts').read_bytes() == (repo / 'new.ts').read_bytes()
        assert (mirror.root / 'src/main.ts').read_bytes() == (repo / 'src/main.ts').read_bytes()
        assert sentinel.read_text() == 'keep cache'
        try:
            mirror.sync('reused', 1, initial=True)
        except RuntimeError as error:
            assert 'already exists' in str(error)
        else:
            raise AssertionError('reused initial output accepted')
        mirror.sync('initial2', 2, initial=True)
        assert sentinel.read_text() == 'keep cache'
        (repo / 'link.ts').symlink_to('src/main.ts')
        rejected = subprocess.run([scope_tool, '--repo', str(repo), '--config', str(config), '--export-selected'], capture_output=True)
        assert rejected.returncode and b'regular files' in rejected.stderr
        print('PASS original export ignores only, config-only escaped exclusions, selected scope, excluded tests/lock/private inputs, tracked Git exemption, nested negation, copied bytes, history sync, unique output, symlink rejection' + (' and pinned legacy main inventory' if old else ''))


def console_probe():
    primary = {'ParsedFiles': 1, 'BaseGeneration': 1, 'TargetGeneration': 1,
               'Work': {'InventoryScans': 0}, 'Stats': {'Events': 0}, 'Reconciled': False}
    catchup = dict(primary, ParsedFiles=3, Reconciled=True, FallbackReason='catch-up reason')
    event = {'result': primary, 'coverage_catchup': [catchup]}
    for name in ('resident.py', 'resident_history.py'):
        tree = ast.parse(Path(__file__).with_name(name).read_text())
        function = next(node for node in tree.body if isinstance(node, ast.FunctionDef) and node.name == 'online')
        env = dict(time=time, receive=lambda:event, streaminfo=lambda:{'last_seq': 0},
                   hashes={}, rows=[], checks=[], save=lambda:None)
        exec(compile(ast.Module(body=[function], type_ignores=[]), '<resident online>', 'exec'), env)
        console = io.StringIO()
        with contextlib.redirect_stdout(console):
            result = env['online']('probe', 'context')
        assert result['aggregate_parsed'] == 4
        assert 'parsed 4 reconciliations 1' in console.getvalue(), console.getvalue()
        assert 'catch-up reason' in console.getvalue()
    print('PASS both resident console summaries include catch-up parses and fallback')


def wait_probe():
    child = subprocess.Popen(['python3', '-c', 'pass'], start_new_session=True)
    wait_process(child, 5)
    assert child.returncode == 0
    child = subprocess.Popen(['python3', '-c', 'import time; time.sleep(10)'], start_new_session=True)
    try:
        wait_process(child, .05)
    except subprocess.TimeoutExpired:
        assert child.poll() is not None
    else:
        raise AssertionError('timeout was not enforced')
    print('PASS blocking process wait and timeout reap')


if __name__ == '__main__':
    parser = argparse.ArgumentParser()
    parser.add_argument('--scope-tool', required=True)
    parser.add_argument('--old')
    args = parser.parse_args()
    cleanup_probe()
    console_probe()
    mirror_probe(args.scope_tool, args.old)
    wait_probe()
