'use strict';
// Owned narrowing of V R3's canonical protected ancestry, text and CSS gates.
function cleanupDOM(g,originalDocument,allowedURLs,window,checkSharedWindow){
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
}
module.exports={cleanupDOM};
