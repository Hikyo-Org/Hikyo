"""Pure next-fixture preparation model. No I/O, provider client or clock reads.

All outputs remain non-authoritative. Root must authenticate external records and
freeze their exact pins/adapters separately before any executor may use them.
"""
import copy
import sys
from datetime import datetime, timedelta, timezone
from decimal import Decimal
import hashlib
import json
import math
import re

ACCOUNT='099256073061'; REGION='eu-central-1'; HORIZON='2026-10-26T12:22:56Z'
USER='hikyo-158-setup'; USER_ID='AIDAROHA7PNS4ZCR3U3FR'
USER_ARN=f'arn:aws:iam::{ACCOUNT}:user/{USER}'; MFA=f'arn:aws:iam::{ACCOUNT}:mfa/{USER}'
SETUP=f'arn:aws:iam::{ACCOUNT}:policy/hikyo-158-setup-policy'; SETUP_ID='ANPAROHA7PNSTPQUQEUTR'
BOUNDARY=f'arn:aws:iam::{ACCOUNT}:policy/hikyo-158-role-boundary'
PROVIDER=f'arn:aws:iam::{ACCOUNT}:oidc-provider/token.actions.githubusercontent.com'; ENV_ID=24756660811
SUBJECT='repo:Hikyo-Org@316726515/Hikyo@1316165429:environment:hikyo-158-aws-sandbox'
HISTORICAL=f'arn:aws:secretsmanager:{REGION}:{ACCOUNT}:secret:hikyo-e2e-20261018b/web-identity/postgres/role-document-vmN1Vj'
ROLES=tuple('hikyo-158-'+name for name in ('static','ambient','assume','web','kms-denied','source','oracle','cleanup'))
FILES=frozenset(['setup-user.managed-policy.json','setup-role-boundary.json']+[r+s for r in ROLES for s in ('.trust.json','.policy.json')])
OLD_KEY=f'arn:aws:kms:{REGION}:{ACCOUNT}:key/990c479f-d673-4bc9-8bca-7b9b86f4b205'
OLD_PREFIX='hikyo-e2e-20261019c02/'; OLD_AUTH='2026-10-20T22:48:15Z'; OLD_CLEANUP='2026-10-21T00:48:15Z'
HISTORICAL_SOURCE_PINS={
 'approval':'20be2b5eb24e47197459185f945d48fd8e8d13156e322f5cdcf22e3fbec25719',
 'iamClosure':'32ac8de2d42408f0b9b6f22f4414d49a8f9af9f72749e6f4ee07013933a9075b',
 'kmsClosure':'72716d744dce730517d60021452503a0f07023b77e0ab8defacf280a34843acd',
 'githubClosure':'a0aa7cfbfaf3069d7e4fa76c188168154029d6b53e8ec53c5376473674127bff',
 'secretClosure':'fd1176b419842e45bd91e4fb430dfb254bbecf523fd7ed04f38dd8c4e2f0d682',
 'billingClosure':'ce6bad9dae94f6c97963bd456cb1b70e8d628da45f35eab5dea285fbab316003',
 'firstEvidence':'d181532d293002dbeb52681c5904a9ed8dd99edf83836d3a62124571c3523d2b',
 'secondLedger':'5f06b8908c358ef690e0e5e04e22e640bad16fa757403c641065c14d64c967e7',
 'firstEpoch':'f429fe1b28f6b84bcf764ba22f30cf8066634a39b800420dbae1436e1db4f2e5',
 'firstProvisionPlan':'53af94ecdc13182a5074b3039688af5030f2932740c0bdfe24a2aaab549233ba',
 'lastClaim':'4e5e53e39d108ca886db4311595a2b5fde7a480cf92327e3eb298fadb5bdb5b3',
 'lastPlan':'15bd0a9ca96386af72484337514b0b977b25adc6739d4315b01fb2c58ecaed4b'}
CONSUMED_SOURCE_PINS={**HISTORICAL_SOURCE_PINS,
 'priorApproval':HISTORICAL_SOURCE_PINS['approval'],
 'approval':'fc5addb180bb28f271b5168ddc590613c6c71280e2228c55d9ce1affb012bb2c',
 'heldHistory':'1f70f640a34fc5baa72d3b91e7223f8d6c352c9420fc3b8f3c1cd67c73e0ffc6',
 'ownerDirective':'e71077ee7ce75a59929e1939fd67122e6c74ae954afdf4c5155fcbdd68dda729',
 'approvalContext':'6f9320dad87ee75c11d59e724ff84edffe5e7aa4beb0456b4572d15ee9ae497e'}
CONSUMED_PROPOSAL_RAW_SHA='f39dd9084c0a4eef4a800e8ba3be0838e80eb6c55f00af6ea722e01d8687903b'
# Deliberately UNCONFIGURED. Only a later actual decision + reviewed source
# adoption may replace these absent values. Tests use synthetic config ONLY.
FUTURE_CONFIGURATION=None
PROPOSAL_RAW_SHA=None
SOURCE_PINS={**CONSUMED_SOURCE_PINS,'consumedApproval':CONSUMED_SOURCE_PINS['approval'],
 'approval':None,'futureDirective':None,'futureApprovalContext':None,
 'completedAttempt':'52026c76c7143db8a1d85081f64b156c21e11adb23314c30fe51f34d1b23fed7'}
# Forward reservation outputs only. Historical21/27/80/112 evidence stays literal.
RETAINED_RESERVATIONS=176; NEW_RESERVATIONS=32; LIFETIME_RESERVATIONS=208
HELD_RAW_PINS={
 'claimRaw':'27f3c046820df9d8ceacf67cdeb1578851a8132f4579fc7a4d722c0d487c7204',
 'globalApprovalMarkerRaw':'27f3c046820df9d8ceacf67cdeb1578851a8132f4579fc7a4d722c0d487c7204',
 'heldMarkerRaw':'9d1d062c59a9a1c7fc4d01f51948b7b3a10203a9977c7055cee72fc00b8cef94',
 'pendingOwnerIntentRaw':'15b5234941e8ed117b9f1c18a5dae129c6761e612dfdbc41ef2c51f3b6239439',
 'oldGuardOriginalRaw':'a425fa0ee769bd2b01cd1e769f508b90878e994b47043c86aed21ffc6f2398ee',
 'oldRealmClosedOriginalRaw':'a134e9a6f1aaa1dc0a74dd1fa8db82296cf81782acdcad2693d294f1e16cf45a',
 'heldPreservedRaw':'7aa3f6ee0d4992e06c6cd3736b8a4ccd75de93117318abd35c487e6b6c7033bb'}
HELD_CAS_RAW_PINS=('f844ef06c93dee9dfb1a2323fdbacbd547ee8ce24206840f8b43b2b275f8d30b',
 'ab49c9a9601216c44d6fb7e1493460c94aa9e7deb6adf678978876d3b99025ca',
 'efb96df7fa495aaeba715933efdcef3333dd437113ef131ed883d0588f06456f')
HELD_ARCHIVE_REFERENCES={
 'priorPreparation':{'path':'final-admission-20261021-r3/prepared.actual.json','rawSHA256':'b72b3d08ca26f6b62c0aa022a838bbdc7ccb1f57955ca8f90b52e20d585aea20'},
 'rootExecutionMap':{'path':'root-reviewed-execution-map-20261021.actual.json','rawSHA256':'dee028ce87cda5f1a4df8b40f14621bb470ba1cd2d7b5a100611156355cde3d1'},
 'priorOperator':{'path':'iam-console-epye-successor-source/next_operator_orchestration_stdout.source-only.py','rawSHA256':'2d58f930195ed48a985f3a93a93f55f24f4dacc7bf0e4b92c69464f28e42c67b'}}
FROZEN={
 'setup_version_operations.source-only.mjs':'7c04cb1c5e4e1d9e4f423f49b08ec1aaef62a4a36e0e262d1b90ea55e9bab454',
 'native_setup_version_operations_guard.source-only.js':'667bd009a82cb13fe293537665c18f91fd74ab8dae2672a3d043f54874c49d45',
 'test_setup_version_operations.source-only.mjs':'2da0ee7fadc828ac6112bf85f3fec2432c5b2aa9530ee78e3c6251f20dfccb97',
 'native_setup_whole_read_observer.source-only.js':'0bfeb00db2473fccb7e220ac6045ffb6e2c0de3dd8a20fe58760f8fbdc8815f7',
 'normalize_setup_whole_reads.source-only.py':'6fa2472c5bdf9242c7d28db57b87874e471b8974887c09eb976b252fa8d09558',
 'test_native_setup_whole_read_observer.source-only.mjs':'f0d81145b2b08584347b20c3696f9efa61d31103aea9eb2a643a423676e65755',
 'test_normalize_setup_whole_reads.py':'86cc96633378f9bf8207852332643f6c39a7b0d6190ffc01d3e1e12eea8bc113'}
TRUSTED_FILES=frozenset(['go.mod','.github/workflows/ci-candidate.yml','.github/workflows/ci-merge-group.yml',
 '.github/workflows/ci.yml','scripts/ci/ci-job-registry.json','scripts/ci/check-required-jobs.sh',
 'scripts/ci/pr-gate/main.go','scripts/ci/pr-gate/queue.go','.github/workflows/aws-acceptance.yml',
 'scripts/ci/aws-acceptance/runner.py','scripts/ci/aws-acceptance/historical_cleanup.py',
 'scripts/ci/aws-acceptance/README.md','scripts/ci/aws-acceptance/test_runner.py',
 'scripts/ci/aws-acceptance/test_historical_cleanup.py','scripts/ci/start-ci-postgres.sh'])
FULL_STATIC_JOBS=frozenset('validation / '+name for name in (
 'changes','supply-chain-checks','client','compose-demo','release-snapshot','docs','test_core',
 'analysis_shards','app-build','headline-guarantee','storybook','lint','k8s-e2e','preflight',
 'floor-bench / measure','source-artifact-security','web-go','web-closure','ci-required'))
FULL_MATRIX_JOBS=frozenset(['validation / scan-xplat ('+os+')' for os in ('windows-latest','macos-latest')]
 +[f'validation / web ({viewport}, {group})' for viewport in ('desktop','mobile') for group in range(1,5)])


COMMON_FLAGS={'runtimeReady':False,'externalAuthenticityVerifiedByModel':False,'executionAuthorityGranted':False,
 'filesystemWritten':False,'reservationClaimCreated':False,'dispatchPerformed':False}


def require(condition,label):
    if not condition: raise ValueError('next-fixture:'+label)


def exact(value,fields):
    require(type(value) is dict and set(value)==set(fields),'closed-fields')


def canonical(value):
    def walk(v,depth=0):
        require(depth<=40,'json-depth')
        if type(v) is dict:
            require(all(type(k) is str for k in v),'json-key')
            for child in v.values():walk(child,depth+1)
        elif type(v) is list:
            require(len(v)<=2048,'json-list-size')
            for child in v:walk(child,depth+1)
        elif type(v) is float: require(math.isfinite(v),'nonfinite-json')
        else: require(v is None or type(v) in (str,int,bool),'json-value')
    walk(value)
    raw=json.dumps(value,sort_keys=True,separators=(',',':'),ensure_ascii=True,allow_nan=False).encode()
    require(len(raw)<=2_000_000,'json-size')
    return raw


def same(left,right):return canonical(left)==canonical(right)


def digest(value):return hashlib.sha256(canonical(value)).hexdigest()


def sha(value,length=64):return type(value) is str and re.fullmatch('[0-9a-f]{'+str(length)+'}',value) is not None


def strict_json(raw):
    require(type(raw) is bytes and len(raw)<=2_000_000,'raw-json-size')
    def pairs(items):
        result={}
        for key,value in items:
            require(key not in result,'duplicate-key');result[key]=value
        return result
    def constant(_):raise ValueError('next-fixture:nonfinite-json')
    result=json.loads(raw,object_pairs_hook=pairs,parse_constant=constant);canonical(result);return result


def instant(value):
    require(type(value) is str and re.fullmatch(r'[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}(?:\.[0-9]{1,6})?Z',value) is not None,'explicit-utc-time')
    return datetime.fromisoformat(value[:-1]+'+00:00')


def clock(now):require(type(now) is datetime and now.tzinfo is not None and now.utcoffset()==timedelta(0),'explicit-utc-clock')


def stamp(now):
    clock(now)
    return now.isoformat(timespec='microseconds' if now.microsecond else 'seconds').replace('+00:00','Z')


def fresh(value,now):clock(now);require(timedelta(0)<=now-instant(value)<=timedelta(minutes=5),'freshness')


def reviewed_records(records,pins,expected):
    exact(records,expected);exact(pins,expected)
    for key,value in records.items():require(sha(pins[key]) and digest(value)==pins[key],'reviewed-record-content')
    # A matching digest establishes integrity only, never external authenticity.


def historical_source_manifest_checked(manifest):
    exact(manifest,{'schema','modelSHA256','testSHA256','frozenNextHelpers','candidateFileSHA256s','workflowFileSHA256s','requiredCIJobs','configurationSHA256'})
    require(manifest['schema']=='hikyo-158/future-fixture-source-manifest/v3' and manifest['configurationSHA256']==digest(configuration_checked()) and sha(manifest['modelSHA256'])
        and sha(manifest['testSHA256']) and manifest['frozenNextHelpers']==FROZEN,'source-manifest')
    exact(manifest['candidateFileSHA256s'],{'go.mod'})
    exact(manifest['workflowFileSHA256s'],TRUSTED_FILES)
    require(all(sha(v) for k in ('candidateFileSHA256s','workflowFileSHA256s') for v in manifest[k].values()),'code-file-pins')
    exact(manifest['requiredCIJobs'],{'candidate','workflow'})
    for kind,names in manifest['requiredCIJobs'].items():
        require(type(names) is dict and 3<=len(names)<=200 and all(type(n) is str and 1<=len(n)<=180 for n in names),'required-ci-inventory')
        marker='candidate-proof' if kind=='candidate' else 'merge-group-proof'
        require(names.get(marker)=='success' and names.get('validation / ci-required')=='success','required-proof-jobs')
        require(all(v=='success' or (kind=='candidate' and k=='vouch' and v=='skipped') for k,v in names.items()),'closed-intended-skips')
        require(kind!='candidate' or names.get('vouch')=='skipped','same-repository-vouch-skip')
        expected=set(FULL_STATIC_JOBS|FULL_MATRIX_JOBS)|{marker}|({'vouch'} if kind=='candidate' else set())
        for family in ('isolation','race','fuzz'):
            pattern=r'validation / '+family+r' shard \((0|[1-9][0-9]*)\)'
            indexes=sorted(int(match[1]) for name in names if (match:=re.fullmatch(pattern,name)))
            require(indexes and indexes==list(range(len(indexes))),'complete-contiguous-shard-inventory')
            expected.update(f'validation / {family} shard ({index})' for index in indexes)
        require(set(names)==expected,'exact-source-full-lane-inventory')


