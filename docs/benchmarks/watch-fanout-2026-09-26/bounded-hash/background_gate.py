"""Darwin background observation; not an all-peer quiet acknowledgment.

CPU records cumulative Mach ticks plus the host timebase, not ps percentages. Missing counters,
identity changes, and sampling gaps invalidate evidence rather than imply idle.
"""
import ctypes, json, os, pathlib, re, subprocess, time

class Usage(ctypes.Structure):
    _fields_ = [('uuid', ctypes.c_ubyte * 16)] + [(name, ctypes.c_uint64) for name in (
        'user', 'system', 'pkg_idle', 'interrupt', 'pageins', 'wired', 'resident',
        'footprint', 'start', 'exit', 'child_user', 'child_system', 'child_idle',
        'child_interrupt', 'child_pageins', 'child_elapsed', 'disk_read', 'disk_write')]


LIBPROC = ctypes.CDLL('/usr/lib/libproc.dylib', use_errno=True)
LIBPROC.proc_pid_rusage.argtypes = [ctypes.c_int, ctypes.c_int, ctypes.c_void_p]
LIBPROC.proc_pid_rusage.restype = ctypes.c_int

class Timebase(ctypes.Structure):
    _fields_ = [("numer", ctypes.c_uint32), ("denom", ctypes.c_uint32)]

def timebase():
    info = Timebase()
    lib = ctypes.CDLL("/usr/lib/libSystem.B.dylib")
    if lib.mach_timebase_info(ctypes.byref(info)) != 0 or not info.denom:
        raise ValueError("unavailable Mach timebase")
    return dict(numer=info.numer, denom=info.denom)

TIMEBASE = timebase()

def process_usage(pid):
    usage = Usage()
    if LIBPROC.proc_pid_rusage(pid, 2, ctypes.byref(usage)) != 0:
        raise OSError(ctypes.get_errno(), f'proc_pid_rusage({pid})')
    return {name: getattr(usage, name) for name, _ in Usage._fields_ if name != 'uuid'}


def descendants(rows, roots):
    result = set(roots)
    while True:
        more = {r['pid'] for r in rows if r['ppid'] in result}
        if more <= result:
            return result
        result |= more


def sample(roster):
    began = time.monotonic()
    raw = subprocess.check_output(['ps', '-axo', 'pid=,ppid=,rss=,command='], text=True, timeout=2)
    rows = []
    for line in raw.splitlines():
        fields = line.strip().split(None, 3)
        if len(fields) != 4:
            raise ValueError('unparseable process row')
        rows.append(dict(pid=int(fields[0]), ppid=int(fields[1]), rss_kib=int(fields[2]), command=fields[3]))
    vm_raw = subprocess.check_output(['vm_stat'], text=True, timeout=2)
    vm = {k: int(v) for k, v in re.findall(r'^([^:\n]+):\s*(\d+)\.', vm_raw, re.M)}
    paging = {key: vm[key] for key in ('Pageins', 'Compressions', 'Swapins')}
    io_raw = subprocess.check_output(['iostat', '-Id'], text=True, timeout=2)
    io_lines = [line.split() for line in io_raw.splitlines() if line.strip()]
    if len(io_lines) != 3 or len(io_lines[2]) != 3 * len(io_lines[0]) or io_lines[1] != ['KB/t', 'xfrs', 'MB'] * len(io_lines[0]):
        raise ValueError('unrecognized iostat schema')
    disk = {name: {'transfers': int(io_lines[2][i * 3 + 1]),
                   'mb': float(io_lines[2][i * 3 + 2])}
            for i, name in enumerate(io_lines[0])}
    parked = {str(pid): process_usage(int(pid)) for pid in roster}
    return dict(time=time.time(), monotonic=began, duration=time.monotonic()-began,
                processes=rows, ps_raw=raw, vm_raw=vm_raw, paging=paging, parked=parked,
                io_raw=io_raw, system_disk=disk, cpu_units="Mach ticks", timebase=TIMEBASE)


