// SOURCE ONLY. Pure semantic projection after Root authenticates complete originals.
// Never parses XML, authenticates a receipt, changes a native body or calls AWS.
import {createHash} from 'node:crypto';
import {canonical,buildNextFixtureIAMWire,BOUNDARY_ARN,SETUP_ID,OLD_BOUNDARY_ID} from './next_fixture_iam_operations.source-only.mjs';

export const HELPER_SHA256='3ce2f92883d62c4cc9ba46b0d56def2a7a67fd5a063c514ecf58639b5bfcfa0e';
export const GUARD_SHA256='2f79843da8df75977874d1bfdd416f5b3124e875b041c1634ffcb489adae781f';
export const OBSERVER_SHA256='35ef5567fcec9654909c3316cd9cb88dad38b8485eabfe40b2cd344b178a33d7';
const MAIN='85a9aed5a11dc8d019b95f399a1502b1c8e19affee5c933fc09623d0d359d0f9';
const VENDORS='e4cdd4a45e6d93e7d1e4c512cfa82eaf02bbb040280c93b1b0f20958118b4ba9';
const hash=value=>createHash('sha256').update(value).digest('hex');
const fail=()=>{throw new Error('hikyo158-native-boundary-default:closed-projection-refused');};
function exact(value,keys){if(!value||Object.getPrototypeOf(value)!==Object.prototype||Object.keys(value).sort().join('|')!==keys.slice().sort().join('|'))fail();}
function instant(value){if(typeof value!=='string'||!/^\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d(?:\.\d{1,6})?(?:Z|\+00:00)$/.test(value))fail();const ms=Date.parse(value);if(!Number.isFinite(ms)||new Date(ms).toISOString().slice(0,19)!==value.slice(0,19))fail();return ms;}

