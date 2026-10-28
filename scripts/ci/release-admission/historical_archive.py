"""Confined exact-byte access to separately retained historical originals.

The archive is never an authority source by itself. These reads establish only
integrity against the reviewed index. No environment, clock or default archive
is consulted by this module.
"""
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import stat


def require(condition, label):
    if not condition:
        raise ValueError('release-archive:' + label)


def strict(raw):
    require(type(raw) is bytes and len(raw) <= 2_000_000, 'bounded-index')
    def pairs(items):
        result = {}
        for key, value in items:
            require(key not in result, 'duplicate-key')
            result[key] = value
        return result
    def bad(_):
        raise ValueError('release-archive:nonfinite-json')
    return json.loads(raw, object_pairs_hook=pairs, parse_constant=bad)


class PinnedArchive:
    def __init__(self, root, index_raw, *, reviewed_index_sha256):
        require(isinstance(root, Path) and root.is_absolute(), 'explicit-absolute-root')
        require(not any(p.is_symlink() for p in (root, *root.parents)), 'plain-root')
        require(hashlib.sha256(index_raw).hexdigest() == reviewed_index_sha256,
                'independently-reviewed-index')
        index = strict(index_raw)
        require(type(index) is dict and set(index) == {'schema', 'files'}
                and index['schema'] == 'hikyo-1046/historical-archive-index/v1', 'closed-index')
        require(type(index['files']) is dict and 1 <= len(index['files']) <= 256, 'bounded-files')
        self.root, self.rows = root, index['files']
        self.observed = set()

    def read(self, relative):
        require(type(relative) is str and relative in self.rows, 'listed-original-only')
        path = Path(relative)
        require(not path.is_absolute() and '..' not in path.parts
                and str(path) == relative, 'confined-relative-original')
        row = self.rows[relative]
        require(type(row) is dict and set(row) == {'bytes', 'rawSHA256'}
                and type(row['bytes']) is int and 0 < row['bytes'] <= 2_000_000,
                'closed-bounded-original-pin')
        target = self.root / path
        require(not any(p.is_symlink() for p in (target, *target.parents)), 'plain-original')
        fd = os.open(target, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
        with os.fdopen(fd, 'rb') as stream:
            info = os.fstat(stream.fileno())
            require(stat.S_ISREG(info.st_mode) and info.st_nlink == 1
                    and info.st_size == row['bytes'], 'regular-single-link-original')
            raw = stream.read(row['bytes'] + 1)
        require(len(raw) == row['bytes']
                and hashlib.sha256(raw).hexdigest() == row['rawSHA256'], 'whole-original-pin')
        self.observed.add(relative)
        return raw

    def verify_all(self):
        for relative in self.rows:
            self.read(relative)
        return len(self.observed)

    def fixture_module(self, basename):
        """Load only a fixed reviewed offline fixture builder, never a launcher."""
        allowed = {'test_next_fixture_model.py', 'test_next_fixture_execution.py',
                   'test_normalize_next_inventory.py', 'test_next_hosted_result.py',
                   'test_next_owner_finalization.py', 'test_partial_setup_cleanup.py'}
        require(basename in allowed, 'fixed-offline-builder')
        relative = ('aws-recovery-20261020/next-run-source-preparation/'
                    'banked-series-first-attempt-source/' + basename)
        self.verify_all()
        source = self.read(relative)
        target = self.root / relative
        spec = importlib.util.spec_from_file_location('historical_offline_fixture', target)
        module = importlib.util.module_from_spec(spec)
        exec(compile(source, str(target), 'exec'), module.__dict__)
        return module
