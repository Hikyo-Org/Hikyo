# Prepared operator source archive, part 5/9

**ARCHIVE ONLY, PARKED, LIVE QUALIFICATION NOT ESTABLISHED.** This preserves the prepared small-route operators and their tests so local report deletion does not erase the implementation. The transcription normalizes machine-local paths to `<report-root>`, `<approval-bank>`, `<application-checkout>`, `<trusted-source-checkout>` or `<local-artifact>`. They are reconstruction placeholders, not working filesystem paths. Original and normalized digests are both recorded. All runtime/source pins and concrete bindings must be re-established before use. Do not copy this code directly into a live operation or claim that normalization preserved an old qualification. In particular the historical unattended helper is an agent interpretation of an overnight request; present parking overrides it, and all applicable source/Ready/policy/deployment/merge gates must be reconciled with current owner direction. No credentials, authentication storage or private challenge URLs are included.

## completion-c12/prepare_iam_once.py

Original machine-bound bytes SHA256 `f8aa31e47129e50849a462bc3b6d3ff7d65fa6369fbe01131dcfba1d235fcd62`; portable transcription SHA256 `71c7d701712d15c664e192e70c5063b454631c31540afeb53fa5dd0e8cf04b65`. These differ when local bindings or typography were normalized.

```python
from pathlib import Path
from datetime import datetime,timezone
import hashlib,json,os,sys
r=Path('<report-root>');c=r/'completion-c12';op=r/'private-operator'
operation=sys.argv[1];assert operation in ['CreatePolicy','DeletePolicyVersion','CreatePolicyVersion']
approval=json.loads((c/'POLICY-OWNER-APPROVAL.json').read_text());policies=json.loads((c/'POLICY-SET.json').read_text());assert approval['canonicalChangeSetSHA256']==policies['canonicalChangeSetSHA256']
w=json.loads((c/'WINDOW-BINDING.json').read_text());now=datetime.now(timezone.utc)
assert datetime.fromisoformat(w['t0'].replace('Z','+00:00'))<=now<datetime.fromisoformat(w['provisioningCutoff'].replace('Z','+00:00'))
sourcepath=c/'iam-final-action-DRAFT.js';assert hashlib.sha256(sourcepath.read_bytes()).hexdigest()==policies['IAMLimiterSHA256']
if operation=='CreatePolicy':name='setup-role-boundary.json';button='Create policy';tabhash='#/policies/create'
else:
 name='setup-user.managed-policy.json';button='Delete' if operation=='DeletePolicyVersion' else 'Save changes'
 draft=json.loads((op/('c12-v11-delete-draft.json' if operation=='DeletePolicyVersion' else 'c12-setup-draft.json')).read_text());tabhash=draft['hash'];button=draft['finalButton']
 if operation=='DeletePolicyVersion':
  assert (c/'BOUNDARY-READBACK.json').is_file()
  archive=json.loads((c/'ARCHIVED-SETUP-V11.json').read_text());assert archive['version']=='v11' and archive['nondefault'] is True
  assert archive['nativeReceiptSHA256']==hashlib.sha256((op/'c12-setup-v11-archive.json').read_bytes()).hexdigest()
 else:assert (c/'V11-DELETION-READBACK.json').is_file()
compact=(c/'resolved'/name.replace('.json','.compact.json')).read_text();pretty=(c/'resolved'/name).read_text()
assert hashlib.sha256(compact.encode()).hexdigest()==policies['documents'][name]['canonicalSHA256']
replacements={'UNBOUND_OPERATION':operation,'UNBOUND_FINAL_BUTTON':button,'UNBOUND_TAB_HASH':tabhash,'UNBOUND_APPROVED_COMPACT_DOCUMENT':compact,'UNBOUND_APPROVED_PRETTY_DOCUMENT':pretty,'UNBOUND_T0':w['t0'],'UNBOUND_PROVISIONING_CUTOFF':w['provisioningCutoff']}
source=sourcepath.read_text()
for old,new in replacements.items():assert source.count("'"+old+"'")==1;source=source.replace("'"+old+"'",json.dumps(new))
script=op/('c12-'+operation.lower()+'-once.js');assert not script.exists();script.write_text(source);script.chmod(0o600)
intent={'recordedAt':now.isoformat(),'operation':operation,'maximumNativeForwards':1,'retry':False,'boundScriptSHA256':hashlib.sha256(source.encode()).hexdigest(),'sourceSHA256':hashlib.sha256(sourcepath.read_bytes()).hexdigest(),'canonicalChangeSetSHA256':policies['canonicalChangeSetSHA256'],'approvalPosition':approval['position'],'window':w,'target':'hikyo-158-role-boundary' if operation=='CreatePolicy' else 'arn:aws:iam::099256073061:policy/hikyo-158-setup-policy','version':'v11 only owned nondefault, archived before deletion' if operation=='DeletePolicyVersion' else 'one new setup version, use actual acknowledged ID, SetAsDefault=true' if operation=='CreatePolicyVersion' else 'initial v1','documentSHA256':policies['documents'][name]['canonicalSHA256'] if operation!='DeletePolicyVersion' else None}
fd=os.open(c/(operation+'-INTENT.json'),os.O_WRONLY|os.O_CREAT|os.O_EXCL,0o600)
with os.fdopen(fd,'w') as f:json.dump(intent,f,indent=2);f.flush();os.fsync(f.fileno())
dirfd=os.open(c,os.O_RDONLY);os.fsync(dirfd);os.close(dirfd)
print(json.dumps({'prepared':operation,'scriptName':script.stem,'maximumNativeForwards':1}))
```

## completion-c12/prior_key_schedules.py

Original machine-bound bytes SHA256 `5fc472cb06adb4afa74fc88f5993ac668479da8f4b88f98ce41aebb920f947fc`; portable transcription SHA256 `5fc472cb06adb4afa74fc88f5993ac668479da8f4b88f98ce41aebb920f947fc`. These differ when local bindings or typography were normalized.

