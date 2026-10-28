# Prepared operator source archive, part 6/9

**ARCHIVE ONLY, PARKED, LIVE QUALIFICATION NOT ESTABLISHED.** This preserves the prepared small-route operators and their tests so local report deletion does not erase the implementation. The transcription normalizes machine-local paths to `<report-root>`, `<approval-bank>`, `<application-checkout>`, `<trusted-source-checkout>` or `<local-artifact>`. They are reconstruction placeholders, not working filesystem paths. Original and normalized digests are both recorded. All runtime/source pins and concrete bindings must be re-established before use. Do not copy this code directly into a live operation or claim that normalization preserved an old qualification. In particular the historical unattended helper is an agent interpretation of an overnight request; present parking overrides it, and all applicable source/Ready/policy/deployment/merge gates must be reconciled with current owner direction. No credentials, authentication storage or private challenge URLs are included.

## completion-c12/run_console_preflight.py

Original machine-bound bytes SHA256 `7b1774753a6030c63fb3f48eb6eff91742891016cec55eeff462a5399a644d2f`; portable transcription SHA256 `7b1774753a6030c63fb3f48eb6eff91742891016cec55eeff462a5399a644d2f`. These differ when local bindings or typography were normalized.

```python
"""Run selected native console reads and the inert key draft, sequentially."""
from datetime import datetime, timezone
from pathlib import Path
import hashlib
import json
import subprocess
import sys

C=Path(__file__).resolve().parent
OP=C.parent/'private-operator'
items=['c12-owner-principal','c12-iam-admission','c12-kms-admission','c12-kms-list-complete','c12-secret-admission','c12-historical-secret-admission','c12-billing-admission','c12-setup-v11-archive','c12-key-draft']
for name in items:
 source=C/(name+'.js')
 target=OP/(name+'.js')
 if target.exists():assert target.read_bytes()==source.read_bytes()
 else:target.write_bytes(source.read_bytes());target.chmod(0o600)
records=[]
for name in items:
 result=subprocess.run([sys.executable,str(OP/'run_c06_metadata.py'),name],capture_output=True,text=True,timeout=50)
 record={'name':name,'exitCode':result.returncode,'at':datetime.now(timezone.utc).isoformat()}
 records.append(record)
 if result.returncode:
  record['reason']='PRIVATE_COMMAND_ERROR_NO_RAW_OUTPUT_PUBLISHED'
  break
 data=json.loads((OP/(name+'.json')).read_text())
 record['selectedMetadataSHA256']=hashlib.sha256((OP/(name+'.json')).read_bytes()).hexdigest()
 if name=='c12-owner-principal':
  assert data['accountVisible'] and data['rootIndicator']
 print(json.dumps(record),flush=True)
(C/'CONSOLE-PREFLIGHT-COMMANDS.json').write_text(json.dumps({'records':records,'window':None,'AWSWritesForwarded':0},indent=2)+'\n')
raise SystemExit(0 if len(records)==len(items) and all(row['exitCode']==0 for row in records) else 1)
```

## completion-c12/schedule-key-once-DRAFT.js

Original machine-bound bytes SHA256 `4604820e3ca47c20b3edf880acd497e6fd81d983e730ad6c0ca8acec33d55c6c`; portable transcription SHA256 `4604820e3ca47c20b3edf880acd497e6fd81d983e730ad6c0ca8acec33d55c6c`. These differ when local bindings or typography were normalized.

```javascript
async page=>{
 if(['UNBOUND_T0','UNBOUND_CLEANUP_UNTIL','UNBOUND_ACKNOWLEDGED_C12_KEY_ID'].some(x=>x.includes('UNBOUND_')))throw Error('unbound-cleanup-data');
 if(Date.now()<Date.parse('UNBOUND_T0')||Date.now()>=Date.parse('UNBOUND_CLEANUP_UNTIL'))throw Error('outside-approved-cleanup-window');
 const context=page.context(),tab=context.pages().find(p=>p.url().endsWith('#/kms/keys/scheduleDeletion/UNBOUND_ACKNOWLEDGED_C12_KEY_ID'));
 if(!tab)throw Error('missing-owned-deletion-draft');
 const key='UNBOUND_ACKNOWLEDGED_C12_KEY_ID',arn='arn:aws:kms:eu-central-1:099256073061:key/'+key;
 let forwarded=0,result=null,localFailure=null;const events=[];
 const reads=new Set(['DescribeKey','GetKeyPolicy','GetKeyRotationStatus','ListKeys','ListAliases','ListResourceTags','ListGrants','ListRetirableGrants','GetKeyLastUsage']);
 const handler=async route=>{const q=route.request(),op=(q.headers()['x-amz-target']||'').replace(/^TrentService\./,'');
  if(q.method()==='OPTIONS'||reads.has(op))return await route.continue();
  let body;try{body=q.postDataJSON();}catch{body=null;}
  if(op==='ScheduleKeyDeletion'&&q.frame().page()===tab&&forwarded===0&&Date.now()<Date.parse('UNBOUND_CLEANUP_UNTIL')&&body&&Object.keys(body).sort().join(',')==='KeyId,PendingWindowInDays'&&[key,arn].includes(body.KeyId)&&body.PendingWindowInDays===7){forwarded++;events.push({operation:op,request:body,forwardedAt:new Date().toISOString()});return await route.continue();}
  events.push({blocked:op||'unknown'});return await route.abort('blockedbyclient');
 };
 await context.route('https://kms.eu-central-1.amazonaws.com/',handler);
 try{
  await tab.getByRole('spinbutton').fill('7');await tab.getByRole('checkbox').check();
  const view=await tab.evaluate(()=>document.body.innerText.slice(-8000));if(!view.includes(key)||!view.includes('7 day waiting period'))throw Error('deletion-review-mismatch');
  events.push({review:view});
  const response=tab.waitForResponse(r=>r.url()==='https://kms.eu-central-1.amazonaws.com/'&&r.request().headers()['x-amz-target']==='TrentService.ScheduleKeyDeletion',{timeout:15000}).then(async r=>({status:r.status(),request:r.request().postData(),body:await r.text(),receivedAt:new Date().toISOString()})).catch(()=>({captureFailure:'no-complete-native-response'}));
  await tab.getByRole('button',{name:'Schedule deletion',exact:true}).click({timeout:5000});result=await response;
 }catch(error){localFailure=error.message==='deletion-review-mismatch'?error.message:error.name;}
 finally{if(!tab.isClosed())await tab.close();await context.unroute('https://kms.eu-central-1.amazonaws.com/',handler);}
 return{at:new Date().toISOString(),forwarded,result,localFailure,events,keyARN:arn,noRetry:true};
}
```

