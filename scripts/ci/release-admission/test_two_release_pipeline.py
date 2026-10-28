"""Configured two-release qualification with actual sealed source and originals.

Missing explicit archive is an error. Native authenticity/provider transport
and temporary storage alone are synthetic. No runtime authority is established.
"""
from copy import deepcopy
from datetime import timedelta
import hashlib
import os
import tempfile
from pathlib import Path
import unittest
from unittest.mock import patch

# pipeline_support establishes verified helpers before direct unittest imports.
from pipeline_support import Execution, Pipeline, archive_from_environment
from bounded import canonical, digest, instant, stamp
from release_records import ReleaseAdmission
from repin_core import repin


class TwoReleasePipelineTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.archive = archive_from_environment()

    def pipeline(self, version=1, **kwargs):
        p = Pipeline(version, archive=self.archive, **kwargs)
        self.addCleanup(p.close)
        return p

    def execution(self, pipeline, **kwargs):
        e = Execution(pipeline, **kwargs)
        self.addCleanup(e.close)
        return e

    def prepare_adapter(self,p):
        from test_adapter_context import prepare_through_adapters
        temporary=tempfile.TemporaryDirectory(prefix='hikyo-real-A-')
        self.addCleanup(temporary.cleanup)
        result=prepare_through_adapters(p,Path(temporary.name))
        self.assertIs(result['sameModel'],p.m)
        self.assertEqual(result['prepared'],p.prepared)
        return result

    def rehash_original_custody(self, bundle, *, pending_predecessors=None):
        """Coherent external-data variant, never a write to original custody."""
        custody = bundle['originalCustody']
        previous = None
        dispatch_predecessor = None
        pending_origins = {}
        for index,event in enumerate(custody['events']):
            event['previousSHA256'] = previous
            if 'claimSHA256' in event['state']:
                event['state']['claimSHA256'] = digest(custody['claim'])
            pending = event['state'].get('pending')
            if type(pending) is dict and 'stateSHA256' in pending:
                identity = (pending.get('schema'),pending.get('claimId'))
                if event['kind'] in ('owner-intent','cli-intent','final-owner-intent'):
                    pending['stateSHA256'] = previous
                    if all(type(v) is str for v in identity):
                        pending_origins[identity] = previous
                elif all(type(v) is str for v in identity) and identity in pending_origins:
                    pending['stateSHA256'] = pending_origins[identity]
                if pending_predecessors is not None and index in pending_predecessors:
                    pending['stateSHA256'] = pending_predecessors[index]
            dispatch = event['state'].get('dispatch')
            if event['kind'] == 'dispatch-intent':
                dispatch_predecessor = previous
            if dispatch is not None and 'stateSHA256' in dispatch:
                dispatch['stateSHA256'] = dispatch_predecessor
            previous = digest(event)
        custody['headSHA256'] = previous

    def test_never_started_original_bindings_through_both_operators_and_cleanup(self):
        field = 'attemptTrustBindingSHA256'
        # Every original state and present dispatch is tested independently.
        # Additional malformed cases target the claim, an intermediate and the
        # final state/dispatch. All hashes and each synthetic original capture
        # follow the variant, so only the real semantic consumer can reject it.
        invalid = {'missing': None, 'short': '0'*63, 'uppercase': 'A'*64,
                   'null': None, 'boolean': True, 'integer': 1,
                   'list': [], 'object': {}}
        for stdout in (False, True):
            for sealed in (False, True):
                with self.subTest(stdout=stdout, sealed=sealed):
                    p = self.pipeline(withdrawal_delay=3)
                    leaf = ('next_operator_orchestration_stdout.source-only.py' if stdout
                            else 'next_operator_orchestration.source-only.py')
                    e = self.execution(p,operator_leaf=leaf)
                    e.reserve(); e.capture_key(); e.bind()
                    if sealed:
                        e.provision_and_seal()
                    bundle = e.never_started_bundle()
                    original = canonical(bundle)
                    ledger_head = e.head()
                    binding = digest(p.binding)
                    def validate(value):
                        before = canonical(value)
                        try:
                            return e.validate_result_bundle(value,never_started=True)
                        finally:
                            self.assertEqual(canonical(value),before)
                    control = validate(bundle)
                    receipt = control['validationReceipt']
                    self.assertEqual(receipt[field],binding)
                    self.assertEqual(receipt['sealedModelSHA256'],digest(e.sealed) if sealed else None)
                    self.assertEqual(receipt['originalClaimSHA256'],digest(bundle['originalCustody']['claim']))
                    self.assertEqual(receipt['originalJournalHeadSHA256'],ledger_head)
                    self.assertEqual(receipt['bundleSHA256'],digest(bundle))
                    self.assertFalse(receipt['acceptancePassed'])
                    self.assertEqual([receipt[k] for k in ('jobCount','artifactCount',
                        'approvedDeploymentCount','chargedCreateAttempts','refund','remainingDispatches')],[0]*6)
                    self.assertEqual([receipt[k] for k in ('oldReservationsRetained',
                        'newReservationRetained','lifetimeReservationCeiling')],[176,32,208])
                    self.assertFalse(receipt['executionAuthorityGranted'])
                    self.assertFalse(receipt['externalAuthenticityVerifiedByModel'])
                    events = bundle['originalCustody']['events']
                    dispatch_indexes = [i for i,event in enumerate(events)
                                        if event['state'].get('dispatch') is not None]
                    self.assertGreater(len(dispatch_indexes),1)
                    targets = [('claim',None)] + [('state',i) for i in range(len(events))]
                    targets += [('dispatch',i) for i in dispatch_indexes]
                    edge_targets = {('claim',None),('state',1),('state',len(events)-1),
                                    ('dispatch',dispatch_indexes[0]),('dispatch',dispatch_indexes[-1])}
                    for kind,index in targets:
                        variants = {'different': '0'*64}
                        if (kind,index) in edge_targets:
                            variants.update(invalid)
                        for variant,value in variants.items():
                            with self.subTest(kind=kind,index=index,variant=variant):
                                changed = deepcopy(bundle)
                                custody = changed['originalCustody']
                                target = custody['claim'] if kind=='claim' else custody['events'][index]['state']
                                if kind=='dispatch':
                                    target = target['dispatch']
                                if variant=='missing':
                                    del target[field]
                                else:
                                    target[field] = deepcopy(value)
                                self.rehash_original_custody(changed)
                                expected = 'original-'+kind+'-attempt-binding'
                                if kind=='claim' and variant=='missing':
                                    expected = 'closed-fields'
                                with self.assertRaisesRegex(ValueError,expected):
                                    validate(changed)
                    self.assertEqual(canonical(bundle),original)
                    self.assertEqual(e.head(),ledger_head)
                    # Valid original-action cleanup survives both withdrawal and
                    # main advancement; it must not acquire present eligibility.
                    p.withdraw(p.now+timedelta(seconds=3))
                    p.authority.advance_main(e.clock)
                    with self.assertRaises(ValueError):
                        p.m.admit_current_release(p.prepared,e.clock)
                    e.result = validate(bundle)
                    self.assertEqual(e.result,control)
                    if sealed:
                        e.retire()
                        closure = e.finalize()
                        self.assertEqual(closure['restoredPolicyModel']['policyState'],
                                         'preserved-v2-v6-default-v5')

    def test_never_started_pending_intent_custody_through_both_operators_and_cleanup(self):
        # Exhaust all binding values at every original pending position. Shape
        # cases cover each producer record, not a recursive search for key names.
        field = 'attemptTrustBindingSHA256'
        invalid = {'different': '0'*64, 'missing': None, 'short': '0'*63,
                   'uppercase': 'A'*64, 'null': None, 'boolean': True,
                   'integer': 1, 'list': [], 'object': {}}
        for stdout in (False,True):
            for sealed in (False,True):
                with self.subTest(stdout=stdout,sealed=sealed):
                    p = self.pipeline(withdrawal_delay=3)
                    leaf = ('next_operator_orchestration_stdout.source-only.py' if stdout
                            else 'next_operator_orchestration.source-only.py')
                    e = self.execution(p,operator_leaf=leaf)
                    e.reserve(); e.capture_key(); e.bind()
                    if sealed:
                        e.provision_and_seal()
                        self.assertTrue(e.hosted_result()['validationReceipt']['acceptancePassed'])
                    bundle = e.never_started_bundle()
                    original = canonical(bundle)
                    binding = digest(p.binding)
                    disk = {path.name:path.read_bytes() for path in e.folder.iterdir() if path.is_file()}
                    ledger_head = e.head()
                    def validate(value):
                        before = canonical(value)
                        try:
                            return e.validate_result_bundle(value,never_started=True)
                        finally:
                            self.assertEqual(canonical(value),before)
                    def refuse(changed,expected,*,pending_predecessors=None):
                        self.rehash_original_custody(changed,pending_predecessors=pending_predecessors)
                        custody = changed['originalCustody']
                        self.assertEqual(custody['claim'][field],binding)
                        previous = None
                        for event_index,event in enumerate(custody['events']):
                            self.assertEqual(event['previousSHA256'],previous)
                            pending = event['state'].get('pending')
                            if type(pending) is dict and 'stateSHA256' in pending and event['kind'] in ('owner-intent','cli-intent','final-owner-intent') and (pending_predecessors is None or event_index not in pending_predecessors):
                                self.assertEqual(pending['stateSHA256'],previous)
                            previous = digest(event)
                            self.assertEqual(event['state'][field],binding)
                            dispatch = event['state'].get('dispatch')
                            if dispatch is not None:
                                self.assertEqual(dispatch[field],binding)
                        self.assertEqual(custody['headSHA256'],previous)
                        with self.assertRaisesRegex(ValueError,expected):
                            validate(changed)
                    control = validate(bundle)
                    self.assertEqual(control['validationReceipt'][field],binding)
                    events = bundle['originalCustody']['events']
                    positions = [i for i,event in enumerate(events) if event['state']['pending'] is not None]
                    self.assertEqual(len(positions),21 if sealed else 1)
                    schemas = {events[i]['state']['pending']['schema'] for i in positions}
                    self.assertEqual(schemas,{'hikyo-158/next-owner-intent/v1',
                        *(['hikyo-158/next-cli-intent/v1'] if sealed else [])})
                    for index in positions:
                        for variant,value in invalid.items():
                            with self.subTest(pending=index,variant=variant):
                                changed = deepcopy(bundle)
                                pending = changed['originalCustody']['events'][index]['state']['pending']
                                if variant=='missing':
                                    del pending[field]
                                else:
                                    pending[field] = deepcopy(value)
                                refuse(changed,'original-pending-attempt-binding')
                    for index in positions:
                        pending = events[index]['state']['pending']
                        keys = ['boundSHA256','stateSHA256']
                        if 'preparationSHA256' in pending:
                            keys.append('preparationSHA256')
                        for key in keys:
                            with self.subTest(pending_reference=index,key=key):
                                changed = deepcopy(bundle)
                                changed['originalCustody']['events'][index]['state']['pending'][key] = '0'*64
                                refuse(changed,'original-pending-event-predecessor' if key=='stateSHA256'
                                       else 'original-pending-attempt-references',
                                       pending_predecessors={index:'0'*64} if key=='stateSHA256' else None)
                    # Every field is required in each actual producer's closed
                    # intent. Unknown schemas, extra fields and non-records also
                    # refuse even when all outer custody bindings remain valid.
                    representatives = {events[i]['state']['pending']['schema']:i for i in positions}
                    for schema,index in representatives.items():
                        pending = events[index]['state']['pending']
                        shape_cases = [('missing-'+key,key,None) for key in pending if key!=field]
                        shape_cases += [('extra','extraIntent',{'attemptTrustBindingSHA256':'0'*64}),
                            ('unknown-schema','schema','hikyo-158/unknown-intent/v1'),
                            ('null-schema','schema',None),('list-schema','schema',[]),
                            ('unknown-operation','operation','UnknownOperation')]
                        for label,key,value in shape_cases:
                            with self.subTest(schema=schema,shape=label):
                                changed = deepcopy(bundle)
                                target = changed['originalCustody']['events'][index]['state']['pending']
                                if label.startswith('missing-'):
                                    del target[key]
                                else:
                                    target[key] = deepcopy(value)
                                refuse(changed,'closed-fields|original-pending-')
                        for key in pending:
                            if key in (field,'schema','stateSHA256'):
                                continue
                            with self.subTest(schema=schema,malformed=key):
                                changed = deepcopy(bundle)
                                changed['originalCustody']['events'][index]['state']['pending'][key] = []
                                refuse(changed,'closed-fields|original-pending-')
                        for value in (None,[],True,1,'intent'):
                            with self.subTest(schema=schema,shape=repr(value)):
                                changed = deepcopy(bundle)
                                changed['originalCustody']['events'][index]['state']['pending'] = value
                                refuse(changed,'original-pending-event-predecessor' if value is None else 'original-pending-record')
                    # Close state slots and scalar/digest metadata too, so an
                    # extra nested intent cannot hide outside pending/dispatch.
                    for index in sorted({0,len(events)//2,len(events)-1}):
                        for slot in ('pending','dispatch'):
                            with self.subTest(state=index,missing=slot):
                                changed = deepcopy(bundle)
                                del changed['originalCustody']['events'][index]['state'][slot]
                                refuse(changed,'closed-fields')
                    for slot in events[-1]['state']:
                        if slot in (field,'pending','dispatch'):
                            continue
                        with self.subTest(state_shape=slot):
                            changed = deepcopy(bundle)
                            del changed['originalCustody']['events'][-1]['state'][slot]
                            refuse(changed,'closed-fields')
                    for slot in ('extraIntent','owner','completed','roles'):
                        with self.subTest(hidden_intent=slot):
                            changed = deepcopy(bundle)
                            state = changed['originalCustody']['events'][-1]['state']
                            state[slot] = {'attemptTrustBindingSHA256':'0'*64}
                            refuse(changed,'closed-fields|original-state-')
                    dispatch_index = next(i for i,event in enumerate(events) if event['state']['dispatch'] is not None)
                    for slot in events[dispatch_index]['state']['dispatch']:
                        if slot==field:
                            continue
                        with self.subTest(dispatch_shape=slot):
                            changed = deepcopy(bundle)
                            del changed['originalCustody']['events'][dispatch_index]['state']['dispatch'][slot]
                            refuse(changed,'closed-fields')
                    self.assertEqual(canonical(bundle),original)
                    self.assertEqual(e.head(),ledger_head)
                    self.assertEqual({path.name:path.read_bytes() for path in e.folder.iterdir() if path.is_file()},disk)
                    p.withdraw(p.now+timedelta(seconds=3))
                    p.authority.advance_main(e.clock)
                    with self.assertRaises(ValueError):
                        p.m.admit_current_release(p.prepared,e.clock)
                    e.result = validate(bundle)
                    self.assertEqual(e.result,control)
                    if sealed:
                        e.retire()
                        self.assertEqual(e.finalize()['restoredPolicyModel']['policyState'],
                                         'preserved-v2-v6-default-v5')
                        # Finalization produces a third pending record and a
                        # binding-bearing native preparation nested within it.
                        # Reconstruct actual post-cleanup custody without adding
                        # a lifecycle prohibition to historical result checking.
                        completed = e.never_started_bundle()
                        completed_original = canonical(completed)
                        completed_head = e.head()
                        completed_disk = {path.name:path.read_bytes() for path in e.folder.iterdir() if path.is_file()}
                        validate(completed)
                        final_positions = [i for i,event in enumerate(completed['originalCustody']['events'])
                                           if event['kind']=='final-owner-intent']
                        self.assertEqual(len(final_positions),2)
                        for index in final_positions:
                            for nested in (False,True):
                                for variant,value in invalid.items():
                                    with self.subTest(final_pending=index,native=nested,variant=variant):
                                        changed = deepcopy(completed)
                                        target = changed['originalCustody']['events'][index]['state']['pending']
                                        if nested:
                                            target = target['requiredNativePreparation']
                                        if variant=='missing':
                                            del target[field]
                                        else:
                                            target[field] = deepcopy(value)
                                        refuse(changed,'original-pending-native-attempt-binding' if nested
                                               else 'original-pending-attempt-binding')
                        for index in final_positions:
                            for key in ('boundPlanSHA256','sealedModelSHA256','originalClaimSHA256','stateSHA256'):
                                with self.subTest(final_reference=index,key=key):
                                    changed = deepcopy(completed)
                                    changed['originalCustody']['events'][index]['state']['pending'][key] = '0'*64
                                    refuse(changed,'original-pending-event-predecessor' if key=='stateSHA256'
                                           else 'original-pending-attempt-references',
                                           pending_predecessors={index:'0'*64} if key=='stateSHA256' else None)
                            for key in ('operation','targetARN','expectedPolicyId','currentDefaultVersionId',
                                        'targetVersionId','reviewedDocumentSHA256','newBoundaryCustodySHA256'):
                                with self.subTest(native_reference=index,key=key):
                                    changed = deepcopy(completed)
                                    changed['originalCustody']['events'][index]['state']['pending']['requiredNativePreparation'][key] = '0'*64
                                    refuse(changed,'original-pending-native-references')
                        for nested in (False,True):
                            index = final_positions[-1]
                            pending = completed['originalCustody']['events'][index]['state']['pending']
                            target = pending['requiredNativePreparation'] if nested else pending
                            for slot in [key for key in target if key!=field]+['extraIntent']:
                                with self.subTest(final_shape=slot,native=nested):
                                    changed = deepcopy(completed)
                                    record = changed['originalCustody']['events'][index]['state']['pending']
                                    if nested:
                                        record = record['requiredNativePreparation']
                                    if slot=='extraIntent':
                                        record[slot] = {field:'0'*64}
                                    else:
                                        del record[slot]
                                    refuse(changed,'closed-fields|original-pending-')
                        self.assertEqual(canonical(completed),completed_original)
                        self.assertEqual(e.head(),completed_head)
                        self.assertEqual({path.name:path.read_bytes() for path in e.folder.iterdir() if path.is_file()},completed_disk)

    def test_never_started_retained_pending_original_custody(self):
        field = 'attemptTrustBindingSHA256'
        for stdout in (False,True):
            for cli in (False,True):
                with self.subTest(stdout=stdout,cli=cli):
                    p = self.pipeline()
                    leaf = ('next_operator_orchestration_stdout.source-only.py' if stdout
                            else 'next_operator_orchestration.source-only.py')
                    e = self.execution(p,operator_leaf=leaf)
                    e.reserve(); e.capture_key(); e.bind()
                    if cli:
                        def stop_after_write(execution,count):
                            raise RuntimeError('synthetic-stop-after-real-cli-intent')
                        with self.assertRaisesRegex(RuntimeError,'Provision held'):
                            e.provision_and_seal(after_write=stop_after_write)
                    else:
                        m = p.m
                        e.controller.arm_owner('DeletePolicyVersion',{'PolicyArn':m.SETUP,'VersionId':'v1'},
                            e.bundle('owner-DeletePolicyVersion',e.active('original',boundary=False)))
                        with e.ledger.lock():
                            e.controller._hold('synthetic-held-pending-original')
                    bundle = e.never_started_bundle()
                    original = canonical(bundle)
                    disk = {path.name:path.read_bytes() for path in e.folder.iterdir() if path.is_file()}
                    control = e.validate_result_bundle(bundle,never_started=True)
                    self.assertEqual(control['validationReceipt'][field],digest(p.binding))
                    events = bundle['originalCustody']['events']
                    self.assertEqual(events[-1]['kind'],'held')
                    self.assertIsNotNone(events[-1]['state']['pending'])
                    self.assertEqual(events[-1]['state']['pending'],events[-2]['state']['pending'])
                    for index in (len(events)-2,len(events)-1):
                        for key in (field,'argumentsSHA256' if cli else 'proofSHA256','stateSHA256'):
                            with self.subTest(retained=index,key=key):
                                changed = deepcopy(bundle)
                                changed['originalCustody']['events'][index]['state']['pending'][key] = '0'*64
                                self.rehash_original_custody(changed,
                                    pending_predecessors={index:'0'*64} if key=='stateSHA256' else None)
                                before = canonical(changed)
                                with self.assertRaisesRegex(ValueError,'original-pending-'):
                                    e.validate_result_bundle(changed,never_started=True)
                                self.assertEqual(canonical(changed),before)
                    for index in (len(events)-2,len(events)-1):
                        with self.subTest(omitted_pending=index):
                            changed = deepcopy(bundle)
                            changed['originalCustody']['events'][index]['state']['pending'] = None
                            self.rehash_original_custody(changed)
                            with self.assertRaisesRegex(ValueError,'original-pending-'):
                                e.validate_result_bundle(changed,never_started=True)
                    self.assertEqual(canonical(bundle),original)
                    self.assertEqual({path.name:path.read_bytes() for path in e.folder.iterdir() if path.is_file()},disk)

    def test_failed_hosted_result_with_original_create_custody_through_both_operators(self):
        for stdout in (False,True):
            with self.subTest(stdout=stdout):
                p = self.pipeline()
                leaf = ('next_operator_orchestration_stdout.source-only.py' if stdout
                        else 'next_operator_orchestration.source-only.py')
                e = self.execution(p,operator_leaf=leaf)
                e.reserve(); e.capture_key(); e.bind(); e.provision_and_seal()
                receipt = e.hosted_result(failed=True)['validationReceipt']
                self.assertFalse(receipt['acceptancePassed'])
                self.assertEqual(receipt['attemptTrustBindingSHA256'],digest(p.binding))
                self.assertTrue(receipt['allCreateOutcomesReconciled'])
                self.assertGreater(receipt['chargedCreateAttempts'],0)
                self.assertEqual(receipt['newSecretsVerifiedAbsent'],1)
                e.retire()
                self.assertEqual(e.finalize()['reservationRefund'],0)

    def test_two_distinct_releases_same_implementation_full_real_chain(self):
        rows = []
        for version in (1, 2):
            with self.subTest(version=version):
                p = self.pipeline(version)
                self.prepare_adapter(p)
                leaf=('next_operator_orchestration_stdout.source-only.py' if version==1
                      else 'next_operator_orchestration.source-only.py')
                e = self.execution(p,operator_leaf=leaf)
                claim = e.reserve()
                self.assertEqual(claim['lifetimeReservations'], 208)
                e.capture_key(native=True)
                self.assertEqual(e.paired_receipt['outcome'], 'completed-public-action')
                self.assertEqual(e.paired_receipt['attemptTrustBindingSHA256'], digest(p.binding))
                self.assertEqual(e.holder_release['attemptTrustBindingSHA256'], digest(p.binding))
                self.assertEqual(e.holder_release['keyARN'], e.f.key_arn)
                self.assertEqual(e.holder_window['attemptTrustBindingSHA256'], digest(p.binding))
                e.bind()
                self.assertEqual(e.f.bound['attemptTrustBindingSHA256'], digest(p.binding))
                sealed = e.provision_and_seal()
                result = e.hosted_result()
                self.assertTrue(result['validationReceipt']['acceptancePassed'])
                self.assertEqual(result['validationReceipt']['passingRealTests'], 46)
                e.retire()
                closure = e.finalize()
                self.assertEqual(closure['restoredPolicyModel']['policyState'], 'preserved-v2-v6-default-v5')
                self.assertEqual(closure['reservationRefund'], 0)
                self.assertFalse(sealed['executionAuthorityGranted'])
                self.assertEqual(len(e.runner.writes), 35)
                with e.ledger.lock():
                    self.assertEqual(e.ledger.snapshot()[1]['phase'], 'owner-cleanup-closure-recorded')
                self.assertIs(p.m.RELEASE_ADMISSION, p.admission)
                self.assertIs(p.m, p.modules['model'])
                self.assertEqual(p.prepared['attemptTrustBindingSHA256'], digest(p.binding))
                self.assertTrue({'releaseCandidate', 'mergeEvidence', 'historicalHistory',
                                 'attemptTrustBinding'} <= set(p.authority.validated))
                rows.append((p.modules['implementation'], p.modules['executionMap'],
                             p.release, p.approval, p.config, p.binding))
        self.assertEqual(len(rows), 2)
        self.assertEqual(rows[0][:2], rows[1][:2])
        for index in range(2, 6):
            self.assertNotEqual(rows[0][index], rows[1][index])

    def test_same_bank_cannot_reserve_again_or_rebind(self):
        one, two = self.pipeline(1), self.pipeline(2)
        e = self.execution(one)
        original = e.reserve()
        with self.assertRaisesRegex(ValueError, 'fresh-empty-ledger'):
            e.reserve()
        other = self.execution(two, base=e.base)
        with self.assertRaisesRegex(ValueError, 'fresh-empty-ledger'):
            other.reserve()
        with other.ledger.lock(), self.assertRaisesRegex(ValueError, 'immutable-reservation-custody'):
            other.controller._state()
        with e.ledger.lock():
            self.assertEqual(e.ledger.claim_record(), original)
        with self.assertRaisesRegex(ValueError, 'one-immutable-release-configuration'):
            one.m.configure_release(two.admission, two.release_collection, two.binding,
                                    two.manifest, two.bootstrap)

    def test_old_owner_approval_rejects_changed_release_source(self):
        one, two = self.pipeline(1), self.pipeline(2)
        records = {**one.records, **two.records}
        changed = deepcopy(one.config)
        changed['releaseSHA256'] = digest(two.release)
        with self.assertRaisesRegex(ValueError, 'configuration-constituent-releaseSHA256'):
            ReleaseAdmission(changed, records, implementation=one.modules['implementation'],
                             historical_history=one.history, authority=one.authority)
        with self.assertRaises(ValueError):
            one.admission.source_manifest_checked(two.manifest)

    def test_withdrawal_and_main_advance_block_forward_preserve_exact_cleanup(self):
        p = self.pipeline(withdrawal_delay=3)
        e = self.execution(p)
        e.reserve()
        e.capture_key()
        e.bind()
        e.provision_and_seal()
        e.hosted_result()
        p.withdraw(p.now + timedelta(seconds=3))
        p.authority.advance_main(e.clock)
        self.assertEqual(p.authority.main_advanced_at,e.clock)
        p.m.sealed_checked(e.sealed)
        with self.assertRaises(ValueError):
            p.m.admit_current_release(p.prepared, e.clock)
        heads = p.m.ci_original_for_cleanup_checked(p.code, p.manifest, p.now, e.clock)
        self.assertEqual(heads, {'candidate': p.binding['candidateHeadSHA'], 'workflow': p.binding['mainSHA']})
        e.retire()
        closure = e.finalize()
        self.assertEqual(closure['restoredPolicyModel']['policyState'], 'preserved-v2-v6-default-v5')

    def test_withdrawal_between_provision_writes_stops_next_forward(self):
        p = self.pipeline()
        e = self.execution(p)
        e.reserve()
        e.capture_key()
        e.bind()
        def withdraw_after_first_write(execution, count):
            if count == 1:
                execution.clock = p.now + timedelta(seconds=1)
                p.withdraw(execution.clock)
        with self.assertRaises(RuntimeError):
            e.provision_and_seal(after_write=withdraw_after_first_write)
        self.assertEqual(len(e.runner.writes), 1)
        self.assertEqual(e.runner.writes[0][0], 'create-open-id-connect-provider')
        with e.ledger.lock():
            self.assertTrue(e.ledger.snapshot()[1]['held'])

    def test_withdrawal_after_transmit_marker_stops_adjacent_creation(self):
        p=self.pipeline()
        e=self.execution(p)
        e.reserve();e.capture_key();e.bind()
        original_once=e.ledger.once
        def withdraw_at_marker(name,value):
            result=original_once(name,value)
            if name=='provision-02-transmit.once.json':
                e.clock=p.now+timedelta(seconds=1)
                p.withdraw(e.clock)
            return result
        e.ledger.once=withdraw_at_marker
        with self.assertRaises(RuntimeError):e.provision_and_seal()
        self.assertEqual(e.runner.writes,[('create-open-id-connect-provider',None)])
        self.assertTrue((e.folder/'provision-02-transmit.once.json').is_file())
        self.assertFalse((e.folder/'provision-02-response.once.json').exists())

    def test_admission_clock_crosses_absolute_cutoff_before_transport(self):
        p=self.pipeline();e=self.execution(p)
        e.reserve();e.capture_key();e.bind()
        original=p.authority.observe
        def advance(release,at,*,historical=False):
            result=original(release,at,historical=historical)
            if not historical and (e.folder/'provision-01-transmit.once.json').exists():
                e.clock=instant(e.f.bound['run']['provisioningCutoff'])+timedelta(seconds=1)
            return result
        p.authority.observe=advance
        with self.assertRaises(RuntimeError):e.provision_and_seal()
        self.assertEqual(e.runner.writes,[])
        self.assertTrue((e.folder/'provision-01-transmit.once.json').exists())
        self.assertFalse((e.folder/'provision-01-response.once.json').exists())

    def test_admission_clock_recomputes_send_timeout_and_retains_receipt(self):
        for late_response in (False,True):
            with self.subTest(late_response=late_response):
                p=self.pipeline();e=self.execution(p)
                e.reserve();e.capture_key();e.bind();e.refresh_synthetic_inventory()
                fixture=p.archive.fixture_module('test_next_fixture_execution.py')
                fixture.m=p.m;fixture.x=e.x
                runner=fixture.SyntheticRunner(e.f)
                timeouts=[]
                def transport(args,*,env,timeout):
                    timeouts.append(timeout)
                    result=runner(args,env=env,timeout=timeout)
                    if late_response:e.clock=p.now+timedelta(seconds=16)
                    return result
                cli=e.x.OfficialFixtureCLI(p.m,e.ledger,e.f.bound,
                    str(e.base/'private-operator/aws-local-login'),
                    {'PATH':'/opt/homebrew/bin:/usr/bin:/bin','LANG':'C','TMPDIR':str(e.base)},
                    ['f'*40],transport,e.explicit_clock)
                cli._active='provision'
                original=p.authority.observe
                def advance(release,at,*,historical=False):
                    result=original(release,at,historical=historical)
                    if not historical:e.clock=p.now+timedelta(seconds=10)
                    return result
                p.authority.observe=advance
                # A stricter explicit deadline stays within the unchanged run
                # cutoff while the independently captured originals stay fresh.
                deadline=stamp(p.now+timedelta(seconds=15))
                with e.ledger.lock():
                    head,state=e.controller._state()
                    def mutate():
                        return e.controller._mutation(cli,'provision',1,
                            'CreateOpenIDConnectProvider',None,head,state,deadline)
                    if late_response:
                        with self.assertRaisesRegex(ValueError,'mutation-error-or-deadline-after-forward'):mutate()
                    else:mutate()
                    outcome=e.ledger.read_once('provision-01-response.once.json')['outcome']
                self.assertEqual(runner.writes,[('create-open-id-connect-provider',None)])
                self.assertEqual(outcome['sentAt'],stamp(p.now+timedelta(seconds=10)))
                self.assertEqual(outcome['value']['OpenIDConnectProviderArn'],p.m.PROVIDER)
                self.assertEqual(timeouts,[5])
                self.assertTrue((e.folder/'provision-01-transmit.once.json').exists())

    def test_withdrawal_after_last_write_refuses_new_model_and_controller_seal(self):
        p=self.pipeline();e=self.execution(p)
        e.reserve();e.capture_key();e.bind()
        def withdraw_last(execution,count):
            if count==17:p.withdraw(p.now+timedelta(seconds=1))
        with self.assertRaisesRegex(ValueError,'current-release-ineligible'):
            e.provision_and_seal(after_write=withdraw_last)
        self.assertEqual(len(e.runner.writes),17)
        self.assertFalse((e.folder/'sealed-model.once.json').exists())
        with self.assertRaisesRegex(ValueError,'current-release-ineligible'):
            p.m.seal_actual_inventory(e.f.bound,e.sealing_records,
                {key:digest(value) for key,value in e.sealing_records.items()},explicit_now=e.clock)

    def test_preseal_authentication_withdrawal_and_clock_crossing_refuse_publication(self):
        for crossing in ('withdrawal','deadline'):
            with self.subTest(crossing=crossing):
                p=self.pipeline();e=self.execution(p)
                e.reserve();e.capture_key();e.bind()
                original=e.ledger.once
                def boundary(name,value):
                    result=original(name,value)
                    if name=='phase-seal-started.once.json':
                        if crossing=='withdrawal':p.withdraw(p.now+timedelta(seconds=1))
                        else:e.clock=instant(e.f.bound['run']['provisioningCutoff'])+timedelta(seconds=1)
                    return result
                e.ledger.once=boundary
                with self.assertRaises(ValueError):e.provision_and_seal()
                self.assertTrue((e.folder/'phase-seal-started.once.json').exists())
                self.assertFalse((e.folder/'sealed-model.once.json').exists())
                with e.ledger.lock():self.assertIsNone(e.ledger.snapshot()[1]['sealedSHA256'])

    def test_existing_seal_reconstructs_after_withdrawal_but_new_creation_refuses(self):
        p=self.pipeline(withdrawal_delay=3);e=self.execution(p)
        e.reserve();e.capture_key();e.bind();e.provision_and_seal()
        p.withdraw(p.now+timedelta(seconds=3));e.clock=p.now+timedelta(seconds=4)
        p.m.sealed_checked(e.sealed)
        with self.assertRaisesRegex(ValueError,'current-release-ineligible'):
            p.m.seal_actual_inventory(e.f.bound,e.sealing_records,
                {key:digest(value) for key,value in e.sealing_records.items()},explicit_now=e.clock)

    def test_real_prebind_and_bound_abort_after_native_create_and_withdrawal(self):
        for bound in (False, True):
            with self.subTest(bound=bound):
                p = self.pipeline(2 if bound else 1)
                self.prepare_adapter(p)
                e = self.execution(p)
                e.reserve()
                e.capture_key(native=True)
                if bound:
                    e.bind()
                p.withdraw(p.now + timedelta(seconds=1))
                e.clock = p.now + timedelta(seconds=2)
                p.authority.advance_main(e.clock)
                closure = e.abort(bound=bound)
                self.assertEqual(closure['retainedVersionIds'], ['v1', 'v2', 'v3', 'v4', 'v5'])
                self.assertEqual(closure['refund'], 0)
                self.assertEqual(e.cleanup_adapter_calls,['reconcile','arm','ticket','accept'])
                self.assertFalse(closure['executionAuthorityGranted'])
                self.assertTrue((e.folder / 'held.once.json').is_file())
                self.assertEqual(e.native_capture['holderFinal']['outcome'], 'validated-release')
                self.assertEqual(e.native_capture['attemptTrustBindingSHA256'], digest(p.binding))
                with self.assertRaises(ValueError):
                    e.controller.arm_dispatch(e.bundle('dispatch', e.f.records))
                with self.assertRaises(ValueError):
                    e.abort_builder.activate()

    def test_actual_admission_uses_durable_journal_CAS_across_restart(self):
        p = self.pipeline()
        _, heads, events = p.authority.journal.snapshot()
        self.assertEqual(len(events), 1)
        self.assertEqual(heads[digest(p.release)].status_sha256, digest(p.genesis))
        self.assertEqual(events[0]['decisionSHA256'], p.genesis['decisionCustodySHA256'])
        old = p.authority.journal
        p.authority.reopen_journal()
        self.assertIsNot(old, p.authority.journal)
        restarted = ReleaseAdmission(p.config, p.records, implementation=p.modules['implementation'],
                                     historical_history=p.history, authority=p.authority)
        restarted.admit_now(p.binding, p.manifest, p.now)
        p.withdraw(p.now + timedelta(seconds=1))
        p.authority.reopen_journal()
        _, heads, events = p.authority.journal.snapshot()
        self.assertEqual(len(events), 2)
        self.assertEqual(heads[digest(p.release)].status_sha256, digest(p.withdrawal))
        with self.assertRaises(ValueError):
            restarted.admit_now(p.binding, p.manifest, p.now + timedelta(seconds=2))
        self.assertEqual(len(p.authority.checkpoints), 2)
        self.assertEqual(len(list(p.authority.journal.folder.glob('event-*.once.json'))), 2)

    def test_missing_archive_is_failure_never_skipped(self):
        with patch.dict(os.environ, {}, clear=True):
            with self.assertRaisesRegex(ValueError, 'qualification-unavailable'):
                archive_from_environment()

    def test_historical_validator_asts_and_constants_are_literal(self):
        import ast
        p = self.pipeline()
        original = self.archive.read('aws-recovery-20261020/next-run-source-preparation/'
                                     'banked-series-first-attempt-source/next_fixture_model.source-only.py')
        before = ast.parse(original)
        after = ast.parse(Path(p.m.__file__).read_bytes())
        names = ('episode_semantics_checked', 'affirmative_merge_checked', 'held_history_checked',
                 'consumed_approval_checked', 'completed_history_checked')
        def functions(tree):
            return {node.name: ast.dump(node, include_attributes=False) for node in tree.body
                    if isinstance(node, ast.FunctionDef) and node.name in names}
        self.assertEqual(functions(before), functions(after))
        for name in ('BOOTSTRAP_1017', 'HISTORICAL_SOURCE_PINS', 'CONSUMED_SOURCE_PINS',
                     'CONSUMED_PROPOSAL_RAW_SHA', 'HELD_RAW_PINS', 'HELD_CAS_RAW_PINS',
                     'HELD_ARCHIVE_REFERENCES', 'COMPLETED_RAW_PINS', 'FROZEN'):
            self.assertEqual(getattr(p.m, name), getattr(p.fixture_source.m, name))

    def test_topological_source_pins_match_real_bytes_and_are_idempotent(self):
        repin(check=True)
        p = self.pipeline()
        for key, filename in (('MODEL_SHA256', 'core/next_fixture_model.source-only.py'),
                              ('MODEL_TEST_SHA256', 'core/test_next_fixture_model.py')):
            self.assertEqual(getattr(p.modules['executor'], key),
                             hashlib.sha256((Path(__file__).parent / filename).read_bytes()).hexdigest())

    def test_source_sized_nested_bounds_never_weakened(self):
        # Full current source bodies, including the 100KB CI workflow, are
        # collected through whole tree/blob originals with unchanged caps.
        p = self.pipeline(source_sized=True)
        self.assertGreater(sum(len(body.encode()) for body in p.code['workflowFiles'].values()), 300_000)
        self.assertGreater(len(canonical(p.prepared)), 400_000)
        self.prepare_adapter(p)
        e = self.execution(p)
        e.reserve()
        e.capture_key(native=True)
        e.bind()
        e.provision_and_seal()
        e.hosted_result()
        e.retire()
        closure = e.finalize()
        self.assertEqual(closure['restoredPolicyModel']['policyState'], 'preserved-v2-v6-default-v5')
        for value in (e.f.bound, e.sealed, e.result, closure):
            self.assertLessEqual(len(canonical(value)), 2_000_000)
        for value in (p.prepared, {'prepared': p.prepared, 'code': p.code}):
            encoded = canonical(value)
            self.assertLessEqual(len(encoded), 2_000_000)
        over = {'first': p.prepared, 'second': p.prepared, 'third': p.prepared}
        with self.assertRaisesRegex(ValueError, 'bound|size'):
            p.m.canonical(over)
        with self.assertRaises(ValueError):
            p.m.canonical({'items': [None] * 2049})
        nested = {}
        for _ in range(42):
            nested = {'child': nested}
        with self.assertRaises(ValueError):
            p.m.canonical(nested)


if __name__ == '__main__':
    unittest.main()
