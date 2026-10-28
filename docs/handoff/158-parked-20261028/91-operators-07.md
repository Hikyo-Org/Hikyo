# Prepared operator source archive, part 7/9

**ARCHIVE ONLY, PARKED, LIVE QUALIFICATION NOT ESTABLISHED.** This preserves the prepared small-route operators and their tests so local report deletion does not erase the implementation. The transcription normalizes machine-local paths to `<report-root>`, `<approval-bank>`, `<application-checkout>`, `<trusted-source-checkout>` or `<local-artifact>`. They are reconstruction placeholders, not working filesystem paths. Original and normalized digests are both recorded. All runtime/source pins and concrete bindings must be re-established before use. Do not copy this code directly into a live operation or claim that normalization preserved an old qualification. In particular the historical unattended helper is an agent interpretation of an overnight request; present parking overrides it, and all applicable source/Ready/policy/deployment/merge gates must be reconciled with current owner direction. No credentials, authentication storage or private challenge URLs are included.

## completion-c12/test_unattended_c12.py

Original machine-bound bytes SHA256 `56d593a01399172c2f9d2f230533cd2e8791a56cd79784e64d847d1578bec67e`; portable transcription SHA256 `56d593a01399172c2f9d2f230533cd2e8791a56cd79784e64d847d1578bec67e`. These differ when local bindings or typography were normalized.

```python
"""Local controls for the one explicit unattended c12 delegation."""
from datetime import datetime, timedelta, timezone
from pathlib import Path
import json
import tempfile
import unittest
from unittest.mock import patch

import unattended_c12 as subject
from bind_policies import bind_templates


class UnattendedC12(unittest.TestCase):
    def setUp(self):
        self.owner = json.loads((Path(__file__).parent / 'OWNER-UNATTENDED-REQUEST.json').read_text())
        self.now = datetime(2026, 10, 28, 6, 0, 0, 123000, tzinfo=timezone.utc)

    def test_exact_unattended_instruction_keeps_unavailability_truthful(self):
        window = subject.window_for_delegation(self.owner, self.now)
        self.assertFalse(window['ownerAvailableFirst40Minutes'])
        self.assertTrue(window['operatorOwnsExecutionAndCleanup'])
        self.assertNotIn('ownerReadyAt', window)
        self.assertEqual(window['ownerInstructionPosition'], 3902)
        self.assertFalse(window['automaticShift'])

    def test_every_cutoff_uses_one_rounded_start(self):
        window = subject.window_for_delegation(self.owner, self.now)
        t0 = datetime.fromisoformat(window['t0'].replace('Z', '+00:00'))
        self.assertTrue(timedelta(minutes=10) <= t0 - self.now < timedelta(minutes=10, seconds=1))
        for field, delta in [('provisioningCutoff', timedelta(minutes=55)), ('authExpiresAt', timedelta(hours=2)), ('cleanupExpiresAt', timedelta(hours=4))]:
            self.assertEqual(datetime.fromisoformat(window[field].replace('Z', '+00:00')) - t0, delta)

    def test_agent_reminder_or_other_position_cannot_authorize_start(self):
        for update in [{'createdBy': 'agent'}, {'creationSource': 'mcp'}, {'position': 3828}, {'position': 3903}, {'text': 'Ensure to finish 158'}]:
            with self.subTest(update=update), self.assertRaises(AssertionError):
                subject.window_for_delegation({**self.owner, **update}, self.now)

    def test_missing_unattended_intent_wrong_account_or_widened_limits_refuse(self):
        for update in [{'ownerUnavailable': False}, {'unattendedExecutionRequested': False}, {'account': '000000000000'}, {'region': 'us-east-1'}, {'existingC12LimitsRetained': False}]:
            with self.subTest(update=update), self.assertRaises(AssertionError):
                subject.window_for_delegation({**self.owner, **update}, self.now)

    def test_naive_foreign_timezone_or_predelegation_start_refuses(self):
        for now in [self.now.replace(tzinfo=None), self.now.astimezone(timezone(timedelta(hours=1))), datetime(2026, 10, 27, tzinfo=timezone.utc)]:
            with self.subTest(now=now), self.assertRaises((AssertionError, TypeError)):
                subject.window_for_delegation(self.owner, now)

    def test_exclusive_window_cannot_be_replaced(self):
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp) / 'window.once.json'
            value = subject.window_for_delegation(self.owner, self.now)
            subject.exclusive(path, value)
            before = path.read_bytes()
            with self.assertRaises(FileExistsError):
                subject.exclusive(path, {**value, 't0': '2099-01-01T00:00:00Z'})
            self.assertEqual(path.read_bytes(), before)

    def test_provider_path_requires_verified_bootstrap_and_exact_claim(self):
        window = subject.window_for_delegation(self.owner, self.now)
        activation = {'status': 'INDEPENDENT_SETUP_FULL_POLICY_READBACK_PASS'}
        subject.verify_provider_prerequisites(self.owner, activation, window)
        for update in [{'startAuthority': 'UNAPPROVED'}, {'claimSHA256': 'foreign'}]:
            with self.subTest(update=update), self.assertRaises(AssertionError):
                subject.verify_provider_prerequisites(self.owner, activation, {**window, **update})

    def test_provider_path_refuses_missing_failed_or_unverified_bootstrap(self):
        window = subject.window_for_delegation(self.owner, self.now)
        for activation in [{}, {'status': 'HOLD'}, {'status': 'ACKNOWLEDGED_ONLY'}]:
            with self.subTest(activation=activation), self.assertRaises((KeyError, AssertionError)):
                subject.verify_provider_prerequisites(self.owner, activation, window)


class DelegatedBoundPolicy(unittest.TestCase):
    def setUp(self):
        actual = Path(__file__).parent
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        root = Path(self.temporary.name)
        self.c = root / 'completion-c12'
        self.c.mkdir()
        (self.c / 'resolved').mkdir()
        (self.c / 'ACTIVE-CLAIM.json').write_bytes((actual / 'ACTIVE-CLAIM.json').read_bytes())
        owner = json.loads((actual / 'OWNER-UNATTENDED-REQUEST.json').read_text())
        self.write('OWNER-UNATTENDED-REQUEST.json', owner)
        window = subject.window_for_delegation(owner, datetime.now(timezone.utc) - timedelta(minutes=11))
        self.write('WINDOW-BINDING.json', window)
        self.key = 'arn:aws:kms:eu-central-1:099256073061:key/00000000-0000-4000-8000-000000000001'
        self.write('KEY-READBACK.json', {'status': 'INDEPENDENT_NATIVE_READBACK_PASS', 'key': {'Arn': self.key}})
        template_dir = root / 'revision-r3/policies-unbound'
        template_dir.mkdir(parents=True)
        templates = {}
        for path in (actual.parent / 'revision-r3/policies-unbound').glob('*.json'):
            (template_dir / path.name).write_bytes(path.read_bytes())
            templates[path.name] = json.loads(path.read_text())
        binding = {'keyARN': self.key, 'runPrefix': 'hikyo-e2e-20261028c12/', 'externalId': 'hikyo-158-20261028c12', 't0': window['t0'], 'authExpiresAt': window['authExpiresAt'], 'cleanupExpiresAt': window['cleanupExpiresAt']}
        documents, _ = bind_templates(templates, binding)
        for name, document in documents.items():
            self.write('resolved/' + name, document)
        import hashlib
        digest = hashlib.sha256(json.dumps(documents, sort_keys=True, separators=(',', ':')).encode()).hexdigest()
        self.write('POLICY-SET.json', {'binding': binding, 'canonicalChangeSetSHA256': digest})
        patcher = patch.object(subject, 'C', self.c)
        patcher.start()
        self.addCleanup(patcher.stop)

    def write(self, name, value):
        (self.c / name).write_text(json.dumps(value))

    def test_exact_rebinding_records_agent_authority_without_human_review_claim(self):
        with patch('builtins.print'):
            subject.approve_bound_policy()
        result = json.loads((self.c / 'POLICY-OWNER-APPROVAL.json').read_text())
        self.assertEqual(result['createdBy'], 'agent')
        self.assertEqual(result['ownerInstructionPosition'], 3902)
        self.assertFalse(result['humanExactHashReviewClaimed'])
        self.assertEqual(result['documents'], 18)

    def test_changed_policy_or_hash_refuses_before_any_approval(self):
        path = self.c / 'resolved/hikyo-158-source.policy.json'
        original = path.read_text()
        path.write_text('{}')
        with self.assertRaises(AssertionError):
            subject.approve_bound_policy()
        self.assertFalse((self.c / 'POLICY-OWNER-APPROVAL.json').exists())
        path.write_text(original)
        policies = json.loads((self.c / 'POLICY-SET.json').read_text())
        self.write('POLICY-SET.json', {**policies, 'canonicalChangeSetSHA256': 'foreign'})
        with self.assertRaises(AssertionError):
            subject.approve_bound_policy()

    def test_foreign_key_or_expired_window_refuses(self):
        self.write('KEY-READBACK.json', {'status': 'INDEPENDENT_NATIVE_READBACK_PASS', 'key': {'Arn': self.key + 'foreign'}})
        with self.assertRaises(AssertionError):
            subject.approve_bound_policy()
        self.write('KEY-READBACK.json', {'status': 'INDEPENDENT_NATIVE_READBACK_PASS', 'key': {'Arn': self.key}})
        window = json.loads((self.c / 'WINDOW-BINDING.json').read_text())
        self.write('WINDOW-BINDING.json', {**window, 'provisioningCutoff': '2000-01-01T00:00:00Z'})
        with self.assertRaises(AssertionError):
            subject.approve_bound_policy()

    def test_concrete_approval_is_once_only(self):
        with patch('builtins.print'):
            subject.approve_bound_policy()
            before = (self.c / 'POLICY-OWNER-APPROVAL.json').read_bytes()
            with self.assertRaises(FileExistsError):
                subject.approve_bound_policy()
        self.assertEqual((self.c / 'POLICY-OWNER-APPROVAL.json').read_bytes(), before)


if __name__ == '__main__':
    unittest.main()
```

