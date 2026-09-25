"""Reduce ablation-metrics.json to a gated frozen-vs-ablated per-mark comparison.

Nothing is interpreted unless the scenario's gates all pass: a profile difference
between arms means nothing if the arms did not do the same work on the same seed
and reach the same graph.
"""
import json, pathlib, re, sys

work = pathlib.Path(sys.argv[1] if len(sys.argv) > 1 else '/tmp/enola-stage21-ablate/work3')
rows = json.loads((work / 'ablation-metrics.json').read_text())

MARK = re.compile(r'^\[graph-profile\]\s+(\S+)\s+([\d.]+)s(?:\s+(.*))?$')

def by_name(row):
    """Mark name -> list of (seconds, tail). Names repeat, so the list is kept."""
    d = {}
    for line in row['phases']:
        m = MARK.match(line.strip())
        if m: d.setdefault(m.group(1), []).append((float(m.group(2)), (m.group(3) or '').strip()))
    return d

def normalized_result(row):
    """Full-result equality with RunID normalized away and nothing else.

    Result.RunID carries the producing run's id, so it can never match between two
    arms; every other field is compared as it stands (root msg_15f335f9830f). The run
    id is not discarded - each arm's wire correlation is gated separately below, so
    normalizing here does not stop a mismatched frame from being caught.
    """
    r = dict(row['result']); r.pop('RunID', None)
    return r

measured = [r for r in rows if r.get('measured')]
cold = {r['scenario']: r for r in rows if r['label'].startswith('cold-')}
report = {'unmeasured_runs': [r['label'] for r in rows if not r.get('measured')], 'scenarios': {}}

for scenario in ['body', 'structural']:
    arms = {r['arm']: r for r in measured if r['scenario'] == scenario}
    if set(arms) != {'frozen', 'ablated'}:
        report['scenarios'][scenario] = {'incomplete': sorted(arms)}
        continue
    f, ab = arms['frozen'], arms['ablated']
    fg, ag = f['gate_observations'], ab['gate_observations']
    fm, am = by_name(f), by_name(ab)

    per_mark = {}
    for n in sorted(set(fm) | set(am)):
        fs = [s for s, _ in fm.get(n, [])]
        as_ = [s for s, _ in am.get(n, [])]
        per_mark[n] = {
            'frozen_s': fs, 'ablated_s': as_,
            'frozen_total_s': round(sum(fs), 4), 'ablated_total_s': round(sum(as_), 4),
            'delta_s': round(sum(as_) - sum(fs), 4),
            'frozen_occurrences': len(fs), 'ablated_occurrences': len(as_),
            'only_on': 'frozen' if not as_ else ('ablated' if not fs else None),
        }
    coldhash = cold.get(scenario, {}).get('graph_hash')
    gates = {
        'both_arms_exit_zero': fg['exit_zero'] and ag['exit_zero'],
        'both_arms_generation_advanced': fg['generation_advanced'] and ag['generation_advanced'],
        'both_arms_bound_their_seed_proof': fg['proof_bound'] and ag['proof_bound'],
        'both_arms_reused_capture': fg['capture_reused'] and ag['capture_reused'],
        'seeds_byte_identical_across_arms':
            f.get('restored_state_manifest') is not None
            and f.get('restored_state_manifest') == ab.get('restored_state_manifest'),
        'both_arms_replayed_the_seed_graph': bool(f.get('replay')) and bool(ab.get('replay'))
            and f['replay']['matched'] == ab['replay']['matched'],
        'base_generation_equal': f['result'].get('BaseGeneration') == ab['result'].get('BaseGeneration'),
        'target_generation_equal': f['result'].get('TargetGeneration') == ab['result'].get('TargetGeneration'),
        'parsed_files_equal': f['result'].get('ParsedFiles') == ab['result'].get('ParsedFiles'),
        'owners_published_equal': f['result'].get('OwnersPublished') == ab['result'].get('OwnersPublished'),
        'full_result_equal_except_run_id': normalized_result(f) == normalized_result(ab),
        'each_arm_wire_run_id_matches_its_result':
            fg.get('wire_run_id_matches_result') is True and ag.get('wire_run_id_matches_result') is True,
        'arms_have_distinct_run_ids':
            f['result'].get('RunID') != ab['result'].get('RunID'),
        'arms_agree_on_graph': f.get('graph_hash') == ab.get('graph_hash') and f.get('graph_hash') is not None,
        'frozen_delta_equals_cold_graph': coldhash is not None and f.get('graph_hash') == coldhash,
        'ablated_delta_equals_cold_graph': coldhash is not None and ab.get('graph_hash') == coldhash,
        'frozen_produced_and_promoted_a_proof': fg['proof_written'] and fg['proof_promoted'],
        'ablated_produced_no_proof': not ag['proof_written'] and not ag['proof_promoted'],
    }
    report['scenarios'][scenario] = {
        'gates': gates,
        'failed_gates': sorted(k for k, v in gates.items() if not v),
        'interpretable': all(gates.values()),
        'seed': {'frozen': f.get('seed'), 'ablated': ab.get('seed')},
        'restored_state_manifest_equal':
            f.get('restored_state_manifest') == ab.get('restored_state_manifest'),
        'replay': {'frozen': f.get('replay'), 'ablated': ab.get('replay')},
        'run_ids': {'frozen': f['result'].get('RunID'), 'ablated': ab['result'].get('RunID'),
                    'frozen_wire': f.get('wire_run_id'), 'ablated_wire': ab.get('wire_run_id')},
        'results': {'frozen': f['result'], 'ablated': ab['result']},
        'graph_hashes': {'frozen': f.get('graph_hash'), 'ablated': ab.get('graph_hash'), 'cold': coldhash},
        'fallbacks': {'frozen': f['fallbacks'], 'ablated': ab['fallbacks']},
        'wall_seconds': {'frozen': round(f['wall_seconds'], 4), 'ablated': round(ab['wall_seconds'], 4),
                         'delta_s': round(ab['wall_seconds'] - f['wall_seconds'], 4)},
        'process_real_s': {'frozen': f['process_real_s'], 'ablated': ab['process_real_s']},
        'rss_bytes': {'frozen': f['rss_bytes'], 'ablated': ab['rss_bytes']},
        'costs_that_disappear_with_the_ablation':
            {n: v for n, v in per_mark.items() if v['only_on'] == 'frozen'},
        'marks_only_on_the_ablated_arm':
            {n: v for n, v in per_mark.items() if v['only_on'] == 'ablated'},
        'marks_on_both_arms_differing_by_5ms_or_more':
            {n: v for n, v in per_mark.items() if v['only_on'] is None and abs(v['delta_s']) >= 0.005},
        'occurrence_count_differences':
            {n: v for n, v in per_mark.items() if v['frozen_occurrences'] != v['ablated_occurrences']},
        'pinned': {'frozen': f['pinned'], 'ablated': ab['pinned']},
    }

out = work / 'ablation-comparison.json'
out.write_text(json.dumps(report, indent=2) + '\n')
for s, v in report['scenarios'].items():
    print(s, 'interpretable' if v.get('interpretable') else 'GATES FAILED: ' + ','.join(v.get('failed_gates', ['incomplete'])))
print('written', out)