def rates(previous, current, roster):
    dt = current['monotonic'] - previous['monotonic']
    if not 0 < dt <= 2:
        raise ValueError('sampling gap outside (0, 2] seconds')
    values = {"sampler.duration": current["duration"]}
    for pid, identity in roster.items():
        before, after = previous['parked'][pid], current['parked'][pid]
        if before['start'] != identity['start'] or after['start'] != identity['start']:
            raise ValueError('parked PID identity changed')
        for field in ('user', 'system', 'disk_read', 'disk_write', 'pageins'):
            delta = after[field] - before[field]
            if delta < 0:
                raise ValueError('counter regressed')
            values[f'{pid}.{field}'] = delta / dt
        values[f'{pid}.resident'] = after['resident']
    for field in ('Pageins', 'Compressions', 'Swapins'):
        delta = current['paging'][field] - previous['paging'][field]
        if delta < 0:
            raise ValueError('paging counter regressed')
        values[f'system.{field}'] = delta / dt
    for field in ('user', 'system', 'disk_read', 'disk_write', 'pageins', 'resident'):
        values[f'parked.{field}'] = sum(values[f'{pid}.{field}'] for pid in roster)
    return values


def check_processes(snapshot, roster, owner_pid):
    rows = snapshot['processes']
    owned = descendants(rows, {owner_pid})
    failures = []
    for row in rows:
        pid, cmd = row['pid'], row['command']
        if pid in owned:
            continue
        executable = cmd.split(None, 1)[0]
        name = pathlib.Path(executable).name
        heavy = (name == 'go' and re.search(r'\s(test|build|install|vet)(\s|$)', cmd)
                 or name.endswith('.test') or '/pkg/tool/' in executable
                 or name.startswith('enola') or name == 'benchobserver'
                 or any(x in cmd for x in ('run-history-bounded.py', 'hash-history-oracles.py')))
        chrome = ('--headless' in cmd or 'chrome-headless-shell' in cmd)
        if heavy:
            failures.append(f'foreign workload {pid}: {cmd}')
        if chrome and str(pid) not in roster:
            failures.append(f'unregistered headless process {pid}: {cmd}')
        if str(pid) in roster and cmd != roster[str(pid)]['command']:
            failures.append(f'parked command identity changed: {pid}')
    if set(roster) - {str(r['pid']) for r in rows}:
        failures.append('parked process missing; roster must be recalibrated')
    return failures


def calibrate(samples, roster):
    if len(samples) < 60:
        raise ValueError('requires at least 60 preflight samples')
    vectors = [rates(a, b, roster) for a, b in zip(samples, samples[1:])]
    # Fixed 10% headroom; zero observed I/O/paging remains a strict zero bound.
    return {key: max(v[key] for v in vectors) * 1.10 for key in vectors[0]}


def violations(vector, limits):
    if set(vector) != set(limits):
        return ['metric schema mismatch']
    return [key for key in vector if vector[key] > limits[key]]


def compare_background(arm_vectors, limits):
    """Descriptive matching rule, not a statistical equivalence claim.

    Across nine arms, mean and p95 of each parked resource must fit within
    10% of the pooled per-arm mean/p95 respectively. Zero permits only zero.
    System paging is gated samplewise but not compared as foreign activity:
    the candidate itself can change its own page-ins.
    """
    import statistics
    if len(arm_vectors) != 9 or any(not values for values in arm_vectors):
        return ['requires nine nonempty arm series']
    failures = []
    for key in limits:
        if not key.startswith('parked.') and key != 'sampler.duration':
            continue
        series = [[v[key] for v in values] for values in arm_vectors]
        means = [statistics.mean(v) for v in series]
        p95s = [sorted(v)[min(len(v)-1, int(.95*len(v)))] for v in series]
        if max(means)-min(means) > .10 * statistics.mean(means) or max(p95s)-min(p95s) > .10 * statistics.mean(p95s):
            failures.append(key)
    return failures
