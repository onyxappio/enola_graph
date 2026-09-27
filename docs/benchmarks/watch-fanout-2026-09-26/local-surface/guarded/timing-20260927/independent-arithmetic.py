from pathlib import Path
import json,statistics
r=Path(__file__).resolve().parent
series=json.loads((r/'timing-series.json').read_text())
expected=[(a,i) for i in range(1,7) for a in (['baseline','candidate'] if i%2 else ['candidate','baseline'])]
assert [(x['arm'],x['repeat']) for x in series['runs']]==expected
arms={a:{s:[] for s in ['initial','noop','body','structural']} for a in ['baseline','candidate']}
for a,i in expected:
 p=r/'cli-pairs'/f'{a}-{i}'
 rows=json.loads((p/'metrics.json').read_text());checks=json.loads((p/'checks.json').read_text())
 assert len(checks)==4 and all(c['pass'] for c in checks)
 for s in arms[a]:arms[a][s].append(next(v for v in rows if v['label']==f'r1-new-{s}'))
result={};ratios=[];sumB=0;sumC=0
for s in arms['baseline']:
 b=arms['baseline'][s];c=arms['candidate'][s]
 mb=statistics.median(x['seconds'] for x in b);mc=statistics.median(x['seconds'] for x in c)
 ratio=mc/mb;ratios.append(ratio);sumB+=mb;sumC+=mc
 rr=statistics.median(x['rss_bytes'] for x in c)/statistics.median(x['rss_bytes'] for x in b)
 pairs=[y['seconds']/x['seconds'] for x,y in zip(b,c)]
 assert rr<=1.05
 assert ratio <= (0.95 if s=='body' else 1.02)
 if s=='body':assert all(v<1 for v in pairs)
 for x,y in zip(b,c):assert x['graph_hash']==y['graph_hash']
 result[s]={'median_baseline_s':mb,'median_candidate_s':mc,'ratio_of_medians':ratio,'paired_ratios':pairs,'rss_median_ratio':rr}
assert statistics.mean(ratios)<1 and sumC<=sumB
out={'arithmetic_pass':True,'source':'independent direct metrics calculation; does not substitute host evidence review','order_verified':True,'scenario_checks':result,'mean_median_ratio':statistics.mean(ratios),'sum_medians_baseline':sumB,'sum_medians_candidate':sumC}
(r/'independent-arithmetic.json').write_text(json.dumps(out,indent=2)+'\n')
print('PASS arithmetic and repeat-directory pairing; host review separate')
