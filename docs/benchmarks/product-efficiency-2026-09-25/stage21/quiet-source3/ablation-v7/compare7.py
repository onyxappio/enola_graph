"""Reduce ablation-metrics.json to a gated per-mark comparison across three arms.

Nothing is interpreted unless the scenario's gates all pass: a profile difference
between arms means nothing if the arms did not do the same work on the same seed
and reach the same graph.

v7 adds the pinned Stage20 pre-proof baseline arm. Its correctness gates are the same
equality gates the other arms face; the proof gates do not apply to a binary that has
no proof mechanism, so they are reported as inapplicable rather than as passes and
rather than by loosening the candidate's gates (root msg_1d7a9d9753b2).
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
    if not {'frozen', 'ablated'} <= set(arms):
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
    base = arms.get('baseline')
    baseline = None
    if base is not None:
        bg, bm = base['gate_observations'], by_name(base)
        bgates = {
            'baseline_exit_zero': bg['exit_zero'],
            'baseline_generation_advanced': bg['generation_advanced'],
            'baseline_restored_the_same_seed_bytes':
                base.get('restored_state_manifest') is not None
                and base.get('restored_state_manifest') == f.get('restored_state_manifest'),
            'baseline_replayed_the_seed_graph': bool(base.get('replay'))
                and bool(f.get('replay')) and base['replay']['matched'] == f['replay']['matched'],
            'baseline_base_generation_equal':
                base['result'].get('BaseGeneration') == f['result'].get('BaseGeneration'),
            'baseline_target_generation_equal':
                base['result'].get('TargetGeneration') == f['result'].get('TargetGeneration'),
            'baseline_parsed_files_equal':
                base['result'].get('ParsedFiles') == f['result'].get('ParsedFiles'),
            'baseline_owners_published_equal':
                base['result'].get('OwnersPublished') == f['result'].get('OwnersPublished'),
            'baseline_full_result_equal_except_run_id': normalized_result(base) == normalized_result(f),
            'baseline_wire_run_id_matches_result': bg.get('wire_run_id_matches_result') is True,
            'baseline_run_id_distinct_from_candidate':
                base['result'].get('RunID') != f['result'].get('RunID'),
            'baseline_delta_equals_cold_graph':
                coldhash is not None and base.get('graph_hash') == coldhash,
        }
        # Not gates. A pre-proof binary cannot bind, reuse or produce a proof, and
        # asserting it did - or relaxing the candidate gates so both fit one rule -
        # would be the wrong shape. Recorded as observations so the absence is on file.
        inapplicable = {
            'baseline_bound_a_proof': bg['proof_bound'],
            'baseline_reused_capture': bg['capture_reused'],
            'baseline_produced_a_proof': bg['proof_written'] or bg['proof_promoted'],
        }
        counts = {}
        for n in sorted(set(fm) | set(am) | set(bm)):
            counts[n] = {
                'baseline': len(bm.get(n, [])), 'frozen': len(fm.get(n, [])),
                'ablated': len(am.get(n, [])),
                'baseline_total_s': round(sum(x for x, _ in bm.get(n, [])), 4),
                'frozen_total_s': round(sum(x for x, _ in fm.get(n, [])), 4),
                'ablated_total_s': round(sum(x for x, _ in am.get(n, [])), 4),
            }
        baseline = {
            'gates': bgates,
            'failed_gates': sorted(k for k, v in bgates.items() if not v),
            'interpretable': all(bgates.values()),
            'inapplicable_proof_observations': inapplicable,
            'phase_call_counts': counts,
            'marks_absent_from_baseline':
                {n: v for n, v in counts.items() if v['baseline'] == 0 and v['frozen'] > 0},
            'marks_the_candidate_calls_more_often':
                {n: v for n, v in counts.items() if v['baseline'] and v['frozen'] > v['baseline']},
            'marks_absent_from_candidate':
                {n: v for n, v in counts.items() if v['baseline'] > 0 and v['frozen'] == 0},
            'result': base['result'],
            'graph_hash': base.get('graph_hash'),
            'wall_seconds': round(base['wall_seconds'], 4),
            'process_real_s': base['process_real_s'], 'rss_bytes': base['rss_bytes'],
            'pinned': base['pinned'],
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
        'baseline': baseline,
        # Separate from 'interpretable', which keeps its original meaning: the
        # candidate pair alone. A scenario is only usable for baseline attribution
        # when all three arms ran and every gate on both sets passed, so an absent
        # or failing baseline cannot be read as a green scenario (root msg_7e11f89f7163).
        'arms_present': sorted(arms),
        'full_cohort_interpretable':
            set(arms) == {'frozen', 'ablated', 'baseline'}
            and all(gates.values())
            and baseline is not None and baseline['interpretable'],
    }

# The whole v7 cohort, across both scenarios. This is the prerequisite for any
# baseline attribution; the comparator exits nonzero without it so an incomplete or
# failed cohort cannot be quietly read as a result.
report['full_cohort_interpretable'] = (
    set(report['scenarios']) == {'body', 'structural'}
    and all(v.get('full_cohort_interpretable') for v in report['scenarios'].values()))

out = work / 'ablation-comparison.json'
out.write_text(json.dumps(report, indent=2) + '\n')
for s, v in report['scenarios'].items():
    print(s, 'interpretable' if v.get('interpretable') else 'GATES FAILED: ' + ','.join(v.get('failed_gates', ['incomplete'])))
    b = v.get('baseline')
    if b is None:
        print(' ', s, 'baseline arm ABSENT')
    else:
        print(' ', s, 'baseline', 'interpretable' if b['interpretable'] else 'GATES FAILED: ' + ','.join(b['failed_gates']))
print('written', out)
print('FULL_COHORT_INTERPRETABLE', report['full_cohort_interpretable'])
if not report['full_cohort_interpretable']:
    print('v7 cohort is incomplete or has failed gates: no baseline attribution may be read '
          'from it. The candidate-pair verdicts above keep their original meaning.')
    sys.exit(1)
