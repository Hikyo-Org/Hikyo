"""Pure source-only hosted-result bridge. No downloads, provider, files or clocks.

The abstract authenticator has no live implementation. Hashes establish integrity
only. Root must authenticate original complete public API/transport/custody records.
"""
from abc import ABC, abstractmethod
from collections import Counter
import copy
import hashlib
import io
import re
import zipfile

LEDGER_SHA256 = 'c93ebd62f4304b880d29eb24812ccbf4c567d803970db1b11f27ff834f55a297'
MODEL_SHA256 = 'f40c07b2204c3305b434836015c99f30af4dfc893a0ce0e4f0318c17ea844a19'
MODEL_TEST_SHA256 = 'f06b2e173ff499070ae8718d65870534510a585339b8fc451462baaea6dc5985'
EXECUTOR_SHA256 = '57c31fce3d373d770930e5e01f4b1a0fb27c4128983c78eb18b0c08a4f27a85c'
EXECUTOR_TEST_SHA256 = '68e0d463956b1151a1c7687336c0d3c72ef4d1892e94287954f93d7be927be8b'
SUITE = 'TestAWSSecretsManagerRealSmoke'
MODES = ('static','ambient','assume-role','web-identity','kms-denied')
TESTS = {SUITE, *(SUITE+'/'+g for g in ('auth-boundaries','lifecycle','role-auth','kms-denial','iam-denial','adoption-witness','promoted-replay'))}
TESTS.update(SUITE+'/preflight-'+mode for mode in MODES)
TESTS.update(SUITE+'/auth-boundaries/expired-'+mode for mode in MODES if mode!='kms-denied')
TESTS.add(SUITE+'/auth-boundaries/configured-identity-refusal')
for group in ('lifecycle','kms-denial'):
    TESTS.update(SUITE+'/'+group+'/'+engine for engine in ('sqlite','postgres'))
for group,scenarios in (('role-auth',('ambient','assume-role','web-identity')),('adoption-witness',('version-before-adopt','version-after-adopt','replacement-after-adopt')),('promoted-replay',('finish-loss','marker-refusal'))):
    for scenario in scenarios:
        TESTS.add(SUITE+'/'+group+'/'+scenario)
        TESTS.update(SUITE+'/'+group+'/'+scenario+'/'+engine for engine in ('sqlite','postgres'))
OPERATIONS = {'DescribeSecret','CreateSecret','PutSecretValue','UpdateSecretVersionStage','TagResource','DeleteSecret','RestoreSecret','GetSecretValue'}
CODES = {'AccessDeniedException','DecryptionFailure','EncryptionFailure','ExpiredToken','ExpiredTokenException','InvalidClientTokenId','InvalidParameterException','InvalidRequestException','LimitExceededException','ResourceExistsException','ResourceNotFoundException','ThrottlingException','UnrecognizedClientException','other'}
EVIDENCE_KEYS = {'schema','candidate_sha','workflow_sha','run_id','status','stage','account','region','prefix','recorded_arns','verified_absent_arns','unresolved_names','historical_cleanup','tests','maximum_create_reservations','creation_reservations','complete_creation_ledger','diagnostics','diagnostics_truncated'}
API = 'https://api.github.com/repos/Hikyo-Org/Hikyo/actions/runs/'


def _evidence_shape(m,data):
    """Closed legacy v1 and assertion-bearing v2; no authority from diagnostics."""
    m.require(type(data) is dict,'evidence-object')
    schema=data.get('schema')
    m.require(type(schema) is str and schema in ('hikyo.aws-acceptance/v1','hikyo.aws-acceptance/v2'),'evidence-schema')
    if schema=='hikyo.aws-acceptance/v1':
        m.exact(data,EVIDENCE_KEYS)
        return
    m.exact(data,EVIDENCE_KEYS|{'failure_assertions','failure_assertions_truncated'})
    rows=data['failure_assertions']
    m.require(type(rows) is list and len(rows)<=128
        and type(data['failure_assertions_truncated']) is bool,'bounded-assertion-diagnostics')
    identifiers={f'assertion-{index:03d}' for index in range(1,225)}
    for row in rows:
        m.exact(row,{'test','assertion'})
        m.require(type(row['test']) is str and row['test'] in TESTS
            and type(row['assertion']) is str and row['assertion'] in identifiers,'closed-assertion-diagnostics')


class RootHostedProofAdapter(ABC):
    """Authenticate complete original evidence; no boolean/hash-only approval.

    A separately reviewed implementation must authenticate exact bound/sealed
    authority, raw all-page API metadata, original artifact archive transport and
    digest, trusted workflow/runner bytes and complete charged ledger. It must
    authenticate sole completed sandbox/runner teardown, no active child/run,
    exact Describe-only responses, and any failed-run CreateSecret outcome
    projection from original durable intent/forward/provider custody. A caller
    label, sanitized projection or hash cannot self-authenticate that provenance.
    """
    @abstractmethod
    def authenticate(self, bundle, context, archive_bytes, explicit_now):
        raise NotImplementedError('No live hosted proof authenticator supplied')


def _positive(m,value):
    m.require(type(value) is int and 0<value<10**25,'positive-exact-id')