def held_history_checked(inputs):
    """Pinned immutable original history only, never current live authority.

    Root authenticates original tools/session/quiescence independently. Raw
    custody and prior-preparation references stay whole; no old model is nested.
    """
    h=inputs['heldHistory']
    exact(h,{'schema',*HELD_RAW_PINS,'casRaw','archiveReferences'})
    require(h['schema']=='hikyo-158/held-c03-history/v1' and len(canonical(h))<=32768
        and same(h['archiveReferences'],HELD_ARCHIVE_REFERENCES),'bounded-held-history-schema')
    values={}
    for name,pin in HELD_RAW_PINS.items():
        raw=h[name];require(type(raw) is str and len(raw.encode())<=24000 and hashlib.sha256(raw.encode()).hexdigest()==pin,'held-original-raw-'+name)
        values[name]=strict_json(raw.encode())
    require(type(h['casRaw']) is list and len(h['casRaw'])==3,'three-original-held-cas-events')
    events=[]
    for raw,pin in zip(h['casRaw'],HELD_CAS_RAW_PINS):
        require(type(raw) is str and hashlib.sha256(raw.encode()).hexdigest()==pin,'held-original-cas-raw')
        events.append(strict_json(raw.encode()))
    claim=values['claimRaw'];held=values['heldMarkerRaw'];pending=values['pendingOwnerIntentRaw']
    require(h['globalApprovalMarkerRaw']==h['claimRaw'] and claim['schema']=='hikyo-158/next-exclusive-reservation/v1'
        and claim['approvalSHA256']==HISTORICAL_SOURCE_PINS['approval'] and same(claim['historicalPins'],HISTORICAL_SOURCE_PINS)
        and claim['namespace']=='hikyo-e2e-20261021c03/' and type(claim['retainedReservations']) is int and claim['retainedReservations']==112
        and type(claim['newReservationsCharged']) is int and claim['newReservationsCharged']==32
        and type(claim['lifetimeReservations']) is int and claim['lifetimeReservations']==144
        and type(claim['maximumDispatches']) is int and claim['maximumDispatches']==1 and claim['automaticRetry'] is False
        and claim['preparationSHA256']==HELD_ARCHIVE_REFERENCES['priorPreparation']['rawSHA256'],'whole-held-claim-no-refund')
    expected_sources={'modelSHA256':'c0afb086a0120343a1f9bed1e9f7fecf1deb0d926bd930bbb6c973a9897cf961',
        'modelTestSHA256':'db7921052cbdaf8b1a2c1dcf55c73063a21027b7e662c0cf8d5cd26810683815',
        'executorSHA256':'3da16f9931d0b18ecd1b1426651b3408db3fc9d487661436cd71b5b8c9b6be3a',
        'executorTestSHA256':'cfe7478ff0244ee2195ddf3faa5c84dd59050be35ee07c769941153cdf202bd0',
        'ledgerSHA256':'ac2ef71b20a5ca231642a4aee9956a3cb0a4fed51e4eb915353fe8c06d2e9be1','frozenNextHelpers':FROZEN}
    require(same(claim['sourcePins'],expected_sources),'original-held-source-ancestry')
    previous=None
    for index,event in enumerate(events,1):
        exact(event,{'sequence','previousSHA256','kind','state'})
        require(type(event['sequence']) is int and event['sequence']==index and event['previousSHA256']==previous
            and event['state']['claimSHA256']==digest(claim),'original-held-cas-chain')
        previous=digest(event)
    state=events[-1]['state']
    require(events[-1]['kind']=='held' and state['held'] is True and state['owner']=={} and state['keyARN'] is None
        and state['dispatch'] is None and state['boundSHA256'] is None and state['sealedSHA256'] is None
        and state['boundaryPolicyId'] is None and state['roles']=={} and state['provider'] is None and state['completed']==[]
        and same(state['pending'],pending) and pending['operation']=='CreateKey'
        and pending['preparationSHA256']==claim['preparationSHA256'],'held-pending-is-history-never-executed')
    require(held['nativeCreateKeySendsObserved']==0 and type(held['nativeCreateKeySendsObserved']) is int
        and held['githubDispatches']==0 and type(held['githubDispatches']) is int
        and held['rootFinishClicked'] is False and held['automaticRetry'] is False,'zero-original-forward-history')
    original=values['oldGuardOriginalRaw'];closed=values['oldRealmClosedOriginalRaw'];preserved=values['heldPreservedRaw']
    require(original['status']==closed['status']=='completed' and original['toolName']==closed['toolName']=='t3-code.preview_evaluate'
        and original['input']['tabId']==closed['input']['tabId'],'same-original-native-tab-custody')
    status=original['output']['value'];guard=status['guard'];observer=status['observer'];teardown=closed['output']['value']
    require(guard['phase']=='observing' and guard['claimConsumed'] is False and type(guard['nativeSendInvocations']) is int
        and guard['nativeSendInvocations']==0 and type(guard['activeReads']) is int and guard['activeReads']==0
        and observer['readySlots']==[] and type(observer['pendingCaptures']) is int and observer['pendingCaptures']==0
        and teardown['guardAbsent'] is True and teardown['diagnosticAbsent'] is True and teardown['accountPresent'] is True
        and teardown['href']=='https://eu-central-1.console.aws.amazon.com/console/home?region=eu-central-1'
        and preserved['newDispatchForwardAbsent'] is True and preserved['newKeyForwardPermitAbsent'] is True
        and preserved['heldSHA256']==HELD_RAW_PINS['heldMarkerRaw'] and same(preserved['heldRecord'],held),'settled-old-realm-closed-no-forward-permits')
    held_at=datetime.fromisoformat(held['observedAt']);clock(held_at)
    require(instant(pending['armedAt'])<=held_at<=instant(original['startedAt'])<=instant(original['completedAt'])
        <instant(closed['startedAt'])<=instant(closed['completedAt'])<=instant(inputs['approval']['recordedAt']),'original-held-teardown-chronology')
    return claim


def consumed_approval_checked(inputs):
    """Bind actual verbatim directive plus explicit bounded Root interpretation."""
    approval=inputs['approval'];context=inputs['approvalContext'];original=inputs['ownerDirective']
    require(approval['schema']=='hikyo-158/after-held-single-bounded-run-owner-approval/v1'
        and approval['ownerDecision']=='Continue until aws readiness is green'
        and approval['decisionInterpretation']=='Proceed with exactly the one reviewed proposed attempt; no automatic retry or further scope increase'
        and approval['proposalMarkdownSHA256']==CONSUMED_PROPOSAL_RAW_SHA and approval['priorReservedCreations']==144
        and approval['maximumAdditionalCreations']==32 and approval['postReservationLifetimeTotal']==176
        and approval['additionalManualDispatches']==1 and approval['noAutomaticRetry'] is True
        and approval['oldHeldClaimPreserved'] is True and approval['requiredHumanDeploymentApprovalSeparate'] is True
        and approval['requiredFullCIAndHumanMergeSeparate'] is True,'actual-single-bounded-owner-directive')
    require(digest(inputs['priorApproval'])==HISTORICAL_SOURCE_PINS['approval']
        and context['originalRawSHA256']==approval['originalCustody']['rawPayloadSHA256']=='de6571a54dbbb7ef55d81f000145c7bbfda8f6498b5f6384b4634e79ab832bac'
        and original['nativeItemRef']['nativeId']==approval['originalCustody']['nativeId']
        and original['toolName']=='t3-code.t3_thread_read' and original['status']=='completed','original-owner-directive-custody')
    matches=[row for row in original['output']['items'] if row.get('position')==8691]
    require(len(matches)==1,'one-actual-owner-directive');message=matches[0]
    require(message['type']=='user_message' and message['createdBy']=='user' and message['creationSource']=='mobile'
        and message['text']=='Continue until aws readiness is green' and message['textTruncated'] is False
        and message['messageId']=='8b2f393c-fd2d-466d-ab92-29802593aa74'
        and same(message,context['ownerMessage']) and all(approval['ownerMessage'][k]==message[k] for k in approval['ownerMessage'])
        and [row['position'] for row in context['precedingProposalContexts']]==[8626,8634]
        and all(row in original['output']['items'] and row['createdBy']=='agent' and row['textTruncated'] is False
            for row in context['precedingProposalContexts'])
        and instant(message['updatedAt'])<=instant(original['completedAt'])<=instant(approval['recordedAt']),'actual-context-before-directive-not-fabricated-A')


def configuration_checked():
    c=FUTURE_CONFIGURATION
    require(type(c) is dict,'future-generation-unconfigured-no-approval-namespace-or-folder')
    exact(c,{'approvalSHA256','directiveSHA256','contextSHA256','proposalRawSHA256','proposalMessageSHA256','namespace','ledgerFolder','cleanupFolder'})
    require(all(sha(c[k]) for k in ('approvalSHA256','directiveSHA256','contextSHA256','proposalRawSHA256','proposalMessageSHA256'))
        and c['approvalSHA256']==SOURCE_PINS['approval'] and c['directiveSHA256']==SOURCE_PINS['futureDirective']
        and c['contextSHA256']==SOURCE_PINS['futureApprovalContext'] and c['proposalRawSHA256']==PROPOSAL_RAW_SHA,'actual-future-approval-source-not-configured')
    require(type(c['namespace']) is str and re.fullmatch(r'hikyo-e2e-[0-9]{8}c[0-9]{2}/',c['namespace']) is not None
        and c['namespace'] not in ('hikyo-e2e-20261021c03/','hikyo-e2e-20261021c04/')
        and all(type(c[k]) is str and re.fullmatch(r'[a-z][a-z0-9-]{4,80}',c[k]) is not None for k in ('ledgerFolder','cleanupFolder'))
        and c['ledgerFolder'] not in ('aws-next-fixture-state','aws-after-held-fixture-state')
        and c['cleanupFolder'] not in ('aws-after-held-fixture-partial-cleanup',c['ledgerFolder']),'actual-future-namespace-folders-not-configured')
    return c


COMPLETED_RAW_PINS={'claimRaw': 'be3bed59c41f8d76c306f43e14bb2deaebf9b4218770d42b41f7795390b5d461', 'closureRaw': '0ef65ecd5a8c4e9bb9b4e43fc10ce1e06bce04a32825c92c0bdd8de9278c1f16', 'heldRaw': '53c48aa1b769648ee21dc1a88b1d832ca58030475ff7653329d41ee8108bce7f', 'keyReadbackRaw': '2f9b9f851d1cf6c71d6952903a91f735b2fcb47d093fd8e398f854eb49e4e7e6', 'scheduleRaw': '83ea3707f58f87c214942e24f00b2ba7097009d7731dba156213471e66a28032'}
COMPLETED_CAS_PINS=['03a5a13b9bc8360db9a696703f8432281e8c1c2dd4087f8e1262d82741c836d2', 'd153dfe710dbc83dde77990d65f0de8d558c4d4b29701c8755daf2569aadadd3']
COMPLETED_ARCHIVES={'fullCompletionRecords': {'path': 'after-held-final-admission-20261021-r3/abort-complete-records.actual.json', 'rawSHA256': 'd949a58ea0153338978463be7f1d1621a61ab2c57f86c15116caf6795fa59465'}, 'originalPrepared': {'path': 'after-held-final-admission-20261021-r3/prepared.actual.json', 'rawSHA256': 'dc9a4b6573af18d245f1c148ecd3a2a08cb5e839b31e8e3facdbcbddc3a828f4'}}
FOURTH_OLD_ID='bfa44dc1-ee8a-43b8-aec7-d8a7fbb38db1'


def completed_history_checked(inputs):
    h=inputs['completedAttempt'];exact(h,{'schema','raw','casRaw','archiveReferences'})
    require(h['schema']=='hikyo-158/completed-c04-history/v1' and len(canonical(h))<=40000
        and same(h['archiveReferences'],COMPLETED_ARCHIVES),'bounded-completed-history')
    exact(h['raw'],COMPLETED_RAW_PINS);values={}
    for name,pin in COMPLETED_RAW_PINS.items():
        raw=h['raw'][name];require(type(raw) is str and hashlib.sha256(raw.encode()).hexdigest()==pin,'immutable-completed-raw-'+name)
        values[name]=strict_json(raw.encode())
    require(type(h['casRaw']) is list and len(h['casRaw'])==2,'complete-original-c04-cas')
    previous=None;events=[]
    for index,(raw,pin) in enumerate(zip(h['casRaw'],COMPLETED_CAS_PINS),1):
        require(type(raw) is str and hashlib.sha256(raw.encode()).hexdigest()==pin,'immutable-completed-cas')
        row=strict_json(raw.encode());require(row['sequence']==index and type(row['sequence']) is int
            and row['previousSHA256']==previous and row['state']['claimSHA256']==digest(values['claimRaw']),'completed-cas-chain')
        events.append(row);previous=digest(row)
    claim=values['claimRaw'];closure=values['closureRaw'];schedule=values['scheduleRaw'];bundle=values['keyReadbackRaw']
    require(claim['namespace']=='hikyo-e2e-20261021c04/' and claim['approvalSHA256']==CONSUMED_SOURCE_PINS['approval']
        and same(claim['historicalPins'],CONSUMED_SOURCE_PINS) and claim['heldHistorySHA256']==CONSUMED_SOURCE_PINS['heldHistory']
        and type(claim['retainedReservations']) is int and claim['retainedReservations']==144
        and type(claim['newReservationsCharged']) is int and claim['newReservationsCharged']==32
        and type(claim['lifetimeReservations']) is int and claim['lifetimeReservations']==176
        and claim['preparationSHA256']==COMPLETED_ARCHIVES['originalPrepared']['rawSHA256'],'consumed176-claim-no-refund')
    require(closure['schema']=='hikyo-158/partial-setup-abort-closure/v1' and closure['originalClaimSHA256']==digest(claim)
        and closure['preparationSHA256']==claim['preparationSHA256'] and closure['approvalSHA256']==claim['approvalSHA256']
        and closure['heldHistorySHA256']==claim['heldHistorySHA256'] and closure['originalHeldRetained'] is True
        and type(closure['lifetimeReservations']) is int and closure['lifetimeReservations']==176
        and type(closure['refund']) is int and closure['refund']==0 and closure['acceptancePassed'] is False
        and closure['executionAuthorityGranted'] is False and closure['newSpendingAuthorized'] is False
        and closure['defaultVersionId']=='v5' and closure['retainedVersionIds']==['v1','v2','v3','v4','v5']
        and closure['retainedUserId']==USER_ID,'actual-complete-cleanup-not-acceptance')
    # The canonical original pending intent remains historical and unaccepted;
    # the actual key is established by separate original native custody below.
    state=events[-1]['state'];require(state['pending']['operation']=='CreateKey' and state['keyARN'] is None
        and state['dispatch'] is None and state['boundSHA256'] is None and state['sealedSHA256'] is None
        and values['heldRaw']['automaticRetry'] is False,'preserved-unaccepted-original-state')
    require(type(bundle['readRows']) is list and len(bundle['readRows'])==7,'original-three-old-plus-new-readback')
    rows={r['slot']:r for r in bundle['readRows']};require(len(rows)==7 and 'new.DescribeKey' in rows,'complete-original-schedule-readback')
    metadata=strict_json(rows['new.DescribeKey']['rawResponse'].encode())['KeyMetadata'];arn=f'arn:aws:kms:{REGION}:{ACCOUNT}:key/'+FOURTH_OLD_ID
    require(metadata['Arn']==arn and metadata['KeyId']==FOURTH_OLD_ID and metadata['KeyState']=='PendingDeletion' and metadata['Enabled'] is False
        and metadata['DeletionDate']==schedule['response']['DeletionDate']
        and schedule['operation']=='ScheduleKeyDeletion' and schedule['request']=={'KeyId':FOURTH_OLD_ID,'PendingWindowInDays':7}
        and schedule['response']['KeyId']==arn and schedule['response']['KeyState']=='PendingDeletion'
        and type(schedule['response']['PendingWindowInDays']) is int and schedule['response']['PendingWindowInDays']==7
        and schedule['forwardsObserved']==1 and type(schedule['forwardsObserved']) is int and schedule['httpStatus']==200
        and schedule['requestId']=='786e90c3-de15-485c-a094-7b7027280be8','actual-fourth-key-scheduled-once')
    for index,(old_arn,old) in enumerate(inputs['kmsClosure']['keys'].items(),1):
        matches=[r for r in rows.values() if r['slot'].startswith('old') and strict_json(r['rawResponse'].encode()).get('KeyMetadata',{}).get('Arn')==old_arn]
        require(len(matches)==1 and same(strict_json(matches[0]['rawResponse'].encode())['KeyMetadata'],old['metadata']),'historical-three-schedules-still-exact')
    return claim,metadata,max(row['receivedAtMS'] for row in rows.values())


def scheduled_key_closure(proposal):
    _,metadata,_=completed_history_checked(proposal['inputs'])
    result=copy.deepcopy(proposal['inputs']['kmsClosure']['keys'])
    result[metadata['Arn']]={'metadata':metadata}
    require(len(result)==4,'exact-four-old-scheduled-keys')
    return result


BANKED_DIRECTIVE='A, accept runs until green but below $15 billable'


