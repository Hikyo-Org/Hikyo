async (page,input)=>{
 'use strict';const fail=()=>{throw new Error('native-create-stage-held');};let cdp;
 try {
 if(typeof input.attemptTrustBindingSHA256!=='string'||!/^[a-f0-9]{64}$/u.test(input.attemptTrustBindingSHA256)||input.descriptor&&input.descriptor.attemptTrustBindingSHA256!==input.attemptTrustBindingSHA256)fail();
 const expected=input.expected,url="https://eu-central-1.console.aws.amazon.com/kms/home?region=eu-central-1#/kms/keys/create",window=input.window;
 if(expected.targetURL!==url)fail();
 cdp=await page.context().newCDPSession(page);
 const identity=async()=>{const {targetInfo:t}=await cdp.send('Target.getTargetInfo');const {frameTree:{frame:f}}=await cdp.send('Page.getFrameTree');
 const value={targetId:t.targetId,targetType:t.type,targetURL:t.url,browserContextId:t.browserContextId,mainFrameId:f.id,loaderId:f.loaderId,frameURL:f.url,frameURLRelation:f.url===t.url?'same-url':t.url.includes('#')&&f.url===t.url.split('#')[0]?'target-url-without-fragment':'unrelated'};
 if(f.parentId||Object.keys(expected).length!==8||Object.keys(value).some(k=>value[k]!==expected[k]))fail();return value;};
 const before=await identity();let captured;

 captured=await page.evaluate(async ({window,manifest,chunks,attemptTrustBindingSHA256})=>{
 function checkSharedWindow(globalObject, sharedWindow, minimumRemainingMS = 0) {
  const now = globalObject.Date.now();
  if (!sharedWindow || Object.keys(sharedWindow).length !== 2 ||
      !Number.isSafeInteger(sharedWindow.notBeforeMS) || !Number.isSafeInteger(sharedWindow.expiresAtMS) ||
      !Number.isSafeInteger(now) || now < sharedWindow.notBeforeMS || now >= sharedWindow.expiresAtMS ||
      sharedWindow.expiresAtMS > sharedWindow.notBeforeMS + 300000 ||
      ![0, 60000, 120000].includes(minimumRemainingMS) || sharedWindow.expiresAtMS - now < minimumRemainingMS)
    throw new Error('aws-operator-held');
  return now;
}
 function createGuardedDocument(globalObject, allowedURLs, sharedWindow) {
  const nativeDocument = globalObject.document;
  const fail = () => { throw new Error('aws-operator-held'); };
  const one = new Set(['#nav-usernameMenu', '#app', '#c', '[data-testid="iam-v2-vite"]']);
  const many = 'header, nav, #awsc-nav-account-menu-button, [data-testid="awsc-nav-account-menu-button"]';
  function guard() {
    checkSharedWindow(globalObject, sharedWindow);
    // The URL check MUST precede every real document/node/collection access.
    if (!allowedURLs.includes(globalObject.location.href)) fail();
    if (globalObject.document !== nativeDocument) fail();
    const banner = Reflect.apply(nativeDocument.querySelector, nativeDocument, ['#nav-usernameMenu']);
    if (!banner || Reflect.apply(banner.getClientRects, banner, []).length === 0) fail();
    const text = banner.textContent;
    if (typeof text !== 'string' || text.length > 512 || !/(^|[^0-9])099256073061([^0-9]|$)/.test(text)) fail();
  }
  function node(value) {
    if (value === null) return null;
    const attribute = name => {
      guard();
      if (name !== 'aria-label') fail();
      return Reflect.apply(value.getAttribute, value, [name]);
    };
    // Returned nodes cannot expose real DOM objects through arbitrary members.
    return Object.freeze(Object.defineProperties(Object.create(null), {
      textContent: { enumerable: true, get() { guard(); return value.textContent; } },
      src: { enumerable: true, get() { guard(); return value.src; } },
      getAttribute: { enumerable: true, get() { guard(); return attribute; } }
    }));
  }
  function collection(value) {
    function* iterate() {
      guard();
      const iterator = Reflect.apply(value[Symbol.iterator], value, []);
      while (true) {
        guard();
        const part = Reflect.apply(iterator.next, iterator, []);
        if (part.done) return;
        yield node(part.value);
      }
    }
    return Object.freeze(Object.defineProperties(Object.create(null), {
      length: { enumerable: true, get() { guard(); return value.length; } },
      [Symbol.iterator]: { get() { guard(); return iterate; } }
    }));
  }
  const queryOne = selector => {
    guard();
    if (!one.has(selector)) fail();
    return node(Reflect.apply(nativeDocument.querySelector, nativeDocument, [selector]));
  };
  const queryMany = selector => {
    guard();
    if (selector !== many) fail();
    return collection(Reflect.apply(nativeDocument.querySelectorAll, nativeDocument, [selector]));
  };
  return Object.freeze(Object.defineProperties(Object.create(null), {
    querySelector: { enumerable: true, get() { guard(); return queryOne; } },
    querySelectorAll: { enumerable: true, get() { guard(); return queryMany; } },
    scripts: { enumerable: true, get() { guard(); return collection(nativeDocument.scripts); } }
  }));
}
 function createNativeStage(globalObject, window, manifest, pins, checkSharedWindow, attemptTrustBindingSHA256) {
  const nativeDocument=globalObject.document, nativeObject=globalObject.Object;
  window=nativeObject.freeze({...window});manifest=nativeObject.freeze({...manifest});pins=nativeObject.freeze({...pins});
  const fail=()=>{throw new Error('native-create-stage-held');};
  const URL='https://eu-central-1.console.aws.amazon.com/kms/home?region=eu-central-1#/kms/keys/create';
  const stages=['Configure key','Add labels','Define key administrative permissions','Define key usage permissions','Review'];
  const canonical=v=>Array.isArray(v)?'['+v.map(canonical).join(',')+']':v!==null&&typeof v==='object'?'{'+nativeObject.keys(v).sort().map(k=>JSON.stringify(k)+':'+canonical(v[k])).join(',')+'}':JSON.stringify(v);
  const same=(a,b)=>canonical(a)===canonical(b);
  const computedStyle=globalObject.getComputedStyle,nativeMatches=globalObject.Element?.prototype.matches;
  let held=false,installed=false,armAttempted=false,armed=false,finishAttempted=false;const attemptBinding=attemptTrustBindingSHA256;
  function pageWindow() {
    checkSharedWindow(globalObject,window,60000);
    if(globalObject.location.href!==URL||globalObject.document!==nativeDocument)fail();
  }
  function publicRead(read) {pageWindow();const value=read();pageWindow();return value;}
  function visible(node) {
    pageWindow();if(!node||typeof computedStyle!=='function'||typeof nativeMatches!=='function')return false;
    if(publicRead(()=>node.ownerDocument)!==nativeDocument||publicRead(()=>node.isConnected)!==true)return false;
    const rects=publicRead(()=>node.getClientRects());
    if(!Number.isSafeInteger(rects.length)||rects.length<1||rects.length>64)return false;
    let area=false;
    for(let i=0;i<rects.length;i++){const r=publicRead(()=>rects[i]);
      const width=publicRead(()=>r.width),height=publicRead(()=>r.height);
      if(!Number.isFinite(width)||!Number.isFinite(height))return false;if(width>0&&height>0)area=true;}
    if(!area)return false;
    // Inspect only this bounded ancestor chain and public rendering state.
    // No unrelated DOM/auth queries, broad traversal or synthetic visibility flag.
    for(let current=node,depth=0;current&&depth<64;depth++){
      if(publicRead(()=>current.ownerDocument)!==nativeDocument||publicRead(()=>current.isConnected)!==true||
         publicRead(()=>current.hidden)||publicRead(()=>current.inert)||
         publicRead(()=>current.getAttribute('hidden'))!==null||publicRead(()=>current.getAttribute('inert'))!==null||
         publicRead(()=>current.getAttribute('aria-hidden'))==='true'||publicRead(()=>current.getAttribute('aria-disabled'))==='true')return false;
      const style=publicRead(()=>Reflect.apply(computedStyle,globalObject,[current]));
      const display=publicRead(()=>style.display),visibility=publicRead(()=>style.visibility),
        contentVisibility=publicRead(()=>style.contentVisibility),opacity=publicRead(()=>style.opacity);
      if(typeof display!=='string'||display==='none'||visibility!=='visible'||!['visible','auto'].includes(contentVisibility)||
         typeof opacity!=='string'||!Number.isFinite(Number(opacity))||Number(opacity)<=0||Number(opacity)>1)return false;
      if(current===publicRead(()=>nativeDocument.documentElement))return true;
      current=publicRead(()=>current.parentElement);
    }
    return false;
  }
  function gate() {
    pageWindow();
    const banner=Reflect.apply(nativeDocument.querySelector,nativeDocument,['#nav-usernameMenu']);
    pageWindow();if(!visible(banner))fail();
    const text=publicRead(()=>banner.textContent);
    if(typeof text!=='string'||text.length>512||!/(^|[^0-9])099256073061([^0-9]|$)/.test(text))fail();
    checkSharedWindow(globalObject,window,60000);
    if(globalObject.location.href!==URL||globalObject.document!==nativeDocument)fail();
  }
  function api(marker,methods) {
    gate();const d=nativeObject.getOwnPropertyDescriptor(globalObject,marker);
    if(!d||!nativeObject.hasOwn(d,'value')||d.configurable||d.writable||!nativeObject.isFrozen(d.value))fail();
    for(const name of methods){const m=nativeObject.getOwnPropertyDescriptor(d.value,name);if(!m||!nativeObject.hasOwn(m,'value')||typeof m.value!=='function')fail();}
    return d.value;
  }
  function exact(value,keys) {if(!value||nativeObject.keys(value).sort().join('|')!==keys.slice().sort().join('|'))fail();}
  const guardKeys=['schema','phase','claimConsumed','nativeSendInvocations','blocked','activeReads','successfulReadProbes','lastReadTarget','responseStatus','reason','responseEndpointMatched','intentSHA256','wireBodySHA256','endpoint','durabilityVerifiedByPage','ownerIdentityVerifiedByPage','nativeSendCounterScope','loadedSourceBytesVerifiedByPage','redirectErrorSupported','physicalHTTPHopBoundEstablished','coversOtherRealmsOrCapturedMethods','cleanup'];
  const observerKeys=['schema','mode','readySlots','pendingCaptures','error','providerAPIsCalledByObserver','ownerAuthenticatedByObserver','publicSourceAuthenticatedByObserver','writeAuthorityGrantedByObserver','coversOtherRealmsOrCapturedMethods','physicalHTTPHopBoundEstablished','cleanup'];
  function statuses() {
    if(!installed||held)fail();
    const g=api('__hikyo158NativeKMSXHRWriteGuardV1',['status','arm']).status();
    const o=api('__hikyo158NextKeyCustodyObserverV1',['status','read']).status();
    exact(g,guardKeys);exact(o,observerKeys);gate();return {guard:g,observer:o};
  }
  function healthy(g,o,phase) {
    exact(g,guardKeys);exact(o,observerKeys);
    if(g.schema!=='hikyo-158/native-kms-xhr-write-status/v1'||g.phase!==phase||g.claimConsumed!==false||g.nativeSendInvocations!==0||g.activeReads!==0||
       !Number.isSafeInteger(g.blocked)||g.blocked<0||!Number.isSafeInteger(g.successfulReadProbes)||g.successfulReadProbes<1||
       !['TrentService.ListKeys','TrentService.ListKeysForService','TrentService.ListAliases','TrentService.DescribeKey','TrentService.GetKeyPolicy','TrentService.GetKeyRotationStatus','TrentService.ListResourceTags'].includes(g.lastReadTarget)||
       g.responseStatus!==null||g.reason!==null||g.responseEndpointMatched!==null||g.intentSHA256!==manifest.intentSHA256||g.wireBodySHA256!==manifest.wireBodySHA256||g.endpoint!==manifest.endpoint||
       g.nativeSendCounterScope!=='CreateKey-only-excludes-metadata-reads'||g.cleanup!=='close-controlled-tab'||
       ['durabilityVerifiedByPage','ownerIdentityVerifiedByPage','loadedSourceBytesVerifiedByPage','redirectErrorSupported','physicalHTTPHopBoundEstablished','coversOtherRealmsOrCapturedMethods'].some(k=>g[k]!==false))fail();
    if(o.schema!=='hikyo-158/next-key-observer-status/v1'||o.mode!=='create'||!Array.isArray(o.readySlots)||o.readySlots.length!==0||o.pendingCaptures!==0||o.error!==null||
       ['providerAPIsCalledByObserver','ownerAuthenticatedByObserver','publicSourceAuthenticatedByObserver','writeAuthorityGrantedByObserver','coversOtherRealmsOrCapturedMethods','physicalHTTPHopBoundEstablished'].some(k=>o[k]!==false)||o.cleanup!=='full-navigation-controlled-tab-preserves-session')fail();
  }
  function review() {
    gate();const nodes=Reflect.apply(nativeDocument.querySelectorAll,nativeDocument,['h1,h2,h3,h4,h5,h6,[role="heading"]']);
    if(nodes.length>16)fail();const found=[];
    for(const node of nodes){gate();if(!visible(node))continue;gate();const label=node.getAttribute('aria-label');gate();const text=label===null?node.textContent:label;
      if(typeof text!=='string'||text.length>128)fail();if(!visible(node))continue;if(stages.includes(text.trim()))found.push(text.trim());}
    gate();if(found.length!==1||found[0]!=='Review')fail();
  }
  function descriptor(d,phase,expected) {
    if(attemptBinding!==null&&attemptBinding!==d.attemptTrustBindingSHA256)fail();
    if(typeof d.attemptTrustBindingSHA256!=='string'||!/^[a-f0-9]{64}$/u.test(d.attemptTrustBindingSHA256))fail();
    gate();const now=globalObject.Date.now();
    if(typeof d.attemptTrustBindingSHA256!=='string'||!/^[a-f0-9]{64}$/u.test(d.attemptTrustBindingSHA256)||d.schema!=='hikyo-158/native-create-stage-preflight/v1'||d.phase!==phase||!same(d.expectedIdentity,expected)||!same(d.sourcePins,pins)||
       d.preparationSHA256!==manifest.preparationSHA256||d.normalizedIntentSHA256!==manifest.intentSHA256||
       !same(d.sharedWindow,window)||!same(d.observerManifest,manifest)||d.guardManifest.intentSHA256!==manifest.intentSHA256||d.guardManifest.wireBodySHA256!==manifest.wireBodySHA256||
       d.guardManifest.wireBodyBytes!==manifest.wireBodyBytes||d.guardManifest.endpoint!==manifest.endpoint||d.guardManifest.expiresAtMS!==manifest.expiresAtMS||
       d.guardManifest.schema!=='hikyo-158/native-kms-xhr-write-manifest/v1'||d.guardManifest.publicMainSHA256!==manifest.publicMainSHA256||d.guardManifest.timeoutMS!==20000||d.guardManifest.redirectLimitationReviewed!==true||
       d.requiredRemainingMS!==60000||d.maximumActionAgeMS!==2000||d.writeAuthorityGranted!==false||d.externalAuthenticityVerified!==false||d.runtimeReady!==false||
       now<d.issuedAtMS||d.effectBeforeMS!==Math.min(d.issuedAtMS+2000,d.physicalWindow.expiresAtMS,window.expiresAtMS-60000)||now>=d.effectBeforeMS||
       now<d.physicalWindow.notBeforeMS||d.physicalWindow.expiresAtMS!==d.physicalWindow.notBeforeMS+55000)fail();
    checkSharedWindow(globalObject,window,60000);review();const s=statuses();healthy(s.guard,s.observer,phase==='arm'?'observing':'armed');
    if(!same(s.guard,d.expectedGuardStatus)||!same(s.observer,d.expectedObserverStatus))fail();time(d);return s;
  }
  function time(d) {
    gate();const now=globalObject.Date.now();
    if(now<d.issuedAtMS||now>=d.effectBeforeMS||now<d.physicalWindow.notBeforeMS||now>=d.physicalWindow.expiresAtMS)fail();
  }
  function arm(d,expected) {
    try {
      if(held||armAttempted||armed||finishAttempted)fail();descriptor(d,'arm',expected);armAttempted=true;
      const guard=api('__hikyo158NativeKMSXHRWriteGuardV1',['status','arm']),arm=guard.arm;
      descriptor(d,'arm',expected);time(d);const result=Reflect.apply(arm,guard,[manifest.intentSHA256]);
      healthy(result,d.expectedObserverStatus,'armed');if(!same(result,{...d.expectedGuardStatus,phase:'armed'}))fail();
      armed=true;time(d);return result;
    }catch(e){held=true;throw e;}
  }
  function finish(d,expected) {
    try {
      if(held||!armed||finishAttempted)fail();descriptor(d,'finish',expected);
      gate();const buttons=Reflect.apply(nativeDocument.querySelectorAll,nativeDocument,['button']);
      if(buttons.length>32)fail();const nodes=[];
      for(const button of buttons){gate();if(!visible(button))continue;gate();const label=button.getAttribute('aria-label');gate();const text=label===null?button.textContent:label;
        if(typeof text!=='string'||text.length>128)fail();if(text.trim()==='Finish')nodes.push(button);}
      gate();if(nodes.length!==1)fail();
      const n=nodes[0];
      function finishButton() {
        gate();if(!visible(n)||publicRead(()=>n.tagName)!=='BUTTON'||!['button','submit'].includes(publicRead(()=>n.type))||
          publicRead(()=>n.disabled)||publicRead(()=>Reflect.apply(nativeMatches,n,[':disabled']))||
          publicRead(()=>n.getAttribute('aria-disabled'))==='true')fail();
        const label=publicRead(()=>n.getAttribute('aria-label')),text=label===null?publicRead(()=>n.textContent):label;
        if(typeof text!=='string'||text.trim()!=='Finish')fail();
        gate();if(!visible(n)||publicRead(()=>Reflect.apply(nativeMatches,n,[':disabled'])))fail();
      }
      finishButton();
      gate();const label=n.getAttribute('aria-label');gate();const text=label===null?n.textContent:label;if(typeof text!=='string'||text.trim()!=='Finish')fail();
      // Resolve the native click first; recheck synchronously after every DOM
      // access and immediately before ONE irreversible local dispatch.
      const click=globalObject.HTMLElement.prototype.click;if(typeof click!=='function')fail();
      finishAttempted=true;descriptor(d,'finish',expected);finishButton();time(d);Reflect.apply(click,n,[]);time(d);
      return {clickDispatched:true,providerOutcomeProven:false};
    }catch(e){held=true;throw e;}
  }
  return nativeObject.freeze({gate,statuses,arm,finish,time,
    // Preserve a truthful zero probe count for inspection only. read() and
    // the unchanged G arm preflight still require a successful native probe.
    inspect:()=>statuses(),
    read:phase=>{review();const s=statuses();healthy(s.guard,s.observer,phase==='arm'?'observing':'armed');return s;},
    bind:(w,m)=>{gate();if(!same(w,window)||!same(m,manifest))fail();},
    installed:()=>{if(installed||held)fail();gate();installed=true;},
    guardObject:()=>new Proxy(nativeObject,{get(target,key){if(key==='defineProperty')return(...args)=>{gate();return Reflect.apply(target.defineProperty,target,args);};return Reflect.get(target,key);}})});
}
 const stage=createNativeStage(globalThis,window,manifest,{"documentSHA256":"81c5ee2e76e7c70703625e03460a3f3c3bf655b858e5dc781ed4e869232f636a","guardSHA256":"6182330cb0a73fcc6e9f31dd5e5080b952e89735e40d0e9e3f961b5c9a145f26","kernelSHA256":"4b015b1f7623deedbf7ff72248caf59020b136b272cd1e38bc0a95fc8ad751f5","modelSHA256":"f40c07b2204c3305b434836015c99f30af4dfc893a0ce0e4f0318c17ea844a19","modelTestSHA256":"f06b2e173ff499070ae8718d65870534510a585339b8fc451462baaea6dc5985","observerSHA256":"5a68216b0e07b7fe207b1f361f2ab7192d3e28e2c13ae1adee8b20211e398700","windowSHA256":"f72078c6663117c4cc82376af3b016a890f08dbeeb27c2d668132826f788739e"},checkSharedWindow,attemptTrustBindingSHA256);stage.gate();
 if(window.notBeforeMS!==manifest.notBeforeMS||window.expiresAtMS!==manifest.expiresAtMS||globalThis.Object.hasOwn(globalThis,'__hikyo158NativeCreateStageV1')||globalThis.Object.hasOwn(globalThis,'__hikyo158NextKeyCustodyObserverV1')||globalThis.Object.hasOwn(globalThis,'__hikyo158NativeKMSXHRWriteGuardV1'))throw Error('native-create-stage-held');
 globalThis.Object.defineProperty(globalThis,'__hikyo158NativeCreateStageV1',{value:stage,writable:false,configurable:false});
 let value;
 {
 const Object=stage.guardObject();
 const document=createGuardedDocument(globalThis,["https://eu-central-1.console.aws.amazon.com/kms/home?region=eu-central-1#/kms/keys/create"],window);
 async function installHikyo158NextKeyCustodyObserverV1(input) {
  'use strict';
  // SOURCE ONLY. Passive native XHR observation. No API calls, auth/storage
  // reads, JSON parsing or write authority. Install create mode BEFORE guard.
  const marker='__hikyo158NextKeyCustodyObserverV1',guardMarker='__hikyo158NativeKMSXHRWriteGuardV1';
  const account='099256073061',region='eu-central-1',endpoint='https://kms.eu-central-1.amazonaws.com/';
  const prefix='arn:aws:kms:'+region+':'+account+':key/';
  const oldIds=['10908b8d-fe54-40e3-9210-a3eefea709b6','990c479f-d673-4bc9-8bca-7b9b86f4b205','fa374267-a456-4e79-b5b1-6cdf356b7d0e','bfa44dc1-ee8a-43b8-aec7-d8a7fbb38db1'];
  const main='https://a.b.cdn.console.awsstatic.com/a/v1/B35BNLNKUKKD7F5DTSPWUSOXQ4VAIKKIFWAKR2KD5MB4MCZXQ4RQ/main.js';
  const publicSHA='20a50d3f9e11cdae453b0f764689073f2adc6c644e6ac2a08660be5b1b2227a2';
  const enc=new TextEncoder(),dec=new TextDecoder('utf-8',{fatal:true});
  const fail=code=>{throw new Error('hikyo158-next-key:'+code);};
  const isSHA=v=>typeof v==='string'&&/^[a-f0-9]{64}$/.test(v);
  function exact(value,names){
    if(!value||Object.getPrototypeOf(value)!==Object.prototype||Reflect.ownKeys(value).length!==names.length)fail('closed-fields');
    const ds=Object.getOwnPropertyDescriptors(value);
    if(!names.every(k=>Object.hasOwn(ds,k)&&Object.hasOwn(ds[k],'value')&&ds[k].enumerable))fail('data-fields');
    return Object.fromEntries(names.map(k=>[k,ds[k].value]));
  }
  const args=exact(input,['manifest','approvedCreateBody']);
  const manifest=exact(args.manifest,['schema','mode','account','region','endpoint','publicMainSHA256','preparationSHA256','ownerContextSHA256','intentSHA256','claimId','wireBodySHA256','wireBodyBytes','notBeforeMS','expiresAtMS','newKeyARN','createReceiptSHA256']);
  const create=manifest.mode==='create';
  if(manifest.schema!=='hikyo-158/next-key-observer-manifest/v1'||!['create','readback'].includes(manifest.mode)||manifest.account!==account||manifest.region!==region||manifest.endpoint!==endpoint||manifest.publicMainSHA256!==publicSHA
    ||!['preparationSHA256','ownerContextSHA256','intentSHA256','wireBodySHA256'].every(k=>isSHA(manifest[k]))||typeof manifest.claimId!=='string'||! /^[a-zA-Z0-9-]{8,100}$/.test(manifest.claimId)
    ||!Number.isInteger(manifest.wireBodyBytes)||manifest.wireBodyBytes<2||manifest.wireBodyBytes>32768
    ||!Number.isInteger(manifest.notBeforeMS)||!Number.isInteger(manifest.expiresAtMS)||manifest.notBeforeMS>Date.now()||Date.now()-manifest.notBeforeMS>300000||manifest.expiresAtMS<=Date.now()||manifest.expiresAtMS>manifest.notBeforeMS+300000)fail('manifest');
  if(create){if(manifest.newKeyARN!==null||manifest.createReceiptSHA256!==null||typeof args.approvedCreateBody!=='string'||enc.encode(args.approvedCreateBody).byteLength!==manifest.wireBodyBytes)fail('create-mode');}
  else if(args.approvedCreateBody!==null||!isSHA(manifest.createReceiptSHA256)||typeof manifest.newKeyARN!=='string'||!new RegExp('^'+prefix+'[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12}$').test(manifest.newKeyARN)||oldIds.some(id=>manifest.newKeyARN===prefix+id))fail('readback-mode');
  Object.freeze(manifest);
  if(!crypto?.subtle||typeof XMLHttpRequest!=='function')fail('platform');
  const hash=async bytes=>Array.from(new Uint8Array(await crypto.subtle.digest('SHA-256',bytes)),v=>v.toString(16).padStart(2,'0')).join('');
  function canonical(v){return v&&typeof v==='object'?'{'+Object.keys(v).sort().map(k=>JSON.stringify(k)+':'+canonical(v[k])).join(',')+'}':JSON.stringify(v);}
  const manifestSHA=await hash(enc.encode(canonical(manifest)));
  if(create&&await hash(enc.encode(args.approvedCreateBody))!==manifest.wireBodySHA256)fail('exact-create-body-hash');
  function page(){return location.origin==='https://eu-central-1.console.aws.amazon.com'&&location.pathname==='/kms/home'&&location.search==='?region=eu-central-1'&&globalThis.__isTangerineBoxHosted__===true;}
  function banner(){return Array.from(document.querySelectorAll('header, nav, #awsc-nav-account-menu-button, [data-testid="awsc-nav-account-menu-button"]')).some(n=>[n.textContent,n.getAttribute('aria-label')].some(v=>typeof v==='string'&&new RegExp('(^|[^0-9])'+account+'([^0-9]|$)').test(v)));}
  function route(){if(create)return location.hash==='#/kms/keys/create';let path;try{path=decodeURIComponent(location.hash);}catch{return false;}
    return [manifest.newKeyARN,...oldIds.map(id=>prefix+id)].some(arn=>[arn,arn.slice(prefix.length)].some(k=>path==='#/kms/keys/'+k||['keyPolicy','cryptographicConfiguration','keyRotation','aliases','tags','keyMaterial'].some(tab=>path==='#/kms/keys/'+k+'/'+tab)));}
  if(!page()||!banner()||!(route()||!create&&location.hash==='#/kms/home')||!Array.from(document.scripts).some(s=>s.src===main)||Date.now()>=manifest.expiresAtMS)fail('native-page-source');
  if([marker,guardMarker,'__hikyo158NextInventoryObserverV1','__hikyo158KMSMetadataObserverV1'].some(k=>Object.hasOwn(globalThis,k)))fail('fresh-document-before-guard');
  const proto=XMLHttpRequest.prototype,originalOpen=proto.open,originalHeader=proto.setRequestHeader,originalSend=proto.send;
  const records=new WeakMap(),claimed=new Set(),receipts=new Map(),settlers=new Set();let closed=false,pending=0,stored=0,error=null;
  const alive=()=>!closed&&Date.now()>=manifest.notBeforeMS&&Date.now()<manifest.expiresAtMS&&page()&&banner();
  const context=()=>alive()&&route();
  function guardSnapshot(){
    const d=Object.getOwnPropertyDescriptor(globalThis,guardMarker);if(!d||!Object.hasOwn(d,'value'))return null;
    const sd=Object.getOwnPropertyDescriptor(d.value,'status');if(!sd||!Object.hasOwn(sd,'value')||typeof sd.value!=='function')return null;
    const s=Reflect.apply(sd.value,d.value,[]);
    const wanted=['schema','phase','claimConsumed','nativeSendInvocations','intentSHA256','wireBodySHA256','endpoint'];
    if(!s||!wanted.every(k=>{const x=Object.getOwnPropertyDescriptor(s,k);return x&&Object.hasOwn(x,'value');}))return null;
    if(s.schema!=='hikyo-158/native-kms-xhr-write-status/v1'||s.phase!=='forwarded'||s.claimConsumed!==true||s.nativeSendInvocations!==1||s.intentSHA256!==manifest.intentSHA256||s.wireBodySHA256!==manifest.wireBodySHA256||s.endpoint!==endpoint)return null;
    return Object.freeze(Object.fromEntries(wanted.map(k=>[k,s[k]])));
  }
  function scope(record,body){
    if(typeof body!=='string'||enc.encode(body).byteLength>32768)return null;
    if(create){if(record.target!=='TrentService.CreateKey'||body!==args.approvedCreateBody)return null;const guard=guardSnapshot();return guard?{slot:'create',action:'CreateKey',keyARN:null,guardAtForward:guard}:null;}
    for(const [index,arn] of [manifest.newKeyARN,...oldIds.map(id=>prefix+id)].entries())for(const wire of [arn,arn.slice(prefix.length)]){
      const action=record.target?.slice('TrentService.'.length);if(record.target!=='TrentService.'+action||!['DescribeKey','GetKeyPolicy','GetKeyRotationStatus','ListAliases'].includes(action)||index>0&&action!=='DescribeKey')continue;
      const variants=action==='GetKeyPolicy'?[JSON.stringify({KeyId:wire,PolicyName:'default'})]:action==='ListAliases'?[JSON.stringify({KeyId:wire}),JSON.stringify({KeyId:wire,Limit:100}),JSON.stringify({Limit:100,KeyId:wire})]:[JSON.stringify({KeyId:wire})];
      if(variants.includes(body))return{slot:(index===0?'new':'old'+index)+'.'+action,action,keyARN:arn,guardAtForward:null};
    }return null;
  }
  function bytes(xhr){if(xhr.responseType==='arraybuffer'){const v=xhr.response;if(!(v instanceof ArrayBuffer)||v.byteLength>32768)fail('response-size');return new Uint8Array(v).slice();}
    if(xhr.responseType===''||xhr.responseType==='text'){const v=xhr.responseText;if(typeof v!=='string'||v.length>32768)fail('response-size');return enc.encode(v);}fail('response-type');}
  function open(method,url,async){records.set(this,arguments.length<=3&&(arguments.length<3||async===true)&&method==='POST'&&url===endpoint?{type:null,target:null,sent:false}:null);return Reflect.apply(originalOpen,this,arguments);}
  function header(name,value){const r=records.get(this);if(r&&!r.sent&&typeof name==='string'){const key=name.toLowerCase()==='content-type'?'type':name.toLowerCase()==='x-amz-target'?'target':null;if(key){if(r[key]!==null||typeof value!=='string'||value.length>128)records.set(this,null);else r[key]=value;}}return Reflect.apply(originalHeader,this,arguments);}
  function send(body){const xhr=this,r=records.get(xhr);let selected=null;
    try{if(context()&&r&&!r.sent&&r.type==='application/x-amz-json-1.1'&&pending<8){r.sent=true;selected=scope(r,body);}}catch{error='classification-held';}
    let settle=null;
    if(selected&&!claimed.has(selected.slot)){
      claimed.add(selected.slot);pending++;const sentAtMS=Date.now();let settled=false;const timer=setTimeout(()=>settle(),Math.min(20000,manifest.expiresAtMS-Date.now()));
      settle=()=>{if(!settled){settled=true;clearTimeout(timer);pending--;settlers.delete(settle);}};settlers.add(settle);
      xhr.addEventListener('loadend',()=>{if(settled)return;void(async()=>{try{
        if(!alive()||xhr.status!==200||xhr.responseURL!==endpoint)return;
        const receivedAtMS=Date.now(),raw=bytes(xhr);if(raw.byteLength<2||raw.byteLength>32768||stored+raw.byteLength>262144)fail('response-bounds');
        const rawResponse=dec.decode(raw);if(/SecretString|SecretBinary|SecretAccessKey|AccessKeyId|SessionToken/.test(rawResponse))fail('credential-or-value-field');
        // Only this exact public response header is accessed, after classification.
        let headerId=null;if(create){try{headerId=xhr.getResponseHeader('x-amzn-requestid');}catch{error='create-request-id-unavailable-reconcile-existing-key';}}
        const requestId=create&&typeof headerId==='string'&&/^[A-Za-z0-9-]{8,128}$/.test(headerId)?headerId:null;
        const requestIdCaptureState=create?(requestId===null?'unavailable':'captured'):'not-requested';
        if(create&&requestId===null)error='create-request-id-unavailable-reconcile-existing-key';
        const receipt={schema:'hikyo-158/next-key-native-response/v1',manifestSHA256:manifestSHA,manifest,...selected,
          status:200,responseEndpoint:endpoint,sentAtMS,receivedAtMS,requestBodyBytes:enc.encode(body).byteLength,requestBodySHA256:await hash(enc.encode(body)),
          rawResponse,rawResponseBytes:raw.byteLength,rawResponseSHA256:await hash(raw),requestId,requestIdCaptureState,requestIdSource:create?'native-response-header:x-amzn-requestid':null,
          JSONParsedByObserver:false,providerAPIsCalledByObserver:false,ownerAuthenticatedByObserver:false,publicSourceAuthenticatedByObserver:false,writeAuthorityGrantedByObserver:false};
        if(!alive()||settled||Date.now()-sentAtMS>20000||receivedAtMS<sentAtMS||Date.now()<receivedAtMS||stored+raw.byteLength>262144||enc.encode(JSON.stringify(receipt)).byteLength>60000)fail('receipt-bounds');
        stored+=raw.byteLength;receipts.set(selected.slot,Object.freeze(receipt));
      }catch{error='capture-held';}finally{settle();}})();},{once:true});
    }
    try{return Reflect.apply(originalSend,xhr,arguments);}catch(e){if(settle)settle();throw e;}
  }
  // Create mode intentionally remains configurable so the separately reviewed
  // guard can retain these wrappers as originals and take final write ownership.
  for(const [name,value]of[['open',open],['setRequestHeader',header],['send',send]])Object.defineProperty(proto,name,{value,configurable:create,writable:create});
  function status(){return Object.freeze({schema:'hikyo-158/next-key-observer-status/v1',mode:manifest.mode,readySlots:[...receipts.keys()].sort(),pendingCaptures:pending,error,
    providerAPIsCalledByObserver:false,ownerAuthenticatedByObserver:false,publicSourceAuthenticatedByObserver:false,writeAuthorityGrantedByObserver:false,coversOtherRealmsOrCapturedMethods:false,physicalHTTPHopBoundEstablished:false,cleanup:'full-navigation-controlled-tab-preserves-session'});}
  function read(slot){if(!alive()||!receipts.has(slot)||Date.now()-receipts.get(slot).sentAtMS>300000)fail('missing-or-expired');return receipts.get(slot);}
  Object.defineProperty(globalThis,marker,{value:Object.freeze({status,read}),configurable:false,writable:false});
  setTimeout(()=>{closed=true;receipts.clear();stored=0;for(const stop of settlers)stop();settlers.clear();},manifest.expiresAtMS-Date.now());return status();
}

 value=await installHikyo158NextKeyCustodyObserverV1({manifest,approvedCreateBody:chunks.join('')});
 }
 stage.installed();stage.gate();
 return {readAtMS:Date.now(),value:{observer:value,installedBeforeGuard:true}};
 },{window,manifest:input.manifest,chunks:input.approvedBodyChunks,attemptTrustBindingSHA256:input.attemptTrustBindingSHA256});
 const after=await identity();await cdp.detach();cdp=null;
 await page.evaluate(({window,descriptor})=>{const s=globalThis.__hikyo158NativeCreateStageV1;if(!s)throw Error('native-create-stage-held');s.gate();if(descriptor)s.time(descriptor);},{window,descriptor:input.descriptor||null});
 return {schema:'hikyo-158/native-create-stage-readout/v1',kind:"install",phase:input.phase||'install',before,after,readAtMS:captured.readAtMS,value:captured.value,controlLeaseClaimed:false,exclusiveControlVerifiedByAdapter:false,writeAuthorityGranted:false,providerOutcomeProven:false};
 }catch{throw new Error('native-create-stage-held');}finally{if(cdp)await cdp.detach();}
}
