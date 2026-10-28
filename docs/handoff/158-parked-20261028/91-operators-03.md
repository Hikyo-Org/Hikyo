# Prepared operator source archive, part 3/9

**ARCHIVE ONLY, PARKED, LIVE QUALIFICATION NOT ESTABLISHED.** This preserves the prepared small-route operators and their tests so local report deletion does not erase the implementation. The transcription normalizes machine-local paths to `<report-root>`, `<approval-bank>`, `<application-checkout>`, `<trusted-source-checkout>` or `<local-artifact>`. They are reconstruction placeholders, not working filesystem paths. Original and normalized digests are both recorded. All runtime/source pins and concrete bindings must be re-established before use. Do not copy this code directly into a live operation or claim that normalization preserved an old qualification. In particular the historical unattended helper is an agent interpretation of an overnight request; present parking overrides it, and all applicable source/Ready/policy/deployment/merge gates must be reconciled with current owner direction. No credentials, authentication storage or private challenge URLs are included.

## completion-c12/fetch_hosted_terminal_evidence.py

Original machine-bound bytes SHA256 `ab29d34a76c74c06454322065c19dfabce26ce4fd3043d0925b144a07aa82cdb`; portable transcription SHA256 `39f30097491041ce2dc285c7c44982de7ce2bea758f4c8ba096e33a984beb9d1`. These differ when local bindings or typography were normalized.

