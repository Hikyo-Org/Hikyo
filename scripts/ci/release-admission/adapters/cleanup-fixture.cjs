'use strict';
// Entirely synthetic public DOM/CDP/native UI transport, never browser/network.
const vm=require('node:vm'),fs=require('node:fs'),path=require('node:path'),{webcrypto,createHash}=require('node:crypto');
const pins=require('./native-pins.json');const canonical=v=>Array.isArray(v)?'['+v.map(canonical).join(',')+']':v&&typeof v==='object'?'{'+Object.keys(v).sort().map(k=>JSON.stringify(k)+':'+canonical(v[k])).join(',')+'}':JSON.stringify(v);
const digest=v=>createHash('sha256').update(canonical(v)).digest('hex');
const BASE='https://eu-central-1.console.aws.amazon.com/kms/home?region=eu-central-1',ID='12345678-1234-4234-8234-123456789abc',PREFIX='arn:aws:kms:eu-central-1:099256073061:key/',OLD=['10908b8d-fe54-40e3-9210-a3eefea709b6','990c479f-d673-4bc9-8bca-7b9b86f4b205','fa374267-a456-4e79-b5b1-6cdf356b7d0e','bfa44dc1-ee8a-43b8-aec7-d8a7fbb38db1'];
function wireBinding(b){const intent=i=>{const {reviewedDocumentSHA256,nativeSourcePins,...rest}=i;return {...rest,reviewedDocumentSHA256JSON:JSON.stringify(reviewedDocumentSHA256),nativeSourcePinsJSON:canonical(nativeSourcePins)};};const {nativeSourcePins,...t}=b.ticket;return {intent:intent(b.intent),ticket:{...t,requiredNativePreparation:intent(t.requiredNativePreparation),nativeSourcePinsJSON:canonical(nativeSourcePins)}};}
async function fixture(options={}){
 const now=Date.parse('2026-10-23T01:00:00Z'),until=now+180000,id=options.id??ID,arn=PREFIX+id;
 const state={now,url:BASE+(options.readback?'#/kms/home':'#/kms/keys/scheduleDeletion/'+id),loader:'loader-1',clicks:0,localClicks:0,setters:0,requests:[],protectedReads:0,disposals:0,detaches:0,documentReads:0,locationReads:0,clickAtDocumentReads:null,clickAtLocationReads:null,phase:'fresh'};
 const stamp=ms=>new Date(ms).toISOString();
 const intent={attemptTrustBindingSHA256:options.attemptTrustBindingSHA256,schema:'hikyo-158/partial-cleanup-intent/v1',operation:'ScheduleKeyDeletion',request:{KeyId:id,PendingWindowInDays:7},maximumForwards:1,preparationSHA256:'a'.repeat(64),originalClaimSHA256:'b'.repeat(64),originalHeadSHA256:'c'.repeat(64),armedAt:stamp(now),cleanupUntil:stamp(until),reconciliationSHA256:'d'.repeat(64),keyCreateCustodySHA256:'e'.repeat(64),currentNewKeyMetadataSHA256:'f'.repeat(64),oldKeysClosureSHA256:'1'.repeat(64),newBoundaryCustodySHA256:'2'.repeat(64),reviewedDocumentSHA256:null,ownerContextSHA256:'3'.repeat(64),nativeSourcePins:pins,executionAuthorization:false};
 const input={newKeyARN:arn,newKeyId:id,originalCreateCustodySHA256:intent.keyCreateCustodySHA256,reviewedCurrentWholeKeyMetadataSHA256:intent.currentNewKeyMetadataSHA256,oldKeysClosureSHA256:intent.oldKeysClosureSHA256,cleanupIntentSHA256:digest(intent),reviewedPreparationSHA256:intent.preparationSHA256,cleanupReconciliationSHA256:intent.reconciliationSHA256,guardSourceSHA256:pins['native_partial_kms_deletion_guard.source-only.js'],helperSourceSHA256:pins['native_partial_kms_deletion_prepare.source-only.mjs'],notBeforeMS:now,expiresAtMS:until,captureTimeoutMS:20000};
 const ticket={attemptTrustBindingSHA256:options.attemptTrustBindingSHA256,schema:'hikyo-158/partial-cleanup-native-contract/v1',operation:'ScheduleKeyDeletion',request:intent.request,intentSHA256:digest(intent),issuedAt:stamp(now),maximumForwards:1,requiredNativePreparation:intent,nativeSourcePins:pins,executionAuthorization:false,nativeHelperInput:input},binding={intent,ticket};
 const {prepareFromTicket}=await import('./prepare-from-ticket.mjs'),preparation=await prepareFromTicket(binding,now);
 let value='30',confirmed=false,context,alive=true,activeDocument;
 const confirmation=()=>`I confirm that I want to schedule these keys for deletion. The keys will become immediately unusable for cryptographic operations. After the ${value} day waiting period, the keys will be permanently deleted.`;
 class N{
  constructor(tag,attrs={},children=[]){this.tag=tag;this.attrs={...attrs};this.children=[];this.parent=null;this.css={display:'block',visibility:'visible',opacity:'1',pointerEvents:'auto'};for(const n of children)this.append(n);}
  append(n){n.parent=this;this.children.push(n);return n;}
  contains(n){for(let x=n;x;x=x.parent)if(x===this)return true;return false;}
  get nodeType(){return this.tag==='#text'?3:1;}get tagName(){return this.tag;}get parentElement(){return this.parent;}get childNodes(){return this.children;}
  get isContentEditable(){return this.attrs.contenteditable==='true';}get type(){return this.attrs.type??'';}get disabled(){return this.disabledValue===true;}get readOnly(){return this.readOnlyValue===true;}
  get nodeValue(){if(this.trap){state.protectedReads++;throw Error('protected plaintext trap');}return typeof this.text==='function'?this.text():this.text;}
  get textContent(){if(this.trap){state.protectedReads++;throw Error('protected aggregate trap');}return this.tag==='#text'?this.nodeValue:this.children.map(n=>n.textContent).join('');}
  get innerText(){throw Error('aggregate text forbidden');}get value(){state.protectedReads++;throw Error('unrelated input value');}
  getAttribute(k){if(this.trapLabel&&k==='aria-label'){state.protectedReads++;throw Error('protected label trap');}return this.attrs[k]??null;}hasAttribute(k){return Object.hasOwn(this.attrs,k);}
  getClientRects(){return this.zeroRect?[]:[{}];}matches(s){if(s!==':disabled')throw Error('unknown matcher');return this.disabled||this.effectiveDisabled===true;}
  get href(){return new URL(this.attrs.href,state.url).href;}
  querySelectorAll(selector){const all=[];function walk(n){for(const c of n.children){all.push(c);walk(c);}}walk(this);
   const sels=selector.split(', ');return all.filter(n=>sels.some(s=>{
    if(s==='thead th')return n.tag==='TH';if(s==='tbody tr')return n.tag==='TR'&&n.parent.tag==='TBODY';if(s==='[id]')return Object.hasOwn(n.attrs,'id');if(s==='a[href]')return n.tag==='A'&&Object.hasOwn(n.attrs,'href');
    const match=/^input\[type="(number|checkbox)"\]$/u.exec(s);if(match)return n.tag==='INPUT'&&n.attrs.type===match[1];
    const attr=/^\[([^=]+)="([^"]+)"\]$/u.exec(s);if(attr)return n.attrs[attr[1]]===attr[2];if(s.startsWith('#'))return n.attrs.id===s.slice(1);return n.tag===s.toUpperCase();
   }));}
  querySelector(s){return this.querySelectorAll(s)[0]??null;}
 }
 class Input extends N{get value(){if(this!==number)throw Error('non-number input value');return value;}set value(v){if(this!==number)throw Error('non-number input setter');state.setters++;value=v;}get checked(){if(this!==box)throw Error('unrelated boolean');return confirmed;}}
 const text=t=>{const n=new N('#text');n.text=t;return n;},html=new N('HTML'),body=html.append(new N('BODY'));
 const banner=body.append(new N('HEADER',{},[text('099256073061')]));const side=body.append(new N('DIV',{'data-integ':'side-nav-container'}));
 const root=body.append(new N('DIV',{id:'schedule-deletion'}));root.append(new N('H1',{},[text('Schedule key deletion')]));
 root.append(new N('H2',{},[text('Keys to delete')]));root.append(new N('H2',{},[text('Confirmation')]));const table=root.append(new N('TABLE'));const tr=table.append(new N('THEAD')).append(new N('TR'));for(const s of ['Aliases','Key ID','Key usage'])tr.append(new N('TH',{},[text(s)]));
 const tb=table.append(new N('TBODY')),row=tb.append(new N('TR'));row.append(new N('TD',{},[text('')]));const idCell=row.append(new N('TD',{},[text(id)]));row.append(new N('TD',{},[text('ENCRYPT_DECRYPT')]));
 const numberWrapper=root.append(new N('DIV')),numberLabel=numberWrapper.append(new N('LABEL',{id:'period-label',for:'period'},[text('Waiting period (in days)')])),number=numberWrapper.append(new Input('INPUT',{id:'period',type:'number','aria-labelledby':'period-label'}));
 const boxWrapper=root.append(new N('DIV')),boxLabel=boxWrapper.append(new N('SPAN',{id:'confirm-label'},[text(confirmation)])),box=boxWrapper.append(new Input('INPUT',{id:'confirm',type:'checkbox','aria-labelledby':'confirm-label'}));
 number.dispatchEvent=e=>{if(e.type!=='input'||!e.bubbles)throw Error('bad native number event');return true;};
 box.click=()=>{state.localClicks++;confirmed=!confirmed;};
 const buttonWrapper=root.append(new N('DIV')),button=buttonWrapper.append(new N('BUTTON',{type:'button'},[text('Schedule deletion')]));
 const privateEditor=root.append(new N('TEXTAREA'));privateEditor.trap=true;
 const metadata=key=>({AWSAccountId:'099256073061',Arn:PREFIX+key,KeyId:key,CreationDate:(now-10000)/1000,Enabled:key===id,KeyState:key===id?'Enabled':'PendingDeletion',KeyManager:'CUSTOMER',Origin:'AWS_KMS',KeyUsage:'ENCRYPT_DECRYPT',KeySpec:'SYMMETRIC_DEFAULT',MultiRegion:false,...(key!==id?{DeletionDate:(now+86400000)/1000}:{})});
 const keys=options.existingKeys?JSON.parse(JSON.stringify(options.existingKeys)):Object.fromEntries([id,...OLD].map(k=>[k,metadata(k)]));const oldBaseline=JSON.parse(JSON.stringify(Object.fromEntries(OLD.map(k=>[PREFIX+k,keys[k]]))));
 class XHR{
  constructor(){this.listeners={};this.record={};this.code=0;this.raw='';this.url='';}
  open(method,url){this.record={method,url,target:null};}setRequestHeader(name,value){if(name.toLowerCase()==='x-amz-target')this.record.target=value;}
  addEventListener(name,fn){(this.listeners[name]??=[]).push(fn);}getResponseHeader(name){if(name!=='x-amzn-requestid')throw Error('forbidden header');return options.missingRequestID?null:'synthetic-schedule-request-id';}
  get responseType(){return '';}get responseText(){return this.raw;}get response(){return this.raw;}get status(){return this.code;}get responseURL(){return this.url;}
  abort(){this.aborted=true;}dispatchEvent(){return true;}
  send(body){state.requests.push({target:this.record.target,body});const req=JSON.parse(body),action=this.record.target.split('.')[1],key=(req.KeyId??id).split('/').at(-1);let response;
   if(action==='ScheduleKeyDeletion'){if(options.uncertain)return;response={KeyId:options.wrongResponseKey?PREFIX+OLD[0]:arn,KeyState:'PendingDeletion',PendingWindowInDays:options.wrongResponseDays?30:7,DeletionDate:(state.now+7*86400000)/1000};keys[id]={...keys[id],Enabled:false,KeyState:'PendingDeletion',DeletionDate:response.DeletionDate};}
   else if(action==='DescribeKey')response={KeyMetadata:keys[key]};else if(action==='GetKeyPolicy')response={PolicyName:'default',Policy:JSON.stringify({Version:'2012-10-17',Statement:[]})};else if(action==='GetKeyRotationStatus')response={KeyId:arn,KeyRotationEnabled:false};else if(action==='ListAliases')response={Aliases:[],Truncated:false};else response={Keys:[]};
   const xhr=this;setImmediate(()=>{xhr.code=200;xhr.url='https://kms.eu-central-1.amazonaws.com/';xhr.raw=JSON.stringify(response);for(const fn of xhr.listeners.loadend??[])fn();});
  }
 }
 function uiRequest(action,request){const xhr=new context.XMLHttpRequest();xhr.open('POST','https://kms.eu-central-1.amazonaws.com/',true);xhr.setRequestHeader('Content-Type','application/x-amz-json-1.1');xhr.setRequestHeader('X-Amz-Target','TrentService.'+action);xhr.send(JSON.stringify(request));return xhr;}
 button.click=()=>{state.clicks++;state.clickAtDocumentReads=state.documentReads;state.clickAtLocationReads=state.locationReads;if(options.advanceInsideClick)state.now+=options.advanceInsideClick;if(options.redirectOnClick){state.url='https://signin.aws.amazon.com/';return;}if(!confirmed||value!=='7'||options.noNativeRequest)return;
  const xhr=uiRequest('ScheduleKeyDeletion',{KeyId:id,PendingWindowInDays:7});xhr.addEventListener('loadend',()=>{state.url=BASE+'#/kms/keys'+(options.autoDetail?'/'+id:'');if(options.autoDetail)uiRequest('DescribeKey',{KeyId:id});if(options.autoList){try{uiRequest('ListKeysForService',{});}catch{state.blockedAutomaticList=true;}}});
 };
 function link(parent,target,onClick){const a=parent.append(new N('A',{href:target},[text('public navigation')]));a.click=()=>{state.url=target.startsWith('/kms/')?BASE+'#'+target:new URL(target,state.url).href;onClick?.();};return a;}
 link(side,'#/kms/keys',()=>{state.aliasCachedAt=state.now;});
 const keyList=body.append(new N('DIV',{'data-integ':'keys-table-container'})),details=body.append(new N('DIV',{'data-integ':'key-details-container'}));
 for(const key of [id,...OLD])link(keyList,'#/kms/keys/'+key,()=>{if(!options.cachedDetail)uiRequest('DescribeKey',{KeyId:key});if(key===id)uiRequest('GetKeyPolicy',{KeyId:key,PolicyName:'default'});});
 for(const tab of ['keyPolicy','keyRotation','aliases'])link(details,(options.literalTabHrefs?'/kms/keys/':'#/kms/keys/')+id+'/'+tab,()=>{if(tab==='aliases'&&state.now-(state.aliasCachedAt??state.now)<=30000)return;return uiRequest(tab==='keyPolicy'?'GetKeyPolicy':tab==='keyRotation'?'GetKeyRotationStatus':'ListAliases',tab==='keyPolicy'?{KeyId:id,PolicyName:'default'}:{KeyId:id});});
 if(options.bulk)tb.append(new N('TR',{},[new N('TD'),new N('TD',{},[text(OLD[0])]),new N('TD')]));
 if(options.wrongTableKey)idCell.children=[text(OLD[0])];
 if(options.protectedWrapper){const kind=options.protectedWrapper;if(kind==='FORM')buttonWrapper.tag='FORM';else if(kind==='contenteditable')buttonWrapper.attrs.contenteditable='true';else buttonWrapper.attrs.role=kind;button.children[0].trap=true;}
 if(options.buttonRole)button.attrs.role=options.buttonRole;if(options.wrongTag)button.tag='DIV';if(options.wrongType)button.attrs.type='submit';if(options.trapClick)Object.defineProperty(button,'click',{get(){state.protectedReads++;throw Error('protected click getter');}});if(options.hidden)buttonWrapper.css.display='none';if(options.effectiveDisabled)button.effectiveDisabled=true;
 if(options.outsideLabel){number.attrs['aria-labelledby']='outside-label';body.append(new N('SPAN',{id:'outside-label'},[text('Waiting period (in days)')])).trap=true;}
 const source='https://a.b.cdn.console.awsstatic.com/a/v1/B35BNLNKUKKD7F5DTSPWUSOXQ4VAIKKIFWAKR2KD5MB4MCZXQ4RQ/main.js';
 const document={scripts:[{src:source}],querySelectorAll:s=>html.querySelectorAll(s),querySelector:s=>html.querySelector(s),contains:n=>html.contains(n)};activeDocument=document;
 const location={get href(){state.locationReads++;if(options.expireAtLocationRead===state.locationReads)state.now=until;if(options.expireLocation&&state.phase==='effect')state.now=until;return state.url;},get origin(){return new URL(state.url).origin;},get pathname(){return new URL(state.url).pathname;},get search(){return new URL(state.url).search;},get hash(){return new URL(state.url).hash;}};
 class Clock extends Date{static now(){return state.now;}}
 const timers=new Set();const sandbox={get document(){state.documentReads++;if(options.expireAtDocumentRead===state.documentReads)state.now=until;return activeDocument;},location,Date:Clock,crypto:webcrypto,TextEncoder,TextDecoder,Uint8Array,ArrayBuffer,XMLHttpRequest:XHR,HTMLInputElement:Input,__isTangerineBoxHosted__:true,Event:class{constructor(type,x={}){this.type=type;Object.assign(this,x);}},fetch:()=>{throw Error('no fetch transport');},getComputedStyle:n=>n.css,setTimeout:(fn,ms)=>{const t=setTimeout(fn,ms);t.unref();timers.add(t);return t;},clearTimeout:t=>{clearTimeout(t);timers.delete(t);}};
 context=vm.createContext(sandbox);
 const expected=()=>({targetId:'target-1',targetType:'page',targetURL:state.url,browserContextId:'context-1',mainFrameId:'frame-1',loaderId:state.loader,frameURL:state.url,frameURLRelation:'same-url'});
 const cdp={send:async method=>method==='Target.getTargetInfo'?{targetInfo:{targetId:'target-1',type:'page',url:state.url,browserContextId:'context-1'}}:{frameTree:{frame:{id:'frame-1',loaderId:state.loader,url:state.url}}},detach:async()=>{state.detaches++;if(options.switchOnDetach){alive=false;activeDocument={...document};state.loader='loader-2';}if(options.expireOnDetach)state.now=until;}};
 const page={context:()=>({newCDPSession:async()=>cdp}),evaluateHandle:async(fn,arg)=>{context.__arg=JSON.stringify(arg);const doc=await vm.runInContext('('+fn.toString()+')(JSON.parse(__arg))',context);return {evaluate:async(fn,arg)=>{if(!alive)throw Error('original execution context destroyed');context.__arg=JSON.stringify(arg);context.__doc=doc;return vm.runInContext('('+fn.toString()+')(__doc,JSON.parse(__arg))',context);},dispose:async()=>{state.disposals++;}};},waitForTimeout:async ms=>{state.now+=ms;await new Promise(resolve=>setImmediate(resolve));}};
 const readbackManifest={schema:'hikyo-158/next-key-observer-manifest/v1',mode:'readback',account:'099256073061',region:'eu-central-1',endpoint:'https://kms.eu-central-1.amazonaws.com/',publicMainSHA256:'20a50d3f9e11cdae453b0f764689073f2adc6c644e6ac2a08660be5b1b2227a2',preparationSHA256:intent.preparationSHA256,ownerContextSHA256:'3'.repeat(64),intentSHA256:'4'.repeat(64),claimId:'synthetic-create-claim',wireBodySHA256:'5'.repeat(64),wireBodyBytes:500,notBeforeMS:now,expiresAtMS:until,newKeyARN:arn,createReceiptSHA256:'6'.repeat(64)};
 async function run(action,extra={}){const source=options.operatorSource&&action==='schedule'?options.operatorSource:fs.readFileSync(path.join(__dirname,'reviewed-scripts/new-key-cleanup-v1.js'),'utf8'),op=vm.runInContext('('+source+')',context);const args={action,expected:expected(),binding:wireBinding(binding),preparation,readbackManifest,choice:'none',slot:'none',...extra};return op(page,args);}
 async function prepare(){for(const action of ['set-seven','confirm','install','arm']){const r=await run(action);if(r.outcome!=='captured')throw Error('synthetic prepare held at '+action);}}
 async function settle(){for(let i=0;i<40;i++)await new Promise(resolve=>setImmediate(resolve));}
 return {state,binding,preparation,readbackManifest,context,page,run,prepare,settle,oldBaseline,keys,uiRequest,button,number,box,numberWrapper,buttonWrapper,document,dispose:()=>{for(const t of timers)clearTimeout(t);}};
}
module.exports={fixture,wireBinding,canonical,digest,BASE,ID,PREFIX,OLD};
