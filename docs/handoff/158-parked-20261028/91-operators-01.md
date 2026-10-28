# Prepared operator source archive, part 1/9

**ARCHIVE ONLY, PARKED, LIVE QUALIFICATION NOT ESTABLISHED.** This preserves the prepared small-route operators and their tests so local report deletion does not erase the implementation. The transcription normalizes machine-local paths to `<report-root>`, `<approval-bank>`, `<application-checkout>`, `<trusted-source-checkout>` or `<local-artifact>`. They are reconstruction placeholders, not working filesystem paths. Original and normalized digests are both recorded. All runtime/source pins and concrete bindings must be re-established before use. Do not copy this code directly into a live operation or claim that normalization preserved an old qualification. In particular the historical unattended helper is an agent interpretation of an overnight request; present parking overrides it, and all applicable source/Ready/policy/deployment/merge gates must be reconciled with current owner direction. No credentials, authentication storage or private challenge URLs are included.

## completion-c12/admit_c12.py

Original machine-bound bytes SHA256 `171e1aa7aaedb78788acadee1ec10103062ea3c6b99e668dc1b380a42ef2373b`; portable transcription SHA256 `8caaed0f2e936471530f16ffdfcff7b0bc024233d1b02c7974862d261ee7043e`. These differ when local bindings or typography were normalized.

