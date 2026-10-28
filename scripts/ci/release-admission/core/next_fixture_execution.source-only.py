"""SOURCE ONLY. New fixture orchestration, closed argv and proof contracts.

Import has no I/O. No default command launcher or live proof authenticator is
provided. The entry point refuses. Tests inject all clocks, public proofs,
transport and temporary storage. No historical controller is imported.
"""
from abc import ABC, abstractmethod
import copy
from datetime import datetime, timedelta
import hashlib
import re

MODEL_SHA256 = 'f40c07b2204c3305b434836015c99f30af4dfc893a0ce0e4f0318c17ea844a19'
MODEL_TEST_SHA256 = 'f06b2e173ff499070ae8718d65870534510a585339b8fc451462baaea6dc5985'
POLICY_NAME = 'hikyo-158-fixture'
OWNER_OPERATIONS = ('CreateKey', 'DeletePolicyVersion', 'CreatePolicyVersion', 'CreatePolicy')
PROVISION_OPERATIONS = frozenset(('CreateOpenIDConnectProvider', 'CreateRole', 'PutRolePolicy'))
RETIRE_OPERATIONS = frozenset(('DeleteRolePolicy', 'DeleteRole', 'DeleteOpenIDConnectProvider', 'ScheduleKeyDeletion'))


class RootProofAdapter(ABC):
    """Later separately reviewed authenticator; never implemented by this source.

    authenticate must independently authenticate the complete original response,
    source/session, byte pins, request selectors, timestamps and context, not
    accept caller booleans, supplied hashes, or this pure model as authority.
    Return the same complete bundle only after that independent work. A later
    live adapter and launcher require separate Root/independent review.
    """
    @abstractmethod
    def authenticate(self, bundle, context, explicit_now):
        raise NotImplementedError('No live Root proof adapter supplied')

    def records_from_authenticated_bundle(self, bundle):
        # Called only after whole original bundle authentication/equality.
        # Default existing structured-record behavior is unchanged.
        return copy.deepcopy(bundle['records'])


def provision_schedule(m):
    order = ['hikyo-158-source', *[r for r in m.ROLES if r not in ('hikyo-158-source', 'hikyo-158-assume')], 'hikyo-158-assume']
    return [('CreateOpenIDConnectProvider', None)] + [(op, role) for role in order for op in ('CreateRole', 'PutRolePolicy')]


def retirement_schedule(m):
    order = sorted(m.ROLES, key=lambda role: (role != 'hikyo-158-assume', role == 'hikyo-158-source', role))
    return [('DeleteRolePolicy', r) for r in m.ROLES] + [('DeleteRole', r) for r in order] + [
        ('DeleteOpenIDConnectProvider', None), ('ScheduleKeyDeletion', None)]


def source_manifest_checked(m, sources):
    m.exact(sources, {'modelSHA256', 'modelTestSHA256', 'ledgerSHA256', 'executorSHA256', 'executorTestSHA256', 'frozenNextHelpers'})
    m.require(sources['modelSHA256'] == MODEL_SHA256 and sources['modelTestSHA256'] == MODEL_TEST_SHA256
              and m.same(sources['frozenNextHelpers'], m.FROZEN)
              and all(m.sha(sources[k]) for k in ('ledgerSHA256', 'executorSHA256', 'executorTestSHA256')), 'execution-source-pins')


def cleanup_code_checked(model,bound,proof,explicit_now):
    """Fresh immutable dispatched source only; never creation/dispatch admission.

    Root authenticates fresh original public commit/file/absence readbacks at
    these immutable SHAs. A moving canonical main is irrelevant to exact cleanup.
    The original complete CI/bootstrap/owner approval is reconstructed at its
    actual original observation, with no retroactive or new spending authority.
    """
    m=model;m.exact(proof,{'schema','observedAt','boundPlanSHA256','candidateCommit','workflowCommit','candidateFiles','workflowFiles','absentFiles'})
    m.fresh(proof['observedAt'],explicit_now)
    m.require(proof['schema']=='hikyo-158/immutable-dispatch-code-readback/v1' and proof['boundPlanSHA256']==m.digest(bound),'exact-immutable-cleanup-source')
    original=bound['records']['code'];manifest=bound['preparation']['proposal']['sourceManifest']
    heads=m.ci_original_for_cleanup_checked(original,manifest,m.instant(original['observedAt']),explicit_now)
    m.require(heads=={'candidate':bound['run']['candidateSHA'],'workflow':bound['run']['workflowSHA']},'original-dispatch-code-authority')
    m.exact(proof['absentFiles'],{'candidate','workflow'})
    for kind in ('candidate','workflow'):
        commit=proof[kind+'Commit'];m.public_commit_checked(commit,requires_dco=kind=='candidate')
        m.require(m.same(commit,original[kind+'Commit']) and m.same(proof[kind+'Files'],original[kind+'Files'])
            and m.same(proof['absentFiles'][kind],{'commitSHA':heads[kind],'path':'go.work','gitTreeEntry':None}),'fresh-exact-dispatched-commit-files')
    return heads


