"""Pure next-fixture metadata bridge. Inject reviewed model/decoder, never I/O.

No controller imports, clocks, provider clients, browser or authentication reads.
Source pins are integrity references. Root must authenticate loaded bytes and
all raw source/owner/creation custody independently before using these outputs.
"""
import copy
from datetime import datetime, timedelta, timezone
from urllib.parse import quote
import hashlib
import itertools
import json
import re
import xml.etree.ElementTree as ET

MODEL_SHA = 'f40c07b2204c3305b434836015c99f30af4dfc893a0ce0e4f0318c17ea844a19'
TEST_SHA = 'f06b2e173ff499070ae8718d65870534510a585339b8fc451462baaea6dc5985'
SETUP_DECODER_SHA = '2b70d44ec3cf19e5893e5f7c7b6578762536c28c514cffeb218076d7d2829221'
PINS={'modelSHA256':MODEL_SHA,'modelTestSHA256':TEST_SHA,'setupNormalizerSHA256':SETUP_DECODER_SHA}
MAIN_IAM='85a9aed5a11dc8d019b95f399a1502b1c8e19affee5c933fc09623d0d359d0f9'
VENDORS='e4cdd4a45e6d93e7d1e4c512cfa82eaf02bbb040280c93b1b0f20958118b4ba9'
MAIN_KMS='20a50d3f9e11cdae453b0f764689073f2adc6c644e6ac2a08660be5b1b2227a2'
OLD_IDS=('10908b8d-fe54-40e3-9210-a3eefea709b6','990c479f-d673-4bc9-8bca-7b9b86f4b205','fa374267-a456-4e79-b5b1-6cdf356b7d0e','bfa44dc1-ee8a-43b8-aec7-d8a7fbb38db1')
USER_ACTIONS=('GetUser','ListMFADevices','ListAttachedUserPolicies','ListUserPolicies','ListGroupsForUser','ListAccessKeys')
ROLE_ACTIONS=('GetRole','GetRolePolicy','ListRolePolicies','ListAttachedRolePolicies','ListRoleTags')
MANIFEST_KEYS={'schema','service','account','region','reviewedPreparationSHA256','publicMainSHA256','publicVendorsSHA256','expiresAtMS','notBeforeMS','stage','newKeyARN','boundaryPolicyId','roleIds','boundPlanSHA256','creationCustodySHA256','newKeyCustodySHA256','ownerContextSHA256'}
ROW_KEYS={'schema','service','stage','account','region','manifestSHA256','notBeforeMS','expiresAtMS','boundPlanSHA256','creationCustodySHA256','newKeyCustodySHA256','ownerContextSHA256','slot','action','requestBodySHA256','requestBodyBytes','selectors','pageMarkerPresent','pageMarkerSHA256','status','responseEndpoint','sentAtMS','receivedAtMS','rawResponseBytes','rawResponseSHA256','rawResponse','reviewedPreparationSHA256','publicMainSHA256','publicVendorsSHA256','JSONParsedByThisCode','providerAPIsCalledByThisCode','ownerProofComplete','nativeWriteAuthorizationOwnedByObserver'}


def require(value,label):
    if not value:raise ValueError('next-inventory:'+label)


def exact(value,fields):require(type(value) is dict and set(value)==set(fields),'closed-fields')


def sha(raw):return hashlib.sha256(raw).hexdigest()


def canonical(value):
    # Manifest/receipt strings are already strictly validated by model canonical;
    # this UTF-8 spelling matches native JSON.stringify canonical manifest bytes.
    return json.dumps(value,sort_keys=True,separators=(',',':'),ensure_ascii=False,allow_nan=False).encode()


def epoch_ms(now):
    delta=now-datetime(1970,1,1,tzinfo=timezone.utc)
    return delta.days*86400000+delta.seconds*1000+delta.microseconds//1000


def stamp_ms(value):
    return (datetime(1970,1,1,tzinfo=timezone.utc)+timedelta(milliseconds=value)).isoformat(timespec='milliseconds').replace('+00:00','Z')


def source_checked(m,source_pins,bound):
    m.exact(source_pins,PINS);require(m.same(source_pins,PINS),'reviewed-source-pins')
    # Reconstruct at its original observation time, without promoting old evidence
    # to a current owner/CI proof. Current stage freshness is checked separately.
    m.bound_checked(bound,m.instant(bound['derivedAt']))


