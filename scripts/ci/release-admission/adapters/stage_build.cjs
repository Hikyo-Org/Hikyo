'use strict';
// Offline generator, explicit invocation only; writes this NEW source folder.
const fs=require('node:fs'),path=require('node:path'),crypto=require('node:crypto');
const {checkSharedWindow}=require('./shared-window.cjs');
const {createGuardedDocument}=require('./guarded-document.cjs');
const {createNativeStage}=require('./stage-kernel.cjs');
const root=__dirname,sha=b=>crypto.createHash('sha256').update(b).digest('hex');
const lit=value=>({kind:'literal',value}),obj=fields=>({kind:'object',fields}),en=values=>({kind:'enum',values});
const integer=(min=0,max=Number.MAX_SAFE_INTEGER)=>({kind:'integer',min,max}),array=(items,maxItems)=>({kind:'array',items,maxItems});
const text=(maxLength=2048,pattern='^[\\s\\S]*$')=>({kind:'pattern',pattern,maxLength}),hash=text(64,'^[a-f0-9]{64}$');
const F=lit(false),N=lit(null),CREATE='https://eu-central-1.console.aws.amazon.com/kms/home?region=eu-central-1#/kms/keys/create';
const nativeObserver=fs.readFileSync(path.join(root,'native-observer.source-only.js'),'utf8');
if(sha(nativeObserver)!=='5a68216b0e07b7fe207b1f361f2ab7192d3e28e2c13ae1adee8b20211e398700')throw Error('frozen-observer-drift');
const sourcePins=JSON.parse(fs.readFileSync(path.join(root,'STAGE-SOURCE-PINS.json'),'utf8'));
const ident=obj({...Object.fromEntries(['targetId','browserContextId','mainFrameId','loaderId'].map(k=>[k,text(256,'^[A-Za-z0-9._:-]+$')])),targetType:lit('page'),targetURL:lit(CREATE),frameURL:en([CREATE,CREATE.split('#')[0]]),frameURLRelation:en(['same-url','target-url-without-fragment'])});
const window=obj({notBeforeMS:integer(),expiresAtMS:integer()});
const reads=['TrentService.ListKeys','TrentService.ListKeysForService','TrentService.ListAliases','TrentService.DescribeKey','TrentService.GetKeyPolicy','TrentService.GetKeyRotationStatus','TrentService.ListResourceTags'];
const guard=obj({schema:lit('hikyo-158/native-kms-xhr-write-status/v1'),phase:en(['observing','armed']),claimConsumed:F,nativeSendInvocations:lit(0),blocked:integer(),activeReads:lit(0),successfulReadProbes:integer(1),lastReadTarget:en(reads),responseStatus:N,reason:N,responseEndpointMatched:N,intentSHA256:hash,wireBodySHA256:hash,endpoint:lit('https://kms.eu-central-1.amazonaws.com/'),durabilityVerifiedByPage:F,ownerIdentityVerifiedByPage:F,nativeSendCounterScope:lit('CreateKey-only-excludes-metadata-reads'),loadedSourceBytesVerifiedByPage:F,redirectErrorSupported:F,physicalHTTPHopBoundEstablished:F,coversOtherRealmsOrCapturedMethods:F,cleanup:lit('close-controlled-tab')});
const observer=obj({schema:lit('hikyo-158/next-key-observer-status/v1'),mode:lit('create'),readySlots:array(text(),0),pendingCaptures:lit(0),error:N,providerAPIsCalledByObserver:F,ownerAuthenticatedByObserver:F,publicSourceAuthenticatedByObserver:F,writeAuthorityGrantedByObserver:F,coversOtherRealmsOrCapturedMethods:F,physicalHTTPHopBoundEstablished:F,cleanup:lit('full-navigation-controlled-tab-preserves-session')});
const manifest=obj({schema:lit('hikyo-158/next-key-observer-manifest/v1'),mode:lit('create'),account:lit('099256073061'),region:lit('eu-central-1'),endpoint:lit('https://kms.eu-central-1.amazonaws.com/'),publicMainSHA256:lit('20a50d3f9e11cdae453b0f764689073f2adc6c644e6ac2a08660be5b1b2227a2'),preparationSHA256:hash,ownerContextSHA256:hash,intentSHA256:hash,claimId:lit('next-owner-CreateKey'),wireBodySHA256:hash,wireBodyBytes:integer(2,32768),notBeforeMS:integer(),expiresAtMS:integer(),newKeyARN:N,createReceiptSHA256:N});
const guardManifest=obj({schema:lit('hikyo-158/native-kms-xhr-write-manifest/v1'),endpoint:manifest.fields.endpoint,wireBodySHA256:hash,wireBodyBytes:integer(2,32768),intentSHA256:hash,publicMainSHA256:manifest.fields.publicMainSHA256,expiresAtMS:integer(),timeoutMS:lit(20000),redirectLimitationReviewed:lit(true)});
const descriptor=obj({attemptTrustBindingSHA256:hash,schema:lit('hikyo-158/native-create-stage-preflight/v1'),phase:en(['arm','finish']),issuedAtMS:integer(),effectBeforeMS:integer(),sharedWindow:window,physicalWindow:window,expectedIdentity:ident,sourcePins:obj(Object.fromEntries(Object.entries(sourcePins).map(([k,v])=>[k,lit(v)]))),
 ...Object.fromEntries(['holderContextSHA256','readyRawSHA256','lockWindowSHA256','holderIndexRawSHA256','preparationSHA256','claimSHA256','stateSHA256','intentSHA256','normalizedIntentSHA256','ticketSHA256','manifestBundleSHA256','statusOriginalRawSHA256','statusOriginalSHA256','statusContextSHA256','preflightSHA256'].map(k=>[k,hash])),statusOriginalSequence:integer(),guardManifest,observerManifest:manifest,expectedGuardStatus:guard,expectedObserverStatus:observer,requiredRemainingMS:lit(60000),maximumActionAgeMS:lit(2000),writeAuthorityGranted:F,externalAuthenticityVerified:F,runtimeReady:F});