```python
"""Read only the sole terminal run and its bounded nonsecret artifact."""
from pathlib import Path
from datetime import datetime, timezone
import hashlib
import io
import json
import os
import subprocess
import zipfile

c=Path('<report-root>/completion-c12')
repo='repos/Hikyo-Org/Hikyo/'
def api(path):return json.loads(subprocess.check_output(['gh','api',repo+path],timeout=25))
run=api('actions/runs/'+json.loads((c/'WORKFLOW-DISPATCH-RESULT.json').read_text())['runId'])
assert run['id']==int(json.loads((c/'WORKFLOW-DISPATCH-RESULT.json').read_text())['runId']) and run['run_attempt']==1 and run['head_sha']==json.loads((c/'MAIN-CORRESPONDENCE.json').read_text())['observedMain'] and run['head_branch']=='main' and run['event']=='workflow_dispatch' and run['actor']['login']=='Dunky13'
if run['status']!='completed':
    print(json.dumps({'status':'NOT_TERMINAL_NO_ARTIFACT_FETCH','runId':str(run['id']),'runStatus':run['status']}));raise SystemExit(2)
artifacts=api('actions/runs/'+json.loads((c/'WORKFLOW-DISPATCH-RESULT.json').read_text())['runId']+'/artifacts?per_page=100')
expected='aws-158-'+json.loads((c/'WORKFLOW-DISPATCH-RESULT.json').read_text())['runId']+'-c253ff9b1ccec7408f13366f8c5640c8c7cf3d0a'
matches=[x for x in artifacts['artifacts'] if x['name']==expected and not x['expired']]
assert len(matches)==1 and len(artifacts['artifacts'])==artifacts['total_count']
artifact=matches[0]
assert artifact['size_in_bytes']<=2*1024*1024
assert artifact['workflow_run']['id']==int(json.loads((c/'WORKFLOW-DISPATCH-RESULT.json').read_text())['runId']) and artifact['workflow_run']['head_sha']==json.loads((c/'MAIN-CORRESPONDENCE.json').read_text())['observedMain']
result=subprocess.run(['gh','api',repo+'actions/artifacts/'+str(artifact['id'])+'/zip'],capture_output=True,timeout=40)
assert result.returncode==0 and 0<len(result.stdout)<=4*1024*1024
actual=hashlib.sha256(result.stdout).hexdigest()
assert artifact['digest']=='sha256:'+actual
archive=zipfile.ZipFile(io.BytesIO(result.stdout))
assert archive.namelist()==['evidence.json']
member=archive.getinfo('evidence.json')
assert member.file_size<=1024*1024 and member.compress_size<=2*1024*1024 and not member.is_dir()
raw=archive.read(member)
evidence=json.loads(raw)
assert evidence['schema']=='hikyo.aws-acceptance/v2' and str(evidence['run_id'])==json.loads((c/'WORKFLOW-DISPATCH-RESULT.json').read_text())['runId'] and evidence['candidate_sha']=='c253ff9b1ccec7408f13366f8c5640c8c7cf3d0a' and evidence['workflow_sha']==json.loads((c/'MAIN-CORRESPONDENCE.json').read_text())['observedMain']
assert evidence['account']=='099256073061' and evidence['region']=='eu-central-1' and evidence['prefix']=='hikyo-e2e-20261028c12/'
folder=c/'hosted-evidence';folder.mkdir(mode=0o700,exist_ok=False)
def exclusive(path,data):
    fd=os.open(path,os.O_CREAT|os.O_EXCL|os.O_WRONLY,0o600)
    with os.fdopen(fd,'wb') as f:f.write(data);f.flush();os.fsync(f.fileno())
exclusive(folder/'evidence.json',raw)
exclusive(folder/'artifact.zip',result.stdout)
exclusive(c/'HOSTED-RUN-TERMINAL.json',(json.dumps(run,indent=2)+'\n').encode())
receipt={'at':datetime.now(timezone.utc).isoformat(),'runId':json.loads((c/'WORKFLOW-DISPATCH-RESULT.json').read_text())['runId'],'attempt':1,'runConclusion':run['conclusion'],'artifact':artifact,'artifactZipSHA256':actual,'evidenceSHA256':hashlib.sha256(raw).hexdigest(),'evidenceStatus':evidence['status'],'stage':evidence['stage'],'tests':evidence['tests'],'failureAssertions':evidence['failure_assertions'],'failureAssertionsTruncated':evidence['failure_assertions_truncated'],'recordedArnCount':len(evidence['recorded_arns']),'verifiedAbsentArnCount':len(evidence['verified_absent_arns']),'unresolvedNames':evidence['unresolved_names'],'reservationCount':len(evidence['creation_reservations']),'completeCreationLedger':evidence['complete_creation_ledger'],'historicalCleanup':evidence['historical_cleanup'],'rawPrivateProjectionDownloaded':False}
exclusive(c/'HOSTED-EVIDENCE-RECEIPT.json',(json.dumps(receipt,indent=2)+'\n').encode())
print(json.dumps({k:receipt[k] for k in ['runId','attempt','runConclusion','evidenceStatus','stage','recordedArnCount','verifiedAbsentArnCount','reservationCount','completeCreationLedger','historicalCleanup']}))
```

## completion-c12/iam-cleanup-DRAFT.js

Original machine-bound bytes SHA256 `9821a4d641b9147a4e5ded29f2314141eed95546c9aa80b671685a51c9ddecf0`; portable transcription SHA256 `9821a4d641b9147a4e5ded29f2314141eed95546c9aa80b671685a51c9ddecf0`. These differ when local bindings or typography were normalized.

