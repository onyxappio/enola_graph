#!/usr/bin/env python3
"""First-parent Product history harness (frozen v2).

Walks the latest N first-parent commits on main, oldest to newest. One initial
analysis at main~N, then persistent delta state through each child. A fresh
cold analysis is the oracle at every commit. Isolated snapshots only; the
tracked Product checkout is never mutated.

Existing docs/results.json is an invalid ordinary-log chronology with a stale
binary. That dump is preserved as results.baseline-invalid-chronology.json.
Live dumps go to --output or $WORK/results.json.

This is diagnostic correctness evidence, not a performance-acceptance run.
"""
from __future__ import annotations

import argparse
import hashlib
import importlib.util
import json
import os
import shutil
import subprocess
import sys
import tempfile
from pathlib import Path


HERE = Path(__file__).resolve().parent
DOCS_BENCH = HERE.parent
ENOLA_ROOT = HERE.parents[2]
SCOPE_RUN = DOCS_BENCH / "invalidation-scope-2026-09-21" / "run.py"
BASELINE_NAME = "results.baseline-invalid-chronology.json"
LOCK_BASENAMES = {
    "package-lock.json",
    "npm-shrinkwrap.json",
    "yarn.lock",
    "pnpm-lock.yaml",
    "bun.lock",
    "bun.lockb",
    "go.sum",
    "Cargo.lock",
    "Gemfile.lock",
    "composer.lock",
    "Pipfile.lock",
    "poetry.lock",
    "uv.lock",
    "pdm.lock",
    "pubspec.lock",
    "packages.lock.json",
    "Package.resolved",
}

spec = importlib.util.spec_from_file_location("scope_harness", SCOPE_RUN)
scope = importlib.util.module_from_spec(spec)
assert spec.loader is not None
spec.loader.exec_module(scope)


def run(cmd, cwd=None, check=True, **kw):
    return subprocess.run(cmd, cwd=cwd, check=check, capture_output=True, text=True, **kw)


def git_out(repo: Path, *args: str) -> str:
    return run(["git", "-C", str(repo), *args]).stdout.strip()


def is_lockfile(path: str) -> bool:
    return Path(path.replace("\\", "/")).name in LOCK_BASENAMES


def file_owner(rel: str) -> str:
    return "file:" + rel.replace("\\", "/")


def sha256_bytes(data: bytes) -> str:
    return hashlib.sha256(data).hexdigest()


def sha256_file(path: Path) -> str:
    h = hashlib.sha256()
    with path.open("rb") as fh:
        for chunk in iter(lambda: fh.read(1024 * 1024), b""):
            h.update(chunk)
    return h.hexdigest()


def first_parent_shas(repo: Path, ref: str, steps: int) -> list[str]:
    """Oldest-first first-parent chain of length steps+1 ending at ref."""
    raw = git_out(repo, "log", "--first-parent", f"-n{steps + 1}", "--format=%H", ref)
    newest_first = [line for line in raw.splitlines() if line.strip()]
    if len(newest_first) < steps + 1:
        raise RuntimeError(f"{ref} has {len(newest_first)} first-parent commits, need {steps + 1}")
    oldest_first = list(reversed(newest_first[: steps + 1]))
    for index, child in enumerate(oldest_first[1:], start=1):
        parent = git_out(repo, "rev-parse", f"{child}^1")
        if parent != oldest_first[index - 1]:
            raise RuntimeError(
                f"first-parent break at {child}: ^1={parent}, chain={oldest_first[index - 1]}"
            )
    return oldest_first


def ordinary_log_shas(repo: Path, ref: str, count: int) -> list[str]:
    raw = git_out(repo, "log", f"-n{count}", "--format=%H", ref)
    return [line for line in raw.splitlines() if line.strip()]


def changed_name_status(repo: Path, parent: str, child: str) -> list[list[str]]:
    out = git_out(repo, "diff", "--name-status", "-M", parent, child)
    rows = []
    for line in out.splitlines():
        if line.strip():
            rows.append(line.split("\t"))
    return rows