def banked_series_scope():
    """Fixed interpretation of the whole directive, never authority or a runner.

    Later controlled manual attempts retain the genuine banked decision, but
    this generation implements ONLY its first 176+32 reservation subset. It
    cannot derive another generation, mint history or reset an approval marker.
    """
    return {'schema':'hikyo-158/banked-manual-until-green-budget-scope/v1',
        'ownerDecision':BANKED_DIRECTIVE,'completionCondition':'real-aws-readiness-green',
        'manualAttemptsBanked':True,'automaticRetry':False,'perAttemptNewReservationsMaximum':32,
        'grossAndBillableExclusiveCeilingUSD':'15.00','targetUSD':'5.00','creditsReduceGrossCeiling':False,
        'humanDeploymentApprovalSeparate':True,'humanMergeApprovalSeparate':True,
        'firstAttemptSubset':{'priorReservationsRetained':176,'maximumNewReservations':32,
            'lifetimeReservationCeiling':208,'maximumDispatches':1},
        'laterManualAttemptDerivationImplemented':False}


def future_approval_checked(inputs):
    c=configuration_checked();a=inputs['approval'];context=inputs['futureApprovalContext'];directive=inputs['futureDirective']
    require(type(a) is dict and a.get('ownerDecision')==BANKED_DIRECTIVE,
        'explicit-recognized-affirmative-future-decision-required')
    exact(a,{'schema','recordedAt','disposition','ownerMessageId','ownerDecision','proposalMarkdownSHA256','priorReservationsRetained',
        'maximumNewReservations','lifetimeReservationCeiling','maximumDispatches','namespace','ledgerFolder','cleanupFolder',
        'noAutomaticRetry','preserveOldScheduledKeys','cleanupResultSHA256','originalCustody','bankedSeries'})
    require(a['schema']=='hikyo-158/banked-series-first-bounded-run-owner-approval/v1' and a['disposition']=='derive-first-bounded-run-from-banked-series'
        and same(a['bankedSeries'],banked_series_scope())
        and a['proposalMarkdownSHA256']==PROPOSAL_RAW_SHA and a['namespace']==c['namespace'] and a['ledgerFolder']==c['ledgerFolder']
        and a['cleanupFolder']==c['cleanupFolder'] and a['noAutomaticRetry'] is True
        and all(type(a[k]) is int for k in ('priorReservationsRetained','maximumNewReservations','lifetimeReservationCeiling','maximumDispatches'))
        and (a['priorReservationsRetained'],a['maximumNewReservations'],a['lifetimeReservationCeiling'],a['maximumDispatches'])==(176,32,208,1)
        and a['preserveOldScheduledKeys']==4 and type(a['preserveOldScheduledKeys']) is int
        and a['cleanupResultSHA256']==COMPLETED_RAW_PINS['closureRaw'],'one-new-208-decision-required')
    exact(directive,{'raw'});require(type(directive['raw']) is str,'whole-future-owner-native-original')
    original=strict_json(directive['raw'].encode());custody=a['originalCustody'];exact(custody,{'nativeId','rawPayloadSHA256','messageId'})
    require(hashlib.sha256(directive['raw'].encode()).hexdigest()==custody['rawPayloadSHA256']
        and original['nativeItemRef']['nativeId']==custody['nativeId'] and original['status']=='completed'
        and original['toolName']=='t3-code.t3_thread_read','exact-future-original-custody')
    exact(context,{'schema','ownerMessage','proposalContext'});message=context['ownerMessage']
    offered=context['proposalContext']
    require(digest(offered)==c['proposalMessageSHA256'] and type(offered.get('text')) is str
        and PROPOSAL_RAW_SHA in offered['text'] and all(value in offered['text'] for value in ('176','32','208'))
        and 'A: Approve this one bounded run' in offered['text'],'exact-offered-208-proposal-original-context')
    require(context['schema']=='hikyo-158/future-owner-decision-context/v1' and message in original['output']['items']
        and context['proposalContext'] in original['output']['items'] and context['proposalContext']['createdBy']=='agent'
        and context['proposalContext']['textTruncated'] is False and message['type']=='user_message' and message['createdBy']=='user'
        and message['textTruncated'] is False and message['messageId']==a['ownerMessageId']==custody['messageId']
        and type(message['text']) is str and message['text']==a['ownerDecision'] and 0<len(message['text'])<=4000
        and context['proposalContext']['position']<message['position']
        and instant(message['updatedAt'])<=instant(original['completedAt'])<=instant(a['recordedAt']),'genuine-new-owner-decision-no-old-transfer')
    _,_,last_read_ms=completed_history_checked(inputs)
    require(datetime(1970,1,1,tzinfo=timezone.utc)+timedelta(milliseconds=last_read_ms)<=instant(a['recordedAt']),'new-decision-after-completed-key-cleanup')


def _future_inputs_checked(inputs):
    configuration_checked();exact(inputs,SOURCE_PINS)
    for name,pin in SOURCE_PINS.items():require(sha(pin) and digest(inputs[name])==pin,'immutable-source-'+name)
    consumed={**inputs,'approval':inputs['consumedApproval']}
    consumed_approval_checked(consumed);held_history_checked(consumed);completed_history_checked(inputs);future_approval_checked(inputs)


def unbound_proposal(inputs,source_manifest):
    configuration_checked();exact(inputs,set(SOURCE_PINS)|{'proposalMarkdown'})
    require(type(inputs['proposalMarkdown']) is bytes and hashlib.sha256(inputs['proposalMarkdown']).hexdigest()==PROPOSAL_RAW_SHA,'accepted-future-proposal')
    snapshots={k:copy.deepcopy(v) for k,v in inputs.items() if k!='proposalMarkdown'}
    _future_inputs_checked(snapshots);source_manifest_checked(source_manifest)
    a=inputs['firstEvidence'];b=inputs['secondLedger'];epoch=inputs['firstEpoch'];claim=inputs['lastClaim'];plan=inputs['lastPlan']
    require(len(a['creation_reservations'])==21 and b['aws']['syntheticCreateSecretAttempts']==27
        and claim['oldReservationsRetained']==80 and claim['newReservationCharged']==32 and claim['conservativeLifetimeReservations']==112
        and claim['oldEpochSHA256']==SOURCE_PINS['firstEpoch'] and claim['planSHA256']==SOURCE_PINS['lastPlan'],'prior-reservations')
    require(len(epoch['runs'])==1 and epoch['runs'][0]['prefix']=='hikyo-e2e-20261019c01/' and plan['run']['prefix']==OLD_PREFIX,'complete-known-prior-history')
    consumed={**inputs,'approval':inputs['consumedApproval']};held=held_history_checked(consumed);completed,_,_=completed_history_checked(inputs)
    used=sorted({a['prefix'],b['authorization']['prefix'],*[r['prefix'] for r in epoch['runs']],plan['run']['prefix'],held['namespace'],completed['namespace']})
    return {'schema':'hikyo-158/next-fixture-unbound/v1','phase':'unbound',**COMMON_FLAGS,
        'acceptedProposalRawSHA256':PROPOSAL_RAW_SHA,'inputs':snapshots,'sourceManifest':copy.deepcopy(source_manifest),
        'attemptTrustBinding':copy.deepcopy(attempt_binding_checked(source_manifest)),
        'attemptTrustBindingSHA256':digest(attempt_binding_checked(source_manifest)),
        'usedNamespaces':used,'priorReservationsRetained':RETAINED_RESERVATIONS,'maximumNewReservations':NEW_RESERVATIONS,'lifetimeAfterReservation':LIFETIME_RESERVATIONS,
        'maximumDispatches':1,'run':None,'newKeyARN':None,'newBoundaryPolicyId':None,'newRoleIds':None,'lease':None}


def proposal_checked(proposal):
    require(same(proposal['attemptTrustBinding'],attempt_binding_checked(proposal['sourceManifest']))
        and proposal['attemptTrustBindingSHA256']==digest(proposal['attemptTrustBinding']),'immutable-attempt-trust-binding')
    configuration_checked();_future_inputs_checked(proposal['inputs']);source_manifest_checked(proposal['sourceManifest'])
    original=proposal['inputs'];consumed={**original,'approval':original['consumedApproval']}
    held=held_history_checked(consumed);completed,_,_=completed_history_checked(original)
    expected_used=sorted({original['firstEvidence']['prefix'],original['secondLedger']['authorization']['prefix'],
        *[r['prefix'] for r in original['firstEpoch']['runs']],original['lastPlan']['run']['prefix'],held['namespace'],completed['namespace']})
    require(proposal['acceptedProposalRawSHA256']==PROPOSAL_RAW_SHA and proposal['schema']=='hikyo-158/next-fixture-unbound/v1'
        and proposal['phase']=='unbound' and all(proposal.get(k) is v for k,v in COMMON_FLAGS.items()),'unbound-not-authority')
    require(all(type(proposal[k]) is int for k in ('priorReservationsRetained','maximumNewReservations','lifetimeAfterReservation','maximumDispatches')),'integer-proposal-caps')
    require(proposal['usedNamespaces']==expected_used and proposal['priorReservationsRetained']==176 and proposal['maximumNewReservations']==32
        and proposal['lifetimeAfterReservation']==208 and proposal['maximumDispatches']==1
        and all(proposal[k] is None for k in ('run','newKeyARN','newBoundaryPolicyId','newRoleIds','lease')),'proposal-shape')
    exact(proposal,{'schema','phase',*COMMON_FLAGS,'acceptedProposalRawSHA256','inputs','sourceManifest',
        'attemptTrustBinding','attemptTrustBindingSHA256','usedNamespaces',
        'priorReservationsRetained','maximumNewReservations','lifetimeAfterReservation','maximumDispatches','run',
        'newKeyARN','newBoundaryPolicyId','newRoleIds','lease'})




BOOTSTRAP_1017={
 'repositoryId':1316165429,'ownerId':316726515,'number':1017,'pullRequestId':'PR_kwDOTnMXNc8AAAABJqKMbw',
 'headSHA':'b8233ff8eb7f8503c69365d3ab65b12b10471d84','baseSHA':'0de713d2df6acbb9a3e344fbfba4827b31f86501',
 'mergeSHA':'fdbf911632e3143d76080bb6b1523133fa99cfce',
 'actor':{'login':'Dunky13','databaseId':991668},
 'addedId':'ATMQE_lADOTnMXNc8AAAABYqxv188AAAAH334gFA','addedRESTId':33814355988,
 'mergedId':'ME_lADOTnMXNc8AAAABYqxv188AAAAH361_GQ','mergedRESTId':33817460505,
 'removedId':'RFMQE_lADOTnMXNc8AAAABYqxv188AAAAH361_gA','removedRESTId':33817460608,
 'entryId':'MQE_lQDOTnMXNc8AAAABJqKMb84ABKLBzgNjW_A','queueRunId':39550546669,
 'addedAt':'2026-10-21T05:36:34Z','mergedAt':'2026-10-21T06:30:56Z',
 'sourceChain':['486ba26ebd95c6b0e0e00976fca3b104d7328b6e','9df4d38529e0ddde1530bc5114f123e1887c8488','b8233ff8eb7f8503c69365d3ab65b12b10471d84']}
EPISODE_RAW_PINS=(
 '65de5e57bbcedc7efebbf553fc5b045d4eba6f3194ed321b77db87c3e3569b14',
 '2d24636704a76a14a9e5c4933059288962f7bca3570f9e14518054414d0d62a6',
 '40fb9bd4d18c2baec0ad4f089e4692d1c159a91bf92125bfa4dccda8c6d55394',
 '5197fcecd0023c0f5e20df5cd94483153a38fa6138cf3a149d015cc7874ffd9c',
 '5d831e8b838a28a37fed331bee4da593cffb4c1de7317fe5e3d43eb03a580895',
 'a3301dc5ff757de82dc99206ec7e6a9d0c6013a6ec126aab0a53512421683e94',
 '340375d763f9c458b886ef05b50de2a3186fd9cc7f92b68a90019a2b00a6ead6',
 '4f53cda18c2baa0c0354bb5f9a3ecbe5ed12ab4d8e11ba873c2f11161202b945',
 'a6f00e330b8720be37e518c2d413d7d52eb952618a3de2c3cba618c1c465a856',
 'ab82959d75c1e84dcd173076d5d214ece712addbe30e24bae2ddd8b6bef31f44')
EPISODE_SEQUENCES=(2941862,2941864,2941943,2941945,2941952,2941956)


def _episode_time(value):
    # Only source capture timestamps use the provider's explicit +00:00 form.
    require(type(value) is str and re.fullmatch(r'[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}(?:\.[0-9]{1,6})?(?:Z|\+00:00)',value) is not None,'original-explicit-episode-utc-time')
    return datetime.fromisoformat(value.replace('Z','+00:00'))


