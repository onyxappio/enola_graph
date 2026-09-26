"""Compare completed correctness diagnostics; wall time is not acceptance data."""
import argparse
import json
from pathlib import Path

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument("baseline", type=Path)
parser.add_argument("candidate", type=Path)
parser.add_argument("--output", type=Path, required=True)
args = parser.parse_args()

def load(path):
    rows = json.loads(path.read_text())
    if len(rows) != 10 or rows[-1]["label"] != "08-noop":
        raise ValueError(f"Incomplete diagnostic: {path}")
    for row in rows[:-1]:
        if row.get("full_field_graph_equal") is not True:
            raise ValueError(f"Cold equality missing: {path}: {row['label']}")
    if rows[-1].get("state_unchanged") is not True or rows[-1]["event_bytes"] != 0:
        raise ValueError(f"No-op contract not proven: {path}")
    return {r["label"]: r for r in rows}

base, candidate = load(args.baseline), load(args.candidate)
if set(base) != set(candidate):
    raise ValueError("Scenario sets differ")
result = {"kind": "correctness-and-wire-diagnostic", "timing_accepted": False,
          "baseline": str(args.baseline), "candidate": str(args.candidate), "scenarios": []}
for label, b in base.items():
    c = candidate[label]
    row = {"label": label}
    for metric in ("owners", "event_bytes", "batches"):
        row[metric] = {"baseline": b.get(metric, 0), "candidate": c.get(metric, 0)}
    for metric in ("ParsedFiles", "OwnersPublished"):
        row[metric] = {"baseline": b["summary"][0][metric], "candidate": c["summary"][0][metric]}
    result["scenarios"].append(row)
args.output.write_text(json.dumps(result, indent=2) + "\n")