def identity_contract(m,bound,custody):
    m.exact(custody,{'schema','boundPlanSHA256','boundary','provider','roles','operations'})
    require(custody['schema']=='hikyo-158/next-created-identities/v1' and custody['boundPlanSHA256']==m.digest(bound),'whole-creation-custody')
    m.exact(custody['boundary'],{'operation','arn','policyId','defaultVersionId','documentSHA256'})
    require(custody['boundary']['operation']=='CreatePolicy' and custody['boundary']['arn']==m.BOUNDARY
        and custody['boundary']['defaultVersionId']=='v1' and custody['boundary']['documentSHA256']==m.digest(bound['documents']['setup-role-boundary.json'])
        and type(custody['boundary']['policyId']) is str and re.fullmatch(r'ANPA[A-Z0-9]{8,64}',custody['boundary']['policyId'])
        and custody['boundary']['policyId'] not in {m.SETUP_ID,'ANPAROHA7PNS5QVDJKPIN'},'actual-new-boundary-custody')
    m.exact(custody['roles'],m.ROLES);role_ids={}
    old=set(bound['preparation']['proposal']['inputs']['firstEpoch']['inventory']['roleIds'].values())
    for name,row in custody['roles'].items():
        m.exact(row,{'operation','arn','roleId','roleName'})
        require(row['operation']=='CreateRole' and row['arn']==f'arn:aws:iam::{m.ACCOUNT}:role/'+name and row['roleName']==name
            and type(row['roleId']) is str and re.fullmatch(r'AROA[A-Z0-9]{8,64}',row['roleId']) and row['roleId'] not in old,'actual-new-role-custody')
        role_ids[name]=row['roleId']
    require(len(set(role_ids.values()))==8,'distinct-new-role-identities')
    return role_ids,custody['boundary']['policyId']


def manifest(m,bound,custody,service,stage,not_before_ms,expires_at_ms,owner_context_sha256):
    """Pure source spec, explicit times/identities. Never installs or authenticates."""
    require(service in ('iam','kms') and stage in ('inventory','restored-setup') and not(service=='kms' and stage!='inventory'),'stage-service')
    role_ids,boundary_id=identity_contract(m,bound,custody)
    require(m.sha(owner_context_sha256) and owner_context_sha256==m.digest(bound['records']['owner']['ownerContext']),'bound-owner-reference')
    return {'schema':'hikyo-158/next-inventory-observer-manifest/v1','service':service,'stage':stage,'account':m.ACCOUNT,'region':m.REGION,
        'newKeyARN':bound['run']['keyARN'],'boundaryPolicyId':boundary_id,'roleIds':role_ids,'boundPlanSHA256':m.digest(bound),
        'creationCustodySHA256':m.digest(custody),'newKeyCustodySHA256':m.digest(bound['records']['key']),
        'ownerContextSHA256':owner_context_sha256,'reviewedPreparationSHA256':bound['preparationSHA256'],
        'publicMainSHA256':MAIN_IAM if service=='iam' else MAIN_KMS,'publicVendorsSHA256':VENDORS if service=='iam' else None,
        'notBeforeMS':not_before_ms,'expiresAtMS':expires_at_ms}


def slots(m,service,stage,new_key):
    if service=='kms':
        return {prefix+'.'+action:(action,{'keyARN':arn}) for arn,prefix,actions in
            ([] if stage=='baseline' else [(new_key,'new',('DescribeKey','GetKeyPolicy','GetKeyRotationStatus','ListAliases'))])+
            [(f'arn:aws:kms:{m.REGION}:{m.ACCOUNT}:key/'+key,'old'+str(i),('DescribeKey',)) for i,key in enumerate(OLD_IDS,1)]
            for action in actions}
    result={'user.'+action:(action,{'UserName':m.USER}) for action in USER_ACTIONS}
    if stage=='restored-setup':return result
    if stage=='baseline':
        return {**result,'roles.ListRoles':('ListRoles',{}),'boundary.GetPolicy':('GetPolicy',{'PolicyArn':m.BOUNDARY}),'provider.ListOpenIDConnectProviders':('ListOpenIDConnectProviders',{})}
    result.update({'role.'+name+'.'+action:(action,{'RoleName':name,**({'PolicyName':'hikyo-158-fixture'} if action=='GetRolePolicy' else {})}) for name in m.ROLES for action in ROLE_ACTIONS})
    result.update({'boundary.'+action+('.v1' if action=='GetPolicyVersion' else ''):(action,{'PolicyArn':m.BOUNDARY,**({'VersionId':'v1'} if action=='GetPolicyVersion' else {})}) for action in ('GetPolicy','ListPolicyVersions','GetPolicyVersion')})
    result['provider.ListOpenIDConnectProviders']=('ListOpenIDConnectProviders',{})
    result['provider.GetOpenIDConnectProvider']=('GetOpenIDConnectProvider',{'OpenIDConnectProviderArn':m.PROVIDER})
    return result