def episode_semantics_checked(decision,pr,observed_at,parsed):
    """Pure causal checks only; called by the pinned whole-original checker.

    This function is not an external authenticator or alternative ci_checked.
    Full code admission ALWAYS also requires original bytes/pins and Root proof.
    """
    b=BOOTSTRAP_1017
    exact(decision,{'schema','disposition',*('repositoryId','ownerId','number','pullRequestId','actor','headSHA','baseSHA','mergeSHA'),
        'addedEvent','mergedEvent','preHeadObservation','postHeadObservation','queueObservation','history','originalEvidence'})
    require(decision['schema']=='hikyo-158/bootstrap-1017-owner-queue-episode/v1' and decision['disposition']=='approve-protected-merge','specific-owner-queue-episode')
    for key in ('repositoryId','ownerId','number'):require(type(decision[key]) is int and decision[key]==b[key],'exact-episode-integer-identity')
    for key in ('pullRequestId','actor','headSHA','baseSHA','mergeSHA'):require(same(decision[key],b[key]),'fixed-bootstrap-episode-identity')
    require(pr['number']==1017 and type(pr['number']) is int and pr['state']=='closed' and pr['baseRef']=='main'
        and pr['headSHA']==b['headSHA'] and pr['baseSHA']==b['baseSHA'] and pr['merge_commit_sha']==b['mergeSHA']
        and pr['merged_at']==b['mergedAt'],'specific-original-merged-bootstrap')
    added=decision['addedEvent'];exact(added,{'__typename','id','createdAt','actor','enqueuer','pullRequest'})
    expected_actor=b['actor']
    for key in ('actor','enqueuer'):
        exact(added[key],{'login','databaseId'});require(type(added[key]['databaseId']) is int and same(added[key],expected_actor),'actual-owner-actor-and-enqueuer')
    require(added['__typename']=='AddedToMergeQueueEvent' and added['id']==b['addedId'] and added['createdAt']==b['addedAt']
        and same(added['pullRequest'],{'id':b['pullRequestId'],'number':1017}) and type(added['pullRequest']['number']) is int,'supported-owner-add-event')
    merged=decision['mergedEvent'];exact(merged,{'__typename','id','createdAt','actor','commit'})
    require(merged['__typename']=='MergedEvent' and merged['id']==b['mergedId'] and merged['createdAt']==b['mergedAt']
        and same(merged['actor'],expected_actor) and type(merged['actor']['databaseId']) is int
        and same(merged['commit'],{'oid':b['mergeSHA'],'parents':{'nodes':[{'oid':b['baseSHA']},{'oid':b['headSHA']}]}}),'exact-owner-merged-event-parents')
    windows=[]
    for name,pair in (('preHeadObservation',(0,1)),('postHeadObservation',(2,3)),('queueObservation',(4,5))):
        observation=decision[name];exact(observation,{'startCaptureIndex','completionCaptureIndex'})
        require(type(observation['startCaptureIndex']) is int and type(observation['completionCaptureIndex']) is int
            and (observation['startCaptureIndex'],observation['completionCaptureIndex'])==pair,'fixed-original-command-pair')
        start,end=(parsed[i] for i in pair)
        common={'id','threadId','runId','nodeId','providerThreadId','providerTurnId','nativeItemRef','parentItemId','ordinal','status','title','startedAt','completedAt','updatedAt','type','input'}
        exact(start,common);exact(end,common|{'output','exitCode'})
        for record in (start,end):
            exact(record['nativeItemRef'],{'driver','nativeId','strength'})
            require(type(record['ordinal']) is int and record['ordinal']>0 and type(record['nativeItemRef']['nativeId']) is str
                and record['id']=='turn-item:provider:codex:native-item:'+record['nativeItemRef']['nativeId'],'closed-native-command-identity')
            require(record['type']=='command_execution' and record['threadId']=='9ac3a5c6-ec11-4281-b1f6-04add14759bc'
                and type(record['input']) is str and record['nativeItemRef']['driver']=='codex' and record['nativeItemRef']['strength']=='strong','actual-original-command-envelope')
        require(start['id']==end['id'] and same(start['nativeItemRef'],end['nativeItemRef']) and start['input']==end['input']
            and start['startedAt']==end['startedAt'] and start['status']=='running' and start['completedAt'] is None and 'output' not in start
            and end['status']=='completed' and type(end['exitCode']) is int and end['exitCode']==0 and type(end['output']) is str,'paired-original-input-output-execution')
        left,right=instant(start['startedAt']),instant(end['completedAt']);require(left<=right,'actual-original-execution-order');windows.append((left,right))
    require(windows[0][1]<instant(b['addedAt'])<windows[1][0]<=windows[1][1]<windows[2][0]<=windows[2][1]<instant(b['mergedAt'])<=instant(observed_at),'genuine-before-after-and-entry-brackets')
    pre=strict_json(parsed[1]['output'].encode());post=strict_json(parsed[3]['output'].encode())
    require(pre['headRefOid']==post['head']==b['headSHA'] and pre['baseRefOid']==post['base']==b['baseSHA'],'original-bracket-exact-head-base')
    require('gh pr view 1017 --repo Hikyo-Org/Hikyo' in parsed[1]['input'] and 'gh pr view 1017 --repo Hikyo-Org/Hikyo' in parsed[3]['input'],'original-head-request-routing')
    lines=parsed[5]['output'].splitlines();require(len(lines)==2 and all(lines),'complete-exact-two-json-line-queue-output')
    summary,queue=(strict_json(v.encode()) for v in lines)
    require(set(summary)=={'conclusion','event','headBranch','headSha','status','url','workflowName'}
        and summary['event']=='merge_group' and summary['workflowName']=='trusted-merge-ci'
        and summary['status']=='queued' and summary['conclusion']=='' and summary['headSha']==b['mergeSHA']
        and summary['headBranch']=='gh-readonly-queue/main/pr-1017-'+b['baseSHA']
        and summary['url']=='https://github.com/Hikyo-Org/Hikyo/actions/runs/'+str(b['queueRunId'])
        and 'gh run view '+str(b['queueRunId'])+' --repo Hikyo-Org/Hikyo --json event,headSha,headBranch,status,conclusion,workflowName,url' in parsed[5]['input'],'actual-run-request-plus-observed-url-binding')
    entry=queue['data']['repository']['pullRequest']['mergeQueueEntry'];exact(entry,{'id','enqueuedAt'})
    require(entry['id']==b['entryId'] and entry['enqueuedAt']==b['addedAt'],'actual-entry-id-time-without-invented-head')
    history=decision['history'];exact(history,{'pageCaptureIndices','addedPosition','mergedPosition','removedPosition'})
    require(same(history['pageCaptureIndices'],[6,7]) and all(type(v) is int for v in history['pageCaptureIndices'])
        and all(type(history[k]) is int for k in ('addedPosition','mergedPosition','removedPosition')),'whole-ordered-history-source-selectors')
    rows=parsed[6];require(type(rows) is list and 0<len(rows)<=100 and parsed[7]==[],'complete-rest-page-plus-terminal-empty')
    known={'committed','commented','labeled','unlabeled','reviewed','mentioned','added_to_merge_queue','removed_from_merge_queue','review_dismissed','renamed','merged','closed','cross-referenced'}
    require(all(type(row) is dict and row.get('event') in known for row in rows),'closed-event-types-no-unknown-or-head-base-rewrites')
    selected=[i for i,row in enumerate(rows) if row.get('node_id')==b['addedId']]
    merges=[i for i,row in enumerate(rows) if row.get('node_id')==b['mergedId']]
    removals=[i for i,row in enumerate(rows) if row.get('node_id')==b['removedId']]
    require(selected==[history['addedPosition']] and merges==[history['mergedPosition']] and removals==[history['removedPosition']]
        and selected[0]<merges[0]<removals[0],'provider-array-order-for-selected-merge-then-removal')
    ai,mi,ri=selected[0],merges[0],removals[0]
    for index,event,ident,when in ((ai,'added_to_merge_queue',b['addedRESTId'],b['addedAt']),(mi,'merged',b['mergedRESTId'],b['mergedAt'])):
        row=rows[index];require(row['event']==event and type(row['id']) is int and row['id']==ident and row['created_at']==when
            and row['actor']['login']=='Dunky13' and type(row['actor']['id']) is int and row['actor']['id']==991668,'rest-owner-event-crosslinks')
    require(rows[mi]['commit_id']==b['mergeSHA'] and rows[ri]['event']=='removed_from_merge_queue'
        and type(rows[ri]['id']) is int and rows[ri]['id']==b['removedRESTId'] and rows[ri]['created_at']==b['mergedAt']
        and rows[ri]['actor']['login']=='github-merge-queue[bot]' and rows[ri]['actor']['id']==118344674,'exact-terminal-merge-and-bot-removal')
    active=False;episodes=0
    for i,row in enumerate(rows):
        event=row['event']
        if event=='added_to_merge_queue':require(not active,'unambiguous-single-active-queue-episode');active=True;episodes+=1
        elif event=='removed_from_merge_queue':require(active,'no-unmatched-queue-removal');active=False
        if ai<i<mi:require(event not in {'added_to_merge_queue','removed_from_merge_queue','merged','closed'},'no-intervening-queue-exit-reentry-or-merge')
    require(not active and episodes==2 and any(i>ri and row['event']=='closed' and row['created_at']==b['mergedAt'] for i,row in enumerate(rows)),'earlier-episode-closed-and-selected-episode-terminal')
    # No use of committedDate/authoredDate/pushedDate or GraphQL totalCount to
    # establish push timing or complete coverage. REST is the independent chain.
    graphql=parsed[8]['data']['repository'];gp=graphql['pullRequest'];nodes=gp['timelineItems']['nodes']
    require(graphql['databaseId']==b['repositoryId'] and graphql['owner']['databaseId']==b['ownerId']
        and gp['id']==b['pullRequestId'] and gp['number']==1017 and gp['headRefOid']==b['headSHA'] and gp['baseRefOid']==b['baseSHA']
        and gp['state']=='MERGED' and gp['mergedAt']==b['mergedAt'] and gp['mergeCommit']['oid']==b['mergeSHA']
        and [n for n in nodes if n.get('id')==b['addedId']]==[added]
        and [n for n in nodes if n.get('id')==b['mergedId']]==[merged],'whole-supported-graphql-event-crosslinks')
    fields=parsed[9]['data']['__type']['fields']
    require({f['name'] for f in fields}=={'actor','createdAt','enqueuer','id','mergeQueue','pullRequest'},'actual-supported-added-event-schema')


def affirmative_merge_checked(decision,pr,observed_at):
    # Raw-body pins protect exact retained history only. They NEVER authenticate
    # a caller/file or replace Root's mandatory actual whole-original decision.
    require(type(decision) is dict and 'originalEvidence' in decision,'whole-new-episode-record-required')
    exact(decision['originalEvidence'],{'records'});records=decision['originalEvidence']['records']
    require(type(records) is list and len(records)==len(EPISODE_RAW_PINS),'whole-original-episode-records-once')
    parsed=[]
    for i,(row,pin) in enumerate(zip(records,EPISODE_RAW_PINS)):
        exact(row,{'raw','capture'});require(type(row['raw']) is str and hashlib.sha256(row['raw'].encode()).hexdigest()==pin,'pinned-complete-original-episode-bytes')
        parsed.append(strict_json(row['raw'].encode()))
        capture=row['capture'];require(type(capture) is dict,'complete-original-capture-metadata')
        if i<6:
            exact(capture,{'sequence','eventId','eventType','originalEventAt','rawPayloadFile','rawPayloadBytes','rawPayloadSHA256','canonicalPayloadFile','canonicalPayloadSHA256'})
            require(capture['eventType']=='turn-item.updated' and type(capture['eventId']) is str and bool(capture['eventId'])
                and all(type(capture[k]) is str and bool(capture[k]) for k in ('rawPayloadFile','canonicalPayloadFile'))
                and capture['canonicalPayloadSHA256']==digest(parsed[-1])
                and instant(parsed[-1]['startedAt'])<=instant(capture['originalEventAt'])<=instant(observed_at),'whole-original-event-envelope-custody')
            require(type(capture['sequence']) is int and capture['sequence']==EPISODE_SEQUENCES[i]
                and capture['rawPayloadSHA256']==pin and type(capture['rawPayloadBytes']) is int and capture['rawPayloadBytes']==len(row['raw'].encode()),'original-start-completion-sequence-custody')
        elif i in (6,7):
            exact(capture,{'transport','endpoint','startedAt','completedAt','status','selectedPublicResponseHeaders','rawResponseSHA256','responseBytes','items'})
            require(capture['transport']=='official-gh-api' and type(capture['items']) is int and capture['items']==len(parsed[-1]),'exact-typed-rest-item-count')
            page=i-5;require(capture['endpoint']==f'repos/Hikyo-Org/Hikyo/issues/1017/timeline?per_page=100&page={page}'
                and type(capture['status']) is int and capture['status']==200 and capture['rawResponseSHA256']==pin
                and type(capture['responseBytes']) is int and capture['responseBytes']==len(row['raw'].encode())
                and _episode_time(capture['startedAt'])<=_episode_time(capture['completedAt'])<=instant(observed_at)
                and instant(BOOTSTRAP_1017['mergedAt'])<=_episode_time(capture['startedAt']),'original-complete-rest-page-custody')
            headers=capture['selectedPublicResponseHeaders'];require(type(headers) is dict and set(headers)<={'content-type','link'}
                and headers.get('content-type','').split(';')[0]=='application/json','reviewed-public-history-headers')
            if page==1:require('link' not in headers,'original-rest-first-page-no-next')
            else:
                links=headers.get('link','');parts=links.split(',')
                require(len(parts)==3 and set(re.findall(r'rel="([a-z]+)"',links))=={'prev','last','first'}
                    and all(re.fullmatch(r'\s*<https://api\.github\.com/repositories/1316165429/issues/1017/timeline\?per_page=100&page=1>; rel="(?:prev|last|first)"\s*',v) is not None for v in parts),'exact-terminal-empty-page-links')
        elif i==8:
            exact(capture,{'schema','transport','requestQuery','startedAt','completedAt','responseRawSHA256','responseBytes','exitCode','fixtureAdmissionGranted','sourceModified','actualFixtureReservation'})
            require(capture['schema']=='hikyo-158/root-direct-provider-readback/v1' and capture['transport']=='official-gh-api-graphql'
                and type(capture['exitCode']) is int and capture['exitCode']==0 and type(capture['responseBytes']) is int and capture['responseBytes']==len(row['raw'].encode())
                and capture['responseRawSHA256']==pin and all(capture[k] is False for k in ('fixtureAdmissionGranted','sourceModified','actualFixtureReservation'))
                and _episode_time(capture['startedAt'])<=_episode_time(capture['completedAt'])<=instant(observed_at)
                and type(capture['requestQuery']) is str and hashlib.sha256(capture['requestQuery'].encode()).hexdigest()=='5788e3a3595532620e463da2434b23aa434ac64a04cf28bf07ab2559ec6cdcf4','whole-retained-graphql-event-capture')
        else:
            # The actual schema query export has no execution timestamp record.
            # Keep its actual query only; authenticity is a separate Root gate.
            exact(capture,{'originalQuery'})
            require(type(capture['originalQuery']) is str and hashlib.sha256(capture['originalQuery'].encode()).hexdigest()=='3d08748c76a55a777b43437984e24220a19b483cde25f3e1e7513b7c81c9b381','original-schema-query-only-no-invented-custody')
    episode_semantics_checked(decision,pr,observed_at,parsed)

def public_commit_checked(commit,*,requires_dco):
    exact(commit,{'sha','treeSHA','parents','authorName','authorEmail','message','verification'})
    exact(commit['verification'],{'verified','reason'})
    require(sha(commit['sha'],40) and sha(commit['treeSHA'],40) and type(commit['parents']) is list
        and 1<=len(commit['parents'])<=2 and all(sha(v,40) for v in commit['parents'])
        and all(type(commit[k]) is str and 0<len(commit[k])<=16384 for k in ('authorName','authorEmail','message'))
        and commit['verification']['verified'] is True and commit['verification']['reason']=='valid','verified-public-commit')
    if requires_dco:
        require(('Signed-off-by: '+commit['authorName']+' <'+commit['authorEmail']+'>') in commit['message'].splitlines(),'signed-dco-pr-commit')


def pr_commit_range_checked(record,associated):
    exact(record,{'repositoryId','number','baseSHA','headSHA','total_count','pages'})
    require(type(record['repositoryId']) is int and record['repositoryId']==1316165429
        and type(record['number']) is int and record['number']==associated['number']
        and record['baseSHA']==associated['baseSHA'] and record['headSHA']==associated['headSHA']
        and type(record['total_count']) is int and 1<=record['total_count']<=250,'exact-whole-pr-commit-range')
    pages=record['pages'];require(type(pages) is list and 1<=len(pages)<=3,'complete-pr-commit-pages')
    commits=[]
    for index,page in enumerate(pages,1):
        exact(page,{'endpoint','page','perPage','commits','nextPage'})
        require(type(page['page']) is int and page['page']==index and type(page['perPage']) is int and page['perPage']==100
            and page['endpoint']==f"repos/Hikyo-Org/Hikyo/pulls/{record['number']}/commits?per_page=100&page={index}"
            and type(page['commits']) is list and 1<=len(page['commits'])<=100
            and same(page['nextPage'],index+1 if index<len(pages) else None)
            and (index==len(pages) or len(page['commits'])==100),'complete-commit-pagination-custody')
        commits.extend(page['commits'])
    require(len(commits)==record['total_count'],'whole-pr-commit-count')
    seen=[]
    for commit in commits:
        public_commit_checked(commit,requires_dco=True);seen.append(commit['sha'])
    require(len(set(seen))==len(seen) and seen[-1]==record['headSHA'],'unique-pr-commits-end-at-head')
    # Root authenticates the complete API/graph range and total count separately.
    return commits