## completion-c12/setup_probes_after_login_once.py

Original machine-bound bytes SHA256 `d909b2ea986afeb669dc9800b7ce592b3b579aa1930b159d71ee33da61224b1a`; portable transcription SHA256 `057fe34e76f09adc4d7e49da6ff69020c1fb46b99a2e6ef6b7fc2d267c5d578e`. These differ when local bindings or typography were normalized.

```python
"""Run only after independently verified v6; emit public, bounded probe receipts."""
from datetime import datetime,timezone
from pathlib import Path
import hashlib,json,os,re,subprocess
r=Path('<report-root>');c=r/'completion-c12'
aws=Path('/opt/homebrew/Cellar/awscli/2.38.4/libexec/bin/aws');login=Path('<local-artifact>/aws-local-login')
assert hashlib.sha256(aws.read_bytes()).hexdigest()=='b643e78fd6c61395b1a89bfe2f1504a38115261aa29f42d1e5e35e0620119f61'
assert login.is_dir() and not login.is_symlink()
assert json.loads((c/'OFFICIAL-SETUP-LOGIN-RESULT.json').read_text())['status']=='OFFICIAL_CLI_LOGIN_SUCCESS_IDENTITY_NOT_YET_VERIFIED'
assert (c/'SETUP-ACTIVATION-READBACK.json').is_file() and json.loads((c/'SETUP-ACTIVATION-READBACK.json').read_text())['status']=='INDEPENDENT_SETUP_FULL_POLICY_READBACK_PASS'
w=json.loads((c/'WINDOW-BINDING.json').read_text());cutoff=datetime.fromisoformat(w['provisioningCutoff'].replace('Z','+00:00'))
assert datetime.fromisoformat(w['t0'].replace('Z','+00:00'))<=datetime.now(timezone.utc)<cutoff
fd=os.open(c/'SETUP-PROBES-AFTER-LOGIN-ONCE.json',os.O_CREAT|os.O_EXCL|os.O_WRONLY,0o600)
with os.fdopen(fd,'w') as stream:json.dump({'startedAt':datetime.now(timezone.utc).isoformat(),'maximumCalls':{'GetCallerIdentity':1,'ListRoles':1,'DescribeSecret':1},'automaticLoginOrRetry':False},stream);stream.flush();os.fsync(stream.fileno())
environment={key:os.environ[key] for key in ('PATH','TMPDIR','LANG') if key in os.environ}
environment.update(AWS_CONFIG_FILE=str(login/'config'),AWS_SHARED_CREDENTIALS_FILE=str(login/'credentials'),AWS_LOGIN_CACHE_DIRECTORY=str(login/'cache'),AWS_REGION='eu-central-1',AWS_DEFAULT_REGION='eu-central-1',AWS_EC2_METADATA_DISABLED='true',AWS_IGNORE_CONFIGURED_ENDPOINT_URLS='true',AWS_CLI_AUTO_PROMPT='off',AWS_PAGER='',AWS_MAX_ATTEMPTS='1',AWS_RETRY_MODE='standard')
records=[]
def save(status):
 receipt={'at':datetime.now(timezone.utc).isoformat(),'status':status,'records':records,'profile':'hikyo-158-setup','account':'099256073061','credentialContentsInspected':False,'rawOutputsPersisted':False,'maximumAttempts':1}
 (c/'SETUP-PROBE-AFTER-LOGIN-RESULT.json').write_text(json.dumps(receipt,indent=2)+'\n');print(json.dumps(receipt))
def call(operation,args):
 assert datetime.fromisoformat(w['t0'].replace('Z','+00:00'))<=datetime.now(timezone.utc)<cutoff
 started=datetime.now(timezone.utc).isoformat();command=[str(aws),'--profile','hikyo-158-setup','--region','eu-central-1','--cli-connect-timeout','10','--cli-read-timeout','10','--cli-error-format','json','--no-cli-pager','--no-cli-auto-prompt','--output','json']+args
 try:result=subprocess.run(command,env=environment,capture_output=True,timeout=min(10,(cutoff-datetime.now(timezone.utc)).total_seconds()))
 except subprocess.TimeoutExpired:records.append({'operation':operation,'sentAt':started,'status':'HOLD_TIMEOUT_NO_RETRY'});save('HOLD');raise SystemExit(1)
 public={'operation':operation,'sentAt':started,'receivedAt':datetime.now(timezone.utc).isoformat(),'exitCode':result.returncode}
 if result.returncode:
  try:
   error=json.loads(result.stderr);public['errorCode']=error.get('Code',error.get('code','UNCLASSIFIED_PRIVATE_OUTPUT_WITHHELD'))
  except (ValueError,AttributeError):public['errorCode']='UNCLASSIFIED_PRIVATE_OUTPUT_WITHHELD'
  records.append(public);return None,public
 body=json.loads(result.stdout);records.append(public);return body,public
identity,row=call('GetCallerIdentity',['sts','get-caller-identity'])
if identity!={'UserId':'AIDAROHA7PNS4ZCR3U3FR','Account':'099256073061','Arn':'arn:aws:iam::099256073061:user/hikyo-158-setup'}:save('HOLD_IDENTITY_OR_ACCESS');raise SystemExit(1)
row['identity']=identity
roles,row=call('ListRoles',['iam','list-roles','--no-paginate'])
if roles is None or roles.get('IsTruncated') is not False or 'Marker' in roles:save('HOLD_EFFECTIVE_MFA_NOT_PROVEN');raise SystemExit(1)
row['complete']=True;row['roleNames']=[x['RoleName'] for x in roles['Roles']]
if set(row['roleNames'])!={'AWSServiceRoleForResourceExplorer','AWSServiceRoleForSupport','AWSServiceRoleForTrustedAdvisor'}:save('HOLD_UNEXPECTED_ROLES');raise SystemExit(1)
sentinel='hikyo-e2e-20261028c12/unused-mfa-capability-sentinel-never-created'
body,row=call('DescribeSecret',['secretsmanager','describe-secret','--secret-id',sentinel]);row['sentinel']=sentinel
if body is not None or row.get('errorCode')!='ResourceNotFoundException':save('HOLD_SENTINEL_NOT_STRICT_NOT_FOUND');raise SystemExit(1)
save('PASS_LITERAL_MFA_AND_UNUSED_SENTINEL')
```

