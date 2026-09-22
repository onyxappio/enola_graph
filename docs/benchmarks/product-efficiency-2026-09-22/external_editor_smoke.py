#!/usr/bin/env python3
"""Validate the external-editor protocol end to end, on the tiny fixture.

This stands in for the real parallel AI editor: it waits for READY.json, reads
the isolated repo path out of it, edits that tree from a SEPARATE process, and
signals completion by creating the STOP-EDITOR file. The soak harness itself
never writes the source in this mode, so anything the report shows as an
observed change came from here -- which is exactly the property a real editor
run depends on.

What this checks that a scripted soak cannot:
  - READY.json appears and names an isolated repo path that really exists;
  - changes made by a process the harness did not launch are still captured,
    including a file type the old extension glob would have missed;
  - the finish signal is recorded but does NOT cut the run short;
  - the report carries the mtime observation basis, never a durable-fsync one.

Not a Product run and not acceptance: tiny fixture, short duration.
"""

from __future__ import annotations

import json
import subprocess
import sys
import time
from pathlib import Path

HERE = Path(__file__).resolve().parent


def wait_ready(path: Path, proc: subprocess.Popen, timeout: float) -> dict:
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        if proc.poll() is not None:
            raise RuntimeError(f"soak exited before READY (rc={proc.returncode})")
        if path.is_file() and path.stat().st_size:
            return json.loads(path.read_text())
        time.sleep(0.2)
    raise RuntimeError(f"no READY file at {path} within {timeout}s")


def main() -> int:
    snapshot = sys.argv[1] if len(sys.argv) > 1 else ""
    if not snapshot:
        raise SystemExit("usage: external_editor_smoke.py <snapshot-dir>")
    work = Path(f"/tmp/enola-extedit-{int(time.time())}")
    ready_path = work / "READY.json"

    soak = subprocess.Popen(
        [sys.executable, str(HERE / "soak.py"),
         "--snapshot", snapshot, "--work", str(work),
         "--external-editor", "--force-tiny",
         "--duration", "45s", "--min-duration", "45s",
         "--editor-allowlist", "src",
         "--input-poll", "1s",
         "--no-restart-watch", "--no-restart-broker"],
        stdout=(Path("/tmp") / "extedit-soak.log").open("w"),
        stderr=subprocess.STDOUT, start_new_session=True,
    )
    try:
        ready = wait_ready(ready_path, soak, 180)
        repo = Path(ready["edit_this_repo"])
        print("READY ->", repo)
        if not repo.is_dir():
            raise RuntimeError(f"READY names a repo that does not exist: {repo}")
        if ready["harness_writes_source"]:
            raise RuntimeError("external mode must not write the source")

        # A separate process edits the tree, like the real editor will.
        editor = subprocess.Popen(
            [sys.executable, str(HERE / "soak.py"), "--editor-worker",
             "--repo", str(repo), "--edit-log", str(work / "external-editor.jsonl"),
             "--duration", "20s", "--edit-interval", "4s", "--seed", "31"],
            stdout=(Path("/tmp") / "extedit-editor.log").open("w"),
            stderr=subprocess.STDOUT, start_new_session=True,
        )
        editor.wait(timeout=120)
        # Outside the declared allowlist AND a file type the retired extension
        # glob would never have hashed: the cheap live poll must miss it and the
        # final full inventory must still report it.
        (repo / "DESIGN.md").write_text("deep-link helper notes\n")
        time.sleep(2)

        finished_at = time.monotonic()
        Path(ready["signal_finished_by_creating"]).write_text("done\n")
        print("finish signalled")

        soak.wait(timeout=300)
        ran_on_after_finish = round(time.monotonic() - finished_at, 1)
        if soak.returncode != 0:
            print(Path("/tmp/extedit-soak.log").read_text()[-3000:])
            raise RuntimeError(f"soak failed rc={soak.returncode}")

        report = json.loads((work / "soak.json").read_text())
        act = report["editor_activity"]
        problems = []
        if report["harness_mutated_source"]:
            problems.append("harness reported mutating source in external mode")
        if report["editor_mode"] != "external-editor":
            problems.append(f"mode={report['editor_mode']}")
        if act["save_observation_basis"] != "filesystem-mtime":
            problems.append(f"basis={act['save_observation_basis']}")
        if act["observed_changes"] < 3:
            problems.append(f"only {act['observed_changes']} observed changes")
        if act["editor_finished_signal_ns"] is None:
            problems.append("finish signal not recorded")
        if ran_on_after_finish < 2:
            problems.append("run did not outlast the finish signal")
        observed_raw = (work / "observed_edits.jsonl").read_text()
        if "DESIGN.md" in observed_raw:
            problems.append("live poll read outside its declared scope")
        if "DESIGN.md" not in " ".join(act["changes_outside_allowlist"]):
            problems.append("final full inventory missed the out-of-allowlist file")
        if act["poll_interval_s"] != 1.0:
            problems.append(f"poll interval not recorded: {act.get('poll_interval_s')}")
        if not act["observation_limits"]:
            problems.append("observation limits not recorded")
        if report["extractor_profile"] != "typescript":
            problems.append(f"tiny fixture profile={report['extractor_profile']}")

        print(json.dumps({
            "work": str(work),
            "elapsed_s": report["elapsed_s"],
            "editor_mode": report["editor_mode"],
            "harness_mutated_source": report["harness_mutated_source"],
            "observed_changes": act["observed_changes"],
            "active_editing_s": act["active_editing_s"],
            "idle_s": act["idle_s"],
            "ran_on_after_finish_s": ran_on_after_finish,
            "changes_outside_allowlist": act["changes_outside_allowlist"],
            "live_poll_scope": act["live_poll_scope"],
            "poll_interval_s": act["poll_interval_s"],
            "final_full_inventory_diff": act["final_full_inventory_diff"],
            "generation_count": report["generation_count"],
            "equal": report["equal"],
            "convergence": report["convergence"],
            "extractor_profile": report["extractor_profile"],
        }, indent=2))
        if problems:
            print("external-editor smoke FAIL")
            for p in problems:
                print("  " + p)
            return 1
        print("external-editor smoke ok", work)
        return 0
    finally:
        for proc in (soak,):
            if proc.poll() is None:
                proc.terminate()


if __name__ == "__main__":
    sys.exit(main())