def historical_ci_checked(proof,manifest,now):
    exact(proof,{'schema','observedAt','repositoryId','ownerId','candidateCommit','workflowCommit','candidateFiles','workflowFiles','absentFiles','ciRuns','prCommitRanges','bootstrapPR','mergeDecision','bootstrapCommit','firstParentLineage','workflowMainRef'})
    fresh(proof['observedAt'],now)
    require(proof['schema']=='hikyo-158/next-code-evidence/v4' and type(proof['repositoryId']) is int
        and proof['repositoryId']==1316165429 and type(proof['ownerId']) is int and proof['ownerId']==316726515,'code-repository')
    heads={}
    for kind in ('candidate','workflow'):
        commit=proof[kind+'Commit'];public_commit_checked(commit,requires_dco=(kind=='candidate'))
        heads[kind]=commit['sha'];files=proof[kind+'Files'];pins=manifest[kind+'FileSHA256s'];exact(files,pins)
        for path,body in files.items():
            require(type(body) is str and len(body)<=262144 and hashlib.sha256(body.encode()).hexdigest()==pins[path],'whole-public-source-pin')
        require(re.search(r'(?m)^go 1\.27\.3$',files['go.mod']) is not None,'go-1.27.3-required')
    exact(proof['absentFiles'],{'candidate','workflow'})
    for kind in heads:
        require(same(proof['absentFiles'][kind],{'commitSHA':heads[kind],'path':'go.work','gitTreeEntry':None}),'exact-go-work-absence')
    bootstrap=proof['bootstrapCommit'];public_commit_checked(bootstrap,requires_dco=False)
    lineage=proof['firstParentLineage'];require(type(lineage) is list and len(lineage)<=200,'finite-main-first-parent-lineage')
    previous=bootstrap;seen={bootstrap['sha']}
    for commit in lineage:
        public_commit_checked(commit,requires_dco=False)
        require(commit['sha'] not in seen and commit['parents'] and commit['parents'][0]==previous['sha'],'complete-unique-main-first-parent-lineage')
        seen.add(commit['sha']);previous=commit
    require(same(previous,proof['workflowCommit']),'current-main-is-exact-lineage-tip')
    reference=proof['workflowMainRef'];exact(reference,{'endpoint','observedAt','ref','object'})
    fresh(reference['observedAt'],now)
    require(reference['endpoint']=='repos/Hikyo-Org/Hikyo/git/ref/heads/main' and reference['ref']=='refs/heads/main'
        and same(reference['object'],{'type':'commit','sha':heads['workflow']})
        and instant(reference['observedAt'])<=instant(proof['observedAt']),'fresh-authenticated-canonical-main-reference')
    pr=proof['bootstrapPR'];exact(pr,{'number','state','merged_at','merge_commit_sha','headSHA','baseSHA','baseRef'})
    require(type(pr['number']) is int and pr['number']>=1017 and pr['state']=='closed' and pr['baseRef']=='main'
        and pr['merge_commit_sha']==bootstrap['sha'] and sha(pr['headSHA'],40) and sha(pr['baseSHA'],40),'merged-bootstrap-pr')
    exact(proof['ciRuns'],{'candidate','workflow'});exact(proof['prCommitRanges'],{'candidate','workflow'})
    for kind,run in proof['ciRuns'].items():
        exact(run,{'id','head_sha','head_branch','name','run_attempt','path','event','status','conclusion','apiPR','testedCommit','testedFileSHA256s','testedAbsentFiles','jobPages'})
        caller=('.github/workflows/ci-candidate.yml','pull_request') if kind=='candidate' else ('.github/workflows/ci-merge-group.yml','merge_group')
        require(type(run['id']) is int and 0<run['id']<10**25 and type(run['run_attempt']) is int and run['run_attempt']>0
            and (run['path'],run['event'])==caller and run['status']=='completed' and run['conclusion']=='success','complete-protected-ci-run')
        associated=run['apiPR'];exact(associated,{'number','headSHA','headRepositoryId','baseSHA','baseRef','mergeSHA'})
        tested=run['testedCommit'];exact(tested,{'sha','treeSHA','parents'})
        require(type(associated['number']) is int and associated['number']>0 and run['name']==('candidate-ci #'+str(associated['number']) if kind=='candidate' else 'trusted-merge-ci')
            and type(associated['headRepositoryId']) is int
            and associated['headRepositoryId']==1316165429 and associated['baseRef']=='main'
            and all(sha(associated[k],40) for k in ('headSHA','baseSHA','mergeSHA'))
            and tested['sha']==associated['mergeSHA'] and sha(tested['treeSHA'],40)
            and same(tested['parents'],[associated['baseSHA'],associated['headSHA']])
            and same(run['testedFileSHA256s'],manifest[kind+'FileSHA256s'])
            and same(run['testedAbsentFiles'],{'commitSHA':tested['sha'],'path':'go.work','gitTreeEntry':None}),'api-and-tested-merge-parent-proof')
        source_commits=pr_commit_range_checked(proof['prCommitRanges'][kind],associated)
        if kind=='candidate':require(same(source_commits[-1],proof['candidateCommit']),'candidate-tip-in-authenticated-pr-range')
        else:
            require([c['sha'] for c in source_commits]==BOOTSTRAP_1017['sourceChain'],'exact-bootstrap-three-commit-range')
            previous=BOOTSTRAP_1017['baseSHA']
            for commit in source_commits:
                require(same(commit['parents'],[previous]),'exact-bootstrap-source-parent-chain');previous=commit['sha']
            require(run['id']==BOOTSTRAP_1017['queueRunId'] and run['head_sha']==tested['sha']==associated['mergeSHA']==BOOTSTRAP_1017['mergeSHA'],'exact-original-bootstrap-queue-run-and-head')
        if kind=='candidate':
            require(run['head_sha']==heads[kind] and associated['headSHA']==heads[kind]
                and type(run['head_branch']) is str and 1<=len(run['head_branch'])<=200,'candidate-api-head-versus-tested-merge')
        else:
            require(run['head_sha']==tested['sha'] and type(run['head_branch']) is str
                and re.fullmatch(r'gh-readonly-queue/main/pr-'+str(pr['number'])+'-'+pr['baseSHA'],run['head_branch']) is not None
                and associated['number']==pr['number'] and associated['headSHA']==pr['headSHA'] and associated['baseSHA']==pr['baseSHA']
                and tested['treeSHA']==bootstrap['treeSHA']
                and same(bootstrap['parents'],tested['parents']),'merged-main-equals-tested-queue-tree-and-parents')
        pages=run['jobPages'];require(type(pages) is list and 1<=len(pages)<=3,'complete-attempt-job-pages')
        jobs=[];total=None
        for index,page in enumerate(pages,1):
            exact(page,{'endpoint','page','perPage','total_count','jobs','nextPage'})
            require(type(page['page']) is int and page['page']==index and type(page['perPage']) is int and page['perPage']==100
                and type(page['total_count']) is int and 1<=page['total_count']<=200
                and page['endpoint']==f"repos/Hikyo-Org/Hikyo/actions/runs/{run['id']}/attempts/{run['run_attempt']}/jobs?per_page=100&page={index}"
                and type(page['jobs']) is list and 1<=len(page['jobs'])<=100,'attempt-pagination-custody')
            require(total is None or total==page['total_count'],'stable-pagination-count');total=page['total_count']
            require(same(page['nextPage'],index+1 if index<len(pages) else None)
                and (index==len(pages) or len(page['jobs'])==100),'no-missing-job-page')
            jobs.extend(page['jobs'])
        require(len(jobs)==total,'complete-job-count')
        names=[];ids=[]
        for row in jobs:
            exact(row,{'id','run_id','run_attempt','head_sha','name','status','conclusion'})
            require(type(row['id']) is int and 0<row['id']<10**25 and type(row['run_id']) is int and row['run_id']==run['id']
                and type(row['run_attempt']) is int and row['run_attempt']==run['run_attempt'] and row['head_sha']==run['head_sha']
                and type(row['name']) is str and row['status']=='completed'
                and row['conclusion']==manifest['requiredCIJobs'][kind].get(row['name']),'exact-full-ci-job')
            names.append(row['name']);ids.append(row['id'])
        require(len(set(ids))==len(ids) and len(set(names))==len(names)
            and set(names)==set(manifest['requiredCIJobs'][kind]),'exact-full-ci-jobs')
    affirmative_merge_checked(proof['mergeDecision'],pr,proof['observedAt'])
    return heads


def fixture_tags_checked(rows,authorization_id):
    """Exact unique two-tag semantics; preserve caller's complete wire order."""
    require(type(rows) is list and len(rows)==2,'exact-two-fixture-tags')
    actual={}
    for row in rows:
        exact(row,{'Key','Value'})
        require(type(row['Key']) is str and type(row['Value']) is str and row['Key'] not in actual,'unique-string-fixture-tags')
        actual[row['Key']]=row['Value']
    require(actual=={'HikyoIssue':'158','Authorization':authorization_id},'exact-fixture-tag-values')
    return True


def setup_policy_checked(policy,archive,ids,default,documents=None):
    exact(policy,{'arn','policyId','defaultVersionId','versions'})
    require(policy['arn']==SETUP and policy['policyId']==SETUP_ID and policy['defaultVersionId']==default,'setup-policy-identity')
    exact(policy['versions'],ids)
    for version,row in policy['versions'].items():
        exact(row,{'document','isDefault'})
        require(type(row['isDefault']) is bool and row['isDefault'] is (version==default),'sole-policy-default')
        expected=documents['setup-user.managed-policy.json'] if version=='v6' else archive['versions'][version]['document']
        require(same(row['document'],expected),'preserved-whole-setup-document')


def owner_checked(proof,proposal,now):
    exact(proof,{'schema','observedAt','ownerContext','setupUser','setupPolicy','roles','providers','boundaryAbsent','oldKeys','environment'})
    fresh(proof['observedAt'],now)
    require(proof['schema']=='hikyo-158/next-owner-baseline/v1','owner-schema')
    context=proof['ownerContext'];exact(context,{'account','principalARN','source','observedAt','sourceReceipt'})
    fresh(context['observedAt'],now)
    require(context['account']==ACCOUNT and context['source']=='native-owner-console' and context['principalARN']!=USER_ARN
        and type(context['principalARN']) is str and re.fullmatch(r'arn:aws:(?:iam|sts)::'+ACCOUNT+r':(?:root|user/[A-Za-z0-9+=,.@_/-]{1,200}|assumed-role/[A-Za-z0-9+=,.@_/-]{1,200})',context['principalARN'])
        and type(context['sourceReceipt']) is dict,'complete-external-owner-context')
    source=context['sourceReceipt'];exact(source,{'schema','capturedAt','pageURL','accountHeaderText','principalARN'})
    fresh(source['capturedAt'],now)
    require(source['schema']=='hikyo-158/native-owner-context-projection/v1'
        and source['pageURL']=='https://us-east-1.console.aws.amazon.com/iam/home?region=eu-central-1#/home'
        and source['accountHeaderText']==ACCOUNT and source['principalARN']==context['principalARN'],'whole-owner-context-projection')
    user=proof['setupUser'];exact(user,{'UserName','UserId','Arn','MFADevices','accessKeys','groups','inlinePolicyNames','attachedPolicyARNs'})
    require(user['UserName']==USER and user['UserId']==USER_ID and user['Arn']==USER_ARN
        and user['MFADevices']==[{'UserName':USER,'SerialNumber':MFA}] and user['accessKeys']==user['groups']==user['inlinePolicyNames']==[]
        and user['attachedPolicyARNs']==[SETUP],'retained-sole-mfa-identity')
    archive=proposal['inputs']['iamClosure']['setupPolicy'];setup_policy_checked(proof['setupPolicy'],archive,['v1','v2','v3','v4','v5'],'v5')
    exact(proof['roles'],{'Roles','IsTruncated'})
    require(proof['roles']['IsTruncated'] is False and type(proof['roles']['Roles']) is list and len(proof['roles']['Roles'])<=1000,'complete-role-inventory')
    seen=[];names=[]
    for row in proof['roles']['Roles']:
        exact(row,{'RoleName','RoleId','Arn'})
        require(type(row['RoleName']) is str and re.fullmatch(r'[A-Za-z0-9+=,.@_-]{1,64}',row['RoleName']) is not None
            and row['RoleName'].casefold() not in {n.casefold() for n in ROLES}
            and type(row['RoleId']) is str and re.fullmatch(r'AROA[A-Z0-9]{8,64}',row['RoleId']) is not None
            and type(row['Arn']) is str and re.fullmatch(re.escape(f'arn:aws:iam::{ACCOUNT}:role/')+r'(?:[!-~]{1,510}/)?'+re.escape(row['RoleName']),row['Arn']) is not None,'foreign-or-existing-role')
        seen.append(row['RoleId']);names.append(row['RoleName'].casefold())
    require(len(seen)==len(set(seen)) and len(names)==len(set(names)) and proof['providers']=={'OpenIDConnectProviderList':[]}
        and proof['boundaryAbsent']=={'operation':'GetPolicy','arn':BOUNDARY,'errorCode':'NoSuchEntity'},'fixture-must-be-absent')
    old=scheduled_key_closure(proposal);exact(proof['oldKeys'],old)
    require(all(same(proof['oldKeys'][arn],row['metadata']) and proof['oldKeys'][arn]['KeyState']=='PendingDeletion'
        for arn,row in old.items()),'all-three-schedules-preserved')
    env=proof['environment'];exact(env,{'id','configuration','branchPolicies','OIDCDefaults','variables'})
    retained=proposal['inputs']['githubClosure']
    require(type(env['id']) is int and env['id']==ENV_ID and same(env['configuration'],retained['protectedEnvironmentConfiguration'])
        and env['branchPolicies']==[{'name':'main','type':'branch'}] and same(env['OIDCDefaults'],retained['OIDCDefaults'])
        and env['variables']==[],'retained-protected-environment')



def historical_absence_checked(proof,now,owner_context_sha):
    """Validate one independent Describe-only absence reference, never its authenticity.

    This is not runner historical cleanup. The hosted variable is permanently
    blank in this model. Root authenticates the exact raw native response,
    independently reviewed read intent and original owner/source custody.
    """
    exact(proof,{'schema','observedAt','account','region','secretARN','ownerContextSHA256',
        'reviewedReadPreparationSHA256','readIntent','readIntentSHA256','sourceReceipt','sourceReceiptSHA256',
        'providerCallsObserved','deleteIntentsObserved','deleteTransmissionsObserved'})
    fresh(proof['observedAt'],now)
    require(proof['schema']=='hikyo-158/next-historical-describe-absence/v1'
        and proof['account']==ACCOUNT and proof['region']==REGION and proof['secretARN']==HISTORICAL
        and sha(owner_context_sha) and proof['ownerContextSHA256']==owner_context_sha
        and sha(proof['reviewedReadPreparationSHA256']),'exact-historical-owner-reference')
    expected_intent={'operation':'DescribeSecret','targetARN':HISTORICAL,
        'endpoint':f'https://secretsmanager.{REGION}.amazonaws.com/',
        'target':'secretsmanager.DescribeSecret','maximumForwards':1,'allowDelete':False}
    require(same(proof['readIntent'],expected_intent) and proof['readIntentSHA256']==digest(expected_intent)
        and type(proof['providerCallsObserved']) is int and proof['providerCallsObserved']==1
        and type(proof['deleteIntentsObserved']) is int and proof['deleteIntentsObserved']==0
        and type(proof['deleteTransmissionsObserved']) is int and proof['deleteTransmissionsObserved']==0,'single-describe-no-delete')
    source=proof['sourceReceipt']
    exact(source,{'schema','account','region','service','operation','target','endpoint','selector',
        'requestBodyBytes','requestBodySHA256','sentAtMS','receivedAtMS','storedAtMS','status','resourceNotFound',
        'rawResponseBytes','rawResponseSHA256','rawResponse','publicMainSHA256','reviewedPreparationSHA256',
        'accountHeaderObserved','ownerIdentityVerifiedByObserver','publicSourceBytesVerifiedByObserver',
        'providerAPIsCalledByObserver'})
    require(proof['sourceReceiptSHA256']==digest(source),'whole-historical-source-reference')
    wire=json.dumps({'SecretId':HISTORICAL},separators=(',',':')).encode()
    require(source['schema']=='hikyo-158/native-historical-describe-response/v1'
        and source['account']==ACCOUNT and source['region']==REGION and source['service']=='secretsmanager'
        and source['operation']=='DescribeSecret' and source['target']=='secretsmanager.DescribeSecret'
        and source['endpoint']==expected_intent['endpoint'] and same(source['selector'],{'SecretId':HISTORICAL})
        and type(source['requestBodyBytes']) is int and source['requestBodyBytes']==len(wire)
        and source['requestBodySHA256']==hashlib.sha256(wire).hexdigest()
        and source['publicMainSHA256']=='d412372053a5b7a265fa704e4d483522f4cff28e93fcf8d46ab7f18f797c2520'
        and source['reviewedPreparationSHA256']==proof['reviewedReadPreparationSHA256']
        and source['accountHeaderObserved'] is True and source['ownerIdentityVerifiedByObserver'] is False
        and source['publicSourceBytesVerifiedByObserver'] is False and source['providerAPIsCalledByObserver'] is False,'exact-historical-native-describe')
    # Millisecond native request times remain intact. A new wrapper observation
    # cannot freshen an older transmission or a response after a key mutation.
    require(all(type(source[key]) is int and source[key]>=0 for key in ('sentAtMS','receivedAtMS','storedAtMS')),'historical-integer-native-times')
    epoch=datetime(1970,1,1,tzinfo=timezone.utc)
    def epoch_ms(at):
        delta=at-epoch
        return delta.days*86400000+delta.seconds*1000+delta.microseconds//1000
    observed_ms=epoch_ms(instant(proof['observedAt']));now_ms=epoch_ms(now)
    require(source['sentAtMS']<=source['receivedAtMS']<=source['storedAtMS']<=observed_ms<=now_ms
        and 0<=now_ms-source['sentAtMS']<=300000
        and source['storedAtMS']-source['sentAtMS']<=20000,'historical-earliest-request-freshness-order')
    require(type(source['status']) is int and source['status']==400 and source['resourceNotFound'] is True
        and type(source['rawResponse']) is str and type(source['rawResponseBytes']) is int,'typed-historical-not-found')
    raw=source['rawResponse'].encode('utf-8')
    require(2<=len(raw)<=16384 and source['rawResponseBytes']==len(raw)
        and source['rawResponseSHA256']==hashlib.sha256(raw).hexdigest(),'bounded-historical-response-bytes')
    # Match the existing observer's closed error grammar before JSON decoding:
    # no success metadata, escaped/duplicate/unknown keys or secret-value fields.
    ws=r'[ \t\r\n]*'
    token=r'"(?:[^"\\\x00-\x1f]|\\(?:["\\/bfnrt]|u[0-9a-fA-F]{4}))*"'
    typed=r'"__type"'+ws+':'+ws+r'"(?:ResourceNotFoundException|com\.amazonaws\.secretsmanager#ResourceNotFoundException)"'
    message=r'"(?:Message|message)"'+ws+':'+ws+token
    pattern=ws+r'\{'+ws+r'(?:'+typed+r'(?:'+ws+','+ws+message+r')?|'+message+ws+','+ws+typed+r')'+ws+r'\}'+ws
    require(re.fullmatch(pattern,source['rawResponse']) is not None
        and 'SecretString' not in source['rawResponse'] and 'SecretBinary' not in source['rawResponse'],'closed-historical-error-projection')
    decoded=strict_json(raw)
    require(decoded['__type'] in ('ResourceNotFoundException','com.amazonaws.secretsmanager#ResourceNotFoundException')
        and set(decoded) in ({'__type'},{'__type','Message'},{'__type','message'}),'typed-historical-provider-error')
    # These digests only crosslink externally authenticated references. They do
    # not grant authority, attest traffic completeness or call a provider.
    return digest(proof)


