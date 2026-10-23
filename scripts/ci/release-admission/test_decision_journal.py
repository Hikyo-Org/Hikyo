"""Real durable CAS and crash recovery, with only native authority doubled."""
from copy import deepcopy
from dataclasses import replace
from pathlib import Path
import os
import multiprocessing
import tempfile
import unittest
from unittest.mock import patch
import stat

from bounded import canonical, digest, strict_json
from decision_journal import DecisionJournal
from release_records import Head, NativeEvent, Observation


def contender(folder, genesis, observation, head, barrier, results):
    store = DecisionJournal(Path(folder), scope=observation.scope, genesis_sha256=genesis)
    barrier.wait(timeout=5)
    try:
        store.compare_and_swap(None, head, '4' * 64, observation,
                               lambda observed, events: deepcopy(observed))
        results.put('committed')
    except ValueError as error:
        results.put(str(error))


class JournalTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.folder = Path(self.temp.name).resolve()
        self.genesis = {'schema': 'hikyo-1046/decision-journal-genesis/v1',
                        'scope': 'synthetic-root', 'rootAuthorizationSHA256': 'a' * 64}
        (self.folder / 'genesis.once.json').write_bytes(canonical(self.genesis))
        self.store = self.reopen()
        self.observation = Observation('synthetic-root', '2026-10-23T12:00:00Z', 1,
            (NativeEvent(1, b'{"synthetic":"whole-native-original"}', None),), None)
        self.head = Head('1' * 64, '2' * 64, '3' * 64, 1, 1)

    def reopen(self):
        return DecisionJournal(self.folder, scope='synthetic-root', genesis_sha256=digest(self.genesis))

    def native(self, observation, events):
        self.assertEqual(observation, self.observation)
        return deepcopy(self.observation)

    def commit(self, store=None, decision='4' * 64):
        return (store or self.store).compare_and_swap(None, self.head, decision,
                                                     self.observation, self.native)

    def test_real_restart_retains_head_watermark_and_global_decision_consumption(self):
        self.assertEqual(self.commit(), self.head)
        _, heads, events = self.reopen().snapshot()
        self.assertEqual(heads[self.head.release_sha256], self.head)
        self.assertEqual(len(events), 1)
        other = replace(self.head, release_sha256='5' * 64)
        with self.assertRaisesRegex(ValueError, 'already-consumed'):
            self.reopen().compare_and_swap(None, other, '4' * 64, self.observation, self.native)
        self.assertEqual(len(self.reopen().snapshot()[2]), 1)

    def test_same_predecessor_race_has_one_durable_winner(self):
        second = self.reopen()
        self.commit()
        with self.assertRaisesRegex(ValueError, 'predecessor-moved'):
            self.commit(second, '6' * 64)
        self.assertEqual(len(self.reopen().snapshot()[2]), 1)

    def test_actual_competing_processes_commit_one_transition(self):
        context = multiprocessing.get_context('spawn')
        barrier, results = context.Barrier(2), context.Queue()
        children = [context.Process(target=contender, args=(str(self.folder), digest(self.genesis),
                    self.observation, self.head, barrier, results)) for _ in range(2)]
        try:
            for child in children:
                child.start()
            for child in children:
                child.join(timeout=10)
                self.assertFalse(child.is_alive(), 'bounded contender did not exit')
                self.assertEqual(child.exitcode, 0)
            outcomes = [results.get(timeout=2) for _ in children]
            self.assertEqual(outcomes.count('committed'), 1)
            self.assertTrue(any('decision-lock-held' in row or 'predecessor-moved' in row for row in outcomes))
            self.assertEqual(len(self.reopen().snapshot()[2]), 1)
        finally:
            for child in children:
                if child.is_alive():
                    child.terminate()
                    child.join(timeout=2)
            results.close()

    def test_changed_native_terminal_refuses_before_any_write(self):
        def moved(observation, events):
            return replace(observation, terminal_sequence=2)
        with self.assertRaisesRegex(ValueError, 'fresh-native-checkpoint'):
            self.store.compare_and_swap(None, self.head, '4' * 64, self.observation, moved)
        self.assertEqual(self.reopen().snapshot()[1], {})
        self.assertFalse((self.folder / 'pending.once.json').exists())

    def test_boolean_cannot_authenticate_a_checkpoint(self):
        with self.assertRaisesRegex(ValueError, 'fresh-native-checkpoint'):
            self.store.compare_and_swap(None, self.head, '4' * 64, self.observation, lambda *_: True)

    def test_crash_before_publish_refuses_until_exact_reauthenticated_recovery(self):
        def crash(event):
            raise OSError('synthetic crash before publication')
        self.store._publish_pending = crash
        with self.assertRaises(OSError):
            self.commit()
        with self.assertRaisesRegex(ValueError, 'explicit-recovery'):
            self.reopen().snapshot()
        event = strict_json((self.folder / 'pending.once.json').read_bytes())
        with self.assertRaises(ValueError):
            self.reopen().recover_pending('f' * 64, deepcopy)
        self.reopen().recover_pending(digest(event), deepcopy)
        self.assertEqual(self.reopen().snapshot()[1][self.head.release_sha256], self.head)
        with self.assertRaisesRegex(ValueError, 'predecessor-moved'):
            self.commit(self.reopen())

    def test_crash_after_link_preserves_one_transition_and_recovers_same_inode(self):
        def crash(event):
            os.link(self.folder / 'pending.once.json', self.folder / 'event-0001.once.json')
            raise OSError('synthetic crash after publication link')
        self.store._publish_pending = crash
        with self.assertRaises(OSError):
            self.commit()
        event = strict_json((self.folder / 'pending.once.json').read_bytes())
        self.reopen().recover_pending(digest(event), deepcopy)
        self.assertFalse((self.folder / 'pending.once.json').exists())
        self.assertEqual((self.folder / 'event-0001.once.json').stat().st_nlink, 1)
        self.assertEqual(len(self.reopen().snapshot()[2]), 1)

    def test_global_watermark_cannot_regress_on_another_release(self):
        self.observation = replace(self.observation, terminal_sequence=2,
            events=(*self.observation.events, NativeEvent(2, b'{"unrelated":true}', None)))
        self.head = replace(self.head, watermark=2)
        self.commit()
        other = replace(self.head, release_sha256='5' * 64, watermark=1)
        observation = replace(self.observation, terminal_sequence=1, events=self.observation.events[:1])
        with self.assertRaisesRegex(ValueError, 'observed-predecessor'):
            self.reopen().compare_and_swap(None, other, '6' * 64, observation, self.native)

    def test_missing_genesis_or_symlink_never_becomes_empty_authority(self):
        (self.folder / 'genesis.once.json').unlink()
        with self.assertRaises(FileNotFoundError):
            self.store.snapshot()
        outside = self.folder / 'elsewhere.json'
        outside.write_bytes(canonical(self.genesis))
        (self.folder / 'genesis.once.json').symlink_to(outside)
        with self.assertRaises(OSError):
            self.store.snapshot()

    def test_boolean_head_counters_refuse_before_publication(self):
        for head in (replace(self.head, revision=True), replace(self.head, watermark=True)):
            with self.subTest(head=head), self.assertRaisesRegex(ValueError, 'closed-durable-head'):
                self.store.compare_and_swap(None, head, '4' * 64, self.observation, self.native)
            self.assertEqual(self.reopen().snapshot()[1], {})

    def test_dangling_pending_symlink_refuses_snapshot(self):
        pending = self.folder / 'pending.once.json'
        pending.symlink_to(self.folder / 'missing.json')
        with self.assertRaisesRegex(ValueError, 'explicit-recovery'):
            self.reopen().snapshot()
        self.assertTrue(pending.is_symlink())

    def interrupted(self):
        def crash(event):
            raise OSError('synthetic interrupted decision')
        self.store._publish_pending = crash
        with self.assertRaises(OSError):
            self.commit()
        return strict_json((self.folder / 'pending.once.json').read_bytes())

    def test_unrelated_pending_hardlink_refuses_without_publication_or_marker_loss(self):
        event = self.interrupted()
        pending = self.folder / 'pending.once.json'
        os.link(pending, self.folder / 'unrelated.once.json')
        with self.assertRaisesRegex(ValueError, 'no-other-link'):
            self.reopen().recover_pending(digest(event), deepcopy)
        self.assertTrue(pending.exists())
        self.assertFalse((self.folder / 'event-0001.once.json').exists())

    def test_invalid_pending_predecessor_refuses_before_publication(self):
        event = self.interrupted()
        event['previousSHA256'] = 'f' * 64
        pending = self.folder / 'pending.once.json'
        pending.write_bytes(canonical(event))
        with self.assertRaisesRegex(ValueError, 'journal-chain'):
            self.reopen().recover_pending(digest(event), deepcopy)
        self.assertTrue(pending.exists())
        self.assertFalse((self.folder / 'event-0001.once.json').exists())

    def test_rejected_native_recovery_preserves_pending_bytes(self):
        event = self.interrupted()
        pending = self.folder / 'pending.once.json'
        before = pending.read_bytes()
        with self.assertRaisesRegex(ValueError, 'fresh-original-recovery'):
            self.reopen().recover_pending(digest(event), lambda _: True)
        self.assertEqual(pending.read_bytes(), before)
        self.assertFalse((self.folder / 'event-0001.once.json').exists())

    def test_recovery_syncs_file_data_before_both_publication_paths(self):
        for published in (False, True):
            with self.subTest(published=published), tempfile.TemporaryDirectory() as folder:
                self.folder = Path(folder).resolve()
                (self.folder / 'genesis.once.json').write_bytes(canonical(self.genesis))
                self.store = self.reopen()
                actual_fsync = os.fsync
                def failed_original(fd):
                    raise OSError('synthetic original file sync failure')
                with patch('decision_journal.os.fsync', failed_original), self.assertRaises(OSError):
                    self.commit()
                pending = self.folder / 'pending.once.json'
                event = strict_json(pending.read_bytes())
                if published:
                    os.link(pending, self.folder / 'event-0001.once.json')
                before = pending.read_bytes()
                with patch('decision_journal.os.fsync', failed_original), self.assertRaises(OSError):
                    self.reopen().recover_pending(digest(event), deepcopy)
                self.assertEqual(pending.read_bytes(), before)
                self.assertEqual((self.folder / 'event-0001.once.json').exists(), published)
                syncs = []
                def traced(fd):
                    syncs.append('file' if stat.S_ISREG(os.fstat(fd).st_mode) else 'directory')
                    actual_fsync(fd)
                with patch('decision_journal.os.fsync', traced):
                    self.assertEqual(self.reopen().recover_pending(digest(event), deepcopy), event)
                self.assertEqual(syncs, ['file', 'directory'])
                self.assertEqual(self.reopen().snapshot()[1][self.head.release_sha256], self.head)

    def test_same_inode_same_size_mutation_during_reauthentication_refuses(self):
        event = self.interrupted()
        pending = self.folder / 'pending.once.json'
        original_info = pending.stat()
        changed = deepcopy(event)
        changed['decisionSHA256'] = '5' * 64
        changed_raw = canonical(changed) + b'\n'
        self.assertEqual(len(changed_raw), original_info.st_size)
        def corrupt(reviewed):
            pending.write_bytes(changed_raw)
            return reviewed
        with self.assertRaisesRegex(ValueError, 'pending-bytes-unchanged'):
            self.reopen().recover_pending(digest(event), corrupt)
        self.assertEqual(pending.stat().st_ino, original_info.st_ino)
        self.assertEqual(pending.read_bytes(), changed_raw)
        self.assertFalse((self.folder / 'event-0001.once.json').exists())

    def test_final_fsync_uncertainty_retains_one_complete_transition(self):
        original_sync = self.store._sync
        calls = []
        def uncertain():
            calls.append(True)
            if len(calls) == 2:
                raise OSError('synthetic final fsync uncertainty')
            original_sync()
        self.store._sync = uncertain
        with self.assertRaises(OSError):
            self.commit()
        self.assertEqual(self.reopen().snapshot()[1][self.head.release_sha256], self.head)
        with self.assertRaisesRegex(ValueError, 'predecessor-moved'):
            self.commit(self.reopen())


if __name__ == '__main__':
    unittest.main()
