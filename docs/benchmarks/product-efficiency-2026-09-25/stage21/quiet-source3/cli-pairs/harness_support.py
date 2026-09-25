"""Shared benchmark mechanics; all mirror setup and evidence collection is untimed."""
from pathlib import Path, PurePosixPath
import hashlib
import json
import os
import shutil
import signal
import subprocess
import tempfile
import threading


def wait_process(child, timeout):
    """Blocking wait avoids POSIX timeout-wait polling in the measured interval."""
    expired = threading.Event()

    def kill():
        if child.poll() is None:
            expired.set()
            try:
                os.killpg(child.pid, signal.SIGKILL)
            except ProcessLookupError:
                pass

    timer = threading.Timer(timeout, kill)
    timer.daemon = True
    timer.start()
    try:
        child.wait()
        if expired.is_set():
            raise subprocess.TimeoutExpired(child.args, timeout)
    finally:
        timer.cancel()
        timer.join()


def restore_all(actions):
    """Try every cleanup, then fail the run if any cleanup failed."""
    errors = []
    for label, action in actions:
        try:
            action()
        except BaseException as error:
            errors.append((label, error))
    if errors:
        raise RuntimeError('Cleanup failed: ' + '; '.join(
            f'{label}: {error}' for label, error in errors)) from errors[0][1]


def digest(data):
    return hashlib.sha256(data).hexdigest()