def required_owners(name_status: list[list[str]]) -> list[str]:
    owners = []
    for parts in name_status:
        status = parts[0]
        if status.startswith("R") and len(parts) >= 3:
            paths = parts[-2:]
        else:
            paths = parts[-1:]
        for path in paths:
            if not is_lockfile(path):
                owners.append(file_owner(path))
    # stable unique
    seen = set()
    out = []
    for owner in owners:
        if owner not in seen:
            seen.add(owner)
            out.append(owner)
    return out


def overlay_input_policy(snapshot: Path, source: Path) -> str:
    """Layer repository-local Enola config onto a snapshot. Never writes source."""
    src = source / "mcp-arch.yaml"
    dest = snapshot / "mcp-arch.yaml"
    if src.is_file():
        dest.write_bytes(src.read_bytes())
        return str(src)
    fallback = DOCS_BENCH / "product-delta-2026-09-21" / "product-graph-scope.yaml"
    if fallback.is_file() and not dest.exists():
        text = fallback.read_text()
        if not text.startswith("repo:"):
            text = "repo: .\n" + text
        dest.write_text(text)
        return str(fallback)
    return ""


def snapshot_at(fetch: Path, sha: str, dest: Path, source: Path) -> None:
    if dest.exists():
        shutil.rmtree(dest)
    dest.parent.mkdir(parents=True, exist_ok=True)
    run(["git", "clone", "--quiet", "--no-checkout", str(fetch), str(dest)])
    run(["git", "-C", str(dest), "checkout", "--quiet", "--detach", sha])
    overlay_input_policy(dest, source)


def checkout_sha(repo: Path, sha: str, source: Path) -> None:
    run(["git", "-C", str(repo), "checkout", "--quiet", "--detach", sha])
    overlay_input_policy(repo, source)


def prepare_fetch_clone(source: Path, dest: Path, ref: str) -> str:
    """Dedicated clone. Fetch main. Never mutates source."""
    if dest.exists():
        shutil.rmtree(dest)
    dest.parent.mkdir(parents=True, exist_ok=True)
    run(["git", "clone", "--quiet", str(source), str(dest)])
    run(["git", "-C", str(dest), "fetch", "--quiet", "origin", "main"])
    tip = git_out(dest, "rev-parse", ref)
    run(["git", "-C", str(dest), "checkout", "--quiet", "--detach", tip])
    return tip


def enola_identity(enola_root: Path) -> dict:
    rev = git_out(enola_root, "rev-parse", "HEAD")
    porcelain = git_out(enola_root, "status", "--porcelain")
    diff = run(["git", "-C", str(enola_root), "diff", "HEAD"]).stdout.encode()
    cached = run(["git", "-C", str(enola_root), "diff", "--cached"]).stdout.encode()
    return {
        "revision": rev,
        "dirty": bool(porcelain.strip()),
        "status_porcelain": porcelain,
        "diff_sha256": sha256_bytes(diff + b"\0" + cached),
    }


def build_enola(enola_root: Path, dest: Path) -> None:
    dest.parent.mkdir(parents=True, exist_ok=True)
    go = Path("/tmp/enola-toolchain/go/bin/go")
    go_bin = str(go) if go.is_file() else "go"
    env = os.environ.copy()
    extra = "/tmp/enola-toolchain/go/bin:/tmp/enola-toolchain/bin"
    env["PATH"] = extra + ":" + env.get("PATH", "")
    proc = subprocess.run(
        [go_bin, "build", "-o", str(dest), "./cmd/enola"],
        cwd=enola_root,
        env=env,
        capture_output=True,
        text=True,
    )
    if proc.returncode != 0:
        raise RuntimeError(f"go build failed:\n{proc.stderr[-4000:]}")


