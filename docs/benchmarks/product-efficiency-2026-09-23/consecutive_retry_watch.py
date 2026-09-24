#!/usr/bin/env python3
"""Controlled two-refusal Product watch diagnostic; same flags as watch.py."""
import ast
from pathlib import Path

HERE = Path(__file__).resolve().parent
CONTROLLED = HERE / 'controlled_retry_watch.py'
BASE = HERE.parent / 'product-efficiency-2026-09-22/watch.py'
constants = {}
for node in ast.parse(CONTROLLED.read_text()).body:
    if isinstance(node, ast.Assign) and len(node.targets) == 1 and isinstance(node.targets[0], ast.Name) and node.targets[0].id in {'OLD', 'NEW'}:
        constants[node.targets[0].id] = ast.literal_eval(node.value)
source = BASE.read_text()
if source.count(constants['OLD']) != 1:
    raise SystemExit('watch harness changed; review injection before running')
new = constants['NEW']
start = new.index('        # Diagnostic scheduling:')
finish = new.index('        burst_timeout =', start)
new = new[:start] + r'''        # Supersede two successive previews, keeping unrelated edits stable.
        injections = []
        with watch_err_path.open() as stream:
            stream.seek(trace_offset)
            for attempt in range(2):
                deadline = time.monotonic() + 40
                captured = []
                while time.monotonic() < deadline:
                    line = stream.readline()
                    if not line:
                        if watch_proc.poll() is not None:
                            raise RuntimeError("watch exited before diagnostic edit")
                        time.sleep(.001)
                        continue
                    captured.append(line)
                    if "[graph-profile] ts_frozen_preview " in line:
                        break
                else:
                    raise RuntimeError(f"no preview signal for attempt {attempt}")
                text = edit_path.read_text() + f"\nexport const __enolaRetryTrigger{attempt} = {71 + attempt};\n"
                injected = {"kind": "burst", "path": str(edit_path), **write_durable(edit_path, text)}
                edits.append(injected)
                injections.append({"attempt": attempt, "trace": captured, "edit": injected})
                (work / "diagnostic-injections.json").write_text(json.dumps({
                    "instrumented": True, "trigger": "first two post-edit frozen-preview logs",
                    "independent_paths": independent_paths, "injections": injections,
                    "note": "controlled scheduling, not natural editing latency"
                }, indent=2))
''' + new[finish:]
source = source.replace(constants['OLD'], new, 1).replace(
    '"measurement_kind": "production-graph-watch",',
    '"measurement_kind": "instrumented-production-watch-two-consecutive-refusals",', 1)
exec(compile(source, str(BASE), 'exec'), {'__file__': str(BASE), '__name__': '__main__'})