```python
"""Read-only reconciliation of pinned prior KMS deletion schedules."""
from datetime import datetime, timezone
import json


def utc(value):
    parsed = datetime.fromisoformat(value.replace('Z', '+00:00'))
    assert parsed.tzinfo is not None and parsed.utcoffset().total_seconds() == 0
    return parsed


def reconcile(records, schedules, absent_selectors, read_started_at, read_finished_at):
    started, finished = utc(read_started_at), utc(read_finished_at)
    assert 0 <= (finished - started).total_seconds() < 300
    assert set(schedules).isdisjoint(absent_selectors)
    expected = set(schedules) | set(absent_selectors)
    assert len(records) == len(expected)
    assert {row['id'] for row in records} == expected
    pending, deleted = [], []
    for row in records:
        assert 'captureFailure' not in row
        selector = row['id']
        assert row['operation'] == 'DescribeKey'
        assert json.loads(row['request']) == {'KeyId': selector}
        body = json.loads(row['body'])
        if selector not in schedules:
            assert row['status'] == 400 and body['__type'] == 'NotFoundException'
            continue
        scheduled = utc(schedules[selector])
        if row['status'] == 400:
            assert body['__type'] == 'NotFoundException' and started >= scheduled
            deleted.append(selector)
            continue
        assert row['status'] == 200
        metadata = body['KeyMetadata']
        assert metadata['KeyId'] == selector
        assert metadata['Arn'] == 'arn:aws:kms:eu-central-1:099256073061:key/' + selector
        assert metadata['AWSAccountId'] == '099256073061'
        assert metadata['KeyState'] == 'PendingDeletion' and metadata['Enabled'] is False
        assert metadata['DeletionDate'] == scheduled.timestamp()
        pending.append(selector)
    return {
        'pendingIds': sorted(pending),
        'naturallyDeletedIds': sorted(deleted),
        'strictAbsentSelectors': sorted(absent_selectors),
        'readStartedAt': started.astimezone(timezone.utc).isoformat(),
        'readFinishedAt': finished.astimezone(timezone.utc).isoformat(),
        'mutations': 0,
    }
```

## completion-c12/provision_fixture_once.py

Original machine-bound bytes SHA256 `60e86d575d28802034a7c12d62607c88a32c550d354ab2d7c0385f146bc2d5d5`; portable transcription SHA256 `7ef157f74ccc41230d8c67367d9fe35f15db5f80bca8ce1a0ec3783cbea9b11a`. These differ when local bindings or typography were normalized.