def validate_publishing_or_noop(records, pairs, info, previous_generation: int, required: list[str]):
    """Strict no-op keeps the previous completed generation; publishers advance by 1."""
    if not records:
        gen = info.get("generation") or []
        if (info.get("parsed") or 0) != 0:
            raise RuntimeError("no-op parsed files")
        if (info.get("events") or 0) != 0:
            raise RuntimeError("no-op published events")
        if (info.get("owners_published") or 0) != 0:
            raise RuntimeError("no-op published owners")
        if len(gen) != 2 or gen[0] != gen[1] or gen[0] != previous_generation:
            raise RuntimeError(f"no-op generation {gen} != previous completed {previous_generation}")
        if required:
            raise RuntimeError(f"semantic owners {required} required a Begin; got no-op")
        return {"kind": "noop", "scope": set(), "base_generation": previous_generation, "target_generation": previous_generation}
    validated = scope.validate_replacement(records, pairs)
    if validated["base_generation"] != previous_generation:
        raise RuntimeError(
            f"delta base {validated['base_generation']} != previous completed {previous_generation}"
        )
    if validated["target_generation"] != previous_generation + 1:
        raise RuntimeError(
            f"delta target {validated['target_generation']} != previous+1 {previous_generation + 1}"
        )
    gen = info.get("generation") or []
    if gen != [validated["base_generation"], validated["target_generation"]]:
        raise RuntimeError(f"summary generation {gen} does not match Begin")
    if not set(required).issubset(validated["scope"]):
        missing = sorted(set(required) - validated["scope"])
        raise RuntimeError(f"required owners outside Begin: {missing[:20]}")
    return validated


def case_id(parent: str, child: str) -> str:
    return f"{parent[:12]}..{child[:12]}"


def _only_match(token: str, index: int, parent: str, child: str) -> bool:
    ident = case_id(parent, child)
    if token.isdigit():
        return str(index) == token
    return ident == token or parent.startswith(token) or child.startswith(token) or ident.startswith(token)


def filter_transitions(transitions: list[tuple[str, str]], only: str) -> list[tuple[str, str]]:
    if not only.strip():
        return transitions
    wanted = [item.strip() for item in only.split(",") if item.strip()]
    out = []
    for index, (parent, child) in enumerate(transitions):
        if any(_only_match(token, index, parent, child) for token in wanted):
            out.append((parent, child))
    if not out:
        raise RuntimeError(f"--only {only!r} matched no first-parent transitions")
    return out


def write_json(path: Path, data) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(data, indent=2) + "\n")


def assert_not_tracked_output(output: Path) -> None:
    tracked = (HERE / "results.json").resolve()
    baseline = (HERE / BASELINE_NAME).resolve()
    resolved = output.resolve()
    if resolved in {tracked, baseline}:
        raise SystemExit(f"refusing to overwrite tracked {resolved}; use --output under --work")


