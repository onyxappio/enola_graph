#!/usr/bin/env python3
"""Production `enola graph watch` harness.

Distinct from request-driven resident.py. Uses CLI default --watch-every 5s
(overridable). Tiny-fixture smoke is the default cheap path; Product source
and scope pins are accepted without running a heavyweight Product suite here.
"""
from __future__ import annotations

import argparse
import importlib.util
import json
import os
import shutil
import subprocess
import sys
import tempfile
import time
from pathlib import Path


HERE = Path(__file__).resolve().parent
spec = importlib.util.spec_from_file_location("efficiency_run", HERE / "run.py")
eff = importlib.util.module_from_spec(spec)
assert spec.loader is not None
spec.loader.exec_module(eff)

PASSWORD = Path("packages/crypto/src/password.ts")
DEFAULT_WATCH_EVERY = "5s"


def parse_every(text: str) -> float:
    text = text.strip()
    if text.endswith("ms"):
        return float(text[:-2]) / 1000.0
    if text.endswith("s"):
        return float(text[:-1])
    if text.endswith("m"):
        return float(text[:-1]) * 60.0
    raise SystemExit(f"unsupported --watch-every {text!r}; use a Go duration such as 5s")


def write_fsync(path: Path, text: str) -> int:
    ns = time.time_ns()
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(text)
    fd = os.open(path, os.O_RDONLY)
    try:
        os.fsync(fd)
    finally:
        os.close(fd)
    return ns


def read_frames(path: Path) -> list:
    if not path.is_file() or path.stat().st_size == 0:
        return []
    return [json.loads(ln) for ln in path.read_text().splitlines() if ln.strip()]


def wait_frames(path: Path, n: int, timeout: float, obs_proc, watch_proc, stderr_paths) -> list:
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        if obs_proc.poll() is not None:
            raise RuntimeError("observer exited:\n" + stderr_paths["observer"].read_text())
        if watch_proc is not None and watch_proc.poll() is not None:
            raise RuntimeError("graph watch exited:\n" + stderr_paths["watch"].read_text()[-4000:])
        frames = read_frames(path)
        if len(frames) >= n:
            return frames
        time.sleep(0.05)
    raise RuntimeError(
        f"timeout waiting for {n} observer frames, have {len(read_frames(path))}\n"
        + (path.read_text() if path.is_file() else "")
    )


def last_seq(observer: Path, url: str) -> int:
    raw = subprocess.check_output([str(observer), url, "--info"], text=True)
    return int(json.loads(raw).get("last_seq") or 0)


def assert_frozen_begin(frame: dict, label: str) -> None:
    if frame.get("schema_version") not in {None, "enola.graph.v2"}:
        # Overlay observer records schema; older frames without the field still ran v2 CLI.
        if frame.get("schema_version") and frame.get("schema_version") != "enola.graph.v2":
            raise RuntimeError(f"{label}: schema_version {frame.get('schema_version')!r}")
    if frame.get("schema_version") == "enola.graph.v2":
        if frame.get("scope_mode") != "complete":
            raise RuntimeError(f"{label}: scope_mode {frame.get('scope_mode')!r} want complete")
        if (frame.get("owner_scope_count") or 0) < 1:
            raise RuntimeError(f"{label}: empty frozen Begin")
        base = frame.get("base_generation")
        target = frame.get("target_generation")
        if type(base) is not int or type(target) is not int or target != base + 1:
            raise RuntimeError(f"{label}: generation {base}->{target}")


def overlay_scope(repo: Path, source: Path, scope_config: Path) -> None:
    src_cfg = source / "mcp-arch.yaml"
    if src_cfg.is_file():
        (repo / "mcp-arch.yaml").write_bytes(src_cfg.read_bytes())
    scope_text = scope_config.read_text()
    if "repo:" in scope_text.split("\n")[0] or scope_text.startswith("repo:"):
        raise RuntimeError("scope-config must not set repository")
    cfg = repo / "mcp-arch.yaml"
    if cfg.is_file():
        text = cfg.read_text()
        if "graph_inputs:" not in text:
            cfg.write_text(text.rstrip() + "\n" + scope_text)
    else:
        cfg.write_text("repo: .\nextractors: [typescript]\nexplainers: []\nrenderers: []\n" + scope_text)


