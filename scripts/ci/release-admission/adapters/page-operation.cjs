'use strict';
async function cleanupStep(g,originalDocument,a,action,checkSharedWindow,cleanupDOM,cleanupBinding,nativePins,installGuard,installReader){
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
}
module.exports={cleanupStep};
