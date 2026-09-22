#!/usr/bin/env python3
"""Production `enola graph watch` harness.

Distinct from request-driven resident.py. Uses CLI default --watch-every 5s
(overridable). Tiny-fixture smoke is the default cheap path; Product source
and scope pins are accepted without running a heavyweight Product suite here.
"""
from __future__ import annotations

import argparse
import importlib.util
import hashlib
import json
import os
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
# One repo identity for watch and cold: facts carry it, so it changes node ids.
REPO_ID = "watch-bench"
WATCH_CTX = "watch-bench"
COLD_CTX = "watch-cold"


def parse_every(text: str) -> float:
    text = text.strip()
    if text.endswith("ms"):
        return float(text[:-2]) / 1000.0
    if text.endswith("s"):
        return float(text[:-1])
    if text.endswith("m"):
        return float(text[:-1]) * 60.0
    raise SystemExit(f"unsupported --watch-every {text!r}; use a Go duration such as 5s")


# Which clock each recorded interval is measured against. The harness, watcher,
# broker and observer all run on this host, but broker metadata timestamps and
# Python/Go wall-clock reads are different paths to it, so any CROSS interval
# carries unquantified skew and must not be reported as a precise latency.
CLOCK_BASIS = {
    "time_since_latest_observed_save_at_begin_ms":
        "local_wall(latest observed save) -> broker(Begin) [CROSS]",
    "begin_to_first_batch_ms": "broker -> broker [single]",
    "begin_to_end_ms": "broker -> broker [single]",
    "end_to_consumer_ms": "broker(End) -> local_wall(observer applied) [CROSS]",
    "time_since_latest_observed_save_at_consumer_ms":
        "local_wall -> local_wall [single]",
    "final_convergence_ms":
        "local_wall(last observed edit) -> local_wall(first final-cold-equal "
        "completion) [single]",
}

# Pairing a generation with the nearest prior save is bookkeeping, not a causal
# claim, and the names say so.
CAUSALITY_NOTE = (
    "time_since_latest_observed_save_* pairs each generation with the NEAREST "
    "PRIOR observed save. That is correlation, not causation: the collection "
    "window is fixed, so a generation may have been triggered by an earlier "
    "save and merely happen to follow a later one, and a save landing inside an "
    "open window is folded into a run that started before it. Generation and "
    "run ids are recorded so a reader can re-pair them. The one causal "
    "end-to-end number here is final_convergence_ms: last observed edit to the "
    "first completed generation that equals the final cold graph."
)

# How a save timestamp was obtained. These are different physical events and are
# never mixed in one series.
SAVE_OBSERVATION_BASIS = {
    "fsync-completion":
        "harness-scripted write; time.time_ns() read after fsync(2) returned",
    "filesystem-mtime":
        "external editor; st_mtime_ns found by polling the checkout, i.e. when "
        "the editor's write landed in the filesystem, with no durability "
        "guarantee and no editor cooperation assumed",
}
VISIBILITY_NOTE = (
    "fsync completion is the chosen latency REFERENCE, not a visibility "
    "guarantee: a reader or filesystem watcher can observe written bytes "
    "before fsync returns. Intervals measured from it can therefore be "
    "legitimately NEGATIVE when the watcher sampled during the write; negative "
    "offsets are reported and labelled, never clamped away."
)
CLOCK_ASSUMPTION = (
    "single host; broker metadata timestamps and local wall clock are the same "
    "physical clock read through different paths; CROSS intervals include "
    "unquantified skew and are indicative, not precise"
)

# What heuristic quiescence does NOT establish. Carried into every report so a
# reader cannot mistake it for an internal drain proof.
QUIESCENCE_LIMITS = [
    "no watcher watermark exists, so the harness cannot read the watcher's own "
    "queue; quiescence is inferred from observed traffic, never proved",
    "an analysis longer than the quiet margin with its Begin not yet published "
    "is indistinguishable from an idle watcher (unobservable pre-Begin work)",
    "a Begin broker timestamp records when the Begin was published, not when "
    "the watcher captured the input, so a Begin after a save does not prove "
    "that run scanned the saved bytes",
    "acceptance therefore rests on cold equality of the final completed "
    "generation after inputs are frozen, not on the quiescence signal",
    "an open Begin that a later Begin supersedes, or that predates a watcher "
    "restart, is treated as abandoned and reported separately rather than "
    "waited on; if that classification were wrong the harness would stop "
    "blocking on a run that is genuinely still in flight",
]


