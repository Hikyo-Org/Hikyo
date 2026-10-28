# Prepared operator source archive, part 8/9

**ARCHIVE ONLY, PARKED, LIVE QUALIFICATION NOT ESTABLISHED.** This preserves the prepared small-route operators and their tests so local report deletion does not erase the implementation. The transcription normalizes machine-local paths to `<report-root>`, `<approval-bank>`, `<application-checkout>`, `<trusted-source-checkout>` or `<local-artifact>`. They are reconstruction placeholders, not working filesystem paths. Original and normalized digests are both recorded. All runtime/source pins and concrete bindings must be re-established before use. Do not copy this code directly into a live operation or claim that normalization preserved an old qualification. In particular the historical unattended helper is an agent interpretation of an overnight request; present parking overrides it, and all applicable source/Ready/policy/deployment/merge gates must be reconciled with current owner direction. No credentials, authentication storage or private challenge URLs are included.

## completion-c12/verify_iam_step.py

Original machine-bound bytes SHA256 `b1a2f161402c55c7c85a59b0f6561b6acd848ac1408f4e92492aa1a16970e59c`; portable transcription SHA256 `9036938f22fb8ac5e95578cc98a3844ad0d2fb74240f360d5994fff05c694449`. These differ when local bindings or typography were normalized.

```python
from pathlib import Path
from datetime import datetime,timezone
import hashlib,json,os,sys,urllib.parse,xml.etree.ElementTree as E
r=Path('<report-root>');c=r/'completion-c12';op=r/'private-operator';phase=sys.argv[1]
setup='arn:aws:iam::099256073061:policy/hikyo-158-setup-policy';boundary='arn:aws:iam::099256073061:policy/hikyo-158-role-boundary'
def load(p):return json.loads(p.read_text())
def sha(p):return hashlib.sha256(p.read_bytes()).hexdigest()
def fields(row,name):return[n.text for n in E.fromstring(row['body']).iter() if n.tag.split('}')[-1]==name]
def save(name,data):
 fd=os.open(c/name,os.O_CREAT|os.O_EXCL|os.O_WRONLY,0o600)
 with os.fdopen(fd,'w') as f:json.dump(data,f,indent=2);f.write('\n');f.flush();os.fsync(f.fileno())
 d=os.open(c,os.O_RDONLY);os.fsync(d);os.close(d)
if phase=='boundary':
 action='createpolicy';readname='c12-boundary-readback';arn=boundary;expected='setup-role-boundary.json';versions={'v1'};default='v1'
elif phase=='v11':
 action='deletepolicyversion';readname='c12-v11-deletion-readback';arn=setup;expected=None;versions={'v2','v3','v4','v5'};default='v5'
elif phase=='setup':
 action='createpolicyversion';readname='c12-setup-readback';arn=setup;expected='setup-user.managed-policy.json';versions={'v2','v3','v4','v5'};default=None
else:raise ValueError('phase')
ackpath=op/('c12-'+action+'-once.json');ack=load(ackpath);assert ack['forwarded']==1 and ack['failed'] is False and ack['classification']=='NATIVE_METADATA_CAPTURED_REQUIRES_INDEPENDENT_READBACK' and ack['response']['status']==200
if phase=='setup':
 ids=fields(ack['response'],'VersionId');assert len(ids)==1 and ids[0].startswith('v') and ids[0][1:].isdigit() and ids[0] not in {'v2','v3','v4','v5','v11'};default=ids[0];versions.add(default)
readpath=op/(readname+'.json');read=load(readpath);rows=read['records'];assert not any('captureFailure' in x for x in rows)
def one(operation):return next(x for x in rows if x['operation']==operation and x['status']==200 and ('PolicyArn='+urllib.parse.quote(arn,safe='')) in x['request'])
policy=one('GetPolicy');listed=one('ListPolicyVersions');assert fields(policy,'Arn')==[arn] and fields(policy,'DefaultVersionId')==[default] and fields(listed,'IsTruncated')==['false'] and set(fields(listed,'VersionId'))==versions
policyid=fields(policy,'PolicyId')[0]
if phase=='boundary':
 assert fields(ack['response'],'PolicyId')==[policyid] and fields(ack['response'],'Arn')==[boundary]
 assert fields(policy,'AttachmentCount')==['0'] and fields(policy,'PermissionsBoundaryUsageCount')==['0']
 tagrow=one('ListPolicyTags');assert fields(tagrow,'IsTruncated')==['false'] and not fields(tagrow,'Key')
else:
 assert policyid=='ANPAROHA7PNSTPQUQEUTR'
 baseline=load(op/'c12-iam-admission.json')['records'];oldlist=next(x for x in baseline if x['operation']=='ListPolicyVersions' and x['status']==200)
 def pairs(row):
  root=E.fromstring(row['body']);result={}
  for parent in root.iter():
   children={n.tag.split('}')[-1]:n.text for n in list(parent)}
   if 'VersionId' in children and 'CreateDate' in children:result[children['VersionId']]=children['CreateDate']
  return result
 before,after=pairs(oldlist),pairs(listed)
 for v in ['v2','v3','v4','v5']:assert before[v]==after[v]
version=next(x for x in rows if x['operation']=='GetPolicyVersion' and x['status']==200 and urllib.parse.parse_qs(x['request']).get('PolicyArn')==[arn] and urllib.parse.parse_qs(x['request']).get('VersionId')==[default]);assert fields(version,'VersionId')==[default] and fields(version,'IsDefaultVersion')==['true'];actual=json.loads(urllib.parse.unquote(fields(version,'Document')[0]))
if expected is None:
 archive=load(Path('<local-artifact>/final-iam-cleanup-and-retained-identity.actual.json'));assert actual==archive['setupPolicy']['versions']['v5']['document']
else:assert actual==load(c/'resolved'/expected)
status={'boundary':'INDEPENDENT_BOUNDARY_FULL_POLICY_READBACK_PASS','v11':'INDEPENDENT_V11_ABSENCE_V2_V5_PRESERVED_PASS','setup':'INDEPENDENT_SETUP_FULL_POLICY_READBACK_PASS'}[phase]
if phase=='setup':
 assert fields(ack['response'],'VersionId')==[default] and fields(ack['response'],'IsDefaultVersion')==['true']
 for operation,member in [('ListUserPolicies','PolicyName'),('ListGroupsForUser','GroupName'),('ListAccessKeys','AccessKeyId')]:
  row=next(x for x in rows if x['operation']==operation and x['status']==200);assert fields(row,'IsTruncated')==['false'] and not fields(row,member)
 user=next(x for x in rows if x['operation']=='GetUser' and x['status']==200);assert fields(user,'UserId')==['AIDAROHA7PNS4ZCR3U3FR']
 attached=next(x for x in rows if x['operation']=='ListAttachedUserPolicies' and x['status']==200);assert fields(attached,'IsTruncated')==['false'] and fields(attached,'PolicyArn')==[setup]
 mfa=next(x for x in rows if x['operation']=='ListMFADevices' and x['status']==200);assert fields(mfa,'IsTruncated')==['false'] and fields(mfa,'SerialNumber')==['arn:aws:iam::099256073061:mfa/hikyo-158-setup']
name={'boundary':'BOUNDARY-READBACK.json','v11':'V11-DELETION-READBACK.json','setup':'SETUP-ACTIVATION-READBACK.json'}[phase]
save(name,{'at':datetime.now(timezone.utc).isoformat(),'status':status,'ARN':arn,'policyId':policyid,'defaultVersion':default,'versions':sorted(versions),'documentSHA256':hashlib.sha256(json.dumps(actual,sort_keys=True,separators=(',',':')).encode()).hexdigest(),'nativeAckSHA256':sha(ackpath),'independentReadbackSHA256':sha(readpath)})
print(json.dumps({'step':phase,'status':status,'policyId':policyid,'defaultVersion':default,'versions':sorted(versions)}))
```

