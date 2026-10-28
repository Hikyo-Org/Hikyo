"""Chrome copy of explicit local data assembly only; no clock/auth/browser/provider/claim default.

Pure functions receive reviewed modules and actual data. Production loaders and CLI entry points are intentionally absent. Root supplies modules and custody.
Root authenticity and execution releases remain external. Import performs no I/O.
"""
import copy
from datetime import datetime
import hashlib
import re


PINS = {'inventory': ('next-run-source-preparation/banked-series-first-attempt-source/normalize_next_inventory.source-only.py', '9fbe10b6284c259c80e2138d99de1d041ed27f19fa8c3f4fe5d789705eb03299'), 'setup': ('next-run-source-preparation/banked-series-first-attempt-source/normalize_setup_whole_reads.source-only.py', '2b70d44ec3cf19e5893e5f7c7b6578762536c28c514cffeb218076d7d2829221'), 'inventoryObserver': ('next-run-source-preparation/banked-series-first-attempt-source/native/native_next_inventory_prearm_observer.source-only.js', 'aded6e0d94d0733b7ff2108e36bb1bbcae3e7984ddf5843b07c1808eb3564966'), 'setupObserver': ('next-run-source-preparation/banked-series-first-attempt-source/native/native_setup_whole_read_prearm_observer.source-only.js', 'c2ee8aa79631fbc08dcb18696f7c40736bd6429e63ad31cefb7b645d342ec914'), 'historicalObserver': ('next-run-source-preparation/banked-series-first-attempt-source/native/native_historical_describe_observer_per_preparation.source-only.js', '7e127ae676b4c8c16e7105881f429a18e4e28f2232e14b549603c67ea60120fd'), 'historicalGate': ('next-run-source-preparation/banked-series-first-attempt-source/native/native_historical_describe_transport_gate_per_preparation.source-only.js', '12583c98990def7f73dd63e06f7e3ca2f40f4e08b6934f265bd14b04fd7d8ffb')}
SM_PINS = {'publicMainSHA256': 'd412372053a5b7a265fa704e4d483522f4cff28e93fcf8d46ab7f18f797c2520',
 'publicRuntimeSHA256': 'f935a35837023598fea6628ce95690c4de2fda88dd8a7f9b8a7cb8b01b824577',
 'publicVendorsSHA256': '345320425b972ad1c068f2780859d68ff4c41714d6c083749e08b3c3f6724a01',
 'publicReactSHA256': '9f67b611afa13d1bf1bd403e6f7eba45eff50265b5d2d0e5b3fd20ba9409343b'}
PLAN_KEYS = {'schema','account','region','ownerContextSHA256','recordedAt','purpose','maximumMilliseconds','fixtureReservationPermitted','writesPermitted'}
HISTORY_KEYS = {'schema','account','region','ownerContext','ownerContextSHA256','chromeCommand','preparationCommandCustody','observerSourceSHA256','gateSourceSHA256',
 'publicSourcePins','readIntent','notBeforeMS','expiresAtMS','captureTimeoutMS','purpose','recordedAt','fixtureAdmissionGranted','fixtureReservation','resourceMutation'}
ENV_ENDPOINT = 'repos/Hikyo-Org/Hikyo/environments/hikyo-158-aws-sandbox'
ENV_ENDPOINTS = {'environment': ENV_ENDPOINT, 'variables': ENV_ENDPOINT+'/variables?per_page=100&page=1',
 'branches': ENV_ENDPOINT+'/deployment-branch-policies?per_page=100&page=1', 'oidcDefaults': 'repos/Hikyo-Org/Hikyo/actions/oidc/customization/sub'}
ASSEMBLY_INPUTS = {'proposal','ownerContext','capturePlan','iamBundle','kmsBundle','setupBundle','historicalPreparation','historicalRecovery',
                  'environmentCaptures','code','budget'}




