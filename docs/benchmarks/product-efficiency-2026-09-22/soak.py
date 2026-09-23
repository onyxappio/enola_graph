#!/usr/bin/env python3
"""Long-running production `enola graph watch` soak harness.

Runs a real `graph watch` session at the CLI default 5s collection window while
an EXTERNAL editor process mutates an isolated checkout, then compares the final
completed generation against a cold analysis of the same tree.

Completion is selected by HEURISTIC quiescence, never by an internal drain
proof: no watcher watermark exists, so the harness cannot read the watcher's
own queue. See wt.QUIESCENCE_LIMITS -- in particular, an analysis whose Begin
has not been published yet is unobservable.

Recorded per generation: last save before the window, broker Begin, first batch,
broker End, consumer completion, owner scope count, and the derived latencies.
Also sampled: watch RSS, broker/watch restarts and their downtime.

Documented `graph watch` semantics this harness depends on (see README):
  * The collection window is FIXED, not a debounce. It opens on the first event
    and closes --watch-every later.
  * Edits landing during analysis are queued; the next window opens only after
    the in-flight analysis finishes.
  * Broker errors terminate the watch process. This is the DOCUMENTED policy;
    this harness records what it actually observed and never reports recovery
    acceptance from a restart that the watcher happened to survive.

Not request-driven resident (resident.py). Not performance acceptance.
"""
from __future__ import annotations

import argparse
import hashlib
import importlib.util
import json
import os
import random
import shutil
import subprocess
import sys
import tempfile
import time
from pathlib import Path


HERE = Path(__file__).resolve().parent


def _load(name: str, path: Path):
    spec = importlib.util.spec_from_file_location(name, path)
    mod = importlib.util.module_from_spec(spec)
    assert spec.loader is not None
    spec.loader.exec_module(mod)
    return mod


eff = _load("efficiency_run", HERE / "run.py")
wt = _load("efficiency_watch", HERE / "watch.py")

REPO_ID = wt.REPO_ID

# How the save timestamps in THIS run's series were obtained. A scripted write
# is stamped after fsync(2) returns; an external editor is stamped from the
# filesystem mtime the poller found. Those are different physical events, so the
# report carries the label of the basis actually used, never a fixed string.
SAVE_NS_BASIS = {
    "fsync-completion": "durable write+fsync completion",
    "filesystem-mtime":
        "filesystem mtime observed by polling the checkout (external editor; "
        "no durability guarantee, no editor cooperation assumed)",
}
WATCH_CTX = "soak-watch"
COLD_CTX = "soak-cold"


def parse_duration(text: str) -> float:
    text = str(text).strip()
    if text.endswith("ms"):
        return float(text[:-2]) / 1000.0
    for suffix, mult in (("h", 3600.0), ("m", 60.0), ("s", 1.0)):
        if text.endswith(suffix):
            return float(text[: -len(suffix)]) * mult
    raise SystemExit(f"unsupported duration {text!r}; use 30m, 90s, 1h")


# --------------------------------------------------------------------------
# fixture
# --------------------------------------------------------------------------

def make_soak_repo(root: Path) -> Path:
    """Multi-file TypeScript fixture so rename/delete/import edits are real."""
    repo = root / "repo"
    (repo / "src").mkdir(parents=True)
    (repo / "package.json").write_text('{"name":"soak","type":"module"}\n')
    (repo / "tsconfig.json").write_text('{"compilerOptions":{"strict":true}}\n')
    (repo / "mcp-arch.yaml").write_text(
        "repo: .\nextractors: [typescript]\nexplainers: []\nrenderers: []\n"
    )
    (repo / "src" / "core.ts").write_text(
        "export const seed = 1;\nexport function core() { return seed; }\n"
    )
    (repo / "src" / "util.ts").write_text(
        "export function util(n: number) { return n + 1; }\n"
    )
    (repo / "src" / "index.ts").write_text(
        "import { core } from './core';\n"
        "import { util } from './util';\n"
        "export function main() { return util(core()); }\n"
    )
    return repo


# --------------------------------------------------------------------------
# external editor process
# --------------------------------------------------------------------------

EDIT_KINDS = (
    "add-export",
    "remove-export",
    "edit-body",
    "add-import",
    "remove-import",
    "create-file",
    "rename-file",
    "delete-file",
)


def _write(path: Path, text: str) -> dict:
    """Write + fsync, returning BOTH the start and the durable completion.

    The durable completion is the REFERENCE this harness reports, because
    timing the instant before the write overstates every save-relative interval
    by the write+fsync cost. It is not a visibility barrier: a reader or
    filesystem watcher can see the bytes before fsync returns (wt.
    VISIBILITY_NOTE), so save-relative offsets may legitimately come out
    negative and are reported as such rather than clamped.
    """
    path.parent.mkdir(parents=True, exist_ok=True)
    start_ns = time.time_ns()
    path.write_text(text)
    fd = os.open(path, os.O_RDONLY)
    try:
        os.fsync(fd)
    finally:
        os.close(fd)
    return {"start_ns": start_ns, "ns": time.time_ns()}



def prepare_product_editor(repo: Path) -> Path:
    """Seed scripted edits in a new module inside the isolated Product copy.

    Product need not have the tiny fixture's src/core.ts, util.ts or index.ts.
    Refuse collisions rather than modifying an existing application module.
    """
    root = repo / "enola-watch-fixture"
    root.mkdir(exist_ok=False)
    src = root / "src"
    src.mkdir()
    (src / "core.ts").write_text("export const seed = 1;\nexport function core() { return seed; }\n")
    (src / "util.ts").write_text("export function util(n: number) { return n + 1; }\n")
    (src / "index.ts").write_text("import { core } from './core';\nimport { util } from './util';\nexport function main() { return util(core()); }\n")
    return root


def editor_worker(args) -> int:
    """Separate process: mixed graph-changing edits on an isolated checkout.

    Stands in for "another agent edits the checkout". Every operation is logged
    with a timestamp so generations can be joined back to their last save.
    """
    repo = Path(args.repo)
    log = Path(args.edit_log)
    rng = random.Random(args.seed)
    deadline = time.monotonic() + parse_duration(args.duration)
    interval = parse_duration(args.edit_interval)
    src = repo / "src"
    extra: list[str] = []
    counter = 0
    with log.open("a", buffering=1) as fh:
        while time.monotonic() < deadline:
            counter += 1
            live = sorted(p.name for p in src.glob("*.ts"))
            kind = rng.choice([k for k in EDIT_KINDS if _applicable(k, live, extra)])
            try:
                record = _apply_edit(kind, src, rng, counter, extra)
            except Exception as exc:  # keep the soak alive; record the failure
                now = time.time_ns()
                record = {"kind": kind, "error": repr(exc), "start_ns": now, "ns": now}
            record["seq"] = counter
            fh.write(json.dumps(record) + "\n")
            time.sleep(interval)
    return 0


def _applicable(kind: str, live: list[str], extra: list[str]) -> bool:
    if kind in {"rename-file", "delete-file"}:
        return bool(extra)
    if kind == "remove-import":
        return "index.ts" in live
    return True


def _apply_edit(kind: str, src: Path, rng: random.Random, counter: int, extra: list[str]) -> dict:
    core = src / "core.ts"
    index = src / "index.ts"
    if kind == "add-export":
        text = core.read_text() + f"export const gen{counter} = {counter};\n"
        return {"kind": kind, "path": str(core), **_write(core, text)}
    if kind == "remove-export":
        lines = [l for l in core.read_text().splitlines(True) if l.startswith("export const gen")]
        text = core.read_text()
        if lines:
            text = text.replace(lines[-1], "")
        else:
            text += f"export const gen{counter} = {counter};\n"
        return {"kind": kind, "path": str(core), **_write(core, text)}
    if kind == "edit-body":
        text = (src / "util.ts").read_text()
        text = f"export function util(n: number) {{ return n + {counter}; }}\n"
        return {"kind": kind, "path": str(src / "util.ts"), **_write(src / "util.ts", text)}
    if kind == "create-file":
        name = f"mod{counter}.ts"
        extra.append(name)
        text = f"export function mod{counter}() {{ return {counter}; }}\n"
        return {"kind": kind, "path": str(src / name), **_write(src / name, text)}
    if kind == "add-import":
        name = extra[-1] if extra else "util.ts"
        stem = name[:-3]
        text = index.read_text()
        line = f"import {{ {stem if stem.startswith('mod') else 'util'} }} from './{stem}';\n"
        if line not in text:
            text = line + text
        return {"kind": kind, "path": str(index), **_write(index, text)}
    if kind == "remove-import":
        text = index.read_text()
        imports = [l for l in text.splitlines(True) if l.startswith("import ")]
        if len(imports) > 2:
            text = text.replace(imports[0], "")
        return {"kind": kind, "path": str(index), **_write(index, text)}
    if kind == "rename-file":
        old = src / extra[rng.randrange(len(extra))]
        new_name = old.stem + "r.ts"
        start_ns = time.time_ns()
        if old.exists():
            os.rename(old, src / new_name)
            extra.remove(old.name)
            extra.append(new_name)
        return {"kind": kind, "path": str(old), "to": str(src / new_name),
                "start_ns": start_ns, "ns": time.time_ns()}
    if kind == "delete-file":
        victim = src / extra.pop(rng.randrange(len(extra)))
        start_ns = time.time_ns()
        if victim.exists():
            victim.unlink()
        return {"kind": kind, "path": str(victim),
                "start_ns": start_ns, "ns": time.time_ns()}
    raise RuntimeError(f"unknown edit kind {kind}")


