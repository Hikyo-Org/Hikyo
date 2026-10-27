"""SOURCE ONLY. Finite unsealed abort cleanup; no transport or import I/O.

Root authenticates whole original public captures through the frozen registry.
This coordinator never creates resources, dispatches, resumes provisioning,
retries a write, fabricates a bound/sealed model, or executes an AWS command.
"""
import copy
from datetime import timedelta
import hashlib
import math
import re
from datetime import datetime, timezone
from pathlib import Path

# Copied reviewed S coordinator; only truthful inventory derivation is changed.
PINS = {'model': 'f40c07b2204c3305b434836015c99f30af4dfc893a0ce0e4f0318c17ea844a19', 'executor': '57c31fce3d373d770930e5e01f4b1a0fb27c4128983c78eb18b0c08a4f27a85c', 'ledger': 'c93ebd62f4304b880d29eb24812ccbf4c567d803970db1b11f27ff834f55a297', 'runtime': '018cb2a29568c47bd9fbed644be60182c448ebd21b2a9b253b2a5c5184a70d16', 'finalizer': '6680b842358be939f0953963c353d2f8392465a59a28db64c8f45dc45dff42ce', 'operator': '1274ce7b4d0e36f071c27ac39e93b374f45949ccde2d8a9a828213ad558a3265', 'metadata': '79940fc3d90ce8bb553c90f67eec679ff692536e53279dff2ab08099094a692b', 'keyFacts': '7a13f5fc03a9643b6535327b55851cb44923911a5422a7f002b871cb2c45db54', 'stdout': '8b644a4788fc2027dadb87ccb8dfefd5e7219a23397a1a70362fc74e1a8ee92a'}
OPERATIONS = ('ScheduleKeyDeletion', 'DeletePolicy', 'SetDefaultPolicyVersion')


