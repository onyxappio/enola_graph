#!/usr/bin/env python3
"""Frozen invalidation-scope harness for Product.

Ten independent clones under /tmp. Never writes the tracked Product tree and
never edits Enola production Go. Initial+delta share one events file so SinkID
matches. Owner-set comparison is separate from the graph hash. Rename uses
git diff --name-status -M and requires both old and new file owner IDs.
"""
from __future__ import annotations

import argparse
import hashlib
import json
import shutil
import subprocess
import sys
import time
from pathlib import Path


PASSWORD = Path("packages/crypto/src/password.ts")
HMAC = Path("packages/crypto/src/hmac.ts")
INDEX = Path("packages/crypto/src/index.ts")
KEYS = Path("packages/crypto/src/keys.ts")
PKG = Path("packages/crypto/package.json")
LOCK = Path("pnpm-lock.yaml")
PROBE = Path("packages/crypto/src/scopeProbe.ts")
HMAC_RENAMED = Path("packages/crypto/src/hmacSha.ts")


def run(cmd, **kw):
    return subprocess.run(cmd, check=True, **kw)


def clone_tree(src: Path, dst: Path) -> None:
    if dst.exists():
        shutil.rmtree(dst)
    dst.parent.mkdir(parents=True, exist_ok=True)
    try:
        run(["cp", "-cR", str(src), str(dst)])
    except subprocess.CalledProcessError:
        run(["cp", "-a", str(src), str(dst)])


def replace_once(path: Path, old: str, new: str) -> None:
    text = path.read_text()
    if old not in text:
        raise RuntimeError(f"{path}: expected snippet missing")
    path.write_text(text.replace(old, new, 1))


def parse_events(path: Path):
    records = []
    if not path.exists() or path.stat().st_size == 0:
        return records
    with path.open() as fh:
        for line in fh:
            line = line.rstrip("\n")
            if not line:
                continue
            parts = line.split(" ", 2)
            if len(parts) < 3:
                continue
            records.append(json.loads(parts[2]))
    return records


def owner_key(owner) -> str:
    if isinstance(owner, dict):
        return f"{owner.get('kind', '')}:{owner.get('id', '')}"
    return str(owner)


def file_owner(rel: str) -> str:
    return "file:" + rel.replace("\\", "/")


def last_begin(records):
    begin = None
    for rec in records:
        if rec.get("type") == "begin_replace":
            begin = rec
    return begin


def last_end(records):
    end = None
    for rec in records:
        if rec.get("type") == "end_replace":
            end = rec
    return end


def manifest_fields(records, after_run=None):
    """Persist Begin count/digest and End/batch fields for the last run."""
    begin = last_begin(records)
    end = last_end(records)
    batches = [r for r in records if r.get("type") == "batch"]
    if begin and after_run:
        batches = [r for r in batches if r.get("run_id") == begin.get("run_id")]
    out = {
        "begin_count": None,
        "begin_digest": None,
        "begin_schema": None,
        "begin_generation": None,
        "end_batch_count": None,
        "end_batch_digest": None,
        "end_owner_scope_len": None,
        "end_owner_scope_digest": None,
        "batch_seqs": [],
        "batch_phases": [],
    }
    if begin:
        out.update(
            {
                "begin_count": begin.get("owner_scope_count"),
                "begin_digest": begin.get("owner_scope_digest"),
                "begin_schema": begin.get("schema_version"),
                "begin_generation": [begin.get("base_generation"), begin.get("target_generation")],
                "begin_owners": [owner_key(o) for o in begin.get("owner_scope") or []],
            }
        )
    if end:
        out.update(
            {
                "end_batch_count": end.get("batch_count"),
                "end_batch_digest": end.get("batch_digest"),
                "end_owner_scope_len": end.get("owner_scope_len"),
                "end_owner_scope_digest": end.get("owner_scope_digest"),
            }
        )
    out["batch_seqs"] = [b.get("seq") for b in batches]
    out["batch_phases"] = [b.get("phase") for b in batches]
    return out