# --------------------------------------------------------------------------
# metrics
# --------------------------------------------------------------------------

def resolve_profile(profile: str, fixture: str) -> str:
    """'auto' means: narrow for the tiny fixture, production default for Product."""
    if profile != "auto":
        return profile
    return "typescript" if fixture == "tiny" else "full"


def build_config_text(repo: Path, profile: str, scope_text: str = "") -> str:
    """Graph config for a run. Pure, so the profile rule is testable.

    profile=full OMITS the extractors key so the binary applies its production
    default set (docs/benchmarks/PRODUCT.md:214). Pinning [typescript] for a
    Product run would silently narrow the graph to less than production builds
    and overstate what the run accepted. Explainers and renderers stay empty
    either way -- that is a CLI requirement of this harness, not a narrowing of
    the graph itself.
    """
    text = f"repo: {repo}\n"
    if profile == "typescript":
        text += "extractors: [typescript]\n"
    elif profile != "full":
        raise ValueError(f"unknown extractor profile {profile!r}")
    text += "explainers: []\nrenderers: []\n"
    if scope_text:
        text += "\n" + scope_text
    return text


def poll_observed_changes(repo: Path, previous: dict, poll_roots, out_path: Path | None):
    """One CHEAP sampling pass over the declared editing subtree.

    In external-editor mode the harness never writes this tree, so there is no
    edit log to read and no fsync of ours to time. Change timestamps come from
    the filesystem (st_mtime_ns): when the editor's write landed, NOT a durable
    fsync completion and nothing the editor told us. No durable-save timestamp
    is invented from a poll; the basis is recorded on every record and never
    mixed with the scripted fsync-completion series (wt.SAVE_OBSERVATION_BASIS).

    Cost matters here. Hashing a whole Product checkout every tick would
    compete with the watcher for I/O and distort the latency the run exists to
    measure, so the live poll walks only `poll_roots` (the declared allowlist)
    and reads metadata, hashing a file only once its mtime or size moved.
    Integrity of the wider tree is covered by the full baseline and final
    inventories, not by this loop -- so this is complete change detection for
    the DECLARED experiment, not generic whole-watcher coverage.

    Polling also means a change is seen no earlier than the next tick:
    `observed_ns - ns` is the harness's detection lag, not watcher latency.
    """
    meta = wt.subtree_state(repo, poll_roots)
    now = wt.hash_changed(previous, meta)
    diff = wt.classify_input_changes(previous, now, repo, None)
    observed_ns = time.time_ns()
    records = []
    for change in ("added", "modified", "deleted"):
        for path in diff[change]:
            state = now.get(path, {})
            records.append({
                "kind": "observed-" + change,
                "change": change,
                "path": path,
                # A deleted file has no mtime left; fall back to detection time
                # and say so rather than inventing one.
                "ns": int(state.get("mtime_ns") or observed_ns),
                "ns_is_detection_fallback": not state.get("mtime_ns"),
                "observed_ns": observed_ns,
                "sha256": state.get("sha256"),
                "observation_basis": "filesystem-mtime",
                "poll_scope": "declared-allowlist-subtree",
            })
    if records and out_path is not None:
        with out_path.open("a") as fh:
            for rec in records:
                fh.write(json.dumps(rec) + "\n")
    return now, records, diff


def write_ready(path: Path, payload: dict) -> None:
    """Publish the external-editor handshake, durably.

    The editor is a separate process launched by someone else, so this file is
    the entire contract: where to edit, what to touch, how to say it finished.
    """
    path.write_text(json.dumps(payload, indent=2) + "\n")
    fd = os.open(path, os.O_RDONLY)
    try:
        os.fsync(fd)
    finally:
        os.close(fd)


def editing_span(records: list) -> dict:
    """Active-editing vs idle wall time, from observed change timestamps."""
    stamps = sorted(int(r["ns"]) for r in records if isinstance(r.get("ns"), int))
    if not stamps:
        return {"observed_changes": 0, "active_editing_s": 0.0}
    return {
        "observed_changes": len(stamps),
        "first_change_ns": stamps[0],
        "last_change_ns": stamps[-1],
        "active_editing_s": round((stamps[-1] - stamps[0]) / 1e9, 3),
    }


def rss_kb(pid: int) -> int | None:
    try:
        out = subprocess.run(["ps", "-o", "rss=", "-p", str(pid)], capture_output=True, text=True)
        val = out.stdout.strip()
        return int(val) if val else None
    except Exception:
        return None


def join_generations(frames: list, edits: list) -> list:
    """Attach the latest OBSERVED save at/before each generation's Begin.

    This pairing is bookkeeping, NOT causation. The collection window is fixed,
    so a generation can have been triggered by an earlier save and merely
    happen to follow a later one, and a save landing inside an open window is
    folded into a run that began before it. The columns are therefore named
    time_since_latest_observed_save_at_*, generation and run ids are carried so
    a reader can re-pair them, and the only causal end-to-end number is the
    convergence measure (wt.first_cold_equal). See wt.CAUSALITY_NOTE.

    `observation_basis` records how the save timestamp was obtained -- fsync
    completion for scripted writes, filesystem mtime for an external editor --
    because those are different physical events (wt.SAVE_OBSERVATION_BASIS).
    Intervals mixing a local wall clock with a broker timestamp are flagged
    cross-clock in wt.CLOCK_BASIS and are indicative, not precise.
    """
    rows = []
    saves = sorted(
        (e["ns"], e.get("start_ns"), e.get("observation_basis", "fsync-completion"),
         e.get("path"))
        for e in edits if isinstance(e.get("ns"), int)
    )
    for fr in frames:
        begin = int(fr.get("first_ns") or 0)
        prior = [s for s in saves if s[0] <= begin]
        save_ns = prior[-1][0] if prior else None
        save_start_ns = prior[-1][1] if prior else None
        save_basis = prior[-1][2] if prior else None
        save_path = prior[-1][3] if prior else None
        first_batch = fr.get("first_batch_ns")
        end = fr.get("broker_end_ns")
        cons = fr.get("consumer_end_ns")
        row = {
            "generation": fr.get("target_generation"),
            "base_generation": fr.get("base_generation"),
            "run_id": fr.get("run_id"),
            "context": fr.get("context"),
            "owner_scope_count": fr.get("owner_scope_count"),
            "scope_mode": fr.get("scope_mode"),
            "latest_observed_save_ns": save_ns,
            "latest_observed_save_start_ns": save_start_ns,
            "latest_observed_save_path": save_path,
            "observation_basis": save_basis,
            "pairing_is_causal": False,
            "begin_ns": begin or None,
            "first_batch_ns": first_batch,
            "broker_end_ns": end,
            "consumer_end_ns": cons,
        }
        row["write_to_durable_ms"] = _ms(save_start_ns, save_ns)
        row["time_since_latest_observed_save_at_begin_ms"] = _ms(save_ns, begin)
        row["begin_to_first_batch_ms"] = _ms(begin, first_batch)
        row["begin_to_end_ms"] = _ms(begin, end)
        row["end_to_consumer_ms"] = _ms(end, cons)
        row["time_since_latest_observed_save_at_consumer_ms"] = _ms(save_ns, cons)
        # Volume metadata for the same generation, projected straight off the
        # frame. Keys the row already carries are not re-derived here, and a
        # frame from an older observer contributes nulls, not zeros.
        telemetry = wt.generation_telemetry(fr)
        row["telemetry_available"] = telemetry["telemetry_available"]
        for key in ("generation_kind",) + wt.TELEMETRY_KEYS:
            row.setdefault(key, telemetry[key])
        rows.append(row)
    return rows


