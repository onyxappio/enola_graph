#!/usr/bin/env python3
"""Prepare and (when agreed) launch frozen v2 resident Product efficiency runs.

Compares Enola 3564785 (current frozen v2 main) with parent 6430d25.
Builds into private /tmp snapshots so cmd/benchresident does not enter the
tracked tree. Does not mutate Product source or historical result dumps.

Self-test is cheap. Full Product runs and snapshot builds wait for coordinator
agreement.
"""
from __future__ import annotations

import argparse
import hashlib
import json
import os
import shutil
import socket
import subprocess
import sys
import tempfile
import time
from pathlib import Path

HERE = Path(__file__).resolve().parent
ENOLA_ROOT = HERE.parents[2]
CANDIDATE = "35647859c639ead615307107f7a956b2ce8a6a9d"
PARENT = "6430d258694073b90c5c6847da239bdf4407f582"
NATS_SERVER = Path("/tmp/enola-toolchain/bin/nats-server")
GO = Path("/tmp/enola-toolchain/go/bin/go")
GOFMT = Path("/tmp/enola-toolchain/go/bin/gofmt")
FORBIDDEN = ("enola-product-frozen-v2-enola", "enola-product-old", "enola-product-observer")


def run(cmd, cwd=None, check=True, **kw):
    return subprocess.run(cmd, cwd=cwd, check=check, text=True, capture_output=True, **kw)


def snapshot_dir(rev: str) -> Path:
    return Path(f"/tmp/enola-efficiency-2026-09-22-{rev[:12]}")


def prepare_snapshot(rev: str, enola_root: Path) -> Path:
    dest = snapshot_dir(rev)
    if dest.exists():
        shutil.rmtree(dest)
    run(["git", "clone", "--quiet", str(enola_root), str(dest)])
    run(["git", "-C", str(dest), "checkout", "--quiet", rev])
    (dest / "cmd" / "benchresident").mkdir(parents=True)
    (dest / "cmd" / "benchobserver").mkdir(parents=True)
    shutil.copyfile(HERE / "resident-driver.go.txt", dest / "cmd" / "benchresident" / "main.go")
    shutil.copyfile(HERE / "observer.go.txt", dest / "cmd" / "benchobserver" / "main.go")
    env = os.environ.copy()
    env["PATH"] = str(GO.parent) + ":" + env.get("PATH", "")
    run([str(GOFMT), "-w", "cmd/benchresident/main.go", "cmd/benchobserver/main.go"], cwd=dest, env=env)
    return dest


def build_snapshot(dest: Path) -> dict:
    env = os.environ.copy()
    env["PATH"] = "/tmp/enola-toolchain/go/bin:/tmp/enola-toolchain/bin:" + env.get("PATH", "")
    bins = dest / "bin"
    bins.mkdir(exist_ok=True)
    for name, pkg in (("enola", "./cmd/enola"), ("benchresident", "./cmd/benchresident"), ("benchobserver", "./cmd/benchobserver")):
        out = bins / name
        proc = subprocess.run([str(GO), "build", "-o", str(out), pkg], cwd=dest, env=env, capture_output=True, text=True)
        if proc.returncode:
            raise RuntimeError(f"build {name} failed:\n{proc.stderr[-4000:]}")
    return {
        "enola": str(bins / "enola"),
        "resident": str(bins / "benchresident"),
        "observer": str(bins / "benchobserver"),
        "enola_sha256": hashlib.sha256((bins / "enola").read_bytes()).hexdigest(),
        "resident_sha256": hashlib.sha256((bins / "benchresident").read_bytes()).hexdigest(),
        "observer_sha256": hashlib.sha256((bins / "benchobserver").read_bytes()).hexdigest(),
    }


def write_nats_conf(work: Path, port: int) -> Path:
    store = work / "nats-store"
    store.mkdir(parents=True, exist_ok=True)
    conf = work / "nats.conf"
    text = (HERE / "nats.conf.template").read_text()
    conf.write_text(text.replace("PORT", str(port)).replace("STORE_DIR", str(store)))
    return conf


def wait_port(host: str, port: int, timeout: float = 15) -> None:
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        try:
            with socket.create_connection((host, port), timeout=0.2):
                return
        except OSError:
            time.sleep(0.05)
    raise RuntimeError(f"nothing listening on {host}:{port}")