```python
"""Verify fresh native metadata, billing and closed c12 authority; no AWS calls."""
from datetime import datetime, timedelta, timezone
from decimal import Decimal
from pathlib import Path
import hashlib
import json
import os
import urllib.parse
import xml.etree.ElementTree as ET

from prior_key_schedules import reconcile

C = Path(__file__).resolve().parent
R = C.parent
OP = R/'private-operator'
LOAD = lambda path: json.loads(path.read_text())
SHA = lambda path: hashlib.sha256(path.read_bytes()).hexdigest()
FIELDS = lambda row, name: [node.text for node in ET.fromstring(row['body']).iter() if node.tag.split('}')[-1] == name]
SETUP = 'arn:aws:iam::099256073061:policy/hikyo-158-setup-policy'


def budget(gross, billable, tax):
    gross, billable, tax = [Decimal(value) for value in (gross, billable, tax)]
    assert all(value.is_finite() and value >= 0 for value in (gross, billable, tax))
    retained = Decimal('6.12')  # Every old reserve, excluding the separate tax floor.
    addition = Decimal('0.83')
    usage = gross + retained + addition
    all_gross = usage + max(Decimal('1.00'), tax)
    all_billable = billable + retained + addition + max(Decimal('1.00'), tax)
    assert usage <= Decimal('7.15') and all_gross < 9 and all_billable < 9
    assert all_gross < 10 and all_gross < 15 and all_billable < 15
    return {'displayedGrossUSD':str(gross),'displayedBillableUSD':str(billable),'displayedTaxUSD':str(tax),'allOldReservesRetainedUSD':str(retained),'newReserveUSD':str(addition),'usageForecastUSD':str(usage),'grossAllReservesUSD':str(all_gross),'billableAllReservesUSD':str(all_billable),'usageCeilingUSD':'7.15'}


def billing_text(native):
    assert type(native) is dict and set(native)=={'at','tables','totals'}
    assert all(type(native[field]) is list and all(type(row) is str for row in native[field]) for field in ['tables','totals'])
    return '\n'.join(native['tables']+native['totals'])


def verify_native_billing(native, normalized):
    """Prove gross from every usage leaf in the pinned expanded native table shape."""
    import re
    billing_text(native)
    raw = Path(__file__).with_name('BILLING-NATIVE-TABLE-PROFILE.json').read_bytes()
    assert hashlib.sha256(raw).hexdigest() == '9d6a3331e89f242d71d756af8713f86e75552346efb720ae1514d64d7f1ca02a'
    profile = json.loads(raw)
    quantity = r'([0-9][0-9,.]*)\s+(Keys|Requests|Secrets|Request|API Requests)'
    money = r'(\()?USD[\s\u00a0]+([0-9]+\.[0-9]{2})(\))?'
    shape, charges = [], []
    for table in native['tables']:
        tokens = [line.strip() for line in table.splitlines() if line.strip()]
        masked = []
        for index, token in enumerate(tokens):
            amount, count = re.fullmatch(money, token), re.fullmatch(quantity, token)
            if amount:
                assert bool(amount.group(1)) == bool(amount.group(3))
                masked.append('<CREDIT>' if amount.group(1) else '<USD>')
            elif count:
                assert index > 0 and index + 1 < len(tokens)
                charge = re.fullmatch(money, tokens[index + 1])
                assert charge and not charge.group(1) and not charge.group(3)
                charges.append(Decimal(charge.group(2)))
                masked.append('<QUANTITY:' + count.group(2) + '>')
            else:
                masked.append(token)
        shape.append(masked)
    assert shape == profile['tables'] and len(charges) == profile['usageLeaves'] == 8
    assert sum(charges, Decimal('0.00')) == Decimal(normalized['grossUSD'])
    for label, field in [('Estimated grand total:', 'billableUSD'), ('Total tax', 'taxUSD')]:
        assert native['totals'].count(label) == 1
        index = native['totals'].index(label)
        assert index + 1 < len(native['totals'])
        observed = re.fullmatch(money, native['totals'][index + 1])
        assert observed and not observed.group(1) and not observed.group(3)
        assert Decimal(observed.group(2)) == Decimal(normalized[field])


def validate_kms_list(value, expected_customer_ids):
    rows=[row for row in value['records'] if row['operation']=='ListKeysForService']
    assert len(rows)==1 and rows[0].get('status')==200
    data=json.loads(rows[0]['body'])
    assert set(data)=={'Keys','Truncated'} and data['Truncated'] is False and type(data['Keys']) is list
    customer=set()
    for key in data['Keys']:
        assert set(key)=={'KeyId','KeyArn','AliasList'}
        key_id=key['KeyId']
        if key_id.startswith('alias/aws/'):
            assert key['KeyArn']=='arn:aws:kms:eu-central-1:099256073061:'+key_id
            assert key['AliasList']==[{'Alias':key['KeyArn']}]
        else:
            assert key_id not in customer and key['KeyArn']=='arn:aws:kms:eu-central-1:099256073061:key/'+key_id and key['AliasList']==[]
            customer.add(key_id)
    assert customer==expected_customer_ids


def dates(row):
    result = {}
    for node in ET.fromstring(row['body']).iter():
        children = {child.tag.split('}')[-1]:child.text for child in list(node)}
        if 'VersionId' in children and 'CreateDate' in children:
            result[children['VersionId']] = children['CreateDate']
    return result


def main():
    now = datetime.now(timezone.utc)
    claim_path = C/'ACTIVE-CLAIM.json'
    assert SHA(claim_path) == 'fea113e1367f578b3a0626068532d242826c597edf6f995647f3bcf9e9725e20'
    claim = LOAD(claim_path)
    assert claim['lifetimeReservations'] == 336 and claim['newReservationsCharged'] == 32
    canonical_dir = Path('<approval-bank>')
    for name, expected in claim['previousImmutableClaimAndClosurePins'].items():
        assert SHA(canonical_dir/name) == expected
    window = LOAD(C/'WINDOW-BINDING.json') if (C/'WINDOW-BINDING.json').exists() else None
    if window is not None:
        assert now < datetime.fromisoformat(window['provisioningCutoff'].replace('Z','+00:00'))
    names = ['owner-access','iam-admission','kms-admission','kms-list-complete','secret-admission','historical-secret-admission','billing-admission','key-draft','setup-v11-archive']
    rows = {}
    for name in names:
        value = LOAD(OP/('c12-'+name+'.json'))
        assert 0 <= (now-datetime.fromisoformat(value['at'].replace('Z','+00:00'))).total_seconds() < 300
        assert not any('captureFailure' in item for item in value.get('records',[]))
        rows[name] = value
    owner = rows['owner-access']
    assert owner['consolePage'] and owner['accountVisible'] and not owner['needsPrivateSignIn']
    github = LOAD(C/'GITHUB-FRESH-ADMISSION.json')
    assert github['status'] == 'PASS_SOURCE_PROTECTION_IMMUTABLE_SUBJECT_EMPTY_BINDINGS_NO_ACTIVE_RUNS'
    assert 0 <= (now-datetime.fromisoformat(github['at'].replace('Z','+00:00'))).total_seconds() < 300
    secret = rows['secret-admission']
    assert secret['status'] == 200 and secret['request']['IncludePlannedDeletion'] is True
    assert not secret['body'].get('NextToken') and secret['body']['SecretList'] == []
    historical = rows['historical-secret-admission']
    assert historical['status'] == 400 and json.loads(historical['body'])['__type'] == 'ResourceNotFoundException'
    kms = rows['kms-admission']['records']
    assert len(kms) == 19 and len({item['id'] for item in kms}) == 19
    schedules = {'81294276-467e-45b2-8279-1abb6fea8e34':'2026-11-03T23:50:52.008000Z','03dcf56b-9687-4173-97d5-6f4925d08797':'2026-11-03T21:32:50.570000Z','88e19f70-fee7-4f9b-9b35-847bba607a55':'2026-11-03T14:19:45.807Z','b71f3c57-626a-4a89-9fcf-f7901a421e49':'2026-11-03T10:51:15.444Z','64224f0e-697e-4090-9978-ea153625f6d9':'2026-11-02T13:48:51.595Z','4005b859-369d-4fa5-a96c-7b8e865ee712':'2026-11-02T17:19:46.043Z','b8806a19-57c8-4d77-9d1d-39c611a933a1':'2026-11-02T20:45:47.974Z','990c479f-d673-4bc9-8bca-7b9b86f4b205':'2026-10-27T21:42:12.444Z','bfa44dc1-ee8a-43b8-aec7-d8a7fbb38db1':'2026-10-28T23:31:42.338Z'}
    prior_key_state = reconcile(kms, schedules, set(['10908b8d-fe54-40e3-9210-a3eefea709b6', 'fa374267-a456-4e79-b5b1-6cdf356b7d0e', 'alias/hikyo-158-20261022c05', 'alias/hikyo-158-20261026c06', 'alias/hikyo-158-20261026c07', 'alias/hikyo-158-20261027c08', 'alias/hikyo-158-20261027c09', 'alias/hikyo-158-20261027c10', 'alias/hikyo-158-20261027c11', 'alias/hikyo-158-20261028c12']), rows['kms-admission']['readStartedAt'], rows['kms-admission']['at'])
    validate_kms_list(rows['kms-list-complete'],set(prior_key_state['pendingIds']))
    draft=rows['key-draft']
    assert draft['finishEnabled'] and draft['serviceWorkers']==0
    for phrase in ['hikyo-158-20261028c12','SYMMETRIC_DEFAULT','Single-Region key','No tags to display','No key administrators selected.','No key users selected.','arn:aws:iam::099256073061:root']:
        assert phrase in draft['review']
    iam = rows['iam-admission']['records']
    def one(action, arn=None):
        return next(item for item in iam if item['operation']==action and item.get('status')==200 and (arn is None or urllib.parse.parse_qs(item['request']).get('PolicyArn')==[arn]))
    assert FIELDS(one('ListRoles'),'IsTruncated') == ['false']
    assert set(FIELDS(one('ListRoles'),'RoleName')) == {'AWSServiceRoleForResourceExplorer','AWSServiceRoleForSupport','AWSServiceRoleForTrustedAdvisor'}
    assert FIELDS(one('ListOpenIDConnectProviders'),'Arn') == []
    for action, member in [('ListGroupsForUser','GroupName'),('ListAccessKeys','AccessKeyId'),('ListUserPolicies','PolicyName')]:
        assert FIELDS(one(action),'IsTruncated') == ['false'] and FIELDS(one(action),member) == []
    assert FIELDS(one('GetUser'),'UserId') == ['AIDAROHA7PNS4ZCR3U3FR']
    assert FIELDS(one('ListMFADevices'),'SerialNumber') == ['arn:aws:iam::099256073061:mfa/hikyo-158-setup']
    assert FIELDS(one('ListAttachedUserPolicies'),'PolicyArn') == [SETUP]
    assert FIELDS(one('GetPolicy',SETUP),'PolicyId') == ['ANPAROHA7PNSTPQUQEUTR']
    assert FIELDS(one('GetPolicy',SETUP),'DefaultVersionId') == ['v5']
    versions = one('ListPolicyVersions',SETUP)
    assert FIELDS(versions,'IsTruncated') == ['false'] and set(FIELDS(versions,'VersionId')) == {'v2','v3','v4','v5','v11'}
    old = next(item for item in LOAD(OP/'c11-final-iam-closure.json')['records'] if item['operation']=='ListPolicyVersions' and item.get('status')==200 and urllib.parse.parse_qs(item['request']).get('PolicyArn')==[SETUP])
    assert dates(versions) == dates(old)
    default = next(item for item in iam if item['operation']=='GetPolicyVersion' and item.get('status')==200 and urllib.parse.parse_qs(item['request']).get('VersionId')==['v5'])
    actual = json.loads(urllib.parse.unquote(FIELDS(default,'Document')[0]))
    archive = LOAD(Path('<local-artifact>/final-iam-cleanup-and-retained-identity.actual.json'))
    assert actual == archive['setupPolicy']['versions']['v5']['document']
    assert any(item['operation']=='GetPolicy' and item.get('status')==404 and FIELDS(item,'Code')==['NoSuchEntity'] and urllib.parse.parse_qs(item['request']).get('PolicyArn')==['arn:aws:iam::099256073061:policy/hikyo-158-role-boundary'] for item in iam)
    v11 = next(item for item in rows['setup-v11-archive']['records'] if item['operation']=='GetPolicyVersion' and item.get('status')==200 and urllib.parse.parse_qs(item['request']).get('PolicyArn')==[SETUP] and urllib.parse.parse_qs(item['request']).get('VersionId')==['v11'])
    assert FIELDS(v11,'VersionId') == ['v11'] and FIELDS(v11,'IsDefaultVersion') == ['false']
    assert json.loads(urllib.parse.unquote(FIELDS(v11,'Document')[0])) == LOAD(R/'completion-c11/resolved/setup-user.managed-policy.json')
    native_bill = rows['billing-admission']
    normalized = LOAD(C/'BILLING-NORMALIZED.json')
    assert normalized['nativeReceiptSHA256'] == SHA(OP/'c12-billing-admission.json')
    verify_native_billing(native_bill, normalized)
    prices = LOAD(C/'PRICE-RECONCILIATION.json')
    assert timedelta(0) <= now-datetime.fromisoformat(prices['at'].replace('Z','+00:00')) < timedelta(hours=24)
    for source in prices['sources']:
        assert SHA(C/(source['service']+'-eu-central-1-prices.json')) == source['sha256']
    rates = {row['usageType']:Decimal(row['USD']) for source in prices['sources'] for row in source['rates']}
    assert rates['eu-central-1-KMS-Keys'] == Decimal('1.00')
    assert rates['eu-central-1-KMS-Requests'] == Decimal('0.000003')
    assert rates['EUC1-AWSSecretsManager-Secrets'] == Decimal('0.40')
    assert rates['EUC1-AWSSecretsManagerAPIRequest'] == Decimal('0.000005')
    status='PASS_C12_ADMISSION' if window is not None else 'PASS_C12_PREFLIGHT_WINDOW_UNBOUND'
    result = {'at':now.isoformat(),'status':status,'claimSHA256':SHA(claim_path),'githubReceiptSHA256':SHA(C/'GITHUB-FRESH-ADMISSION.json'),'receipts':{name:SHA(OP/('c12-'+name+'.json')) for name in names},'budget':budget(normalized['grossUSD'],normalized['billableUSD'],normalized['taxUSD']),'newReservationsCharged':32,'lifetimeReservations':336,'AWSCalls':0,'window':window,'priorKeyReconciliation':prior_key_state}
    archive_dir = C/'admission-history'; archive_dir.mkdir(mode=0o700,exist_ok=True)
    data = json.dumps(result,indent=2)+'\n'
    with (archive_dir/(hashlib.sha256(data.encode()).hexdigest()+'.json')).open('x') as stream:
        stream.write(data);stream.flush();os.fsync(stream.fileno())
    (C/('FRESH-ADMISSION.json' if window is not None else 'OWNER-CONSOLE-PREFLIGHT.json')).write_text(data)
    (C/'ARCHIVED-SETUP-V11.json').write_text(json.dumps({'at':now.isoformat(),'version':'v11','nondefault':True,'nativeReceiptSHA256':SHA(OP/'c12-setup-v11-archive.json'),'fullNativeResponse':v11},indent=2)+'\n')
    print(json.dumps({'status':result['status'],'budget':result['budget'],'AWSCalls':0}))


if __name__ == '__main__':
    main()
```