def money(value):
    require(type(value) is str and re.fullmatch(r'(?:0|[1-9][0-9]{0,5})\.[0-9]{2}',value) is not None,'explicit-gross-money')
    return Decimal(value)


def banked_billing_checked(billing,gross,now,*,fresh_required):
    """Closed actual combined projection; no authority or credit assumptions."""
    exact(billing,{'schema','source','observedAt','account','currency',
        'grossDisplayedUSD','billableDisplayedUSD','unreflectedTaxFeesReserveUSD'})
    if fresh_required:fresh(billing['observedAt'],now)
    else:require(instant(billing['observedAt'])<=now,'no-future-combined-billing')
    require(billing['schema']=='hikyo-158/native-combined-billing-projection/v1' and billing['source']=='native-billing-console'
        and billing['account']==ACCOUNT and billing['currency']=='USD' and money(billing['grossDisplayedUSD'])==gross,
        'fresh-actual-combined-billing-no-credit-offset')
    return money(billing['billableDisplayedUSD']),money(billing['unreflectedTaxFeesReserveUSD'])


def budget_checked(proof,now):
    exact(proof,{'schema','observedAt','currency','grossCharges','completionReserves','targetUSD','absoluteMaximumUSD','bankedBilling'})
    fresh(proof['observedAt'],now)
    require(proof['schema']=='hikyo-158/next-gross-budget/v1' and proof['currency']=='USD'
        and proof['targetUSD']=='5.00' and proof['absoluteMaximumUSD']=='10.00','gross-budget-scope')
    require(type(proof['grossCharges']) is list and 1<=len(proof['grossCharges'])<=32,'actual-positive-gross-records')
    gross=Decimal(0)
    for row in proof['grossCharges']:
        exact(row,{'service','amountUSD','sourceRecord'})
        require(type(row['service']) is str and 1<=len(row['service'])<=128,'billing-service')
        source=row['sourceRecord'];exact(source,{'schema','source','observedAt','account','period','currency','service','grossDisplayedUSD'})
        fresh(source['observedAt'],now)
        require(source['schema']=='hikyo-158/native-gross-charge-projection/v1' and source['source']=='native-billing-console'
            and source['account']==ACCOUNT and source['currency']=='USD' and source['service']==row['service']
            and source['grossDisplayedUSD']==row['amountUSD'] and type(source['period']) is str
            and re.fullmatch(r'[0-9]{4}-(?:0[1-9]|1[0-2])',source['period']) is not None,'whole-gross-source-record')
        gross+=money(row['amountUSD'])
    floors={'existingCompletionReserve':'0.74','newKeyReserve':'0.24','secretStorageReserve':'0.43',
        'apiReserve':'0.10','lagReserve':'0.25','contingency':'0.50'}
    exact(proof['completionReserves'],floors)
    reserve=sum((money(v) for v in proof['completionReserves'].values()),Decimal(0))
    billable,fees=banked_billing_checked(proof['bankedBilling'],gross,now,fresh_required=True)
    require(gross+reserve+fees<Decimal('15.00') and billable+reserve+fees<Decimal('15.00'),
        'banked-gross-and-billable-strictly-below-fifteen')
    require(all(money(proof['completionReserves'][k])>=money(v) for k,v in floors.items())
        and gross+reserve<=Decimal('3.00') and gross<Decimal('5.00'),'fresh-conservative-admission-three')
    return format(gross+reserve,'.2f')


def _prepare_with_checker(proposal,records,reviewed_pins,*,lease_start,lease_until,namespace,explicit_now,ci_checker):
    proposal_checked(proposal);clock(explicit_now)
    reviewed_records(records,reviewed_pins,{'code','owner','budget','historicalAbsence'})
    heads=ci_checker(records['code'],proposal['sourceManifest'],explicit_now);owner_checked(records['owner'],proposal,explicit_now)
    forecast=budget_checked(records['budget'],explicit_now)
    historical_absence_checked(records['historicalAbsence'],explicit_now,digest(records['owner']['ownerContext']))
    start,until=instant(lease_start),instant(lease_until)
    require(instant(proposal['inputs']['approval']['recordedAt'])<=start<=explicit_now<until
        <=min(start+timedelta(hours=24),instant(HORIZON)),'finite-lease-horizon')
    require(type(namespace) is str and namespace==configuration_checked()['namespace']
        and namespace not in proposal['usedNamespaces'],'new-unused-actually-approved-namespace-only')
    suffix=namespace[len('hikyo-e2e-'):-1]
    return {'schema':'hikyo-158/next-fixture-preparation/v1','phase':'prepared-unbound-run',**COMMON_FLAGS,
        'proposal':copy.deepcopy(proposal),'proposalSHA256':digest(proposal),'records':copy.deepcopy(records),
        'attemptTrustBindingSHA256':proposal['attemptTrustBindingSHA256'],
        'reviewedRecordPins':copy.deepcopy(reviewed_pins),'derivedAt':stamp(explicit_now),'candidateSHA':heads['candidate'],
        'workflowSHA':heads['workflow'],'lease':{'start':lease_start,'until':lease_until,'horizon':HORIZON},
        'namespace':namespace,'externalId':'hikyo-158-'+suffix,'authorizationId':'hikyo-158-owner-approved-'+suffix,
        'grossCompletionForecastUSD':forecast,'reservationProposal':{'oldRetained':RETAINED_RESERVATIONS,'newMaximum':NEW_RESERVATIONS,'total':LIFETIME_RESERVATIONS,'remainingDispatchesAfterOne':0},
        'run':None,'inventory':None}


def preparation_checked(prepared):
    expected=_prepare_with_checker(prepared['proposal'],prepared['records'],prepared['reviewedRecordPins'],lease_start=prepared['lease']['start'],
        lease_until=prepared['lease']['until'],namespace=prepared['namespace'],explicit_now=instant(prepared['derivedAt']),ci_checker=_ci_reconstruct_original)
    require(same(expected,prepared),'whole-preparation-reconstruction')


def key_checked(proof,proposal,now):
    exact(proof,{'schema','observedAt','intent','createResponse','readback','preparationSHA256','sentAt','receivedAt','claimId','intentSHA256','forwardsObserved'})
    require(sha(proof['preparationSHA256']) and instant(proof['sentAt'])<=instant(proof['receivedAt'])<=instant(proof['observedAt']),'key-custody-order')
    fresh(proof['observedAt'],now)
    require(proof['schema']=='hikyo-158/next-created-key-custody/v1','key-custody-schema')
    intent=proof['intent'];exact(intent,{'operation','account','region','actorARN','maximumForwards','request'})
    require(type(proof['claimId']) is str and re.fullmatch(r'[a-zA-Z0-9-]{8,100}',proof['claimId']) is not None
        and type(proof['forwardsObserved']) is int and proof['forwardsObserved']==1
        and proof['intentSHA256']==digest({'preparationSHA256':proof['preparationSHA256'],'claimId':proof['claimId'],'intent':intent}),'one-key-custody-crosslink')
    require(intent['operation']=='CreateKey' and intent['account']==ACCOUNT and intent['region']==REGION
        and type(intent['actorARN']) is str and intent['actorARN']!=USER_ARN
        and type(intent['maximumForwards']) is int and intent['maximumForwards']==1,'one-new-key-intent')
    request=intent['request'];exact(request,{'Policy','Description','Origin','Tags','KeyUsage','KeySpec','MultiRegion'})
    require(request['Origin']=='AWS_KMS' and request['Tags']==[] and request['KeyUsage']=='ENCRYPT_DECRYPT'
        and request['KeySpec']=='SYMMETRIC_DEFAULT' and request['MultiRegion'] is False
        and type(request['Description']) is str and 1<=len(request['Description'])<=8192,'new-key-request')
    policy=request['Policy'];exact(policy,{'Version','Id','Statement'})
    require(policy['Version']=='2012-10-17' and type(policy['Id']) is str and 1<=len(policy['Id'])<=128
        and policy['Statement']==[{'Sid':'Enable IAM User Permissions','Effect':'Allow','Principal':{'AWS':f'arn:aws:iam::{ACCOUNT}:root'},'Action':'kms:*','Resource':'*'}],'root-delegation-only')
    exact(proof['createResponse'],{'KeyMetadata','RequestId'})
    meta=proof['createResponse']['KeyMetadata']
    required={'Arn','KeyId','AWSAccountId','KeyState','Enabled','Origin','KeyUsage','KeySpec','MultiRegion','CreationDate','KeyManager'}
    optional={'CustomerMasterKeySpec','EncryptionAlgorithms','Description','CurrentKeyMaterialId'}
    require(type(meta) is dict and required<=set(meta)<=required|optional,'closed-new-key-metadata')
    require(type(meta['KeyId']) is str and re.fullmatch(r'[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12}',meta['KeyId']) is not None
        and meta['Arn']==f'arn:aws:kms:{REGION}:{ACCOUNT}:key/'+meta['KeyId'] and meta['Arn'] not in scheduled_key_closure(proposal)
        and meta['AWSAccountId']==ACCOUNT and meta['KeyState']=='Enabled' and meta['Enabled'] is True and meta['MultiRegion'] is False
        and meta['Origin']=='AWS_KMS' and meta['KeyUsage']=='ENCRYPT_DECRYPT' and meta['KeySpec']=='SYMMETRIC_DEFAULT' and meta['KeyManager']=='CUSTOMER'
        and type(meta['CreationDate']) in (int,float) and math.isfinite(meta['CreationDate'])
        and instant(proof['sentAt']).timestamp()<=meta['CreationDate']<=instant(proof['receivedAt']).timestamp()<=now.timestamp()
        and type(proof['createResponse']['RequestId']) is str and 8<=len(proof['createResponse']['RequestId'])<=128,'new-key-actual-identity')
    read=proof['readback'];exact(read,{'metadata','policy','rotationEnabled','aliases','Truncated'})
    require(same(read,{'metadata':meta,'policy':policy,'rotationEnabled':False,'aliases':[],'Truncated':False}),'whole-new-key-readback')
    return meta['Arn']


def render_documents(proposal,run):
    originals=proposal['inputs']['lastPlan']['documents'];require(set(originals)==FILES,'eighteen-template-documents')
    documents={};audit=[]
    for name,original in originals.items():
        def walk(value,path=()):
            if type(value) is dict:return {k:walk(v,path+(k,)) for k,v in value.items()}
            if type(value) is list:return [walk(v,path+(i,)) for i,v in enumerate(value)]
            new=value
            if len(path)==5 and path[0]=='Statement' and path[2]=='Condition' and path[3] in ('DateLessThan','DateGreaterThanEquals') and path[4]=='aws:CurrentTime':
                require(value in (OLD_AUTH,OLD_CLEANUP,HORIZON),'known-template-clock')
                new=run['authUntil'] if value==OLD_AUTH else run['cleanupUntil'] if value==OLD_CLEANUP else HORIZON
            elif value==OLD_KEY:
                require(path[-1] in ('Resource','secretsmanager:KmsKeyArn'),'exact-key-path');new=run['keyARN']
            elif type(value) is str and ':secret:'+OLD_PREFIX in value:
                require(path[-1] in ('Resource','kms:EncryptionContext:SecretARN') or len(path)>=2 and path[-2]=='Resource','exact-namespace-path')
                new=value.replace(':secret:'+OLD_PREFIX,':secret:'+run['prefix'])
            elif value=='hikyo-158-20261019c02':
                require(path[-1]=='sts:ExternalId','exact-external-id-path');new=run['externalId']
            if new!=value:audit.append({'file':name,'path':list(path),'before':value,'after':new})
            return new
        documents[name]=walk(original)
    cleanup=documents['hikyo-158-cleanup.policy.json'];rows=cleanup['Statement']
    deleted=[row for row in rows if row.get('Sid')=='HistoricalExactForceDelete']
    require(len(deleted)==1 and deleted[0]['Action']==['secretsmanager:DeleteSecret'] and deleted[0]['Resource']==HISTORICAL,'exact-obsolete-delete-row')
    rows.remove(deleted[0]);audit.append({'file':'hikyo-158-cleanup.policy.json','removedHistoricalDeleteStatement':deleted[0]})
    boundary=documents['setup-role-boundary.json']
    changed=0
    for row in boundary['Statement']:
        if row.get('Action')==['secretsmanager:DeleteSecret'] and type(row.get('Resource')) is list and HISTORICAL in row['Resource']:
            before=copy.deepcopy(row['Resource']);row['Resource']=[v for v in row['Resource'] if v!=HISTORICAL];changed+=1
            audit.append({'file':'setup-role-boundary.json','removedHistoricalDeleteResource':HISTORICAL,'before':before,'after':row['Resource']})
    require(changed==1,'exact-boundary-delete-removal')
    for name,doc in documents.items():
        for row in doc['Statement']:
            actions=row.get('Action',[]);actions=[actions] if type(actions) is str else actions
            resources=row.get('Resource',[]);resources=[resources] if type(resources) is str else resources
            require(not (row.get('Effect')=='Allow' and 'secretsmanager:DeleteSecret' in actions and HISTORICAL in resources),'no-historical-delete')
        if name in ('setup-user.managed-policy.json','setup-role-boundary.json'):require(len(canonical(doc))<=6144,'managed-policy-quota')
    require(documents['hikyo-158-source.trust.json']['Statement'][0]['Condition']['StringEquals']=={
        'token.actions.githubusercontent.com:aud':'sts.amazonaws.com','token.actions.githubusercontent.com:sub':SUBJECT},'immutable-subject')
    return documents,audit


