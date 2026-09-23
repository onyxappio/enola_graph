#!/usr/bin/env python3
"""Request-driven frozen v2 resident Product benchmark.

Mutates only an isolated Product snapshot. Real NATS JetStream and an
independent observer built from the same Enola revision. Not a performance
acceptance result until coordinator runs it.

This driver is request-driven ApplyChanges (benchresident). It is not
production `enola graph watch`. Watch measurements live in watch.py.
"""
from __future__ import annotations

import argparse
import hashlib
import json
import os
import re
import select
import shutil
import signal
import subprocess
import sys
import threading
import time
from pathlib import Path

HERE = Path(__file__).resolve().parent
SUPPORT = HERE.parent / "product-delta-2026-09-21"
sys.path.insert(0, str(SUPPORT))
from harness_support import restore_all, wait_process  # noqa: E402

PASSWORD = Path("packages/crypto/src/password.ts")
FORBIDDEN_BINARIES = (
    "enola-product-frozen-v2-enola",
    "enola-product-old",
    "enola-product-observer",
)


def refuse_stale(path: Path) -> None:
    name = path.name
    if any(token in str(path) for token in FORBIDDEN_BINARIES):
        raise SystemExit(f"refusing stale/v1 binary {path}")


p = argparse.ArgumentParser()
p.add_argument("--binary", required=True)
p.add_argument("--resident", required=True)
p.add_argument("--observer", required=True)
p.add_argument("--observer-pid", type=int, default=0, help="fail promptly if this observer process exits")
p.add_argument("--observer-stderr", default="", help="observer stderr path to surface FAILED lines")
p.add_argument("--root", required=True)
p.add_argument("--source", default="/tmp/enola-product-benchmark-source")
p.add_argument("--nats", required=True)
p.add_argument("--repeat", type=int, default=1)
p.add_argument("--source-mode", choices=["watch", "queue"], default="watch")
p.add_argument("--scope-config", default=str(HERE / "product-graph-scope.yaml"))
p.add_argument("--rev", default="")
args = p.parse_args()
if args.repeat < 1:
    p.error("--repeat must be positive")
for item in (args.binary, args.resident, args.observer):
    refuse_stale(Path(item))

root = Path(args.root).resolve()
root.mkdir(parents=True, exist_ok=True)
if any(root.glob("state*")) or (root / "provenance.json").exists():
    raise RuntimeError("Output root contains prior run artifacts; use a fresh root")
source = Path(args.source).absolute()
live = root / "product-live"
if live.exists():
    shutil.rmtree(live)
try:
    subprocess.run(["cp", "-cR", str(source), str(live)], check=True)
except subprocess.CalledProcessError:
    subprocess.run(["cp", "-a", str(source), str(live)], check=True)
# Overlay dirty Product graph-input policy without writing the original tree.
src_cfg = source / "mcp-arch.yaml"
if src_cfg.is_file():
    (live / "mcp-arch.yaml").write_bytes(src_cfg.read_bytes())
repo = live
f = repo / PASSWORD
original = f.read_text()
body = original.replace("return email.trim().toLowerCase();", "return email.normalize('NFKC').trim().toLowerCase();")
if body == original:
    raise RuntimeError("body edit snippet missing in password.ts")

config_dir = root / "config"
config_dir.mkdir()
cfg = config_dir / "config.yaml"
cfg.write_text("repo: " + str(repo) + "\nextractors: [typescript]\nexplainers: []\nrenderers: []\n")
scope_text = Path(args.scope_config).read_text()
if re.search(r"^repo(?:s)?\s*:", scope_text, re.M):
    raise RuntimeError("scope-config must not set repository")
with cfg.open("a") as stream:
    stream.write("\n" + scope_text)

rows = []
checks = []
hashes = {}
prefix = root.name
consumer_file = root / "consumer.jsonl"


