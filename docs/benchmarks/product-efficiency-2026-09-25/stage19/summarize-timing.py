"""Fail-closed Stage19 timing summary. No comparison from incomplete cohorts."""
import hashlib, importlib.util, json, pathlib, statistics, sys
BASE = pathlib.Path(__file__).resolve().parent

def spread(values):
    return dict(n=len(values), median=statistics.median(values), min=min(values),
                max=max(values), values=values)

def summarize(base):
    pins = json.loads((base / 'cli-pairs/pins.json').read_text())
    validator_path = base / 'cli-pairs/validate_results.py'
    expected = next((sha for name, sha in pins['files'].items()
                     if pathlib.Path(name).resolve() == validator_path.resolve()), None)
    if not expected or hashlib.sha256(validator_path.read_bytes()).hexdigest() != expected:
        raise ValueError('validator pin mismatch')
    spec = importlib.util.spec_from_file_location('stage19_validator', validator_path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    problems, pairs = [], []
    for repeat in range(1, 4):
        pair = {}
        for arm in ('baseline', 'candidate'):
            directory = base / 'cli-pairs' / f'{arm}-{repeat}'
            try:
                receipt = json.loads((directory / 'pair-receipt.json').read_text())
                if not (receipt['timing_eligible'] and receipt['run_succeeded']
                        and receipt['all_checks_validated'] and receipt['exit'] == 0
                        and not receipt['correctness_only'] and not receipt['competing_samples']
                        and receipt['quiet_ack_message_id']):
                    raise ValueError('ineligible receipt')
                if receipt['arm'] != arm or receipt['repeat'] != repeat:
                    raise ValueError('arm/repeat identity mismatch')
                if receipt['binary_sha256'] != pins['binaries'][arm]['sha256']:
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
    if problems or len(pairs) != 3:
        return dict(complete=False, complete_pairs=len(pairs), problems=problems,
                    note='No partial-cohort speed comparison is reported.')
    arms = {}
    for arm in ('baseline','candidate'):
        scenarios = {}
        for scenario in ('initial','noop','body','structural'):
            rows = [p[arm][scenario] for p in pairs]
            fields = {k: spread([r[k] for r in rows]) for k in
                      ('seconds','rss_bytes','first_batch_s','broker_end_s','consumer_end_s')
                      if all(r.get(k) is not None for r in rows)}
            fields['parsed_files'] = spread([r['result']['ParsedFiles'] for r in rows])
            for key in ('owner_scope_count','payload_bytes_total','messages_observed'):
                if all(r.get('wire') and key in r['wire'] for r in rows):
                    fields[key] = spread([r['wire'][key] for r in rows])
            fields['ratio_to_initial'] = spread([p[arm][scenario]['seconds'] /
                                                p[arm]['initial']['seconds'] for p in pairs])
            scenarios[scenario] = fields
        arms[arm] = scenarios
    comparisons = {}
    for scenario in arms['baseline']:
        before, after = (arms[a][scenario]['seconds'] for a in ('baseline','candidate'))
        comparisons[scenario] = dict(
            median_percent_change=100*(after['median']/before['median']-1),
            paired_percent_changes=[100*(p['candidate'][scenario]['seconds'] /
                                        p['baseline'][scenario]['seconds']-1) for p in pairs],
            ranges_overlap=max(before['min'], after['min']) <= min(before['max'], after['max']))
    return dict(complete=True, arms=arms, comparisons=comparisons,
                note='Fresh CLI through process exit including producer ACKs; broker/consumer boundaries separate. Three pairs are descriptive, not a significance test. No watch or near-zero startup claim.')

if __name__ == '__main__':
    result = summarize(BASE)
    print(json.dumps(result, indent=2))
    if result['complete']:
        (BASE / 'timing-comparison.json').write_text(json.dumps(result, indent=2) + '\n')
    sys.exit(0 if result['complete'] else 2)
