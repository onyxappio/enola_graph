import re
EXPECTED_LABELS = {'r1-new-initial','r1-new-noop','r1-new-body','r1-new-structural','cold-structural','cold-body','cold-original'}
EXPECTED_CHECKS = {'r1-new-noop no-op','r1-new-initial cold graph equality','r1-new-body cold graph equality','r1-new-structural cold graph equality'}
def validate(metrics, checks):
 if len(metrics)!=7 or {m.get('label') for m in metrics}!=EXPECTED_LABELS: raise ValueError('incomplete metrics')
 if len(checks)!=4 or {c.get('check') for c in checks}!=EXPECTED_CHECKS or any(c.get('pass') is not True for c in checks): raise ValueError('missing or failed checks')
 rows={m['label']:m for m in metrics}
 for m in metrics:
  if m.get('exit')!=0 or not re.fullmatch('[0-9a-f]{64}',m.get('graph_hash') or ''): raise ValueError('failed run or invalid hash')
 for label,cold in [('r1-new-initial','cold-original'),('r1-new-body','cold-body'),('r1-new-structural','cold-structural')]:
  if rows[label]['graph_hash']!=rows[cold]['graph_hash']: raise ValueError('cold mismatch')
 noop=rows['r1-new-noop']; result=noop['result']
 if result['ParsedFiles']!=0 or result['BaseGeneration']!=result['TargetGeneration'] or noop['noop_wire_messages']!=0: raise ValueError('non-silent noop')
 if not re.fullmatch('[0-9a-f]{64}',noop.get('noop_state_before') or '') or noop['noop_state_before']!=noop.get('noop_state_after'): raise ValueError('state changed or unverified')
 if noop['graph_hash']!=rows['r1-new-initial']['graph_hash']: raise ValueError('noop graph changed')
 return True