def request_bodies(service,action,selectors):
    if service=='kms':
        key=selectors['keyARN'];values=[key,key.rsplit('/',1)[1]];result=[]
        for value in values:
            variants=([{'KeyId':value,'PolicyName':'default'}] if action=='GetKeyPolicy' else
                [{'KeyId':value},{'KeyId':value,'Limit':100},{'Limit':100,'KeyId':value}] if action=='ListAliases' else [{'KeyId':value}])
            result.extend(json.dumps(v,separators=(',',':')).encode() for v in variants)
        return result
    variants=[dict(selectors)]
    if action.startswith('List') and action!='ListOpenIDConnectProviders':
        variants.extend([{**selectors,'MaxItems':n} for n in ('100','1000')])
        if action.startswith('ListAttached') or action=='ListRoles':
            variants.extend([{**selectors,'PathPrefix':'/'},*[{**selectors,'PathPrefix':'/','MaxItems':n} for n in ('100','1000')]])
    # The public QuerySerializer supplies schema field order. Finite permutations
    # model every ordering of these public selectors; the exact bytes/hash bind
    # the observed request. No unknown selectors, markers or values are accepted.
    result=[]
    for fields in variants:
        for ordered in itertools.permutations(fields):
            result.append(('Action='+action+'&Version=2010-05-08'+''.join('&'+quote(k,safe='-._~')+'='+quote(fields[k],safe='-._~') for k in ordered)).encode())
    return result


def native_bundle(m,decoder,bound,custody,bundle,service,stage,owner_sha,now,baseline_preparation_sha=None):
    exact(bundle,{'manifest','rows'});m.canonical(bundle)
    given=bundle['manifest'];exact(given,MANIFEST_KEYS)
    expected=(baseline_manifest(m,service,given['notBeforeMS'],given['expiresAtMS'],owner_sha,baseline_preparation_sha) if stage=='baseline' else manifest(m,bound,custody,service,stage,given['notBeforeMS'],given['expiresAtMS'],owner_sha))
    require(m.same(given,expected),'exact-custody-manifest')
    now_ms=epoch_ms(now)
    require(type(given['notBeforeMS']) is int and type(given['expiresAtMS']) is int
        and 0<=now_ms-given['notBeforeMS']<=300000 and now_ms<given['expiresAtMS']<=given['notBeforeMS']+300000,'fresh-manifest-window')
    required=slots(m,service,stage,None if stage=='baseline' else bound['run']['keyARN']);rows=bundle['rows']
    require(type(rows) is list and len(rows)==len(required),'complete-native-read-count')
    output={};times=[];receipt_pins={};manifest_sha=sha(canonical(given))
    for row in rows:
        exact(row,ROW_KEYS);slot=row['slot'];require(type(slot) is str and slot in required and slot not in output,'unique-read-slot')
        action,selectors=required[slot]
        require(row['schema']=='hikyo-158/next-inventory-native-response/v1' and row['service']==service and row['stage']==stage
            and row['account']==m.ACCOUNT and row['region']==m.REGION and row['action']==action and row['manifestSHA256']==manifest_sha
            and type(row['status']) is int and row['status']==(404 if stage=='baseline' and slot=='boundary.GetPolicy' else 200) and row['responseEndpoint']==('https://iam.amazonaws.com/' if service=='iam' else f'https://kms.{m.REGION}.amazonaws.com/')
            and all(m.same(row[k],given[k]) for k in ('notBeforeMS','expiresAtMS','boundPlanSHA256','creationCustodySHA256','newKeyCustodySHA256','ownerContextSHA256','reviewedPreparationSHA256','publicMainSHA256','publicVendorsSHA256'))
            and row['pageMarkerPresent'] is False and row['pageMarkerSHA256'] is None
            and all(row[k] is False for k in ('JSONParsedByThisCode','providerAPIsCalledByThisCode','ownerProofComplete','nativeWriteAuthorizationOwnedByObserver')),'raw-native-provenance')
        actual=row['selectors'];require(type(actual) is dict,'selectors-object')
        if service=='iam':
            optional=set(actual)-set(selectors)
            require(optional<=({'MaxItems'}|({'PathPrefix'} if action.startswith('ListAttached') or action=='ListRoles' else set())) and
                all(actual.get(k)==v for k,v in selectors.items()) and
                ('MaxItems' not in actual or action.startswith('List') and actual['MaxItems'] in ('100','1000')) and
                ('PathPrefix' not in actual or actual['PathPrefix']=='/'),'exact-native-selectors')
        else:require(m.same(actual,selectors),'exact-key-selector')
        require(type(row['requestBodyBytes']) is int and any(row['requestBodyBytes']==len(body) and row['requestBodySHA256']==sha(body) for body in request_bodies(service,action,actual)),'exact-request-bytes')
        sent,received=row['sentAtMS'],row['receivedAtMS']
        require(type(sent) is int and type(received) is int and given['notBeforeMS']<=sent<=received<=now_ms
            and now_ms-sent<=300000 and received-sent<=20000,'earliest-native-request-freshness')
        raw=row['rawResponse'];require(type(raw) is str,'raw-response-text');encoded=raw.encode()
        require(2<=len(encoded)<=32768 and type(row['rawResponseBytes']) is int and row['rawResponseBytes']==len(encoded) and row['rawResponseSHA256']==sha(encoded)
            and not re.search(r'SecretString|SecretBinary|SecretAccessKey|AccessKeyId|SessionToken',raw),'bounded-safe-response')
        output[slot]=(absent_boundary(raw) if row['status']==404 else decoder.xml(raw,action)) if service=='iam' else m.strict_json(encoded)
        times.append(sent);receipt_pins[slot]=m.digest(row)
    require(set(output)==set(required),'all-native-selectors-required')
    return output,times,{'manifestSHA256':manifest_sha,'rawReceiptSHA256s':receipt_pins}


