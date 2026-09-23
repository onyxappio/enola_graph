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
PINNED_POLICY = DOCS_BENCH / "product-delta-2026-09-21" / "product-mcp-arch.yaml"
GO_BIN_CANDIDATES = (Path("/tmp/enola-toolchain/go/bin/go"),)
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


def semantic_required_owners(name_status: list[list[str]], prior_owners, cold_owners) -> list[str]:
    """Keep changed paths with nonempty prior or target contributions.

    Deleted and old-rename owners live on the prior semantic set. New and
    renamed-to owners live on the target graph. Both arguments must be
    nonempty contribution sets, not Begin manifests: a conservative initial
    manifest can include inventory-only files that never contributed facts.
    Prior nonempty owners remain required even when their target is empty.
    Exact whole-graph equality separately catches unchanged-path dependents.
    """
    allowed = set(prior_owners) | set(cold_owners)
    return [owner for owner in required_owners(name_status) if owner in allowed]


def load_pinned_policy(source: Path) -> dict:
    """Freeze overlay bytes for the run. Do not copy a drifting live source file."""
    if not PINNED_POLICY.is_file():
        raise RuntimeError(f"missing pinned policy {PINNED_POLICY}")
    pinned_bytes = PINNED_POLICY.read_bytes()
    source_cfg = source / "mcp-arch.yaml"
    source_hash = sha256_file(source_cfg) if source_cfg.is_file() else ""
    return {
        "path": str(PINNED_POLICY),
        "sha256": sha256_bytes(pinned_bytes),
        "bytes": pinned_bytes,
        "source_mcp_arch": str(source_cfg) if source_cfg.is_file() else "",
        "source_mcp_arch_sha256": source_hash,
        "source_matches_pin": bool(source_hash) and source_hash == sha256_bytes(pinned_bytes),
    }


def git_tracked_file(repo: Path, rel: str) -> bool:
    out = run(["git", "-C", str(repo), "ls-files", "--", rel])
    return bool(out.stdout.strip())


def apply_pinned_policy(snapshot: Path, policy: dict) -> dict:
    """Write the pinned overlay unless the SHA already has a tracked config."""
    dest = snapshot / "mcp-arch.yaml"
    tracked = git_tracked_file(snapshot, "mcp-arch.yaml")
    info = {
        "dest": str(dest),
        "pinned_source": policy["path"],
        "pinned_sha256": policy["sha256"],
        "tracked": tracked,
        "applied": False,
        "historical_config_sha256": None,
    }
    if tracked:
        info["historical_config_sha256"] = sha256_file(dest)
        return info
    dest.write_bytes(policy["bytes"])
    info["applied"] = True
    return info


def snapshot_at(fetch: Path, sha: str, dest: Path, policy: dict) -> dict:
    if dest.exists():
        shutil.rmtree(dest)
    dest.parent.mkdir(parents=True, exist_ok=True)
    run(["git", "clone", "--quiet", "--no-checkout", str(fetch), str(dest)])
    run(["git", "-C", str(dest), "checkout", "--quiet", "--detach", sha])
    return apply_pinned_policy(dest, policy)


def checkout_sha(repo: Path, sha: str, policy: dict) -> dict:
    run(["git", "-C", str(repo), "checkout", "--quiet", "--detach", sha])
    return apply_pinned_policy(repo, policy)


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


def go_bin() -> str:
    for cand in GO_BIN_CANDIDATES:
        if cand.is_file():
            return str(cand)
    return "go"


def go_env() -> dict:
    env = os.environ.copy()
    extra = "/tmp/enola-toolchain/go/bin:/tmp/enola-toolchain/bin"
    env["PATH"] = extra + ":" + env.get("PATH", "")
    return env


def enola_identity(enola_root: Path) -> dict:
    rev = git_out(enola_root, "rev-parse", "HEAD")
    porcelain = git_out(enola_root, "status", "--porcelain")
    diff = run(["git", "-C", str(enola_root), "diff", "HEAD"]).stdout.encode()
    cached = run(["git", "-C", str(enola_root), "diff", "--cached"]).stdout.encode()
    untracked = []
    for line in porcelain.splitlines():
        if line.startswith("?? "):
            untracked.append(line[3:])
    untracked_blob = "\n".join(untracked).encode()
    return {
        "revision": rev,
        "dirty": bool(porcelain.strip()),
        "status_porcelain": porcelain,
        "diff_sha256": sha256_bytes(diff + b"\0" + cached),
        "untracked_names_sha256": sha256_bytes(untracked_blob),
    }


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


