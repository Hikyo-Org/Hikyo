async function installHikyo158HistoricalDescribeTransportGateV2(input) {
  'use strict';
  // SOURCE ONLY: page-local gate, never a request initiator or source authenticator.
  const marker='__hikyo158HistoricalDescribeTransportGateV2',observerMarker='__hikyo158HistoricalDescribeObserverV2';
  const account='099256073061',region='eu-central-1';
  const arn='arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261018b/web-identity/postgres/role-document-vmN1Vj';
  const endpoint='https://secretsmanager.eu-central-1.amazonaws.com/',target='secretsmanager.DescribeSecret';
  const base='https://a.b.cdn.console.awsstatic.com/a/v1/';
  const app=base+'TJSQGUMIVFEENQQX4MGXCXUOCNT774UXHMTEHFLOY3762Z4ZSXUQ/';
  const shared=base+'6DBWFASSP54OHK6DYP2MM5HOATDYIYDOCRIDN74DJRJNWQ4DNPUA/';
  const pins={publicMainSHA256:'d412372053a5b7a265fa704e4d483522f4cff28e93fcf8d46ab7f18f797c2520',
    publicRuntimeSHA256:'f935a35837023598fea6628ce95690c4de2fda88dd8a7f9b8a7cb8b01b824577',
    publicVendorsSHA256:'345320425b972ad1c068f2780859d68ff4c41714d6c083749e08b3c3f6724a01',
    publicReactSHA256:'9f67b611afa13d1bf1bd403e6f7eba45eff50265b5d2d0e5b3fd20ba9409343b',
    observerSourceSHA256:'7e127ae676b4c8c16e7105881f429a18e4e28f2232e14b549603c67ea60120fd'};
  const wrappers={fetch:'b5e34e1ee082e870a4b7028ca8153b85622c97fc074b7fa71f1d59b65d74b9ea',send:'223acf53882c8b0a3c342fb13c3d46ea0b31baa109c9ecee982b912fb89ca3b0'};
  const fields=['schema','account','region','secretARN','reviewedPreparationSHA256','ownerContextSHA256',...Object.keys(pins),
    'requestBodySHA256','requestBodyBytes','notBeforeMS','expiresAtMS','captureTimeoutMS','maximumMatchingCalls'];
  const fail=()=>{throw new Error('hikyo158-historical-describe-transport-held');};
  if(!input||Object.getPrototypeOf(input)!==Object.prototype||Reflect.ownKeys(input).length!==fields.length)fail();
  const descriptors=Object.getOwnPropertyDescriptors(input);
  if(!fields.every(k=>Object.hasOwn(descriptors,k)&&Object.hasOwn(descriptors[k],'value')&&descriptors[k].enumerable))fail();
  const manifest=Object.freeze(Object.fromEntries(fields.map(k=>[k,descriptors[k].value]))),start=Date.now();
  const sha=v=>typeof v==='string'&&/^[0-9a-f]{64}$/.test(v);
  if(manifest.schema!=='hikyo-158/historical-describe-transport-gate/v2'||manifest.account!==account||manifest.region!==region||manifest.secretARN!==arn||
    !sha(manifest.reviewedPreparationSHA256)||!sha(manifest.ownerContextSHA256)||!Object.keys(pins).every(k=>manifest[k]===pins[k])||
    !Number.isInteger(manifest.notBeforeMS)||manifest.notBeforeMS>start||start-manifest.notBeforeMS>300000||!Number.isInteger(manifest.expiresAtMS)||
    manifest.expiresAtMS<=start||manifest.expiresAtMS>manifest.notBeforeMS+300000||manifest.maximumMatchingCalls!==1||
    !Number.isInteger(manifest.captureTimeoutMS)||manifest.captureTimeoutMS<1000||manifest.captureTimeoutMS>20000||!sha(manifest.requestBodySHA256))fail();
  if(typeof fetch!=='function'||typeof Request!=='function'||typeof Headers!=='function'||typeof XMLHttpRequest!=='function'||!crypto?.subtle||Object.hasOwn(globalThis,marker)||Object.hasOwn(globalThis,observerMarker))fail();
  const proto=XMLHttpRequest.prototype,fetchDescriptor=Object.getOwnPropertyDescriptor(globalThis,'fetch'),sendDescriptor=Object.getOwnPropertyDescriptor(proto,'send');
  if(!fetchDescriptor||fetchDescriptor.configurable!==true||!Object.hasOwn(fetchDescriptor,'value')||!sendDescriptor||sendDescriptor.configurable!==true||!Object.hasOwn(sendDescriptor,'value'))fail();
  const encoder=new TextEncoder(),wire=encoder.encode(JSON.stringify({SecretId:arn}));
  const hash=async bytes=>Array.from(new Uint8Array(await crypto.subtle.digest('SHA-256',bytes)),v=>v.toString(16).padStart(2,'0')).join('');
  function route(detailOnly=false){
    if(location.origin!=='https://eu-central-1.console.aws.amazon.com'||location.hash!=='')return false;
    const q=new URLSearchParams(location.search);
    if(location.pathname==='/secretsmanager/secret')return q.size===2&&q.getAll('name').length===1&&q.get('name')===arn&&q.getAll('region').length===1&&q.get('region')===region;
    return !detailOnly&&['/secretsmanager/listsecrets','/secretsmanager/home'].includes(location.pathname)&&q.size===1&&q.get('region')===region;
  }
  function accountAndMounts(){return !!document.querySelector('#c')&&[app+'main.js',app+'runtime.js',shared+'vendors.js',shared+'vendors_react.js'].every(url=>Array.from(document.scripts).some(s=>s.src===url))&&
    Array.from(document.querySelectorAll('header, nav, #awsc-nav-account-menu-button, [data-testid="awsc-nav-account-menu-button"]')).some(n=>[n.textContent,n.getAttribute('aria-label')].some(v=>typeof v==='string'&&/(^|[^0-9])(?:099256073061|0992-5607-3061)([^0-9]|$)/.test(v)));}
  const live=()=>Date.now()>=manifest.notBeforeMS&&Date.now()<manifest.expiresAtMS;
  const context=()=>live()&&route()&&accountAndMounts();
  if(!context()||!['/secretsmanager/listsecrets','/secretsmanager/home'].includes(location.pathname)||manifest.requestBodyBytes!==wire.byteLength||await hash(wire)!==manifest.requestBodySHA256)fail();
  if(!context()||!['/secretsmanager/listsecrets','/secretsmanager/home'].includes(location.pathname))fail();
  const nativeFetch=globalThis.fetch,toString=Function.prototype.toString,requestClone=Request.prototype.clone;
  const requestMethod=Object.getOwnPropertyDescriptor(Request.prototype,'method').get,requestURL=Object.getOwnPropertyDescriptor(Request.prototype,'url').get;
  const requestHeaders=Object.getOwnPropertyDescriptor(Request.prototype,'headers').get,headerGet=Headers.prototype.get;
  function exactSelectors(request){
    const headers=Reflect.apply(requestHeaders,request,[]);
    return Reflect.apply(requestMethod,request,[])==='POST'&&Reflect.apply(requestURL,request,[])===endpoint&&
      Reflect.apply(headerGet,headers,['x-amz-target'])===target&&Reflect.apply(headerGet,headers,['content-type'])==='application/x-amz-json-1.1';
  }
  let consumed=false,forwards=0,blockedFetch=0,blockedXHR=0,lastLockVerified=false;
  async function verifyObserverLock(){
    lastLockVerified=false;
    if(!context())fail();
    const fd=Object.getOwnPropertyDescriptor(globalThis,'fetch'),sd=Object.getOwnPropertyDescriptor(proto,'send'),od=Object.getOwnPropertyDescriptor(globalThis,observerMarker);
    if(!fd||!sd||!od||!Object.hasOwn(fd,'value')||!Object.hasOwn(sd,'value')||!Object.hasOwn(od,'value')||
      fd.configurable!==false||fd.writable!==false||sd.configurable!==false||sd.writable!==false||od.configurable!==false||od.writable!==false||
      typeof fd.value!=='function'||typeof sd.value!=='function'||fd.value===gatedFetch||sd.value===blockedSend||!Object.isFrozen(od.value)||Reflect.ownKeys(od.value).length!==2)fail();
    const methods=Object.getOwnPropertyDescriptors(od.value);
    if(!['status','read'].every(k=>Object.hasOwn(methods,k)&&Object.hasOwn(methods[k],'value')&&typeof methods[k].value==='function'))fail();
    const a=await hash(encoder.encode(Reflect.apply(toString,fd.value,[]))),b=await hash(encoder.encode(Reflect.apply(toString,sd.value,[])));
    if(!context()||a!==wrappers.fetch||b!==wrappers.send||Object.getOwnPropertyDescriptor(globalThis,'fetch').value!==fd.value||Object.getOwnPropertyDescriptor(proto,'send').value!==sd.value)fail();
    lastLockVerified=true;return Object.freeze({exactObserverWrappersLocked:true,sourceAuthenticityVerifiedByGate:false,observerClosureCustodyVerifiedByGate:false});
  }
  async function gatedFetch(request){
    if(arguments.length!==1||!(request instanceof Request)||!context()||!route(true)||consumed||!exactSelectors(request)){blockedFetch++;fail();}
    consumed=true; // Burn synchronously before any classification, await or possible native error.
    let reader=null,active=true,rejectTimeout;
    const expired=new Promise((resolve,reject)=>{rejectTimeout=reject;});
    const timer=setTimeout(()=>{active=false;if(reader)void reader.cancel().catch(()=>{});rejectTimeout(new Error('hikyo158-historical-describe-transport-held'));},Math.max(1,Math.min(manifest.captureTimeoutMS,manifest.expiresAtMS-Date.now())));
    try{
      await Promise.race([(async()=>{
        await verifyObserverLock();if(!active||!context()||!route(true))fail();
        const clone=Reflect.apply(requestClone,request,[]);reader=clone.body?.getReader();if(!reader)fail();let offset=0;
        while(active&&context()&&route(true)){
          const part=await reader.read();if(part.done){if(offset!==wire.byteLength)fail();return;}
          if(!(part.value instanceof Uint8Array))fail();
          for(const value of part.value){if(offset>=wire.byteLength||value!==wire[offset++])fail();}
        }
        fail();
      })(),expired]);
      if(!active||!context()||!route(true)||forwards!==0||!exactSelectors(request))fail();
      clearTimeout(timer);forwards++;
      return Reflect.apply(nativeFetch,this,arguments); // Original opaque Request/this/arguments, one native forward.
    }catch(error){blockedFetch++;throw error;}
    finally{active=false;clearTimeout(timer);if(reader)void reader.cancel().catch(()=>{});}
  }
  function blockedSend(){blockedXHR++;fail();} // All native XHR sends held; never inspect a body or auth field.
  function status(){return Object.freeze({schema:'hikyo-158/historical-describe-transport-gate-status/v2',consumed,forwards,blockedFetch,blockedXHR,lastLockVerified,live:live(),
    expiresAtMS:manifest.expiresAtMS,reviewedPreparationSHA256:manifest.reviewedPreparationSHA256,ownerContextSHA256:manifest.ownerContextSHA256,
    maximumMatchingCalls:1,providerRequestsInitiatedByGate:false,authenticationReadByGate:false,ownerIdentityVerifiedByGate:false,publicSourceBytesVerifiedByGate:false,
    coversOtherRealmsOrCapturedTransports:false,publicStaticFetchPassthrough:false,cleanup:'normal-document-unload-preserves-session-no-storage-reset'});}
  // Intentionally replaceable only until the unchanged frozen observer wraps and locks these two functions.
  Object.defineProperty(globalThis,'fetch',{value:gatedFetch,configurable:true,writable:false});
  Object.defineProperty(proto,'send',{value:blockedSend,configurable:true,writable:false});
  Object.defineProperty(globalThis,marker,{value:Object.freeze({status,verifyObserverLock}),configurable:false,writable:false});
  return status();
}