def members(value):
    require(value=='' or type(value) is list,'xml-member-list');return [] if value=='' else value


def page(result,field):
    exact(result,{field,'IsTruncated'});require(result['IsTruncated']=='false','complete-untruncated-page')
    return members(result[field])


def tags(value,expected):
    rows=members(value);require(len(rows)==len(expected),'exact-tag-count');found={}
    for row in rows:
        exact(row,{'Key','Value'});require(type(row['Key']) is str and type(row['Value']) is str and row['Key'] not in found,'unique-tag-fields');found[row['Key']]=row['Value']
    require(found=={r['Key']:r['Value'] for r in expected},'whole-reviewed-tags');return copy.deepcopy(rows)



def enrollment_instant(raw):
    require(type(raw) is str and re.fullmatch(r'\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d(?:\.\d{1,6})?(?:Z|\+00:00)',raw),'utc-mfa-enrollment')
    return datetime.fromisoformat(raw.replace('Z','+00:00'))


def retained_mfa_checked(m,decoder,proposal,result,explicit_now):
    """Semantic check for the separately authenticated pre-key/native adapter.

    This accepts a complete parsed ListMFADevices result, not source authority.
    Root must validate its raw receipt/provenance/freshness before calling it.
    """
    m.clock(explicit_now);m.proposal_checked(proposal)
    mfa=page(result,'MFADevices');require(len(mfa)==1,'sole-retained-mfa')
    exact(mfa[0],{'EnableDate','SerialNumber','UserName'})
    archived=proposal['inputs']['iamClosure']['mfaDevices'];require(type(archived) is list and len(archived)==1,'immutable-sole-mfa-custody')
    require(mfa[0]['SerialNumber']==m.MFA and mfa[0]['UserName']==m.USER and archived[0]['SerialNumber']==m.MFA and archived[0]['UserName']==m.USER
        and enrollment_instant(mfa[0]['EnableDate'])==enrollment_instant(archived[0]['EnableDate'])<=explicit_now,'retained-original-mfa-enrollment')
    return copy.deepcopy(mfa[0])


def user_checked(m,decoder,proposal,data,now):
    user=data['user.GetUser'];exact(user,{'User'});user=user['User']
    require(type(user) is dict and {'Path','UserName','UserId','Arn','CreateDate'}<=set(user)<=
        {'Path','UserName','UserId','Arn','CreateDate','PasswordLastUsed','Tags'},'closed-user-metadata')
    require(user['Path']=='/' and user['UserName']==m.USER and user['UserId']==m.USER_ID and user['Arn']==m.USER_ARN,'retained-setup-user')
    require(decoder.instant(user['CreateDate'])<=now and ('PasswordLastUsed' not in user or decoder.instant(user['PasswordLastUsed'])<=now),'user-metadata-dates')
    if 'Tags' in user:require(members(user['Tags'])==[],'setup-no-extra-tags')
    mfa=retained_mfa_checked(m,decoder,proposal,data['user.ListMFADevices'],now)
    attached=page(data['user.ListAttachedUserPolicies'],'AttachedPolicies')
    require(attached==[{'PolicyArn':m.SETUP,'PolicyName':'hikyo-158-setup-policy'}],'sole-setup-policy')
    for action,field in [('ListUserPolicies','PolicyNames'),('ListGroupsForUser','Groups'),('ListAccessKeys','AccessKeyMetadata')]:require(page(data['user.'+action],field)==[],'no-setup-keys-groups-inline')
    return mfa


def owner_record(m,decoder,bound,data,setup_bundle,owner_sha,now,stage):
    mfa=user_checked(m,decoder,bound['preparation']['proposal'],data,now)
    exact(setup_bundle,{'manifest','rows'});given=setup_bundle['manifest']
    require(type(given) is dict and given.get('stage')==('activated' if stage=='inventory' else 'restored') and given.get('reviewedPreparationSHA256')==bound['preparationSHA256'],'exact-setup-stage-reference')
    v6_sha=decoder.sha(decoder.canonical(bound['documents']['setup-user.managed-policy.json']).encode())
    normalized=decoder.normalize(given,setup_bundle['rows'],owner_sha,v6_sha,epoch_ms(now))
    normalized['provenance']['retainedMFADevice']=mfa
    policy=normalized['proof']['policy'];archive=bound['preparation']['proposal']['inputs']['iamClosure']['setupPolicy']
    m.setup_policy_checked(policy,archive,['v2','v3','v4','v5','v6'],'v6' if stage=='inventory' else 'v5',bound['documents'])
    observed=stamp_ms(normalized['provenance']['earliestActualSentAtMS'])
    return {'schema':'hikyo-158/next-activated-owner/v1' if stage=='inventory' else 'hikyo-158/next-restored-owner/v1','observedAt':observed,'setupPolicy':policy,
        'setupUserId':m.USER_ID,'MFADevices':[{'UserName':m.USER,'SerialNumber':m.MFA}],'attachedPolicyARNs':[m.SETUP],'inlinePolicyNames':[],'groups':[],'accessKeys':[]},normalized


