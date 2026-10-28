async function installHikyo158IAMWriteResponseObserverV1(input){
 'use strict';
 // SOURCE ONLY. Passive exact native write-response custody; not a write guard.
 const marker='__hikyo158IAMWriteResponseObserverV1',endpoint='https://iam.amazonaws.com/';
 const setup='arn:aws:iam::099256073061:policy/hikyo-158-setup-policy',setupID='ANPAROHA7PNSTPQUQEUTR',boundary='arn:aws:iam::099256073061:policy/hikyo-158-role-boundary';
 const main='https://a.b.cdn.console.awsstatic.com/a/v1/EPYETUDGRYAX3B5DPL5Z2CIJYNSNNCGH7C7BCGW564I7W6F6ZYAQ/main.js';
 const mainSHA='85a9aed5a11dc8d019b95f399a1502b1c8e19affee5c933fc09623d0d359d0f9',vendorsSHA='e4cdd4a45e6d93e7d1e4c512cfa82eaf02bbb040280c93b1b0f20958118b4ba9';
 const families={setup:{marker:'__hikyo158NextSetupVersionGuardV1',schema:'hikyo-158/next-setup-version-write-status/v1',guard:'577ed656f42e56072decf6dc58a0c5c35f4b3dff2062a930e022c1a35ec8944f',helper:'dd64d90d4ba15066fe0716092fe692f3df9dad3838af247f54c73617f33e1ec8'},
  fixture:{marker:'__hikyo158NextFixtureIAMGuardV1',schema:'hikyo-158/next-fixture-iam-write-status/v1',guard:'2f79843da8df75977874d1bfdd416f5b3124e875b041c1634ffcb489adae781f',helper:'3ce2f92883d62c4cc9ba46b0d56def2a7a67fd5a063c514ecf58639b5bfcfa0e'}};
 const refs=['reviewedPreparationSHA256','writePreparationSHA256','controllerIntentSHA256','controllerTicketSHA256','guardManifestSHA256','observerSourceSHA256'];
 const fields=['schema','account','region','operation','targetARN','expectedPolicyId','currentDefaultVersionId','targetVersionId','writeRoute','publicMainSHA256','publicVendorsSHA256','guardSourceSHA256','helperSourceSHA256',...refs,'wireBodySHA256','wireBodyBytes','notBeforeMS','expiresAtMS','captureTimeoutMS'];
 const fail=()=>{throw Error('hikyo158-iam-write-custody-held');};
 function exact(v,ks){if(!v||Object.getPrototypeOf(v)!==Object.prototype||Reflect.ownKeys(v).length!==ks.length)fail();const ds=Object.getOwnPropertyDescriptors(v);
  if(!ks.every(k=>Object.hasOwn(ds,k)&&Object.hasOwn(ds[k],'value')&&ds[k].enumerable))fail();return Object.fromEntries(ks.map(k=>[k,ds[k].value]));}
 const args=exact(input,['manifest','approvedBody']),m=Object.freeze(exact(args.manifest,fields)),start=Date.now();
 const deletion=m.operation==='DeletePolicyVersion',restore=m.operation==='SetDefaultPolicyVersion',activate=m.operation==='CreatePolicyVersion',create=m.operation==='CreatePolicy',remove=m.operation==='DeletePolicy';
 if(!deletion&&!restore&&!activate&&!create&&!remove)fail();const family=deletion||restore?families.setup:families.fixture;
 const sha=v=>typeof v==='string'&&/^[a-f0-9]{64}$/.test(v),newID=v=>typeof v==='string'&&/^ANPA[A-Z0-9]{17}$/.test(v)&&![setupID,'ANPAROHA7PNS5QVDJKPIN'].includes(v);
 if(m.schema!=='hikyo-158/iam-write-response-manifest/v1'||m.account!=='099256073061'||m.region!=='eu-central-1'||m.targetARN!==(deletion||restore||activate?setup:boundary)||
  m.expectedPolicyId!==(create?null:remove?m.expectedPolicyId:setupID)||remove&&!newID(m.expectedPolicyId)||m.currentDefaultVersionId!==(restore?'v6':remove?'v1':create?null:'v5')||
  m.targetVersionId!==(deletion?'v1':restore?'v5':activate?'v6':create?'v1':null)||m.publicMainSHA256!==mainSHA||m.publicVendorsSHA256!==vendorsSHA||m.guardSourceSHA256!==family.guard||m.helperSourceSHA256!==family.helper||
  ![...refs,'wireBodySHA256'].every(k=>sha(m[k]))||typeof args.approvedBody!=='string'||!Number.isInteger(m.wireBodyBytes)||m.wireBodyBytes<32||m.wireBodyBytes>(deletion||restore||remove?512:32768)||
  !Number.isInteger(m.notBeforeMS)||m.notBeforeMS>start||start-m.notBeforeMS>300000||!Number.isInteger(m.expiresAtMS)||m.expiresAtMS<=start||m.expiresAtMS>m.notBeforeMS+300000||
  !Number.isInteger(m.captureTimeoutMS)||m.captureTimeoutMS<1000||m.captureTimeoutMS>20000)fail();
 const esc=v=>encodeURIComponent(v).replace(/[!'()*]/g,c=>'%'+c.charCodeAt(0).toString(16).toUpperCase());
 const prefix='Action='+m.operation+'&Version=2010-05-08&';let expected;
 if(deletion||restore)expected=prefix+'PolicyArn='+esc(setup)+'&VersionId='+m.targetVersionId;
 else if(remove)expected=prefix+'PolicyArn='+esc(boundary);
 else{
  const q=new URLSearchParams(args.approvedBody),keys=activate?['Action','Version','PolicyArn','PolicyDocument','SetAsDefault']:['Action','Version','PolicyName','PolicyDocument','Description','Tags'];
  if(q.size!==keys.length||!keys.every(k=>q.getAll(k).length===1)||q.get('Action')!==m.operation||q.get('Version')!=='2010-05-08'||!q.get('PolicyDocument')||q.get('PolicyDocument').length>12000)fail();
  if(activate){if(q.get('PolicyArn')!==setup||q.get('SetAsDefault')!=='true')fail();expected=prefix+'PolicyArn='+esc(setup)+'&PolicyDocument='+esc(q.get('PolicyDocument'))+'&SetAsDefault=true';}
  else{if(q.get('PolicyName')!=='hikyo-158-role-boundary'||q.get('Description')!==''||q.get('Tags')!=='')fail();expected=prefix+'PolicyName=hikyo-158-role-boundary&PolicyDocument='+esc(q.get('PolicyDocument'))+'&Description=&Tags=';}
 }
 if(args.approvedBody!==expected)fail();
 const policyRoutes=[m.targetARN.replace('/','%2F'),encodeURIComponent(m.targetARN)].flatMap(a=>['','?section=permissions','?section=versions','?section=entities'].map(s=>'#/policies/details/'+a+s));
 const editRoutes=[setup.replace('/','%2F'),encodeURIComponent(setup)].flatMap(a=>['','/0','/1'].flatMap(s=>['','/'].map(t=>'#/policies/details/'+a+'/edit/v5'+s+t)));
 const routes=create?['#/policies/create']:activate?editRoutes:deletion||restore?policyRoutes.filter(r=>r.endsWith('?section=versions')):policyRoutes;
 if(!routes.includes(m.writeRoute))fail();
 const conflicts=[marker,...Object.values(families).map(f=>f.marker),'__hikyo158NativeIAMWriteGuardV1','__hikyo158RecoveryManagedVersionGuardV1','__hikyo158BoundaryDeleteGuardV1','__hikyo158RecoveryMetadataObserverV1','__hikyo158RecoveryMetadataObserverV2'];
 if(conflicts.some(k=>Object.hasOwn(globalThis,k))||typeof fetch!=='function'||typeof Request!=='function'||typeof Headers!=='function'||typeof Response!=='function'||typeof ReadableStream!=='function'||typeof ReadableStreamDefaultReader!=='function'||!crypto?.subtle)fail();
 const nativeFetch=globalThis.fetch,requestClone=Request.prototype.clone,responseClone=Response.prototype.clone,then=Promise.prototype.then;
 const streamGetReader=ReadableStream.prototype.getReader,streamCancel=ReadableStream.prototype.cancel,readerRead=ReadableStreamDefaultReader.prototype.read,readerCancel=ReadableStreamDefaultReader.prototype.cancel;
 const encoderEncode=TextEncoder.prototype.encode,decoderDecode=TextDecoder.prototype.decode,subtle=crypto.subtle,digest=subtle.digest,byteSlice=Uint8Array.prototype.slice,byteSet=Uint8Array.prototype.set;
 const getter=(p,k)=>Object.getOwnPropertyDescriptor(p,k)?.get;
 const reqMethod=getter(Request.prototype,'method'),reqURL=getter(Request.prototype,'url'),reqHeaders=getter(Request.prototype,'headers'),reqBody=getter(Request.prototype,'body');
 const resStatus=getter(Response.prototype,'status'),resURL=getter(Response.prototype,'url'),resBody=getter(Response.prototype,'body'),headerGet=Headers.prototype.get,headerHas=Headers.prototype.has;
 if(![requestClone,responseClone,reqMethod,reqURL,reqHeaders,reqBody,resStatus,resURL,resBody,headerGet,headerHas,then,streamGetReader,streamCancel,readerRead,readerCancel,encoderEncode,decoderDecode,digest,byteSlice,byteSet].every(v=>typeof v==='function')||Object.getOwnPropertyDescriptor(globalThis,'fetch')?.configurable!==true)fail();
 function cancelIntrinsic(fn,receiver){try{Reflect.apply(then,Reflect.apply(fn,receiver,[]),[undefined,()=>{}]);}catch{}}
 const enc=new TextEncoder(),dec=new TextDecoder('utf-8',{fatal:true,ignoreBOM:true}),encode=text=>Reflect.apply(encoderEncode,enc,[text]),wire=encode(expected);
 const hash=async bytes=>Array.from(new Uint8Array(await Reflect.apply(digest,subtle,['SHA-256',bytes])),v=>v.toString(16).padStart(2,'0')).join('');
 const canonical='{'+fields.slice().sort().map(k=>JSON.stringify(k)+':'+JSON.stringify(m[k])).join(',')+'}';
 const manifestSHA256=await hash(encode(canonical));if(wire.byteLength!==m.wireBodyBytes||await hash(wire)!==m.wireBodySHA256)fail();
 let closed=false,claimed=false,pending=0,receipt=null,error=null;const stops=new Set();
 const live=()=>!closed&&Date.now()>=m.notBeforeMS&&Date.now()<m.expiresAtMS;
 function page(){return location.origin==='https://us-east-1.console.aws.amazon.com'&&location.pathname==='/iam/home'&&['','?region=eu-central-1','?region=us-east-1'].includes(location.search)&&
  !!document.querySelector('#app')&&!!document.querySelector('[data-testid="iam-v2-vite"]')&&[main,main.replace('/main.js','/vendors.js')].every(src=>Array.from(document.scripts).some(s=>s.src===src))&&
  Array.from(document.querySelectorAll('header, nav, #awsc-nav-account-menu-button, [data-testid="awsc-nav-account-menu-button"]')).some(n=>[n.textContent,n.getAttribute('aria-label')].some(v=>typeof v==='string'&&/(^|[^0-9])(?:099256073061|0992-5607-3061)([^0-9]|$)/.test(v)));}
 const context=()=>live()&&page();if(!context()||location.hash!==m.writeRoute)fail();
 function guardSnapshot(){
  const gd=Object.getOwnPropertyDescriptor(globalThis,family.marker),fd=Object.getOwnPropertyDescriptor(globalThis,'fetch');
  if(!gd||!Object.hasOwn(gd,'value')||gd.configurable!==false||gd.writable!==false||!Object.isFrozen(gd.value)||!fd||fd.configurable!==false||fd.writable!==false||fd.value===observedFetch)return null;
  const sd=Object.getOwnPropertyDescriptor(gd.value,'status');if(!sd||!Object.hasOwn(sd,'value')||typeof sd.value!=='function')return null;
  const s=Reflect.apply(sd.value,gd.value,[]),ks=['schema','phase','claimConsumed','forwards','operation','targetARN','expectedPolicyId','currentDefaultVersionId','targetVersionId','intentSHA256','wireBodySHA256','reviewedPreparationSHA256','armedUntilMS'];
  if(!s||!ks.every(k=>{const d=Object.getOwnPropertyDescriptor(s,k);return d&&Object.hasOwn(d,'value');}))return null;
  if(s.schema!==family.schema||s.phase!=='forwarded'||s.claimConsumed!==true||s.forwards!==1||s.operation!==m.operation||s.targetARN!==m.targetARN||s.expectedPolicyId!==m.expectedPolicyId||
   s.currentDefaultVersionId!==m.currentDefaultVersionId||s.targetVersionId!==m.targetVersionId||s.intentSHA256!==m.controllerIntentSHA256||s.wireBodySHA256!==m.wireBodySHA256||
   s.reviewedPreparationSHA256!==m.reviewedPreparationSHA256||!Number.isInteger(s.armedUntilMS)||s.armedUntilMS<m.expiresAtMS)return null;
  return Object.freeze(Object.fromEntries(ks.map(k=>[k,s[k]])));
 }
 function protocol(request){const h=Reflect.apply(reqHeaders,request,[]);return Reflect.apply(reqMethod,request,[])==='POST'&&Reflect.apply(reqURL,request,[])===endpoint&&
  Reflect.apply(headerGet,h,['content-type'])==='application/x-www-form-urlencoded'&&!Reflect.apply(headerHas,h,['x-amz-target']);}
 function capture(candidate,result,sentAtMS,guardAtForward){
  pending++;const readers=new Set();let active=true,finishStop;const stopped=new Promise(resolve=>{finishStop=resolve;});
  const stop=()=>{active=false;for(const r of readers)cancelIntrinsic(readerCancel,r);finishStop();};stops.add(stop);
  const timer=setTimeout(()=>{error='capture-timeout-held';stop();},Math.max(1,Math.min(m.captureTimeoutMS,m.expiresAtMS-Date.now())));
  async function bytes(stream,max,exactBody=false){if(!stream)fail();const r=Reflect.apply(streamGetReader,stream,[]);readers.add(r);const chunks=[];let length=0;
   try{while(active&&context()){const p=await Reflect.apply(readerRead,r,[]);if(p.done)break;if(!(p.value instanceof Uint8Array)||length+p.value.byteLength>max)fail();
     if(exactBody)for(let i=0;i<p.value.length;i++)if(p.value[i]!==wire[length+i])fail();length+=p.value.byteLength;chunks.push(Reflect.apply(byteSlice,p.value,[]));}
    if(!active||!context()||length<2||exactBody&&length!==wire.length)fail();const all=new Uint8Array(length);let offset=0;for(const c of chunks){Reflect.apply(byteSet,all,[c,offset]);offset+=c.length;}return all;
   }finally{readers.delete(r);cancelIntrinsic(readerCancel,r);}}
  const scope=bytes(Reflect.apply(reqBody,candidate,[]),wire.length,true);scope.catch(()=>{});
  // Clone the classified guard-forwarded response before the SDK consumes its
  // original stream; do not read cloned response bytes until exact scope holds.
  let cloned=null;
  let response;try{response=Reflect.apply(then,result,[value=>{
   if(!active||!context()||Reflect.apply(resStatus,value,[])!==200||Reflect.apply(resURL,value,[])!==endpoint)return null;
   cloned=Reflect.apply(responseClone,value,[]);return{response:cloned,receivedAtMS:Date.now()};
  },()=>null]);}catch{stop();clearTimeout(timer);stops.delete(stop);pending--;throw Error('capture-registration-held');}response.catch(()=>{});
  void(async()=>{try{
   const [requestBytes,captured]=await Promise.race([Promise.all([scope,response]),stopped.then(()=>{throw Error('bounded-capture-stopped');})]);if(!captured||!active||!context())fail();
   const raw=await bytes(Reflect.apply(resBody,captured.response,[]),32768),rawResponse=Reflect.apply(decoderDecode,dec,[raw]),roundtrip=encode(rawResponse);
   if(roundtrip.byteLength!==raw.byteLength)fail();for(let i=0;i<raw.length;i++)if(roundtrip[i]!==raw[i])fail();
   if(!/^\s*</.test(rawResponse)||/<!DOCTYPE|<!ENTITY/i.test(rawResponse)||/<(?:[A-Za-z_][\w.-]*:)?(?:Password|SecretString|SecretBinary|SecretAccessKey|AccessKeyId|SessionToken|SecurityToken|AccessToken|RefreshToken|IdToken|ClientSecret|Authorization|Token)(?:\s|>|\/)/i.test(rawResponse))fail();
   const row={schema:'hikyo-158/iam-native-write-response/v1',operation:m.operation,targetARN:m.targetARN,expectedPolicyId:m.expectedPolicyId,currentDefaultVersionId:m.currentDefaultVersionId,targetVersionId:m.targetVersionId,
    endpoint,status:200,sentAtMS,receivedAtMS:captured.receivedAtMS,requestBodyBytes:requestBytes.byteLength,requestBodySHA256:await hash(requestBytes),rawResponse,rawResponseBytes:raw.byteLength,rawResponseSHA256:await hash(raw),
    guardAtForward,manifestSHA256,manifest:m,RequestIdParsedByObserver:false,XMLParsedByObserver:false,authenticationReadByObserver:false,providerAPIsInitiatedByObserver:false,externalAuthenticityVerifiedByObserver:false,writeAuthorityGrantedByObserver:false};
   if(!active||!context()||captured.receivedAtMS<sentAtMS||Date.now()<captured.receivedAtMS||Date.now()-sentAtMS>m.captureTimeoutMS||encode(JSON.stringify(row)).byteLength>60000)fail();receipt=Object.freeze(row);
  }catch{error='capture-held-no-replay';}finally{stop();clearTimeout(timer);if(cloned)try{const stream=Reflect.apply(resBody,cloned,[]);if(stream)cancelIntrinsic(streamCancel,stream);}catch{}stops.delete(stop);pending--;}})();
 }
 function observedFetch(request){let candidate=null,guard=null;
  try{if(arguments.length===1&&request instanceof Request&&!claimed&&context()&&location.hash===m.writeRoute&&protocol(request)&&(guard=guardSnapshot())){claimed=true;candidate=Reflect.apply(requestClone,request,[]);}}catch{error='candidate-held';}
  const sentAtMS=Date.now();let result;try{result=Reflect.apply(nativeFetch,this,arguments);}catch(e){if(candidate){error='native-exception-no-replay';try{const stream=Reflect.apply(reqBody,candidate,[]);if(stream)cancelIntrinsic(streamCancel,stream);}catch{}}throw e;}
  if(candidate)try{capture(candidate,result,sentAtMS,guard);}catch{error='capture-registration-held';}
  return result;
 }
 function status(){return Object.freeze({schema:'hikyo-158/iam-write-response-status/v1',operation:m.operation,claimed,pending,receiptReady:receipt!==null,error,live:live(),manifestSHA256,
  providerAPIsInitiatedByObserver:false,authenticationReadByObserver:false,externalAuthenticityVerifiedByObserver:false,writeAuthorityGrantedByObserver:false,coversCapturedFetchOrOtherRealms:false});}
 function read(){if(!context()||!receipt)fail();return receipt;}
 Object.defineProperty(globalThis,'fetch',{value:observedFetch,configurable:true,writable:false});
 Object.defineProperty(globalThis,marker,{value:Object.freeze({status,read}),configurable:false,writable:false});
 setTimeout(()=>{closed=true;receipt=null;for(const stop of stops)stop();},m.expiresAtMS-Date.now());return status();
}