## completion-c12/setup_probes_existing_provider_once.py

Original machine-bound bytes SHA256 `e1f336e92ec21dc40424f97d50d67127c21990223937506cebdc767a7e954d7a`; portable transcription SHA256 `09ed99137dd6012b681800ea9baf7a8dca5099048d93f1eb31c4c613ebc38561`. These differ when local bindings or typography were normalized.

```python
"""Run only after independently verified bounded setup policy; emit public, bounded probe receipts."""
from datetime import datetime,timezone
from pathlib import Path
import hashlib,json,os,re,subprocess
r=Path('<report-root>');c=r/'completion-c12'
aws=Path('/opt/homebrew/Cellar/awscli/2.38.4/libexec/bin/aws');login=Path('<local-artifact>/aws-local-login')
assert hashlib.sha256(aws.read_bytes()).hexdigest()=='b643e78fd6c61395b1a89bfe2f1504a38115261aa29f42d1e5e35e0620119f61'
assert login.is_dir() and not login.is_symlink()
from unattended_c12 import verify_provider_prerequisites
assert (c/'SETUP-ACTIVATION-READBACK.json').is_file() and json.loads((c/'SETUP-ACTIVATION-READBACK.json').read_text())['status']=='INDEPENDENT_SETUP_FULL_POLICY_READBACK_PASS'
w=json.loads((c/'WINDOW-BINDING.json').read_text());cutoff=datetime.fromisoformat(w['provisioningCutoff'].replace('Z','+00:00'))
assert datetime.fromisoformat(w['t0'].replace('Z','+00:00'))<=datetime.now(timezone.utc)<cutoff
verify_provider_prerequisites(json.loads((c/'OWNER-UNATTENDED-REQUEST.json').read_text()),json.loads((c/'SETUP-ACTIVATION-READBACK.json').read_text()),w)
fd=os.open(c/'SETUP-PROBES-AFTER-LOGIN-ONCE.json',os.O_CREAT|os.O_EXCL|os.O_WRONLY,0o600)
with os.fdopen(fd,'w') as stream:json.dump({'startedAt':datetime.now(timezone.utc).isoformat(),'maximumCalls':{'GetCallerIdentity':1,'ListRoles':1,'DescribeSecret':1},'automaticLoginOrRetry':False},stream);stream.flush();os.fsync(stream.fileno())
environment={key:os.environ[key] for key in ('PATH','TMPDIR','LANG') if key in os.environ}
environment.update(AWS_CONFIG_FILE=str(login/'config'),AWS_SHARED_CREDENTIALS_FILE=str(login/'credentials'),AWS_LOGIN_CACHE_DIRECTORY=str(login/'cache'),AWS_REGION='eu-central-1',AWS_DEFAULT_REGION='eu-central-1',AWS_EC2_METADATA_DISABLED='true',AWS_IGNORE_CONFIGURED_ENDPOINT_URLS='true',AWS_CLI_AUTO_PROMPT='off',AWS_PAGER='',AWS_MAX_ATTEMPTS='1',AWS_RETRY_MODE='standard')
records=[]
def save(status):
 receipt={'at':datetime.now(timezone.utc).isoformat(),'status':status,'records':records,'profile':'hikyo-158-setup','account':'099256073061','credentialContentsInspected':False,'rawOutputsPersisted':False,'authenticationMethod':'official CLI existing login credential provider and normal refresh','interactiveLoginInvoked':False,'ownerDelegationPosition':3902,'maximumAttempts':1}
 (c/'SETUP-PROBE-AFTER-LOGIN-RESULT.json').write_text(json.dumps(receipt,indent=2)+'\n');print(json.dumps(receipt))
def call(operation,args):
 assert datetime.fromisoformat(w['t0'].replace('Z','+00:00'))<=datetime.now(timezone.utc)<cutoff
 started=datetime.now(timezone.utc).isoformat();command=[str(aws),'--profile','hikyo-158-setup','--region','eu-central-1','--cli-connect-timeout','10','--cli-read-timeout','10','--cli-error-format','json','--no-cli-pager','--no-cli-auto-prompt','--output','json']+args
 try:result=subprocess.run(command,env=environment,capture_output=True,timeout=min(10,(cutoff-datetime.now(timezone.utc)).total_seconds()))
 except subprocess.TimeoutExpired:records.append({'operation':operation,'sentAt':started,'status':'HOLD_TIMEOUT_NO_RETRY'});save('HOLD');raise SystemExit(1)
 public={'operation':operation,'sentAt':started,'receivedAt':datetime.now(timezone.utc).isoformat(),'exitCode':result.returncode}
 if result.returncode:
  try:
   error=json.loads(result.stderr);public['errorCode']=error.get('Code',error.get('code','UNCLASSIFIED_PRIVATE_OUTPUT_WITHHELD'))
  except (ValueError,AttributeError):public['errorCode']='UNCLASSIFIED_PRIVATE_OUTPUT_WITHHELD'
  records.append(public);return None,public
 body=json.loads(result.stdout);records.append(public);return body,public
identity,row=call('GetCallerIdentity',['sts','get-caller-identity'])
if identity!={'UserId':'AIDAROHA7PNS4ZCR3U3FR','Account':'099256073061','Arn':'arn:aws:iam::099256073061:user/hikyo-158-setup'}:save('HOLD_IDENTITY_OR_ACCESS');raise SystemExit(1)
row['identity']=identity
roles,row=call('ListRoles',['iam','list-roles','--no-paginate'])
if roles is None or roles.get('IsTruncated') is not False or 'Marker' in roles:save('HOLD_EFFECTIVE_MFA_NOT_PROVEN');raise SystemExit(1)
row['complete']=True;row['roleNames']=[x['RoleName'] for x in roles['Roles']]
if set(row['roleNames'])!={'AWSServiceRoleForResourceExplorer','AWSServiceRoleForSupport','AWSServiceRoleForTrustedAdvisor'}:save('HOLD_UNEXPECTED_ROLES');raise SystemExit(1)
sentinel='hikyo-e2e-20261028c12/unused-mfa-capability-sentinel-never-created'
body,row=call('DescribeSecret',['secretsmanager','describe-secret','--secret-id',sentinel]);row['sentinel']=sentinel
if body is not None or row.get('errorCode')!='ResourceNotFoundException':save('HOLD_SENTINEL_NOT_STRICT_NOT_FOUND');raise SystemExit(1)
save('PASS_LITERAL_MFA_AND_UNUSED_SENTINEL')
```

