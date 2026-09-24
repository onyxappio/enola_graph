#!/usr/bin/env python3
"""Controlled multi-file retry diagnostic using production graph watch.

Requires ENOLA_GRAPH_PROFILE=1 and the same CLI arguments as watch.py.
Edits three independent Product sources and password.ts, then supersedes
password.ts after its first post-edit frozen-preview trace. This is deliberate
scheduling for mechanism validation, not natural editing latency.
"""
from pathlib import Path

BASE = Path(__file__).resolve().parent.parent / "product-efficiency-2026-09-22" / "watch.py"
source = BASE.read_text()
OLD = r'''        burst_started = time.time_ns()
        for text in burst_texts:
            edits.append({"kind": "burst", "path": str(edit_path), **write_durable(edit_path, text)})
            time.sleep(0.05)
        burst_timeout = max(every_s + 15, 20)
'''
NEW = r'''        burst_started = time.time_ns()
        trace_offset = watch_err_path.stat().st_size
        independent_paths = [
            "packages/tracking-server/src/publisher.ts",
            "packages/state-machine-telemetry/src/identity.ts",
            "packages/scan-engine/src/providerJobClaimIdentity.ts",
        ]
        for i, rel in enumerate(independent_paths):
            target = repo / rel
            text = target.read_text() + f"\nexport const __enolaRetryProbe{i} = {i + 1};\n"
            edits.append({"kind": "burst", "path": str(target), **write_durable(target, text)})
        for text in burst_texts:
            edits.append({"kind": "burst", "path": str(edit_path), **write_durable(edit_path, text)})
            time.sleep(0.05)
        # Diagnostic scheduling: supersede exactly one member after preview,
        # leaving the independent parses eligible for reuse. No Enola hooks.
        deadline = time.monotonic() + 40
        captured = []
        with watch_err_path.open() as stream:
            stream.seek(trace_offset)
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
                raise RuntimeError("no preview signal for diagnostic edit")
        text = edit_path.read_text() + "\nexport const __enolaRetryTrigger = 71;\n"
        injected = {"kind": "burst", "path": str(edit_path), **write_durable(edit_path, text)}
        edits.append(injected)
        (work / "diagnostic-injection.json").write_text(json.dumps({
            "instrumented": True, "trigger": "first post-edit ts_frozen_preview log",
            "trace": captured, "independent_paths": independent_paths, "edit": injected,
            "note": "controlled production CLI scenario; not natural editor timing"
        }, indent=2))
        burst_timeout = max(every_s + 35, 40)
'''
if source.count(OLD) != 1:
    raise SystemExit("watch harness changed; review injection before running")
source = source.replace(OLD, NEW, 1).replace(
    '"measurement_kind": "production-graph-watch",',
    '"measurement_kind": "instrumented-production-watch-controlled-disjoint-retry",', 1)
exec(compile(source, str(BASE), "exec"), {"__file__": str(BASE), "__name__": "__main__"})
