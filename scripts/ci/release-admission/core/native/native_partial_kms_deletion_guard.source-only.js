async function installHikyo158PartialKMSDeletionGuardV1(input){
 'use strict';
 // SOURCE ONLY. One native ScheduleKeyDeletion7 forward. No request initiator.
 const marker='__hikyo158PartialKMSDeletionGuardV1',account='099256073061',region='eu-central-1';
 const endpoint='https://kms.eu-central-1.amazonaws.com/',prefix='arn:aws:kms:'+region+':'+account+':key/';
 const old=['10908b8d-fe54-40e3-9210-a3eefea709b6','990c479f-d673-4bc9-8bca-7b9b86f4b205','fa374267-a456-4e79-b5b1-6cdf356b7d0e','bfa44dc1-ee8a-43b8-aec7-d8a7fbb38db1'];
 const main='https://a.b.cdn.console.awsstatic.com/a/v1/B35BNLNKUKKD7F5DTSPWUSOXQ4VAIKKIFWAKR2KD5MB4MCZXQ4RQ/main.js';
 const mainSHA='20a50d3f9e11cdae453b0f764689073f2adc6c644e6ac2a08660be5b1b2227a2';
 const refs=['originalCreateCustodySHA256','reviewedCurrentWholeKeyMetadataSHA256','oldKeysClosureSHA256','cleanupIntentSHA256','reviewedPreparationSHA256','cleanupReconciliationSHA256','guardSourceSHA256','helperSourceSHA256'];
 const fields=['schema','account','region','endpoint','publicMainSHA256','newKeyARN','newKeyId',...refs,'notBeforeMS','expiresAtMS','captureTimeoutMS','requestBodySHA256','requestBodyBytes','maximumNativeScheduleForwards','maximumNativeDescribeForwardsPerKey','helperPreparationSHA256'];
 const fail=()=>{throw Error('hikyo158-partial-kms-deletion-held');};
 function exact(v,ks){if(!v||Object.getPrototypeOf(v)!==Object.prototype||Reflect.ownKeys(v).length!==ks.length)fail();const ds=Object.getOwnPropertyDescriptors(v);
  if(!ks.every(k=>Object.hasOwn(ds,k)&&Object.hasOwn(ds[k],'value')&&ds[k].enumerable))fail();return Object.fromEntries(ks.map(k=>[k,ds[k].value]));}
 const args=exact(input,['manifest','approvedBody']),m=Object.freeze(exact(args.manifest,fields)),now=Date.now();
 const isSHA=v=>typeof v==='string'&&/^[0-9a-f]{64}$/.test(v);
 if(m.schema!=='hikyo-158/partial-kms-deletion-manifest/v1'||m.account!==account||m.region!==region||m.endpoint!==endpoint||m.publicMainSHA256!==mainSHA||
  typeof m.newKeyId!=='string'||!/^[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12}$/.test(m.newKeyId)||old.includes(m.newKeyId)||m.newKeyARN!==prefix+m.newKeyId||
  ![...refs,'requestBodySHA256','helperPreparationSHA256'].every(k=>isSHA(m[k]))||!Number.isInteger(m.notBeforeMS)||!Number.isInteger(m.expiresAtMS)||
  m.notBeforeMS>now||now-m.notBeforeMS>300000||m.expiresAtMS<=now||m.expiresAtMS>m.notBeforeMS+300000||m.maximumNativeScheduleForwards!==1||m.maximumNativeDescribeForwardsPerKey!==2||
  !Number.isInteger(m.captureTimeoutMS)||m.captureTimeoutMS<1000||m.captureTimeoutMS>20000||typeof args.approvedBody!=='string')fail();
 if(typeof XMLHttpRequest!=='function'||typeof TextEncoder!=='function'||!crypto?.subtle||typeof fetch!=='function')fail();
 const enc=new TextEncoder(),dec=new TextDecoder('utf-8',{fatal:true}),wire=JSON.stringify({KeyId:m.newKeyId,PendingWindowInDays:7});
 const hash=async bytes=>Array.from(new Uint8Array(await crypto.subtle.digest('SHA-256',bytes)),v=>v.toString(16).padStart(2,'0')).join('');
 const canonical=v=>'{'+Object.keys(v).sort().map(k=>JSON.stringify(k)+':'+JSON.stringify(v[k])).join(',')+'}';
 const base=Object.fromEntries(fields.filter(k=>k!=='helperPreparationSHA256').map(k=>[k,m[k]]));
 if(args.approvedBody!==wire||m.requestBodyBytes!==enc.encode(wire).byteLength||await hash(enc.encode(wire))!==m.requestBodySHA256||await hash(enc.encode(canonical(base)))!==m.helperPreparationSHA256)fail();
 const manifestSHA256=await hash(enc.encode(canonical(m)));
 function page(){return location.origin==='https://eu-central-1.console.aws.amazon.com'&&location.pathname==='/kms/home'&&location.search==='?region=eu-central-1'&&globalThis.__isTangerineBoxHosted__===true&&
  Array.from(document.scripts).some(s=>s.src===main)&&Array.from(document.querySelectorAll('header, nav, #awsc-nav-account-menu-button, [data-testid="awsc-nav-account-menu-button"]')).some(n=>[n.textContent,n.getAttribute('aria-label')].some(v=>typeof v==='string'&&/(^|[^0-9])(?:099256073061|0992-5607-3061)([^0-9]|$)/.test(v)));}
 const scheduleRoute=()=>location.hash==='#/kms/keys/scheduleDeletion/'+m.newKeyId;
 const live=()=>Date.now()>=m.notBeforeMS&&Date.now()<m.expiresAtMS;
 const context=()=>live()&&page();
 const conflicts=[marker,'__hikyo158NativeKMSXHRWriteGuardV1','__hikyo158NextKeyCustodyObserverV1','__hikyo158NextInventoryObserverV1','__hikyo158KMSMetadataObserverV1'];
 if(!context()||!scheduleRoute()||conflicts.some(k=>Object.hasOwn(globalThis,k)))fail();
 const p=XMLHttpRequest.prototype,native={open:p.open,header:p.setRequestHeader,send:p.send,abort:p.abort,add:p.addEventListener,get:p.getResponseHeader};
 const responseType=Object.getOwnPropertyDescriptor(p,'responseType')?.get,responseText=Object.getOwnPropertyDescriptor(p,'responseText')?.get;
 const response=Object.getOwnPropertyDescriptor(p,'response')?.get,statusCode=Object.getOwnPropertyDescriptor(p,'status')?.get,responseURL=Object.getOwnPropertyDescriptor(p,'responseURL')?.get;
 if(![native.open,native.header,native.send,native.abort,native.add,native.get,responseType,responseText,response,statusCode,responseURL].every(v=>typeof v==='function')||
  !['open','setRequestHeader','send'].every(k=>Object.getOwnPropertyDescriptor(p,k)?.configurable===true)||Object.getOwnPropertyDescriptor(globalThis,'fetch')?.configurable!==true)fail();
 const state={phase:'observing',claimConsumed:false,forwardsObserved:0,blocked:0,pending:0,error:null};
 const readForwards=new Map();
 const requests=new WeakMap(),receipts=new Map(),claimed=new Set(),stops=new Set();let stored=0;
 const hold=reason=>{state.phase='held';state.error=reason;};
 const reject=()=>{state.blocked++;fail();};
 const readNative=(getter,x)=>Reflect.apply(getter,x,[]);
 function describe(record,body){
  if(record.target!=='TrentService.DescribeKey'||typeof body!=='string')return null;
  for(const [i,id]of[m.newKeyId,...old].entries())for(const value of[id,prefix+id])if(body===JSON.stringify({KeyId:value})){
   const path=location.hash;let decoded;try{decoded=decodeURIComponent(path);}catch{return null;}
   if(![id,prefix+id].some(k=>decoded==='#/kms/keys/'+k||['keyPolicy','cryptographicConfiguration','keyRotation','aliases','tags','keyMaterial'].some(t=>decoded==='#/kms/keys/'+k+'/'+t)))return null;
   return{slot:(i===0?'new':'old'+i)+'.DescribeKey',operation:'DescribeKey',request:{KeyId:value}};
  }return null;
 }
 function observe(xhr,selected,body){
  if(claimed.has(selected.slot)||state.pending>=5)return;
  claimed.add(selected.slot);state.pending++;const sentAtMS=Date.now();let ended=false;
  const finish=()=>{if(!ended){ended=true;clearTimeout(timer);state.pending--;stops.delete(finish);}};
  const timer=setTimeout(()=>{if(!ended)hold('response-timeout-reconcile-no-replay');finish();},Math.max(1,Math.min(m.captureTimeoutMS,m.expiresAtMS-Date.now())));stops.add(finish);
  Reflect.apply(native.add,xhr,['loadend',()=>{if(ended)return;void(async()=>{try{
   if(!context())return;const httpStatus=readNative(statusCode,xhr),url=readNative(responseURL,xhr);
   if(httpStatus!==200||url!==endpoint){hold('response-status-or-endpoint-reconcile-no-replay');return;}
   const receivedAtMS=Date.now(),type=readNative(responseType,xhr);let bytes;
   if(type==='arraybuffer'){const v=readNative(response,xhr);if(!(v instanceof ArrayBuffer)||v.byteLength>32768)fail();bytes=new Uint8Array(v).slice();}
   else if(type===''||type==='text'){const v=readNative(responseText,xhr);if(typeof v!=='string'||v.length>32768)fail();bytes=enc.encode(v);}else fail();
   if(bytes.byteLength<2||bytes.byteLength>32768||stored+bytes.byteLength>163840)fail();const rawResponse=dec.decode(bytes);
   if(/SecretString|SecretBinary|SecretAccessKey|AccessKeyId|SessionToken/.test(rawResponse))fail();
   let requestId=null;if(selected.operation==='ScheduleKeyDeletion'){let v;try{v=Reflect.apply(native.get,xhr,['x-amzn-requestid']);}catch{}
    if(typeof v==='string'&&/^[A-Za-z0-9-]{8,128}$/.test(v))requestId=v;else hold('request-id-unavailable-reconcile-existing-schedule');}
   const row={schema:'hikyo-158/partial-kms-native-response/v1',...selected,request:Object.freeze({...selected.request}),httpStatus,responseEndpoint:url,sentAtMS,receivedAtMS,
    rawResponse,rawResponseBytes:bytes.byteLength,rawResponseSHA256:await hash(bytes),requestBodyBytes:enc.encode(body).byteLength,requestBodySHA256:await hash(enc.encode(body)),
    requestId,requestIdSource:selected.operation==='ScheduleKeyDeletion'?'native-response-header:x-amzn-requestid':null,
    requestIdCaptureState:selected.operation==='ScheduleKeyDeletion'?(requestId===null?'unavailable':'captured'):'not-requested',forwardsObserved:state.forwardsObserved,
    guardSourceSHA256:m.guardSourceSHA256,helperPreparationSHA256:m.helperPreparationSHA256,manifestSHA256,manifest:m,
    JSONParsedByGuard:false,authenticationReadByGuard:false,providerAPIsInitiatedByGuard:false,externalAuthenticityVerifiedByGuard:false,writeAuthorityGrantedByGuard:false};
   if(!context()||ended||receivedAtMS<sentAtMS||Date.now()<receivedAtMS||Date.now()-sentAtMS>m.captureTimeoutMS||enc.encode(JSON.stringify(row)).byteLength>60000)fail();
   stored+=bytes.byteLength;receipts.set(selected.slot,Object.freeze(row));if(selected.slot==='schedule'&&requestId!==null&&state.phase==='forwarded')state.phase='settled';
  }catch{hold('capture-held-reconcile-no-replay');}finally{finish();}})();},{once:true}]);return finish;
 }
 function open(method,url,async){
  if(arguments.length>3||method!=='POST'||url!==endpoint||arguments.length===3&&async!==true)return reject();
  if(requests.get(this)?.sent)return reject();requests.set(this,{type:null,target:null,sent:false});return Reflect.apply(native.open,this,arguments);
 }
 function header(name,value){if(arguments.length!==2)return reject();const r=requests.get(this);if(!r||r.sent||typeof name!=='string')return reject();const k=name.toLowerCase()==='content-type'?'type':name.toLowerCase()==='x-amz-target'?'target':null;
  if(k){if(r[k]!==null||typeof value!=='string'||value.length>128)return reject();r[k]=value;}return Reflect.apply(native.header,this,arguments);
 }
 function error(xhr,reason){hold(reason);try{Reflect.apply(native.abort,xhr,[]);}catch{}try{xhr.dispatchEvent(new Event('error'));}catch{}}
 function send(body){const xhr=this,r=requests.get(xhr);if(arguments.length!==1||!context()||!r||r.sent||r.type!=='application/x-amz-json-1.1')return reject();r.sent=true;
  const read=describe(r,body);if(read&&state.forwardsObserved===1){const count=readForwards.get(read.slot)||0;if(count>=m.maximumNativeDescribeForwardsPerKey)return reject();readForwards.set(read.slot,count+1);if(receipts.has('schedule'))observe(xhr,read,body);return Reflect.apply(native.send,xhr,arguments);}
  if(r.target!=='TrentService.ScheduleKeyDeletion'||state.phase!=='armed'||state.claimConsumed||!scheduleRoute())return reject();
  state.claimConsumed=true;state.phase='checking'; // Burn before body validation, async hashing or any forwarding.
  if(typeof body!=='string'||body!==wire||enc.encode(body).byteLength!==m.requestBodyBytes){error(xhr,'wrong-exact-request-body');return;}
  let done=false;const deadline=Math.min(m.expiresAtMS,Date.now()+m.captureTimeoutMS);
  const timer=setTimeout(()=>{if(!done){done=true;error(xhr,'classification-timeout');}},Math.max(1,deadline-Date.now()));
  void hash(enc.encode(body)).then(digest=>{if(done)return;done=true;clearTimeout(timer);
   if(digest!==m.requestBodySHA256||!context()||!scheduleRoute()||state.phase!=='checking'||Date.now()>=deadline){error(xhr,'classification-or-context-held');return;}
   state.phase='forwarded';state.forwardsObserved=1;const stopCapture=observe(xhr,{slot:'schedule',operation:'ScheduleKeyDeletion',request:{KeyId:m.newKeyId,PendingWindowInDays:7}},body);
   try{Reflect.apply(native.send,xhr,[body]);}catch{if(stopCapture)stopCapture();error(xhr,'native-send-failed-reconcile-no-replay');}
  },()=>{if(!done){done=true;clearTimeout(timer);error(xhr,'classification-hash-held');}});return undefined;
 }
 function status(){return Object.freeze({schema:'hikyo-158/partial-kms-deletion-status/v1',...state,readySlots:[...receipts.keys()].sort(),nativeDescribeForwards:Object.fromEntries(readForwards),newKeyARN:m.newKeyARN,newKeyId:m.newKeyId,
  cleanupIntentSHA256:m.cleanupIntentSHA256,helperPreparationSHA256:m.helperPreparationSHA256,guardSourceSHA256:m.guardSourceSHA256,live:live(),
  providerAPIsInitiatedByGuard:false,authenticationReadByGuard:false,externalAuthenticityVerifiedByGuard:false,writeAuthorityGrantedByGuard:false,
  physicalHTTPHopBoundEstablished:false,coversOtherRealmsOrCapturedTransports:false,cleanup:'ordinary-document-unload-preserves-session'});}
 function arm(intent){if(!context()||!scheduleRoute()||intent!==m.cleanupIntentSHA256||state.phase!=='observing'||state.claimConsumed)fail();state.phase='armed';return status();}
 function read(slot){if(!context()||!receipts.has(slot))fail();return receipts.get(slot);}
 for(const[k,v]of[['open',open],['setRequestHeader',header],['send',send]])Object.defineProperty(p,k,{value:v,configurable:false,writable:false});
 Object.defineProperty(globalThis,'fetch',{value:function(){return reject();},configurable:false,writable:false});
 Object.defineProperty(globalThis,marker,{value:Object.freeze({status,arm,read}),configurable:false,writable:false});
 setTimeout(()=>{hold('expired-reconcile-no-replay');receipts.clear();stored=0;for(const stop of stops)stop();},m.expiresAtMS-Date.now());return status();
}