class Consumer:
    """Mirrors graphsession.Consumer replacement, including empty scoped owners."""

    def __init__(self):
        self.owners = {}
        self.edges = {}
        self.open = {}
        self.generation = 0

    def apply(self, records):
        for rec in records:
            typ = rec.get("type")
            if typ == "begin_replace":
                self._begin(rec)
            elif typ == "batch":
                self._batch(rec)
            elif typ == "end_replace":
                self._end(rec)

    def _begin(self, b):
        scope = {owner_key(o): o for o in b.get("owner_scope") or []}
        self.open[b["run_id"]] = {
            "scope": scope,
            "epoch": b.get("phase") == "epoch",
            "seq": {},
            "begin": b,
        }

    def _batch(self, b):
        st = self.open.get(b.get("run_id"))
        if st is None:
            return
        st["seq"][int(b.get("seq") or 0)] = b

    def _end(self, e):
        st = self.open.pop(e.get("run_id"), None)
        if st is None:
            return
        for key in st["scope"]:
            self.owners[key] = []
            self.edges[key] = []
        for seq in sorted(st["seq"]):
            b = st["seq"][seq]
            phase = b.get("phase")
            if phase == "local":
                continue
            if phase == "scope":
                for o in b.get("owners") or []:
                    key = owner_key(o)
                    self.owners[key] = []
                    self.edges[key] = []
                continue
            for n in b.get("nodes") or []:
                key = owner_key(n.get("owner") or {})
                self.owners.setdefault(key, []).append(n)
            for ed in b.get("edges") or []:
                key = owner_key(ed.get("owner") or {})
                self.edges.setdefault(key, []).append(ed)
        if st["epoch"]:
            keep = set(st["scope"])
            for key in list(self.owners):
                if key not in keep:
                    self.owners.pop(key, None)
                    self.edges.pop(key, None)
        self.generation = st["begin"].get("target_generation") or 0

    def owner_set_all(self):
        return set(self.owners) | set(self.edges)

    def owner_set_nonempty(self):
        keys = set()
        for k, ns in self.owners.items():
            if ns:
                keys.add(k)
        for k, es in self.edges.items():
            if es:
                keys.add(k)
        return keys

    def empty_owners(self):
        return sorted(self.owner_set_all() - self.owner_set_nonempty())

    def synthetic_owners(self):
        return sorted(k for k in self.owner_set_all() if k.startswith("synthetic:"))

    def graph_hash(self):
        rows = []
        keys = sorted(self.owner_set_nonempty())
        for k in keys:
            nodes = sorted(
                (self.owners.get(k) or []),
                key=lambda n: (n.get("id") or "", n.get("occurrence") or 0, n.get("kind") or ""),
            )
            edges = sorted(
                (self.edges.get(k) or []),
                key=lambda e: (
                    e.get("from_id") or "",
                    e.get("kind") or "",
                    e.get("target_name") or "",
                    e.get("occurrence") or 0,
                ),
            )
            rows.append({"owner": k, "nodes": nodes, "edges": edges})
        blob = json.dumps(rows, sort_keys=True, separators=(",", ":"))
        return hashlib.sha256(blob.encode()).hexdigest()


def necessary_from_consumers(base: Consumer, cold: Consumer):
    keys = base.owner_set_all() | cold.owner_set_all()
    nec = []
    for k in sorted(keys):
        if (base.owners.get(k) or []) != (cold.owners.get(k) or []) or (base.edges.get(k) or []) != (cold.edges.get(k) or []):
            nec.append(k)
    return nec


def classify(necessary, current, parsed, changed, graph_ok, events):
    nec = set(necessary)
    cur = set(current or [])
    changed_owners = {file_owner(p) for p in changed}
    if not graph_ok:
        return "equality-failed"
    if not events and nec:
        return "missed-invalidation"
    if not nec:
        return "stable-facts" if events else "noop"
    if nec <= changed_owners:
        return "local"
    if len(nec) <= max(8, 2 * len(changed_owners)):
        return "import-closure"
    if current and len(nec) >= 0.8 * max(len(cur), 1):
        return "global"
    if parsed >= 100 or len(nec) > 50:
        return "broad"
    return "import-closure"


def summarize_json(path: Path) -> dict:
    if not path.exists() or path.stat().st_size == 0:
        return {}
    data = json.loads(path.read_text())
    if isinstance(data, list) and data:
        data = data[0]
    return data