## completion-c12/bind_console_data.py

Original machine-bound bytes SHA256 `fb82cd063ea40d0d99b877237490fc9db0889719265b5a251b207938b525c91e`; portable transcription SHA256 `fb82cd063ea40d0d99b877237490fc9db0889719265b5a251b207938b525c91e`. These differ when local bindings or typography were normalized.

```python
"""Substitute approved public data into the existing private native UI drafts."""
from datetime import datetime, timezone
from pathlib import Path
import hashlib
import json
import os
import sys

C = Path(__file__).resolve().parent
OP = C.parent/'private-operator'
LOAD = lambda path: json.loads(path.read_text())


def main():
    mode = sys.argv[1]
    specs = {'key-readback':('c12-key-readback-DRAFT.js','c12-key-readback.js',None),'boundary':('c12-boundary-draft-DRAFT.js','c12-boundary-draft.js','setup-role-boundary.json'),'setup':('c12-setup-draft-DRAFT.js','c12-setup-draft.js','setup-user.managed-policy.json'),'retire-alias':('retire-alias-once-DRAFT.js','c12-retire-alias-once.js',None),'schedule-key':('schedule-key-once-DRAFT.js','c12-schedule-key-once.js',None)}
    assert mode in specs
    source_name, target_name, document = specs[mode]
    source = (C/source_name).read_text()
    if mode == 'key-readback':
        ack = LOAD(OP/'c12-key-finish-bound.json')
        assert ack['classification']=='ACKNOWLEDGED_REQUIRES_READBACK' and ack['dispatched']=={'CreateKey':1,'CreateAlias':1}
        key = ack['key']['KeyId']
    else:
        key = LOAD(C/'KEY-READBACK.json')['key']['KeyId']
    replacements = {'UNBOUND_ACKNOWLEDGED_C12_KEY_ID':key}
    if document:
        policies, approval = LOAD(C/'POLICY-SET.json'), LOAD(C/'POLICY-OWNER-APPROVAL.json')
        assert approval['canonicalChangeSetSHA256']==policies['canonicalChangeSetSHA256']
        compact = (C/'resolved'/document.replace('.json','.compact.json')).read_text()
        assert hashlib.sha256(compact.encode()).hexdigest()==policies['documents'][document]['canonicalSHA256']
        assert source.count("'UNBOUND_EXACT_APPROVED_POLICY_DOCUMENT'")==1
        source=source.replace("'UNBOUND_EXACT_APPROVED_POLICY_DOCUMENT'",json.dumps(compact))
    if mode in {'retire-alias','schedule-key'}:
        reconciliation = LOAD(C/'CLEANUP-RESOURCE-RECONCILIATION.json')
        assert reconciliation['status']=='PASS_NO_OWNED_SECRETS_ROLES_ISSUER_OR_BOUNDARY_V5_RESTORED'
        assert reconciliation['keyId']==key and reconciliation['newKeyStillEnabled'] is True
        w=LOAD(C/'WINDOW-BINDING.json');now=datetime.now(timezone.utc)
        assert 0 <= (now-datetime.fromisoformat(reconciliation['at'].replace('Z','+00:00'))).total_seconds() < 300
        assert datetime.fromisoformat(w['t0'].replace('Z','+00:00'))<=now<datetime.fromisoformat(w['cleanupExpiresAt'].replace('Z','+00:00'))
        replacements.update(UNBOUND_T0=w['t0'],UNBOUND_CLEANUP_UNTIL=w['cleanupExpiresAt'])
        if mode=='schedule-key':
            assert LOAD(C/'ALIAS-ABSENCE-READBACK.json')['status']=='PASS_EXACT_C12_ALIAS_NOT_FOUND'
        # This is local durable once bookkeeping, preceding any browser forward.
        intent=C/(mode+'-INTENT.once.json')
        fd=os.open(intent,os.O_CREAT|os.O_EXCL|os.O_WRONLY,0o600)
        with os.fdopen(fd,'w') as stream:
            json.dump({'at':now.isoformat(),'keyId':key,'alias':'alias/hikyo-158-20261028c12','window':w,'maximumForwards':1,'retry':False},stream,indent=2);stream.flush();os.fsync(stream.fileno())
        directory=os.open(C,os.O_RDONLY);os.fsync(directory);os.close(directory)
    for token,value in replacements.items():
        source=source.replace(token,value)
    target=OP/target_name
    with target.open('x') as stream:
        stream.write(source)
    target.chmod(0o600)
    print(json.dumps({'scriptName':target.stem,'sourceSHA256':hashlib.sha256(source.encode()).hexdigest(),'AWSCalls':0}))


if __name__=='__main__':
    main()
```