def pending_job_checked(job,prepared,explicit_now,variables,expected_id=None):
    exact(job,{'schema','observedAt','run','jobs','environment','deploymentReviews'})
    fresh(job['observedAt'],explicit_now)
    require(job['schema']=='hikyo-158/next-pending-hosted-job/v1','pending-job-schema')
    r=job['run'];exact(r,{'id','run_attempt','head_sha','head_branch','event','path','repositoryId','ownerId','inputs'})
    require(type(r['id']) is int and 0<r['id']<10**25 and str(r['id']) not in {'39495071346','39338286913','39334716444','39336083859','39221282489','39158333194'}
        and type(r['run_attempt']) is int and r['run_attempt']==1 and r['head_sha']==prepared['workflowSHA'] and r['head_branch']=='main'
        and r['event']=='workflow_dispatch' and r['path']=='.github/workflows/aws-acceptance.yml'
        and type(r['repositoryId']) is int and r['repositoryId']==1316165429 and type(r['ownerId']) is int and r['ownerId']==316726515
        and r['inputs']=={'reviewed_sha':prepared['candidateSHA']},'new-exact-dispatch')
    require(same(job['jobs'],[{'name':'sandbox','status':'waiting','conclusion':None,'run_id':r['id'],'run_attempt':1}])
        and job['deploymentReviews']==[] and same(job['environment'],{**prepared['records']['owner']['environment'],'variables':variables}),'human-deployment-still-pending')
    require(expected_id is None or str(r['id'])==expected_id,'same-pending-run-at-seal')
    return r


def first_mutation_evidence(prepared,key):
    """Pure reference to included immutable preparation records, never authority."""
    preparation_checked(prepared)
    return {'schema':'hikyo-158/first-key-forward-evidence/v1',
        'preparationSHA256':digest(prepared),'evidencePath':['preparation','records'],
        'evidenceSHA256':digest(prepared['records']),'operation':'CreateKey',
        'claimId':key['claimId'],'keyIntentSHA256':key['intentSHA256'],'sentAt':key['sentAt']}


def first_key_forward_window(prepared,*,explicit_now,minimum_remaining_ms):
    """Future-only preclaim/adjacent-arm gate; no clock, guard or grant is created.

    A future reviewed caller must check before consuming a new reservation and
    again before guard activation/Finish. The returned expiry is a ceiling,
    never a new five-minute lease starting at an intent/arm timestamp.
    """
    preparation_checked(prepared);clock(explicit_now)
    require(type(minimum_remaining_ms) is int and 0<minimum_remaining_ms<=300000,'explicit-positive-first-forward-startup-buffer')
    records=prepared['records'];owner=records['owner'];budget=records['budget'];historical=records['historicalAbsence']
    heads=ci_checked(records['code'],prepared['proposal']['sourceManifest'],explicit_now)
    owner_checked(owner,prepared['proposal'],explicit_now);budget_checked(budget,explicit_now)
    historical_absence_checked(historical,explicit_now,digest(owner['ownerContext']))
    require(heads=={'candidate':prepared['candidateSHA'],'workflow':prepared['workflowSHA']},'first-forward-code-heads-preserved')
    observations=[('ci.observedAt',records['code']['observedAt']),
        ('ci.workflowMainRef',records['code']['workflowMainRef']['observedAt']),
        ('owner.observedAt',owner['observedAt']),('owner.context',owner['ownerContext']['observedAt']),
        ('owner.source',owner['ownerContext']['sourceReceipt']['capturedAt']),
        ('budget.observedAt',budget['observedAt']),('historical.observedAt',historical['observedAt'])]
    observations.extend(('budget.source.'+str(i),row['sourceRecord']['observedAt']) for i,row in enumerate(budget['grossCharges']))
    observations.append(('budget.bankedBilling',budget['bankedBilling']['observedAt']))
    epoch=datetime(1970,1,1,tzinfo=timezone.utc)
    sent=epoch+timedelta(milliseconds=historical['sourceReceipt']['sentAtMS'])
    observations.append(('historical.nativeSentAt',stamp(sent)))
    deadlines=[{'gate':gate,'observedAt':value,'expiresAt':stamp(instant(value)+timedelta(minutes=5))} for gate,value in observations]
    deadlines.extend([{'gate':'lease.until','observedAt':None,'expiresAt':prepared['lease']['until']},
        {'gate':'horizon','observedAt':None,'expiresAt':HORIZON}])
    require(instant(prepared['lease']['start'])<=explicit_now,'first-forward-lease-started')
    def floor_ms(value):
        delta=value-epoch
        return delta.days*86400000+delta.seconds*1000+delta.microseconds//1000
    expiry=min(instant(row['expiresAt']) for row in deadlines)
    expiry_ms=floor_ms(expiry);now_ms=floor_ms(explicit_now)
    require(explicit_now+timedelta(milliseconds=minimum_remaining_ms)<=epoch+timedelta(milliseconds=expiry_ms),
        'insufficient-first-forward-startup-buffer')
    return {'schema':'hikyo-158/first-key-forward-window/v1',**COMMON_FLAGS,
        'preparationSHA256':digest(prepared),'observedAt':stamp(explicit_now),
        'minimumRemainingMS':minimum_remaining_ms,'notBeforeMS':now_ms,'expiresAtMS':expiry_ms,
        'deadlines':deadlines}


def _bind_with_checker(prepared,records,reviewed_pins,*,reviewed_start,explicit_now,ci_checker):
    preparation_checked(prepared);clock(explicit_now)
    legacy_fields={'key','job','budget','code','owner','historicalAbsence'}
    require(type(records) is dict,'binding-records-object')
    dual=set(records)==legacy_fields|{'firstMutationEvidence'}
    reviewed_records(records,reviewed_pins,legacy_fields|{'firstMutationEvidence'} if dual else legacy_fields)
    # Legacy reconstruction retains its original same-record semantics. The
    # future dual branch resolves first-send evidence ONLY from the already
    # included original preparation, not an alternate supplied baseline.
    heads=ci_checker(records['code'],prepared['proposal']['sourceManifest'],explicit_now)
    owner_checked(records['owner'],prepared['proposal'],explicit_now)
    require(heads=={'candidate':prepared['candidateSHA'],'workflow':prepared['workflowSHA']},'binding-code-heads-preserved')
    require(records['owner']['ownerContext']['principalARN']==prepared['records']['owner']['ownerContext']['principalARN'],'binding-owner-preserved')
    first_mutation=instant(records['key']['sentAt'])
    if dual:
        reference=records['firstMutationEvidence']
        exact(reference,{'schema','preparationSHA256','evidencePath','evidenceSHA256','operation','claimId','keyIntentSHA256','sentAt'})
        require(same(reference,first_mutation_evidence(prepared,records['key'])),'exact-original-first-mutation-reference')
        require(instant(prepared['derivedAt'])<=first_mutation,'first-mutation-after-original-preparation')
        original=prepared['records']
        original_heads=ci_checker(original['code'],prepared['proposal']['sourceManifest'],first_mutation)
        require(original_heads==heads,'binding-original-and-current-heads-preserved')
        owner_checked(original['owner'],prepared['proposal'],first_mutation)
        budget_checked(original['budget'],first_mutation)
        historical_absence_checked(original['historicalAbsence'],first_mutation,digest(original['owner']['ownerContext']))
        historical_absence_checked(records['historicalAbsence'],explicit_now,digest(records['owner']['ownerContext']))
    else:
        ci_checker(records['code'],prepared['proposal']['sourceManifest'],first_mutation)
        owner_checked(records['owner'],prepared['proposal'],first_mutation)
        budget_checked(records['budget'],first_mutation)
        historical_absence_checked(records['historicalAbsence'],explicit_now,digest(records['owner']['ownerContext']))
        historical_absence_checked(records['historicalAbsence'],first_mutation,digest(records['owner']['ownerContext']))
    require(records['historicalAbsence']['sourceReceipt']['sentAtMS']>=prepared['records']['historicalAbsence']['sourceReceipt']['sentAtMS'],'historical-binding-read-order')
    require(instant(prepared['lease']['start'])<=explicit_now<instant(prepared['lease']['until']),'lease-active')
    key=key_checked(records['key'],prepared['proposal'],explicit_now);budget_checked(records['budget'],explicit_now)
    require(records['key']['intent']['actorARN']==prepared['records']['owner']['ownerContext']['principalARN']
        and records['key']['preparationSHA256']==digest(prepared)
        and instant(prepared['lease']['start'])<=instant(records['key']['sentAt']),'key-bound-to-this-preparation')
    r=pending_job_checked(records['job'],prepared,explicit_now,[])
    begin=instant(reviewed_start)
    require(explicit_now<=begin<=explicit_now+timedelta(minutes=5)
        and begin+timedelta(hours=4)<=instant(prepared['lease']['until']),'imminent-run-windows')
    run={'runId':str(r['id']),'runAttempt':1,'candidateSHA':prepared['candidateSHA'],'workflowSHA':prepared['workflowSHA'],
        'environmentId':ENV_ID,'prefix':prepared['namespace'],'externalId':prepared['externalId'],'authorizationId':prepared['authorizationId'],
        'keyARN':key,'start':reviewed_start,'authUntil':stamp(begin+timedelta(hours=2)),
        'cleanupUntil':stamp(begin+timedelta(hours=4)),'setupUntil':stamp(begin+timedelta(hours=4)),
        'provisioningCutoff':stamp(begin+timedelta(minutes=55)),'leaseUntil':prepared['lease']['until'],'horizon':HORIZON}
    documents,audit=render_documents(prepared['proposal'],run)
    return {'schema':'hikyo-158/next-fixture-bound-plan/v1','phase':'bound-not-sealed',**COMMON_FLAGS,
        'preparation':copy.deepcopy(prepared),'preparationSHA256':digest(prepared),'records':copy.deepcopy(records),
        'attemptTrustBindingSHA256':prepared['attemptTrustBindingSHA256'],
        'reviewedRecordPins':copy.deepcopy(reviewed_pins),'derivedAt':stamp(explicit_now),'run':run,
        'documents':documents,'documentsSHA256':digest(documents),'audit':audit,
        'plannedInventory':{'keyARN':key,'setupUserId':USER_ID,'environmentId':ENV_ID,'boundaryARN':BOUNDARY,
            'boundaryPolicyId':None,'oidcProviderARN':PROVIDER,'roleIds':None},'reservations':{'oldRetained':RETAINED_RESERVATIONS,'newMaximum':NEW_RESERVATIONS,'total':LIFETIME_RESERVATIONS}}


def bound_checked(bound,now):
    expected=_bind_with_checker(bound['preparation'],bound['records'],bound['reviewedRecordPins'],reviewed_start=bound['run']['start'],explicit_now=instant(bound['derivedAt']),ci_checker=_ci_reconstruct_original)
    require(same(expected,bound),'whole-bound-reconstruction');clock(now)
    require(instant(bound['run']['start'])<=now<instant(bound['run']['provisioningCutoff'])
        and now+timedelta(minutes=65)<=instant(bound['run']['authUntil']),'immutable-startup-buffer')


def _seal_inventory_with_admission(bound,records,reviewed_pins,*,explicit_now,admission_checker):
    bound_checked(bound,explicit_now)
    reviewed_records(records,reviewed_pins,{'owner','inventory','custody','job','historicalAbsence'})
    historical_absence_checked(records['historicalAbsence'],explicit_now,digest(bound['records']['owner']['ownerContext']))
    require(records['historicalAbsence']['sourceReceipt']['sentAtMS']>=bound['records']['historicalAbsence']['sourceReceipt']['sentAtMS'],'historical-seal-read-order')
    pending_job_checked(records['job'],bound['preparation'],explicit_now,environment_variables(bound['run']),bound['run']['runId'])
    owner=records['owner'];exact(owner,{'schema','observedAt','setupPolicy','setupUserId','MFADevices','attachedPolicyARNs','inlinePolicyNames','groups','accessKeys'})
    fresh(owner['observedAt'],explicit_now)
    require(owner['schema']=='hikyo-158/next-activated-owner/v1' and owner['setupUserId']==USER_ID
        and owner['MFADevices']==[{'UserName':USER,'SerialNumber':MFA}] and owner['attachedPolicyARNs']==[SETUP]
        and owner['inlinePolicyNames']==owner['groups']==owner['accessKeys']==[],'activated-identity')
    proposal=bound['preparation']['proposal'];archive=proposal['inputs']['iamClosure']['setupPolicy']
    setup_policy_checked(owner['setupPolicy'],archive,['v2','v3','v4','v5','v6'],'v6',bound['documents'])
    inventory=records['inventory'];exact(inventory,{'schema','observedAt','roles','provider','boundary','key','oldKeys'})
    fresh(inventory['observedAt'],explicit_now);require(inventory['schema']=='hikyo-158/next-effective-inventory/v1','inventory-schema')
    exact(inventory['oldKeys'],scheduled_key_closure(proposal))
    require(all(same(inventory['oldKeys'][arn],row['metadata']) for arn,row in scheduled_key_closure(proposal).items()),'old-schedules-after-provision')
    require(same(inventory['key'],bound['records']['key']['readback']),'whole-new-key-preserved')
    boundary=inventory['boundary'];exact(boundary,{'arn','policyId','defaultVersionId','versions'})
    require(boundary['arn']==BOUNDARY and type(boundary['policyId']) is str and re.fullmatch(r'ANPA[A-Z0-9]{8,64}',boundary['policyId']) is not None
        and boundary['policyId'] not in {SETUP_ID,'ANPAROHA7PNS5QVDJKPIN'} and boundary['defaultVersionId']=='v1'
        and same(boundary['versions'],{'v1':{'isDefault':True,'document':bound['documents']['setup-role-boundary.json']}}),'actual-new-boundary')
    custody=records['custody'];exact(custody,{'schema','boundPlanSHA256','boundary','provider','roles','operations'})
    require(custody['schema']=='hikyo-158/next-created-identities/v1' and custody['boundPlanSHA256']==digest(bound),'custody-schema')
    require(custody['boundary']=={'operation':'CreatePolicy','arn':BOUNDARY,'policyId':boundary['policyId'],'defaultVersionId':'v1',
        'documentSHA256':digest(bound['documents']['setup-role-boundary.json'])},'actual-boundary-create-custody')
    exact(inventory['roles'],ROLES);exact(custody['roles'],ROLES)
    ids=[];old_ids=set(proposal['inputs']['firstEpoch']['inventory']['roleIds'].values())
    for name,row in inventory['roles'].items():
        exact(row,{'Arn','RoleName','RoleId','Path','MaxSessionDuration','Tags','PermissionsBoundary','AssumeRolePolicyDocument','inlinePolicies','attachedPolicies'})
        require(row['Arn']==f'arn:aws:iam::{ACCOUNT}:role/'+name and row['RoleName']==name and row['Path']=='/'
            and type(row['RoleId']) is str and re.fullmatch(r'AROA[A-Z0-9]{8,64}',row['RoleId']) is not None and row['RoleId'] not in old_ids
            and same(row['AssumeRolePolicyDocument'],bound['documents'][name+'.trust.json'])
            and row['PermissionsBoundary']=={'PermissionsBoundaryType':'Policy','PermissionsBoundaryArn':BOUNDARY}
            and row['attachedPolicies']==[] and same(row['inlinePolicies'],{'hikyo-158-fixture':bound['documents'][name+'.policy.json']}),'new-role-whole-metadata')
        require(type(row['MaxSessionDuration']) is int and row['MaxSessionDuration']==
            next(r['maxSessionDuration'] for r in proposal['inputs']['firstProvisionPlan']['roles'] if r['name']==name),'exact-role-duration')
        fixture_tags_checked(row['Tags'],bound['run']['authorizationId'])
        require(custody['roles'][name]=={'operation':'CreateRole','arn':row['Arn'],'roleId':row['RoleId'],'roleName':name},'actual-role-create-custody')
        ids.append(row['RoleId'])
    require(len(set(ids))==8,'unique-new-role-ids')
    provider=inventory['provider'];exact(provider,{'arn','allProviders','metadata'})
    meta=provider['metadata'];exact(meta,{'Url','ClientIDList','ThumbprintList','Tags','CreateDate'})
    require(provider['arn']==PROVIDER and provider['allProviders']==[{'Arn':PROVIDER}]
        and meta['Url']=='token.actions.githubusercontent.com' and meta['ClientIDList']==['sts.amazonaws.com']
        and type(meta['ThumbprintList']) is list and 1<=len(meta['ThumbprintList'])<=2
        and all(sha(t,40) for t in meta['ThumbprintList']) and len(set(meta['ThumbprintList']))==len(meta['ThumbprintList'])
        and instant(bound['run']['start'])<=instant(meta['CreateDate'])<=explicit_now
        and fixture_tags_checked(meta['Tags'],bound['run']['authorizationId'])
        and custody['provider']=={'operation':'CreateOpenIDConnectProvider','arn':PROVIDER,'metadataSHA256':digest(meta)},'whole-new-provider-custody')
    creation_custody_checked(custody['operations'],bound,inventory,explicit_now)
    require(instant(custody['operations'][-1]['receivedAt'])<=min(instant(owner['observedAt']),instant(inventory['observedAt'])),'post-write-whole-readback-order')
    admission_checker(bound['preparation'],explicit_now)
    return {'schema':'hikyo-158/next-fixture-sealed-model/v1','phase':'sealed-model-only',**COMMON_FLAGS,
        'boundPlan':copy.deepcopy(bound),'boundPlanSHA256':digest(bound),'records':copy.deepcopy(records),
        'reviewedRecordPins':copy.deepcopy(reviewed_pins),'derivedAt':stamp(explicit_now),
        'inventory':{'keyARN':bound['run']['keyARN'],'boundaryPolicyId':boundary['policyId'],
            'roleIds':{name:inventory['roles'][name]['RoleId'] for name in ROLES},'setupUserId':USER_ID,'environmentId':ENV_ID,'oidcProviderARN':PROVIDER},
        'reservationPlan':{'oldRetained':RETAINED_RESERVATIONS,'newMaximum':NEW_RESERVATIONS,'total':LIFETIME_RESERVATIONS,'refund':0},
        'environmentVariables':environment_variables(bound['run']),
        'requiredFinalSetupTransition':{'operation':'SetDefaultPolicyVersion','PolicyArn':SETUP,'VersionId':'v5',
            'preservedVersionIds':['v2','v3','v4','v5','v6'],'requiresIndependentExactRetirement':True}}