def enola(binary: Path, mode: str, repo: Path, state: Path, events: Path, summary: Path) -> dict:
    cmd = [
        str(binary),
        "graph",
        mode,
        "--authoritative-scope",
        "--max-begin-bytes",
        "1048576",
        "--events",
        str(events),
        "--state-dir",
        str(state),
        "--context",
        "scope-bench",
        "--repo-id",
        "product-scope",
        "--summary-json",
        str(repo),
    ]
    t0 = time.time()
    proc = subprocess.run(cmd, stdout=summary.open("w"), stderr=subprocess.PIPE, text=True)
    wall = round(time.time() - t0, 3)
    if proc.returncode != 0:
        raise RuntimeError(f"{mode} failed exit={proc.returncode}\n{(proc.stderr or '')[-4000:]}")
    out = summarize_json(summary)
    recs = parse_events(events)
    return {
        "exit": 0,
        "wall_s": wall,
        "parsed": out.get("ParsedFiles"),
        "cached": (out.get("Stats") or {}).get("cached_files"),
        "files_read": (out.get("Stats") or {}).get("files_read"),
        "generation": [out.get("BaseGeneration"), out.get("TargetGeneration")],
        "owners_published": out.get("OwnersPublished"),
        "events": len(recs),
        "run_id": out.get("RunID"),
        "stderr_graph": "\n".join(ln for ln in (proc.stderr or "").splitlines() if "[graph]" in ln),
    }


def git_name_status(repo: Path):
    proc = subprocess.run(
        ["git", "diff", "--name-status", "-M", "HEAD"],
        cwd=repo,
        check=True,
        capture_output=True,
        text=True,
    )
    rows = []
    for line in proc.stdout.splitlines():
        if not line.strip():
            continue
        parts = line.split("\t")
        rows.append(parts)
    return rows


def mutate_body(root: Path):
    replace_once(
        root / PASSWORD,
        "return email.trim().toLowerCase();",
        "return email.normalize('NFKC').trim().toLowerCase();",
    )
    return [str(PASSWORD)]


def mutate_add_fn(root: Path):
    path = root / HMAC
    path.write_text(path.read_text() + "\nexport function invalidationScopeProbe(): number {\n  return 42;\n}\n")
    return [str(HMAC)]


def mutate_rename_symbol(root: Path):
    replace_once(root / HMAC, "export function hmacEquals", "export function hmacEqualSafe")
    text = (root / HMAC).read_text()
    (root / HMAC).write_text(text.replace("hmacEquals(", "hmacEqualSafe("))
    replace_once(root / INDEX, "export { computeHmac, hmacEquals } from './hmac';", "export { computeHmac, hmacEqualSafe } from './hmac';")
    return [str(HMAC), str(INDEX)]


def mutate_import_target(root: Path):
    replace_once(
        root / HMAC,
        "return createHmac('sha256', key.keyBytes).update(canonicalValue, 'utf8').digest('hex');",
        "return createHmac('sha256', key.keyBytes).update('scope:' + canonicalValue, 'utf8').digest('hex');",
    )
    return [str(HMAC)]


def mutate_add_file(root: Path):
    (root / PROBE).write_text("export const invalidationScopeProbe = 1;\n")
    replace_once(
        root / HMAC,
        "import { createHmac, timingSafeEqual } from 'node:crypto';",
        "import { createHmac, timingSafeEqual } from 'node:crypto';\nimport { invalidationScopeProbe } from './scopeProbe';",
    )
    replace_once(
        root / HMAC,
        "return createHmac('sha256', key.keyBytes).update(canonicalValue, 'utf8').digest('hex');",
        "return createHmac('sha256', key.keyBytes).update(String(invalidationScopeProbe) + canonicalValue, 'utf8').digest('hex');",
    )
    return [str(PROBE), str(HMAC)]


def mutate_delete_file(root: Path):
    (root / HMAC).unlink()
    text = (root / INDEX).read_text()
    text = text.replace("export { computeHmac, hmacEquals } from './hmac';\n", "")
    text = text.replace("export type { HmacKey } from './hmac';\n", "")
    (root / INDEX).write_text(text)
    return [str(HMAC), str(INDEX)]


def mutate_rename_file(root: Path):
    run(["git", "mv", str(HMAC), str(HMAC_RENAMED)], cwd=root)
    idx = root / INDEX
    idx.write_text(idx.read_text().replace("from './hmac'", "from './hmacSha'"))
    keys = root / KEYS
    keys.write_text(keys.read_text().replace("from './hmac'", "from './hmacSha'"))
    return [str(HMAC), str(HMAC_RENAMED), str(INDEX), str(KEYS)]


def mutate_config(root: Path):
    data = json.loads((root / PKG).read_text())
    deps = data.setdefault("dependencies", {})
    deps["invalidation-scope-probe"] = "1.0.0"
    (root / PKG).write_text(json.dumps(data, indent=2) + "\n")
    return [str(PKG)]


