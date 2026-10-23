async function installHikyo158NextSetupWholeReadPrearmObserverV1(input) {
  'use strict';
  // SOURCE ONLY: prearm on the exact normal policy list; capture only target details.
  // No navigation, cache access, SDK, imports,
  // network initiation, authentication, cookies, storage or mutation authority.
  const marker='__hikyo158NextSetupWholeReadPrearmObserverV1';
  const ARN='arn:aws:iam::099256073061:policy/hikyo-158-setup-policy',ID='ANPAROHA7PNSTPQUQEUTR';
  const MAIN='https://a.b.cdn.console.awsstatic.com/a/v1/EPYETUDGRYAX3B5DPL5Z2CIJYNSNNCGH7C7BCGW564I7W6F6ZYAQ/main.js';
  const MAIN_SHA='85a9aed5a11dc8d019b95f399a1502b1c8e19affee5c933fc09623d0d359d0f9',VENDORS_SHA='e4cdd4a45e6d93e7d1e4c512cfa82eaf02bbb040280c93b1b0f20958118b4ba9';
  const endpoint='https://iam.amazonaws.com/';
  const stages={beforeDelete:{ids:['v1','v2','v3','v4','v5'],defaultId:'v5'},afterDelete:{ids:['v2','v3','v4','v5'],defaultId:'v5'},
    activated:{ids:['v2','v3','v4','v5','v6'],defaultId:'v6'},restored:{ids:['v2','v3','v4','v5','v6'],defaultId:'v5'}};
  const fields=['schema','stage','account','region','policyARN','policyId','notBeforeMS','expiresAtMS','reviewedPreparationSHA256','publicMainSHA256','publicVendorsSHA256'];
  const fail=code=>{throw new Error('hikyo158-next-whole-read:'+code);};
  if(!input||Object.getPrototypeOf(input)!==Object.prototype||Reflect.ownKeys(input).length!==fields.length)fail('manifest-fields');
  const descriptors=Object.getOwnPropertyDescriptors(input);
  if(!fields.every(key=>Object.hasOwn(descriptors,key)&&Object.hasOwn(descriptors[key],'value')&&descriptors[key].enumerable))fail('manifest-data');
  const manifest=Object.freeze(Object.fromEntries(fields.map(key=>[key,descriptors[key].value]))),start=Date.now();
  if(manifest.schema!=='hikyo-158/next-setup-whole-read-manifest/v1'||typeof manifest.stage!=='string'||!Object.hasOwn(stages,manifest.stage)||manifest.account!=='099256073061'||manifest.region!=='eu-central-1'||
    manifest.policyARN!==ARN||manifest.policyId!==ID||manifest.publicMainSHA256!==MAIN_SHA||manifest.publicVendorsSHA256!==VENDORS_SHA||
    (typeof manifest.reviewedPreparationSHA256!=='string'||!/^[a-f0-9]{64}$/.test(manifest.reviewedPreparationSHA256))||!Number.isInteger(manifest.notBeforeMS)||manifest.notBeforeMS>start||start-manifest.notBeforeMS>300000||
    !Number.isInteger(manifest.expiresAtMS)||manifest.expiresAtMS<=start||manifest.expiresAtMS>manifest.notBeforeMS+300000)fail('manifest-binding');
  if(typeof fetch!=='function'||typeof Request!=='function'||!crypto?.subtle||Object.hasOwn(globalThis,marker)||
    ['__hikyo158NextSetupWholeReadObserverV1','__hikyo158NextSetupVersionGuardV1','__hikyo158RecoveryManagedVersionGuardV1','__hikyo158BoundaryDeleteGuardV1','__hikyo158RecoveryMetadataObserverV1','__hikyo158RecoveryMetadataObserverV2'].some(key=>Object.hasOwn(globalThis,key)))fail('fresh-passive-document-required');
  const routes=[ARN.replace('/','%2F'),encodeURIComponent(ARN)].flatMap(encoded=>['','?section=versions','?section=permissions'].map(tail=>'#/policies/details/'+encoded+tail));
  function page(){return location.origin==='https://us-east-1.console.aws.amazon.com'&&location.pathname==='/iam/home'&&location.search==='?region=eu-central-1';}
  function prearmRoute(){return page()&&location.hash==='#/policies';}
  function route(){return page()&&routes.includes(location.hash);}
  function account(){return Array.from(document.querySelectorAll('header, nav, #awsc-nav-account-menu-button, [data-testid="awsc-nav-account-menu-button"]')).some(node=>
    [node.textContent,node.getAttribute('aria-label')].some(text=>typeof text==='string'&&/(^|[^0-9])(?:099256073061|0992-5607-3061)([^0-9]|$)/.test(text)));}
  if(!prearmRoute()||!account()||!document.querySelector('#app')||!document.querySelector('[data-testid="iam-v2-vite"]')||
    !Array.from(document.scripts).some(s=>s.src===MAIN)||!Array.from(document.scripts).some(s=>s.src===MAIN.replace('/main.js','/vendors.js')))fail('public-page-binding');
  const encoder=new TextEncoder(),decoder=new TextDecoder('utf-8',{fatal:true});
  const hash=async bytes=>Array.from(new Uint8Array(await crypto.subtle.digest('SHA-256',bytes)),b=>b.toString(16).padStart(2,'0')).join('');
  const manifestSHA=await hash(encoder.encode(JSON.stringify(Object.fromEntries(fields.slice().sort().map(k=>[k,manifest[k]])))));
  if(Date.now()>=manifest.expiresAtMS||!prearmRoute()||!account())fail('preinstall-expired');
  const shape=stages[manifest.stage],readers=new Set(),stoppers=new Set(),claimed=new Set(),receipts=new Map();
  let closed=false,pending=0,attempts=0,skipped=0,captureError=null;
  const live=()=>!closed&&Date.now()<manifest.expiresAtMS;
  const context=()=>live()&&route()&&account();
  const wire=(action,id,max)=>'Action='+action+'&Version=2010-05-08&PolicyArn='+encodeURIComponent(ARN)+(id?'&VersionId='+id:'')+(max?'&MaxItems=1000':'');
  const wires=new Map([[wire('GetPolicy'),{action:'GetPolicy',versionId:null}],[wire('ListPolicyVersions'),{action:'ListPolicyVersions',versionId:null}],
    [wire('ListPolicyVersions',null,true),{action:'ListPolicyVersions',versionId:null}],...shape.ids.map(id=>[wire('GetPolicyVersion',id),{action:'GetPolicyVersion',versionId:id}])]);
  async function requestScope(request,active,ownedReaders){
    const reader=request.body?.getReader();if(!reader)return null;readers.add(reader);ownedReaders.add(reader);
    let bytes=[],key=[],value=[],isValue=false,action=null;const seen=new Set();
    const allowed=()=>action==='GetPolicyVersion'?['Action','Version','PolicyArn','VersionId']:action==='ListPolicyVersions'?['Action','Version','PolicyArn','MaxItems']:['Action','Version','PolicyArn'];
    function finish(){
      const name=decoder.decode(Uint8Array.from(key));
      if(!isValue||seen.has(name))fail('request-field');
      if(name==='Action'){
        action=decoder.decode(Uint8Array.from(value));
        if(!['GetPolicy','ListPolicyVersions','GetPolicyVersion'].includes(action))fail('not-closed-metadata');
      }
      seen.add(name);key=[];value=[];isValue=false;
    }
    try{
      while(active()&&context()){
        const chunk=await reader.read();if(chunk.done)break;
        if(!(chunk.value instanceof Uint8Array))fail('request-bytes');
        for(const byte of chunk.value){
          if(bytes.length>=1024)fail('request-size');
          if(byte===61&&!isValue){const name=decoder.decode(Uint8Array.from(key));if((seen.size===0&&name!=='Action')||!allowed().includes(name)||seen.has(name))fail('unknown-request-field');isValue=true;}
          else if(byte===38)finish();
          else if(isValue)value.push(byte);else{if(key.length>=32)fail('request-key');key.push(byte);}
          bytes.push(byte);
        }
      }
      if(!active()||!context())return null;finish();
      const body=Uint8Array.from(bytes),scope=wires.get(decoder.decode(body));
      if(!scope)return null;
      const slot=scope.action+(scope.versionId?'.'+scope.versionId:'');
      if(claimed.has(slot))return null;claimed.add(slot);
      return{...scope,slot,requestBodyBytes:body.byteLength,requestBodySHA256:await hash(body)};
    }catch{return null;}
    finally{readers.delete(reader);ownedReaders.delete(reader);void reader.cancel().catch(()=>{});}
  }
  async function responseBytes(response,active,ownedReaders){
    const reader=response.body?.getReader();if(!reader)fail('missing-response');readers.add(reader);ownedReaders.add(reader);const chunks=[];let size=0;
    try{
      while(active()&&context()){const part=await reader.read();if(part.done)break;
        if(!(part.value instanceof Uint8Array)||(size+=part.value.byteLength)>32768)fail('response-size');chunks.push(part.value.slice());}
      if(!active()||!context()||size<2)fail('response-expired');
      const all=new Uint8Array(size);let offset=0;for(const chunk of chunks){all.set(chunk,offset);offset+=chunk.byteLength;}return all;
    }finally{readers.delete(reader);ownedReaders.delete(reader);void reader.cancel().catch(()=>{});}
  }
  function task(work){
    pending++;const ownedReaders=new Set();let active=true,finish;const stopped=new Promise(resolve=>{finish=()=>{active=false;for(const reader of ownedReaders)void reader.cancel().catch(()=>{});resolve();};});stoppers.add(finish);
    const timer=setTimeout(finish,Math.max(1,Math.min(20000,manifest.expiresAtMS-Date.now())));
    void(async()=>{try{await Promise.race([work(()=>active,ownedReaders),stopped]);if(!active)captureError='capture-timeout-held';}catch{captureError='bounded-capture-held';}
      finally{active=false;clearTimeout(timer);stoppers.delete(finish);for(const reader of ownedReaders)void reader.cancel().catch(()=>{});pending--;}})();
  }
  const nativeFetch=globalThis.fetch;
  function observedFetch(request){
    let candidate=null;
    try{if(arguments.length===1&&request instanceof Request&&context()&&attempts<64&&pending<8&&request.method==='POST'&&request.url===endpoint&&
      request.headers.get('content-type')==='application/x-www-form-urlencoded'&&!request.headers.has('x-amz-target'))candidate=request.clone();}catch{}
    const sentAtMS=Date.now(),result=Reflect.apply(nativeFetch,this,arguments);
    if(candidate){attempts++;task(async (active,ownedReaders)=>{
      let resolved=false,scope=null;
      const classified=requestScope(candidate,active,ownedReaders).then(value=>{scope=value;resolved=true;return value;});
      const response=result.then(value=>{
        // Never clone unknown/value/write responses. An early unclassified reply
        // is deliberately skipped without delaying/replacing the native promise.
        if(!resolved||!scope||!active()||!context()||value.status!==200||value.url!==endpoint)return null;
        return{value:value.clone(),receivedAtMS:Date.now()};
      },()=>null);
      const [known,captured]=await Promise.all([classified,response]);
      if(!known||!captured||!active()||!context()){skipped++;return;}
      const bytes=await responseBytes(captured.value,active,ownedReaders),rawResponse=decoder.decode(bytes),rawResponseSHA256=await hash(bytes);
      if(!active()||!context()||Date.now()-sentAtMS>20000)return;
      const receipt=Object.freeze({schema:'hikyo-158/next-setup-native-read-response/v1',stage:manifest.stage,account:'099256073061',region:'eu-central-1',
        action:known.action,versionId:known.versionId,policyARN:ARN,policyId:ID,slot:known.slot,requestBodyBytes:known.requestBodyBytes,requestBodySHA256:known.requestBodySHA256,
        status:200,endpoint,sentAtMS,receivedAtMS:captured.receivedAtMS,rawResponseBytes:bytes.byteLength,rawResponseSHA256,rawResponse,
        manifestSHA256:manifestSHA,notBeforeMS:manifest.notBeforeMS,expiresAtMS:manifest.expiresAtMS,reviewedPreparationSHA256:manifest.reviewedPreparationSHA256,
        publicMainSHA256:MAIN_SHA,publicVendorsSHA256:VENDORS_SHA,ownerIdentityVerifiedByObserver:false,publicSourceBytesVerifiedByObserver:false,
        providerAPIsCalledByObserver:false,policyDocumentsDecodedByObserver:false});
      if(encoder.encode(JSON.stringify(receipt)).byteLength>60000)fail('receipt-output-size');receipts.set(known.slot,receipt);
    });}
    return result;
  }
  const slots=['GetPolicy','ListPolicyVersions',...shape.ids.map(id=>'GetPolicyVersion.'+id)];
  function status(){return Object.freeze({schema:'hikyo-158/next-setup-whole-read-status/v1',stage:manifest.stage,pending,attempts,skipped,captureError,
    readySlots:Object.freeze([...receipts.keys()].sort()),requiredSlots:Object.freeze(slots.slice()),completeNativeReads:slots.every(slot=>receipts.has(slot)),
    live:live(),manifestSHA256:manifestSHA,providerAPIsCalledByObserver:false,ownerIdentityVerifiedByObserver:false,policyDocumentsDecodedByObserver:false});}
  function read(slot){if(typeof slot!=='string'||!receipts.has(slot))fail('unknown-slot');const row=receipts.get(slot);
    if(!live()||Date.now()<row.sentAtMS||Date.now()-row.sentAtMS>300000)fail('stale-receipt');return row;}
  Object.defineProperty(globalThis,'fetch',{value:observedFetch,configurable:false,writable:false});
  Object.defineProperty(globalThis,marker,{value:Object.freeze({status,read}),configurable:false,writable:false});
  setTimeout(()=>{closed=true;receipts.clear();for(const stop of stoppers)stop();for(const reader of readers)void reader.cancel().catch(()=>{});},manifest.expiresAtMS-Date.now());
  return status();
}
