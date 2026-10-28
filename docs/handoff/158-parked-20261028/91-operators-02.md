# Prepared operator source archive, part 2/9

**ARCHIVE ONLY, PARKED, LIVE QUALIFICATION NOT ESTABLISHED.** This preserves the prepared small-route operators and their tests so local report deletion does not erase the implementation. The transcription normalizes machine-local paths to `<report-root>`, `<approval-bank>`, `<application-checkout>`, `<trusted-source-checkout>` or `<local-artifact>`. They are reconstruction placeholders, not working filesystem paths. Original and normalized digests are both recorded. All runtime/source pins and concrete bindings must be re-established before use. Do not copy this code directly into a live operation or claim that normalization preserved an old qualification. In particular the historical unattended helper is an agent interpretation of an overnight request; present parking overrides it, and all applicable source/Ready/policy/deployment/merge gates must be reconciled with current owner direction. No credentials, authentication storage or private challenge URLs are included.

## completion-c12/c12-final-iam-reconciliation.js

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

## completion-c12/c12-final-secret-closure.js

Original machine-bound bytes SHA256 `ec18f6fc1ef9db40b9ac6e921362b39ed257143ffe41cecc639ffab3ff7e2fb7`; portable transcription SHA256 `daa7f4f422ac15a1265bf9c97e255fb57424e24e6ef301f02fa4696b53036c09`. These differ when local bindings or typography were normalized.

```javascript
async page => {
 const context=page.context(),tab=await context.newPage();
 let capture=tab.waitForResponse(r=>r.url()==='https://secretsmanager.eu-central-1.amazonaws.com/'&&r.request().headers()['x-amz-target']==='secretsmanager.ListSecrets',{timeout:15000}).then(async response=>({request:response.request().postDataJSON(),status:response.status(),body:await response.json()})).catch(error=>({captureFailure:error.name}));
 try{
  await tab.goto('https://eu-central-1.console.aws.amazon.com/secretsmanager/listsecrets?region=eu-central-1',{waitUntil:'domcontentloaded',timeout:15000});
  let record=await capture;if(record.captureFailure)return record;
  if(record.request.IncludePlannedDeletion!==true){
   await tab.getByRole('button',{name:'Preferences',exact:true}).click({timeout:5000});
   capture=tab.waitForResponse(r=>r.url()==='https://secretsmanager.eu-central-1.amazonaws.com/'&&r.request().headers()['x-amz-target']==='secretsmanager.ListSecrets'&&r.request().postDataJSON().IncludePlannedDeletion===true,{timeout:12000}).then(async response=>({request:response.request().postDataJSON(),status:response.status(),body:await response.json()})).catch(error=>({captureFailure:error.name}));
   await tab.getByRole('checkbox',{name:'Show secrets scheduled for deletion',exact:true}).check();await tab.getByRole('button',{name:'Save',exact:true}).click();record=await capture;
  }
  if(JSON.stringify(record).length>65536)throw Error('metadata-too-large');return{at:new Date().toISOString(),operation:'ListSecrets',...record};
 }finally{await capture;if(!tab.isClosed())await tab.close();}
}
```

## completion-c12/c12-historical-secret-admission.js

Original machine-bound bytes SHA256 `6c51689b6d490efdeec4b0d6261a1aabe087c41450c7d9780580dc7ce5f0fe78`; portable transcription SHA256 `c5fcec64d282ca6ab6d13dab4e31efdcfa30d7935b4ab9d876a7e6e94d304e2d`. These differ when local bindings or typography were normalized.

```javascript
async page => {
 const old='arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261018b/web-identity/postgres/role-document-vmN1Vj';
 const tab=await page.context().newPage();
 const capture=tab.waitForResponse(r=>r.url()==='https://secretsmanager.eu-central-1.amazonaws.com/'&&r.request().headers()['x-amz-target']==='secretsmanager.DescribeSecret'&&r.request().postData()===JSON.stringify({SecretId:old}),{timeout:15000}).then(async response=>{const body=await response.text();if(body.length>16384||/SecretString|SecretBinary/.test(body))throw Error('metadata-response-refused');return{status:response.status(),body};}).catch(error=>({captureFailure:error.name}));
 try{await tab.goto('https://eu-central-1.console.aws.amazon.com/secretsmanager/secret?name='+encodeURIComponent(old)+'&region=eu-central-1',{waitUntil:'domcontentloaded',timeout:15000});return{at:new Date().toISOString(),operation:'DescribeSecret',secretARN:old,...await capture};}finally{await capture;if(!tab.isClosed())await tab.close();}
}
```