## completion-c12/test_variable_retirement.py

Original machine-bound bytes SHA256 `1ba699ed2758f1b2e9830a70a8ae6588dfec67fba3a658aa4183d73e2642ebd9`; portable transcription SHA256 `1ba699ed2758f1b2e9830a70a8ae6588dfec67fba3a658aa4183d73e2642ebd9`. These differ when local bindings or typography were normalized.

```python
"""Offline consumer checks for the exact nine stored GitHub bindings."""
from datetime import datetime, timezone
from pathlib import Path
from unittest.mock import patch
import contextlib
import io
import json
import subprocess
import tempfile
import unittest

C=Path(__file__).resolve().parent
SOURCE=(C/'retire_run_variables_once.py').read_text()


class VariableRetirement(unittest.TestCase):
    def scenario(self,wrong=False,timeout=False):
        with tempfile.TemporaryDirectory() as tmp:
            c=Path(tmp)
            values={'AWS_ACCEPTANCE_'+str(index):'synthetic-'+str(index) for index in range(9)}
            (c/'github-bindings').mkdir()
            for name,value in values.items():(c/'github-bindings'/(name+'.json')).write_text(json.dumps({'name':name,'value':value}))
            (c/'HOSTED-RUN-TERMINAL.json').write_text(json.dumps({'id':987654321,'status':'completed','run_attempt':1}))
            (c/'WORKFLOW-DISPATCH-RESULT.json').write_text(json.dumps({'runId':'987654321'}))
            (c/'WINDOW-BINDING.json').write_text(json.dumps({'cleanupExpiresAt':'2050-01-01T00:00:00Z'}))
            writes=[]
            def read(argv,**kwargs):
                rows=[{'name':name,'value':value} for name,value in values.items() if name not in writes]
                if wrong:rows[0]['value']='unowned-value'
                return json.dumps({'total_count':len(rows),'variables':rows}).encode()
            def delete(argv,**kwargs):
                self.assertEqual(argv[-2:],['--method','DELETE'])
                name=argv[-3].rsplit('/',1)[-1]
                self.assertIn(name,values);self.assertNotIn(name,writes);writes.append(name)
                if timeout:raise subprocess.TimeoutExpired(argv,20)
                return subprocess.CompletedProcess(argv,0,b'',b'')
            text=SOURCE.replace(str(C),str(c))
            error=None
            with patch('subprocess.check_output',side_effect=read),patch('subprocess.run',side_effect=delete),contextlib.redirect_stdout(io.StringIO()):
                try:exec(compile(text,'offline-variable-retirement','exec'),{})
                except (SystemExit,AssertionError) as caught:error=caught
            return writes,error

    def test_all_nine_stored_variables_are_removed_once(self):
        writes,error=self.scenario();self.assertIsNone(error);self.assertEqual(len(writes),9)

    def test_changed_value_stops_before_any_delete(self):
        writes,error=self.scenario(wrong=True);self.assertIsInstance(error,AssertionError);self.assertEqual(writes,[])

    def test_timeout_consumes_one_forward_and_stops(self):
        writes,error=self.scenario(timeout=True);self.assertIsInstance(error,SystemExit);self.assertEqual(error.code,1);self.assertEqual(len(writes),1)


if __name__=='__main__':unittest.main()
```