```javascript
async page => {
  // Inert until separately bound to an owner-approved operation, window and data.
  const step='UNBOUND_STEP', tabHash='UNBOUND_TAB_HASH';
  const compactDocument='UNBOUND_COMPACT_DOCUMENT', prettyDocument='UNBOUND_PRETTY_DOCUMENT';
  const t0='UNBOUND_T0', cutoff='UNBOUND_CUTOFF';
  if([step,tabHash,compactDocument,prettyDocument,t0,cutoff].some(x=>x.includes('UNBOUND_')))throw Error('unbound-data');
  const setup='arn:aws:iam::099256073061:policy/hikyo-158-setup-policy';
  const boundary='arn:aws:iam::099256073061:policy/hikyo-158-role-boundary';
  const specs={
    RestoreSetupV5:{action:'SetDefaultPolicyVersion',arn:setup,version:'v5',button:'Set as default',dialog:false,cleanup:true},
    DeleteSetupV6:{action:'DeletePolicyVersion',arn:setup,version:'v6',button:'Delete',dialog:true,cleanup:false},
    RenewBoundary:{action:'CreatePolicyVersion',arn:boundary,button:'Save changes',dialog:false,cleanup:false},
    RenewSetup:{action:'CreatePolicyVersion',arn:setup,button:'Save changes',dialog:false,cleanup:false},
    DeleteBoundaryV1:{action:'DeletePolicyVersion',arn:boundary,version:'v1',button:'Delete',dialog:true,cleanup:true},
    DeleteUnusedBoundary:{action:'DeletePolicy',arn:boundary,button:'Delete',dialog:true,cleanup:true},
  };
  const spec=specs[step];if(!spec)throw Error('unsupported-step');
  const utc=value=>{
    if(!/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z$/.test(value))throw Error('invalid-window');
    const n=Date.parse(value);if(!Number.isFinite(n)||new Date(n).toISOString()!==value.replace('Z','.000Z'))throw Error('invalid-window');return n;
  };
  const start=utc(t0),end=utc(cutoff),maximum=(spec.cleanup?240:55)*60000;
  if(end<=start||end-start>maximum)throw Error('invalid-window');
  const inside=()=>Number.isFinite(Date.now())&&Date.now()>=start&&Date.now()<end;
  if(!inside())throw Error('outside-window');
  const context=page.context(),tabs=context.pages().filter(p=>p.url().startsWith('https://us-east-1.console.aws.amazon.com/iam/home')&&p.url().endsWith(tabHash));
  if(tabs.length!==1||context.serviceWorkers().length)throw Error('unsupported-owner-context');
  const tab=tabs[0],records=[];let forwarded=0,failed=false,response=null;
  const parse=text=>{
    if(text.length>65536)throw Error('query-size');const result=Object.create(null);
    for(const pair of text.split('&')){
      const i=pair.indexOf('=');if(i<1)throw Error('query-shape');
      const name=decodeURIComponent(pair.slice(0,i).replace(/\+/g,' '));
      if(!['Action','Version','PolicyArn','VersionId','PolicyDocument','SetAsDefault'].includes(name)||Object.hasOwn(result,name))throw Error('unexpected-or-duplicate-field');
      result[name]=decodeURIComponent(pair.slice(i+1).replace(/\+/g,' '));
    }return result;
  };
  const action=q=>{const pair=(q.postData()||'').split('&').find(x=>x.startsWith('Action='));return pair?decodeURIComponent(pair.slice(7).replace(/\+/g,' ')):'';};
  const handler=async route=>{
    try{
      const q=route.request(),op=action(q);
      if(q.method()==='OPTIONS'||/^(Get|List)[A-Za-z]+$/.test(op))return await route.continue();
      if(q.method()!=='POST'||q.frame().page()!==tab||op!==spec.action||forwarded||failed)return await route.abort('blockedbyclient');
      const f=parse(q.postData()||''),keys=Object.keys(f).sort().join(',');
      let valid=f.Action===spec.action&&f.Version==='2010-05-08'&&f.PolicyArn===spec.arn;
      if(spec.action==='CreatePolicyVersion')valid=valid&&keys==='Action,PolicyArn,PolicyDocument,SetAsDefault,Version'&&f.SetAsDefault==='true'&&typeof f.PolicyDocument==='string'&&[compactDocument.trim(),prettyDocument.trim()].includes(f.PolicyDocument.trim());
      else if(spec.action==='DeletePolicy')valid=valid&&keys==='Action,PolicyArn,Version';
      else valid=valid&&keys==='Action,PolicyArn,Version,VersionId'&&f.VersionId===spec.version;
      if(!valid||!inside())return await route.abort('blockedbyclient');
      forwarded++;records.push({operation:op,forwardedAt:new Date(Date.now()).toISOString()});await route.continue();
    }catch(error){failed=true;records.push({routeFailure:error.name||'Error'});await route.abort('failed').catch(()=>records.push({routeAbortFailure:true}));}
  };
  await context.route('https://iam.amazonaws.com/',handler);
  try{
    const pending=tab.waitForResponse(r=>r.url()==='https://iam.amazonaws.com/'&&action(r.request())===spec.action,{timeout:15000})
      .then(async r=>{const body=await r.text();if(body.length>131072)throw Error('metadata-size');return{status:r.status(),body,receivedAt:new Date().toISOString()};})
      .catch(()=>({captureFailure:'native-response-unavailable'}));
    const target=spec.dialog?tab.getByRole('dialog'):tab;
    await target.getByRole('button',{name:spec.button,exact:true}).click({timeout:5000});response=await pending;
  }catch(error){failed=true;records.push({localFailure:error.name||'Error'});}
  finally{if(!tab.isClosed())await tab.close();await context.unroute('https://iam.amazonaws.com/',handler);}
  return{step,operation:spec.action,forwarded,failed,response,records,classification:!failed&&forwarded===1&&response?.status===200?'NATIVE_METADATA_REQUIRES_INDEPENDENT_READBACK':'HOLD_NO_RETRY'};
}
```

