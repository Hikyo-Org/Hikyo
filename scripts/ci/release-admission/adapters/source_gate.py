"""Verify this exact sibling and pinned N dependencies before importing N."""

from . import adapter_context
import importlib
import os
from pathlib import Path
import stat
import sys

from . import driver_data as d


def read(path,pin,limit=2_000_000):
    d.hash_checked(pin);path=Path(path)
    d.require(path.is_absolute() and '..' not in path.parts and not any(p.is_symlink() for p in (path,*path.parents)),'plain-source-or-public-input-path')
    fd=os.open(path,os.O_RDONLY|os.O_NOFOLLOW|os.O_NONBLOCK)
    with os.fdopen(fd,'rb') as stream:
        info=os.fstat(stream.fileno());d.require(stat.S_ISREG(info.st_mode) and info.st_nlink==1 and 0<info.st_size<=limit,'bounded-plain-file')
        raw=stream.read(limit+1)
    d.require(0<len(raw)<=limit and d.sha(raw)==pin,'exact-whole-raw-pin');return raw


def verify(pin):
    context = adapter_context.get()
    manifest = context.source_check()
    d.require(pin == context.source_sha('adapters/paired_core.py'), 'exact-reviewed-paired-driver-source')
    return manifest


def imports():
    from . import root_preflight, stage_custody
    adapter_context.get().source_check()
    return root_preflight, stage_custody