export function projectNativeBoundaryDefault(input,explicitNowMS){
 canonical(input);exact(input,['helperSourceSHA256','guardSourceSHA256','expectedCustody','expectedInventory','document','nativeBody','observerReceipt','createResponse','getPolicyReadback']);
 if(input.helperSourceSHA256!==HELPER_SHA256||input.guardSourceSHA256!==GUARD_SHA256||!Number.isSafeInteger(explicitNowMS))fail();
 const body=buildNextFixtureIAMWire({operation:'CreatePolicy',document:input.document}),bytes=Buffer.byteLength(body),sha=hash(body);
 if(input.nativeBody!==body||body.includes('&Path=')||!body.endsWith('&Description=&Tags='))fail();
 const row=input.observerReceipt,read=input.getPolicyReadback;exact(read,['observedAtMS','metadata','sourceReceipt','manifest']);
 if(!row||row.schema!=='hikyo-158/iam-native-write-response/v1'||row.operation!=='CreatePolicy'||row.targetARN!==BOUNDARY_ARN||row.expectedPolicyId!==null||row.status!==200||row.endpoint!=='https://iam.amazonaws.com/'||row.requestBodyBytes!==bytes||row.requestBodySHA256!==sha)fail();
 const manifest=row.manifest,guard=row.guardAtForward;
 const refs=['reviewedPreparationSHA256','writePreparationSHA256','controllerIntentSHA256','controllerTicketSHA256','guardManifestSHA256','observerSourceSHA256'];exact(input.expectedCustody,refs);
 if(!manifest||refs.some(key=>typeof input.expectedCustody[key]!=='string'||!/^[a-f0-9]{64}$/.test(input.expectedCustody[key])||manifest[key]!==input.expectedCustody[key])||manifest.observerSourceSHA256!==OBSERVER_SHA256||manifest.operation!=='CreatePolicy'||manifest.targetARN!==BOUNDARY_ARN||manifest.account!=='099256073061'||manifest.region!=='eu-central-1'||manifest.expectedPolicyId!==null||manifest.currentDefaultVersionId!==null||manifest.targetVersionId!=='v1')fail();
 if(!manifest||manifest.guardSourceSHA256!==GUARD_SHA256||manifest.helperSourceSHA256!==HELPER_SHA256||manifest.publicMainSHA256!==MAIN||manifest.publicVendorsSHA256!==VENDORS||manifest.wireBodyBytes!==bytes||manifest.wireBodySHA256!==sha||row.manifestSHA256!==hash(canonical(manifest))||!guard||guard.phase!=='forwarded'||guard.claimConsumed!==true||guard.forwards!==1||guard.wireBodySHA256!==sha)fail();
 if(guard.operation!=='CreatePolicy'||guard.targetARN!==BOUNDARY_ARN||guard.reviewedPreparationSHA256!==manifest.reviewedPreparationSHA256||guard.intentSHA256!==manifest.controllerIntentSHA256)fail();
 for(const name of ['RequestIdParsedByObserver','XMLParsedByObserver','authenticationReadByObserver','providerAPIsInitiatedByObserver','externalAuthenticityVerifiedByObserver','writeAuthorityGrantedByObserver'])if(row[name]!==false)fail();
 if(typeof row.rawResponse!=='string'||Buffer.byteLength(row.rawResponse)!==row.rawResponseBytes||hash(row.rawResponse)!==row.rawResponseSHA256)fail();
 const source=read.sourceReceipt;
 if(!source||source.schema!=='hikyo-158/next-inventory-native-response/v1'||source.service!=='iam'||source.account!=='099256073061'||source.region!=='eu-central-1'||source.action!=='GetPolicy'||source.slot!=='boundary.GetPolicy'||source.status!==200||source.responseEndpoint!=='https://iam.amazonaws.com/'||source.selectors?.PolicyArn!==BOUNDARY_ARN||source.publicMainSHA256!==MAIN||source.publicVendorsSHA256!==VENDORS)fail();
 if(source.reviewedPreparationSHA256!==manifest.reviewedPreparationSHA256)fail();
 const inventory=read.manifest,expected=input.expectedInventory;
 const custodyKeys=['boundPlanSHA256','creationCustodySHA256','newKeyCustodySHA256','ownerContextSHA256'];
 exact(expected,[...custodyKeys,'newKeyARN','boundaryPolicyId','roleIds']);
 exact(inventory,['schema','service','account','region','reviewedPreparationSHA256','publicMainSHA256','publicVendorsSHA256','expiresAtMS','notBeforeMS','stage','newKeyARN','boundaryPolicyId','roleIds',...custodyKeys]);
 if(inventory.schema!=='hikyo-158/next-inventory-observer-manifest/v1'||inventory.service!=='iam'||inventory.stage!=='inventory'||source.stage!=='inventory'||inventory.account!=='099256073061'||inventory.region!=='eu-central-1'||inventory.reviewedPreparationSHA256!==manifest.reviewedPreparationSHA256||inventory.publicMainSHA256!==MAIN||inventory.publicVendorsSHA256!==VENDORS||hash(canonical(inventory))!==source.manifestSHA256)fail();
 if(custodyKeys.some(key=>typeof expected[key]!=='string'||!/^[a-f0-9]{64}$/.test(expected[key])||inventory[key]!==expected[key]||source[key]!==expected[key]))fail();
 const roles=['static','ambient','assume','web','kms-denied','source','oracle','cleanup'].map(name=>'hikyo-158-'+name);exact(inventory.roleIds,roles);
 if(canonical(inventory.roleIds)!==canonical(expected.roleIds)||new Set(Object.values(inventory.roleIds)).size!==8||Object.values(inventory.roleIds).some(id=>typeof id!=='string'||!/^AROA[A-Z0-9]{8,64}$/.test(id)))fail();
 if(inventory.boundaryPolicyId!==expected.boundaryPolicyId||inventory.boundaryPolicyId!==input.createResponse.PolicyId||inventory.newKeyARN!==expected.newKeyARN||!/^arn:aws:kms:eu-central-1:099256073061:key\/[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12}$/.test(inventory.newKeyARN)||['10908b8d-fe54-40e3-9210-a3eefea709b6','990c479f-d673-4bc9-8bca-7b9b86f4b205','fa374267-a456-4e79-b5b1-6cdf356b7d0e','bfa44dc1-ee8a-43b8-aec7-d8a7fbb38db1'].some(id=>inventory.newKeyARN.endsWith('/'+id)))fail();
 if(typeof source.rawResponse!=='string'||Buffer.byteLength(source.rawResponse)!==source.rawResponseBytes||hash(source.rawResponse)!==source.rawResponseSHA256)fail();
 const getBody='Action=GetPolicy&Version=2010-05-08&PolicyArn='+encodeURIComponent(BOUNDARY_ARN);
 if(source.requestBodyBytes!==Buffer.byteLength(getBody)||source.requestBodySHA256!==hash(getBody))fail();
 // Creation is immutable historical custody; only the seal readback is fresh.
 // Full bound/model validation separately enforces the original lifecycle cutoff.
 for(const at of [row.sentAtMS,row.receivedAtMS])if(!Number.isSafeInteger(at)||at>explicitNowMS)fail();
 for(const at of [source.sentAtMS,source.receivedAtMS,read.observedAtMS])if(!Number.isSafeInteger(at)||at>explicitNowMS||at<explicitNowMS-300000)fail();
 if(!Number.isSafeInteger(manifest.notBeforeMS)||!Number.isSafeInteger(manifest.expiresAtMS)||manifest.notBeforeMS>row.sentAtMS||manifest.expiresAtMS<=row.receivedAtMS||manifest.expiresAtMS>manifest.notBeforeMS+300000||!Number.isSafeInteger(manifest.captureTimeoutMS)||manifest.captureTimeoutMS<1000||manifest.captureTimeoutMS>20000||row.receivedAtMS-row.sentAtMS>manifest.captureTimeoutMS)fail();
 if(row.receivedAtMS<row.sentAtMS||source.sentAtMS<row.receivedAtMS||source.receivedAtMS<source.sentAtMS||read.observedAtMS!==source.sentAtMS)fail();
 const p=read.metadata,required=['PolicyName','PolicyId','Arn','Path','DefaultVersionId','AttachmentCount','PermissionsBoundaryUsageCount','IsAttachable','CreateDate','UpdateDate'];
 if(!p||!required.every(k=>Object.hasOwn(p,k))||Object.keys(p).some(k=>!required.concat(['Description','Tags']).includes(k))||p.PolicyName!=='hikyo-158-role-boundary'||p.Arn!==BOUNDARY_ARN||typeof p.PolicyId!=='string'||!/^ANPA[A-Z0-9]{17}$/.test(p.PolicyId)||[SETUP_ID,OLD_BOUNDARY_ID].includes(p.PolicyId)||p.Path!=='/'||p.DefaultVersionId!=='v1'||p.AttachmentCount!=='0'||p.PermissionsBoundaryUsageCount!=='8'||p.IsAttachable!=='true')fail();
 if((Object.hasOwn(p,'Description')&&p.Description!=='')||(Object.hasOwn(p,'Tags')&&!(p.Tags===''||(Array.isArray(p.Tags)&&p.Tags.length===0)))||instant(p.CreateDate)>instant(p.UpdateDate)||instant(p.UpdateDate)>source.receivedAtMS)fail();
 exact(input.createResponse,['Arn','PolicyId','DefaultVersionId']);
 if(input.createResponse.Arn!==p.Arn||input.createResponse.PolicyId!==p.PolicyId||input.createResponse.DefaultVersionId!==p.DefaultVersionId)fail();
 return {schema:'hikyo-158/native-boundary-default-resolution/v1',purpose:'post-provision-seal-creation-custody-only',effectiveRequest:{PolicyName:p.PolicyName,Path:'/',PolicyDocument:structuredClone(input.document)},
  nativeWire:{body,bytes,sha256:sha,pathFieldWasPresent:false,description:'',tags:[]},
  appliedProviderDefault:{field:'Path',value:'/',basis:'IAM CreatePolicy optional Path default, independently corroborated by fresh whole GetPolicy'},
  provenance:{helperSourceSHA256:HELPER_SHA256,guardSourceSHA256:GUARD_SHA256,expectedCustody:structuredClone(input.expectedCustody),expectedInventory:structuredClone(expected),observerReceipt:structuredClone(row),createResponse:structuredClone(input.createResponse),getPolicyReadback:structuredClone(read)},
  XMLMetadataProjectionAuthenticatedByThisCode:false,externalAuthenticityVerifiedByThisCode:false,providerAPIsCalledByThisCode:false,executionAuthorityGranted:false,runtimeReady:false};
}
