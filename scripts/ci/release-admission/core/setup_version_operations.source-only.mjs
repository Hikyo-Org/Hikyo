// SOURCE ONLY. Pure request/readback preparation. No clock, I/O or provider use.
import { createHash } from 'node:crypto';
export const SETUP_ARN = 'arn:aws:iam::099256073061:policy/hikyo-158-setup-policy';
export const SETUP_POLICY_ID = 'ANPAROHA7PNSTPQUQEUTR';
export const APPROVAL_SHA=null;
export const ARCHIVE_SHA = 'c794a6c61e131c972ea5e201bc6e6189fdb974acea91be362dfb984d490a6c46';
export const BASELINE_SHA = '5d0cb52fdd94cb0bc68db4e2ffc5c5570a62c9a459da7c5399fda68e05e7afd7';
// Closed union of Allow actions from archived approved setup v4 fixture and v5
// self-service documents, whose whole hashes are pinned below. Do not normalize
// case or expand IAM wildcards. Resource/condition/full-v6 review stays external.
export const APPROVED_SETUP_ALLOW_ACTIONS = Object.freeze([
 'iam:ChangePassword', 'iam:CreateOpenIDConnectProvider', 'iam:CreateRole', 'iam:CreateVirtualMFADevice',
 'iam:DeleteOpenIDConnectProvider', 'iam:DeleteRole', 'iam:DeleteRolePolicy', 'iam:DeleteVirtualMFADevice',
 'iam:EnableMFADevice', 'iam:GetAccountPasswordPolicy', 'iam:GetLoginProfile', 'iam:GetOpenIDConnectProvider',
 'iam:GetRole', 'iam:GetRolePolicy', 'iam:GetUser', 'iam:ListAttachedRolePolicies', 'iam:ListMFADevices',
 'iam:ListOpenIDConnectProviderTags', 'iam:ListOpenIDConnectProviders', 'iam:ListRolePolicies', 'iam:ListRoleTags',
 'iam:ListRoles', 'iam:ListVirtualMFADevices', 'iam:PutRolePolicy', 'iam:ResyncMFADevice',
 'iam:TagOpenIDConnectProvider', 'iam:TagRole', 'iam:UpdateAssumeRolePolicy', 'iam:UpdateRole',
 'kms:DescribeKey', 'kms:GetKeyPolicy', 'kms:GetKeyRotationStatus', 'kms:ScheduleKeyDeletion',
 'signin:AuthorizeOAuth2Access', 'signin:CreateOAuth2Token',
]);

const oldHashes = Object.freeze({v1:'75cad189d23a96fa441aca47de0834ec6b8c3dbeac2d19a5d929a485b732e4a8',v2:BASELINE_SHA,
 v3:'fc272c1a9676cdbf4f10e991f10750b4d00fa5ece69d81be0d4f88226dd02537',v4:'c5301c647e710f04e8726713fa474f1b44e19ce1176b1b2e6560478c1db38579',v5:BASELINE_SHA});