## completion-c12/c12-iam-admission.js

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

## completion-c12/c12-key-draft.js

Original machine-bound bytes SHA256 `23f8306192578a410e35c7d0c6681c19a5fe4a0d391ffe5400f30e46f9b10d2d`; portable transcription SHA256 `d4b010b4e4963593b6ed6a3f6df9dea3a8147826ad216d20917b88acbcbab45c`. These differ when local bindings or typography were normalized.

```javascript
async page=>{
 const context=page.context(),records=[],tasks=[];
 for(const prior of context.pages().filter(p=>p.url().includes('/kms/home')&&p.url().includes('#/kms/keys/create'))){const text=await prior.locator('body').innerText();if(!text.includes('hikyo-158-20261028c12'))throw Error('unowned-key-draft');await prior.close();}
 const readOnly=new Set(['DescribeKey','GetKeyPolicy','GetKeyRotationStatus','ListKeys','ListKeysForService','ListAliases','ListResourceTags','ListGrants','ListRetirableGrants','ListKeyPolicies']);
 await context.route('https://kms.eu-central-1.amazonaws.com/',async route=>{const q=route.request(),op=(q.headers()['x-amz-target']||'').replace(/^TrentService\./,'');if(q.method()==='OPTIONS'||readOnly.has(op))return await route.continue();return await route.abort('blockedbyclient');});
 const listener=r=>{const q=r.request(),op=(q.headers()['x-amz-target']||'').replace(/^TrentService\./,'');if(q.url()==='https://kms.eu-central-1.amazonaws.com/'&&['ListKeys','ListKeysForService','ListAliases'].includes(op))tasks.push(r.text().then(body=>{if(body.length>65536)throw Error('metadata-too-large');records.push({operation:op,request:q.postData(),status:r.status(),body});}).catch(error=>records.push({operation:op,captureFailure:error.name})));};
 context.on('response',listener);
 const tab=await context.newPage();
 try{
 await tab.goto('https://eu-central-1.console.aws.amazon.com/kms/home?region=eu-central-1#/kms/keys',{waitUntil:'domcontentloaded',timeout:15000});await tab.getByRole('link',{name:'Create key',exact:true}).waitFor({state:'visible',timeout:10000});
 await tab.goto('https://eu-central-1.console.aws.amazon.com/kms/home?region=eu-central-1#/kms/keys/create',{waitUntil:'domcontentloaded',timeout:15000});
 await tab.getByRole('heading',{name:/Configure key/}).first().waitFor({state:'visible',timeout:10000});
 const decline=tab.getByRole('button',{name:'Decline',exact:true});if(await decline.isVisible())await decline.click();
 await tab.getByRole('button',{name:'Next',exact:true}).click({timeout:5000});
 await tab.getByPlaceholder('Display name for the key',{exact:true}).fill('hikyo-158-20261028c12');
 await tab.getByRole('button',{name:'Skip to Review',exact:true}).click({timeout:5000});
 await tab.getByRole('button',{name:'Finish',exact:true}).waitFor({state:'visible',timeout:5000});
 await Promise.all(tasks);
 return{at:new Date().toISOString(),review:await tab.evaluate(()=>document.body.innerText.slice(-14000)),finishEnabled:await tab.getByRole('button',{name:'Finish',exact:true}).isEnabled(),serviceWorkers:context.serviceWorkers().length,records};
 }finally{context.off('response',listener);await Promise.all(tasks);}
}
```

## completion-c12/c12-key-readback-DRAFT.js

Original machine-bound bytes SHA256 `3e267852cf18aed175eddf1664866b61c8fdfed60f975093c085fd43389580f0`; portable transcription SHA256 `3e267852cf18aed175eddf1664866b61c8fdfed60f975093c085fd43389580f0`. These differ when local bindings or typography were normalized.