## completion-c12/bind_final_iam_cleanup.py

Original machine-bound bytes SHA256 `b02911517f3784935f973c7aa03272487bdaf78025cb89862c7100ce56661a06`; portable transcription SHA256 `b02911517f3784935f973c7aa03272487bdaf78025cb89862c7100ce56661a06`. These differ when local bindings or typography were normalized.

```python
"""Bind the existing root-console limiter to restoring v5 or deleting c12 boundary."""
from datetime import datetime, timezone
from pathlib import Path
import hashlib
import json
import os
import sys

C=Path(__file__).resolve().parent
OP=C.parent/'private-operator'
LOAD=lambda path:json.loads(path.read_text())


def main():
    step=sys.argv[1]
    assert step in {'RestoreSetupV5','DeleteUnusedBoundary'}
    assert LOAD(C/'IAM-FIXTURE-RETIREMENT-INDEPENDENT.json')['status']=='PASS_INDEPENDENT_EIGHT_ROLE_AND_ISSUER_ABSENCE_BOUNDARY_UNUSED'
    assert LOAD(C/'NAMESPACE-SECRET-CLEANUP-READBACK.json')['status']=='PASS_STRICT_NO_C12_SECRETS_REMAIN'
    boundary=LOAD(C/'BOUNDARY-READBACK.json')
    if step=='DeleteUnusedBoundary':
        assert LOAD(C/'FINAL-V5-RESTORATION-READBACK.json')['status']=='ORIGINAL_V5_RESTORED_V2_V5_AND_NEW_VERSION_PRESERVED_PASS'
        read=LOAD(C/'UNUSED-BOUNDARY-READBACK.json')
        assert read['policyId']==boundary['policyId'] and read['versions']==['v1'] and read['attachmentCount']==0 and read['permissionsBoundaryUsageCount']==0
    w=LOAD(C/'WINDOW-BINDING.json');now=datetime.now(timezone.utc)
    assert datetime.fromisoformat(w['t0'].replace('Z','+00:00'))<=now<datetime.fromisoformat(w['cleanupExpiresAt'].replace('Z','+00:00'))
    sourcepath=C/'iam-cleanup-DRAFT.js'
    assert hashlib.sha256(sourcepath.read_bytes()).hexdigest()=='9821a4d641b9147a4e5ded29f2314141eed95546c9aa80b671685a51c9ddecf0'
    draft=LOAD(OP/('c12-cleanup-'+step.lower()+'-draft.json'))
    assert draft['AWSWritesForwarded']==0
    source=sourcepath.read_text()
    for token,value in {'UNBOUND_STEP':step,'UNBOUND_TAB_HASH':draft['hash'],'UNBOUND_COMPACT_DOCUMENT':'','UNBOUND_PRETTY_DOCUMENT':'','UNBOUND_T0':w['t0'],'UNBOUND_CUTOFF':w['cleanupExpiresAt']}.items():
        assert source.count("'"+token+"'")==1
        source=source.replace("'"+token+"'",json.dumps(value))
    intent={'at':now.isoformat(),'step':step,'maximumForwards':1,'retry':False,'boundSourceSHA256':hashlib.sha256(source.encode()).hexdigest(),'window':w,'ownedPolicyIds':{'setup':'ANPAROHA7PNSTPQUQEUTR','boundary':boundary['policyId']}}
    fd=os.open(C/('Cleanup-'+step+'-INTENT.once.json'),os.O_CREAT|os.O_EXCL|os.O_WRONLY,0o600)
    with os.fdopen(fd,'w') as stream:
        json.dump(intent,stream,indent=2);stream.flush();os.fsync(stream.fileno())
    directory=os.open(C,os.O_RDONLY);os.fsync(directory);os.close(directory)
    script=OP/('c12-cleanup-'+step.lower()+'-once.js')
    with script.open('x') as stream:stream.write(source)
    script.chmod(0o600)
    print(json.dumps({'step':step,'scriptName':script.stem,'AWSCalls':0,'maximumForwards':1}))


if __name__=='__main__':main()
```

## completion-c12/bind_policies.py

Original machine-bound bytes SHA256 `fc2b8beef44c6a017451f68f3e4940d5f193ca2e271b121b47fdbc1676d5e5a0`; portable transcription SHA256 `fc2b8beef44c6a017451f68f3e4940d5f193ca2e271b121b47fdbc1676d5e5a0`. These differ when local bindings or typography were normalized.