def write_durable(path: Path, text: str) -> dict:
    """Write + fsync, returning BOTH the start and the durable completion.

    Timing the instant BEFORE the write overstates every save-relative interval
    by the write+fsync cost, so the durable completion is the reference this
    harness reports. That is a choice of reference, not a visibility claim: the
    watcher may well have read the bytes before fsync returned (see
    VISIBILITY_NOTE), which is why both timestamps are returned and why
    negative offsets are kept rather than clamped.
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


def read_frames(path: Path) -> list:
    if not path.is_file() or path.stat().st_size == 0:
        return []
    return [json.loads(ln) for ln in path.read_text().splitlines() if ln.strip()]


def read_lifecycle(path: Path) -> list:
    """Begin/End records from the observer (OBSERVER_LIFECYCLE_FILE)."""
    if path is None or not Path(path).is_file() or Path(path).stat().st_size == 0:
        return []
    return [json.loads(ln) for ln in Path(path).read_text().splitlines() if ln.strip()]


def open_begins(records: list, context: str | None = None) -> list:
    """Begin records with no matching End: OBSERVED in-flight generations.

    consumer.jsonl only gains a line when a generation COMPLETES, so without
    these records an analysis still running looks exactly like an idle watcher.
    """
    ended = {r.get("run_id") for r in records if r.get("record") == "end"}
    return [
        r for r in records
        if r.get("record") == "begin"
        and r.get("run_id") not in ended
        and (context is None or r.get("context") == context)
    ]


def classify_begins(records: list, context: str | None = None, restart_ns: int = 0) -> dict:
    """Split open Begins into the ACTIVE head and the ABANDONED ones.

    An open Begin is not automatically an in-flight run. If the watcher was
    killed mid-publication, or errored and retried, its Begin stays open
    forever and waiting on it would hang the harness indefinitely. `graph
    watch` analyses one generation at a time per context -- the next collection
    window opens only after the current analysis finishes -- so an open Begin
    that a LATER Begin follows has been superseded and will never End. A Begin
    published before the watcher restarted belongs to a process that no longer
    exists.

    Abandoned Begins are returned rather than discarded: an uncompleted run
    that vanishes silently is exactly the failure this tracking exists to
    catch, and only the head is allowed to block quiescence.
    """
    in_ctx = [
        r for r in records
        if r.get("record") == "begin" and (context is None or r.get("context") == context)
    ]
    ended = {r.get("run_id") for r in records if r.get("record") == "end"}
    # Supersession is decided against EVERY later Begin, not just later open
    # ones: a run that started and finished after this one still proves the
    # watcher moved on and this Begin will never End.
    latest_begin_ns = max((int(r.get("broker_ns") or 0) for r in in_ctx), default=0)

    def ref(rec, reason):
        return {
            "run_id": rec.get("run_id"),
            "context": rec.get("context"),
            "target_generation": rec.get("target_generation"),
            "broker_ns": rec.get("broker_ns"),
            "abandoned_reason": reason,
        }

    active, abandoned = None, []
    for rec in sorted((r for r in in_ctx if r.get("run_id") not in ended),
                      key=lambda r: int(r.get("broker_ns") or 0)):
        began_ns = int(rec.get("broker_ns") or 0)
        if began_ns < latest_begin_ns:
            abandoned.append(ref(rec, "superseded-by-later-begin"))
        elif restart_ns and began_ns < int(restart_ns):
            abandoned.append(ref(rec, "predates-watcher-restart"))
        else:
            active = rec
    return {"active": active, "abandoned": abandoned}


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


def wait_context_frame(
    jsonl: Path, context: str, exclude_run_ids: set, timeout: float,
    obs_proc, watch_proc, stderr_paths,
) -> dict:
    """Wait for a completed generation of ONE exact context/run.

    Waiting on a total frame count instead would be satisfied by an unrelated
    context publishing -- a concurrent watch generation, say -- and would then
    compare against whatever frame happened to land last.
    """
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        if obs_proc.poll() is not None:
            raise RuntimeError("observer exited:\n" + stderr_paths["observer"].read_text())
        if watch_proc is not None and watch_proc.poll() is not None:
            raise RuntimeError("graph watch exited:\n" + stderr_paths["watch"].read_text()[-4000:])
        matches = [
            f for f in read_frames(jsonl)
            if f.get("context") == context and f.get("run_id") not in exclude_run_ids
        ]
        if matches:
            return matches[-1]
        time.sleep(0.05)
    raise RuntimeError(f"timeout waiting for a new completed frame in context {context!r}")


# Directories never treated as analysis inputs. Everything else in the isolated
# checkout is, whatever its extension.
IGNORED_INPUT_DIRS = {
    ".git", "node_modules", ".venv", "__pycache__", "dist", "build",
    ".next", ".turbo", ".enola",
}


def checkout_inventory(repo: Path, ignore_dirs=None) -> list:
    """EVERY file in the isolated checkout, not an extension glob.

    An extension glob cannot support an all-inputs claim. The previous set
    (**/*.ts, *.tsx, *.js, *.mjs, *.json, *.yaml, *.yml) silently omitted .md,
    .py, .tf, .swift, .mts, .cts, .vue, .svelte, .gitignore and every other
    dotfile or config -- any of which an extractor or the scope config can
    read. Walking the whole checkout instead means a file of ANY type being
    added, edited or deleted changes the state and is detected.
    """
    ignore = IGNORED_INPUT_DIRS if ignore_dirs is None else set(ignore_dirs)
    out = []
    for dirpath, dirnames, filenames in os.walk(repo):
        dirnames[:] = sorted(d for d in dirnames if d not in ignore)
        for name in sorted(filenames):
            out.append(Path(dirpath) / name)
    return sorted(out)


def file_state(paths) -> dict:
    """Content hash + mtime for each input path."""
    out = {}
    for path in paths:
        path = Path(path)
        if not path.exists():
            out[str(path)] = {"exists": False}
            continue
        data = path.read_bytes()
        out[str(path)] = {
            "exists": True,
            "sha256": hashlib.sha256(data).hexdigest(),
            "mtime_ns": path.stat().st_mtime_ns,
            "size": len(data),
        }
    return out


def stat_state(paths) -> dict:
    """Metadata-only state: size and mtime, WITHOUT reading file contents.

    Hashing a whole Product checkout on every poll competes with the watcher
    for I/O and distorts the latency the run exists to measure. Metadata is
    enough to find CANDIDATES; only those get read and hashed.
    """
    out = {}
    for path in paths:
        path = Path(path)
        try:
            st = path.stat()
        except (FileNotFoundError, NotADirectoryError):
            out[str(path)] = {"exists": False}
            continue
        out[str(path)] = {"exists": True, "mtime_ns": st.st_mtime_ns, "size": st.st_size}
    return out


def subtree_state(repo: Path, roots) -> dict:
    """Metadata state for the declared editing subtrees only.

    `roots` are repo-relative prefixes the external editor was asked to work
    in. This is the LIVE poll scope, deliberately smaller than the integrity
    scope: completeness for the declared experiment, not generic coverage of
    every path a watcher anywhere might care about. Out-of-allowlist changes
    are caught by the full final inventory instead, not missed.
    """
    repo = Path(repo)
    paths = []
    for root in (roots or []):
        base = repo / root
        if base.is_file():
            paths.append(base)
        elif base.is_dir():
            paths.extend(checkout_inventory(base))
    return stat_state(paths)


def hash_changed(before: dict, after_meta: dict) -> dict:
    """Upgrade a metadata poll to content hashes for the changed paths only."""
    out = dict(after_meta)
    for path, meta in after_meta.items():
        was = before.get(path)
        unchanged = (
            was is not None and was.get("exists") and meta.get("exists")
            and was.get("mtime_ns") == meta.get("mtime_ns")
            and was.get("size") == meta.get("size")
        )
        if unchanged:
            # Carry the old hash forward rather than re-reading the file.
            out[path] = dict(was)
            continue
        if meta.get("exists"):
            out[path] = dict(meta, **file_state([path])[path])
    return out


def input_state(repo: Path) -> dict:
    """Hash of the whole isolated checkout, re-walked on every call."""
    return file_state(checkout_inventory(repo))


def classify_input_changes(before: dict, after: dict, repo: Path, allowlist=None) -> dict:
    """Added / modified / deleted paths between two checkout inventories.

    `allowlist` is the set of repo-relative prefixes an external editor was
    asked to work in. Changes outside it are REPORTED, never suppressed: they
    are still real analysis inputs, and filtering them out would reintroduce
    the narrow-glob blind spot from the other direction.
    """
    added, modified, deleted = [], [], []
    for path, now in after.items():
        was = before.get(path)
        if was is None or not was.get("exists"):
            added.append(path)
        elif now.get("exists") and now.get("sha256") != was.get("sha256"):
            modified.append(path)
    for path, was in before.items():
        if was.get("exists") and not after.get(path, {}).get("exists"):
            deleted.append(path)
    outside = []
    if allowlist:
        root = Path(repo).resolve()
        for path in sorted(added + modified + deleted):
            try:
                rel = str(Path(path).resolve().relative_to(root))
            except ValueError:
                rel = str(path)
            if not any(rel == a or rel.startswith(a.rstrip("/") + "/") for a in allowlist):
                outside.append(rel)
    return {
        "added": sorted(added),
        "modified": sorted(modified),
        "deleted": sorted(deleted),
        "changed_count": len(added) + len(modified) + len(deleted),
        "outside_allowlist": outside,
    }


def first_cold_equal(frames: list, cold_hash: str, context: str, last_edit_ns: int) -> dict:
    """Strict final convergence: last observed edit -> first FINAL-cold-equal completion.

    "The first frame whose hash matches" is not enough by itself: a generation
    can match the final hash and then be followed by a differing one. The frame
    reported here starts the unbroken FINAL run of cold-equal generations, so
    every later generation matches too. It is stronger than the save-relative
    intervals -- it does not depend on a Begin timestamp being at or after a
    save, so it survives the fact that a Begin does not bind the bytes a run
    captured -- but it is still an OBSERVED matching suffix, not a causal claim
    and not a guarantee about later generations. An edit that does not change
    the normalized graph (a comment, a reformat, a no-op rename) leaves an
    EARLIER generation equal to the final hash, which extends the matching
    suffix backwards and makes convergence look faster than the work actually
    was. Read it as "from this generation onward the graph already matched",
    never as "this generation was caused by the last edit".
    """
    in_ctx = [f for f in frames if f.get("context") in (None, context)]
    idx = None
    for i in range(len(in_ctx) - 1, -1, -1):
        if in_ctx[i].get("normalized_hash") == cold_hash:
            idx = i
        else:
            break
    if idx is None:
        return {
            "converged": False,
            "reason": "no completed generation equals the final cold hash",
            "final_convergence_ms": None,
        }
    frame = in_ctx[idx]
    applied_ns = int(frame.get("consumer_end_ns") or 0)
    ms = None
    if last_edit_ns and applied_ns:
        ms = round((applied_ns - int(last_edit_ns)) / 1e6, 3)
    return {
        "converged": True,
        "first_cold_equal_generation": frame.get("target_generation"),
        "first_cold_equal_run_id": frame.get("run_id"),
        "first_cold_equal_consumer_ns": applied_ns,
        "last_observed_edit_ns": int(last_edit_ns) if last_edit_ns else None,
        "final_convergence_ms": ms,
        "cold_equal_generations_at_tail": len(in_ctx) - idx,
        "basis": "last observed edit -> start of the observed final matching "
                 "suffix: the first completed generation that equals the final "
                 "cold graph and is never contradicted afterwards",
        "is_causal_claim": False,
        "basis_limitation": "observed matching suffix only; a graph-neutral "
                            "edit can make an earlier generation match, which "
                            "shortens this number without the work being faster",
    }


def inventory_stable_across_cold(repo: Path, frozen: dict) -> dict:
    """Re-inventory the WHOLE checkout after cold analyze.

    Cold equality compares a watch generation against a graph the cold run
    builds by reading the tree *after* that generation was selected. If any
    input moved in between, the two graphs were built from different bytes and
    equality (or inequality) proves nothing about the incremental path. Freezing
    the inventory before the wait is not enough on its own -- the same whole
    inventory has to still hold once cold has read the tree.
    """
    after = input_state(repo)
    diff = classify_input_changes(frozen, after, repo)
    return {
        "stable": diff["changed_count"] == 0,
        "files_before_cold": len(frozen),
        "files_after_cold": len(after),
        "diff": diff,
        "scope": "whole-isolated-checkout",
        "note": "if unstable, the watch and cold graphs read different inputs "
                "and the equality result is inconclusive, not a verdict",
    }


def assert_isolated_inputs(repo: Path, work: Path) -> None:
    """Only this harness may write the watched tree.

    A stable-input claim is meaningless if some other process can edit the
    checkout, so the watched tree must be our own private copy under --work.
    """
    repo, work = Path(repo).resolve(), Path(work).resolve()
    if repo != work and work not in repo.parents:
        raise RuntimeError(
            f"watched tree {repo} is not isolated under the work dir {work}; "
            "refusing to make stable-input claims about a shared tree"
        )


def evaluate_quiescence(
    frames: list,
    lifecycle: list,
    inputs_expected: dict,
    inputs_now: dict,
    last_save_ns: int,
    context: str,
    quiet_elapsed: bool,
    lifecycle_available: bool,
    restart_ns: int = 0,
) -> dict:
    """Decide whether the watch looks settled. Pure function, so it is testable.

    This reports HEURISTIC quiescence. It never asserts the watcher's internal
    queue is drained -- see QUIESCENCE_LIMITS. It refuses to report ready while
    any blocking condition is observable, so a late analysis cannot be mistaken
    for an idle watcher.
    """
    in_ctx = [f for f in frames if f.get("context") in (None, context)]
    pending = open_begins(lifecycle, context) if lifecycle_available else []
    split = (classify_begins(lifecycle, context, restart_ns) if lifecycle_available
             else {"active": None, "abandoned": []})
    active = split["active"]
    after_save = [f for f in in_ctx if int(f.get("first_ns") or 0) >= last_save_ns]
    state = {
        "ready": False,
        "reason": "",
        "selected": None,
        "context_frames": len(in_ctx),
        # Every observed open Begin for this exact context, whether or not it
        # still blocks: tracking them is the point, gating is separate.
        "incomplete_begin_end_pairs": [
            {"run_id": r.get("run_id"), "context": r.get("context"),
             "target_generation": r.get("target_generation")}
            for r in pending
        ],
        # Only the head can still be running; superseded or pre-restart Begins
        # are surfaced here instead of blocking forever.
        "active_begin": (
            {"run_id": active.get("run_id"), "context": active.get("context"),
             "target_generation": active.get("target_generation")}
            if active else None
        ),
        "abandoned_begins": split["abandoned"],
        # Liveness only: a Begin published after the save does NOT prove that
        # run captured the saved bytes.
        "generations_begun_after_last_save": [f.get("target_generation") for f in after_save],
        "begin_after_save_is_not_capture_proof": True,
    }
    if inputs_now != inputs_expected:
        state["reason"] = "input-unstable"
        return state
    if active is not None:
        state["reason"] = "incomplete-begin-end-pair"
        return state
    if not in_ctx:
        state["reason"] = "no-completed-generation"
        return state
    if not after_save:
        state["reason"] = "no-generation-after-last-save"
        return state
    if not quiet_elapsed:
        state["reason"] = "activity-within-quiet-margin"
        return state
    # The FINAL completed generation for this context, reached after the inputs
    # were frozen. Its correctness is established by cold equality, not here.
    state["ready"] = True
    state["reason"] = "quiescent"
    state["selected"] = in_ctx[-1]
    return state


def wait_quiescent(
    jsonl: Path,
    every_s: float,
    last_save_ns: int,
    inputs_expected: dict,
    obs_proc,
    watch_proc,
    stderr_paths,
    observer: Path,
    url: str,
    context: str,
    lifecycle_path: Path | None = None,
    lifecycle_available: bool = True,
    cap_s: float | None = None,
    repo: Path | None = None,
    restart_ns: int = 0,
) -> dict:
    """Wait for heuristic quiescence, then select the final completed generation.

    NOT a drained proof. `graph watch` uses a FIXED collection window, not a
    debounce: the window opens on the first event and closes `--watch-every`
    later, and edits landing during analysis are queued until that analysis
    finishes. So the first End after an edit can describe a partial batch.

    What is actually checked here:
      1. stable input   - every analysis input still hashes to what we wrote;
      2. no open pair   - no OBSERVED Begin is missing its End (needs the
                          observer lifecycle file; without it this degrades to
                          pure timing and is labelled as such);
      3. quiet margin   - frame count, JetStream last_seq and lifecycle record
                          count all unchanged for the margin.
    Condition 3 is a heuristic: an analysis slower than the margin whose Begin
    is not yet published is unobservable. Acceptance comes from cold equality
    of the selected generation, not from this function.
    """
    quiet = max(every_s * 2, every_s + 2.0)
    cap = time.monotonic() + (cap_s if cap_s is not None else max(every_s * 20, 120))
    activity = None
    stable_until = time.monotonic() + quiet
    state = {"reason": "not-evaluated"}
    while time.monotonic() < cap:
        if obs_proc.poll() is not None:
            raise RuntimeError("observer exited:\n" + stderr_paths["observer"].read_text())
        if watch_proc is not None and watch_proc.poll() is not None:
            raise RuntimeError("graph watch exited:\n" + stderr_paths["watch"].read_text()[-4000:])
        frames = read_frames(jsonl)
        lifecycle = read_lifecycle(lifecycle_path) if lifecycle_path else []
        now_activity = (len(frames), last_seq(observer, url), len(lifecycle))
        if now_activity != activity:
            activity = now_activity
            stable_until = time.monotonic() + quiet
        inputs_now = input_state(repo) if repo is not None else file_state(inputs_expected.keys())
        state = evaluate_quiescence(
            frames, lifecycle, inputs_expected, inputs_now, last_save_ns, context,
            time.monotonic() >= stable_until, lifecycle_available, restart_ns,
        )
        if state["reason"] == "input-unstable":
            raise RuntimeError(
                "analysis input changed under the harness; inputs were supposed "
                "to be frozen before this wait"
            )
        if state["ready"]:
            selected = state["selected"]
            return {
                "frames": frames,
                "selected": selected,
                "selected_index": frames.index(selected),
                "selection_basis": "heuristic-quiescence + observed cold equality",
                "internal_drain_proved": False,
                "requires_watcher_watermark_for_proof": True,
                "quiet_margin_s": quiet,
                "open_begin_tracking": "observed" if lifecycle_available else "unavailable",
                "incomplete_begin_end_pairs_at_selection": state["incomplete_begin_end_pairs"],
                "abandoned_begins": state["abandoned_begins"],
                "generations_begun_after_last_save": state["generations_begun_after_last_save"],
                "selected_generation": selected.get("target_generation"),
                "last_seq": activity[1],
                "stable_inputs": inputs_expected,
                "limitations": list(QUIESCENCE_LIMITS),
            }
        time.sleep(0.05)
    final = read_lifecycle(lifecycle_path) if lifecycle_path else []
    split = classify_begins(final, context, restart_ns)
    raise RuntimeError(
        f"watch did not reach quiescence within cap; last reason={state.get('reason')!r} "
        f"frames={len(read_frames(jsonl))} "
        f"active_begin={split['active'].get('run_id') if split['active'] else None} "
        f"abandoned_begins={[r['run_id'] for r in split['abandoned']]}"
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


def assert_scope_config(scope_text: str) -> None:
    """A scope overlay may narrow inputs; it must never repoint the repository."""
    for line in scope_text.splitlines():
        if line.startswith("#") or not line.strip():
            continue
        if line[:1] not in " \t" and line.split(":")[0].strip() == "repo":
            raise RuntimeError("scope-config must not set repository")


def check_product(args) -> int:
    """Validate the Product invocation without running a heavyweight benchmark."""
    source = Path(args.source)
    scope_config = Path(args.scope_config)
    problems = []
    if not source.is_dir():
        problems.append(f"missing Product source {source}")
    if not scope_config.is_file():
        problems.append(f"missing scope config {scope_config}")
    else:
        try:
            assert_scope_config(scope_config.read_text())
        except RuntimeError as exc:
            problems.append(str(exc))
    edit_path = source / PASSWORD
    mutated = original = ""
    if not edit_path.is_file():
        problems.append(f"missing edit anchor {edit_path}")
    else:
        original = edit_path.read_text()
        mutated = original.replace(
            "return email.trim().toLowerCase();",
            "return email.normalize('NFKC').trim().toLowerCase();",
        )
        if mutated == original:
            problems.append(f"edit anchor snippet not found in {edit_path}")
    pins = {"snapshot": args.snapshot, "binary": args.binary, "observer": args.observer}
    if not args.snapshot and not args.binary:
        problems.append("pass --snapshot or --binary to pin the build under test")
    out = {
        "check": "product-invocation",
        "executed_product_benchmark": False,
        "source": str(source),
        "scope_config": str(scope_config),
        "scope_overlay_sets_repo": False,
        "edit_anchor": str(edit_path),
        "edit_anchor_applies": bool(mutated and mutated != original),
        "watch_every": args.watch_every,
        "repo_id": REPO_ID,
        "pins": pins,
        "isolation": "clone into <work>/product-live; source is never written",
        "problems": problems,
    }
    print(json.dumps(out, indent=2))
    if problems:
        print("product invocation NOT ready", file=sys.stderr)
        return 1
    print("product invocation ready (not executed)")
    return 0


def overlay_scope(repo: Path, source: Path, scope_config: Path) -> None:
    src_cfg = source / "mcp-arch.yaml"
    if src_cfg.is_file():
        (repo / "mcp-arch.yaml").write_bytes(src_cfg.read_bytes())
    scope_text = scope_config.read_text()
    assert_scope_config(scope_text)
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
        raise SystemExit(f"refusing to delete existing isolated copy {dest}; pass a fresh --work")
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
            burst_texts = [
                "export const a = 1;\nexport const b = 2;\n",
                "export const a = 1;\nexport const b = 2;\nexport function c() { return b; }\n",
                "export const a = 1;\nexport const b = 2;\nexport function d() { return b; }\n",
            ]
            fixture = "tiny"

        # Stable-input claims require that nothing else can write this tree.
        assert_isolated_inputs(repo, work)

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
            WATCH_CTX,
            "--repo-id",
            REPO_ID,
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
        # The initial generation must have left a Begin and an End record. If it
        # did not, this observer cannot report in-flight generations and the
        # quiescence check would silently degrade to pure timing.
        lifecycle_available = bool(read_lifecycle(lifecycle_path))
        if not lifecycle_available and not args.allow_legacy_observer:
            raise RuntimeError(
                "observer wrote no Begin/End lifecycle records: open generations "
                "would be unobservable and quiescence would be timing-only. "
                "Omit --observer so the harness rebuilds it, or pass "
                "--allow-legacy-observer to accept the weaker, labelled check."
            )

        time.sleep(max(every_s * 2, 0.5))
        if watch_proc.poll() is not None:
            raise RuntimeError("graph watch exited during idle:\n" + watch_err_path.read_text()[-4000:])
        idle_seq = last_seq(observer_bin, url)
        idle_frames = read_frames(jsonl)
        if idle_seq != seq_after_initial or len(idle_frames) != 1:
            raise RuntimeError(f"idle published events: seq {seq_after_initial}->{idle_seq} frames {len(idle_frames)}")

        dup = write_durable(edit_path, original)
        edits.append({"kind": "duplicate", "path": str(edit_path), **dup})
        time.sleep(max(every_s * 2, 0.5))
        dup_seq = last_seq(observer_bin, url)
        dup_frames = read_frames(jsonl)
        if dup_seq != seq_after_initial or len(dup_frames) != 1:
            raise RuntimeError(f"duplicate published events: seq {seq_after_initial}->{dup_seq} frames {len(dup_frames)}")

        burst_started = time.time_ns()
        for text in burst_texts:
            edits.append({"kind": "burst", "path": str(edit_path), **write_durable(edit_path, text)})
            time.sleep(0.05)
        burst_timeout = max(every_s + 15, 20)
        wait_frames(jsonl, 2, burst_timeout, obs_proc, watch_proc, stderr_paths)
        # Do NOT take the first End after the burst: it can be a partial batch
        # with another generation still queued. Inputs are frozen here, then we
        # wait for heuristic quiescence and take the FINAL completed generation.
        # "ns" is the durable write completion, not the pre-write instant.
        last_save_ns = max(e["ns"] for e in edits if e["kind"] == "burst")
        frozen_inputs = input_state(repo)
        settled = wait_quiescent(
            jsonl, every_s, last_save_ns, frozen_inputs,
            obs_proc, watch_proc, stderr_paths, observer_bin, url,
            context=WATCH_CTX, lifecycle_path=lifecycle_path,
            lifecycle_available=lifecycle_available, repo=repo,
        )
        frames = settled["frames"]
        burst_frames = frames[1:]
        for i, fr in enumerate(burst_frames):
            assert_frozen_begin(fr, f"watch-burst-{i}")
        completed = [fr.get("target_generation") for fr in frames]
        if any(g in (None, 0) for g in completed):
            raise RuntimeError(f"missing completed generations {completed}")
        final = settled["selected"]
        final_hash = final["normalized_hash"]
        initial_hash = initial["normalized_hash"]
        # Cold equality only proves something if the burst actually moved the
        # graph. A literal-only edit leaves the normalized graph identical and
        # would make the comparison pass even if the delta were dropped.
        mutation_changed_graph = final_hash != initial_hash
        if not mutation_changed_graph:
            msg = (
                "burst did not change the normalized graph "
                f"({initial_hash[:12]}); cold equality would be vacuous"
            )
            if fixture == "tiny":
                raise RuntimeError(msg)
            print("WARNING:", msg, file=sys.stderr)

        cold_ctx = COLD_CTX
        # Target the exact cold context/run. Waiting on a total frame count
        # would also be satisfied by an unrelated context publishing.
        seen_runs = {fr.get("run_id") for fr in read_frames(jsonl)}
        # REPO_ID must match the watch run: facts are tagged with the repo id, so
        # a different one changes node identity and cold equality can never hold.
        cold = graph_analyze(
            enola_bin, repo, cfg, work / "state-cold", url, cold_ctx, REPO_ID
        )
        cold_frame = wait_context_frame(
            jsonl, cold_ctx, seen_runs, 40 if fixture != "product" else 120,
            obs_proc, watch_proc, stderr_paths,
        )
        cold_stability = inventory_stable_across_cold(repo, frozen_inputs)
        if not cold_stability["stable"]:
            raise RuntimeError(
                "inputs moved while cold analyze ran; equality is inconclusive: "
                f"{cold_stability['diff']}"
            )
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
            "selected_generation": final.get("target_generation"),
            "quiescence_limitations": settled["limitations"],
            "input_hash_scope": "whole-isolated-checkout",
            "input_stability_across_cold": cold_stability,
            "input_scope_excluded_dirs": sorted(IGNORED_INPUT_DIRS),
            "input_files_hashed": len(frozen_inputs),
            "inputs_isolated_under_work": True,
            "save_observation_basis": "fsync-completion",
            "save_observation_basis_note": SAVE_OBSERVATION_BASIS["fsync-completion"],
            "fsync_visibility_note": VISIBILITY_NOTE,
            "clock_basis": CLOCK_BASIS,
            "clock_assumption": CLOCK_ASSUMPTION,
            "causality_note": CAUSALITY_NOTE,
            "window_semantics": "fixed collection window, not debounce",
            "initial_hash": initial_hash,
            "mutation_changed_graph": mutation_changed_graph,
            "watch_hash": final_hash,
            "cold_hash": cold_frame["normalized_hash"],
            "cold_context": cold_ctx,
            "cold_run_id": cold_frame.get("run_id"),
            "convergence": first_cold_equal(
                frames, cold_frame["normalized_hash"], WATCH_CTX, last_save_ns),
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


def _frame(gen, begin, ctx="watch-bench", run=None, hsh=None):
    """Synthetic completed-generation frame, shaped like an observer End frame."""
    return {
        "context": ctx,
        "run_id": run or f"run-{gen}",
        "first_ns": begin,
        "first_batch_ns": begin + 10,
        "broker_end_ns": begin + 20,
        "consumer_end_ns": begin + 30,
        "target_generation": gen,
        "base_generation": gen - 1,
        "normalized_hash": hsh or f"hash-{gen}",
        "scope_mode": "complete",
        "owner_scope_count": 3,
        "schema_version": "enola.graph.v2",
    }


def _begin(run, ctx="watch-bench", gen=2, broker_ns=None):
    return {"record": "begin", "run_id": run, "context": ctx,
            "target_generation": gen, "broker_ns": broker_ns if broker_ns is not None else gen * 1000}


def _end(run, ctx="watch-bench", gen=2):
    return {"record": "end", "run_id": run, "context": ctx, "target_generation": gen}


class _AliveProc:
    def poll(self):
        return None


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
    stale_cold_id = "watch-bench" + "-cold"
    check("single repo id for watch and cold", 'REPO_ID = "watch-bench"' in text and stale_cold_id not in text)
    check("cold reuses REPO_ID", "url, cold_ctx, REPO_ID" in text)
    check("mutation must move the graph", "mutation_changed_graph" in text)
    check("tiny burst changes exported symbols", "export function c()" in text)
    check("product check does not execute", "executed_product_benchmark" in text)
    check("fixed window documented", "not a debounce" in text)

    # --- claims must be heuristic, never an internal drain proof -------------
    proof_word = "drain" + "ed"
    check("no drained-proof claim in reports",
          f'"{proof_word}"' not in text and "internal_drain_proved" in text)
    check("selection basis is heuristic",
          "heuristic-quiescence + observed cold equality" in text)
    check("watermark named as the missing proof",
          "requires_watcher_watermark_for_proof" in text and "watermark" in " ".join(QUIESCENCE_LIMITS))
    check("limits state pre-Begin work is unobservable",
          any("pre-Begin" in l for l in QUIESCENCE_LIMITS))
    check("limits state Begin does not bind capture",
          any("captured the input" in l for l in QUIESCENCE_LIMITS))
    check("clock domains separated",
          "[CROSS]" in str(CLOCK_BASIS) and "[single]" in str(CLOCK_BASIS))
    check("cross-clock save->begin",
          "CROSS" in CLOCK_BASIS["time_since_latest_observed_save_at_begin_ms"])
    check("single-clock broker interval", "single" in CLOCK_BASIS["begin_to_end_ms"])

    # --- save-relative latency is labelled non-causal ------------------------
    causal_name = "time_since_latest_observed" + "_save_at_begin_ms"
    check("save-relative latency renamed, not called save->begin",
          causal_name in CLOCK_BASIS and "save_to_begin_ms" not in CLOCK_BASIS)
    check("nearest-prior-save named as correlation",
          "correlation, not causation" in CAUSALITY_NOTE)
    check("causal measure is convergence", "final_convergence_ms" in CAUSALITY_NOTE)
    check("observation bases kept apart",
          set(SAVE_OBSERVATION_BASIS) == {"fsync-completion", "filesystem-mtime"})
    check("external editor basis is mtime, not durability",
          "no durability guarantee" in SAVE_OBSERVATION_BASIS["filesystem-mtime"])
    check("fsync is not claimed as a visibility barrier",
          "not a visibility guarantee" in VISIBILITY_NOTE
          and "before fsync returns" in VISIBILITY_NOTE)
    check("negative offsets are kept", "never clamped" in VISIBILITY_NOTE)
    # Built at runtime: a literal here would be its own counterexample.
    false_claim = "cannot observe " + "bytes"
    check("write_durable docstring makes no visibility claim",
          false_claim not in text)

    # --- write timing: durable completion, not the pre-write instant --------
    with tempfile.TemporaryDirectory() as tmp:
        big = Path(tmp) / "big.ts"
        rec = write_durable(big, "x" * (4 << 20))
        check("write_durable returns start and completion",
              set(rec) == {"start_ns", "ns"}, str(rec.keys()))
        check("completion is after the write starts", rec["ns"] > rec["start_ns"],
              f"{rec['start_ns']} -> {rec['ns']}")
        check("write_durable content", big.stat().st_size == (4 << 20))
        check("save latency uses completion", 'e["ns"] for e in edits' in text)

    # --- all analysis inputs hashed, not just the edited path ---------------
    with tempfile.TemporaryDirectory() as tmp:
        repo = Path(tmp) / "repo"
        (repo / "src").mkdir(parents=True)
        (repo / "a.ts").write_text("export const a = 1;\n")
        (repo / "src" / "b.ts").write_text("export const b = 2;\n")
        (repo / "package.json").write_text("{}\n")
        (repo / "node_modules").mkdir()
        (repo / "node_modules" / "c.ts").write_text("export const c = 3;\n")
        # Every extension the old glob dropped on the floor.
        for name in ("README.md", "build.py", "main.tf", "App.swift", "x.mts",
                     "y.cts", "C.vue", "D.svelte", ".gitignore", ".enolarc"):
            (repo / name).write_text("content of " + name + "\n")
        names = {p.name for p in checkout_inventory(repo)}
        check("hashes nested inputs", {"a.ts", "b.ts", "package.json"} <= names, str(names))
        check("skips node_modules", "c.ts" not in names, str(names))
        missed_by_old_glob = {"README.md", "build.py", "main.tf", "App.swift",
                              "x.mts", "y.cts", "C.vue", "D.svelte",
                              ".gitignore", ".enolarc"}
        check("inventory covers what an extension glob missed",
              missed_by_old_glob <= names, str(sorted(missed_by_old_glob - names)))
        # The docstring quotes the retired glob as an example, so count rather
        # than merely look: more than that one mention means it is still in use.
        old_glob = "**/*" + ".ts"
        check("scope is the whole checkout, not a glob",
              '"whole-isolated-checkout"' in text and text.count(old_glob) <= 2,
              f"{old_glob} appears {text.count(old_glob)}x")
        # A file type the old glob ignored must still break stable input.
        md_before = input_state(repo)
        (repo / "README.md").write_text("edited\n")
        check("markdown edit breaks stable input", input_state(repo) != md_before)
        (repo / "README.md").write_text("content of README.md\n")
        before = input_state(repo)
        (repo / "src" / "d.ts").write_text("export const d = 4;\n")
        check("created file breaks stable input", input_state(repo) != before)
        after_delete = dict(before)
        (repo / "src" / "b.ts").unlink()
        check("deleted file breaks stable input", input_state(repo) != after_delete)
        work = Path(tmp)
        ok = True
        try:
            assert_isolated_inputs(repo, work)
        except RuntimeError:
            ok = False
        check("isolated tree accepted", ok)
        caught = False
        try:
            assert_isolated_inputs(Path("/etc"), work)
        except RuntimeError:
            caught = True
        check("shared tree rejected", caught)

    # --- synthetic quiescence regressions -----------------------------------
    frozen = {"a.ts": {"exists": True, "sha256": "aa", "mtime_ns": 1, "size": 1}}
    moved = {"a.ts": {"exists": True, "sha256": "bb", "mtime_ns": 2, "size": 1}}
    save = 1_000
    gen2 = _frame(2, save + 100)

    def ev(frames, lifecycle, now=frozen, quiet=True, avail=True, last=save):
        return evaluate_quiescence(frames, lifecycle, frozen, now, last,
                                   "watch-bench", quiet, avail)

    st = ev([gen2], [_begin("run-2"), _end("run-2")])
    check("clean case is quiescent", st["ready"] and st["selected"]["target_generation"] == 2,
          st["reason"])

    # An analysis still running past the quiet margin must never read as settled.
    st = ev([gen2], [_begin("run-2"), _end("run-2"), _begin("run-3", gen=3)], quiet=True)
    check("open Begin blocks selection", not st["ready"], st["reason"])
    check("open Begin reason", st["reason"] == "incomplete-begin-end-pair", st["reason"])
    check("open pair reported", [r["run_id"] for r in st["incomplete_begin_end_pairs"]] == ["run-3"],
          str(st["incomplete_begin_end_pairs"]))

    # Delayed End: the same shape, arriving after the margin has already passed.
    st = ev([gen2], [_begin("run-3", gen=3)], quiet=True)
    check("delayed End blocks selection", not st["ready"] and st["selected"] is None, st["reason"])

    # Queued second generation: once it completes, the FINAL one is selected.
    gen3 = _frame(3, save + 200)
    st = ev([gen2, gen3], [_begin("run-2"), _end("run-2"), _begin("run-3", gen=3), _end("run-3", gen=3)])
    check("queued generation wins over first End",
          st["ready"] and st["selected"]["target_generation"] == 3, str(st))

    # Unrelated context traffic must neither block us nor be selected.
    other = _frame(9, save + 300, ctx="someone-else", run="other-run")
    st = ev([gen2, other], [_begin("run-2"), _end("run-2"),
                            _begin("other-open", ctx="someone-else", gen=9)])
    check("other context open Begin does not block", st["ready"], st["reason"])
    check("other context frame not selected",
          st["selected"]["context"] == "watch-bench", str(st["selected"]))
    check("context frame count excludes others", st["context_frames"] == 1, str(st))

    # Edit landing during our write: inputs are no longer what we froze.
    st = ev([gen2], [_begin("run-2"), _end("run-2")], now=moved)
    check("edit during write blocks selection", not st["ready"], st["reason"])
    check("edit during write reason", st["reason"] == "input-unstable", st["reason"])

    # Capture-before-Begin: a Begin published after the save may still have
    # scanned pre-save bytes. Liveness only, never a capture proof.
    st = ev([gen2], [_begin("run-2"), _end("run-2")])
    check("begin-after-save is liveness not proof", st["begin_after_save_is_not_capture_proof"])
    check("generations after save listed", st["generations_begun_after_last_save"] == [2], str(st))
    st = ev([_frame(2, save - 500)], [_begin("run-2"), _end("run-2")])
    check("no generation after save blocks selection",
          not st["ready"] and st["reason"] == "no-generation-after-last-save", st["reason"])

    # Quiet margin not yet elapsed.
    st = ev([gen2], [_begin("run-2"), _end("run-2")], quiet=False)
    check("activity within margin blocks selection",
          not st["ready"] and st["reason"] == "activity-within-quiet-margin", st["reason"])

    # Legacy observer: open pairs unobservable, so the check is timing-only.
    st = ev([gen2], [_begin("run-3", gen=3)], avail=False)
    check("legacy observer degrades to timing", st["ready"], st["reason"])
    check("legacy observer reports no observed pairs", st["incomplete_begin_end_pairs"] == [])
    check("legacy path must be opt-in", "--allow-legacy-observer" in text)

    check("open_begins pairs by run", [r["run_id"] for r in open_begins(
        [_begin("a"), _end("a"), _begin("b")])] == ["b"])

    # --- aborted Begins must not block forever ------------------------------
    # A watcher killed mid-publication leaves a Begin that will never End. The
    # next valid run publishes a later Begin, which supersedes it.
    aborted = [_begin("run-9", gen=9, broker_ns=9_000),
               _begin("run-10", gen=10, broker_ns=10_000), _end("run-10", gen=10)]
    split = classify_begins(aborted, "watch-bench")
    check("superseded Begin is not active", split["active"] is None, str(split))
    check("superseded Begin is recorded as abandoned",
          [r["run_id"] for r in split["abandoned"]] == ["run-9"], str(split["abandoned"]))
    check("abandoned Begin carries a reason",
          split["abandoned"][0]["abandoned_reason"] == "superseded-by-later-begin")
    st = ev([_frame(10, save + 50, run="run-10")], aborted)
    check("aborted historical Begin does not block a completed retry",
          st["ready"] and st["selected"]["target_generation"] == 10, st["reason"])
    check("aborted Begin still reported at selection",
          [r["run_id"] for r in st["abandoned_begins"]] == ["run-9"],
          str(st["abandoned_begins"]))
    check("aborted Begin is not silently dropped from tracking",
          "run-9" in [r["run_id"] for r in st["incomplete_begin_end_pairs"]],
          str(st["incomplete_begin_end_pairs"]))

    # The newest open Begin is the live head and MUST still block.
    live = [_begin("run-9", gen=9, broker_ns=9_000), _end("run-9", gen=9),
            _begin("run-11", gen=11, broker_ns=11_000)]
    split = classify_begins(live, "watch-bench")
    check("latest open Begin is the active head", split["active"]["run_id"] == "run-11")
    check("no false abandonment of the head", split["abandoned"] == [], str(split))
    st = ev([_frame(9, save + 10, run="run-9")], live)
    check("active head still blocks", not st["ready"] and st["reason"] == "incomplete-begin-end-pair",
          st["reason"])

    # A Begin published before the watcher restarted belongs to a dead process.
    stale = [_begin("run-5", gen=5, broker_ns=5_000)]
    split = classify_begins(stale, "watch-bench", restart_ns=6_000)
    check("pre-restart Begin is abandoned, not active",
          split["active"] is None
          and split["abandoned"][0]["abandoned_reason"] == "predates-watcher-restart",
          str(split))
    check("same Begin is active without a restart",
          classify_begins(stale, "watch-bench")["active"]["run_id"] == "run-5")

    # --- editor allowlist reports, never suppresses -------------------------
    with tempfile.TemporaryDirectory() as tmp:
        repo = Path(tmp) / "repo"
        (repo / "src" / "deeplinks").mkdir(parents=True)
        (repo / "src" / "deeplinks" / "parse.ts").write_text("export const a = 1;\n")
        (repo / "src" / "other.ts").write_text("export const b = 2;\n")
        (repo / "keep.ts").write_text("export const k = 3;\n")
        before = input_state(repo)
        (repo / "src" / "deeplinks" / "parse.ts").write_text("export const a = 9;\n")
        (repo / "src" / "deeplinks" / "invite.ts").write_text("export const i = 1;\n")
        (repo / "src" / "other.ts").write_text("export const b = 9;\n")
        (repo / "keep.ts").unlink()
        diff = classify_input_changes(before, input_state(repo), repo,
                                      allowlist=["src/deeplinks"])
        check("added file detected", any(p.endswith("invite.ts") for p in diff["added"]),
              str(diff["added"]))
        check("modified file detected", any(p.endswith("parse.ts") for p in diff["modified"]),
              str(diff["modified"]))
        check("deleted file detected", any(p.endswith("keep.ts") for p in diff["deleted"]),
              str(diff["deleted"]))
        check("changes outside the allowlist are surfaced",
              sorted(diff["outside_allowlist"]) == ["keep.ts", "src/other.ts"],
              str(diff["outside_allowlist"]))
        check("allowlisted changes are still counted", diff["changed_count"] == 4, str(diff))

    # --- strict convergence: last edit -> first FINAL cold-equal completion --
    cold = "hash-final"
    conv_frames = [
        _frame(1, 1_000, run="r1", hsh="hash-a"),
        # matches by coincidence, then is contradicted: not convergence
        _frame(2, 2_000, run="r2", hsh=cold),
        _frame(3, 3_000, run="r3", hsh="hash-b"),
        _frame(4, 4_000, run="r4", hsh=cold),
        _frame(5, 5_000, run="r5", hsh=cold),
    ]
    conv = first_cold_equal(conv_frames, cold, "watch-bench", last_edit_ns=1_000)
    check("convergence skips a contradicted early match",
          conv["converged"] and conv["first_cold_equal_generation"] == 4, str(conv))
    check("convergence counts the stable tail", conv["cold_equal_generations_at_tail"] == 2)
    check("convergence measured to consumer apply",
          conv["final_convergence_ms"] == round((4_030 - 1_000) / 1e6, 3), str(conv))
    check("convergence reports run id", conv["first_cold_equal_run_id"] == "r4")
    check("no convergence when nothing equals cold",
          first_cold_equal(conv_frames, "hash-missing", "watch-bench", 1)["converged"] is False)
    check("convergence ignores other contexts",
          first_cold_equal(conv_frames + [_frame(6, 6_000, ctx="other", hsh="hash-z")],
                           cold, "watch-bench", 1_000)["first_cold_equal_generation"] == 4)
    check("convergence is not a Begin>=save proof",
          "depend on a Begin timestamp" in (first_cold_equal.__doc__ or ""))
    check("convergence is labelled observed, not causal",
          conv["is_causal_claim"] is False and "matching suffix" in conv["basis"], str(conv))
    check("convergence records the no-op caveat",
          "graph-neutral" in conv["basis_limitation"], conv["basis_limitation"])
    # A graph-neutral edit leaves an earlier generation already equal, so the
    # matching suffix starts earlier than the work did. The number must not be
    # read as "the last edit caused this generation".
    noop_frames = [_frame(1, 1_500, hsh=cold), _frame(2, 2_500, hsh=cold)]
    noop = first_cold_equal(noop_frames, cold, "watch-bench",
                            last_edit_ns=1_000_000_000)
    check("a graph-neutral edit shortens convergence below the causal interval",
          noop["first_cold_equal_generation"] == 1
          and noop["final_convergence_ms"] < -900,
          str(noop))
    check("a negative convergence offset is reported, not clamped",
          noop["final_convergence_ms"] is not None)

    # --- cold equality needs the whole inventory on BOTH sides -------------
    with tempfile.TemporaryDirectory() as tmp:
        repo = Path(tmp) / "repo"
        (repo / "src").mkdir(parents=True)
        (repo / "src" / "a.ts").write_text("export const a = 1;\n")
        (repo / "README.md").write_text("docs\n")
        frozen = input_state(repo)
        check("cold stability check passes on an untouched tree",
              inventory_stable_across_cold(repo, frozen)["stable"] is True)
        (repo / "README.md").write_text("docs edited during cold\n")
        moved = inventory_stable_across_cold(repo, frozen)
        check("a non-source input moving during cold is detected",
              moved["stable"] is False, str(moved))
        check("cold stability re-reads the whole checkout",
              moved["scope"] == "whole-isolated-checkout" and moved["files_after_cold"] == 2,
              str(moved))
        check("watch fails closed when inputs move across cold",
              "inputs moved while cold analyze ran" in text)

    # --- cold wait targets one context/run, not a frame count ---------------
    with tempfile.TemporaryDirectory() as tmp:
        jsonl = Path(tmp) / "consumer.jsonl"
        jsonl.write_text(
            json.dumps(_frame(2, 10)) + "\n"
            + json.dumps(_frame(9, 20, ctx="someone-else", run="noise")) + "\n"
            + json.dumps(_frame(1, 30, ctx="watch-cold", run="cold-1")) + "\n"
        )
        stub = {"observer": Path(tmp) / "o", "watch": Path(tmp) / "w"}
        got = wait_context_frame(jsonl, "watch-cold", {"noise"}, 2,
                                 _AliveProc(), _AliveProc(), stub)
        check("cold wait picks the cold context", got["run_id"] == "cold-1", str(got))
        raised = False
        try:
            wait_context_frame(jsonl, "watch-cold", {"cold-1"}, 0.3,
                               _AliveProc(), _AliveProc(), stub)
        except RuntimeError:
            raised = True
        check("cold wait ignores already-seen runs", raised)
        # Build the forbidden token at runtime: written literally it would be
        # its own counterexample and the check could never pass.
        count_wait = "len(frames)" + " + 1"
        check("cold wait not by frame count", count_wait not in text)

    ok = True
    try:
        assert_scope_config("# c\ngraph_inputs:\n  exclude:\n    - x\n")
    except RuntimeError:
        ok = False
    check("scope overlay accepted", ok)
    caught = False
    try:
        assert_scope_config("graph_inputs: {}\nrepo: /elsewhere\n")
    except RuntimeError:
        caught = True
    check("scope overlay repointing repo rejected", caught)
    check("parse 5s", parse_every("5s") == 5.0)
    check("parse 1s", parse_every("1s") == 1.0)

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
    p.add_argument("--check-product", action="store_true", help="validate Product invocation without running it")
    p.add_argument("--force-tiny", action="store_true")
    p.add_argument("--allow-legacy-observer", action="store_true",
                   help="accept an observer without Begin/End records; quiescence "
                        "then degrades to timing-only and is labelled as such")
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
    if args.check_product:
        if not args.source:
            args.source = "/tmp/enola-product-benchmark-source"
        return check_product(args)
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
