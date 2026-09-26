"""Preregistered normal-pressure/no-new-swapout evidence; Swapins contextual."""
import json
import re
from pressure_monitor import validate

def counters(raw):
    found = re.findall(r"^(Swapins|Swapouts):\s+(\d+)\.", raw, re.M)
    if len(found) != 2 or {k for k,v in found} != {"Swapins", "Swapouts"}:
        raise ValueError("missing or duplicate swap counters")
    return {k:int(v) for k,v in found}

def require_memory_evidence(base, row):
    label = f"{row['arm']}-{row['repeat']}"
    before = counters((base/(label+'-vm-before.txt')).read_text())
    after = counters((base/(label+'-vm-after.txt')).read_text())
    delta = {k:after[k]-before[k] for k in before}
    if any(v < 0 for v in delta.values()):
        raise ValueError("swap counter reset")
    if delta['Swapouts'] != 0:
        raise ValueError("new swapouts invalidate cohort")
    samples = [json.loads(line) for line in (base/(label+'-pressure.jsonl')).read_text().splitlines()]
    pressure = validate(samples, row['monotonic_start'], row['monotonic_end'])
    return dict(swap_pages=delta, pressure=pressure,
                swapins_policy="context only; no disqualification or bounded-latency claim")