```python
"""One c07 fixture from the exact approved documents; no credential export."""
from datetime import datetime,timezone
from pathlib import Path
import hashlib,json,os,re,subprocess,urllib.parse
r=Path('<report-root>');c=r/'completion-c12';aws=Path('/opt/homebrew/Cellar/awscli/2.38.4/libexec/bin/aws');login=Path('<local-artifact>/aws-local-login')
assert hashlib.sha256(aws.read_bytes()).hexdigest()=='b643e78fd6c61395b1a89bfe2f1504a38115261aa29f42d1e5e35e0620119f61'
assert json.loads((c/'SETUP-PROBE-AFTER-LOGIN-RESULT.json').read_text())['status']=='PASS_LITERAL_MFA_AND_UNUSED_SENTINEL'
approval=json.loads((c/'POLICY-OWNER-APPROVAL.json').read_text());policies=json.loads((c/'POLICY-SET.json').read_text());assert approval['canonicalChangeSetSHA256']==policies['canonicalChangeSetSHA256']
w=json.loads((c/'WINDOW-BINDING.json').read_text());cutoff=datetime.fromisoformat(w['provisioningCutoff'].replace('Z','+00:00'));assert datetime.fromisoformat(w['t0'].replace('Z','+00:00'))<=datetime.now(timezone.utc)<cutoff
assert not (c/'FIXTURE-RESULT.json').exists() and not (c/'fixture').exists()
claimfd=os.open(c/'C12-POOL-FIXTURE-CLAIM.once.json',os.O_CREAT|os.O_EXCL|os.O_WRONLY,0o600)
with os.fdopen(claimfd,'w') as f:json.dump({'at':datetime.now(timezone.utc).isoformat(),'cumulativeFixtureMaximum':1,'newReservations':32,'lifetime':336},f);f.flush();os.fsync(f.fileno())
folder=c/'fixture';folder.mkdir(mode=0o700,exist_ok=False)
env={k:os.environ[k] for k in ('PATH','TMPDIR','LANG') if k in os.environ};env.update(AWS_CONFIG_FILE=str(login/'config'),AWS_SHARED_CREDENTIALS_FILE=str(login/'credentials'),AWS_LOGIN_CACHE_DIRECTORY=str(login/'cache'),AWS_REGION='eu-central-1',AWS_DEFAULT_REGION='eu-central-1',AWS_EC2_METADATA_DISABLED='true',AWS_IGNORE_CONFIGURED_ENDPOINT_URLS='true',AWS_CLI_AUTO_PROMPT='off',AWS_PAGER='',AWS_MAX_ATTEMPTS='1',AWS_RETRY_MODE='standard')
records=[];owned={'provider':None,'roles':[]};policyname='hikyo-158-acceptance';tags=[{'Key':'HikyoIssue','Value':'158'},{'Key':'Authorization','Value':w['authorizationId']}]
def write(path,data):
 fd=os.open(path,os.O_CREAT|os.O_EXCL|os.O_WRONLY,0o600)
 with os.fdopen(fd,'w') as f:json.dump(data,f,indent=2);f.write('\n');f.flush();os.fsync(f.fileno())
 d=os.open(path.parent,os.O_RDONLY);os.fsync(d);os.close(d)
def save(status):
 (c/'FIXTURE-RESULT.json').write_text(json.dumps({'at':datetime.now(timezone.utc).isoformat(),'status':status,'owned':owned,'records':records,'inlinePolicyName':policyname,'rawCredentialOrPrivateErrorOutputRetained':False,'newSecretCalls':0,'newReservationsCharged':32,'lifetime':336},indent=2)+'\n')
def call(operation,data,mutation=False):
 assert datetime.fromisoformat(w['t0'].replace('Z','+00:00'))<=datetime.now(timezone.utc)<cutoff
 serial=len(records)+1;request=folder/(str(serial).zfill(3)+'-'+operation+'-request.json');write(request,data)
 if mutation:write(folder/(str(serial).zfill(3)+'-'+operation+'-intent.once.json'),{'recordedAt':datetime.now(timezone.utc).isoformat(),'operation':operation,'requestSHA256':hashlib.sha256(request.read_bytes()).hexdigest(),'maximumNativeAttempts':1,'retry':False,'cutoff':w['provisioningCutoff']})
 args=[str(aws),'--profile','hikyo-158-setup','--region','eu-central-1','--cli-connect-timeout','10','--cli-read-timeout','10','--cli-error-format','json','--no-cli-pager','--no-cli-auto-prompt','--no-paginate','--output','json','iam',operation,'--cli-input-json','file://'+str(request)]
 stamp=datetime.now(timezone.utc).isoformat()
 try:result=subprocess.run(args,env=env,capture_output=True,timeout=min(10,(cutoff-datetime.now(timezone.utc)).total_seconds()))
 except subprocess.TimeoutExpired:records.append({'operation':operation,'startedAt':stamp,'status':'HOLD_TIMEOUT_AMBIGUOUS_IF_MUTATION','mutation':mutation});save('HOLD_NO_RETRY');raise SystemExit(1)
 record={'operation':operation,'startedAt':stamp,'receivedAt':datetime.now(timezone.utc).isoformat(),'exitCode':result.returncode,'mutation':mutation};records.append(record)
 if result.returncode:

  try:
   error=json.loads(result.stderr);record['errorCode']=error.get('Code',error.get('code','UNCLASSIFIED_PRIVATE_OUTPUT_WITHHELD'))
  except (ValueError,AttributeError):record['errorCode']='UNCLASSIFIED_PRIVATE_OUTPUT_WITHHELD'
  save('HOLD_NO_RETRY');raise SystemExit(1)
 if not result.stdout.strip():
  assert operation=='put-role-policy';body={};record['emptyNativeSuccess']=True
 else:body=json.loads(result.stdout)
 record['metadata']=body;write(folder/(str(serial).zfill(3)+'-'+operation+'-result.json'),record);save('IN_PROGRESS');return body
def document(name):
 path=c/'resolved'/name;doc=json.loads(path.read_text());canonical=json.dumps(doc,sort_keys=True,separators=(',',':'));assert hashlib.sha256(canonical.encode()).hexdigest()==policies['documents'][name]['canonicalSHA256'];return doc,canonical
def same_tags(items):return sorted(items,key=lambda x:x['Key'])==sorted(tags,key=lambda x:x['Key'])
def decoded(value):return json.loads(urllib.parse.unquote(value)) if isinstance(value,str) else value
try:
 names=['source','static','ambient','web','kms-denied','oracle','cleanup','assume'];assert len(names)==8
 for name in names:document('hikyo-158-'+name+'.trust.json');document('hikyo-158-'+name+'.policy.json')
 rolelist=call('list-roles',{'MaxItems':1000});assert rolelist['IsTruncated'] is False and not rolelist.get('Marker');assert not {x['RoleName'].lower() for x in rolelist['Roles']} & {'hikyo-158-'+x for x in names}
 providerlist=call('list-open-id-connect-providers',{});assert providerlist['OpenIDConnectProviderList']==[]
 issuer='arn:aws:iam::099256073061:oidc-provider/token.actions.githubusercontent.com'
 created=call('create-open-id-connect-provider',{'Url':'https://token.actions.githubusercontent.com','ClientIDList':['sts.amazonaws.com'],'Tags':tags},True);assert created['OpenIDConnectProviderArn']==issuer;owned['provider']={'arn':issuer,'acknowledged':True,'tags':tags};save('IN_PROGRESS')
 read=call('get-open-id-connect-provider',{'OpenIDConnectProviderArn':issuer});assert read['Url'] in ['token.actions.githubusercontent.com','https://token.actions.githubusercontent.com'] and read['ClientIDList']==['sts.amazonaws.com'] and same_tags(read['Tags']);owned['provider']['readback']=read
 for name in names:
  role='hikyo-158-'+name;trust,trusttext=document(role+'.trust.json');policy,policytext=document(role+'.policy.json');duration=14400 if name=='cleanup' else 3600;boundary='arn:aws:iam::099256073061:policy/hikyo-158-role-boundary'
  created=call('create-role',{'RoleName':role,'Path':'/','AssumeRolePolicyDocument':trusttext,'MaxSessionDuration':duration,'PermissionsBoundary':boundary,'Tags':tags},True)['Role'];arn='arn:aws:iam::099256073061:role/'+role
  assert created['Arn']==arn and created['RoleName']==role and created['RoleId'];entry={'name':role,'arn':arn,'roleId':created['RoleId'],'createDate':created['CreateDate'],'maxSessionDuration':duration,'tags':tags,'boundary':boundary};owned['roles'].append(entry);save('IN_PROGRESS')
  read=call('get-role',{'RoleName':role})['Role'];assert read['Arn']==arn and read['RoleId']==entry['roleId'] and read['Path']=='/' and read['MaxSessionDuration']==duration and read['PermissionsBoundary']['PermissionsBoundaryArn']==boundary and same_tags(read['Tags']) and decoded(read['AssumeRolePolicyDocument'])==trust
  call('put-role-policy',{'RoleName':role,'PolicyName':policyname,'PolicyDocument':policytext},True);entry['inlinePolicyName']=policyname;save('IN_PROGRESS')
  inline=call('get-role-policy',{'RoleName':role,'PolicyName':policyname});assert inline['RoleName']==role and inline['PolicyName']==policyname and decoded(inline['PolicyDocument'])==policy
  inline_names=call('list-role-policies',{'RoleName':role,'MaxItems':1000});assert inline_names['IsTruncated'] is False and inline_names['PolicyNames']==[policyname]
  attached=call('list-attached-role-policies',{'RoleName':role,'MaxItems':1000});assert attached['IsTruncated'] is False and attached['AttachedPolicies']==[];entry['effectiveReadbackVerified']=True;save('IN_PROGRESS')
 save('PASS_EIGHT_EXACT_ROLES_PROVIDER_POLICIES_TRUSTS_BOUNDARIES_TAGS')
 print(json.dumps({'status':'PASS_EIGHT_EXACT_ROLES_PROVIDER_POLICIES_TRUSTS_BOUNDARIES_TAGS','roles':len(owned['roles']),'provider':issuer,'newSecretCalls':0,'lifetime':336}))
except (AssertionError,KeyError,ValueError):
 save('HOLD_METADATA_MISMATCH_NO_RETRY');print(json.dumps({'status':'HOLD_METADATA_MISMATCH_NO_RETRY','rolesAcknowledged':len(owned['roles']),'providerAcknowledged':bool(owned['provider'])}));raise SystemExit(1)
```

## completion-c12/qualify_unattended.py

Original machine-bound bytes SHA256 `8b4b39477647a0067a8647c6e76db83577413f528a4c90c6415dc1e4908d8812`; portable transcription SHA256 `8b4b39477647a0067a8647c6e76db83577413f528a4c90c6415dc1e4908d8812`. These differ when local bindings or typography were normalized.

