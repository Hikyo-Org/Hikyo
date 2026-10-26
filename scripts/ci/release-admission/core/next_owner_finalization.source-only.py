"""SOURCE ONLY. Exact final owner cleanup custody; import has no I/O.

No transport, credentials, clock defaults, resource creation or retry. Native
DeletePolicy and existing-v5 restoration remain separately authenticated guards.
"""
from abc import ABC, abstractmethod
import copy
from datetime import timedelta
import math

LEDGER_SHA256 = 'c93ebd62f4304b880d29eb24812ccbf4c567d803970db1b11f27ff834f55a297'
MODEL_SHA256 = 'f40c07b2204c3305b434836015c99f30af4dfc893a0ce0e4f0318c17ea844a19'
EXECUTOR_SHA256 = '57c31fce3d373d770930e5e01f4b1a0fb27c4128983c78eb18b0c08a4f27a85c'
RESULT_SHA256 = '1b827401e65985ae7480ada9e25b503680490faa6f3cfb5cd9ae610cf25998c8'
HISTORICAL_NATIVE_GUARD_PINS={'next_fixture_iam_operations.source-only.mjs':'416f56cac09284ded03b44be08c100165f8e799257bea987065a5cf16e392613',
    'native_next_fixture_iam_guard.source-only.js':'9fb4c016531ab676f5e8f79f6c8c795bbc8a698a3cb831ecd65d28681ee281fc',
    'setup_version_operations.source-only.mjs':'7c04cb1c5e4e1d9e4f423f49b08ec1aaef62a4a36e0e262d1b90ea55e9bab454',
    'native_setup_version_operations_guard.source-only.js':'667bd009a82cb13fe293537665c18f91fd74ab8dae2672a3d043f54874c49d45'}
NATIVE_GUARD_PINS = {'next_fixture_iam_operations.source-only.mjs': '3ce2f92883d62c4cc9ba46b0d56def2a7a67fd5a063c514ecf58639b5bfcfa0e', 'native_next_fixture_iam_guard.source-only.js': '2f79843da8df75977874d1bfdd416f5b3124e875b041c1634ffcb489adae781f', 'setup_version_operations.source-only.mjs': 'dd64d90d4ba15066fe0716092fe692f3df9dad3838af247f54c73617f33e1ec8', 'native_setup_version_operations_guard.source-only.js': '577ed656f42e56072decf6dc58a0c5c35f4b3dff2062a930e022c1a35ec8944f'}
EXECUTOR_TEST_SHA256 = '68e0d463956b1151a1c7687336c0d3c72ef4d1892e94287954f93d7be927be8b'
OPERATIONS=('DeletePolicy','SetDefaultPolicyVersion')


class RootFinalizationAdapter(ABC):
    """No live implementation. Authenticate whole original public evidence.

    Required external gates: mounted source/guard bytes, original reservation and
    compatible lock, native MFA-backed owner session and immutable principal,
    complete hosted result provenance/charged outcomes/runner termination,
    original 18-slot actual retirement custody, complete native readbacks and
    exact new boundary identity, once-ticket/native-forward adjacency and actual
    response provenance. Caller hashes, flags and normalized observations never
    authenticate themselves. Do not read or expose private credentials/values.
    """
    @abstractmethod
    def authenticate(self,bundle,context,explicit_now):
        raise NotImplementedError('No live final owner proof adapter supplied')


def _budget(m,proof,now):
    result={'status':'unavailable','grossUSD':None,'targetExceeded':None,'absoluteExceeded':None,'newSpendingAuthorized':False}
    if proof is None:return result
    try:
        m.require(proof['currency']=='USD' and proof['targetUSD']=='5.00' and proof['absoluteMaximumUSD']=='10.00'
            and type(proof['grossCharges']) is list and 1<=len(proof['grossCharges'])<=32,'cleanup-cost-shape')
        gross=m.money('0.00');times=[proof['observedAt']]
        for row in proof['grossCharges']:
            source=row['sourceRecord'];m.require(source['account']==m.ACCOUNT and source['currency']=='USD'
                and source['source']=='native-billing-console' and source['grossDisplayedUSD']==row['amountUSD'],'cleanup-cost-source')
            gross+=m.money(row['amountUSD']);times.append(source['observedAt'])
        billable,_=m.banked_billing_checked(proof['bankedBilling'],gross,now,fresh_required=False)
        times.append(proof['bankedBilling']['observedAt'])
        m.require(all(m.instant(at)<=now for at in times),'no-future-cost')
        result.update(status='fresh' if all(now-m.instant(at)<=timedelta(minutes=5) for at in times) else 'stale',
            grossUSD=format(gross,'.2f'),targetExceeded=gross>m.money('5.00'),
            absoluteExceeded=gross>m.money('10.00') or billable>=m.money('15.00'))
    except (ValueError,TypeError,KeyError):pass
    return result