def _ms(a, b):
    if not isinstance(a, int) or not isinstance(b, int) or a <= 0 or b <= 0:
        return None
    return round((b - a) / 1e6, 3)


def summarize(values: list) -> dict:
    vals = sorted(v for v in values if isinstance(v, (int, float)))
    if not vals:
        return {"count": 0}
    mid = len(vals) // 2
    median = vals[mid] if len(vals) % 2 else (vals[mid - 1] + vals[mid]) / 2
    return {
        "count": len(vals),
        "min": vals[0],
        "median": round(median, 3),
        "max": vals[-1],
    }


def outage_verdict(obs: dict, generations_after: int) -> str:
    """State what the outage actually showed. Never upgrade it to acceptance.

    A short bounce landing between publishes exercises nothing: the watcher
    never had to publish into a dead broker. Only an outage during publication
    can show the documented terminate-on-broker-error behaviour.
    """
    if not obs.get("attempted"):
        return "untested: no deliberate outage was requested"
    if not obs.get("during_publish"):
        return ("untested: the outage did not land during publication; a bounce "
                "between publishes is not recovery acceptance")
    if not obs.get("watch_exited"):
        return ("observed: watcher SURVIVED an outage during publication; the "
                "documented terminate-on-broker-error path was not reproduced")
    if generations_after > 0:
        return ("observed: watch exited on broker error, was restarted, and "
                "later generations completed")
    return ("observed: watch exited on broker error and was restarted, but no "
            "later generation completed, so replay was NOT observed")


# --------------------------------------------------------------------------
# soak
# --------------------------------------------------------------------------

def start_watch(enola_bin: Path, repo: Path, cfg: Path, work: Path, url: str, every: str, log):
    cmd = [
        str(enola_bin), "graph", "watch",
        "--authoritative-scope",
        "--max-begin-bytes", "1048576",
        "--watch-every", every,
        "--nats", url,
        "--state-dir", str(work / "state-watch"),
        "--context", WATCH_CTX,
        "--repo-id", REPO_ID,
        "--config", str(cfg),
        str(repo),
    ]
    return subprocess.Popen(cmd, stdout=log, stderr=log, start_new_session=True)