def self_test() -> int:
    failures = []

    def check(name: str, cond: bool, detail: str = ""):
        if not cond:
            failures.append(f"{name}: {detail or 'failed'}")

    driver = (HERE / "resident-driver.go.txt").read_text()
    observer = (HERE / "observer.go.txt").read_text()
    resident = (HERE / "resident.py").read_text()
    check("AuthoritativeFiles true", "AuthoritativeFiles: true" in driver)
    check("MaxBeginBytes 1MiB", "MaxBeginBytes:      1024 * 1024" in driver or "MaxBeginBytes: 1024 * 1024" in driver)
    check("NATS MaxPayload 1MiB", "MaxPayload: 1024 * 1024" in driver)
    check("independent Consumer", "graphsession.NewConsumer()" in observer)
    check("Canonical hash", "cons.Canonical()" in observer)
    check("route by run_id", "unregistered run" in observer)
    check("persistent context consumer", "byContext" in observer)
    check("broker timestamp", "brokerTimestampNS" in observer)
    check("strict stream sequence", "nextSeq" in observer and "pending[nextSeq]" in observer)
    check("ordered Fetch not Consume callbacks", "cons.Fetch(" in observer and "Consume(" not in observer)
    check("reject malformed stream", "malformed stream" in observer)
    check("observer pid fail-fast", "observer-pid" in resident)
    check("smoke waits READY before graph", "observer READY timeout" in (HERE / "run.py").read_text())
    check("smoke uses ready file", "OBSERVER_READY_FILE" in (HERE / "run.py").read_text())
    check("observer writes ready file", "OBSERVER_READY_FILE" in observer)
    check("no v1 observer default", "enola-product-observer" not in resident.split("FORBIDDEN")[0])
    check("idle scenario", "resident-noop" in resident)
    check("duplicate same-content", "resident-duplicate" in resident)
    check("duplicate allows verification hash", "verification hash of at most one file allowed" in resident)
    check("idle still zero work", 'label + " zero work/no events"' in resident)
    check("body edit", "resident-body" in resident)
    check("cold equality", "cold equality" in resident)
    check("authoritative CLI cold", "--authoritative-scope" in resident)
    check("nats-server path", NATS_SERVER.is_file(), str(NATS_SERVER))
    check("gofmt present", GOFMT.is_file(), str(GOFMT))
    check("go present", GO.is_file(), str(GO))
    check("parent/child revs", CANDIDATE.startswith("3564785") and PARENT.startswith("6430d25"))
    with tempfile.TemporaryDirectory() as tmp:
        tmp_path = Path(tmp)
        for name in ("resident-driver.go.txt", "observer.go.txt"):
            src = (HERE / name).read_text()
            dest = tmp_path / (name.replace(".txt", ""))
            dest.write_text(src)
            proc = subprocess.run([str(GOFMT), "-l", str(dest)], capture_output=True, text=True)
            # gofmt -l on non-module file still formats syntax
            if proc.returncode != 0:
                failures.append(f"gofmt {name}: {proc.stderr}")
            formatted = subprocess.check_output([str(GOFMT), str(dest)])
            dest.write_bytes(formatted)
            check(f"gofmt {name}", True)
    if failures:
        print("self-test FAIL", file=sys.stderr)
        for item in failures:
            print(" ", item, file=sys.stderr)
        return 1
    print("self-test ok")
    print("nats-server", NATS_SERVER)
    print("candidate", CANDIDATE)
    print("parent", PARENT)
    print("ready: tiny --smoke uses a fresh NATS port; do not touch 14232")
    return 0


def install_observer(dest: Path) -> Path:
    shutil.copyfile(HERE / "observer.go.txt", dest / "cmd" / "benchobserver" / "main.go")
    env = os.environ.copy()
    env["PATH"] = "/tmp/enola-toolchain/go/bin:/tmp/enola-toolchain/bin:" + env.get("PATH", "")
    run([str(GOFMT), "-w", "cmd/benchobserver/main.go"], cwd=dest, env=env)
    out = dest / "bin" / "benchobserver"
    proc = subprocess.run(
        [str(GO), "build", "-o", str(out), "./cmd/benchobserver"],
        cwd=dest,
        env=env,
        capture_output=True,
        text=True,
    )
    if proc.returncode:
        raise RuntimeError("rebuild observer failed:\n" + proc.stderr[-4000:])
    return out