def mutate_lock(root: Path):
    path = root / LOCK
    path.write_text(path.read_text() + "\n# invalidation-scope lock-only probe\n")
    return [str(LOCK)]


def mutate_multi(root: Path):
    return mutate_body(root) + mutate_import_target(root)


SCENARIOS = [
    ("01-body", "body of one symbol/function", mutate_body),
    ("02-add-function", "add a function", mutate_add_fn),
    ("03-rename-symbol", "rename symbol", mutate_rename_symbol),
    ("04-import-target-body", "import target body", mutate_import_target),
    ("05-add-file-import", "add file and import", mutate_add_file),
    ("06-delete-file", "delete file", mutate_delete_file),
    ("07-rename-file", "rename file", mutate_rename_file),
    ("08-package-config", "package/config change", mutate_config),
    ("09-lock-only", "lock-only", mutate_lock),
    ("10-multi-file", "multi-file change", mutate_multi),
]


def slim_run(info: dict, manifest=None) -> dict:
    out = {k: info.get(k) for k in ("exit", "wall_s", "parsed", "cached", "files_read", "generation", "owners_published", "events", "run_id")}
    if manifest:
        out["manifest"] = {
            k: manifest.get(k)
            for k in (
                "begin_count",
                "begin_digest",
                "begin_schema",
                "begin_generation",
                "end_batch_count",
                "end_batch_digest",
                "end_owner_scope_len",
                "end_owner_scope_digest",
            )
        }
        out["manifest"]["batch_seq_count"] = len(manifest.get("batch_seqs") or [])
        out["manifest"]["batch_phase_set"] = sorted(set(manifest.get("batch_phases") or []))
    return out


def run_scenario(binary: Path, gold: Path, work: Path, name: str, title: str, mutate) -> dict:
    root = work / name / "src"
    clone_tree(gold, root)
    out = work / name
    out.mkdir(parents=True, exist_ok=True)
    result = {"id": name, "title": title, "changed_files": [], "blocker": None, "repeats": 1}
    live_events = out / "events-live.jsonl"
    cold_events = out / "events-cold.jsonl"
    try:
        if live_events.exists():
            live_events.unlink()
        initial = enola(binary, "analyze", root, out / "state-live", live_events, out / "summary-initial.json")
        initial_recs = parse_events(live_events)
        initial_man = manifest_fields(initial_recs)
        result["initial"] = slim_run(initial, initial_man)
        base = Consumer()
        base.apply(initial_recs)
        before_size = live_events.stat().st_size
        changed = mutate(root)
        result["changed_files"] = changed
        if name == "07-rename-file":
            status = git_name_status(root)
            result["git_name_status"] = status
            renamed = [row for row in status if row and row[0].startswith("R")]
            if not renamed:
                raise RuntimeError(f"git diff --name-status -M did not record a rename: {status}")
            old_path, new_path = renamed[0][-2], renamed[0][-1]
            result["rename_old_new"] = [old_path, new_path]
        delta = enola(binary, "delta", root, out / "state-live", live_events, out / "summary-delta.json")
        live_recs = parse_events(live_events)
        delta_recs = parse_events_from_offset(live_events, before_size)
        delta["events"] = len(delta_recs)
        delta_man = manifest_fields(delta_recs)
        result["delta"] = slim_run(delta, delta_man)
        applied = Consumer()
        applied.apply(live_recs)
        if cold_events.exists():
            cold_events.unlink()
        cold_run = enola(binary, "analyze", root, out / "state-cold", cold_events, out / "summary-cold.json")
        cold_recs = parse_events(cold_events)
        cold_man = manifest_fields(cold_recs)
        result["cold"] = slim_run(cold_run, cold_man)
        cold = Consumer()
        cold.apply(cold_recs)
        nec = necessary_from_consumers(base, cold)
        current = delta_man.get("begin_owners") or []
        graph_ok = applied.graph_hash() == cold.graph_hash()
        nonempty_ok = applied.owner_set_nonempty() == cold.owner_set_nonempty()
        all_ok = applied.owner_set_all() == cold.owner_set_all()
        result["current_begin_scope_count"] = len(current)
        result["current_begin_digest"] = delta_man.get("begin_digest")
        result["end_batch_count"] = delta_man.get("end_batch_count")
        result["end_batch_digest"] = delta_man.get("end_batch_digest")
        result["end_owner_scope_len"] = delta_man.get("end_owner_scope_len")
        result["end_owner_scope_digest"] = delta_man.get("end_owner_scope_digest")
        result["necessary_owner_count"] = len(nec)
        result["necessary_owners_sample"] = nec[:25]
        result["graph_hash_equal"] = graph_ok
        result["nonempty_owner_set_equal"] = nonempty_ok
        result["all_owner_set_equal"] = all_ok
        result["applied_empty_owners_sample"] = applied.empty_owners()[:20]
        result["cold_empty_owners_sample"] = cold.empty_owners()[:20]
        result["applied_synthetic"] = applied.synthetic_owners()
        result["cold_synthetic"] = cold.synthetic_owners()
        result["kind"] = classify(nec, current, delta.get("parsed") or 0, changed, graph_ok, delta.get("events") or 0)
        if name == "07-rename-file":
            old_id = file_owner(result["rename_old_new"][0])
            new_id = file_owner(result["rename_old_new"][1])
            result["rename_old_in_begin"] = old_id in current
            result["rename_new_in_begin"] = new_id in current
            result["rename_old_in_necessary"] = old_id in nec
            result["rename_new_in_necessary"] = new_id in nec
            if current and (old_id not in current or new_id not in current):
                result["kind"] = "rename-missing-owner-ids"
        if current and nec:
            result["scope_ratio"] = round(len(current) / max(len(nec), 1), 2)
        elif not current and not nec:
            result["scope_ratio"] = 1.0
        else:
            result["scope_ratio"] = None
    except Exception as exc:
        result["blocker"] = str(exc)[:4000]
        result["kind"] = "blocker"
    (out / "result.json").write_text(json.dumps(result, indent=2) + "\n")
    return result


