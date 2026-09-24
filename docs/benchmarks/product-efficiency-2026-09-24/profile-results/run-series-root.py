import argparse,datetime,json,pathlib,subprocess,sys
p=argparse.ArgumentParser();p.add_argument('--quiet-message-id',required=True);p.add_argument('--deadline-utc',required=True);a=p.parse_args()
b=pathlib.Path(__file__).resolve().parent
end=datetime.datetime.fromisoformat(a.deadline_utc)
assert end.tzinfo is not None
expected=None
for arm,n in [('baseline',1),('candidate',1),('candidate',2),('baseline',2),('baseline',3),('candidate',3)]:
 if (end-datetime.datetime.now(datetime.timezone.utc)).total_seconds()<60:raise SystemExit('Deadline margin reached; remaining runs deferred')
 cmd=[sys.executable,'-B',str(b/'run-profile-arm.py'),'--arm',arm,'--repeat',str(n),'--quiet-message-id',a.quiet_message_id]
 if n==1:cmd+=['--oracle']
 if expected:cmd+=['--expect-hash',expected]
 print('START',arm,n,datetime.datetime.now(datetime.timezone.utc).isoformat(),flush=True)
 subprocess.run(cmd,check=True)
 receipt=json.loads((b/f'{arm}-{n}'/'receipt.json').read_text());assert receipt['timing_eligible']
 if expected is None:expected=receipt['body_hash']
subprocess.run([sys.executable,'-B',str(b/'summarize.py')],check=True)