```python
"""Finite local qualification; no authentication or AWS client invocation."""
from datetime import datetime, timezone
from pathlib import Path
import ast
import hashlib
import json
import os
import subprocess
import tempfile

C = Path(__file__).resolve().parent
sha = lambda path: hashlib.sha256(path.read_bytes()).hexdigest()
names = ['unattended_c12.py', 'test_unattended_c12.py',
         'setup_probes_existing_provider_once.py', 'qualify_unattended.py',
         'UNATTENDED-SOURCE-DERIVATION.json', 'UNATTENDED-AUTHORITY-DISPOSITION.json',
         'OWNER-UNATTENDED-REQUEST.json', 'TYPED-OFFICIAL-PROVIDER-RESULT-02.json']
pins = {name: sha(C / name) for name in names}
for name, pin in json.loads((C / 'PREPARATION-PINS.console-qualified.json').read_text())['files'].items():
    assert sha(C / name) == pin, name
for name in names:
    if name.endswith('.py'):
        ast.parse((C / name).read_text(), filename=name)
env = {key: os.environ[key] for key in ('PATH', 'HOME', 'TMPDIR', 'LANG') if key in os.environ}
env.update(PYTHONDONTWRITEBYTECODE='1', AWS_EC2_METADATA_DISABLED='true')
python = str(C.parent / 'completion-c08/tools/venv/bin/python')
with tempfile.TemporaryDirectory(prefix='offline-unattended-', dir=C) as folder:
    for name in ('config', 'credentials'):
        Path(folder, name).touch(mode=0o600)
    env.update(AWS_CONFIG_FILE=str(Path(folder, 'config')), AWS_SHARED_CREDENTIALS_FILE=str(Path(folder, 'credentials')))
    with (C / 'UNATTENDED-QUALIFICATION.log').open('x') as log:
        result = subprocess.run([python, '-m', 'unittest', 'discover', '-s', str(C), '-p', 'test_*.py', '-v'], cwd=C, env=env, stdout=log, stderr=subprocess.STDOUT, timeout=90)
assert all(sha(C / name) == pin for name, pin in pins.items())
receipt = {'at': datetime.now(timezone.utc).isoformat(), 'passed': result.returncode == 0,
           'exitCode': result.returncode, 'sourcePins': pins,
           'logSHA256': sha(C / 'UNATTENDED-QUALIFICATION.log'),
           'original104PinsPassed': True, 'sourceUnchanged': True,
           'temporaryConfigurationRemoved': True, 'AWSCalls': 0,
           'formalReview': False, 'independentCLEAN': False, 'review6': False,
           'liveOwnerAdmission': 'HOLD_NO_WINDOW_OR_RESOURCE'}
(C / 'UNATTENDED-QUALIFICATION.json').write_text(json.dumps(receipt, indent=2) + '\n')
print(json.dumps(receipt))
raise SystemExit(result.returncode)
```

## completion-c12/refresh_prices.py

Original machine-bound bytes SHA256 `78da4f860eb413245fbf50f09421ce7f0e47d70c285a59cbc79340cb0f24830d`; portable transcription SHA256 `78da4f860eb413245fbf50f09421ce7f0e47d70c285a59cbc79340cb0f24830d`. These differ when local bindings or typography were normalized.

```python
"""Fetch public regional AWS pricing; no authenticated AWS calls."""
from datetime import datetime, timezone
from decimal import Decimal
from pathlib import Path
import hashlib
import json
import urllib.request

C=Path(__file__).resolve().parent
sources=[]
for service,wanted in [('awskms',{'eu-central-1-KMS-Keys':Decimal('1.00'),'eu-central-1-KMS-Requests':Decimal('0.000003')}),('AWSSecretsManager',{'EUC1-AWSSecretsManager-Secrets':Decimal('0.40'),'EUC1-AWSSecretsManagerAPIRequest':Decimal('0.000005')})]:
 url='https://pricing.us-east-1.amazonaws.com/offers/v1.0/aws/'+service+'/current/eu-central-1/index.json'
 with urllib.request.urlopen(url,timeout=30) as response:
  assert response.status==200
  blob=response.read(2*1024*1024+1)
 assert len(blob)<=2*1024*1024
 data=json.loads(blob);rates=[]
 for sku,product in data['products'].items():
  usage=product['attributes'].get('usagetype')
  if usage not in wanted:continue
  for term in data['terms']['OnDemand'][sku].values():
   for dimension in term['priceDimensions'].values():
    amount=dimension['pricePerUnit']['USD'];assert Decimal(amount)==wanted[usage]
    rates.append({'sku':sku,'usageType':usage,'unit':dimension['unit'],'USD':amount,'description':dimension['description']})
 assert {row['usageType'] for row in rates}==set(wanted) and len(rates)==2
 (C/(service+'-eu-central-1-prices.json')).write_bytes(blob)
 sources.append({'service':service,'url':url,'sha256':hashlib.sha256(blob).hexdigest(),'publicationDate':data['publicationDate'],'rates':rates})
(C/'PRICE-RECONCILIATION.json').write_text(json.dumps({'at':datetime.now(timezone.utc).isoformat(),'region':'eu-central-1','sources':sources,'billingStillRequired':True,'reserveReduction':False},indent=2)+'\n')
print(json.dumps({'publicPriceSources':2,'expectedRatesVerified':4,'reserveReduction':False,'authenticatedAWSCalls':0}))
```

## completion-c12/resolve_and_report.py

Original machine-bound bytes SHA256 `2606151802ae653d17fe9b5864901cf0b00c2c90bc4acbd0c5ef58aa34399281`; portable transcription SHA256 `cb8aa4d4eea4e671803109e1876a3248526fb2d63b3bf708d47ac1dbde29cecb`. These differ when local bindings or typography were normalized.