const scripts=[];
function add(kind,effect,args,value,body){
 const code=`async (page,input)=>{
 'use strict';const fail=()=>{throw new Error('native-create-stage-held');};let cdp;
 try {
 if(typeof input.attemptTrustBindingSHA256!=='string'||!/^[a-f0-9]{64}$/u.test(input.attemptTrustBindingSHA256)||input.descriptor&&input.descriptor.attemptTrustBindingSHA256!==input.attemptTrustBindingSHA256)fail();
 const expected=input.expected,url=${JSON.stringify(CREATE)},window=input.window;
 if(expected.targetURL!==url)fail();
 cdp=await page.context().newCDPSession(page);
 const identity=async()=>{const {targetInfo:t}=await cdp.send('Target.getTargetInfo');const {frameTree:{frame:f}}=await cdp.send('Page.getFrameTree');
 const value={targetId:t.targetId,targetType:t.type,targetURL:t.url,browserContextId:t.browserContextId,mainFrameId:f.id,loaderId:f.loaderId,frameURL:f.url,frameURLRelation:f.url===t.url?'same-url':t.url.includes('#')&&f.url===t.url.split('#')[0]?'target-url-without-fragment':'unrelated'};
 if(f.parentId||Object.keys(expected).length!==8||Object.keys(value).some(k=>value[k]!==expected[k]))fail();return value;};
 const before=await identity();let captured;
${body}
 const after=await identity();await cdp.detach();cdp=null;
 await page.evaluate(({window,descriptor})=>{const s=globalThis.__hikyo158NativeCreateStageV1;if(!s)throw Error('native-create-stage-held');s.gate();if(descriptor)s.time(descriptor);},{window,descriptor:input.descriptor||null});
 return {schema:'hikyo-158/native-create-stage-readout/v1',kind:${JSON.stringify(kind)},phase:input.phase||'install',before,after,readAtMS:captured.readAtMS,value:captured.value,controlLeaseClaimed:false,exclusiveControlVerifiedByAdapter:false,writeAuthorityGranted:false,providerOutcomeProven:false};
 }catch{throw new Error('native-create-stage-held');}finally{if(cdp)await cdp.detach();}
}\n`;
 if(Buffer.byteLength(code)>65536)throw Error('script-size');const id='native-create-'+kind+'-v1',file='reviewed-scripts/'+id+'.js';fs.mkdirSync(path.join(root,'reviewed-scripts'),{recursive:true});fs.writeFileSync(path.join(root,file),code);
 scripts.push({id,file,sha256:sha(code),effect,arguments:obj({attemptTrustBindingSHA256:hash,expected:ident,window,...args}),result:obj({schema:lit('hikyo-158/native-create-stage-readout/v1'),kind:lit(kind),phase:kind==='install'?lit('install'):kind==='inspect'?lit('inspect'):en(['arm','finish']),before:ident,after:ident,readAtMS:integer(),value,controlLeaseClaimed:F,exclusiveControlVerifiedByAdapter:F,writeAuthorityGranted:F,providerOutcomeProven:F})});
}
add('install','mutation',{manifest,approvedBodyChunks:array(text(),32)},obj({observer,installedBeforeGuard:lit(true)}),`
 captured=await page.evaluate(async ({window,manifest,chunks,attemptTrustBindingSHA256})=>{
 ${checkSharedWindow.toString()}
 ${createGuardedDocument.toString()}
 ${createNativeStage.toString()}
 const stage=createNativeStage(globalThis,window,manifest,${JSON.stringify(sourcePins)},checkSharedWindow,attemptTrustBindingSHA256);stage.gate();
 if(window.notBeforeMS!==manifest.notBeforeMS||window.expiresAtMS!==manifest.expiresAtMS||globalThis.Object.hasOwn(globalThis,'__hikyo158NativeCreateStageV1')||globalThis.Object.hasOwn(globalThis,'__hikyo158NextKeyCustodyObserverV1')||globalThis.Object.hasOwn(globalThis,'__hikyo158NativeKMSXHRWriteGuardV1'))throw Error('native-create-stage-held');
 globalThis.Object.defineProperty(globalThis,'__hikyo158NativeCreateStageV1',{value:stage,writable:false,configurable:false});
 let value;
 {
 const Object=stage.guardObject();
 const document=createGuardedDocument(globalThis,[${JSON.stringify(CREATE)}],window);
 ${nativeObserver}
 value=await installHikyo158NextKeyCustodyObserverV1({manifest,approvedCreateBody:chunks.join('')});
 }
 stage.installed();stage.gate();
 return {readAtMS:Date.now(),value:{observer:value,installedBeforeGuard:true}};
 },{window,manifest:input.manifest,chunks:input.approvedBodyChunks,attemptTrustBindingSHA256:input.attemptTrustBindingSHA256});`);
