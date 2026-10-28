function installHikyo158NextFixtureIAMGuardV1(manifest) {
 'use strict';
 // SOURCE ONLY. Exactly three operations; no actual authority, installation or provider use.
 const marker='__hikyo158NextFixtureIAMGuardV1';
 const setupARN='arn:aws:iam::099256073061:policy/hikyo-158-setup-policy', setupID='ANPAROHA7PNSTPQUQEUTR';
 const boundaryARN='arn:aws:iam::099256073061:policy/hikyo-158-role-boundary', userName='hikyo-158-setup';
 const keys=['schema','operation','targetARN','expectedPolicyId','currentDefaultVersionId','targetVersionId','wireBodySHA256','wireBodyBytes',
 'intentSHA256','reviewedPreparationSHA256','publicMainSHA256','publicVendorsSHA256','expiresAtMS','timeoutMS','bodyPrefix','writeRoute','readProbeRoute',
 'ownerFullProofSHA256','proofObservedAtMS','reviewedDocumentSHA256','retirementProofSHA256','newBoundaryCustodySHA256','analysisWires'];
 function fail(code){throw new Error('hikyo158-next-fixture-iam:'+code);}
 const shaLike=v=>typeof v==='string'&&/^[a-f0-9]{64}$/.test(v);
 function data(object,fields){
  if(!object||typeof object!=='object'||Array.isArray(object)||Reflect.ownKeys(object).length!==fields.length)fail('fields');
  const ds=Object.getOwnPropertyDescriptors(object);
  if(!fields.every(k=>Object.hasOwn(ds,k)&&Object.hasOwn(ds[k],'value')&&ds[k].enumerable))fail('data-fields');
 }
 data(manifest,keys);
 const create=manifest.operation==='CreatePolicy',activate=manifest.operation==='CreatePolicyVersion',remove=manifest.operation==='DeletePolicy';
 if(manifest.schema!=='hikyo-158/next-fixture-iam-write-manifest/v1'||!create&&!activate&&!remove)fail('operation');
 const policyARN=activate?setupARN:boundaryARN;
 const newID=id=>typeof id==='string'&&/^ANPA[A-Z0-9]{17}$/.test(id)&&![setupID,'ANPAROHA7PNS5QVDJKPIN'].includes(id);
 if(manifest.targetARN!==policyARN||manifest.expectedPolicyId!==(activate?setupID:create?null:manifest.expectedPolicyId)||
  remove&&!newID(manifest.expectedPolicyId)||manifest.currentDefaultVersionId!==(activate?'v5':remove?'v1':null)||
  manifest.targetVersionId!==(activate?'v6':create?'v1':null))fail('target-identity');
 for(const k of ['wireBodySHA256','intentSHA256','reviewedPreparationSHA256','publicMainSHA256','publicVendorsSHA256','ownerFullProofSHA256','reviewedDocumentSHA256'])if(!shaLike(manifest[k]))fail('manifest-hash');
 if(remove?(!shaLike(manifest.retirementProofSHA256)||!shaLike(manifest.newBoundaryCustodySHA256)):(manifest.retirementProofSHA256!==null||manifest.newBoundaryCustodySHA256!==null))fail('retirement-custody');
 if(manifest.publicMainSHA256!=='85a9aed5a11dc8d019b95f399a1502b1c8e19affee5c933fc09623d0d359d0f9'||
 manifest.publicVendorsSHA256!=='e4cdd4a45e6d93e7d1e4c512cfa82eaf02bbb040280c93b1b0f20958118b4ba9')fail('historical-public-source-pins');
 const fresh=()=>Number.isSafeInteger(manifest.proofObservedAtMS)&&manifest.proofObservedAtMS<=Date.now()&&manifest.proofObservedAtMS>=Date.now()-300000;
 if(!fresh()||!Number.isSafeInteger(manifest.expiresAtMS)||manifest.expiresAtMS<=Date.now()||manifest.expiresAtMS>Date.now()+300000||
 !Number.isSafeInteger(manifest.timeoutMS)||manifest.timeoutMS<1000||manifest.timeoutMS>20000||
 !Number.isSafeInteger(manifest.wireBodyBytes)||manifest.wireBodyBytes<32||manifest.wireBodyBytes>(remove?512:32768))fail('bounds');
 if(manifest.bodyPrefix!=='Action='+manifest.operation+'&Version=2010-05-08&')fail('serializer-prefix');
 if(location.origin!=='https://us-east-1.console.aws.amazon.com'||location.pathname!=='/iam/home'||!['?region=eu-central-1','?region=us-east-1',''].includes(location.search))fail('document-origin');
 if([marker,'__hikyo158NextSetupVersionGuardV1','__hikyo158NativeIAMWriteGuardV1','__hikyo158RecoveryManagedVersionGuardV1','__hikyo158BoundaryDeleteGuardV1',
 '__hikyo158RecoveryMetadataObserverV1','__hikyo158RecoveryMetadataObserverV2'].some(k=>Object.hasOwn(globalThis,k)))fail('already-installed');
 if(typeof Request!=='function'||typeof fetch!=='function'||typeof XMLHttpRequest!=='function'||typeof AbortController!=='function'||typeof TextDecoder!=='function'||typeof TextEncoder!=='function'||!crypto?.subtle)fail('platform');
 const encodings=[policyARN.replace('/','%2F'),encodeURIComponent(policyARN)];
 const policyRoutes=encodings.flatMap(a=>['','?section=permissions','?section=versions','?section=entities'].map(s=>'#/policies/details/'+a+s));
 const editRoutes=encodings.flatMap(a=>['','/0','/1'].flatMap(s=>['','/'].map(t=>'#/policies/details/'+a+'/edit/v5'+s+t)));
 const writeRoutes=activate?editRoutes:create?['#/policies/create']:policyRoutes;
 const userRoutes=['#/users/details/'+userName,'#/users/details/'+userName+'?section=permissions'];
 if(!writeRoutes.includes(manifest.writeRoute)||!userRoutes.includes(manifest.readProbeRoute)||location.hash!==manifest.writeRoute)fail('route');
 function accountBanner(){return Array.from(document.querySelectorAll('header, nav, #awsc-nav-account-menu-button, [data-testid="awsc-nav-account-menu-button"]')).some(n=>[n.textContent,n.getAttribute('aria-label')].some(v=>typeof v==='string'&&/(^|[^0-9])(?:099256073061|0992-5607-3061)([^0-9]|$)/.test(v)));}
 if(!accountBanner())fail('account-banner');
 const main='https://a.b.cdn.console.awsstatic.com/a/v1/EPYETUDGRYAX3B5DPL5Z2CIJYNSNNCGH7C7BCGW564I7W6F6ZYAQ/main.js';
 if(![main,main.replace('/main.js','/vendors.js')].every(src=>Array.from(document.scripts).some(s=>s.src===src))||!document.querySelector('#app')||!document.querySelector('[data-testid="iam-v2-vite"]'))fail('loaded-module');
 if(!Array.isArray(manifest.analysisWires)||manifest.analysisWires.length!==(remove?0:2)||Reflect.ownKeys(manifest.analysisWires).length!==manifest.analysisWires.length+1)fail('analysis-array');
 const analysis=new Map();
 for(let i=0;i<manifest.analysisWires.length;i++){
  const d=Object.getOwnPropertyDescriptor(manifest.analysisWires,String(i));if(!d||!Object.hasOwn(d,'value'))fail('analysis-accessor');const row=d.value;
  data(row,['path','wireBodyBytes','wireBodySHA256']);
  if(!['/prod/validate','/prod/policySummary'].includes(row.path)||analysis.has(row.path)||!shaLike(row.wireBodySHA256)||!Number.isSafeInteger(row.wireBodyBytes)||row.wireBodyBytes<2||row.wireBodyBytes>32768)fail('analysis-binding');
  analysis.set(row.path,Object.freeze({...row}));
 }
 const approved=Object.freeze({...manifest}),installRoute=manifest.writeRoute,nativeFetch=globalThis.fetch;
 const state={phase:'observing',claimConsumed:false,forwards:0,blocked:0,activeReads:0,readReservations:0,observedOwnUserReads:0,
 preludeReserved:0,preludeSettled:0,preludeActions:[],analysisForwards:0,responseStatus:null,reason:null};
 const analysisCounts=new Map(),analysisSuccesses=new Set();
 const freshProof=()=>Date.now()>=approved.proofObservedAtMS&&Date.now()-approved.proofObservedAtMS<=300000;
 const bound=()=>Date.now()<approved.expiresAtMS&&freshProof()&&location.hash===installRoute&&accountBanner();
 const hex=bytes=>Array.from(new Uint8Array(bytes),v=>v.toString(16).padStart(2,'0')).join('');
 function status(){return Object.freeze({schema:'hikyo-158/next-fixture-iam-write-status/v1',...state,preludeActions:Object.freeze([...state.preludeActions]),
 operation:approved.operation,targetARN:policyARN,expectedPolicyId:approved.expectedPolicyId,currentDefaultVersionId:approved.currentDefaultVersionId,targetVersionId:approved.targetVersionId,
 ownerFullProofSHA256:approved.ownerFullProofSHA256,reviewedDocumentSHA256:approved.reviewedDocumentSHA256,proofObservedAtMS:approved.proofObservedAtMS,
 retirementProofSHA256:approved.retirementProofSHA256,newBoundaryCustodySHA256:approved.newBoundaryCustodySHA256,intentSHA256:approved.intentSHA256,
 reviewedPreparationSHA256:approved.reviewedPreparationSHA256,wireBodySHA256:approved.wireBodySHA256,armedUntilMS:approved.expiresAtMS,
 successfulAnalysisPaths:Object.freeze([...analysisSuccesses]),externalProofAuthenticityVerifiedByPage:false,policyIdentityAndUsageVerifiedByPage:false,
 ownerIdentityVerifiedByPage:false,loadedSourceBytesVerifiedByPage:false,durabilityVerifiedByPage:false,coversCustomFetchOrOtherRealms:false,
 nativeXHRSupported:false,publicAssetLoadingSupported:false,cleanup:'full-navigation-controlled-tab-preserves-session'});}
 function hold(reason){state.phase='held';state.reason=reason;}
 function blocked(code){state.blocked++;if(['armed','armed-prelude','checking'].includes(state.phase)){state.claimConsumed=true;hold('rejected-attempt-needs-reconciliation');}fail(code);}
 function destination(value){const url=new URL(value,location.href);if(url.username||url.password||url.hash||url.href.length>4096)fail('request-url-shape');return url;}
  async function boundedBody(request, limit, signal) {
    const reader = request.clone().body?.getReader();
    if (!reader) fail('missing-body');
    let count = 0;
    const chunks = [];
    function cancel() { void reader.cancel().catch(() => {}); }
    signal.addEventListener('abort', cancel, { once: true });
    try {
      while (true) {
        if (signal.aborted) fail('body-aborted');
        const result = await reader.read();
        if (signal.aborted) fail('body-aborted');
        if (result.done) break;
        count += result.value.byteLength;
        if (count > limit) fail('body-size');
        chunks.push(result.value);
      }
      const bytes = new Uint8Array(count);
      let offset = 0;
      for (const chunk of chunks) { bytes.set(chunk, offset); offset += chunk.byteLength; }
      return { bytes, text: new TextDecoder('utf-8', { fatal: true }).decode(bytes) };
    } finally {
      signal.removeEventListener('abort', cancel);
      cancel();
    }
  }
  // Decode only public action/key selectors first. Unknown credential/password
  // field names refuse before decoding any associated values.
  function selectors(text, names) {
    const raw = text.split('&');
    const entries = [];
    const seen = new Set();
    for (const pair of raw) {
      const equal = pair.indexOf('=');
      if (equal < 1) fail('query-shape');
      const name = decodeURIComponent(pair.slice(0, equal).replace(/\+/g, ' '));
      if (!names.has(name) || seen.has(name)) fail('query-fields');
      seen.add(name); entries.push([name, pair.slice(equal + 1)]);
    }
    return new Map(entries.map(([name, value]) => [name, decodeURIComponent(value.replace(/\+/g, ' '))]));
  }

 function validateWrite(text){
  if(!text.startsWith(approved.bodyPrefix))fail('wire-prefix');
  const fields=selectors(text,new Set(remove?['Action','Version','PolicyArn']:activate?['Action','Version','PolicyArn','PolicyDocument','SetAsDefault']:['Action','Version','PolicyName','PolicyDocument','Description','Tags']));
  if(fields.get('Action')!==approved.operation||fields.get('Version')!=='2010-05-08')fail('write-action');
  const esc=v=>encodeURIComponent(v).replace(/[!'()*]/g,c=>'%'+c.charCodeAt(0).toString(16).toUpperCase());
  let exact;
  if(remove){if(fields.size!==3||fields.get('PolicyArn')!==boundaryARN)fail('delete-binding');exact=approved.bodyPrefix+'PolicyArn='+esc(boundaryARN);}
  else if(activate){if(fields.size!==5||fields.get('PolicyArn')!==setupARN||fields.get('SetAsDefault')!=='true'||!fields.get('PolicyDocument'))fail('activate-binding');
   exact=approved.bodyPrefix+'PolicyArn='+esc(setupARN)+'&PolicyDocument='+esc(fields.get('PolicyDocument'))+'&SetAsDefault=true';}
  else{if(fields.size!==6||fields.get('PolicyName')!=='hikyo-158-role-boundary'||fields.get('Description')!==''||fields.get('Tags')!==''||!fields.get('PolicyDocument'))fail('create-binding');
   exact=approved.bodyPrefix+'PolicyName=hikyo-158-role-boundary&PolicyDocument='+esc(fields.get('PolicyDocument'))+'&Description=&Tags=';}
  if(text!==exact)fail('exact-wire-order');
  if(!remove&&fields.get('PolicyDocument').length>12000)fail('document-size');
  return remove?null:fields.get('PolicyDocument');
 }
 function scopedRead(text){
  const fields=selectors(text,new Set(['Action','Version','UserName','PolicyArn','VersionId','MaxItems'])),action=fields.get('Action');
  if(fields.get('Version')!=='2010-05-08')fail('read-version');
  if(['GetUser','ListMFADevices','ListAttachedUserPolicies','ListUserPolicies','ListGroupsForUser','ListAccessKeys'].includes(action)){
   if(fields.size!==3||fields.get('UserName')!==userName||!userRoutes.includes(location.hash))fail('own-user-read');
  }else if(!create&&['GetPolicy','ListPolicyVersions','GetPolicyVersion'].includes(action)){
   if(![...policyRoutes,...(activate?editRoutes:[])].includes(location.hash)||fields.get('PolicyArn')!==policyARN||
    fields.size!==(action==='GetPolicyVersion'||action==='ListPolicyVersions'&&fields.has('MaxItems')?4:3)||
    fields.has('MaxItems')&&(action!=='ListPolicyVersions'||fields.get('MaxItems')!=='1000')||
    action==='GetPolicyVersion'&&!(activate?['v2','v3','v4','v5']:['v1']).includes(fields.get('VersionId')))fail('policy-read');
  }else fail('unapproved-read');return action;
 }
 async function guardedFetch(request,init){
  if(!(request instanceof Request)||init!==undefined)return blocked('request-form');
  let url;try{url=destination(request.url);}catch{return blocked('request-url');}
  const iam=url.origin==='https://iam.amazonaws.com'&&url.pathname==='/'&&!url.search;
  const p3=!remove&&url.origin==='https://global.iam-cx.console.api.aws'&&!url.search&&analysis.has(url.pathname);
  if(!iam&&!p3)return blocked('endpoint');
  if(!['observing','armed-prelude','armed'].includes(state.phase))return blocked('closed');
  if(request.method!=='POST'||request.headers.get('content-type')!==(p3?'application/json':'application/x-www-form-urlencoded')||request.headers.has('x-amz-target'))return blocked('protocol');
  if(p3&&(state.phase!=='observing'||!bound()))return blocked('analysis-phase-route');
  const prelude=iam&&state.phase==='armed-prelude',mutation=iam&&state.phase==='armed';
  if(mutation){state.claimConsumed=true;state.phase='checking';if(state.activeReads!==0||!bound()){hold('mutation-not-idle-or-bound');fail('mutation-not-idle-or-bound');}}
  else{
   if(state.readReservations>=48)return blocked('read-cap');state.readReservations++;
   if(prelude){if(state.preludeReserved>=2||!bound())return blocked('prelude-cap');state.preludeReserved++;}
   if(p3){const count=analysisCounts.get(url.pathname)??0;if(count>=8)return blocked('analysis-cap');analysisCounts.set(url.pathname,count+1);}
   state.activeReads++;
  }
  const controller=new AbortController(),abort=()=>controller.abort();request.signal.addEventListener('abort',abort,{once:true});if(request.signal.aborted)controller.abort();
  const deadline=Math.min(Date.now()+approved.timeoutMS,approved.expiresAtMS),startRoute=location.hash;let timer;
  const timeout=new Promise((resolve,reject)=>{timer=setTimeout(()=>{if(mutation||prelude)hold('deadline');controller.abort();reject(new Error('hikyo158-next-fixture-iam:deadline'));},Math.max(1,deadline-Date.now()));});
  async function perform(){try{
   const binding=p3?analysis.get(url.pathname):null;
   const body=await boundedBody(request,mutation?approved.wireBodyBytes:p3?binding.wireBodyBytes:2048,controller.signal);
   if(controller.signal.aborted||Date.now()>=deadline)fail('deadline');
   if(p3){
    if(body.bytes.byteLength!==binding.wireBodyBytes||hex(await crypto.subtle.digest('SHA-256',body.bytes))!==binding.wireBodySHA256||
     controller.signal.aborted||Date.now()>=deadline||state.phase!=='observing'||!bound())fail('analysis-binding');
    state.analysisForwards++;const response=await nativeFetch(new Request(request,{redirect:'error',signal:controller.signal}));
    if(response.status===200&&!controller.signal.aborted&&Date.now()<deadline&&state.phase==='observing'&&bound())analysisSuccesses.add(url.pathname);
    return response;
   }
   if(prelude){
    const fields=selectors(body.text,new Set(['Action','Version','PolicyArn'])),action=fields.get('Action');
    if(fields.size!==3||fields.get('Version')!=='2010-05-08'||fields.get('PolicyArn')!==boundaryARN||!['ListEntitiesForPolicy','ListPolicyVersions'].includes(action)||
     body.text!=='Action='+action+'&Version=2010-05-08&PolicyArn='+encodeURIComponent(boundaryARN)||state.preludeActions.includes(action)||state.phase!=='armed-prelude'||!bound())fail('prelude-binding');
    state.preludeActions.push(action);const response=await nativeFetch(new Request(request,{redirect:'error',signal:controller.signal}));
    if(response.status!==200||controller.signal.aborted||Date.now()>=deadline||state.phase!=='armed-prelude'||!bound())fail('prelude-response');
    state.preludeSettled++;if(state.preludeSettled===2&&state.preludeActions.length===2)state.phase='armed';return response;
   }
   if(!mutation){
    const action=scopedRead(body.text);if(state.phase!=='observing'||location.hash!==startRoute||Date.now()>=approved.expiresAtMS||!freshProof()||!accountBanner())fail('read-phase');
    const response=await nativeFetch(new Request(request,{redirect:'error',signal:controller.signal}));
    if(response.status===200&&!controller.signal.aborted&&Date.now()<deadline&&state.phase==='observing'&&location.hash===startRoute&&freshProof()&&accountBanner()&&action==='GetUser'&&userRoutes.includes(startRoute))state.observedOwnUserReads++;
    return response;
   }
   if(!bound()||body.bytes.byteLength!==approved.wireBodyBytes)fail('mutation-bound');const documentText=validateWrite(body.text);
   if(documentText!==null&&hex(await crypto.subtle.digest('SHA-256',new TextEncoder().encode(documentText)))!==approved.reviewedDocumentSHA256)fail('document-hash');
   if(hex(await crypto.subtle.digest('SHA-256',body.bytes))!==approved.wireBodySHA256)fail('wire-hash');
   if(controller.signal.aborted||Date.now()>=deadline||!bound()||state.phase!=='checking')fail('mutation-bound');
   state.phase='forwarded';state.forwards++;const response=await nativeFetch(new Request(request,{redirect:'error',signal:controller.signal}));
   if(controller.signal.aborted||state.phase!=='forwarded'||Date.now()>=deadline)fail('mutation-deadline');state.responseStatus=response.status;state.phase='settled';return response;
  }catch{if((mutation||prelude)&&state.phase!=='held')hold('consumed-attempt-needs-reconciliation');fail('held-operation');}}
  try{return await Promise.race([perform(),timeout]);}finally{clearTimeout(timer);request.signal.removeEventListener('abort',abort);if(!mutation)state.activeReads--;}
 }
 function arm(intent){if(state.phase!=='observing'||state.claimConsumed||state.activeReads!==0||state.observedOwnUserReads<1||intent!==approved.intentSHA256||!bound()||!remove&&analysisSuccesses.size!==2)fail('cannot-arm');state.phase=remove?'armed-prelude':'armed';return status();}
 const control=Object.freeze({status,arm});
 Object.defineProperty(XMLHttpRequest.prototype,'send',{configurable:false,writable:false,value:function(){return blocked('xhr-unsupported');}});
 Object.defineProperty(globalThis,'fetch',{configurable:false,writable:false,value:guardedFetch});
 Object.defineProperty(globalThis,marker,{configurable:false,writable:false,value:control});return status();
}