## completion-c12/test-iam-final-action.mjs

Original machine-bound bytes SHA256 `4c3c9263da58d1a3b7345792f5863fa9f8cbffb28bc203cda45ec5a6141c42f5`; portable transcription SHA256 `4c3c9263da58d1a3b7345792f5863fa9f8cbffb28bc203cda45ec5a6141c42f5`. These differ when local bindings or typography were normalized.

```javascript
import assert from 'node:assert/strict';
import {readFile} from 'node:fs/promises';
import vm from 'node:vm';

const source=await readFile(new URL('./iam-final-action-DRAFT.js',import.meta.url),'utf8');
const compact=JSON.stringify({Version:'2012-10-17',Statement:[]}),pretty=JSON.stringify({Version:'2012-10-17',Statement:[]},null,2);
const setup='arn:aws:iam::099256073061:policy/hikyo-158-setup-policy';
const base=Date.parse('2030-01-01T00:00:00Z'),end=base+55*60000;
const bodies={
 CreatePolicy:{Action:'CreatePolicy',Version:'2010-05-08',PolicyName:'hikyo-158-role-boundary',PolicyDocument:compact},
 DeletePolicyVersion:{Action:'DeletePolicyVersion',Version:'2010-05-08',PolicyArn:setup,VersionId:'v11'},
 CreatePolicyVersion:{Action:'CreatePolicyVersion',Version:'2010-05-08',PolicyArn:setup,PolicyDocument:pretty,SetAsDefault:'true'},
};
const buttons={CreatePolicy:'Create policy',DeletePolicyVersion:'Delete',CreatePolicyVersion:'Save changes'};
const form=body=>Object.entries(body).map(([k,v])=>encodeURIComponent(k)+'='+encodeURIComponent(v)).join('&');
function bind(operation){
 let text=source;
 const values={UNBOUND_OPERATION:operation,UNBOUND_FINAL_BUTTON:buttons[operation],UNBOUND_TAB_HASH:'#/policies/offline-fixture',UNBOUND_APPROVED_COMPACT_DOCUMENT:compact,UNBOUND_APPROVED_PRETTY_DOCUMENT:pretty,UNBOUND_T0:'2030-01-01T00:00:00Z',UNBOUND_PROVISIONING_CUTOFF:'2030-01-01T00:55:00Z'};
 for(const [token,value]of Object.entries(values))text=text.replace("'"+token+"'",JSON.stringify(value));
 return text;
}
async function run(operation,options={}){
 let now=base,handler,resolveResponse,rejectResponse,responded=false,closed=false;
 const actions=[];
 class Clock extends Date{constructor(...args){super(...(args.length?args:[now]));}static now(){return now;}}
 const tab={
  url:()=> 'https://us-east-1.console.aws.amazon.com/iam/home#/policies/offline-fixture',
  waitForResponse:()=>new Promise((resolve,reject)=>{resolveResponse=resolve;rejectResponse=reject;}),
  getByRole:()=>({click:async()=>{
   now=options.expired?end:base;
   const body=options.body||bodies[operation],raw=options.raw||form(body);
   const q={method:()=> 'POST',frame:()=>({page:()=>tab}),postData:()=>raw};
   const makeRoute=()=>({request:()=>q,
    continue:async()=>{actions.push('continue');if(options.continueReject)throw Error('async transport');if(!responded){responded=true;resolveResponse({status:()=>200,text:async()=>'<OfflineMetadataResponse/>'});}},
    abort:async()=>{actions.push('abort');if(options.abortReject)throw Error('async abort');},
   });
   await Promise.all([handler(makeRoute()),handler(makeRoute())]);
   if(!responded)rejectResponse(Error('no native response forwarded'));
  }}),
  isClosed:()=>closed,close:async()=>{closed=true;},
 };
 // Native dialog preflight found three ambient Delete buttons. Only the dialog locator is unambiguous.
 let dialogLookups=0;const originalLookup=tab.getByRole;
 tab.getByRole=(role,options)=>{
  if(role==='dialog'){dialogLookups++;return{getByRole:originalLookup};}
  if(operation==='DeletePolicyVersion'&&role==='button')throw Error('strict-mode-three-Delete-buttons');
  return originalLookup(role,options);
 };
 const context={pages:()=>[tab],serviceWorkers:()=>[],route:async(_,fn)=>{handler=fn;},unroute:async()=>assert.equal(closed,true)};
 const result=await vm.runInNewContext(bind(operation),{Date:Clock})({context:()=>context});
 assert.equal(closed,true);
 return{result,actions,dialogLookups};
}
for(const op of Object.keys(bodies)){
 const x=await run(op);assert.equal(x.result.forwarded,1);if(op==='DeletePolicyVersion')assert.equal(x.dialogLookups,1);assert.equal(x.actions.filter(v=>v==='continue').length,1);assert.equal(x.result.classification,'NATIVE_METADATA_CAPTURED_REQUIRES_INDEPENDENT_READBACK');
}
const nativeEmptyTags=await run('CreatePolicy',{body:{...bodies.CreatePolicy,Description:'',Tags:''}});assert.equal(nativeEmptyTags.result.forwarded,1);assert.equal(nativeEmptyTags.actions.filter(v=>v==='continue').length,1);
for(const op of Object.keys(bodies)){
 const tagged=await run(op,{body:{...bodies[op],Tags:'nonempty'}});assert.equal(tagged.result.forwarded,0);
 if(op!=='CreatePolicy'){const empty=await run(op,{body:{...bodies[op],Tags:''}});assert.equal(empty.result.forwarded,0);}
}
const duplicateEmptyTags=await run('CreatePolicy',{raw:form(bodies.CreatePolicy)+'&Tags=&Tags='});assert.equal(duplicateEmptyTags.result.forwarded,0);
const wrong=await run('DeletePolicyVersion',{body:{...bodies.DeletePolicyVersion,VersionId:'v5'}});assert.equal(wrong.result.forwarded,0);
const priorV10=await run('DeletePolicyVersion',{body:{...bodies.DeletePolicyVersion,VersionId:'v10'}});assert.equal(priorV10.result.forwarded,0);
const oldV1=await run('DeletePolicyVersion',{body:{...bodies.DeletePolicyVersion,VersionId:'v1'}});assert.equal(oldV1.result.forwarded,0);
const extra=await run('CreatePolicy',{body:{...bodies.CreatePolicy,'Tags.member.1.Key':'Unapproved'}});assert.equal(extra.result.forwarded,0);
const duplicate=await run('DeletePolicyVersion',{raw:form(bodies.DeletePolicyVersion)+'&VersionId=v11'});assert.equal(duplicate.result.forwarded,0);
const expired=await run('CreatePolicyVersion',{expired:true});assert.equal(expired.result.forwarded,0);
const failed=await run('CreatePolicy',{continueReject:true});assert.equal(failed.result.forwarded,1);assert.equal(failed.result.failed,true);assert.equal(failed.result.classification,'HOLD_NO_RETRY');
assert.ok(failed.result.records.some(x=>x.routeFailure==='Error'));
await assert.rejects(vm.runInNewContext(source)({context(){throw Error('browser touched');}}),/unbound-execution-data/);
process.stdout.write(JSON.stringify({classification:'OFFLINE_FIXTURE_ONLY',positiveOperations:4,negativeScenarios:13,inertEntry:true,deleteDialogScoped:true,awsCalls:0,passed:true})+'\n');
```

