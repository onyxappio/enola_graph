"""Reduce cohort-metrics.json to a gated per-mark comparison across three arms.

Nothing is interpreted unless the scenario's gates all pass: a profile difference
between arms means nothing if the arms did not do the same work on the same seed
and reach the same graph.

v7 added the pinned Stage20 pre-proof baseline arm. Its correctness gates are the same
equality gates the other arms face; the proof gates do not apply to a binary that has
no proof mechanism, so they are reported as inapplicable rather than as passes and
rather than by loosening the candidate's gates (root msg_1d7a9d9753b2).

v8 replaces the ablated arm with the option C retain candidate. Two things change in the
semantics, both tightenings (root msg_3551ecaf8af8):

  * The retain arm MUST produce and promote a proof. The ablated arm's absent-proof gate
    was an assertion that the ablation really happened; it says nothing about a candidate
    that keeps the whole production path, and it is not reused or weakened into a gate
    that both binaries could satisfy - it is replaced by its opposite.
  * The state work itself is gated, not merely reported: the retain delta must read the
    committed state twice and digest it twice in total - once on the read side, once for
    the state it writes - and compare bytes exactly once, against the frozen delta's
    three digests and no comparison. That is the whole claim of option C stated as a
    count, so a run that did not actually remove the second digest cannot be read as one
    that did, and a run that removed the comparison instead cannot either.

Two further tightenings, both from review of this reducer rather than of the runs
(root msg_f07af11614d6): each measured delta must decode the state exactly once, since
equality between the arms would also be satisfied by two decodes each or none at all;
and each arm must contribute exactly one measured row, a duplicate being a reason to
refuse the arm rather than to keep whichever row happened to be read last.

Every correctness gate v7 carried is kept as it stood.
"""
import json, pathlib, re, sys

work = pathlib.Path(sys.argv[1] if len(sys.argv) > 1 else '/tmp/enola-stage21-ablate/work8')
rows = json.loads((work / 'cohort-metrics.json').read_text())

MARK = re.compile(r'^\[graph-profile\]\s+(\S+)\s+([\d.]+)s(?:\s+(.*))?$')

def by_name(row):
    """Mark name -> list of (seconds, tail). Names repeat, so the list is kept."""
    d = {}
    for line in row['phases']:
        m = MARK.match(line.strip())
        if m: d.setdefault(m.group(1), []).append((float(m.group(2)), (m.group(3) or '').strip()))
    return d

def state_reads(marks):
    """The occurrences of state_read_bytes that read the committed state.

    The mark's tail begins with the file's base name, so a recovery read of
    pending-state.json is not counted as a read of state.json.
    """
    return [x for x in marks.get('state_read_bytes', []) if x[1].split()[:1] == ['state.json']]