```python
from pathlib import Path
import importlib.util,json,hashlib,html,re,os
r=Path('<report-root>');out=r/'completion-c12';op=r/'private-operator'
def load(p):return json.loads(p.read_text())
def digest(p):return hashlib.sha256(p.read_bytes()).hexdigest()
def canonical(x):return json.dumps(x,sort_keys=True,separators=(',',':'))
ack=load(op/'c12-key-finish-bound.json');assert ack['classification']=='ACKNOWLEDGED_REQUIRES_READBACK' and ack['dispatched']=={'CreateKey':1,'CreateAlias':1}
key=ack['key'];readback=load(op/'c12-key-readback.json');records=readback['records']
def record(operation):
 row=next(row for row in records if row['operation']==operation and row.get('status')==200);return json.loads(row['body'])
actual=record('DescribeKey')['KeyMetadata'];assert actual['Arn']==key['Arn'] and actual['KeyId']==key['KeyId'] and actual['KeyState']=='Enabled' and actual['AWSAccountId']=='099256073061'
assert actual['KeySpec']=='SYMMETRIC_DEFAULT' and actual['KeyUsage']=='ENCRYPT_DECRYPT' and actual['Origin']=='AWS_KMS' and actual['MultiRegion'] is False
policy=json.loads(record('GetKeyPolicy')['Policy']);expected={'Id':'key-consolepolicy-3','Version':'2012-10-17','Statement':[{'Sid':'Enable IAM User Permissions','Effect':'Allow','Principal':{'AWS':'arn:aws:iam::099256073061:root'},'Action':'kms:*','Resource':'*'}]};assert policy==expected
aliases=record('ListAliases');assert aliases.get('Truncated') is False and len(aliases['Aliases'])==1
alias=aliases['Aliases'][0];assert alias['AliasName']=='alias/hikyo-158-20261028c12' and alias['TargetKeyId']==key['KeyId']
tags=record('ListResourceTags');assert tags.get('Truncated') is False and tags['Tags']==[]
assert record('GetKeyRotationStatus')['KeyRotationEnabled'] is False
pins=load(r/'revision-r3/REVIEW-SUBJECT.json')['files'];templates={}
for path in sorted((r/'revision-r3/policies-unbound').glob('*.json')):
 assert digest(path)==pins['policies-unbound/'+path.name]['sha256'];templates[path.name]=load(path)
modulepath=r/'post-round4-fixes-20261026/bind_policies.py';assert digest(modulepath)=='fc2b8beef44c6a017451f68f3e4940d5f193ca2e271b121b47fdbc1676d5e5a0'
spec=importlib.util.spec_from_file_location('approved_binder',modulepath);module=importlib.util.module_from_spec(spec);spec.loader.exec_module(module)
window=load(out/'WINDOW-BINDING.json');bindings={'keyARN':key['Arn'],'runPrefix':'hikyo-e2e-20261028c12/','externalId':'hikyo-158-20261028c12','t0':window['t0'],'authExpiresAt':window['authExpiresAt'],'cleanupExpiresAt':window['cleanupExpiresAt']}
documents,audit=module.bind_templates(templates,bindings);assert len(documents)==18 and len(audit)==129
assert not any('UNBOUND_' in canonical(d) or 'hikyo-e2e-20261028c12//' in canonical(d) for d in documents.values())
resolved=out/'resolved';resolved.mkdir(exist_ok=False)
individual={}
for name,doc in documents.items():
 compact=canonical(doc);pretty=json.dumps(doc,indent=2)+'\n';(resolved/name).write_text(pretty);(resolved/name.replace('.json','.compact.json')).write_text(compact)
 individual[name]={'canonicalSHA256':hashlib.sha256(compact.encode()).hexdigest(),'canonicalBytes':len(compact.encode()),'IAMNonWhitespaceCharacters':len(re.sub(r'\s','',compact))}
 for kind in ['setup-user.managed-policy.json','setup-role-boundary.json']:
  if name==kind:assert len(compact)<=6144
changeset=hashlib.sha256(canonical(documents).encode()).hexdigest()
metadata={'status':'PREPARED_SEPARATE_OWNER_HASH_APPROVAL_REQUIRED','binding':bindings,'window':window,'documents':individual,'canonicalChangeSetSHA256':changeset,'hashDefinition':'SHA256 of UTF-8 JSON object keyed by the 18 template filenames, sort_keys=True, separators comma colon, no trailing newline','changedLeaves':len(audit),'keyReadbackSHA256':digest(op/'c12-key-readback.json'),'sourceManifestSHA256':digest(r/'completion-c12-proposal/FROZEN-SOURCE-MANIFEST.json'),'IAMLimiterSHA256':digest(out/'iam-final-action-DRAFT.js'),'trustedMainObserved':load(out/'MAIN-CORRESPONDENCE.json')['observedMain'],'newReservationsCharged':32,'lifetimeReservations':336,'maximumPoolCreations':32,'maximumPoolDispatches':1}
(out/'KEY-READBACK.json').write_text(json.dumps({'status':'INDEPENDENT_NATIVE_READBACK_PASS','key':actual,'alias':alias,'keyPolicy':policy,'tags':[],'rotationEnabled':False,'sourceReceiptSHA256':digest(op/'c12-key-readback.json')},indent=2)+'\n')
(out/'POLICY-SET.json').write_text(json.dumps(metadata,indent=2)+'\n');(out/'POLICY-AUDIT.json').write_text(json.dumps(audit,indent=2)+'\n')
report=f'''# Issue 158 c12: exact final 18-policy approval\n\nSeparate owner hash approval is required. These documents are resolved data from the owner-adopted fingerprint-constrained binder. They are not independently CLEAN. Round five's Spec CHANGES_REQUIRED is preserved. The c12 owner adopted its corrections; five of five reviews are consumed. No sixth review.\n\nCanonical change-set SHA-256: `{changeset}`.\n\nAccount 099256073061, eu-central-1. Independently read-back new key `{key['Arn']}`; alias `alias/hikyo-158-20261028c12` targets only this key. Symmetric single-region AWS_KMS ENCRYPT_DECRYPT, default root-only IAM delegation, no tags, rotation disabled. All seven prior bootstrap keys remain PendingDeletion, never canceled or reused.\n\nT0 `{window['t0']}`; ordinary mutation expires `{window['authExpiresAt']}`; cleanup/setup expires `{window['cleanupExpiresAt']}`; provisioning cutoff `{window['provisioningCutoff']}`. Amsterdam is UTC+1 today. Namespace `hikyo-e2e-20261028c12/`, external ID `hikyo-158-20261028c12`.\n\n## Decision\n\nA (recommended): approve exactly these 18 resolved policies and hash. This authorizes exact temporary boundary creation, archival/deletion of only owned nondefault setup v11, and activation of one bounded new setup version with its actual acknowledged ID after the required preceding readbacks pass. Preserve v2-v5 and their dates, restore exact v5 on retirement and retain the actual newly created setup version. Keep literal setup MFA ListRoles and strict unused-sentinel DescribeSecret before roles/provider/secrets/dispatch, explicit value-read/assumption/escalation denials, all resource/budget/cleanup limits and separate human approval of the exact pending GitHub deployment.\n\nB: hold before IAM mutation and retire the new alias/key within the bound window. No policy or deployment approval is inferred.\n\nThe c12 successor adds 32 newly charged slots, lifetime 336, and one hosted dispatch at attempt 1. The closed 304 reservations and original failed run remain charged and closed. Maximum eight new keys across c05 through c12; all seven prior new-key deletion schedules remain unchanged. No retries, refunds or resets. The native acknowledgement records exactly one CreateKey and one CreateAlias forward. All blocked or ambiguous outcomes remain in their original transport receipt. No failed or ambiguous AWS creation is retried. Independent native metadata, not console banner alone, establishes this key binding.\n\nAll 18 raw frozen template hashes and canonical fingerprints match. The closed binder made 129 approved leaf replacements only. Both managed policies fit 6144 characters. All policy text appears below.\n\n## Binding and hash definition\n\n```json\n{json.dumps(metadata,indent=2)}\n```\n\n## Complete resolved documents\n\n'''
for name in sorted(documents):report+=f'### {name}\n\n```json\n{json.dumps(documents[name],indent=2)}\n```\n\n'
report+='## Complete closed substitution audit\n\n```json\n'+json.dumps(audit,indent=2)+'\n```\n'
(out/'OWNER-POLICIES.md').write_text(report)
companion='<!doctype html><html lang="en"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Issue 158 c12 final policy approval</title><style>body{margin:0;background:#f6f7f9;color:#17212b;font:16px/1.55 system-ui,sans-serif}main{max-width:1100px;margin:32px auto;padding:24px;background:white}pre{white-space:pre-wrap;overflow-wrap:anywhere;font:14px/1.6 ui-monospace,monospace}</style><main><pre>'+html.escape(report)+'</pre></main></html>'
(out/'OWNER-POLICIES.html').write_text(companion)
print(json.dumps({'keyId':key['KeyId'],'keyReadback':'PASS','documents':18,'changedLeaves':len(audit),'canonicalChangeSetSHA256':changeset,'managedPolicyLengths':{k:individual[k]['canonicalBytes'] for k in ['setup-user.managed-policy.json','setup-role-boundary.json']}}))
```

