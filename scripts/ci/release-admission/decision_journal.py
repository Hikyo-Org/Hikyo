"""Durable CAS mechanics for an externally authenticated Root decision scope.

This is storage, not an authority implementation. The existing native-original
adapter must authenticate the scope, complete stream and journal checkpoint.
No local file, digest or empty directory establishes current eligibility.
"""
from contextlib import contextmanager
from copy import deepcopy
from dataclasses import asdict
import fcntl
import hashlib
import os
from pathlib import Path
import stat

from bounded import canonical, digest, exact, require, sha, strict_json
from release_records import Head, Observation


def observation_digest(observation):
    """Hash the bounded native stream without nesting 50,000 items in JSON."""
    stream = hashlib.sha256()
    for row in observation.events:
        stream.update(canonical({'sequence': row.sequence,
            'rawSHA256': hashlib.sha256(row.raw).hexdigest(), 'decision': row.decision}) + b'\n')
    return digest({'scope': observation.scope, 'observedAt': observation.observed_at,
        'terminalSequence': observation.terminal_sequence,
        'head': None if observation.head is None else asdict(observation.head),
        'nativeEventsSHA256': stream.hexdigest()})


class DecisionJournal:
    def __init__(self, folder, *, scope, genesis_sha256):
        require(isinstance(folder, Path) and folder.is_absolute() and folder.is_dir()
                and not any(path.is_symlink() for path in (folder, *folder.parents)), 'plain-journal-root')
        require(type(scope) is str and 1 <= len(scope) <= 256 and sha(genesis_sha256), 'explicit-reviewed-scope')
        self.folder, self.scope = folder, scope
        self.genesis_sha256 = genesis_sha256

    def _read(self, path):
        fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
        with os.fdopen(fd, 'rb') as stream:
            info = os.fstat(stream.fileno())
            require(stat.S_ISREG(info.st_mode) and info.st_nlink == 1
                    and 0 < info.st_size <= 2_000_000, 'bounded-single-link-journal-file')
            raw = stream.read(2_000_001)
        return strict_json(raw)

    def _sync(self):
        fd = os.open(self.folder, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
        try:
            os.fsync(fd)
        finally:
            os.close(fd)

    @contextmanager
    def lock(self):
        fd = os.open(self.folder / '.decision.lock', os.O_RDWR | os.O_CREAT | os.O_NOFOLLOW, 0o600)
        try:
            info = os.fstat(fd)
            require(stat.S_ISREG(info.st_mode) and info.st_nlink == 1, 'plain-decision-lock')
            try:
                fcntl.flock(fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
            except BlockingIOError:
                raise ValueError('release-journal:decision-lock-held') from None
            yield
        finally:
            os.close(fd)

    def _snapshot(self):
        require(not os.path.lexists(self.folder / 'pending.once.json'), 'pending-decision-needs-explicit-recovery')
        return self._committed_snapshot()

    def _committed_snapshot(self, excluded=None):
        genesis = self._read(self.folder / 'genesis.once.json')
        exact(genesis, {'schema', 'scope', 'rootAuthorizationSHA256'})
        require(genesis['schema'] == 'hikyo-1046/decision-journal-genesis/v1'
                and genesis['scope'] == self.scope and sha(genesis['rootAuthorizationSHA256'])
                and digest(genesis) == self.genesis_sha256, 'exact-external-journal-genesis')
        paths = sorted(path for path in self.folder.glob('event-*.once.json') if path != excluded)
        require(len(paths) <= 512, 'original-finite-CAS-journal')
        heads, used, events, previous = {}, set(), [], None
        for sequence, path in enumerate(paths, 1):
            require(path.name == f'event-{sequence:04d}.once.json', 'contiguous-journal')
            event = self._read(path)
            proposed = self._event_checked(event, sequence, previous, heads, used, events)
            heads[proposed.release_sha256] = proposed
            used.add(event['decisionSHA256'])
            previous = digest(event)
            events.append(event)
        return previous, heads, used, events

    def _event_shape(self,event):
        exact(event, {'schema', 'scope', 'sequence', 'previousSHA256', 'expected', 'proposed',
                      'decisionSHA256', 'terminalSequence', 'observationSHA256'})
        require(event['schema']=='hikyo-1046/decision-journal-event/v1' and event['scope']==self.scope
            and type(event['sequence']) is int and 1<=event['sequence']<=512
            and (event['previousSHA256'] is None or sha(event['previousSHA256']))
            and sha(event['decisionSHA256']) and sha(event['observationSHA256'])
            and type(event['terminalSequence']) is int and 1<=event['terminalSequence']<=50_000,
            'closed-journal-event')
        proposed=self._head(event['proposed'])
        expected=None if event['expected'] is None else self._head(event['expected'])
        return expected,proposed

    def _event_checked(self,event,sequence,previous,heads,used,events):
        expected,proposed=self._event_shape(event)
        require(event['sequence']==sequence and event['previousSHA256']==previous,'exact-journal-chain')
        require(heads.get(proposed.release_sha256)==expected and event['decisionSHA256'] not in used
            and proposed.watermark==event['terminalSequence'],'atomic-head-watermark-and-consumption')
        require(not events or event['terminalSequence']>=events[-1]['terminalSequence'],
            'global-watermark-never-regresses')
        self._advance_checked(expected,proposed)
        return proposed

    @staticmethod
    def _head(value):
        exact(value, {'release_sha256', 'adoption_sha256', 'status_sha256', 'revision', 'watermark'})
        require(all(sha(value[key]) for key in ('release_sha256', 'adoption_sha256', 'status_sha256'))
                and all(type(value[key]) is int and value[key] > 0 for key in ('revision', 'watermark')),
                'closed-durable-head')
        return Head(**value)

    @staticmethod
    def _advance_checked(expected, proposed):
        require(proposed.revision == (1 if expected is None else expected.revision + 1)
                and (expected is None or proposed.watermark >= expected.watermark), 'monotonic-CAS-head')

    def snapshot(self):
        with self.lock():
            previous, heads, _, events = self._snapshot()
            return previous, deepcopy(heads), deepcopy(events)

    def compare_and_swap(self, expected, proposed, decision_sha256, observation, authenticate_checkpoint):
        """Called by the existing original authority after truthful decision checks.

        authenticate_checkpoint MUST re-read and authenticate the complete native
        stream and this exact durable predecessor under the lock. Its returned
        observation must be byte-identical; a boolean is never accepted.
        """
        require(callable(authenticate_checkpoint), 'external-current-checkpoint-required')
        require(type(proposed) is Head and (expected is None or type(expected) is Head)
            and type(observation) is Observation,'closed-CAS-input-types')
        self._head(asdict(proposed))
        if expected is not None:self._head(asdict(expected))
        with self.lock():
            previous, heads, used, events = self._snapshot()
            require(len(events) < 512, 'finite-CAS-journal-exhausted')
            require(heads.get(proposed.release_sha256) == expected, 'CAS-predecessor-moved')
            require(sha(decision_sha256) and decision_sha256 not in used, 'decision-already-consumed')
            require(observation.scope == self.scope and observation.head == expected
                    and observation.terminal_sequence == proposed.watermark
                    and (not events or proposed.watermark >= events[-1]['terminalSequence']),
                    'exact-observed-predecessor')
            verified = authenticate_checkpoint(deepcopy(observation), deepcopy(events))
            require(type(verified) is type(observation) and verified == observation,
                    'whole-fresh-native-checkpoint')
            self._advance_checked(expected, proposed)
            event = {'schema': 'hikyo-1046/decision-journal-event/v1', 'scope': self.scope,
                     'sequence': len(events) + 1, 'previousSHA256': previous,
                     'expected': None if expected is None else asdict(expected), 'proposed': asdict(proposed),
                     'decisionSHA256': decision_sha256, 'terminalSequence': observation.terminal_sequence,
                     'observationSHA256': observation_digest(observation)}
            self._event_checked(event,len(events)+1,previous,heads,used,events)
            self._write_pending(event)
            self._publish_pending(event)
            return proposed

    def _write_pending(self, event):
        raw = canonical(event) + b'\n'
        fd = os.open(self.folder / 'pending.once.json', os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
        with os.fdopen(fd, 'wb') as stream:
            stream.write(raw)
            stream.flush()
            os.fsync(stream.fileno())
        self._sync()

    def _publish_pending(self, event):
        pending = self.folder / 'pending.once.json'
        final = self.folder / f"event-{event['sequence']:04d}.once.json"
        os.link(pending, final, follow_symlinks=False)
        pending.unlink()
        self._sync()

    def recover_pending(self, reviewed_event_sha256, reauthenticate):
        """Finish the exact interrupted transition after fresh native revalidation."""
        require(sha(reviewed_event_sha256) and callable(reauthenticate), 'explicit-reviewed-recovery')
        with self.lock():
            pending = self.folder / 'pending.once.json'
            # Publication may have created the second link before a crash. Read
            # it only after proving it is the exact same inode as the final file.
            info = pending.lstat()
            require(stat.S_ISREG(info.st_mode) and info.st_nlink in (1, 2), 'plain-pending-recovery')
            fd = os.open(pending, os.O_RDWR | os.O_NOFOLLOW | os.O_NONBLOCK)
            with os.fdopen(fd, 'r+b', buffering=0) as stream:
                opened = os.fstat(stream.fileno())
                require((opened.st_ino, opened.st_dev, opened.st_size, opened.st_nlink)
                    == (info.st_ino, info.st_dev, info.st_size, info.st_nlink)
                    and 0 < opened.st_size <= 2_000_000, 'bounded-exact-pending-inode')
                raw = stream.read(2_000_001)
                event = strict_json(raw)
                require(digest(event) == reviewed_event_sha256, 'exact-pending-transition')
                self._event_shape(event)
                final = self.folder / f"event-{event['sequence']:04d}.once.json"
                published = os.path.lexists(final)
                if published:
                    final_info = final.lstat()
                    require(stat.S_ISREG(final_info.st_mode) and info.st_nlink == final_info.st_nlink == 2
                        and (final_info.st_ino, final_info.st_dev) == (info.st_ino, info.st_dev),
                        'same-published-pending-inode')
                else:
                    require(info.st_nlink == 1, 'unpublished-pending-has-no-other-link')
                previous, heads, used, events = self._committed_snapshot(final if published else None)
                self._event_checked(event, len(events) + 1, previous, heads, used, events)
                verified = reauthenticate(deepcopy(event))
                require(type(verified) is dict and canonical(verified) == canonical(event), 'fresh-original-recovery')
                # Keep the no-follow descriptor through authentication. Metadata
                # alone cannot detect a same-size write to the original inode.
                stream.seek(0)
                require(stream.read(2_000_001) == raw, 'pending-bytes-unchanged-after-authentication')
                # The interrupted original write may never have completed its
                # file-data sync. Recovery must do so before removing its marker.
                os.fsync(stream.fileno())
                stream.seek(0)
                require(stream.read(2_000_001) == raw, 'pending-bytes-unchanged-after-sync')
                current = pending.lstat()
                require((current.st_ino, current.st_dev, current.st_size, current.st_nlink)
                    == (info.st_ino, info.st_dev, info.st_size, info.st_nlink),
                    'pending-unchanged-after-authentication')
                if published:
                    final_info = final.lstat()
                    require((final_info.st_ino, final_info.st_dev, final_info.st_nlink)
                        == (info.st_ino, info.st_dev, 2), 'published-pending-unchanged')
                    pending.unlink()
                    self._sync()
                else:
                    require(not os.path.lexists(final), 'no-concurrent-final-publication')
                    self._publish_pending(event)
                self._snapshot()
                return deepcopy(event)