def binary_provenance(binary: Path) -> dict:
    text = go_version_m(binary)
    vcs_revision = ""
    vcs_modified = ""
    go_version = ""
    for line in text.splitlines():
        stripped = line.strip()
        if stripped.startswith("go ") or stripped.endswith("go1.") or ": go" in stripped:
            go_version = stripped
        if "build" in stripped and "vcs.revision=" in stripped:
            vcs_revision = stripped.split("vcs.revision=", 1)[-1]
        if "build" in stripped and "vcs.modified=" in stripped:
            vcs_modified = stripped.split("vcs.modified=", 1)[-1]
    return {
        "path": str(binary),
        "sha256": sha256_file(binary),
        "go_version_m": text,
        "go_version_m_sha256": sha256_bytes(text.encode()),
        "go_version": go_version,
        "vcs_revision": vcs_revision,
        "vcs_modified": vcs_modified,
    }


def build_enola(enola_root: Path, dest: Path) -> None:
    dest.parent.mkdir(parents=True, exist_ok=True)
    proc = subprocess.run(
        [go_bin(), "build", "-o", str(dest), "./cmd/enola"],
        cwd=enola_root,
        env=go_env(),
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
    """Require a contiguous prefix of the first-parent chain starting at index 0."""
    if not only.strip():
        return transitions
    wanted = [item.strip() for item in only.split(",") if item.strip()]
    matched = []
    for index, (parent, child) in enumerate(transitions):
        if any(_only_match(token, index, parent, child) for token in wanted):
            matched.append(index)
    if not matched:
        raise RuntimeError(f"--only {only!r} matched no first-parent transitions")
    expected = list(range(matched[0], matched[-1] + 1))
    if matched != expected:
        raise RuntimeError(
            f"noncontiguous --only {only!r} indexes {matched}; require a contiguous first-parent chain"
        )
    if matched[0] != 0:
        raise RuntimeError(
            f"--only {only!r} skips earlier transitions {list(range(matched[0]))}; "
            "require a contiguous prefix from the initial SHA so the parent is initialized"
        )
    return [transitions[i] for i in matched]


def require_fresh_work(work: Path) -> None:
    if work.exists():
        raise SystemExit(
            f"refusing to reuse existing work dir {work}; pass a fresh nonexistent path "
            "so persisted state cannot contaminate initial"
        )


def transition_failed(row: dict) -> bool:
    return bool(row.get("blocker")) or row.get("graph_hash_equal") is not True


def walk_until_failure(outcomes):
    rows = []
    for outcome in outcomes:
        rows.append(outcome)
        if transition_failed(outcome):
            break
    return rows


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
    require_fresh_work(work)
    work.mkdir(parents=True, exist_ok=False)
    assert_not_tracked_output(Path(args.output))

    ident = enola_identity(enola_root)
    binary = Path(args.binary) if args.binary else work / "enola"
    if not args.skip_build:
        build_enola(enola_root, binary)
    elif not binary.is_file():
        raise SystemExit(f"missing enola binary {binary}")
    bin_prov = binary_provenance(binary)
    policy = load_pinned_policy(source)

    fetch = work / "product-fetch"
    ref = args.ref
    tip = prepare_fetch_clone(source, fetch, ref)
    shas = first_parent_shas(fetch, ref, args.count)
    transitions = list(zip(shas[:-1], shas[1:]))
    transitions = filter_transitions(transitions, args.only)

    provenance = {
        "note": "first-parent history; persistent delta state; cold oracle per commit; file-sink --events JSONL timings, not NATS JetStream",
        "sink": "file",
        "source_tree": str(source),
        "source_dirty_untracked": git_out(source, "status", "--porcelain") if (source / ".git").exists() else "",
        "fetch_clone": str(fetch),
        "ref": ref,
        "ref_sha": tip,
        "first_parent_shas": shas,
        "selected_transitions": [case_id(p, c) for p, c in transitions],
        "enola": ident,
        "binary": bin_prov,
        "input_policy": {
            "pinned_source": policy["path"],
            "pinned_sha256": policy["sha256"],
            "source_mcp_arch": policy["source_mcp_arch"],
            "source_mcp_arch_sha256": policy["source_mcp_arch_sha256"],
            "source_matches_pin": policy["source_matches_pin"],
            "note": "pinned overlay applied only when the SHA has no tracked mcp-arch.yaml; historical tracked config is left in place",
            "lockfiles_excluded_from_required_owners": True,
        },
        "baseline": str(HERE / BASELINE_NAME),
    }
    write_json(work / "provenance.json", provenance)

    live = work / "live"
    snapshot_at(fetch, shas[0], live, policy)
    events = work / "events-live.jsonl"
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
            "sink": "file",
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
            prior = applied.snapshot()
            prior_owners = prior.owner_set_nonempty()
            write_json(case / "changed.json", {"name_status": changed, "changed_owners": required_owners(changed)})
            before = events.stat().st_size if events.exists() else 0
            checkout_sha(live, child, policy)
            delta = scope.enola(binary, "delta", live, state_live, events, case / "summary-delta.json")
            delta_pairs = scope.parse_event_records(events, before)
            delta_records = [rec for rec, _ in delta_pairs]
            delta["events"] = len(delta_records)
            validated = validate_publishing_or_noop(delta_records, delta_pairs, delta, completed, [])
            cold_root = case / "cold-src"
            snapshot_at(fetch, child, cold_root, policy)
            cold_events = case / "events-cold.jsonl"
            if cold_events.exists():
                cold_events.unlink()
            cold = scope.enola(binary, "analyze", cold_root, case / "state-cold", cold_events, case / "summary-cold.json")
            cold_pairs = scope.parse_event_records(cold_events)
            cold_records = [rec for rec, _ in cold_pairs]
            cold_validated = scope.validate_replacement(cold_records, cold_pairs)
            expected = scope.Consumer()
            expected.apply(cold_records)
            required = semantic_required_owners(changed, prior_owners, expected.owner_set_nonempty())
            begin_scope = set(validated.get("scope") or [])
            if not set(required).issubset(begin_scope):
                missing = sorted(set(required) - begin_scope)
                raise RuntimeError(f"required owners outside Begin: {missing[:20]}")
            if delta_records:
                applied.apply(delta_records)
                completed = validated["target_generation"]
            graph_equal = applied.graph_hash() == expected.graph_hash()
            if not graph_equal:
                raise RuntimeError("initial+deltas graph hash does not match cold")
            nec = scope.necessary_from_consumers(prior, expected)
            extra_begin = sorted(begin_scope - set(nec))
            row = {
                "id": ident_case,
                "index": index,
                "parent": parent,
                "child": child,
                "changed_file_count": len(changed),
                "required_owner_count": len(required),
                "required_owners_sample": required[:25],
                "delta": scope.slim_run(delta, scope.manifest_fields(delta_records)),
                "cold": scope.slim_run(cold, scope.manifest_fields(cold_records)),
                "begin_scope_count": len(begin_scope),
                "necessary_owner_count": len(nec),
                "necessary_owners_sample": nec[:25],
                "extra_begin_owner_count": len(extra_begin),
                "extra_begin_owners_sample": extra_begin[:25],
                "graph_hash_equal": True,
                "kind": validated.get("kind") or "delta",
                "completed_generation": completed,
                "sink": "file",
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
                    "necessary_owner_count": row.get("necessary_owner_count"),
                    "graph_hash_equal": row.get("graph_hash_equal"),
                    "blocker": bool(row.get("blocker")),
                    "sink": "file",
                }
            ),
            flush=True,
        )
        if transition_failed(row):
            print(f"stopping after first failed transition {ident_case}", file=sys.stderr)
            break

    summary = {
        "provenance": provenance,
        "initial_sha": shas[0],
        "transitions": rows,
        "work": str(work),
        "output": args.output,
        "sink": "file",
        "repeat_limitation": "file-sink diagnostic; not a NATS JetStream or performance-acceptance run; launch the 10-case only when agreed",
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
        only0 = filter_transitions(trans, "0")
        check("--only prefix 0", only0 == [trans[0]], str(only0))
        only01 = filter_transitions(trans, "0,1")
        check("--only prefix 0,1", only01 == trans[:2], str(only01))
        only_sha = filter_transitions(trans, linear_shas[0][:8])
        check("--only first parent sha", only_sha == [trans[0]], str(only_sha))
        only_id = filter_transitions(trans, case_id(*trans[0]))
        check("--only case id", only_id == [trans[0]], str(only_id))
        raised = False
        try:
            filter_transitions(trans, "1")
        except RuntimeError:
            raised = True
        check("--only non-prefix index rejected", raised)
        raised = False
        try:
            filter_transitions(trans, "0,2")
        except RuntimeError:
            raised = True
        check("--only gap rejected", raised)
        raised = False
        try:
            filter_transitions(trans, linear_shas[2][:8])
        except RuntimeError:
            raised = True
        check("--only later sha rejected", raised)
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

        prior = {"file:old.ts", "file:packages/crypto/src/a.ts"}
        cold = {"file:new.ts", "file:packages/crypto/src/a.ts"}
        semantic = semantic_required_owners(rows, prior, cold)
        check("deleted/old rename kept vs prior", "file:old.ts" in semantic, str(semantic))
        check("rename new kept vs cold", "file:new.ts" in semantic, str(semantic))
        target_only = [o for o in required_owners(rows) if o in cold]
        check("target-only would drop old rename", "file:old.ts" not in target_only, str(target_only))
        ignored = semantic_required_owners([["M", "ignored.fixture.ts"], ["D", "old.ts"]], prior, cold)
        check("ignored path dropped", "file:ignored.fixture.ts" not in ignored, str(ignored))
        check("deleted owner kept from prior", "file:old.ts" in ignored, str(ignored))

        # Begin membership alone is not a contribution. Exercise the same
        # Consumer sets used by the history loop, including edge-only owners.
        old_graph, new_graph = scope.Consumer(), scope.Consumer()
        old_graph.owners = {
            "file:inventory.json": [], "file:retired.ts": [{"id": "old"}],
            "file:old.ts": [{"id": "renamed"}], "file:zero.ts": [{"id": "gone"}],
        }
        old_graph.edges = {"file:edge-only.ts": [{"id": "edge"}]}
        new_graph.owners = {
            "file:inventory.json": [], "file:new.ts": [{"id": "renamed"}],
            "file:zero.ts": [], "file:added.ts": [{"id": "added"}],
        }
        contribution_rows = [
            ["M", "inventory.json"], ["D", "retired.ts"],
            ["R100", "old.ts", "new.ts"], ["M", "zero.ts"],
            ["D", "edge-only.ts"], ["A", "added.ts"],
        ]
        semantic = set(semantic_required_owners(
            contribution_rows, old_graph.owner_set_nonempty(), new_graph.owner_set_nonempty()))
        check("inventory-only identity is not required", "file:inventory.json" not in semantic)
        check("retired node and edge contributions required",
              {"file:retired.ts", "file:edge-only.ts"} <= semantic)
        check("owner becoming empty still required", "file:zero.ts" in semantic)
        check("nonempty rename identities and addition required",
              {"file:old.ts", "file:new.ts", "file:added.ts"} <= semantic)
        check("inventory-set negative control catches false positive",
              "file:inventory.json" in semantic_required_owners(
                  contribution_rows, old_graph.owner_set_all(), new_graph.owner_set_all()))
        omitted = old_graph.snapshot()
        check("retaining deleted contributions fails cold equality",
              omitted.graph_hash() != new_graph.graph_hash())

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

        walked = walk_until_failure(
            [
                {"id": "00", "graph_hash_equal": True, "blocker": None},
                {"id": "01", "graph_hash_equal": False, "blocker": "boom"},
                {"id": "02", "graph_hash_equal": True, "blocker": None},
            ]
        )
        check("stop after first failure", [r["id"] for r in walked] == ["00", "01"], str(walked))

        existing = root / "existing-work"
        existing.mkdir()
        raised = False
        try:
            require_fresh_work(existing)
        except SystemExit:
            raised = True
        check("refuse existing work dir", raised)
        require_fresh_work(root / "fresh-work")
        check("nonexistent work dir allowed", True)

        src = root / "src"
        src.mkdir()
        (src / "mcp-arch.yaml").write_text("repo: .\ngraph_inputs:\n  exclude: [live-source-should-not-be-copied/**]\n")
        policy = load_pinned_policy(src)
        check("pin path recorded", Path(policy["path"]) == PINNED_POLICY)
        check("pin hash recorded", len(policy["sha256"]) == 64)
        check("live source not used as pin", "live-source-should-not-be-copied" not in policy["bytes"].decode())
        snap = root / "snap"
        snap.mkdir()
        init_repo(snap)
        applied = apply_pinned_policy(snap, policy)
        check("overlay applied on untracked", applied["applied"] and (snap / "mcp-arch.yaml").is_file())
        check("overlay is pinned bytes", sha256_file(snap / "mcp-arch.yaml") == policy["sha256"])
        hist = root / "hist"
        init_repo(hist)
        (hist / "mcp-arch.yaml").write_text("repo: .\nhistorical: true\n")
        run(["git", "add", "mcp-arch.yaml"], cwd=hist)
        run(["git", "commit", "-q", "-m", "tracked policy"], cwd=hist)
        skipped = apply_pinned_policy(hist, policy)
        check("historical tracked config not overwritten", not skipped["applied"], str(skipped))
        check("historical content preserved", "historical: true" in (hist / "mcp-arch.yaml").read_text())

        prior_c = scope.Consumer()
        prior_c.owners["file:old.ts"] = [{"id": "o", "kind": "sym", "occurrence": 0, "name": "old"}]
        prior_c.owners["file:keep.ts"] = [{"id": "k", "kind": "sym", "occurrence": 0, "name": "keep"}]
        cold_c = scope.Consumer()
        cold_c.owners["file:keep.ts"] = [{"id": "k", "kind": "sym", "occurrence": 0, "name": "keep"}]
        nec = scope.necessary_from_consumers(prior_c, cold_c)
        check("necessary includes deleted owner", "file:old.ts" in nec, str(nec))
        check("necessary omits unchanged owner", "file:keep.ts" not in nec, str(nec))
        n1 = {"id": "a", "occurrence": 0, "kind": "k", "props": {"z": 1, "a": 2}}
        n2 = {"id": "a", "occurrence": 0, "kind": "k", "props": {"a": 2, "z": 1}}
        n3 = {"id": "a", "occurrence": 0, "kind": "k", "props": {"z": 1, "a": 3}}
        ca = scope.Consumer()
        cb = scope.Consumer()
        ca.owners["file:x.ts"] = [n1, n3, n1]
        cb.owners["file:x.ts"] = [n3, n2, n2]
        check("complete JSON hash order-independent", ca.graph_hash() == cb.graph_hash())
        cc = scope.Consumer()
        cc.owners["file:x.ts"] = [n1, n3]
        check("complete JSON hash keeps duplicates", ca.graph_hash() != cc.graph_hash())

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
    print("first-parent oldest->newest, lockfile policy, no-op generation, pinned overlay, contiguous --only")
    print("baseline", HERE / BASELINE_NAME)
    print("readiness: harness self-test passed; Product10 first-parent run is gated on a fresh work dir")
    return 0


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--source", default="/tmp/enola-product-history-source")
    parser.add_argument("--work", default="", help="fresh nonexistent work directory (required for a real run)")
    parser.add_argument("--output", default="")
    parser.add_argument("--enola-root", default=str(ENOLA_ROOT))
    parser.add_argument("--binary", default="", help="optional prebuilt binary; default builds from --enola-root")
    parser.add_argument("--skip-build", action="store_true")
    parser.add_argument("--ref", default="a609c19f3861971930fae7b33dcb2950598953c5")
    parser.add_argument("--count", type=int, default=10)
    parser.add_argument("--only", default="")
    parser.add_argument("--self-test", action="store_true")
    args = parser.parse_args()
    if args.self_test:
        return self_test()
    if not args.work:
        raise SystemExit("pass a fresh nonexistent --work directory")
    if not args.output:
        args.output = str(Path(args.work) / "results.json")
    result = run_history(args)
    return result["exit"]


if __name__ == "__main__":
    sys.exit(main())
