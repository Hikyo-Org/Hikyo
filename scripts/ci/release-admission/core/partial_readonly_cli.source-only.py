"""SOURCE ONLY: fixed readonly official CLI calls through an injected runner.

No default launcher, subprocess, credential/cache read, auth implementation,
write, retry, policy upgrade, fixture creation, retirement or dispatch exists.
Identity/MFA/wire patterns are copied from the reviewed V OfficialFixtureCLI.
"""
import copy
from datetime import timedelta
import hashlib
import re

POLICY='hikyo-158-fixture'


class ReadonlyHold(RuntimeError):
    def __init__(self,custody):
        super().__init__('Readonly metadata held; no retry; Root retains original private command outcome')
        self.custody=copy.deepcopy(custody)


def collect_partial_readonly(m,bound,inventory,owner_stage,boundary_readback,*,known_role_ids,runner=None,explicit_clock=None,login_root=None,base_environment=None,deadline=None):
    """Collect only currently present fixed names, preserving successful raw JSON.

    Injected runner calls may be real provider reads: this is transport, not an
    authenticator. Root must authenticate its exact source/runtime/session and
    the actual literal-MFA v6 policy before injection. Never set a write adapter
    active or invent a phase to obtain these independent readonly calls.
    """
    m.require(callable(runner) and callable(explicit_clock),'no-default-readonly-runner-or-clock')
    m.bound_checked(bound,m.instant(bound['derivedAt']));now=explicit_clock();m.clock(now)
    m.exact(owner_stage,{'observedAt','setupUser','setupPolicy'});m.fresh(owner_stage['observedAt'],now)
    m.require(m.same(owner_stage['setupUser'],bound['preparation']['records']['owner']['setupUser']),'retained-setup-identity')
    m.setup_policy_checked(owner_stage['setupPolicy'],bound['preparation']['proposal']['inputs']['iamClosure']['setupPolicy'],{'v2','v3','v4','v5','v6'},'v6',bound['documents'])
    m.exact(boundary_readback,{'observedAt','value'});m.fresh(boundary_readback['observedAt'],now);boundary=boundary_readback['value']
    m.exact(boundary,{'arn','policyId','defaultVersionId','versions'})
    m.require(boundary['arn']==m.BOUNDARY and type(boundary['policyId']) is str and re.fullmatch(r'ANPA[A-Z0-9]{17}',boundary['policyId']) and boundary['policyId'] not in (m.SETUP_ID,'ANPAROHA7PNS5QVDJKPIN') and boundary['defaultVersionId']=='v1' and m.same(boundary['versions'],{'v1':{'isDefault':True,'document':bound['documents']['setup-role-boundary.json']}}),'whole-current-boundary-identity')
    m.require(type(login_root) is str and login_root.startswith('/') and '..' not in login_root.split('/') and '\x00' not in login_root and login_root.endswith('/private-operator/aws-local-login'),'opaque-approved-login-location')
    m.exact(base_environment,{'PATH','LANG','TMPDIR'})
    m.require(base_environment['PATH']=='/opt/homebrew/bin:/usr/bin:/bin' and base_environment['LANG'] in ('C','C.UTF-8') and type(base_environment['TMPDIR']) is str and base_environment['TMPDIR'].startswith('/') and '\x00' not in base_environment['TMPDIR'],'closed-clean-base-environment')
    m.clock(deadline);m.require(now<deadline<=m.instant(bound['run']['cleanupUntil']) and deadline<=m.instant(m.HORIZON),'finite-existing-cleanup-deadline')
    m.require(type(known_role_ids) is dict and set(known_role_ids)<=set(m.ROLES),'known-partial-role-map')
    for role,role_id in known_role_ids.items():m.require(type(role_id) is str and re.fullmatch(r'AROA[A-Z0-9]{8,64}',role_id),'actual-known-role-id')
    m.require(len(set(known_role_ids.values()))==len(known_role_ids),'distinct-known-role-ids')
    m.exact(inventory,{'schema','observedAt','roles','allRoles','provider','allProviders','source'});m.exact(inventory['roles'],m.ROLES)
    m.require(inventory['schema']=='hikyo-158/partial-inventory-derived/v1','checked-native-list-inventory-required')
    m.fresh(inventory['observedAt'],now)
    for name,row in inventory['roles'].items():
        m.require(type(row) is dict and row.get('schema') in ('hikyo-158/inventory-observed-role/v1','hikyo-158/inventory-derived-role-absence/v1'),'closed-role-observation')
        m.exact(row,{'schema','RoleName','RoleId','Arn','listReceiptSHA256'} if row['schema']=='hikyo-158/inventory-observed-role/v1' else {'schema','RoleName','inlinePolicyAbsence','listReceiptSHA256'})
        m.require(row['RoleName']==name,'fixed-role-observation')
    present={name:row for name,row in inventory['roles'].items() if row['schema']=='hikyo-158/inventory-observed-role/v1'}
    m.require(set(inventory['roles'])==set(m.ROLES) and all(row['RoleId']==known_role_ids[name] for name,row in present.items() if name in known_role_ids),'actual-known-identity-match')
    environment={**base_environment,'AWS_CONFIG_FILE':login_root+'/config','AWS_SHARED_CREDENTIALS_FILE':login_root+'/credentials','AWS_LOGIN_CACHE_DIRECTORY':login_root+'/cache',
                 'AWS_REGION':m.REGION,'AWS_DEFAULT_REGION':m.REGION,'AWS_STS_REGIONAL_ENDPOINTS':'regional','AWS_EC2_METADATA_DISABLED':'true','AWS_IGNORE_CONFIGURED_ENDPOINT_URLS':'true','AWS_CLI_AUTO_PROMPT':'off','AWS_PAGER':'','AWS_MAX_ATTEMPTS':'1','AWS_RETRY_MODE':'standard'}
    receipts=[];total=0

    def read(operation,role=None):
        nonlocal total
        m.require(len(receipts)<48,'finite-fixed-read-count')
        fixed={'Identity':['sts','get-caller-identity'],'User':['iam','get-user'],'MFA':['iam','list-mfa-devices','--user-name',m.USER,'--no-paginate'],
               'Roles':['iam','list-roles','--no-paginate'],'Providers':['iam','list-open-id-connect-providers'],
               'Provider':['iam','get-open-id-connect-provider','--open-id-connect-provider-arn',m.PROVIDER]}
        if operation in fixed:m.require(role is None,'no-extra-role-selector');parts=fixed[operation]
        else:
            m.require(role in present,'only-actually-listed-fixed-role')
            by_role={'Role':['iam','get-role','--role-name',role],'Inline':['iam','list-role-policies','--role-name',role,'--no-paginate'],
                     'Attached':['iam','list-attached-role-policies','--role-name',role,'--no-paginate'],'Tags':['iam','list-role-tags','--role-name',role,'--no-paginate'],
                     'Policy':['iam','get-role-policy','--role-name',role,'--policy-name',POLICY]}
            m.require(operation in by_role,'closed-readonly-registry');parts=by_role[operation]
        args=['/opt/homebrew/bin/aws',*parts,'--profile',m.USER,'--region',m.REGION,'--no-cli-pager','--no-cli-auto-prompt','--output','json','--cli-connect-timeout','10','--cli-read-timeout','20']
        sent=explicit_clock();m.clock(sent);remaining=(deadline-sent).total_seconds();m.require(0<remaining and now<=sent,'adjacent-finite-read-deadline')
        # Exactly one runner call. Neither CLI nor this code is given a retry.
        try:result=runner(args,env=copy.deepcopy(environment),timeout=min(30,remaining))
        except Exception:raise ReadonlyHold({'operation':operation,'role':role,'argumentsSHA256':m.digest(args),'outcome':'runner-exception-unknown','resendPermitted':False}) from None
        received=explicit_clock();m.clock(received);m.exact(result,{'returncode','stdout','stderr'})
        m.require(type(result['returncode']) is int and type(result['stdout']) is bytes and type(result['stderr']) is bytes and len(result['stdout'])<=65536 and len(result['stderr'])<=32768,'bounded-original-command-outcome')
        diagnostic={'operation':operation,'role':role,'argumentsSHA256':m.digest(args),'sentAt':m.stamp(sent),'receivedAt':m.stamp(received),'returncode':result['returncode'],
                    'stdoutSHA256':hashlib.sha256(result['stdout']).hexdigest(),'stderrSHA256':hashlib.sha256(result['stderr']).hexdigest(),'stdoutBytes':len(result['stdout']),'stderrBytes':len(result['stderr']),'resendPermitted':False}
        if result['returncode']!=0 or result['stderr'] or not(sent<=received<deadline and received-sent<=timedelta(seconds=30)):
            raise ReadonlyHold(diagnostic)
        try:
            parsed=m.strict_json(result['stdout']);m.require(type(parsed) is dict,'closed-public-json-object')
            def safe(value):
                if type(value) is dict:
                    m.require(not any(k.lower() in {'password','secretstring','secretbinary','secretaccesskey','accesskeyid','sessiontoken','securitytoken','accesstoken','refreshtoken','clientsecret','authorization','token'} for k in value),'unexpected-credential-key')
                    for child in value.values():safe(child)
                elif type(value) is list:
                    for child in value:safe(child)
            safe(parsed);raw=result['stdout'].decode('utf-8',errors='strict');m.require(raw.encode()==result['stdout'],'lossless-stdout')
        except Exception:raise ReadonlyHold(diagnostic) from None
        total+=len(result['stdout']);m.require(total<=524288,'bounded-total-public-custody')
        receipts.append({**diagnostic,'arguments':args,'rawStdout':raw,'value':copy.deepcopy(parsed)})
        return parsed

    identity=read('Identity');m.require(m.same(identity,{'Account':m.ACCOUNT,'Arn':m.USER_ARN,'UserId':m.USER_ID}),'exact-retained-nonroot-principal')
    user=read('User').get('User',{});m.require(user.get('UserId')==m.USER_ID and user.get('UserName')==m.USER and user.get('Arn')==m.USER_ARN,'actual-cli-user')
    devices=read('MFA');m.require(devices.get('IsTruncated') is False and not devices.get('Marker') and type(devices.get('MFADevices')) is list and len(devices['MFADevices'])==1,'complete-sole-cli-mfa')
    actual=devices['MFADevices'][0];m.exact(actual,{'UserName','SerialNumber','EnableDate'});original=bound['preparation']['proposal']['inputs']['iamClosure']['mfaDevices'][0]
    def enrollment(value):
        m.require(type(value) is str and re.fullmatch(r'\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d(?:\.\d{1,6})?(?:Z|\+00:00)',value),'utc-mfa-date')
        return m.instant(value.replace('+00:00','Z'))
    m.require(actual['UserName']==m.USER and actual['SerialNumber']==m.MFA and enrollment(actual['EnableDate'])==enrollment(original['EnableDate']),'original-retained-mfa-enrollment')
    listed=read('Roles');m.exact(listed,{'Roles','IsTruncated'});m.require(listed['IsTruncated'] is False and type(listed['Roles']) is list,'literal-mfa-v6-complete-role-list')
    names=set();ids=set()
    for row in listed['Roles']:
        m.require(type(row) is dict and type(row.get('RoleName')) is str and type(row.get('RoleId')) is str and row['RoleName'].casefold() not in names and row['RoleId'] not in ids,'unique-cli-role-name-and-id')
        m.require(row['RoleName'].casefold() not in {name.casefold() for name in m.ROLES} or row['RoleName'] in m.ROLES,'no-cli-casefold-fixture-ambiguity')
        names.add(row['RoleName'].casefold());ids.add(row['RoleId'])
    actual_present={row['RoleName']:row for row in listed['Roles'] if row['RoleName'] in m.ROLES}
    m.require(set(actual_present)==set(present) and all(actual_present[name].get('RoleId')==present[name]['RoleId'] and actual_present[name].get('Arn')==present[name]['Arn'] for name in present),'same-native-and-cli-actual-identities')
    providers=read('Providers');m.require(m.same(providers,inventory['allProviders']),'same-complete-provider-inventory')
    old=set(bound['preparation']['proposal']['inputs']['firstEpoch']['inventory']['roleIds'].values());roles={name:None for name in m.ROLES}
    for name in m.ROLES:
        if name not in present:continue
        value=read('Role',name);m.exact(value,{'Role'});role=value['Role'];role_id=role.get('RoleId')
        m.require(role.get('RoleName')==name and role.get('Arn')==f'arn:aws:iam::{m.ACCOUNT}:role/'+name and role.get('Path')=='/' and role_id==present[name]['RoleId'] and role_id not in old,'actual-new-role-identity')
        expected_duration=next(row['maxSessionDuration'] for row in bound['preparation']['proposal']['inputs']['firstProvisionPlan']['roles'] if row['name']==name)
        m.require(type(role.get('MaxSessionDuration')) is int and role['MaxSessionDuration']==expected_duration,'exact-role-duration')
        m.require(m.same(role.get('AssumeRolePolicyDocument'),bound['documents'][name+'.trust.json']) and role.get('PermissionsBoundary')=={'PermissionsBoundaryArn':m.BOUNDARY,'PermissionsBoundaryType':'Policy'},'whole-exact-role-policy-and-tags')
        m.fixture_tags_checked(role.get('Tags'),bound['run']['authorizationId'])
        inline=read('Inline',name);m.exact(inline,{'PolicyNames','IsTruncated'});m.require(inline['IsTruncated'] is False and inline['PolicyNames'] in ([],[POLICY]),'complete-owned-inline-inventory')
        attached=read('Attached',name);m.require(m.same(attached,{'AttachedPolicies':[],'IsTruncated':False}),'no-attached-managed-policy')
        tags=read('Tags',name);m.exact(tags,{'Tags','IsTruncated'});m.require(tags['IsTruncated'] is False,'complete-retained-role-tags')
        m.fixture_tags_checked(tags['Tags'],bound['run']['authorizationId'])
        policies={}
        if inline['PolicyNames']:
            policy=read('Policy',name);m.exact(policy,{'RoleName','PolicyName','PolicyDocument'});m.require(policy['RoleName']==name and policy['PolicyName']==POLICY and m.same(policy['PolicyDocument'],bound['documents'][name+'.policy.json']),'whole-exact-inline-policy');policies[POLICY]=policy['PolicyDocument']
        roles[name]={'roleId':role_id,'inlinePolicyNames':inline['PolicyNames'],'attachedPolicyARNs':[],'trust':role['AssumeRolePolicyDocument'],'inlinePolicies':policies,'boundaryARN':m.BOUNDARY}
    provider=None
    if inventory['provider']['schema']=='hikyo-158/inventory-observed-provider/v1':
        data=read('Provider');m.exact(data,{'Url','ClientIDList','ThumbprintList','Tags','CreateDate'})
        m.require(data['Url']=='token.actions.githubusercontent.com' and data['ClientIDList']==['sts.amazonaws.com'] and type(data['ThumbprintList']) is list and 1<=len(data['ThumbprintList'])<=2 and len(set(data['ThumbprintList']))==len(data['ThumbprintList']) and all(m.sha(t,40) for t in data['ThumbprintList']),'whole-exact-provider')
        m.fixture_tags_checked(data['Tags'],bound['run']['authorizationId']);provider=m.PROVIDER
    final=explicit_clock();m.clock(final);m.require(now<=final<deadline and final-now<=timedelta(minutes=5),'complete-fresh-collection')
    return {'schema':'hikyo-158/partial-readonly-cli-metadata/v1','observedAt':m.stamp(now),'boundPlanSHA256':m.digest(bound),'nativeInventorySHA256':m.digest(inventory),
            'boundaryReadbackSHA256':m.digest(boundary_readback),
            'retirementInventory':{'observedAt':m.stamp(now),'roles':roles,'provider':provider,'keyARN':bound['run']['keyARN'],'boundaryPolicyId':boundary['policyId']},
            'receipts':receipts,'providerReadRunnerCalls':len(receipts),'writeRunnerCalls':0,'automaticRetry':False,'defaultLiveRunnerAvailable':False,
            'externalAuthenticityVerifiedByCollector':False,'authenticationVerifiedByCollector':False,'retirementAuthorityGrantedByCollector':False}