## completion-c12/verify_secret_absence_once.py

Original machine-bound bytes SHA256 `e21ed9b81e7de78e25152a0535c41b1571abee0837cc0297a07a32e612178a53`; portable transcription SHA256 `09f35e143832f021d3aeb927769ca04e4c9af97e2a8f4de5f308787c4c5be144`. These differ when local bindings or typography were normalized.

```python
"""Independent metadata-only cleanup proof, preserving failed acceptance status."""
from pathlib import Path
from datetime import datetime, timezone
import hashlib
import json
import os
import re
import subprocess
import time

c = Path('<report-root>/completion-c12')
aws = Path('/opt/homebrew/Cellar/awscli/2.38.4/libexec/bin/aws')
login = Path('<local-artifact>/aws-local-login')
assert hashlib.sha256(aws.read_bytes()).hexdigest() == 'b643e78fd6c61395b1a89bfe2f1504a38115261aa29f42d1e5e35e0620119f61'
load = lambda p: json.loads(p.read_text())
terminal = load(c/'HOSTED-RUN-TERMINAL.json')
assert terminal['id'] ==int(json.loads((c/'WORKFLOW-DISPATCH-RESULT.json').read_text())['runId']) and terminal['run_attempt'] == 1 and terminal['status'] == 'completed'
e = load(c/'hosted-evidence/evidence.json')
assert e['schema']=='hikyo.aws-acceptance/v2' and e['candidate_sha']=='c253ff9b1ccec7408f13366f8c5640c8c7cf3d0a' and e['workflow_sha']==json.loads((c/'MAIN-CORRESPONDENCE.json').read_text())['observedMain'] and str(e['run_id'])==json.loads((c/'WORKFLOW-DISPATCH-RESULT.json').read_text())['runId']
assert e['account']=='099256073061' and e['region']=='eu-central-1' and e['prefix']=='hikyo-e2e-20261028c12/'
assert e['status'] in {'passed','failed-reconcile-before-rerun'} and e['unresolved_names']==[] and 0<len(e['creation_reservations'])<=32
expected_tests=set(load(c/'EXPECTED-HOSTED-TESTS.json')['testNames'])
assert set(e['tests'])==expected_tests and set(e['tests'].values())<={'pass','fail'}
assert (e['status']=='passed') == (e['complete_creation_ledger'] is True)
assert 0<len(e['recorded_arns'])<=32 and set(e['recorded_arns'])==set(e['verified_absent_arns'])
arn_prefix='arn:aws:secretsmanager:eu-central-1:099256073061:secret:'
names=set(e['creation_reservations']);covered=set();targets=[]
for arn in sorted(set(e['recorded_arns'])):
    assert re.fullmatch(re.escape(arn_prefix+'hikyo-e2e-20261028c12/')+r'[A-Za-z0-9/_+=.@-]{1,450}-[A-Za-z0-9]{6}',arn)
    name=arn[len(arn_prefix):-7];assert name in names;covered.add(name);targets.append(arn)
for name in sorted(names-covered):
    assert re.fullmatch(r'hikyo-e2e-20261028c12/[A-Za-z0-9/_+=.@-]{1,450}',name);targets.append(name)
w=load(c/'WINDOW-BINDING.json');cut=datetime.fromisoformat(w['cleanupExpiresAt'].replace('Z','+00:00'));assert datetime.now(timezone.utc)<cut
fd=os.open(c/'INDEPENDENT-ABSENCE-INTENT.once.json',os.O_CREAT|os.O_EXCL|os.O_WRONLY,0o600)
with os.fdopen(fd,'w') as f:
    json.dump({'at':datetime.now(timezone.utc).isoformat(),'targets':targets,'maximumReadsPerTarget':3,'mutations':0,'hostedEvidenceSHA256':hashlib.sha256((c/'hosted-evidence/evidence.json').read_bytes()).hexdigest()},f,indent=2);f.flush();os.fsync(f.fileno())
env={k:os.environ[k] for k in ('PATH','TMPDIR','LANG') if k in os.environ}
env.update(AWS_CONFIG_FILE=str(login/'config'),AWS_SHARED_CREDENTIALS_FILE=str(login/'credentials'),AWS_LOGIN_CACHE_DIRECTORY=str(login/'cache'),AWS_REGION='eu-central-1',AWS_DEFAULT_REGION='eu-central-1',AWS_EC2_METADATA_DISABLED='true',AWS_IGNORE_CONFIGURED_ENDPOINT_URLS='true',AWS_CLI_AUTO_PROMPT='off',AWS_PAGER='',AWS_MAX_ATTEMPTS='1',AWS_RETRY_MODE='standard')
records=[]
def save(status):
    receipt={'at':datetime.now(timezone.utc).isoformat(),'status':status,'runId':json.loads((c/'WORKFLOW-DISPATCH-RESULT.json').read_text())['runId'],'hostedRunTerminal':True,'prefix':e['prefix'],'records':records,'targetCount':len(targets),'recordedArnCount':len(e['recorded_arns']),'reservationCount':len(e['creation_reservations']),'acceptanceStatusPreserved':e['status'],'hostedCompleteCreationLedgerFlagPreserved':e['complete_creation_ledger'],'mutations':0,'rawOutputRetained':False,'credentialContentsInspected':False}
    (c/'INDEPENDENT-SECRET-ABSENCE.json').write_text(json.dumps(receipt,indent=2)+'\n');return receipt
def call(service,operation,args):
    assert datetime.now(timezone.utc)<cut
    argv=[str(aws),'--profile','hikyo-158-setup','--region','eu-central-1','--cli-error-format','json','--cli-connect-timeout','10','--cli-read-timeout','10','--no-cli-pager','--no-cli-auto-prompt','--no-paginate','--output','json',service,operation]+args
    row={'operation':operation,'sentAt':datetime.now(timezone.utc).isoformat()};records.append(row)
    try:r=subprocess.run(argv,env=env,stdin=subprocess.DEVNULL,capture_output=True,timeout=12)
    except subprocess.TimeoutExpired:row['status']='HOLD_READ_TIMEOUT';save('HOLD');raise SystemExit(1)
    row['exitCode']=r.returncode;row['receivedAt']=datetime.now(timezone.utc).isoformat()
    if not r.returncode:return json.loads(r.stdout),row
    try:error=json.loads(r.stderr);row['errorCode']=error.get('Code',error.get('code','UNCLASSIFIED'))
    except (ValueError,AttributeError):row['errorCode']='UNCLASSIFIED_PRIVATE_OUTPUT_WITHHELD'
    return None,row
identity,_=call('sts','get-caller-identity',[])
if identity!={'UserId':'AIDAROHA7PNS4ZCR3U3FR','Account':'099256073061','Arn':'arn:aws:iam::099256073061:user/hikyo-158-setup'}:save('HOLD_IDENTITY');raise SystemExit(1)
roles,_=call('iam','list-roles',['--cli-input-json','{"MaxItems":1000}'])
expected={'AWSServiceRoleForResourceExplorer','AWSServiceRoleForSupport','AWSServiceRoleForTrustedAdvisor'}|{x['name'] for x in load(c/'FIXTURE-RESULT.json')['owned']['roles']}
if roles is None or roles.get('IsTruncated') is not False or {x['RoleName'] for x in roles['Roles']}!=expected:save('HOLD_LITERAL_MFA_OR_INVENTORY');raise SystemExit(1)
for target in targets:
    for attempt in range(3):
        body,row=call('secretsmanager','describe-secret',['--secret-id',target]);row['target']=target;row['boundedReadAttempt']=attempt+1
        if body is None:
            if row.get('errorCode')=='ResourceNotFoundException':row['status']='STRICT_NOT_FOUND';break
            save('HOLD_UNCLASSIFIED_OR_ACCESS');raise SystemExit(1)
        if body.get('ARN') not in e['recorded_arns'] or body.get('Name') not in names:save('HOLD_UNEXPECTED_METADATA');raise SystemExit(1)
        row['status']='STILL_PRESENT_NO_MUTATION'
        if attempt==2:save('HOLD_STILL_PRESENT_AFTER_BOUNDED_READS');raise SystemExit(1)
        time.sleep(2)
receipt=save('PASS_ALL_RECORDED_ARNS_AND_UNACKNOWLEDGED_RESERVED_NAMES_STRICT_NOT_FOUND')
print(json.dumps({k:receipt[k] for k in ['status','targetCount','recordedArnCount','reservationCount','mutations']}))
```