def run_soak(args) -> int:
    t0 = time.monotonic()
    duration_s = parse_duration(args.duration)
    every_s = wt.parse_every(args.watch_every)
    bins = eff.resolve_smoke_bins(args)
    enola_bin = bins["enola"]
    work = Path(args.work) if args.work else Path("/tmp/enola-soak-" + str(os.getpid()))
    if work.exists():
        raise SystemExit(f"refusing to reuse existing work dir {work}")
    work.mkdir(parents=True)
    observer_bin = bins["observer"] or eff.build_observer_overlay(
        bins["observer_module"], work / "bin" / "benchobserver"
    )

    port = eff.free_port()
    conf = eff.write_nats_conf(work, port)
    url = f"nats://127.0.0.1:{port}"
    nats_log = (work / "nats.log").open("w")
    nats_proc = subprocess.Popen(
        [str(eff.NATS_SERVER), "-c", str(conf)],
        stdout=nats_log, stderr=subprocess.STDOUT, start_new_session=True,
    )
    obs_proc = watch_proc = editor_proc = None
    watch_log = None
    events: list = []
    rss_samples: list = []
    try:
        eff.wait_port("127.0.0.1", port)
        jsonl = work / "consumer.jsonl"
        lifecycle_path = work / "lifecycle.jsonl"
        ready_file = work / "observer.ready"
        obs_err_path = work / "observer.stderr"
        obs_err = obs_err_path.open("w")
        env = os.environ.copy()
        env["OBSERVER_READY_FILE"] = str(ready_file)
        # Begin/End records: without them an in-flight analysis is invisible.
        env["OBSERVER_LIFECYCLE_FILE"] = str(lifecycle_path)
        obs_proc = subprocess.Popen(
            [str(observer_bin), url, str(jsonl)],
            stdout=obs_err, stderr=obs_err, env=env, start_new_session=True,
        )
        eff.wait_observer_ready(ready_file, obs_err_path, obs_proc)

        if args.source and Path(args.source).is_dir() and not args.force_tiny:
            repo = wt.isolate_product(Path(args.source), work / "product-live", Path(args.scope_config))
            fixture = "product"
        else:
            repo = make_soak_repo(work)
            fixture = "tiny"
        editor_repo = repo
        if fixture == "product" and not args.external_editor:
            editor_repo = prepare_product_editor(repo)
        cfg = work / "config.yaml"
        # Product acceptance must measure what production builds, so the tiny
        # fixture keeps its narrow set but a Product run does not.
        profile = resolve_profile(args.profile, fixture)
        scope_text = Path(args.scope_config).read_text() if fixture == "product" else ""
        cfg_text = build_config_text(repo, profile, scope_text)
        cfg.write_text(cfg_text)
        if profile == "full" and "extractors:" in cfg_text:
            raise RuntimeError(
                "profile=full must leave the extractors key unset, but the "
                "effective config pins it; refusing to report a full-profile "
                "run that silently narrowed the extractor set"
            )

        watch_log_path = work / "watch.stderr"
        watch_log = watch_log_path.open("w")
        # Stable-input claims require that only our editor writes this tree.
        wt.assert_isolated_inputs(repo, work)
        # Everything before the spawn is harness setup and is reported on its
        # own, so it can never be folded into the observed startup interval.
        setup_before_launch_s = round(time.monotonic() - t0, 3)
        watch_launch_monotonic = time.monotonic()
        watch_launch_wall_ns = time.time_ns()
        watch_proc = start_watch(enola_bin, repo, cfg, work, url, args.watch_every, watch_log)
        stderr_paths = {"observer": obs_err_path, "watch": watch_log_path}
        initial_frames = wt.wait_frames(
            jsonl, 1, max(every_s * 6, 60), obs_proc, watch_proc, stderr_paths)
        # Ends where the harness NOTICED the frame, so it includes detection
        # lag; the apply-stamped variant ends inside the observer instead.
        launch_to_initial_frame_observed_ms = round(
            (time.monotonic() - watch_launch_monotonic) * 1000.0, 3
        )
        launch_to_initial_applied_ms = wt._ms_from_ns(
            watch_launch_wall_ns, initial_frames[0].get("consumer_end_ns"))
        lifecycle_available = bool(wt.read_lifecycle(lifecycle_path))
        if not lifecycle_available and not args.allow_legacy_observer:
            raise RuntimeError(
                "observer wrote no Begin/End lifecycle records: in-flight "
                "generations would be unobservable and quiescence would be "
                "timing-only. Omit --observer so the harness rebuilds it, or "
                "pass --allow-legacy-observer to accept the weaker check."
            )

        edit_log = work / "edits.jsonl"
        edit_log.touch()
        allowlist = [a.strip() for a in (args.editor_allowlist or "").split(",") if a.strip()]
        observed_log = work / "observed_edits.jsonl"
        ready_path = Path(args.ready_file) if args.ready_file else work / "READY.json"
        finish_path = Path(args.finish_file) if args.finish_file else work / "STOP-EDITOR"
        observed_records: list = []
        input_snapshot = None
        full_baseline = None
        editor_finished_ns = None
        # Live polling walks only the declared subtree; an empty allowlist means
        # the whole tree, which is fine for the tiny fixture but would be far
        # too expensive on Product.
        poll_roots = allowlist or ["."]
        if args.external_editor:
            # A real AI editor drives this run. The harness must NOT mutate the
            # source: anything it wrote would be indistinguishable from the
            # editor's work in the observed-change stream.
            #
            # Two scopes, deliberately different:
            #   integrity - ONE full checkout hash now and ONE at the end, so an
            #               out-of-allowlist change cannot escape unnoticed;
            #   live poll - the declared subtree only, metadata first, hashing
            #               just the files whose mtime or size moved, so the
            #               measurement does not compete with the watcher for
            #               I/O and distort the latency being measured.
            full_baseline = wt.input_state(repo)
            input_snapshot = wt.hash_changed({}, wt.subtree_state(repo, poll_roots))
            write_ready(ready_path, {
                "status": "READY",
                "protocol": "external-editor",
                "edit_this_repo": str(repo),
                "repo_is_isolated_copy": True,
                "harness_writes_source": False,
                "signal_finished_by_creating": str(finish_path),
                "finish_is_advisory": (
                    "the run continues to its full requested duration after the "
                    "finish signal; the signal only marks when active editing "
                    "stopped"
                ),
                "editor_allowlist": allowlist or None,
                "changes_outside_allowlist_are_reported_not_blocked": True,
                "extractor_profile": profile,
                "effective_config": str(cfg),
                "watch_context": WATCH_CTX,
                "repo_id": REPO_ID,
                "watch_every": args.watch_every,
                "artifacts": {
                    "consumer_frames": str(jsonl),
                    "observer_lifecycle": str(lifecycle_path),
                    "observed_edits": str(observed_log),
                    "watch_stderr": str(watch_log_path),
                    "observer_stderr": str(obs_err_path),
                    "report": str(work / "soak.json"),
                },
                "input_capture": "whole isolated checkout, any file type, "
                                 "polled every " + str(args.input_poll) + "s",
                "save_timestamp_basis": "filesystem-mtime",
                "do_not_copy_credentials": True,
                "requested_duration_s": duration_s,
                "min_duration_s": parse_duration(args.min_duration) if args.min_duration else None,
            })
            print("READY " + str(ready_path), flush=True)
        else:
            editor_proc = subprocess.Popen(
                [sys.executable, str(HERE / "soak.py"), "--editor-worker",
                 "--repo", str(editor_repo), "--edit-log", str(edit_log),
                 "--duration", str(max(duration_s - every_s * 3, 5)) + "s",
                 "--edit-interval", args.edit_interval, "--seed", str(args.seed)],
                stdout=(work / "editor.log").open("w"), stderr=subprocess.STDOUT,
                start_new_session=True,
            )

        restart_watch_at = t0 + duration_s * 0.35 if args.restart_watch else None
        restart_broker_at = t0 + duration_s * 0.65 if args.restart_broker else None
        outage_s = parse_duration(args.outage) if args.restart_broker else 0.0
        outage_obs = {"attempted": False}
        outage_ns = None
        next_rss = t0
        next_input_poll = t0
        input_poll_s = parse_duration(args.input_poll)
        # An editor that finishes early must not shorten the watch window: the
        # point is sustained watch behaviour, not just the edit burst.
        min_duration_s = parse_duration(args.min_duration) if args.min_duration else 0.0
        deadline = t0 + max(duration_s, min_duration_s)
        last_restart_ns = 0
        while time.monotonic() < deadline:
            now = time.monotonic()
            if args.external_editor and now >= next_input_poll:
                input_snapshot, found, _ = poll_observed_changes(
                    repo, input_snapshot, poll_roots, observed_log)
                observed_records.extend(found)
                next_input_poll = now + input_poll_s
            if args.external_editor and editor_finished_ns is None and finish_path.exists():
                editor_finished_ns = time.time_ns()
                events.append({"kind": "editor-finished-signal", "ns": editor_finished_ns})
            if now >= next_rss:
                sample = {"ns": time.time_ns(), "watch_rss_kb": rss_kb(watch_proc.pid)}
                if sample["watch_rss_kb"] is not None:
                    rss_samples.append(sample)
                next_rss = now + parse_duration(args.rss_interval)
            if obs_proc.poll() is not None:
                raise RuntimeError("observer exited mid-soak:\n" + obs_err_path.read_text()[-4000:])
            if watch_proc.poll() is not None:
                # Documented: broker errors terminate the watch process.
                events.append({
                    "kind": "watch-exited",
                    "ns": time.time_ns(),
                    "returncode": watch_proc.returncode,
                    "tail": watch_log_path.read_text()[-600:],
                })
                down = time.time_ns()
                watch_proc = start_watch(enola_bin, repo, cfg, work, url, args.watch_every, watch_log)
                last_restart_ns = time.time_ns()
                events.append({"kind": "watch-restarted", "ns": last_restart_ns,
                               "downtime_ms": round((last_restart_ns - down) / 1e6, 3)})
            if restart_watch_at and now >= restart_watch_at:
                restart_watch_at = None
                down = time.time_ns()
                eff.stop_process(watch_proc)
                events.append({"kind": "watch-restart-planned", "ns": down})
                watch_proc = start_watch(enola_bin, repo, cfg, work, url, args.watch_every, watch_log)
                last_restart_ns = time.time_ns()
                events.append({"kind": "watch-restarted", "ns": last_restart_ns,
                               "downtime_ms": round((last_restart_ns - down) / 1e6, 3)})
            if restart_broker_at and now >= restart_broker_at:
                restart_broker_at = None
                caught = None
                if args.outage_during_publish:
                    # Kill the broker WHILE a generation is publishing. A bounce
                    # landing between publishes never makes the watcher publish
                    # into a dead broker and so proves nothing.
                    catch_until = time.monotonic() + max(every_s * 4, 20)
                    while time.monotonic() < catch_until:
                        pending = wt.open_begins(
                            wt.read_lifecycle(lifecycle_path), WATCH_CTX
                        )
                        if pending:
                            caught = pending[0].get("run_id")
                            break
                        if watch_proc.poll() is not None:
                            break
                        time.sleep(0.02)
                down = time.time_ns()
                outage_ns = down
                events.append({
                    "kind": "broker-outage-planned", "ns": down,
                    "during_publish": bool(caught),
                    "open_begin_run_id": caught,
                    "requested_outage_s": outage_s,
                })
                eff.stop_process(nats_proc)
                if outage_s > 0:
                    time.sleep(outage_s)
                nats_proc = subprocess.Popen(
                    [str(eff.NATS_SERVER), "-c", str(conf)],
                    stdout=nats_log, stderr=subprocess.STDOUT, start_new_session=True,
                )
                eff.wait_port("127.0.0.1", port)
                broker_ready_ns = time.time_ns()
                # Watch-exit observation below is not broker downtime.
                broker_restart_ms = round((broker_ready_ns - down) / 1e6, 3)
                # Documented policy says broker errors terminate graph watch.
                # Record what actually happened instead of asserting the policy.
                settle = time.monotonic() + min(max(every_s, 2.0), 10.0)
                exited = None
                while time.monotonic() < settle:
                    if watch_proc.poll() is not None:
                        exited = watch_proc.returncode
                        break
                    time.sleep(0.05)
                events.append({
                    "kind": "broker-restarted", "ns": broker_ready_ns,
                    "downtime_ms": broker_restart_ms,
                    "during_publish": bool(caught),
                    "watch_exited": exited is not None,
                    "watch_returncode": exited,
                })
                outage_obs = {
                    "attempted": True,
                    "during_publish": bool(caught),
                    "open_begin_run_id": caught,
                    "requested_outage_s": outage_s,
                    "measured_outage_ms": broker_restart_ms,
                    "outage_measurement_basis": "stop request to broker port ready; excludes subsequent watch-exit observation",
                    "watch_exited": exited is not None,
                    "watch_returncode": exited,
                }
            time.sleep(0.25)

        eff.stop_process(editor_proc)
        editor_proc = None
        if args.external_editor:
            # Final sweep: catch anything written between the last poll and the
            # deadline before the inputs are declared frozen.
            input_snapshot, found, _ = poll_observed_changes(
                repo, input_snapshot, poll_roots, observed_log)
            observed_records.extend(found)
            # ONE full re-inventory, after the editing window: this is what
            # catches a change the subtree poll could never have seen.
            final_full = wt.input_state(repo)
            full_diff = wt.classify_input_changes(full_baseline, final_full, repo, allowlist)
            edits = observed_records
            save_basis = "filesystem-mtime"
        else:
            edits = [json.loads(l) for l in edit_log.read_text().splitlines() if l.strip()]
            if not edits:
                raise RuntimeError("editor produced no edits")
            save_basis = "fsync-completion"
        edit_errors = [e for e in edits if e.get("error")]
        edits = [e for e in edits if not e.get("error")]
        edit_stamps = [e["ns"] for e in edits if isinstance(e.get("ns"), int)]
        if not edit_stamps and not args.external_editor:
            raise RuntimeError("no edit timestamps recorded")
        # Scripted: durable write completion. External: observed filesystem
        # mtime. Never both in one series.
        last_save_ns = max(edit_stamps) if edit_stamps else 0
        span = editing_span(edits)
        editor_activity = {
            "mode": "external-editor" if args.external_editor else "scripted",
            "save_observation_basis": save_basis,
            **span,
            "editor_finished_signal_ns": editor_finished_ns,
            "run_continued_after_finish_s": (
                round((time.time_ns() - editor_finished_ns) / 1e9, 3)
                if editor_finished_ns else None
            ),
            "idle_s": round(max(0.0, (time.monotonic() - t0) - span["active_editing_s"]), 3),
            "editor_allowlist": allowlist or None,
        }
        if args.external_editor:
            editor_activity.update({
                "live_poll_scope": poll_roots,
                "poll_interval_s": input_poll_s,
                "integrity_scope": "whole isolated checkout, hashed once at "
                                   "start and once after editing",
                "final_full_inventory_diff": {
                    k: full_diff[k] for k in ("added", "modified", "deleted", "changed_count")
                },
                "changes_outside_allowlist": full_diff["outside_allowlist"],
                "observation_limits": [
                    "changes are seen no earlier than the next poll tick, so "
                    f"per-change detection lag is up to {input_poll_s}s and "
                    "observed_ns - ns is harness lag, never watcher latency",
                    "a file changed and reverted between two ticks is invisible "
                    "to the live poll; the final full inventory only shows net "
                    "difference, not every intermediate state",
                    "out-of-allowlist changes are detected by the final full "
                    "inventory, so they are reported without a timestamp",
                    "mtime comes from the editor's write landing and is not a "
                    "durable-save time; no durable timestamp is inferred from "
                    "polling",
                    "this is complete change detection for the DECLARED "
                    "experiment scope, not generic coverage of every path a "
                    "watcher might observe",
                ],
            })
        if args.external_editor and not edits:
            # Cold equality would pass trivially against an untouched checkout.
            editor_activity["inconclusive"] = (
                "no input change was observed, so cold equality here says "
                "nothing about incremental correctness"
            )

        # Inputs are frozen here: hash EVERY analysis input, not just the files
        # we happen to know about, so an unrelated input moving is detected.
        frozen_inputs = wt.input_state(repo)
        settled = wt.wait_quiescent(
            jsonl, every_s, last_save_ns, frozen_inputs,
            obs_proc, watch_proc, stderr_paths, observer_bin, url,
            context=WATCH_CTX, lifecycle_path=lifecycle_path,
            lifecycle_available=lifecycle_available,
            cap_s=max(every_s * 30, 180), repo=repo,
            restart_ns=last_restart_ns,
        )
        frames = settled["frames"]
        watch_frames = [f for f in frames if f.get("context") in (None, WATCH_CTX)]
        for i, fr in enumerate(watch_frames):
            wt.assert_frozen_begin(fr, f"soak-gen-{i}")
        final = settled["selected"]
        generations_after_outage = (
            len([f for f in watch_frames if int(f.get("first_ns") or 0) > outage_ns])
            if outage_ns else 0
        )

        # Target the exact cold context/run: a total frame count would also be
        # satisfied by an unrelated context publishing.
        seen_runs = {fr.get("run_id") for fr in wt.read_frames(jsonl)}
        cold = wt.graph_analyze(
            enola_bin, repo, cfg, work / "state-cold", url, COLD_CTX, REPO_ID
        )
        cold_frame = wt.wait_context_frame(
            jsonl, COLD_CTX, seen_runs, max(every_s * 20, 180),
            obs_proc, watch_proc, stderr_paths,
        )
        # The whole inventory is checked on BOTH sides of cold: frozen before
        # the quiescence wait, re-read after cold has finished reading the tree.
        cold_stability = wt.inventory_stable_across_cold(repo, frozen_inputs)
        equal = cold_frame["normalized_hash"] == final["normalized_hash"]
        equality_inconclusive = None
        if not cold_stability["stable"]:
            if args.external_editor:
                # A real editor may write again after signalling; say so rather
                # than reporting an equality verdict built on shifted inputs.
                equality_inconclusive = (
                    "inputs changed between the frozen inventory and the end of "
                    "cold analyze, so the two graphs were not built from the "
                    "same bytes"
                )
            else:
                raise RuntimeError(
                    "harness-owned inputs moved while cold analyze ran: "
                    f"{cold_stability['diff']}"
                )

        rows = join_generations(watch_frames, edits)
        elapsed = round(time.monotonic() - t0, 3)
        report = {
            "measurement_kind": "production-graph-watch-soak",
            "not_request_driven_resident": True,
            "not_product_acceptance": True,
            "fixture": fixture,
            "work": str(work),
            "port": port,
            "elapsed_s": elapsed,
            "requested_duration_s": duration_s,
            "watch_every": args.watch_every,
            "window_semantics": "fixed collection window, not debounce; "
                                "edits during analysis queue and the next window "
                                "opens after that analysis finishes",
            "broker_error_policy_documented":
                "broker errors terminate graph watch; the harness restarts it",
            "broker_outage_observation": outage_obs,
            "broker_outage_verdict": outage_verdict(outage_obs, generations_after_outage),
            "generations_completed_after_outage": generations_after_outage,
            "broker_recovery_acceptance": "untested until observed in a Product run",
            "binary": eff.binary_record(enola_bin),
            "observer": eff.binary_record(Path(observer_bin)),
            "snapshot": str(bins["snapshot"]),
            "snapshot_note": bins["note"],
            "repo_id": REPO_ID,
            "completion_selection": settled["selection_basis"],
            "internal_drain_proved": settled["internal_drain_proved"],
            "requires_watcher_watermark_for_proof": True,
            "quiet_margin_s": settled["quiet_margin_s"],
            "open_begin_tracking": settled["open_begin_tracking"],
            "incomplete_begin_end_pairs_at_selection":
                settled["incomplete_begin_end_pairs_at_selection"],
            "abandoned_begins": settled["abandoned_begins"],
            "generations_begun_after_last_save":
                settled["generations_begun_after_last_save"],
            "begin_after_save_is_not_capture_proof": True,
            "quiescence_limitations": settled["limitations"],
            "input_hash_scope": "whole-isolated-checkout",
            "input_scope_excluded_dirs": sorted(wt.IGNORED_INPUT_DIRS),
            "input_stability_across_cold": cold_stability,
            "equality_inconclusive_reason": equality_inconclusive,
            "input_files_hashed": len(frozen_inputs),
            "inputs_isolated_under_work": True,
            "save_ns_basis": SAVE_NS_BASIS[save_basis],
            "harness_setup_before_watch_launch_s": setup_before_launch_s,
            "watch_launch_to_initial_frame_observed_ms": launch_to_initial_frame_observed_ms,
            "watch_launch_to_initial_consumer_applied_ms": launch_to_initial_applied_ms,
            "frame_detection_poll_interval_s": wt.FRAME_POLL_INTERVAL_S,
            "observed_startup_note": wt.OBSERVED_STARTUP_NOTE,
            "clock_basis": wt.CLOCK_BASIS,
            "clock_assumption": wt.CLOCK_ASSUMPTION,
            "causality_note": wt.CAUSALITY_NOTE,
            "fsync_visibility_note": wt.VISIBILITY_NOTE,
            "save_observation_bases": wt.SAVE_OBSERVATION_BASIS,
            "extractor_profile": profile,
            "extractors_key_omitted_for_production_default": profile == "full",
            "effective_config": cfg_text,
            "editor_mode": editor_activity["mode"],
            "editor_activity": editor_activity,
            "harness_mutated_source": not args.external_editor,
            "scripted_editor_root": str(editor_repo) if not args.external_editor else None,
            "synthetic_module_in_product": fixture == "product" and not args.external_editor,
            "edit_errors": edit_errors,
            "edit_count": len(edits),
            "edit_kinds": {k: sum(1 for e in edits if e.get("kind") == k) for k in EDIT_KINDS},
            "observed_change_kinds": {
                k: sum(1 for e in edits if e.get("change") == k)
                for k in ("added", "modified", "deleted")
            },
            "input_hash_scope": "whole-isolated-checkout",
            "input_scope_excluded_dirs": sorted(wt.IGNORED_INPUT_DIRS),
            "input_files_hashed": len(frozen_inputs),
            "generation_count": len(watch_frames),
            "generations": rows,
            "telemetry_totals": wt.telemetry_totals(watch_frames),
            "latency_summary": {
                "write_to_durable_ms": summarize([r["write_to_durable_ms"] for r in rows]),
                "time_since_latest_observed_save_at_begin_ms":
                    summarize([r["time_since_latest_observed_save_at_begin_ms"] for r in rows]),
                "begin_to_first_batch_ms": summarize([r["begin_to_first_batch_ms"] for r in rows]),
                "begin_to_end_ms": summarize([r["begin_to_end_ms"] for r in rows]),
                "end_to_consumer_ms": summarize([r["end_to_consumer_ms"] for r in rows]),
                "time_since_latest_observed_save_at_consumer_ms":
                    summarize([r["time_since_latest_observed_save_at_consumer_ms"] for r in rows]),
            },
            "rss_samples": len(rss_samples),
            "rss_kb": summarize([s["watch_rss_kb"] for s in rss_samples]),
            "lifecycle_events": events,
            "selected_generation": final.get("target_generation"),
            "watch_hash": final["normalized_hash"],
            "cold_hash": cold_frame["normalized_hash"],
            "cold_context": COLD_CTX,
            "cold_run_id": cold_frame.get("run_id"),
            "equal": equal,
            "convergence": wt.first_cold_equal(
                watch_frames, cold_frame["normalized_hash"], WATCH_CTX, last_save_ns),
            "cold_summary": {
                "BaseGeneration": cold.get("BaseGeneration"),
                "TargetGeneration": cold.get("TargetGeneration"),
                "ParsedFiles": cold.get("ParsedFiles"),
                "OwnersPublished": cold.get("OwnersPublished"),
            },
        }
        (work / "soak.json").write_text(json.dumps(report, indent=2) + "\n")
        (work / "rss.jsonl").write_text("".join(json.dumps(s) + "\n" for s in rss_samples))
        print(json.dumps({k: v for k, v in report.items() if k != "generations"}, indent=2))
        print("generations:", len(rows), "→", work / "soak.json")
        if edit_errors:
            print("SOAK FAIL: scripted editor operations failed", file=sys.stderr)
            return 1
        if not equal:
            print("SOAK FAIL: cold equality mismatch", file=sys.stderr)
            return 1
        print("soak ok", elapsed, "s", work)
        return 0
    finally:
        eff.stop_process(editor_proc)
        eff.stop_process(watch_proc)
        eff.stop_process(obs_proc)
        eff.stop_process(nats_proc)
        if watch_log:
            watch_log.close()
        nats_log.close()


