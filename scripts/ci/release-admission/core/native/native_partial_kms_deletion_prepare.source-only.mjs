// SOURCE ONLY: serialization and reference binding, no I/O, claims or authority.
import {createHash} from 'node:crypto';
const OLD=['10908b8d-fe54-40e3-9210-a3eefea709b6','990c479f-d673-4bc9-8bca-7b9b86f4b205','fa374267-a456-4e79-b5b1-6cdf356b7d0e','bfa44dc1-ee8a-43b8-aec7-d8a7fbb38db1'];
const PREFIX='arn:aws:kms:eu-central-1:099256073061:key/';
const REFS=['originalCreateCustodySHA256','reviewedCurrentWholeKeyMetadataSHA256','oldKeysClosureSHA256','cleanupIntentSHA256','reviewedPreparationSHA256','cleanupReconciliationSHA256','guardSourceSHA256','helperSourceSHA256'];
export function prepareHikyo158PartialKMSDeletion(input,explicitNowMS){
 const fail=()=>{throw Error('hikyo158-partial-kms-preparation-held');};
 const fields=['newKeyARN','newKeyId',...REFS,'notBeforeMS','expiresAtMS','captureTimeoutMS'];
 if(!input||Object.getPrototypeOf(input)!==Object.prototype||Reflect.ownKeys(input).length!==fields.length)fail();
 const ds=Object.getOwnPropertyDescriptors(input);
 if(!fields.every(k=>Object.hasOwn(ds,k)&&Object.hasOwn(ds[k],'value')&&ds[k].enumerable))fail();
 const x=Object.fromEntries(fields.map(k=>[k,ds[k].value]));
 if(typeof x.newKeyId!=='string'||!/^[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12}$/.test(x.newKeyId)||OLD.includes(x.newKeyId)||x.newKeyARN!==PREFIX+x.newKeyId||
  !REFS.every(k=>typeof x[k]==='string'&&/^[0-9a-f]{64}$/.test(x[k]))||!Number.isInteger(explicitNowMS)||!Number.isInteger(x.notBeforeMS)||!Number.isInteger(x.expiresAtMS)||
  x.notBeforeMS>explicitNowMS||explicitNowMS-x.notBeforeMS>300000||x.expiresAtMS<=explicitNowMS||x.expiresAtMS>x.notBeforeMS+300000||
  !Number.isInteger(x.captureTimeoutMS)||x.captureTimeoutMS<1000||x.captureTimeoutMS>20000)fail();
 const approvedBody=JSON.stringify({KeyId:x.newKeyId,PendingWindowInDays:7});
 const hash=s=>createHash('sha256').update(s).digest('hex');
 const manifest={schema:'hikyo-158/partial-kms-deletion-manifest/v1',account:'099256073061',region:'eu-central-1',endpoint:'https://kms.eu-central-1.amazonaws.com/',
  publicMainSHA256:'20a50d3f9e11cdae453b0f764689073f2adc6c644e6ac2a08660be5b1b2227a2',...x,
  requestBodySHA256:hash(approvedBody),requestBodyBytes:Buffer.byteLength(approvedBody),maximumNativeScheduleForwards:1,maximumNativeDescribeForwardsPerKey:2};
 manifest.helperPreparationSHA256=hash('{'+Object.keys(manifest).sort().map(k=>JSON.stringify(k)+':'+JSON.stringify(manifest[k])).join(',')+'}');
 return Object.freeze({manifest:Object.freeze(manifest),approvedBody});
}
