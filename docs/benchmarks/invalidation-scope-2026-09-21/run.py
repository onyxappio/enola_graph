#!/usr/bin/env python3
"""Frozen invalidation-scope harness for Product.

Ten independent clones under --work. Never writes the tracked Product tree and
never edits Enola production Go. Initial+delta share one events file so SinkID
matches. Owner-set comparison is separate from the graph hash. Rename uses
git diff --name-status -M and requires both old and new file owner IDs.

Writes $WORK/results.json. Does not overwrite the tracked docs snapshot.
One diagnostic pass is not a performance-acceptance run. Any contract,
graph-hash, required-owner, or lock-only failure exits nonzero.
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
    return [record for record, _ in parse_event_records(path)]


def parse_event_records(path: Path, offset: int = 0):
    """Return (decoded record, exact JSON payload bytes) pairs."""
    records = []
    if not path.exists() or path.stat().st_size == 0:
        return records
    with path.open("rb") as fh:
        fh.seek(offset)
        for line in fh:
            line = line.rstrip(b"\n")
            if not line:
                continue
            parts = line.split(b" ", 2)
            if len(parts) < 3:
                continue
            records.append((json.loads(parts[2]), parts[2]))
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


def validate_replacement(records, raw_records=None):
    """Validate the frozen v2 complete immutable replacement contract for one run."""
    if raw_records is None:
        raise RuntimeError("raw batch payloads are required to recompute End batch_digest")
    extra_types = sorted({r.get("type") for r in records if r.get("type") not in {"begin_replace", "end_replace", "batch"}})
    if extra_types:
        raise RuntimeError(f"replacement contains non-replacement events: {extra_types}")
    begins = [r for r in records if r.get("type") == "begin_replace"]
    ends = [r for r in records if r.get("type") == "end_replace"]
    if len(begins) != 1 or len(ends) != 1:
        raise RuntimeError(f"expected exactly one Begin and End, got {len(begins)}/{len(ends)}")
    begin, end = begins[0], ends[0]
    if begin.get("schema_version") != "enola.graph.v2":
        raise RuntimeError(f"unexpected schema_version {begin.get('schema_version')!r}")
    if begin.get("scope_mode") != "complete":
        raise RuntimeError(f"unexpected scope_mode {begin.get('scope_mode')!r}")
    base_gen = begin.get("base_generation")
    target_gen = begin.get("target_generation")
    if type(base_gen) is not int or type(target_gen) is not int or target_gen != base_gen + 1:
        raise RuntimeError(f"target_generation must be base_generation+1, got {base_gen!r}->{target_gen!r}")
    run_id = begin.get("run_id")
    if end.get("run_id") != run_id:
        raise RuntimeError("End run_id does not match Begin")
    raw_owners = list(begin.get("owner_scope") or [])
    for owner in raw_owners:
        if not isinstance(owner, dict) or owner.get("kind") != "file" or not owner.get("id"):
            raise RuntimeError("Begin owner scope must contain nonempty file owners only")
    owners = [owner_key(o) for o in raw_owners]
    if len(owners) != len(set(owners)):
        raise RuntimeError("Begin owner scope contains duplicate owner")
    if begin.get("owner_scope_count") != len(owners):
        raise RuntimeError("Begin owner_scope_count mismatch")
    expected_owner_digest = digest_owners(raw_owners)
    if begin.get("owner_scope_digest") != expected_owner_digest:
        raise RuntimeError("Begin owner_scope_digest mismatch")
    if end.get("owner_scope_len") != len(owners) or end.get("owner_scope_digest") != expected_owner_digest:
        raise RuntimeError("End owner manifest mismatch")
    comp = end.get("completeness")
    if not isinstance(comp, dict) or comp.get("status") != "success":
        raise RuntimeError(f"replacement incomplete: {comp}")
    unread = comp.get("files_unreadable") or []
    if unread:
        raise RuntimeError(f"replacement incomplete: {comp}")
    batches = [r for r in records if r.get("type") == "batch" and r.get("run_id") == run_id]
    seqs = [b.get("seq") for b in batches]
    if len(seqs) != len(set(seqs)) or sorted(seqs) != list(range(1, len(seqs) + 1)):
        raise RuntimeError(f"batch sequences are not unique contiguous 1..N: {seqs[:20]}")
    if any(b.get("phase") == "scope" or (b.get("owners") or []) for b in batches):
        raise RuntimeError("v2 complete replacement contains PhaseScope additions")
    if any(b.get("phase") != "resolved" for b in batches):
        raise RuntimeError("v2 complete replacement contains a non-resolved batch")
    scope = set(owners)
    for b in batches:
        for item in (b.get("nodes") or []) + (b.get("edges") or []):
            if owner_key(item.get("owner") or {}) not in scope:
                raise RuntimeError("batch contains owner outside Begin scope")
    if end.get("batch_count") != len(batches):
        raise RuntimeError("End batch_count mismatch")
    payload_by_seq = {}
    for rec, raw in raw_records:
        if rec.get("type") != "batch" or rec.get("run_id") != run_id:
            continue
        seq = rec.get("seq")
        if seq in payload_by_seq:
            raise RuntimeError(f"duplicate raw batch seq {seq}")
        payload_by_seq[seq] = raw
    if sorted(payload_by_seq) != list(range(1, len(batches) + 1)):
        raise RuntimeError("raw batch payloads missing or extra relative to seq 1..N")
    payloads = [payload_by_seq[seq] for seq in range(1, len(batches) + 1)]
    expected_batch_digest = digest_batches(payloads)
    if end.get("batch_digest") != expected_batch_digest:
        raise RuntimeError("End batch_digest mismatch")
    return {"run_id": run_id, "scope": scope, "batch_count": len(batches), "base_generation": base_gen, "target_generation": target_gen}


def validate_lock_only_noop(records, info):
    """Lock-only deltas must publish nothing and must not advance generation."""
    if records:
        types = sorted({r.get("type") for r in records})
        raise RuntimeError(f"lock-only published events: {types}")
    if (info.get("parsed") or 0) != 0:
        raise RuntimeError("lock-only parsed files")
    if (info.get("events") or 0) != 0:
        raise RuntimeError("lock-only published events")
    if (info.get("owners_published") or 0) != 0:
        raise RuntimeError("lock-only published owners")
    gen = info.get("generation") or []
    if len(gen) != 2 or gen[0] != gen[1]:
        raise RuntimeError(f"lock-only advanced generation: {gen}")


def digest_owners(owners):
    ordered = sorted((o.get("kind", ""), o.get("id", "")) for o in owners)
    h = hashlib.sha256()
    for kind, ident in ordered:
        h.update(kind.encode())
        h.update(b"\0")
        h.update(ident.encode())
        h.update(b"\0")
    return h.hexdigest()


def digest_batches(payloads):
    h = hashlib.sha256()
    for payload in payloads:
        h.update(payload)
    return h.hexdigest()


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


def owner_canonical(cons: Consumer, key: str) -> str:
    nodes = sorted(
        (cons.owners.get(key) or []),
        key=lambda n: (n.get("id") or "", n.get("occurrence") or 0, n.get("kind") or "", json.dumps(n, sort_keys=True)),
    )
    edges = sorted(
        (cons.edges.get(key) or []),
        key=lambda e: (
            e.get("from_id") or "",
            e.get("kind") or "",
            e.get("target_name") or "",
            e.get("occurrence") or 0,
            json.dumps(e, sort_keys=True),
        ),
    )
    return json.dumps({"nodes": nodes, "edges": edges}, sort_keys=True, separators=(",", ":"))


def necessary_from_consumers(base: Consumer, cold: Consumer):
    keys = base.owner_set_all() | cold.owner_set_all()
    return sorted(k for k in keys if owner_canonical(base, k) != owner_canonical(cold, k))


def classify(necessary, current, parsed, changed, graph_ok, events):
    nec = set(necessary)
    cur = set(current or [])
    changed_owners = {file_owner(p) for p in changed}
    if not graph_ok:
        return "equality-failed"
    if not events and nec:
        # Same graph hash with a nonempty "necessary" set is oracle noise, not a
        # missed Enola invalidation. Lock-only required owners stay empty.
        return "oracle-instability"
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
        initial_pairs = parse_event_records(live_events)
        initial_recs = [r for r, _ in initial_pairs]
        initial_man = manifest_fields(initial_recs)
        validate_replacement(initial_recs, initial_pairs)
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
        live_pairs = parse_event_records(live_events)
        live_recs = [r for r, _ in live_pairs]
        delta_pairs = parse_event_records(live_events, before_size)
        delta_recs = [r for r, _ in delta_pairs]
        delta["events"] = len(delta_recs)
        delta_man = manifest_fields(delta_recs)
        if name == "09-lock-only":
            validate_lock_only_noop(delta_recs, delta)
        else:
            validated = validate_replacement(delta_recs, delta_pairs)
            gen = delta.get("generation") or []
            if gen != [validated["base_generation"], validated["target_generation"]]:
                raise RuntimeError(f"summary generation {gen} does not match Begin {validated['base_generation']}->{validated['target_generation']}")
        result["delta"] = slim_run(delta, delta_man)
        applied = Consumer()
        applied.apply(live_recs)
        if cold_events.exists():
            cold_events.unlink()
        cold_run = enola(binary, "analyze", root, out / "state-cold", cold_events, out / "summary-cold.json")
        cold_pairs = parse_event_records(cold_events)
        cold_recs = [r for r, _ in cold_pairs]
        cold_man = manifest_fields(cold_recs)
        validate_replacement(cold_recs, cold_pairs)
        result["cold"] = slim_run(cold_run, cold_man)
        cold = Consumer()
        cold.apply(cold_recs)
        nec = necessary_from_consumers(base, cold)
        current = delta_man.get("begin_owners") or []
        graph_ok = applied.graph_hash() == cold.graph_hash()
        nonempty_ok = applied.owner_set_nonempty() == cold.owner_set_nonempty()
        all_ok = applied.owner_set_all() == cold.owner_set_all()
        result["current_begin_scope_count"] = len(current)
        result["begin_owners"] = current
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
        if not set(nec).issubset(set(current)):
            raise RuntimeError("required owners are outside Begin scope")
        if name == "09-lock-only" and (current or nec or delta_recs or (delta.get("events") or 0)):
            raise RuntimeError("lock-only Begin/necessary/events must all be empty")
        if not graph_ok:
            raise RuntimeError("initial+delta graph hash does not match cold")
        result["kind"] = classify(nec, current, delta.get("parsed") or 0, changed, graph_ok, delta.get("events") or 0)
        if name in {"05-add-file-import", "06-delete-file", "07-rename-file"} and len(current) > 100:
            result["kind"] = "whole-domain-membership"
            result["fallback"] = "add/delete/rename: prior file graph cannot prove a safe subset"
        if name == "08-package-config" and len(current) > 100:
            result["kind"] = "whole-domain-config"
            result["fallback"] = "config/manifest change: conservative whole-domain fallback"
        if name in {"01-body", "02-add-function", "03-rename-symbol", "04-import-target-body", "10-multi-file"} and len(current) > 100:
            raise RuntimeError(f"ordinary TS edit fell back to whole-domain Begin ({len(current)} owners)")
        if name == "07-rename-file":
            old_id = file_owner(result["rename_old_new"][0])
            new_id = file_owner(result["rename_old_new"][1])
            result["rename_old_in_begin"] = old_id in current
            result["rename_new_in_begin"] = new_id in current
            result["rename_old_in_necessary"] = old_id in nec
            result["rename_new_in_necessary"] = new_id in nec
            if old_id not in current or new_id not in current:
                raise RuntimeError(f"rename Begin omitted old or new file owner: {old_id} / {new_id}")
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
    p.add_argument("--self-test", action="store_true", help="validate the harness against unit cases and optional narrow2 events")
    args = p.parse_args()
    if args.self_test:
        return self_test()
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
        "repeat_limitation": "one diagnostic pass of all 10 clones; not a performance-acceptance run. Three repeats skipped because each pass is a full Product initial+delta+cold.",
        "scenarios": results,
    }
    out_json = work / "results.json"
    out_json.write_text(json.dumps(summary, indent=2) + "\n")
    print("wrote", out_json)
    failed = [r for r in results if scenario_failed(r)]
    if failed:
        print(f"benchmark failed: {len(failed)} scenario(s)", file=sys.stderr)
        for row in failed:
            print(f"  {row.get('id')}: kind={row.get('kind')} blocker={row.get('blocker')!r} graph_eq={row.get('graph_hash_equal')}", file=sys.stderr)
        return 1
    return 0


def scenario_failed(row: dict) -> bool:
    if row.get("blocker") or row.get("kind") == "blocker":
        return True
    if row.get("graph_hash_equal") is not True:
        return True
    if row.get("kind") in {"equality-failed", "missed-invalidation", "rename-missing-owner-ids"}:
        return True
    if row.get("id") == "09-lock-only":
        delta = row.get("delta") or {}
        gen = delta.get("generation") or []
        if (delta.get("events") or 0) != 0 or (delta.get("parsed") or 0) != 0:
            return True
        if len(gen) != 2 or gen[0] != gen[1]:
            return True
        if row.get("current_begin_scope_count"):
            return True
        if row.get("necessary_owner_count"):
            return True
    if row.get("id") in {"01-body", "02-add-function", "03-rename-symbol", "04-import-target-body", "10-multi-file"}:
        if (row.get("current_begin_scope_count") or 0) > 100:
            return True
    return False


def split_runs(path: Path):
    pairs = parse_event_records(path)
    runs = []
    cur = []
    for rec, raw in pairs:
        if rec.get("type") == "begin_replace" and cur:
            runs.append(cur)
            cur = []
        cur.append((rec, raw))
    if cur:
        runs.append(cur)
    return runs


def self_test() -> int:
    """Focused harness checks. Uses narrow2 artifacts when present; does not start a Product run."""
    owners = [{"kind": "file", "id": "b.ts"}, {"kind": "file", "id": "a.ts"}]
    if digest_owners(owners) != digest_owners(list(reversed(owners))):
        print("self-test FAIL: owner digest not order-stable", file=sys.stderr)
        return 1
    if not scenario_failed({"id": "01-body", "kind": "local", "graph_hash_equal": True, "current_begin_scope_count": 8645}):
        print("self-test FAIL: whole-domain ordinary TS edit must fail", file=sys.stderr)
        return 1
    if scenario_failed({"id": "01-body", "kind": "stable-facts", "graph_hash_equal": True, "current_begin_scope_count": 2, "necessary_owner_count": 0, "delta": {"parsed": 1, "events": 4, "generation": [1, 2]}}):
        print("self-test FAIL: narrow 01-body marked failed", file=sys.stderr)
        return 1
    lock_row = {"id": "09-lock-only", "kind": "noop", "graph_hash_equal": True, "current_begin_scope_count": 0, "necessary_owner_count": 0, "delta": {"parsed": 0, "events": 0, "generation": [1, 1]}}
    if scenario_failed(lock_row):
        print("self-test FAIL: lock-only no-op marked failed", file=sys.stderr)
        return 1
    narrow = Path("/tmp/enola-invalidation-scope-narrow2-2026-09-21")
    expect_delta = {
        "01-body": 2,
        "02-add-function": 3,
        "03-rename-symbol": 3,
        "04-import-target-body": 3,
        "10-multi-file": 4,
    }
    for name, begin_n in expect_delta.items():
        live = narrow / name / "events-live.jsonl"
        if not live.is_file():
            continue
        runs = split_runs(live)
        if len(runs) != 2:
            print(f"self-test FAIL: {name} expected 2 runs, got {len(runs)}", file=sys.stderr)
            return 1
        recs = [r for r, _ in runs[-1]]
        out = validate_replacement(recs, runs[-1])
        if out["base_generation"] != 1 or out["target_generation"] != 2 or len(out["scope"]) != begin_n:
            print(f"self-test FAIL: {name} delta contract {out} want Begin {begin_n}", file=sys.stderr)
            return 1
        begin = [r for r, _ in runs[-1] if r.get("type") == "begin_replace"][0]
        if digest_owners(begin.get("owner_scope") or []) != begin.get("owner_scope_digest"):
            print(f"self-test FAIL: {name} owner digest recompute mismatch", file=sys.stderr)
            return 1
        print(f"self-test {name} delta v2 complete Begin {begin_n} seq 1..N End success")
    for name in ("05-add-file-import", "06-delete-file", "07-rename-file", "08-package-config"):
        live = narrow / name / "events-live.jsonl"
        if not live.is_file():
            continue
        runs = split_runs(live)
        if len(runs) != 2:
            print(f"self-test FAIL: {name} expected 2 runs, got {len(runs)}", file=sys.stderr)
            return 1
        out = validate_replacement([r for r, _ in runs[-1]], runs[-1])
        if out["base_generation"] != 1 or out["target_generation"] != 2 or len(out["scope"]) < 100:
            print(f"self-test FAIL: {name} membership/config contract {out}", file=sys.stderr)
            return 1
        print(f"self-test {name} delta v2 complete whole-domain Begin {len(out['scope'])}")
    lock_live = narrow / "09-lock-only" / "events-live.jsonl"
    if lock_live.is_file():
        runs = split_runs(lock_live)
        if len(runs) != 1:
            print(f"self-test FAIL: lock-only expected only initial run, got {len(runs)}", file=sys.stderr)
            return 1
        print("self-test 09-lock-only no delta Begin")
    lock_row_path = narrow / "09-lock-only" / "result.json"
    if lock_row_path.is_file():
        row = json.loads(lock_row_path.read_text())
        if scenario_failed(row):
            print("self-test FAIL: stored 09-lock-only row failed", file=sys.stderr)
            return 1
    print("self-test ok")
    return 0


if __name__ == "__main__":
    sys.exit(main())