def self_test() -> int:
    failures = []

    def check(name, cond, detail=""):
        if not cond:
            failures.append(f"{name}: {detail or 'failed'}")

    text = Path(__file__).read_text()
    check("duration 30m", parse_duration("30m") == 1800.0)
    check("duration 90s", parse_duration("90s") == 90.0)
    check("duration 1h", parse_duration("1h") == 3600.0)
    check("real 5s default", '"--watch-every", default="5s"' in text or "default=\"5s\"" in text)
    check("external editor process", "--editor-worker" in text and "Popen" in text)
    check("mixed edit kinds", all(k in text for k in EDIT_KINDS))
    check("rename and delete covered", "rename-file" in EDIT_KINDS and "delete-file" in EDIT_KINDS)
    check("per-generation latencies", "time_since_latest_observed_save_at_consumer_ms" in text and "begin_to_first_batch_ms" in text)
    check("rss sampled", "watch_rss_kb" in text)
    check("broker restart", "broker-restarted" in text)
    check("watch restart", "watch-restarted" in text)
    check("broker error policy documented", "broker errors terminate" in text)
    check("fixed window documented", "not a debounce" in text)
    check("quiescence helper reused", "wait_quiescent" in text)
    check("same repo id as cold", "COLD_CTX, REPO_ID" in text)

    # --- claims must stay heuristic ---------------------------------------
    proof_word = "drain" + "ed"
    check("no drained-proof claim", f'"{proof_word}"' not in text
          and "internal_drain_proved" in text)
    check("watermark named as missing proof",
          "requires_watcher_watermark_for_proof" in text)
    check("limits carried from watch.py", "quiescence_limitations" in text)
    check("clock domains separated",
          "wt.CLOCK_BASIS" in text and "wt.CLOCK_ASSUMPTION" in text)
    check("open Begin tracking wired",
          "OBSERVER_LIFECYCLE_FILE" in text and "open_begins" in text)
    check("legacy observer must be opt-in", "--allow-legacy-observer" in text)
    check("all analysis inputs hashed", "wt.input_state(repo)" in text)
    check("isolated inputs enforced", "assert_isolated_inputs" in text)
    check("cold wait by context/run", "wait_context_frame" in text)
    count_wait = "len(frames)" + " + 1"
    check("cold wait not by frame count", count_wait not in text)
    check("durable save basis", "durable write+fsync completion" in text)
    # The report must carry the basis of the series it actually recorded: an
    # external-editor run is timed from filesystem mtime, not from fsync.
    # Built at runtime: written literally, this token would be its own
    # counterexample and the check could never pass.
    fixed_basis = '"save_ns_basis": ' + '"durable write+fsync completion"'
    check("save basis label is derived, not fixed",
          '"save_ns_basis": SAVE_NS_BASIS[save_basis]' in text
          and fixed_basis not in text)
    check("external editor basis is filesystem mtime",
          "mtime" in SAVE_NS_BASIS["filesystem-mtime"]
          and "fsync" not in SAVE_NS_BASIS["filesystem-mtime"])
    check("scripted basis keeps its fsync label",
          SAVE_NS_BASIS["fsync-completion"] == "durable write+fsync completion")
    check("both editor modes have a label",
          set(SAVE_NS_BASIS) == set(wt.SAVE_OBSERVATION_BASIS))

    # --- per-generation telemetry on joined rows ---------------------------
    tel_rows = join_generations(
        [wt._telemetry_frame(1, 0, nodes=40, edges=12, batches=2, node_records=40),
         wt._telemetry_frame(2, 1, nodes=42, edges=13, batches=3, node_records=37,
                             prev_nodes=40, prev_edges=12)],
        [{"ns": 1}],
    )
    check("rows carry per-generation volume metadata",
          all(r["telemetry_available"] for r in tel_rows)
          and tel_rows[1]["batches_observed"] == 3, str(tel_rows[1]))
    check("rows keep latency columns alongside telemetry",
          "begin_to_end_ms" in tel_rows[0] and "payload_bytes_total" in tel_rows[0])
    check("rows distinguish initial from delta",
          [r["generation_kind"] for r in tel_rows] == ["initial", "delta"])
    check("rows separate records sent from net entity change",
          tel_rows[1]["node_records_sent"] == 37 and tel_rows[1]["graph_nodes_delta"] == 2)
    legacy_rows = join_generations(
        [{"target_generation": 2, "base_generation": 1, "first_ns": 300,
          "broker_end_ns": 500}], [{"ns": 1}])
    check("older observer rows report null telemetry",
          legacy_rows[0]["telemetry_available"] is False
          and legacy_rows[0]["node_records_sent"] is None, str(legacy_rows[0]))

    totals = wt.telemetry_totals(
        [wt._telemetry_frame(1, 0, nodes=40, edges=12, batches=2, node_records=40)])
    check("soak reports completed-only totals",
          '"telemetry_totals": wt.telemetry_totals(watch_frames)' in text
          and totals["aborted_or_in_flight_included"] is False)
    check("startup intervals are measured at the spawn and named for their endpoints",
          "watch_launch_monotonic = time.monotonic()" in text
          and "watch_launch_wall_ns = time.time_ns()" in text
          and '"watch_launch_to_initial_frame_observed_ms"' in text
          and '"watch_launch_to_initial_consumer_applied_ms"' in text
          and '"harness_setup_before_watch_launch_s": setup_before_launch_s' in text)
    check("soak uses the same interval definitions as watch",
          '"frame_detection_poll_interval_s": wt.FRAME_POLL_INTERVAL_S' in text
          and "wt._ms_from_ns(" in text)

    # --- broker outage verdicts: never upgrade a bounce to acceptance -----
    check("no outage requested is untested",
          outage_verdict({"attempted": False}, 0).startswith("untested"))
    bounce = {"attempted": True, "during_publish": False, "watch_exited": False}
    check("bounce between publishes is untested",
          outage_verdict(bounce, 5).startswith("untested"), outage_verdict(bounce, 5))
    check("bounce verdict names the reason",
          "not recovery acceptance" in outage_verdict(bounce, 5))
    survived = {"attempted": True, "during_publish": True, "watch_exited": False}
    check("survived outage is reported, not accepted",
          "not reproduced" in outage_verdict(survived, 3), outage_verdict(survived, 3))
    exited = {"attempted": True, "during_publish": True, "watch_exited": True}
    check("exit with later generations is replay",
          "later generations completed" in outage_verdict(exited, 2))
    check("exit without later generations is not replay",
          "replay was NOT observed" in outage_verdict(exited, 0))
    check("acceptance always deferred",
          "untested until observed in a Product run" in text)
    check("stops children", text.count("eff.stop_process") >= 4)
    check("frozen begin asserted", "assert_frozen_begin" in text)

    rows = join_generations(
        [{"target_generation": 2, "base_generation": 1, "first_ns": 300,
          "first_batch_ns": 400, "broker_end_ns": 500, "consumer_end_ns": 600}],
        [{"ns": 100}, {"ns": 250}, {"ns": 900}],
    )
    check("join picks last save before begin", rows[0]["latest_observed_save_ns"] == 250, str(rows[0]))
    # Realistic nanosecond scale: a 30ms fsync ahead of a 5s collection window.
    MS = 1_000_000
    dur = join_generations(
        [{"target_generation": 2, "first_ns": 5_030 * MS, "first_batch_ns": 5_070 * MS,
          "broker_end_ns": 5_110 * MS, "consumer_end_ns": 5_111 * MS}],
        [{"ns": 31 * MS, "start_ns": 1 * MS}],
    )
    check("join records the write->durable cost",
          dur[0]["write_to_durable_ms"] == 30.0, str(dur[0]))
    check("join measures save latency from durable completion",
          dur[0]["time_since_latest_observed_save_at_begin_ms"] == 4999.0, str(dur[0]))
    pre_write = join_generations(
        [{"target_generation": 2, "first_ns": 5_030 * MS, "first_batch_ns": 5_070 * MS,
          "broker_end_ns": 5_110 * MS, "consumer_end_ns": 5_111 * MS}],
        [{"ns": 1 * MS, "start_ns": 1 * MS}],
    )
    # Timing the instant BEFORE the write attributes the whole fsync cost to
    # the collection window: 5029ms instead of the true 4999ms.
    check("timing from the pre-write instant would overstate the window",
          pre_write[0]["time_since_latest_observed_save_at_begin_ms"] == 5029.0
          and pre_write[0]["time_since_latest_observed_save_at_begin_ms"]
          > dur[0]["time_since_latest_observed_save_at_begin_ms"],
          str(pre_write[0]))
    check("latency math", rows[0]["begin_to_end_ms"] == 0.0002 or rows[0]["begin_to_end_ms"] is not None)
    check("ignores later saves", rows[0]["time_since_latest_observed_save_at_consumer_ms"] is not None)
    check("summarize empty", summarize([])["count"] == 0)
    check("summarize median", summarize([1, 3, 2])["median"] == 2)

    with tempfile.TemporaryDirectory() as tmp:
        repo = make_soak_repo(Path(tmp))
        check("fixture multi-file", len(list((repo / "src").glob("*.ts"))) == 3)
        rng = random.Random(7)
        extra: list[str] = []
        for kind in ("create-file", "add-export", "edit-body", "add-import"):
            rec = _apply_edit(kind, repo / "src", rng, 1, extra)
            check(f"edit {kind}", isinstance(rec.get("ns"), int), str(rec))
            check(f"edit {kind} records write start",
                  isinstance(rec.get("start_ns"), int)
                  and rec["start_ns"] <= rec["ns"], str(rec))
        check("create tracked in extra", extra == ["mod1.ts"], str(extra))
        rec = _apply_edit("rename-file", repo / "src", rng, 2, extra)
        check("rename tracked", extra == ["mod1r.ts"], str(extra))
        rec = _apply_edit("delete-file", repo / "src", rng, 3, extra)
        check("delete tracked", extra == [], str(rec))
        check("delete records both timestamps",
              isinstance(rec.get("start_ns"), int) and isinstance(rec.get("ns"), int))
        big = _write(Path(tmp) / "big.ts", "y" * (4 << 20))
        check("durable write completion after start", big["ns"] > big["start_ns"], str(big))

    with tempfile.TemporaryDirectory() as tmp:
        product = Path(tmp) / "product"
        product.mkdir()
        sentinel = product / "application.ts"
        sentinel.write_text("export const untouched = true;\n")
        editor_root = prepare_product_editor(product)
        rng = random.Random(7)
        extra = []
        for i, kind in enumerate(("create-file", "add-export", "remove-export", "edit-body", "add-import", "remove-import", "rename-file", "delete-file"), 1):
            record = _apply_edit(kind, editor_root / "src", rng, i, extra)
            check("Product editor " + kind, "error" not in record and "ns" in record)
        check("Product application unchanged", sentinel.read_text() == "export const untouched = true;\n")
        try:
            prepare_product_editor(product)
            check("Product fixture collision refused", False)
        except FileExistsError:
            pass

    # --- extractor profile: full must not pin the extractor set -------------
    ts_cfg = build_config_text(Path("/tmp/r"), "typescript")
    full_cfg = build_config_text(Path("/tmp/r"), "full")
    check("typescript profile pins the extractor", "extractors: [typescript]" in ts_cfg)
    check("full profile omits the extractors key", "extractors:" not in full_cfg, full_cfg)
    check("full profile still disables explainers/renderers",
          "explainers: []" in full_cfg and "renderers: []" in full_cfg)
    check("full profile keeps the repo root", "repo: /tmp/r" in full_cfg)
    check("scope config appended when given",
          "owner:" in build_config_text(Path("/tmp/r"), "full", "owner: x\n"))
    check("auto picks production default for Product",
          resolve_profile("auto", "product") == "full")
    check("auto keeps the tiny fixture narrow",
          resolve_profile("auto", "tiny") == "typescript")
    check("explicit profile overrides auto",
          resolve_profile("typescript", "product") == "typescript"
          and resolve_profile("full", "tiny") == "full")
    bad = False
    try:
        build_config_text(Path("/tmp/r"), "nonsense")
    except ValueError:
        bad = True
    check("unknown profile rejected", bad)
    check("soak fixture config is no longer hardcoded to typescript",
          "cfg_text = build_config_text(" in text)
    check("full profile documented against PRODUCT.md", "PRODUCT.md:214" in text)

    # --- external-editor mode ------------------------------------------------
    check("external editor flag exists", '"--external-editor"' in text)
    check("external mode does not mutate source",
          '"harness_mutated_source": not args.external_editor' in text)
    check("READY exposes the isolated repo path", '"edit_this_repo": str(repo)' in text)
    check("READY exposes observer artifacts",
          '"observer_lifecycle": str(lifecycle_path)' in text)
    check("finish trigger is a file", "STOP-EDITOR" in text)
    check("run outlasts an early finish", "--min-duration" in text)

    with tempfile.TemporaryDirectory() as tmp:
        repo = Path(tmp) / "repo"
        (repo / "src" / "deeplinks").mkdir(parents=True)
        (repo / "src" / "deeplinks" / "password.ts").write_text("export const p = 1;\n")
        (repo / "src" / "app.ts").write_text("export const a = 1;\n")
        (repo / "media").mkdir()
        (repo / "media" / "big.bin").write_bytes(b"\0" * 4096)
        log = Path(tmp) / "observed.jsonl"
        roots = ["src/deeplinks"]

        # Integrity baseline: whole checkout, once.
        full_baseline = wt.input_state(repo)
        check("integrity baseline covers the whole checkout",
              any(k.endswith("big.bin") for k in full_baseline), str(list(full_baseline)))
        # Live poll scope: the declared subtree only.
        poll_state = wt.hash_changed({}, wt.subtree_state(repo, roots))
        check("live poll scope excludes unrelated media",
              not any(k.endswith("big.bin") for k in poll_state), str(list(poll_state)))
        check("metadata poll reads no file contents",
              all("sha256" not in v for v in wt.stat_state([repo / "media" / "big.bin"]).values()))

        # An external editor writes without telling us.
        (repo / "src" / "deeplinks" / "invite.ts").write_text("export const i = 1;\n")
        (repo / "src" / "deeplinks" / "password.ts").write_text("export const p = 2;\n")
        poll_state, recs, _ = poll_observed_changes(repo, poll_state, roots, log)
        kinds = {r["path"].split("/")[-1]: r["change"] for r in recs}
        check("external add observed", kinds.get("invite.ts") == "added", str(kinds))
        check("external modify observed", kinds.get("password.ts") == "modified", str(kinds))
        check("observed changes use the mtime basis",
              all(r["observation_basis"] == "filesystem-mtime" for r in recs))
        check("observed changes are persisted",
              log.is_file() and log.read_text().count("\n") == 2)
        check("no durable-save timestamp invented from a poll",
              all("start_ns" not in r for r in recs))

        # An idle poll must observe nothing AND not re-hash the subtree.
        unchanged_before = dict(poll_state)
        poll_state, again, _ = poll_observed_changes(repo, poll_state, roots, log)
        check("idle poll observes nothing", again == [], str(again))
        check("idle poll carries hashes forward instead of re-reading",
              all(poll_state[k].get("sha256") == v.get("sha256")
                  for k, v in unchanged_before.items()), "hashes changed on an idle poll")

        # A deletion INSIDE the poll scope is seen live.
        (repo / "src" / "deeplinks" / "invite.ts").unlink()
        poll_state, gone, _ = poll_observed_changes(repo, poll_state, roots, log)
        check("in-scope deletion observed live",
              [r["change"] for r in gone] == ["deleted"], str(gone))
        check("deleted timestamp labelled as a fallback",
              gone[0]["ns_is_detection_fallback"] is True, str(gone[0]))

        # A change OUTSIDE the poll scope is invisible live, and that is the
        # point of the final full inventory: it must still be reported.
        (repo / "NOTES.md").write_text("design notes\n")
        (repo / "src" / "app.ts").write_text("export const a = 2;\n")
        _, missed, _ = poll_observed_changes(repo, poll_state, roots, log)
        check("out-of-scope change is not seen by the cheap live poll",
              missed == [], str(missed))
        full_diff = wt.classify_input_changes(full_baseline, wt.input_state(repo), repo, roots)
        check("final full inventory catches the out-of-allowlist markdown",
              "NOTES.md" in full_diff["outside_allowlist"], str(full_diff["outside_allowlist"]))
        check("final full inventory catches the out-of-allowlist source edit",
              "src/app.ts" in full_diff["outside_allowlist"], str(full_diff["outside_allowlist"]))
        check("in-allowlist changes are not flagged as violations",
              not any("deeplinks" in p for p in full_diff["outside_allowlist"]),
              str(full_diff["outside_allowlist"]))
        check("final inventory still sees the in-scope edit",
              any(p.endswith("password.ts") for p in full_diff["modified"]),
              str(full_diff["modified"]))

        # Seeding the live poll from the WHOLE-checkout baseline would make the
        # first tick claim every file outside the subtree had been deleted. The
        # run seeds the poll from the subtree itself, so the first tick is
        # empty; this asserts both the behaviour and the wiring.
        fresh_seed = wt.hash_changed({}, wt.subtree_state(repo, roots))
        _, first_tick, _ = poll_observed_changes(repo, fresh_seed, roots, log)
        check("first tick after a scoped seed reports nothing",
              first_tick == [], str(first_tick))
        _, bad_tick, _ = poll_observed_changes(repo, dict(full_baseline), roots, log)
        check("a full-baseline seed WOULD mass-report deletions (why we scope the seed)",
              any(r["change"] == "deleted" and "media" in r["path"] for r in bad_tick),
              str(bad_tick))
        check("run seeds the live poll from the subtree, not the full baseline",
              "wt.hash_changed({}, wt.subtree_state(repo, poll_roots))" in text)

        # Comparing against the INITIAL inventory, not merely rehashing the
        # final tree: a deletion has no trace in the final tree at all.
        (repo / "media" / "big.bin").unlink()
        gone_diff = wt.classify_input_changes(full_baseline, wt.input_state(repo), repo, roots)
        check("final diff vs initial catches a deletion a rehash cannot see",
              any(p.endswith("big.bin") for p in gone_diff["deleted"]),
              str(gone_diff["deleted"]))
        check("deletion outside the allowlist is reported as a violation",
              any("big.bin" in p for p in gone_diff["outside_allowlist"]),
              str(gone_diff["outside_allowlist"]))

        # Cold equality is only meaningful if the whole tree held still across
        # the cold run as well as before it.
        frozen = wt.input_state(repo)
        check("inventory stable across an untouched cold run",
              wt.inventory_stable_across_cold(repo, frozen)["stable"] is True)
        (repo / "src" / "deeplinks" / "password.ts").write_text("export const p = 3;\n")
        moved = wt.inventory_stable_across_cold(repo, frozen)
        check("an input moving during cold is detected",
              moved["stable"] is False and moved["diff"]["changed_count"] == 1, str(moved))
        check("scripted mode fails closed when inputs move across cold",
              "harness-owned inputs moved while cold analyze ran" in text)
        check("external-editor mode labels equality inconclusive instead",
              "equality_inconclusive_reason" in text)

    check("poll overhead limits recorded", '"poll_interval_s": input_poll_s' in text)
    check("observation lag labelled as harness lag, not watcher latency",
          "never watcher latency" in text)
    check("integrity and live scopes are distinguished",
          '"integrity_scope"' in text and '"live_poll_scope"' in text)

    # active vs idle split
    span = editing_span([{"ns": 5_000_000_000}, {"ns": 12_000_000_000}, {"ns": 7_000_000_000}])
    check("active editing span measured", span["active_editing_s"] == 7.0, str(span))
    check("empty span is zero, not an error",
          editing_span([])["active_editing_s"] == 0.0)

    # external-editor rows must not claim a durable fsync basis
    ext_rows = join_generations(
        [{"target_generation": 2, "run_id": "r-ext", "first_ns": 10 * MS,
          "first_batch_ns": 11 * MS, "broker_end_ns": 12 * MS, "consumer_end_ns": 13 * MS}],
        [{"ns": 5 * MS, "observation_basis": "filesystem-mtime", "path": "a.ts"}],
    )
    check("external rows carry the mtime basis",
          ext_rows[0]["observation_basis"] == "filesystem-mtime", str(ext_rows[0]))
    check("rows carry the run id for re-pairing", ext_rows[0]["run_id"] == "r-ext")
    check("pairing is flagged non-causal", ext_rows[0]["pairing_is_causal"] is False)
    check("no durable-write number invented for an external edit",
          ext_rows[0]["write_to_durable_ms"] is None, str(ext_rows[0]))

    if failures:
        print("soak self-test FAIL", file=sys.stderr)
        for f in failures:
            print(" ", f, file=sys.stderr)
        return 1
    print("soak self-test ok")
    return 0