## completion-c12/test_window_budget.py

Original machine-bound bytes SHA256 `e66afcbb202268fe007c826aece8afb77ef0b00f99178ae90b94f7a57006693b`; portable transcription SHA256 `e66afcbb202268fe007c826aece8afb77ef0b00f99178ae90b94f7a57006693b`. These differ when local bindings or typography were normalized.

```python
"""Offline regressions for expiring Ready and retained budget limits."""
from datetime import datetime, timedelta, timezone
from pathlib import Path
import json
import tempfile
import unittest
from unittest.mock import patch

import admit_c12
import bind_window_once


class WindowAndBudget(unittest.TestCase):
    def setUp(self):
        self.now = datetime(2026,10,27,10,0,0,123000,tzinfo=timezone.utc)
        self.ready = {'createdBy':'user','position':4000,'authenticatedAt':self.now.isoformat(),'ownerAvailableFirst40Minutes':True}

    def test_ready_binds_one_fixed_window_with_all_cutoffs(self):
        w = bind_window_once.window_for(self.ready,self.now)
        t0 = datetime.fromisoformat(w['t0'].replace('Z','+00:00'))
        self.assertTrue(timedelta(minutes=10) <= t0-self.now < timedelta(minutes=10,seconds=1))
        for field, delta in [('provisioningCutoff',timedelta(minutes=55)),('authExpiresAt',timedelta(hours=2)),('cleanupExpiresAt',timedelta(hours=4))]:
            self.assertEqual(datetime.fromisoformat(w[field].replace('Z','+00:00'))-t0,delta)
        self.assertFalse(w['automaticShift'])

    def test_stale_future_or_missing_human_availability_cannot_start_clock(self):
        for update, now in [({},self.now+timedelta(minutes=5)),({},self.now-timedelta(seconds=1)),({'createdBy':'agent'},self.now),({'ownerAvailableFirst40Minutes':False},self.now),({'position':3828},self.now)]:
            value={**self.ready,**update}
            with self.subTest(value=value,now=now),self.assertRaises(AssertionError):
                bind_window_once.window_for(value,now)

    def test_immutable_window_blocks_second_binding(self):
        actual = Path(__file__).resolve().parent
        with tempfile.TemporaryDirectory() as tmp:
            c=Path(tmp); canonical=c/'canonical-window.once.json'
            (c/'ACTIVE-CLAIM.json').write_bytes((actual/'ACTIVE-CLAIM.json').read_bytes())
            (c/'SOURCE-PUBLICATION-RECEIPT.json').write_bytes((actual/'SOURCE-PUBLICATION-RECEIPT.json').read_bytes())
            (c/'PREPARATION-PINS.console-qualified.json').write_text(json.dumps({'files':{'ACTIVE-CLAIM.json':bind_window_once.CLAIM_SHA}}))
            (c/'OWNER-CONSOLE-PREFLIGHT.json').write_text(json.dumps({'status':'PASS_C12_PREFLIGHT_WINDOW_UNBOUND'}))
            ready={**self.ready,'authenticatedAt':datetime.now(timezone.utc).isoformat()}
            (c/'READY-OWNER-ATTESTATION.json').write_text(json.dumps(ready))
            with patch.object(bind_window_once,'C',c),patch.object(bind_window_once,'CANONICAL',canonical),patch('builtins.print'):
                bind_window_once.main()
                before=canonical.read_bytes()
                with self.assertRaises(FileExistsError):bind_window_once.main()
                self.assertEqual(before,canonical.read_bytes())
                self.assertEqual(before,(c/'WINDOW-BINDING.json').read_bytes())

    def test_all_previous_reserves_and_new_83_cents_are_retained(self):
        result=admit_c12.budget('0.05','0.00','0.00')
        self.assertEqual(result['usageForecastUSD'],'7.00')
        self.assertEqual(result['grossAllReservesUSD'],'8.00')
        self.assertEqual(result['billableAllReservesUSD'],'7.95')

    def test_fresh_billing_can_exhaust_ceiling_without_new_authority(self):
        self.assertEqual(admit_c12.budget('0.20','0','0')['usageForecastUSD'],'7.15')
        for gross,billable,tax in [('0.21','0','0'),('NaN','0','0'),('-1','0','0'),('0.05','0.05','2.00')]:
            with self.subTest(gross=gross,billable=billable,tax=tax),self.assertRaises(AssertionError):
                admit_c12.budget(gross,billable,tax)

    def test_native_billing_tables_and_totals_are_lists(self):
        native={'at':'2026-10-27T07:00:00Z','tables':['Gross USD\u00a00.05'],'totals':['Billable USD\u00a00.00','Total tax USD\u00a00.00']}
        self.assertEqual(admit_c12.billing_text(native),'Gross USD\u00a00.05\nBillable USD\u00a00.00\nTotal tax USD\u00a00.00')

    def test_native_billing_unexpected_shapes_are_refused(self):
        for value in [{'at':'now','tables':[],'totals':'USD 0.00'},{'at':'now','tables':[1],'totals':[]},{'at':'now','tables':[],'totals':[],'extra':'unknown'}]:
            with self.subTest(value=value),self.assertRaises(AssertionError):admit_c12.billing_text(value)


if __name__=='__main__':
    unittest.main()
```