class OfficialFixtureCLI:
    """Closed official AWS CLI registry, with mandatory injected command runner.

    A command runner is transport, not authority. Calls require the controller's
    one-invocation active context and an adjacent exclusive transmit marker.
    No environment or login file is read by this wrapper.
    """
    def __init__(self, model, ledger, bound, login_root, base_environment, thumbprints, command, explicit_clock):
        self.m, self.ledger, self.bound = model, ledger, copy.deepcopy(bound)
        m = self.m
        m.require(callable(command) and callable(explicit_clock), 'no-default-command-launcher-or-clock')
        self.now=explicit_clock
        m.require(type(login_root) is str and login_root.startswith('/') and '..' not in login_root.split('/')
                  and '\x00' not in login_root and login_root.endswith('/private-operator/aws-local-login'), 'exact-private-login-location')
        m.exact(base_environment, {'PATH', 'LANG', 'TMPDIR'})
        m.require(base_environment['PATH'] == '/opt/homebrew/bin:/usr/bin:/bin'
                  and base_environment['LANG'] in ('C', 'C.UTF-8') and type(base_environment['TMPDIR']) is str
                  and base_environment['TMPDIR'].startswith('/') and '\x00' not in base_environment['TMPDIR'], 'closed-base-environment')
        m.require(type(thumbprints) is list and 1 <= len(thumbprints) <= 2 and all(m.sha(t, 40) for t in thumbprints)
                  and len(set(thumbprints)) == len(thumbprints), 'reviewed-provider-thumbprints')
        self.thumbprints, self.command = copy.deepcopy(thumbprints), command
        self.environment = {**base_environment, 'AWS_CONFIG_FILE': login_root + '/config',
            'AWS_SHARED_CREDENTIALS_FILE': login_root + '/credentials', 'AWS_LOGIN_CACHE_DIRECTORY': login_root + '/cache',
            'AWS_REGION': m.REGION, 'AWS_DEFAULT_REGION': m.REGION, 'AWS_STS_REGIONAL_ENDPOINTS': 'regional',
            'AWS_EC2_METADATA_DISABLED': 'true', 'AWS_IGNORE_CONFIGURED_ENDPOINT_URLS': 'true',
            'AWS_CLI_AUTO_PROMPT': 'off', 'AWS_PAGER': '', 'AWS_MAX_ATTEMPTS': '1', 'AWS_RETRY_MODE': 'standard'}
        self._active = None

    def arguments(self, operation, role=None):
        m, bound = self.m, self.bound
        tags = [{'Key': 'HikyoIssue', 'Value': '158'}, {'Key': 'Authorization', 'Value': bound['run']['authorizationId']}]
        fixed = {
            'Identity': ['sts', 'get-caller-identity'],
            'HistoricalDescribe': ['secretsmanager', 'describe-secret', '--secret-id', m.HISTORICAL], 'User': ['iam', 'get-user'],
            'MFA': ['iam', 'list-mfa-devices', '--user-name', m.USER, '--no-paginate'],
            'Roles': ['iam', 'list-roles', '--no-paginate'], 'Providers': ['iam', 'list-open-id-connect-providers'],
            'Provider': ['iam', 'get-open-id-connect-provider', '--open-id-connect-provider-arn', m.PROVIDER],
            'Key': ['kms', 'describe-key', '--key-id', bound['run']['keyARN']],
            'KeyPolicy': ['kms', 'get-key-policy', '--key-id', bound['run']['keyARN'], '--policy-name', 'default'],
            'Rotation': ['kms', 'get-key-rotation-status', '--key-id', bound['run']['keyARN']],
            'CreateOpenIDConnectProvider': ['iam', 'create-open-id-connect-provider', '--url', 'https://token.actions.githubusercontent.com',
                '--client-id-list', 'sts.amazonaws.com', '--thumbprint-list', *self.thumbprints, '--tags', m.canonical(tags).decode()],
            'DeleteOpenIDConnectProvider': ['iam', 'delete-open-id-connect-provider', '--open-id-connect-provider-arn', m.PROVIDER],
            'ScheduleKeyDeletion': ['kms', 'schedule-key-deletion', '--key-id', bound['run']['keyARN'], '--pending-window-in-days', '7']}
        if operation in fixed:
            m.require(role is None, 'no-foreign-selector')
            args = fixed[operation]
        else:
            m.require(role in m.ROLES, 'closed-role-selector')
            duration = next(r['maxSessionDuration'] for r in bound['preparation']['proposal']['inputs']['firstProvisionPlan']['roles'] if r['name'] == role)
            by_role = {
                'Role': ['iam', 'get-role', '--role-name', role],
                'Policy': ['iam', 'get-role-policy', '--role-name', role, '--policy-name', POLICY_NAME],
                'Inline': ['iam', 'list-role-policies', '--role-name', role, '--no-paginate'],
                'Attached': ['iam', 'list-attached-role-policies', '--role-name', role, '--no-paginate'],
                'CreateRole': ['iam', 'create-role', '--role-name', role, '--path', '/', '--permissions-boundary', m.BOUNDARY,
                    '--max-session-duration', str(duration), '--tags', m.canonical(tags).decode(),
                    '--assume-role-policy-document', m.canonical(bound['documents'][role + '.trust.json']).decode()],
                'PutRolePolicy': ['iam', 'put-role-policy', '--role-name', role, '--policy-name', POLICY_NAME,
                    '--policy-document', m.canonical(bound['documents'][role + '.policy.json']).decode()],
                'DeleteRolePolicy': ['iam', 'delete-role-policy', '--role-name', role, '--policy-name', POLICY_NAME],
                'DeleteRole': ['iam', 'delete-role', '--role-name', role]}
            m.require(operation in by_role, 'closed-operation-registry')
            args = by_role[operation]
        return ['/opt/homebrew/bin/aws', *args, '--profile', m.USER, '--region', m.REGION,
            '--no-cli-pager', '--no-cli-auto-prompt', '--output', 'json', '--cli-connect-timeout', '10', '--cli-read-timeout', '20']

    def call(self, operation, role, *, timeout, deadline, intent=None):
        m = self.m
        m.require(self._active in ('provision', 'retire') and type(timeout) in (int, float) and 0 < timeout <= 45, 'finite-active-invocation')
        args = self.arguments(operation, role)
        write = operation in PROVISION_OPERATIONS | RETIRE_OPERATIONS
        if write:
            m.require(operation in (PROVISION_OPERATIONS if self._active=='provision' else RETIRE_OPERATIONS),'closed-operation-per-phase')
            m.require(type(intent) is dict and intent['operation'] == operation and intent['role'] == role
                      and intent['argumentsSHA256'] == m.digest(args) and intent['maximumRequests'] is not True
                      and type(intent['maximumRequests']) is int and intent['maximumRequests'] == 1, 'durable-exact-intent')
            m.require(m.same(self.ledger.read_once(intent['file']), intent), 'persisted-intent-required')
            self.ledger.once(intent['transmitFile'], {'intentSHA256': m.digest(intent), 'maximumRequests': 1})
        else:
            m.require(intent is None, 'reads-have-no-mutation-intent')
        # Exactly one call. Timeout/exception is ambiguous, never retried.
        environment=copy.deepcopy(self.environment)
        if write and self._active=='provision':
            # Recheck after durable intent/transmit persistence and every setup
            # read, immediately before the actual creation transport call.
            # Retirement reconstructs original authority and stays independent
            # of present release eligibility.
            m.admit_current_release(self.bound['preparation'],self.now())
        # Admission is an external observation and can consume the remaining
        # lease. Timestamp and bound the actual send only after it returns.
        sent=self.now();m.clock(sent);remaining=(m.instant(deadline)-sent).total_seconds()
        m.require(remaining>0,'deadline-adjacent-to-forward')
        result = self.command(args, env=environment, timeout=min(timeout,remaining))
        received=self.now();m.clock(received);m.require(sent<=received,'explicit-cli-clock-order')
        m.exact(result, {'returncode', 'stdout', 'stderr'})
        m.require(type(result['returncode']) is int and type(result['stdout']) is bytes and type(result['stderr']) is bytes
                  and len(result['stdout']) <= 2_000_000 and len(result['stderr']) <= 32768, 'bounded-command-outcome')
        try:
            parsed = m.strict_json(result['stdout']) if result['stdout'].strip() else {}
            m.require(type(parsed) is dict, 'metadata-json-object')
        except Exception:
            if write:self.ledger.once(intent['file'].replace('-intent.', '-wire-outcome.'), {'intentSHA256':m.digest(intent),
                'sentAt':m.stamp(sent),'receivedAt':m.stamp(received),'returncode':result['returncode'],'stdoutSHA256':hashlib.sha256(result['stdout']).hexdigest(),
                'stderrSHA256':hashlib.sha256(result['stderr']).hexdigest(),'parsedPublicResponse':None,'responseAmbiguous':True})
            raise RuntimeError('Unparseable public response; preserved wire hashes; no retry') from None
        if write:self.ledger.once(intent['file'].replace('-intent.', '-wire-outcome.'), {'intentSHA256':m.digest(intent),
            'sentAt':m.stamp(sent),'receivedAt':m.stamp(received),'returncode':result['returncode'],'stdoutSHA256':hashlib.sha256(result['stdout']).hexdigest(),
            'stderrSHA256':hashlib.sha256(result['stderr']).hexdigest(),'parsedPublicResponse':parsed,'responseAmbiguous':False})
        return {'sentAt':m.stamp(sent),'receivedAt':m.stamp(received),'returncode': result['returncode'], 'value': parsed, 'stderrEmpty': result['stderr'] == b'',
            'stdoutSHA256': hashlib.sha256(result['stdout']).hexdigest(), 'stderrSHA256': hashlib.sha256(result['stderr']).hexdigest()}


