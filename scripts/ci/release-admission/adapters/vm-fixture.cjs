'use strict';
// Synthetic Page/CDP/DOM/native XHR only. Never loads a browser/CLI/provider.
const fs=require('node:fs'),path=require('node:path'),vm=require('node:vm'),crypto=require('node:crypto');
if(!process.argv[2]||!path.isAbsolute(process.argv[2]))throw Error('explicit-pinned-test-transport-root-required');
const owner=process.argv[2];
const {z}=require(owner+'/vendor/playwright-core/lib/utilsBundle.js');
const {compile}=require(owner+'/contracts.cjs');const {schemas}=require(owner+'/protocol.cjs');
const registry=schemas(z).manifest.parse(JSON.parse(fs.readFileSync(path.join(__dirname,'stage-registry.json'))));
const guard=JSON.parse(fs.readFileSync(path.join(__dirname,'U-GUARD-REGISTRY.json')));
const sha=b=>crypto.createHash('sha256').update(b).digest('hex');
function fixture(input,options={}){
 const context=vm.createContext({URL,TextEncoder,TextDecoder,Event,Request,Headers,queueMicrotask,
  crypto:{subtle:{digest:async(...args)=>{const result=await crypto.webcrypto.subtle.digest(...args);if(options.afterDigest)options.afterDigest(context);return result;}}},
  setTimeout:(fn,ms)=>{timers.push(ms);return timers.length;},clearTimeout:()=>{},INPUT:input});
 const timers=[],calls=[];let evaluations=0;
 vm.runInContext(`
 globalThis.NOW=INPUT.now;globalThis.Date=class extends Date{static now(){return NOW;}};
 globalThis.location=new URL(INPUT.expected.targetURL);globalThis.__isTangerineBoxHosted__=true;
 globalThis.reads=[];globalThis.clicks=0;globalThis.nativeClickInvocations=0;globalThis.nativeSends=0;globalThis.responseHeaderReads=0;
 globalThis.fetch=function(){throw Error('synthetic-native-fetch-forbidden');};
 globalThis.XMLHttpRequest=class {
   constructor(){this.listeners={};this.responseType='';this.headers={};}
   open(method,url){this.method=method;this.url=url;}
   setRequestHeader(name,value){this.headers[name.toLowerCase()]=value;}
   addEventListener(type,fn,options){(this.listeners[type]??=[]).push({fn,once:options?.once});}
   dispatchEvent(event){for(const item of [...(this.listeners[event.type]||[])]){item.fn.call(this,event);if(item.once)this.listeners[event.type]=this.listeners[event.type].filter(x=>x!==item);}return true;}
   send(){nativeSends++;this.status=200;this.responseURL='https://kms.eu-central-1.amazonaws.com/';this.responseText=INPUT.rawProviderResponse;queueMicrotask(()=>this.dispatchEvent(new Event('loadend')));}
   abort(){}getResponseHeader(name){if(name!=='x-amzn-requestid')throw Error('forbidden-header');responseHeaderReads++;return 'SYNTHETIC-NATIVE-REQUEST-0001';}
 };
 globalThis.document={};
 // Native DOM/CSS behavior is a disposable double, never real UI evidence.
 globalThis.Element=class{
  constructor(){this.ownerDocument=document;this.isConnected=true;this.parentElement=null;this.hidden=false;this.inert=false;this.attributes={};this.css={};}
  getClientRects(){return [{width:100,height:20}];}
  getAttribute(name){return this.attributes[name]??null;}
  contains(node){for(let p=node,depth=0;p&&depth<128;p=p.parentElement,depth++)if(p===this)return true;return false;}
  matches(selector){if(selector!==':disabled')throw Error('unapproved-native-match');if(this.disabled===true)return true;
   for(let p=this.parentElement,depth=0;p&&depth<128;p=p.parentElement,depth++)if(p.tagName==='FIELDSET'&&p.disabled===true&&!(p.firstLegend&&p.firstLegend.contains(this)))return true;
   return false;}
 };
 globalThis.getComputedStyle=node=>({display:'block',visibility:'visible',contentVisibility:'visible',opacity:'1',...node.css});
 globalThis.HTMLElement=class extends Element{click(){nativeClickInvocations++;if(Element.prototype.matches.call(this,':disabled'))return;clicks++;const x=new XMLHttpRequest();x.open('POST','https://kms.eu-central-1.amazonaws.com/',true);x.setRequestHeader('content-type','application/x-amz-json-1.1');x.setRequestHeader('x-amz-target','TrentService.CreateKey');x.send(INPUT.approvedBodyChunks.join(''));}};
 document.documentElement=new Element();document.documentElement.tagName='HTML';
 const node=(text)=>{const value=new Element();value.textContent=text;value.parentElement=document.documentElement;return value;};
 globalThis.button=new HTMLElement();button.tagName='BUTTON';button.type='submit';button.disabled=false;button.textContent='Finish';button.parentElement=document.documentElement;
 globalThis.headings=[node('Review')];globalThis.buttons=[button];globalThis.banner=node('Account 099256073061');
 Object.assign(document,{scripts:[{src:'https://a.b.cdn.console.awsstatic.com/a/v1/B35BNLNKUKKD7F5DTSPWUSOXQ4VAIKKIFWAKR2KD5MB4MCZXQ4RQ/main.js'}],
   querySelector(selector){reads.push(selector);if(selector==='#nav-usernameMenu')return banner;throw Error('unapproved-selector');},
   querySelectorAll(selector){reads.push(selector);if(selector==='header, nav, #awsc-nav-account-menu-button, [data-testid="awsc-nav-account-menu-button"]')return [banner];if(selector==='h1,h2,h3,h4,h5,h6,[role="heading"]')return headings;if(selector==='button')return buttons;throw Error('unapproved-selector');}});
 Object.defineProperty(document,'cookie',{get(){throw Error('forbidden-cookie');}});
 for(const name of ['localStorage','sessionStorage'])Object.defineProperty(globalThis,name,{get(){throw Error('forbidden-storage');}});
 `,context);
 const page={context(){return {newCDPSession:async()=>({send:async method=>{
  calls.push(method);const e=input.expected;const url=context.location.href;
  if(method==='Target.getTargetInfo')return {targetInfo:{targetId:options.targetId||e.targetId,type:'page',url,browserContextId:e.browserContextId}};
  if(method==='Page.getFrameTree')return {frameTree:{frame:{id:e.mainFrameId,loaderId:options.loaderId||e.loaderId,url:url.split('#')[0]}}};throw Error('unapproved-CDP');
 },detach:async()=>{calls.push('detach');if(options.afterDetach)options.afterDetach(context);}})};},
 async evaluate(fn,arg){evaluations++;if(options.beforeEvaluate)options.beforeEvaluate(context,evaluations,fn);
  context.ARG=JSON.stringify(arg);return await vm.runInContext('('+fn.toString()+')(JSON.parse(ARG))',context,{timeout:2000});}};
 return {input,context,page,timers,calls,options};
}
async function run(f,kind,args){
 const entry=kind==='guard'?guard:registry.scripts.find(row=>row.id==='native-create-'+kind+'-v1');
 const raw=fs.readFileSync(kind==='guard'?path.join(__dirname,entry.file):path.join(__dirname,entry.file));
 if(sha(raw)!==entry.sha256)throw Error('source-drift');
 const input=compile(z,entry.arguments).parse(kind==='guard'?args:{attemptTrustBindingSHA256:f.input.attemptTrustBindingSHA256,...args});
 let code=raw.toString();
 if(f.options.mutateInstall&&kind==='install')code=code.replace('const Object=stage.guardObject();','const Object=globalThis.Object;');
 if(f.options.mutateStage&&kind==='install')code=f.options.mutateStage(code);
 if(f.options.debug)code=code.replace("}catch{throw new Error('native-create-stage-held');}","}catch(e){throw e;}");
 const fn=vm.runInNewContext('('+code+')',{}, {timeout:1000});
 const result=await fn(f.page,input);return JSON.parse(JSON.stringify(compile(z,entry.result).parse(result)));
}
async function flush(){for(let n=0;n<8;n++)await new Promise(resolve=>setImmediate(resolve));}
async function bootstrap(f,probe=true){
 const x=f.input;
 const installed=await run(f,'install',{expected:x.expected,window:x.window,manifest:x.manifest,approvedBodyChunks:x.approvedBodyChunks});
 await run(f,'guard',{expected:x.expected,window:x.window,routeId:'kms-create',manifest:x.guardManifest});
 if(probe)vm.runInContext(`{const x=new XMLHttpRequest();x.open('POST','https://kms.eu-central-1.amazonaws.com/',true);x.setRequestHeader('content-type','application/x-amz-json-1.1');x.setRequestHeader('x-amz-target','TrentService.ListKeys');x.send('{}');}`,f.context);
 await flush();return installed;
}
async function status(f,phase){return run(f,'status',{expected:f.input.expected,window:f.input.window,phase,manifest:f.input.manifest});}
module.exports={fixture,run,bootstrap,status,flush,registry,compile,z};
if(require.main===module){
 const readline=require('node:readline');
 (async()=>{let f;for await(const line of readline.createInterface({input:process.stdin,crlfDelay:Infinity})){
  try{const msg=JSON.parse(line);let result;
   if(msg.operation==='bootstrap'){f=fixture(msg.fixture);await bootstrap(f);result=await status(f,'arm');}
   else if(msg.operation==='status')result=await status(f,msg.phase);
   else if(['arm','finish'].includes(msg.operation)){result=await run(f,msg.operation,{expected:f.input.expected,window:f.input.window,phase:msg.operation,descriptor:msg.descriptor});if(msg.operation==='finish'){await flush();result={readout:result,value:vm.runInContext(`({create:__hikyo158NextKeyCustodyObserverV1.read('create'),guard:__hikyo158NativeKMSXHRWriteGuardV1.status()})`,f.context)};}}
   else throw Error('closed-test-operation');
   process.stdout.write(JSON.stringify({ok:true,result})+'\n');
  }catch(error){process.stdout.write(JSON.stringify({ok:false,error:error.message})+'\n');}
 }})().catch(()=>{process.exitCode=1;});
}
