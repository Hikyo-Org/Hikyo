"""SOURCE ONLY: partial-abort public metadata and truthful inventory facts.

No launcher, clock default, credentials, browser, provider client or controller.
Raw receipt validation copies the closed wire/namespace mechanisms from reviewed
S/normalize_next_inventory and N/normalize_setup_whole_reads, without importing
or executing historical controllers. Root authenticates all injected sources.
"""
import copy
from datetime import datetime, timedelta, timezone
import hashlib
import itertools
import json
import re
from urllib.parse import quote
import xml.etree.ElementTree as ET

ACCOUNT='099256073061'; REGION='eu-central-1'; USER='hikyo-158-setup'
SETUP=f'arn:aws:iam::{ACCOUNT}:policy/hikyo-158-setup-policy'
BOUNDARY=f'arn:aws:iam::{ACCOUNT}:policy/hikyo-158-role-boundary'
PROVIDER=f'arn:aws:iam::{ACCOUNT}:oidc-provider/token.actions.githubusercontent.com'
ROLES=tuple('hikyo-158-'+s for s in ('static','ambient','assume','web','kms-denied','source','oracle','cleanup'))
MAIN='85a9aed5a11dc8d019b95f399a1502b1c8e19affee5c933fc09623d0d359d0f9'; VENDORS='e4cdd4a45e6d93e7d1e4c512cfa82eaf02bbb040280c93b1b0f20958118b4ba9'
USER_ACTIONS=('GetUser','ListMFADevices','ListAttachedUserPolicies','ListUserPolicies','ListGroupsForUser','ListAccessKeys')
MANIFEST={'schema','service','account','region','reviewedPreparationSHA256','publicMainSHA256','publicVendorsSHA256','expiresAtMS','notBeforeMS','stage','newKeyARN','boundaryPolicyId','roleIds','boundPlanSHA256','creationCustodySHA256','newKeyCustodySHA256','ownerContextSHA256'}
ROW={'schema','service','stage','account','region','manifestSHA256','notBeforeMS','expiresAtMS','boundPlanSHA256','creationCustodySHA256','newKeyCustodySHA256','ownerContextSHA256','slot','action','requestBodySHA256','requestBodyBytes','selectors','pageMarkerPresent','pageMarkerSHA256','status','responseEndpoint','sentAtMS','receivedAtMS','rawResponseBytes','rawResponseSHA256','rawResponse','reviewedPreparationSHA256','publicMainSHA256','publicVendorsSHA256','JSONParsedByThisCode','providerAPIsCalledByThisCode','ownerProofComplete','nativeWriteAuthorizationOwnedByObserver'}
FLAGS=('JSONParsedByThisCode','providerAPIsCalledByThisCode','ownerProofComplete','nativeWriteAuthorizationOwnedByObserver')
NS='{https://iam.amazonaws.com/doc/2010-05-08/}'


class StrictNamespaceBuilder(ET.TreeBuilder):
    def start_ns(self,prefix,uri): require(uri==NS[1:-1],'foreign-namespace-declaration')


def require(value,reason):
    if not value: raise ValueError('partial-metadata:'+reason)


def exact(value,fields): require(type(value) is dict and set(value)==set(fields),'closed-fields')


def sha(value): return hashlib.sha256(value).hexdigest()


def canonical(value): return json.dumps(value,sort_keys=True,separators=(',',':'),ensure_ascii=False,allow_nan=False).encode()


def epoch_ms(value):
    require(type(value) is datetime and value.tzinfo is not None and value.utcoffset()==timedelta(0),'explicit-utc-clock')
    delta=value-datetime(1970,1,1,tzinfo=timezone.utc)
    return delta.days*86400000+delta.seconds*1000+delta.microseconds//1000


def stamp_ms(value): return (datetime(1970,1,1,tzinfo=timezone.utc)+timedelta(milliseconds=value)).isoformat(timespec='milliseconds').replace('+00:00','Z')