add('status','read',{phase:en(['arm','finish']),manifest},obj({guard,observer}),`
 captured=await page.evaluate(({window,manifest,phase})=>{const s=globalThis.__hikyo158NativeCreateStageV1;if(!s)throw Error('native-create-stage-held');s.bind(window,manifest);const value=s.read(phase);return {readAtMS:Date.now(),value};},{window,manifest:input.manifest,phase:input.phase});`);
// The full G statuses have nullable/terminal fields. Preserve their exact JSON
// in a bounded codec, without shortening them or weakening the arm schema.
add('inspect','read',{phase:lit('inspect'),manifest},obj({schema:lit('hikyo-158/public-json-chunks/v1'),chunks:array(text(),32)}),`
 captured=await page.evaluate(({window,manifest})=>{const s=globalThis.__hikyo158NativeCreateStageV1;if(!s)throw Error('native-create-stage-held');s.bind(window,manifest);const raw=JSON.stringify(s.inspect());if(new TextEncoder().encode(raw).length>32768)throw Error('native-create-stage-held');const chunks=[];for(let i=0;i<raw.length;i+=1024)chunks.push(raw.slice(i,i+1024));return {readAtMS:Date.now(),value:{schema:'hikyo-158/public-json-chunks/v1',chunks}};},{window,manifest:input.manifest});`);
add('arm','mutation',{phase:lit('arm'),descriptor},obj({guard,armInvokedOnce:lit(true),providerOutcomeProven:F}),`
 captured=await page.evaluate(({window,descriptor,expected})=>{const s=globalThis.__hikyo158NativeCreateStageV1;if(!s)throw Error('native-create-stage-held');s.bind(window,descriptor.observerManifest);const guard=s.arm(descriptor,expected);return {readAtMS:Date.now(),value:{guard,armInvokedOnce:true,providerOutcomeProven:false}};},{window,descriptor:input.descriptor,expected});`);
add('finish','mutation',{phase:lit('finish'),descriptor},obj({clickDispatched:lit(true),providerOutcomeProven:F}),`
 captured=await page.evaluate(({window,descriptor,expected})=>{const s=globalThis.__hikyo158NativeCreateStageV1;if(!s)throw Error('native-create-stage-held');s.bind(window,descriptor.observerManifest);const value=s.finish(descriptor,expected);return {readAtMS:Date.now(),value};},{window,descriptor:input.descriptor,expected});`);
fs.writeFileSync(path.join(root,'stage-registry.json'),JSON.stringify({schema:'hikyo-158/reviewed-owner-scripts/v1',scripts},null,2)+'\n');
fs.writeFileSync(path.join(root,'STAGE-SOURCE-PINS.json'),JSON.stringify(sourcePins,null,2)+'\n');
fs.writeFileSync(path.join(root,'DESCRIPTOR-CONTRACT.json'),JSON.stringify(descriptor,null,2)+'\n');