class FixtureController:
    def __init__(self, model, ledger, prepared, sources, adapter, explicit_clock):
        self.m, self.ledger, self.prepared = model, ledger, copy.deepcopy(prepared)
        self.sources, self.adapter, self.now = copy.deepcopy(sources), adapter, explicit_clock
        model.require(isinstance(adapter, RootProofAdapter) and callable(explicit_clock), 'reviewed-adapter-and-explicit-clock')
        source_manifest_checked(model, sources)
        model.preparation_checked(prepared)
        model.require(ledger.folder.name==model.configuration_checked()['ledgerFolder'],'exact-configured-ledger-folder')
        manifest=prepared['proposal']['sourceManifest']
        model.require(manifest['modelSHA256']==sources['modelSHA256'] and manifest['testSHA256']==sources['modelTestSHA256'],'prepared-model-source-crosslink')
        self.bound = None

    def _proof(self, kind, bundle, state_sha=None):
        m = self.m
        m.exact(bundle, {'schema', 'kind', 'observedAt', 'records', 'rawEvidence'})
        m.require(bundle['schema'] == 'hikyo-158/next-external-proof-bundle/v1' and bundle['kind'] == kind
                  and type(bundle['records']) is dict and type(bundle['rawEvidence']) is list
                  and 1 <= len(bundle['rawEvidence']) <= 128, 'whole-external-proof-required')
        now = self.now();m.fresh(bundle['observedAt'], now)
        for raw in bundle['rawEvidence']:
            m.exact(raw, {'source', 'operation', 'selector', 'response', 'responseSHA256', 'observedAt'})
            m.require(raw['source'] in ('native-owner-console', 'github-api', 'official-cli', 'public-source')
                      and type(raw['operation']) is str and type(raw['selector']) is dict
                      and (type(raw['response']) in (dict,list) or (type(raw['response']) is str
                          and raw['source']=='public-source' and raw['operation']=='RootCompleteModelWireStdout'
                          and raw['selector']=={'schema':'hikyo-158/complete-model-stdout-selector/v1','kind':'phase-records'})) and raw['responseSHA256'] == m.digest(raw['response']), 'complete-public-source-custody')
            if kind=='retire' and raw['operation']=='GrossChargesReadback' and raw['source']=='native-owner-console':
                m.require(m.instant(raw['observedAt'])<=now,'no-future-billing-observation')
            else:m.fresh(raw['observedAt'], now)
        context = {'phase': kind, 'account': m.ACCOUNT, 'region': m.REGION,
            'approvalSHA256': m.SOURCE_PINS['approval'], 'preparationSHA256': m.digest(self.prepared),
            'attemptTrustBindingSHA256':self.prepared['attemptTrustBindingSHA256'],
            'boundSHA256': m.digest(self.bound) if self.bound else None, 'stateSHA256': state_sha, 'sourcePins': self.sources,
            'storagePaths':{'ledger':str(self.ledger.folder),'globalParent':str(self.ledger.parent),
                'globalLock':str(self.ledger.parent / '.provisioning.lock')}}
        verified = self.adapter.authenticate(copy.deepcopy(bundle), copy.deepcopy(context), now)
        m.require(m.same(verified, bundle), 'adapter-must-authenticate-whole-original-bundle')
        records=self.adapter.records_from_authenticated_bundle(copy.deepcopy(verified))
        m.require(type(records) is dict,'authenticated-phase-records-object')
        return copy.deepcopy(records)

    def _baseline(self, records):
        m = self.m;m.exact(records, {'code', 'owner', 'budget','historicalAbsence'})
        m.require(m.ci_checked(records['code'], self.prepared['proposal']['sourceManifest'], self.now()) ==
                  {'candidate': self.prepared['candidateSHA'], 'workflow': self.prepared['workflowSHA']}, 'unchanged-reviewed-heads')
        m.owner_checked(records['owner'], self.prepared['proposal'], self.now());m.budget_checked(records['budget'], self.now())
        m.historical_absence_checked(records['historicalAbsence'],self.now(),m.digest(records['owner']['ownerContext']))
        m.require(m.instant(self.prepared['lease']['start']) <= self.now() < m.instant(self.prepared['lease']['until']), 'active-original-lease')

    def reserve(self, proof):
        m = self.m
        with self.ledger.lock():
            records = self._proof('reserve', proof);self._baseline(records)
            m.first_key_forward_window(self.prepared,explicit_now=self.now(),minimum_remaining_ms=120000)
            record = {'schema': 'hikyo-158/next-exclusive-reservation/v1', 'approvalSHA256': m.SOURCE_PINS['approval'],
                'preparationSHA256': m.digest(self.prepared),
                'attemptTrustBindingSHA256':self.prepared['attemptTrustBindingSHA256'], 'heldHistorySHA256': m.digest(self.prepared['proposal']['inputs']['heldHistory']),
                'completedHistorySHA256':m.digest(self.prepared['proposal']['inputs']['completedAttempt']),
                'configurationSHA256':m.digest(m.release_configuration_checked()), 'sourcePins': self.sources,
                'historicalPins': m.SOURCE_PINS, 'usedNamespaces': self.prepared['proposal']['usedNamespaces'],
                'namespace': self.prepared['namespace'], 'retainedReservations': 176, 'newReservationsCharged': 32,
                'lifetimeReservations': 208, 'maximumDispatches': 1, 'automaticRetry': False,
                'reservedAt': m.stamp(self.now()), 'proofSHA256': m.digest(proof)}
            return self.ledger.claim(record)

    def _state(self):
        m = self.m;head, state = self.ledger.snapshot();claim = self.ledger.claim_record()
        m.require(claim['schema'] == 'hikyo-158/next-exclusive-reservation/v1'
                  and claim['approvalSHA256'] == m.SOURCE_PINS['approval'] and claim['preparationSHA256'] == m.digest(self.prepared)
                  and claim['attemptTrustBindingSHA256']==self.prepared['attemptTrustBindingSHA256']
                  and claim['heldHistorySHA256'] == m.SOURCE_PINS['heldHistory'] and claim['completedHistorySHA256']==m.SOURCE_PINS['completedAttempt'] and claim['configurationSHA256']==m.digest(m.release_configuration_checked()) and claim['heldHistorySHA256']==m.digest(self.prepared['proposal']['inputs']['heldHistory'])
                  and m.same(claim['sourcePins'], self.sources) and m.same(claim['historicalPins'], m.SOURCE_PINS)
                  and type(claim['retainedReservations']) is int and claim['retainedReservations'] == 176
                  and type(claim['newReservationsCharged']) is int and claim['newReservationsCharged'] == 32
                  and type(claim['lifetimeReservations']) is int and claim['lifetimeReservations'] == 208
                  and type(claim['maximumDispatches']) is int and claim['maximumDispatches'] == 1
                  and claim['automaticRetry'] is False and claim['namespace'] == self.prepared['namespace'], 'immutable-reservation-custody')
        m.require(state is not None and state['attemptTrustBindingSHA256']==self.prepared['attemptTrustBindingSHA256'], 'partial-reservation-hold')
        try:
            self.ledger.read_once('held.once.json');state['held'] = True
        except FileNotFoundError:
            pass
        return head, state

    def _hold(self, reason):
        # A failed CAS or result write cannot erase earlier intent/transmit files.
        # If disk failure also prevents this marker, their presence still forbids replay.
        try:
            self.ledger.once('held.once.json', {'reason': reason, 'automaticRetry': False})
            head, state = self._state();state['held'] = True
            self.ledger.cas(head, 'held', state)
        except (OSError, ValueError):
            pass

    def arm_owner(self, operation, request, proof):
        m = self.m;m.require(operation in OWNER_OPERATIONS, 'owner-only-closed-operation')
        with self.ledger.lock():
            head, state = self._state();m.require(not state['held'] and state['pending'] is None, 'no-pending-or-held-owner-write')
            records = self._proof('owner-' + operation, proof, head)
            if operation == 'CreateKey':
                self._baseline(records)
                m.first_key_forward_window(self.prepared,explicit_now=self.now(),minimum_remaining_ms=60000)
                m.require(state['keyARN'] is None and state['owner'] == {} and self.bound is None, 'key-first-once')
                self._key_request(request)
            else:
                m.require(self.bound is not None and state['boundSHA256'] == m.digest(self.bound), 'bound-owner-policy-stage')
                self._active_gate(records)
                docs = self.bound['documents']
                expected = {'DeletePolicyVersion': {'PolicyArn': m.SETUP, 'VersionId': 'v1'},
                    'CreatePolicyVersion': {'PolicyArn': m.SETUP, 'PolicyDocument': docs['setup-user.managed-policy.json'], 'SetAsDefault': True},
                    'CreatePolicy': {'PolicyName': 'hikyo-158-role-boundary', 'Path': '/', 'PolicyDocument': docs['setup-role-boundary.json']}}[operation]
                m.require(m.same(request, expected), 'exact-owner-request')
                required = {'DeletePolicyVersion': ['CreateKey'], 'CreatePolicyVersion': ['CreateKey', 'DeletePolicyVersion'],
                    'CreatePolicy': ['CreateKey', 'DeletePolicyVersion', 'CreatePolicyVersion']}[operation]
                m.require(set(state['owner']) == set(required), 'owner-stage-order')
                wanted_stage = {'DeletePolicyVersion': ({'v1','v2','v3','v4','v5'},'v5'),
                    'CreatePolicyVersion': ({'v2','v3','v4','v5'},'v5'), 'CreatePolicy': ({'v2','v3','v4','v5','v6'},'v6')}[operation]
                policy_stage = records['ownerStage']['setupPolicy']
                m.require((set(policy_stage['versions']),policy_stage['defaultVersionId']) == wanted_stage
                          and records['boundary']['value'] is None, 'exact-before-owner-stage')
            intent = {'schema': 'hikyo-158/next-owner-intent/v1', 'operation': operation, 'request': copy.deepcopy(request),
                'preparationSHA256': m.digest(self.prepared),
                'attemptTrustBindingSHA256':self.prepared['attemptTrustBindingSHA256'], 'boundSHA256': m.digest(self.bound) if self.bound else None,
                'stateSHA256': head, 'claimId': 'next-owner-' + operation, 'maximumRequests': 1, 'armedAt': m.stamp(self.now()), 'proofSHA256': m.digest(proof)}
            self.ledger.once('owner-' + operation + '-intent.once.json', intent)
            state['pending'] = intent;self.ledger.cas(head, 'owner-intent', state)
            return copy.deepcopy(intent)

    def _key_request(self, request):
        m = self.m;m.exact(request, {'Policy', 'Description', 'Origin', 'Tags', 'KeyUsage', 'KeySpec', 'MultiRegion'})
        m.require(request['Origin'] == 'AWS_KMS' and request['Tags'] == [] and request['KeyUsage'] == 'ENCRYPT_DECRYPT'
                  and request['KeySpec'] == 'SYMMETRIC_DEFAULT' and request['MultiRegion'] is False
                  and type(request['Description']) is str and 1 <= len(request['Description']) <= 8192, 'closed-new-key-request')
        policy = request['Policy'];m.exact(policy, {'Version', 'Id', 'Statement'})
        m.require(policy['Version'] == '2012-10-17' and type(policy['Id']) is str and 1 <= len(policy['Id']) <= 128
                  and m.same(policy['Statement'], [{'Sid': 'Enable IAM User Permissions', 'Effect': 'Allow',
                      'Principal': {'AWS': f'arn:aws:iam::{m.ACCOUNT}:root'}, 'Action': 'kms:*', 'Resource': '*'}]), 'root-delegation-only-key')

    def owner_forward_ticket(self, operation, proof):
        """External native guard must burn this ticket immediately before its one forward."""
        m = self.m
        with self.ledger.lock():
            head, state = self._state();intent = self.ledger.read_once('owner-' + operation + '-intent.once.json')
            m.require(not state['held'] and m.same(state['pending'], intent), 'exact-pending-owner-intent')
            records = self._proof('owner-forward-' + operation, proof, head)
            if operation == 'CreateKey':
                self._baseline(records)
                m.first_key_forward_window(self.prepared,explicit_now=self.now(),minimum_remaining_ms=60000)
            else:
                self._active_gate(records)
                expected={'DeletePolicyVersion':({'v1','v2','v3','v4','v5'},'v5'),
                    'CreatePolicyVersion':({'v2','v3','v4','v5'},'v5'),'CreatePolicy':({'v2','v3','v4','v5','v6'},'v6')}[operation]
                current=records['ownerStage']['setupPolicy']
                m.require((set(current['versions']),current['defaultVersionId'])==expected and records['boundary']['value'] is None,'exact-before-native-forward-stage')
            m.fresh(intent['armedAt'], self.now())
            ticket = {'intentSHA256': m.digest(intent), 'attemptTrustBindingSHA256':self.prepared['attemptTrustBindingSHA256'], 'maximumRequests': 1, 'issuedAt': m.stamp(self.now())}
            self.ledger.once('owner-' + operation + '-transmit.once.json', ticket)
            return ticket

    def accept_owner_receipt(self, operation, proof):
        m = self.m
        with self.ledger.lock():
            head, state = self._state();intent = self.ledger.read_once('owner-' + operation + '-intent.once.json')
            ticket=self.ledger.read_once('owner-' + operation + '-transmit.once.json')
            m.require(m.same(state['pending'], intent), 'pending-owner-custody-only')
            records = self._proof('owner-result-' + operation, proof, head)
            m.exact(records, {'intentSHA256', 'maximumForwards', 'forwardsObserved', 'sentAt', 'receivedAt', 'response', 'keyProof'})
            # Persist actual returned public metadata even when custody validation holds.
            self.ledger.once('owner-' + operation + '-response.once.json', records)
            try:
                m.require(records['intentSHA256'] == m.digest(intent) and type(records['maximumForwards']) is int
                          and records['maximumForwards'] == 1 and type(records['forwardsObserved']) is int
                          and records['forwardsObserved'] == 1 and ticket['intentSHA256']==m.digest(intent)
                          and m.instant(intent['armedAt']) <= m.instant(ticket['issuedAt']) <= m.instant(records['sentAt'])
                          <= m.instant(records['receivedAt']) <= self.now(), 'exact-one-native-forward-custody')
                if operation == 'CreateKey':
                    key = records['keyProof'];m.require(type(key) is dict, 'whole-key-readback-proof')
                    arn = m.key_checked(key, self.prepared['proposal'], self.now())
                    m.require(key['preparationSHA256'] == m.digest(self.prepared)
                              and key['claimId'] == intent['claimId'] and key['sentAt'] == records['sentAt'] and key['receivedAt'] == records['receivedAt']
                              and m.same(key['intent']['request'], intent['request']) and m.same(key['createResponse'], records['response']), 'key-response-bound-to-intent')
                    state['keyARN'] = arn
                elif operation == 'DeletePolicyVersion':m.require(records['response'] == {} and records['keyProof'] is None, 'exact-owner-delete-response')
                elif operation == 'CreatePolicyVersion':
                    m.require(m.same(records['response'], {'VersionId': 'v6', 'IsDefaultVersion': True}) and records['keyProof'] is None, 'exact-v6-response')
                elif operation == 'CreatePolicy':
                    response = records['response'];m.exact(response, {'Arn', 'PolicyId', 'DefaultVersionId'})
                    m.require(response['Arn'] == m.BOUNDARY and type(response['PolicyId']) is str
                              and re.fullmatch(r'ANPA[A-Z0-9]{8,64}', response['PolicyId']) is not None
                              and response['PolicyId'] not in (m.SETUP_ID, 'ANPAROHA7PNS5QVDJKPIN')
                              and response['DefaultVersionId'] == 'v1' and records['keyProof'] is None, 'new-boundary-response')
                    state['boundaryPolicyId'] = response['PolicyId']
                state['owner'][operation] = m.digest(records);state['pending'] = None
                self.ledger.cas(head, 'owner-custody', state)
            except Exception:
                self._hold('owner-response-or-cas-held');raise RuntimeError('Owner response held; preserved custody, no retry') from None

    def arm_dispatch(self, proof):
        """Durable dispatch intent; GitHub API forwarding remains external."""
        m = self.m
        with self.ledger.lock():
            head, state = self._state();m.require(not state['held'] and state['pending'] is None and state['dispatch'] is None
                and state['keyARN'] is not None and set(state['owner'])=={'CreateKey'}
                and state['boundSHA256'] is None and self.bound is None, 'captured-key-before-single-dispatch')
            records = self._proof('dispatch', proof, head)
            self._baseline(records)
            # No legacy helper invocation is admitted. Source-compatible pins
            # still require separate Root review before any live execution.
            template={key:None for key in ('runId','workflowSHA','candidateSHA','authorizationId','prefix','authUntil','cleanupUntil','keyARN','externalId')}
            historical=[row for row in m.environment_variables(template) if row['name']=='AWS_ACCEPTANCE_HISTORICAL_CLEANUP_ARN']
            m.require(historical==[] or m.same(historical,[{'name':'AWS_ACCEPTANCE_HISTORICAL_CLEANUP_ARN','value':''}]), 'historical-describe-only-configuration-still-held')
            intent = {'schema': 'hikyo-158/next-dispatch-intent/v1', 'preparationSHA256': m.digest(self.prepared),
                'attemptTrustBindingSHA256':self.prepared['attemptTrustBindingSHA256'],
                'workflowSHA': self.prepared['workflowSHA'], 'candidateSHA': self.prepared['candidateSHA'],
                'path': '.github/workflows/aws-acceptance.yml', 'ref': 'main', 'inputs': {'reviewed_sha': self.prepared['candidateSHA']},
                'maximumRequests': 1, 'maximumAttempts': 1, 'stateSHA256': head, 'armedAt': m.stamp(self.now()), 'proofSHA256': m.digest(proof)}
            self.ledger.once('dispatch-intent.once.json', intent)
            state['dispatch'] = intent;self.ledger.cas(head, 'dispatch-intent', state)
            return intent

    def dispatch_forward_ticket(self, proof):
        m = self.m
        with self.ledger.lock():
            head,state = self._state();intent = self.ledger.read_once('dispatch-intent.once.json')
            m.require(not state['held'] and state['pending'] is None and m.same(state['dispatch'],intent), 'pending-single-dispatch')
            records = self._proof('dispatch-forward',proof,head);self._baseline(records);m.fresh(intent['armedAt'],self.now())
            ticket = {'intentSHA256':m.digest(intent),'attemptTrustBindingSHA256':self.prepared['attemptTrustBindingSHA256'],'maximumRequests':1,'issuedAt':m.stamp(self.now())}
            self.ledger.once('dispatch-transmit.once.json',ticket)
            return ticket

    def bind(self, proof):
        m = self.m
        with self.ledger.lock():
            head, state = self._state();m.require(not state['held'] and state['pending'] is None and state['boundSHA256'] is None
                and state['keyARN'] is not None and state['dispatch'] is not None, 'new-key-and-single-dispatch-required')
            records = self._proof('bind', proof, head)
            m.exact(records, {'bindingRecords', 'reviewedStart', 'dispatchCustody','historicalAbsence'})
            m.exact(records['bindingRecords'],{'key','job','budget','code','owner','historicalAbsence','firstMutationEvidence'})
            m.require(m.same(records['historicalAbsence'],records['bindingRecords']['historicalAbsence']),'same-independent-bind-describe-proof')
            self._historical_absence(records['historicalAbsence'],records['bindingRecords']['owner']['ownerContext'])
            ticket = self.ledger.read_once('dispatch-transmit.once.json')
            m.require(ticket['intentSHA256'] == m.digest(state['dispatch']), 'exact-dispatch-transmit-ticket')
            custody = records['dispatchCustody'];m.exact(custody, {'intentSHA256', 'forwardsObserved', 'sentAt', 'receivedAt', 'runId'})
            m.require(custody['intentSHA256'] == m.digest(state['dispatch']) and type(custody['forwardsObserved']) is int
                      and custody['forwardsObserved'] == 1 and m.instant(state['dispatch']['armedAt']) <= m.instant(ticket['issuedAt']) <= m.instant(custody['sentAt'])
                      <= m.instant(custody['receivedAt']) <= self.now(), 'single-dispatch-custody')
            bound = m.bind_pending_run(self.prepared, records['bindingRecords'], {k: m.digest(v) for k,v in records['bindingRecords'].items()},
                reviewed_start=records['reviewedStart'], explicit_now=self.now())
            accepted=self.ledger.read_once('owner-CreateKey-response.once.json')['keyProof']
            current=records['bindingRecords']['key']
            m.require(m.instant(accepted['observedAt'])<=m.instant(current['observedAt'])
                and m.same({k:v for k,v in accepted.items() if k!='observedAt'},
                           {k:v for k,v in current.items() if k!='observedAt'}),'immutable-accepted-key-facts-current-readback')
            # Current observedAt must originate from a genuine fresh normalized
            # eight-row readback in the Root-authenticated whole phase capture.
            # Never rewrite the original accepted receipt or creation clocks.
            m.require(bound['run']['runId'] == str(custody['runId']) and bound['run']['keyARN'] == state['keyARN'], 'bound-custody-crosslinks')
            self.ledger.once('bound-plan.once.json', bound)
            state['boundSHA256'] = m.digest(bound);state['phase'] = 'bound'
            self.ledger.cas(head, 'bound', state);self.bound = copy.deepcopy(bound)
            return bound

    def _active_gate(self, records):
        m = self.m;m.exact(records, {'code', 'budget', 'ownerStage', 'ownerContext','historicalAbsence','oldKeys', 'newKey', 'boundary'})
        m.require(self.bound is not None, 'bound-plan-required')
        m.bound_checked(self.bound, self.now());m.ci_checked(records['code'], self.prepared['proposal']['sourceManifest'], self.now())
        m.budget_checked(records['budget'], self.now())
        m.require(records['code']['candidateCommit']['sha'] == self.prepared['candidateSHA']
                  and records['code']['workflowCommit']['sha'] == self.prepared['workflowSHA'], 'active-heads')
        self._owner_context(records['ownerContext']);self._historical_absence(records['historicalAbsence'],records['ownerContext'])
        stage = records['ownerStage'];m.exact(stage, {'observedAt', 'setupUser', 'setupPolicy'})
        m.fresh(stage['observedAt'], self.now())
        m.require(m.same(stage['setupUser'], self.prepared['records']['owner']['setupUser']), 'retained-whole-identity-and-mfa')
        archive = self.prepared['proposal']['inputs']['iamClosure']['setupPolicy']
        current = stage['setupPolicy'];default = current['defaultVersionId'];versions = set(current['versions'])
        m.require((versions, default) in (({'v1','v2','v3','v4','v5'},'v5'), ({'v2','v3','v4','v5'},'v5'), ({'v2','v3','v4','v5','v6'},'v6')), 'closed-owner-version-stage')
        m.setup_policy_checked(current, archive, versions, default, self.bound['documents'])
        m.require(m.same(records['oldKeys'], self.prepared['records']['owner']['oldKeys'])
                  and m.same(records['newKey'], self.bound['records']['key']['readback']), 'new-key-and-four-old-schedules')
        boundary = records['boundary'];m.exact(boundary, {'observedAt', 'value'});m.fresh(boundary['observedAt'], self.now())
        if boundary['value'] is not None:
            value = boundary['value'];m.exact(value, {'arn','policyId','defaultVersionId','versions'})
            m.require(value['arn'] == m.BOUNDARY and value['defaultVersionId'] == 'v1'
                      and m.same(value['versions'], {'v1': {'isDefault': True, 'document': self.bound['documents']['setup-role-boundary.json']}}), 'whole-new-boundary')

    def _identity(self, cli, deadline):
        m = self.m
        identity = self._read(cli, 'Identity', None, deadline)
        m.require(m.same(identity, {'Account': m.ACCOUNT, 'Arn': m.USER_ARN, 'UserId': m.USER_ID}), 'exact-nonroot-cli-identity')
        user = self._read(cli, 'User', None, deadline).get('User', {})
        m.require(user.get('UserId') == m.USER_ID and user.get('UserName') == m.USER and user.get('Arn') == m.USER_ARN, 'exact-cli-user')
        devices = self._read(cli, 'MFA', None, deadline)
        rows=devices.get('MFADevices')
        m.require(devices.get('IsTruncated') is False and not devices.get('Marker') and type(rows) is list
                  and len(rows)==1, 'sole-complete-attached-cli-mfa')
        actual=rows[0];m.exact(actual,{'UserName','SerialNumber','EnableDate'})
        m.require(actual['UserName']==m.USER and actual['SerialNumber']==m.MFA,'exact-attached-cli-mfa-identity')
        pinned=self.prepared['proposal']['inputs']['iamClosure']['mfaDevices']
        m.require(type(pinned) is list and len(pinned)==1,'sole-immutable-retained-mfa')
        m.exact(pinned[0],{'UserName','SerialNumber','EnableDate'})
        m.require(pinned[0]['UserName']==m.USER and pinned[0]['SerialNumber']==m.MFA
                  and self._mfa_date(actual['EnableDate'])==self._mfa_date(pinned[0]['EnableDate']), 'retained-mfa-enable-date')
        # Literal-MFA ListRoles succeeds only under the independently read whole
        # activated setup policy and this exact nonroot identity.
        roles = self._read(cli, 'Roles', None, deadline)
        m.require(roles.get('IsTruncated') is False and not roles.get('Marker') and type(roles.get('Roles')) is list, 'literal-mfa-complete-listroles')
        return roles['Roles']

    def _mfa_date(self,value):
        m=self.m
        m.require(type(value) is str and re.fullmatch(r'[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}(?:\.[0-9]{1,6})?(?:Z|\+00:00)',value) is not None,'explicit-utc-mfa-enable-date')
        parsed=datetime.fromisoformat(value.replace('Z','+00:00'))
        m.require(parsed.utcoffset()==timedelta(0) and parsed<=self.now(),'nonfuture-mfa-enable-date')
        return parsed

    def _read(self, cli, operation, role, deadline):
        outcome = cli.call(operation, role, timeout=self._timeout(deadline),deadline=deadline)
        self.m.require(outcome['returncode'] == 0 and outcome['stderrEmpty'] is True, 'read-failed-no-retry')
        return outcome['value']

    def _timeout(self, deadline):
        remaining = (self.m.instant(deadline) - self.now()).total_seconds()
        self.m.require(remaining > 0, 'finite-phase-deadline')
        return min(45, remaining)

    def _verify_key(self,cli,deadline):
        m=self.m;value=self._read(cli,'Key',None,deadline).get('KeyMetadata',{})
        expected=self.bound['records']['key']['readback']['metadata']
        value=copy.deepcopy(value)
        if type(value.get('CreationDate')) is str:
            instant=datetime.fromisoformat(value['CreationDate'].replace('Z','+00:00'))
            m.require(instant.tzinfo is not None and instant.utcoffset()==timedelta(0),'explicit-cli-key-creation-utc')
            value['CreationDate']=instant.timestamp()
        m.require(m.same(value,expected),'whole-new-key-cli-readback')
        policy=self._read(cli,'KeyPolicy',None,deadline).get('Policy')
        if type(policy) is str:policy=m.strict_json(policy.encode())
        m.require(m.same(policy,self.bound['records']['key']['readback']['policy']),'whole-key-policy-cli-readback')
        rotation=self._read(cli,'Rotation',None,deadline)
        m.require(rotation.get('KeyRotationEnabled') is False,'new-key-rotation-off')

    def _verify_role(self,cli,role,role_id,deadline,*,inline_present):
        m=self.m;actual=self._read(cli,'Role',role,deadline).get('Role',{})
        duration=next(r['maxSessionDuration'] for r in self.prepared['proposal']['inputs']['firstProvisionPlan']['roles'] if r['name']==role)
        m.require(actual.get('RoleId')==role_id and actual.get('RoleName')==role and actual.get('Arn')==f'arn:aws:iam::{m.ACCOUNT}:role/'+role
            and actual.get('Path')=='/' and type(actual.get('MaxSessionDuration')) is int and actual['MaxSessionDuration']==duration
            and m.same(actual.get('AssumeRolePolicyDocument'),self.bound['documents'][role+'.trust.json'])
            and m.same(actual.get('PermissionsBoundary'),{'PermissionsBoundaryType':'Policy','PermissionsBoundaryArn':m.BOUNDARY})
            and m.fixture_tags_checked(actual.get('Tags'),self.bound['run']['authorizationId']),'whole-current-role-before-write')
        inline=self._read(cli,'Inline',role,deadline);attached=self._read(cli,'Attached',role,deadline)
        m.require(inline.get('IsTruncated') is False and not inline.get('Marker')
            and m.same(inline.get('PolicyNames'),[POLICY_NAME] if inline_present else [])
            and attached.get('IsTruncated') is False and not attached.get('Marker') and attached.get('AttachedPolicies')==[], 'exact-inline-and-no-attachments')
        if inline_present:
            policy=self._read(cli,'Policy',role,deadline)
            m.require(policy.get('RoleName')==role and policy.get('PolicyName')==POLICY_NAME
                and m.same(policy.get('PolicyDocument'),self.bound['documents'][role+'.policy.json']),'whole-inline-before-delete')

    def _owner_context(self,context):
        m=self.m;m.exact(context,{'account','principalARN','source','observedAt','sourceReceipt'})
        m.fresh(context['observedAt'],self.now());source=context['sourceReceipt']
        m.exact(source,{'schema','capturedAt','pageURL','accountHeaderText','principalARN'});m.fresh(source['capturedAt'],self.now())
        m.require(context['account']==m.ACCOUNT and context['principalARN']==self.prepared['records']['owner']['ownerContext']['principalARN']
            and context['source']=='native-owner-console' and source['schema']=='hikyo-158/native-owner-context-projection/v1'
            and source['pageURL']=='https://us-east-1.console.aws.amazon.com/iam/home?region=eu-central-1#/home'
            and source['accountHeaderText']==m.ACCOUNT and source['principalARN']==context['principalARN'],'exact-fresh-owner-context')

    def _historical_absence(self,row,context=None):
        context=context or self.prepared['records']['owner']['ownerContext']
        self.m.historical_absence_checked(row,self.now(),self.m.digest(context))

    def _validate_created(self, operation, role, value, state):
        m = self.m
        if operation == 'CreateOpenIDConnectProvider':
            m.exact(value,{'OpenIDConnectProviderArn','Tags'})
            m.require(value['OpenIDConnectProviderArn']==m.PROVIDER and m.fixture_tags_checked(value['Tags'],self.bound['run']['authorizationId']), 'created-provider-arn-and-tags')
            state['provider'] = m.PROVIDER
        elif operation == 'CreateRole':
            actual = value.get('Role', {});expected_arn = f'arn:aws:iam::{m.ACCOUNT}:role/' + role
            old = set(self.prepared['proposal']['inputs']['firstEpoch']['inventory']['roleIds'].values())
            m.require(type(actual) is dict and actual.get('Arn') == expected_arn and actual.get('RoleName') == role
                      and actual.get('Path') == '/' and type(actual.get('RoleId')) is str
                      and re.fullmatch(r'AROA[A-Z0-9]{8,64}', actual['RoleId']) is not None
                      and actual['RoleId'] not in old | set(state['roles'].values())
                      and m.fixture_tags_checked(actual.get('Tags'),self.bound['run']['authorizationId']), 'new-unique-role-response')
            state['roles'][role] = actual['RoleId']
        else:m.require(value == {}, 'empty-iam-mutation-response')

    def provision(self, cli, proof):
        m = self.m
        with self.ledger.lock():
            head, state = self._state()
            m.require(self.bound is not None and not state['held'] and state['pending'] is None
                      and set(state['owner']) == set(OWNER_OPERATIONS) and state['roles'] == {} and state['provider'] is None
                      and state['boundaryPolicyId'] is not None and state['boundSHA256'] == m.digest(self.bound), 'fresh-provision-phase-only')
            m.require(type(cli) is OfficialFixtureCLI and cli.ledger is self.ledger and cli._active is None
                      and m.same(cli.bound, self.bound), 'exact-closed-official-transport')
            records = self._proof('provision', proof, head);self._active_gate(records)
            m.require(records['ownerStage']['setupPolicy']['defaultVersionId'] == 'v6'
                      and records['boundary']['value']['policyId'] == state['boundaryPolicyId'], 'activated-v6-new-boundary')
            self.ledger.once('phase-provision-started.once.json', {'stateSHA256': head, 'boundSHA256': m.digest(self.bound), 'maximumInvocations': 1})
            cli._active = 'provision';deadline = self.bound['run']['provisioningCutoff']
            try:
                roles = self._identity(cli, deadline)
                m.require(not any(type(row) is not dict or row.get('RoleName','').casefold() in {r.casefold() for r in m.ROLES} for row in roles), 'same-eight-roles-absent')
                m.require(self._read(cli, 'Providers', None, deadline) == {'OpenIDConnectProviderList': []}, 'provider-absent-before-create')
                self._verify_key(cli,deadline)
                for sequence, (operation, role) in enumerate(provision_schedule(m), 1):
                    self._active_gate(records)
                    if operation == 'CreateRole' and not state['roles']:
                        roles = self._identity(cli, deadline)
                        m.require(not any(row.get('RoleName','').casefold() in {r.casefold() for r in m.ROLES} for row in roles), 'fresh-mfa-before-first-role')
                    if operation == 'PutRolePolicy':
                        self._verify_role(cli,role,state['roles'][role],deadline,inline_present=False)
                    head, state = self._mutation(cli, 'provision', sequence, operation, role, head, state, deadline)
                state['phase'] = 'provisioned';self.ledger.cas(head, 'provision-complete', state)
                return copy.deepcopy(state)
            except Exception:
                self._hold('provision-held-no-resume');raise RuntimeError('Provision held; no retry or resume; reconcile preserved custody') from None
            finally:cli._active = None

    def _mutation(self, cli, phase, sequence, operation, role, head, state, deadline):
        m = self.m
        intent = {'attemptTrustBindingSHA256':self.prepared['attemptTrustBindingSHA256'],'schema': 'hikyo-158/next-cli-intent/v1', 'phase': phase, 'sequence': sequence, 'operation': operation,
            'role': role, 'claimId': 'next-' + phase + '-' + str(sequence), 'boundSHA256': m.digest(self.bound), 'stateSHA256': head, 'maximumRequests': 1,
            'argumentsSHA256': m.digest(cli.arguments(operation, role)), 'file': f'{phase}-{sequence:02d}-intent.once.json',
            'transmitFile': f'{phase}-{sequence:02d}-transmit.once.json', 'armedAt': m.stamp(self.now())}
        self.ledger.once(intent['file'], intent);state['pending'] = intent
        head = self.ledger.cas(head, 'cli-intent', state)
        outcome = cli.call(operation, role, timeout=self._timeout(deadline),deadline=deadline, intent=intent)
        # Public returned IDs survive every subsequent validation/CAS/readback.
        self.ledger.once(f'{phase}-{sequence:02d}-response.once.json', {'intentSHA256': m.digest(intent), 'outcome': outcome})
        m.require(outcome['returncode'] == 0 and outcome['stderrEmpty'] is True
            and m.instant(outcome['receivedAt'])<=m.instant(deadline), 'mutation-error-or-deadline-after-forward')
        if phase == 'provision':self._validate_created(operation, role, outcome['value'], state)
        elif operation == 'ScheduleKeyDeletion':
            value = outcome['value']
            m.require(value.get('KeyId') in (self.bound['run']['keyARN'], self.bound['run']['keyARN'].rsplit('/',1)[1])
                      and value.get('KeyState') == 'PendingDeletion' and type(value.get('PendingWindowInDays')) is int
                      and value['PendingWindowInDays'] == 7, 'exact-seven-day-new-key-schedule')
        else:m.require(outcome['value'] == {}, 'empty-retirement-response')
        state['completed'].append({'phase': phase, 'sequence': sequence, 'operation': operation, 'role': role,
            'responseSHA256': m.digest(outcome)})
        state['pending'] = None;head = self.ledger.cas(head, 'cli-custody', state)
        return head, state

    def seal(self, proof):
        m = self.m
        with self.ledger.lock():
            head, state = self._state();m.require(not state['held'] and state['pending'] is None and state['phase'] == 'provisioned'
                and state['sealedSHA256'] is None, 'successful-provision-required-before-seal')
            outer = self._proof('seal', proof, head);m.exact(outer,{'sealingRecords','historicalAbsence'})
            self._historical_absence(outer['historicalAbsence'],self.bound['records']['owner']['ownerContext']);records=outer['sealingRecords']
            m.require(m.same(outer['historicalAbsence'],records['historicalAbsence']),'same-independent-seal-describe-proof')
            sealed = m.seal_actual_inventory(self.bound, records, {k:m.digest(v) for k,v in records.items()}, explicit_now=self.now())
            m.require(m.same(sealed['inventory']['roleIds'], state['roles']) and sealed['inventory']['boundaryPolicyId'] == state['boundaryPolicyId']
                and sealed['inventory']['keyARN'] == state['keyARN'], 'sealed-actual-identities-match-durable-responses')
            self.ledger.once('phase-seal-started.once.json', {'boundSHA256':m.digest(self.bound),'maximumInvocations':1})
            self._seal_crosslinks(records)
            # Authentication and readback may cross a withdrawal or deadline.
            # Re-admit after those boundaries before publishing a new seal.
            m.admit_current_release(self.prepared,self.now())
            now=self.now();m.clock(now)
            m.require(m.instant(self.prepared['lease']['start'])<=now<m.instant(self.prepared['lease']['until'])
                and m.instant(self.bound['run']['start'])<=now<m.instant(self.bound['run']['provisioningCutoff'])
                and now+timedelta(minutes=65)<=m.instant(self.bound['run']['authUntil']), 'seal-adjacent-original-deadline')
            self.ledger.once('sealed-model.once.json', sealed)
            state['sealedSHA256'] = m.digest(sealed);state['phase'] = 'sealed-model-only'
            self.ledger.cas(head, 'sealed', state)
            return sealed

    def _seal_crosslinks(self, records):
        m=self.m;rows=records['custody']['operations']
        for index,operation in enumerate(('DeletePolicyVersion','CreatePolicyVersion','CreatePolicy')):
            intent=self.ledger.read_once('owner-'+operation+'-intent.once.json')
            response=self.ledger.read_once('owner-'+operation+'-response.once.json')
            row=rows[index]
            m.require(row['claimId']==intent['claimId'] and m.same(row['request'],intent['request'])
                and m.same(row['response'],response['response']) and row['sentAt']==response['sentAt']
                and row['receivedAt']==response['receivedAt'], 'owner-model-custody-crosslinks')
        for sequence,(operation,role) in enumerate(provision_schedule(m),1):
            intent=self.ledger.read_once(f'provision-{sequence:02d}-intent.once.json')
            response=self.ledger.read_once(f'provision-{sequence:02d}-response.once.json')['outcome']['value']
            projected=({'Arn':response['Role']['Arn'],'RoleId':response['Role']['RoleId'],'RoleName':response['Role']['RoleName']}
                if operation=='CreateRole' else response)
            row=rows[sequence+2]
            m.require(row['claimId']==intent['claimId'] and row['operation']==operation
                and m.same(row['response'],projected)
                and row['sentAt']==self.ledger.read_once(f'provision-{sequence:02d}-response.once.json')['outcome']['sentAt']
                and row['receivedAt']==self.ledger.read_once(f'provision-{sequence:02d}-response.once.json')['outcome']['receivedAt'], 'cli-model-custody-crosslinks')

    def remaining_cleanup(self):
        """Read-only diagnostic. Never grants a second invocation or resumes writes."""
        with self.ledger.lock():
            _,state=self._state()
            complete={(r['operation'],r['role']) for r in state['completed'] if r['phase']=='retire'}
            return {'schema':'hikyo-158/next-remaining-cleanup/v1','runtimeReady':False,'executionAuthorityGranted':False,
                'preservedRoles':copy.deepcopy(state['roles']),'keyARN':state['keyARN'],'boundaryPolicyId':state['boundaryPolicyId'],
                'pending':copy.deepcopy(state['pending']),'capturedPublicResponses':self._captured_responses(),
                'remainingSlots':self._remaining_slots(complete),
                'requiresIndependentReconciliation':True,'automaticRetry':False}

    def _captured_responses(self):
        records=[]
        for path in sorted(self.ledger.folder.glob('*-response.once.json')):
            record=self.ledger.read_once(path.name)
            records.append({'responseFile':path.name,'publicResponse':record.get('outcome',record.get('response'))})
        self.m.require(len(records)<=100,'finite-public-custody-diagnostics')
        return records

    def _remaining_slots(self,complete):
        remaining=[]
        for sequence,(operation,role) in enumerate(retirement_schedule(self.m),1):
            if (operation,role) in complete:continue
            try:
                record=self.ledger.read_once(f'retire-{sequence:02d}-absent.once.json')
            except FileNotFoundError:
                remaining.append({'operation':operation,'role':role});continue
            self.m.require(record['boundSHA256']==self.m.digest(self.bound) and record['operation']==operation and record['role']==role,'absence-slot-crosslink')
        return remaining

    def _secret_absence(self, proof):
        m = self.m;m.exact(proof, {'schema','observedAt','artifact','absenceRecords','historicalAbsence'})
        m.require(proof['schema'] == 'hikyo-158/next-secret-absence/v1', 'new-run-secret-proof-schema');m.fresh(proof['observedAt'], self.now())
        artifact = proof['artifact'];m.exact(artifact, {'runId','runAttempt','candidateSHA','workflowSHA','prefix','terminalConclusion','creationAttempts','createdSecretARNs'})
        m.require(artifact['runId'] == self.bound['run']['runId'] and type(artifact['runAttempt']) is int and artifact['runAttempt'] == 1
            and artifact['candidateSHA'] == self.bound['run']['candidateSHA'] and artifact['workflowSHA'] == self.bound['run']['workflowSHA']
            and artifact['prefix'] == self.bound['run']['prefix'] and artifact['terminalConclusion'] in ('success','failure','cancelled')
            and type(artifact['creationAttempts']) is int and 0 <= artifact['creationAttempts'] <= 32
            and type(artifact['createdSecretARNs']) is list and len(artifact['createdSecretARNs']) <= artifact['creationAttempts'], 'terminal-new-run-artifact')
        arns = artifact['createdSecretARNs'];m.require(len(set(arns)) == len(arns), 'unique-created-secret-arns')
        for arn in arns:m.require(type(arn) is str and re.fullmatch(r'arn:aws:secretsmanager:'+m.REGION+':'+m.ACCOUNT+r':secret:'+re.escape(self.bound['run']['prefix'])+r'[A-Za-z0-9/_+=.@-]{1,300}-[A-Za-z0-9]{6}', arn) is not None, 'exact-new-run-secret-namespace')
        m.require(type(proof['absenceRecords']) is list and len(proof['absenceRecords']) == len(arns), 'every-created-secret-absent')
        self._historical_absence(proof['historicalAbsence'],self.bound['records']['owner']['ownerContext'])
        rows = proof['absenceRecords']
        seen = []
        for row in rows:
            m.exact(row, {'arn','operation','errorCode','httpStatus','observedAt','requestId','sourceReceiptSHA256'})
            m.require(row['operation'] == 'DescribeSecret' and row['errorCode'] == 'ResourceNotFoundException'
                and type(row['httpStatus']) is int and row['httpStatus'] == 400 and type(row['requestId']) is str and 8 <= len(row['requestId']) <= 128
                and m.sha(row['sourceReceiptSHA256']), 'exact-provider-secret-absence')
            m.fresh(row['observedAt'], self.now());seen.append(row['arn'])
        m.require(set(seen) == set(arns), 'no-unrecorded-or-historical-delete')

    def retire(self, cli, proof):
        """One cleanup invocation, including partial provision; no provision resumes.

        Exact absent slots may skip without a mutation. Ambiguous created roles
        require independently authenticated immutable-ID reconciliation first.
        Owner boundary deletion and existing-v5 restoration remain external.
        """
        m = self.m
        with self.ledger.lock():
            head, state = self._state();m.require(self.bound is not None and state['boundSHA256'] == m.digest(self.bound), 'bound-cleanup-only')
            m.require(type(cli) is OfficialFixtureCLI and cli.ledger is self.ledger and cli._active is None and m.same(cli.bound,self.bound), 'closed-cleanup-transport')
            records = self._proof('retire', proof, head)
            m.exact(records, {'secretAbsence','inventory','ownerStage','ownerContext','historicalAbsence','oldKeys','newKey','boundary','code','budget'})
            self._secret_absence(records['secretAbsence'])
            self._retire_gate(records, state)
            budget_diagnostic=self._cleanup_budget(records['budget'])
            self.ledger.once('retirement-reconciled-inventory.once.json',{'boundSHA256':m.digest(self.bound),'proofSHA256':m.digest(proof),'inventory':records['inventory']})
            self.ledger.once('phase-retire-started.once.json', {'stateSHA256':head,'boundSHA256':m.digest(self.bound),'maximumInvocations':1,'budgetDiagnostic':budget_diagnostic})
            cli._active = 'retire';deadline = self.bound['run']['cleanupUntil']
            state['pending'] = None
            try:
                self._identity(cli, deadline)
                for sequence,(operation,role) in enumerate(retirement_schedule(m),1):
                    self._retire_gate(records,state)
                    current = records['inventory']['roles'][role] if role else None
                    absent = (operation == 'DeleteRolePolicy' and current is not None and current['inlinePolicyNames'] == []) or (role is not None and current is None)
                    absent = absent or (operation == 'DeleteOpenIDConnectProvider' and records['inventory']['provider'] is None)
                    if absent:
                        self.ledger.once(f'retire-{sequence:02d}-absent.once.json', {'boundSHA256':m.digest(self.bound),'operation':operation,'role':role,'proofSHA256':m.digest(proof)})
                        continue
                    if role:
                        self._verify_role(cli,role,current['roleId'],deadline,inline_present=(operation=='DeleteRolePolicy'))
                    elif operation=='DeleteOpenIDConnectProvider':
                        provider=self._read(cli,'Provider',None,deadline)
                        m.require(provider.get('Url')=='token.actions.githubusercontent.com' and provider.get('ClientIDList')==['sts.amazonaws.com']
                            and m.same(provider.get('ThumbprintList'),cli.thumbprints) and m.fixture_tags_checked(provider.get('Tags'),self.bound['run']['authorizationId']),'exact-provider-before-delete')
                    elif operation=='ScheduleKeyDeletion':
                        roles=self._read(cli,'Roles',None,deadline)
                        m.require(roles.get('IsTruncated') is False and not roles.get('Marker') and type(roles.get('Roles')) is list
                            and not any(row.get('RoleName','').casefold() in {r.casefold() for r in m.ROLES} for row in roles['Roles'])
                            and self._read(cli,'Providers',None,deadline)=={'OpenIDConnectProviderList':[]},'all-owned-iam-absent-before-key')
                        self._verify_key(cli,deadline)
                    head,state = self._mutation(cli,'retire',sequence,operation,role,head,state,deadline)
                state['phase'] = 'retirement-writes-complete-await-independent-readback'
                self.ledger.cas(head,'retirement-writes-complete',state)
                return copy.deepcopy(state)
            except Exception:
                self._hold('retirement-held-no-resume');raise RuntimeError('Retirement held; preserve exact remaining cleanup, no retry') from None
            finally:cli._active = None

    def _cleanup_boundary(self,proof,policy_id):
        m=self.m;m.exact(proof,{'observedAt','value'});m.fresh(proof['observedAt'],self.now())
        value=proof['value'];m.exact(value,{'arn','policyId','defaultVersionId','versions'})
        m.require(type(policy_id) is str and value['policyId']==policy_id and value['arn']==m.BOUNDARY
            and value['defaultVersionId']=='v1' and m.same(value['versions'],{'v1':{'isDefault':True,
                'document':self.bound['documents']['setup-role-boundary.json']}}),'cleanup-whole-new-boundary')

    def _cleanup_budget(self,proof):
        """Budget alarms stop creation, never already-authorized exact cleanup.

        No unavailable, stale or over-cap record is called a successful cost
        check. Identity/provenance and cleanup authority remain separate gates.
        """
        m=self.m
        result={'schema':'hikyo-158/cleanup-budget-diagnostic/v1','status':'unavailable','grossUSD':None,
            'targetExceeded':None,'absoluteExceeded':None,'newSpendingAuthorized':False,'recordSHA256':None}
        if proof is None:return result
        try:
            result['recordSHA256']=m.digest(proof)
            m.exact(proof,{'schema','observedAt','currency','grossCharges','completionReserves','targetUSD','absoluteMaximumUSD','bankedBilling'})
            m.require(proof['schema']=='hikyo-158/next-gross-budget/v1' and proof['currency']=='USD'
                and proof['targetUSD']=='5.00' and proof['absoluteMaximumUSD']=='10.00'
                and type(proof['grossCharges']) is list and 1<=len(proof['grossCharges'])<=32,'public-cleanup-billing-record')
            gross=m.money('0.00');timestamps=[proof['observedAt']]
            for row in proof['grossCharges']:
                m.exact(row,{'service','amountUSD','sourceRecord'});source=row['sourceRecord']
                m.exact(source,{'schema','source','observedAt','account','period','currency','service','grossDisplayedUSD'})
                m.require(source['schema']=='hikyo-158/native-gross-charge-projection/v1' and source['source']=='native-billing-console'
                    and source['account']==m.ACCOUNT and source['currency']=='USD' and source['service']==row['service']
                    and source['grossDisplayedUSD']==row['amountUSD'],'public-cleanup-gross-source')
                gross+=m.money(row['amountUSD']);timestamps.append(source['observedAt'])
            billable,_=m.banked_billing_checked(proof['bankedBilling'],gross,self.now(),fresh_required=False)
            timestamps.append(proof['bankedBilling']['observedAt'])
            m.require(all(m.instant(value)<=self.now() for value in timestamps),'no-future-cleanup-cost')
            result.update(status='fresh' if all(self.now()-m.instant(value)<=timedelta(minutes=5) for value in timestamps) else 'stale',
                grossUSD=format(gross,'.2f'),targetExceeded=gross>m.money('5.00'),
                absoluteExceeded=gross>m.money('10.00') or billable>=m.money('15.00'))
        except (ValueError,TypeError,KeyError):
            result['status']='unavailable'
        return result

    def _retire_gate(self, records, state):
        m = self.m
        m.require(m.instant(self.bound['run']['start']) <= self.now() < m.instant(self.bound['run']['cleanupUntil']) <= m.instant(m.HORIZON), 'finite-cleanup-deadline')
        m.require(cleanup_code_checked(m,self.bound,records['code'],self.now())==
            {'candidate':self.prepared['candidateSHA'],'workflow':self.prepared['workflowSHA']},'cleanup-unchanged-dispatched-source')
        self._owner_context(records['ownerContext']);self._historical_absence(records['historicalAbsence'],records['ownerContext'])
        stage=records['ownerStage'];m.exact(stage,{'observedAt','setupUser','setupPolicy'});m.fresh(stage['observedAt'],self.now())
        m.require(m.same(stage['setupUser'],self.prepared['records']['owner']['setupUser']), 'cleanup-retains-setup-mfa')
        m.setup_policy_checked(stage['setupPolicy'],self.prepared['proposal']['inputs']['iamClosure']['setupPolicy'],['v2','v3','v4','v5','v6'],'v6',self.bound['documents'])
        m.require(m.same(records['oldKeys'],self.prepared['records']['owner']['oldKeys']) and m.same(records['newKey'],self.bound['records']['key']['readback']), 'cleanup-preserves-four-old-keys')
        self._cleanup_boundary(records['boundary'],state['boundaryPolicyId'])
        inventory=records['inventory'];m.exact(inventory,{'observedAt','roles','provider','keyARN','boundaryPolicyId'})
        m.fresh(inventory['observedAt'],self.now());m.exact(inventory['roles'],m.ROLES)
        m.require(inventory['keyARN']==state['keyARN'] and inventory['boundaryPolicyId']==state['boundaryPolicyId'], 'cleanup-new-key-boundary-ids')
        old=set(self.prepared['proposal']['inputs']['firstEpoch']['inventory']['roleIds'].values());seen=[]
        for role,row in inventory['roles'].items():
            if row is None:continue
            m.exact(row,{'roleId','inlinePolicyNames','attachedPolicyARNs','trust','inlinePolicies','boundaryARN'})
            m.require(type(row['roleId']) is str and re.fullmatch(r'AROA[A-Z0-9]{8,64}',row['roleId']) is not None and row['roleId'] not in old
                and (role not in state['roles'] or row['roleId']==state['roles'][role]) and row['attachedPolicyARNs']==[]
                and row['boundaryARN']==m.BOUNDARY and m.same(row['trust'],self.bound['documents'][role+'.trust.json'])
                and row['inlinePolicyNames'] in ([],[POLICY_NAME])
                and m.same(row['inlinePolicies'],{} if row['inlinePolicyNames']==[] else {POLICY_NAME:self.bound['documents'][role+'.policy.json']}), 'exact-surviving-role-custody')
            if role not in state['roles']:
                sequence=next(i for i,(op,name) in enumerate(provision_schedule(m),1) if op=='CreateRole' and name==role)
                intent=self.ledger.read_once(f'provision-{sequence:02d}-intent.once.json')
                transmit=self.ledger.read_once(f'provision-{sequence:02d}-transmit.once.json')
                m.require(intent['operation']=='CreateRole' and intent['role']==role and transmit['intentSHA256']==m.digest(intent),'ambiguous-role-requires-burned-create-custody')
            seen.append(row['roleId'])
        m.require(len(set(seen))==len(seen) and inventory['provider'] in (None,m.PROVIDER), 'cleanup-distinct-fixture-identities')
        if inventory['provider'] is not None and state['provider'] is None:
            intent=self.ledger.read_once('provision-01-intent.once.json');transmit=self.ledger.read_once('provision-01-transmit.once.json')
            m.require(intent['operation']=='CreateOpenIDConnectProvider' and transmit['intentSHA256']==m.digest(intent),'ambiguous-provider-requires-burned-create-custody')


if __name__ == '__main__':
    raise SystemExit('SOURCE ONLY: execution refused; reviewed live adapters and Root authority are absent')