def fingerprints(marks):
    """(total, read_side, write_side) state_fingerprint occurrences.

    One mark name covers both sides. The digest taken of the state a run is about to
    write logs a tail carrying its byte count as 'write bytes=', which is what separates
    it from the digests taken of a state that was read. Both are reported: a candidate
    that removed the write-side digest instead of the duplicate read-side one would show
    the same total, and that is exactly the confusion this split exists to prevent.
    """
    all_ = marks.get('state_fingerprint', [])
    write = [x for x in all_ if 'write' in x[1]]
    return (len(all_), len(all_) - len(write), len(write))

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
    # One measured row per arm, and a second row is a reason to refuse the arm rather
    # than to keep whichever one a dict comprehension happened to leave behind. A
    # protocol that produced two measured deltas for an arm is not the protocol this
    # comparator reduces, and silently reading one of them would hide that.
    arm_rows = {}
    for r in measured:
        if r['scenario'] == scenario: arm_rows.setdefault(r['arm'], []).append(r)
    duplicates = {a: [x['label'] for x in v] for a, v in arm_rows.items() if len(v) != 1}
    arms = {a: v[0] for a, v in arm_rows.items() if len(v) == 1}
    if not {'frozen', 'retain'} <= set(arms):
        report['scenarios'][scenario] = {'incomplete': sorted(arms),
                                        'duplicate_measured_rows': duplicates}
        continue
    f, ab = arms['frozen'], arms['retain']
    fg, ag = f['gate_observations'], ab['gate_observations']
    fm, am = by_name(f), by_name(ab)

    per_mark = {}
    for n in sorted(set(fm) | set(am)):
        fs = [s for s, _ in fm.get(n, [])]
        as_ = [s for s, _ in am.get(n, [])]
        per_mark[n] = {
            'frozen_s': fs, 'retain_s': as_,
            'frozen_total_s': round(sum(fs), 4), 'retain_total_s': round(sum(as_), 4),
            'delta_s': round(sum(as_) - sum(fs), 4),
            'frozen_occurrences': len(fs), 'retain_occurrences': len(as_),
            'only_on': 'frozen' if not as_ else ('retain' if not fs else None),
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
        'retain_delta_equals_cold_graph': coldhash is not None and ab.get('graph_hash') == coldhash,
        'frozen_produced_and_promoted_a_proof': fg['proof_written'] and fg['proof_promoted'],
        # The replacement for the ablated arm's absent-proof gate: this candidate keeps
        # the entire production path, so it has to be seen producing and installing one.
        'retain_produced_and_promoted_a_proof': ag['proof_written'] and ag['proof_promoted'],
        # The option C claim, as counts. Read the state twice, digest it twice in total
        # (one read side, one write side), compare bytes once - against the frozen arm's
        # three digests, two reads and no comparison.
        'retain_delta_read_committed_state_twice': len(state_reads(am)) == 2,
        'frozen_delta_read_committed_state_twice': len(state_reads(fm)) == 2,
        'retain_delta_fingerprinted_twice_one_read_one_write': fingerprints(am) == (2, 1, 1),
        'frozen_delta_fingerprinted_three_times_two_read_one_write': fingerprints(fm) == (3, 2, 1),
        'retain_delta_compared_bytes_once': len(am.get('state_bytes_compared', [])) == 1,
        'frozen_delta_compared_no_bytes': len(fm.get('state_bytes_compared', [])) == 0,
        # Each arm decodes the state exactly once. Equality between the arms alone would
        # be satisfied by both decoding twice, or neither decoding at all, and neither of
        # those is the run being compared.
        'frozen_delta_decoded_the_state_exactly_once':
            len(fm.get('state_json_unmarshal', [])) == 1,
        'retain_delta_decoded_the_state_exactly_once':
            len(am.get('state_json_unmarshal', [])) == 1,
        'frozen_and_retain_each_have_exactly_one_measured_row':
            not ({'frozen', 'retain'} & set(duplicates)),
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
            'baseline_has_exactly_one_measured_row': 'baseline' not in duplicates,
            'baseline_decoded_the_state_exactly_once':
                len(bm.get('state_json_unmarshal', [])) == 1,
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
                'retain': len(am.get(n, [])),
                'baseline_total_s': round(sum(x for x, _ in bm.get(n, [])), 4),
                'frozen_total_s': round(sum(x for x, _ in fm.get(n, [])), 4),
                'retain_total_s': round(sum(x for x, _ in am.get(n, [])), 4),
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
            # Observations, not gates. What the pre-proof binary's own state work was,
            # so the retain arm's counts can be read against a run that never had a proof.
            'state_work': {
                'committed_state_reads': len(state_reads(bm)),
                'fingerprints_total_read_write': list(fingerprints(bm)),
                'byte_comparisons': len(bm.get('state_bytes_compared', [])),
                'state_decodes': len(bm.get('state_json_unmarshal', [])),
            },
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
        'seed': {'frozen': f.get('seed'), 'retain': ab.get('seed')},
        'restored_state_manifest_equal':
            f.get('restored_state_manifest') == ab.get('restored_state_manifest'),
        'replay': {'frozen': f.get('replay'), 'retain': ab.get('replay')},
        'run_ids': {'frozen': f['result'].get('RunID'), 'retain': ab['result'].get('RunID'),
                    'frozen_wire': f.get('wire_run_id'), 'retain_wire': ab.get('wire_run_id')},
        'results': {'frozen': f['result'], 'retain': ab['result']},
        'graph_hashes': {'frozen': f.get('graph_hash'), 'retain': ab.get('graph_hash'), 'cold': coldhash},
        'fallbacks': {'frozen': f['fallbacks'], 'retain': ab['fallbacks']},
        'wall_seconds': {'frozen': round(f['wall_seconds'], 4), 'retain': round(ab['wall_seconds'], 4),
                         'delta_s': round(ab['wall_seconds'] - f['wall_seconds'], 4)},
        'process_real_s': {'frozen': f['process_real_s'], 'retain': ab['process_real_s']},
        'rss_bytes': {'frozen': f['rss_bytes'], 'retain': ab['rss_bytes']},
        'marks_only_on_the_frozen_arm':
            {n: v for n, v in per_mark.items() if v['only_on'] == 'frozen'},
        'marks_only_on_the_retain_arm':
            {n: v for n, v in per_mark.items() if v['only_on'] == 'retain'},
        'marks_on_both_arms_differing_by_5ms_or_more':
            {n: v for n, v in per_mark.items() if v['only_on'] is None and abs(v['delta_s']) >= 0.005},
        'occurrence_count_differences':
            {n: v for n, v in per_mark.items() if v['frozen_occurrences'] != v['retain_occurrences']},
        'pinned': {'frozen': f['pinned'], 'retain': ab['pinned']},
        'state_work': {
            arm: {
                'committed_state_reads': len(state_reads(marks)),
                'fingerprints_total_read_write': list(fingerprints(marks)),
                'byte_comparisons': len(marks.get('state_bytes_compared', [])),
                'state_decodes': len(marks.get('state_json_unmarshal', [])),
            }
            for arm, marks in [('frozen', fm), ('retain', am)]
        },
        'baseline': baseline,
        # Separate from 'interpretable', which keeps its original meaning: the
        # candidate pair alone. A scenario is only usable for baseline attribution
        # when all three arms ran and every gate on both sets passed, so an absent
        # or failing baseline cannot be read as a green scenario (root msg_7e11f89f7163).
        'arms_present': sorted(arms),
        'duplicate_measured_rows': duplicates,
        'full_cohort_interpretable':
            set(arms) == {'frozen', 'retain', 'baseline'}
            and all(gates.values())
            and baseline is not None and baseline['interpretable'],
    }

# The whole v8 cohort, across both scenarios. This is the prerequisite for any
# baseline attribution; the comparator exits nonzero without it so an incomplete or
# failed cohort cannot be quietly read as a result.
report['full_cohort_interpretable'] = (
    set(report['scenarios']) == {'body', 'structural'}
    and all(v.get('full_cohort_interpretable') for v in report['scenarios'].values()))

out = work / 'cohort-comparison.json'
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
    print('v8 cohort is incomplete or has failed gates: no baseline attribution may be read '
          'from it. The candidate-pair verdicts above keep their original meaning.')
    sys.exit(1)
