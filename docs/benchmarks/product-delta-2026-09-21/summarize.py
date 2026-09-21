"""Summarize completed benchmark roots without treating unverified data as acceptance."""
import argparse,json,re,statistics
from pathlib import Path
p=argparse.ArgumentParser();p.add_argument('roots',nargs='+');a=p.parse_args()
for name in a.roots:
 root=Path(name);rows=json.loads((root/'metrics.json').read_text());checks=json.loads((root/'checks.json').read_text())
 print('\n## '+root.name+'\n')
 print('Checks: '+str(sum(bool(c['pass']) for c in checks))+'/'+str(len(checks))+' passed. Run conditions and review status remain in the accompanying report.\n')
 print('| Operation | n | Median s | Min–max s | Parsed (min–max) |')
 print('|---|---:|---:|---:|---:|')
 groups={}
 for r in rows:
  label=re.sub(r'^r[0-9]+-','',r['label']);label=re.sub(r'noop[0-9]+$','noop',label)
  groups.setdefault(label,[]).append(r)
 for label,group in groups.items():
  times=[r['seconds'] for r in group];parsed=[r.get('aggregate_parsed',r.get('result',{}).get('ParsedFiles')) for r in group]
  parsed=[value for value in parsed if value is not None];parse_text=f'{min(parsed)}–{max(parsed)}' if parsed else 'not measured'
  print(f'| {label} | {len(group)} | {statistics.median(times):.6f} | {min(times):.6f}–{max(times):.6f} | {parse_text} |')