def free_port() -> int:
    s = socket.socket()
    s.bind(("127.0.0.1", 0))
    port = s.getsockname()[1]
    s.close()
    if port == 14232:
        return free_port()
    return port


def smoke() -> int:
    """Tiny initial+delta+cold equality on a fresh NATS port. Not a Product run."""
    t0 = time.monotonic()
    dest = snapshot_dir(CANDIDATE)
    if not dest.exists() or not (dest / "bin" / "enola").is_file():
        print("preparing snapshot (enola already needed for smoke)", flush=True)
        dest = prepare_snapshot(CANDIDATE, ENOLA_ROOT)
        build_snapshot(dest)
    observer_bin = install_observer(dest)
    enola_bin = dest / "bin" / "enola"
    work = Path("/tmp/enola-observer-smoke-" + str(os.getpid()))
    if work.exists():
        shutil.rmtree(work)
    work.mkdir()
    port = free_port()
    if port == 14232:
        raise RuntimeError("refusing port 14232")
    conf = write_nats_conf(work, port)
    nats_log = (work / "nats.log").open("w")
    nats_proc = subprocess.Popen(
        [str(NATS_SERVER), "-c", str(conf)],
        stdout=nats_log,
        stderr=subprocess.STDOUT,
        start_new_session=True,
    )
    obs_proc = None
    try:
        wait_port("127.0.0.1", port)
        url = f"nats://127.0.0.1:{port}"
        jsonl = work / "consumer.jsonl"
        ready_file = work / "observer.ready"
        obs_err = (work / "observer.stderr").open("w")
        env = os.environ.copy()
        env["OBSERVER_READY_FILE"] = str(ready_file)
        obs_proc = subprocess.Popen(
            [str(observer_bin), url, str(jsonl)],
            stdout=obs_err,
            stderr=obs_err,
            env=env,
            start_new_session=True,
        )
        ready_deadline = time.monotonic() + 20
        while time.monotonic() < ready_deadline:
            if obs_proc.poll() is not None:
                raise RuntimeError("observer exited before READY:\n" + (work / "observer.stderr").read_text())
            marker = ready_file.read_text() if ready_file.is_file() else ""
            stderr_txt = (work / "observer.stderr").read_text()
            if "READY" in marker or "READY" in stderr_txt:
                break
            time.sleep(0.05)
        else:
            raise RuntimeError("observer READY timeout:\n" + (work / "observer.stderr").read_text())
        if not ready_file.is_file() and "READY" not in (work / "observer.stderr").read_text():
            raise RuntimeError("observer READY missing after wait")
        repo = work / "repo"
        repo.mkdir()
        (repo / "package.json").write_text('{"name":"smoke","type":"module"}\n')
        (repo / "tsconfig.json").write_text('{"compilerOptions":{"strict":true}}\n')
        (repo / "mcp-arch.yaml").write_text("repo: .\nextractors: [typescript]\nexplainers: []\nrenderers: []\n")
        (repo / "a.ts").write_text("export const a = 1;\n")
        cfg = work / "config.yaml"
        cfg.write_text("repo: " + str(repo) + "\nextractors: [typescript]\nexplainers: []\nrenderers: []\n")

        def graph(mode, state, ctx):
            cmd = [
                str(enola_bin),
                "graph",
                mode,
                "--authoritative-scope",
                "--max-begin-bytes",
                "1048576",
                "--summary-json",
                "--nats",
                url,
                "--state-dir",
                str(work / state),
                "--context",
                ctx,
                "--repo-id",
                "smoke",
                str(cfg),
            ]
            proc = subprocess.run(cmd, capture_output=True, text=True)
            if proc.returncode:
                raise RuntimeError(mode + " failed:\n" + proc.stderr[-2000:] + "\n" + proc.stdout[-1000:])
            rows = json.loads(proc.stdout)
            return rows[0]

        initial = graph("analyze", "state-live", "live")
        (repo / "a.ts").write_text("export const a = 2;\n")
        delta = graph("delta", "state-live", "live")
        cold = graph("analyze", "state-cold", "cold")
        deadline = time.monotonic() + 30
        frames = []
        while time.monotonic() < deadline:
            if obs_proc.poll() is not None:
                raise RuntimeError("observer exited:\n" + (work / "observer.stderr").read_text())
            if jsonl.is_file() and jsonl.read_text().strip():
                frames = [json.loads(ln) for ln in jsonl.read_text().splitlines() if ln.strip()]
                if len(frames) >= 3:
                    break
            time.sleep(0.05)
        if len(frames) < 3:
            raise RuntimeError("expected 3 observer frames, got " + str(len(frames)) + "\n" + jsonl.read_text())
        live_hash = frames[1]["normalized_hash"]
        cold_hash = frames[2]["normalized_hash"]
        if live_hash != cold_hash:
            raise RuntimeError(f"live delta hash {live_hash} != cold {cold_hash}")
        if frames[1].get("time_base") != "broker_metadata_timestamp":
            raise RuntimeError("broker timestamps not labeled")
        elapsed = round(time.monotonic() - t0, 3)
        report = {
            "work": str(work),
            "port": port,
            "elapsed_s": elapsed,
            "initial_generation": [initial.get("BaseGeneration"), initial.get("TargetGeneration")],
            "delta_generation": [delta.get("BaseGeneration"), delta.get("TargetGeneration")],
            "cold_generation": [cold.get("BaseGeneration"), cold.get("TargetGeneration")],
            "live_delta_hash": live_hash,
            "cold_hash": cold_hash,
            "equal": True,
            "observer_frames": len(frames),
            "rebuild": f"{GO} build -o {observer_bin} ./cmd/benchobserver  # cwd {dest}",
        }
        (work / "smoke.json").write_text(json.dumps(report, indent=2) + "\n")
        print(json.dumps(report, indent=2))
        print("smoke ok", elapsed, "s", work)
        return 0
    finally:
        for child in (obs_proc, nats_proc):
            if child is None:
                continue
            if child.poll() is None:
                try:
                    os.killpg(child.pid, 15)
                except (ProcessLookupError, PermissionError, AttributeError):
                    child.terminate()
                try:
                    child.wait(timeout=5)
                except Exception:
                    child.kill()
        nats_log.close()