```python
"""Pure local policy-data binding. This module performs no API or filesystem write."""
from datetime import datetime, timedelta
import json
import hashlib
import re


# Canonical content hashes derived from the hash-verified frozen round-3 templates.
PINNED_TEMPLATE_SHA256 = {'hikyo-158-ambient.policy.json': 'be9b30bd9a6f3e4d9ee9dd4f7f480eaaec42f8da1f104025beb8fcaa2cad38e3',
 'hikyo-158-ambient.trust.json': 'b39b3ee57c1fb9f10b3aea389429cd12d64326e631c23a9e992de9caf04bf188',
 'hikyo-158-assume.policy.json': '5e72eba03eb62d57fb0ac5d50d68e633b063b5f68dc821d7841ce77a5bc0d1ba',
 'hikyo-158-assume.trust.json': 'c1a5c0a03a3354bab2792cdc4b1633235bba1ce99309a38f190dcba624f82a4c',
 'hikyo-158-cleanup.policy.json': '7242df4500caa402bab0922e1eabae5441eb91127a677dbd07f8d889f45e88bc',
 'hikyo-158-cleanup.trust.json': 'b39b3ee57c1fb9f10b3aea389429cd12d64326e631c23a9e992de9caf04bf188',
 'hikyo-158-kms-denied.policy.json': '743ccfe2d9754404944cddc4510ea57be5eca6bd0294da0abaaaf68ce4f47fbe',
 'hikyo-158-kms-denied.trust.json': 'b39b3ee57c1fb9f10b3aea389429cd12d64326e631c23a9e992de9caf04bf188',
 'hikyo-158-oracle.policy.json': '2a78a1240f44df01fcc9824552745ff656e10bc90a8088649e069681980be537',
 'hikyo-158-oracle.trust.json': 'b39b3ee57c1fb9f10b3aea389429cd12d64326e631c23a9e992de9caf04bf188',
 'hikyo-158-source.policy.json': '0b7bf375f8d88c1af3536a5800d67f2ccd7272ffbdbd139dbca82fe1f009589d',
 'hikyo-158-source.trust.json': 'b39b3ee57c1fb9f10b3aea389429cd12d64326e631c23a9e992de9caf04bf188',
 'hikyo-158-static.policy.json': '3aaf3c7976d5610c2769f336c79ed69aff03aeaa82b5d56e7fe52891ef924280',
 'hikyo-158-static.trust.json': 'b39b3ee57c1fb9f10b3aea389429cd12d64326e631c23a9e992de9caf04bf188',
 'hikyo-158-web.policy.json': 'b89444e780ec2ddc9c732929da61e9ebc08ba22aab036b47383025baf978419a',
 'hikyo-158-web.trust.json': 'b39b3ee57c1fb9f10b3aea389429cd12d64326e631c23a9e992de9caf04bf188',
 'setup-role-boundary.json': '51290dc8c0f03a10df8d799b77a7233a64932d7911f19568064ef86d07415d00',
 'setup-user.managed-policy.json': '0ed3d2d602205f96b54487a7bf8001cb1760797316f037ea60d14cfb511a94ca'}

def bind_templates(templates, bindings):
    if set(templates) != set(PINNED_TEMPLATE_SHA256):
        raise ValueError('template-file-set')
    for name, document in templates.items():
        canonical = json.dumps(document, sort_keys=True, separators=(',', ':')).encode()
        if hashlib.sha256(canonical).hexdigest() != PINNED_TEMPLATE_SHA256[name]:
            raise ValueError('unapproved-template')
    required = {'keyARN', 'runPrefix', 'externalId', 't0', 'authExpiresAt', 'cleanupExpiresAt'}
    if set(bindings) != required:
        raise ValueError('binding-fields')
    if not re.fullmatch(r'arn:aws:kms:eu-central-1:099256073061:key/[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}', bindings['keyARN']):
        raise ValueError('key-arn')
    run_prefix = bindings['runPrefix']
    if not re.fullmatch(r'hikyo-e2e-[0-9]{8}c[0-9]{2}/', run_prefix):
        raise ValueError('run-prefix')
    t0, auth, cleanup = [datetime.fromisoformat(bindings[name]) for name in ['t0', 'authExpiresAt', 'cleanupExpiresAt']]
    if not all(bindings[name].endswith('Z') for name in ['t0', 'authExpiresAt', 'cleanupExpiresAt']):
        raise ValueError('utc-time-required')
    if auth != t0 + timedelta(hours=2) or cleanup != t0 + timedelta(hours=4):
        raise ValueError('window-shape')
    if not re.fullmatch(r'hikyo-158-[0-9]{8}c[0-9]{2}', bindings['externalId']):
        raise ValueError('external-id')
    # Every frozen ARN template already contains the separator after this token.
    prefix_component = run_prefix[:-1]
    replacements = {
        'UNBOUND_NEW_KEY_ARN': bindings['keyARN'],
        'UNBOUND_APPROVED_RUN_PREFIX': prefix_component,
        'UNBOUND_APPROVED_EXTERNAL_ID': bindings['externalId'],
        'UNBOUND_APPROVED_AUTH_UNTIL': bindings['authExpiresAt'],
        'UNBOUND_APPROVED_CLEANUP_UNTIL': bindings['cleanupExpiresAt'],
        'UNBOUND_APPROVED_SETUP_UNTIL': bindings['cleanupExpiresAt'],
    }
    audit = []

    def visit(value, path, name):
        if isinstance(value, dict):
            return {key: visit(child, path + [key], name) for key, child in value.items()}
        if isinstance(value, list):
            return [visit(child, path + [str(index)], name) for index, child in enumerate(value)]
        if not isinstance(value, str):
            return value
        result = value
        for token, replacement in replacements.items():
            if token not in value:
                continue
            if token.endswith('_UNTIL'):
                permitted = path[-1] == 'aws:CurrentTime' and 'Condition' in path
            elif token == 'UNBOUND_NEW_KEY_ARN':
                permitted = 'Resource' in path or path[-1] == 'secretsmanager:KmsKeyArn'
            elif token == 'UNBOUND_APPROVED_RUN_PREFIX':
                permitted = 'Resource' in path or path[-1] == 'kms:EncryptionContext:SecretARN'
            else:
                permitted = path[-1] == 'sts:ExternalId'
            if not permitted:
                raise ValueError('unapproved-leaf')
            result = result.replace(token, replacement)
        if 'UNBOUND_' in result:
            raise ValueError('unresolved-token')
        if result != value:
            audit.append({'file': name, 'path': '/' + '/'.join(path), 'before': value, 'after': result})
        return result

    documents = {name: visit(document, [], name) for name, document in templates.items()}
    for name in ['setup-user.managed-policy.json', 'setup-role-boundary.json']:
        if len(json.dumps(documents[name], sort_keys=True, separators=(',', ':'))) > 6144:
            raise ValueError('managed-policy-size')
    return documents, audit
```

## completion-c12/bind_protected_vars.py

Original machine-bound bytes SHA256 `1ba327f088da6abf253e8f18cd06e91a5b058f3714e7fa9576a81d390b353382`; portable transcription SHA256 `effda6ae0b5f47db7e9d4ea9b308b1b9b2c8b914ef3b1bb3f09f526ca8e57ee0`. These differ when local bindings or typography were normalized.

