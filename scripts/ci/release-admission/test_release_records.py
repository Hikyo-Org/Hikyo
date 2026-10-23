"""Synthetic whole-record and lifecycle tests; no historical/private originals."""
from concurrent.futures import ThreadPoolExecutor
from copy import deepcopy
from dataclasses import replace
import json
import builtins
from pathlib import Path
import unittest
import threading

from bounded import canonical, digest, instant, strict_json
from release_policy import FROZEN, POLICY_SHA256, REQUIRED_CI_JOBS, SCHEMAS, design_originals_checked
from release_records import NativeEvent, UnavailableAuthority, implementation_checked, record_checked
from release_test_support import (
    ReleaseFixture, T0, T2, T3, T4, T5, T6, T7, T8, T9,
    git, pin,
)


class BoundedTests(unittest.TestCase):
    def test_original_limits_are_literal(self):
        self.assertEqual(len(strict_json(b'"' + b'x' * 1_999_998 + b'"')), 1_999_998)
        for raw in (b'{"a":1,"a":2}', b'{"x":NaN}', b'{"x":Infinity}', b'\xff', b'[' * 1000):
            with self.subTest(raw=raw[:30]), self.assertRaises(ValueError):
                strict_json(raw)
        with self.assertRaises(ValueError):
            strict_json(b' ' * 2_000_001)
        canonical([0] * 2048)
        with self.assertRaises(ValueError):
            canonical([0] * 2049)
        value = 0
        for _ in range(40):
            value = [value]
        canonical(value)
        with self.assertRaises(ValueError):
            canonical([value])
        value = []
        value.append(value)
        with self.assertRaises(ValueError):
            canonical(value)

    def test_ascii_canonical_and_fixed_inventories(self):
        self.assertEqual(canonical({'z': 'é', 'a': 1}), b'{"a":1,"z":"\\u00e9"}')
        self.assertEqual([len(REQUIRED_CI_JOBS[key]) for key in ('candidate', 'workflow')], [49, 48])
        self.assertEqual(len(FROZEN), 7)
        with self.assertRaises(TypeError):
            REQUIRED_CI_JOBS['workflow']['new-job'] = 'success'
        self.assertEqual(len(POLICY_SHA256), 64)
        contracts = json.loads(Path(__file__).with_name('spec').joinpath('CONTRACTS.json').read_text())
        self.assertEqual(set(SCHEMAS), set(contracts['records']))
        cases = json.loads(Path(__file__).with_name('spec').joinpath('CONFORMANCE-CASES.json').read_text())
        self.assertEqual(len(cases['cases']), 69)

    def test_policy_import_is_inert_and_explicit_design_check_matches_originals(self):
        source = Path(__file__).with_name('release_policy.py').read_bytes()
        compiled = compile(source, 'release_policy.py', 'exec')
        namespace = {'__name__': 'inert_policy_test'}
        original_open = builtins.open

        def refuse_open(*args, **kwargs):
            raise AssertionError('policy import performed filesystem I/O')

        try:
            builtins.open = refuse_open
            exec(compiled, namespace)
        finally:
            builtins.open = original_open
        self.assertEqual(namespace['POLICY_SHA256'], POLICY_SHA256)
        spec = Path(__file__).with_name('spec')
        originals = {name: (spec / name).read_bytes() for name in ('POLICY.md', 'CONTRACTS.json', 'CONFORMANCE-CASES.json')}
        self.assertEqual(design_originals_checked(originals), POLICY_SHA256)
        with self.assertRaises(ValueError):
            design_originals_checked(dict(originals, **{'CONTRACTS.json': originals['CONTRACTS.json'] + b' '}))


