// SOURCE ONLY. No clocks, I/O, imports of prior controllers or provider operations.
import { createHash } from 'node:crypto';
export const SETUP_ARN='arn:aws:iam::099256073061:policy/hikyo-158-setup-policy';
export const SETUP_ID='ANPAROHA7PNSTPQUQEUTR';
export const BOUNDARY_ARN='arn:aws:iam::099256073061:policy/hikyo-158-role-boundary';
export const BASELINE_SHA='5d0cb52fdd94cb0bc68db4e2ffc5c5570a62c9a459da7c5399fda68e05e7afd7';
export const APPROVAL_SHA=null;
export const ARCHIVE_SHA='c794a6c61e131c972ea5e201bc6e6189fdb974acea91be362dfb984d490a6c46';
export const OLD_BOUNDARY_ID='ANPAROHA7PNS5QVDJKPIN';
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

const oldHashes = Object.freeze({v2:BASELINE_SHA,
 v3:'fc272c1a9676cdbf4f10e991f10750b4d00fa5ece69d81be0d4f88226dd02537',v4:'c5301c647e710f04e8726713fa474f1b44e19ce1176b1b2e6560478c1db38579',v5:BASELINE_SHA});
const fail = code => { throw new Error('hikyo158-next-fixture-iam-spec:' + code); };
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