```python
from pathlib import Path
from datetime import datetime,timezone
import base64,hashlib,json,os,subprocess,time

def stored_bindings(values):
 expected={'AWS_ACCEPTANCE_APPROVED_WORKFLOW_SHA','AWS_ACCEPTANCE_APPROVED_SHA','AWS_ACCEPTANCE_APPROVED_RUN_ID','AWS_ACCEPTANCE_AUTHORIZATION_ID','AWS_ACCEPTANCE_RUN_PREFIX','AWS_ACCEPTANCE_AUTH_EXPIRES_AT','AWS_ACCEPTANCE_CLEANUP_EXPIRES_AT','AWS_ACCEPTANCE_KMS_KEY_ARN','AWS_ACCEPTANCE_EXTERNAL_ID','AWS_ACCEPTANCE_HISTORICAL_CLEANUP_ARN'}
 if type(values) is not dict or set(values)!=expected:raise ValueError('closed-binding-fields')
 if values['AWS_ACCEPTANCE_HISTORICAL_CLEANUP_ARN']!='':raise ValueError('historical-cleanup-must-be-disabled')
 if any(type(v) is not str or not v for k,v in values.items() if k!='AWS_ACCEPTANCE_HISTORICAL_CLEANUP_ARN'):raise ValueError('nonempty-exact-binding-values')
 return {k:v for k,v in values.items() if k!='AWS_ACCEPTANCE_HISTORICAL_CLEANUP_ARN'}

c=Path('<report-root>/completion-c12');r=c.parent;op=r/'private-operator';repo='Hikyo-Org/Hikyo';base='repos/'+repo+'/environments/hikyo-158-aws-sandbox'
run=json.loads((c/'WORKFLOW-DISPATCH-RESULT.json').read_text());w=json.loads((c/'WINDOW-BINDING.json').read_text());assert run['status']=='ONE_RUN_IDENTIFIED_SEPARATE_PROTECTED_APPROVAL_PENDING' and not run['sourceCorrespondenceStillRequired'];assert datetime.now(timezone.utc)<datetime.fromisoformat(w['provisioningCutoff'].replace('Z','+00:00'))
def api(path):return json.loads(subprocess.check_output(['gh','api',path]))
actual=api('repos/'+repo+'/actions/runs/'+run['runId']);assert actual['head_sha']==run['headSHA'] and actual['run_attempt']==1
old=json.loads((op/'c12-historical-secret-before-dispatch.json').read_text());assert old['status']==400 and json.loads(old['body'])['__type']=='ResourceNotFoundException' and (datetime.now(timezone.utc)-datetime.fromisoformat(old['at'].replace('Z','+00:00'))).total_seconds()<300
workflow=api('repos/'+repo+'/contents/.github/workflows/aws-acceptance.yml?ref='+run['headSHA']);workflow_bytes=base64.b64decode(workflow['content'],validate=False)
assert hashlib.sha256(workflow_bytes).hexdigest()=='0bb9e4fe28055d03ba7a055c3e7595aa316b3869d22eddd59bfafe2d54314b6a' and workflow_bytes.count(b'AWS_ACCEPTANCE_HISTORICAL_CLEANUP_ARN: \"\"')==1
assert api(base+'/variables')['total_count']==0
key=json.loads((c/'KEY-READBACK.json').read_text())['key']['Arn'];values={'AWS_ACCEPTANCE_APPROVED_WORKFLOW_SHA':run['headSHA'],'AWS_ACCEPTANCE_APPROVED_SHA':'c253ff9b1ccec7408f13366f8c5640c8c7cf3d0a','AWS_ACCEPTANCE_APPROVED_RUN_ID':run['runId'],'AWS_ACCEPTANCE_AUTHORIZATION_ID':w['authorizationId'],'AWS_ACCEPTANCE_RUN_PREFIX':'hikyo-e2e-20261028c12/','AWS_ACCEPTANCE_AUTH_EXPIRES_AT':w['authExpiresAt'],'AWS_ACCEPTANCE_CLEANUP_EXPIRES_AT':w['cleanupExpiresAt'],'AWS_ACCEPTANCE_KMS_KEY_ARN':key,'AWS_ACCEPTANCE_EXTERNAL_ID':'hikyo-158-20261028c12','AWS_ACCEPTANCE_HISTORICAL_CLEANUP_ARN':''}
folder=c/'github-bindings';folder.mkdir(mode=0o700,exist_ok=False)
stored=stored_bindings(values)
for name,value in stored.items():
 path=folder/(name+'.json');fd=os.open(path,os.O_WRONLY|os.O_CREAT|os.O_EXCL,0o600)
 with os.fdopen(fd,'w') as f:json.dump({'name':name,'value':value},f);f.flush();os.fsync(f.fileno())
 result=subprocess.run(['gh','api',base+'/variables','--method','POST','--input',str(path)],capture_output=True,timeout=20)
 if result.returncode:raise SystemExit('HOLD_PROTECTED_VARIABLE_BINDING_FAILED_NO_RETRY')
read=api(base+'/variables');assert read['total_count']==9 and {x['name']:x['value'] for x in read['variables']}==stored
pending=[]
for index in range(6):
 pending=api('repos/'+repo+'/actions/runs/'+run['runId']+'/pending_deployments')
 if pending:break
 if index<5:time.sleep(2)
assert len(pending)==1 and pending[0]['environment']['id']==24756660811
record={'at':datetime.now(timezone.utc).isoformat(),'runId':run['runId'],'runURL':run['url'],'attempt':1,'headSHA':run['headSHA'],'candidate':'c253ff9b1ccec7408f13366f8c5640c8c7cf3d0a','values':values,'storedValues':stored,'literalEmptyHistoricalWorkflowSHA256':hashlib.sha256(workflow_bytes).hexdigest(),'actualReadback':read,'pendingDeployment':pending,'status':'EXACT_NINE_BINDINGS_AND_LITERAL_EMPTY_READ_BACK_PENDING_OWNER_APPROVAL','ownerApprovalStillRequired':True}
(c/'PROTECTED-DEPLOYMENT-READBACK.json').write_text(json.dumps(record,indent=2)+'\n');print(json.dumps({k:record[k] for k in ['runId','runURL','headSHA','status','ownerApprovalStillRequired']}))
```

## completion-c12/bind_window_once.py

Original machine-bound bytes SHA256 `02df54767610ea66c371f4e6be00f14e0f96b3956fdc2c10db0b1a4a1cedaed9`; portable transcription SHA256 `11471df1ceb55efbfbdaf4f0376bf57f88ef4cefa13791e1ba7d36c365380161`. These differ when local bindings or typography were normalized.

```python
"""Bind the approved c12 grant to one later authenticated Ready; no AWS calls."""
from datetime import datetime, timedelta, timezone
from pathlib import Path
import hashlib
import json
import os

C = Path(__file__).resolve().parent
CANONICAL = Path('<approval-bank>/owner-assisted-20261028c12.window.once.json')
CLAIM_SHA = 'fea113e1367f578b3a0626068532d242826c597edf6f995647f3bcf9e9725e20'


def window_for(ready, now):
    assert ready['createdBy'] == 'user' and ready['ownerAvailableFirst40Minutes'] is True
    assert type(ready['position']) is int and ready['position'] > 3828
    at = datetime.fromisoformat(ready['authenticatedAt'].replace('Z', '+00:00'))
    assert at.utcoffset() == timedelta(0) and timedelta(0) <= now - at < timedelta(minutes=5)
    # Console guards accept whole UTC seconds. Round up once, never shift later.
    rounded = at.replace(microsecond=0) + (timedelta(seconds=1) if at.microsecond else timedelta(0))
    t0 = rounded + timedelta(minutes=10)
    utc = lambda value: value.strftime('%Y-%m-%dT%H:%M:%SZ')
    return {'boundAt': now.isoformat(), 't0': utc(t0), 'authExpiresAt': utc(t0 + timedelta(hours=2)), 'cleanupExpiresAt': utc(t0 + timedelta(hours=4)), 'provisioningCutoff': utc(t0 + timedelta(minutes=55)), 'authorizationId': 'hikyo158-owner-assisted-' + t0.strftime('%Y%m%dt%H%M%S') + '-c12', 'ownerReadyAt': ready['authenticatedAt'], 'ownerReadyPosition': ready['position'], 'automaticShift': False, 'claimSHA256': CLAIM_SHA}


def main():
    digest = lambda path: hashlib.sha256(path.read_bytes()).hexdigest()
    assert digest(C/'ACTIVE-CLAIM.json') == CLAIM_SHA
    publication = json.loads((C/'SOURCE-PUBLICATION-RECEIPT.json').read_text())
    assert publication['sourceAndLandedTreeIdentical'] and publication['mergeGroupRun']['conclusion'] == 'success'
    manifest = json.loads((C/'PREPARATION-PINS.console-qualified.json').read_text())
    assert all(digest(C/name) == pin for name, pin in manifest['files'].items())
    ready = json.loads((C/'READY-OWNER-ATTESTATION.json').read_text())
    assert json.loads((C/'OWNER-CONSOLE-PREFLIGHT.json').read_text())['status']=='PASS_C12_PREFLIGHT_WINDOW_UNBOUND'
    window = window_for(ready, datetime.now(timezone.utc))
    blob = (json.dumps(window, indent=2) + '\n').encode()
    fd = os.open(CANONICAL, os.O_CREAT | os.O_EXCL | os.O_WRONLY, 0o600)
    with os.fdopen(fd, 'wb') as stream:
        stream.write(blob); stream.flush(); os.fsync(stream.fileno())
    directory = os.open(CANONICAL.parent, os.O_RDONLY)
    os.fsync(directory); os.close(directory)
    os.link(CANONICAL, C/'WINDOW-BINDING.json')
    print(json.dumps(window))


if __name__ == '__main__':
    main()
```