def main() -> int:
    p = argparse.ArgumentParser()
    p.add_argument("--self-test", action="store_true")
    p.add_argument("--editor-worker", action="store_true", help="internal: external editor process")
    p.add_argument("--repo", default="")
    p.add_argument("--edit-log", default="")
    p.add_argument("--duration", default="30m", help="soak length, e.g. 30m (default) or 1h")
    p.add_argument("--edit-interval", default="20s")
    p.add_argument("--rss-interval", default="10s")
    p.add_argument("--seed", type=int, default=1729)
    p.add_argument("--watch-every", default="5s", help="real collection window; default 5s")
    p.add_argument("--binary", default="")
    p.add_argument("--observer", default="")
    p.add_argument("--snapshot", default="")
    p.add_argument("--work", default="")
    p.add_argument("--source", default="", help="Product checkout; isolated clone only")
    p.add_argument("--scope-config", default=str(HERE / "product-graph-scope.yaml"))
    p.add_argument("--force-tiny", action="store_true")
    p.add_argument("--no-restart-watch", dest="restart_watch", action="store_false")
    p.add_argument("--no-restart-broker", dest="restart_broker", action="store_false")
    p.add_argument("--outage-during-publish", action="store_true",
                   help="wait for an in-flight generation before killing the broker, "
                        "so the watcher must publish into a dead broker")
    p.add_argument("--outage", default="0s",
                   help="hold the broker down this long (default 0s: immediate restart)")
    p.add_argument("--allow-legacy-observer", action="store_true",
                   help="accept an observer without Begin/End records; quiescence "
                        "then degrades to timing-only and is labelled as such")
    p.add_argument("--profile", choices=("auto", "full", "typescript"), default="auto",
                   help="extractor profile. 'full' omits the extractors key so the "
                        "binary uses its production default set (PRODUCT.md:214); "
                        "'typescript' pins [typescript]; 'auto' picks typescript for "
                        "the tiny fixture and full for a Product checkout")
    p.add_argument("--external-editor", action="store_true",
                   help="do NOT mutate the source; a separate editor drives the run. "
                        "Writes READY.json with the isolated repo path and artifact "
                        "paths, polls the whole checkout for changes, and finishes "
                        "when the STOP-EDITOR file appears")
    p.add_argument("--editor-allowlist", default="",
                   help="comma-separated repo-relative prefixes the external editor "
                        "was asked to work in; changes outside are reported, not blocked")
    p.add_argument("--input-poll", default="1s",
                   help="external-editor mode: how often the checkout is sampled")
    p.add_argument("--min-duration", default="",
                   help="keep watching at least this long even if the editor finishes early")
    p.add_argument("--ready-file", default="", help="override the READY.json path")
    p.add_argument("--finish-file", default="", help="override the STOP-EDITOR path")
    p.add_argument("--smoke", action="store_true",
                   help="short validation run of this harness; keeps the real 5s window")
    args = p.parse_args()
    if args.self_test:
        return self_test()
    if args.editor_worker:
        return editor_worker(args)
    if args.smoke:
        if args.duration == "30m":
            args.duration = "90s"
        if args.edit_interval == "20s":
            args.edit_interval = "7s"
        if args.rss_interval == "10s":
            args.rss_interval = "5s"
        if args.outage == "0s":
            args.outage = "2s"
        args.outage_during_publish = True
        args.force_tiny = True
    if not args.binary and not args.snapshot:
        raise SystemExit("pass --binary or --snapshot")
    return run_soak(args)


if __name__ == "__main__":
    sys.exit(main())