class AbortCleanup:
    """One original charged claim; a separate fixed append-only cleanup journal.

    The sole addition to original storage is the canonical held marker, if
    absent. Its preexisting bytes, claim, events and all receipts remain intact.
    Actual Root release, native source authentication and physical once-forward
    adjacency remain external. An injected authority subclass alone is no proof.
    """
    def __init__(self, modules, original_ledger, prepared, execution_sources,
                 native_pins, cleanup_folder, explicit_clock, *, archive):
        self.modules = dict(modules); self.m = modules['model']; self.x = modules['executor']
        self.b = modules['runtime']; self.l = modules['ledger']; self.n = modules['finalizer']; self.o = modules['operator']
        m = self.m; m.exact(modules, PINS)
        for name, pin in PINS.items(): self.b.source_pin(modules[name], pin)
        m.require(type(original_ledger) is self.l.Ledger and callable(explicit_clock), 'original-ledger-explicit-clock')
        m.preparation_checked(prepared); self.x.source_manifest_checked(m, execution_sources)
        m.require(execution_sources['executorSHA256'] == PINS['executor']
                  and execution_sources['ledgerSHA256'] == PINS['ledger'], 'original-reviewed-execution-map')
        m.exact(native_pins, set(self.n.NATIVE_GUARD_PINS) | {'native_partial_kms_deletion_prepare.source-only.mjs', 'native_partial_kms_deletion_guard.source-only.js'})
        m.require(all(native_pins[k] == v for k, v in self.n.NATIVE_GUARD_PINS.items())
                  and all(m.sha(v) for v in native_pins.values()), 'reviewed-native-source-map')
        self.original = original_ledger; self.prepared = copy.deepcopy(prepared)
        self.sources = copy.deepcopy(execution_sources); self.native_pins = dict(native_pins); self.now = explicit_clock
        m.require(m.digest(archive) == 'c794a6c61e131c972ea5e201bc6e6189fdb974acea91be362dfb984d490a6c46', 'whole-original-native-policy-archive')
        self.archive = copy.deepcopy(archive)
        self.source_sha256 = hashlib.sha256(Path(__file__).read_bytes()).hexdigest()
        for name in ('native_partial_kms_deletion_prepare.source-only.mjs', 'native_partial_kms_deletion_guard.source-only.js'):
            m.require(hashlib.sha256((Path(__file__).resolve().parent/'native'/name).read_bytes()).hexdigest() == native_pins[name], 'actual-reviewed-cleanup-helper-bytes')
        reader_path = Path(__file__).resolve().parent/'native/native_next_key_custody_prearm_observer.source-only.js'
        reader_raw = reader_path.read_bytes()
        m.require(hashlib.sha256(reader_raw).hexdigest() == '5a68216b0e07b7fe207b1f361f2ab7192d3e28e2c13ae1adee8b20211e398700', 'exact-fresh-reader-realm-proof-source')
        self.reader_source = reader_raw.decode('utf-8')
        self.folder = self.l.checked_dir(Path(cleanup_folder))
        m.require(self.folder.parent == self.original.folder.parent
                  and self.folder.name == m.configuration_checked()['cleanupFolder'], 'fixed-separate-cleanup-folder')

    def _original(self):
        m = self.m; head, state = self.original.snapshot(); claim = self.original.claim_record()
        m.require(state is not None and claim['schema'] == 'hikyo-158/next-exclusive-reservation/v1'
                  and claim['approvalSHA256'] == m.SOURCE_PINS['approval'] and claim['preparationSHA256'] == m.digest(self.prepared)
                  and claim['attemptTrustBindingSHA256']==state['attemptTrustBindingSHA256']==self.prepared['attemptTrustBindingSHA256']
                  and claim['heldHistorySHA256'] == m.SOURCE_PINS['heldHistory'] and claim['completedHistorySHA256']==m.SOURCE_PINS['completedAttempt'] and claim['configurationSHA256']==m.digest(m.release_configuration_checked()) and claim['heldHistorySHA256']==m.digest(self.prepared['proposal']['inputs']['heldHistory'])
                  and m.same(claim['sourcePins'], self.sources) and m.same(claim['historicalPins'], m.SOURCE_PINS)
                  and claim['namespace'] == self.prepared['namespace'] and claim['automaticRetry'] is False
                  and type(claim['retainedReservations']) is int and claim['retainedReservations'] == 176
                  and type(claim['newReservationsCharged']) is int and claim['newReservationsCharged'] == 32
                  and type(claim['lifetimeReservations']) is int and claim['lifetimeReservations'] == 208
                  and type(claim['maximumDispatches']) is int and claim['maximumDispatches'] == 1, 'same-charged-original-claim')
        paths = sorted(self.original.folder.glob('*.once.json'))
        m.require(len(paths) <= 512, 'bounded-original-custody')
        files = {p.name: self.original.read_once(p.name) for p in paths}
        return {'claim': claim, 'headSHA256': head, 'state': state, 'files': files}

    def _journal(self):
        m = self.m; paths = sorted(self.folder.glob('event-*.once.json')); m.require(len(paths) <= 32, 'finite-abort-journal')
        previous = None; result = []
        for i, path in enumerate(paths, 1):
            row = self.l.read_public(path); m.exact(row, {'sequence', 'previousSHA256', 'kind', 'value'})
            m.require(path.name == f'event-{i:04d}.once.json' and type(row['sequence']) is int and row['sequence'] == i
                      and row['previousSHA256'] == previous, 'abort-journal-chain')
            previous = m.digest(row); result.append(row)
        return previous, result

    def _once(self, name, value):
        self.m.require(type(name) is str and name.endswith('.once.json') and '/' not in name and '\\' not in name, 'fixed-cleanup-record')
        self.l.write_exclusive(self.folder / name, value)

    def _append(self, expected, kind, value):
        head, rows = self._journal(); self.m.require(head == expected, 'cleanup-cas-conflict')
        self._once(f'event-{len(rows)+1:04d}.once.json', {'sequence': len(rows)+1, 'previousSHA256': head, 'kind': kind, 'value': value})

    def context(self, kind, original, cleanup_head):
        return {'attemptTrustBindingSHA256':self.prepared['attemptTrustBindingSHA256'],'schema': 'hikyo-158/partial-cleanup-context/v1', 'kind': kind, 'account': self.m.ACCOUNT, 'region': self.m.REGION,
                'preparationSHA256': self.m.digest(self.prepared), 'originalClaimSHA256': self.m.digest(original['claim']),
                'originalHeadSHA256': original['headSHA256'], 'cleanupHeadSHA256': cleanup_head,
                'cleanupSourceSHA256': self.source_sha256, 'sourcePins': dict(PINS), 'nativeSourcePins': dict(self.native_pins),
                'storage': {'original': str(self.original.folder), 'cleanup': str(self.folder),
                            'lock': str(self.original.parent / '.provisioning.lock')}}

    def describe_context(self, kind):
        with self.original.lock():
            original = self._original(); head, _ = self._journal()
            return self.context(kind, original, head), original

    def _proof(self, kind, proof, authority, original, head):
        m = self.m; m.exact(proof, {'schema', 'kind', 'observedAt', 'records', 'rawEvidence'})
        m.require(proof['schema'] == 'hikyo-158/next-external-proof-bundle/v1' and proof['kind'] == kind, 'closed-abort-proof-kind')
        m.fresh(proof['observedAt'], self.now())
        registry = self.b.OriginalCaptureRegistry(m, authority, self.now)
        checked = registry.authenticate(proof, self.context(kind, original, head), self.now())
        m.require(m.same(checked, proof), 'whole-root-original-release')
        records = copy.deepcopy(proof['records'])
        if type(records) is dict and set(records) == {'stdoutEvidenceIndex'}:
            index = records['stdoutEvidenceIndex']; m.require(type(index) is int and 0 <= index < len(proof['rawEvidence']), 'closed-authenticated-stdout-evidence-index')
            records = self.modules['stdout'].authenticated_stdout_wire(m, proof['rawEvidence'][index], 'phase-records')
            m.require(type(records) is dict, 'whole-authenticated-stdout-cleanup-records')
        self._decode_records(records)
        if kind == 'abort-reconcile-schedule':
            m.require(type(records.get('readback')) is dict,'closed-reconciliation-readback')
            self._decode_records(records['readback'])
        return records

    def _decode_records(self, records):
        # Only these closed phase fields carry whole lossless models. No generic
        # recursive decoding or caller-chosen path can turn data into authority.
        m = self.m
        for key in ('original','source'):
            if key in records and type(records[key]) is dict and records[key].get('schema') == self.o.WIRE_SCHEMA:
                records[key] = self.o.decode_tool_wire(m,records[key],'phase-records')
        if type(records.get('original')) is dict:
            for key,value in list(records['original'].get('files',{}).items()):
                if type(value) is dict and value.get('schema') == self.o.WIRE_SCHEMA:
                    records['original']['files'][key] = self.o.decode_tool_wire(m,value,'phase-records')

    def _same_original(self, left, right):
        m = self.m; m.exact(left, {'claim', 'headSHA256', 'state', 'files'})
        m.exact(left['files'], right['files'])
        return all(m.same(left[k], right[k]) for k in ('claim', 'headSHA256', 'state')) and all(m.same(left['files'][k], v) for k, v in right['files'].items())

    def activate(self, proof, authority):
        with self.original.lock():
            original = self._original(); head, rows = self._journal()
            records = self._proof('abort-activate', proof, authority, original, head)
            self.m.exact(records, {'original'}); self.m.require(self._same_original(records['original'], original) and not rows, 'fresh-exact-original-abort-anchor')
            try: self.original.read_once('held.once.json')
            except FileNotFoundError:
                self.original.once('held.once.json', {'reason': 'partial-setup-abort-only', 'automaticRetry': False})
            # Burn the abort anchor before its event; partial persistence cannot replay.
            anchor = {'originalClaimSHA256': self.m.digest(original['claim']), 'preparationSHA256': self.m.digest(self.prepared),
                      'originalFilePins': {k: self.m.digest(v) for k, v in original['files'].items()},
                      'activatedAt': self.m.stamp(self.now()), 'refund': 0, 'newSpendingAuthorized': False}
            self._once('abort-anchor.once.json', anchor); self._append(head, 'abort-frozen', anchor)
            return copy.deepcopy(anchor)

    def _anchored(self, original):
        m = self.m; anchor = self.l.read_public(self.folder / 'abort-anchor.once.json')
        m.require(anchor['originalClaimSHA256'] == m.digest(original['claim']) and anchor['preparationSHA256'] == m.digest(self.prepared)
                  and 'held.once.json' in original['files'], 'original-claim-permanently-held')
        for name, pin in anchor['originalFilePins'].items():
            m.require(name in original['files'] and m.digest(original['files'][name]) == pin, 'original-custody-never-rewritten')
        # The unchanged retirement may append genuine retire custody; no creator may resume.
        m.require(not any(k not in anchor['originalFilePins'] and (k.startswith('provision-') or k.startswith('dispatch-')
                      or k.startswith('owner-')) for k in original['files']), 'no-new-creation-dispatch-after-abort')
        return anchor

    def _abort_key_origin(self, custody, owner_intent, transmit):
        m = self.m
        if type(custody) is dict and custody.get('schema') == 'hikyo-158/cleanup-only-key-origin/v1':
            m.require(custody['claimId'] == owner_intent['claimId']
                      and m.same(custody['intent']['request'], owner_intent['request'])
                      and transmit['intentSHA256'] == m.digest(owner_intent), 'cleanup-only-original-intent-transmit')
            return self.modules['keyFacts'].origin_checked(m, self.prepared, custody, explicit_now=self.now())
        m.exact(custody, {'schema','preparationSHA256','claimId','intent','approvedCreateBody','nativeCreate','readback'})
        m.require(custody['preparationSHA256'] == m.digest(self.prepared) and custody['claimId'] == owner_intent['claimId'], 'abort-original-key-reference')
        intent = custody['intent']; m.exact(intent, {'operation','account','region','actorARN','maximumForwards','request'})
        m.require(intent['operation'] == 'CreateKey' and intent['account'] == m.ACCOUNT and intent['region'] == m.REGION
                  and intent['actorARN'] == self.prepared['records']['owner']['ownerContext']['principalARN']
                  and type(intent['maximumForwards']) is int and intent['maximumForwards'] == 1 and m.same(intent['request'],owner_intent['request']), 'abort-actual-create-intent')
        self.x.FixtureController._key_request(self,intent['request'])
        body = custody['approvedCreateBody']; m.require(type(body) is str and 2 <= len(body.encode()) <= 32768,'abort-original-create-body')
        parsed = m.strict_json(body.encode()); m.exact(parsed, set(intent['request'])); m.require(type(parsed['Policy']) is str,'native-policy-wire-string')
        parsed['Policy'] = m.strict_json(parsed['Policy'].encode()); m.require(m.same(parsed,intent['request']),'original-native-create-wire')
        row = custody['nativeCreate']
        m.exact(row, {'schema','manifestSHA256','manifest','slot','action','keyARN','guardAtForward','status','responseEndpoint','sentAtMS','receivedAtMS',
                     'requestBodyBytes','requestBodySHA256','rawResponse','rawResponseBytes','rawResponseSHA256','requestId','requestIdSource','requestIdCaptureState',
                     'JSONParsedByObserver','providerAPIsCalledByObserver','ownerAuthenticatedByObserver','publicSourceAuthenticatedByObserver','writeAuthorityGrantedByObserver'})
        manifest = row['manifest']; expected = {'schema':'hikyo-158/next-key-observer-manifest/v1','mode':'create','account':m.ACCOUNT,'region':m.REGION,
                    'endpoint':'https://kms.eu-central-1.amazonaws.com/','publicMainSHA256':'20a50d3f9e11cdae453b0f764689073f2adc6c644e6ac2a08660be5b1b2227a2',
                    'preparationSHA256':m.digest(self.prepared),'ownerContextSHA256':m.digest(self.prepared['records']['owner']['ownerContext']),
                    'intentSHA256':m.digest({k:custody[k] for k in ('preparationSHA256','claimId','intent')}),'claimId':custody['claimId'],
                    'wireBodySHA256':hashlib.sha256(body.encode()).hexdigest(),'wireBodyBytes':len(body.encode()),
                    'notBeforeMS':manifest.get('notBeforeMS'),'expiresAtMS':manifest.get('expiresAtMS'),'newKeyARN':None,'createReceiptSHA256':None}
        m.require(m.same(manifest,expected) and row['manifestSHA256'] == m.digest(manifest) and row['schema'] == 'hikyo-158/next-key-native-response/v1'
                  and row['slot'] == 'create' and row['action'] == 'CreateKey' and row['keyARN'] is None
                  and row['status'] == 200 and type(row['status']) is int and row['responseEndpoint'] == manifest['endpoint']
                  and row['requestId'] is None and row['requestIdCaptureState'] == 'unavailable' and row['requestIdSource'] == 'native-response-header:x-amzn-requestid', 'genuine-missing-header-native-create')
        for key in ('sentAtMS','receivedAtMS'): m.require(type(row[key]) is int,'native-create-explicit-ms')
        m.require(type(manifest['notBeforeMS']) is int and type(manifest['expiresAtMS']) is int
                  and manifest['notBeforeMS'] <= row['sentAtMS'] <= row['receivedAtMS'] < manifest['expiresAtMS'] <= manifest['notBeforeMS']+300000
                  and row['receivedAtMS']-row['sentAtMS'] <= 20000 and row['receivedAtMS'] <= int(self.now().timestamp()*1000), 'original-create-time-custody')
        guard = row['guardAtForward']; m.require(m.same(guard, {'schema':'hikyo-158/native-kms-xhr-write-status/v1','phase':'forwarded','claimConsumed':True,
                  'nativeSendInvocations':1,'intentSHA256':manifest['intentSHA256'],'wireBodySHA256':manifest['wireBodySHA256'],'endpoint':manifest['endpoint']})
                  and type(guard['nativeSendInvocations']) is int, 'original-one-create-forward')
        raw = row['rawResponse']; m.require(type(raw) is str and 2 <= len(raw.encode()) <= 32768 and type(row['rawResponseBytes']) is int
                  and row['rawResponseBytes'] == len(raw.encode()) and row['rawResponseSHA256'] == hashlib.sha256(raw.encode()).hexdigest()
                  and row['requestBodySHA256'] == manifest['wireBodySHA256'] and type(row['requestBodyBytes']) is int
                  and row['requestBodyBytes'] == manifest['wireBodyBytes'], 'whole-native-create-body-no-invented-header')
        value = m.strict_json(raw.encode()); m.exact(value, {'KeyMetadata'}); meta = value['KeyMetadata']
        required = {'Arn','KeyId','AWSAccountId','KeyState','Enabled','Origin','KeyUsage','KeySpec','MultiRegion','CreationDate','KeyManager'}
        optional = {'CustomerMasterKeySpec','EncryptionAlgorithms','Description','CurrentKeyMaterialId'}
        m.require(type(meta) is dict and required <= set(meta) <= required|optional and type(meta['KeyId']) is str
                  and re.fullmatch(r'[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12}',meta['KeyId']) is not None
                  and meta['Arn'] == f'arn:aws:kms:{m.REGION}:{m.ACCOUNT}:key/'+meta['KeyId'] and meta['Arn'] not in m.scheduled_key_closure(self.prepared['proposal'])
                  and meta['AWSAccountId'] == m.ACCOUNT and meta['KeyState'] == 'Enabled' and meta['Enabled'] is True and meta['MultiRegion'] is False
                  and meta['Origin'] == 'AWS_KMS' and meta['KeyUsage'] == 'ENCRYPT_DECRYPT' and meta['KeySpec'] == 'SYMMETRIC_DEFAULT' and meta['KeyManager'] == 'CUSTOMER'
                  and type(meta['CreationDate']) in (int,float) and math.isfinite(meta['CreationDate'])
                  and row['sentAtMS']/1000 <= meta['CreationDate'] <= row['receivedAtMS']/1000, 'actual-new-key-origin-without-request-id')
        if 'Description' in meta: m.require(meta['Description'] == intent['request']['Description'],'actual-key-description')
        if 'CustomerMasterKeySpec' in meta: m.require(meta['CustomerMasterKeySpec'] == 'SYMMETRIC_DEFAULT','actual-key-compat-spec')
        if 'EncryptionAlgorithms' in meta: m.require(meta['EncryptionAlgorithms'] == ['SYMMETRIC_DEFAULT'],'actual-key-encryption-algorithm')
        m.require(m.same(custody['readback'], {'metadata':meta,'policy':intent['request']['Policy'],'rotationEnabled':False,'aliases':[],'Truncated':False}), 'whole-origin-key-readback')
        for key in ('JSONParsedByObserver','providerAPIsCalledByObserver','ownerAuthenticatedByObserver','publicSourceAuthenticatedByObserver','writeAuthorityGrantedByObserver'):
            m.require(row[key] is False,'no-observer-authentication-claims')
        return meta['Arn'], datetime.fromtimestamp(row['sentAtMS']/1000,tz=timezone.utc)

    def _historical_teardown(self, realm, original, custody):
        """An immutable native unload fact plus fresh readback, never a new unload.

        No document IDs are invented. A genuine same-tab path-changing native
        navigation plus successful exact fresh-reader installation proves the
        nonconfigurable creator guard realm was replaced. Root authenticates
        every whole original; matching caller metadata alone is not authority.
        """
        m = self.m
        m.exact(realm, {'schema','observedAt','teardown','teardownSHA256','observationReceipt'})
        m.fresh(realm['observedAt'], self.now())
        history = realm['teardown']
        m.exact(history, {'schema','tabId','createOutcome','createRow','navigateAway','navigateBack','readerInstalled','readout'})
        m.require(history['schema'] == 'hikyo-158/same-tab-native-navigation-fresh-reader/v1'
                  and m.digest(history) == realm['teardownSHA256']
                  and type(realm['observationReceipt']) is dict and bool(realm['observationReceipt']), 'whole-historical-teardown-fresh-readback')
        files = original['files']
        m.require(original['state']['keyARN'] is None and original['state']['boundSHA256'] is None
                  and {name for name in files if name.startswith('owner-')} == {'owner-CreateKey-intent.once.json','owner-CreateKey-transmit.once.json'}
                  and not any(name.startswith(('dispatch-','provision-')) for name in files), 'historical-teardown-key-only-no-dispatch')
        tab = history['tabId']; m.require(type(tab) is str and 8 <= len(tab) <= 160, 'historical-original-tab')
        keys = {'id','threadId','runId','nodeId','providerThreadId','providerTurnId','nativeItemRef','parentItemId','ordinal','status','title','startedAt','completedAt','updatedAt','type','toolName','input','output'}
        rows = [history[key] for key in ('createOutcome','createRow','navigateAway','navigateBack','readerInstalled','readout')]
        for i, row in enumerate(rows):
            m.exact(row, keys)
            m.require(row['type'] == 'dynamic_tool' and row['status'] == 'completed'
                      and row['threadId'] == '9ac3a5c6-ec11-4281-b1f6-04add14759bc'
                      and row['toolName'] == ('t3-code.preview_navigate' if i in (2,3) else 't3-code.preview_evaluate')
                      and type(row['input']) is dict and row['input'].get('tabId') == tab and type(row['output']) is dict
                      and m.instant(row['startedAt']) <= m.instant(row['completedAt']) <= m.instant(row['updatedAt'])
                      and (i == 0 or m.instant(rows[i-1]['completedAt']) <= m.instant(row['startedAt'])), 'original-native-teardown-chronology')
            ref = row['nativeItemRef']; m.exact(ref, {'driver','nativeId','strength'})
            m.require(ref['driver'] == 'codex' and ref['strength'] == 'strong' and type(ref['nativeId']) is str
                      and 8 <= len(ref['nativeId']) <= 256, 'original-native-teardown-identity')
        m.require(len({row['nativeItemRef']['nativeId'] for row in rows}) == 6, 'distinct-original-native-teardown-events')
        create = custody['nativeCreate']; outcome = rows[0]['output'].get('value')
        m.require(type(outcome) is dict and m.same(outcome.get('create'), create)
                  and m.same(rows[1]['output'].get('value'), create), 'original-settled-create-and-whole-row')
        guard = outcome.get('guard'); observer = outcome.get('observer')
        m.require(type(guard) is dict and guard.get('schema') == 'hikyo-158/native-kms-xhr-write-status/v1'
                  and guard.get('phase') == 'settled' and guard.get('claimConsumed') is True
                  and type(guard.get('nativeSendInvocations')) is int and guard['nativeSendInvocations'] == 1
                  and type(guard.get('activeReads')) is int and guard['activeReads'] == 0
                  and guard.get('intentSHA256') == create['manifest']['intentSHA256']
                  and guard.get('wireBodySHA256') == create['requestBodySHA256']
                  and type(observer) is dict and observer.get('mode') == 'create' and observer.get('readySlots') == ['create']
                  and observer.get('error') is None and observer.get('pendingCaptures') == 0, 'original-settled-creator-no-active-read')
        away = 'https://eu-central-1.console.aws.amazon.com/console/home?region=eu-central-1'
        back = 'https://eu-central-1.console.aws.amazon.com/kms/home?region=eu-central-1#/kms/home'
        for row, url in ((rows[2],away),(rows[3],back)):
            m.require(m.same(row['input'], {'tabId':tab,'url':url,'readiness':'domContentLoaded'}), 'actual-path-changing-native-navigation')
            output = row['output']; control = output.get('control')
            m.require(output.get('tabId') == tab and output.get('url') == url and output.get('available') is True
                      and type(control) is dict and control.get('owner') == 'agent' and control.get('ownedByCaller') is True
                      and type(control.get('generation')) is int and control['generation'] >= 0, 'same-controlled-tab-native-navigation')
        readout = rows[5]['output'].get('value'); m.exact(readout, {'manifest','readRows'})
        m.require(m.same(readout['readRows'], custody['nativeReadRows']) and bool(readout['readRows'])
                  and all(m.same(row['manifest'], readout['manifest']) for row in readout['readRows']), 'whole-original-reader-key-rows')
        prefix = self.reader_source + '\ninstallHikyo158NextKeyCustodyObserverV1('
        expression = rows[4]['input'].get('expression')
        m.require(type(expression) is str and expression.startswith(prefix) and expression.endswith(')'), 'exact-pinned-fresh-reader-install-source')
        arguments = m.strict_json(expression[len(prefix):-1].encode()); m.exact(arguments, {'manifest','approvedCreateBody'})
        m.require(arguments['approvedCreateBody'] is None and m.same(arguments['manifest'], readout['manifest']), 'exact-original-readonly-install-manifest')
        status = rows[4]['output'].get('value')
        m.require(type(status) is dict and status.get('schema') == 'hikyo-158/next-key-observer-status/v1'
                  and status.get('mode') == 'readback' and status.get('error') is None
                  and type(status.get('pendingCaptures')) is int and status['pendingCaptures'] == 0
                  and status.get('readySlots') == []
                  and all(status.get(key) is False for key in ('providerAPIsCalledByObserver','ownerAuthenticatedByObserver','publicSourceAuthenticatedByObserver','writeAuthorityGrantedByObserver')), 'actual-fresh-reader-installed-with-marker-refusal')
        completed = m.instant(rows[2]['completedAt']); observed = m.instant(realm['observedAt'])
        m.require(datetime.fromtimestamp(create['receivedAtMS']/1000,tz=timezone.utc) <= m.instant(rows[0]['completedAt']) <= completed
                  and m.instant(rows[5]['completedAt']) <= observed <= self.now(), 'immutable-original-unload-before-current-readback')
        # origin_checked authenticates none of this; it only reconstructs the
        # whole factual source projection and failed original admission gates.
        self.modules['keyFacts'].origin_checked(m, self.prepared, custody, explicit_now=self.now())

    def _work(self, work, original, bound):
        m = self.m; m.exact(work, {'schema', 'observedAt', 'mode', 'run', 'jobs', 'artifacts', 'deployments', 'result', 'dispatchCorrelation'})
        m.fresh(work['observedAt'], self.now()); m.require(work['schema'] == 'hikyo-158/abort-no-active-work/v1', 'no-active-work-schema')
        files = original['files']; dispatched = 'dispatch-transmit.once.json' in files or 'operator-github-dispatch-contract.once.json' in files
        if not dispatched:
            m.require(work['mode'] == 'no-dispatch' and all(work[k] is None for k in ('run', 'jobs', 'artifacts', 'deployments', 'result', 'dispatchCorrelation')), 'no-dispatch-original-custody')
            return
        m.require(work['mode'] in ('never-started-cancelled', 'validated-terminal'), 'every-dispatch-reconciled')
        run = work['run']; m.exact(run, {'id', 'run_attempt', 'event', 'path', 'head_sha', 'status', 'conclusion'})
        m.require(type(run['id']) is int and run['id'] > 0 and type(run['run_attempt']) is int and run['run_attempt'] == 1
                  and run['event'] == 'workflow_dispatch' and run['path'] == '.github/workflows/aws-acceptance.yml'
                  and run['head_sha'] == self.prepared['workflowSHA'] and run['status'] == 'completed'
                  and run['conclusion'] in ('success', 'failure', 'cancelled'), 'exact-terminal-original-run')
        correlation = work['dispatchCorrelation']; m.exact(correlation, {'intentSHA256','transmitSHA256','runId','matchingRunIds','request','sentAt','receivedAt','forwardsObserved','sourceReceipt'})
        intent = files['dispatch-intent.once.json']; ticket = files['dispatch-transmit.once.json']
        m.require(correlation['intentSHA256'] == m.digest(intent) and correlation['transmitSHA256'] == m.digest(ticket)
                  and ticket['intentSHA256'] == m.digest(intent) and type(correlation['forwardsObserved']) is int and correlation['forwardsObserved'] == 1
                  and type(correlation['runId']) is int and correlation['runId'] == run['id'] and correlation['matchingRunIds'] == [run['id']]
                  and type(correlation['sourceReceipt']) is dict and bool(correlation['sourceReceipt'])
                  and m.same(correlation['request'], {k:intent[k] for k in ('path','ref','inputs')})
                  and m.instant(ticket['issuedAt']) <= m.instant(correlation['sentAt']) <= m.instant(correlation['receivedAt']) <= self.now(), 'unique-original-dispatch-to-run')
        if bound is not None: m.require(str(run['id']) == bound['run']['runId'], 'same-original-bound-run-id')
        for key in ('jobs', 'artifacts', 'deployments'):
            page = work[key]; m.exact(page, {'runId', 'attempt', 'total', 'pages', 'items'})
            m.require(page['runId'] == run['id'] and type(page['runId']) is int and type(page['attempt']) is int and page['attempt'] == 1
                      and type(page['total']) is int and type(page['items']) is list and page['total'] == len(page['items'])
                      and type(page['pages']) is list and len(page['pages']) >= 1, 'whole-complete-terminal-provider-pages')
            # Each immutable native API page is retained, in provider order, with explicit terminal cursor.
            collected = []
            for i, row in enumerate(page['pages'], 1):
                m.exact(row, {'page', 'items', 'nextPage'})
                m.require(type(row['page']) is int and row['page'] == i and type(row['items']) is list
                          and (row['nextPage'] is None if i == len(page['pages']) else type(row['nextPage']) is int and row['nextPage'] == i+1), 'terminal-pagination-custody')
                collected.extend(row['items'])
            m.require(m.same(collected, page['items']), 'whole-page-item-projection')
        if work['mode'] == 'never-started-cancelled':
            m.require(run['conclusion'] == 'cancelled' and all(work[k]['items'] == [] for k in ('jobs', 'artifacts', 'deployments'))
                      and work['result'] is None, 'strict-zero-job-artifact-approval-cancel')
        else:
            result = work['result']; m.require(bound is not None and type(result) is dict, 'real-bound-terminal-result-required')
            m.exact(result, {'validationReceipt', 'secretAbsenceProof'})
            receipt = result['validationReceipt']; proof = result['secretAbsenceProof']
            m.require(receipt['schema'] in ('hikyo-158/next-hosted-result-validation/v1', 'hikyo-158/next-never-started-cleanup-validation/v1')
                      and receipt['boundPlanSHA256'] == m.digest(bound) and receipt['noActiveFixtureWork'] is True
                      and receipt['allCreateOutcomesReconciled'] is True and receipt['remainingDispatches'] == 0
                      and receipt['refund'] == 0 and receipt['executionAuthorityGranted'] is False, 'actual-frozen-result-validation')
            checker = object.__new__(self.x.FixtureController); checker.m = m; checker.bound = bound; checker.prepared = self.prepared; checker.now = self.now
            checker._secret_absence(proof)
            m.require(proof['artifact']['runId'] == str(run['id']) and proof['artifact']['terminalConclusion'] == run['conclusion'], 'terminal-run-result-crosslinks')

    def _gate(self, records, original):
        m = self.m; m.exact(records, {'original', 'ownerContext', 'ownerMFA', 'setupUser', 'setupPolicy', 'keyCustody', 'newKey',
                                     'oldKeys', 'inventory', 'boundary', 'work', 'source', 'budget', 'quiescence'})
        m.require(self._same_original(records['original'], original), 'fresh-whole-original-custody'); self._anchored(original)
        now = self.now(); m.clock(now); m.require(m.instant(self.prepared['lease']['start']) <= now < m.instant(self.prepared['lease']['until']), 'original-finite-abort-lease')
        context = records['ownerContext']; m.exact(context, {'account', 'principalARN', 'source', 'observedAt', 'sourceReceipt'})
        m.fresh(context['observedAt'], now); baseline = self.prepared['records']['owner']
        m.require(context['account'] == m.ACCOUNT and context['principalARN'] == baseline['ownerContext']['principalARN']
                  and context['source'] == 'native-owner-console', 'same-current-owner')
        source = context['sourceReceipt']; m.require(source['schema'] == 'hikyo-158/native-owner-context-projection/v1'
                  and source['principalARN'] == context['principalARN'] and source['accountHeaderText'] == m.ACCOUNT, 'whole-native-owner-projection')
        auth = records['ownerMFA']; m.exact(auth, {'schema', 'account', 'principalARN', 'observedAt', 'methods', 'originalObservation'})
        m.fresh(auth['observedAt'], now); m.require(auth['schema'] == 'hikyo-158/owner-mfa-custody/v1' and auth['account'] == m.ACCOUNT
                  and auth['principalARN'] == context['principalARN'] and auth['methods'] == ['password', 'mfa']
                  and type(auth['originalObservation']) is dict and bool(auth['originalObservation']), 'actual-owner-mfa-original')
        m.require(m.same(records['setupUser'], baseline['setupUser']) and m.same(records['oldKeys'], baseline['oldKeys']), 'retained-user-mfa-old-four-schedules')
        quiet = records['quiescence']; m.exact(quiet, {'schema', 'observedAt', 'originalClaimSHA256', 'settledTransmits', 'ownerRealmTeardown', 'officialCLIProcesses', 'githubRequests'})
        m.fresh(quiet['observedAt'], now); m.require(quiet['schema'] == 'hikyo-158/abort-transport-quiescence/v1' and quiet['originalClaimSHA256'] == m.digest(original['claim']), 'actual-original-transport-quiescence')
        for name in ('officialCLIProcesses', 'githubRequests'):
            m.require(type(quiet[name]) is list and all(type(row) is dict and row.get('state') == 'terminated' and type(row.get('sourceReceipt')) is dict and bool(row['sourceReceipt']) for row in quiet[name]), 'no-active-owned-process-request')
        phases = [phase for phase in ('provision','retire') if 'phase-'+phase+'-started.once.json' in original['files']]
        m.require(len(quiet['officialCLIProcesses']) == len(phases), 'complete-original-cli-phase-coverage')
        for phase, row in zip(phases,quiet['officialCLIProcesses']):
            m.exact(row, {'phase','phaseStartSHA256','state','originalProcessIdentity','sourceReceipt'})
            m.require(row['phase'] == phase and row['phaseStartSHA256'] == m.digest(original['files']['phase-'+phase+'-started.once.json'])
                      and type(row['originalProcessIdentity']) is dict and bool(row['originalProcessIdentity']), 'actual-original-cli-handle-closure')
        dispatch_started = 'dispatch-transmit.once.json' in original['files']
        m.require(len(quiet['githubRequests']) == (1 if dispatch_started else 0), 'complete-original-dispatch-process-coverage')
        if dispatch_started:
            call = quiet['githubRequests'][0]; m.exact(call, {'state','transmitSHA256','originalProcessIdentity','sourceReceipt'})
            m.require(call['transmitSHA256'] == m.digest(original['files']['dispatch-transmit.once.json']) and type(call['originalProcessIdentity']) is dict
                      and bool(call['originalProcessIdentity']), 'actual-original-github-request-handle')
        realm = quiet['ownerRealmTeardown']
        if type(realm) is dict and realm.get('schema') == 'hikyo-158/immutable-native-realm-teardown-readback/v1':
            m.require(type(records['keyCustody']) is dict and records['keyCustody'].get('schema') == 'hikyo-158/cleanup-only-key-origin/v1', 'historical-teardown-factual-origin-only')
            self._historical_teardown(realm, original, records['keyCustody'])
        else:
            m.exact(realm, {'oldDocumentId', 'newDocumentId', 'completedAt', 'sourceReceipt'})
            m.require(type(realm['oldDocumentId']) is str and type(realm['newDocumentId']) is str and realm['oldDocumentId'] != realm['newDocumentId']
                      and type(realm['sourceReceipt']) is dict and bool(realm['sourceReceipt']), 'actual-controlled-realm-teardown')
            m.fresh(realm['completedAt'], now)
        transmits = {name: value for name, value in original['files'].items() if name.endswith('-transmit.once.json') and not name.startswith('retire-')}
        m.exact(quiet['settledTransmits'], transmits)
        for name, value in transmits.items():
            settled = quiet['settledTransmits'][name]; m.exact(settled, {'transmitSHA256', 'outcome', 'sourceReceipt'})
            m.require(settled['transmitSHA256'] == m.digest(value) and settled['outcome'] in ('applied', 'not-applied')
                      and type(settled['sourceReceipt']) is dict and bool(settled['sourceReceipt']), 'settled-original-forward-no-late-create')
        files = original['files']; bound = files.get('bound-plan.once.json')
        if bound is not None:
            m.bound_checked(bound, m.instant(bound['derivedAt'])); m.require(m.same(bound['preparation'], self.prepared), 'real-original-bound-only')
            m.require(original['state']['boundSHA256'] in (None, m.digest(bound)), 'bound-original-head-link')
            self.x.cleanup_code_checked(m, bound, records['source'], now)
        else:
            code = records['source']; m.exact(code, {'schema', 'observedAt', 'preparationSHA256', 'candidateCommit', 'workflowCommit', 'candidateFiles', 'workflowFiles', 'absentFiles'})
            m.fresh(code['observedAt'], now); prior = self.prepared['records']['code']
            m.require(code['schema'] == 'hikyo-158/immutable-preparation-code-readback/v1' and code['preparationSHA256'] == m.digest(self.prepared), 'prebind-immutable-reviewed-source')
            m.ci_original_for_cleanup_checked(prior, self.prepared['proposal']['sourceManifest'], m.instant(prior['observedAt']), now)
            for key in ('candidateCommit', 'workflowCommit', 'candidateFiles', 'workflowFiles', 'absentFiles'):
                m.require(m.same(code[key], prior[key]), 'unchanged-original-public-source')
        self._work(records['work'], original, bound)
        expected_requests = {'DeletePolicyVersion': {'PolicyArn': m.SETUP, 'VersionId': 'v1'}}
        if bound is not None:
            expected_requests.update(CreatePolicyVersion={'PolicyArn': m.SETUP, 'PolicyDocument': bound['documents']['setup-user.managed-policy.json'], 'SetAsDefault': True},
                                     CreatePolicy={'PolicyName': 'hikyo-158-role-boundary', 'Path': '/', 'PolicyDocument': bound['documents']['setup-role-boundary.json']})
        for op in self.x.OWNER_OPERATIONS:
            transmit = files.get('owner-'+op+'-transmit.once.json')
            if transmit is None: continue
            intent = files.get('owner-'+op+'-intent.once.json')
            m.require(type(intent) is dict and transmit['intentSHA256'] == m.digest(intent) and type(transmit['maximumRequests']) is int and transmit['maximumRequests'] == 1
                      and intent['preparationSHA256'] == m.digest(self.prepared) and m.instant(intent['armedAt']) <= m.instant(transmit['issuedAt']) <= now, 'exact-original-owner-forward')
            if op != 'CreateKey':
                m.require(bound is not None and intent['boundSHA256'] == m.digest(bound) and m.same(intent['request'], expected_requests[op]), 'exact-original-generated-owner-request')
        policy = records['setupPolicy']; ids = set(policy['versions']); default = policy['defaultVersionId']
        m.require((ids, default) in (({'v1','v2','v3','v4','v5'}, 'v5'), ({'v2','v3','v4','v5'}, 'v5'),
                  ({'v2','v3','v4','v5','v6'}, 'v6'), ({'v2','v3','v4','v5','v6'}, 'v5')), 'closed-real-partial-policy-stage')
        m.require('v6' not in ids or (bound is not None and 'owner-CreatePolicyVersion-transmit.once.json' in files), 'generated-v6-origin')
        m.setup_policy_checked(policy, self.prepared['proposal']['inputs']['iamClosure']['setupPolicy'], ids, default, bound['documents'] if bound else None)
        custody = records['keyCustody']; whole_key = records['newKey']; key = whole_key['metadata'] if type(whole_key) is dict else None
        if whole_key is not None:
            m.exact(whole_key, {'metadata', 'policy', 'rotationEnabled', 'aliases', 'Truncated'})
            m.require(custody is not None and m.same(whole_key['policy'], custody['readback']['policy']) and whole_key['rotationEnabled'] is False
                      and whole_key['aliases'] == [] and whole_key['Truncated'] is False, 'whole-current-key-policy-rotation-aliases')
        if 'owner-CreateKey-transmit.once.json' not in files:
            m.require(custody is None and key is None, 'no-new-key-without-original-forward')
        elif custody is None and key is None:
            m.require('owner-CreateKey-response.once.json' not in files and original['state']['keyARN'] is None, 'no-recorded-created-key-for-zero-forward')
            zero = quiet['settledTransmits']['owner-CreateKey-transmit.once.json']
            m.require(zero['outcome'] == 'not-applied', 'proven-zero-native-forward-required')
            evidence = zero['sourceReceipt']; m.exact(evidence, {'schema','documentId','intentRecord','guardStatus'})
            original_intent = files['owner-CreateKey-intent.once.json']; pure = evidence['intentRecord']
            m.exact(pure, {'preparationSHA256','claimId','intent'})
            m.require(evidence['schema'] == 'hikyo-158/actual-native-zero-forward/v1' and evidence['documentId'] == realm['oldDocumentId']
                      and pure['preparationSHA256'] == m.digest(self.prepared) and pure['claimId'] == original_intent['claimId']
                      and m.same(pure['intent']['request'],original_intent['request']), 'original-unloaded-guard-zero-forward-custody')
            guard = evidence['guardStatus']; m.require(type(guard) is dict and guard.get('schema') == 'hikyo-158/native-kms-xhr-write-status/v1'
                      and type(guard.get('nativeSendInvocations')) is int and guard['nativeSendInvocations'] == 0
                      and type(guard.get('claimConsumed')) is bool and guard.get('intentSHA256') == m.digest(pure)
                      and guard.get('endpoint') == 'https://kms.eu-central-1.amazonaws.com/', 'actual-native-guard-never-sent')
        else:
            m.require(type(custody) is dict and type(key) is dict, 'resolved-actual-new-key-required')
            intent = files['owner-CreateKey-intent.once.json']; ticket = files['owner-CreateKey-transmit.once.json']
            cleanup_only = custody.get('schema') == 'hikyo-158/cleanup-only-key-origin/v1'
            abort_origin = custody.get('schema') in ('hikyo-158/abort-key-origin/v1','hikyo-158/cleanup-only-key-origin/v1')
            if cleanup_only:
                m.require(bound is None and original['state']['keyARN'] is None
                          and 'owner-CreateKey-response.once.json' not in files, 'cleanup-only-unaccepted-unbound-factual-origin')
            if abort_origin: arn, created_at = self._abort_key_origin(custody, intent, ticket)
            else:
                arn = m.key_checked(custody, self.prepared['proposal'], m.instant(custody['observedAt'])); created_at = m.instant(custody['sentAt'])
            m.require(custody['preparationSHA256'] == m.digest(self.prepared) and custody['claimId'] == intent['claimId']
                      and custody['intent']['actorARN'] == baseline['ownerContext']['principalARN']
                      and m.same(custody['intent']['request'], intent['request']) and ticket['intentSHA256'] == m.digest(intent)
                      and m.instant(ticket['issuedAt']) <= created_at, 'original-new-key-once-custody')
            if 'owner-CreateKey-response.once.json' in files:
                saved = files['owner-CreateKey-response.once.json']
                if abort_origin:
                    m.require(saved['intentSHA256'] == m.digest(intent) and m.same(saved['response'].get('KeyMetadata'),custody['readback']['metadata'])
                              and saved['response'].get('RequestId') is None and saved['forwardsObserved'] == 1 and type(saved['forwardsObserved']) is int, 'persisted-failed-create-custody-retained')
                else: m.require(m.same(saved['keyProof'], custody), 'persisted-create-response-unchanged')
            base = custody['readback']['metadata']
            m.require(original['state']['keyARN'] in (None,arn),'same-original-key-cas-custody')
            if bound is not None: m.require(bound['run']['keyARN'] == arn and m.same(bound['records']['key']['readback']['metadata'],base), 'genuine-bound-same-original-key')
            m.require(key.get('Arn') == arn and key.get('KeyId') == base['KeyId'], 'only-original-new-key-identity')
            expected = {k: v for k, v in base.items() if k not in ('KeyState', 'Enabled')}
            m.require(all(m.same(key.get(k), v) for k, v in expected.items()) and set(key) in (set(base), set(base)|{'DeletionDate'})
                      and (key['KeyState'], key['Enabled']) in (('Enabled', True), ('Disabled', False), ('PendingDeletion', False))
                      and type(key['Enabled']) is bool, 'whole-current-new-key-metadata')
        deadline = min(m.instant(self.prepared['lease']['until']), m.instant(m.HORIZON),
                       m.instant(bound['run']['cleanupUntil']) if bound else created_at+timedelta(hours=4) if custody else m.instant(self.prepared['lease']['until']))
        m.require(now < deadline, 'immutable-first-key-four-hour-abort-deadline')
        inventory = self.modules['metadata'].checked_inventory(m,records['inventory'],preparation_sha256=m.digest(self.prepared),
            owner_context_sha256=m.digest(context),explicit_now=now,not_before=m.instant(quiet['observedAt']))
        def absent_role(name,row):
            return row['schema']=='hikyo-158/inventory-derived-role-absence/v1' and row['RoleName']==name
        survivors = [name for name,row in inventory['roles'].items() if not absent_role(name,row)]
        provider_present = inventory['provider']['schema']=='hikyo-158/inventory-observed-provider/v1'
        if survivors or provider_present:
            m.require(bound is not None and default == 'v6' and type(original['state']['boundaryPolicyId']) is str,
                      'partial-fixture-origin-must-be-reconciled')
            return {'stage': 'existing-retire-required' if 'phase-retire-started.once.json' not in files else 'retirement-outcome-reconciliation-required',
                    'boundPlanSHA256': m.digest(bound), 'survivingRoles': survivors, 'providerPresent': provider_present}
        m.exact(inventory['allRoles'], {'Roles', 'IsTruncated'})
        m.require(inventory['allRoles']['IsTruncated'] is False and type(inventory['allRoles']['Roles']) is list
                  and all(type(r) is dict and type(r.get('RoleName')) is str and r['RoleName'].casefold() not in {n.casefold() for n in m.ROLES}
                          for r in inventory['allRoles']['Roles']) and m.same(inventory['allProviders'], {'OpenIDConnectProviderList': []}), 'complete-owned-iam-absence')
        for role, row in inventory['roles'].items(): m.require(absent_role(role, row), 'every-fixture-role-absent')
        boundary = records['boundary']; m.exact(boundary, {'value', 'attachments', 'abortOrigin'})
        boundary_absent = m.same(boundary['value'], {'operation': 'GetPolicy', 'arn': m.BOUNDARY, 'errorCode': 'NoSuchEntity'})
        if not boundary_absent:
            value = boundary['value']; m.require(bound is not None and 'owner-CreatePolicy-transmit.once.json' in files, 'actual-boundary-create-origin')
            response = files.get('owner-CreatePolicy-response.once.json')
            origin = boundary['abortOrigin']
            if response is None:
                m.exact(origin, {'schema','intentSHA256','transmitSHA256','sentAt','receivedAt','nativeResponse','sourceReceipt'})
                owner_intent = files['owner-CreatePolicy-intent.once.json']; owner_ticket = files['owner-CreatePolicy-transmit.once.json']
                m.require(origin['schema'] == 'hikyo-158/abort-created-boundary-origin/v1' and origin['intentSHA256'] == m.digest(owner_intent)
                          and origin['transmitSHA256'] == m.digest(owner_ticket) and m.instant(owner_ticket['issuedAt']) <= m.instant(origin['sentAt'])
                          <= m.instant(origin['receivedAt']) <= now and type(origin['sourceReceipt']) is dict and bool(origin['sourceReceipt']), 'actual-otherwise-lost-native-boundary-outcome')
                native = origin['nativeResponse']; m.exact(native, {'Arn','PolicyId','DefaultVersionId'})
                m.require(native['Arn'] == m.BOUNDARY and native['DefaultVersionId'] == 'v1' and type(native['PolicyId']) is str
                          and re.fullmatch(r'ANPA[A-Z0-9]{17}',native['PolicyId']) is not None
                          and native['PolicyId'] not in (m.SETUP_ID,'ANPAROHA7PNS5QVDJKPIN'), 'unambiguous-new-boundary-response-id')
                response = {'response':native}
            else: m.require(origin is None, 'no-replacing-saved-boundary-outcome')
            m.require(type(value['policyId']) is str and re.fullmatch(r'ANPA[A-Z0-9]{17}',value['policyId']) is not None
                      and value['policyId'] not in (m.SETUP_ID,'ANPAROHA7PNS5QVDJKPIN') and value['policyId'] == response['response']['PolicyId']
                      and value['arn'] == m.BOUNDARY and value['defaultVersionId'] == 'v1'
                      and m.same(value['versions'], {'v1': {'isDefault': True, 'document': bound['documents']['setup-role-boundary.json']}}), 'captured-new-boundary-whole-document')
            m.require(m.same(boundary['attachments'], {'users': [], 'groups': [], 'roles': [], 'attachmentCount': 0, 'permissionsBoundaryUsageCount': 0})
                      and type(boundary['attachments']['attachmentCount']) is int and type(boundary['attachments']['permissionsBoundaryUsageCount']) is int,
                      'boundary-no-usage')
        else: m.require(boundary['attachments'] is None and boundary['abortOrigin'] is None, 'absent-boundary-no-attachments')
        return {'stage': 'owner-cleanup-ready', 'bound': bound, 'key': key, 'boundary': None if boundary_absent else boundary['value'], 'defaultVersionId': default, 'cleanupUntil': m.stamp(deadline),
                'budgetDiagnostic': self.n._budget(m, records['budget'], now)}

    def reconcile(self, proof, authority):
        with self.original.lock():
            original = self._original(); head, _ = self._journal()
            records = self._proof('abort-reconcile', proof, authority, original, head)
            result = self._gate(records, original)
            return {**{k:v for k,v in result.items() if k != 'bound'}, 'executionAuthorityGranted': False, 'automaticRetry': False, 'refund': 0}

    def arm(self, operation, proof, authority):
        m = self.m; m.require(operation in OPERATIONS, 'only-three-existing-cleanup-writes')
        with self.original.lock():
            original = self._original(); head, _ = self._journal()
            records = self._proof('abort-arm-'+operation, proof, authority, original, head); plan = self._gate(records, original)
            m.require(plan['stage'] == 'owner-cleanup-ready', 'survivors-or-retirement-outcomes-hold-owner-cleanup')
            key = plan['key']; boundary = plan['boundary']
            if operation == 'ScheduleKeyDeletion':
                m.require(key is not None and key['KeyState'] in ('Enabled', 'Disabled'), 'only-unscheduled-actual-new-key')
                m.require(not any('ScheduleKeyDeletion' == r.get('operation') for r in original['state']['completed']), 'original-schedule-never-retried')
                m.require('retire-18-transmit.once.json' not in original['files'], 'ambiguous-original-schedule-never-retried')
                request = {'KeyId': key['KeyId'], 'PendingWindowInDays': 7}
            else:
                self._scheduled(records, original)
                if operation == 'DeletePolicy':
                    m.require(boundary is not None, 'only-existing-unused-new-boundary'); request = {'PolicyArn': m.BOUNDARY}
                else:
                    m.require(boundary is None and plan['defaultVersionId'] == 'v6', 'boundary-absent-before-existing-v5-restore')
                    request = {'PolicyArn': m.SETUP, 'VersionId': 'v5'}
            intent = {'attemptTrustBindingSHA256':self.prepared['attemptTrustBindingSHA256'],'schema': 'hikyo-158/partial-cleanup-intent/v1', 'operation': operation, 'request': request,
                      'maximumForwards': 1, 'preparationSHA256': m.digest(self.prepared), 'originalClaimSHA256': m.digest(original['claim']),
                      'originalHeadSHA256': original['headSHA256'], 'armedAt': m.stamp(self.now()), 'cleanupUntil': plan['cleanupUntil'],
                      'reconciliationSHA256': m.digest(proof), 'keyCreateCustodySHA256': m.digest(records['keyCustody']),
                      'currentNewKeyMetadataSHA256': m.digest(key), 'oldKeysClosureSHA256': m.digest(records['oldKeys']),
                      'newBoundaryCustodySHA256': m.digest(records['boundary']['abortOrigin'] or original['files'].get('owner-CreatePolicy-response.once.json')),
                      'reviewedDocumentSHA256': m.digest(plan['bound']['documents']['setup-role-boundary.json' if operation == 'DeletePolicy' else 'setup-user.managed-policy.json']) if plan['bound'] else None,
                      'ownerContextSHA256': m.digest(records['ownerContext']), 'nativeSourcePins': dict(self.native_pins), 'executionAuthorization': False}
            self._once(operation+'-intent.once.json', intent); self._append(head, 'intent-'+operation, intent); return copy.deepcopy(intent)

    def ticket(self, operation, proof, authority):
        m = self.m; m.require(operation in OPERATIONS, 'closed-cleanup-ticket')
        with self.original.lock():
            original = self._original(); head, _ = self._journal(); intent = self.l.read_public(self.folder/(operation+'-intent.once.json'))
            records = self._proof('abort-ticket-'+operation, proof, authority, original, head); plan = self._gate(records, original)
            m.require(plan['stage'] == 'owner-cleanup-ready', 'fresh-cleanup-gate'); m.fresh(intent['armedAt'], self.now())
            # Recheck the exact once intent against current unchanged target metadata and policy stage.
            m.require(intent['currentNewKeyMetadataSHA256'] == m.digest(plan['key'])
                      and intent['oldKeysClosureSHA256'] == m.digest(records['oldKeys'])
                      and intent['ownerContextSHA256'] == m.digest(records['ownerContext']), 'fresh-identical-native-target-context')
            if operation != 'ScheduleKeyDeletion': self._scheduled(records, original)
            if operation == 'DeletePolicy': m.require(plan['boundary'] is not None, 'boundary-still-present-unused')
            if operation == 'SetDefaultPolicyVersion': m.require(plan['boundary'] is None and plan['defaultVersionId'] == 'v6', 'restore-still-applicable')
            ticket = {'attemptTrustBindingSHA256':self.prepared['attemptTrustBindingSHA256'],'schema': 'hikyo-158/partial-cleanup-native-contract/v1', 'operation': operation, 'request': intent['request'],
                      'intentSHA256': m.digest(intent), 'issuedAt': m.stamp(self.now()), 'maximumForwards': 1,
                      'requiredNativePreparation': copy.deepcopy(intent), 'nativeSourcePins': dict(self.native_pins),
                      'executionAuthorization': False}
            ticket['nativeHelperInput'] = self._native_input(operation, intent, ticket, records, plan)
            self._once(operation+'-transmit.once.json', ticket); self._append(head, 'transmit-'+operation, ticket); return ticket

    def accept(self, operation, proof, authority):
        m = self.m; m.require(operation in OPERATIONS, 'closed-cleanup-receipt')
        with self.original.lock():
            original = self._original(); self._anchored(original); head, _ = self._journal()
            intent = self.l.read_public(self.folder/(operation+'-intent.once.json')); ticket = self.l.read_public(self.folder/(operation+'-transmit.once.json'))
            row = self._proof('abort-receipt-'+operation, proof, authority, original, head)
            # Whole original public outcome survives semantic failure and prohibits replay.
            self._once(operation+'-response.once.json', row)
            m.exact(row, {'attemptTrustBindingSHA256','schema', 'operation', 'intentSHA256', 'ticketSHA256', 'request', 'sentAt', 'receivedAt',
                          'forwardsObserved', 'httpStatus', 'requestId', 'response', 'sourceReceipt'})
            m.require(row['attemptTrustBindingSHA256']==ticket['attemptTrustBindingSHA256']==intent['attemptTrustBindingSHA256']==self.prepared['attemptTrustBindingSHA256']
                      and row['schema'] == 'hikyo-158/partial-cleanup-response/v1' and row['operation'] == operation
                      and row['intentSHA256'] == m.digest(intent) and row['ticketSHA256'] == m.digest(ticket)
                      and m.same(row['request'], intent['request']) and type(row['forwardsObserved']) is int and row['forwardsObserved'] == 1
                      and type(row['httpStatus']) is int and row['httpStatus'] == 200 and type(row['requestId']) is str and 8 <= len(row['requestId']) <= 128
                      and m.instant(intent['armedAt']) <= m.instant(ticket['issuedAt']) <= m.instant(row['sentAt']) <= m.instant(row['receivedAt']) <= self.now()
                      and m.instant(row['sentAt'])-m.instant(ticket['issuedAt']) <= timedelta(seconds=30)
                      and m.instant(row['receivedAt']) < m.instant(intent['cleanupUntil']), 'actual-once-native-cleanup-outcome')
            source = row['sourceReceipt']
            if operation == 'ScheduleKeyDeletion':
                self._native_schedule(row, intent, ticket)
            else: m.require(type(source) is dict and source.get('cleanupIntentSHA256') == m.digest(intent)
                      and source.get('guardSourceSHA256') == self.native_pins['native_partial_kms_deletion_guard.source-only.js' if operation == 'ScheduleKeyDeletion'
                            else 'native_next_fixture_iam_guard.source-only.js' if operation == 'DeletePolicy' else 'native_setup_version_operations_guard.source-only.js']
                      and m.sha(source.get('helperPreparationSHA256')), 'whole-original-native-source-custody')
            if operation == 'ScheduleKeyDeletion': self._schedule_value(row['response'], intent['request']['KeyId'])
            else: m.require(m.same(row['response'], {}), 'native-empty-iam-outcome')
            self._append(head, 'response-'+operation, row); return copy.deepcopy(row)

    def _native_input(self, operation, intent, ticket, records, plan):
        m = self.m; at_ms = int(m.instant(ticket['issuedAt']).timestamp()*1000)
        if operation == 'ScheduleKeyDeletion':
            return {'newKeyARN': plan['key']['Arn'], 'newKeyId': plan['key']['KeyId'],
                    'originalCreateCustodySHA256': intent['keyCreateCustodySHA256'],
                    'reviewedCurrentWholeKeyMetadataSHA256': intent['currentNewKeyMetadataSHA256'],
                    'oldKeysClosureSHA256': intent['oldKeysClosureSHA256'], 'cleanupIntentSHA256': m.digest(intent),
                    'reviewedPreparationSHA256': m.digest(self.prepared), 'cleanupReconciliationSHA256': intent['reconciliationSHA256'],
                    'guardSourceSHA256': self.native_pins['native_partial_kms_deletion_guard.source-only.js'],
                    'helperSourceSHA256': self.native_pins['native_partial_kms_deletion_prepare.source-only.mjs'],
                    'notBeforeMS': at_ms, 'expiresAtMS': min(at_ms+300000, int(m.instant(intent['cleanupUntil']).timestamp()*1000)), 'captureTimeoutMS': 20000}
        common = {'operation': operation, 'approval': self.prepared['proposal']['inputs']['approval'],
                  'ownerContextSHA256': intent['ownerContextSHA256'], 'reviewedPreparationSHA256': m.digest(self.prepared)}
        policy = records['setupPolicy'] if operation == 'SetDefaultPolicyVersion' else records['boundary']['value']
        proof = {'schema': 'hikyo-158/next-setup-whole-policy-proof/v1' if operation == 'SetDefaultPolicyVersion' else 'hikyo-158/next-boundary-zero-use-proof/v1',
                 'account': m.ACCOUNT, 'observedAtMS': at_ms, 'ownerContextSHA256': intent['ownerContextSHA256'], 'policy': policy}
        if operation == 'DeletePolicy':
            proof['attachments'] = records['boundary']['attachments']
            return {**common, 'proof': proof, 'document': None, 'reviewedDocumentSHA256': intent['reviewedDocumentSHA256'],
                    'retirementProofSHA256': intent['reconciliationSHA256'], 'newBoundaryCustodySHA256': intent['newBoundaryCustodySHA256']}
        return {**common, 'proof': proof, 'reviewedV6DocumentSHA256': intent['reviewedDocumentSHA256'],
                'retirementProofSHA256': intent['reconciliationSHA256'],
                'archive': copy.deepcopy(self.archive)}

    def _native_schedule(self, row, intent, ticket, *, missing_header=False):
        m = self.m; source = row['sourceReceipt']
        fields = {'schema','slot','operation','request','httpStatus','responseEndpoint','sentAtMS','receivedAtMS',
                  'rawResponse','rawResponseBytes','rawResponseSHA256','requestBodyBytes','requestBodySHA256','requestId','requestIdSource',
                  'requestIdCaptureState','forwardsObserved','guardSourceSHA256','helperPreparationSHA256','manifestSHA256','manifest',
                  'JSONParsedByGuard','authenticationReadByGuard','providerAPIsInitiatedByGuard','externalAuthenticityVerifiedByGuard','writeAuthorityGrantedByGuard'}
        m.exact(source, fields); inputs = ticket['nativeHelperInput']
        # The helper serializes these two fields in this exact native insertion order.
        body = '{"KeyId":"'+intent['request']['KeyId']+'","PendingWindowInDays":7}'
        manifest = {'schema':'hikyo-158/partial-kms-deletion-manifest/v1','account':m.ACCOUNT,'region':m.REGION,
                    'endpoint':'https://kms.eu-central-1.amazonaws.com/',
                    'publicMainSHA256':'20a50d3f9e11cdae453b0f764689073f2adc6c644e6ac2a08660be5b1b2227a2', **inputs,
                    'requestBodySHA256':hashlib.sha256(body.encode()).hexdigest(),'requestBodyBytes':len(body.encode()),
                    'maximumNativeScheduleForwards':1,'maximumNativeDescribeForwardsPerKey':2}
        manifest['helperPreparationSHA256'] = m.digest(manifest)
        m.require(m.same(source['manifest'],manifest) and source['manifestSHA256'] == m.digest(manifest)
                  and source['helperPreparationSHA256'] == manifest['helperPreparationSHA256']
                  and source['guardSourceSHA256'] == inputs['guardSourceSHA256'], 'exact-reviewed-native-manifest')
        m.require(source['schema'] == 'hikyo-158/partial-kms-native-response/v1' and source['slot'] == 'schedule'
                  and source['operation'] == 'ScheduleKeyDeletion' and m.same(source['request'],row['request'])
                  and type(source['httpStatus']) is int and source['httpStatus'] == 200
                  and source['responseEndpoint'] == manifest['endpoint'] and source['requestId'] == row['requestId']
                  and source['requestIdSource'] == 'native-response-header:x-amzn-requestid'
                  and ((source['requestId'] is None and source['requestIdCaptureState'] == 'unavailable') if missing_header else source['requestIdCaptureState'] == 'captured')
                  and type(source['forwardsObserved']) is int and source['forwardsObserved'] == 1, 'exact-native-schedule-response')
        for key in ('sentAtMS','receivedAtMS','rawResponseBytes','requestBodyBytes'):
            m.require(type(source[key]) is int, 'exact-native-response-integer')
        m.require(source['sentAtMS'] == int(m.instant(row['sentAt']).timestamp()*1000)
                  and source['receivedAtMS'] == int(m.instant(row['receivedAt']).timestamp()*1000)
                  and manifest['notBeforeMS'] <= source['sentAtMS'] <= source['receivedAtMS'] < manifest['expiresAtMS']
                  and source['receivedAtMS']-source['sentAtMS'] <= manifest['captureTimeoutMS']
                  and source['requestBodyBytes'] == manifest['requestBodyBytes'] and source['requestBodySHA256'] == manifest['requestBodySHA256'], 'native-response-wire-time')
        raw = source['rawResponse']; m.require(type(raw) is str and 2 <= len(raw.encode()) <= 32768
                  and source['rawResponseBytes'] == len(raw.encode()) and source['rawResponseSHA256'] == hashlib.sha256(raw.encode()).hexdigest()
                  and m.same(m.strict_json(raw.encode()),row['response']), 'whole-actual-native-json-response')
        for key in ('JSONParsedByGuard','authenticationReadByGuard','providerAPIsInitiatedByGuard','externalAuthenticityVerifiedByGuard','writeAuthorityGrantedByGuard'):
            m.require(source[key] is False, 'native-no-authority-or-authentication-claims')

    def _schedule_value(self, value, key_id):
        m = self.m; m.exact(value, {'KeyId', 'DeletionDate', 'KeyState', 'PendingWindowInDays'})
        m.require(value['KeyId'] == f'arn:aws:kms:{m.REGION}:{m.ACCOUNT}:key/'+key_id and value['KeyState'] == 'PendingDeletion'
                  and type(value['PendingWindowInDays']) is int and value['PendingWindowInDays'] == 7
                  and type(value['DeletionDate']) in (int, float) and math.isfinite(value['DeletionDate']), 'exact-new-key-seven-day-schedule')

    def reconcile_schedule(self, proof, authority):
        """No send. Retain a burned schedule and its independently proven outcome.

        A fresh PendingDeletion read alone is insufficient. Missing-header
        native HTTP200 can use its preserved whole response. Lost/timeout/error
        captures need an actual externally authenticated provider outcome record
        containing the original request and response; no adapter is supplied.
        """
        m = self.m
        with self.original.lock():
            original = self._original(); head, _ = self._journal()
            records = self._proof('abort-reconcile-schedule',proof,authority,original,head)
            m.exact(records, {'readback','nativeOutcome','providerOutcome'})
            plan = self._gate(records['readback'],original); m.require(plan['stage'] == 'owner-cleanup-ready' and plan['key'] is not None
                      and plan['key']['KeyState'] == 'PendingDeletion', 'fresh-original-key-already-scheduled')
            intent = self.l.read_public(self.folder/'ScheduleKeyDeletion-intent.once.json')
            ticket = self.l.read_public(self.folder/'ScheduleKeyDeletion-transmit.once.json')
            outcome = records['nativeOutcome']
            if outcome is not None:
                m.require(records['providerOutcome'] is None,'single-reconciled-outcome-source')
                m.exact(outcome, {'schema','operation','intentSHA256','ticketSHA256','request','sentAt','receivedAt','forwardsObserved','httpStatus','requestId','response','sourceReceipt'})
                m.require(outcome['intentSHA256'] == m.digest(intent) and outcome['ticketSHA256'] == m.digest(ticket)
                          and m.same(outcome['request'],intent['request']) and outcome['requestId'] is None,'missing-header-burned-schedule')
                self._native_schedule(outcome,intent,ticket,missing_header=True); value = outcome['response']
            else:
                provider = records['providerOutcome']
                m.exact(provider, {'schema','intentSHA256','ticketSHA256','request','response','observedAt','originalRecord','requestPath','responsePath','eventAt'})
                m.fresh(provider['observedAt'],self.now())
                m.require(provider['schema'] == 'hikyo-158/actual-provider-schedule-outcome/v1'
                          and provider['intentSHA256'] == m.digest(intent) and provider['ticketSHA256'] == m.digest(ticket)
                          and m.same(provider['request'],intent['request']) and type(provider['originalRecord']) is dict
                          and m.instant(ticket['issuedAt']) <= m.instant(provider['eventAt']) <= m.instant(provider['observedAt']), 'actual-existing-provider-outcome-required')
                for path,key in ((provider['requestPath'],'request'),(provider['responsePath'],'response')):
                    m.require(type(path) is list and 1 <= len(path) <= 16,'explicit-original-provider-projection-path')
                    selected = provider['originalRecord']
                    for name in path:
                        m.require(type(selected) is dict and type(name) is str and name in selected,'actual-provider-original-field')
                        selected = selected[name]
                    m.require(m.same(selected,provider[key]),'whole-original-provider-outcome-projection')
                value = provider['response']
            self._schedule_value(value,intent['request']['KeyId'])
            m.require(plan['key']['DeletionDate'] == value['DeletionDate'],'fresh-same-actual-seven-day-outcome')
            result = {'schema':'hikyo-158/reconciled-existing-schedule/v1','intentSHA256':m.digest(intent),'ticketSHA256':m.digest(ticket),
                      'response':value,'proof':copy.deepcopy(proof),'noForwardIssued':True,'refund':0}
            self._once('ScheduleKeyDeletion-reconciled.once.json',result); self._append(head,'schedule-reconciled',result); return result

    def _scheduled(self, records, original):
        m = self.m; whole_key = records['newKey']; key = whole_key['metadata'] if whole_key is not None else None
        if key is None: return
        m.require(key['KeyState'] == 'PendingDeletion' and key['Enabled'] is False, 'new-key-scheduling-complete-before-owner-finalization')
        reconciled = self.folder/'ScheduleKeyDeletion-reconciled.once.json'
        path = self.folder/'ScheduleKeyDeletion-response.once.json'
        if reconciled.exists():
            row = self.l.read_public(reconciled); m.require(any(r['kind'] == 'schedule-reconciled' and m.same(r['value'],row) for r in self._journal()[1]), 'durable-reconciled-schedule-outcome')
            value = row['response']; self._schedule_value(value,key['KeyId'])
        elif path.exists():
            row = self.l.read_public(path); intent = self.l.read_public(self.folder/'ScheduleKeyDeletion-intent.once.json')
            self._schedule_value(row['response'], intent['request']['KeyId'])
            m.require(any(r['kind'] == 'response-ScheduleKeyDeletion' and m.same(r['value'], row) for r in self._journal()[1]), 'successfully-validated-schedule-custody')
            value = row['response']
        else:
            files = original['files']; row = files.get('retire-18-response.once.json'); intent = files.get('retire-18-intent.once.json'); transmit = files.get('retire-18-transmit.once.json')
            m.require(type(row) is dict and type(intent) is dict and type(transmit) is dict
                      and intent['operation'] == 'ScheduleKeyDeletion' and transmit['intentSHA256'] == m.digest(intent)
                      and row['outcome']['returncode'] == 0 and row['outcome']['stderrEmpty'] is True, 'actual-existing-retire-schedule-receipt')
            value = row['outcome']['value']; self._schedule_value(value, key['KeyId'])
        m.require(key['DeletionDate'] == value['DeletionDate'], 'fresh-describe-exact-actual-deletion-date')

    def complete(self, proof, authority):
        with self.original.lock():
            original = self._original(); head, _ = self._journal()
            records = self._proof('abort-complete', proof, authority, original, head); plan = self._gate(records, original)
            self.m.require(plan['stage'] == 'owner-cleanup-ready' and plan['boundary'] is None and plan['defaultVersionId'] == 'v5', 'exact-unsealed-abort-closure')
            self._scheduled(records, original)
            result = {'schema': 'hikyo-158/partial-setup-abort-closure/v1', 'preparationSHA256': self.m.digest(self.prepared),
                      'originalClaimSHA256': self.m.digest(original['claim']), 'readbackSHA256': self.m.digest(proof),
                      'retainedVersionIds': sorted(records['setupPolicy']['versions']), 'defaultVersionId': 'v5', 'retainedUserId': self.m.USER_ID,
                      'originalHeldRetained': True, 'refund': 0, 'lifetimeReservations': 208,
                      'approvalSHA256': self.m.SOURCE_PINS['approval'], 'heldHistorySHA256': self.m.SOURCE_PINS['heldHistory'], 'acceptancePassed': False,
                      'executionAuthorityGranted': False, 'newSpendingAuthorized': False, 'budgetDiagnostic': plan['budgetDiagnostic']}
            self._once('abort-closure.once.json', result); self._append(head, 'abort-closed', result); return result


if __name__ == '__main__':
    raise SystemExit('SOURCE ONLY: no execution entry point or Root authority supplied')