const sha=text=>createHash('sha256').update(text).digest('hex');
const esc=value=>encodeURIComponent(value).replace(/[!'()*]/g,char=>'%'+char.charCodeAt(0).toString(16).toUpperCase());
const policyID=value=>typeof value==='string'&&/^ANPA[A-Z0-9]{17}$/.test(value)&&![SETUP_ID,OLD_BOUNDARY_ID].includes(value);
export function buildNextFixtureIAMWire(input){
 const p=exact(input,['operation','document']);canonical(p);
 if(!['CreatePolicyVersion','CreatePolicy','DeletePolicy'].includes(p.operation))fail('operation');
 if(p.operation==='DeletePolicy'&&p.document!==null)fail('delete-document');
 if(p.operation!=='DeletePolicy'&&(!p.document||p.document.Version!=='2012-10-17'||!Array.isArray(p.document.Statement)||!p.document.Statement.length))fail('document');
 const text=p.document===null?null:canonical(p.document);
 if(text!==null&&(text.length>12000||text.length<2))fail('document-size');
 const fields=p.operation==='CreatePolicyVersion'?[['PolicyArn',SETUP_ARN],['PolicyDocument',text],['SetAsDefault','true']]:
  p.operation==='CreatePolicy'?[['PolicyName','hikyo-158-role-boundary'],['PolicyDocument',text],['Description',''],['Tags','']]:[['PolicyArn',BOUNDARY_ARN]];
 const body='Action='+p.operation+'&Version=2010-05-08'+fields.map(([k,v])=>'&'+esc(k)+'='+esc(v)).join('');
 if(Buffer.byteLength(body)>32768)fail('wire-size');return body;
}
export function buildNextFixtureAnalysisWires(document){
 canonical(document);if(!document||document.Version!=='2012-10-17'||!Array.isArray(document.Statement))fail('analysis-document');
 const text=canonical(document);if(text.length>12000)fail('document-size');
 return ['/prod/validate','/prod/policySummary'].map(path=>{const wireBody=JSON.stringify(path==='/prod/validate'?{policy:text,type:'MANAGED'}:{policyDocument:text});
 return Object.freeze({path,wireBody,wireBodyBytes:Buffer.byteLength(wireBody),wireBodySHA256:sha(wireBody)});});
}
function baseProof(proof,owner,now){
 canonical(proof);if(proof.account!=='099256073061'||proof.ownerContextSHA256!==owner||!shaLike(owner)||!Number.isSafeInteger(now)||
 !Number.isSafeInteger(proof.observedAtMS)||proof.observedAtMS>now||proof.observedAtMS<now-300000)fail('fresh-owner-proof');
}
export function prepareNextFixtureIAMOperation(input,explicitNow, reviewedApprovalSHA256){
 const p=exact(input,['operation','approval','proof','ownerContextSHA256','reviewedPreparationSHA256','document','reviewedDocumentSHA256','retirementProofSHA256','newBoundaryCustodySHA256']);
 canonical(p);if((!shaLike(reviewedApprovalSHA256)||digest(p.approval)!==reviewedApprovalSHA256)||!shaLike(p.reviewedPreparationSHA256))fail('approval-review');
 const create=p.operation==='CreatePolicy',activate=p.operation==='CreatePolicyVersion',remove=p.operation==='DeletePolicy';
 if(!create&&!activate&&!remove)fail('operation');baseProof(p.proof,p.ownerContextSHA256,explicitNow);
 let expectedPolicyId=null;
 if(activate){
  exact(p.proof,['schema','account','observedAtMS','ownerContextSHA256','policy']);
  if(p.proof.schema!=='hikyo-158/next-setup-whole-policy-proof/v1')fail('setup-schema');
  const policy=exact(p.proof.policy,['arn','policyId','defaultVersionId','versions']);
  if(policy.arn!==SETUP_ARN||policy.policyId!==SETUP_ID||policy.defaultVersionId!=='v5')fail('setup-identity');
  exact(policy.versions,['v2','v3','v4','v5']);
  for(const id of ['v2','v3','v4','v5']){const row=exact(policy.versions[id],['document','isDefault']);if(row.isDefault!==(id==='v5')||digest(row.document)!==oldHashes[id])fail('whole-setup-v2-v5');}
  if(!p.document||p.document.Version!=='2012-10-17'||!Array.isArray(p.document.Statement)||!p.document.Statement.length)fail('v6-document');
  for(const row of p.document.Statement){if(!['Allow','Deny'].includes(row.Effect))fail('effect');if(row.Effect==='Allow'){
    const actions=Array.isArray(row.Action)?row.Action:[row.Action];if(Object.hasOwn(row,'NotAction')||!actions.length||actions.some(a=>!APPROVED_SETUP_ALLOW_ACTIONS.includes(a)))fail('unapproved-setup-action');}}
  for(const deny of policy.versions.v5.document.Statement.filter(s=>s.Effect==='Deny'))if(!p.document.Statement.some(row=>canonical(row)===canonical(deny)))fail('preserved-denies');
  expectedPolicyId=SETUP_ID;
 }else if(create){
  exact(p.proof,['schema','account','observedAtMS','ownerContextSHA256','arn','exists']);
  if(p.proof.schema!=='hikyo-158/next-boundary-absence-proof/v1'||p.proof.arn!==BOUNDARY_ARN||p.proof.exists!==false)fail('boundary-absence');
 }else{
  exact(p.proof,['schema','account','observedAtMS','ownerContextSHA256','policy','attachments']);
  if(p.proof.schema!=='hikyo-158/next-boundary-zero-use-proof/v1')fail('boundary-schema');
  const policy=exact(p.proof.policy,['arn','policyId','defaultVersionId','versions']);
  if(policy.arn!==BOUNDARY_ARN||!policyID(policy.policyId)||policy.defaultVersionId!=='v1')fail('boundary-identity');
  exact(policy.versions,['v1']);const row=exact(policy.versions.v1,['document','isDefault']);
  if(row.isDefault!==true||digest(row.document)!==p.reviewedDocumentSHA256)fail('boundary-whole-v1');
  const usage=exact(p.proof.attachments,['users','groups','roles','attachmentCount','permissionsBoundaryUsageCount']);
  if(!['users','groups','roles'].every(k=>Array.isArray(usage[k])&&usage[k].length===0)||usage.attachmentCount!==0||usage.permissionsBoundaryUsageCount!==0)fail('boundary-in-use');
  expectedPolicyId=policy.policyId;
 }
 if(!shaLike(p.reviewedDocumentSHA256)||(remove?p.document!==null:digest(p.document)!==p.reviewedDocumentSHA256))fail('reviewed-document');
 if(remove?(!shaLike(p.retirementProofSHA256)||!shaLike(p.newBoundaryCustodySHA256)):(p.retirementProofSHA256!==null||p.newBoundaryCustodySHA256!==null))fail('retirement-custody');
 const wireBody=buildNextFixtureIAMWire({operation:p.operation,document:p.document});
 return Object.freeze({schema:'hikyo-158/next-fixture-iam-operation-spec/v1',operation:p.operation,
 targetARN:activate?SETUP_ARN:BOUNDARY_ARN,expectedPolicyId,currentDefaultVersionId:activate?'v5':remove?'v1':null,targetVersionId:activate?'v6':create?'v1':null,
 wireBody,wireBodyBytes:Buffer.byteLength(wireBody),wireBodySHA256:sha(wireBody),analysisWires:Object.freeze(remove?[]:buildNextFixtureAnalysisWires(p.document)),
 approvalSHA256:reviewedApprovalSHA256,ownerFullProofSHA256:digest(p.proof),proofObservedAtMS:p.proof.observedAtMS,ownerContextSHA256:p.ownerContextSHA256,
 reviewedPreparationSHA256:p.reviewedPreparationSHA256,reviewedDocumentSHA256:p.reviewedDocumentSHA256,retirementProofSHA256:p.retirementProofSHA256,newBoundaryCustodySHA256:p.newBoundaryCustodySHA256,
 maximumMutationForwards:1,executionAuthorization:false,providerCallsMade:false,externalProofAuthenticityVerifiedByThisHelper:false});
}