## completion-c12/iam-final-action-DRAFT.js

Original machine-bound bytes SHA256 `da3406c6410c172550403c8d3c90675942a29ace27ca40957e7386624a3cc4c7`; portable transcription SHA256 `da3406c6410c172550403c8d3c90675942a29ace27ca40957e7386624a3cc4c7`. These differ when local bindings or typography were normalized.

```javascript
async page => {
  // Local native-console limiter, not an AWS client or execution authorization.
  const operation='UNBOUND_OPERATION', button='UNBOUND_FINAL_BUTTON', tabHash='UNBOUND_TAB_HASH';
  const compactDocument='UNBOUND_APPROVED_COMPACT_DOCUMENT', prettyDocument='UNBOUND_APPROVED_PRETTY_DOCUMENT';
  const t0='UNBOUND_T0', cutoff='UNBOUND_PROVISIONING_CUTOFF';
  if ([operation,button,tabHash,compactDocument,prettyDocument,t0,cutoff].some(x=>x.includes('UNBOUND_'))) throw Error('unbound-execution-data');
  const allowedButtons={CreatePolicy:['Create policy'],DeletePolicyVersion:['Delete','Delete policy version'],CreatePolicyVersion:['Save changes','Save policy']};
  if (!allowedButtons[operation]?.includes(button)) throw Error('unsupported-owner-operation');
  const parseUtcBound=value=>{
    if (!/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z$/.test(value)) throw Error('invalid-approved-provisioning-window');
    const parsed=Date.parse(value);
    if (!Number.isFinite(parsed)||new Date(parsed).toISOString()!==value.replace('Z','.000Z')) throw Error('invalid-approved-provisioning-window');
    return parsed;
  };
  const startsAt=parseUtcBound(t0),endsAt=parseUtcBound(cutoff);
  if(endsAt<=startsAt||endsAt-startsAt>55*60*1000)throw Error('invalid-approved-provisioning-window');
  const withinWindow=now=>Number.isFinite(now)&&now>=startsAt&&now<endsAt;
  if(!withinWindow(Date.now()))throw Error('outside-approved-provisioning-window');
  const context=page.context();
  const tab=context.pages().find(p=>p.url().startsWith('https://us-east-1.console.aws.amazon.com/iam/home')&&p.url().endsWith(tabHash));
  if(!tab||context.serviceWorkers().length)throw Error('unsupported-owner-context');
  const policyArn='arn:aws:iam::099256073061:policy/hikyo-158-setup-policy';
  let forwarded=0,failed=false,response=null;const records=[];
  const readAction=q=>{
    const pair=(q.postData()||'').split('&').find(x=>x.startsWith('Action='));
    return pair?decodeURIComponent(pair.slice(7).replace(/\+/g,' ')):'';
  };
  const fieldsFor=text=>{
    const permitted=new Set(['Action','Version','PolicyName','PolicyDocument','Path','Description','PolicyArn','VersionId','SetAsDefault','Tags']);
    const result=Object.create(null);
    for(const pair of text.split('&')){
      const i=pair.indexOf('=');if(i<1)throw Error('query-shape');
      const name=decodeURIComponent(pair.slice(0,i).replace(/\+/g,' '));
      if(!permitted.has(name)||Object.hasOwn(result,name))throw Error('unapproved-or-duplicate-field');
      result[name]=decodeURIComponent(pair.slice(i+1).replace(/\+/g,' '));
    }
    return result;
  };
  const handler=async route=>{
    try{
      const q=route.request(),action=readAction(q);
      if(q.method()==='OPTIONS'||/^(Get|List)[A-Za-z]+$/.test(action))return await route.continue();
      if(q.method()!=='POST'||q.frame().page()!==tab||action!==operation||forwarded||failed) return await route.abort('blockedbyclient');
      const raw=q.postData()||'';if(raw.length>65536)throw Error('query-size');
      const f=fieldsFor(raw),keys=Object.keys(f).sort().join(',');
      const documentMatches=typeof f.PolicyDocument==='string'&&[compactDocument.trim(),prettyDocument.trim()].includes(f.PolicyDocument.trim());
      let matches=f.Action===operation&&f.Version==='2010-05-08';
      if(operation==='CreatePolicy')matches=matches&&f.PolicyName==='hikyo-158-role-boundary'&&documentMatches&&(!f.Path||f.Path==='/')&&!f.Description&&(f.Tags===undefined||f.Tags==='')&&Object.keys(f).every(k=>['Action','Version','PolicyName','PolicyDocument','Path','Description','Tags'].includes(k));
      if(operation==='DeletePolicyVersion')matches=matches&&keys==='Action,PolicyArn,Version,VersionId'&&f.PolicyArn===policyArn&&f.VersionId==='v11';
      if(operation==='CreatePolicyVersion')matches=matches&&keys==='Action,PolicyArn,PolicyDocument,SetAsDefault,Version'&&f.PolicyArn===policyArn&&f.SetAsDefault==='true'&&documentMatches;
      const at=Date.now();if(!matches||!withinWindow(at))return await route.abort('blockedbyclient');
      forwarded++;records.push({operation,forwardedAt:new Date(at).toISOString()});
      return await route.continue();
    }catch(error){failed=true;records.push({routeFailure:error.name||'Error'});await route.abort('failed').catch(()=>records.push({routeAbortFailure:true}));}
  };
  await context.route('https://iam.amazonaws.com/',handler);
  try{
    const pending=tab.waitForResponse(r=>r.url()==='https://iam.amazonaws.com/'&&readAction(r.request())===operation,{timeout:15000})
      .then(async r=>{const body=await r.text();if(body.length>131072)throw Error('metadata-size');return{status:r.status(),body,receivedAt:new Date().toISOString()};})
      .catch(()=>({captureFailure:'native-response-unavailable'}));
    const finalTarget=operation==='DeletePolicyVersion'?tab.getByRole('dialog'):tab;
    await finalTarget.getByRole('button',{name:button,exact:true}).click({timeout:5000});response=await pending;
  }catch(error){failed=true;records.push({localFailure:error.name||'Error'});}
  finally{if(!tab.isClosed())await tab.close();await context.unroute('https://iam.amazonaws.com/',handler);}
  return{operation,forwarded,failed,response,records,classification:!failed&&forwarded===1&&response?.status===200?'NATIVE_METADATA_CAPTURED_REQUIRES_INDEPENDENT_READBACK':'HOLD_NO_RETRY'};
}
```