class RecordTests(unittest.TestCase):
    def setUp(self):
        self.f = ReleaseFixture()

    def test_all_nine_closed_records(self):
        records = [self.f.release, self.f.approval, self.f.adoption, self.f.genesis,
                   self.f.config, self.f.manifest, self.f.binding, self.f.merge]
        hold = self.f.status('held', T4, self.f.genesis)
        recovery = self.f.recovery(hold)
        records.append(recovery)
        self.assertEqual({record['schema'] for record in records}, set(SCHEMAS.values()))
        for record in records:
            self.assertEqual(record_checked(record), record)
            for malformed in (dict(record, extra=True), {key: value for key, value in record.items() if key != 'schema'}, dict(record, schema={} )):
                with self.subTest(schema=record['schema']), self.assertRaises(ValueError):
                    record_checked(malformed)

    def test_boolean_ids_and_wrong_nested_types_refuse(self):
        mutations = [
            (self.f.release, ('releasePR', 'number'), True),
            (self.f.release, ('repository', 'repositoryId'), True),
            (self.f.release, ('workflowRun', 'id'), True),
            (self.f.release, ('workflowRun', 'attempt'), True),
            (self.f.approval, ('actor', 'databaseId'), True),
            (self.f.binding, ('currentMainRun', 'mergedPRNumber'), True),
            (self.f.binding, ('candidateRun', 'attempt'), True),
            (self.f.merge, ('mergedEvent', 'restId'), True),
            (self.f.genesis, ('sequence',), True),
            (self.f.genesis, ('reason',), []),
            (self.f.genesis, ('disposition',), []),
        ]
        for record, path, value in mutations:
            mutated = deepcopy(record)
            target = mutated
            for key in path[:-1]:
                target = target[key]
            target[path[-1]] = value
            with self.subTest(path=path), self.assertRaises(ValueError):
                record_checked(mutated)

    def test_merge_identity_parents_mode_and_jobs_are_fixed(self):
        bad = deepcopy(self.f.release)
        bad['mergeCommit']['orderedParents'].reverse()
        with self.assertRaises(ValueError):
            record_checked(bad)
        bad = dict(self.f.release, evidenceMode='causal-enqueue')
        with self.assertRaises(ValueError):
            record_checked(bad)
        bad = deepcopy(self.f.manifest)
        bad['requiredCIJobs']['workflow']['validation / race shard (11)'] = 'skipped'
        with self.assertRaises(ValueError):
            self.f.admission().source_manifest_checked(bad)
        bad = deepcopy(self.f.manifest)
        bad['frozenNextHelpers'][next(iter(FROZEN))] = pin('new-helper')
        with self.assertRaises(ValueError):
            record_checked(bad)

    def test_content_addressed_and_typed_references(self):
        bad = deepcopy(self.f.records)
        bad[self.f.release_pin]['collectedAt'] = T2
        with self.assertRaises(ValueError):
            self.f.admission(records=bad)
        bad = dict(self.f.config, releaseSHA256=digest(self.f.approval))
        with self.assertRaises(ValueError):
            self.f.admission(configuration=bad)
        bad = dict(self.f.config, rootAdoptionSHA256=digest(self.f.merge))
        with self.assertRaises(ValueError):
            self.f.admission(configuration=bad)

    def test_configuration_dag_and_no_input_or_output_mutation(self):
        m = self.f.admission()
        manifest, binding = deepcopy(self.f.manifest), deepcopy(self.f.binding)
        self.assertEqual(m.source_manifest_checked(manifest), manifest)
        self.assertEqual(m.attempt_binding_checked(binding, manifest), binding)
        self.f.config['resourceConfiguration']['namespace'] = 'hikyo-e2e-20261024c06/'
        self.assertEqual(m.resource_configuration['namespace'], 'hikyo-e2e-20261023c05/')
        output = m.configuration
        output['resourceConfiguration']['namespace'] = 'changed'
        self.assertEqual(m.resource_configuration['namespace'], 'hikyo-e2e-20261023c05/')
        for record, extra in ((manifest, {'attemptTrustBindingSHA256': digest(binding)}),
                              (binding, {'claimSHA256': pin('future')}),
                              (m.configuration, {'sourceManifestSHA256': digest(manifest)})):
            with self.assertRaises(ValueError):
                record_checked(dict(record, **extra))
        with self.assertRaises(ValueError):
            m.source_manifest_checked(dict(manifest, configurationSHA256=digest(binding)))

    def test_mixed_configuration_source_vector_static_manifest_and_old_schema(self):
        second = ReleaseFixture('two')
        with self.assertRaises(ValueError):
            self.f.admission().attempt_binding_checked(second.binding, second.manifest)
        bad = deepcopy(self.f.manifest)
        bad['candidateCIFileSHA256s'][next(iter(bad['candidateCIFileSHA256s']))] = pin('changed-ci')
        with self.assertRaises(ValueError):
            self.f.admission().source_manifest_checked(bad)
        with self.assertRaises(ValueError):
            record_checked(dict(self.f.manifest, schema='hikyo-158/future-fixture-source-manifest/v3'))
        bad = deepcopy(self.f.implementation)
        bad['files']['runtime/release-configuration.json'] = digest(self.f.config)
        with self.assertRaises(ValueError):
            self.f.admission(implementation=bad)

    def test_static_json_cjs_closure_and_runtime_exclusion(self):
        static = deepcopy(self.f.implementation)
        allowed = ('spec/CONTRACTS.json', 'spec/CONFORMANCE-CASES.json', 'spec/DESIGN-PINS.json',
                   'BASELINE-SOURCE-PINS.json', 'HISTORICAL-ARCHIVE-PINS.json',
                   'adapters/native-pins.json', 'adapters/ORIGINS.json', 'adapters/prepare-registry.json',
                   'adapters/create-registry.json', 'adapters/U-GUARD-REGISTRY.json',
                   'adapters/TRANSPORT-CODE-PINS.json', 'adapters/stage-kernel.cjs',
                   'adapters/dependencies/shared-window.cjs')
        for path in allowed:
            static['files'][path] = pin('reviewed-static-' + path)
        self.assertEqual(implementation_checked(static), static)
        for path in ('adapters/releaseConfiguration.json', 'spec/owner-approval.json',
                     'runtime/prepare-registry.json', 'records/approval.py',
                     'status/current.py', 'claims/executor.py', '../external.py',
                     '/absolute.py', '$HOME/local.py', '~/.config/local.py', 'C:\\local.py'):
            invalid = deepcopy(static)
            invalid['files'][path] = pin('runtime-data')
            with self.subTest(path=path), self.assertRaises(ValueError):
                implementation_checked(invalid)

    def test_exact_original_chronology_and_expiry(self):
        bad = dict(self.f.approval, decidedAt=T0)
        self.f.config['ownerApprovalSHA256'] = self.f.put(bad)
        with self.assertRaises(ValueError):
            self.f.admission()
        with self.assertRaises(ValueError):
            record_checked(dict(self.f.approval, validUntil='2026-10-27T00:00:00Z'))

    def test_size_depth_and_list_limits_apply_to_real_record_shapes(self):
        for extra in ('x' * 2_000_001, [0] * 2049):
            bad = deepcopy(self.f.merge)
            bad['mergedEvent']['nodeId'] = extra
            with self.assertRaises(ValueError):
                record_checked(bad)