## completion-c12/test_billing_proof.py

Original machine-bound bytes SHA256 `1c3108dae5665a65914aa3b42b9c33c3d0193ccdcd5cf3978e366afa136e06de`; portable transcription SHA256 `1c3108dae5665a65914aa3b42b9c33c3d0193ccdcd5cf3978e366afa136e06de`. These differ when local bindings or typography were normalized.

```python
"""Offline regressions for the exact expanded native billing proof."""
from copy import deepcopy
from pathlib import Path
from unittest.mock import patch
import importlib.util
import json
import unittest

ROOT = Path(__file__).resolve().parent
spec = importlib.util.spec_from_file_location('corrected_admission', ROOT / 'admit_c12.py')
admit = importlib.util.module_from_spec(spec)
spec.loader.exec_module(admit)


class BillingProof(unittest.TestCase):
    def setUp(self):
        self.native = json.loads((ROOT / 'BILLING-NATIVE-RED-FIXTURE.json').read_text())
        self.normalized = json.loads((ROOT / 'BILLING-NORMALIZED-FIXTURE.json').read_text())

    def test_real_split_charge_witness_passes_without_a_fabricated_total(self):
        text = admit.billing_text(self.native)
        self.assertNotIn('USD\u00a00.07', text)
        admit.verify_native_billing(self.native, self.normalized)
        self.assertEqual(admit.budget('0.07', '0.00', '0.00')['usageForecastUSD'], '7.02')

    def test_underreported_gross_billable_and_tax_are_rejected(self):
        for update in ({'grossUSD': '0.06'}, {'billableUSD': '0.01'}, {'taxUSD': '0.01'}):
            with self.subTest(update=update), self.assertRaises(AssertionError):
                admit.verify_native_billing(self.native, {**self.normalized, **update})

    def test_unexplained_charge_or_changed_rate_is_rejected(self):
        extra = deepcopy(self.native)
        extra['tables'][-1] += '\nUnexpected charge\nUSD\u00a00.02'
        changed = deepcopy(self.native)
        changed['tables'][-1] = changed['tables'][-1].replace('$0.05 per 10000 API Requests', '$0.10 per 10000 API Requests')
        for value in (extra, changed):
            with self.subTest(value=value), self.assertRaises(AssertionError):
                admit.verify_native_billing(value, self.normalized)

    def test_missing_and_duplicate_usage_leaves_are_rejected(self):
        for replacement in ('', '1,117 API Requests\nUSD\u00a00.01\n1,117 API Requests'):
            value = deepcopy(self.native)
            value['tables'][-1] = value['tables'][-1].replace('1,117 API Requests', replacement)
            with self.subTest(replacement=replacement), self.assertRaises(AssertionError):
                admit.verify_native_billing(value, self.normalized)

    def test_profile_tampering_is_rejected(self):
        with patch.object(Path, 'read_bytes', return_value=b'{}'), self.assertRaises(AssertionError):
            admit.verify_native_billing(self.native, self.normalized)

    def test_changed_amounts_still_obey_the_unchanged_usage_ceiling(self):
        value = deepcopy(self.native)
        value['tables'] = [table.replace('USD\u00a00.06', 'USD\u00a00.20') for table in value['tables']]
        admit.verify_native_billing(value, {**self.normalized, 'grossUSD': '0.21'})
        with self.assertRaises(AssertionError):
            admit.budget('0.21', '0.00', '0.00')


if __name__ == '__main__':
    unittest.main()
```