def expected_creation_operations(bound,inventory):
    """Closed owner activation/boundary + seventeen setup-write custody contract."""
    docs=bound['documents'];tags=[{'Key':'HikyoIssue','Value':'158'},{'Key':'Authorization','Value':bound['run']['authorizationId']}]
    boundary=inventory['boundary'];provider=inventory['provider']
    operations=[{'operation':'DeletePolicyVersion','request':{'PolicyArn':SETUP,'VersionId':'v1'},'response':{}},
        {'operation':'CreatePolicyVersion','request':{'PolicyArn':SETUP,'PolicyDocument':docs['setup-user.managed-policy.json'],
        'SetAsDefault':True},'response':{'VersionId':'v6','IsDefaultVersion':True}},
        {'operation':'CreatePolicy','request':{'PolicyName':'hikyo-158-role-boundary','Path':'/',
        'PolicyDocument':docs['setup-role-boundary.json']},'response':{'Arn':BOUNDARY,'PolicyId':boundary['policyId'],'DefaultVersionId':'v1'}},
        {'operation':'CreateOpenIDConnectProvider','request':{'Url':'https://token.actions.githubusercontent.com',
        'ClientIDList':['sts.amazonaws.com'],'ThumbprintList':provider['metadata']['ThumbprintList'],'Tags':tags},'response':{'OpenIDConnectProviderArn':PROVIDER,'Tags':copy.deepcopy(provider['metadata']['Tags'])}}]
    # The source role must exist before the assume role's trust is accepted.
    order=['hikyo-158-source',*[name for name in ROLES if name not in ('hikyo-158-source','hikyo-158-assume')],'hikyo-158-assume']
    for name in order:
        role=inventory['roles'][name]
        operations.append({'operation':'CreateRole','request':{'RoleName':name,'Path':'/',
            'MaxSessionDuration':role['MaxSessionDuration'],'PermissionsBoundary':BOUNDARY,'Tags':tags,
            'AssumeRolePolicyDocument':docs[name+'.trust.json']},'response':{'Arn':role['Arn'],'RoleId':role['RoleId'],'RoleName':name}})
        operations.append({'operation':'PutRolePolicy','request':{'RoleName':name,'PolicyName':'hikyo-158-fixture',
            'PolicyDocument':docs[name+'.policy.json']},'response':{}})
    return operations


def creation_custody_checked(operations,bound,inventory,now):
    expected=expected_creation_operations(bound,inventory)
    require(type(operations) is list and len(operations)==20,'twenty-exact-activation-creation-custodies')
    prior=instant(bound['run']['start']);claim_ids=[]
    for sequence,(row,wanted) in enumerate(zip(operations,expected),1):
        exact(row,{'sequence','boundPlanSHA256','claimId','maximumForwards','forwardsObserved','intentSHA256','operation','request','response','sentAt','receivedAt','requestId','actorARN'})
        require(type(row['sequence']) is int and row['sequence']==sequence and row['boundPlanSHA256']==digest(bound)
            and type(row['maximumForwards']) is int and row['maximumForwards']==1
            and type(row['forwardsObserved']) is int and row['forwardsObserved']==1
            and type(row['claimId']) is str and re.fullmatch(r'[a-zA-Z0-9-]{8,100}',row['claimId']) is not None
            and type(row['requestId']) is str and 8<=len(row['requestId'])<=128,'once-custody-metadata')
        require(row['actorARN']==(bound['preparation']['records']['owner']['ownerContext']['principalARN'] if sequence<=3 else USER_ARN),'exact-owner-versus-setup-actor')
        actual={k:row[k] for k in ('operation','request','response')}
        compared_actual=copy.deepcopy(actual);compared_wanted=copy.deepcopy(wanted)
        if row['operation']=='CreateOpenIDConnectProvider':
            exact(row['response'],{'OpenIDConnectProviderArn','Tags'})
            fixture_tags_checked(row['response']['Tags'],bound['run']['authorizationId'])
            compared_actual['response']['Tags']=sorted(compared_actual['response']['Tags'],key=lambda tag:tag['Key'])
            compared_wanted['response']['Tags']=sorted(compared_wanted['response']['Tags'],key=lambda tag:tag['Key'])
        require(same(compared_actual,compared_wanted),'exact-create-request-response')
        intent={'sequence':sequence,'boundPlanSHA256':digest(bound),'claimId':row['claimId'],
            'maximumForwards':1,'operation':row['operation'],'request':row['request'],'actorARN':row['actorARN']}
        require(row['intentSHA256']==digest(intent),'exact-create-intent-crosslink')
        sent,received=instant(row['sentAt']),instant(row['receivedAt'])
        require(prior<=sent<=received<=now<instant(bound['run']['provisioningCutoff']),'creation-custody-chronology')
        prior=received;claim_ids.append(row['claimId'])
    require(len(set(claim_ids))==20,'distinct-once-claims')


def seal_actual_inventory(bound,records,reviewed_pins,*,explicit_now):
    """Create a seal only under the currently admitted reserved release."""
    return _seal_inventory_with_admission(bound,records,reviewed_pins,
        explicit_now=explicit_now,admission_checker=admit_current_release)


def _reconstruct_original_seal(prepared,action_at):
    return ci_original_for_cleanup_checked(prepared['records']['code'],
        prepared['proposal']['sourceManifest'],action_at,action_at)


def sealed_checked(sealed):
    expected=_seal_inventory_with_admission(sealed['boundPlan'],sealed['records'],sealed['reviewedRecordPins'],
        explicit_now=instant(sealed['derivedAt']),admission_checker=_reconstruct_original_seal)
    require(same(expected,sealed),'whole-sealed-reconstruction')


def restored_setup_checked(sealed,record,reviewed_pin,*,explicit_now):
    """Check final owner policy state only; never attests retirement or authorizes restore."""
    sealed_checked(sealed);reviewed_records({'owner':record},{'owner':reviewed_pin},{'owner'})
    exact(record,{'schema','observedAt','setupPolicy','setupUserId','MFADevices','attachedPolicyARNs','inlinePolicyNames','groups','accessKeys'})
    require(record['schema']=='hikyo-158/next-restored-owner/v1','restored-owner-schema');fresh(record['observedAt'],explicit_now)
    require(instant(sealed['derivedAt'])<=instant(record['observedAt']),'restoration-observed-after-seal')
    require(record['setupUserId']==USER_ID and record['MFADevices']==[{'UserName':USER,'SerialNumber':MFA}]
        and record['attachedPolicyARNs']==[SETUP] and record['inlinePolicyNames']==record['groups']==record['accessKeys']==[],'restored-identity')
    proposal=sealed['boundPlan']['preparation']['proposal']
    setup_policy_checked(record['setupPolicy'],proposal['inputs']['iamClosure']['setupPolicy'],['v2','v3','v4','v5','v6'],'v5',sealed['boundPlan']['documents'])
    return {'schema':'hikyo-158/next-restored-policy-model/v1',**COMMON_FLAGS,'sealedModelSHA256':digest(sealed),
        'reviewedOwnerRecordSHA256':reviewed_pin,'policyState':'preserved-v2-v6-default-v5',
        'retirementVerifiedByModel':False,'observedAt':record['observedAt']}


def environment_variables(run):
    values={'AWS_ACCEPTANCE_APPROVED_RUN_ID':run['runId'],'AWS_ACCEPTANCE_APPROVED_WORKFLOW_SHA':run['workflowSHA'],
        'AWS_ACCEPTANCE_APPROVED_SHA':run['candidateSHA'],'AWS_ACCEPTANCE_AUTHORIZATION_ID':run['authorizationId'],
        'AWS_ACCEPTANCE_RUN_PREFIX':run['prefix'],'AWS_ACCEPTANCE_HISTORICAL_CLEANUP_ARN':'',
        'AWS_ACCEPTANCE_AUTH_EXPIRES_AT':run['authUntil'],'AWS_ACCEPTANCE_CLEANUP_EXPIRES_AT':run['cleanupUntil'],
        'AWS_ACCEPTANCE_KMS_KEY_ARN':run['keyARN'],'AWS_ACCEPTANCE_EXTERNAL_ID':run['externalId']}
    return [{'name':name,'value':value} for name,value in sorted(values.items())]


# Successor release data is configured explicitly, after independent source and
# original-custody review. These globals never contain default authority.
RELEASE_ADMISSION = None
RELEASE_EVIDENCE = None
ATTEMPT_TRUST_BINDING = None
PAIR_COLLECTIONS = {}


def release_configuration_checked():
    require(RELEASE_ADMISSION is not None,'release-generation-unconfigured')
    composite = RELEASE_ADMISSION.configuration_checked()
    require(same(composite['resourceConfiguration'],configuration_checked()),
        'unchanged-eight-field-resource-configuration')
    return composite


def source_manifest_checked(manifest):
    release_configuration_checked()
    RELEASE_ADMISSION.source_manifest_checked(manifest)


def attempt_binding_checked(manifest):
    source_manifest_checked(manifest)
    require(type(ATTEMPT_TRUST_BINDING) is dict,'attempt-binding-unconfigured')
    RELEASE_ADMISSION.attempt_binding_checked(ATTEMPT_TRUST_BINDING,manifest)
    return copy.deepcopy(ATTEMPT_TRUST_BINDING)


def configure_release(admission,release_evidence,binding,manifest,historical_bootstrap):
    global RELEASE_ADMISSION,RELEASE_EVIDENCE,ATTEMPT_TRUST_BINDING
    import release_collector
    require(RELEASE_ADMISSION is None and ATTEMPT_TRUST_BINDING is None,
        'one-immutable-release-configuration-per-model-instance')
    require(same(admission.resource_configuration,configuration_checked()),'same-resource-configuration')
    admission.source_manifest_checked(manifest);admission.attempt_binding_checked(binding,manifest)
    release_code=release_collector.release_checked(sys.modules[__name__],release_evidence)
    require({path:hashlib.sha256(body.encode()).hexdigest() for path,body in release_code['releaseSource']['files'].items()}
        ==admission.release_vector,'release-collection-matches-approved-vector')
    exact(historical_bootstrap,{'bootstrapPR','mergeDecision','observedAt'})
    affirmative_merge_checked(historical_bootstrap['mergeDecision'],historical_bootstrap['bootstrapPR'],
        historical_bootstrap['observedAt'])
    RELEASE_ADMISSION=admission;RELEASE_EVIDENCE=copy.deepcopy(release_evidence)
    ATTEMPT_TRUST_BINDING=copy.deepcopy(binding)


def register_pair_collection(collection,manifest,verifier):
    import release_collector
    release_configuration_checked()
    release_collector.require_external_originals(collection,verifier)
    code=collection['code']
    binding=attempt_binding_checked(manifest)
    heads=release_collector.pair_checked(sys.modules[__name__],collection,manifest,RELEASE_EVIDENCE,
        instant(code['observedAt']))
    require(heads=={'candidate':binding['candidateHeadSHA'],'workflow':binding['mainSHA']},
        'collection-matches-immutable-attempt-pair')
    proof=_registered_pair_code(collection)
    key=digest(proof)
    require(key not in PAIR_COLLECTIONS or release_collector.collection_digest(sys.modules[__name__],PAIR_COLLECTIONS[key])
        ==release_collector.collection_digest(sys.modules[__name__],collection),
        'immutable-whole-pair-capture')
    PAIR_COLLECTIONS[key]=copy.deepcopy(collection)
    return proof


def _registered_pair_code(collection):
    # Whole tested source/range/job/episode bytes stay in the authenticated
    # registry. The nested preparation retains the actual bodies its readers
    # need once, never repeated tested/main episode copies or raw captures.
    import release_collector
    code=collection['code']
    fields=('repositoryId','ownerId','candidateCommit','workflowCommit',
        'candidateFiles','workflowFiles','absentFiles','workflowMainRef','observedAt')
    return {'schema':'hikyo-158/registered-pair-code/v1',
        'pairCollectionSHA256':release_collector.collection_digest(sys.modules[__name__],collection),
        **{name:copy.deepcopy(code[name]) for name in fields}}


def _pair_integrity_checked(proof,manifest,at):
    import release_collector
    require(digest(proof) in PAIR_COLLECTIONS,'whole-pair-originals-unavailable')
    collection=PAIR_COLLECTIONS[digest(proof)]
    require(same(_registered_pair_code(collection),proof),'exact-semantic-original-correspondence')
    binding=attempt_binding_checked(manifest)
    heads=release_collector.pair_checked(sys.modules[__name__],collection,manifest,RELEASE_EVIDENCE,at)
    require(heads=={'candidate':binding['candidateHeadSHA'],'workflow':binding['mainSHA']}
        and collection['selection']['candidateRunId']==binding['candidateRun']['id']
        and collection['selection']['candidateAttempt']==binding['candidateRun']['attempt']
        and collection['selection']['mainRunId']==binding['currentMainRun']['id']
        and collection['selection']['mainAttempt']==binding['currentMainRun']['attempt'],
        'exact-immutable-pair-and-run-binding')
    return heads


def ci_checked(proof,manifest,now):
    heads=_pair_integrity_checked(proof,manifest,now)
    RELEASE_ADMISSION.admit_now(attempt_binding_checked(manifest),manifest,now)
    return heads


def ci_original_for_cleanup_checked(proof,manifest,action_at,now):
    heads=_pair_integrity_checked(proof,manifest,action_at)
    RELEASE_ADMISSION.reconstruct_original_for_cleanup(attempt_binding_checked(manifest),manifest,action_at,now)
    return heads


def prepare(proposal,records,reviewed_pins,*,lease_start,lease_until,namespace,explicit_now):
    return _prepare_with_checker(proposal,records,reviewed_pins,lease_start=lease_start,
        lease_until=lease_until,namespace=namespace,explicit_now=explicit_now,ci_checker=ci_checked)


def bind_pending_run(prepared,records,reviewed_pins,*,reviewed_start,explicit_now):
    return _bind_with_checker(prepared,records,reviewed_pins,reviewed_start=reviewed_start,
        explicit_now=explicit_now,ci_checker=ci_checked)


def _ci_reconstruct_original(proof,manifest,action_at):
    return ci_original_for_cleanup_checked(proof,manifest,action_at,action_at)


def admit_current_release(prepared,now):
    manifest=prepared['proposal']['sourceManifest']
    binding=attempt_binding_checked(manifest)
    require(prepared['attemptTrustBindingSHA256']==digest(binding),'same-reserved-trust-binding')
    return RELEASE_ADMISSION.admit_now(binding,manifest,now,reserved_binding=binding)