## completion-c12/unattended_c12.py

Original machine-bound bytes SHA256 `1acdd3f086e6575488dc9a8bdcc7b2bf40c8e5c267ffc3f8b155372c1d25c0d2`; portable transcription SHA256 `884522f92962916f642666d6517753713dd7ba9924c6df6c7a67b9bf9acb738c`. These differ when local bindings or typography were normalized.

```python
"""One c12 execution under the owner's later unattended instruction.

This never impersonates a human Ready, supplies credentials, or changes AWS
permissions. Existing guards still enforce the approved resource and MFA gates.
"""
from datetime import datetime, timedelta, timezone
from pathlib import Path
import hashlib
import json
import os
import sys

C = Path(__file__).resolve().parent
BANK = Path('<approval-bank>')
CLAIM_SHA = 'fea113e1367f578b3a0626068532d242826c597edf6f995647f3bcf9e9725e20'
OWNER_POSITION = 3902
OWNER_SUFFIX = "I'm going to bed. Just figure it out to finish 158 without my intervention"


def load(path):
    return json.loads(path.read_text())


def sha(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def verify_delegation(owner):
    assert owner['createdBy'] == 'user' and owner['creationSource'] == 'server'
    assert owner['position'] == OWNER_POSITION and owner['ownerUnavailable'] is True
    assert owner['unattendedExecutionRequested'] is True
    assert owner['account'] == '099256073061' and owner['region'] == 'eu-central-1'
    assert owner['existingC12LimitsRetained'] is True
    assert owner['text'].endswith(OWNER_SUFFIX)
    assert 'af454a5d8be681d231c35f3ef714ea3ce4bf832835755fb4b185360fb6da08c3' in owner['text']


def window_for_delegation(owner, operator_ready):
    verify_delegation(owner)
    assert operator_ready.utcoffset() == timedelta(0)
    delegated = datetime.fromisoformat(owner['authenticatedAt'].replace('Z', '+00:00'))
    assert delegated.utcoffset() == timedelta(0) and operator_ready >= delegated
    rounded = operator_ready.replace(microsecond=0)
    if operator_ready.microsecond:
        rounded += timedelta(seconds=1)
    t0 = rounded + timedelta(minutes=10)
    utc = lambda value: value.strftime('%Y-%m-%dT%H:%M:%SZ')
    return {
        'boundAt': operator_ready.isoformat(), 't0': utc(t0),
        'authExpiresAt': utc(t0 + timedelta(hours=2)),
        'cleanupExpiresAt': utc(t0 + timedelta(hours=4)),
        'provisioningCutoff': utc(t0 + timedelta(minutes=55)),
        'authorizationId': 'hikyo158-owner-assisted-' + t0.strftime('%Y%m%dt%H%M%S') + '-c12',
        'startAuthority': 'OWNER_DELEGATED_UNATTENDED_C12',
        'ownerInstructionPosition': OWNER_POSITION,
        'ownerInstructionAt': owner['authenticatedAt'],
        'operatorReadyAt': operator_ready.isoformat(),
        'ownerAvailableFirst40Minutes': False,
        'operatorOwnsExecutionAndCleanup': True,
        'automaticShift': False, 'claimSHA256': CLAIM_SHA,
    }


def verify_provider_prerequisites(owner, activation, window):
    verify_delegation(owner)
    assert activation['status'] == 'INDEPENDENT_SETUP_FULL_POLICY_READBACK_PASS'
    assert window['startAuthority'] == 'OWNER_DELEGATED_UNATTENDED_C12'
    assert window['claimSHA256'] == CLAIM_SHA


def exclusive(path, value):
    fd = os.open(path, os.O_CREAT | os.O_EXCL | os.O_WRONLY, 0o600)
    with os.fdopen(fd, 'w') as stream:
        json.dump(value, stream, indent=2)
        stream.write('\n')
        stream.flush()
        os.fsync(stream.fileno())
    directory = os.open(path.parent, os.O_RDONLY)
    os.fsync(directory)
    os.close(directory)


def bind_window():
    assert sha(C / 'ACTIVE-CLAIM.json') == CLAIM_SHA
    owner = load(C / 'OWNER-UNATTENDED-REQUEST.json')
    verify_delegation(owner)
    for name, pin in load(C / 'PREPARATION-PINS.console-qualified.json')['files'].items():
        assert sha(C / name) == pin, name
    qualified = load(C / 'UNATTENDED-QUALIFICATION.json')
    assert qualified['passed'] and qualified['AWSCalls'] == 0
    for name, pin in qualified['sourcePins'].items():
        assert sha(C / name) == pin, name
    admitted = load(C / 'OWNER-CONSOLE-PREFLIGHT.json')
    now = datetime.now(timezone.utc)
    assert admitted['status'] == 'PASS_C12_PREFLIGHT_WINDOW_UNBOUND'
    assert 0 <= (now - datetime.fromisoformat(admitted['at'].replace('Z', '+00:00'))).total_seconds() < 300
    assert admitted['newReservationsCharged'] == 32 and admitted['lifetimeReservations'] == 336
    assert load(C / 'TYPED-OFFICIAL-PROVIDER-RESULT-02.json')['exceptionClass'] == 'LoginInsufficientPermissions'
    window = window_for_delegation(owner, now)
    window['ownerInstructionSHA256'] = sha(C / 'OWNER-UNATTENDED-REQUEST.json')
    window['unattendedQualificationSHA256'] = sha(C / 'UNATTENDED-QUALIFICATION.json')
    target = BANK / 'owner-assisted-20261028c12.window.once.json'
    assert not (C / 'WINDOW-BINDING.json').exists()
    exclusive(target, window)
    os.link(target, C / 'WINDOW-BINDING.json')
    target.chmod(0o444)
    print(json.dumps(window))


def approve_bound_policy():
    verify_delegation(load(C / 'OWNER-UNATTENDED-REQUEST.json'))
    assert sha(C / 'ACTIVE-CLAIM.json') == CLAIM_SHA
    window = load(C / 'WINDOW-BINDING.json')
    assert window['startAuthority'] == 'OWNER_DELEGATED_UNATTENDED_C12'
    now = datetime.now(timezone.utc)
    assert datetime.fromisoformat(window['t0'].replace('Z', '+00:00')) <= now < datetime.fromisoformat(window['provisioningCutoff'].replace('Z', '+00:00'))
    policies = load(C / 'POLICY-SET.json')
    key = load(C / 'KEY-READBACK.json')
    assert key['status'] == 'INDEPENDENT_NATIVE_READBACK_PASS'
    assert policies['binding'] == {
        'keyARN': key['key']['Arn'], 'runPrefix': 'hikyo-e2e-20261028c12/',
        'externalId': 'hikyo-158-20261028c12', 't0': window['t0'],
        'authExpiresAt': window['authExpiresAt'], 'cleanupExpiresAt': window['cleanupExpiresAt'],
    }
    from bind_policies import bind_templates
    templates = {p.name: load(p) for p in (C.parent / 'revision-r3/policies-unbound').glob('*.json')}
    expected, audit = bind_templates(templates, policies['binding'])
    assert len(expected) == 18 and len(audit) == 129
    actual = {name: load(C / 'resolved' / name) for name in expected}
    assert actual == expected
    canonical = json.dumps(actual, sort_keys=True, separators=(',', ':')).encode()
    assert hashlib.sha256(canonical).hexdigest() == policies['canonicalChangeSetSHA256']
    approval = {
        'at': now.isoformat(), 'createdBy': 'agent',
        'authority': 'EXACT_BOUND_DATA_ACCEPTED_UNDER_OWNER_UNATTENDED_DELEGATION',
        'position': OWNER_POSITION, 'ownerInstructionPosition': OWNER_POSITION,
        'ownerInstructionSHA256': sha(C / 'OWNER-UNATTENDED-REQUEST.json'),
        'canonicalChangeSetSHA256': policies['canonicalChangeSetSHA256'],
        'independentTemplateRebindingMatched': True, 'documents': 18,
        'changedLeaves': 129, 'humanExactHashReviewClaimed': False,
        'independentCLEAN': False, 'review6': False,
    }
    exclusive(C / 'POLICY-OWNER-APPROVAL.json', approval)
    print(json.dumps(approval))


if __name__ == '__main__':
    assert len(sys.argv) == 2
    if sys.argv[1] == 'bind-window':
        bind_window()
    elif sys.argv[1] == 'approve-policy':
        approve_bound_policy()
    else:
        raise SystemExit('unsupported c12 operation')
```

