"""SOURCE ONLY. Explicit append-only storage; import performs no I/O.

No executable entry point, provider, clock, random ID, retry or recovery reset.
"""
from contextlib import contextmanager
import copy
import fcntl
import hashlib
import json
import os
from pathlib import Path
import stat


def require(ok, label):
    if not ok:
        raise ValueError('next-ledger:' + label)


def raw(value):
    return json.dumps(value, sort_keys=True, separators=(',', ':'), allow_nan=False).encode()


def digest(value):
    return hashlib.sha256(raw(value)).hexdigest()


def parse(data):
    require(type(data) is bytes and len(data) <= 2_000_000, 'bounded-json')
    def pairs(items):
        result = {}
        for key, value in items:
            require(key not in result, 'duplicate-key')
            result[key] = value
        return result
    def bad(_):
        raise ValueError('next-ledger:nonfinite-json')
    value = json.loads(data, object_pairs_hook=pairs, parse_constant=bad)
    raw(value)
    return value


def sync_dir(path):
    fd = os.open(path, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
    try:
        os.fsync(fd)
    finally:
        os.close(fd)


def checked_dir(path):
    require(type(path) is Path or isinstance(path, Path), 'explicit-path')
    require(path.is_absolute() and '..' not in path.parts and path.is_dir(), 'absolute-existing-directory')
    require(not any(p.is_symlink() for p in (path, *path.parents)), 'no-directory-symlink')
    return path


def read_public(path):
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW)
    try:
        info = os.fstat(fd)
        require(stat.S_ISREG(info.st_mode) and info.st_nlink == 1 and info.st_size <= 2_000_000, 'plain-bounded-single-link-file')
        with os.fdopen(fd, 'rb', closefd=False) as stream:
            data = stream.read(2_000_001)
        return parse(data)
    finally:
        os.close(fd)


def write_exclusive(path, value):
    data = raw(value) + b'\n'
    require(len(data) <= 2_000_000, 'bounded-exclusive-record')
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    try:
        with os.fdopen(fd, 'wb', closefd=False) as stream:
            stream.write(data)
            stream.flush()
            os.fsync(fd)
    finally:
        os.close(fd)
    sync_dir(path.parent)


class Ledger:
    """One stable global lock and exclusive approval marker across all ledgers.

    Explicit methods may write only after Root's separate proof gate. Tests use
    temporary sibling directories. Files never overwrite; incomplete writes hold.
    """
    def __init__(self, folder, parent, *, reviewed_folder=None):
        require(type(reviewed_folder) is str and reviewed_folder not in ('aws-next-fixture-state','aws-after-held-fixture-state'), 'actual-future-ledger-folder-required')
        self.folder, self.parent = checked_dir(Path(folder)), checked_dir(Path(parent))
        require(self.folder.parent == self.parent.parent and self.parent.name == 'aws-revalidation-20261018b'
                and self.folder.name == reviewed_folder, 'fresh-sibling-and-compatible-parent')
        self._locked = False

    @contextmanager
    def lock(self):
        require(not self._locked, 'non-nested-lock')
        fd = os.open(self.parent / '.provisioning.lock', os.O_RDWR | os.O_CREAT | os.O_NOFOLLOW, 0o600)
        try:
            info = os.fstat(fd)
            require(stat.S_ISREG(info.st_mode) and info.st_nlink == 1, 'plain-global-lock')
            try:
                fcntl.flock(fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
            except BlockingIOError:
                raise RuntimeError('next-ledger:shared-provisioning-lock-held') from None
            self._locked = True
            yield
        finally:
            self._locked = False
            os.close(fd)

    def claim(self, record):
        require(self._locked, 'lock-required')
        require(not list(self.folder.iterdir()), 'fresh-empty-ledger-only')
        approval = record['approvalSHA256']
        require(type(approval) is str and len(approval) == 64 and all(c in '0123456789abcdef' for c in approval), 'approval-hash')
        marker = self.parent / ('next-approval-' + approval + '.once.json')
        # The global marker burns the entire reservation even if the following
        # local write fails. Another folder or process cannot claim it again.
        write_exclusive(marker, record)
        write_exclusive(self.folder / 'claim.once.json', record)
        self.cas(None, 'reserved', {'claimSHA256': digest(record), 'attemptTrustBindingSHA256':record['attemptTrustBindingSHA256'], 'phase': 'reserved',
            'held': False, 'pending': None, 'completed': [], 'owner': {}, 'roles': {},
            'provider': None, 'keyARN': None, 'boundaryPolicyId': None, 'boundSHA256': None,
            'sealedSHA256': None, 'dispatch': None})
        return copy.deepcopy(record)

    def claim_record(self):
        record = read_public(self.folder / 'claim.once.json')
        marker = self.parent / ('next-approval-' + record['approvalSHA256'] + '.once.json')
        require(raw(read_public(marker)) == raw(record), 'global-claim-crosslink')
        return record

    def snapshot(self):
        require(self._locked, 'lock-required')
        files = sorted(self.folder.glob('event-*.once.json'))
        require(len(files) <= 512, 'finite-journal')
        previous = None
        last = None
        claim = self.claim_record()
        for sequence, path in enumerate(files, 1):
            require(path.name == f'event-{sequence:04d}.once.json', 'contiguous-events')
            event = read_public(path)
            require(type(event) is dict and set(event) == {'sequence', 'previousSHA256', 'kind', 'state'}, 'closed-event')
            require(type(event['sequence']) is int and event['sequence'] == sequence
                    and event['previousSHA256'] == previous and event['state']['claimSHA256'] == digest(claim), 'hash-chain')
            previous, last = digest(event), event
        return previous, copy.deepcopy(last['state']) if last else None

    def cas(self, expected, kind, state):
        require(self._locked and type(kind) is str and 1 <= len(kind) <= 80, 'locked-cas')
        current, _ = self.snapshot()
        require(current == expected, 'cas-conflict-no-forward')
        require(type(state) is dict and state['claimSHA256'] == digest(self.claim_record()), 'claim-bound-state')
        files = list(self.folder.glob('event-*.once.json'))
        event = {'sequence': len(files) + 1, 'previousSHA256': current, 'kind': kind, 'state': copy.deepcopy(state)}
        write_exclusive(self.folder / f"event-{event['sequence']:04d}.once.json", event)
        return digest(event)

    def once(self, name, value):
        require(self._locked and type(name) is str and name.endswith('.once.json')
                and '/' not in name and '\\' not in name and name not in ('.once.json', 'claim.once.json'), 'closed-leaf-record')
        write_exclusive(self.folder / name, value)

    def read_once(self, name):
        require(self._locked and '/' not in name and '\\' not in name and name.endswith('.once.json'), 'closed-leaf-read')
        return read_public(self.folder / name)


if __name__ == '__main__':
    raise SystemExit('SOURCE ONLY: no execution entry point; Root review and proof adapters required')
