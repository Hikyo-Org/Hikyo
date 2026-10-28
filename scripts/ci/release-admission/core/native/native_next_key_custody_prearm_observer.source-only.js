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
