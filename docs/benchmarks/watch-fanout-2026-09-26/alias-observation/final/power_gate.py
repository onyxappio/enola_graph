"""Prospective AC/charge/power-mode checks at arm boundaries, outside timing."""
import json, pathlib, re, subprocess, time

MIN_CHARGE = 20

def validate(observation):
    battery = observation['battery_output']
    if "Now drawing from 'AC Power'" not in battery:
        raise ValueError('AC power required')
    charge = re.search(r'\b(\d+)%;', battery)
    if not charge or int(charge.group(1)) < MIN_CHARGE:
        raise ValueError('battery charge below 20 percent or unavailable')
    custom = observation['custom_output']
    if 'AC Power:' not in custom:
        raise ValueError('AC power settings unavailable')
    ac = custom.split('AC Power:', 1)[1]
    mode = re.search(r'^\s*lowpowermode\s+(\d+)\s*$', ac, re.M)
    if not mode or mode.group(1) != '0':
        raise ValueError('Low Power Mode must be off on AC')
    return {'source': 'AC', 'charge_percent': int(charge.group(1)), 'low_power_mode': 0}

def capture(path):
    observation = {'wall_time': time.time(), 'monotonic': time.monotonic()}
    for key, cmd in [('battery_output', ['pmset', '-g', 'batt']),
                     ('custom_output', ['pmset', '-g', 'custom'])]:
        observation[key] = subprocess.check_output(cmd, text=True, timeout=5)
    # Save rejected observations too.
    pathlib.Path(path).write_text(json.dumps(observation, indent=2) + '\n')
    return validate(observation)

def require_power_evidence(base, row):
    label = str(row['arm']) + '-' + str(row['repeat'])
    before = json.loads((base / (label + '-power-before.json')).read_text())
    after = json.loads((base / (label + '-power-after.json')).read_text())
    result = {'before': validate(before), 'after': validate(after)}
    if not before['monotonic'] <= row['monotonic_start'] < row['monotonic_end'] <= after['monotonic']:
        raise ValueError('power observations do not bracket arm')
    result['coverage'] = 'boundaries only; does not prove uninterrupted AC within arm'
    return result
