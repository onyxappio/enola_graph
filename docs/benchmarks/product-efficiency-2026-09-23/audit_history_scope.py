#!/usr/bin/env python3
"""Compare complete per-owner contributions in a historical event stream."""
import argparse
from collections import Counter
import importlib.util
import json
from pathlib import Path


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--work', type=Path, required=True)
    parser.add_argument('--output', type=Path, required=True)
    args = parser.parse_args()
    module = Path(__file__).resolve().parents[1] / 'invalidation-scope-2026-09-21/run.py'
    spec = importlib.util.spec_from_file_location('scope_audit_source', module)
    scope = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(scope)
    cases = {}
    for path in (args.work / 'cases').glob('*/result.json'):
        row = json.loads(path.read_text())
        if row.get('graph_hash_equal') and not row.get('blocker') and 'delta' in row:
            cases[row['delta']['run_id']] = row
    consumer = scope.Consumer()
    before = {}
    owners = []
    rows = []
    with (args.work / 'events-live.jsonl').open() as stream:
        for line in stream:
            if not line.endswith('\n'):
                break  # A still-running producer may have an incomplete trailing line.
            record = json.loads(line.split(' ', 2)[2])
            run_id = record.get('run_id')
            if record['type'] == 'begin_replace':
                owners = [scope.owner_key(o) for o in record['owner_scope']]
                before = {owner: scope.owner_canonical(consumer, owner) for owner in owners}
            consumer.apply([record])
            if record['type'] != 'end_replace' or run_id not in cases:
                continue
            case = cases[run_id]
            changed = [owner for owner in owners
                       if before[owner] != scope.owner_canonical(consumer, owner)]
            unchanged = set(owners) - set(changed)
            extensions = Counter(Path(owner.removeprefix('file:')).suffix or '[none]' for owner in owners)
            changed_extensions = Counter(Path(owner.removeprefix('file:')).suffix or '[none]' for owner in changed)
            rows.append({'case': case['id'], 'scope': len(owners),
                         'changed_contributions_in_scope': len(changed),
                         'unchanged_contributions_in_scope': len(unchanged),
                         'scope_extensions': dict(extensions.most_common()),
                         'changed_extensions': dict(changed_extensions.most_common()),
                         'unchanged_markdown': sum(owner.endswith('.md') for owner in unchanged),
                         'cold_equal': case['graph_hash_equal']})
    result = {'method': 'Complete node/edge JSON per owner, preserving duplicates; excludes only run envelopes',
              'limitation': 'Unchanged final contribution does not alone prove it was safe to omit planning or analysis',
              'completed_publishing_cases': rows}
    args.output.write_text(json.dumps(result, indent=2) + '\n')
    print(json.dumps(result, indent=2))


if __name__ == '__main__':
    main()