def run_history(args) -> dict:
    source = Path(args.source)
    work = Path(args.work)
    enola_root = Path(args.enola_root)
    if not source.is_dir():
        raise SystemExit(f"missing Product source {source}")
    if source.resolve() == Path("/tmp/enola-product-benchmark-source").resolve():
        # Read-only: dedicated clone only.
        pass
    work.mkdir(parents=True, exist_ok=True)
    assert_not_tracked_output(Path(args.output))

    ident = enola_identity(enola_root)
    binary = Path(args.binary) if args.binary else work / "enola"
    if not args.skip_build:
        build_enola(enola_root, binary)
    elif not binary.is_file():
        raise SystemExit(f"missing enola binary {binary}")
    binary_hash = sha256_file(binary)

    fetch = work / "product-fetch"
    ref = args.ref
    tip = prepare_fetch_clone(source, fetch, ref)
    shas = first_parent_shas(fetch, ref, args.count)
    transitions = list(zip(shas[:-1], shas[1:]))
    transitions = filter_transitions(transitions, args.only)

    provenance = {
        "note": "first-parent history; persistent delta state; cold oracle per commit",
        "source_tree": str(source),
        "source_dirty_untracked": git_out(source, "status", "--porcelain") if (source / ".git").exists() else "",
        "fetch_clone": str(fetch),
        "ref": ref,
        "ref_sha": tip,
        "first_parent_shas": shas,
        "enola": ident,
        "binary": str(binary),
        "binary_sha256": binary_hash,
        "input_policy": "mcp-arch.yaml overlay from source when present; lockfiles excluded from required owners",
        "baseline": str(HERE / BASELINE_NAME),
    }
    write_json(work / "provenance.json", provenance)

    live = work / "live"
    snapshot_at(fetch, shas[0], live, source)
    events = work / "events-live.jsonl"
    if events.exists():
        events.unlink()
    state_live = work / "state-live"
    initial_case = work / "cases" / f"00-initial-{shas[0][:12]}"
    initial_case.mkdir(parents=True, exist_ok=True)
    initial = scope.enola(binary, "analyze", live, state_live, events, initial_case / "summary-initial.json")
    initial_pairs = scope.parse_event_records(events)
    initial_records = [rec for rec, _ in initial_pairs]
    validated_initial = scope.validate_replacement(initial_records, initial_pairs)
    write_json(
        initial_case / "result.json",
        {
            "id": f"00-initial-{shas[0][:12]}",
            "sha": shas[0],
            "initial": scope.slim_run(initial, scope.manifest_fields(initial_records)),
            "begin_scope_count": len(validated_initial["scope"]),
        },
    )
    applied = scope.Consumer()
    applied.apply(initial_records)
    completed = validated_initial["target_generation"]
    rows = []

    for index, (parent, child) in enumerate(transitions):
        ident_case = f"{index:02d}-{case_id(parent, child)}"
        case = work / "cases" / ident_case
        case.mkdir(parents=True, exist_ok=True)
        print(f"== {ident_case} {parent[:12]} -> {child[:12]}", flush=True)
        try:
            changed = changed_name_status(fetch, parent, child)
            required = required_owners(changed)
            write_json(case / "changed.json", {"name_status": changed, "required_owners": required})
            before = events.stat().st_size if events.exists() else 0
            checkout_sha(live, child, source)
            delta = scope.enola(binary, "delta", live, state_live, events, case / "summary-delta.json")
            delta_pairs = scope.parse_event_records(events, before)
            delta_records = [rec for rec, _ in delta_pairs]
            delta["events"] = len(delta_records)
            # Validate the protocol before applying; changed files that produce
            # no Enola facts (for example ignored test fixtures) are filtered
            # against the cold manifest below rather than treated as missing
            # authoritative owners.
            validated = validate_publishing_or_noop(delta_records, delta_pairs, delta, completed, [])
            if delta_records:
                applied.apply(delta_records)
                completed = validated["target_generation"]
            cold_root = case / "cold-src"
            snapshot_at(fetch, child, cold_root, source)
            cold_events = case / "events-cold.jsonl"
            if cold_events.exists():
                cold_events.unlink()
            cold = scope.enola(binary, "analyze", cold_root, case / "state-cold", cold_events, case / "summary-cold.json")
            cold_pairs = scope.parse_event_records(cold_events)
            cold_records = [rec for rec, _ in cold_pairs]
            cold_validated = scope.validate_replacement(cold_records, cold_pairs)
            cold_scope = set(cold_validated.get("scope") or [])
            required = [owner for owner in required if owner in cold_scope]
            if not set(required).issubset(set(validated.get("scope") or [])):
                missing = sorted(set(required) - set(validated.get("scope") or []))
                raise RuntimeError(f"required owners outside Begin: {missing[:20]}")
            expected = scope.Consumer()
            expected.apply(cold_records)
            graph_equal = applied.graph_hash() == expected.graph_hash()
            if not graph_equal:
                raise RuntimeError("initial+deltas graph hash does not match cold")
            begin_scope = sorted(validated.get("scope") or [])
            row = {
                "id": ident_case,
                "index": index,
                "parent": parent,
                "child": child,
                "changed_file_count": len(changed),
                "required_owner_count": len(required),
                "delta": scope.slim_run(delta, scope.manifest_fields(delta_records)),
                "cold": scope.slim_run(cold, scope.manifest_fields(cold_records)),
                "begin_scope_count": len(begin_scope),
                "necessary_owner_count": len(required),
                "graph_hash_equal": True,
                "kind": validated.get("kind") or "delta",
                "completed_generation": completed,
                "blocker": None,
            }
        except Exception as exc:
            row = {
                "id": ident_case,
                "index": index,
                "parent": parent,
                "child": child,
                "blocker": str(exc)[:4000],
                "graph_hash_equal": False,
            }
        write_json(case / "result.json", row)
        rows.append(row)
        print(
            json.dumps(
                {
                    "id": row.get("id"),
                    "changed_file_count": row.get("changed_file_count"),
                    "begin_scope_count": row.get("begin_scope_count"),
                    "graph_hash_equal": row.get("graph_hash_equal"),
                    "blocker": bool(row.get("blocker")),
                }
            ),
            flush=True,
        )

    summary = {
        "provenance": provenance,
        "initial_sha": shas[0],
        "transitions": rows,
        "work": str(work),
        "output": args.output,
        "repeat_limitation": "not a performance-acceptance run; launch the 10-case only when agreed",
    }
    output = Path(args.output)
    write_json(output, summary)
    print("wrote", output)
    failed = [row for row in rows if row.get("blocker") or row.get("graph_hash_equal") is not True]
    if failed:
        print(f"history harness failed: {len(failed)} case(s)", file=sys.stderr)
        return {"summary": summary, "exit": 1}
    return {"summary": summary, "exit": 0}