## post-round4-fixes-20261026/bind_policies.py

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

## post-round4-fixes-20261026/key-finish-next-DRAFT.js

Original machine-bound bytes SHA256 `79b75b4e03d44b8fe300a067379b2e284c82a8b423f111d2bb5dd325d00cada8`; portable transcription SHA256 `79b75b4e03d44b8fe300a067379b2e284c82a8b423f111d2bb5dd325d00cada8`. These differ when local bindings or typography were normalized.

```javascript
async page => {
  // Inert draft. Resolve only after a new explicit owner grant and durable intent.
  const approvedAliasName='UNBOUND_APPROVED_ALIAS_NAME', t0='UNBOUND_T0', cutoff='UNBOUND_PROVISIONING_CUTOFF';
  if ([approvedAliasName,t0,cutoff].some(x=>x.includes('UNBOUND_'))) throw Error('unbound-execution-data');
  const parseUtcBound = value => {
    if (!/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z$/.test(value)) throw Error('invalid-approved-provisioning-window');
    const parsed = Date.parse(value);
    if (!Number.isFinite(parsed) || new Date(parsed).toISOString() !== value.replace('Z','.000Z')) throw Error('invalid-approved-provisioning-window');
    return parsed;
  };
  const startsAt = parseUtcBound(t0), endsAt = parseUtcBound(cutoff);
  if (endsAt<=startsAt || endsAt-startsAt>55*60*1000) throw Error('invalid-approved-provisioning-window');
  const withinWindow = now => Number.isFinite(now) && now>=startsAt && now<endsAt;
  if (!withinWindow(Date.now())) throw Error('outside-approved-provisioning-window');
  const context = page.context();
  const wizard = context.pages().find(p => p.url().includes('/kms/home') && p.url().includes('#/kms/keys/create'));
  if (!wizard || context.serviceWorkers().length) throw Error('unsupported-context');
  const expectedPolicy = {Id:'key-consolepolicy-3',Version:'2012-10-17',Statement:[{Sid:'Enable IAM User Permissions',Effect:'Allow',Principal:{AWS:'arn:aws:iam::099256073061:root'},Action:'kms:*',Resource:'*'}]};
  const normalize = value => value && typeof value === 'object' ? (Array.isArray(value) ? value.map(normalize) : Object.fromEntries(Object.keys(value).sort().map(k => [k,normalize(value[k])]))) : value;
  const records = [], tasks = [], dispatched = {CreateKey:0,CreateAlias:0};
  let key = null, aliasAcknowledged = false, routeFailed = false;
  const readOnly = new Set(['DescribeKey','GetKeyPolicy','GetKeyRotationStatus','ListKeys','ListAliases','ListResourceTags','ListGrants','ListRetirableGrants']);
  const handler = async route => {
    try {
    const request = route.request(), operation = (request.headers()['x-amz-target'] || '').replace(/^TrentService\./,'');
    if (request.method()==='OPTIONS' || readOnly.has(operation)) return await route.continue();
    let body;
    try { body = request.postDataJSON(); } catch { records.push({operation,blocked:'invalid-json'}); return await route.abort('blockedbyclient'); }
    if (!body || typeof body!=='object' || Array.isArray(body)) { records.push({operation,blocked:'invalid-shape'}); return await route.abort('blockedbyclient'); }
    let allowed = false;
    if (operation === 'CreateKey' && request.frame().page() === wizard && dispatched.CreateKey === 0) {
      const keys = new Set(['Policy','KeyUsage','KeySpec','CustomerMasterKeySpec','Origin','MultiRegion','Description','Tags','BypassPolicyLockoutSafetyCheck']);
      let policy;
      try { policy = JSON.parse(body.Policy); } catch { policy = null; }
      allowed = Object.keys(body).every(k => keys.has(k)) && JSON.stringify(normalize(policy)) === JSON.stringify(normalize(expectedPolicy)) &&
        (body.KeyUsage === undefined || body.KeyUsage === 'ENCRYPT_DECRYPT') &&
        (body.KeySpec === undefined || body.KeySpec === 'SYMMETRIC_DEFAULT') &&
        (body.CustomerMasterKeySpec === undefined || body.CustomerMasterKeySpec === 'SYMMETRIC_DEFAULT') &&
        (body.Origin === undefined || body.Origin === 'AWS_KMS') && !body.MultiRegion && !body.BypassPolicyLockoutSafetyCheck &&
        (!body.Description) && (body.Tags === undefined || (Array.isArray(body.Tags) && body.Tags.length === 0));
    } else if (operation === 'CreateAlias' && request.frame().page() === wizard && dispatched.CreateAlias === 0 && key) {
      allowed = Object.keys(body).sort().join(',') === 'AliasName,TargetKeyId' && body.AliasName === approvedAliasName && [key.KeyId,key.Arn].includes(body.TargetKeyId);
    }
    if (!allowed) { records.push({operation,blocked:'outside-once-or-exact-data'}); return await route.abort('blockedbyclient'); }
    const forwardedAt = Date.now();
    if (routeFailed || !withinWindow(forwardedAt)) {
      records.push({operation,blocked:routeFailed?'prior-transport-failure':'outside-approved-provisioning-window'});
      return await route.abort('blockedbyclient');
    }
    dispatched[operation] += 1;
    records.push({operation,forwardedAt:new Date(forwardedAt).toISOString(),request:body});
    return await route.continue();
    } catch (error) {
      routeFailed = true;
      records.push({routeFailure:error.name || 'Error',outcome:'HOLD_TRANSPORT_OR_SELECTOR_FAILURE'});
      await route.abort('failed').catch(() => { records.push({routeAbortFailure:true}); });
    }
  };
  const listener = response => {
    const request = response.request();
    if (request.url() !== 'https://kms.eu-central-1.amazonaws.com/') return;
    const operation = (request.headers()['x-amz-target'] || '').replace(/^TrentService\./,'');
    if (!['CreateKey','CreateAlias'].includes(operation)) return;
    const task = (async () => {
      if (operation === 'CreateAlias') {
        const body = await response.text();
        if (body.length > 16384) throw Error('metadata-body-too-large');
        records.push({operation,status:response.status(),body,receivedAt:new Date().toISOString()});
        if (response.status() === 200 && ['', '{}'].includes(body.trim())) aliasAcknowledged = true;
        return;
      }
      const body = await response.json();
      records.push({operation,status:response.status(),body,receivedAt:new Date().toISOString()});
      if (response.status() === 200 && body.KeyMetadata && /^arn:aws:kms:eu-central-1:099256073061:key\/[0-9a-f-]{36}$/.test(body.KeyMetadata.Arn)) key = body.KeyMetadata;
    })().catch(error => {
      records.push({operation,captureFailure:error.name || 'Error',receivedAt:new Date().toISOString()});
    });
    // Attach rejection handling immediately, while still inside the event callback.
    tasks.push(task);
  };
  await context.route('https://kms.eu-central-1.amazonaws.com/',handler);
  context.on('response',listener);
  try {
    await wizard.getByRole('button',{name:'Finish',exact:true}).click({timeout:5000});
    const until = Date.now() + 20000;
    while (Date.now() < until && !aliasAcknowledged) await wizard.waitForTimeout(100);
    await Promise.all(tasks);
  } catch (error) {
    records.push({localFailure:error.name || 'Error'});
  } finally {
    // Stop the UI before removing the guard, so console SDK retries cannot escape.
    if (!wizard.isClosed()) await wizard.close();
    context.off('response',listener);
    await Promise.all(tasks);
    await context.unroute('https://kms.eu-central-1.amazonaws.com/',handler);
  }
  return {dispatched,key,aliasAcknowledged,records,classification:!routeFailed && key && aliasAcknowledged ? 'ACKNOWLEDGED_REQUIRES_READBACK' : 'HOLD_FAILED_OR_AMBIGUOUS_NO_RETRY'};
}
```