```javascript
async page=>{
 const context=page.context(),tab=await context.newPage(),key='UNBOUND_ACKNOWLEDGED_C12_KEY_ID',records=[],pending=[];
 const allowed=new Set(['DescribeKey','GetKeyPolicy','ListAliases','ListResourceTags','GetKeyRotationStatus']);
 const listener=r=>{const q=r.request(),operation=(q.headers()['x-amz-target']||'').replace(/^TrentService\./,'');
  if(q.url()!=='https://kms.eu-central-1.amazonaws.com/'||!allowed.has(operation))return;
  const selector=q.postDataJSON().KeyId;if(!['UNBOUND_ACKNOWLEDGED_C12_KEY_ID','arn:aws:kms:eu-central-1:099256073061:key/UNBOUND_ACKNOWLEDGED_C12_KEY_ID'].includes(selector))return;
  pending.push((async()=>{try{const body=await r.text();if(body.length>65536)throw Error('metadata-too-large');records.push({operation,request:q.postData(),status:r.status(),body});}catch(error){records.push({operation,captureFailure:error.name});}})());};
 context.on('response',listener);
 try{
  await tab.goto('https://eu-central-1.console.aws.amazon.com/kms/home?region=eu-central-1#/kms/keys/'+key,{waitUntil:'domcontentloaded',timeout:15000});
  await tab.getByRole('tab',{name:/^Key policy/}).click({timeout:10000});
  await tab.getByRole('tab',{name:/^Aliases/}).click({timeout:10000});
  await tab.getByRole('row').filter({hasText:'hikyo-158-20261028c12'}).first().waitFor({state:'visible',timeout:10000});
  const aliases=await tab.evaluate(()=>Array.from(document.querySelectorAll('table')).map(n=>n.innerText));
  const tags=tab.getByRole('tab',{name:/^Tags/});if(await tags.count())await tags.click({timeout:5000});
  await tab.getByRole('tab',{name:'Key material and rotations',exact:true}).click({timeout:5000});await tab.waitForLoadState('networkidle',{timeout:5000}).catch(()=>{});
  await Promise.all(pending);
  return{at:new Date().toISOString(),records,aliases,view:await tab.evaluate(()=>document.body.innerText.slice(-14000))};
 }finally{context.off('response',listener);await Promise.all(pending);if(!tab.isClosed())await tab.close();}
}
```

## completion-c12/c12-kms-admission.js

Original machine-bound bytes SHA256 `f4bb34b353bd9c42b03dc4af4a319267261e8cd011f502eda21a0bf382c00363`; portable transcription SHA256 `800c0df067b6472c43d06c1c9bf482eb381719ad43382a512bf70bbdb00d4fa4`. These differ when local bindings or typography were normalized.

```javascript
async page => {
 const readStartedAt=new Date().toISOString();
 const ids=['81294276-467e-45b2-8279-1abb6fea8e34','alias/hikyo-158-20261028c12','03dcf56b-9687-4173-97d5-6f4925d08797','alias/hikyo-158-20261027c10','88e19f70-fee7-4f9b-9b35-847bba607a55','alias/hikyo-158-20261027c09','b71f3c57-626a-4a89-9fcf-f7901a421e49','alias/hikyo-158-20261027c08','alias/hikyo-158-20261027c11','b8806a19-57c8-4d77-9d1d-39c611a933a1','4005b859-369d-4fa5-a96c-7b8e865ee712','10908b8d-fe54-40e3-9210-a3eefea709b6','990c479f-d673-4bc9-8bca-7b9b86f4b205','fa374267-a456-4e79-b5b1-6cdf356b7d0e','bfa44dc1-ee8a-43b8-aec7-d8a7fbb38db1','64224f0e-697e-4090-9978-ea153625f6d9','alias/hikyo-158-20261022c05','alias/hikyo-158-20261026c06','alias/hikyo-158-20261026c07'];
 const records=[];
 for(let index=0;index<ids.length;index+=2) await Promise.all(ids.slice(index,index+2).map(async id=>{
  const tab=await page.context().newPage();
  const capture=tab.waitForResponse(r=>r.url()==='https://kms.eu-central-1.amazonaws.com/'&&r.request().headers()['x-amz-target']==='TrentService.DescribeKey'&&r.request().postData()===JSON.stringify({KeyId:id}),{timeout:12000}).then(async r=>{const body=await r.text();if(body.length>32768)throw Error('metadata-too-large');return {id,operation:'DescribeKey',request:r.request().postData(),status:r.status(),body};}).catch(error=>({id,captureFailure:error.name}));
  try{await tab.goto('https://eu-central-1.console.aws.amazon.com/kms/home?region=eu-central-1#/kms/keys/'+id,{waitUntil:'domcontentloaded',timeout:15000});records.push(await capture);}finally{await capture;if(!tab.isClosed())await tab.close();}
 }));
 return{readStartedAt,at:new Date().toISOString(),records};
}
```