def print_commands() -> None:
    print("Dependencies:")
    print("  nats-server", NATS_SERVER)
    print("  go/gofmt", GO, GOFMT)
    print("  Product snapshot source /tmp/enola-product-benchmark-source (read-only)")
    print("  overlay mcp-arch.yaml from that tree if present")
    print()
    print("Prepare private snapshots (does not touch tracked cmd/):")
    print("  python3", HERE / "run.py", "--prepare", "--rev", "candidate")
    print("  python3", HERE / "run.py", "--prepare", "--rev", "parent")
    print()
    print("Build (expensive; wait for agreement):")
    print("  python3", HERE / "run.py", "--build", "--rev", "candidate")
    print()
    print("Launch one measured repeat after broker+observer:")
    print("  NATS=/tmp/enola-toolchain/bin/nats-server")
    print("  $NATS -c $WORK/nats.conf")
    print("  $SNAP/bin/benchobserver nats://127.0.0.1:PORT $WORK/consumer.jsonl")
    print("  python3", HERE / "resident.py", "\\")
    print("    --binary $SNAP/bin/enola --resident $SNAP/bin/benchresident \\")
    print("    --observer $SNAP/bin/benchobserver --nats nats://127.0.0.1:PORT \\")
    print("    --root $WORK --repeat 1 --source /tmp/enola-product-benchmark-source")


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--self-test", action="store_true")
    parser.add_argument("--prepare", action="store_true")
    parser.add_argument("--build", action="store_true")
    parser.add_argument("--rev", choices=["candidate", "parent"], default="candidate")
    parser.add_argument("--enola-root", default=str(ENOLA_ROOT))
    parser.add_argument("--print-commands", action="store_true")
    parser.add_argument("--smoke", action="store_true", help="tiny isolated NATS initial+delta+cold; never uses port 14232")
    args = parser.parse_args()
    if args.self_test:
        return self_test()
    if args.smoke:
        return smoke()
    if args.print_commands:
        print_commands()
        return 0
    rev = CANDIDATE if args.rev == "candidate" else PARENT
    if args.prepare:
        dest = prepare_snapshot(rev, Path(args.enola_root))
        print("prepared", dest)
        if not args.build:
            return 0
    if args.build:
        dest = snapshot_dir(rev)
        if not dest.exists():
            dest = prepare_snapshot(rev, Path(args.enola_root))
        info = build_snapshot(dest)
        print(json.dumps(info, indent=2))
        return 0
    print_commands()
    return 0


if __name__ == "__main__":
    sys.exit(main())