## post-round4-fixes-20261026/observer-regression.mjs

Original machine-bound bytes SHA256 `2c7fbde281e4c2cb7ee8fc75748c41626082e095ac7a5bd7d575812e7081bf3f`; portable transcription SHA256 `2c7fbde281e4c2cb7ee8fc75748c41626082e095ac7a5bd7d575812e7081bf3f`. These differ when local bindings or typography were normalized.

```javascript
import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import vm from 'node:vm';

const path = process.argv[2];
if (!path) throw Error('Provide the exact operator source to test');
const source = await readFile(path, 'utf8');
const start = source.indexOf('  const listener = response => {');
const end = source.indexOf("  await context.route(", start);
assert.ok(start >= 0 && end > start, 'real response-listener seam exists');
const listener = source.slice(start, end);
const responses = [
  {
    operation: 'CreateKey', status: 200,
    body: '{"KeyMetadata":{"KeyId":"00000000-0000-4000-8000-000000000000","Arn":"arn:aws:kms:eu-central-1:099256073061:key/00000000-0000-4000-8000-000000000000"}}',
  },
  { operation: 'CreateAlias', status: 200, body: '' },
];
const context = vm.createContext({
  responses: responses.map(item => ({
    request: () => ({ url: () => 'https://kms.eu-central-1.amazonaws.com/', headers: () => ({ 'x-amz-target': `TrentService.${item.operation}` }) }),
    status: () => item.status,
    json: async () => new Response(item.body).json(),
    text: async () => item.body,
  })),
});
// No setTimeout, URL or other globals unavailable in the CLI sandbox are added.
const result = await vm.runInContext(`(async () => {
 const records = [], tasks = [];
 let key = null, aliasAcknowledged = false;
 ${listener}
 for (const response of responses) listener(response);
 const settled = await Promise.allSettled(tasks);
 return {records, key, aliasAcknowledged, rejected: settled.filter(x => x.status === 'rejected').map(x => x.reason.message)};
})()`, context);
assert.deepEqual(Array.from(result.rejected), [], 'response callbacks retain failures without rejected tasks');
assert.equal(result.aliasAcknowledged, true, 'native HTTP 200 empty-body alias response is acknowledged');
assert.ok(result.key?.Arn.endsWith('/00000000-0000-4000-8000-000000000000'));
const alias = result.records.find(item => item.operation === 'CreateAlias');
assert.equal(alias.status, 200);
assert.equal(alias.body, '', 'preserve the actual empty native response rather than inventing JSON');
process.stdout.write(JSON.stringify({classification:'OFFLINE_FIXTURE_ONLY',checks:4,passed:true,awsCalls:0}) + '\n');
```

