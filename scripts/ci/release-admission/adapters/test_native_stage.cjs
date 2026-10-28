'use strict';
// Executes actual pinned R2/Zod compiler and native successors against synthetic
// Page/CDP/DOM/XHR boundaries. No owner, browser, provider or network is started.
const fs=require('node:fs'),path=require('node:path'),assert=require('node:assert/strict');
if(!process.argv[2]||!path.isAbsolute(process.argv[2]))throw Error('explicit-test-transport-root-required');
const transport=process.argv[2],{z}=require(path.join(transport,'vendor/playwright-core/lib/utilsBundle.js'));
const {compile}=require(path.join(transport,'contracts.cjs')),{schemas}=require(path.join(transport,'protocol.cjs'));
let compiled=0;
for(const leaf of ['prepare-registry.json','stage-registry.json','cleanup-registry.json']){
 const registry=schemas(z).manifest.parse(JSON.parse(fs.readFileSync(path.join(__dirname,leaf))));
 for(const row of registry.scripts){
  compile(z,row.arguments);compile(z,row.result);compiled++;
  const source=fs.readFileSync(path.join(__dirname,row.file),'utf8');
  new (require('node:vm').Script)('('+source+')');
 }
}
(async()=>{
 let nativeCases=0;
 if(process.argv[3]){
  const data=JSON.parse(fs.readFileSync(process.argv[3])),{fixture,run,bootstrap}=require('./vm-fixture.cjs');
  const args=d=>({expected:data.fixture.expected,window:data.fixture.window,phase:'arm',descriptor:d});
  const good=fixture(data.fixture);await bootstrap(good);const armed=await run(good,'arm',args(data.arm));
  assert.equal(armed.value.guard.phase,'armed');assert.equal(good.context.clicks,0);nativeCases++;
  const wrong=fixture(data.fixture);await bootstrap(wrong);
  await assert.rejects(run(wrong,'arm',args({...data.arm,attemptTrustBindingSHA256:'0'.repeat(64)})),/native-create-stage-held/u);
  assert.equal(wrong.context.clicks,0);nativeCases++;
  const cleanup=await require('./cleanup-fixture.cjs').fixture({attemptTrustBindingSHA256:data.arm.attemptTrustBindingSHA256});
  try{
   await cleanup.prepare();
   const bad={intent:{...cleanup.binding.intent,attemptTrustBindingSHA256:'0'.repeat(64)},ticket:cleanup.binding.ticket};
   await assert.rejects(require('./binding.cjs').cleanupBinding(globalThis,bad,cleanup.preparation,require('./native-pins.json')),/cleanup-binding-held/u);
   const scheduled=await cleanup.run('schedule');assert.equal(scheduled.outcome,'captured');
   assert.equal(cleanup.state.requests.filter(r=>r.target==='TrentService.ScheduleKeyDeletion').length,1);nativeCases++;
  }finally{cleanup.dispose();}
 }
 process.stdout.write(JSON.stringify({compiled,nativeCases,actualNodeCompilerExecuted:true,persistentOwnerStarted:false,providerCalled:false})+'\n');
})().catch(error=>{process.stderr.write(error.stack+'\n');process.exitCode=1;});