## completion-c12/validate_operator.py

Original machine-bound bytes SHA256 `697df2c850591d4227383948bfe9b1397e7f3a5f868cdd9314f8e9fe0f9b6d98`; portable transcription SHA256 `9ad34939cba8e54e6d8b484d88ca12589addbaaf22feb504cab8ced8a4917017`. These differ when local bindings or typography were normalized.

```python
"""Finite offline qualification of c12 data bindings. No live API clients."""
from datetime import datetime, timezone
from pathlib import Path
import hashlib
import json
import os
import subprocess
import tempfile

C=Path(__file__).resolve().parent
python=str(C.parent/'completion-c08/tools/venv/bin/python')
node='<local-artifact>/node'
load=lambda path:json.loads(path.read_text())
sha=lambda path:hashlib.sha256(path.read_bytes()).hexdigest()
manifest=load(C/'OPERATOR-PREPARATION-MANIFEST.json')
before={name:sha(C/name) for name in manifest['files']}
assert all(before[name]==row['sha256'] for name,row in manifest['files'].items())
assert not (C/'WINDOW-BINDING.json').exists()
commands=[['python-units',[python,'-m','unittest','discover','-s',str(C),'-p','test_*.py','-v']],['native-iam-guard',[node,str(C/'test-iam-final-action.mjs')]]]
commands += [('syntax-'+path.name,[node,'--check',str(path)]) for path in sorted(C.glob('*.js'))]
result={'startedAt':datetime.now(timezone.utc).isoformat(),'checks':[],'before':before,'AWSCalls':0,'window':None,'independentReview':False}
env={key:os.environ[key] for key in ('PATH','HOME','TMPDIR','LANG') if key in os.environ}
env.update(PYTHONDONTWRITEBYTECODE='1',AWS_EC2_METADATA_DISABLED='true')
with tempfile.TemporaryDirectory(prefix='offline-config-',dir=C) as folder:
 for name in ('config','credentials'):Path(folder,name).touch(mode=0o600)
 env.update(AWS_CONFIG_FILE=str(Path(folder,'config')),AWS_SHARED_CREDENTIALS_FILE=str(Path(folder,'credentials')))
 with (C/'OPERATOR-VALIDATION-02.log').open('x') as log:
  for label,argv in commands:
   log.write('\nCHECK '+label+'\n');log.flush()
   completed=subprocess.run(argv,cwd=C,env=env,stdout=log,stderr=subprocess.STDOUT,timeout=90)
   result['checks'].append({'label':label,'argv':argv,'exitCode':completed.returncode})
result['after']={name:sha(C/name) for name in manifest['files']}
result['sourceUnchanged']=result['before']==result['after']
result['completedAt']=datetime.now(timezone.utc).isoformat()
result['logSHA256']=sha(C/'OPERATOR-VALIDATION-02.log')
(C/'OPERATOR-VALIDATION-02.json').write_text(json.dumps(result,indent=2)+'\n')
print(json.dumps({'checks':len(result['checks']),'failures':[r for r in result['checks'] if r['exitCode']],'sourceUnchanged':result['sourceUnchanged'],'AWSCalls':0}))
raise SystemExit(0 if result['sourceUnchanged'] and all(r['exitCode']==0 for r in result['checks']) else 1)
```