class OwnerFinalization:
    def __init__(self,model,executor,ledger,bound,sealed,sources,adapter,explicit_clock):
        self.m,self.x,self.ledger=model,executor,ledger
        self.bound,self.sealed,self.sources=copy.deepcopy(bound),copy.deepcopy(sealed),copy.deepcopy(sources)
        self.adapter,self.now=adapter,explicit_clock;m=model
        m.require(isinstance(adapter,RootFinalizationAdapter) and callable(explicit_clock),'explicit-owner-adapter-and-clock')
        m.sealed_checked(sealed);m.require(m.same(sealed['boundPlan'],bound),'finalization-whole-bound-sealed')
        executor.source_manifest_checked(m,sources['execution'])
        m.require(sources['execution']['executorSHA256']==EXECUTOR_SHA256 and sources['execution']['executorTestSHA256']==EXECUTOR_TEST_SHA256
            and sources['execution']['ledgerSHA256']==LEDGER_SHA256,'same-reviewed-original-execution-sources')
        m.exact(sources,{'execution','modelSHA256','executorSHA256','resultSHA256','finalizationSHA256','nativeGuardSHA256s'})
        m.require(sources['modelSHA256']==MODEL_SHA256 and sources['executorSHA256']==EXECUTOR_SHA256 and sources['resultSHA256']==RESULT_SHA256
            and m.sha(sources['finalizationSHA256']),'finalization-reviewed-source-pins')
        m.exact(sources['nativeGuardSHA256s'],{'next_fixture_iam_operations.source-only.mjs','native_next_fixture_iam_guard.source-only.js',
            'setup_version_operations.source-only.mjs','native_setup_version_operations_guard.source-only.js'})
        m.require(m.same(sources['nativeGuardSHA256s'],NATIVE_GUARD_PINS)
            and all(HISTORICAL_NATIVE_GUARD_PINS[name]==m.FROZEN[name] for name in m.FROZEN if name in HISTORICAL_NATIVE_GUARD_PINS),'frozen-native-cleanup-guards')

    def _state(self):
        m=self.m;head,state=self.ledger.snapshot();claim=self.ledger.claim_record()
        m.require(claim['schema']=='hikyo-158/next-exclusive-reservation/v1' and claim['approvalSHA256']==m.SOURCE_PINS['approval']
            and claim['attemptTrustBindingSHA256']==self.bound['attemptTrustBindingSHA256']
            and claim['heldHistorySHA256']==m.SOURCE_PINS['heldHistory'] and claim['completedHistorySHA256']==m.SOURCE_PINS['completedAttempt'] and claim['configurationSHA256']==m.digest(m.release_configuration_checked()) and claim['heldHistorySHA256']==m.digest(self.bound['preparation']['proposal']['inputs']['heldHistory'])
            and claim['preparationSHA256']==m.digest(self.bound['preparation']) and m.same(claim['sourcePins'],self.sources['execution'])
            and m.same(claim['historicalPins'],m.SOURCE_PINS) and claim['namespace']==self.bound['run']['prefix']
            and type(claim['retainedReservations']) is int and claim['retainedReservations']==176
            and type(claim['newReservationsCharged']) is int and claim['newReservationsCharged']==32
            and type(claim['lifetimeReservations']) is int and claim['lifetimeReservations']==208
            and m.same(claim['usedNamespaces'],self.bound['preparation']['proposal']['usedNamespaces'])
            and m.instant(claim['reservedAt'])<=m.instant(self.bound['records']['key']['sentAt'])
            and claim['automaticRetry'] is False and type(claim['maximumDispatches']) is int and claim['maximumDispatches']==1,'original-once-reservation')
        m.require(state is not None and state['attemptTrustBindingSHA256']==self.bound['attemptTrustBindingSHA256'] and state['boundSHA256']==m.digest(self.bound) and state['sealedSHA256']==m.digest(self.sealed)
            and state['keyARN']==self.bound['run']['keyARN'] and state['boundaryPolicyId']==self.sealed['inventory']['boundaryPolicyId'],'same-new-fixture-custody')
        for file in ('held.once.json','owner-finalization-held.once.json'):
            try:self.ledger.read_once(file);raise ValueError('next-owner:held-no-replay')
            except FileNotFoundError:pass
        m.require(state['held'] is False,'held-finalization-refuses')
        return head,state

    def _proof(self,kind,bundle,head):
        m=self.m;now=self.now();m.clock(now)
        m.exact(bundle,{'schema','kind','observedAt','records','rawEvidence'})
        m.require(bundle['schema']=='hikyo-158/next-owner-finalization-proof/v1' and bundle['kind']==kind
            and type(bundle['rawEvidence']) is list and 1<=len(bundle['rawEvidence'])<=128,'complete-original-finalization-proof')
        m.fresh(bundle['observedAt'],now)
        for row in bundle['rawEvidence']:
            m.exact(row,{'source','operation','selector','response','responseSHA256','observedAt'})
            m.require(row['source'] in ('native-owner-console','official-cli','github-api','public-source')
                and type(row['operation']) is str and type(row['selector']) is dict and type(row['response']) in (dict,list)
                and row['responseSHA256']==m.digest(row['response']),'original-public-finalization-custody')
            if row['operation']=='GrossChargesReadback':m.require(m.instant(row['observedAt'])<=now,'no-future-billing')
            else:m.fresh(row['observedAt'],now)
        context={'attemptTrustBindingSHA256':self.bound['attemptTrustBindingSHA256'],'phase':kind,'account':m.ACCOUNT,'region':m.REGION,'approvalSHA256':m.SOURCE_PINS['approval'],
            'boundPlanSHA256':m.digest(self.bound),'sealedModelSHA256':m.digest(self.sealed),'stateSHA256':head,
            'sourcePins':self.sources,'storagePaths':{'ledger':str(self.ledger.folder),'parent':str(self.ledger.parent),'lock':str(self.ledger.parent/'.provisioning.lock')}}
        verified=self.adapter.authenticate(copy.deepcopy(bundle),copy.deepcopy(context),now)
        m.require(m.same(verified,bundle),'whole-authenticated-original-finalization-proof')
        return copy.deepcopy(bundle['records'])

    def _retirement_custody(self,state):
        m=self.m;rows=[r for r in state['completed'] if r['phase']=='retire'];completed={(r['sequence'],r['operation'],r['role']):r for r in rows}
        m.require(len(rows)==len(completed) and len(completed)<=18,'closed-retirement-slot-inventory');last=m.instant(self.bound['run']['start'])
        for sequence,(operation,role) in enumerate(self.x.retirement_schedule(m),1):
            key=(sequence,operation,role)
            if key not in completed:
                absent=self.ledger.read_once(f'retire-{sequence:02d}-absent.once.json')
                m.require(absent['boundSHA256']==m.digest(self.bound) and absent['operation']==operation and absent['role']==role
                    and m.sha(absent['proofSHA256']),'exact-retirement-absence-slot')
                m.require(operation!='ScheduleKeyDeletion','new-key-must-have-actual-seven-day-schedule');continue
            intent=self.ledger.read_once(f'retire-{sequence:02d}-intent.once.json');ticket=self.ledger.read_once(f'retire-{sequence:02d}-transmit.once.json')
            response=self.ledger.read_once(f'retire-{sequence:02d}-response.once.json');out=response['outcome']
            wire=self.ledger.read_once(f'retire-{sequence:02d}-wire-outcome.once.json')
            m.require(intent['phase']=='retire' and type(intent['sequence']) is int and intent['sequence']==sequence and intent['operation']==operation
                and intent['role']==role and intent['boundSHA256']==m.digest(self.bound) and type(intent['maximumRequests']) is int and intent['maximumRequests']==1
                and ticket['intentSHA256']==m.digest(intent) and type(ticket['maximumRequests']) is int and ticket['maximumRequests']==1
                and response['intentSHA256']==m.digest(intent) and completed[key]['responseSHA256']==m.digest(out)
                and wire['intentSHA256']==m.digest(intent) and wire['responseAmbiguous'] is False and type(wire['returncode']) is int and wire['returncode']==0
                and m.same(wire['parsedPublicResponse'],out['value']) and wire['sentAt']==out['sentAt'] and wire['receivedAt']==out['receivedAt']
                and wire['stdoutSHA256']==out['stdoutSHA256'] and wire['stderrSHA256']==out['stderrSHA256'],'actual-18-slot-retirement-custody')
            m.require(type(out['returncode']) is int and out['returncode']==0 and out['stderrEmpty'] is True
                and last<=m.instant(out['sentAt'])<=m.instant(out['receivedAt'])<=self.now(),'ordered-actual-retirement-responses')
            last=m.instant(out['receivedAt'])
            if operation=='ScheduleKeyDeletion':
                value=out['value'];m.require(value['KeyId'] in (self.bound['run']['keyARN'],self.bound['run']['keyARN'].rsplit('/',1)[1])
                    and value['KeyState']=='PendingDeletion' and type(value['PendingWindowInDays']) is int and value['PendingWindowInDays']==7
                    and type(value['DeletionDate']) in (int,float) and math.isfinite(value['DeletionDate']),'actual-new-key-schedule-seven')
                return value,last
            m.require(out['value']=={},'empty-exact-retirement-response')
        raise ValueError('next-owner:missing-new-key-schedule')

    def _gate(self,records,state,*,after_boundary=False,after_restore=False):
        m=self.m;now=self.now();run=self.bound['run'];prepared=self.bound['preparation']
        m.exact(records,{'hostedResult','retirement','ownerContext','ownerMFA','setupUser','setupPolicy','boundary','budget','code','historicalAbsence'})
        m.require(m.instant(run['start'])<=now<m.instant(run['cleanupUntil'])<=m.instant(prepared['lease']['until'])<=m.instant(m.HORIZON),'finite-approved-final-cleanup-window')
        m.require(self.x.cleanup_code_checked(m,self.bound,records['code'],now)=={'candidate':run['candidateSHA'],'workflow':run['workflowSHA']},'finalization-fresh-source-heads')
        context=records['ownerContext'];m.exact(context,{'account','principalARN','source','observedAt','sourceReceipt'})
        m.fresh(context['observedAt'],now);m.require(context['account']==m.ACCOUNT and context['principalARN']==self.bound['records']['owner']['ownerContext']['principalARN']
            and context['source']=='native-owner-console','same-exact-owner-principal')
        source=context['sourceReceipt'];m.exact(source,{'schema','capturedAt','pageURL','accountHeaderText','principalARN'});m.fresh(source['capturedAt'],now)
        m.require(source['schema']=='hikyo-158/native-owner-context-projection/v1' and source['principalARN']==context['principalARN'] and source['accountHeaderText']==m.ACCOUNT
            and source['pageURL']=='https://us-east-1.console.aws.amazon.com/iam/home?region=eu-central-1#/home','actual-owner-console-custody')
        auth=records['ownerMFA'];m.exact(auth,{'schema','account','principalARN','observedAt','methods','originalObservation'})
        m.fresh(auth['observedAt'],now);m.require(auth['schema']=='hikyo-158/owner-mfa-custody/v1' and auth['account']==m.ACCOUNT
            and auth['principalARN']==context['principalARN'] and auth['methods']==['password','mfa'] and type(auth['originalObservation']) is dict and bool(auth['originalObservation']),'external-current-owner-mfa-custody')
        m.require(m.same(records['setupUser'],prepared['records']['owner']['setupUser']),'retain-sole-setup-user-mfa')
        m.setup_policy_checked(records['setupPolicy'],prepared['proposal']['inputs']['iamClosure']['setupPolicy'],['v2','v3','v4','v5','v6'],
            'v5' if after_restore else 'v6',self.bound['documents'])
        hosted=records['hostedResult'];m.exact(hosted,{'validationReceipt','secretAbsenceProof'})
        receipt=hosted['validationReceipt'];m.require(receipt['schema'] in ('hikyo-158/next-hosted-result-validation/v1','hikyo-158/next-never-started-cleanup-validation/v1')
            and receipt['attemptTrustBindingSHA256']==self.bound['attemptTrustBindingSHA256']
            and receipt['approvalSHA256']==m.SOURCE_PINS['approval'] and receipt['heldHistorySHA256']==m.SOURCE_PINS['heldHistory']
            and receipt['completedHistorySHA256']==m.SOURCE_PINS['completedAttempt'] and receipt['configurationSHA256']==m.digest(m.release_configuration_checked())
            and receipt['boundPlanSHA256']==m.digest(self.bound) and receipt['sealedModelSHA256']==m.digest(self.sealed)
            and receipt['noActiveFixtureWork'] is True and receipt['allCreateOutcomesReconciled'] is True and receipt['executionAuthorityGranted'] is False
            and type(receipt['newReservationRetained']) is int and receipt['newReservationRetained']==32 and type(receipt['oldReservationsRetained']) is int and receipt['oldReservationsRetained']==176
            and type(receipt['remainingDispatches']) is int and receipt['remainingDispatches']==0 and type(receipt['acceptancePassed']) is bool
            and type(receipt['lifetimeReservationCeiling']) is int
            and receipt['lifetimeReservationCeiling']==208 and type(receipt['refund']) is int and receipt['refund']==0,'strict-authenticated-hosted-result')
        # The frozen checker validates the exact output schema; the adapter above
        # must authenticate the original reviewed result bridge and its evidence.
        checker=object.__new__(self.x.FixtureController);checker.m=m;checker.bound=self.bound;checker.prepared=prepared;checker.now=self.now
        checker._secret_absence(hosted['secretAbsenceProof'])
        m.historical_absence_checked(records['historicalAbsence'],now,m.digest(context))
        schedule,last=self._retirement_custody(state)
        retirement=records['retirement'];m.exact(retirement,{'schema','observedAt','roles','provider','newKey','oldKeys','allRoles','allProviders'})
        m.fresh(retirement['observedAt'],now);m.require(retirement['schema']=='hikyo-158/final-retirement-readback/v1'
            and last<=m.instant(retirement['observedAt']),'fresh-whole-after-retirement-readback')
        m.exact(retirement['roles'],m.ROLES)
        for role,row in retirement['roles'].items():
            m.require(m.same(row,{'roleId':self.sealed['inventory']['roleIds'][role],
                'role':{'operation':'GetRole','RoleName':role,'errorCode':'NoSuchEntity'},
                'inline':{'operation':'GetRolePolicy','RoleName':role,'PolicyName':'hikyo-158-fixture','errorCode':'NoSuchEntity'}}),'all-eight-new-roles-and-inline-absent')
        m.require(m.same(retirement['provider'],{'operation':'GetOpenIDConnectProvider','arn':m.PROVIDER,'errorCode':'NoSuchEntity'})
            and retirement['allProviders']=={'OpenIDConnectProviderList':[]},'fresh-provider-absence')
        m.exact(retirement['allRoles'],{'Roles','IsTruncated'});m.require(retirement['allRoles']['IsTruncated'] is False
            and type(retirement['allRoles']['Roles']) is list and all(type(r) is dict and type(r.get('RoleName')) is str
                and r['RoleName'].casefold() not in {n.casefold() for n in m.ROLES} for r in retirement['allRoles']['Roles']),'complete-no-fixture-role-inventory')
        metadata=retirement['newKey'];expected=copy.deepcopy(self.bound['records']['key']['readback']['metadata']);expected.update(KeyState='PendingDeletion',Enabled=False,DeletionDate=schedule['DeletionDate'])
        m.require(m.same(metadata,expected) and m.same(retirement['oldKeys'],prepared['records']['owner']['oldKeys']),'actual-new-key-pending-and-old-four-preserved')
        boundary=records['boundary'];m.exact(boundary,{'observedAt','value','attachments'});m.fresh(boundary['observedAt'],now)
        if after_boundary:
            deletion=self.ledger.read_once('final-owner-DeletePolicy-response.once.json')
            m.require(m.instant(deletion['receivedAt'])<=m.instant(boundary['observedAt']),'boundary-absence-after-actual-delete')
            m.require(m.same(boundary['value'],{'operation':'GetPolicy','arn':m.BOUNDARY,'errorCode':'NoSuchEntity'}) and boundary['attachments'] is None,'fresh-exact-boundary-no-such-entity')
        else:
            expected={'arn':m.BOUNDARY,'policyId':self.sealed['inventory']['boundaryPolicyId'],'defaultVersionId':'v1',
                'versions':{'v1':{'document':self.bound['documents']['setup-role-boundary.json'],'isDefault':True}}}
            m.require(m.same(boundary['value'],expected) and m.same(boundary['attachments'],{'users':[],'groups':[],'roles':[],
                'attachmentCount':0,'permissionsBoundaryUsageCount':0}),'only-new-boundary-whole-v1-zero-use')
        return _budget(m,records['budget'],now)

    def arm(self,operation,proof):
        m=self.m;m.require(operation in OPERATIONS,'closed-final-owner-operation')
        with self.ledger.lock():
            head,state=self._state();expected='retirement-writes-complete-await-independent-readback' if operation=='DeletePolicy' else 'owner-boundary-delete-captured'
            m.require(state['phase']==expected and state['pending'] is None,'ordered-final-owner-stage')
            records=self._proof('arm-'+operation,proof,head);budget=self._gate(records,state,after_boundary=operation=='SetDefaultPolicyVersion')
            request={'PolicyArn':m.BOUNDARY} if operation=='DeletePolicy' else {'PolicyArn':m.SETUP,'VersionId':'v5'}
            intent={'attemptTrustBindingSHA256':self.bound['attemptTrustBindingSHA256'],'schema':'hikyo-158/final-owner-intent/v1','operation':operation,'request':request,'maximumForwards':1,
                'claimId':'next-final-owner-'+operation,'boundPlanSHA256':m.digest(self.bound),'sealedModelSHA256':m.digest(self.sealed),
                'originalClaimSHA256':state['claimSHA256'],'stateSHA256':head,'armedAt':m.stamp(self.now()),'proofSHA256':m.digest(proof),
                'boundaryPolicyId':state['boundaryPolicyId'],'budgetDiagnostic':budget,
                'requiredNativePreparation':{'attemptTrustBindingSHA256':self.bound['attemptTrustBindingSHA256'],'operation':operation,'targetARN':request['PolicyArn'],
                    'expectedPolicyId':state['boundaryPolicyId'] if operation=='DeletePolicy' else m.SETUP_ID,
                    'currentDefaultVersionId':'v1' if operation=='DeletePolicy' else 'v6',
                    'targetVersionId':None if operation=='DeletePolicy' else 'v5',
                    'reviewedDocumentSHA256':m.digest(self.bound['documents']['setup-role-boundary.json' if operation=='DeletePolicy' else 'setup-user.managed-policy.json']),
                    'retirementProofSHA256':m.digest(records['retirement']),
                    'newBoundaryCustodySHA256':m.digest(self.sealed['records']['custody']['boundary']),
                    'ownerContextSHA256':m.digest(records['ownerContext'])}}
            self.ledger.once('final-owner-'+operation+'-intent.once.json',intent);state['pending']=intent
            self.ledger.cas(head,'final-owner-intent',state);return copy.deepcopy(intent)

    def forward_ticket(self,operation,proof):
        m=self.m;m.require(operation in OPERATIONS,'closed-final-owner-operation')
        with self.ledger.lock():
            head,state=self._state();intent=self.ledger.read_once('final-owner-'+operation+'-intent.once.json')
            m.require(m.same(state['pending'],intent) and intent['attemptTrustBindingSHA256']==state['attemptTrustBindingSHA256'],'same-final-owner-pending-intent')
            records=self._proof('ticket-'+operation,proof,head);self._gate(records,state,after_boundary=operation=='SetDefaultPolicyVersion')
            m.fresh(intent['armedAt'],self.now())
            ticket={'attemptTrustBindingSHA256':self.bound['attemptTrustBindingSHA256'],'schema':'hikyo-158/final-owner-forward-ticket/v1','operation':operation,'intentSHA256':m.digest(intent),
                'request':intent['request'],'expectedBoundaryPolicyId':intent['boundaryPolicyId'],'maximumForwards':1,
                'issuedAt':m.stamp(self.now()),'proofSHA256':m.digest(proof),'requiredNativePreparation':intent['requiredNativePreparation'],'nativeGuardSourcePins':self.sources['nativeGuardSHA256s']}
            self.ledger.once('final-owner-'+operation+'-ticket.once.json',ticket);return ticket

    def accept_receipt(self,operation,proof):
        m=self.m;m.require(operation in OPERATIONS,'closed-final-owner-operation')
        with self.ledger.lock():
            head,state=self._state();intent=self.ledger.read_once('final-owner-'+operation+'-intent.once.json');ticket=self.ledger.read_once('final-owner-'+operation+'-ticket.once.json')
            m.require(m.same(state['pending'],intent) and ticket['intentSHA256']==m.digest(intent)
                and ticket['attemptTrustBindingSHA256']==intent['attemptTrustBindingSHA256']==state['attemptTrustBindingSHA256'],'same-final-owner-ticket')
            records=self._proof('receipt-'+operation,proof,head)
            # Capture whole public receipt before validation/CAS; ambiguity and
            # failed persistence must never authorize forwarding this intent again.
            self.ledger.once('final-owner-'+operation+'-response.once.json',records)
            try:
                m.exact(records,{'schema','operation','intentSHA256','ticketSHA256','request','sentAt','receivedAt','forwardsObserved','httpStatus','requestId','response','sourceReceipt'})
                m.require(records['schema']=='hikyo-158/final-owner-response/v1' and records['operation']==operation and records['intentSHA256']==m.digest(intent)
                    and records['ticketSHA256']==m.digest(ticket) and m.same(records['request'],intent['request']) and type(records['forwardsObserved']) is int
                    and records['forwardsObserved']==1 and type(records['httpStatus']) is int and records['httpStatus']==200 and records['response']=={}
                    and type(records['requestId']) is str and 8<=len(records['requestId'])<=128 and type(records['sourceReceipt']) is dict
                    and records['sourceReceipt'].get('requiredPreparationSHA256')==m.digest(intent['requiredNativePreparation'])
                    and m.sha(records['sourceReceipt'].get('nativePreparationSHA256'))
                    and records['sourceReceipt'].get('nativeGuardSourceSHA256')==self.sources['nativeGuardSHA256s']['native_next_fixture_iam_guard.source-only.js' if operation=='DeletePolicy' else 'native_setup_version_operations_guard.source-only.js']
                    and m.instant(intent['armedAt'])<=m.instant(ticket['issuedAt'])<=m.instant(records['sentAt'])<=m.instant(records['receivedAt'])<=self.now()
                    and m.instant(records['sentAt'])-m.instant(ticket['issuedAt'])<=timedelta(seconds=30)
                    and m.instant(records['receivedAt'])<m.instant(self.bound['run']['cleanupUntil']),'actual-single-exact-final-owner-response')
                state['pending']=None;state['phase']='owner-boundary-delete-captured' if operation=='DeletePolicy' else 'owner-v5-restore-captured'
                state.setdefault('finalOwner',{})[operation]=m.digest(records);self.ledger.cas(head,'final-owner-custody',state)
                return copy.deepcopy(records)
            except Exception:
                self.ledger.once('owner-finalization-held.once.json',{'reason':'ambiguous-or-uncommitted-final-owner-response','operation':operation,'intentSHA256':m.digest(intent)})
                raise RuntimeError('Final owner cleanup held; preserve response and reconcile, no replay') from None

    def complete(self,proof):
        m=self.m
        with self.ledger.lock():
            head,state=self._state();m.require(state['phase']=='owner-v5-restore-captured' and state['pending'] is None,'both-final-owner-mutations-captured')
            records=self._proof('complete',proof,head)
            m.exact(records,{'readback','restoredOwner'});self._gate(records['readback'],state,after_boundary=True,after_restore=True)
            restored=records['restoredOwner'];model=m.restored_setup_checked(self.sealed,restored,m.digest(restored),explicit_now=self.now())
            response=self.ledger.read_once('final-owner-SetDefaultPolicyVersion-response.once.json')
            m.require(m.instant(response['receivedAt'])<=m.instant(restored['observedAt'])
                and m.instant(response['receivedAt'])<=m.instant(records['readback']['boundary']['observedAt']),'final-readbacks-after-restoration')
            receipt={'schema':'hikyo-158/final-owner-closure/v1','observedAt':m.stamp(self.now()),'boundPlanSHA256':m.digest(self.bound),
                'sealedModelSHA256':m.digest(self.sealed),'originalClaimSHA256':state['claimSHA256'],'boundaryPolicyId':state['boundaryPolicyId'],
                'boundaryAbsence':records['readback']['boundary'],'restoredPolicyModel':model,'ownerMutations':state['finalOwner'],
                'reservationRefund':0,'remainingDispatches':0,'retainedSetupUserId':m.USER_ID,'executionAuthorityGranted':False}
            self.ledger.once('final-owner-closure.once.json',receipt);state['phase']='owner-cleanup-closure-recorded'
            self.ledger.cas(head,'final-owner-complete',state);return receipt


if __name__=='__main__':
    raise SystemExit('SOURCE ONLY: final owner transport and authentication are absent')