class LifecycleTests(unittest.TestCase):
    def setUp(self):
        self.f = ReleaseFixture()

    def adopt(self):
        return self.f.admission().adopt_now(instant(T3))

    def hold(self):
        self.adopt()
        hold = self.f.status('held', T4)
        self.f.admission().transition_now(digest(hold), instant(T4))
        return hold

    def withdraw(self):
        hold = self.hold()
        withdrawal = self.f.status('revoked', T5)
        self.f.admission().transition_now(digest(withdrawal), instant(T5))
        return hold, withdrawal

    def test_default_authority_and_boolean_or_digest_only_receipts_refuse(self):
        m = self.f.admission(authority=None)
        with self.assertRaisesRegex(ValueError, 'unavailable'):
            m.adopt_now(instant(T3))
        self.assertIsInstance(m._authority, UnavailableAuthority)
        for transform in (lambda receipt: True, lambda receipt: digest(receipt.subject),
                          lambda receipt: replace(receipt, originals=()),
                          lambda receipt: replace(receipt, subject={'forged': True})):
            self.f.authority.receipt_transform = transform
            with self.assertRaises(ValueError):
                self.f.admission().adopt_now(instant(T3))
        self.assertEqual(self.f.authority.heads, {})

    def test_two_releases_same_static_code_two_pairs(self):
        first = self.f
        second = ReleaseFixture('two', first.authority)
        # First adoption cannot silently skip later decisions for its own release.
        first.admission().adopt_now(instant(T3))
        second.admission().adopt_now(instant(T3))
        self.assertEqual(first.implementation, second.implementation)
        self.assertNotEqual(first.release_pin, second.release_pin)
        for fixture in (first, second):
            m = fixture.admission()
            result = m.admit_now(fixture.binding, fixture.manifest, instant(T3))
            self.assertEqual(result['mode'], 'admission-now')
            self.assertFalse(result['executionAuthorityGranted'])
            moved = dict(fixture.binding, candidateHeadSHA=git('later-candidate'), mainSHA=git('later-main'),
                         currentMainRun=dict(fixture.binding['currentMainRun'], id=300, mergeSHA=git('later-main')))
            with self.assertRaises(ValueError):
                m.admit_now(moved, fixture.manifest, instant(T4))
            fixture.authority.register('attemptTrustBinding', moved)
            self.assertEqual(m.admit_now(moved, fixture.manifest, instant(T4))['mode'], 'admission-now')

    def test_held_to_withdrawn_keeps_hold_and_no_in_chain_unhold(self):
        hold, withdrawal = self.withdraw()
        chain = self.f.admission()._status_chain(digest(withdrawal))
        self.assertEqual([row['disposition'] for _, row in chain], ['eligible', 'held', 'revoked'])
        self.assertEqual(chain[1], (digest(hold), hold))
        with self.assertRaises(ValueError):
            self.f.admission().admit_now(self.f.binding, self.f.manifest, instant(T5))
        with self.assertRaises(ValueError):
            self.f.status('eligible', T6)
            self.f.admission()
        with self.assertRaises(ValueError):
            self.f.admission().transition_now(digest(withdrawal), instant(T6))

    def test_pending_hold_and_withdrawal_can_both_append_without_hiding_withdrawal(self):
        self.adopt()
        hold = self.f.status('held', T4)
        withdrawal = self.f.status('revoked', T5, hold)
        self.f.admission().transition_now(digest(hold), instant(T5))
        self.f.admission().transition_now(digest(withdrawal), instant(T5))
        self.assertEqual(self.f.authority.heads[self.f.release_pin].status_sha256, digest(withdrawal))

    def test_root_recovery_revalidates_standing_grant_consumes_new_decision_once(self):
        hold = self.hold()
        old_binding, old_manifest = deepcopy(self.f.binding), deepcopy(self.f.manifest)
        approval = deepcopy(self.f.approval)
        recovery = self.f.recovery(hold)
        m = self.f.admission()
        m.adopt_now(instant(T6))
        self.assertEqual(self.f.approval, approval)
        self.assertIn(recovery['rootDecisionCustodySHA256'], self.f.authority.used)
        self.assertEqual(m.admit_now(self.f.binding, self.f.manifest, instant(T6))['mode'], 'admission-now')
        with self.assertRaises(ValueError):
            m.adopt_now(instant(T6))
        with self.assertRaisesRegex(ValueError, 'reserved-attempt-never-rebinds'):
            m.admit_now(self.f.binding, self.f.manifest, instant(T6), reserved_binding=old_binding)
        # Reconstruction retains the originally legitimate action-time generation.
        old_config = dict(self.f.config, rootAdoptionSHA256=old_binding['rootAdoptionSHA256'],
                          statusGenesisSHA256=old_binding['statusGenesisSHA256'])
        old = self.f.admission(configuration=old_config)
        self.assertEqual(old.reconstruct_original_for_cleanup(old_binding, old_manifest, instant(T3), instant(T6))['mode'], 'cleanup-only')

    def test_root_recovery_after_any_withdrawal_refuses(self):
        hold, _ = self.withdraw()
        self.f.recovery(hold, T6, T7)
        with self.assertRaisesRegex(ValueError, 'root-recovery-only-held-no-withdrawal'):
            self.f.admission().adopt_now(instant(T7))

    def test_pending_unapplied_withdrawal_blocks_root_recovery_and_forward(self):
        hold = self.hold()
        self.f.status('revoked', T5)
        with self.assertRaises(ValueError):
            self.f.admission().admit_now(self.f.binding, self.f.manifest, instant(T5))
        self.f.recovery(hold, T6, T7)
        with self.assertRaisesRegex(ValueError, 'never-overrides-withdrawal'):
            self.f.admission().adopt_now(instant(T7))

    def test_later_owner_reauthorization_new_generation_and_original_cleanup(self):
        old_config, old_binding, old_manifest = deepcopy(self.f.config), deepcopy(self.f.binding), deepcopy(self.f.manifest)
        _, withdrawal = self.withdraw()
        self.f.reauthorize(withdrawal)
        self.f.admission().adopt_now(instant(T8))
        self.assertIn(self.f.approval['originalDecisionCustodySHA256'], self.f.authority.used)
        self.assertEqual(self.f.admission().admit_now(self.f.binding, self.f.manifest, instant(T8))['mode'], 'admission-now')
        old = self.f.admission(configuration=old_config)
        self.assertEqual(old.reconstruct_original_for_cleanup(old_binding, old_manifest, instant(T3), instant(T9))['mode'], 'cleanup-only')
        with self.assertRaises(ValueError):
            old.admit_now(old_binding, old_manifest, instant(T9))
        with self.assertRaises(ValueError):
            old.reconstruct_original_for_cleanup(old_binding, old_manifest, instant(T5), instant(T9))

    def test_duplicate_initial_adoption_cannot_hide_hold_or_revocation(self):
        self.withdraw()
        duplicate = self.f.new_adoption('initial', None, None, T6)
        self.f.configure(duplicate, self.f.new_genesis(duplicate, T6))
        with self.assertRaisesRegex(ValueError, 'no-duplicate-initial-adoption'):
            self.f.admission().adopt_now(instant(T6))

    def test_reauthorization_must_reference_actual_withdrawal_and_prior_owner(self):
        for key in ('supersedesRevocationSHA256', 'supersedesAdoptionSHA256', 'supersedesOwnerApprovalSHA256'):
            self.f = ReleaseFixture()
            hold, withdrawal = self.withdraw()
            if key == 'supersedesRevocationSHA256':
                wrong_pin = digest(hold)
            elif key == 'supersedesAdoptionSHA256':
                wrong_pin = self.f.put(dict(self.f.adoption, adoptedAt=T6))
            else:
                wrong_pin = self.f.put(dict(self.f.approval, originalDecisionCustodySHA256=pin('other-approval')))
            self.f.reauthorize(withdrawal, approval_changes={key: wrong_pin})
            with self.subTest(key=key), self.assertRaisesRegex(ValueError, 'explicit-actual-withdrawal-reauthorization'):
                self.f.admission().adopt_now(instant(T8))

    def test_recovery_changed_implementation_and_expired_grant_refuse(self):
        hold = self.hold()
        self.f.implementation['files']['core/next_fixture_model.source-only.py'] = pin('different-model')
        self.f.authority.register('implementation', self.f.implementation)
        self.f.recovery(hold)
        with self.assertRaisesRegex(ValueError, 'unchanged-recovery-subject-implementationManifestSHA256'):
            self.f.admission().adopt_now(instant(T6))
        other = ReleaseFixture()
        with self.assertRaisesRegex(ValueError, 'adoption-time-unexpired'):
            other.admission().adopt_now(instant('2026-10-24T10:00:00Z'))

    def test_no_recovery_decision_can_be_rewrapped_and_reused(self):
        hold = self.hold()
        first_recovery = self.f.recovery(hold)
        self.f.admission().adopt_now(instant(T6))
        second_hold = self.f.status('held', T7)
        self.f.admission().transition_now(digest(second_hold), instant(T7))
        old = self.f.adoption
        recovery = dict(first_recovery, priorAdoptionSHA256=digest(old),
                        priorHeldStatusSHA256=digest(second_hold), decidedAt=T8)
        recovery_pin = self.f.put(recovery)
        self.f.authority.add('root-recovery', self.f.release_pin, digest(old), recovery_pin,
                             recovery['rootDecisionCustodySHA256'], T8)
        adoption = self.f.new_adoption('root-safety-recovery', digest(old), recovery_pin, T9)
        self.f.configure(adoption, self.f.new_genesis(adoption, T9))
        head = self.f.authority.heads[self.f.release_pin]
        with self.assertRaisesRegex(ValueError, 'decision-replay'):
            self.f.admission().adopt_now(instant(T9))
        self.assertEqual(head, self.f.authority.heads[self.f.release_pin])

    def test_second_withdrawal_has_no_outgoing_transition_and_invalid_cleanup_is_refused(self):
        _, withdrawal = self.withdraw()
        repeated = self.f.status('revoked', T6)
        with self.assertRaisesRegex(ValueError, 'sticky-status-transition'):
            self.f.admission().transition_now(digest(repeated), instant(T6))
        old = self.f.admission()
        with self.assertRaises(ValueError):
            old.reconstruct_original_for_cleanup(self.f.binding, self.f.manifest, instant(T2), instant(T8))
        self.assertEqual(self.f.authority.heads[self.f.release_pin].status_sha256, digest(withdrawal))

    def test_pending_later_owner_approval_cannot_be_hidden_by_old_head(self):
        self.adopt()
        approval = dict(self.f.approval, decidedAt=T4, originalDecisionCustodySHA256=pin('new-incompatible-original'))
        self.f.authority.add('owner-approval', self.f.release_pin, None, self.f.put(approval),
                             approval['originalDecisionCustodySHA256'], T4)
        with self.assertRaisesRegex(ValueError, 'newer-incompatible-release-decision'):
            self.f.admission().admit_now(self.f.binding, self.f.manifest, instant(T4))

    def test_new_owner_approval_before_initial_adoption_cannot_be_hidden(self):
        approval = dict(self.f.approval, decidedAt='2026-10-23T10:02:30Z',
                        originalDecisionCustodySHA256=pin('new-before-initial'))
        original_adoption = self.f.authority.events.pop()
        self.f.authority.add('owner-approval', self.f.release_pin, None, self.f.put(approval),
                             approval['originalDecisionCustodySHA256'], approval['decidedAt'])
        self.f.authority.events.append(NativeEvent(3, original_adoption.raw, original_adoption.decision))
        with self.assertRaisesRegex(ValueError, 'newer-incompatible-release-decision'):
            self.f.admission().adopt_now(instant(T3))
        self.assertEqual(self.f.authority.heads, {})

    def test_new_owner_approval_before_root_recovery_invalidates_standing_grant(self):
        hold = self.hold()
        approval = dict(self.f.approval, decidedAt=T5, validUntil=T7,
                        originalDecisionCustodySHA256=pin('new-before-recovery'))
        self.f.authority.add('owner-approval', self.f.release_pin, None, self.f.put(approval),
                             approval['originalDecisionCustodySHA256'], T5)
        self.f.recovery(hold, T6, T7)
        original_head = self.f.authority.heads[self.f.release_pin]
        with self.assertRaisesRegex(ValueError, 'newer-incompatible-release-decision'):
            self.f.admission().adopt_now(instant(T7))
        self.assertEqual(self.f.authority.heads[self.f.release_pin], original_head)
        # Even an externally supplied matching new head cannot make _eligible
        # forget the approval boundary during subsequent forward admission.
        self.f.authority.heads[self.f.release_pin] = replace(original_head,
            adoption_sha256=digest(self.f.adoption), status_sha256=digest(self.f.genesis),
            revision=original_head.revision + 1, watermark=len(self.f.authority.events))
        with self.assertRaisesRegex(ValueError, 'newer-incompatible-release-decision'):
            self.f.admission().admit_now(self.f.binding, self.f.manifest, instant(T8))

    def test_gap_fork_replay_cross_adoption_and_stale_stream(self):
        self.adopt()
        transforms = [
            lambda o: replace(o, events=o.events[1:]),
            lambda o: replace(o, events=(o.events[0], o.events[0])),
            lambda o: replace(o, terminal_sequence=50_001),
            lambda o: replace(o, observed_at=T0),
            lambda o: replace(o, head=replace(o.head, watermark=99)),
            lambda o: replace(o, head=replace(o.head, adoption_sha256=digest(self.f.approval))),
        ]
        for transform in transforms:
            self.f.authority.observation_transform = transform
            with self.assertRaises(ValueError):
                self.f.admission().admit_now(self.f.binding, self.f.manifest, instant(T6))

    def test_unavailable_observation_is_transient_and_never_creates_hold(self):
        self.adopt()
        old = self.f.authority.heads.copy()
        self.f.authority.observation_transform = lambda _: None
        with self.assertRaises(ValueError):
            self.f.admission().admit_now(self.f.binding, self.f.manifest, instant(T4))
        self.assertEqual(self.f.authority.heads, old)
        self.f.authority.observation_transform = None
        self.f.admission().admit_now(self.f.binding, self.f.manifest, instant(T4))

    def test_missing_whole_review_or_original_refuses_before_cas(self):
        self.f.authority.verified.remove(('mergeEvidence', canonical(self.f.merge)))
        with self.assertRaisesRegex(ValueError, 'unregistered-whole-original'):
            self.adopt()
        self.assertEqual(self.f.authority.heads, {})

    def test_same_predecessor_cas_race_and_stream_movement_are_atomic(self):
        # Hold both callers after observing the identical predecessor.
        barrier = threading.Barrier(2, timeout=2)
        self.f.authority.before_cas = lambda _: barrier.wait()
        with ThreadPoolExecutor(max_workers=2) as pool:
            results = list(pool.map(lambda _: self.try_adopt(), range(2)))
        self.f.authority.before_cas = None
        self.assertEqual(results.count(True), 1)
        self.assertEqual(len(self.f.authority.used), 1)
        other = ReleaseFixture('two')
        other.authority.before_cas = lambda authority: authority.add_unrelated()
        with self.assertRaisesRegex(ValueError, 'stream-moved'):
            other.admission().adopt_now(instant(T3))
        self.assertEqual(other.authority.heads, {})
        self.assertEqual(other.authority.used, set())

    def try_adopt(self):
        try:
            self.adopt()
        except ValueError:
            return False
        return True


if __name__ == '__main__':
    unittest.main()
