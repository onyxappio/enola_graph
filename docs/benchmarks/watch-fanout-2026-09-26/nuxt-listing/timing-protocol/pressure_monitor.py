"""Read-only Darwin pressure observations; no performance acceptance decision."""
import ctypes
import json
import math
import threading
import time

NORMAL = 1
CADENCE_SECONDS = 0.25
MAX_GAP_SECONDS = 1.0


def pressure_reader():
    libc = ctypes.CDLL(None, use_errno=True)
    query = libc.sysctlbyname
    query.argtypes = [ctypes.c_char_p, ctypes.c_void_p,
                      ctypes.POINTER(ctypes.c_size_t), ctypes.c_void_p, ctypes.c_size_t]
    query.restype = ctypes.c_int

    def read():
        level = ctypes.c_uint32()
        size = ctypes.c_size_t(ctypes.sizeof(level))
        if query(b"kern.memorystatus_vm_pressure_level", ctypes.byref(level),
                 ctypes.byref(size), None, 0) != 0:
            raise OSError(ctypes.get_errno(), "pressure sysctl failed")
        if size.value != ctypes.sizeof(level):
            raise ValueError("unexpected pressure value size")
        return level.value
    return read


def validate(samples, start, end):
    """Require interval coverage, normal samples, and no long observation gaps.

    Sampling cannot exclude transients between observations. This validates only
    pressure evidence, not paging, competing workload, or benchmark correctness.
    """
    if not math.isfinite(start) or not math.isfinite(end) or end <= start:
        raise ValueError("invalid observation interval")
    if len(samples) < 2:
        raise ValueError("incomplete pressure observations")
    previous = None
    for row in samples:
        timestamp = row.get("monotonic")
        if not isinstance(timestamp, (int, float)) or not math.isfinite(timestamp):
            raise ValueError("invalid pressure timestamp")
        if row.get("error") or row.get("level") != NORMAL:
            raise ValueError("pressure unavailable or non-normal")
        if previous is not None and not 0 < timestamp - previous <= MAX_GAP_SECONDS:
            raise ValueError("pressure observation gap or unordered samples")
        previous = timestamp
    if samples[0]["monotonic"] > start or samples[-1]["monotonic"] < end:
        raise ValueError("pressure observations do not cover measured interval")
    return {"samples": len(samples), "max_gap_seconds": max(
        b["monotonic"] - a["monotonic"] for a, b in zip(samples, samples[1:]))}


class Monitor:
    def __init__(self, path, reader=None):
        self.path = path
        self.reader = reader if reader is not None else pressure_reader()
        self.samples = []
        self.stop_event = threading.Event()
        self.thread = None
        self.output = None

    def sample(self):
        row = {"monotonic": time.monotonic(), "wall_time": time.time()}
        try:
            row["level"] = self.reader()
        except Exception as error:
            row["error"] = repr(error)
        self.samples.append(row)
        self.output.write(json.dumps(row) + "\n")
        self.output.flush()

    def loop(self):
        try:
            while not self.stop_event.wait(CADENCE_SECONDS):
                self.sample()
        except Exception as error:
            # Preserve a failed writer/thread observation for fail-closed validation.
            self.samples.append({"monotonic": time.monotonic(), "error": repr(error)})

    def start(self):
        if self.output is not None:
            raise RuntimeError("monitor already started")
        self.output = self.path.open("x")
        try:
            self.sample()  # before caller starts timing
            self.thread = threading.Thread(target=self.loop, daemon=True)
            self.thread.start()
        except BaseException:
            self.output.close()
            raise

    def finish(self, start, end):
        self.stop_event.set()
        if self.thread is not None:
            self.thread.join(timeout=2)
            if self.thread.is_alive():
                raise RuntimeError("pressure sampler did not stop")
        try:
            self.sample()  # after caller ends timing
        finally:
            self.output.close()
        return validate(self.samples, start, end)