## post-round4-fixes-20261026/review4-regressions.mjs

Original machine-bound bytes SHA256 `b62c83307d28b194c73c77a453a31e4dd81f024dc173cdb8758dd49694ce47d1`; portable transcription SHA256 `b62c83307d28b194c73c77a453a31e4dd81f024dc173cdb8758dd49694ce47d1`. These differ when local bindings or typography were normalized.

```javascript
// Entirely fake clock/context/requests; this file cannot call Playwright or AWS.
import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import vm from 'node:vm';

const source = await readFile(new URL('./key-finish-next-DRAFT.js', import.meta.url), 'utf8');
const base = Date.parse('2030-01-01T00:00:00Z');
const start = '2030-01-01T00:00:00Z', cutoff = '2030-01-01T00:55:00Z';
const key = {KeyId:'00000000-0000-4000-8000-000000000000',Arn:'arn:aws:kms:eu-central-1:099256073061:key/00000000-0000-4000-8000-000000000000'};
const policy = {Id:'key-consolepolicy-3',Version:'2012-10-17',Statement:[{Sid:'Enable IAM User Permissions',Effect:'Allow',Principal:{AWS:'arn:aws:iam::099256073061:root'},Action:'kms:*',Resource:'*'}]};
function bind(t0 = start, end = cutoff) {
  return source.replace('UNBOUND_APPROVED_ALIAS_NAME','alias/offline-fixture').replace('UNBOUND_T0',t0).replace('UNBOUND_PROVISIONING_CUTOFF',end);
}
for (const [t0,end] of [['not-a-date',cutoff],[start,'not-a-date'],['2030-02-30T00:00:00Z',cutoff],[cutoff,start],[start,start],['2030-01-01T01:00:00+01:00',cutoff]]) {
  let touched = false;
  await assert.rejects(vm.runInNewContext(bind(t0,end))({context(){touched=true;throw Error('browser touched');}}), /invalid-approved-provisioning-window/);
  assert.equal(touched,false,'invalid bounds rejected before browser access');
}

async function run(options = {}) {
  let clock = base, handler, listener, closed = false, detached = false;
  const actions = [];
  class Clock extends Date { constructor(...args){super(...(args.length?args:[clock]));} static now(){return clock;} }
  const wizard = {
    url:()=>'/kms/home#/kms/keys/create',
    getByRole:()=>({click:async()=>{
      async function send(operation, body, at) {
        clock = at;
        const request = {headers:()=>({'x-amz-target':`TrentService.${operation}`}),method:()=> 'POST',postDataJSON:()=>body,frame:()=>({page:()=>wizard}),url:()=> 'https://kms.eu-central-1.amazonaws.com/'};
        await handler({request:()=>request,
          continue:async()=>{actions.push({operation,action:'continue',at:clock});if(options.continueReject)throw Error('async-continue-rejection');
            listener({request:()=>request,status:()=>200,json:async()=>({KeyMetadata:key}),text:async()=>''});
          },
          abort:async()=>{actions.push({operation,action:'abort',at:clock});if(options.abortReject)throw Error('async-abort-rejection');},
        });
        await Promise.resolve();await Promise.resolve();
      }
      await send('CreateKey',options.invalidBody?null:{Policy:JSON.stringify(policy)},options.keyAt??base);
      if(!options.onlyKey)await send('CreateAlias',{AliasName:'alias/offline-fixture',TargetKeyId:key.KeyId},options.aliasAt??base);
    }}),
    waitForTimeout:async()=>{clock+=1000;},isClosed:()=>closed,close:async()=>{closed=true;},
  };
  const context={pages:()=>[wizard],serviceWorkers:()=>[],route:async(_,fn)=>{handler=fn;},on:(_,fn)=>{listener=fn;},off:()=>{},unroute:async()=>{assert.equal(closed,true);detached=true;}};
  const result=await vm.runInNewContext(bind(),{Date:Clock})({context:()=>context});
  assert.equal(closed,true);assert.equal(detached,true);
  return{result,actions};
}

const expired=await run({keyAt:base+55*60000+1});
assert.equal(expired.actions.filter(x=>x.action==='continue').length,0,'no late write forwarded');
assert.equal(expired.result.dispatched.CreateKey,0);
assert.equal(expired.result.classification,'HOLD_FAILED_OR_AMBIGUOUS_NO_RETRY');

const aliasExpired=await run({aliasAt:base+55*60000});
assert.equal(aliasExpired.actions.filter(x=>x.action==='continue').length,1);
assert.equal(aliasExpired.result.dispatched.CreateKey,1,'existing charge retained');
assert.equal(aliasExpired.result.dispatched.CreateAlias,0);
assert.equal(aliasExpired.result.classification,'HOLD_FAILED_OR_AMBIGUOUS_NO_RETRY');

const rejected=await run({continueReject:true});
assert.equal(rejected.result.dispatched.CreateKey,1,'failed forwarding stays counted');
assert.ok(rejected.result.records.some(x=>x.routeFailure==='Error'));
assert.ok(rejected.actions.some(x=>x.action==='abort'));
assert.equal(rejected.result.dispatched.CreateAlias,0,'transport failure stops later mutations');
assert.equal(rejected.result.classification,'HOLD_FAILED_OR_AMBIGUOUS_NO_RETRY');

const abortRejected=await run({invalidBody:true,abortReject:true,onlyKey:true});
assert.ok(abortRejected.result.records.some(x=>x.routeFailure==='Error'));
assert.ok(abortRejected.result.records.some(x=>x.routeAbortFailure===true));
assert.equal(abortRejected.result.dispatched.CreateKey,0);
assert.equal(abortRejected.result.classification,'HOLD_FAILED_OR_AMBIGUOUS_NO_RETRY');

const positive=await run();
assert.equal(positive.result.dispatched.CreateKey,1);assert.equal(positive.result.dispatched.CreateAlias,1);
assert.equal(positive.result.classification,'ACKNOWLEDGED_REQUIRES_READBACK');
process.stdout.write(JSON.stringify({classification:'OFFLINE_FIXTURE_ONLY',invalidWindowCases:6,callbackScenarios:5,passed:true,awsCalls:0})+'\n');
```

