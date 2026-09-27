"""Validate timing authority without inventing an unavailable peer ACK."""
import hashlib, json, pathlib
import background_gate as bg

def load_observation(hold):
    root=pathlib.Path(hold['preflight_path'])
    receipt=json.loads((root/'receipt.json').read_text())
    roster=json.loads((root/'roster.json').read_text())
    if not receipt.get('valid') or receipt.get('failures') or receipt.get('samples',0)<60:
        raise ValueError('invalid observational preflight')
    for name in ('receipt.json','roster.json','samples.jsonl'):
        if hashlib.sha256((root/name).read_bytes()).hexdigest()!=hold['preflight_sha256'][name]:
            raise ValueError('preflight evidence pin mismatch')
    if receipt['source_sha256']!=hashlib.sha256(pathlib.Path(bg.__file__).read_bytes()).hexdigest():
        raise ValueError('collector source changed after calibration')
    if receipt['release_message']!=hold['plugin_message']:
        raise ValueError('plugin release identity mismatch')
    return roster,receipt['limits']

def validate_hold(hold):
    if hold.get('mode') == 'current-roster':
        if not hold.get('combined_authorized') or not hold.get('worker_message'):
            raise ValueError('missing combined authorization or primary acknowledgment')
        participants = hold.get('participants', [])
        if not participants or len({p['handle'] for p in participants}) != len(participants):
            raise ValueError('missing or duplicate participant roster')
        for participant in participants:
            path = pathlib.Path(participant['evidence_path'])
            if hashlib.sha256(path.read_bytes()).hexdigest() != participant['sha256']:
                raise ValueError('participant evidence pin mismatch')
            evidence = json.loads(path.read_text())
            if participant['status'] == 'held':
                message = evidence['result']['message']
                if message['id'] != participant['message_id'] or message['from_handle'] != participant['handle']:
                    raise ValueError('hold author/message mismatch')
                if message['created_at'][:10] != hold['start_utc'][:10]:
                    raise ValueError('stale participant acknowledgment')
                if not all(v in message['body'] for v in (hold['start_utc'][11:16], hold['end_utc'][11:16])):
                    raise ValueError('hold interval not present in participant acknowledgment')
            elif participant['status'] == 'terminal-held':
                terminal=evidence['result']['terminal']
                quote=participant['quote']
                if terminal['handle'] != participant['handle'] or quote not in '\n'.join(terminal['tail']):
                    raise ValueError('terminal acknowledgment author or quote mismatch')
                if evidence['observed_at'][:10] != hold['start_utc'][:10]:
                    raise ValueError('stale terminal acknowledgment')
                if not all(v in quote for v in (hold['start_utc'][11:16],hold['end_utc'][11:16])):
                    raise ValueError('terminal acknowledgment interval mismatch')
            elif participant['status'] == 'exited':
                terminal = evidence['result']['terminal']
                if terminal['handle'] != participant['handle'] or not terminal.get('exitCause'):
                    raise ValueError('no positive terminal exit evidence')
            else:
                raise ValueError('unconfirmed participant state')
        if hold['worker_message'] not in [p.get('message_id') for p in participants if p['status'] == 'held']:
            raise ValueError('primary hold acknowledgment missing')
        return
    if not all(hold.get(k) for k in ('codata_message','worker_message','plugin_message')):
        raise ValueError('missing available participant acknowledgment')
    if not hold.get('stage23_authorized') or not hold.get('stage24_authorized'):
        raise ValueError('unauthorized stage')
    mode=hold.get('mode','coordinated')
    if mode=='coordinated':
        if not hold.get('fsm_message'): raise ValueError('missing FSM acknowledgment')
    elif mode=='controlled-background':
        if not hold.get('observation_review_message'): raise ValueError('missing observational review provenance')
        load_observation(hold)
    else:
        raise ValueError('unknown timing authority mode')
