"""Original N physical lock/journal/registry checks, explicit parent wiring."""
from . import adapter_context
import fcntl
import os
from pathlib import Path
import stat
from . import local_files as files
from . import runtime_binding
HERE = Path(__file__).resolve().parent


def physical_exclusion(parent):
    """One read-only LOCK_SH|LOCK_NB probe; no wait, renewal or holder takeover.

    Busy alone never identifies the owner. Root authenticates H's retained ready
    original/context and owns exclusive control independently.
    """
    fd = os.open(files.plain_path(parent / '.provisioning.lock'), os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
    try:
        info = os.fstat(fd)
        files.require(stat.S_ISREG(info.st_mode) and info.st_nlink == 1, 'existing-plain-physical-global-lock')
        try:
            fcntl.flock(fd, fcntl.LOCK_SH | fcntl.LOCK_NB)
        except BlockingIOError:
            return
        fcntl.flock(fd, fcntl.LOCK_UN)
        raise ValueError('native-preflight:physical-holder-not-exclusive')
    finally:
        os.close(fd)


def readonly_records(m, ledger_module, folder, parent, runtime_directory):
    """Same finite original hash-chain/claim links, no Ledger lock flag spoofing."""
    # Keep G's exact closed parser/hash semantics, adding nonblocking plain
    # source-path/file checks before any file read. Never wait on a FIFO.
    read = lambda path: ledger_module.parse(files.read_plain(path))
    claim = read(folder / 'claim.once.json')
    global_claim = read(parent / ('next-approval-' + claim['approvalSHA256'] + '.once.json'))
    m.require(m.same(claim, global_claim), 'same-original-global-exclusive-claim')
    paths = sorted(folder.glob('event-*.once.json')); m.require(1 <= len(paths) <= 512, 'finite-current-journal')
    previous = None; state = None
    for sequence, path in enumerate(paths, 1):
        m.require(path.name == f'event-{sequence:04d}.once.json', 'contiguous-original-journal')
        event = read(path); m.exact(event, {'sequence', 'previousSHA256', 'kind', 'state'})
        m.require(type(event['sequence']) is int and event['sequence'] == sequence
            and event['previousSHA256'] == previous and event['state']['claimSHA256'] == m.digest(claim), 'unchanged-original-journal-chain')
        previous = m.digest(event); state = event['state']
    for leaf in ('held.once.json', 'owner-CreateKey-response.once.json'):
        m.require(not (folder / leaf).exists(), 'no-held-or-completed-create-state')
    for leaf in ('final.json', 'release.once.json', 'submitted.json'):
        m.require(not (runtime_directory / leaf).exists(), 'H-window-not-released-or-failed')
    adapter_context.checked(m, claim, state)
    return {'claim': claim, 'state': state, 'state_head': previous,
            'intent_record': read(folder / 'owner-CreateKey-intent.once.json'),
            'ticket_record': read(folder / 'owner-CreateKey-transmit.once.json'),
            'locked_window': read(folder / 'root-native-CreateKey-lock.once.json')}


def verify_registry(m, binding, registry):
    # H's exact R2 code/contract/Node/create-readout verifier remains unchanged.
    runtime_binding.runtime_binding_checked(m, binding)
    review = m.strict_json(files.read_pinned(binding['reviewManifest']['path'], binding['reviewManifest']['rawSHA256']))
    by_id = {row['id']: row for row in review['scripts']}
    required = [*registry['scripts'], m.strict_json((HERE / 'U-GUARD-REGISTRY.json').read_bytes())]
    for row in required:
        m.require(row['id'] in by_id and m.same(row, by_id[row['id']]), 'exact-startup-create-stage-and-U-guard-registry')
        source = files.read_pinned(adapter_context.get(m).transport_root / row['file'], row['sha256'], limit=65536)
        m.require(len(source) > 0, 'pinned-stage-code-at-owner-startup')