def init_repo(path: Path) -> None:
    path.mkdir(parents=True, exist_ok=True)
    run(["git", "init", "-q", "-b", "main"], cwd=path)
    run(["git", "config", "user.email", "harness@example.test"], cwd=path)
    run(["git", "config", "user.name", "Harness"], cwd=path)


def commit_file(repo: Path, rel: str, text: str, message: str) -> str:
    dest = repo / rel
    dest.parent.mkdir(parents=True, exist_ok=True)
    dest.write_text(text)
    run(["git", "add", rel], cwd=repo)
    run(["git", "commit", "-q", "-m", message], cwd=repo)
    return git_out(repo, "rev-parse", "HEAD")


def self_test() -> int:
    failures = []

    def check(name: str, cond: bool, detail: str = ""):
        if not cond:
            failures.append(f"{name}: {detail or 'failed'}")

    with tempfile.TemporaryDirectory() as tmp:
        root = Path(tmp)
        mainline = root / "repo"
        init_repo(mainline)
        shas = [commit_file(mainline, "a.txt", "0\n", "base")]
        for i in range(1, 6):
            shas.append(commit_file(mainline, "a.txt", f"{i}\n", f"main {i}"))
        run(["git", "checkout", "-q", "-b", "side", shas[2]], cwd=mainline)
        side = commit_file(mainline, "side.txt", "s\n", "side")
        run(["git", "checkout", "-q", shas[-1]], cwd=mainline)
        run(["git", "merge", "-q", "--no-ff", "-m", "merge side", "side"], cwd=mainline)
        head = git_out(mainline, "rev-parse", "HEAD")
        fp = first_parent_shas(mainline, "HEAD", 2)
        check("first-parent excludes side", side not in fp, f"side={side} chain={fp}")
        check("first-parent ends at HEAD", fp[-1] == head, f"{fp[-1]} vs {head}")
        ordinary = ordinary_log_shas(mainline, "HEAD", 8)
        check("ordinary log includes side", side in ordinary, f"ordinary={ordinary}")

        linear = root / "linear"
        init_repo(linear)
        linear_shas = [commit_file(linear, "f.txt", f"{i}\n", f"c{i}") for i in range(4)]
        chain = first_parent_shas(linear, "HEAD", 3)
        check("linear oldest is first commit", chain[0] == linear_shas[0], f"{chain} vs {linear_shas}")
        check("linear newest is HEAD", chain[-1] == linear_shas[-1])
        trans = list(zip(chain[:-1], chain[1:]))
        only = filter_transitions(trans, "1")
        check("--only index", only == [trans[1]], str(only))
        only_sha = filter_transitions(trans, linear_shas[2][:8])
        check("--only sha", any(linear_shas[2] in pair for pair in only_sha), str(only_sha))
        raised = False
        try:
            filter_transitions(trans, "no-such")
        except RuntimeError:
            raised = True
        check("--only unknown raises", raised)

        rows = [["M", "packages/crypto/src/a.ts"], ["M", "pnpm-lock.yaml"], ["R100", "old.ts", "new.ts"]]
        req = required_owners(rows)
        check("lockfile dropped", "file:pnpm-lock.yaml" not in req, str(req))
        check("rename both ids", "file:old.ts" in req and "file:new.ts" in req, str(req))
        check("source kept", "file:packages/crypto/src/a.ts" in req, str(req))
        check("lock-only required empty", required_owners([["M", "pnpm-lock.yaml"]]) == [])

        info_ok = {"parsed": 0, "events": 0, "owners_published": 0, "generation": [3, 3]}
        try:
            validate_publishing_or_noop([], [], info_ok, 3, [])
            check("noop generation match", True)
        except Exception as exc:
            check("noop generation match", False, str(exc))
        raised = False
        try:
            validate_publishing_or_noop([], [], info_ok, 2, [])
        except RuntimeError:
            raised = True
        check("noop wrong previous", raised)
        raised = False
        try:
            validate_publishing_or_noop([], [], info_ok, 3, ["file:a.ts"])
        except RuntimeError:
            raised = True
        check("noop with required owners", raised)

        src = root / "src"
        src.mkdir()
        (src / "mcp-arch.yaml").write_text("repo: .\ngraph_inputs:\n  exclude: [worker-reports/**]\n")
        snap = root / "snap"
        snap.mkdir()
        overlay_input_policy(snap, src)
        check("overlay copies dirty config", (snap / "mcp-arch.yaml").is_file())
        check("overlay content", "worker-reports/**" in (snap / "mcp-arch.yaml").read_text())

        owners = [{"kind": "file", "id": "b.ts"}, {"kind": "file", "id": "a.ts"}]
        check("digest order-stable", scope.digest_owners(owners) == scope.digest_owners(list(reversed(owners))))
        check("baseline preserved", (HERE / BASELINE_NAME).is_file())

        raised = False
        try:
            assert_not_tracked_output(HERE / "results.json")
        except SystemExit:
            raised = True
        check("refuse tracked results overwrite", raised)
        raised = False
        try:
            assert_not_tracked_output(HERE / BASELINE_NAME)
        except SystemExit:
            raised = True
        check("refuse baseline overwrite", raised)

    if failures:
        print("self-test FAIL", file=sys.stderr)
        for item in failures:
            print(" ", item, file=sys.stderr)
        return 1
    print("self-test ok")
    print("first-parent oldest->newest, lockfile policy, no-op generation, overlay, --only")
    print("baseline", HERE / BASELINE_NAME)
    print("readiness: harness self-test passed; latest full run is recorded in the benchmark README")
    return 0


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--source", default="/tmp/enola-product-benchmark-source")
    parser.add_argument("--work", default="/tmp/enola-invalidation-history-2026-09-22")
    parser.add_argument("--output", default="")
    parser.add_argument("--enola-root", default=str(ENOLA_ROOT))
    parser.add_argument("--binary", default="", help="optional prebuilt binary; default builds from --enola-root")
    parser.add_argument("--skip-build", action="store_true")
    parser.add_argument("--ref", default="origin/main")
    parser.add_argument("--count", type=int, default=10)
    parser.add_argument("--only", default="")
    parser.add_argument("--self-test", action="store_true")
    args = parser.parse_args()
    if args.self_test:
        return self_test()
    if not args.output:
        args.output = str(Path(args.work) / "results.json")
    result = run_history(args)
    return result["exit"]


if __name__ == "__main__":
    sys.exit(main())
