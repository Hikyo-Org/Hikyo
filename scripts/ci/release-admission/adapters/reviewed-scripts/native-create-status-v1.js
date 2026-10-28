async (page,input)=>{
 'use strict';const fail=()=>{throw new Error('native-create-stage-held');};let cdp;
 try {
 if(typeof input.attemptTrustBindingSHA256!=='string'||!/^[a-f0-9]{64}$/u.test(input.attemptTrustBindingSHA256)||input.descriptor&&input.descriptor.attemptTrustBindingSHA256!==input.attemptTrustBindingSHA256)fail();
 const expected=input.expected,url="https://eu-central-1.console.aws.amazon.com/kms/home?region=eu-central-1#/kms/keys/create",window=input.window;
 if(expected.targetURL!==url)fail();
 cdp=await page.context().newCDPSession(page);
 const identity=async()=>{const {targetInfo:t}=await cdp.send('Target.getTargetInfo');const {frameTree:{frame:f}}=await cdp.send('Page.getFrameTree');
 const value={targetId:t.targetId,targetType:t.type,targetURL:t.url,browserContextId:t.browserContextId,mainFrameId:f.id,loaderId:f.loaderId,frameURL:f.url,frameURLRelation:f.url===t.url?'same-url':t.url.includes('#')&&f.url===t.url.split('#')[0]?'target-url-without-fragment':'unrelated'};
 if(f.parentId||Object.keys(expected).length!==8||Object.keys(value).some(k=>value[k]!==expected[k]))fail();return value;};
 const before=await identity();let captured;

 captured=await page.evaluate(({window,manifest,phase})=>{const s=globalThis.__hikyo158NativeCreateStageV1;if(!s)throw Error('native-create-stage-held');s.bind(window,manifest);const value=s.read(phase);return {readAtMS:Date.now(),value};},{window,manifest:input.manifest,phase:input.phase});
 const after=await identity();await cdp.detach();cdp=null;
 await page.evaluate(({window,descriptor})=>{const s=globalThis.__hikyo158NativeCreateStageV1;if(!s)throw Error('native-create-stage-held');s.gate();if(descriptor)s.time(descriptor);},{window,descriptor:input.descriptor||null});
 return {schema:'hikyo-158/native-create-stage-readout/v1',kind:"status",phase:input.phase||'install',before,after,readAtMS:captured.readAtMS,value:captured.value,controlLeaseClaimed:false,exclusiveControlVerifiedByAdapter:false,writeAuthorityGranted:false,providerOutcomeProven:false};
 }catch{throw new Error('native-create-stage-held');}finally{if(cdp)await cdp.detach();}
}