## completion-c12/retire-alias-once-DRAFT.js

Original machine-bound bytes SHA256 `9888a1ea42df7fc8897bce3107d3c693ef2fe5ab7775d1da7aafe7a2e57068d7`; portable transcription SHA256 `9888a1ea42df7fc8897bce3107d3c693ef2fe5ab7775d1da7aafe7a2e57068d7`. These differ when local bindings or typography were normalized.

```javascript
async page=>{
 if(['UNBOUND_T0','UNBOUND_CLEANUP_UNTIL','UNBOUND_ACKNOWLEDGED_C12_KEY_ID'].some(x=>x.includes('UNBOUND_')))throw Error('unbound-cleanup-data');
 if(Date.now()<Date.parse('UNBOUND_T0')||Date.now()>=Date.parse('UNBOUND_CLEANUP_UNTIL'))throw Error('outside-approved-cleanup-window');
 const context=page.context(),tab=await context.newPage(),key='UNBOUND_ACKNOWLEDGED_C12_KEY_ID',alias='alias/hikyo-158-20261028c12';
 let forwarded=0,associationVerified=false;const events=[];
 const reads=new Set(['DescribeKey','GetKeyPolicy','GetKeyRotationStatus','ListKeys','ListAliases','ListResourceTags','ListGrants','ListRetirableGrants','GetKeyLastUsage']);
 const handler=async route=>{
  const q=route.request(),op=(q.headers()['x-amz-target']||'').replace(/^TrentService\./,'');
  if(q.method()==='OPTIONS'||reads.has(op))return await route.continue();
  let body;try{body=q.postDataJSON();}catch{body=null;}
  if(op==='DeleteAlias'&&q.frame().page()===tab&&associationVerified&&forwarded===0&&Date.now()<Date.parse('UNBOUND_CLEANUP_UNTIL')&&body&&Object.keys(body).length===1&&body.AliasName===alias){
   forwarded++;events.push({operation:op,request:body,forwardedAt:new Date().toISOString()});return await route.continue();
  }
  events.push({blocked:op||'unknown'});return await route.abort('blockedbyclient');
 };
 await context.route('https://kms.eu-central-1.amazonaws.com/',handler);
 let association=null,result=null,localFailure=null;
 try{
  const aliases=tab.waitForResponse(r=>r.url()==='https://kms.eu-central-1.amazonaws.com/'&&r.request().headers()['x-amz-target']==='TrentService.ListAliases'&&r.request().postDataJSON().KeyId===key,{timeout:12000});
  await tab.goto('https://eu-central-1.console.aws.amazon.com/kms/home?region=eu-central-1#/kms/keys/'+key,{waitUntil:'domcontentloaded',timeout:15000});
  await tab.getByRole('tab',{name:'Aliases',exact:true}).click({timeout:10000});
  const a=await aliases;association={status:a.status(),request:a.request().postData(),body:await a.json()};
  if(association.status!==200||association.body.Truncated!==false||association.body.Aliases.length!==1||association.body.Aliases[0].AliasName!==alias||association.body.Aliases[0].TargetKeyId!==key)throw Error('association-mismatch');
  associationVerified=true;
  await tab.getByRole('row').filter({hasText:'arn:aws:kms:eu-central-1:099256073061:alias/hikyo-158-20261028c12'}).getByRole('checkbox').check({timeout:5000});
  const response=tab.waitForResponse(r=>r.url()==='https://kms.eu-central-1.amazonaws.com/'&&r.request().headers()['x-amz-target']==='TrentService.DeleteAlias',{timeout:15000}).then(async r=>({status:r.status(),request:r.request().postData(),body:await r.text(),receivedAt:new Date().toISOString()})).catch(()=>({captureFailure:'no-complete-native-response'}));
  await tab.getByRole('button',{name:'Delete',exact:true}).click({timeout:5000});
  const dialog=tab.getByRole('dialog');
  const shown=await dialog.waitFor({state:'visible',timeout:3000}).then(()=>true).catch(()=>false);
  if(shown){
   const text=await dialog.innerText();events.push({confirmationText:text});
   if(!text.includes('hikyo-158-20261028c12'))throw Error('confirmation-target-not-shown');
   if(text.includes('enter "delete"'))await dialog.getByRole('textbox').fill('delete',{timeout:5000});
   const names=(await dialog.getByRole('button').allTextContents()).map(x=>x.trim());
   const name=names.find(x=>['Delete','Delete alias','Delete aliases'].includes(x));if(!name)throw Error('unsupported-confirmation');
   await dialog.getByRole('button',{name,exact:true}).click({timeout:5000});
  }
  result=await response;
 }catch(error){localFailure=['association-mismatch','confirmation-target-not-shown','unsupported-confirmation'].includes(error.message)?error.message:error.name;}
 finally{if(!tab.isClosed())await tab.close();await context.unroute('https://kms.eu-central-1.amazonaws.com/',handler);}
 return{at:new Date().toISOString(),forwarded,association,result,localFailure,events,resource:'exact-new-alias',noRetry:true};
}
```

## completion-c12/retire_fixture_once.py

Original machine-bound bytes SHA256 `645e668b35c75e12d8e5d060aa8f0404abdc8ccc3c5cc15d829507adc5e134f6`; portable transcription SHA256 `02a2a3e5a06c480946308f575546eb47c1aa387dba773c01d8003d07687e5555`. These differ when local bindings or typography were normalized.