def xml(raw,action):
    """Whole IAM XML metadata only; declarations, attributes and mixed data hold."""
    require(type(raw) is str and 2<=len(raw)<=32768 and len(raw.encode())<=32768,'bounded-xml')
    remainder=re.sub(r'^\ufeff?<\?xml\s[^?]*\?>','',raw,count=1)
    require('<!' not in remainder and '<?' not in remainder,'xml-declarations-pi')
    try: root=ET.fromstring(raw,parser=ET.XMLParser(target=StrictNamespaceBuilder(insert_comments=True,insert_pis=True)))
    except ET.ParseError as error: raise ValueError('partial-metadata:malformed-xml') from error
    count=0
    def tree(node,depth=0):
        nonlocal count
        count+=1
        require(count<=1024 and depth<=24 and type(node.tag) is str and node.tag.startswith(NS) and node.tag.count('}')==1 and not node.attrib and not(node.tail or '').strip(),'xml-namespace-attributes-depth')
        name=node.tag[len(NS):]
        require(name.lower() not in {'password','secretstring','secretbinary','secretaccesskey','accesskeyid','sessiontoken','securitytoken','accesstoken','refreshtoken','idtoken','clientsecret','authorization','token'},'sensitive-field')
        children=list(node)
        if not children:return node.text or ''
        require(not(node.text or '').strip(),'xml-mixed')
        names=[child.tag[len(NS):] if type(child.tag) is str and child.tag.startswith(NS) else None for child in children]
        if names==['member']*len(names):return [tree(child,depth+1) for child in children]
        require(None not in names and 'member' not in names and len(names)==len(set(names)),'duplicate-foreign-xml')
        return {name:tree(child,depth+1) for name,child in zip(names,children)}
    require(root.tag==NS+action+'Response','xml-operation')
    value=tree(root)
    if action=='Error':
        exact(value,{'Error','RequestId'});exact(value['Error'],{'Type','Code','Message'})
        require(value['Error']['Type']=='Sender' and value['Error']['Code']=='NoSuchEntity' and type(value['Error']['Message']) is str and 1<=len(value['Error']['Message'])<=2048 and type(value['RequestId']) is str and re.fullmatch(r'[A-Za-z0-9-]{8,128}',value['RequestId']),'actual-typed-NoSuchEntity-only')
        return value
    exact(value,{action+'Result','ResponseMetadata'});exact(value['ResponseMetadata'],{'RequestId'})
    require(type(value['ResponseMetadata']['RequestId']) is str and re.fullmatch(r'[A-Za-z0-9-]{8,128}',value['ResponseMetadata']['RequestId']),'actual-request-id')
    require(type(value[action+'Result']) is dict,'xml-result')
    return value[action+'Result']


def members(value):
    require(value=='' or type(value) is list,'member-array')
    return [] if value=='' else value


def wires(action,selectors):
    return [('Action='+action+'&Version=2010-05-08'+''.join('&'+quote(k,safe='-._~')+'='+quote(selectors[k],safe='-._~') for k in ordering)).encode() for ordering in itertools.permutations(selectors)]