def normalize_inventory(m,decoder,source_pins,bound,custody,iam_bundle,kms_bundle,setup_bundle,*,owner_context_sha256,explicit_now):
    m.clock(explicit_now);source_checked(m,source_pins,bound);m.bound_checked(bound,explicit_now)
    iam,times1,proof1=native_bundle(m,decoder,bound,custody,iam_bundle,'iam','inventory',owner_context_sha256,explicit_now)
    kms,times2,proof2=native_bundle(m,decoder,bound,custody,kms_bundle,'kms','inventory',owner_context_sha256,explicit_now)
    owner,setup=owner_record(m,decoder,bound,iam,setup_bundle,owner_context_sha256,explicit_now,'inventory')
    expected_tags=[{'Key':'HikyoIssue','Value':'158'},{'Key':'Authorization','Value':bound['run']['authorizationId']}]
    roles={};role_ids,boundary_id=identity_contract(m,bound,custody)
    for name in m.ROLES:
        result=iam['role.'+name+'.GetRole'];exact(result,{'Role'});role=result['Role']
        required={'Arn','RoleName','RoleId','Path','MaxSessionDuration','PermissionsBoundary','AssumeRolePolicyDocument','Tags','CreateDate'}
        require(type(role) is dict and required<=set(role)<=required|{'Description','RoleLastUsed'},'closed-role-metadata')
        require(role['Arn']==f'arn:aws:iam::{m.ACCOUNT}:role/'+name and role['RoleName']==name and role['RoleId']==role_ids[name] and role['Path']=='/'
            and decoder.instant(role['CreateDate'])>=m.instant(bound['records']['key']['sentAt']) and decoder.instant(role['CreateDate'])<=explicit_now
            and ('Description' not in role or role['Description']=='') and ('RoleLastUsed' not in role or role['RoleLastUsed']==''),'exact-created-role')
        require(role['PermissionsBoundary']=={'PermissionsBoundaryArn':m.BOUNDARY,'PermissionsBoundaryType':'Policy'},'exact-role-boundary')
        duration=decoder.decimal_integer(role['MaxSessionDuration']);require(duration==next(r['maxSessionDuration'] for r in bound['preparation']['proposal']['inputs']['firstProvisionPlan']['roles'] if r['name']==name),'exact-role-duration')
        trust=decoder.policy(role['AssumeRolePolicyDocument']);require(m.same(trust,bound['documents'][name+'.trust.json']),'whole-role-trust')
        role_tags=tags(role['Tags'],expected_tags);tags(page(iam['role.'+name+'.ListRoleTags'],'Tags'),expected_tags)
        require(page(iam['role.'+name+'.ListAttachedRolePolicies'],'AttachedPolicies')==[] and page(iam['role.'+name+'.ListRolePolicies'],'PolicyNames')==['hikyo-158-fixture'],'exact-role-policy-inventory')
        policy=iam['role.'+name+'.GetRolePolicy'];exact(policy,{'RoleName','PolicyName','PolicyDocument'})
        require(policy['RoleName']==name and policy['PolicyName']=='hikyo-158-fixture','inline-policy-identity');document=decoder.policy(policy['PolicyDocument'])
        require(m.same(document,bound['documents'][name+'.policy.json']),'whole-inline-role-policy')
        roles[name]={'Arn':role['Arn'],'RoleName':name,'RoleId':role['RoleId'],'Path':'/','MaxSessionDuration':duration,'Tags':role_tags,
            'PermissionsBoundary':dict(role['PermissionsBoundary']),'AssumeRolePolicyDocument':trust,'inlinePolicies':{'hikyo-158-fixture':document},'attachedPolicies':[]}
    meta=iam['boundary.GetPolicy'];exact(meta,{'Policy'});meta=meta['Policy']
    required={'PolicyName','PolicyId','Arn','Path','DefaultVersionId','AttachmentCount','PermissionsBoundaryUsageCount','IsAttachable','CreateDate','UpdateDate'}
    require(type(meta) is dict and required<=set(meta)<=required|{'Description','Tags'} and meta['PolicyName']=='hikyo-158-role-boundary'
        and meta['PolicyId']==boundary_id and meta['Arn']==m.BOUNDARY and meta['Path']=='/' and meta['DefaultVersionId']=='v1'
        and meta['IsAttachable']=='true' and decoder.decimal_integer(meta['AttachmentCount'])==0 and decoder.decimal_integer(meta['PermissionsBoundaryUsageCount'])==8
        and decoder.instant(meta['CreateDate'])<=decoder.instant(meta['UpdateDate'])<=explicit_now,'whole-new-boundary-metadata')
    require(('Tags' not in meta or members(meta['Tags'])==[]) and ('Description' not in meta or meta['Description']==''),'boundary-no-extra-tags-description')
    versions=page(iam['boundary.ListPolicyVersions'],'Versions');require(len(versions)==1,'boundary-sole-v1');version=versions[0]
    exact(version,{'VersionId','IsDefaultVersion','CreateDate'});require(version['VersionId']=='v1' and version['IsDefaultVersion']=='true','boundary-default-v1')
    row=iam['boundary.GetPolicyVersion.v1'];exact(row,{'PolicyVersion'});row=row['PolicyVersion'];exact(row,{'VersionId','IsDefaultVersion','CreateDate','Document'})
    require(row['VersionId']=='v1' and row['IsDefaultVersion']=='true' and decoder.instant(row['CreateDate'])==decoder.instant(version['CreateDate'])<=explicit_now,'boundary-version-metadata')
    document=decoder.policy(row['Document']);require(m.same(document,bound['documents']['setup-role-boundary.json']),'whole-boundary-document')
    boundary={'arn':m.BOUNDARY,'policyId':boundary_id,'defaultVersionId':'v1','versions':{'v1':{'document':document,'isDefault':True}}}
    providers=iam['provider.ListOpenIDConnectProviders'];exact(providers,{'OpenIDConnectProviderList'});all_providers=members(providers['OpenIDConnectProviderList'])
    require(len(all_providers)==1,'sole-provider');listed=all_providers[0]
    require(type(listed) is dict and {'Arn'}<=set(listed)<={'Arn','IsTrustedIdentityPropagationEnabled'} and listed['Arn']==m.PROVIDER
        and listed.get('IsTrustedIdentityPropagationEnabled','false')=='false','sole-exact-provider')
    provider=iam['provider.GetOpenIDConnectProvider'];exact(provider,{'Url','ClientIDList','ThumbprintList','Tags','CreateDate'})
    date=decoder.instant(provider['CreateDate']);require(m.instant(bound['run']['start'])<=date<=explicit_now,'provider-whole-date')
    thumbs=members(provider['ThumbprintList']);require(provider['Url']=='token.actions.githubusercontent.com' and members(provider['ClientIDList'])==['sts.amazonaws.com']
        and 1<=len(thumbs)<=2 and all(m.sha(t,40) for t in thumbs) and len(set(thumbs))==len(thumbs),'exact-provider-metadata')
    provider={'arn':m.PROVIDER,'allProviders':[{'Arn':m.PROVIDER}],'metadata':{'Url':provider['Url'],'ClientIDList':['sts.amazonaws.com'],
        'ThumbprintList':thumbs,'Tags':tags(provider['Tags'],expected_tags),'CreateDate':m.stamp(date)}}
    key=kms['new.DescribeKey'];exact(key,{'KeyMetadata'});key=key['KeyMetadata'];require(m.same(key,bound['records']['key']['readback']['metadata']),'whole-new-key-metadata')
    key_policy=kms['new.GetKeyPolicy'];exact(key_policy,{'Policy','PolicyName'});require(key_policy['PolicyName']=='default' and type(key_policy['Policy']) is str,'default-key-policy')
    key_policy=m.strict_json(key_policy['Policy'].encode());require(m.same(key_policy,bound['records']['key']['readback']['policy']),'whole-root-delegation-key-policy')
    rotation=kms['new.GetKeyRotationStatus'];exact(rotation,{'KeyId','KeyRotationEnabled'});require(rotation['KeyId']==bound['run']['keyARN'] and rotation['KeyRotationEnabled'] is False,'rotation-disabled')
    aliases=kms['new.ListAliases'];require(type(aliases) is dict and {'Aliases','Truncated'}<=set(aliases)<={'Aliases','Truncated','AliasCount'}
        and aliases['Aliases']==[] and aliases['Truncated'] is False and ('AliasCount' not in aliases or type(aliases['AliasCount']) is int and aliases['AliasCount']==0),'complete-zero-aliases')
    old={}
    expected_old=m.scheduled_key_closure(bound['preparation']['proposal'])
    for i,key_id in enumerate(OLD_IDS,1):
        value=kms['old'+str(i)+'.DescribeKey'];exact(value,{'KeyMetadata'});arn=f'arn:aws:kms:{m.REGION}:{m.ACCOUNT}:key/'+key_id
        require(m.same(value['KeyMetadata'],expected_old[arn]['metadata']) and value['KeyMetadata']['KeyState']=='PendingDeletion' and value['KeyMetadata']['Enabled'] is False,'all-four-whole-old-schedules-unchanged');old[arn]=value['KeyMetadata']
    earliest=min([*times1,*times2,setup['provenance']['earliestActualSentAtMS']]);observed=stamp_ms(earliest);owner['observedAt']=observed
    inventory={'schema':'hikyo-158/next-effective-inventory/v1','observedAt':observed,'roles':roles,'provider':provider,'boundary':boundary,
        'key':{'metadata':key,'policy':key_policy,'rotationEnabled':False,'aliases':[],'Truncated':False},'oldKeys':old}
    m.creation_custody_checked(custody['operations'],bound,inventory,explicit_now)
    require(custody['provider']=={'operation':'CreateOpenIDConnectProvider','arn':m.PROVIDER,'metadataSHA256':m.digest(provider['metadata'])},'provider-create-custody')
    latest=m.instant(custody['operations'][-1]['receivedAt']);require(latest<=m.instant(observed),'all-inventory-reads-after-creations')
    return {'schema':'hikyo-158/next-native-inventory-normalized/v1','owner':owner,'inventory':inventory,
        'provenance':{'modelSHA256':MODEL_SHA,'modelTestSHA256':TEST_SHA,'setupNormalizerSHA256':SETUP_DECODER_SHA,'earliestActualSentAtMS':earliest,
            'iam':proof1,'kms':proof2,'setup':setup['provenance'],'creationCustodySHA256':m.digest(custody),'boundPlanSHA256':m.digest(bound)},
        'runtimeReady':False,'externalAuthenticityVerifiedByNormalizer':False,'providerAPIsCalledByNormalizer':False,'executionAuthorityGranted':False}