def stop_process(child):
    if child is None:
        return
    try:
        if child.poll() is None:
            try:
                os.killpg(child.pid, signal.SIGTERM)
            except ProcessLookupError:
                pass
            try:
                child.wait(timeout=10)
            except subprocess.TimeoutExpired:
                try:
                    os.killpg(child.pid, signal.SIGKILL)
                except ProcessLookupError:
                    pass
                child.wait()
    finally:
        for channel in [child.stdin, child.stdout, child.stderr]:
            if channel:
                try:
                    channel.close()
                except OSError:
                    pass


def interrupted(signum, frame):
    raise KeyboardInterrupt("Signal " + str(signum))


signal.signal(signal.SIGTERM, interrupted)
signal.signal(signal.SIGINT, interrupted)


def save():
    (root / "metrics.json").write_text(json.dumps(rows, indent=2) + "\n")
    (root / "checks.json").write_text(json.dumps(checks, indent=2) + "\n")


(root / "provenance.json").write_text(
    json.dumps(
        {
            "rev": args.rev,
            "product_sha": subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=source).decode().strip(),
            "binary": args.binary,
            "binary_sha256": hashlib.sha256(Path(args.binary).read_bytes()).hexdigest(),
            "resident": args.resident,
            "resident_sha256": hashlib.sha256(Path(args.resident).read_bytes()).hexdigest(),
            "observer": args.observer,
            "observer_sha256": hashlib.sha256(Path(args.observer).read_bytes()).hexdigest(),
            "nats": args.nats,
            "measurement_kind": "request-driven-resident",
            "not_production_graph_watch": True,
            "authoritative_files": True,
            "max_begin_bytes": 1048576,
            "source_mode": args.source_mode,
            "config": cfg.read_text(),
            "started_ns": time.time_ns(),
        },
        indent=2,
    )
    + "\n"
)


def observer_alive() -> bool:
    if not args.observer_pid:
        return True
    try:
        os.kill(args.observer_pid, 0)
        return True
    except OSError:
        return False


def observer_fail_text() -> str:
    path = args.observer_stderr
    if not path:
        return ""
    p = Path(path)
    if not p.is_file():
        return ""
    text = p.read_text()
    lines = [ln for ln in text.splitlines() if ln.startswith("FAILED:") or "panic:" in ln]
    return "\n".join(lines[-8:])


def streaminfo():
    if not observer_alive():
        raise RuntimeError("observer exited\n" + observer_fail_text())
    return json.loads(subprocess.check_output([args.observer, args.nats, "--info"]))


def wire(ctx, start, run_id):
    until = time.monotonic() + 120
    while time.monotonic() < until:
        if not observer_alive():
            raise RuntimeError("observer exited while waiting for " + ctx + "\n" + observer_fail_text())
        if consumer_file.exists():
            for line in consumer_file.read_text().splitlines():
                try:
                    v = json.loads(line)
                except json.JSONDecodeError:
                    continue
                if v.get("run_id") == run_id and (v.get("context") == ctx or not ctx):
                    if v.get("first_ns", 0) and start and v.get("first_ns") < start - 10**12:
                        continue
                    if not re.fullmatch(r"[0-9a-f]{64}", v.get("normalized_hash", "")):
                        raise RuntimeError("Invalid consumer graph digest")
                    if v.get("time_base") and v.get("time_base") != "broker_metadata_timestamp":
                        raise RuntimeError("observer time_base is not broker metadata")
                    return v
        time.sleep(0.05)
    raise RuntimeError("No completed consumer frame for " + ctx + "\n" + observer_fail_text())


