'use strict';
// Synchronous page capability. No request construction, auth or source authority.
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
module.exports={createNativeStage};
