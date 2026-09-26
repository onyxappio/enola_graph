import copy, hashlib, json, tempfile
from pathlib import Path
from timing_gate import validate_hold
with tempfile.TemporaryDirectory() as td:
    path=Path(td)/'ack.json'
    ack={'result':{'message':{'id':'msg_test','from_handle':'term_test','body':'Heavy jobs idle; hold 17:05–17:30 UTC','created_at':'2026-09-26T17:00:00Z'}}}
    path.write_text(json.dumps(ack))
    member={'handle':'term_test','status':'held','message_id':'msg_test','evidence_path':str(path),'sha256':hashlib.sha256(path.read_bytes()).hexdigest()}
    hold={'mode':'current-roster','combined_authorized':True,'worker_message':'msg_test','start_utc':'2026-09-26T17:05:00+00:00','end_utc':'2026-09-26T17:30:00+00:00','participants':[member]}
    validate_hold(hold)
    def refused(value):
        try: validate_hold(value)
        except (ValueError, KeyError): return
        raise AssertionError('invalid hold accepted')
    refused(dict(hold,participants=[]))
    refused(dict(hold,worker_message='invented'))
    refused(dict(hold,combined_authorized=False))
    refused(dict(hold,end_utc='2026-09-26T18:30:00+00:00'))
    refused(dict(hold,start_utc='2026-09-27T17:05:00+00:00'))
    path.write_text('{}');refused(hold)
print('PASS: current roster rejects missing/mismatched/stale/tampered acknowledgments')