## post-round4-fixes-20261026/route-regression.mjs

Original machine-bound bytes SHA256 `b2a57f1d56a376d082a6c5572a4f2a41d86d0e1a3662af662910a7db6a8788c0`; portable transcription SHA256 `b2a57f1d56a376d082a6c5572a4f2a41d86d0e1a3662af662910a7db6a8788c0`. These differ when local bindings or typography were normalized.

```javascript
import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import vm from 'node:vm';

const source = await readFile(new URL('./key-finish-next-DRAFT.js', import.meta.url), 'utf8');
const start = source.indexOf('  const expectedPolicy = ');
const end = source.indexOf('  const listener = response => {', start);
assert.ok(start >= 0 && end > start);
const key = {KeyId:'00000000-0000-4000-8000-000000000000',Arn:'arn:aws:kms:eu-central-1:099256073061:key/00000000-0000-4000-8000-000000000000'};
const policy = {Id:'key-consolepolicy-3',Version:'2012-10-17',Statement:[{Sid:'Enable IAM User Permissions',Effect:'Allow',Principal:{AWS:'arn:aws:iam::099256073061:root'},Action:'kms:*',Resource:'*'}]};
const inputWizard = {};
const actions = [];
function route(operation, body, options = {}) {
  return {
    request: () => ({
      headers: () => ({'x-amz-target':`TrentService.${operation}`}),
      method: () => options.method || 'POST',
      postDataJSON: () => { if(options.noBody) throw SyntaxError('empty'); return body; },
      frame: () => { if(options.frameError) throw Error('no frame'); return {page:()=>inputWizard}; },
    }),
    continue: async () => { actions.push({operation,action:'continue'}); },
    abort: async () => { actions.push({operation,action:'abort'}); },
  };
}
const entry = source.slice(source.indexOf('{')+1,source.indexOf('  const context = page.context();')).replace('UNBOUND_APPROVED_ALIAS_NAME','alias/offline-fixture').replace('UNBOUND_T0','2030-01-01T00:00:00Z').replace('UNBOUND_PROVISIONING_CUTOFF','2030-01-01T00:55:00Z');
class Clock extends Date { static now(){return Date.parse('2030-01-01T00:01:00Z');} }
const context = vm.createContext({Date:Clock,inputWizard,offlineKey:key,inputRoutes:[
  route('',null,{method:'OPTIONS',noBody:true}),
  route('CreateKey',{Policy:JSON.stringify(policy)}),
  route('CreateKey',{Policy:JSON.stringify(policy)}),
  route('CreateAlias',{AliasName:'alias/wrong',TargetKeyId:key.KeyId}),
  route('CreateAlias',{AliasName:'alias/offline-fixture',TargetKeyId:key.KeyId}),
  route('CreateAlias',{AliasName:'alias/offline-fixture',TargetKeyId:key.KeyId}),
  route('CreateKey',null),
  route('CreateKey',{Policy:JSON.stringify(policy)},{frameError:true}),
]});
const result = await vm.runInContext(`(async()=>{
 const wizard=inputWizard;
 ${entry}
 ${source.slice(start,end)}
 await handler(inputRoutes[0]);
 await Promise.all([handler(inputRoutes[1]),handler(inputRoutes[2])]);
 key=offlineKey;
 await handler(inputRoutes[3]);
 await Promise.all([handler(inputRoutes[4]),handler(inputRoutes[5])]);
 const settled=await Promise.allSettled([handler(inputRoutes[6]),handler(inputRoutes[7])]);
 return{dispatched,records,rejected:settled.filter(x=>x.status==='rejected').length};
})()`,context);
assert.equal(result.rejected,0,'unsupported frame/body remains an explicit held outcome');
assert.equal(result.dispatched.CreateKey,1);
assert.equal(result.dispatched.CreateAlias,1);
assert.equal(actions.filter(x=>x.operation==='CreateKey'&&x.action==='continue').length,1);
assert.equal(actions.filter(x=>x.operation==='CreateAlias'&&x.action==='continue').length,1);
assert.equal(actions[0].action,'continue','native CORS preflight is not a mutating API call');
assert.ok(result.records.some(x=>x.blocked==='invalid-shape'));
assert.ok(result.records.some(x=>x.routeFailure==='Error'));
const inert = vm.runInNewContext(source);
await assert.rejects(inert({context:()=>{throw Error('must not touch browser');}}),/unbound-execution-data/);
process.stdout.write(JSON.stringify({classification:'OFFLINE_FIXTURE_ONLY',checks:9,passed:true,awsCalls:0})+'\n');
```