## completion-c12/c12-kms-list-complete.js

Original machine-bound bytes SHA256 `de95be93d13418d2582b4c41019859d052d4ff2bb809ea1976db24d559921cc9`; portable transcription SHA256 `de95be93d13418d2582b4c41019859d052d4ff2bb809ea1976db24d559921cc9`. These differ when local bindings or typography were normalized.

```javascript
async page=>{
 const context=page.context(),tab=await context.newPage(),records=[],tasks=[];
 const listener=response=>{const q=response.request(),operation=(q.headers()['x-amz-target']||'').replace(/^TrentService\./,'');if(response.url()!=='https://kms.eu-central-1.amazonaws.com/'||!['ListKeys','ListKeysForService','ListAliases','DescribeKey'].includes(operation))return;tasks.push(response.text().then(body=>{if(body.length>131072)throw Error('metadata-too-large');records.push({operation,request:q.postData(),status:response.status(),body});}).catch(error=>records.push({operation,captureFailure:error.name})));};
 const reads=new Set(['ListKeys','ListKeysForService','ListAliases','DescribeKey','GetKeyLastUsage','GetKeyPolicy','ListKeyPolicies','ListResourceTags','GetKeyRotationStatus','ListGrants','ListRetirableGrants']);
 await tab.route('https://kms.eu-central-1.amazonaws.com/',async route=>{try{const q=route.request(),operation=(q.headers()['x-amz-target']||'').replace(/^TrentService\./,'');if(q.method()==='OPTIONS'||(q.method()==='POST'&&q.frame().page()===tab&&reads.has(operation)))return await route.continue();return await route.abort('blockedbyclient');}catch(error){await route.abort('failed').catch(()=>{});}});
 context.on('response',listener);
 try{await tab.goto('https://eu-central-1.console.aws.amazon.com/kms/home?region=eu-central-1#/kms/keys',{waitUntil:'domcontentloaded',timeout:15000});await tab.getByRole('table').waitFor({state:'visible',timeout:10000});await tab.getByText('Loading keys',{exact:true}).waitFor({state:'hidden',timeout:15000});await tab.waitForLoadState('networkidle',{timeout:5000}).catch(()=>{});await Promise.all(tasks);return{at:new Date().toISOString(),records,tables:await tab.getByRole('table').allTextContents(),AWSWritesForwarded:0};}
 finally{context.off('response',listener);await Promise.all(tasks);if(!tab.isClosed())await tab.close();}
}
```

## completion-c12/c12-owner-access.js

Original machine-bound bytes SHA256 `5d2a2f2667f149666449b20de6d432b52954d4752bf21bfd69deaec12781de7c`; portable transcription SHA256 `5d2a2f2667f149666449b20de6d432b52954d4752bf21bfd69deaec12781de7c`. These differ when local bindings or typography were normalized.

```javascript
async page=>{
 await page.goto('https://eu-central-1.console.aws.amazon.com/kms/home?region=eu-central-1#/kms/keys',{waitUntil:'domcontentloaded',timeout:15000});
 await page.waitForLoadState('networkidle',{timeout:8000}).catch(()=>{});
 return await page.evaluate(()=>({at:new Date().toISOString(),consolePage:location.hostname.endsWith('console.aws.amazon.com'),accountVisible:document.body.innerText.includes('099256073061'),needsPrivateSignIn:!location.hostname.endsWith('console.aws.amazon.com')}));
}
```

## completion-c12/c12-owner-principal.js

Original machine-bound bytes SHA256 `ab3c85482cb3faed9231554ac4325dff75d930fe02ae96e7ff20767aa3ecbc96`; portable transcription SHA256 `ab3c85482cb3faed9231554ac4325dff75d930fe02ae96e7ff20767aa3ecbc96`. These differ when local bindings or typography were normalized.