def _context(m, source, now):
    m.exact(source, {'schema','capturedAt','pageURL','accountHeaderText','principalARN'})
    m.fresh(source['capturedAt'], now)
    m.require(source['schema']=='hikyo-158/native-owner-context-projection/v1'
        and source['pageURL']=='https://us-east-1.console.aws.amazon.com/iam/home?region=eu-central-1#/home'
        and source['accountHeaderText']==m.ACCOUNT and type(source['principalARN']) is str
        and source['principalARN']!=m.USER_ARN
        and re.fullmatch(r'arn:aws:(?:iam|sts)::'+m.ACCOUNT+r':(?:root|user/[A-Za-z0-9+=,.@_/-]{1,200}|assumed-role/[A-Za-z0-9+=,.@_/-]{1,200})',source['principalARN']), 'assembly-original-owner-projection')
    return {'account':m.ACCOUNT,'principalARN':source['principalARN'],'source':'native-owner-console',
            'observedAt':source['capturedAt'],'sourceReceipt':copy.deepcopy(source)}


def _read_intent(m):
    return {'operation':'DescribeSecret','targetARN':m.HISTORICAL,'endpoint':f'https://secretsmanager.{m.REGION}.amazonaws.com/',
            'target':'secretsmanager.DescribeSecret','maximumForwards':1,'allowDelete':False}


def _metadata_reference(m, value):
    m.exact(value,PLAN_KEYS)
    m.require(value['schema']=='hikyo-158/root-final-metadata-capture-plan/v1'
        and value['account']==m.ACCOUNT and value['region']==m.REGION
        and type(value['maximumMilliseconds']) is int and value['maximumMilliseconds']==300000
        and value['fixtureReservationPermitted'] is False and value['writesPermitted'] is False
        and type(value['purpose']) is str and 0<len(value['purpose'])<=4096, 'assembly-inert-metadata-reference')


def _historical_reference(m, value, custody):
    m.exact(value,HISTORY_KEYS)
    custody.command_context_checked(m,value['chromeCommand'])
    command_custody=value['preparationCommandCustody']
    m.exact(command_custody,{'originalRawSHA256','originalSHA256','originalSequence','commandContextSHA256'})
    m.require(value['chromeCommand']['kind']=='identity'
        and all(m.sha(command_custody[k]) for k in ('originalRawSHA256','originalSHA256','commandContextSHA256'))
        and type(command_custody['originalSequence']) is int and command_custody['originalSequence']>=0
        and command_custody['commandContextSHA256']==m.digest(value['chromeCommand']), 'chrome-history-preparation-command-custody')
    m.require(value['schema']=='hikyo-158/persistent-chrome-historical-read-preparation/v1'
        and value['account']==m.ACCOUNT and value['region']==m.REGION
        and value['observerSourceSHA256']==PINS['historicalObserver'][1] and value['gateSourceSHA256']==PINS['historicalGate'][1]
        and m.same(value['publicSourcePins'],SM_PINS) and m.same(value['readIntent'],_read_intent(m))
        and type(value['readIntent']['maximumForwards']) is int
        and type(value['captureTimeoutMS']) is int and 1000<=value['captureTimeoutMS']<=20000
        and type(value['purpose']) is str and 0<len(value['purpose'])<=4096
        and all(value[k] is False for k in ('fixtureAdmissionGranted','fixtureReservation','resourceMutation')),
        'assembly-inert-historical-reference')