def _read(row,manifest,slot,action,selectors,now_ms,not_before_ms):
    exact(row,ROW);exact(manifest,MANIFEST)
    require(manifest['schema']=='hikyo-158/next-inventory-observer-manifest/v1' and manifest['stage']=='baseline' and manifest['service']=='iam'
            and manifest['account']==ACCOUNT and manifest['region']==REGION and manifest['publicMainSHA256']==MAIN and manifest['publicVendorsSHA256']==VENDORS
            and all(manifest[k] is None for k in ('newKeyARN','boundaryPolicyId','roleIds','boundPlanSHA256','creationCustodySHA256','newKeyCustodySHA256')),'original-baseline-manifest')
    start,end=manifest['notBeforeMS'],manifest['expiresAtMS'];sent,received=row['sentAtMS'],row['receivedAtMS']
    require(type(start) is int and type(end) is int and start<=now_ms<end<=start+300000 and now_ms-start<=300000
            and type(sent) is int and type(received) is int and max(start,not_before_ms)<=sent<=received<=now_ms and received-sent<=20000,'actual-fresh-read-after-quiescence')
    require(row['schema']=='hikyo-158/next-inventory-native-response/v1' and row['slot']==slot and row['action']==action and row['service']=='iam' and row['stage']=='baseline'
            and row['account']==ACCOUNT and row['region']==REGION and row['manifestSHA256']==sha(canonical(manifest))
            and row['responseEndpoint']=='https://iam.amazonaws.com/' and row['pageMarkerPresent'] is False and row['pageMarkerSHA256'] is None
            and all(row[k] is False for k in FLAGS),'original-native-read')
    for key in ('notBeforeMS','expiresAtMS','boundPlanSHA256','creationCustodySHA256','newKeyCustodySHA256','ownerContextSHA256','reviewedPreparationSHA256','publicMainSHA256','publicVendorsSHA256'):
        require(row[key]==manifest[key],'whole-native-manifest-binding')
    actual=row['selectors'];require(type(actual) is dict and all(actual.get(k)==v for k,v in selectors.items()),'fixed-selectors')
    optional=set(actual)-set(selectors)
    require(optional<=({'MaxItems','PathPrefix'} if action=='ListRoles' or action.startswith('ListAttached') else {'MaxItems'} if action.startswith('List') and action!='ListOpenIDConnectProviders' else set())
            and ('MaxItems' not in actual or actual['MaxItems'] in ('100','1000')) and ('PathPrefix' not in actual or actual['PathPrefix']=='/'),'no-filter-marker-or-extra-selector')
    require(type(row['requestBodyBytes']) is int and any(len(body)==row['requestBodyBytes'] and sha(body)==row['requestBodySHA256'] for body in wires(action,actual)),'original-whole-wire')
    raw=row['rawResponse'];require(type(raw) is str and 2<=len(raw)<=32768,'raw-text')
    encoded=raw.encode();require(type(row['rawResponseBytes']) is int and row['rawResponseBytes']==len(encoded)<=32768 and row['rawResponseSHA256']==sha(encoded),'raw-whole-custody')
    require(type(row['status']) is int and row['status'] in ((200,404) if slot=='boundary.GetPolicy' else (200,)),'typed-http-status')
    return xml(raw,action) if row['status']==200 else xml(raw,'Error')


def inventory_from_lists(m,source,*,preparation_sha256,owner_context_sha256,explicit_now,not_before):
    exact(source,{'manifest','roles','providers'});manifest=source['manifest'];exact(manifest,MANIFEST)
    require(manifest['reviewedPreparationSHA256']==preparation_sha256 and manifest['ownerContextSHA256']==owner_context_sha256,'actual-preparation-owner-reference')
    now_ms=epoch_ms(explicit_now);minimum=epoch_ms(not_before)
    roles=_read(source['roles'],manifest,'roles.ListRoles','ListRoles',{},now_ms,minimum);exact(roles,{'Roles','IsTruncated'})
    require(roles['IsTruncated']=='false','complete-role-inventory-no-marker');listed=members(roles['Roles']);names=set();ids=set()
    for row in listed:
        required={'Path','RoleName','RoleId','Arn','CreateDate','AssumeRolePolicyDocument','MaxSessionDuration'}
        require(type(row) is dict and required<=set(row)<=required|{'Description','PermissionsBoundary','Tags','RoleLastUsed'},'whole-list-role-metadata')
        name=row['RoleName'];role_id=row['RoleId'];path=row['Path']
        require(type(name) is str and re.fullmatch(r'[A-Za-z0-9+=,.@_-]{1,64}',name) and name.casefold() not in names
                and type(role_id) is str and re.fullmatch(r'AROA[A-Z0-9]{8,64}',role_id) and role_id not in ids
                and type(path) is str and (path=='/' or re.fullmatch(r'/[!-~]+/',path)) and row['Arn']==f'arn:aws:iam::{ACCOUNT}:role'+path+name,'same-account-unique-role-path-identity')
        names.add(name.casefold());ids.add(role_id)
        require(name.casefold() not in {r.casefold() for r in ROLES} or name in ROLES,'ambiguous-case-role')
    providers=_read(source['providers'],manifest,'provider.ListOpenIDConnectProviders','ListOpenIDConnectProviders',{},now_ms,minimum);exact(providers,{'OpenIDConnectProviderList'})
    provider_rows=members(providers['OpenIDConnectProviderList']);require(len(provider_rows)<=1,'at-most-approved-provider')
    for row in provider_rows:require(type(row) is dict and set(row)=={'Arn'} and row['Arn']==PROVIDER,'exact-provider-list')
    present={row['RoleName']:row for row in listed if row['RoleName'] in ROLES};role_hash=m.digest(source['roles']);provider_hash=m.digest(source['providers'])
    observed=stamp_ms(min(source['roles']['sentAtMS'],source['providers']['sentAtMS']))
    return {'schema':'hikyo-158/partial-inventory-derived/v1','observedAt':observed,
            'roles':{name:({'schema':'hikyo-158/inventory-observed-role/v1','RoleName':name,'RoleId':present[name]['RoleId'],'Arn':present[name]['Arn'],'listReceiptSHA256':role_hash} if name in present else
                           {'schema':'hikyo-158/inventory-derived-role-absence/v1','RoleName':name,'inlinePolicyAbsence':'nonexistent-parent-role','listReceiptSHA256':role_hash}) for name in ROLES},
            'allRoles':{'Roles':copy.deepcopy(listed),'IsTruncated':False},
            'provider':{'schema':'hikyo-158/inventory-observed-provider/v1' if provider_rows else 'hikyo-158/inventory-derived-provider-absence/v1','arn':PROVIDER,'listReceiptSHA256':provider_hash},
            'allProviders':{'OpenIDConnectProviderList':copy.deepcopy(provider_rows)},'source':copy.deepcopy(source)}