def ng(label, state, ctx):
    print("START", label, flush=True)
    ns = time.time_ns()
    t = time.monotonic()
    cmd = [
        args.binary,
        "graph",
        "analyze",
        "--authoritative-scope",
        "--max-begin-bytes",
        "1048576",
        "--summary-json",
        "--nats",
        args.nats,
        "--state-dir",
        str(root / state),
        "--context",
        ctx,
        str(cfg),
    ]
    with (root / (label + ".out")).open("w") as out, (root / (label + ".log")).open("w") as err:
        child = subprocess.Popen(["/usr/bin/time", "-l", *cmd], cwd=root, stdout=out, stderr=err, start_new_session=True)
        wait_process(child, 600)
    if child.returncode:
        raise RuntimeError(label + " failed: " + (root / (label + ".log")).read_text()[-1800:])
    result_rows = json.loads((root / (label + ".out")).read_text())
    m = {"label": label, "start_ns": ns, "seconds": time.monotonic() - t, "result": result_rows[0], "exit": 0}
    m["result"].pop("Facts", None)
    w = wire(ctx, ns, m["result"]["RunID"])
    m["wire"] = w
    m["graph_hash"] = w["normalized_hash"]
    hashes[ctx] = w["normalized_hash"]
    for k in ["first_ns", "first_batch_ns", "broker_end_ns", "consumer_end_ns"]:
        if k in w:
            m[k.replace("_ns", "_s")] = (w[k] - ns) / 1e9
    rows.append(m)
    save()
    print("DONE", label, round(m["seconds"], 3), flush=True)
    return m


proc = None


def receive(timeout=180):
    if not select.select([proc.stdout], [], [], timeout)[0]:
        raise RuntimeError("resident response timeout")
    line = proc.stdout.readline()
    if not line:
        raise RuntimeError("resident exited before response; see driver log")
    return json.loads(line)


def online(label, ctx, request=None, newtext=None, initial=False, expect_noop=False):
    before = driver_before if initial else streaminfo()
    ns = time.time_ns()
    t = time.monotonic()
    if newtext is not None:
        f.write_text(newtext)
    if request is not None:
        proc.stdin.write(json.dumps(request) + "\n")
        proc.stdin.flush()
    event = receive()
    res = event["result"]
    m = {"label": label, "start_ns": ns, "seconds": time.monotonic() - t, "driver": event, "result": res}
    results = [res, *(event.get("coverage_catchup") or [])]
    m["aggregate_parsed"] = sum(x["ParsedFiles"] for x in results)
    m["aggregate_work"] = {k: sum(x["Work"][k] for x in results) for k in res["Work"]}
    if initial:
        if res["BaseGeneration"] != 0 or res["TargetGeneration"] != 1:
            raise RuntimeError("Resident initial is not fresh")
        m["seconds"] = time.monotonic() - driver_started
        m["start_ns"] = driver_start_ns
        ns = driver_start_ns
    if res["BaseGeneration"] != res["TargetGeneration"]:
        w = wire(ctx, ns, m["result"]["RunID"])
        m["wire"] = w
        hashes[ctx] = w["normalized_hash"]
        for k in ["first_ns", "first_batch_ns", "broker_end_ns", "consumer_end_ns"]:
            if k in w:
                m[k.replace("_ns", "_s")] = (w[k] - ns) / 1e9
    m["graph_hash"] = hashes.get(ctx)
    m["wire_messages"] = streaminfo()["last_seq"] - before["last_seq"]
    work = res.get("Work") or {}
    if "-noop" in label and "-duplicate" not in label:
        checks.append(
            {
                "check": label + " zero work/no events",
                "pass": bool(work)
                and {"InventoryScans", "HashedFiles", "Checkpoints", "PublishedEvents", "FactAssemblies"}.issubset(work)
                and m["wire_messages"] == 0
                and all(
                    x["ParsedFiles"] == 0
                    and x["BaseGeneration"] == res["BaseGeneration"] == x["TargetGeneration"]
                    and bool(x["Work"])
                    and all(v == 0 for v in x["Work"].values())
                    for x in results
                ),
            }
        )
    if expect_noop or "-duplicate" in label:
        verify = {"HashedFiles", "DirtyHashBytes", "InventoryScans"}
        hashed = 0
        other_zero = True
        for x in results:
            w = x.get("Work") or {}
            hashed += int(w.get("HashedFiles") or 0)
            if x.get("ParsedFiles") != 0:
                other_zero = False
            if x.get("BaseGeneration") != x.get("TargetGeneration"):
                other_zero = False
            if int(w.get("PublishedEvents") or 0) != 0 or int(w.get("Checkpoints") or 0) != 0 or int(w.get("FactAssemblies") or 0) != 0:
                other_zero = False
            for k, v in w.items():
                if k in verify:
                    continue
                if k in {"PublishedEvents", "Checkpoints", "FactAssemblies"}:
                    continue
                if v not in (0, None):
                    other_zero = False
        checks.append(
            {
                "check": label + " same-content duplicate: no parse/publish/checkpoint/fact assembly, generation unchanged (verification hash of at most one file allowed)",
                "pass": other_zero and m["wire_messages"] == 0 and hashed <= 1 and res["BaseGeneration"] == res["TargetGeneration"],
            }
        )
    if request and request.get("paths") and not expect_noop and "-duplicate" not in label and "body" in label:
        batch = event["observed_batch"]
        expected = set(request["paths"])
        observed = {os.path.relpath(x, repo) if os.path.isabs(x) else x for x in batch["Paths"]}
        checks.append(
            {
                "check": label + " real observed input and fast path",
                "pass": expected.issubset(observed) and batch["Covered"] and not batch["Reconcile"] and not res["Reconciled"],
            }
        )
    rows.append(m)
    save()
    print("DONE", label, round(m["seconds"], 6), "parsed", m["aggregate_parsed"], flush=True)
    return m