## completion-c12/key-finish-DRAFT.js

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

## completion-c12/key_once.py

Original machine-bound bytes SHA256 `5190a661bcc53ff625dbaa9837b66020fcac6fe7e71571abc857c6f6d21261a0`; portable transcription SHA256 `5190a661bcc53ff625dbaa9837b66020fcac6fe7e71571abc857c6f6d21261a0`. These differ when local bindings or typography were normalized.

```python
"""Prepare or invoke the existing guarded native key operation exactly once."""
from datetime import datetime, timezone
from pathlib import Path
import hashlib
import json
import os
import subprocess
import sys

C = Path(__file__).resolve().parent
OP = C.parent/'private-operator'
SHA = lambda path: hashlib.sha256(path.read_bytes()).hexdigest()
LOAD = lambda path: json.loads(path.read_text())


def exclusive(path, value):
    fd = os.open(path, os.O_CREAT | os.O_EXCL | os.O_WRONLY, 0o600)
    with os.fdopen(fd, 'w') as stream:
        json.dump(value, stream, indent=2); stream.write('\n'); stream.flush(); os.fsync(stream.fileno())
    directory = os.open(path.parent, os.O_RDONLY)
    os.fsync(directory); os.close(directory)


def main():
    mode = sys.argv[1]
    assert mode in {'prepare', 'invoke'}
    w = LOAD(C/'WINDOW-BINDING.json')
    admission = LOAD(C/'FRESH-ADMISSION.json')
    now = datetime.now(timezone.utc)
    assert admission['status'] == 'PASS_C12_ADMISSION'
    assert 0 <= (now-datetime.fromisoformat(admission['at'].replace('Z','+00:00'))).total_seconds() < 300
    assert now < datetime.fromisoformat(w['provisioningCutoff'].replace('Z','+00:00'))
    source = C/'key-finish-DRAFT.js'
    assert SHA(source) == '79b75b4e03d44b8fe300a067379b2e284c82a8b423f111d2bb5dd325d00cada8'
    bound = source.read_text()
    for token, value in {'UNBOUND_APPROVED_ALIAS_NAME':'alias/hikyo-158-20261028c12', 'UNBOUND_T0':w['t0'], 'UNBOUND_PROVISIONING_CUTOFF':w['provisioningCutoff']}.items():
        assert bound.count("'"+token+"'") == 1
        bound = bound.replace("'"+token+"'", json.dumps(value))
    script = OP/'c12-key-finish-bound.js'
    bound_sha = hashlib.sha256(bound.encode()).hexdigest()
    if mode == 'prepare':
        draft = LOAD(OP/'c12-key-draft.json')
        assert draft['finishEnabled'] and draft['serviceWorkers'] == 0
        for text in ['hikyo-158-20261028c12','SYMMETRIC_DEFAULT','Single-Region key','No tags to display','No key administrators selected.','No key users selected.','arn:aws:iam::099256073061:root']:
            assert text in draft['review']
        exclusive(C/'BOOTSTRAP-INTENT.json', {'at':now.isoformat(), 'boundSourceSHA256':bound_sha, 'sourceSHA256':SHA(source), 'maximumForwards':{'CreateKey':1,'CreateAlias':1}, 'alias':'alias/hikyo-158-20261028c12', 'retry':False, 'window':w})
        with script.open('x') as stream:
            stream.write(bound)
        script.chmod(0o600)
        print(json.dumps({'prepared':True,'AWSCalls':0}))
        return
    assert datetime.fromisoformat(w['t0'].replace('Z','+00:00')) <= now
    assert SHA(script) == bound_sha == LOAD(C/'BOOTSTRAP-INTENT.json')['boundSourceSHA256']
    exclusive(C/'KEY-NATIVE-ONCE-INVOCATION.json', {'at':now.isoformat(), 'intentSHA256':SHA(C/'BOOTSTRAP-INTENT.json'), 'maximumForwards':{'CreateKey':1,'CreateAlias':1}, 'retry':False})
    raise SystemExit(subprocess.run([sys.executable,str(OP/'run_c06_metadata.py'),'c12-key-finish-bound']).returncode)


if __name__ == '__main__':
    main()
```