def checked_inventory(m,inventory,*,preparation_sha256,owner_context_sha256,explicit_now,not_before):
    exact(inventory,{'schema','observedAt','roles','allRoles','provider','allProviders','source'})
    expected=inventory_from_lists(m,inventory['source'],preparation_sha256=preparation_sha256,owner_context_sha256=owner_context_sha256,explicit_now=explicit_now,not_before=not_before)
    require(m.same(expected,inventory),'whole-derived-inventory-not-caller-flags')
    return copy.deepcopy(expected)


def boundary_from_native(m,decoder,bound,bundle,*,policy_id,original_claim_sha256,boundary_origin_sha256,preparation_sha256,explicit_now,not_before):
    exact(bundle,{'manifest','rows'});given=bundle['manifest']
    fields={'schema','stage','account','region','policyARN','policyId','notBeforeMS','expiresAtMS','reviewedPreparationSHA256','publicMainSHA256','publicVendorsSHA256','originalClaimSHA256','boundPlanSHA256','boundaryOriginSHA256'}
    exact(given,fields)
    require(type(policy_id) is str and re.fullmatch(r'ANPA[A-Z0-9]{17}',policy_id) and policy_id not in (m.SETUP_ID,'ANPAROHA7PNS5QVDJKPIN'),'known-actual-new-boundary-id')
    require(given['schema']=='hikyo-158/partial-boundary-read-manifest/v1' and given['stage']=='boundary-v1' and given['account']==ACCOUNT and given['region']==REGION
            and given['policyARN']==BOUNDARY and given['policyId']==policy_id and given['publicMainSHA256']==MAIN and given['publicVendorsSHA256']==VENDORS
            and given['reviewedPreparationSHA256']==preparation_sha256 and given['originalClaimSHA256']==original_claim_sha256 and given['boundPlanSHA256']==m.digest(bound) and given['boundaryOriginSHA256']==boundary_origin_sha256,'exact-current-boundary-origin')
    now_ms=epoch_ms(explicit_now);minimum=epoch_ms(not_before);required={'GetPolicy':None,'ListPolicyVersions':None,'GetPolicyVersion.v1':'v1'};values={};times=[]
    row_fields={'schema','stage','account','region','action','versionId','policyARN','policyId','slot','requestBodyBytes','requestBodySHA256','status','endpoint','sentAtMS','receivedAtMS','rawResponseBytes','rawResponseSHA256','rawResponse','originalClaimSHA256','boundPlanSHA256','boundaryOriginSHA256','manifestSHA256','notBeforeMS','expiresAtMS','reviewedPreparationSHA256','publicMainSHA256','publicVendorsSHA256','ownerIdentityVerifiedByObserver','publicSourceBytesVerifiedByObserver','providerAPIsCalledByObserver','policyDocumentsDecodedByObserver'}
    require(type(bundle['rows']) is list and len(bundle['rows'])==3,'three-boundary-originals')
    for row in bundle['rows']:
        exact(row,row_fields);slot=row['slot'];require(slot in required and slot not in values,'unique-boundary-slot')
        action=slot.split('.')[0];version=required[slot];start,end=given['notBeforeMS'],given['expiresAtMS'];sent,received=row['sentAtMS'],row['receivedAtMS']
        require(type(start) is int and type(end) is int and start<=now_ms<end<=start+300000 and now_ms-start<=300000 and type(sent) is int and type(received) is int
                and max(start,minimum)<=sent<=received<=now_ms and received-sent<=20000,'fresh-boundary-after-quiescence')
        require(row['schema']=='hikyo-158/partial-boundary-native-response/v1' and row['action']==action and row['versionId']==version and row['policyARN']==BOUNDARY and row['policyId']==policy_id
                and row['stage']=='boundary-v1' and row['account']==ACCOUNT and row['region']==REGION and row['status']==200 and type(row['status']) is int and row['endpoint']=='https://iam.amazonaws.com/'
                and row['manifestSHA256']==sha(canonical(given)) and all(row[k]==given[k] for k in ('originalClaimSHA256','boundPlanSHA256','boundaryOriginSHA256','notBeforeMS','expiresAtMS','reviewedPreparationSHA256','publicMainSHA256','publicVendorsSHA256'))
                and all(row[k] is False for k in ('ownerIdentityVerifiedByObserver','publicSourceBytesVerifiedByObserver','providerAPIsCalledByObserver','policyDocumentsDecodedByObserver')),'original-boundary-response')
        selectors={'PolicyArn':BOUNDARY,**({'VersionId':'v1'} if version else {})};bodies=wires(action,selectors)
        if action=='ListPolicyVersions':bodies+=wires(action,{**selectors,'MaxItems':'1000'})
        require(type(row['requestBodyBytes']) is int and any(row['requestBodyBytes']==len(body) and row['requestBodySHA256']==sha(body) for body in bodies),'boundary-whole-native-wire')
        raw=row['rawResponse'];require(type(raw) is str and 2<=len(raw)<=32768 and type(row['rawResponseBytes']) is int and row['rawResponseBytes']==len(raw.encode())<=32768 and row['rawResponseSHA256']==sha(raw.encode()),'whole-boundary-xml-custody')
        values[slot]=xml(raw,action);times.append(sent)
    exact(values['GetPolicy'],{'Policy'});metadata=values['GetPolicy']['Policy']
    required_meta={'PolicyName','PolicyId','Arn','Path','DefaultVersionId','AttachmentCount','PermissionsBoundaryUsageCount','IsAttachable','CreateDate','UpdateDate'}
    require(type(metadata) is dict and required_meta<=set(metadata)<=required_meta|{'Description','Tags'} and metadata['PolicyName']=='hikyo-158-role-boundary' and metadata['PolicyId']==policy_id and metadata['Arn']==BOUNDARY
            and metadata['Path']=='/' and metadata['DefaultVersionId']=='v1' and metadata['IsAttachable']=='true' and decoder.instant(metadata['CreateDate'])<=decoder.instant(metadata['UpdateDate'])<=explicit_now,'whole-new-boundary-metadata')
    require(('Tags' not in metadata or members(metadata['Tags'])==[]) and ('Description' not in metadata or metadata['Description']==''),'no-boundary-tags-description')
    versions=values['ListPolicyVersions'];exact(versions,{'Versions','IsTruncated'});require(versions['IsTruncated']=='false','complete-boundary-versions');rows=members(versions['Versions']);require(len(rows)==1,'sole-boundary-v1')
    version=rows[0];exact(version,{'VersionId','IsDefaultVersion','CreateDate'});require(version['VersionId']=='v1' and version['IsDefaultVersion']=='true','boundary-default-v1')
    exact(values['GetPolicyVersion.v1'],{'PolicyVersion'});full=values['GetPolicyVersion.v1']['PolicyVersion'];exact(full,{'VersionId','IsDefaultVersion','CreateDate','Document'})
    require(full['VersionId']=='v1' and full['IsDefaultVersion']=='true' and decoder.instant(full['CreateDate'])==decoder.instant(version['CreateDate'])<=explicit_now,'same-actual-version-date')
    document=decoder.policy(full['Document']);require(m.same(document,bound['documents']['setup-role-boundary.json']),'whole-boundary-document')
    attachments=decoder.decimal_integer(metadata['AttachmentCount']);usage=decoder.decimal_integer(metadata['PermissionsBoundaryUsageCount'])
    return {'observedAt':stamp_ms(min(times)),'value':{'arn':BOUNDARY,'policyId':policy_id,'defaultVersionId':'v1','versions':{'v1':{'isDefault':True,'document':document}}},
            'metadata':metadata,'attachments':({'users':[],'groups':[],'roles':[],'attachmentCount':0,'permissionsBoundaryUsageCount':0} if attachments==0 and usage==0 else None),
            'usageDerivation':'actual-GetPolicy-zero-total-counts' if attachments==0 and usage==0 else 'nonzero-or-in-use-no-empty-entity-claim','source':copy.deepcopy(bundle)}