def make_tiny_repo(root: Path) -> Path:
    repo = root / "repo"
    repo.mkdir()
    (repo / "package.json").write_text('{"name":"watch-smoke","type":"module"}\n')
    (repo / "tsconfig.json").write_text('{"compilerOptions":{"strict":true}}\n')
    (repo / "mcp-arch.yaml").write_text("repo: .\nextractors: [typescript]\nexplainers: []\nrenderers: []\n")
    (repo / "a.ts").write_text("export const a = 1;\n")
    return repo


def isolate_product(source: Path, dest: Path, scope_config: Path) -> Path:
    if dest.exists():
        shutil.rmtree(dest)
    dest.parent.mkdir(parents=True, exist_ok=True)
    try:
        subprocess.run(["cp", "-cR", str(source), str(dest)], check=True)
    except subprocess.CalledProcessError:
        subprocess.run(["cp", "-a", str(source), str(dest)], check=True)
    overlay_scope(dest, source, scope_config)
    return dest


def graph_analyze(binary: Path, repo: Path, cfg: Path, state: Path, url: str, ctx: str, repo_id: str) -> dict:
    cmd = [
        str(binary),
        "graph",
        "analyze",
        "--authoritative-scope",
        "--max-begin-bytes",
        "1048576",
        "--summary-json",
        "--nats",
        url,
        "--state-dir",
        str(state),
        "--context",
        ctx,
        "--repo-id",
        repo_id,
        str(cfg),
    ]
    proc = subprocess.run(cmd, capture_output=True, text=True)
    if proc.returncode:
        raise RuntimeError("cold analyze failed:\n" + proc.stderr[-2000:] + proc.stdout[-1000:])
    rows = json.loads(proc.stdout)
    return rows[0]