```javascript
async page=>{
 const tab=await page.context().newPage();
 try{
  await tab.goto('https://us-east-1.console.aws.amazon.com/iam/home?region=eu-central-1#/security_credentials',{waitUntil:'domcontentloaded',timeout:15000});
  await tab.getByRole('heading',{name:/^My security credentials\s+Root user$/}).waitFor({state:'visible',timeout:10000});
  return await tab.evaluate(()=>({at:new Date().toISOString(),accountVisible:document.body.innerText.includes('099256073061'),rootIndicator:[...document.querySelectorAll('h1,h2')].some(n=>n.textContent.replace(/\s+/g,' ').trim()==='My security credentials Root user')}));
 }finally{if(!tab.isClosed())await tab.close();}
}
```

## completion-c12/c12-post-iam-retirement-readback.js

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

## completion-c12/c12-secret-admission.js

Original machine-bound bytes SHA256 `ec18f6fc1ef9db40b9ac6e921362b39ed257143ffe41cecc639ffab3ff7e2fb7`; portable transcription SHA256 `daa7f4f422ac15a1265bf9c97e255fb57424e24e6ef301f02fa4696b53036c09`. These differ when local bindings or typography were normalized.

```javascript
async page => {
 const context=page.context(),tab=await context.newPage();
 let capture=tab.waitForResponse(r=>r.url()==='https://secretsmanager.eu-central-1.amazonaws.com/'&&r.request().headers()['x-amz-target']==='secretsmanager.ListSecrets',{timeout:15000}).then(async response=>({request:response.request().postDataJSON(),status:response.status(),body:await response.json()})).catch(error=>({captureFailure:error.name}));
 try{
  await tab.goto('https://eu-central-1.console.aws.amazon.com/secretsmanager/listsecrets?region=eu-central-1',{waitUntil:'domcontentloaded',timeout:15000});
  let record=await capture;if(record.captureFailure)return record;
  if(record.request.IncludePlannedDeletion!==true){
   await tab.getByRole('button',{name:'Preferences',exact:true}).click({timeout:5000});
   capture=tab.waitForResponse(r=>r.url()==='https://secretsmanager.eu-central-1.amazonaws.com/'&&r.request().headers()['x-amz-target']==='secretsmanager.ListSecrets'&&r.request().postDataJSON().IncludePlannedDeletion===true,{timeout:12000}).then(async response=>({request:response.request().postDataJSON(),status:response.status(),body:await response.json()})).catch(error=>({captureFailure:error.name}));
   await tab.getByRole('checkbox',{name:'Show secrets scheduled for deletion',exact:true}).check();await tab.getByRole('button',{name:'Save',exact:true}).click();record=await capture;
  }
  if(JSON.stringify(record).length>65536)throw Error('metadata-too-large');return{at:new Date().toISOString(),operation:'ListSecrets',...record};
 }finally{await capture;if(!tab.isClosed())await tab.close();}
}
```

## completion-c12/c12-secret-pre-key-retirement.js

Original machine-bound bytes SHA256 `ec18f6fc1ef9db40b9ac6e921362b39ed257143ffe41cecc639ffab3ff7e2fb7`; portable transcription SHA256 `daa7f4f422ac15a1265bf9c97e255fb57424e24e6ef301f02fa4696b53036c09`. These differ when local bindings or typography were normalized.

```javascript
async page => {
 const context=page.context(),tab=await context.newPage();
 let capture=tab.waitForResponse(r=>r.url()==='https://secretsmanager.eu-central-1.amazonaws.com/'&&r.request().headers()['x-amz-target']==='secretsmanager.ListSecrets',{timeout:15000}).then(async response=>({request:response.request().postDataJSON(),status:response.status(),body:await response.json()})).catch(error=>({captureFailure:error.name}));
 try{
  await tab.goto('https://eu-central-1.console.aws.amazon.com/secretsmanager/listsecrets?region=eu-central-1',{waitUntil:'domcontentloaded',timeout:15000});
  let record=await capture;if(record.captureFailure)return record;
  if(record.request.IncludePlannedDeletion!==true){
   await tab.getByRole('button',{name:'Preferences',exact:true}).click({timeout:5000});
   capture=tab.waitForResponse(r=>r.url()==='https://secretsmanager.eu-central-1.amazonaws.com/'&&r.request().headers()['x-amz-target']==='secretsmanager.ListSecrets'&&r.request().postDataJSON().IncludePlannedDeletion===true,{timeout:12000}).then(async response=>({request:response.request().postDataJSON(),status:response.status(),body:await response.json()})).catch(error=>({captureFailure:error.name}));
   await tab.getByRole('checkbox',{name:'Show secrets scheduled for deletion',exact:true}).check();await tab.getByRole('button',{name:'Save',exact:true}).click();record=await capture;
  }
  if(JSON.stringify(record).length>65536)throw Error('metadata-too-large');return{at:new Date().toISOString(),operation:'ListSecrets',...record};
 }finally{await capture;if(!tab.isClosed())await tab.close();}
}
```