## post-round4-fixes-20261026/test_bind_policies.py

Original machine-bound bytes SHA256 `88c6cd900c597d2706e809787e923b885f9a41713734984d3c3995cf64af184d`; portable transcription SHA256 `88c6cd900c597d2706e809787e923b885f9a41713734984d3c3995cf64af184d`. These differ when local bindings or typography were normalized.

```python
"""Offline reproduction and correspondence checks; no AWS client or credentials."""
import json
import copy
from pathlib import Path
import unittest

from bind_policies import bind_templates

ROOT = Path(__file__).resolve().parent.parent


class BindPoliciesTest(unittest.TestCase):
    def setUp(self):
        self.templates = {
            path.name: json.loads(path.read_text())
            for path in (ROOT / 'revision-r3/policies-unbound').glob('*.json')
        }
        self.window = json.loads((ROOT / 'WINDOW-BINDING.json').read_text())
        self.bindings = {
            'keyARN': 'arn:aws:kms:eu-central-1:099256073061:key/64224f0e-697e-4090-9978-ea153625f6d9',
            'runPrefix': 'hikyo-e2e-20261022c05/',
            'externalId': 'hikyo-158-20261022c05',
            't0': self.window['t0'],
            'authExpiresAt': self.window['authExpiresAt'],
            'cleanupExpiresAt': self.window['cleanupExpiresAt'],
        }

    def test_matches_all_eighteen_owner_approved_corrected_documents(self):
        actual, audit = bind_templates(self.templates, self.bindings)
        self.assertEqual(len(actual), 18)
        self.assertEqual(len(audit), 129)
        for name, document in actual.items():
            expected = json.loads((ROOT / 'resolved-124809-r2' / name).read_text())
            self.assertEqual(document, expected, name)

    def test_legacy_literal_binding_reproduces_double_separator(self):
        text = json.dumps(self.templates['hikyo-158-static.policy.json'])
        legacy = text.replace('UNBOUND_APPROVED_RUN_PREFIX', self.bindings['runPrefix'])
        self.assertIn('hikyo-e2e-20261022c05//static/', legacy)
        actual, _ = bind_templates(self.templates, self.bindings)
        resource = actual['hikyo-158-static.policy.json']['Statement'][0]['Resource']
        self.assertEqual(resource, 'arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261022c05/static/*')

    def test_rejects_ambiguous_extra_separator(self):
        self.bindings['runPrefix'] += '/'
        with self.assertRaisesRegex(ValueError, 'run-prefix'):
            bind_templates(self.templates, self.bindings)

    def test_rejects_foreign_key(self):
        self.bindings['keyARN'] = self.bindings['keyARN'].replace('099256073061', '111111111111')
        with self.assertRaisesRegex(ValueError, 'key-arn'):
            bind_templates(self.templates, self.bindings)

    def test_rejects_mutation_in_an_unapproved_leaf(self):
        self.templates['hikyo-158-static.policy.json']['Statement'][0]['Action'] = 'UNBOUND_APPROVED_RUN_PREFIX'
        with self.assertRaisesRegex(ValueError, 'unapproved-template'):
            bind_templates(self.templates, self.bindings)

    def test_rejects_time_token_under_unapproved_operator(self):
        self.templates['hikyo-158-static.policy.json']['Statement'][0].setdefault('Condition', {})['StringEquals'] = {'aws:CurrentTime': 'UNBOUND_APPROVED_AUTH_UNTIL'}
        with self.assertRaisesRegex(ValueError, 'unapproved-template'):
            bind_templates(self.templates, self.bindings)

    def test_rejects_nested_resource_object(self):
        self.templates['hikyo-158-static.policy.json']['Statement'][0]['Resource'] = {'Unexpected': 'UNBOUND_APPROVED_RUN_PREFIX'}
        with self.assertRaisesRegex(ValueError, 'unapproved-template'):
            bind_templates(self.templates, self.bindings)

    def test_rejects_additional_template(self):
        self.templates['extra.policy.json'] = copy.deepcopy(self.templates['hikyo-158-static.policy.json'])
        with self.assertRaisesRegex(ValueError, 'template-file-set'):
            bind_templates(self.templates, self.bindings)


if __name__ == '__main__':
    unittest.main()
```