## completion-c12/verify_cleanup_native-DRAFT.py

Original machine-bound bytes SHA256 `4d695ad3eb9b07ee51166317e633e63befec6624fa1fc1f659f966e3ce636da3`; portable transcription SHA256 `2a27881eacb03bbad8ac718e9d7d84335f89729575792c9e0422907dfc61eafe`. These differ when local bindings or typography were normalized.

```python
"""Check selected native AWS metadata; never forward a request."""
from datetime import datetime, timezone
from pathlib import Path
import hashlib
import json
import os
import sys
import urllib.parse
import xml.etree.ElementTree as ET

C = Path(__file__).resolve().parent
OP = C.parent / 'private-operator'
SETUP = 'arn:aws:iam::099256073061:policy/hikyo-158-setup-policy'
BOUNDARY = 'arn:aws:iam::099256073061:policy/hikyo-158-role-boundary'
KEY = 'UNBOUND_ACKNOWLEDGED_C12_KEY_ID'


def load(path):
    return json.loads(path.read_text())


def sha(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def fields(row, name):
    return [n.text for n in ET.fromstring(row['body']).iter()
            if n.tag.split('}')[-1] == name]


def versions(row):
    result = {}
    for parent in ET.fromstring(row['body']).iter():
        children = {n.tag.split('}')[-1]: n.text for n in parent}
        if 'VersionId' in children and 'CreateDate' in children:
            assert children['VersionId'] not in result
            result[children['VersionId']] = children['CreateDate']
    return result


def rows(path):
    value = load(path)
    assert not any('captureFailure' in r for r in value['records'])
    return value['records']


def one(records, operation, arn=None, code=200):
    found = [r for r in records if r['operation'] == operation
             and r['status'] == code and
             (arn is None or urllib.parse.parse_qs(r['request']).get('PolicyArn') == [arn])]
    assert found, (operation, arn, code)
    for r in found:
        assert urllib.parse.parse_qs(r['request'])['Action'] == [operation]
    return found[-1]


def save(name, value):
    value = {'at': datetime.now(timezone.utc).isoformat(), **value}
    fd = os.open(C / name, os.O_CREAT | os.O_EXCL | os.O_WRONLY, 0o600)
    with os.fdopen(fd, 'w') as stream:
        json.dump(value, stream, indent=2)
        stream.write('\n')
        stream.flush()
        os.fsync(stream.fileno())
    print(json.dumps({'receipt': name, **value}))


def identities(records):
    listed = one(records, 'ListRoles')
    assert fields(listed, 'IsTruncated') == ['false']
    assert set(fields(listed, 'RoleName')) == {
        'AWSServiceRoleForResourceExplorer', 'AWSServiceRoleForSupport',
        'AWSServiceRoleForTrustedAdvisor'}
    assert not fields(one(records, 'ListOpenIDConnectProviders'), 'Arn')
    user = one(records, 'GetUser')
    assert fields(user, 'UserId') == ['AIDAROHA7PNS4ZCR3U3FR']
    assert fields(user, 'Arn') == ['arn:aws:iam::099256073061:user/hikyo-158-setup']
    for operation, member in [('ListUserPolicies', 'PolicyName'),
                              ('ListGroupsForUser', 'GroupName'),
                              ('ListAccessKeys', 'AccessKeyId')]:
        r = one(records, operation)
        assert fields(r, 'IsTruncated') == ['false'] and not fields(r, member)
    attached = one(records, 'ListAttachedUserPolicies')
    assert fields(attached, 'IsTruncated') == ['false']
    assert fields(attached, 'PolicyArn') == [SETUP]
    mfa = one(records, 'ListMFADevices')
    assert fields(mfa, 'IsTruncated') == ['false']
    assert fields(mfa, 'SerialNumber') == ['arn:aws:iam::099256073061:mfa/hikyo-158-setup']


def setup(records, default):
    p = one(records, 'GetPolicy', SETUP)
    assert fields(p, 'PolicyId') == ['ANPAROHA7PNSTPQUQEUTR']
    assert fields(p, 'Arn') == [SETUP]
    assert fields(p, 'DefaultVersionId') == [default]
    listed = one(records, 'ListPolicyVersions', SETUP)
    assert fields(listed, 'IsTruncated') == ['false']
    actual = versions(listed)
    assert set(actual) == {'v2', 'v3', 'v4', 'v5', 'UNBOUND_ACTUAL_NEW_SETUP_VERSION'}
    baseline = one(rows(OP / 'c12-iam-admission.json'), 'ListPolicyVersions', SETUP)
    for v in ['v2', 'v3', 'v4', 'v5']:
        assert actual[v] == versions(baseline)[v]
    activated = one(rows(OP / 'c12-setup-readback.json'), 'ListPolicyVersions', SETUP)
    assert actual['UNBOUND_ACTUAL_NEW_SETUP_VERSION'] == versions(activated)['UNBOUND_ACTUAL_NEW_SETUP_VERSION']
    candidates = [r for r in records if r['operation'] == 'GetPolicyVersion'
                  and r['status'] == 200
                  and urllib.parse.parse_qs(r['request']).get('PolicyArn') == [SETUP]
                  and urllib.parse.parse_qs(r['request']).get('VersionId') == [default]]
    assert candidates
    version = candidates[-1]
    assert fields(version, 'VersionId') == [default]
    assert fields(version, 'IsDefaultVersion') == ['true']
    document = json.loads(urllib.parse.unquote(fields(version, 'Document')[0]))
    if default == 'v5':
        original = load(Path('<local-artifact>/final-iam-cleanup-and-retained-identity.actual.json'))
        assert document == original['setupPolicy']['versions']['v5']['document']
    else:
        assert document == load(C / 'resolved/setup-user.managed-policy.json')
    return actual


def main():
    mode = sys.argv[1]
    assert mode in {'unused', 'v5', 'resources'}
    assert load(C / 'HOSTED-RUN-TERMINAL.json')['status'] == 'completed'
    assert load(C / 'IAM-RETIREMENT-RESULT.json')['status'] == 'PASS_EXACT_EIGHT_ROLES_INLINE_POLICIES_AND_OWNED_ISSUER_RETIRED'
    path = OP / ({'unused': 'c12-post-iam-retirement-readback.json',
                  'v5': 'c12-final-v5-readback.json',
                  'resources': 'c12-final-iam-reconciliation.json'}[mode])
    records = rows(path)
    identities(records)
    dates = setup(records, 'UNBOUND_ACTUAL_NEW_SETUP_VERSION' if mode == 'unused' else 'v5')
    if mode == 'unused':
        extra = OP / 'c12-unused-boundary-readback.json'
        boundary_rows = records + rows(extra)
        p = one(boundary_rows, 'GetPolicy', BOUNDARY)
        policyid = load(C / 'BOUNDARY-READBACK.json')['policyId']
        assert fields(p, 'PolicyId') == [policyid]
        assert fields(p, 'Arn') == [BOUNDARY]
        assert fields(p, 'AttachmentCount') == ['0']
        assert fields(p, 'PermissionsBoundaryUsageCount') == ['0']
        assert fields(p, 'DefaultVersionId') == ['v1']
        v = one(boundary_rows, 'ListPolicyVersions', BOUNDARY)
        assert fields(v, 'IsTruncated') == ['false'] and fields(v, 'VersionId') == ['v1']
        tags = one(boundary_rows, 'ListPolicyTags', BOUNDARY)
        assert fields(tags, 'IsTruncated') == ['false'] and not fields(tags, 'Key')
        common = {'nativeReceiptSHA256': sha(path), 'boundaryReceiptSHA256': sha(extra), 'policyId': policyid}
        save('IAM-FIXTURE-RETIREMENT-INDEPENDENT.json', {
            **common, 'status': 'PASS_INDEPENDENT_EIGHT_ROLE_AND_ISSUER_ABSENCE_BOUNDARY_UNUSED',
            'ownedRolesAbsent': 8, 'issuerAbsent': True})
        save('UNUSED-BOUNDARY-READBACK.json', {**common, 'versions': ['v1'],
             'attachmentCount': 0, 'permissionsBoundaryUsageCount': 0})
    elif mode == 'v5':
        ack = load(OP / 'c12-cleanup-restoresetupv5-once.json')
        assert ack['forwarded'] == 1 and not ack['failed'] and ack['response']['status'] == 200
        save('FINAL-V5-RESTORATION-READBACK.json', {
            'status': 'ORIGINAL_V5_RESTORED_V2_V5_AND_NEW_VERSION_PRESERVED_PASS',
            'defaultVersion': 'v5', 'retainedVersions': sorted(dates),
            'creationDates': dates, 'sourceSHA256': sha(path)})
    else:
        absent = one(records, 'GetPolicy', BOUNDARY, 404)
        assert fields(absent, 'Code') == ['NoSuchEntity']
        secrets_path = OP / 'c12-secret-pre-key-retirement.json'
        secrets = load(secrets_path)
        assert secrets['status'] == 200 and secrets['request']['IncludePlannedDeletion'] is True
        assert secrets['body'] == {'SecretList': []}
        key_path = OP / 'c12-key-pre-retirement.json'
        key = load(key_path)
        assert not any('captureFailure' in r for r in key['records'])
        metadata_rows = [r for r in key['records'] if r['operation'] == 'DescribeKey'
                         and r['status'] == 200
                         and json.loads(r['request'])['KeyId'] in {
                             KEY, 'arn:aws:kms:eu-central-1:099256073061:key/' + KEY}]
        assert metadata_rows
        meta = json.loads(metadata_rows[-1]['body'])['KeyMetadata']
        assert meta['KeyId'] == KEY and meta['AWSAccountId'] == '099256073061' and meta['KeyState'] == 'Enabled'
        original_key = load(C / 'KEY-READBACK.json')['key']
        assert meta['CreationDate'] == original_key['CreationDate']
        aliases = [r for r in key['records'] if r['operation'] == 'ListAliases' and r['status'] == 200]
        assert aliases
        for alias_record in aliases:
            alias_body = json.loads(alias_record['body'])
            assert alias_body['Truncated'] is False
            assert [(a['AliasName'], a['TargetKeyId']) for a in alias_body['Aliases']] == [
                ('alias/hikyo-158-20261028c12', KEY)]
        for operation in ['GetKeyPolicy', 'ListResourceTags', 'GetKeyRotationStatus']:
            selected = [r for r in key['records'] if r['operation'] == operation and r['status'] == 200]
            assert selected
            body = json.loads(selected[-1]['body'])
            if operation == 'GetKeyPolicy':
                assert body['PolicyName'] == 'default'
                assert json.loads(body['Policy']) == load(C / 'KEY-READBACK.json')['keyPolicy']
            elif operation == 'ListResourceTags':
                assert body == {'Tags': [], 'Truncated': False}
            else:
                assert body['KeyRotationEnabled'] is False
        now = datetime.now(timezone.utc)
        for receipt in [load(path), secrets, key]:
            assert 0 <= (now - datetime.fromisoformat(receipt['at'].replace('Z', '+00:00'))).total_seconds() < 300
        save('CLEANUP-RESOURCE-RECONCILIATION.json', {
            'status': 'PASS_NO_OWNED_SECRETS_ROLES_ISSUER_OR_BOUNDARY_V5_RESTORED',
            'keyId': KEY, 'newKeyStillEnabled': True, 'rolesAbsent': 8,
            'issuerAbsent': True, 'boundaryAbsent': True, 'setupDefault': 'v5',
            'newSetupVersionRetained': 'UNBOUND_ACTUAL_NEW_SETUP_VERSION', 'completeSecretInventory': 0,
            'nativeReceipts': {p.stem: sha(p) for p in [path, secrets_path, key_path]},
            'namespaceQuiescenceReceiptSHA256': sha(C / 'NAMESPACE-SECRET-CLEANUP-READBACK.json'),
            'acceptanceStillFailed': True})


if __name__ == '__main__':
    main()
```