## completion-c12/c12-setup-draft-DRAFT.js

Original machine-bound bytes SHA256 `8399d2342f798821f5ca244393415622a902df0b8810a6c263c83cd2fe6dd679`; portable transcription SHA256 `229b769160e56149e4391958f31c00539b688052bb216717d9f25a86259cf569`. These differ when local bindings or typography were normalized.

```javascript
async page=>{
 const context=page.context(),tab=await context.newPage(),arn='arn:aws:iam::099256073061:policy/hikyo-158-setup-policy';
 await tab.goto('https://us-east-1.console.aws.amazon.com/iam/home?region=eu-central-1#/policies/details/'+encodeURIComponent(arn),{waitUntil:'domcontentloaded',timeout:15000});
 await tab.getByRole('button',{name:'Edit policy permissions',exact:true}).first().click({timeout:10000});
 await tab.getByRole('button',{name:'JSON',exact:true}).click({timeout:10000});
 const reviewed='UNBOUND_EXACT_APPROVED_POLICY_DOCUMENT';const input=tab.locator('textarea.ace_text-input');await input.focus();await input.press('Meta+A');await tab.keyboard.insertText(reviewed);
 if((await tab.locator('.ace_editor').evaluate(e=>e.env.editor.getValue())).trim()!==reviewed.trim())throw Error('editor-mismatch');
 await tab.getByRole('button',{name:'Next',exact:true}).click({timeout:5000});await tab.getByRole('button',{name:'Save changes',exact:true}).waitFor({state:'visible',timeout:10000});
 const review=await tab.evaluate(()=>document.body.innerText.slice(-4000));if(/maximum of 5 versions|replacing the oldest/i.test(review))throw Error('stale-version-state-refused');
 const checkbox=tab.getByRole('checkbox',{name:/Set this new version as the default/});if(await checkbox.count()!==1)throw Error('unsupported-default-control');await checkbox.check({timeout:3000});
 return{at:new Date().toISOString(),hash:await tab.evaluate(()=>location.hash),finalButton:'Save changes',setAsDefault:true,editorExact:true,review,AWSWritesForwarded:0};
}
```

## completion-c12/c12-setup-readback.js

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

## completion-c12/c12-setup-v11-archive.js

Original machine-bound bytes SHA256 `0d9fe1af299ddc7b56bd77036277896c4ac28897eb7aa2828ba24b0748d475d7`; portable transcription SHA256 `0d9fe1af299ddc7b56bd77036277896c4ac28897eb7aa2828ba24b0748d475d7`. These differ when local bindings or typography were normalized.

```javascript
async page=>{
 const context=page.context(),tab=await context.newPage(),arn='arn:aws:iam::099256073061:policy/hikyo-158-setup-policy';
 try{
  await tab.goto('https://us-east-1.console.aws.amazon.com/iam/home?region=eu-central-1#/policies/details/'+encodeURIComponent(arn),{waitUntil:'domcontentloaded',timeout:15000});
  await tab.getByRole('tab',{name:/^Policy versions/}).click({timeout:10000});
  const row=tab.getByRole('row').filter({hasText:'Version 11'}).first();await row.waitFor({state:'visible',timeout:10000});
  const capture=tab.waitForResponse(response=>{if(response.url()!=='https://iam.amazonaws.com/')return false;const text=response.request().postData()||'';return text.includes('Action=GetPolicyVersion')&&text.includes('PolicyArn='+encodeURIComponent(arn))&&/(?:^|&)VersionId=v11(?:&|$)/.test(text);},{timeout:12000})
    .then(async response=>{const body=await response.text();if(body.length>131072)throw Error('metadata-too-large');return{operation:'GetPolicyVersion',request:response.request().postData(),status:response.status(),body};}).catch(error=>({operation:'GetPolicyVersion',captureFailure:error.name}));
  await row.getByRole('button',{name:'Toggle open the preview of this policy with name hikyo-158-setup-policy',exact:true}).click({timeout:5000});
  return{at:new Date().toISOString(),records:[await capture],AWSWritesForwarded:0};
 }finally{if(!tab.isClosed())await tab.close();}
}
```