def plan(modules, owner_source, metadata_reference, historical_reference, *, chrome_command,
         original_raw, connection, sequence_min, sequence_max, explicit_now, custody):
    """Create inert reference/manifests; never alter a receipt or release a plan."""
    m,s,d=(modules[k] for k in ('model','inventory','setup'));m.clock(explicit_now)
    context=_context(m,owner_source,explicit_now);owner_sha=m.digest(context)
    _metadata_reference(m,metadata_reference)
    # A new, identity-free intent template. An old T3 preparation is never relabeled.
    template_keys=HISTORY_KEYS-{'ownerContext','ownerContextSHA256','chromeCommand','preparationCommandCustody',
                               'notBeforeMS','expiresAtMS','recordedAt'}
    m.exact(historical_reference,template_keys)
    checked=custody.command_readout_checked(m,original_raw,chrome_command,connection=connection,
        sequence_min=sequence_min,sequence_max=sequence_max,explicit_now=explicit_now)
    m.require(chrome_command['kind']=='identity' and checked['readout']['value'] is None,
              'chrome-inert-preparation-identity-command')
    start=s.epoch_ms(m.instant(owner_source['capturedAt']));end=start+metadata_reference['maximumMilliseconds']
    m.require(start<=s.epoch_ms(explicit_now)<end, 'assembly-original-owner-window')
    capture_plan={**copy.deepcopy(metadata_reference),'ownerContextSHA256':owner_sha,'recordedAt':owner_source['capturedAt']}
    capture_sha=m.digest(capture_plan)
    setup={'schema':'hikyo-158/next-setup-whole-read-manifest/v1','stage':'beforeDelete','account':m.ACCOUNT,'region':m.REGION,
        'policyARN':d.ARN,'policyId':d.POLICY_ID,'notBeforeMS':start,'expiresAtMS':end,'reviewedPreparationSHA256':capture_sha,
        'publicMainSHA256':d.MAIN,'publicVendorsSHA256':d.VENDORS}
    history={**copy.deepcopy(historical_reference),'ownerContext':context,'ownerContextSHA256':owner_sha,
        'chromeCommand':copy.deepcopy(chrome_command),
        'preparationCommandCustody':{k:checked[k] for k in ('originalRawSHA256','originalSHA256','originalSequence','commandContextSHA256')},
        'notBeforeMS':start,'expiresAtMS':end,'recordedAt':m.stamp(explicit_now)}
    _historical_reference(m,history,custody)
    m.require(start<=checked['readout']['readAtMS']<end, 'chrome-preparation-command-inside-original-capture-window')
    history_sha=m.digest(history)
    observer={'schema':'hikyo-158/historical-describe-observer-manifest/v2','account':m.ACCOUNT,'region':m.REGION,
        'secretARN':m.HISTORICAL,'publicMainSHA256':SM_PINS['publicMainSHA256'],'reviewedPreparationSHA256':history_sha,
        'expiresAtMS':end,'captureTimeoutMS':history['captureTimeoutMS']}
    wire=m.canonical({'SecretId':m.HISTORICAL})
    gate={'schema':'hikyo-158/historical-describe-transport-gate/v2','account':m.ACCOUNT,'region':m.REGION,'secretARN':m.HISTORICAL,
        'reviewedPreparationSHA256':history_sha,'ownerContextSHA256':owner_sha,**SM_PINS,'observerSourceSHA256':PINS['historicalObserver'][1],
        'requestBodySHA256':hashlib.sha256(wire).hexdigest(),'requestBodyBytes':len(wire),'notBeforeMS':start,'expiresAtMS':end,
        'captureTimeoutMS':history['captureTimeoutMS'],'maximumMatchingCalls':1}
    return {'schema':'hikyo-158/persistent-chrome-inert-admission-data-plan/v1','ownerContext':context,'capturePlan':capture_plan,'capturePlanSHA256':capture_sha,
        'iamManifest':s.baseline_manifest(m,'iam',start,end,owner_sha,capture_sha),'kmsManifest':s.baseline_manifest(m,'kms',start,end,owner_sha,capture_sha),
        'setupManifest':setup,'historicalPreparation':history,'historicalPreparationSHA256':history_sha,'historicalObserverManifest':observer,
        'historicalGateManifest':gate,'historicalStorageKey':'hikyo158-historical-describe-metadata-'+history_sha,
        'installationOrder':{'historical':['gateV2','observerV2','verifyObserverLock','trusted-native-traversal'],
                             'kmsCreate':['observer','guard']},
        'runtimeReady':False,'fixtureAdmissionGranted':False,'executionAuthorityGranted':False,'providerAPIsCalled':False}