## completion-c12/c12-billing-admission.js

Original machine-bound bytes SHA256 `4604f4fbb32c99c1d850bb05345e66f75aedd22ccdecc7cba6c70a8c1da2c3a0`; portable transcription SHA256 `0150d3cd2b043b7b23114c97ed12e3d2523f649b715af5ff2d0df9f4c3d38fe3`. These differ when local bindings or typography were normalized.

```javascript
async page => {
 const tab=await page.context().newPage();
 try{await tab.goto('https://us-east-1.console.aws.amazon.com/billing/home?region=eu-central-1#/bills',{waitUntil:'domcontentloaded',timeout:15000});
 await tab.getByRole('button',{name:'Expand all',exact:true}).first().click({timeout:15000});
 await tab.waitForFunction(()=>Array.from(document.querySelectorAll('table')).flatMap(t=>t.innerText.split('\n').map(x=>x.trim()).filter(Boolean)).filter(x=>/^[0-9][0-9,.]*\s+(Keys|Requests|Secrets|Request|API Requests)$/.test(x)).length===8,{},{timeout:12000});
 return{at:new Date().toISOString(),tables:await tab.evaluate(()=>Array.from(document.querySelectorAll('table')).map(n=>n.innerText)),totals:await tab.evaluate(()=>document.body.innerText.split('\n').filter(line=>/Total|USD|tax|credit|estimated|month|October/i.test(line)).slice(-100))};}finally{if(!tab.isClosed())await tab.close();}
}
```

## completion-c12/c12-boundary-draft-DRAFT.js

Original machine-bound bytes SHA256 `ade2cab2d764315b7d3d222fa63259314c766245b10a2635a61c9770f3be370b`; portable transcription SHA256 `6677d6fe3e46a7351a649afae30dd6e5b37b0057ec5c6ebd66eef46c4160d666`. These differ when local bindings or typography were normalized.

```javascript
async page=>{
 const context=page.context();
 for(const old of context.pages().filter(p=>p.url().startsWith('https://us-east-1.console.aws.amazon.com/iam/home')&&p.url().includes('#/policies/create')))await old.close();
 const tab=await context.newPage();
 await tab.goto('https://us-east-1.console.aws.amazon.com/iam/home?region=eu-central-1#/policies/create',{waitUntil:'domcontentloaded',timeout:15000});
 await tab.getByRole('button',{name:'JSON',exact:true}).click({timeout:10000});
 const policyDocument='UNBOUND_EXACT_APPROVED_POLICY_DOCUMENT';
 const input=tab.locator('textarea.ace_text-input');await input.focus();await input.press('Meta+A');await tab.keyboard.insertText(policyDocument);
 const actual=await tab.locator('.ace_editor').evaluate(el=>el.env.editor.getValue());if(actual.trim()!==policyDocument.trim())throw Error('editor-mismatch');
 await tab.getByRole('button',{name:'Next',exact:true}).click({timeout:5000});
 await tab.getByRole('heading',{name:'Review and create',exact:true}).waitFor({state:'visible',timeout:10000});
 await tab.getByLabel('Policy name',{exact:true}).fill('hikyo-158-role-boundary');
 return{at:new Date().toISOString(),hash:await tab.evaluate(()=>location.hash),policyDocumentExact:true,view:await tab.evaluate(()=>document.body.innerText.slice(-16000)),finalButtonEnabled:await tab.getByRole('button',{name:'Create policy',exact:true}).isEnabled(),serviceWorkers:context.serviceWorkers().length};
}
```

## completion-c12/c12-boundary-readback.js

Original machine-bound bytes SHA256 `ed7796ca6a53162c2a749a26aab9b64cede8a7fe3b08a3ae473889dcd26fd197`; portable transcription SHA256 `987fc4b943956d371941e5d37b07a24411e6f73a4543b61859f130fef6195e87`. These differ when local bindings or typography were normalized.

```javascript
async page=>{
 const context=page.context(),tab=await context.newPage(),records=[],tasks=[];
 const arn='arn:aws:iam::099256073061:policy/hikyo-158-role-boundary';
 const allowed=new Set(['GetPolicy','ListPolicyVersions','GetPolicyVersion','ListPolicyTags','ListEntitiesForPolicy']);
 const listener=r=>{const q=r.request(),request=q.postData()||'',m=request.match(/(?:^|&)Action=([^&]+)/);if(q.url()!=='https://iam.amazonaws.com/'||!m||!allowed.has(m[1])||!request.includes(encodeURIComponent(arn)))return;tasks.push(r.text().then(body=>{if(body.length>65536)throw Error('metadata-too-large');records.push({operation:m[1],request,status:r.status(),body});}).catch(error=>records.push({operation:m[1],captureFailure:error.name})));};
 context.on('response',listener);
 try{
 await tab.goto('https://us-east-1.console.aws.amazon.com/iam/home?region=eu-central-1#/policies/details/'+encodeURIComponent(arn),{waitUntil:'domcontentloaded',timeout:15000});
 await tab.getByRole('tab',{name:/^Policy versions/}).click({timeout:10000});await tab.getByRole('row').filter({hasText:'Version 1'}).first().waitFor({state:'visible',timeout:10000});
 await tab.getByRole('tab',{name:/^Tags/}).click({timeout:5000});await tab.waitForLoadState('networkidle',{timeout:5000}).catch(()=>{});await Promise.all(tasks);return{at:new Date().toISOString(),records};
 }finally{context.off('response',listener);await Promise.all(tasks);if(!tab.isClosed())await tab.close();}
}
```