def run_watch(args) -> int:
    t0 = time.monotonic()
    bins = eff.resolve_smoke_bins(args)
    enola_bin = bins["enola"]
    work = Path(args.work) if args.work else Path("/tmp/enola-watch-smoke-" + str(os.getpid()))
    if work.exists():
        raise SystemExit(f"refusing to reuse existing work dir {work}")
    work.mkdir(parents=True)
    observer_bin = bins["observer"] or eff.build_observer_overlay(
        bins["observer_module"], work / "bin" / "benchobserver"
    )
    every = args.watch_every
    every_s = parse_every(every)
    port = eff.free_port()
    conf = eff.write_nats_conf(work, port)
    nats_log = (work / "nats.log").open("w")
    nats_proc = subprocess.Popen(
        [str(eff.NATS_SERVER), "-c", str(conf)],
        stdout=nats_log,
        stderr=subprocess.STDOUT,
        start_new_session=True,
    )
    obs_proc = None
    watch_proc = None
    edits = []
    try:
        eff.wait_port("127.0.0.1", port)
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
        eff.wait_observer_ready(ready_file, obs_err_path, obs_proc)

        use_product = bool(args.source) and Path(args.source).is_dir() and not args.force_tiny
        if use_product:
            repo = isolate_product(Path(args.source), work / "product-live", Path(args.scope_config))
            edit_path = repo / PASSWORD
            original = edit_path.read_text()
            mutated = original.replace(
                "return email.trim().toLowerCase();",
                "return email.normalize('NFKC').trim().toLowerCase();",
            )
            if mutated == original:
                raise RuntimeError("Product body edit snippet missing")
            burst_texts = [
                mutated,
                mutated.replace("NFKC", "NFKD"),
                mutated.replace("NFKC", "NFC"),
            ]
            fixture = "product"
        else:
            repo = make_tiny_repo(work)
            edit_path = repo / "a.ts"
            original = "export const a = 1;\n"
            burst_texts = ["export const a = 2;\n", "export const a = 3;\n", "export const a = 4;\n"]
            fixture = "tiny"

        cfg = work / "config.yaml"
        cfg_text = "repo: " + str(repo) + "\nextractors: [typescript]\nexplainers: []\nrenderers: []\n"
        if fixture == "product":
            cfg_text += "\n" + Path(args.scope_config).read_text()
        cfg.write_text(cfg_text)

        watch_err_path = work / "watch.stderr"
        watch_err = watch_err_path.open("w")
        watch_cmd = [
            str(enola_bin),
            "graph",
            "watch",
            "--authoritative-scope",
            "--max-begin-bytes",
            "1048576",
            "--watch-every",
            every,
            "--nats",
            url,
            "--state-dir",
            str(work / "state-watch"),
            "--context",
            "watch-bench",
            "--repo-id",
            "watch-bench",
            "--config",
            str(cfg),
            str(repo),
        ]
        watch_proc = subprocess.Popen(
            watch_cmd,
            stdout=watch_err,
            stderr=watch_err,
            start_new_session=True,
        )
        stderr_paths = {"observer": obs_err_path, "watch": watch_err_path}
        frames = wait_frames(jsonl, 1, 120 if fixture == "product" else 40, obs_proc, watch_proc, stderr_paths)
        initial = frames[0]
        assert_frozen_begin(initial, "watch-initial")
        seq_after_initial = last_seq(observer_bin, url)

        time.sleep(max(every_s * 2, 0.5))
        if watch_proc.poll() is not None:
            raise RuntimeError("graph watch exited during idle:\n" + watch_err_path.read_text()[-4000:])
        idle_seq = last_seq(observer_bin, url)
        idle_frames = read_frames(jsonl)
        if idle_seq != seq_after_initial or len(idle_frames) != 1:
            raise RuntimeError(f"idle published events: seq {seq_after_initial}->{idle_seq} frames {len(idle_frames)}")

        dup_ns = write_fsync(edit_path, original)
        edits.append({"kind": "duplicate", "ns": dup_ns, "path": str(edit_path)})
        time.sleep(max(every_s * 2, 0.5))
        dup_seq = last_seq(observer_bin, url)
        dup_frames = read_frames(jsonl)
        if dup_seq != seq_after_initial or len(dup_frames) != 1:
            raise RuntimeError(f"duplicate published events: seq {seq_after_initial}->{dup_seq} frames {len(dup_frames)}")

        burst_started = time.time_ns()
        for text in burst_texts:
            edits.append({"kind": "burst", "ns": write_fsync(edit_path, text), "path": str(edit_path)})
            time.sleep(0.05)
        burst_timeout = max(every_s + 15, 20)
        frames = wait_frames(jsonl, 2, burst_timeout, obs_proc, watch_proc, stderr_paths)
        burst_frames = frames[1:]
        for i, fr in enumerate(burst_frames):
            assert_frozen_begin(fr, f"watch-burst-{i}")
        completed = [fr.get("target_generation") for fr in frames]
        if any(g in (None, 0) for g in completed):
            raise RuntimeError(f"missing completed generations {completed}")
        final = frames[-1]
        final_hash = final["normalized_hash"]

        cold_ctx = "watch-cold"
        cold = graph_analyze(
            enola_bin, repo, cfg, work / "state-cold", url, cold_ctx, "watch-bench-cold"
        )
        frames = wait_frames(jsonl, len(burst_frames) + 2, 40 if fixture != "product" else 120, obs_proc, watch_proc, stderr_paths)
        cold_frame = frames[-1]
        if cold_frame.get("context") != cold_ctx:
            # last frame should be cold; if watch emitted extra, pick matching context
            matches = [fr for fr in frames if fr.get("context") == cold_ctx]
            if not matches:
                raise RuntimeError("missing cold observer frame")
            cold_frame = matches[-1]
        if cold_frame["normalized_hash"] != final_hash:
            raise RuntimeError(
                f"watch hash {final_hash} != cold {cold_frame['normalized_hash']}"
            )
        elapsed = round(time.monotonic() - t0, 3)
        report = {
            "measurement_kind": "production-graph-watch",
            "not_request_driven_resident": True,
            "not_product_acceptance": True,
            "fixture": fixture,
            "watch_every": every,
            "watch_every_s": every_s,
            "work": str(work),
            "port": port,
            "elapsed_s": elapsed,
            "binary": eff.binary_record(enola_bin),
            "observer": eff.binary_record(Path(observer_bin)),
            "snapshot": str(bins["snapshot"]),
            "snapshot_note": bins["note"],
            "edits": edits,
            "burst_started_ns": burst_started,
            "initial_generation": [initial.get("base_generation"), initial.get("target_generation")],
            "completed_generations": completed,
            "burst_frame_count": len(burst_frames),
            "idle_events": 0,
            "duplicate_events": 0,
            "watch_hash": final_hash,
            "cold_hash": cold_frame["normalized_hash"],
            "equal": True,
            "initial_owner_scope_count": initial.get("owner_scope_count"),
            "burst_owner_scope_counts": [fr.get("owner_scope_count") for fr in burst_frames],
            "broker_first_ns": final.get("first_ns"),
            "broker_first_batch_ns": final.get("first_batch_ns"),
            "broker_end_ns": final.get("broker_end_ns"),
            "consumer_end_ns": final.get("consumer_end_ns"),
            "cold_summary": {
                "BaseGeneration": cold.get("BaseGeneration"),
                "TargetGeneration": cold.get("TargetGeneration"),
                "ParsedFiles": cold.get("ParsedFiles"),
                "OwnersPublished": cold.get("OwnersPublished"),
            },
        }
        (work / "watch.json").write_text(json.dumps(report, indent=2) + "\n")
        print(json.dumps(report, indent=2))
        print("watch smoke ok", elapsed, "s", work)
        return 0
    finally:
        eff.stop_process(watch_proc)
        eff.stop_process(obs_proc)
        eff.stop_process(nats_proc)
        nats_log.close()