def _capture_clock(m, value):
    m.require(type(value) is str and re.fullmatch(r'\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d(?:\.\d{1,6})?(?:Z|\+00:00)',value), 'assembly-original-utc-clock')
    result=datetime.fromisoformat(value.replace('Z','+00:00'));m.clock(result);return result


def environment(modules, proposal, captures, owner_context, *, explicit_now):
    m=modules['model'];m.exact(captures,ENV_ENDPOINTS);values={}
    for name, row in captures.items():
        m.exact(row,{'endpoint','method','startedAt','finishedAt','exitCode','stdout','stderr'})
        m.require(row['endpoint']==ENV_ENDPOINTS[name] and row['method']=='GET' and type(row['exitCode']) is int
            and row['exitCode']==0 and row['stderr']=='' and type(row['stdout']) is str, 'assembly-original-github-get')
        sent,received=(_capture_clock(m,row[k]) for k in ('startedAt','finishedAt'))
        m.require(m.instant(owner_context['observedAt'])<=sent<=received<=explicit_now, 'assembly-common-owner-github-order')
        m.fresh(m.stamp(sent),explicit_now);m.fresh(m.stamp(received),explicit_now)
        values[name]=m.strict_json(row['stdout'].encode())
    variables=values['variables'];m.exact(variables,{'variables','total_count'})
    branches=values['branches'];m.exact(branches,{'branch_policies','total_count'})
    m.require(type(variables['total_count']) is int and variables['total_count']==0 and variables['variables']==[]
        and type(branches['total_count']) is int and branches['total_count']==1 and type(branches['branch_policies']) is list
        and len(branches['branch_policies'])==1,'assembly-complete-empty-vars-one-branch')
    branch=branches['branch_policies'][0];m.exact(branch,{'id','node_id','name','type'})
    m.require(type(branch['id']) is int and branch['id']>0 and type(branch['node_id']) is str,'assembly-original-branch-id')
    config=values['environment'];expected=proposal['inputs']['githubClosure']['protectedEnvironmentConfiguration']
    m.require(type(config) is dict and set(expected)<=set(config) and type(config.get('id')) is int,'assembly-original-env-config')
    result={'id':config['id'],'configuration':{k:copy.deepcopy(config[k]) for k in expected},
        'variables':[],'branchPolicies':[{k:branch[k] for k in ('name','type')}],'OIDCDefaults':values['oidcDefaults']}
    m.require(result['id']==m.ENV_ID and m.same(result['configuration'],expected)
        and result['branchPolicies']==[{'name':'main','type':'branch'}]
        and m.same(result['OIDCDefaults'],proposal['inputs']['githubClosure']['OIDCDefaults']), 'assembly-retained-environment')
    return result


def normalize_owner(modules, proposal, owner_context, capture_plan, iam_bundle, kms_bundle, setup_bundle, environment_captures, *, explicit_now):
    m,s,d=(modules[k] for k in ('model','inventory','setup'));m.clock(explicit_now);_metadata_reference(m,capture_plan)
    m.require(capture_plan['ownerContextSHA256']==m.digest(owner_context)
        and capture_plan['recordedAt']==owner_context['sourceReceipt']['capturedAt']
        and capture_plan['maximumMilliseconds']==300000 and type(capture_plan['maximumMilliseconds']) is int
        and capture_plan['fixtureReservationPermitted'] is False and capture_plan['writesPermitted'] is False,
        'assembly-common-capture-plan')
    env=environment(modules,proposal,environment_captures,owner_context,explicit_now=explicit_now)
    return s.normalize_baseline(m,d,s.PINS,proposal,iam_bundle,kms_bundle,setup_bundle,
        owner_context=owner_context,environment=env,reviewed_preparation_sha256=m.digest(capture_plan),explicit_now=explicit_now)