## completion-c12/c12-unused-boundary-readback.js

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

## completion-c12/c12-v11-delete-draft.js

Original machine-bound bytes SHA256 `4462e3ab41eb832028a3fd85c9ae769ca33f347f9214ed857742263303fe4d10`; portable transcription SHA256 `a4258aa9b29b82d9f4799677bcb33ebc50ad8a6d7dc17dc81e951f61c32004c7`. These differ when local bindings or typography were normalized.

```javascript
async page=>{
 const context=page.context(),arn='arn:aws:iam::099256073061:policy/hikyo-158-setup-policy',hash='#/policies/details/'+encodeURIComponent(arn);
 for(const old of context.pages().filter(p=>p.url().startsWith('https://us-east-1.console.aws.amazon.com/iam/home')&&p.url().endsWith(hash)))await old.close();
 const tab=await context.newPage();await tab.goto('https://us-east-1.console.aws.amazon.com/iam/home?region=eu-central-1'+hash,{waitUntil:'domcontentloaded',timeout:15000});
 await tab.getByRole('tab',{name:/^Policy versions/}).click({timeout:10000});
 const row=tab.getByRole('row').filter({hasText:'Version 11'}).first();await row.getByRole('checkbox').check({timeout:10000});
 const buttons=tab.getByRole('button',{name:'Delete',exact:true});if(await buttons.count()!==2)throw Error('unsupported-delete-controls');await buttons.last().click({timeout:5000});
 const dialog=tab.getByRole('dialog');await dialog.waitFor({state:'visible',timeout:5000});const text=await dialog.innerText();
 if(!/version\s*11|v11/i.test(text)||text.includes('Version 5'))throw Error('wrong-version-dialog');
 const allowed=['Delete','Delete policy version'];const names=(await dialog.getByRole('button').allTextContents()).map(x=>x.trim());const finalButton=names.find(x=>allowed.includes(x));if(!finalButton)throw Error('unsupported-delete-final');
 const globalMatches=await tab.getByRole('button',{name:finalButton,exact:true}).count();
 return{at:new Date().toISOString(),hash:await tab.evaluate(()=>location.hash),dialog:text,finalButton,globalButtonMatches:globalMatches,selectedVersion:'v11',AWSMutationsForwarded:0};
}
```

## completion-c12/c12-v11-deletion-readback.js

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

## completion-c12/dispatch_workflow_once.py

Original machine-bound bytes SHA256 `620002d755872481454080ced261facdf64c220b50dc3fb8a48a9022e41cd3e7`; portable transcription SHA256 `d208a469dbd76eebcce6b5bf7fc75e0d5f295739bc1a09d0bce1019c07467423`. These differ when local bindings or typography were normalized.