## completion-c12/test_bind_policies.py

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

## completion-c12/test_kms_inventory.py

Original machine-bound bytes SHA256 `5ee98dc3a0f7201387add553502fbd2ed24ca3b556c9463d1c676d86a0a0d8eb`; portable transcription SHA256 `5ee98dc3a0f7201387add553502fbd2ed24ca3b556c9463d1c676d86a0a0d8eb`. These differ when local bindings or typography were normalized.

```python
"""Offline regression for the native console's complete key-list response."""
import copy
import json
import unittest

from admit_c12 import validate_kms_list


class KMSInventory(unittest.TestCase):
    def fixture(self):
        key='11111111-1111-4111-8111-111111111111'
        body={'Keys':[{'KeyId':key,'KeyArn':'arn:aws:kms:eu-central-1:099256073061:key/'+key,'AliasList':[]},{'KeyId':'alias/aws/s3','KeyArn':'arn:aws:kms:eu-central-1:099256073061:alias/aws/s3','AliasList':[{'Alias':'arn:aws:kms:eu-central-1:099256073061:alias/aws/s3'}]}],'Truncated':False}
        return key,body

    def test_complete_customer_ids_and_service_aliases(self):
        key,body=self.fixture()
        validate_kms_list({'records':[{'operation':'ListKeysForService','status':200,'body':json.dumps(body)}]},{key})

    def test_truncation_unknown_or_duplicated_customer_key_is_refused(self):
        key,body=self.fixture()
        variants=[]
        value=copy.deepcopy(body);value['Truncated']=True;variants.append(value)
        value=copy.deepcopy(body);value['Keys'].append(copy.deepcopy(value['Keys'][0]));variants.append(value)
        value=copy.deepcopy(body);value['Keys'][0]['KeyArn']=value['Keys'][0]['KeyArn'].replace('099256073061','111111111111');variants.append(value)
        value=copy.deepcopy(body);value['NextMarker']='unknown';variants.append(value)
        for value in variants:
            with self.subTest(value=value),self.assertRaises(AssertionError):
                validate_kms_list({'records':[{'operation':'ListKeysForService','status':200,'body':json.dumps(value)}]},{key})
        with self.assertRaises(AssertionError):
            validate_kms_list({'records':[{'operation':'ListKeysForService','status':200,'body':json.dumps(body)}]},set())


if __name__=='__main__':unittest.main()
```

## completion-c12/test_operator_bindings.py

Original machine-bound bytes SHA256 `962a7d74f75b883df73400855c55b4645fbd5a99e68279b6c1328d37c718e85d`; portable transcription SHA256 `962a7d74f75b883df73400855c55b4645fbd5a99e68279b6c1328d37c718e85d`. These differ when local bindings or typography were normalized.

```python
"""The binding projection is pure; never import the executable operator script."""
import ast
from pathlib import Path
import unittest


ROOT = Path(__file__).parent
source = ast.parse((ROOT / "bind_protected_vars.py").read_text())
function = next(node for node in source.body if isinstance(node, ast.FunctionDef) and node.name == "stored_bindings")
namespace = {}
exec(compile(ast.Module(body=[function], type_ignores=[]), "bind_protected_vars.py", "exec"), namespace)
stored_bindings = namespace["stored_bindings"]


class OperatorBindingTests(unittest.TestCase):
    def setUp(self):
        fields = next(node for node in function.body if isinstance(node, ast.Assign))
        self.values = {key: "synthetic-bound-value" for key in ast.literal_eval(fields.value)}
        self.values["AWS_ACCEPTANCE_HISTORICAL_CLEANUP_ARN"] = ""

    def test_only_nine_nonempty_values_are_stored(self):
        values = stored_bindings(self.values)
        self.assertEqual(len(values), 9)
        self.assertNotIn("AWS_ACCEPTANCE_HISTORICAL_CLEANUP_ARN", values)
        self.assertTrue(all(values.values()))

    def test_missing_extra_empty_required_or_nonstring_is_refused(self):
        variants = []
        value = dict(self.values); del value["AWS_ACCEPTANCE_APPROVED_SHA"]; variants.append(value)
        value = dict(self.values); value["extra"] = "unexpected"; variants.append(value)
        for invalid in ("", None, 1):
            value = dict(self.values); value["AWS_ACCEPTANCE_APPROVED_SHA"] = invalid; variants.append(value)
        for value in variants:
            with self.subTest(value=value), self.assertRaises(ValueError):
                stored_bindings(value)

    def test_nonempty_historical_cleanup_is_refused(self):
        self.values["AWS_ACCEPTANCE_HISTORICAL_CLEANUP_ARN"] = "unapproved-resource"
        with self.assertRaises(ValueError):
            stored_bindings(self.values)

    def test_workflow_literal_cannot_inherit_organization_variable(self):
        workflow = (ROOT / "aws-acceptance.yml").read_text()
        self.assertEqual(workflow.count('AWS_ACCEPTANCE_HISTORICAL_CLEANUP_ARN: ""'), 1)
        self.assertNotIn("vars.AWS_ACCEPTANCE_HISTORICAL_CLEANUP_ARN", workflow)


if __name__ == "__main__":
    unittest.main()
```

## completion-c12/test_prior_key_schedules.py