def normalize_restored_owner(m,decoder,source_pins,sealed,iam_bundle,setup_bundle,*,owner_context_sha256,explicit_now):
    m.clock(explicit_now);m.sealed_checked(sealed);bound=sealed['boundPlan'];custody=sealed['records']['custody'];source_checked(m,source_pins,bound)
    data,times,provenance=native_bundle(m,decoder,bound,custody,iam_bundle,'iam','restored-setup',owner_context_sha256,explicit_now)
    owner,setup=owner_record(m,decoder,bound,data,setup_bundle,owner_context_sha256,explicit_now,'restored-setup')
    earliest=min([*times,setup['provenance']['earliestActualSentAtMS']]);owner['observedAt']=stamp_ms(earliest)
    m.restored_setup_checked(sealed,owner,m.digest(owner),explicit_now=explicit_now)
    return {'schema':'hikyo-158/next-native-restored-owner-normalized/v1','owner':owner,'provenance':{'iam':provenance,'setup':setup['provenance'],'earliestActualSentAtMS':earliest},
        'retirementVerifiedByNormalizer':False,'runtimeReady':False,'externalAuthenticityVerifiedByNormalizer':False,'providerAPIsCalledByNormalizer':False,'executionAuthorityGranted':False}


def baseline_manifest(m,service,not_before_ms,expires_at_ms,owner_context_sha256,reviewed_preparation_sha256):
    """Explicit read-only preparation reference, with no new resource identity."""
    require(service in ('iam','kms') and m.sha(owner_context_sha256) and m.sha(reviewed_preparation_sha256),'baseline-source-references')
    return {'schema':'hikyo-158/next-inventory-observer-manifest/v1','service':service,'stage':'baseline','account':m.ACCOUNT,'region':m.REGION,
        **{k:None for k in ('newKeyARN','boundaryPolicyId','roleIds','boundPlanSHA256','creationCustodySHA256','newKeyCustodySHA256')},
        'ownerContextSHA256':owner_context_sha256,'reviewedPreparationSHA256':reviewed_preparation_sha256,
        'publicMainSHA256':MAIN_IAM if service=='iam' else MAIN_KMS,'publicVendorsSHA256':VENDORS if service=='iam' else None,
        'notBeforeMS':not_before_ms,'expiresAtMS':expires_at_ms}