## private-operator/run_c06_metadata.py

Original machine-bound bytes SHA256 `069b3695cbc29e8090e6b86dde90b3cec48454fe177f4980f0875bf53daef6c9`; portable transcription SHA256 `1d9ae2c9f97a07d992b1c5b10f2519410b23b2ee5e3ea3c8549548e9892f7300`. These differ when local bindings or typography were normalized.

```python
"""Invoke one private Chrome command; retain only its selected nonsecret result."""
from pathlib import Path
import json
import os
import subprocess
import sys

root = Path(__file__).resolve().parent
name = sys.argv[1]
assert name.replace('-', '').isalnum()
environment = dict(os.environ)
environment['PATH'] = '<local-artifact>/bin'
argv = ['<local-artifact>/playwright-cli', '-s=hikyo-158-owner-approved', 'run-code', (root / (name + '.js')).read_text()]
try:
    result = subprocess.run(argv, cwd=root, env=environment, capture_output=True, timeout=45)
except subprocess.TimeoutExpired:
    print(json.dumps({'exit': 1, 'reason': 'private-command-timeout', 'rawOutputRetained': False}))
    raise SystemExit(1)
if result.returncode:
    print(json.dumps({'exit': result.returncode, 'name': name, 'reason': 'private-command-error', 'rawOutputRetained': False}))
    raise SystemExit(result.returncode)
text = result.stdout.decode()
start = text.index('### Result\n') + len('### Result\n')
end = text.find('\n### ', start)
data = json.loads(text[start:end if end >= 0 else None])
path = root / (name + '.json')
path.write_text(json.dumps(data, indent=2) + '\n')
path.chmod(0o600)
print(json.dumps({'exit': 0, 'name': name, 'selectedResult': str(path), 'rawOutputRetained': False}))
```

## private-operator/run_c12_setup_metadata.py

Original machine-bound bytes SHA256 `c010eefbe5b70a6ae822a73ec96cd09da5785554ef9d6d15db056e1efbfc3af3`; portable transcription SHA256 `0675700196172a8399dc7e3980a7d0d27701192aa24603b4f72284b4824e4779`. These differ when local bindings or typography were normalized.

```python
"""Invoke one private Chrome command; retain only its selected nonsecret result."""
from pathlib import Path
import json
import os
import subprocess
import sys

root = Path(__file__).resolve().parent
name = sys.argv[1]
assert name.replace('-', '').isalnum()
environment = dict(os.environ)
environment['PATH'] = '<local-artifact>/bin'
argv = ['<local-artifact>/playwright-cli', '-s=hikyo-158-setup-c12', 'run-code', (root / (name + '.js')).read_text()]
try:
    result = subprocess.run(argv, cwd=root, env=environment, capture_output=True, timeout=45)
except subprocess.TimeoutExpired:
    print(json.dumps({'exit': 1, 'reason': 'private-command-timeout', 'rawOutputRetained': False}))
    raise SystemExit(1)
if result.returncode:
    print(json.dumps({'exit': result.returncode, 'name': name, 'reason': 'private-command-error', 'rawOutputRetained': False}))
    raise SystemExit(result.returncode)
text = result.stdout.decode()
start = text.index('### Result\n') + len('### Result\n')
end = text.find('\n### ', start)
data = json.loads(text[start:end if end >= 0 else None])
path = root / (name + '.json')
path.write_text(json.dumps(data, indent=2) + '\n')
path.chmod(0o600)
print(json.dumps({'exit': 0, 'name': name, 'selectedResult': str(path), 'rawOutputRetained': False}))
```