def _pages(m,pages,endpoint,key):
    """Closed ordered projections of every actual API page and next Link."""
    m.require(type(pages) is list and 1<=len(pages)<=10,'bounded-complete-api-pages')
    rows=[];total=None
    for index,page in enumerate(pages,1):
        m.exact(page,{'endpoint','page','per_page','total_count','next','items'})
        m.require(page['endpoint']==endpoint and type(page['page']) is int and page['page']==index
            and type(page['per_page']) is int and page['per_page']==100 and type(page['total_count']) is int
            and 0<=page['total_count']<=1000 and type(page['items']) is list and len(page['items'])<=100,'exact-api-page')
        if total is None:total=page['total_count']
        m.require(page['total_count']==total and page['next']==(endpoint+'?per_page=100&page='+str(index+1) if index<len(pages) else None),'stable-page-chain')
        m.require(index==len(pages) or len(page['items'])==100,'untruncated-intermediate-page')
        rows.extend(page['items'])
    m.require(len(rows)==total and len(pages)==max(1,(total+99)//100),'complete-api-count')
    ids=[]
    for row in rows:m.require(type(row) is dict and key in row,'page-object');_positive(m,row[key]);ids.append(row[key])
    m.require(len(set(ids))==len(ids),'unique-api-objects')
    return rows


def _archive(m,raw,artifact):
    m.require(type(raw) is bytes and 0<len(raw)<=2_000_000 and type(artifact['size_in_bytes']) is int
        and artifact['size_in_bytes']==len(raw) and artifact['digest']=='sha256:'+hashlib.sha256(raw).hexdigest(),'exact-archive-byte-custody')
    with zipfile.ZipFile(io.BytesIO(raw)) as archive:
        entries=archive.infolist()
        m.require(len(entries)==1 and entries[0].filename=='evidence.json' and not entries[0].is_dir()
            and entries[0].flag_bits&1==0 and entries[0].compress_type in (zipfile.ZIP_STORED,zipfile.ZIP_DEFLATED)
            and 0<entries[0].file_size<=1_000_000,'sole-bounded-metadata-artifact')
        body=archive.read(entries[0]);m.require(len(body)==entries[0].file_size,'complete-artifact-entry')
    return body


def _terminal(m,bundle,bound,now):
    run=bound['run'];actual=bundle['run']
    m.exact(actual,{'id','repository_id','owner_id','run_attempt','head_sha','path','name','event','head_branch','status','conclusion','created_at','updated_at'})
    _positive(m,actual['id'])
    m.require(str(actual['id'])==run['runId'] and type(actual['repository_id']) is int and actual['repository_id']==1316165429
        and type(actual['owner_id']) is int and actual['owner_id']==316726515 and type(actual['run_attempt']) is int and actual['run_attempt']==1
        and actual['head_sha']==run['workflowSHA'] and actual['path']=='.github/workflows/aws-acceptance.yml' and actual['name']=='aws-acceptance'
        and actual['event']=='workflow_dispatch' and actual['head_branch']=='main' and actual['status']=='completed'
        and actual['conclusion'] in ('success','failure','cancelled'),'exact-terminal-new-run')
    m.require(m.instant(bound['records']['key']['receivedAt'])<=m.instant(actual['created_at'])<=m.instant(bound['derivedAt'])<=m.instant(actual['updated_at'])<=m.instant(bundle['observedAt']),'terminal-run-order')
    jobs=_pages(m,bundle['jobPages'],API+run['runId']+'/attempts/1/jobs','id')
    m.require(len(jobs)==1,'sole-complete-protected-job')
    job=jobs[0];m.exact(job,{'id','run_id','run_attempt','head_sha','name','status','conclusion','runner_group_name','started_at','completed_at','steps'})
    m.require(type(job['run_id']) is int and str(job['run_id'])==run['runId'] and type(job['run_attempt']) is int and job['run_attempt']==1
        and job['head_sha']==run['workflowSHA'] and job['name']=='sandbox' and job['status']=='completed'
        and job['conclusion']==actual['conclusion'] and job['runner_group_name']=='GitHub Actions','completed-exact-sandbox')
    m.require(m.instant(bound['derivedAt'])<=m.instant(job['started_at'])<=m.instant(job['completed_at'])<=m.instant(actual['updated_at']),'terminal-job-order')
    steps=job['steps'];m.require(type(steps) is list and 1<=len(steps)<=30,'complete-job-step-inventory')
    numbers=[]
    for step in steps:
        m.exact(step,{'number','name','status','conclusion','started_at','completed_at'})
        m.require(type(step['number']) is int and step['number']>0 and type(step['name']) is str and 1<=len(step['name'])<=200
            and step['status']=='completed' and step['conclusion'] in ('success','failure','cancelled','skipped'),'terminal-step')
        numbers.append(step['number'])
        if step['conclusion']=='skipped' and step['started_at'] is None:m.require(step['completed_at'] is None,'skipped-step-time')
        else:m.require(m.instant(job['started_at'])<=m.instant(step['started_at'])<=m.instant(step['completed_at'])<=m.instant(job['completed_at']),'step-time-order')
    m.require(numbers==sorted(set(numbers)) and max(numbers)<=100,'ordered-unique-native-step-numbers')
    artifacts=_pages(m,bundle['artifactPages'],API+run['runId']+'/artifacts','id')
    m.require(len(artifacts)==1,'sole-exact-run-artifact')
    artifact=artifacts[0]
    m.exact(artifact,{'id','name','expired','size_in_bytes','digest','created_at','updated_at','workflow_run'})
    m.require(artifact['name']=='aws-158-'+run['runId']+'-'+run['candidateSHA'] and artifact['expired'] is False
        and m.same(artifact['workflow_run'],{'id':actual['id'],'repository_id':1316165429,'head_repository_id':1316165429,'head_branch':'main','head_sha':run['workflowSHA']})
        and m.instant(job['started_at'])<=m.instant(artifact['created_at'])<=m.instant(artifact['updated_at'])<=m.instant(bundle['observedAt']),'exact-artifact-run-custody')
    return actual,job,artifact


def _native_absence(m,record,arn,completed,now):
    m.exact(record,{'arn','operation','errorCode','httpStatus','observedAt','requestId','sourceReceiptSHA256','sourceReceipt'})
    source=record['sourceReceipt']
    m.exact(source,{'schema','account','region','operation','endpoint','selector','requestBodySHA256','rawResponse','rawResponseSHA256','sentAt','receivedAt','requestId','httpStatus'})
    m.require(record['arn']==arn and record['operation']=='DescribeSecret' and record['errorCode']=='ResourceNotFoundException'
        and type(record['httpStatus']) is int and record['httpStatus']==400 and type(record['requestId']) is str and 8<=len(record['requestId'])<=128
        and record['sourceReceiptSHA256']==m.digest(source),'whole-native-absence-custody')
    m.require(source['schema']=='hikyo-158/new-secret-describe-source/v1' and source['account']==m.ACCOUNT and source['region']==m.REGION
        and source['operation']=='DescribeSecret' and source['endpoint']=='https://secretsmanager.'+m.REGION+'.amazonaws.com/'
        and m.same(source['selector'],{'SecretId':arn}) and source['requestBodySHA256']==hashlib.sha256(m.canonical({'SecretId':arn})).hexdigest()
        and source['requestId']==record['requestId'] and type(source['httpStatus']) is int and source['httpStatus']==400,'exact-new-secret-describe')
    raw=source['rawResponse'];m.require(type(raw) is str and 0<len(raw.encode())<=16384
        and hashlib.sha256(raw.encode()).hexdigest()==source['rawResponseSHA256'],'bounded-native-error-body')
    body=m.strict_json(raw.encode())
    m.require(type(body) is dict and set(body) in ({'__type'},{'__type','Message'},{'__type','message'})
        and body['__type'] in ('ResourceNotFoundException','com.amazonaws.secretsmanager#ResourceNotFoundException')
        and all(type(v) is str and len(v)<=8192 for v in body.values()),'typed-new-secret-not-found')
    m.require(m.instant(completed)<=m.instant(source['sentAt'])<=m.instant(source['receivedAt'])<=m.instant(record['observedAt'])<=now,'post-terminal-secret-absence-order')
    m.fresh(source['sentAt'],now);m.fresh(record['observedAt'],now)
    return {k:copy.deepcopy(v) for k,v in record.items() if k!='sourceReceipt'}


def _failed_outcomes(m,proof,reservations,arns,run,completed,now):
    """No failed artifact alone can prove completeness of interrupted creates."""
    m.exact(proof,{'schema','observedAt','runId','runAttempt','prefix','evidenceRawSHA256','intents','outcomes'})
    m.require(proof['schema']=='hikyo-158/failed-create-custody/v1' and proof['runId']==run['runId'] and type(proof['runAttempt']) is int
        and proof['runAttempt']==1 and proof['prefix']==run['prefix'],'failed-create-custody-scope')
    m.fresh(proof['observedAt'],now)
    m.require(m.instant(completed)<=m.instant(proof['observedAt'])<=now and type(proof['intents']) is list
        and type(proof['outcomes']) is list and len(proof['intents'])==len(proof['outcomes'])==len(reservations),'complete-durable-failed-create-custody')
    created=[]
    for index,(name,intent,outcome) in enumerate(zip(reservations,proof['intents'],proof['outcomes']),1):
        m.exact(intent,{'sequence','runId','name','maximumForwards','requestMetadata','sentAt','claimId'})
        m.require(type(intent['sequence']) is int and intent['sequence']==index and intent['runId']==run['runId'] and intent['name']==name
            and type(intent['maximumForwards']) is int and intent['maximumForwards']==1 and type(intent['claimId']) is str and 8<=len(intent['claimId'])<=128
            and m.same(intent['requestMetadata'],{'Name':name,'KmsKeyId':run['keyARN']})
            and m.instant(run['start'])<=m.instant(intent['sentAt'])<=m.instant(completed),'exact-failed-create-intent')
        m.exact(outcome,{'sequence','intentSHA256','operation','httpStatus','requestId','receivedAt','body'})
        m.require(type(outcome['sequence']) is int and outcome['sequence']==index and outcome['intentSHA256']==m.digest(intent)
            and outcome['operation']=='CreateSecret' and type(outcome['requestId']) is str and 8<=len(outcome['requestId'])<=128
            and m.instant(intent['sentAt'])<=m.instant(outcome['receivedAt'])<=m.instant(completed),'exact-failed-create-response-custody')
        if type(outcome['httpStatus']) is int and outcome['httpStatus']==200:
            body=outcome['body'];m.exact(body,{'ARN','Name','VersionId'})
            m.require(body['Name']==name and type(body['ARN']) is str and re.fullmatch(re.escape('arn:aws:secretsmanager:'+m.REGION+':'+m.ACCOUNT+':secret:'+name)+r'-[A-Za-z0-9]{6}',body['ARN']) is not None and type(body['VersionId']) is str and 1<=len(body['VersionId'])<=64,'created-response-identity')
            created.append(body['ARN'])
        else:
            m.require(type(outcome['httpStatus']) is int and outcome['httpStatus']==400
                and type(outcome['body']) is dict and set(outcome['body'])=={'__type'}
                and outcome['body']['__type'] in ('AccessDeniedException','EncryptionFailure'),'unambiguous-no-resource-create-outcome')
    m.require(len({r['claimId'] for r in proof['intents']})==len(reservations) and len(set(created))==len(created) and set(created)==set(arns),'all-failed-create-outcomes-resolved')


def validate(model, archive_bytes, bound, sealed, bundle, adapter, *, explicit_now):
    """Validate injected bytes and externally authenticated whole public evidence.

    No provider/fixture action or cleanup mutation is performed or authorized.
    The output is an executor input only after the separate external gates pass.
    """
    m=model;m.clock(explicit_now)
    m.require(isinstance(adapter,RootHostedProofAdapter),'explicit-external-hosted-authenticator')
    m.exact(bundle,{'schema','observedAt','boundPlanSHA256','sealedModelSHA256','run','jobPages','artifactPages','historicalAbsence','absenceRecords','failedCreationCustody','deploymentApproval'})
    context={'modelSHA256':MODEL_SHA256,'modelTestSHA256':MODEL_TEST_SHA256,'executorSHA256':EXECUTOR_SHA256,
        'boundPlanSHA256':m.digest(bound),'sealedModelSHA256':m.digest(sealed),'account':m.ACCOUNT,'region':m.REGION,
        'run':copy.deepcopy(bound['run']),'sourceManifest':copy.deepcopy(bound['preparation']['proposal']['sourceManifest'])}
    authenticated=adapter.authenticate(copy.deepcopy(bundle),copy.deepcopy(context),archive_bytes,explicit_now)
    m.require(m.same(authenticated,bundle),'authenticator-must-preserve-whole-original-bundle')
    m.require(bundle['schema']=='hikyo-158/next-hosted-result-bundle/v1' and bundle['boundPlanSHA256']==context['boundPlanSHA256']
        and bundle['sealedModelSHA256']==context['sealedModelSHA256'],'exact-new-bound-sealed-result')
    manifest=context['sourceManifest'];m.require(manifest['modelSHA256']==MODEL_SHA256 and manifest['testSHA256']==MODEL_TEST_SHA256,'reviewed-model-byte-pins')
    m.bound_checked(bound,m.instant(bound['derivedAt']));m.sealed_checked(sealed)
    m.require(sealed['boundPlanSHA256']==m.digest(bound) and m.same(sealed['boundPlan'],bound)
        and all(sealed[k] is False for k in ('runtimeReady','executionAuthorityGranted','externalAuthenticityVerifiedByModel')),'whole-sealed-bound-crosslink')
    m.fresh(bundle['observedAt'],explicit_now)
    m.require(m.instant(sealed['derivedAt'])<=m.instant(bundle['observedAt'])<=explicit_now,'sealed-before-result')
    actual,job,artifact=_terminal(m,bundle,bound,explicit_now)
    m.require(m.instant(sealed['derivedAt'])<=m.instant(job['started_at']),'job-starts-after-reviewed-seal')
    review=bundle['deploymentApproval'];m.exact(review,{'endpoint','observedAt','review'})
    m.require(review['endpoint']==API+bound['run']['runId']+'/approvals','exact-run-deployment-approval-endpoint')
    approval=review['review'];m.exact(approval,{'state','user','environments'})
    m.require(approval['state']=='approved' and m.same(approval['user'],{'id':991668,'login':'Dunky13'})
        and m.same(approval['environments'],[{'id':m.ENV_ID,'name':'hikyo-158-aws-sandbox'}])
        and m.instant(job['completed_at'])<=m.instant(review['observedAt'])<=m.instant(bundle['observedAt']),'exact-human-protected-deployment-review')
    raw=_archive(m,archive_bytes,artifact);data=m.strict_json(raw)
    _evidence_shape(m,data);run=bound['run'];passed=actual['conclusion']=='success'
    m.require(data['schema'] in ('hikyo.aws-acceptance/v1','hikyo.aws-acceptance/v2') and data['candidate_sha']==run['candidateSHA'] and data['workflow_sha']==run['workflowSHA']
        and data['run_id']==run['runId'] and data['account']==m.ACCOUNT and data['region']==m.REGION and data['prefix']==run['prefix']
        and type(data['maximum_create_reservations']) is int and data['maximum_create_reservations']==32
        and data['unresolved_names']==[] and data['diagnostics_truncated'] is False,'exact-complete-bounded-metadata-evidence')
    m.require(m.same(data['historical_cleanup'],{'arn':None,'status':'not-requested','delete_attempts':0,'delete_response_exact_arn':False,
        'metadata_verified':False,'absence_checks':0,'verified_absent':False}),'historical-runner-delete-path-forbidden')
    m.require(sealed['environmentVariables']==m.environment_variables(run)
        and next(v['value'] for v in sealed['environmentVariables'] if v['name']=='AWS_ACCEPTANCE_HISTORICAL_CLEANUP_ARN')=='','blank-historical-runner-environment')
    m.historical_absence_checked(bundle['historicalAbsence'],explicit_now,m.digest(bound['records']['owner']['ownerContext']))
    m.require(bundle['historicalAbsence']['sourceReceipt']['sentAtMS']>=m.instant(job['completed_at']).timestamp()*1000
        and m.instant(job['completed_at'])<=m.instant(bundle['historicalAbsence']['observedAt'])<=m.instant(bundle['observedAt']),'post-terminal-historical-absence')
    tests=data['tests'];m.require(type(tests) is dict and set(tests)<=TESTS and all(v in ('pass','fail','skip') for v in tests.values()),'closed-real-test-results')
    diagnostics=data['diagnostics'];m.require(type(diagnostics) is list and len(diagnostics)<=128,'bounded-diagnostics')
    for row in diagnostics:
        m.exact(row,{'test','operation','code'});m.require(row['test'] in TESTS and row['operation'] in OPERATIONS and row['code'] in CODES,'closed-metadata-diagnostics')
    base='arn:aws:secretsmanager:'+m.REGION+':'+m.ACCOUNT+':secret:'
    for field in ('recorded_arns','verified_absent_arns'):
        arns=data[field];m.require(type(arns) is list and len(arns)<=32 and all(type(a) is str and re.fullmatch(re.escape(base+run['prefix'])+r'[A-Za-z0-9/_+=.@-]{1,300}-[A-Za-z0-9]{6}',a) for a in arns)
            and len(set(arns))==len(arns),'unique-new-run-secret-identities')
    arns=data['recorded_arns'];m.require(set(data['verified_absent_arns'])==set(arns),'all-recorded-secret-absence-in-artifact')
    reservations=data['creation_reservations'];m.require(type(reservations) is list and len(reservations)<=32 and all(type(n) is str
        and re.fullmatch(re.escape(run['prefix'])+r'[A-Za-z0-9/_+=.@-]{1,300}',n) for n in reservations),'charged-create-reservation-cap')
    reserved=Counter(reservations);recorded=Counter(a[len(base):-7] for a in arns)
    m.require(all(reserved[n]>=count for n,count in recorded.items()),'every-created-id-charged')
    if passed:
        denied=Counter({run['prefix']+'kms-denied/'+engine+'/denied-document':1 for engine in ('sqlite','postgres')})
        m.require(data['status']=='passed' and data['stage']=='real-suite' and data['complete_creation_ledger'] is True
            and len(tests)==len(TESTS)==46 and set(tests)==TESTS and all(v=='pass' for v in tests.values()) and bool(arns)
            and reserved-recorded==denied and not recorded.keys()&denied.keys()
            and all(any(d=={'test':SUITE+'/kms-denial/'+engine,'operation':'CreateSecret','code':code}
                for d in diagnostics for code in ('AccessDeniedException','EncryptionFailure')) for engine in ('sqlite','postgres'))
            and bundle['failedCreationCustody'] is None,'all-46-real-tests-and-charged-outcomes-pass')
    else:
        m.require(data['status'] in ('preparing','failed-reconcile-before-rerun') and data['stage'] in ('compile','private-auth-projection','real-suite')
            and data['complete_creation_ledger'] is False,'failed-run-never-acceptance-success')
        proof=bundle['failedCreationCustody'];m.require(type(proof) is dict and proof.get('evidenceRawSHA256')==hashlib.sha256(raw).hexdigest(),'failed-run-needs-independent-complete-create-custody')
        _failed_outcomes(m,proof,reservations,arns,run,job['completed_at'],explicit_now)
    rows=bundle['absenceRecords'];m.require(type(rows) is list and len(rows)==len(arns),'complete-fresh-new-secret-absence')
    m.require(len({r.get('arn') for r in rows if type(r) is dict})==len(arns),'unique-secret-absence-records')
    output=[_native_absence(m,row,row['arn'],job['completed_at'],explicit_now) for row in rows]
    m.require({row['arn'] for row in output}==set(arns) and all(m.instant(row['observedAt'])<=m.instant(bundle['observedAt']) for row in rows),'exact-all-secret-absence')
    receipt={'attemptTrustBindingSHA256':bound['attemptTrustBindingSHA256'],'schema':'hikyo-158/next-hosted-result-validation/v1','observedAt':bundle['observedAt'],'boundPlanSHA256':m.digest(bound),
        'sealedModelSHA256':m.digest(sealed),'runId':run['runId'],'runAttempt':1,'candidateSHA':run['candidateSHA'],'workflowSHA':run['workflowSHA'],
        'archiveSHA256':hashlib.sha256(archive_bytes).hexdigest(),'evidenceRawSHA256':hashlib.sha256(raw).hexdigest(),'bundleSHA256':m.digest(bundle),
        'acceptancePassed':passed,'expectedRealTests':46,'passingRealTests':sum(v=='pass' for v in tests.values()),
        'chargedCreateAttempts':len(reservations),'allCreateOutcomesReconciled':True,'newSecretsVerifiedAbsent':len(arns),'noActiveFixtureWork':True,
        'approvalSHA256':m.SOURCE_PINS['approval'],'heldHistorySHA256':m.SOURCE_PINS['heldHistory'],'completedHistorySHA256':m.SOURCE_PINS['completedAttempt'],'configurationSHA256':m.digest(m.release_configuration_checked()),
        'oldReservationsRetained':176,'newReservationRetained':32,'lifetimeReservationCeiling':208,'refund':0,'remainingDispatches':0,
        'executionAuthorityGranted':False,'externalAuthenticityVerifiedByModel':False}
    absence={'schema':'hikyo-158/next-secret-absence/v1','observedAt':bundle['observedAt'],
        'artifact':{'runId':run['runId'],'runAttempt':1,'candidateSHA':run['candidateSHA'],'workflowSHA':run['workflowSHA'],'prefix':run['prefix'],
            'terminalConclusion':actual['conclusion'],'creationAttempts':len(reservations),'createdSecretARNs':sorted(arns)},
        'absenceRecords':output,'historicalAbsence':copy.deepcopy(bundle['historicalAbsence'])}
    return {'validationReceipt':receipt,'secretAbsenceProof':absence}


def _original_state_intents(m,state,binding,refs):
    """Close the actual ledger/executor/finalizer records before binding them.

    Only pending and dispatch embed full intents. owner/finalOwner contain
    response digests; completed contains CLI response metadata, not intents.
    These are record contracts, not a new lifecycle/admission policy.
    """
    fields={'claimSHA256','attemptTrustBindingSHA256','phase','held','pending','completed','owner','roles',
        'provider','keyARN','boundaryPolicyId','boundSHA256','sealedSHA256','dispatch'}
    m.exact(state,fields|({'finalOwner'} if 'finalOwner' in state else set()))
    m.require(type(state['phase']) is str and type(state['held']) is bool,'original-state-scalars')
    for key in ('provider','keyARN','boundaryPolicyId'):
        m.require(state[key] is None or type(state[key]) is str,'original-state-identities')
    for key in ('boundSHA256','sealedSHA256'):
        m.require(state[key] is None or m.sha(state[key]),'original-state-digests')
    m.require(all(state[k] is None or state[k]==refs[k] for k in ('boundSHA256','sealedSHA256'))
        and (state['keyARN'] is None or state['keyARN']==refs['keyARN']),'original-state-attempt-references')
    for key,operations in (('owner',{'CreateKey','DeletePolicyVersion','CreatePolicyVersion','CreatePolicy'}),
                           ('finalOwner',{'DeletePolicy','SetDefaultPolicyVersion'})):
        if key in state:
            rows=state[key]
            m.require(type(rows) is dict and set(rows)<=operations and all(m.sha(v) for v in rows.values()),
                'original-state-response-digests')
    m.require(type(state['roles']) is dict and set(state['roles'])<=set(m.ROLES)
        and all(type(v) is str for v in state['roles'].values()),'original-state-role-identities')
    m.require(type(state['completed']) is list,'original-state-completed-metadata')
    for row in state['completed']:
        m.exact(row,{'phase','sequence','operation','role','responseSHA256'})
        m.require(type(row['phase']) is str and type(row['sequence']) is int and row['sequence']>0
            and type(row['operation']) is str and (row['role'] is None or type(row['role']) is str)
            and m.sha(row['responseSHA256']),'original-state-completed-metadata')
    dispatch=state['dispatch']
    if dispatch is not None:
        m.require(type(dispatch) is dict and type(dispatch.get('attemptTrustBindingSHA256')) is str
            and dispatch['attemptTrustBindingSHA256']==binding,'original-dispatch-attempt-binding')
        m.exact(dispatch,{'schema','preparationSHA256','attemptTrustBindingSHA256','workflowSHA','candidateSHA',
            'path','ref','inputs','maximumRequests','maximumAttempts','stateSHA256','armedAt','proofSHA256'})
        m.require(dispatch['schema']=='hikyo-158/next-dispatch-intent/v1','original-dispatch-schema')
        m.require(dispatch['preparationSHA256']==refs['preparationSHA256'],'original-dispatch-attempt-references')
        m.exact(dispatch['inputs'],{'reviewed_sha'})
        m.require(all(m.sha(dispatch[k]) for k in ('preparationSHA256','stateSHA256','proofSHA256'))
            and all(type(dispatch[k]) is str for k in ('workflowSHA','candidateSHA','path','ref','armedAt'))
            and type(dispatch['inputs']['reviewed_sha']) is str
            and type(dispatch['maximumRequests']) is int and dispatch['maximumRequests']==1
            and type(dispatch['maximumAttempts']) is int and dispatch['maximumAttempts']==1,'original-dispatch-shape')
    pending=state['pending']
    if pending is None:return
    m.require(type(pending) is dict,'original-pending-record')
    m.require(type(pending.get('attemptTrustBindingSHA256')) is str
        and pending['attemptTrustBindingSHA256']==binding,'original-pending-attempt-binding')
    schema=pending.get('schema')
    common={'schema','attemptTrustBindingSHA256','operation','claimId','stateSHA256','armedAt'}
    if schema=='hikyo-158/next-owner-intent/v1':
        m.exact(pending,common|{'request','preparationSHA256','boundSHA256','maximumRequests','proofSHA256'})
        operation=pending['operation']
        m.require(type(operation) is str and operation in ('CreateKey','DeletePolicyVersion','CreatePolicyVersion','CreatePolicy'),
            'original-pending-owner-operation')
        request_fields={'CreateKey':{'Policy','Description','Origin','Tags','KeyUsage','KeySpec','MultiRegion'},
            'DeletePolicyVersion':{'PolicyArn','VersionId'},
            'CreatePolicyVersion':{'PolicyArn','PolicyDocument','SetAsDefault'},
            'CreatePolicy':{'PolicyName','Path','PolicyDocument'}}
        m.exact(pending['request'],request_fields[operation])
        expected_requests={'CreateKey':refs['keyRequest'],
            'DeletePolicyVersion':{'PolicyArn':m.SETUP,'VersionId':'v1'},
            'CreatePolicyVersion':{'PolicyArn':m.SETUP,'PolicyDocument':refs['setupDocument'],'SetAsDefault':True},
            'CreatePolicy':{'PolicyName':'hikyo-158-role-boundary','Path':'/','PolicyDocument':refs['boundaryDocument']}}
        m.require(m.same(pending['request'],expected_requests[operation]),'original-pending-owner-request')
        m.require(m.sha(pending['preparationSHA256']) and m.sha(pending['proofSHA256'])
            and (pending['boundSHA256'] is None or m.sha(pending['boundSHA256'])),'original-pending-owner-digests')
    elif schema=='hikyo-158/next-cli-intent/v1':
        m.exact(pending,common|{'phase','sequence','role','boundSHA256','maximumRequests','argumentsSHA256','file','transmitFile'})
        m.require(type(pending['phase']) is str and pending['phase'] in ('provision','retire')
            and type(pending['sequence']) is int and pending['sequence']>0 and type(pending['operation']) is str
            and (pending['role'] is None or type(pending['role']) is str)
            and type(pending['file']) is str and type(pending['transmitFile']) is str
            and m.sha(pending['boundSHA256']) and m.sha(pending['argumentsSHA256']),'original-pending-cli-shape')
    elif schema=='hikyo-158/final-owner-intent/v1':
        m.exact(pending,common|{'request','maximumForwards','boundPlanSHA256','sealedModelSHA256','originalClaimSHA256',
            'proofSHA256','boundaryPolicyId','budgetDiagnostic','requiredNativePreparation'})
        operation=pending['operation']
        m.require(type(operation) is str and operation in ('DeletePolicy','SetDefaultPolicyVersion'),'original-pending-final-operation')
        m.exact(pending['request'],{'PolicyArn'}|({'VersionId'} if operation=='SetDefaultPolicyVersion' else set()))
        m.require(m.same(pending['request'],{'PolicyArn':m.BOUNDARY} if operation=='DeletePolicy'
            else {'PolicyArn':m.SETUP,'VersionId':'v5'}),'original-pending-final-request')
        m.require(all(m.sha(pending[k]) for k in ('boundPlanSHA256','sealedModelSHA256','originalClaimSHA256','proofSHA256'))
            and type(pending['boundaryPolicyId']) is str,'original-pending-final-digests')
        m.exact(pending['budgetDiagnostic'],{'status','grossUSD','targetExceeded','absoluteExceeded','newSpendingAuthorized'})
        diagnostic=pending['budgetDiagnostic']
        m.require(type(diagnostic['status']) is str and (diagnostic['grossUSD'] is None or type(diagnostic['grossUSD']) is str)
            and all(diagnostic[k] is None or type(diagnostic[k]) is bool for k in ('targetExceeded','absoluteExceeded'))
            and diagnostic['newSpendingAuthorized'] is False,'original-pending-budget-shape')
        native=pending['requiredNativePreparation']
        m.require(type(native) is dict and type(native.get('attemptTrustBindingSHA256')) is str
            and native['attemptTrustBindingSHA256']==binding,'original-pending-native-attempt-binding')
        m.exact(native,{'attemptTrustBindingSHA256','operation','targetARN','expectedPolicyId','currentDefaultVersionId',
            'targetVersionId','reviewedDocumentSHA256','retirementProofSHA256','newBoundaryCustodySHA256','ownerContextSHA256'})
        m.require(all(type(native[k]) is str for k in ('operation','targetARN','expectedPolicyId','currentDefaultVersionId'))
            and (native['targetVersionId'] is None or type(native['targetVersionId']) is str)
            and all(m.sha(native[k]) for k in ('reviewedDocumentSHA256','retirementProofSHA256',
                'newBoundaryCustodySHA256','ownerContextSHA256')),'original-pending-native-shape')
    else:
        m.require(False,'original-pending-schema')
    if schema=='hikyo-158/next-owner-intent/v1':
        m.require(pending['preparationSHA256']==refs['preparationSHA256']
            and pending['boundSHA256']==state['boundSHA256']==(None if operation=='CreateKey' else refs['boundSHA256'])
            and pending['claimId']=='next-owner-'+operation,'original-pending-attempt-references')
    elif schema=='hikyo-158/next-cli-intent/v1':
        phase=pending['phase'];sequence=pending['sequence']
        if phase=='provision':
            order=['hikyo-158-source',*[r for r in m.ROLES if r not in ('hikyo-158-source','hikyo-158-assume')],'hikyo-158-assume']
            schedule=[('CreateOpenIDConnectProvider',None)]+[(op,role) for role in order for op in ('CreateRole','PutRolePolicy')]
        else:
            order=sorted(m.ROLES,key=lambda role:(role!='hikyo-158-assume',role=='hikyo-158-source',role))
            schedule=[('DeleteRolePolicy',r) for r in m.ROLES]+[('DeleteRole',r) for r in order]+[
                ('DeleteOpenIDConnectProvider',None),('ScheduleKeyDeletion',None)]
        m.require(sequence<=len(schedule) and (pending['operation'],pending['role'])==schedule[sequence-1],
            'original-pending-cli-schedule')
        m.require(pending['boundSHA256']==state['boundSHA256']==refs['boundSHA256']
            and pending['claimId']=='next-'+phase+'-'+str(sequence)
            and pending['file']==f'{phase}-{sequence:02d}-intent.once.json'
            and pending['transmitFile']==f'{phase}-{sequence:02d}-transmit.once.json','original-pending-attempt-references')
    else:
        m.require(pending['boundPlanSHA256']==state['boundSHA256']==refs['boundSHA256']
            and pending['sealedModelSHA256']==state['sealedSHA256']==refs['sealedSHA256']
            and pending['originalClaimSHA256']==state['claimSHA256']==refs['claimSHA256']
            and pending['boundaryPolicyId']==state['boundaryPolicyId']
            and pending['claimId']=='next-final-owner-'+operation,'original-pending-attempt-references')
        m.require(native['operation']==operation and native['targetARN']==pending['request']['PolicyArn']
            and native['expectedPolicyId']==(state['boundaryPolicyId'] if operation=='DeletePolicy' else m.SETUP_ID)
            and native['currentDefaultVersionId']==('v1' if operation=='DeletePolicy' else 'v6')
            and native['targetVersionId']==(None if operation=='DeletePolicy' else 'v5')
            and native['reviewedDocumentSHA256']==refs['documents'][operation]
            and native['newBoundaryCustodySHA256']==refs['boundaryCustodySHA256'],'original-pending-native-references')
    maximum='maximumForwards' if schema=='hikyo-158/final-owner-intent/v1' else 'maximumRequests'
    m.require(type(pending[maximum]) is int and pending[maximum]==1 and type(pending['claimId']) is str
        and m.sha(pending['stateSHA256']) and type(pending['armedAt']) is str,'original-pending-shape')


def validate_never_started(model,bound,sealed,bundle,adapter,*,explicit_now):
    """Separate exact cancelled-before-any-job proof, never an archive fallback.

    Root authenticates the entire original exclusive claim/journal plus pinned
    closed setup executor and complete remote history. No assertion that an
    archive is missing or a caller zero-counter can establish no Secrets calls.
    """
    m=model;m.clock(explicit_now);m.require(isinstance(adapter,RootHostedProofAdapter),'explicit-external-hosted-authenticator')
    m.exact(bundle,{'schema','observedAt','boundPlanSHA256','sealedModelSHA256','run','jobPages','artifactPages','approvalHistory','originalCustody','historicalAbsence'})
    run=bound['run'];sealed_sha=m.digest(sealed) if sealed is not None else None
    context={'phase':'never-started-cancelled-only','modelSHA256':MODEL_SHA256,'modelTestSHA256':MODEL_TEST_SHA256,
        'executorSHA256':EXECUTOR_SHA256,'boundPlanSHA256':m.digest(bound),'sealedModelSHA256':sealed_sha,
        'account':m.ACCOUNT,'region':m.REGION,'run':copy.deepcopy(run),'sourceManifest':bound['preparation']['proposal']['sourceManifest']}
    authenticated=adapter.authenticate(copy.deepcopy(bundle),copy.deepcopy(context),b'',explicit_now)
    m.require(m.same(authenticated,bundle),'authenticator-must-preserve-whole-original-bundle')
    m.require(bundle['schema']=='hikyo-158/never-started-cancelled-bundle/v1' and bundle['boundPlanSHA256']==m.digest(bound)
        and bundle['sealedModelSHA256']==sealed_sha,'exact-never-started-bound-model')
    m.bound_checked(bound,m.instant(bound['derivedAt']));manifest=context['sourceManifest']
    m.require(manifest['modelSHA256']==MODEL_SHA256 and manifest['testSHA256']==MODEL_TEST_SHA256,'reviewed-model-byte-pins')
    if sealed is not None:
        m.sealed_checked(sealed);m.require(m.same(sealed['boundPlan'],bound),'same-never-started-sealed-bound')
    m.fresh(bundle['observedAt'],explicit_now)
    actual=bundle['run'];m.exact(actual,{'id','repository_id','owner_id','run_attempt','head_sha','path','name','event','head_branch','status','conclusion','created_at','updated_at'})
    _positive(m,actual['id'])
    m.require(str(actual['id'])==run['runId'] and type(actual['repository_id']) is int and actual['repository_id']==1316165429
        and type(actual['owner_id']) is int and actual['owner_id']==316726515 and type(actual['run_attempt']) is int and actual['run_attempt']==1
        and actual['head_sha']==run['workflowSHA'] and actual['path']=='.github/workflows/aws-acceptance.yml' and actual['name']=='aws-acceptance'
        and actual['event']=='workflow_dispatch' and actual['head_branch']=='main' and actual['status']=='completed' and actual['conclusion']=='cancelled'
        and m.instant(bound['records']['key']['receivedAt'])<=m.instant(actual['created_at'])<=m.instant(bound['derivedAt'])
        and m.instant(actual['created_at'])<=m.instant(actual['updated_at'])<=m.instant(bundle['observedAt']),'exact-never-started-terminal-cancelled-run')
    m.require(_pages(m,bundle['jobPages'],API+run['runId']+'/attempts/1/jobs','id')==[]
        and _pages(m,bundle['artifactPages'],API+run['runId']+'/artifacts','id')==[],'complete-zero-jobs-and-artifacts-only')
    approval=bundle['approvalHistory'];m.exact(approval,{'endpoint','observedAt','response'})
    m.require(approval['endpoint']==API+run['runId']+'/approvals' and approval['response']==[]
        and m.instant(actual['updated_at'])<=m.instant(approval['observedAt'])<=m.instant(bundle['observedAt']),'original-empty-approval-history-only')
    custody=bundle['originalCustody'];m.exact(custody,{'claim','events','headSHA256'})
    claim=custody['claim'];m.exact(claim,{'schema','approvalSHA256','preparationSHA256','attemptTrustBindingSHA256','sourcePins','historicalPins','usedNamespaces','namespace',
        'retainedReservations','newReservationsCharged','lifetimeReservations','maximumDispatches','automaticRetry','reservedAt','proofSHA256','heldHistorySHA256','completedHistorySHA256','configurationSHA256'})
    binding=bound['attemptTrustBindingSHA256']
    m.require(type(claim['attemptTrustBindingSHA256']) is str and claim['attemptTrustBindingSHA256']==binding,'original-claim-attempt-binding')
    m.require(claim['schema']=='hikyo-158/next-exclusive-reservation/v1' and claim['approvalSHA256']==m.SOURCE_PINS['approval']
        and claim['heldHistorySHA256']==m.SOURCE_PINS['heldHistory'] and claim['completedHistorySHA256']==m.SOURCE_PINS['completedAttempt'] and claim['configurationSHA256']==m.digest(m.release_configuration_checked()) and claim['heldHistorySHA256']==m.digest(bound['preparation']['proposal']['inputs']['heldHistory'])
        and claim['preparationSHA256']==m.digest(bound['preparation']) and m.same(claim['historicalPins'],m.SOURCE_PINS)
        and claim['usedNamespaces']==bound['preparation']['proposal']['usedNamespaces'] and claim['namespace']==run['prefix']
        and type(claim['retainedReservations']) is int and claim['retainedReservations']==176
        and type(claim['newReservationsCharged']) is int and claim['newReservationsCharged']==32
        and type(claim['lifetimeReservations']) is int and claim['lifetimeReservations']==208
        and type(claim['maximumDispatches']) is int and claim['maximumDispatches']==1 and claim['automaticRetry'] is False,'original-exclusive-32-no-refund')
    pins=claim['sourcePins'];m.exact(pins,{'modelSHA256','modelTestSHA256','ledgerSHA256','executorSHA256','executorTestSHA256','frozenNextHelpers'})
    m.require(pins['modelSHA256']==MODEL_SHA256 and pins['modelTestSHA256']==MODEL_TEST_SHA256 and pins['executorSHA256']==EXECUTOR_SHA256
        and pins['ledgerSHA256']==LEDGER_SHA256
        and pins['executorTestSHA256']==EXECUTOR_TEST_SHA256
        and m.same(pins['frozenNextHelpers'],m.FROZEN),'original-closed-executor-source-custody')
    events=custody['events'];m.require(type(events) is list and 1<=len(events)<=512,'whole-original-executor-journal')
    refs={'preparationSHA256':m.digest(bound['preparation']),'boundSHA256':m.digest(bound),
        'sealedSHA256':sealed_sha,'claimSHA256':m.digest(claim),'keyARN':run['keyARN'],
        'keyRequest':bound['records']['key']['intent']['request'],
        'setupDocument':bound['documents']['setup-user.managed-policy.json'],
        'boundaryDocument':bound['documents']['setup-role-boundary.json'],
        'documents':{operation:m.digest(bound['documents'][document]) for operation,document in
            (('DeletePolicy','setup-role-boundary.json'),('SetDefaultPolicyVersion','setup-user.managed-policy.json'))},
        'boundaryCustodySHA256':m.digest(sealed['records']['custody']['boundary']) if sealed is not None else None}
    previous=None;last=None;pending_originals={};dispatch_original=None
    pending_kinds={'hikyo-158/next-owner-intent/v1':'owner-intent','hikyo-158/next-cli-intent/v1':'cli-intent',
        'hikyo-158/final-owner-intent/v1':'final-owner-intent'}
    for index,event in enumerate(events,1):
        m.exact(event,{'sequence','previousSHA256','kind','state'})
        state=event['state']
        m.require(type(state) is dict and type(state.get('attemptTrustBindingSHA256')) is str
            and state['attemptTrustBindingSHA256']==binding,'original-state-attempt-binding')
        _original_state_intents(m,state,binding,refs)
        pending=state['pending'];kind=event['kind']
        if type(kind) is str and kind in pending_kinds.values():
            m.require(pending is not None and kind==pending_kinds[pending['schema']]
                and pending['stateSHA256']==previous,'original-pending-event-predecessor')
            identity=(pending['schema'],pending['claimId'])
            m.require(identity not in pending_originals,'original-pending-once-custody')
            pending_originals[identity]=m.canonical(pending)
        elif pending is not None:
            m.require(pending_originals.get((pending['schema'],pending['claimId']))==m.canonical(pending),
                'original-pending-retained-custody')
        if kind=='held':
            m.require(last is not None and m.same(pending,last['pending']),'original-pending-retained-custody')
        dispatch=state['dispatch']
        if kind=='dispatch-intent':
            m.require(dispatch is not None and dispatch_original is None and dispatch['stateSHA256']==previous,
                'original-dispatch-event-predecessor')
            dispatch_original=m.canonical(dispatch)
        elif dispatch_original is not None or dispatch is not None:
            m.require(dispatch_original==m.canonical(dispatch),'original-dispatch-retained-custody')
        m.require(type(event['sequence']) is int and event['sequence']==index and event['previousSHA256']==previous
            and type(event['kind']) is str and event['state']['claimSHA256']==m.digest(claim),'original-executor-hash-chain')
        previous=m.digest(event);last=event['state']
    m.require(events[0]['kind']=='reserved' and events[0]['state']['boundSHA256'] is None and events[0]['state']['sealedSHA256'] is None
        and events[0]['state']['dispatch'] is None,'original-first-reservation-event')
    m.require(custody['headSHA256']==previous and last['boundSHA256']==m.digest(bound)
        and last['sealedSHA256']==sealed_sha and last['keyARN']==run['keyARN'] and last['dispatch'] is not None,'whole-original-current-journal-head')
    dispatch=last['dispatch'];m.exact(dispatch,{'schema','preparationSHA256','attemptTrustBindingSHA256','workflowSHA','candidateSHA','path','ref','inputs','maximumRequests','maximumAttempts','stateSHA256','armedAt','proofSHA256'})
    m.require(dispatch['schema']=='hikyo-158/next-dispatch-intent/v1' and dispatch['preparationSHA256']==m.digest(bound['preparation'])
        and dispatch['workflowSHA']==run['workflowSHA'] and dispatch['candidateSHA']==run['candidateSHA'] and dispatch['path']=='.github/workflows/aws-acceptance.yml'
        and dispatch['ref']=='main' and m.same(dispatch['inputs'],{'reviewed_sha':run['candidateSHA']})
        and type(dispatch['maximumRequests']) is int and dispatch['maximumRequests']==1 and type(dispatch['maximumAttempts']) is int and dispatch['maximumAttempts']==1
        and m.instant(claim['reservedAt'])<=m.instant(dispatch['armedAt'])<=m.instant(actual['created_at']),'exact-once-original-dispatch-intent')
    matches=[event for event in events if event['kind']=='dispatch-intent' and m.same(event['state'].get('dispatch'),dispatch)]
    m.require(len(matches)==1 and matches[0]['previousSHA256']==dispatch['stateSHA256'],'original-dispatch-event-custody')
    # Only the pinned new controller, never a historical controller, may have
    # produced this original journal. Its registry has IAM/KMS metadata/setup
    # writes and Describe-only historical access, with no Secret creation API.
    # Authentic completeness, source bytes and no competing actor are external.
    m.historical_absence_checked(bundle['historicalAbsence'],explicit_now,m.digest(bound['records']['owner']['ownerContext']))
    m.require(bundle['historicalAbsence']['sourceReceipt']['sentAtMS']>=m.instant(actual['updated_at']).timestamp()*1000
        and m.instant(bundle['historicalAbsence']['observedAt'])<=m.instant(bundle['observedAt']),'post-terminal-never-started-historical-absence')
    receipt={'schema':'hikyo-158/next-never-started-cleanup-validation/v1','observedAt':bundle['observedAt'],'boundPlanSHA256':m.digest(bound),
        'sealedModelSHA256':sealed_sha,'runId':run['runId'],'runAttempt':1,'candidateSHA':run['candidateSHA'],'workflowSHA':run['workflowSHA'],
        'acceptancePassed':False,'jobCount':0,'artifactCount':0,'approvedDeploymentCount':0,'chargedCreateAttempts':0,
        'attemptTrustBindingSHA256':bound['attemptTrustBindingSHA256'],'allCreateOutcomesReconciled':True,'noActiveFixtureWork':True,'approvalSHA256':m.SOURCE_PINS['approval'],'heldHistorySHA256':m.SOURCE_PINS['heldHistory'],'completedHistorySHA256':m.SOURCE_PINS['completedAttempt'],'configurationSHA256':m.digest(m.release_configuration_checked()),
        'oldReservationsRetained':176,'newReservationRetained':32,
        'lifetimeReservationCeiling':208,'refund':0,'remainingDispatches':0,'executionAuthorityGranted':False,
        'externalAuthenticityVerifiedByModel':False,'originalClaimSHA256':m.digest(claim),'originalJournalHeadSHA256':previous,'bundleSHA256':m.digest(bundle)}
    absence={'schema':'hikyo-158/next-secret-absence/v1','observedAt':bundle['observedAt'],
        'artifact':{'runId':run['runId'],'runAttempt':1,'candidateSHA':run['candidateSHA'],'workflowSHA':run['workflowSHA'],'prefix':run['prefix'],
            'terminalConclusion':'cancelled','creationAttempts':0,'createdSecretARNs':[]},'absenceRecords':[],
        'historicalAbsence':copy.deepcopy(bundle['historicalAbsence'])}
    return {'validationReceipt':receipt,'secretAbsenceProof':absence}


if __name__=='__main__':
    raise SystemExit('SOURCE ONLY: no live hosted-result execution entry point')