Original machine-bound bytes SHA256 `82551cc18fa52150cebf5e050cbea6aa6f00cb60e04be87690390c58ba0d0618`; portable transcription SHA256 `82551cc18fa52150cebf5e050cbea6aa6f00cb60e04be87690390c58ba0d0618`. These differ when local bindings or typography were normalized.

```python
"""Reject premature absence, permission refusals and changed prior schedules."""
import copy
import json
import unittest

from prior_key_schedules import reconcile


class PriorScheduleTests(unittest.TestCase):
    def setUp(self):
        self.key = '00000000-0000-4000-8000-000000000001'
        self.alias = 'alias/hikyo-158-offline-only'
        self.date = '2030-01-01T00:00:00Z'
        self.schedules = {self.key: self.date}
        self.absent = {self.alias}
        metadata = {
            'KeyId': self.key,
            'Arn': 'arn:aws:kms:eu-central-1:099256073061:key/' + self.key,
            'AWSAccountId': '099256073061',
            'KeyState': 'PendingDeletion',
            'Enabled': False,
            'DeletionDate': 1893456000.0,
        }
        self.rows = [self.row(self.key, 200, {'KeyMetadata': metadata}),
                     self.row(self.alias, 400, {'__type': 'NotFoundException'})]

    def row(self, selector, status, body):
        return {'id': selector, 'operation': 'DescribeKey',
                'request': json.dumps({'KeyId': selector}),
                'status': status, 'body': json.dumps(body)}

    def check(self, rows=None, at=None):
        point = self.date if at is None else at
        return reconcile(self.rows if rows is None else rows, self.schedules,
                         self.absent, point, point)

    def test_straddling_batch_cannot_turn_early_absence_into_deletion(self):
        rows = [self.row(self.key, 400, {'__type': 'NotFoundException'}), self.rows[1]]
        with self.assertRaises(AssertionError):
            reconcile(rows, self.schedules, self.absent,
                      '2029-12-31T23:59:59Z', '2030-01-01T00:00:01Z')

    def test_reversed_or_stale_batch_bounds_are_refused(self):
        for end in ['2029-12-31T23:59:59Z', '2030-01-01T00:05:00Z']:
            with self.assertRaises(AssertionError):
                reconcile(self.rows, self.schedules, self.absent, self.date, end)

    def test_pending_exact_schedule_before_and_after_due_time(self):
        for at in ['2029-12-31T23:59:59Z', '2030-01-02T00:00:00Z']:
            self.assertEqual(self.check(at=at)['pendingIds'], [self.key])

    def test_strict_not_found_at_or_after_due_time(self):
        rows = [self.row(self.key, 400, {'__type': 'NotFoundException'}), self.rows[1]]
        for at in [self.date, '2030-01-02T00:00:00Z']:
            self.assertEqual(self.check(rows, at)['naturallyDeletedIds'], [self.key])

    def test_not_found_before_due_time_is_refused(self):
        rows = [self.row(self.key, 400, {'__type': 'NotFoundException'}), self.rows[1]]
        with self.assertRaises(AssertionError):
            self.check(rows, '2029-12-31T23:59:59Z')

    def test_access_denied_is_not_absence_for_key_or_alias(self):
        for index in [0, 1]:
            rows = copy.deepcopy(self.rows)
            rows[index] = self.row(rows[index]['id'], 400, {'__type': 'AccessDeniedException'})
            with self.assertRaises(AssertionError):
                self.check(rows)

    def test_changed_schedule_state_identity_or_enabled_is_refused(self):
        for name, value in [('DeletionDate', 1893456001.0), ('KeyState', 'Enabled'),
                            ('KeyId', 'foreign'), ('Arn', 'foreign'),
                            ('AWSAccountId', '000000000000'), ('Enabled', True)]:
            rows = copy.deepcopy(self.rows)
            body = json.loads(rows[0]['body'])
            body['KeyMetadata'][name] = value
            rows[0]['body'] = json.dumps(body)
            with self.assertRaises(AssertionError):
                self.check(rows)

    def test_duplicate_missing_or_extra_selector_is_refused(self):
        for rows in [self.rows[:1], self.rows + self.rows[:1],
                     [self.rows[0], self.rows[0]],
                     [self.rows[0], self.row('foreign', 400, {'__type': 'NotFoundException'})]]:
            with self.assertRaises(AssertionError):
                self.check(rows)

    def test_request_selector_or_operation_substitution_is_refused(self):
        for name, value in [('request', '{"KeyId":"foreign"}'), ('operation', 'DeleteAlias')]:
            rows = copy.deepcopy(self.rows)
            rows[0][name] = value
            with self.assertRaises(AssertionError):
                self.check(rows)

    def test_capture_failure_and_unexpected_status_are_refused(self):
        for field, value in [('captureFailure', 'Timeout'), ('status', 403)]:
            rows = copy.deepcopy(self.rows)
            rows[0][field] = value
            with self.assertRaises(AssertionError):
                self.check(rows)

    def test_naive_or_invalid_observation_and_schedule_are_refused(self):
        for at in ['2030-01-01T00:00:00', 'invalid', '2030-01-01T01:00:00+01:00']:
            with self.assertRaises((AssertionError, ValueError)):
                self.check(at=at)
        self.schedules[self.key] = '2030-01-01T00:00:00'
        with self.assertRaises(AssertionError):
            self.check()


if __name__ == '__main__':
    unittest.main()
```

## completion-c12/test_retire_fixture.py

Original machine-bound bytes SHA256 `89e2ae0b953602a8eb63c6939f7a7e6e3620b5a1cb779e6bf454b8d08185cb49`; portable transcription SHA256 `20ab92231e54d49db8917dfd3374185ec679515eb9b7843bb7e8810026248112`. These differ when local bindings or typography were normalized.