class LegacyMirror:
    """A newly owned physical tree containing only candidate-selected repo inputs.

    The mirror is retained as evidence. It has no Git metadata or dependency
    installation and is not an oracle for identical extractor semantics or for
    external side reads. Unsupported selected symlinks are rejected by export.
    """
    def __init__(self, repo, output, scope_tool, config):
        self.repo = Path(repo).resolve()
        self.output = Path(output).resolve()
        if self.output == self.repo or self.repo in self.output.parents:
            raise RuntimeError('Legacy mirror output must be outside the candidate checkout')
        self.scope_tool = str(scope_tool)
        self.config = str(config)
        self.container = Path(tempfile.mkdtemp(prefix='legacy-mirror-', dir=self.output))
        self.root = self.container / self.repo.name
        self.root.mkdir()
        self.previous = []
        self.settings = None
        self.candidate_inventory = None

    @staticmethod
    def relative(value):
        path = PurePosixPath(value)
        if not value or path.is_absolute() or '..' in path.parts or str(path) != value:
            raise RuntimeError('Unsafe selected path: ' + value)
        if path.parts[0] == '.enola':
            raise RuntimeError('Selected input collides with reserved legacy output')
        return path

    def sync(self, label, iteration, initial=False):
        exported = json.loads(subprocess.check_output([
            self.scope_tool, '--repo', str(self.repo), '--config', self.config,
            '--export-selected']))
        if exported.get('version') != 1 or Path(exported['repo']).resolve() != self.repo:
            raise RuntimeError('Unexpected selected-input export')
        selected = exported['selected']
        paths = [str(self.relative(row['path'])) for row in selected]
        if len(paths) != len(set(paths)):
            raise RuntimeError('Duplicate selected input')
        # Remove only inputs previously placed by this object. Keep legacy cache.
        for row in reversed(self.previous):
            path = self.root / row['path']
            if row['kind'] == 'directory':
                path.rmdir()
            else:
                path.unlink()
        for row in selected:
            rel = self.relative(row['path'])
            source, target = self.repo / rel, self.root / rel
            if row['kind'] == 'directory':
                target.mkdir(parents=True, exist_ok=True)
                continue
            if row['kind'] not in ('semantic', 'name_only') or source.is_symlink() or not source.is_file():
                raise RuntimeError('Unsupported selected input: ' + str(rel))
            data = source.read_bytes()
            if digest(data) != row['sha256']:
                raise RuntimeError('Source changed after selected-input export: ' + str(rel))
            target.parent.mkdir(parents=True, exist_ok=True)
            target.write_bytes(data)
            target.chmod(row['mode'])
        self.previous = selected
        self.candidate_inventory = exported['candidate_inventory']
        self.settings = exported['config']
        self.settings['repo'] = str(self.root)
        self.settings.pop('repos', None)
        self.settings.setdefault('output', {})['dir'] = f'.enola/old-output-{iteration}'
        destination = self.root / self.settings['output']['dir']
        if initial and destination.exists():
            raise RuntimeError('Legacy initial output already exists')
        # Compare physical availability, not just the generated ignore config.
        actual = self.inventory()
        expected = {row['path']: row['sha256'] for row in selected if row['kind'] != 'directory'}
        if actual != expected:
            raise RuntimeError('Legacy physical input manifest differs from selected inputs')
        head = subprocess.run(['git', 'rev-parse', '--verify', 'HEAD'], cwd=self.repo,
                              text=True, capture_output=True)
        exported.update({'source_head': head.stdout.strip() if head.returncode == 0 else None,
                         'candidate_config_sha256': digest(Path(self.config).read_bytes()),
                         'mirror': str(self.root), 'physical_file_sha256': actual,
                         'physical_manifest_sha256': digest(json.dumps(actual, sort_keys=True).encode()),
                         'comparison': 'selected-repository-input mirror; legacy semantics and external side reads not proven equivalent'})
        (self.output / (label + '-selected-inputs.json')).write_text(json.dumps(exported, indent=2))
        path = self.output / (label + '-config.json')
        path.write_text(json.dumps(self.settings, indent=2))
        return path

    def inventory(self):
        result = {}
        actual_dirs = set()
        expected_dirs = {row['path'] for row in self.previous if row['kind'] == 'directory'}
        for directory, dirs, files in os.walk(self.root):
            if Path(directory) == self.root:
                dirs[:] = [name for name in dirs if name != '.enola']
            for name in dirs:
                path = Path(directory) / name
                if path.is_symlink():
                    raise RuntimeError('Unexpected mirror directory symlink: ' + str(path))
                actual_dirs.add(str(path.relative_to(self.root)))
            for name in files:
                path = Path(directory) / name
                if path.is_symlink():
                    raise RuntimeError('Unexpected mirror symlink: ' + str(path))
                result[str(path.relative_to(self.root))] = digest(path.read_bytes())
        if actual_dirs != expected_dirs:
            raise RuntimeError('Legacy physical directory names differ from selected inputs')
        return result

    def evidence(self, label):
        out = self.root / self.settings['output']['dir']
        evidence = {'mirror': str(self.root), 'physical_file_sha256': self.inventory(),
                    'legacy_main_inventory_available': False,
                    'limitation': 'snapshot file_hashes cover legacy main inventory, not every reference parse or side read'}
        expected = {row['path']: row['sha256'] for row in self.previous if row['kind'] != 'directory'}
        if evidence['physical_file_sha256'] != expected:
            raise RuntimeError('Legacy run changed physical source inputs')
        for name in ('snapshot.meta.json', 'receipt.json'):
            source = out / name
            if source.exists():
                shutil.copyfile(source, self.output / (label + '-' + name))
                if name == 'snapshot.meta.json':
                    metadata = json.loads(source.read_text())
                    evidence['legacy_main_inventory_available'] = True
                    evidence['legacy_file_hashes'] = metadata.get('file_hashes', [])
                    actual_main = {row['path']: row['hash'] for row in evidence['legacy_file_hashes']}
                    expected_main = {path: expected[path] for path in self.candidate_inventory['Files'] or []}
                    evidence['main_inventory_matches_candidate'] = actual_main == expected_main
                    if actual_main != expected_main:
                        (self.output / (label + '-inventory-evidence.json')).write_text(json.dumps(evidence, indent=2))
                        raise RuntimeError('Legacy snapshot main inventory differs from candidate selected main inventory')
        (self.output / (label + '-inventory-evidence.json')).write_text(json.dumps(evidence, indent=2))