## completion-c12/c12-cleanup-deleteunusedboundary-draft.js

Original machine-bound bytes SHA256 `f29964da36fa6b33882e187719151a3b5b80cc15878ef1735a8fc67d8629e7b6`; portable transcription SHA256 `f29964da36fa6b33882e187719151a3b5b80cc15878ef1735a8fc67d8629e7b6`. These differ when local bindings or typography were normalized.

```javascript
async page=>{
 const context=page.context(),hash='#/policies/details/arn%3Aaws%3Aiam%3A%3A099256073061%3Apolicy%2Fhikyo-158-role-boundary?section=permissions';
 const tabs=context.pages().filter(p=>p.url().startsWith('https://us-east-1.console.aws.amazon.com/iam/home')&&p.url().endsWith(hash));
 if(tabs.length!==1)throw Error('exact-owned-policy-tab-unavailable');
 const tab=tabs[0],dialog=tab.getByRole('dialog'),text=await dialog.innerText();
 if(!text.includes('Delete hikyo-158-role-boundary policy and all its versions permanently?'))throw Error('wrong-owned-policy-confirmation');
 await dialog.getByPlaceholder('hikyo-158-role-boundary',{exact:true}).fill('hikyo-158-role-boundary');
 if(!await dialog.getByRole('button',{name:'Delete',exact:true}).isEnabled())throw Error('confirmation-not-ready');
 return{at:new Date().toISOString(),hash,dialogText:text,confirmedName:'hikyo-158-role-boundary',AWSWritesForwarded:0};
}
```

## completion-c12/c12-cleanup-restoresetupv5-DRAFT.js

Original machine-bound bytes SHA256 `e6af83600f9b4d618ea97aab0d4de524c51b20c0e7dbc1f8ecd9f7145ff14306`; portable transcription SHA256 `28772bd4e44dba142ff305ee394a47649f3181764732697cbf26f79667d164eb`. These differ when local bindings or typography were normalized.

```javascript
async page=>{
 const context=page.context(),tab=await context.newPage(),arn='arn:aws:iam::099256073061:policy/hikyo-158-setup-policy';
 await context.route('https://iam.amazonaws.com/',async route=>{const raw=route.request().postData()||'',m=raw.match(/(?:^|&)Action=([^&]+)/);if(route.request().method()==='OPTIONS'||m&&/^(Get|List)/.test(m[1]))return await route.continue();return await route.abort('blockedbyclient');});
 const pending=tab.waitForResponse(r=>r.url()==='https://iam.amazonaws.com/'&&(r.request().postData()||'').startsWith('Action=GetPolicy&')&&(r.request().postData()||'').includes(encodeURIComponent(arn)),{timeout:12000}).then(async r=>({status:r.status(),body:await r.text()})).catch(()=>({captureFailure:true}));
 await tab.goto('https://us-east-1.console.aws.amazon.com/iam/home?region=eu-central-1#/policies/details/'+encodeURIComponent(arn),{waitUntil:'domcontentloaded',timeout:15000});
 const metadata=await pending;if(metadata.status!==200||!metadata.body.includes('<PolicyId>ANPAROHA7PNSTPQUQEUTR</PolicyId>')||!metadata.body.includes('<DefaultVersionId>UNBOUND_ACTUAL_NEW_SETUP_VERSION</DefaultVersionId>'))throw Error('retained-policy-identity-or-default-mismatch');
 await tab.getByRole('tab',{name:/^Policy versions/}).click({timeout:10000});await tab.getByRole('row').filter({hasText:'Version 5'}).first().getByRole('checkbox').check({timeout:10000});
 const button=tab.getByRole('button',{name:'Set as default',exact:true});if(await button.count()!==1||!await button.isEnabled())throw Error('unsupported-default-button');
 return{at:new Date().toISOString(),hash:await tab.evaluate(()=>location.hash),operation:'SetDefaultPolicyVersion',finalButton:'Set as default',metadata,selectedVersion:'v5',AWSWritesForwarded:0};
}
```

## completion-c12/c12-final-iam-closure.js

Original machine-bound bytes SHA256 `646b47d171a91b379c8cfd8a995953941cc08b031249e3a7e86bae01c63ac93a`; portable transcription SHA256 `646b47d171a91b379c8cfd8a995953941cc08b031249e3a7e86bae01c63ac93a`. These differ when local bindings or typography were normalized.

```javascript
async page=>{
 const context=page.context(),records=[],tasks=[],tabs=[];
 const policy='arn:aws:iam::099256073061:policy/hikyo-158-setup-policy',boundary='arn:aws:iam::099256073061:policy/hikyo-158-role-boundary';
 const allowed=new Set(['ListRoles','ListOpenIDConnectProviders','GetUser','ListMFADevices','ListAttachedUserPolicies','ListUserPolicies','ListGroupsForUser','ListAccessKeys','GetPolicy','ListPolicyVersions','GetPolicyVersion']);
 const listener=r=>{
  const q=r.request(),request=q.postData()||'',m=request.match(/(?:^|&)Action=([^&]+)/);
  if(q.url()!=='https://iam.amazonaws.com/'||!m||!allowed.has(m[1]))return;
  const operation=m[1];
  if(!['ListRoles','ListOpenIDConnectProviders'].includes(operation)&&!request.includes('UserName=hikyo-158-setup')&&!request.includes(encodeURIComponent(policy))&&!request.includes(encodeURIComponent(boundary)))return;
  tasks.push(r.text().then(body=>{if(body.length>131072)throw Error('metadata-too-large');records.push({operation,request,status:r.status(),body});}).catch(error=>records.push({operation,captureFailure:error.name})));
 };
 context.on('response',listener);
 try{
  const routes=['#/roles','#/identity_providers','#/users/details/hikyo-158-setup?section=security_credentials','#/users/details/hikyo-158-setup?section=permissions','#/policies/details/'+encodeURIComponent(policy),'#/policies/details/'+encodeURIComponent(boundary)];
  for(let index=0;index<routes.length;index+=2){
   await Promise.all(routes.slice(index,index+2).map(async route=>{
    const tab=await context.newPage();tabs.push(tab);await tab.goto('https://us-east-1.console.aws.amazon.com/iam/home?region=eu-central-1'+route,{waitUntil:'domcontentloaded',timeout:15000});
    if(route==='#/policies/details/'+encodeURIComponent(policy)){
     await tab.getByRole('tab',{name:/^Policy versions/}).click({timeout:10000});
     await tab.getByRole('row').filter({hasText:'Version 5'}).first().waitFor({state:'visible',timeout:10000});
    }else await tab.waitForLoadState('networkidle',{timeout:5000}).catch(()=>{});
   }));
  }
  await Promise.all(tasks);return{at:new Date().toISOString(),records};
 }finally{context.off('response',listener);await Promise.all(tasks);for(const tab of tabs)if(!tab.isClosed())await tab.close();}
}
```
