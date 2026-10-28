"""Finite nonblocking local byte framing. Never serializes a native original."""
import math
import os
import select
import time


def remaining(stop, clock):
    now = clock()
    if type(now) not in (int, float) or not math.isfinite(now) or now < 0:
        raise ValueError('pipe:finite-monotonic-clock-required')
    value = stop - now
    if not math.isfinite(value) or value <= 0:
        raise TimeoutError('pipe:deadline-reconcile-only')
    return value


def write_all(fd, raw, *, stop, clock=time.monotonic):
    if type(raw) is not bytes or not raw:
        raise ValueError('pipe:nonempty-original-bytes-required')
    offset = 0
    while offset < len(raw):
        wait = remaining(stop, clock)
        if not select.select([], [fd], [], wait)[1]:
            raise TimeoutError('pipe:write-deadline-reconcile-only')
        try:
            count = os.write(fd, raw[offset:offset + 65536])
        except (BlockingIOError, InterruptedError):
            continue
        if count <= 0:
            raise EOFError('pipe:write-eof-reconcile-only')
        offset += count
    remaining(stop, clock)


def read_frame(fd, *, limit, stop, clock=time.monotonic):
    raw = bytearray()
    while b'\n' not in raw:
        wait = remaining(stop, clock)
        if not select.select([fd], [], [], wait)[0]:
            raise TimeoutError('pipe:read-deadline-reconcile-only')
        try:
            part = os.read(fd, min(65536, limit + 1 - len(raw)))
        except (BlockingIOError, InterruptedError):
            continue
        if not part:
            raise EOFError('pipe:read-eof-reconcile-only')
        raw.extend(part)
        if len(raw) > limit:
            raise ValueError('pipe:oversize-reconcile-only')
    if raw.count(b'\n') != 1 or not raw.endswith(b'\n') or len(raw) <= 1:
        raise ValueError('pipe:single-whole-original-frame-required')
    remaining(stop, clock)
    return bytes(raw)