```python
from pathlib import Path
from datetime import datetime,timezone
import hashlib,json,os,re,subprocess,time
r=Path('<report-root>');c=r/'completion-c12'
assert json.loads((c/'FIXTURE-RESULT.json').read_text())['status']=='PASS_EIGHT_EXACT_ROLES_PROVIDER_POLICIES_TRUSTS_BOUNDARIES_TAGS'
assert json.loads((c/'SETUP-PROBE-AFTER-LOGIN-RESULT.json').read_text())['status']=='PASS_LITERAL_MFA_AND_UNUSED_SENTINEL'
w=json.loads((c/'WINDOW-BINDING.json').read_text());cutoff=datetime.fromisoformat(w['provisioningCutoff'].replace('Z','+00:00'));assert datetime.fromisoformat(w['t0'].replace('Z','+00:00'))<=datetime.now(timezone.utc)<cutoff
repo='Hikyo-Org/Hikyo';candidate='c253ff9b1ccec7408f13366f8c5640c8c7cf3d0a'
def api(path):return json.loads(subprocess.check_output(['gh','api','repos/'+repo+'/'+path]))
assert json.loads(subprocess.check_output(['gh','api','user']))['login']=='Dunky13'
main=api('branches/main')['commit']['sha'];assert main==json.loads((c/'MAIN-CORRESPONDENCE.json').read_text())['observedMain']
env=api('environments/hikyo-158-aws-sandbox');assert env['can_admins_bypass'] is False and any(x['type']=='required_reviewers' and [r['reviewer']['login'] for r in x['reviewers']]==['Dunky13'] for x in env['protection_rules'])
assert api('environments/hikyo-158-aws-sandbox/variables')['total_count']==0
before=api('actions/workflows/aws-acceptance.yml/runs?per_page=100')['workflow_runs'];assert all(x['status']=='completed' for x in before);known={x['id'] for x in before}
intent={'sentAt':datetime.now(timezone.utc).isoformat(),'operation':'one_workflow_dispatch','workflow':'aws-acceptance.yml','ref':'main','reviewed_sha':candidate,'observedMain':main,'maximumDispatchesForEntirePool':1,'attempt':1,'retry':False,'fixtureSHA256':hashlib.sha256((c/'FIXTURE-RESULT.json').read_bytes()).hexdigest()}
assert not (c/'WORKFLOW-DISPATCH-INTENT.once.json').exists()
claimfd=os.open(c/'C12-POOL-DISPATCH-CLAIM.once.json',os.O_CREAT|os.O_EXCL|os.O_WRONLY,0o600)
with os.fdopen(claimfd,'w') as f:json.dump({'at':datetime.now(timezone.utc).isoformat(),'maximumDispatchesEntireOriginalPool':1,'attempt':1,'newReservations':32,'lifetime':336},f);f.flush();os.fsync(f.fileno())
fd=os.open(c/'WORKFLOW-DISPATCH-INTENT.once.json',os.O_WRONLY|os.O_CREAT|os.O_EXCL,0o600)
with os.fdopen(fd,'w') as f:json.dump(intent,f,indent=2);f.flush();os.fsync(f.fileno())
d=os.open(c,os.O_RDONLY);os.fsync(d);os.close(d)
result=subprocess.run(['gh','workflow','run','aws-acceptance.yml','--repo',repo,'--ref','main','-f','reviewed_sha='+candidate],capture_output=True,timeout=30)
record={'at':datetime.now(timezone.utc).isoformat(),'exit':result.returncode,'automaticRedispatch':False,'inputs':{'reviewed_sha':candidate},'ref':'main','observedMain':main,'status':'HOLD_AMBIGUOUS_DISPATCH_NO_RETRY'}
if result.returncode:
 (c/'WORKFLOW-DISPATCH-RESULT.json').write_text(json.dumps(record,indent=2)+'\n');print(json.dumps(record));raise SystemExit(1)
ids=set(re.findall(r'https://github\.com/Hikyo-Org/Hikyo/actions/runs/([0-9]+)',result.stdout.decode()))
run=None
if len(ids)==1:run=api('actions/runs/'+next(iter(ids)))
else:
 for index in range(6):
  found=[x for x in api('actions/workflows/aws-acceptance.yml/runs?per_page=20')['workflow_runs'] if x['id'] not in known and x['event']=='workflow_dispatch' and x['head_branch']=='main' and x['actor']['login']=='Dunky13']
  if len(found)>1:break
  if len(found)==1:run=found[0];break
  if index<5:time.sleep(2)
if run is None:
 (c/'WORKFLOW-DISPATCH-RESULT.json').write_text(json.dumps(record,indent=2)+'\n');print(json.dumps(record));raise SystemExit(1)
assert run['run_attempt']==1 and run['head_branch']=='main' and run['event']=='workflow_dispatch' and run['repository']['full_name']==repo and run['actor']['login']=='Dunky13'
record.update({'runId':str(run['id']),'attempt':run['run_attempt'],'headSHA':run['head_sha'],'url':run['html_url'],'status':'ONE_RUN_IDENTIFIED_SEPARATE_PROTECTED_APPROVAL_PENDING','sourceCorrespondenceStillRequired':run['head_sha']!=main})
(c/'WORKFLOW-DISPATCH-RESULT.json').write_text(json.dumps(record,indent=2)+'\n');print(json.dumps(record))
```