def normalize_partial_metadata(m,inventory_normalizer,setup_decoder,key_normalizer,prepared,inputs,*,explicit_now):
    """Project original public metadata only; no original/cost/work/auth release."""
    fields={'ownerContext','ownerMFA','quiescenceAt','originalClaimSHA256','iam','setup','kms','keyCustody','keyIntentRecord','approvedCreateBody','nativeCreate','bound','boundary','boundaryPolicyId','boundaryOrigin','boundaryOriginSHA256'}
    exact(inputs,fields);m.clock(explicit_now);m.preparation_checked(prepared)
    owner=inputs['ownerContext'];m.exact(owner,{'account','principalARN','source','observedAt','sourceReceipt'});m.fresh(owner['observedAt'],explicit_now)
    require(owner['account']==ACCOUNT and owner['principalARN']==prepared['records']['owner']['ownerContext']['principalARN'] and owner['source']=='native-owner-console','same-retained-current-owner')
    require(type(owner['sourceReceipt']) is dict and owner['sourceReceipt'].get('schema')=='hikyo-158/native-owner-context-projection/v1' and owner['sourceReceipt'].get('principalARN')==owner['principalARN'] and owner['sourceReceipt'].get('accountHeaderText')==ACCOUNT,'whole-public-owner-projection')
    auth=inputs['ownerMFA'];m.exact(auth,{'schema','account','principalARN','observedAt','methods','originalObservation'});m.fresh(auth['observedAt'],explicit_now)
    require(auth['schema']=='hikyo-158/owner-mfa-custody/v1' and auth['account']==ACCOUNT and auth['principalARN']==owner['principalARN'] and auth['methods']==['password','mfa'] and type(auth['originalObservation']) is dict and bool(auth['originalObservation']),'explicit-owner-mfa-original-no-inference')
    minimum=m.instant(inputs['quiescenceAt']);m.fresh(inputs['quiescenceAt'],explicit_now);owner_sha=m.digest(owner);prep_sha=m.digest(prepared);basic=inputs['iam'];exact(basic,{'manifest','rows'})
    require(basic['manifest']['reviewedPreparationSHA256']==prep_sha and basic['manifest']['ownerContextSHA256']==owner_sha,'same-current-basic-context')
    required={**{'user.'+action:(action,{'UserName':USER}) for action in USER_ACTIONS},'roles.ListRoles':('ListRoles',{}),'provider.ListOpenIDConnectProviders':('ListOpenIDConnectProviders',{}),'boundary.GetPolicy':('GetPolicy',{'PolicyArn':BOUNDARY})}
    require(type(basic['rows']) is list and len(basic['rows'])==9,'all-nine-proven-native-baseline-reads');data={};raw={}
    for row in basic['rows']:
        slot=row['slot'];require(slot in required and slot not in data,'unique-basic-slot');action,selectors=required[slot]
        data[slot]=_read(row,basic['manifest'],slot,action,selectors,epoch_ms(explicit_now),epoch_ms(minimum));raw[slot]=row
    inventory=inventory_from_lists(m,{'manifest':basic['manifest'],'roles':raw['roles.ListRoles'],'providers':raw['provider.ListOpenIDConnectProviders']},preparation_sha256=prep_sha,owner_context_sha256=owner_sha,explicit_now=explicit_now,not_before=minimum)
    inventory_normalizer.user_checked(m,setup_decoder,prepared['proposal'],data,explicit_now)
    setup=inputs['setup'];exact(setup,{'manifest','rows'});stage=setup['manifest']['stage'];require(stage in setup_decoder.STAGES and setup['manifest']['reviewedPreparationSHA256']==prep_sha,'actual-setup-stage')
    bound=inputs['bound']
    if bound is not None:
        m.bound_checked(bound,m.instant(bound['derivedAt']));require(m.same(bound['preparation'],prepared),'same-original-bound-preparation')
    v6_sha=m.digest(bound['documents']['setup-user.managed-policy.json']) if bound is not None and stage in ('activated','restored') else None
    normalized_setup=setup_decoder.normalize(setup['manifest'],setup['rows'],owner_sha,v6_sha,epoch_ms(explicit_now));require(normalized_setup['provenance']['earliestActualSentAtMS']>=epoch_ms(minimum),'setup-after-quiescence')
    policy=normalized_setup['proof']['policy'];m.setup_policy_checked(policy,prepared['proposal']['inputs']['iamClosure']['setupPolicy'],setup_decoder.STAGES[stage][0],setup_decoder.STAGES[stage][1],bound['documents'] if bound else None)
    old={};custody=inputs['keyCustody'];new=None;kms=inputs['kms']
    if custody is None:
        require(inputs['keyIntentRecord'] is None and inputs['approvedCreateBody'] is None and inputs['nativeCreate'] is None,'unknown-key-is-not-inferred-absent')
        values,times,_=inventory_normalizer.native_bundle(m,setup_decoder,None,None,kms,'kms','baseline',owner_sha,explicit_now,prep_sha)
        require(min(times)>=epoch_ms(minimum),'old-key-reads-after-quiescence')
        for i,key_id in enumerate(inventory_normalizer.OLD_IDS,1):old[f'arn:aws:kms:{REGION}:{ACCOUNT}:key/'+key_id]=values['old'+str(i)+'.DescribeKey']['KeyMetadata']
    else:
        require(type(custody) is dict and custody.get('schema') in ('hikyo-158/next-created-key-custody/v1','hikyo-158/abort-key-origin/v1','hikyo-158/cleanup-only-key-origin/v1'),'original-key-custody-only')
        base=custody['readback'];key=base['metadata']['Arn'];require(key not in m.scheduled_key_closure(prepared['proposal']),'not-an-old-key')
        expected_intent={k:custody[k] for k in ('preparationSHA256','claimId','intent')}
        require(m.same(inputs['keyIntentRecord'],expected_intent),'same-original-create-intent-reference')
        wire=m.strict_json(inputs['approvedCreateBody'].encode());require(type(wire.get('Policy')) is str,'original-key-policy-wire');wire['Policy']=m.strict_json(wire['Policy'].encode())
        require(m.same(wire,custody['intent']['request']),'same-original-complete-create-body')
        create=inputs['nativeCreate'];create_value=m.strict_json(create['rawResponse'].encode());exact(create_value,{'KeyMetadata'})
        require(m.same(create_value['KeyMetadata'],base['metadata']),'same-original-native-create-metadata')
        if custody['schema'] in ('hikyo-158/abort-key-origin/v1','hikyo-158/cleanup-only-key-origin/v1'):
            require(m.same(create,custody['nativeCreate']) and inputs['approvedCreateBody']==custody['approvedCreateBody'],'unchanged-factual-origin-no-model-key-proof')
        else:
            require(create['requestId']==custody['createResponse']['RequestId'] and m.instant(stamp_ms(create['sentAtMS']))==m.instant(custody['sentAt']) and m.instant(stamp_ms(create['receivedAtMS']))==m.instant(custody['receivedAt']),'original-create-response-id-and-time-custody')
        require(type(kms) is list and len(kms)==8,'all-eight-current-key-originals');values={};creation_sha=m.digest(inputs['nativeCreate'])
        required_keys={**{'new.'+action:(action,key) for action in ('DescribeKey','GetKeyPolicy','GetKeyRotationStatus','ListAliases')},**{'old'+str(i)+'.DescribeKey':('DescribeKey',f'arn:aws:kms:{REGION}:{ACCOUNT}:key/'+key_id) for i,key_id in enumerate(inventory_normalizer.OLD_IDS,1)}}
        for row in kms:
            slot=row['slot'];require(slot in required_keys and slot not in values,'unique-key-original');action,arn=required_keys[slot]
            require(row['sentAtMS']>=epoch_ms(minimum),'key-reads-after-quiescence')
            values[slot]=key_normalizer.row_checked(m,prepared,inputs['keyIntentRecord'],inputs['approvedCreateBody'],row,'readback',slot,action,arn,explicit_now,key,creation_sha)
        current=values['new.DescribeKey'];exact(current,{'KeyMetadata'});current=current['KeyMetadata'];original=base['metadata']
        require(current.get('Arn')==key and current.get('KeyId')==original['KeyId'] and set(current) in (set(original),set(original)|{'DeletionDate'}) and all(m.same(current.get(k),v) for k,v in original.items() if k not in ('KeyState','Enabled')) and type(current.get('Enabled')) is bool and (current['KeyState'],current['Enabled']) in (('Enabled',True),('Disabled',False),('PendingDeletion',False)),'only-supported-actual-key-state-change')
        value=values['new.GetKeyPolicy'];exact(value,{'PolicyName','Policy'});require(value['PolicyName']=='default','actual-default-key-policy');key_policy=m.strict_json(value['Policy'].encode());require(m.same(key_policy,base['policy']),'same-whole-key-policy')
        require(m.same(values['new.GetKeyRotationStatus'],{'KeyId':key,'KeyRotationEnabled':False}),'actual-rotation-disabled')
        aliases=values['new.ListAliases'];require(type(aliases) is dict and {'Aliases','Truncated'}<=set(aliases)<={'Aliases','Truncated','AliasCount'} and aliases['Aliases']==[] and aliases['Truncated'] is False and ('AliasCount' not in aliases or type(aliases['AliasCount']) is int and aliases['AliasCount']==0),'actual-complete-empty-aliases')
        new={'metadata':current,'policy':key_policy,'rotationEnabled':False,'aliases':[],'Truncated':False}
        for i,key_id in enumerate(inventory_normalizer.OLD_IDS,1):old[f'arn:aws:kms:{REGION}:{ACCOUNT}:key/'+key_id]=values['old'+str(i)+'.DescribeKey']['KeyMetadata']
    require(m.same(old,prepared['records']['owner']['oldKeys']),'all-four-old-whole-schedules-retained')
    boundary_row=raw['boundary.GetPolicy']
    if boundary_row['status']==404:
        inventory_normalizer.absent_boundary(boundary_row['rawResponse'])
        require(inputs['boundary'] is None and inputs['boundaryOrigin'] is None,'no-invented-absent-boundary-fields')
        boundary={'value':{'operation':'GetPolicy','arn':BOUNDARY,'errorCode':'NoSuchEntity'},'attachments':None,'abortOrigin':None};boundary_provenance=None
    else:
        require(bound is not None and inputs['boundary'] is not None,'present-boundary-needs-actual-v1-custody')
        proof=boundary_from_native(m,setup_decoder,bound,inputs['boundary'],policy_id=inputs['boundaryPolicyId'],original_claim_sha256=inputs['originalClaimSHA256'],boundary_origin_sha256=inputs['boundaryOriginSHA256'],preparation_sha256=prep_sha,explicit_now=explicit_now,not_before=minimum)
        require(data['boundary.GetPolicy']['Policy']==proof['metadata'],'same-current-whole-boundary')
        boundary={'value':proof['value'],'attachments':proof['attachments'],'abortOrigin':copy.deepcopy(inputs['boundaryOrigin'])};boundary_provenance=proof
    baseline=prepared['records']['owner']['setupUser']
    return {'schema':'hikyo-158/partial-native-metadata-normalized/v1','records':{'ownerContext':copy.deepcopy(owner),'ownerMFA':copy.deepcopy(auth),'setupUser':copy.deepcopy(baseline),'setupPolicy':policy,
            'keyCustody':copy.deepcopy(custody),'newKey':new,'oldKeys':old,'inventory':inventory,'boundary':boundary},
            'originalSources':{'iam':copy.deepcopy(basic),'setup':copy.deepcopy(setup),'kms':copy.deepcopy(kms),'boundary':copy.deepcopy(inputs['boundary'])},
            'boundaryDerivation':boundary_provenance,'retirementBoundary':None if boundary_provenance is None else {'observedAt':boundary_provenance['observedAt'],'value':copy.deepcopy(boundary_provenance['value'])},'retainedMFAEnableDate':data['user.ListMFADevices']['MFADevices'],
            'externalAuthenticityVerifiedByBridge':False,'providerAPIsCalledByBridge':False,'executionAuthorityGranted':False,'creationChargeRefunded':False}
