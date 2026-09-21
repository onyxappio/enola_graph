from pathlib import Path
import json,statistics,sys
root=Path(sys.argv[1]);rows=json.loads((root/'metrics.json').read_text());checks=json.loads((root/'checks.json').read_text())
def select(suffix):return [r for r in rows if (r['label']==suffix or r['label'].endswith('-'+suffix))]
def fmt(v):return f'{statistics.median(v):.3f} ({min(v):.3f}–{max(v):.3f})' if v else '—'
print('| Scenario | Old process s: median (range) | New process s: median (range) | Broker End s | Consumer applied s | Speedup |')
print('|---|---:|---:|---:|---:|---:|')
for scenario in ('initial','noop','body','structural'):
 old=select('old-'+scenario);new=select('new-'+scenario)
 ot=[r['seconds'] for r in old];nt=[r['seconds'] for r in new]
 speed=f'{statistics.median(ot)/statistics.median(nt):.2f}×' if ot and nt else '—'
 print('| '+' | '.join([scenario,fmt(ot),fmt(nt),fmt([r['broker_end_s'] for r in new if 'broker_end_s' in r]),fmt([r['consumer_end_s'] for r in new if 'consumer_end_s' in r]),speed])+' |')
print('\nValidation:')
for check in checks:print(('PASS' if check['pass'] else 'FAIL'),check['check'])
reference={'full':'79e537d738832cd975a4d8b679bd16b9a5c06d7c88c0f41c92a4088d50e8054f','ts':'5236fa12c2b0ed0141511bd89f6b6f826e53d920c28c45ea79c4270b1385bf66'}
p=json.loads((root/'provenance.json').read_text())
direct={'full':'d727bfe7f954069a6b16c636c8ad6bd87b288bf11a5e214c71b88b3b75eae2d2','ts':'a52bd38e666922e1db871e766bd43b5495483c9154fd5b1f84a9a54f13120f3b'}
for r in select('new-initial'):
 print('DIRECT_IO_CONTRACT_MATCH',r['label'],r.get('graph_hash')==direct[p['profile']])
 print('PREOPT_COLD_MATCH',r['label'],r.get('graph_hash')==reference[p['profile']],r.get('graph_hash'))
print('\nResource and output:')
for r in rows:
 if '-new-' in r['label'] or r['label'].startswith('new-'):
  w=r.get('wire',{});print(r['label'],json.dumps({k:r.get(k) for k in ['rss_bytes','state_peak_sampled_bytes','spool_peak_sampled_bytes','state_final_bytes','result']},ensure_ascii=False),json.dumps({k:w.get(k) for k in ['messages','bytes','nodes','edges','total_nodes','total_edges','empty_batches']}))