## completion-c12/normalize_native_billing.py

Original machine-bound bytes SHA256 `880bc918d94508804eb40d835c5e206cbf6575c193cc17e61e4462d8c34374ed`; portable transcription SHA256 `880bc918d94508804eb40d835c5e206cbf6575c193cc17e61e4462d8c34374ed`. These differ when local bindings or typography were normalized.

```python
"""Derive native billing data; the owner-adopted admission independently proves it."""
from pathlib import Path
from decimal import Decimal
import hashlib
import json
import re

import admit_c12

C = Path(__file__).resolve().parent
native_path = C.parent / 'private-operator/c12-billing-admission.json'
native = json.loads(native_path.read_text())
admit_c12.billing_text(native)
charges = []
quantity = r'[0-9][0-9,.]*\s+(Keys|Requests|Secrets|Request|API Requests)'
money = r'USD[\s\u00a0]+([0-9]+\.[0-9]{2})'
for table in native['tables']:
    tokens = [line.strip() for line in table.splitlines() if line.strip()]
    for index, token in enumerate(tokens):
        if re.fullmatch(quantity, token):
            assert index > 0 and index + 1 < len(tokens)
            amount = re.fullmatch(money, tokens[index + 1])
            assert amount
            charges.append({'rate': tokens[index - 1], 'quantity': token, 'USD': amount.group(1)})


def total(label):
    assert native['totals'].count(label) == 1
    index = native['totals'].index(label)
    assert index + 1 < len(native['totals'])
    value = re.fullmatch(money, native['totals'][index + 1])
    assert value
    return value.group(1)


record = {'at': native['at'], 'grossUSD': str(sum((Decimal(row['USD']) for row in charges), Decimal('0.00'))), 'billableUSD': total('Estimated grand total:'), 'taxUSD': total('Total tax'), 'nativeReceiptSHA256': hashlib.sha256(native_path.read_bytes()).hexdigest(), 'grossDerivation': 'Sum of all expanded usage leaves before credits, without ancestor duplication', 'usageCharges': charges, 'providerHardCapClaimed': False}
admit_c12.verify_native_billing(native, record)
forecast = admit_c12.budget(record['grossUSD'], record['billableUSD'], record['taxUSD'])
(C / 'BILLING-NORMALIZED.json').write_text(json.dumps(record, indent=2) + '\n')
print(json.dumps({'nativeAt': record['at'], 'usageLeaves': len(charges), 'budget': forecast, 'AWSCalls': 0}))
```