```python
"""Offline boundary tests; temporary fake receipts, no AWS/browser calls."""
from pathlib import Path
from datetime import datetime, timezone
from unittest.mock import patch
from tempfile import TemporaryDirectory
import contextlib
import copy
import io
import json
import subprocess
import unittest

actual = Path(__file__).resolve().parent
historical = Path('<report-root>/continuation-c07/renewal-proposal')
fixture = json.loads((historical/'FIXTURE-RESULT.json').read_text())
source = (actual/'retire_fixture_once.py').read_text()

class RetirementBoundary(unittest.TestCase):
    def scenario(self, failure=None):
        with TemporaryDirectory(prefix='hikyo158-retirement-test-') as temp:
            root=Path(temp);c=root/'completion-c12';c.mkdir(parents=True);(c/'resolved').mkdir()
            (c/'FIXTURE-RESULT.json').write_text(json.dumps(fixture))
            (c/'WINDOW-BINDING.json').write_text(json.dumps({'t0':'2000-01-01T00:00:00Z','cleanupExpiresAt':'2050-01-01T00:00:00Z'}))
            gate={'status':'PASS_STRICT_NO_C12_SECRETS_REMAIN','hostedRunTerminal':True,'runId':'987654321','prefix':'hikyo-e2e-20261028c12/','at':datetime.now(timezone.utc).isoformat()}
            (c/'NAMESPACE-SECRET-CLEANUP-READBACK.json').write_text(json.dumps(gate))
            (c/'WORKFLOW-DISPATCH-RESULT.json').write_text(json.dumps({'runId':'987654321'}))
            for p in (historical/'resolved').glob('*.json'):
                (c/'resolved'/p.name).write_bytes(p.read_bytes())
            roles={x['name'] for x in fixture['owned']['roles']};mutations=[];attempts=[];deleted=set();provider_deleted=False
            def fake_run(argv,**kwargs):
                nonlocal provider_deleted
                operation=argv[argv.index('--output')+3]
                request=Path(argv[-1].removeprefix('file://'));self.assertTrue(request.is_relative_to(root));data=json.loads(request.read_text());attempts.append((operation,data))
                name=data.get('RoleName');body={}
                if operation=='get-caller-identity':body={'UserId':'AIDAROHA7PNS4ZCR3U3FR','Account':'099256073061','Arn':'arn:aws:iam::099256073061:user/hikyo-158-setup'}
                elif operation=='list-roles':
                    names={'AWSServiceRoleForResourceExplorer','AWSServiceRoleForSupport','AWSServiceRoleForTrustedAdvisor'} | (roles-deleted)
                    if failure=='extra-role':names.add('unowned-role')
                    body={'IsTruncated':False,'Roles':[{'RoleName':n} for n in names]}
                elif operation=='get-open-id-connect-provider':
                    body=copy.deepcopy(fixture['owned']['provider']['readback'])
                    if failure=='provider-identity':body['CreateDate']='unowned-date'
                elif operation=='get-role':
                    body=copy.deepcopy(next(x['metadata'] for x in fixture['records'] if x['operation']=='get-role' and x['metadata']['Role']['RoleName']==name))
                    if failure=='role-identity':body['Role']['RoleId']='UNOWNED_ROLE_ID'
                elif operation=='get-role-policy':body=copy.deepcopy(next(x['metadata'] for x in fixture['records'] if x['operation']=='get-role-policy' and x['metadata']['RoleName']==name))
                elif operation=='list-role-policies':body={'IsTruncated':False,'PolicyNames':['hikyo-158-acceptance']}
                elif operation=='list-attached-role-policies':body={'IsTruncated':False,'AttachedPolicies':[]}
                elif operation in {'delete-role-policy','delete-role','delete-open-id-connect-provider'}:
                    mutations.append((operation,data))
                    if failure=='timeout':raise subprocess.TimeoutExpired(argv,12)
                    if operation=='delete-role':deleted.add(name)
                    if operation=='delete-open-id-connect-provider':provider_deleted=True
                    return subprocess.CompletedProcess(argv,0,b'',b'')
                elif operation=='list-open-id-connect-providers':body={'OpenIDConnectProviderList':[] if provider_deleted else [{'Arn':fixture['owned']['provider']['arn']}]}
                else:self.fail('unexpected operation '+operation)
                return subprocess.CompletedProcess(argv,0,json.dumps(body).encode(),b'')
            text=source.replace("r = Path('<report-root>')",'r = Path('+repr(str(root))+')',1)
            exit_code=0
            with patch('subprocess.run',side_effect=fake_run),contextlib.redirect_stdout(io.StringIO()):
                try:exec(compile(text,'offline-retirement','exec'),{})
                except SystemExit as error:exit_code=error.code
            result=json.loads((c/'IAM-RETIREMENT-RESULT.json').read_text())
            return exit_code,result,mutations,attempts
    def test_native_empty_success_and_exact_complete_retirement(self):
        code,result,writes,_=self.scenario();self.assertEqual(code,0);self.assertEqual(result['status'],'PASS_EXACT_EIGHT_ROLES_INLINE_POLICIES_AND_OWNED_ISSUER_RETIRED');self.assertEqual(len(writes),17)
        self.assertEqual(len({json.dumps(x,sort_keys=True) for x in writes}),17)
    def test_recreated_role_stops_before_any_write(self):
        code,result,writes,_=self.scenario('role-identity');self.assertEqual(code,1);self.assertEqual(writes,[]);self.assertEqual(result['status'],'HOLD_METADATA_MISMATCH_NO_RETRY')
    def test_recreated_provider_stops_before_any_write(self):
        code,_,writes,_=self.scenario('provider-identity');self.assertEqual(code,1);self.assertEqual(writes,[])
    def test_unrelated_role_stops_before_any_write(self):
        code,_,writes,_=self.scenario('extra-role');self.assertEqual(code,1);self.assertEqual(writes,[])
    def test_timeout_is_once_and_no_later_mutation(self):
        code,result,writes,_=self.scenario('timeout');self.assertEqual(code,1);self.assertEqual(len(writes),1);self.assertEqual(result['status'],'HOLD_NO_RETRY')

if __name__=='__main__':unittest.main()
```
