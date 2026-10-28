async(page,input)=>{
 'use strict';
 const action=/*ACTION*/,nativePins=/*PINS*/;let cdp,handle,before=null,after=null,detail=null;
 const flags={controlLeaseClaimed:false,exclusiveControlVerifiedByAdapter:false,writeAuthorityGranted:false,providerOutcomeProven:false,externalAuthenticityVerified:false};
 const parts=value=>{const raw=JSON.stringify(value,(_key,item)=>{if(typeof item==='string')for(const scalar of item){const code=scalar.codePointAt(0);if(code>=0xD800&&code<=0xDFFF)throw Error('invalid-public-unicode');}return item;});let part='',out=[];for(const scalar of raw){const code=scalar.codePointAt(0);if(code>=0xD800&&code<=0xDFFF)throw Error('invalid-public-unicode');if(part.length+scalar.length>2048){out.push(part);part='';}part+=scalar;}if(part)out.push(part);if(out.length>64||new TextEncoder().encode(raw).length>120000)throw Error('public-capture-bound');return out;};
 const result=(outcome,partialEffect)=>{let publicCaptureParts;try{publicCaptureParts=parts(detail);}catch{outcome='held';partialEffect='effect-uncertain-reconcile-before-any-replay';publicCaptureParts=['null'];}return {schema:'hikyo-158/new-key-cleanup-ui/v1',action,outcome,partialEffect,beforeJSON:JSON.stringify(before),afterJSON:JSON.stringify(after),publicCaptureParts,...flags};};
 const fail=()=>{throw Error('new-key-cleanup-held');};
 try{
  const decodeIntent=i=>{const {reviewedDocumentSHA256JSON,nativeSourcePinsJSON,...rest}=i;if(nativeSourcePinsJSON!==JSON.stringify(Object.fromEntries(Object.entries(nativePins).sort())))fail();if(typeof reviewedDocumentSHA256JSON!=='string'||!/^(?:null|"[a-f0-9]{64}")$/u.test(reviewedDocumentSHA256JSON))fail();return {...rest,nativeSourcePins:JSON.parse(nativeSourcePinsJSON),reviewedDocumentSHA256:JSON.parse(reviewedDocumentSHA256JSON)};};
  input={...input,binding:{intent:decodeIntent(input.binding.intent),ticket:(()=>{const {nativeSourcePinsJSON,...ticket}=input.binding.ticket;if(nativeSourcePinsJSON!==JSON.stringify(Object.fromEntries(Object.entries(nativePins).sort())))fail();return {...ticket,nativeSourcePins:JSON.parse(nativeSourcePinsJSON),requiredNativePreparation:decodeIntent(ticket.requiredNativePreparation)};})()}};
  const expected={...input.expected},m=input.preparation.manifest,base='https://eu-central-1.console.aws.amazon.com/kms/home?region=eu-central-1';
  const keys=['targetId','targetType','targetURL','browserContextId','mainFrameId','loaderId','frameURL','frameURLRelation'];
  const fields=['action','expected','binding','preparation','readbackManifest','choice','slot'];
  if(!['inspect','set-seven','confirm','install','arm','schedule','status','readback-install','navigate','readback-collect'].includes(action)||action!=='navigate'&&input.choice!=='none'||action!=='readback-collect'&&input.slot!=='none')fail();
  if(!input||Object.keys(input).sort().join('|')!==fields.sort().join('|')||Object.keys(expected).sort().join('|')!==keys.sort().join('|')||expected.targetType!=='page'||keys.some(k=>typeof expected[k]!=='string'||!expected[k])||!expected.targetURL.startsWith(base+'#'))fail();
  const id=input.binding.intent.request.KeyId,old=['10908b8d-fe54-40e3-9210-a3eefea709b6','990c479f-d673-4bc9-8bca-7b9b86f4b205','fa374267-a456-4e79-b5b1-6cdf356b7d0e','bfa44dc1-ee8a-43b8-aec7-d8a7fbb38db1'];
  const allowedReadURLs=[base+'#/kms/home',base+'#/kms/keys',...[id,...old].flatMap(key=>['','/keyPolicy','/keyRotation','/aliases','/cryptographicConfiguration'].map(tab=>base+'#/kms/keys/'+key+tab))];
  const schedule=base+'#/kms/keys/scheduleDeletion/'+id;
  const allowed=['readback-install','readback-collect','navigate'].includes(action)?allowedReadURLs:action==='status'?[schedule,...allowedReadURLs]:[schedule];if(!allowed.includes(expected.targetURL))fail();
  cdp=await page.context().newCDPSession(page);
  async function identity(allowRouteChange=false){
   const {targetInfo:t}=await cdp.send('Target.getTargetInfo'),{frameTree:{frame:f}}=await cdp.send('Page.getFrameTree');
   const relation=f.url===t.url?'same-url':f.url===t.url.split('#')[0]?'target-url-without-fragment':'unrelated';
   const actual={targetId:t.targetId,targetType:t.type,targetURL:t.url,browserContextId:t.browserContextId,mainFrameId:f.id,loaderId:f.loaderId,frameURL:f.url,frameURLRelation:relation};
   const urls=allowRouteChange?(action==='schedule'?[schedule,...allowedReadURLs]:allowedReadURLs):[expected.targetURL];
   if(JSON.stringify(actual).length>2048||f.parentId||relation==='unrelated'||!urls.includes(t.url)||['targetId','targetType','browserContextId','mainFrameId','loaderId'].some(k=>actual[k]!==expected[k])||!allowRouteChange&&keys.some(k=>actual[k]!==expected[k]))fail();return actual;
  }
  before=await identity();
  handle=await page.evaluateHandle(({url,window})=>{const checkSharedWindow=/*WINDOW*/;const originalDocument=document;if(location.href!==url)throw Error('held');checkSharedWindow(globalThis,window);return originalDocument;},{url:expected.targetURL,window:{notBeforeMS:m.notBeforeMS,expiresAtMS:m.expiresAtMS}});
  const callback=async(originalDocument,{a,action})=>{
   const checkSharedWindow=/*WINDOW*/,cleanupDOM=/*DOM*/,cleanupBinding=/*BINDING*/,cleanupStep=/*STEP*/,nativePins=/*PINS*/;
   const installGuard=/*GUARD*/,installReader=/*READER*/;
   return cleanupStep(globalThis,originalDocument,a,action,checkSharedWindow,cleanupDOM,cleanupBinding,nativePins,installGuard,installReader);
  };
  const a={...input,allowedReadURLs};
  async function step(which,deadlineMS){await identity(which==='poll'||action==='navigate');return handle.evaluate(callback,{a:{...a,deadlineMS},action:which});}
  detail=await step(action);
  if(action==='schedule'){
   const deadlineMS=detail.deadlineMS;if(!Number.isSafeInteger(deadlineMS))fail();
   for(let attempt=0;!detail.ready&&attempt<20;attempt++){await page.waitForTimeout(100);detail=await step('poll',deadlineMS);}
   if(!detail.ready)fail();a.deadlineMS=deadlineMS;
  }
  after=await identity(['schedule','navigate'].includes(action));await cdp.detach();cdp=null;
  // Completion stays in the original Document's execution context, after detach.
  const validation=await handle.evaluate(async(originalDocument,{url,window,deadlineMS})=>{const checkSharedWindow=/*WINDOW*/;if(globalThis.document!==originalDocument||location.href!==url)throw Error('held');const now=checkSharedWindow(globalThis,window);if(deadlineMS!==undefined&&now>=deadlineMS)throw Error('held');return true;},{url:after.targetURL,window:{notBeforeMS:m.notBeforeMS,expiresAtMS:m.expiresAtMS},deadlineMS:a.deadlineMS});
  if(validation!==true)fail();return result('captured',action==='schedule'?'native-schedule-may-have-forwarded':['set-seven','confirm'].includes(action)?'local-public-input-may-have-changed':action==='navigate'?'native-navigation-may-have-started':'observer-state-may-have-changed');
 }catch{return result('held','effect-uncertain-reconcile-before-any-replay');}finally{
  // A cleanup failure rejects the whole original command; partial capture remains caller custody.
  let failed=false;if(cdp){try{await cdp.detach();}catch{failed=true;}}if(handle){try{await handle.dispose();}catch{failed=true;}}if(failed)throw Error('new-key-cleanup-cleanup-held-reconcile-before-replay');
 }
}
