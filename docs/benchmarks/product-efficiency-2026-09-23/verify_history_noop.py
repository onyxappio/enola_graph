#!/usr/bin/env python3
"""Run only after a history driver exits; verify its final persisted no-op."""
import argparse
import hashlib
import importlib.util
import json
from pathlib import Path
import sys


def hashes(root):
    result = {}
    for path in sorted(root.rglob('*')):
        if not path.is_file() or path.name == 'session.lock':
            continue
        digest = hashlib.sha256()
        with path.open('rb') as stream:
            for chunk in iter(lambda: stream.read(1024 * 1024), b''):
                digest.update(chunk)
        result[str(path.relative_to(root))] = digest.hexdigest()
    return result


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--work', type=Path, required=True)
    parser.add_argument('--binary', type=Path, required=True)
    args = parser.parse_args()
    module = Path(__file__).resolve().parents[1] / 'invalidation-history-2026-09-22/run.py'
    spec = importlib.util.spec_from_file_location('history_noop_source', module)
    history = importlib.util.module_from_spec(spec)
    sys.modules[spec.name] = history
    spec.loader.exec_module(history)
    # The driver writes this only after its transition loop finishes. Do not
    # compete with an in-flight history writer for the same live state.
    final_report = args.work / 'results.json'
    if not final_report.is_file():
        raise RuntimeError('History has no final results.json; wait for its driver to finish')
    transitions = json.loads(final_report.read_text())['transitions']
    if not transitions or any(row.get('blocker') or not row.get('graph_hash_equal') for row in transitions):
        raise RuntimeError('Need successful history transitions before final no-op')
    previous = max(transitions, key=lambda row: row['index'])['completed_generation']
    state = args.work / 'state-live'
    events = args.work / 'events-live.jsonl'
    before_state = hashes(state)
    before_size = events.stat().st_size
    info = history.scope.enola(args.binary, 'delta', args.work / 'live', state, events,
                               args.work / 'summary-final-noop.json')
    pairs = history.scope.parse_event_records(events, before_size)
    info['events'] = len(pairs)
    assert info['parsed'] == 0, info
    assert not pairs and events.stat().st_size == before_size, 'No-op modified event stream'
    validated = history.validate_publishing_or_noop([], [], info, previous, [])
    after_state = hashes(state)
    assert before_state == after_state, 'No-op mutated persistent state'
    result = {'kind': validated['kind'], 'run': info, 'completed_generation': previous,
              'state_unchanged': True, 'state_hashes': after_state,
              'event_bytes_unchanged': before_size,
              'coverage': 'Final history state only; not each intermediate transition',
              'sink': 'file', 'timing': 'Single diagnostic fresh CLI run on shared host'}
    (args.work / 'final-noop-result.json').write_text(json.dumps(result, indent=2) + '\n')
    print(json.dumps(result))


if __name__ == '__main__':
    main()