## completion-c12/verify_github_preparation.py

Original machine-bound bytes SHA256 `dc21e05b2dbcee66f27794590fc30354874f270bf63b5f6d57d595e9244b104a`; portable transcription SHA256 `dc21e05b2dbcee66f27794590fc30354874f270bf63b5f6d57d595e9244b104a`. These differ when local bindings or typography were normalized.

```python
"""Read-only protected-environment and exact published-source correspondence."""
from datetime import datetime, timezone
from pathlib import Path
import base64
import hashlib
import json
import subprocess

C = Path(__file__).resolve().parent
REPO = 'repos/Hikyo-Org/Hikyo/'
MAIN = '24e198b32a3ffd8d4013207b2f974d76ec49be6f'


def api(path):
    return json.loads(subprocess.check_output(['gh','api',REPO+path], timeout=25))


def main():
    main_sha = api('branches/main')['commit']['sha']
    assert main_sha == MAIN, 'main-advanced-recheck-correspondence-before-dispatch'
    candidate = 'c253ff9b1ccec7408f13366f8c5640c8c7cf3d0a'
    pr = api('pulls/969')
    assert pr['state'] == 'open' and pr['head']['sha'] == candidate
    source = json.loads((C/'SOURCE-PUBLICATION-CANDIDATE.json').read_text())
    runtime = []
    for name, expected in source['currentSourceFiles'].items():
        data = api('contents/'+name+'?ref='+main_sha)
        blob = base64.b64decode(data['content'])
        assert hashlib.sha256(blob).hexdigest() == expected
        runtime.append({'path':name,'sha256':expected,'equalPublishedBytes':True})
    env = api('environments/hikyo-158-aws-sandbox')
    assert env['id'] == 24756660811 and env['can_admins_bypass'] is False
    assert any(row['type']=='required_reviewers' and row.get('prevent_self_review') is False and [x['reviewer']['login'] for x in row['reviewers']]==['Dunky13'] for row in env['protection_rules'])
    bindings = api('environments/hikyo-158-aws-sandbox/variables?per_page=30')
    assert bindings['total_count'] == 0 and bindings['variables'] == []
    branches = api('environments/hikyo-158-aws-sandbox/deployment-branch-policies')
    assert branches['total_count'] == 1 and len(branches['branch_policies']) == 1 and branches['branch_policies'][0]['name'] == 'main'
    oidc = api('actions/oidc/customization/sub')
    assert oidc == {'use_default':True,'use_immutable_subject':True,'sub_claim_prefix':'repo:Hikyo-Org@316726515/Hikyo@1316165429'}
    runs = api('actions/workflows/aws-acceptance.yml/runs?per_page=100')
    assert runs['total_count'] <= 100 and all(row['status']=='completed' for row in runs['workflow_runs'])
    assert all(row['id'] in {40390662284,40485386416,40510334025,40600194878} or row['created_at']<'2026-10-26T00:00:00Z' for row in runs['workflow_runs'])
    at = datetime.now(timezone.utc).isoformat()
    record = {'at':at,'status':'PASS_SOURCE_PROTECTION_IMMUTABLE_SUBJECT_EMPTY_BINDINGS_NO_ACTIVE_RUNS','observedMain':main_sha,'candidate':candidate,'runtimeFiles':runtime,'environment':env,'variables':bindings,'branchPolicies':branches,'OIDC':oidc,'workflowRuns':[{'id':row['id'],'status':row['status'],'created_at':row['created_at'],'run_attempt':row['run_attempt']} for row in runs['workflow_runs']]}
    (C/'GITHUB-FRESH-ADMISSION.json').write_text(json.dumps(record,indent=2)+'\n')
    (C/'MAIN-CORRESPONDENCE.json').write_text(json.dumps({'at':at,'observedMain':main_sha,'candidate':candidate,'runtimeFiles':runtime},indent=2)+'\n')
    print(json.dumps({'status':record['status'],'sourceFiles':len(runtime),'main':main_sha,'AWSCalls':0}))


if __name__ == '__main__':
    main()
```