def historical_proof(modules, preparation, original_raw, *, chrome_command, connection,
                     sequence_min, sequence_max, explicit_now, preparation_original_raw, custody):
    m=modules['model'];m.clock(explicit_now);_historical_reference(m,preparation,custody)
    prepared_command=custody.command_readout_checked(m,preparation_original_raw,preparation['chromeCommand'],connection=connection,
        sequence_min=sequence_min,sequence_max=sequence_max,explicit_now=explicit_now)
    m.require(m.same(preparation['preparationCommandCustody'],{k:prepared_command[k] for k in
        ('originalRawSHA256','originalSHA256','originalSequence','commandContextSHA256')})
        and prepared_command['readout']['value'] is None, 'chrome-original-preparation-custody-not-replaced')
    checked=custody.command_readout_checked(m,original_raw,chrome_command,connection=connection,
        sequence_min=sequence_min,sequence_max=sequence_max,explicit_now=explicit_now)
    m.require(chrome_command['kind']=='historical'
        and m.same(chrome_command['identity'],preparation['chromeCommand']['identity'])
        and checked['originalSequence']>preparation['preparationCommandCustody']['originalSequence'],
        'chrome-same-prepared-document-genuine-later-history-command')
    # Explicit projection of the public observer's in-memory read(), no storage read.
    # Native command and raw provider response bytes remain unmodified.
    recovered={'schema':'hikyo-158/exact-own-historical-metadata-reconciliation/v1',
        'observedAt':checked['original']['completedAt'],
        'storageKey':'hikyo158-historical-describe-metadata-'+m.digest(preparation),
        'rawReceipt':m.canonical(checked['publicValue']).decode(),
        'storageReset':False,'providerRequest':False}
    m.exact(recovered,{'schema','observedAt','storageKey','rawReceipt','storageReset','providerRequest'})
    prep_sha=m.digest(preparation)
    m.require(recovered['schema']=='hikyo-158/exact-own-historical-metadata-reconciliation/v1'
        and recovered['storageKey']=='hikyo158-historical-describe-metadata-'+prep_sha
        and recovered['storageReset'] is False and recovered['providerRequest'] is False
        and type(recovered['rawReceipt']) is str, 'assembly-exact-own-metadata-no-reset')
    receipt=m.strict_json(recovered['rawReceipt'].encode())
    m.require(m.instant(prepared_command['original']['completedAt']).timestamp()*1000<=receipt['sentAtMS']
        <=receipt['storedAtMS']<=checked['readout']['readAtMS']
        and m.instant(preparation['recordedAt']).timestamp()*1000<=receipt['sentAtMS'],
        'chrome-preparation-before-describe-before-public-readout')
    m.require(type(preparation['notBeforeMS']) is int and type(preparation['expiresAtMS']) is int
        and preparation['notBeforeMS']<=receipt['sentAtMS']<=receipt['storedAtMS']<=preparation['expiresAtMS']
        <=preparation['notBeforeMS']+300000 and preparation['ownerContextSHA256']==m.digest(preparation['ownerContext']),
        'assembly-exact-historical-reference-window')
    m.require(receipt['reviewedPreparationSHA256']==prep_sha
        and m.instant(modules['inventory'].stamp_ms(receipt['storedAtMS']))<=m.instant(recovered['observedAt'])<=explicit_now,
        'assembly-original-historical-recovery-order')
    proof={'schema':'hikyo-158/next-historical-describe-absence/v1','observedAt':recovered['observedAt'],
        'account':m.ACCOUNT,'region':m.REGION,'secretARN':m.HISTORICAL,'ownerContextSHA256':preparation['ownerContextSHA256'],
        'reviewedReadPreparationSHA256':prep_sha,'readIntent':copy.deepcopy(preparation['readIntent']),
        'readIntentSHA256':m.digest(preparation['readIntent']),'sourceReceipt':receipt,'sourceReceiptSHA256':m.digest(receipt),
        'providerCallsObserved':1,'deleteIntentsObserved':0,'deleteTransmissionsObserved':0}
    m.historical_absence_checked(proof,explicit_now,preparation['ownerContextSHA256'])
    return proof