def parse_events_from_offset(path: Path, offset: int):
    if not path.exists():
        return []
    records = []
    with path.open() as fh:
        fh.seek(offset)
        for line in fh:
            line = line.rstrip("\n")
            if not line:
                continue
            parts = line.split(" ", 2)
            if len(parts) < 3:
                continue
            records.append(json.loads(parts[2]))
    return records


def main():
    p = argparse.ArgumentParser()
    p.add_argument("--source", default="/tmp/enola-product-benchmark-source")
    p.add_argument("--binary", default="/tmp/enola-product-frozen-v2-enola")
    p.add_argument("--work", default="/tmp/enola-invalidation-scope-2026-09-21")
    p.add_argument("--only", default="")
    args = p.parse_args()
    source = Path(args.source)
    binary = Path(args.binary)
    work = Path(args.work)
    if not source.is_dir():
        raise SystemExit(f"missing Product source {source}")
    if not binary.is_file():
        raise SystemExit(f"missing enola binary {binary}")
    work.mkdir(parents=True, exist_ok=True)
    gold = work / "gold"
    if not gold.exists():
        clone_tree(source, gold)
    wanted = {x.strip() for x in args.only.split(",") if x.strip()}
    results = []
    for name, title, mutate in SCENARIOS:
        if wanted and name not in wanted and not any(name.startswith(w) for w in wanted):
            continue
        print(f"== {name} {title}", flush=True)
        row = run_scenario(binary, gold, work, name, title, mutate)
        results.append(row)
        print(
            json.dumps(
                {
                    "id": row["id"],
                    "kind": row.get("kind"),
                    "changed": row.get("changed_files"),
                    "parsed": (row.get("delta") or {}).get("parsed"),
                    "events": (row.get("delta") or {}).get("events"),
                    "wall": (row.get("delta") or {}).get("wall_s"),
                    "current": row.get("current_begin_scope_count"),
                    "necessary": row.get("necessary_owner_count"),
                    "graph_eq": row.get("graph_hash_equal"),
                    "owners_eq": row.get("nonempty_owner_set_equal"),
                    "blocker": bool(row.get("blocker")),
                }
            ),
            flush=True,
        )
    summary = {
        "source": str(source),
        "binary": str(binary),
        "work": str(work),
        "repeats": 1,
        "repeat_limitation": "one pass of all 10 clones; three repeats skipped because each pass is a full Product initial+delta+cold (~8-10 min)",
        "scenarios": results,
    }
    out_json = Path(__file__).with_name("results.json")
    out_json.write_text(json.dumps(summary, indent=2) + "\n")
    print("wrote", out_json)


if __name__ == "__main__":
    sys.exit(main())
