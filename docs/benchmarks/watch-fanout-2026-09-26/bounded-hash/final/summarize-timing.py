"""Fail-closed full-profile fanout timing summary. No comparison from incomplete cohorts."""
import datetime, hashlib, importlib.util, json, pathlib, statistics, sys
from timing_gate import validate_hold, load_observation
from memory_gate import require_memory_evidence
from power_gate import require_power_evidence
from background_gate import compare_background, rates, violations, check_processes
BASE = pathlib.Path(__file__).resolve().parent

def spread(values):
    return dict(n=len(values), median=statistics.median(values), min=min(values),
                max=max(values), values=values)

def stage_decision(arms, comparisons, before, after):
    comparison=comparisons[after+'_vs_'+before]
    primary=comparison['noop']
    gains=['noop'] if primary['median_percent_change']<=-3 and all(v<0 for v in primary['paired_percent_changes']) else []
    total_before=sum(v['seconds']['median'] for v in arms[before].values())
    total_after=sum(v['seconds']['median'] for v in arms[after].values())
    aggregate_percent_change=100*(total_after/total_before-1)
    mean_median_ratio=statistics.mean(arms[after][name]['seconds']['median']/arms[before][name]['seconds']['median'] for name in arms[before])
    time_regressions=[name for name,c in comparison.items() if c['median_percent_change']>2]
    rss_regressions=[name for name in arms[before] if arms[after][name]['rss_bytes']['median']>1.05*arms[before][name]['rss_bytes']['median']]
    return dict(engineering_accepted=bool(gains and not time_regressions and not rss_regressions and aggregate_percent_change<=0 and mean_median_ratio<1),
                primary_scenario='noop',mean_scenario_median_ratio=mean_median_ratio,aggregate_median_seconds_percent_change=aggregate_percent_change,qualifying_scenarios=gains,time_regressions=time_regressions,rss_regressions=rss_regressions,
                scope='limited stage only; broader startup/delta/watch goal remains open')