## completion-c12/official_setup_login_once.py

Original machine-bound bytes SHA256 `10de1ea8befcbac5a21ea2d00b386b1a478871d3ea4328867fef4921e17b3fdb`; portable transcription SHA256 `3fef563ba8c01f3b5f1cd68d50c0bd475fb1d92d6abb728ae497fee54cbe2518`. These differ when local bindings or typography were normalized.

```python
"""One official CLI renewal; native Chrome auth is private and never inspected."""
from pathlib import Path
from datetime import datetime,timezone
import hashlib,json,os,subprocess,time
c=Path('<report-root>/completion-c12');p=Path('<local-artifact>/aws-local-login');aws=Path('/opt/homebrew/Cellar/awscli/2.38.4/libexec/bin/aws')
assert hashlib.sha256(aws.read_bytes()).hexdigest()=='b643e78fd6c61395b1a89bfe2f1504a38115261aa29f42d1e5e35e0620119f61'
assert json.loads((c/'SETUP-ACTIVATION-READBACK.json').read_text())['status']=='INDEPENDENT_SETUP_FULL_POLICY_READBACK_PASS'
assert json.loads((c/'FRESH-ADMISSION.json').read_text())['status']=='PASS_C12_ADMISSION'
cutoff=datetime.fromisoformat(json.loads((c/'WINDOW-BINDING.json').read_text())['provisioningCutoff'].replace('Z','+00:00'));cleanup=datetime.fromisoformat(json.loads((c/'WINDOW-BINDING.json').read_text())['cleanupExpiresAt'].replace('Z','+00:00'));remaining=(cutoff-datetime.now(timezone.utc)).total_seconds();assert remaining>180
fd=os.open(c/'OFFICIAL-SETUP-LOGIN-INTENT.once.json',os.O_CREAT|os.O_EXCL|os.O_WRONLY,0o600)
with os.fdopen(fd,'w') as f:json.dump({'at':datetime.now(timezone.utc).isoformat(),'profile':'hikyo-158-setup','expectedPrincipal':'arn:aws:iam::099256073061:user/hikyo-158-setup','maximumOfficialLoginInvocationsThisExplicitManualRequest':1,'originalLoginTimeoutPreserved':True,'notProvisioningAuthority':True,'priorMFAProbeAttempts':0,'priorSentinelProbeAttempts':0,'rawOutputRetained':False,'credentialContentsInspected':False,'method':'official aws login with fresh isolated native Chrome profile; no custom login broker','trigger':'c12 approved private setup login after owner bootstrap','changes':'browser profile isolation only; no cookies inspected or cleared','provisioningStillHeld':True,'pid':os.getpid()},f,indent=2);f.flush();os.fsync(f.fileno())
env={k:os.environ[k] for k in ('PATH','TMPDIR','LANG') if k in os.environ};env.update(AWS_CONFIG_FILE=str(p/'config'),AWS_SHARED_CREDENTIALS_FILE=str(p/'credentials'),AWS_LOGIN_CACHE_DIRECTORY=str(p/'cache'),AWS_REGION='eu-central-1',AWS_DEFAULT_REGION='eu-central-1',AWS_EC2_METADATA_DISABLED='true',AWS_IGNORE_CONFIGURED_ENDPOINT_URLS='true',AWS_CLI_AUTO_PROMPT='off',AWS_PAGER='',AWS_MAX_ATTEMPTS='1',AWS_RETRY_MODE='standard',BROWSER='/usr/bin/open -n -a "Google Chrome" --args --user-data-dir=<report-root>/private-operator/setup-login-clean-c11 --no-first-run --no-default-browser-check --new-window %s')
print(json.dumps({'phase':'OFFICIAL_SETUP_LOGIN_PRIVATE_CHROME','profile':'hikyo-158-setup','rawOutputsWillBeDiscarded':True}),flush=True)
try:
 result=subprocess.run([str(aws),'login','--profile','hikyo-158-setup','--region','eu-central-1','--no-cli-pager','--no-cli-auto-prompt'],env=env,stdin=subprocess.DEVNULL,stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=min(480,remaining-180))
 status='OFFICIAL_CLI_LOGIN_SUCCESS_IDENTITY_NOT_YET_VERIFIED' if result.returncode==0 else 'HOLD_OFFICIAL_LOGIN_FAILED_NO_RETRY';code=result.returncode
except subprocess.TimeoutExpired:status='HOLD_OFFICIAL_LOGIN_TIMEOUT_NO_RETRY';code=None
record={'at':datetime.now(timezone.utc).isoformat(),'status':status,'exitCode':code,'profile':'hikyo-158-setup','rawOutputRetained':False,'credentialContentsInspected':False,'MFAAndSentinelStillRequired':True}
(c/'OFFICIAL-SETUP-LOGIN-RESULT.json').write_text(json.dumps(record,indent=2)+'\n');print(json.dumps(record),flush=True)
raise SystemExit(0 if status=='OFFICIAL_CLI_LOGIN_SUCCESS_IDENTITY_NOT_YET_VERIFIED' else 1)
```