```python
"""Retire only independently matched c07 IAM fixtures after terminal-run secret cleanup."""
from datetime import datetime, timezone
from pathlib import Path
import hashlib
import json
import os
import subprocess
import urllib.parse

r = Path('<report-root>')
c = r / 'completion-c12'
login = Path('<local-artifact>/aws-local-login')
aws = Path('/opt/homebrew/Cellar/awscli/2.38.4/libexec/bin/aws')
load = lambda p: json.loads(p.read_text())
assert hashlib.sha256(aws.read_bytes()).hexdigest() == 'b643e78fd6c61395b1a89bfe2f1504a38115261aa29f42d1e5e35e0620119f61'
gate = load(c / 'NAMESPACE-SECRET-CLEANUP-READBACK.json')
assert gate['status'] == 'PASS_STRICT_NO_C12_SECRETS_REMAIN' and gate['hostedRunTerminal'] is True
assert gate['runId'] == json.loads((c/'WORKFLOW-DISPATCH-RESULT.json').read_text())['runId'] and gate['prefix'] == 'hikyo-e2e-20261028c12/'
assert (datetime.now(timezone.utc) - datetime.fromisoformat(gate['at'].replace('Z', '+00:00'))).total_seconds() < 300
w = load(c / 'WINDOW-BINDING.json')
cutoff = datetime.fromisoformat(w['cleanupExpiresAt'].replace('Z', '+00:00'))
assert datetime.now(timezone.utc) < cutoff
fixture = load(c / 'FIXTURE-RESULT.json')
assert fixture['status'] == 'PASS_EIGHT_EXACT_ROLES_PROVIDER_POLICIES_TRUSTS_BOUNDARIES_TAGS'
roles = fixture['owned']['roles']
provider = fixture['owned']['provider']
assert len(roles) == 8 and provider['arn'] == 'arn:aws:iam::099256073061:oidc-provider/token.actions.githubusercontent.com'
allowed_names = {x['name'] for x in roles}
assert allowed_names == {'hikyo-158-' + x for x in ['source','static','ambient','web','kms-denied','oracle','cleanup','assume']}
folder = c / 'iam-retirement'
folder.mkdir(mode=0o700, exist_ok=False)
env = {k: os.environ[k] for k in ('PATH','TMPDIR','LANG') if k in os.environ}
env.update(AWS_CONFIG_FILE=str(login/'config'), AWS_SHARED_CREDENTIALS_FILE=str(login/'credentials'), AWS_LOGIN_CACHE_DIRECTORY=str(login/'cache'), AWS_REGION='eu-central-1', AWS_DEFAULT_REGION='eu-central-1', AWS_EC2_METADATA_DISABLED='true', AWS_IGNORE_CONFIGURED_ENDPOINT_URLS='true', AWS_CLI_AUTO_PROMPT='off', AWS_PAGER='', AWS_MAX_ATTEMPTS='1', AWS_RETRY_MODE='standard')
records, retired = [], []
def save(status):
    (c/'IAM-RETIREMENT-RESULT.json').write_text(json.dumps({'at':datetime.now(timezone.utc).isoformat(),'status':status,'records':records,'retiredRoles':retired,'providerArn':provider['arn'],'automaticRetry':False,'secretCalls':0,'credentialContentsInspected':False},indent=2)+'\n')
def exclusive(path, value):
    fd=os.open(path,os.O_CREAT|os.O_EXCL|os.O_WRONLY,0o600)
    with os.fdopen(fd,'w') as f:
        json.dump(value,f,indent=2);f.write('\n');f.flush();os.fsync(f.fileno())
    d=os.open(path.parent,os.O_RDONLY);os.fsync(d);os.close(d)
def call(service, operation, data, mutation=False):
    assert datetime.fromisoformat(w['t0'].replace('Z','+00:00'))<=datetime.now(timezone.utc)<cutoff
    if mutation:
        assert service=='iam'
        assert (operation=='delete-role-policy' and set(data)=={'RoleName','PolicyName'} and data['RoleName'] in allowed_names and data['PolicyName']=='hikyo-158-acceptance') or (operation=='delete-role' and set(data)=={'RoleName'} and data['RoleName'] in allowed_names) or (operation=='delete-open-id-connect-provider' and data=={'OpenIDConnectProviderArn':provider['arn']})
    else:
        assert (service=='sts' and operation=='get-caller-identity' and data=={}) or (service=='iam' and operation in {'list-roles','list-open-id-connect-providers','get-open-id-connect-provider','get-role','get-role-policy','list-role-policies','list-attached-role-policies'})
    n=len(records)+1;request=folder/(f'{n:03d}-{operation}-request.json');exclusive(request,data)
    if mutation:exclusive(folder/(f'{n:03d}-{operation}-intent.once.json'),{'at':datetime.now(timezone.utc).isoformat(),'operation':operation,'requestSHA256':hashlib.sha256(request.read_bytes()).hexdigest(),'maximumCalls':1,'retry':False,'cleanupExpiresAt':w['cleanupExpiresAt']})
    argv=[str(aws),'--profile','hikyo-158-setup','--region','eu-central-1','--cli-error-format','json','--cli-connect-timeout','10','--cli-read-timeout','10','--no-cli-pager','--no-cli-auto-prompt','--no-paginate','--output','json',service,operation,'--cli-input-json','file://'+str(request)]
    row={'operation':operation,'mutation':mutation,'sentAt':datetime.now(timezone.utc).isoformat()};records.append(row)
    try: result=subprocess.run(argv,env=env,stdin=subprocess.DEVNULL,capture_output=True,timeout=min(12,(cutoff-datetime.now(timezone.utc)).total_seconds()))
    except subprocess.TimeoutExpired:
        row['status']='HOLD_TIMEOUT_AMBIGUOUS_IF_MUTATION';save('HOLD_NO_RETRY');raise SystemExit(1)
    row['exitCode']=result.returncode;row['receivedAt']=datetime.now(timezone.utc).isoformat()
    if result.returncode:
        try:error=json.loads(result.stderr);row['errorCode']=error.get('Code',error.get('code','UNCLASSIFIED'))
        except (ValueError,AttributeError):row['errorCode']='UNCLASSIFIED_PRIVATE_OUTPUT_WITHHELD'
        save('HOLD_NO_RETRY');raise SystemExit(1)
    body=json.loads(result.stdout) if result.stdout.strip() else {}
    if not result.stdout.strip():assert mutation
    row['metadata']=body;save('IN_PROGRESS');return body
def tags_equal(a,b):return sorted(a,key=lambda x:x['Key'])==sorted(b,key=lambda x:x['Key'])
def decoded(x):return json.loads(urllib.parse.unquote(x)) if isinstance(x,str) else x
try:
    assert call('sts','get-caller-identity',{})=={'UserId':'AIDAROHA7PNS4ZCR3U3FR','Account':'099256073061','Arn':'arn:aws:iam::099256073061:user/hikyo-158-setup'}
    listing=call('iam','list-roles',{'MaxItems':1000});assert listing['IsTruncated'] is False and not listing.get('Marker')
    service_roles={'AWSServiceRoleForResourceExplorer','AWSServiceRoleForSupport','AWSServiceRoleForTrustedAdvisor'}
    assert {x['RoleName'] for x in listing['Roles']}==service_roles|allowed_names
    issuer=call('iam','get-open-id-connect-provider',{'OpenIDConnectProviderArn':provider['arn']})
    assert issuer['Url'] in ['token.actions.githubusercontent.com','https://token.actions.githubusercontent.com'] and issuer['ClientIDList']==['sts.amazonaws.com'] and issuer['CreateDate']==provider['readback']['CreateDate'] and tags_equal(issuer['Tags'],provider['tags'])
    for owned in sorted(roles, key=lambda row: row['name'] != 'hikyo-158-assume'):
        name=owned['name'];actual=call('iam','get-role',{'RoleName':name})['Role']
        assert actual['Arn']==owned['arn'] and actual['RoleId']==owned['roleId'] and actual['CreateDate']==owned['createDate'] and actual['Path']=='/' and actual['MaxSessionDuration']==owned['maxSessionDuration'] and actual['PermissionsBoundary']['PermissionsBoundaryArn']==owned['boundary'] and tags_equal(actual['Tags'],owned['tags'])
        assert decoded(actual['AssumeRolePolicyDocument'])==load(c/'resolved'/(name+'.trust.json'))
        inline=call('iam','list-role-policies',{'RoleName':name,'MaxItems':1000});assert inline['IsTruncated'] is False and inline['PolicyNames']==['hikyo-158-acceptance']
        attached=call('iam','list-attached-role-policies',{'RoleName':name,'MaxItems':1000});assert attached['IsTruncated'] is False and attached['AttachedPolicies']==[]
        policy=call('iam','get-role-policy',{'RoleName':name,'PolicyName':'hikyo-158-acceptance'});assert decoded(policy['PolicyDocument'])==load(c/'resolved'/(name+'.policy.json'))
        call('iam','delete-role-policy',{'RoleName':name,'PolicyName':'hikyo-158-acceptance'},True)
        call('iam','delete-role',{'RoleName':name},True);retired.append({'name':name,'roleId':owned['roleId'],'arn':owned['arn']});save('IN_PROGRESS')
    listing=call('iam','list-roles',{'MaxItems':1000});assert listing['IsTruncated'] is False and {x['RoleName'] for x in listing['Roles']}==service_roles
    call('iam','delete-open-id-connect-provider',{'OpenIDConnectProviderArn':provider['arn']},True)
    assert call('iam','list-open-id-connect-providers',{})['OpenIDConnectProviderList']==[]
    save('PASS_EXACT_EIGHT_ROLES_INLINE_POLICIES_AND_OWNED_ISSUER_RETIRED')
    print(json.dumps({'status':'PASS_EXACT_EIGHT_ROLES_INLINE_POLICIES_AND_OWNED_ISSUER_RETIRED','roles':len(retired),'providerDeleted':True,'secretCalls':0}))
except (AssertionError,KeyError,ValueError):
    save('HOLD_METADATA_MISMATCH_NO_RETRY');print(json.dumps({'status':'HOLD_METADATA_MISMATCH_NO_RETRY','retiredRoleCount':len(retired)}));raise SystemExit(1)
```