def absent_boundary(raw):
    """Only actual typed IAM HTTP404 NoSuchEntity, never an ambiguous failure."""
    require(not re.search(r'<!\s*(?:DOCTYPE|ENTITY)',raw,re.I),'absence-no-dtd')
    try:root=ET.fromstring(raw,parser=ET.XMLParser(target=ET.TreeBuilder(insert_comments=True,insert_pis=True)))
    except ET.ParseError as error:raise ValueError('next-inventory:absence-xml-parse') from error
    ns='https://iam.amazonaws.com/doc/2010-05-08/'
    def tree(node,depth=0):
        require(depth<=8 and type(node.tag) is str and node.tag.startswith('{'+ns+'}') and not node.attrib and not(node.tail or '').strip(),'absence-xml-shape')
        kids=list(node)
        if not kids:return node.text or ''
        require(not(node.text or '').strip(),'absence-mixed');names=[x.tag.removeprefix('{'+ns+'}') if type(x.tag) is str else None for x in kids]
        require(None not in names and len(names)==len(set(names)),'absence-duplicate');return {k:tree(v,depth+1) for k,v in zip(names,kids)}
    require(root.tag=='{'+ns+'}ErrorResponse','absence-error-envelope');value=tree(root);exact(value,{'Error','RequestId'});exact(value['Error'],{'Type','Code','Message'})
    error=value['Error'];require(error['Type']=='Sender' and error['Code']=='NoSuchEntity' and type(error['Message']) is str and 1<=len(error['Message'])<=2048
        and type(value['RequestId']) is str and re.fullmatch(r'[A-Za-z0-9-]{1,128}',value['RequestId']),'typed-absence-only')
    return value


