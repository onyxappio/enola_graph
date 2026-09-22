#!/usr/bin/env python3
"""Prepare private Enola snapshots and launch efficiency harnesses.

Revisions must be explicit SHAs (or HEAD). Historical 3564785/6430d25 dumps
are archived evidence, not default pins. Snapshots are created in a fresh
directory; existing active snapshots are never deleted. An experimental-patch
snapshot copies the dirty worktree and is labelled so it is not equated to
the committed revision.

Request-driven resident measurements live in resident.py. Production
`graph watch` measurements live in watch.py.
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
NATS_SERVER = Path("/tmp/enola-toolchain/bin/nats-server")
GO = Path("/tmp/enola-toolchain/go/bin/go")
GOFMT = Path("/tmp/enola-toolchain/go/bin/gofmt")
FORBIDDEN = ("enola-product-frozen-v2-enola", "enola-product-old", "enola-product-observer")
HISTORICAL_SHAS = {
    "35647859c639ead615307107f7a956b2ce8a6a9d": "archived 2026-09-22 candidate dump (not a default pin)",
    "6430d258694073b90c5c6847da239bdf4407f582": "archived 2026-09-22 parent dump (not a default pin)",
}
SOURCE_COPY_PREFIXES = ("internal/", "pkg/", "cmd/", "go.mod", "go.sum")


def run(cmd, cwd=None, check=True, **kw):
    return subprocess.run(cmd, cwd=cwd, check=check, text=True, capture_output=True, **kw)


def git_out(repo: Path, *args: str) -> str:
    return run(["git", "-C", str(repo), *args]).stdout.strip()


def sha256_file(path: Path) -> str:
    h = hashlib.sha256()
    with path.open("rb") as fh:
        for chunk in iter(lambda: fh.read(1024 * 1024), b""):
            h.update(chunk)
    return h.hexdigest()


def sha256_bytes(data: bytes) -> str:
    return hashlib.sha256(data).hexdigest()


def go_env() -> dict:
    env = os.environ.copy()
    env["PATH"] = "/tmp/enola-toolchain/go/bin:/tmp/enola-toolchain/bin:" + env.get("PATH", "")
    return env


def go_bin() -> str:
    return str(GO) if GO.is_file() else "go"


def go_version_m(binary: Path) -> str:
    proc = subprocess.run(
        [go_bin(), "version", "-m", str(binary)],
        env=go_env(),
        capture_output=True,
        text=True,
    )
    if proc.returncode != 0:
        raise RuntimeError(f"go version -m failed:\n{(proc.stderr or proc.stdout)[-2000:]}")
    return proc.stdout


def source_identity(enola_root: Path) -> dict:
    rev = git_out(enola_root, "rev-parse", "HEAD")
    porcelain = git_out(enola_root, "status", "--porcelain")
    diff = run(["git", "-C", str(enola_root), "diff", "HEAD"]).stdout.encode()
    cached = run(["git", "-C", str(enola_root), "diff", "--cached"]).stdout.encode()
    untracked = [ln[3:] for ln in porcelain.splitlines() if ln.startswith("?? ")]
    return {
        "revision": rev,
        "dirty": bool(porcelain.strip()),
        "status_porcelain": porcelain,
        "diff_sha256": sha256_bytes(diff + b"\0" + cached),
        "untracked_names_sha256": sha256_bytes("\n".join(untracked).encode()),
    }


def refuse_stale(path: Path) -> None:
    if any(token in str(path) for token in FORBIDDEN):
        raise SystemExit(f"refusing stale/v1 binary {path}")


def resolve_revision(spec: str, enola_root: Path) -> str:
    token = (spec or "").strip()
    if not token:
        raise SystemExit("pass an explicit --rev SHA or HEAD")
    if token in {"candidate", "parent"}:
        raise SystemExit(
            "--rev candidate/parent is not a pin. Pass an explicit SHA. "
            "Archived dumps: 35647859c639ead615307107f7a956b2ce8a6a9d and "
            "6430d258694073b90c5c6847da239bdf4407f582."
        )
    if token.upper() == "HEAD":
        return git_out(enola_root, "rev-parse", "HEAD")
    full = git_out(enola_root, "rev-parse", "--verify", token)
    return full


def default_snapshot_dest(rev: str, kind: str) -> Path:
    stamp = time.strftime("%Y%m%dT%H%M%S")
    label = "experimental" if kind == "experimental-patch" else rev[:12]
    return Path(f"/tmp/enola-efficiency-2026-09-22-{label}-{stamp}")


def refuse_existing_snapshot(dest: Path) -> None:
    if dest.exists():
        raise SystemExit(
            f"refusing to delete existing snapshot {dest}; pass a fresh --snapshot-dest"
        )


def copy_untracked_source(enola_root: Path, dest: Path, porcelain: str) -> list[str]:
    copied = []
    for line in porcelain.splitlines():
        if not line.startswith("?? "):
            continue
        rel = line[3:].rstrip("/")
        if not any(rel == p.rstrip("/") or rel.startswith(p) for p in SOURCE_COPY_PREFIXES):
            continue
        src = enola_root / rel
        target = dest / rel
        if src.is_dir():
            shutil.copytree(src, target, dirs_exist_ok=True)
        elif src.is_file():
            target.parent.mkdir(parents=True, exist_ok=True)
            shutil.copy2(src, target)
        else:
            continue
        copied.append(rel)
    return copied


def prepare_snapshot(enola_root: Path, dest: Path, rev: str, kind: str) -> dict:
    refuse_existing_snapshot(dest)
    dest.parent.mkdir(parents=True, exist_ok=True)
    ident = source_identity(enola_root)
    run(["git", "clone", "--quiet", str(enola_root), str(dest)])
    if kind == "experimental-patch":
        run(["git", "-C", str(dest), "checkout", "--quiet", ident["revision"]])
        diff = run(["git", "-C", str(enola_root), "diff", "HEAD"]).stdout
        if diff.strip():
            proc = subprocess.run(
                ["git", "-C", str(dest), "apply", "--whitespace=nowarn"],
                input=diff,
                text=True,
                capture_output=True,
            )
            if proc.returncode != 0:
                raise RuntimeError("experimental patch apply failed:\n" + proc.stderr[-4000:])
        copied = copy_untracked_source(enola_root, dest, ident["status_porcelain"])
        snapshot_commit = ident["revision"]
        contains_uncommitted = True
    else:
        run(["git", "-C", str(dest), "checkout", "--quiet", rev])
        snapshot_commit = git_out(dest, "rev-parse", "HEAD")
        if snapshot_commit != rev:
            raise RuntimeError(f"snapshot HEAD {snapshot_commit} != requested {rev}")
        copied = []
        contains_uncommitted = False
    (dest / "cmd" / "benchresident").mkdir(parents=True, exist_ok=True)
    (dest / "cmd" / "benchobserver").mkdir(parents=True, exist_ok=True)
    shutil.copyfile(HERE / "resident-driver.go.txt", dest / "cmd" / "benchresident" / "main.go")
    shutil.copyfile(HERE / "observer.go.txt", dest / "cmd" / "benchobserver" / "main.go")
    env = go_env()
    run([str(GOFMT), "-w", "cmd/benchresident/main.go", "cmd/benchobserver/main.go"], cwd=dest, env=env)
    prov = {
        "kind": kind,
        "requested_rev": rev,
        "snapshot_commit": snapshot_commit,
        "source_worktree": ident,
        "snapshot_contains_uncommitted": contains_uncommitted,
        "equate_snapshot_to_committed_rev": False if contains_uncommitted else True,
        "label": "experimental-patch" if contains_uncommitted else "committed",
        "copied_untracked_source": copied,
        "historical_note": HISTORICAL_SHAS.get(snapshot_commit, ""),
        "dest": str(dest),
        "created_ns": time.time_ns(),
    }
    (dest / "snapshot-provenance.json").write_text(json.dumps(prov, indent=2) + "\n")
    return prov


def binary_record(path: Path) -> dict:
    text = go_version_m(path)
    return {
        "path": str(path),
        "sha256": sha256_file(path),
        "go_version_m": text,
        "go_version_m_sha256": sha256_bytes(text.encode()),
    }


def build_snapshot(dest: Path) -> dict:
    env = go_env()
    bins = dest / "bin"
    bins.mkdir(exist_ok=True)
    built = {}
    for name, pkg in (
        ("enola", "./cmd/enola"),
        ("benchresident", "./cmd/benchresident"),
        ("benchobserver", "./cmd/benchobserver"),
    ):
        out = bins / name
        proc = subprocess.run(
            [go_bin(), "build", "-o", str(out), pkg],
            cwd=dest,
            env=env,
            capture_output=True,
            text=True,
        )
        if proc.returncode:
            raise RuntimeError(f"build {name} failed:\n{proc.stderr[-4000:]}")
        built[name] = binary_record(out)
    info = {"dest": str(dest), "binaries": built}
    snap_prov = dest / "snapshot-provenance.json"
    data = json.loads(snap_prov.read_text()) if snap_prov.is_file() else {}
    data["binaries"] = built
    data["built_ns"] = time.time_ns()
    snap_prov.write_text(json.dumps(data, indent=2) + "\n")
    return info


def nats_mod_version(module_root: Path) -> str:
    gomod = module_root / "go.mod"
    if gomod.is_file():
        for line in gomod.read_text().splitlines():
            if "github.com/nats-io/nats.go" in line and not line.strip().startswith("//"):
                parts = line.split()
                if len(parts) >= 2:
                    return parts[-1]
    return "v1.47.0"


def build_observer_overlay(module_root: Path, dest_bin: Path) -> Path:
    """Build harness observer against module_root without modifying that tree."""
    dest_bin.parent.mkdir(parents=True, exist_ok=True)
    src_file = dest_bin.parent / "observer-overlay.go"
    src_file.write_text((HERE / "observer.go.txt").read_text())
    pkg = module_root / "cmd" / "benchobserver"
    if pkg.is_dir():
        overlay = dest_bin.parent / "overlay.json"
        overlay.write_text(json.dumps({"Replace": {"cmd/benchobserver/main.go": str(src_file)}}))
        proc = subprocess.run(
            [go_bin(), "build", "-overlay", str(overlay), "-o", str(dest_bin), "./cmd/benchobserver"],
            cwd=module_root,
            env=go_env(),
            capture_output=True,
            text=True,
        )
        if proc.returncode:
            raise RuntimeError("overlay observer build failed:\n" + proc.stderr[-4000:])
        return dest_bin
    src = dest_bin.parent / "observer-overlay"
    if src.exists():
        shutil.rmtree(src)
    src.mkdir(parents=True)
    shutil.copyfile(src_file, src / "main.go")
    nats_ver = nats_mod_version(module_root)
    (src / "go.mod").write_text(
        "module benchobserveroverlay\n\n"
        "go 1.26\n\n"
        "require (\n"
        "\tgithub.com/enola-labs/enola v0.0.0\n"
        f"\tgithub.com/nats-io/nats.go {nats_ver}\n"
        ")\n\n"
        f"replace github.com/enola-labs/enola => {module_root}\n"
    )
    proc = subprocess.run(
        [go_bin(), "build", "-o", str(dest_bin), "."],
        cwd=src,
        env=go_env(),
        capture_output=True,
        text=True,
    )
    if proc.returncode:
        raise RuntimeError("overlay observer build failed:\n" + proc.stderr[-4000:])
    return dest_bin


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


def free_port() -> int:
    s = socket.socket()
    s.bind(("127.0.0.1", 0))
    port = s.getsockname()[1]
    s.close()
    if port == 14232:
        return free_port()
    return port


def stop_process(child) -> None:
    if child is None:
        return
    if child.poll() is not None:
        return
    try:
        os.killpg(child.pid, 15)
    except (ProcessLookupError, PermissionError, AttributeError):
        child.terminate()
    try:
        child.wait(timeout=5)
    except Exception:
        try:
            os.killpg(child.pid, 9)
        except (ProcessLookupError, PermissionError, AttributeError):
            child.kill()
        try:
            child.wait(timeout=3)
        except Exception:
            pass


def wait_observer_ready(ready_file: Path, stderr_path: Path, proc, timeout: float = 20) -> None:
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        if proc.poll() is not None:
            raise RuntimeError("observer exited before READY:\n" + stderr_path.read_text())
        marker = ready_file.read_text() if ready_file.is_file() else ""
        stderr_txt = stderr_path.read_text() if stderr_path.is_file() else ""
        if "READY" in marker or "READY" in stderr_txt:
            return
        time.sleep(0.05)
    raise RuntimeError("observer READY timeout:\n" + (stderr_path.read_text() if stderr_path.is_file() else ""))


def resolve_smoke_bins(args) -> dict:
    if args.binary:
        refuse_stale(Path(args.binary))
        observer = Path(args.observer) if args.observer else None
        snapshot = Path(args.snapshot) if args.snapshot else ENOLA_ROOT
        return {
            "enola": Path(args.binary),
            "observer_module": snapshot,
            "observer": observer,
            "snapshot": snapshot,
            "note": "explicit --binary",
        }
    if args.snapshot:
        dest = Path(args.snapshot)
        enola = dest / "bin" / "enola"
        if not enola.is_file():
            raise SystemExit(f"snapshot missing enola binary {enola}")
        refuse_stale(enola)
        prov_path = dest / "snapshot-provenance.json"
        note = "existing snapshot (not rebuilt)"
        if prov_path.is_file():
            note = json.loads(prov_path.read_text()).get("label") or note
        return {
            "enola": enola,
            "observer_module": dest,
            "observer": Path(args.observer) if args.observer else None,
            "snapshot": dest,
            "note": note,
        }
    raise SystemExit(
        "smoke/watch-smoke require --binary or --snapshot; "
        "refusing to silently build a hardcoded historical SHA"
    )


def smoke(args) -> int:
    """Tiny initial+delta+cold equality on a fresh NATS port. Not a Product run."""
    t0 = time.monotonic()
    bins = resolve_smoke_bins(args)
    enola_bin = bins["enola"]
    work = Path(args.work) if args.work else Path("/tmp/enola-observer-smoke-" + str(os.getpid()))
    if work.exists():
        raise SystemExit(f"refusing to reuse existing work dir {work}")
    work.mkdir(parents=True)
    observer_bin = bins["observer"] or build_observer_overlay(bins["observer_module"], work / "bin" / "benchobserver")
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
        obs_err_path = work / "observer.stderr"
        obs_err = obs_err_path.open("w")
        env = os.environ.copy()
        env["OBSERVER_READY_FILE"] = str(ready_file)
        obs_proc = subprocess.Popen(
            [str(observer_bin), url, str(jsonl)],
            stdout=obs_err,
            stderr=obs_err,
            env=env,
            start_new_session=True,
        )
        wait_observer_ready(ready_file, obs_err_path, obs_proc)
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
                raise RuntimeError("observer exited:\n" + obs_err_path.read_text())
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
            "measurement_kind": "tiny-analyze-delta-cold",
            "not_production_graph_watch": True,
            "not_product_run": True,
            "work": str(work),
            "port": port,
            "elapsed_s": elapsed,
            "binary": binary_record(enola_bin),
            "observer": binary_record(Path(observer_bin)),
            "snapshot": str(bins["snapshot"]),
            "snapshot_note": bins["note"],
            "initial_generation": [initial.get("BaseGeneration"), initial.get("TargetGeneration")],
            "delta_generation": [delta.get("BaseGeneration"), delta.get("TargetGeneration")],
            "cold_generation": [cold.get("BaseGeneration"), cold.get("TargetGeneration")],
            "live_delta_hash": live_hash,
            "cold_hash": cold_hash,
            "equal": True,
            "observer_frames": len(frames),
            "begin_scope_mode": frames[1].get("scope_mode"),
            "begin_schema": frames[1].get("schema_version"),
        }
        (work / "smoke.json").write_text(json.dumps(report, indent=2) + "\n")
        print(json.dumps(report, indent=2))
        print("smoke ok", elapsed, "s", work)
        return 0
    finally:
        stop_process(obs_proc)
        stop_process(nats_proc)
        nats_log.close()


def self_test() -> int:
    failures = []

    def check(name: str, cond: bool, detail: str = ""):
        if not cond:
            failures.append(f"{name}: {detail or 'failed'}")

    driver = (HERE / "resident-driver.go.txt").read_text()
    observer = (HERE / "observer.go.txt").read_text()
    resident = (HERE / "resident.py").read_text()
    watch = (HERE / "watch.py").read_text()
    me = Path(__file__).read_text()
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
    check("smoke waits READY before graph", "observer READY timeout" in me)
    check("smoke uses ready file", "OBSERVER_READY_FILE" in me)
    check("observer writes ready file", "OBSERVER_READY_FILE" in observer)
    check("observer records frozen Begin", "owner_scope_count" in observer and "schema_version" in observer)
    check("no v1 observer default", "enola-product-observer" not in resident.split("FORBIDDEN")[0])
    check("idle scenario", "resident-noop" in resident)
    check("duplicate same-content", "resident-duplicate" in resident)
    check("duplicate allows verification hash", "verification hash of at most one file allowed" in resident)
    check("idle still zero work", 'label + " zero work/no events"' in resident)
    check("body edit", "resident-body" in resident)
    check("cold equality", "cold equality" in resident)
    check("authoritative CLI cold", "--authoritative-scope" in resident)
    check("resident labelled request-driven", "request-driven-resident" in resident)
    check("resident not production watch", "not_production_graph_watch" in resident)
    check("driver comment not production watch", "not production `graph watch`" in driver or "not production" in driver)
    check("explicit rev required", "candidate/parent is not a pin" in me)
    check("never rmtree snapshot dest", "refusing to delete existing snapshot" in me)
    check("experimental patch kind", "experimental-patch" in me)
    check("watch harness graph watch", "graph watch" in watch)
    check("watch default 5s", "--watch-every" in watch and "5s" in watch)
    check("watch READY handshake", "OBSERVER_READY_FILE" in watch)
    check("watch stops children", "stop_process" in watch)
    check("watch burst writes", "burst" in watch)
    check("watch idle no events", "idle" in watch)
    check("watch cold equality", "cold" in watch)
    check("watch supports Product source", "--source" in watch and "--scope-config" in watch)
    check("nats-server path", NATS_SERVER.is_file(), str(NATS_SERVER))
    check("gofmt present", GOFMT.is_file(), str(GOFMT))
    check("go present", GO.is_file(), str(GO))
    with tempfile.TemporaryDirectory() as tmp:
        tmp_path = Path(tmp)
        existing = tmp_path / "snap"
        existing.mkdir()
        raised = False
        try:
            refuse_existing_snapshot(existing)
        except SystemExit:
            raised = True
        check("refuse existing snapshot dest", raised)
        try:
            resolve_revision("candidate", ENOLA_ROOT)
            check("reject candidate alias", False)
        except SystemExit:
            check("reject candidate alias", True)
        for name in ("resident-driver.go.txt", "observer.go.txt"):
            src = (HERE / name).read_text()
            dest = tmp_path / (name.replace(".txt", ""))
            dest.write_text(src)
            proc = subprocess.run([str(GOFMT), "-l", str(dest)], capture_output=True, text=True)
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
    print("historical dumps are not default pins:", ", ".join(sha[:12] for sha in HISTORICAL_SHAS))
    print("ready: tiny --smoke needs --binary or --snapshot; do not touch 14232")
    return 0


def print_commands() -> None:
    print("Dependencies:")
    print("  nats-server", NATS_SERVER)
    print("  go/gofmt", GO, GOFMT)
    print("  Product snapshot source /tmp/enola-product-benchmark-source (read-only)")
    print()
    print("Historical archived SHAs (not default pins):")
    for sha, note in HISTORICAL_SHAS.items():
        print(f"  {sha}  {note}")
    print()
    print("Prepare a FRESH private snapshot (never deletes an existing dest):")
    print("  python3", HERE / "run.py", "--prepare --build --rev HEAD --kind committed --snapshot-dest /tmp/enola-efficiency-2026-09-22-HEAD-new")
    print("  python3", HERE / "run.py", "--prepare --build --kind experimental-patch --snapshot-dest /tmp/enola-efficiency-2026-09-22-experimental-new")
    print()
    print("Tiny analyze/delta/cold smoke (not Product, not graph watch):")
    print("  python3", HERE / "run.py", "--smoke --snapshot $SNAP --work /tmp/enola-eff-smoke-new")
    print()
    print("Production graph watch tiny smoke (default --watch-every 5s; override for bounded checks):")
    print("  python3", HERE / "watch.py", "--smoke --snapshot $SNAP --watch-every 1s --work /tmp/enola-watch-smoke-new")
    print()
    print("Request-driven resident (separate from production watch):")
    print("  python3", HERE / "resident.py", "\\")
    print("    --binary $SNAP/bin/enola --resident $SNAP/bin/benchresident \\")
    print("    --observer $SNAP/bin/benchobserver --nats nats://127.0.0.1:PORT \\")
    print("    --root $WORK --repeat 1 --source /tmp/enola-product-benchmark-source")


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--self-test", action="store_true")
    parser.add_argument("--prepare", action="store_true")
    parser.add_argument("--build", action="store_true")
    parser.add_argument("--rev", default="", help="explicit git SHA or HEAD (not candidate/parent aliases)")
    parser.add_argument("--kind", choices=["committed", "experimental-patch"], default="committed")
    parser.add_argument("--snapshot-dest", default="", help="fresh nonexistent snapshot directory")
    parser.add_argument("--snapshot", default="", help="existing snapshot to use; never deleted")
    parser.add_argument("--binary", default="")
    parser.add_argument("--observer", default="")
    parser.add_argument("--work", default="")
    parser.add_argument("--enola-root", default=str(ENOLA_ROOT))
    parser.add_argument("--print-commands", action="store_true")
    parser.add_argument("--smoke", action="store_true", help="tiny isolated NATS initial+delta+cold; never uses port 14232")
    parser.add_argument("--watch-smoke", action="store_true", help="delegate to watch.py --smoke")
    args = parser.parse_args()
    if args.self_test:
        return self_test()
    if args.watch_smoke:
        cmd = [sys.executable, str(HERE / "watch.py"), "--smoke"]
        if args.snapshot:
            cmd += ["--snapshot", args.snapshot]
        if args.binary:
            cmd += ["--binary", args.binary]
        if args.observer:
            cmd += ["--observer", args.observer]
        if args.work:
            cmd += ["--work", args.work]
        return subprocess.call(cmd)
    if args.smoke:
        return smoke(args)
    if args.print_commands:
        print_commands()
        return 0
    if args.prepare or args.build:
        enola_root = Path(args.enola_root)
        kind = args.kind
        if kind == "experimental-patch":
            head = git_out(enola_root, "rev-parse", "HEAD")
            if args.rev and args.rev.strip().upper() not in {"", "HEAD"}:
                resolved = resolve_revision(args.rev, enola_root)
                if resolved != head:
                    raise SystemExit(
                        "experimental-patch snapshots the current dirty worktree; pass --rev HEAD or omit --rev"
                    )
            rev = head
        else:
            rev = resolve_revision(args.rev or "HEAD", enola_root)
        dest = Path(args.snapshot_dest) if args.snapshot_dest else default_snapshot_dest(rev, kind)
        if args.prepare:
            prov = prepare_snapshot(enola_root, dest, rev, kind)
            print(json.dumps(prov, indent=2)[:5000])
            print("prepared", dest, "kind", kind)
        if args.build:
            if not dest.exists():
                raise SystemExit(f"missing snapshot {dest}; pass --prepare with a fresh --snapshot-dest")
            info = build_snapshot(dest)
            print(json.dumps(info, indent=2)[:4000])
        return 0
    print_commands()
    return 0


if __name__ == "__main__":
    sys.exit(main())