## completion-c12/retire_run_variables_once.py

Original machine-bound bytes SHA256 `9f80d47d04e8bce8b49828fbea781da72f239178c439ee419d8894625020c5ce`; portable transcription SHA256 `3130ec868f3ad79bb5e8921ab4dcebafbc42e886b00198842fed9e40acfa8855`. These differ when local bindings or typography were normalized.

```python
"""Remove only the nine exact consumed environment variables after this run is terminal."""
from pathlib import Path
from datetime import datetime, timezone
import hashlib
import json
import os
import subprocess

c=Path('<report-root>/completion-c12')
base='repos/Hikyo-Org/Hikyo/environments/hikyo-158-aws-sandbox/variables'
run=json.loads((c/'HOSTED-RUN-TERMINAL.json').read_text())
assert run['id']==int(json.loads((c/'WORKFLOW-DISPATCH-RESULT.json').read_text())['runId']) and run['status']=='completed' and run['run_attempt']==1
cut=datetime.fromisoformat(json.loads((c/'WINDOW-BINDING.json').read_text())['cleanupExpiresAt'].replace('Z','+00:00'))
assert datetime.now(timezone.utc)<cut
def api(path):return json.loads(subprocess.check_output(['gh','api',path],timeout=20))
actual=api(base+'?per_page=30');assert actual['total_count']==len(actual['variables'])==9
intended={}
for p in (c/'github-bindings').glob('*.json'):
    x=json.loads(p.read_text());intended[x['name']]=x['value']
assert len(intended)==9 and 'AWS_ACCEPTANCE_HISTORICAL_CLEANUP_ARN' not in intended
assert {x['name']:x['value'] for x in actual['variables']}==intended
folder=c/'github-variable-retirement';folder.mkdir(mode=0o700,exist_ok=False)
records=[]
def save(status):
    (c/'VARIABLE-RETIREMENT-RESULT.json').write_text(json.dumps({'at':datetime.now(timezone.utc).isoformat(),'status':status,'runId':json.loads((c/'WORKFLOW-DISPATCH-RESULT.json').read_text())['runId'],'records':records,'protectedEnvironmentRetained':True,'repoAndOrgVariablesUntouched':True,'absentOptionalVariableNotDeleted':True},indent=2)+'\n')
for name in sorted(intended):
    assert datetime.now(timezone.utc)<cut
    row={'name':name,'expectedValue':intended[name],'sentAt':datetime.now(timezone.utc).isoformat(),'maximumCalls':1,'retry':False}
    fd=os.open(folder/(name+'-intent.once.json'),os.O_CREAT|os.O_EXCL|os.O_WRONLY,0o600)
    with os.fdopen(fd,'w') as f:json.dump(row,f,indent=2);f.write('\n');f.flush();os.fsync(f.fileno())
    d=os.open(folder,os.O_RDONLY);os.fsync(d);os.close(d)
    records.append(row)
    try:r=subprocess.run(['gh','api',base+'/'+name,'--method','DELETE'],capture_output=True,timeout=20)
    except subprocess.TimeoutExpired:row['status']='HOLD_AMBIGUOUS_DELETE_NO_RETRY';save('HOLD');raise SystemExit(1)
    row['exitCode']=r.returncode;row['receivedAt']=datetime.now(timezone.utc).isoformat()
    if r.returncode:row['status']='HOLD_DELETE_FAILURE_NO_RETRY';save('HOLD');raise SystemExit(1)
    row['status']='ACKNOWLEDGED';save('IN_PROGRESS')
after=api(base+'?per_page=30');assert after['total_count']==0 and after['variables']==[]
save('PASS_NINE_EXACT_CONSUMED_VARIABLES_ABSENT_ENVIRONMENT_RETAINED')
print(json.dumps({'status':'PASS_NINE_EXACT_CONSUMED_VARIABLES_ABSENT_ENVIRONMENT_RETAINED','deleted':len(records),'protectedEnvironmentRetained':True}))
```