const fail = code => { throw new Error('hikyo158-next-setup:' + code); };
const shaLike = value => typeof value === 'string' && /^[a-f0-9]{64}$/.test(value);
function exact(object, fields) {
 if (!object || Object.getPrototypeOf(object)!==Object.prototype || Reflect.ownKeys(object).length!==fields.length) fail('fields');
 const descriptors=Object.getOwnPropertyDescriptors(object);
 if (!fields.every(key=>Object.hasOwn(descriptors,key)&&Object.hasOwn(descriptors[key],'value')&&descriptors[key].enumerable)) fail('data-fields');
 return Object.fromEntries(fields.map(key=>[key,descriptors[key].value]));
}
export function canonical(value) {
 let nodes=0;
 function walk(v,depth) {
  if (++nodes>50000 || depth>30) fail('json-bound');
  if (v===null || typeof v==='boolean' || typeof v==='string') return JSON.stringify(v);
  if (typeof v==='number' && Number.isFinite(v)) return JSON.stringify(v);
  if (Array.isArray(v)) {
   if (Reflect.ownKeys(v).length!==v.length+1 || Array.from({length:v.length},(_,i)=>i).some(i=>!Object.hasOwn(v,i))) fail('json-array');
   return '['+Array.from({length:v.length},(_,i)=>{const d=Object.getOwnPropertyDescriptor(v,String(i));if(!Object.hasOwn(d,'value'))fail('json-accessor');return walk(d.value,depth+1);}).join(',')+']';
  }
  if (!v || Object.getPrototypeOf(v)!==Object.prototype) fail('json-value');
  const keys=Reflect.ownKeys(v);if(keys.some(key=>typeof key!=='string'))fail('json-key');
  const descriptors=Object.getOwnPropertyDescriptors(v);
  return '{'+keys.sort().map(key=>{const d=descriptors[key];if(!Object.hasOwn(d,'value')||!d.enumerable)fail('json-accessor');return JSON.stringify(key)+':'+walk(d.value,depth+1);}).join(',')+'}';
 }
 const result=walk(value,0);if(Buffer.byteLength(result)>131072)fail('json-bytes');return result;
}
export const digest = value => createHash('sha256').update(canonical(value)).digest('hex');
export function buildSetupVersionWire(input) {
 const {operation}=exact(input,['operation']);
 if (!['DeletePolicyVersion','SetDefaultPolicyVersion'].includes(operation)) fail('operation');
 const escape=value=>encodeURIComponent(value).replace(/[!'()*]/g,char=>'%'+char.charCodeAt(0).toString(16).toUpperCase());
 return 'Action='+operation+'&Version=2010-05-08&PolicyArn='+escape(SETUP_ARN)+'&VersionId='+(operation==='DeletePolicyVersion'?'v1':'v5');
}
function inventory(proof, ids, defaultId, reviewedV6SHA, explicitNow) {
 exact(proof,['schema','account','observedAtMS','ownerContextSHA256','policy']);
 if(proof.schema!=='hikyo-158/next-setup-whole-policy-proof/v1'||proof.account!=='099256073061'||!shaLike(proof.ownerContextSHA256)||
  !Number.isInteger(explicitNow)||!Number.isInteger(proof.observedAtMS)||proof.observedAtMS>explicitNow||proof.observedAtMS<explicitNow-300000)fail('fresh-owner-policy-proof');
 const policy=exact(proof.policy,['arn','policyId','defaultVersionId','versions']);
 if(policy.arn!==SETUP_ARN||policy.policyId!==SETUP_POLICY_ID||policy.defaultVersionId!==defaultId)fail('policy-identity-default');
 exact(policy.versions,ids);
 for(const id of ids){const row=exact(policy.versions[id],['document','isDefault']);
  if(row.isDefault!==(id===defaultId)||digest(row.document)!==(id==='v6'?reviewedV6SHA:oldHashes[id]))fail('whole-version-document');
 }
 if(ids.includes('v6')){
  // Complete v6 semantics are independently reviewed from the actual new plan.
  // This independently limits every Allow to the exact approved archive action set.
  const doc=policy.versions.v6.document;
  if(doc.Version!=='2012-10-17'||!Array.isArray(doc.Statement))fail('v6-document');
  for(const row of doc.Statement){
   if(!['Allow','Deny'].includes(row.Effect))fail('v6-statement-effect');
   if(row.Effect!=='Allow')continue;
   if(Object.hasOwn(row,'NotAction'))fail('v6-not-action');
   const actions=Array.isArray(row.Action)?row.Action:[row.Action];
   if(actions.length===0||actions.some(action=>typeof action!=='string'||!APPROVED_SETUP_ALLOW_ACTIONS.includes(action)))fail('setup-unapproved-allow-action');
  }
  const expectedDenies=policy.versions.v5.document.Statement.filter(row=>row.Effect==='Deny');
  for(const deny of expectedDenies)if(!doc.Statement.some(row=>canonical(row)===canonical(deny)))fail('v6-preserved-denies');
 }
 return policy;
}
export function prepareSetupVersionOperation(input, explicitNow, reviewedApprovalSHA256) {
 const p=exact(input,['operation','approval','archive','proof','ownerContextSHA256','reviewedPreparationSHA256','reviewedV6DocumentSHA256','retirementProofSHA256']);
 const deletion=p.operation==='DeletePolicyVersion';
 if(!deletion&&p.operation!=='SetDefaultPolicyVersion')fail('operation');
 if((!shaLike(reviewedApprovalSHA256)||digest(p.approval)!==reviewedApprovalSHA256)||digest(p.archive)!==ARCHIVE_SHA)fail('immutable-approval-archive');
 digest(p.proof);
 if(!shaLike(p.ownerContextSHA256)||!shaLike(p.reviewedPreparationSHA256)||p.proof.ownerContextSHA256!==p.ownerContextSHA256)fail('owner-review-custody');
 if(deletion?(p.reviewedV6DocumentSHA256!==null||p.retirementProofSHA256!==null):(!shaLike(p.reviewedV6DocumentSHA256)||!shaLike(p.retirementProofSHA256)))fail('operation-gates');
 const ids=deletion?['v1','v2','v3','v4','v5']:['v2','v3','v4','v5','v6'];
 inventory(p.proof,ids,deletion?'v5':'v6',p.reviewedV6DocumentSHA256,explicitNow);
 const wireBody=buildSetupVersionWire({operation:p.operation});
 return Object.freeze({schema:'hikyo-158/next-setup-version-operation-spec/v1',operation:p.operation,targetARN:SETUP_ARN,expectedPolicyId:SETUP_POLICY_ID,
  currentDefaultVersionId:deletion?'v5':'v6',targetVersionId:deletion?'v1':'v5',wireBody,wireBodyBytes:Buffer.byteLength(wireBody),
  wireBodySHA256:createHash('sha256').update(wireBody).digest('hex'),ownerFullPolicyProofSHA256:digest(p.proof),proofObservedAtMS:p.proof.observedAtMS,
  archivedV1V5ProofSHA256:ARCHIVE_SHA,ownerContextSHA256:p.ownerContextSHA256,reviewedPreparationSHA256:p.reviewedPreparationSHA256,
  reviewedV6DocumentSHA256:p.reviewedV6DocumentSHA256,retirementProofSHA256:p.retirementProofSHA256,
  expectedAfterVersionIds:Object.freeze(deletion?['v2','v3','v4','v5']:['v2','v3','v4','v5','v6']),expectedAfterDefaultVersionId:'v5',
  maximumMutationForwards:1,executionAuthorization:false,providerCallsMade:false,externalRetirementAuthenticatedByThisHelper:false});
}
export function verifySetupVersionReadback(preparation, before, after, explicitNow) {
 exact(preparation,['schema','operation','targetARN','expectedPolicyId','currentDefaultVersionId','targetVersionId','wireBody','wireBodyBytes','wireBodySHA256',
  'ownerFullPolicyProofSHA256','proofObservedAtMS','archivedV1V5ProofSHA256','ownerContextSHA256','reviewedPreparationSHA256','reviewedV6DocumentSHA256','retirementProofSHA256',
  'expectedAfterVersionIds','expectedAfterDefaultVersionId','maximumMutationForwards','executionAuthorization','providerCallsMade','externalRetirementAuthenticatedByThisHelper']);
 canonical(preparation);
 const expectedWire=buildSetupVersionWire({operation:preparation.operation});
 if(preparation.schema!=='hikyo-158/next-setup-version-operation-spec/v1'||preparation.targetARN!==SETUP_ARN||preparation.expectedPolicyId!==SETUP_POLICY_ID||
  preparation.archivedV1V5ProofSHA256!==ARCHIVE_SHA||preparation.wireBody!==expectedWire||preparation.ownerFullPolicyProofSHA256!==digest(before))fail('prepared-spec');
 const deletion=preparation.operation==='DeletePolicyVersion';
 if(preparation.currentDefaultVersionId!==(deletion?'v5':'v6')||preparation.targetVersionId!==(deletion?'v1':'v5')||
  preparation.wireBodyBytes!==Buffer.byteLength(expectedWire)||preparation.wireBodySHA256!==createHash('sha256').update(expectedWire).digest('hex')||
  preparation.proofObservedAtMS!==before.observedAtMS||preparation.ownerContextSHA256!==before.ownerContextSHA256||!shaLike(preparation.reviewedPreparationSHA256)||
  canonical(preparation.expectedAfterVersionIds)!==canonical(deletion?['v2','v3','v4','v5']:['v2','v3','v4','v5','v6'])||preparation.expectedAfterDefaultVersionId!=='v5'||
  preparation.maximumMutationForwards!==1||preparation.executionAuthorization!==false||preparation.providerCallsMade!==false||preparation.externalRetirementAuthenticatedByThisHelper!==false||
  (deletion?(preparation.reviewedV6DocumentSHA256!==null||preparation.retirementProofSHA256!==null):(!shaLike(preparation.reviewedV6DocumentSHA256)||!shaLike(preparation.retirementProofSHA256))))fail('prepared-spec-semantics');
 inventory(before,deletion?['v1','v2','v3','v4','v5']:['v2','v3','v4','v5','v6'],deletion?'v5':'v6',preparation.reviewedV6DocumentSHA256,explicitNow);
 inventory(after,deletion?['v2','v3','v4','v5']:['v2','v3','v4','v5','v6'],'v5',preparation.reviewedV6DocumentSHA256,explicitNow);
 if(after.observedAtMS<before.observedAtMS||after.ownerContextSHA256!==before.ownerContextSHA256)fail('readback-custody-order');
 for(const id of Object.keys(after.policy.versions))if(canonical(after.policy.versions[id].document)!==canonical(before.policy.versions[id].document))fail('changed-retained-document');
 return Object.freeze({schema:'hikyo-158/next-setup-version-readback-checked/v1',operation:preparation.operation,
  afterWholePolicyProofSHA256:digest(after),observedAtMS:after.observedAtMS,allRetainedDocumentsUnchanged:true,
  targetARN:SETUP_ARN,policyId:SETUP_POLICY_ID,defaultVersionId:'v5',ownerIdentityAuthenticatedByThisHelper:false,providerCallsMade:false});
}