def summarize(base):
    # A complete cohort must also fit the jointly acknowledged window.
    try:
        hold = json.loads((base / 'timing-window.json').read_text())
        series = json.loads((base / 'timing-series.json').read_text())
        if series.get('hold') != hold:
            raise ValueError('hold mismatch or unauthorized stage')
        validate_hold(hold)
        start = datetime.datetime.fromisoformat(hold['start_utc']).timestamp()
        end = datetime.datetime.fromisoformat(hold['end_utc']).timestamp()
        orders = [('baseline','candidate'), ('candidate','baseline')] * 3
        expected = [(arm,n) for n,order in enumerate(orders,1) for arm in order]
        if [(r['arm'],r['repeat']) for r in series['runs']] != expected:
            raise ValueError('incomplete or nonalternating cohort')
        previous = start
        for r in series['runs']:
            began = datetime.datetime.fromisoformat(r['start_utc']).timestamp()
            ended = datetime.datetime.fromisoformat(r['end_utc']).timestamp()
            if not previous <= began < ended <= end or r['exit'] != 0:
                raise ValueError('failed, overlapping or out-of-window arm')
            if r.get('error'):
                raise ValueError('runner recorded an error')
            observed = require_memory_evidence(base, r)
            if observed != r.get('memory_evidence'):
                raise ValueError('memory evidence mismatch')
            if require_power_evidence(base, r) != r.get('power_evidence'):
                raise ValueError('power evidence mismatch')
            previous = ended
    except (OSError, ValueError, KeyError) as error:
        return dict(complete=False, problems=[str(error)],
                    note='No comparison without a complete in-window cohort.')
    pins = json.loads((base / 'cli-pairs/pins.json').read_text())
    validator_path = base / 'cli-pairs/validate_results.py'
    expected = next((sha for name, sha in pins['files'].items()
                     if pathlib.Path(name).resolve() == validator_path.resolve()), None)
    if not expected or hashlib.sha256(validator_path.read_bytes()).hexdigest() != expected:
        raise ValueError('validator pin mismatch')
    spec = importlib.util.spec_from_file_location('stage23_validator', validator_path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    problems, pairs = [], []
    for repeat in range(1, 7):
        pair = {}
        for arm in ('baseline', 'candidate'):
            directory = base / 'cli-pairs' / f'{arm}-{repeat}'
            try:
                receipt = json.loads((directory / 'pair-receipt.json').read_text())
                if not (receipt['timing_eligible'] and receipt['run_succeeded']
                        and receipt['all_checks_validated'] and receipt['exit'] == 0
                        and not receipt['correctness_only'] and not receipt['competing_samples']
                        and receipt['quiet_ack_message_id'] == hold['worker_message']):
                    raise ValueError('ineligible receipt')
                if receipt['arm'] != arm or receipt['repeat'] != repeat:
                    raise ValueError('arm/repeat identity mismatch')
                digest = pins['binaries'][arm]['sha256']
                if not isinstance(digest, str) or len(digest) != 64 or any(c not in '0123456789abcdef' for c in digest):
                    raise ValueError('invalid binary pin')
                if receipt['binary_sha256'] != digest:
                    raise ValueError('binary pin mismatch')
                metrics = json.loads((directory / 'metrics.json').read_text())
                checks = json.loads((directory / 'checks.json').read_text())
                if not module.validate(metrics, checks):
                    raise ValueError('correctness validation failed')
                rows = [r for r in metrics if r['label'].startswith('r1-new-')]
                values = {r['label'].removeprefix('r1-new-'): r for r in rows}
                if len(rows) != 4 or set(values) != {'initial','noop','body','structural'}:
                    raise ValueError('missing or duplicated scenario')
                for row in rows:
                    if row['seconds'] <= 0 or row['rss_bytes'] <= 0:
                        raise ValueError('invalid wall/RSS measurement')
                pair[arm] = values
            except (OSError, ValueError, KeyError, AssertionError) as error:
                problems.append(dict(repeat=repeat, arm=arm, reason=str(error)))
        if len(pair) == 2:
            pairs.append(pair)
    if problems or len(pairs) != 6:
        return dict(complete=False, complete_pairs=len(pairs), problems=problems,
                    note='No partial-cohort speed comparison is reported.')
    # Own-arm cold equality alone does not establish baseline/candidate parity.
    expected_labels = {'r1-new-initial','r1-new-noop','r1-new-body',
                       'r1-new-structural','cold-original','cold-body','cold-structural'}
    hashes = {}
    for repeat in range(1, 7):
        for arm in ('baseline','candidate'):
            metrics = json.loads((base / 'cli-pairs' / f'{arm}-{repeat}' / 'metrics.json').read_text())
            if len(metrics) != 7 or {r['label'] for r in metrics} != expected_labels:
                return dict(complete=False, problems=['missing/duplicate graph scenario'])
            for row in metrics:
                digest = row.get('graph_hash')
                if not isinstance(digest, str) or len(digest) != 64 or any(c not in '0123456789abcdef' for c in digest):
                    return dict(complete=False, problems=['invalid normalized graph hash'])
                if row['label'] in hashes and hashes[row['label']] != digest:
                    return dict(complete=False, problems=['cross-arm/repeat graph mismatch: ' + row['label']])
                hashes[row['label']] = digest
    if hold.get('mode') == 'controlled-background':
        vectors=[]
        roster,limits=load_observation(hold)
        for repeat in range(1,7):
            for arm in ('baseline','candidate'):
                receipt=json.loads((base/'cli-pairs'/f'{arm}-{repeat}'/'pair-receipt.json').read_text())
                if receipt.get('background_mode')!='controlled-background' or receipt.get('background_errors'):
                    return dict(complete=False,problems=['invalid background receipt'])
                try:
                    samples=[json.loads(line) for line in (base/'cli-pairs'/f'{arm}-{repeat}'/'background-load.jsonl').read_text().splitlines()]
                    if len(samples)<2 or any('error' in sample for sample in samples):raise ValueError('missing or failed samples')
                    if any(check_processes(sample,roster,receipt['background_owner_pid']) for sample in samples):raise ValueError('foreign workload or roster change')
                    verified=[rates(a,b,roster) for a,b in zip(samples,samples[1:])]
                    if any(violations(v,limits) for v in verified):raise ValueError('background threshold exceeded')
                    if verified!=receipt.get('background_vectors'):raise ValueError('background derived metrics mismatch')
                    vectors.append(verified)
                except (OSError,ValueError,KeyError) as error:
                    return dict(complete=False,problems=['invalid raw background evidence: '+str(error)])
        mismatch=compare_background(vectors,limits)
        if mismatch:
            return dict(complete=False,problems=['background distributions outside preregistered matching band',mismatch])
    arms = {}
    for arm in ('baseline','candidate'):
        scenarios = {}
        for scenario in ('initial','noop','body','structural'):
            rows = [p[arm][scenario] for p in pairs]
            fields = {k: spread([r[k] for r in rows]) for k in
                      ('seconds','rss_bytes','first_batch_s','broker_end_s','consumer_end_s')
                      if all(r.get(k) is not None for r in rows)}
            fields['parsed_files'] = spread([r['result']['ParsedFiles'] for r in rows])
            for key in ('files_read', 'cached_files', 'graphql_parsed', 'summary_scans', 'derived_indexes'):
                if all(key in r['result'].get('Stats', {}) for r in rows):
                    fields[key] = spread([r['result']['Stats'][key] for r in rows])
            for key in ('owner_scope_count','payload_bytes_total','messages_observed'):
                if all(r.get('wire') and key in r['wire'] for r in rows):
                    fields[key] = spread([r['wire'][key] for r in rows])
            fields['ratio_to_initial'] = spread([p[arm][scenario]['seconds'] /
                                                p[arm]['initial']['seconds'] for p in pairs])
            scenarios[scenario] = fields
        arms[arm] = scenarios
    comparisons = {}
    for before_arm, after_arm in [('baseline','candidate')]:
        comparison = {}
        for scenario in arms[before_arm]:
            before, after = (arms[a][scenario]['seconds'] for a in (before_arm,after_arm))
            comparison[scenario] = dict(
                median_percent_change=100*(after['median']/before['median']-1),
                paired_percent_changes=[100*(p[after_arm][scenario]['seconds']/p[before_arm][scenario]['seconds']-1) for p in pairs],
                ranges_overlap=max(before['min'],after['min']) <= min(before['max'],after['max']))
        comparisons[after_arm + '_vs_' + before_arm] = comparison
    decisions={after+'_vs_'+before:stage_decision(arms,comparisons,before,after) for before,after in [('baseline','candidate')]}
    return dict(complete=True, paging_context=[dict(arm=r["arm"], repeat=r["repeat"], **r["memory_evidence"]) for r in series["runs"]], arms=arms, comparisons=comparisons, stage_decisions=decisions,
                promotion_decision=decisions['candidate_vs_baseline'],
                promotion_rule='PROMOTION_RULE.md; combined versus published baseline; prior controls not part of this cohort',
                note='Fresh CLI through process exit including producer ACKs; broker/consumer boundaries separate. Six counterbalanced pairs are descriptive, not a significance test. No watch or near-zero startup claim.')

if __name__ == '__main__':
    result = summarize(BASE)
    print(json.dumps(result, indent=2))
    if result['complete']:
        (BASE / 'timing-comparison.json').write_text(json.dumps(result, indent=2) + '\n')
    sys.exit(0 if result['complete'] else 2)
