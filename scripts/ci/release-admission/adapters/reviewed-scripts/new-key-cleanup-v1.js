async(page,input)=>{
 'use strict';
 const action=input.action,nativePins={"next_fixture_iam_operations.source-only.mjs":"3ce2f92883d62c4cc9ba46b0d56def2a7a67fd5a063c514ecf58639b5bfcfa0e","native_next_fixture_iam_guard.source-only.js":"2f79843da8df75977874d1bfdd416f5b3124e875b041c1634ffcb489adae781f","setup_version_operations.source-only.mjs":"dd64d90d4ba15066fe0716092fe692f3df9dad3838af247f54c73617f33e1ec8","native_setup_version_operations_guard.source-only.js":"577ed656f42e56072decf6dc58a0c5c35f4b3dff2062a930e022c1a35ec8944f","native_partial_kms_deletion_guard.source-only.js":"aee32b7cca291edc42a33d78f2cf3831bc281d65a5b2dbd4a407e2ad06ac11be","native_partial_kms_deletion_prepare.source-only.mjs":"bb806cc3090fdab4f46c4ea0acf72fdb3db324e89583bf03174976a8486411fb"};let cdp,handle,before=null,after=null,detail=null;
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
  handle=await page.evaluateHandle(({url,window})=>{const checkSharedWindow=function checkSharedWindow(globalObject, sharedWindow, minimumRemainingMS = 0) {
  const now = globalObject.Date.now();
  if (!sharedWindow || Object.keys(sharedWindow).length !== 2 ||
      !Number.isSafeInteger(sharedWindow.notBeforeMS) || !Number.isSafeInteger(sharedWindow.expiresAtMS) ||
      !Number.isSafeInteger(now) || now < sharedWindow.notBeforeMS || now >= sharedWindow.expiresAtMS ||
      sharedWindow.expiresAtMS > sharedWindow.notBeforeMS + 300000 ||
      ![0, 60000, 120000].includes(minimumRemainingMS) || sharedWindow.expiresAtMS - now < minimumRemainingMS)
    throw new Error('aws-operator-held');
  return now;
};const originalDocument=document;if(location.href!==url)throw Error('held');checkSharedWindow(globalThis,window);return originalDocument;},{url:expected.targetURL,window:{notBeforeMS:m.notBeforeMS,expiresAtMS:m.expiresAtMS}});
  const callback=async(originalDocument,{a,action})=>{
   const checkSharedWindow=function checkSharedWindow(globalObject, sharedWindow, minimumRemainingMS = 0) {
  const now = globalObject.Date.now();
  if (!sharedWindow || Object.keys(sharedWindow).length !== 2 ||
      !Number.isSafeInteger(sharedWindow.notBeforeMS) || !Number.isSafeInteger(sharedWindow.expiresAtMS) ||
      !Number.isSafeInteger(now) || now < sharedWindow.notBeforeMS || now >= sharedWindow.expiresAtMS ||
      sharedWindow.expiresAtMS > sharedWindow.notBeforeMS + 300000 ||
      ![0, 60000, 120000].includes(minimumRemainingMS) || sharedWindow.expiresAtMS - now < minimumRemainingMS)
    throw new Error('aws-operator-held');
  return now;
},cleanupDOM=function cleanupDOM(g,originalDocument,allowedURLs,window,checkSharedWindow){
 const fail=()=>{throw Error('cleanup-public-dom-held');};
 function clock(deadline){if(g.document!==originalDocument||!allowedURLs.includes(g.location.href))fail();const now=checkSharedWindow(g,window);if(deadline!==undefined&&now>=deadline)fail();return now;}
 function read(n,k){clock();const v=n[k];clock();return v;}
 function call(n,k,args=[]){const f=read(n,k);if(typeof f!=='function')fail();clock();const v=Reflect.apply(f,n,args);clock();return v;}
 function list(n,s,cap){const a=call(n,'querySelectorAll',[s]),len=read(a,'length');if(!Number.isSafeInteger(len)||len<0||len>cap)fail();return Array.from({length:len},(_,i)=>read(a,i));}
 function contains(root,n){if(!root||!n||!call(root,'contains',[n]))fail();}
 function protectedControl(n,root,allowedInput=false){
  let x=read(n,'nodeType')===3?read(n,'parentElement'):n;const seen=new Set();
  for(let depth=0;x&&depth<32;depth++){
   if(seen.has(x))fail();seen.add(x);const tag=read(x,'tagName');
   if(['INPUT','TEXTAREA','SELECT','OPTION','OPTGROUP','DATALIST','OUTPUT','FORM','SCRIPT','STYLE','TEMPLATE','IFRAME','OBJECT'].includes(tag)&&!(allowedInput&&x===root&&tag==='INPUT')||tag==='BUTTON'&&x!==root)return true;
   const ce=call(x,'getAttribute',['contenteditable']),role=call(x,'getAttribute',['role']);
   if(read(x,'isContentEditable')===true||ce!==null&&['','true','plaintext-only'].includes(ce.toLowerCase()))return true;
   if(role!==null&&(typeof role!=='string'||role!==role.toLowerCase()||!role||/\s/u.test(role)||['textbox','searchbox','combobox','spinbutton','listbox','option','slider','checkbox','radio','switch'].includes(role)&&!(allowedInput&&x===root&&['spinbutton','checkbox'].includes(role))||role==='button'&&x!==root))return true;
   x=read(x,'parentElement');
  }if(x)fail();return false;
 }
 function qualify(n,root,kind){if(protectedControl(n,n,['number','checkbox'].includes(kind)))fail();contains(root,n);
  const tag=read(n,'tagName'),role=call(n,'getAttribute',['role']);
  if(kind==='button'&&(tag!=='BUTTON'||read(n,'type')!=='button'||![null,'button'].includes(role)))fail();
  if(['number','checkbox'].includes(kind)&&(tag!=='INPUT'||read(n,'type')!==kind||![null,kind==='number'?'spinbutton':'checkbox'].includes(role)||read(n,'readOnly')===true))fail();
  if(kind==='link'&&(tag!=='A'||![null,'link','tab'].includes(role)))fail();
 }
 function visible(n){let x=n;const seen=new Set();const rects=call(n,'getClientRects');if(!read(rects,'length')||read(rects,'length')>32)fail();
  for(let i=0;x&&i<32;i++){
   if(seen.has(x))fail();seen.add(x);if(call(x,'hasAttribute',['hidden'])||call(x,'hasAttribute',['inert'])||call(x,'getAttribute',['aria-hidden'])==='true')fail();
   const s=call(g,'getComputedStyle',[x]),d=read(s,'display'),v=read(s,'visibility'),o=read(s,'opacity');
   if(typeof d!=='string'||typeof v!=='string'||typeof o!=='string'||!o.trim()||!Number.isFinite(Number(o))||Number(o)<=0||Number(o)>1||d==='none'||['hidden','collapse'].includes(v)||read(s,'contentVisibility')==='hidden'||read(s,'pointerEvents')==='none')fail();
   if(call(x,'getAttribute',['aria-disabled'])==='true')fail();x=read(x,'parentElement');
  }if(x)fail();
 }
 function eligible(n,root,kind){qualify(n,root,kind);visible(n);if(call(n,'matches',[':disabled'])||read(n,'disabled')===true)fail();}
 function text(root,max=512){if(protectedControl(root,root))fail();let count=0,out='';
  function walk(n){if(++count>128)fail();const type=read(n,'nodeType');if(protectedControl(n,root))return;
   if(type===3){const value=read(n,'nodeValue');if(typeof value!=='string')fail();out+=value;if(out.length>max)fail();return;}
   if(type!==1)return;visible(n);const children=read(n,'childNodes'),len=read(children,'length');if(!Number.isSafeInteger(len)||len<0||len>64)fail();for(let i=0;i<len;i++)walk(read(children,i));
  }walk(root);if(/[\u0000-\u0008\u000b\u000c\u000e-\u001f\u007f]/u.test(out))fail();return out.trim().replace(/\s+/gu,' ');
 }
 function guard(){clock();const d=originalDocument;
  const headers=list(d,'header, nav, #awsc-nav-account-menu-button, [data-testid="awsc-nav-account-menu-button"]',8);if(!headers.length)fail();let found=false;
  for(const h of headers){if(protectedControl(h,h))fail();const t=text(h),ids=(t.replaceAll('-','').match(/[0-9]+/gu)||[]).filter(x=>x.length===12);if(ids.some(x=>x!=='099256073061'))fail();if(ids.includes('099256073061'))found=true;}
  if(!found)fail();clock();
 }
 // Frozen G reads aggregate candidate headers. Refuse protected descendants first.
 function guardForG(){guard();let visited=0;for(const h of list(originalDocument,'header, nav, #awsc-nav-account-menu-button, [data-testid="awsc-nav-account-menu-button"]',8)){
  function walk(n){if(++visited>256||protectedControl(n,h))fail();if(read(n,'nodeType')!==1)return;const cs=read(n,'childNodes'),len=read(cs,'length');if(!Number.isSafeInteger(len)||len<0||len>64)fail();for(let i=0;i<len;i++)walk(read(cs,i));}walk(h);
 }clock();}
 return {clock,read,call,list,contains,qualify,eligible,text,guard,guardForG,visible,protectedControl,fail};
},cleanupBinding=async function cleanupBinding(g,binding,preparation,nativePins){
 const fail=()=>{throw Error('cleanup-binding-held');};
 const exact=(v,keys)=>{if(!v||typeof v!=='object'||Array.isArray(v)||Object.keys(v).sort().join('|')!==keys.slice().sort().join('|'))fail();for(const k of keys){const d=Object.getOwnPropertyDescriptor(v,k);if(!d||!Object.hasOwn(d,'value'))fail();}};
 const canonical=v=>Array.isArray(v)?'['+v.map(canonical).join(',')+']':v&&typeof v==='object'?'{'+Object.keys(v).sort().map(k=>JSON.stringify(k)+':'+canonical(v[k])).join(',')+'}':JSON.stringify(v);
 const hash=async v=>Array.from(new Uint8Array(await g.crypto.subtle.digest('SHA-256',new TextEncoder().encode(canonical(v)))),v=>v.toString(16).padStart(2,'0')).join('');
 exact(binding,['intent','ticket']);const {intent:i,ticket:t}=binding;
 exact(i,['attemptTrustBindingSHA256','schema','operation','request','maximumForwards','preparationSHA256','originalClaimSHA256','originalHeadSHA256','armedAt','cleanupUntil','reconciliationSHA256','keyCreateCustodySHA256','currentNewKeyMetadataSHA256','oldKeysClosureSHA256','newBoundaryCustodySHA256','reviewedDocumentSHA256','ownerContextSHA256','nativeSourcePins','executionAuthorization']);
 exact(t,['attemptTrustBindingSHA256','schema','operation','request','intentSHA256','issuedAt','maximumForwards','requiredNativePreparation','nativeSourcePins','executionAuthorization','nativeHelperInput']);
 exact(i.request,['KeyId','PendingWindowInDays']);exact(t.request,['KeyId','PendingWindowInDays']);
 if(typeof i.attemptTrustBindingSHA256!=='string'||!/^[a-f0-9]{64}$/u.test(i.attemptTrustBindingSHA256)||i.attemptTrustBindingSHA256!==t.attemptTrustBindingSHA256||i.schema!=='hikyo-158/partial-cleanup-intent/v1'||t.schema!=='hikyo-158/partial-cleanup-native-contract/v1'||i.operation!=='ScheduleKeyDeletion'||t.operation!==i.operation||i.maximumForwards!==1||t.maximumForwards!==1||i.executionAuthorization!==false||t.executionAuthorization!==false||canonical(i.request)!==canonical(t.request)||i.request.PendingWindowInDays!==7||t.intentSHA256!==await hash(i)||canonical(t.requiredNativePreparation)!==canonical(i)||canonical(t.nativeSourcePins)!==canonical(nativePins)||canonical(i.nativeSourcePins)!==canonical(nativePins))fail();
 if(['preparationSHA256','originalClaimSHA256','originalHeadSHA256','reconciliationSHA256','keyCreateCustodySHA256','currentNewKeyMetadataSHA256','oldKeysClosureSHA256','newBoundaryCustodySHA256','ownerContextSHA256'].some(k=>typeof i[k]!=='string'||!/^[a-f0-9]{64}$/u.test(i[k]))||i.reviewedDocumentSHA256!==null&&!/^[a-f0-9]{64}$/u.test(i.reviewedDocumentSHA256)||[i.armedAt,i.cleanupUntil,t.issuedAt].some(v=>typeof v!=='string'||!Number.isFinite(Date.parse(v))))fail();
 const old=['10908b8d-fe54-40e3-9210-a3eefea709b6','990c479f-d673-4bc9-8bca-7b9b86f4b205','fa374267-a456-4e79-b5b1-6cdf356b7d0e','bfa44dc1-ee8a-43b8-aec7-d8a7fbb38db1'],id=i.request.KeyId,prefix='arn:aws:kms:eu-central-1:099256073061:key/';
 if(typeof id!=='string'||!/^[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12}$/u.test(id)||old.includes(id))fail();
 exact(preparation,['manifest','approvedBody']);const m=preparation.manifest;
 const refs={newKeyId:id,newKeyARN:prefix+id,originalCreateCustodySHA256:i.keyCreateCustodySHA256,reviewedCurrentWholeKeyMetadataSHA256:i.currentNewKeyMetadataSHA256,oldKeysClosureSHA256:i.oldKeysClosureSHA256,cleanupIntentSHA256:t.intentSHA256,reviewedPreparationSHA256:i.preparationSHA256,cleanupReconciliationSHA256:i.reconciliationSHA256,guardSourceSHA256:nativePins['native_partial_kms_deletion_guard.source-only.js'],helperSourceSHA256:nativePins['native_partial_kms_deletion_prepare.source-only.mjs'],notBeforeMS:Date.parse(t.issuedAt),expiresAtMS:Math.min(Date.parse(t.issuedAt)+300000,Date.parse(i.cleanupUntil)),captureTimeoutMS:20000};
 if(canonical(t.nativeHelperInput)!==canonical(refs)||Object.entries(refs).some(([k,v])=>m[k]!==v)||preparation.approvedBody!==JSON.stringify({KeyId:id,PendingWindowInDays:7})||m.maximumNativeScheduleForwards!==1||m.maximumNativeDescribeForwardsPerKey!==2||m.schema!=='hikyo-158/partial-kms-deletion-manifest/v1')fail();
 const expected={schema:'hikyo-158/partial-kms-deletion-manifest/v1',account:'099256073061',region:'eu-central-1',endpoint:'https://kms.eu-central-1.amazonaws.com/',publicMainSHA256:'20a50d3f9e11cdae453b0f764689073f2adc6c644e6ac2a08660be5b1b2227a2',...refs,requestBodySHA256:Array.from(new Uint8Array(await g.crypto.subtle.digest('SHA-256',new TextEncoder().encode(preparation.approvedBody))),v=>v.toString(16).padStart(2,'0')).join(''),requestBodyBytes:new TextEncoder().encode(preparation.approvedBody).byteLength,maximumNativeScheduleForwards:1,maximumNativeDescribeForwardsPerKey:2};
 expected.helperPreparationSHA256=await hash(expected);if(canonical(m)!==canonical(expected))fail();
 const window={notBeforeMS:m.notBeforeMS,expiresAtMS:m.expiresAtMS};if(!Number.isSafeInteger(window.notBeforeMS)||!Number.isSafeInteger(window.expiresAtMS)||window.expiresAtMS>Date.parse('2026-10-26T12:22:56Z')||Date.parse(i.armedAt)>Date.parse(t.issuedAt))fail();
 return {id,prefix,old,window,manifestSHA256:await hash(m),intentSHA256:t.intentSHA256,ticketSHA256:await hash(t)};
},cleanupStep=async function cleanupStep(g,originalDocument,a,action,checkSharedWindow,cleanupDOM,cleanupBinding,nativePins,installGuard,installReader){
 const base='https://eu-central-1.console.aws.amazon.com/kms/home?region=eu-central-1';
 const id=a.binding.intent.request.KeyId,schedule=base+'#/kms/keys/scheduleDeletion/'+id,home=base+'#/kms/home',listURL=base+'#/kms/keys';
 // Public URL and original document identity precede every DOM or observer read.
 const m=a.preparation.manifest,w={notBeforeMS:m.notBeforeMS,expiresAtMS:m.expiresAtMS};
 const allowed=action.startsWith('readback')||action==='navigate'?[...a.allowedReadURLs]:['status','poll'].includes(action)?[schedule,...a.allowedReadURLs]:[schedule];
 const dom=cleanupDOM(g,originalDocument,allowed,w,checkSharedWindow);dom.clock(a.deadlineMS);
 const bound=await cleanupBinding(g,a.binding,a.preparation,nativePins);dom.clock(a.deadlineMS);dom.guard();
 const fail=dom.fail;
 function form(){
  if(g.location.href!==schedule)fail();const roots=dom.list(originalDocument,'#schedule-deletion',2);if(roots.length!==1)fail();const root=roots[0];if(dom.protectedControl(root,root))fail();dom.visible(root);
  const headings=dom.list(root,'h1',2);if(headings.length!==1||dom.text(headings[0])!=='Schedule key deletion')fail();
  const sectionHeadings=dom.list(root,'h2',8).map(h=>dom.text(h,100));if(sectionHeadings.filter(t=>t==='Keys to delete').length!==1||sectionHeadings.filter(t=>t==='Confirmation').length!==1)fail();
  const tables=dom.list(root,'table',2);if(tables.length!==1)fail();const table=tables[0];if(dom.protectedControl(table,table))fail();
  const headers=dom.list(table,'thead th',8),names=headers.map(h=>dom.text(h,80)),column=names.indexOf('Key ID');if(column<0||names.lastIndexOf('Key ID')!==column)fail();
  const rows=dom.list(table,'tbody tr',2);if(rows.length!==1)fail();if(dom.protectedControl(rows[0],rows[0]))fail();const cells=dom.list(rows[0],'td',8);if(cells.length!==headers.length||dom.text(cells[column],100)!==id)fail();
  return root;
 }
 function label(root,input){
  const refs=dom.call(input,'getAttribute',['aria-labelledby']);let nodes;
  if(refs!==null){const ids=refs.split(/\s+/u);if(ids.length<1||ids.length>2||new Set(ids).size!==ids.length||ids.some(x=>!/^[A-Za-z0-9_-]{1,128}$/u.test(x)))fail();
   const candidates=dom.list(root,'[id]',128);nodes=ids.map(id=>{const matches=candidates.filter(n=>!dom.protectedControl(n,n)&&dom.call(n,'getAttribute',['id'])===id);if(matches.length!==1)fail();return matches[0];});
  }else{const inputID=dom.call(input,'getAttribute',['id']);if(typeof inputID!=='string'||!/^[A-Za-z0-9_-]{1,128}$/u.test(inputID))fail();nodes=dom.list(root,'label',32).filter(n=>!dom.protectedControl(n,n)&&dom.call(n,'getAttribute',['for'])===inputID);if(nodes.length!==1)fail();}
  for(const n of nodes)dom.contains(root,n);return nodes.map(n=>dom.text(n,512)).join(' ');
 }
 const confirmation=days=>'I confirm that I want to schedule these keys for deletion. The keys will become immediately unusable for cryptographic operations. After the '+days+' day waiting period, the keys will be permanently deleted.';
 function inputs(root,days){const numbers=dom.list(root,'input[type="number"]',2),boxes=dom.list(root,'input[type="checkbox"]',2);if(numbers.length!==1||boxes.length!==1)fail();
  dom.eligible(numbers[0],root,'number');dom.eligible(boxes[0],root,'checkbox');
  if(label(root,numbers[0])!=='Waiting period (in days)'||label(root,boxes[0])!==confirmation(days))fail();return {number:numbers[0],box:boxes[0]};
 }
 function guardObject(){dom.guardForG();const d=Object.getOwnPropertyDescriptor(g,'__hikyo158PartialKMSDeletionGuardV1');if(!d||!Object.hasOwn(d,'value')||d.configurable||d.writable||!Object.isFrozen(d.value))fail();return d.value;}
 function guardStatus(){const guard=guardObject(),s=guard.status();dom.clock(a.deadlineMS);
  const fields=['schema','phase','claimConsumed','forwardsObserved','blocked','pending','error','readySlots','nativeDescribeForwards','newKeyARN','newKeyId','cleanupIntentSHA256','helperPreparationSHA256','guardSourceSHA256','live','providerAPIsInitiatedByGuard','authenticationReadByGuard','externalAuthenticityVerifiedByGuard','writeAuthorityGrantedByGuard','physicalHTTPHopBoundEstablished','coversOtherRealmsOrCapturedTransports','cleanup'];
  if(!s||!Object.isFrozen(s)||fields.some(k=>!Object.hasOwn(Object.getOwnPropertyDescriptor(s,k)??{},'value'))||Object.keys(s).sort().join('|')!==fields.sort().join('|')||s.schema!=='hikyo-158/partial-kms-deletion-status/v1'||s.newKeyId!==id||s.newKeyARN!==bound.prefix+id||s.cleanupIntentSHA256!==bound.intentSHA256||s.helperPreparationSHA256!==m.helperPreparationSHA256||s.guardSourceSHA256!==nativePins['native_partial_kms_deletion_guard.source-only.js']||s.live!==true||!['observing','armed','checking','forwarded','settled','held'].includes(s.phase)||typeof s.claimConsumed!=='boolean'||![0,1].includes(s.forwardsObserved)||!Number.isSafeInteger(s.pending)||s.pending<0||s.pending>5||!Number.isSafeInteger(s.blocked)||s.blocked<0||!Array.isArray(s.readySlots)||s.readySlots.length>6||new Set(s.readySlots).size!==s.readySlots.length||s.error!==null&&(typeof s.error!=='string'||s.error.length>128)||s.cleanup!=='ordinary-document-unload-preserves-session'||!s.nativeDescribeForwards||Object.entries(s.nativeDescribeForwards).some(([k,v])=>!['new.DescribeKey','old1.DescribeKey','old2.DescribeKey','old3.DescribeKey','old4.DescribeKey'].includes(k)||!Number.isSafeInteger(v)||v<1||v>2)||s.readySlots.some(x=>!['schedule','new.DescribeKey','old1.DescribeKey','old2.DescribeKey','old3.DescribeKey','old4.DescribeKey'].includes(x))||['providerAPIsInitiatedByGuard','authenticationReadByGuard','externalAuthenticityVerifiedByGuard','writeAuthorityGrantedByGuard','physicalHTTPHopBoundEstablished','coversOtherRealmsOrCapturedTransports'].some(k=>s[k]!==false))fail();return {guard,s};
 }
 function readSchedule(){const {guard,s}=guardStatus();let row=null;if(s.readySlots.includes('schedule')){row=guard.read('schedule');dom.clock(a.deadlineMS);if(row.manifestSHA256!==bound.manifestSHA256||row.slot!=='schedule'||row.request.KeyId!==id||row.forwardsObserved!==1)fail();}return {status:s,schedule:row};}
 if(action==='inspect'||action==='set-seven'||action==='confirm'){
  const root=form();const number=dom.list(root,'input[type="number"]',2);if(number.length!==1)fail();dom.eligible(number[0],root,'number');if(label(root,number[0])!=='Waiting period (in days)')fail();const value=dom.read(number[0],'value');if(!['30','7'].includes(value))fail();const fields=inputs(root,value);
  if(action==='inspect'){dom.clock();return {ready:true,waitingPeriod:Number(value),confirmed:dom.read(fields.box,'checked')};}
  if(action==='set-seven'){
   if(value==='7'){dom.clock();return {ready:true,localEffect:'retained-seven'};}if(dom.read(fields.box,'checked')!==false)fail();
   const setter=Object.getOwnPropertyDescriptor(g.HTMLInputElement.prototype,'value')?.set;if(typeof setter!=='function')fail();
   const again=inputs(form(),'30');if(again.number!==fields.number||again.box!==fields.box)fail();dom.eligible(fields.number,root,'number');dom.guard();dom.qualify(fields.number,root,'number');dom.clock();
   Reflect.apply(setter,fields.number,['7']);dom.clock();dom.eligible(fields.number,root,'number');dom.clock();dom.call(fields.number,'dispatchEvent',[new g.Event('input',{bubbles:true})]);dom.clock();
   const final=inputs(form(),'7');if(dom.read(final.number,'value')!=='7'||dom.read(final.box,'checked')!==false)fail();dom.clock();return {ready:true,localEffect:'set-seven'};
  }
  if(value!=='7')fail();const checked=dom.read(fields.box,'checked');if(checked===true){dom.clock();return {ready:true,localEffect:'retained-confirmation'};}if(checked!==false)fail();
  const click=dom.read(fields.box,'click'),again=inputs(form(),'7');if(again.box!==fields.box||again.number!==fields.number||dom.read(fields.box,'checked')!==false)fail();dom.eligible(fields.box,root,'checkbox');dom.guard();dom.qualify(fields.box,root,'checkbox');dom.clock();Reflect.apply(click,fields.box,[]);dom.clock();
  const final=inputs(form(),'7');if(final.box!==fields.box||dom.read(final.box,'checked')!==true)fail();dom.clock();return {ready:true,localEffect:'confirmed'};
 }
 if(action==='install'){
  const root=form(),fields=inputs(root,'7');if(dom.read(fields.number,'value')!=='7'||dom.read(fields.box,'checked')!==true)fail();dom.guardForG();dom.clock();const value=await installGuard(a.preparation);dom.clock();return {ready:true,status:value};
 }
 if(action==='arm'){
  const root=form(),fields=inputs(root,'7');if(dom.read(fields.number,'value')!=='7'||dom.read(fields.box,'checked')!==true)fail();const {guard,s}=guardStatus();if(s.phase!=='observing'||s.claimConsumed||s.forwardsObserved||s.pending)fail();dom.guardForG();dom.clock();const value=guard.arm(bound.intentSHA256);dom.clock();return {ready:true,status:value};
 }
 if(action==='schedule'){
  // Refusal-only document capability. Existence burns the UI attempt even if
  // no native send is observed. It never grants authority and has no reset.
  const attemptKey='__hikyo158CleanupUIAttemptV1';
  if(Object.getOwnPropertyDescriptor(originalDocument,attemptKey))fail();
  const root=form(),fields=inputs(root,'7');if(dom.read(fields.number,'value')!=='7'||dom.read(fields.box,'checked')!==true)fail();
  function findButton(){const candidates=dom.list(root,'button',16),matches=[];for(const b of candidates){dom.qualify(b,root,'button');if(dom.text(b,80)==='Schedule deletion')matches.push(b);}if(matches.length!==1)fail();return matches[0];}const button=findButton();dom.eligible(button,root,'button');const click=dom.read(button,'click');
  const again=inputs(form(),'7'),{s}=guardStatus();if(again.number!==fields.number||again.box!==fields.box||dom.read(fields.box,'checked')!==true||s.phase!=='armed'||s.claimConsumed||s.forwardsObserved||s.pending||s.error!==null)fail();
  dom.eligible(button,root,'button');if(dom.text(button,80)!=='Schedule deletion')fail();dom.guardForG();if(findButton()!==button)fail();dom.eligible(button,root,'button');dom.qualify(button,root,'button');
  Object.defineProperty(originalDocument,attemptKey,{value:Object.freeze({schema:'hikyo-158/cleanup-ui-attempt/v1',operation:'ScheduleKeyDeletion',intentSHA256:bound.intentSHA256,ticketSHA256:bound.ticketSHA256,helperPreparationSHA256:m.helperPreparationSHA256,expectedDocumentJSON:JSON.stringify(a.expected),notBeforeMS:w.notBeforeMS,expiresAtMS:w.expiresAtMS,authorityGranted:false}),writable:false,configurable:false});
  const effectNowMS=dom.clock();if(effectNowMS>=w.notBeforeMS+30000)fail();const deadlineMS=Math.min(effectNowMS+2000,w.expiresAtMS,w.notBeforeMS+30000);
  Reflect.apply(click,button,[]);allowed.push(...a.allowedReadURLs);dom.clock(deadlineMS);return {ready:false,deadlineMS,partialEffect:'native-schedule-may-have-forwarded'};
 }
 if(action==='poll'||action==='status'){const result=readSchedule();dom.clock(a.deadlineMS);return {ready:result.schedule!==null&&result.status.pending===0,...result};}
 if(action==='readback-install'){
  if(Object.hasOwn(g,'__hikyo158PartialKMSDeletionGuardV1')||g.location.href!==home||a.readbackManifest.mode!=='readback'||a.readbackManifest.newKeyARN!==bound.prefix+id||a.readbackManifest.preparationSHA256!==m.reviewedPreparationSHA256||a.readbackManifest.notBeforeMS!==w.notBeforeMS||a.readbackManifest.expiresAtMS!==w.expiresAtMS)fail();dom.guardForG();dom.clock();const status=await installReader({manifest:a.readbackManifest,approvedCreateBody:null});dom.clock();return {ready:true,status};
 }
 function reader(){dom.guardForG();const d=Object.getOwnPropertyDescriptor(g,'__hikyo158NextKeyCustodyObserverV1');if(!d||!Object.hasOwn(d,'value')||!Object.isFrozen(d.value)||d.writable||d.configurable)fail();const observer=d.value,s=observer.status();dom.clock();const fields=['schema','mode','readySlots','pendingCaptures','error','providerAPIsCalledByObserver','ownerAuthenticatedByObserver','publicSourceAuthenticatedByObserver','writeAuthorityGrantedByObserver','coversOtherRealmsOrCapturedMethods','physicalHTTPHopBoundEstablished','cleanup'];if(!Object.isFrozen(s)||Object.keys(s).sort().join('|')!==fields.sort().join('|')||fields.some(k=>!Object.hasOwn(Object.getOwnPropertyDescriptor(s,k)??{},'value'))||!Array.isArray(s.readySlots)||s.readySlots.length>8||new Set(s.readySlots).size!==s.readySlots.length||s.readySlots.some(k=>!['new.DescribeKey','new.GetKeyPolicy','new.GetKeyRotationStatus','new.ListAliases','old1.DescribeKey','old2.DescribeKey','old3.DescribeKey','old4.DescribeKey'].includes(k))||['providerAPIsCalledByObserver','ownerAuthenticatedByObserver','publicSourceAuthenticatedByObserver','writeAuthorityGrantedByObserver','coversOtherRealmsOrCapturedMethods','physicalHTTPHopBoundEstablished'].some(k=>s[k]!==false)||s.cleanup!=='full-navigation-controlled-tab-preserves-session'||s.schema!=='hikyo-158/next-key-observer-status/v1'||s.mode!=='readback'||s.error!==null||!Number.isSafeInteger(s.pendingCaptures)||s.pendingCaptures<0||s.pendingCaptures>8)fail();return {observer,s};}
 if(action==='readback-collect'){
  const {observer,s}=reader(),required=['new.DescribeKey','new.GetKeyPolicy','new.GetKeyRotationStatus','new.ListAliases','old1.DescribeKey','old2.DescribeKey','old3.DescribeKey','old4.DescribeKey'];if(!required.includes(a.slot))fail();if(s.pendingCaptures||!s.readySlots.includes(a.slot))return {ready:false,status:s};
  dom.clock();const row=observer.read(a.slot);dom.clock();if(row.manifest.newKeyARN!==bound.prefix+id||row.manifest.notBeforeMS!==w.notBeforeMS||row.manifest.expiresAtMS!==w.expiresAtMS||Object.keys(row.manifest).sort().join('|')!==Object.keys(a.readbackManifest).sort().join('|')||Object.keys(row.manifest).some(k=>row.manifest[k]!==a.readbackManifest[k]))fail();dom.clock();return {ready:true,status:s,row};
 }
 if(action==='navigate'){
  const current=reader();
  if(a.choice==='aliases'){if(current.s.readySlots.includes('new.ListAliases')){dom.clock();return {ready:true,localEffect:'retained-alias-capture'};}if(!current.s.readySlots.includes('new.DescribeKey')||current.s.pendingCaptures)fail();const receipt=current.observer.read('new.DescribeKey');const earliestMS=receipt.receivedAtMS+30001;if(!Number.isSafeInteger(earliestMS))fail();if(dom.clock()<earliestMS)return {ready:false,heldFor:'public-source-alias-cache',earliestNativeAliasClickMS:earliestMS};}
 let target,rootSelector;
  if(a.choice==='managedKeys'){target=listURL;rootSelector='[data-integ="side-nav-container"]';}else if(['new','old1','old2','old3','old4'].includes(a.choice)){const key=a.choice==='new'?id:bound.old[Number(a.choice.slice(3))-1];if(g.location.href!==listURL)fail();target=base+'#/kms/keys/'+key;rootSelector='[data-integ="keys-table-container"]';}else if(['keyPolicy','keyRotation','aliases'].includes(a.choice)){if(!g.location.href.startsWith(base+'#/kms/keys/'+id))fail();target=base+'#/kms/keys/'+id+'/'+a.choice;rootSelector='[data-integ="key-details-container"]';}else fail();
  const scopes=dom.list(originalDocument,rootSelector,2);if(scopes.length!==1)fail();const scope=scopes[0];if(dom.protectedControl(scope,scope))fail();const matches=[];
  for(const link of dom.list(scope,'a[href]',128)){dom.qualify(link,scope,'link');const href=dom.call(link,'getAttribute',['href']),route=target.slice(base.length+1);if(href==='#'+route||['keyPolicy','keyRotation','aliases'].includes(a.choice)&&href===route)matches.push(link);}if(matches.length!==1)fail();const link=matches[0];dom.eligible(link,scope,'link');const click=dom.read(link,'click');const rawHref=dom.call(link,'getAttribute',['href']),route=target.slice(base.length+1);if(rawHref!=='#'+route&&!(a.choice==='keyPolicy'||a.choice==='keyRotation'||a.choice==='aliases')||rawHref!=='#'+route&&rawHref!==route)fail();dom.guard();dom.eligible(link,scope,'link');dom.qualify(link,scope,'link');dom.clock();Reflect.apply(click,link,[]);dom.clock();return {ready:true,targetURL:target,partialEffect:'native-navigation-may-have-started'};
 }
 fail();
},nativePins={"next_fixture_iam_operations.source-only.mjs":"3ce2f92883d62c4cc9ba46b0d56def2a7a67fd5a063c514ecf58639b5bfcfa0e","native_next_fixture_iam_guard.source-only.js":"2f79843da8df75977874d1bfdd416f5b3124e875b041c1634ffcb489adae781f","setup_version_operations.source-only.mjs":"dd64d90d4ba15066fe0716092fe692f3df9dad3838af247f54c73617f33e1ec8","native_setup_version_operations_guard.source-only.js":"577ed656f42e56072decf6dc58a0c5c35f4b3dff2062a930e022c1a35ec8944f","native_partial_kms_deletion_guard.source-only.js":"aee32b7cca291edc42a33d78f2cf3831bc281d65a5b2dbd4a407e2ad06ac11be","native_partial_kms_deletion_prepare.source-only.mjs":"bb806cc3090fdab4f46c4ea0acf72fdb3db324e89583bf03174976a8486411fb"};
   const installGuard=async function installHikyo158PartialKMSDeletionGuardV1(input){
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
},installReader=async function installHikyo158NextKeyCustodyObserverV1(input) {
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
};
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
  const validation=await handle.evaluate(async(originalDocument,{url,window,deadlineMS})=>{const checkSharedWindow=function checkSharedWindow(globalObject, sharedWindow, minimumRemainingMS = 0) {
  const now = globalObject.Date.now();
  if (!sharedWindow || Object.keys(sharedWindow).length !== 2 ||
      !Number.isSafeInteger(sharedWindow.notBeforeMS) || !Number.isSafeInteger(sharedWindow.expiresAtMS) ||
      !Number.isSafeInteger(now) || now < sharedWindow.notBeforeMS || now >= sharedWindow.expiresAtMS ||
      sharedWindow.expiresAtMS > sharedWindow.notBeforeMS + 300000 ||
      ![0, 60000, 120000].includes(minimumRemainingMS) || sharedWindow.expiresAtMS - now < minimumRemainingMS)
    throw new Error('aws-operator-held');
  return now;
};if(globalThis.document!==originalDocument||location.href!==url)throw Error('held');const now=checkSharedWindow(globalThis,window);if(deadlineMS!==undefined&&now>=deadlineMS)throw Error('held');return true;},{url:after.targetURL,window:{notBeforeMS:m.notBeforeMS,expiresAtMS:m.expiresAtMS},deadlineMS:a.deadlineMS});
  if(validation!==true)fail();return result('captured',action==='schedule'?'native-schedule-may-have-forwarded':['set-seven','confirm'].includes(action)?'local-public-input-may-have-changed':action==='navigate'?'native-navigation-may-have-started':'observer-state-may-have-changed');
 }catch{return result('held','effect-uncertain-reconcile-before-any-replay');}finally{
  // A cleanup failure rejects the whole original command; partial capture remains caller custody.
  let failed=false;if(cdp){try{await cdp.detach();}catch{failed=true;}}if(handle){try{await handle.dispose();}catch{failed=true;}}if(failed)throw Error('new-key-cleanup-cleanup-held-reconcile-before-replay');
 }
}