def normalize_baseline(m,decoder,source_pins,proposal,iam_bundle,kms_bundle,setup_bundle,*,owner_context,environment,reviewed_preparation_sha256,explicit_now):
    """Fresh pre-key metadata projection. Root authenticates all external sources."""
    m.clock(explicit_now);m.proposal_checked(proposal);m.exact(source_pins,PINS);require(m.same(source_pins,PINS),'reviewed-source-pins')
    owner_sha=m.digest(owner_context)
    iam,t1,p1=native_bundle(m,decoder,None,None,iam_bundle,'iam','baseline',owner_sha,explicit_now,reviewed_preparation_sha256)
    kms,t2,p2=native_bundle(m,decoder,None,None,kms_bundle,'kms','baseline',owner_sha,explicit_now,reviewed_preparation_sha256)
    mfa=user_checked(m,decoder,proposal,iam,explicit_now)
    exact(setup_bundle,{'manifest','rows'});given=setup_bundle['manifest']
    require(type(given) is dict and given.get('stage')=='beforeDelete' and given.get('reviewedPreparationSHA256')==reviewed_preparation_sha256,'baseline-whole-original-setup')
    setup=decoder.normalize(given,setup_bundle['rows'],owner_sha,None,epoch_ms(explicit_now));setup['provenance']['retainedMFADevice']=mfa
    roles=[]
    for role in page(iam['roles.ListRoles'],'Roles'):
        required={'Path','RoleName','RoleId','Arn','CreateDate','AssumeRolePolicyDocument','MaxSessionDuration'}
        require(type(role) is dict and required<=set(role)<=required|{'Description','PermissionsBoundary','Tags','RoleLastUsed'},'whole-list-role-metadata')
        require(type(role['Path']) is str and role['Arn']==f'arn:aws:iam::{m.ACCOUNT}:role'+role['Path']+role['RoleName']
            and decoder.instant(role['CreateDate'])<=explicit_now,'actual-role-path-custody')
        decoder.policy(role['AssumeRolePolicyDocument']);decoder.decimal_integer(role['MaxSessionDuration'])
        roles.append({k:role[k] for k in ('RoleName','RoleId','Arn')})
    providers=iam['provider.ListOpenIDConnectProviders'];exact(providers,{'OpenIDConnectProviderList'});require(members(providers['OpenIDConnectProviderList'])==[],'baseline-zero-providers')
    old={}
    for i,key_id in enumerate(OLD_IDS,1):
        value=kms['old'+str(i)+'.DescribeKey'];exact(value,{'KeyMetadata'});arn=f'arn:aws:kms:{m.REGION}:{m.ACCOUNT}:key/'+key_id
        require(m.same(value['KeyMetadata'],m.scheduled_key_closure(proposal)[arn]['metadata']),'baseline-four-old-whole-schedules');old[arn]=value['KeyMetadata']
    earliest=min([*t1,*t2,setup['provenance']['earliestActualSentAtMS']])
    proof={'schema':'hikyo-158/next-owner-baseline/v1','observedAt':stamp_ms(earliest),'ownerContext':copy.deepcopy(owner_context),
        'setupUser':{'UserName':m.USER,'UserId':m.USER_ID,'Arn':m.USER_ARN,'MFADevices':[{'UserName':m.USER,'SerialNumber':m.MFA}],
            'accessKeys':[],'groups':[],'inlinePolicyNames':[],'attachedPolicyARNs':[m.SETUP]},'setupPolicy':setup['proof']['policy'],
        'roles':{'Roles':roles,'IsTruncated':False},'providers':{'OpenIDConnectProviderList':[]},
        'boundaryAbsent':{'operation':'GetPolicy','arn':m.BOUNDARY,'errorCode':'NoSuchEntity'},'oldKeys':old,'environment':copy.deepcopy(environment)}
    m.owner_checked(proof,proposal,explicit_now)
    return {'schema':'hikyo-158/next-native-baseline-normalized/v1','owner':proof,
        'provenance':{'modelSHA256':MODEL_SHA,'modelTestSHA256':TEST_SHA,'setupNormalizerSHA256':SETUP_DECODER_SHA,'earliestActualSentAtMS':earliest,
            'iam':p1,'kms':p2,'setup':setup['provenance'],'externalOwnerContextSHA256':owner_sha,'externalEnvironmentSHA256':m.digest(environment)},
        'runtimeReady':False,'externalAuthenticityVerifiedByNormalizer':False,'providerAPIsCalledByNormalizer':False,'executionAuthorityGranted':False}