def self_test() -> int:
    failures = []

    def check(name: str, cond: bool, detail: str = ""):
        if not cond:
            failures.append(f"{name}: {detail or 'failed'}")

    text = Path(__file__).read_text()
    check("uses graph watch", '"watch"' in text and "--watch-every" in text)
    check("default 5s constant", DEFAULT_WATCH_EVERY == "5s")
    check("READY handshake", "wait_observer_ready" in text)
    check("stop children", "stop_process" in text)
    check("burst", "burst" in text)
    check("idle", "idle" in text)
    check("duplicate", "duplicate" in text)
    check("cold equality", "cold" in text)
    check("Product source flag", "--source" in text)
    check("scope-config", "--scope-config" in text)
    check("distinct from resident", "not_request_driven_resident" in text)
    check("parse 5s", parse_every("5s") == 5.0)
    check("parse 1s", parse_every("1s") == 1.0)
    with tempfile.TemporaryDirectory() as tmp:
        p = Path(tmp) / "a.ts"
        ns = write_fsync(p, "x\n")
        check("write_fsync", p.read_text() == "x\n" and ns > 0)
    if failures:
        print("self-test FAIL", file=sys.stderr)
        for item in failures:
            print(" ", item, file=sys.stderr)
        return 1
    print("watch self-test ok")
    return 0


def main() -> int:
    p = argparse.ArgumentParser()
    p.add_argument("--self-test", action="store_true")
    p.add_argument("--smoke", action="store_true", help="tiny fixture; not a Product run")
    p.add_argument("--force-tiny", action="store_true")
    p.add_argument("--watch-every", default=DEFAULT_WATCH_EVERY, help="CLI window; default 5s")
    p.add_argument("--binary", default="")
    p.add_argument("--observer", default="")
    p.add_argument("--snapshot", default="")
    p.add_argument("--work", default="")
    p.add_argument("--source", default="", help="optional Product checkout; isolated clone only")
    p.add_argument("--scope-config", default=str(HERE / "product-graph-scope.yaml"))
    args = p.parse_args()
    if args.self_test:
        return self_test()
    if args.smoke:
        args.force_tiny = True
        args.smoke_fixture_only = True
        if args.watch_every == DEFAULT_WATCH_EVERY:
            args.watch_every = "1s"
        return run_watch(args)
    if not args.binary and not args.snapshot:
        raise SystemExit("pass --binary or --snapshot")
    return run_watch(args)


if __name__ == "__main__":
    sys.exit(main())