initial_runs = []
idle_runs = []
dup_runs = []
body_runs = []
try:
    for i in range(1, args.repeat + 1):
        f.write_text(original)
        ctx = prefix + f"-main{i}"
        state = root / f"state{i}"
        log = (root / f"r{i}-resident.log").open("w")
        driver_before = streaminfo()
        driver_started = time.monotonic()
        driver_start_ns = time.time_ns()
        proc = subprocess.Popen(
            [
                "/usr/bin/time",
                "-l",
                args.resident,
                "--repo",
                str(repo),
                "--config",
                str(cfg),
                "--state",
                str(state),
                "--context",
                ctx,
                "--nats",
                args.nats,
                "--source",
                args.source_mode,
            ],
            stdin=subprocess.PIPE,
            stdout=subprocess.PIPE,
            stderr=log,
            text=True,
            bufsize=1,
            start_new_session=True,
        )
        init = online(f"r{i}-resident-initial", ctx, initial=True)
        initial_runs.append(init)
        for j in range(10):
            idle_runs.append(online(f"r{i}-resident-noop{j}", ctx, {"label": f"noop{j}"}))
        f.write_text(original)
        dup_runs.append(
            online(
                f"r{i}-resident-duplicate",
                ctx,
                {"label": "duplicate", "paths": [str(PASSWORD)]},
                original,
                expect_noop=True,
            )
        )
        body_runs.append(online(f"r{i}-resident-body", ctx, {"label": "body", "paths": [str(PASSWORD)]}, body))
        proc.stdin.close()
        proc.wait(timeout=20)
        if proc.returncode:
            raise RuntimeError("resident exit " + str(proc.returncode))
        proc = None
        log.close()
        rss_match = re.search(r"(\d+)\s+maximum resident set size", (root / f"r{i}-resident.log").read_text())
        init["lifetime_rss_bytes"] = int(rss_match[1]) if rss_match else None
        save()
    f.write_text(body)
    body_ref = ng("cold-body", "cold-body", prefix + "-cold-body")["graph_hash"]
    f.write_text(original)
    initial_ref = ng("cold-original", "cold-original", prefix + "-cold-original")["graph_hash"]
    for op, ref in [(initial_runs, initial_ref), (body_runs, body_ref)]:
        for d in op:
            checks.append({"check": d["label"] + " cold equality", "pass": d["graph_hash"] == ref})
    save()
    print("CHECKS", checks, flush=True)
    if any(not c["pass"] for c in checks):
        raise RuntimeError("resident acceptance failed")
finally:
    restore_all(
        [
            ("stop resident", lambda: stop_process(proc)),
            ("restore password", lambda: f.write_text(original)),
        ]
    )
