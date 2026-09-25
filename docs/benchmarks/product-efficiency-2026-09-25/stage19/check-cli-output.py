"""Observable CLI correctness smoke; debug sink, never performance evidence."""
import argparse
import hashlib
import json
from pathlib import Path
import subprocess

p = argparse.ArgumentParser()
p.add_argument('--binary', type=Path, required=True)
p.add_argument('--output', type=Path, required=True)
a = p.parse_args()
b = a.output.resolve()
b.mkdir(exist_ok=False)
repo = b / 'repo'
(repo / 'src').mkdir(parents=True)
(repo / 'package.json').write_text('{"name":"stage19-cli-smoke","version":"1.0.0"}\n')
(repo / 'tsconfig.json').write_text('{"compilerOptions":{"target":"ES2022","module":"ESNext"},"include":["src"]}\n')
(repo / 'src/io.ts').write_text('export async function readRemote(url: string) { return fetch(url); }\nexport function twice(x: number) { return x * 2; }\n')
(repo / 'src/main.ts').write_text('import { twice } from "./io";\nexport function answer() { return twice(21); }\n')

def sha(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()

def snap():
    return {str(f.relative_to(b)): sha(f) for f in (b / 'state').rglob('*') if f.is_file()} | {'events.jsonl': sha(b / 'events.jsonl')}

def canonical(facts):
    return sorted(json.dumps(f, sort_keys=True) for f in facts)

def invoke(label, mode, flags):
    cmd = [str(a.binary.resolve()), 'graph', mode, '--events', str(b / 'events.jsonl'), '--state-dir', str(b / 'state'), '--context', 'stage19-cli-smoke', '--repo-id', 'smoke', *flags, str(repo)]
    r = subprocess.run(cmd, capture_output=True, text=True, timeout=120)
    (b / (label + '.stdout')).write_text(r.stdout)
    (b / (label + '.stderr')).write_text(r.stderr)
    assert r.returncode == 0, (label, r.returncode, r.stderr)
    return r

initial = json.loads(invoke('initial', 'analyze', ['--json']).stdout)[0]
assert initial['ParsedFiles'] == 2 and len(initial['Facts']) > 0
before = snap()
rows = []
for label, flags in [('full', ['--json']), ('summary', ['--summary-json']), ('both', ['--json', '--summary-json']), ('plain', [])]:
    r = invoke(label, 'delta', flags)
    assert snap() == before, (label, 'state or events changed')
    row = {'mode': label, 'all_state_bytes_unchanged': True, 'events_unchanged': True}
    if flags:
        x = json.loads(r.stdout)[0]
        assert x['ParsedFiles'] == 0 and x['OwnersPublished'] == 0
        assert x['BaseGeneration'] == x['TargetGeneration'] == initial['TargetGeneration']
        if label == 'summary':
            assert x['Facts'] is None
            row['facts_omitted_as_requested'] = True
        else:
            assert canonical(x['Facts']) == canonical(initial['Facts'])
            row['complete_facts_match_initial'] = True
        row.update(parsed=x['ParsedFiles'], generation=x['TargetGeneration'], facts=len(x['Facts'] or []))
    else:
        assert not r.stdout.strip()
        assert 'generation 1→1 parsed=0' in r.stderr
    rows.append(row)
receipt = {'binary_sha256': sha(a.binary), 'harness_sha256': sha(Path(__file__)), 'timing_eligible': False, 'purpose': __doc__, 'initial_facts': len(initial['Facts']), 'checks': rows, 'all_passed': True, 'state_before_after': before}
(b / 'receipt.json').write_text(json.dumps(receipt, indent=2) + '\n')
print(json.dumps(receipt, indent=2))
