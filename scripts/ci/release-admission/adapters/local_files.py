"""Owned local runtime files; explicit bounded reads and permanent O_EXCL burns."""
import hashlib
import json
import os
from pathlib import Path
import re
import stat


def require(condition, message):
    if not condition:
        raise ValueError('runner:' + message)


def canonical(value):
    return json.dumps(value, sort_keys=True, separators=(',', ':'), ensure_ascii=False, allow_nan=False).encode()


def sha(raw):
    return hashlib.sha256(raw).hexdigest()


def plain_path(value):
    path = Path(value)
    require(path.is_absolute() and str(path) == str(value) and '..' not in path.parts
            and not any(c in str(path) for c in ('\0', '\r', '\n')), 'absolute-normalized-path')
    for part in [*reversed(path.parents), path]:
        require(not part.is_symlink(), 'no-symlink-path-components')
    return path


def read_pinned(path, pin, *, limit=2_000_000):
    require(type(pin) is str and re.fullmatch('[0-9a-f]{64}', pin), 'explicit-raw-pin')
    raw = read_plain(path, limit=limit)
    require(sha(raw) == pin, 'exact-whole-file-pin')
    return raw


def read_plain(path, *, limit=2_000_000, owned=False):
    path = plain_path(path)
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
    with os.fdopen(fd, 'rb') as stream:
        info = os.fstat(stream.fileno())
        require(stat.S_ISREG(info.st_mode) and info.st_nlink == 1 and 0 < info.st_size <= limit,
                'bounded-plain-single-link-file')
        if owned:
            require(info.st_uid == os.getuid() and stat.S_IMODE(info.st_mode) == 0o600,
                    'owned-0600-runtime-file')
        raw = stream.read(limit + 1)
    require(0 < len(raw) <= limit, 'bounded-whole-file')
    return raw


def exclusive(path, raw):
    path = plain_path(path)
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    try:
        os.fchmod(fd, 0o600)
        view = memoryview(raw)
        while view:
            count = os.write(fd, view)
            require(count > 0, 'exclusive-write-progress')
            view = view[count:]
        os.fsync(fd)
    finally:
        os.close(fd)
    directory = os.open(path.parent, os.O_RDONLY)
    try:
        os.fsync(directory)
    finally:
        os.close(directory)


def directory_checked(path):
    path = plain_path(path)
    info = path.stat()
    require(stat.S_ISDIR(info.st_mode) and info.st_uid == os.getuid()
            and stat.S_IMODE(info.st_mode) == 0o700, 'owned-0700-runtime-directory')
    return path


def directory_info(path):
    path = directory_checked(path)
    info = path.stat()
    return {'device': info.st_dev, 'inode': info.st_ino, 'uid': info.st_uid,
            'mode': stat.S_IMODE(info.st_mode), 'type': 'directory'}


def fifo_info(fd):
    info = os.fstat(fd)
    require(stat.S_ISFIFO(info.st_mode) and info.st_uid == os.getuid()
            and stat.S_IMODE(info.st_mode) == 0o600 and info.st_nlink == 1
            and not os.get_blocking(fd), 'owned-0600-nonblocking-fifo')
    return {'device': info.st_dev, 'inode': info.st_ino, 'uid': info.st_uid,
            'mode': stat.S_IMODE(info.st_mode), 'type': 'FIFO'}


def open_fifo(path, expected=None):
    path = plain_path(path)
    before = path.lstat()
    require(stat.S_ISFIFO(before.st_mode), 'fifo-required-before-open')
    fd = os.open(path, os.O_RDWR | os.O_NONBLOCK | os.O_NOFOLLOW)
    try:
        identity = fifo_info(fd)
        require(identity['device'] == before.st_dev and identity['inode'] == before.st_ino
                and (expected is None or identity == expected), 'exact-reviewed-fifo-inode')
        return fd, identity
    except BaseException:
        os.close(fd)
        raise
