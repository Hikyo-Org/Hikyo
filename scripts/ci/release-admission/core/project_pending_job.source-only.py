"""Pure pending-job projection. No I/O, clock read, transport, auth or dispatch.

Root must authenticate each complete original request/response and sole actual
forward before releasing this projection through the existing capture registry.
Hashes and this parser provide integrity/shape checks, never Root authority.
"""
import copy
import hashlib

API_VERSION = '2026-03-10'
MODEL_SHA256 = 'f40c07b2204c3305b434836015c99f30af4dfc893a0ce0e4f0318c17ea844a19'
WORKFLOW_PATH = '.github/workflows/aws-acceptance.yml'
REPOSITORY = 'Hikyo-Org/Hikyo'
ROOT = '/repos/' + REPOSITORY
ENVIRONMENT = 'hikyo-158-aws-sandbox'
ENV_PATH = ROOT + '/environments/' + ENVIRONMENT
API_ROOT = 'https://api.github.com'
WEB_ROOT = 'https://github.com/' + REPOSITORY
CAPTURE_KEYS = {'request', 'status', 'rawBody', 'sentAt', 'receivedAt', 'original', 'pagination'}
GET_KEYS = {'runBefore', 'runAfter', 'jobs', 'pending', 'reviews', 'environment', 'variables', 'branches', 'oidcDefaults'}


def _positive(m, value):
    m.require(type(value) is int and 0 < value < 10**25, 'pending-positive-provider-id')
    return value


def _capture(m, row, method, path, body, now, *, pagination=None, historical=False):
    m.exact(row, CAPTURE_KEYS)
    request = row['request'];m.exact(request, {'method', 'path', 'apiVersion', 'body'})
    m.require(request == {'method': method, 'path': path, 'apiVersion': API_VERSION, 'body': body}
              and type(row['status']) is int and row['status'] == 200, 'pending-exact-supported-request-response')
    m.require(m.same(row['pagination'], pagination), 'pending-original-pagination-metadata')
    if not historical:
        m.fresh(row['sentAt'], now);m.fresh(row['receivedAt'], now)
    m.require(m.instant(row['sentAt']) <= m.instant(row['receivedAt']) <= now, 'pending-actual-response-order')
    raw = row['rawBody']
    m.require(type(raw) is str and 0 < len(raw.encode()) <= 500_000, 'pending-bounded-whole-original-body')
    original = row['original'];m.exact(original, {'toolName', 'toolCallId', 'toolResult', 'responsePath'})
    m.require(type(original['toolName']) is str and 0 < len(original['toolName']) <= 256
              and type(original['toolCallId']) is str and 8 <= len(original['toolCallId']) <= 256
              and type(original['toolResult']) in (dict, list) and type(original['responsePath']) is list
              and len(original['responsePath']) <= 32, 'pending-whole-original-tool-custody')
    projected = original['toolResult']
    for key in original['responsePath']:
        m.require((type(projected) is dict and type(key) is str and key in projected)
                  or (type(projected) is list and type(key) is int and 0 <= key < len(projected)), 'pending-original-response-path')
        projected = projected[key]
    m.require(type(projected) is str and projected == raw, 'pending-original-raw-body-string-equality')
    value = m.strict_json(raw.encode())
    m.require(type(value) in (dict, list), 'pending-public-json-body')
    return value


def _pages(m, rows, path, field, now):
    m.require(type(rows) is list and 1 <= len(rows) <= 4, 'pending-complete-finite-pages')
    values = [];total = None
    for i, row in enumerate(rows, 1):
        pg = row['pagination'];m.exact(pg, {'page', 'perPage', 'nextPage'})
        m.require(type(pg['page']) is int and pg['page'] == i and type(pg['perPage']) is int and pg['perPage'] == 100
                  and (pg['nextPage'] is None if i == len(rows) else type(pg['nextPage']) is int and pg['nextPage'] == i + 1),
                  'pending-unbroken-terminal-pagination')
        value = _capture(m, row, 'GET', path + f'?per_page=100&page={i}', None, now, pagination=pg)
        m.exact(value, {'total_count', field})
        m.require(type(value['total_count']) is int and 0 <= value['total_count'] <= 400
                  and type(value[field]) is list and len(value[field]) <= 100, 'pending-exact-page-count-types')
        if total is None:total = value['total_count']
        m.require(total == value['total_count'], 'pending-page-total-stable')
        values.extend(value[field])
    m.require(len(values) == total, 'pending-complete-total-count')
    return values


def _run(m, value, prepared, run_id):
    m.require(type(value) is dict and {'id','run_attempt','status','conclusion','head_sha','head_branch','event','path','name','url','html_url','repository','workflow_id','created_at'} <= set(value) and value.get('id') == run_id and type(value.get('id')) is int
              and type(value.get('run_attempt')) is int and value['run_attempt'] == 1
              and value.get('status') == 'waiting' and value.get('conclusion') is None
              and value.get('head_sha') == prepared['workflowSHA'] and value.get('head_branch') == 'main'
              and value.get('event') == 'workflow_dispatch' and value.get('path') == WORKFLOW_PATH
              and value.get('name') == 'aws-acceptance' and value.get('url') == API_ROOT + ROOT + f'/actions/runs/{run_id}'
              and value.get('html_url') == WEB_ROOT + f'/actions/runs/{run_id}', 'pending-exact-unapproved-run')
    repo = value.get('repository');m.require(type(repo) is dict and type(repo.get('id')) is int and repo['id'] == 1316165429
        and repo.get('full_name') == REPOSITORY and type(repo.get('owner')) is dict
        and type(repo['owner'].get('id')) is int and repo['owner']['id'] == 316726515, 'pending-original-repository-identity')
    # Current GET may omit inputs or return null. Never infer the candidate there.
    if value.get('inputs') is not None:
        m.require(m.same(value['inputs'], {'reviewed_sha': prepared['candidateSHA']}), 'pending-no-contradictory-returned-inputs')
    _positive(m, value.get('workflow_id'))
    return {key: copy.deepcopy(value[key]) for key in ('id', 'run_attempt', 'head_sha', 'head_branch', 'event', 'path', 'workflow_id')}


def _project(m, prepared, dispatch_contract, dispatch_capture, captures, *, explicit_now, bound=None):
    """Return inert job/custody projections from complete actual capture inputs.

    No caller verification boolean is accepted. Root must independently verify
    original transport/status/API-version/pagination and immutable ledger ticket.
    `pagination.nextPage` is reviewed direct transport metadata, not a guess.
    """
    m.clock(explicit_now);m.preparation_checked(prepared);m.exact(captures, GET_KEYS)
    if bound is not None:
        m.bound_checked(bound, explicit_now)
        m.require(m.same(bound['preparation'], prepared), 'pending-same-original-bound-preparation')
    expected_variables = [] if bound is None else m.environment_variables(bound['run'])
    contract = dispatch_contract
    m.exact(contract, {'operation', 'endpoint', 'request', 'intentSHA256', 'ticket', 'maximumForwards', 'automaticRetry',
                       'runtimeReady', 'executionAuthorityGranted', 'requiredExternalSteps','attemptTrustBindingSHA256'})
    expected_request = {'ref': 'main', 'inputs': {'reviewed_sha': prepared['candidateSHA']}}
    dispatch_path = ROOT + '/actions/workflows/aws-acceptance.yml/dispatches'
    m.require(contract['operation'] == 'GitHubDispatch' and contract['endpoint'] == API_ROOT + dispatch_path
        and m.same(contract['request'], expected_request) and m.sha(contract['intentSHA256'])
        and type(contract['maximumForwards']) is int and contract['maximumForwards'] == 1
        and contract['automaticRetry'] is False and contract['runtimeReady'] is False and contract['executionAuthorityGranted'] is False,
        'pending-original-single-dispatch-contract')
    ticket = contract['ticket'];m.exact(ticket, {'intentSHA256', 'maximumRequests', 'issuedAt','attemptTrustBindingSHA256'})
    m.require(contract['attemptTrustBindingSHA256']==ticket['attemptTrustBindingSHA256']==prepared['attemptTrustBindingSHA256'],'pending-same-trust-binding')
    m.require(ticket['intentSHA256'] == contract['intentSHA256'] and type(ticket['maximumRequests']) is int and ticket['maximumRequests'] == 1
        and m.instant(prepared['derivedAt']) <= m.instant(ticket['issuedAt']) <= m.instant(dispatch_capture['sentAt']), 'pending-original-dispatch-ticket-order')
    result = _capture(m, dispatch_capture, 'POST', dispatch_path, expected_request, explicit_now, historical=bound is not None)
    m.exact(result, {'workflow_run_id', 'run_url', 'html_url'})
    run_id = _positive(m, result['workflow_run_id']);run_path = ROOT + f'/actions/runs/{run_id}'
    m.require(result['run_url'] == API_ROOT + run_path and result['html_url'] == WEB_ROOT + f'/actions/runs/{run_id}', 'pending-native-post-returned-run-id')
    if bound is not None:
        m.require(str(run_id) == bound['run']['runId'] and run_id == bound['records']['job']['run']['id']
            and m.same(bound['records']['job']['run']['inputs'], expected_request['inputs']), 'pending-original-bound-dispatch-crosslinks')
    before = _capture(m, captures['runBefore'], 'GET', run_path, None, explicit_now)
    run_projection = _run(m, before, prepared, run_id)
    m.require(m.instant(dispatch_capture['receivedAt']) <= m.instant(captures['runBefore']['sentAt']), 'pending-get-after-accepted-dispatch')
    # GitHub created_at has whole seconds. Compare its actual timestamp against
    # the send second without rounding/rewriting the captured observation times.
    created = m.instant(before['created_at'])
    m.require(m.instant(dispatch_capture['sentAt']).replace(microsecond=0) <= created <= m.instant(captures['runBefore']['receivedAt']),
        'pending-provider-created-after-original-dispatch-second')
    jobs = _pages(m, captures['jobs'], run_path + '/attempts/1/jobs', 'jobs', explicit_now)
    variables = _pages(m, captures['variables'], ENV_PATH + '/variables', 'variables', explicit_now)
    branches = _pages(m, captures['branches'], ENV_PATH + '/deployment-branch-policies', 'branch_policies', explicit_now)
    env = _capture(m, captures['environment'], 'GET', ENV_PATH, None, explicit_now)
    oidc = _capture(m, captures['oidcDefaults'], 'GET', ROOT + '/actions/oidc/customization/sub', None, explicit_now)
    pending = _capture(m, captures['pending'], 'GET', run_path + '/pending_deployments', None, explicit_now)
    reviews = _capture(m, captures['reviews'], 'GET', run_path + '/approvals', None, explicit_now)
    projected_variables = []
    for variable in variables:
        m.require(type(variable) is dict and {'name', 'value'} <= set(variable) <= {'name', 'value', 'created_at', 'updated_at'}
            and type(variable['name']) is str and type(variable['value']) is str
            and all(type(variable[k]) is str for k in ('created_at', 'updated_at') if k in variable), 'pending-whole-variable-metadata')
        projected_variables.append({k: variable[k] for k in ('name', 'value')})
    m.require(len({v['name'] for v in projected_variables}) == len(projected_variables), 'pending-no-duplicate-variables')
    projected_variables.sort(key=lambda row: row['name'])
    m.require(reviews == [] and m.same(projected_variables, expected_variables), 'pending-no-approval-and-exact-stage-variables')
    expected_env = copy.deepcopy(prepared['records']['owner']['environment']);expected_env['variables'] = expected_variables
    m.require(type(env) is dict and set(expected_env['configuration']) <= set(env), 'pending-whole-environment-response')
    configuration = {key: copy.deepcopy(env[key]) for key in expected_env['configuration']}
    projected_branches = []
    for branch in branches:
        m.require(type(branch) is dict and {'id', 'node_id', 'name', 'type'} == set(branch) and type(branch['id']) is int,
                  'pending-whole-branch-policy')
        projected_branches.append({key: branch[key] for key in ('name', 'type')})
    environment = {'id': env['id'], 'configuration': configuration, 'branchPolicies': projected_branches, 'OIDCDefaults': oidc, 'variables': projected_variables}
    m.require(type(env['id']) is int and m.same(environment, expected_env), 'pending-unchanged-whole-protected-environment')
    m.require(type(pending) is list and len(pending) == 1, 'pending-one-required-environment')
    pending_env = pending[0];m.exact(pending_env, {'environment', 'wait_timer', 'wait_timer_started_at', 'reviewers', 'current_user_can_approve'})
    e = pending_env['environment'];m.exact(e, {'id', 'node_id', 'name', 'url', 'html_url'})
    m.require(type(e['id']) is int and e['id'] == m.ENV_ID and e['name'] == ENVIRONMENT
        and e['url'] == API_ROOT + ENV_PATH and e['html_url'] == WEB_ROOT + '/deployments/activity_log?environments_filter=' + ENVIRONMENT
        and type(pending_env['wait_timer']) is int and pending_env['wait_timer'] == 0 and pending_env['wait_timer_started_at'] is None
        and type(pending_env['current_user_can_approve']) is bool, 'pending-protected-review-state')
    def reviewer_ids(rows):
        m.require(type(rows) is list, 'pending-reviewer-list');out = []
        for row in rows:
            m.exact(row, {'type', 'reviewer'});r = row['reviewer']
            m.require(row['type'] == 'User' and type(r) is dict and type(r.get('id')) is int and type(r.get('login')) is str, 'pending-reviewer-identity')
            out.append((row['type'], r['id'], r['login']))
        m.require(len(out) == len(set(out)), 'pending-no-duplicate-reviewers');return out
    expected_reviewers = [r['reviewers'] for r in configuration['protection_rules'] if r['type'] == 'required_reviewers']
    m.require(len(expected_reviewers) == 1 and reviewer_ids(pending_env['reviewers']) == reviewer_ids(expected_reviewers[0]), 'pending-required-human-reviewers')
    from workflow_contract import single_sandbox_checked
    workflow = prepared['records']['code']['workflowFiles'][WORKFLOW_PATH]
    workflow_pin=prepared['proposal']['sourceManifest']['workflowFileSHA256s'][WORKFLOW_PATH]
    m.require(type(workflow) is str and hashlib.sha256(workflow.encode()).hexdigest()==workflow_pin,
        'pending-exact-approved-release-workflow')
    single_sandbox_checked(m,workflow)
    if jobs:
        m.require(len(jobs) == 1, 'pending-only-sandbox-provider-job')
        job = jobs[0]
        m.require(type(job) is dict and {'id','name','run_id','run_attempt','status','conclusion','started_at','completed_at','runner_id','steps'} <= set(job) and type(job.get('id')) is int and job['id'] > 0 and job.get('name') == 'sandbox'
            and job.get('run_id') == run_id and type(job.get('run_id')) is int and type(job.get('run_attempt')) is int and job['run_attempt'] == 1
            and job.get('status') == 'waiting' and job.get('conclusion') is None and job.get('started_at') is None
            and job.get('completed_at') is None and (job.get('runner_id') is None or type(job.get('runner_id')) is int and job['runner_id'] == 0)
            and job.get('steps') == [] and (job.get('head_sha') is None or job['head_sha'] == prepared['workflowSHA']),
            'pending-never-started-waiting-provider-job')
        mode = 'actual-unstarted-provider-sandbox-job'
    else:
        mode = 'trusted-single-job-pending-environment'
    after = _capture(m, captures['runAfter'], 'GET', run_path, None, explicit_now)
    m.require(m.same(_run(m, after, prepared, run_id), run_projection) and before['created_at'] == after['created_at'], 'pending-no-run-attempt-head-or-state-drift')
    all_get = [captures[k] for k in ('runBefore', 'pending', 'reviews', 'environment', 'oidcDefaults')] + captures['jobs'] + captures['variables'] + captures['branches']
    m.require(all(m.instant(captures['runBefore']['sentAt']) <= m.instant(c['sentAt']) <= m.instant(c['receivedAt']) <= m.instant(captures['runAfter']['sentAt']) for c in all_get),
              'pending-complete-read-bracket')
    observed_at = min((c['sentAt'] for c in all_get), key=m.instant)
    logical_job = {'name': 'sandbox', 'status': 'waiting', 'conclusion': None, 'run_id': run_id, 'run_attempt': 1}
    job_proof = {'schema': 'hikyo-158/next-pending-hosted-job/v1', 'observedAt': observed_at,
        'run': {'id': run_id, 'run_attempt': 1, 'head_sha': prepared['workflowSHA'], 'head_branch': 'main', 'event': 'workflow_dispatch',
                'path': WORKFLOW_PATH, 'repositoryId': 1316165429, 'ownerId': 316726515, 'inputs': copy.deepcopy(expected_request['inputs'])},
        'jobs': [logical_job], 'environment': environment, 'deploymentReviews': []}
    m.pending_job_checked(job_proof, prepared, explicit_now, expected_variables, None if bound is None else bound['run']['runId'])
    custody = {'intentSHA256': contract['intentSHA256'], 'forwardsObserved': 1, 'sentAt': dispatch_capture['sentAt'],
               'receivedAt': dispatch_capture['receivedAt'], 'runId': run_id}
    return {'schema': 'hikyo-158/pending-readonly-projection/v1', 'job': job_proof, 'dispatchCustody': custody,
        'provenance': {'modelSHA256': MODEL_SHA256, 'workflowBodySHA256': workflow_pin, 'jobProjectionMode': mode,
            'actualRESTJobCount': len(jobs), 'phase': 'initial-bind' if bound is None else 'seal-reobservation',
            'boundPlanSHA256': None if bound is None else m.digest(bound), 'originalDispatchObservationRefreshed': False, 'inputsSource': 'exact-original-accepted-POST-body-not-run-GET',
            'runIdSource': 'exact-original-HTTP200-workflow_run_id-not-discovery', 'dispatchContractSHA256': m.digest(contract),
            'dispatchCaptureSHA256': m.digest(dispatch_capture), 'getCapturesSHA256': m.digest(captures),
            'paginationAuthority': 'Root must authenticate direct transport pagination metadata',
            'requiresExternalOriginalAndOnceForwardAuthentication': True},
        'originalDispatchContract': copy.deepcopy(contract), 'originalDispatchCapture': copy.deepcopy(dispatch_capture), 'originalGetCaptures': copy.deepcopy(captures),
        'externalAuthenticityVerifiedByProjector': False, 'providerAPIsCalledByProjector': False, 'executionAuthorityGranted': False, 'runtimeReady': False}


def project_pending(m, prepared, dispatch_contract, dispatch_capture, captures, *, explicit_now):
    """Initial bind: fresh POST200 custody, fresh GETs, empty environment vars."""
    return _project(m, prepared, dispatch_contract, dispatch_capture, captures, explicit_now=explicit_now)


def project_seal_pending(m, bound, original_dispatch_contract, original_dispatch_capture, fresh_captures, *, explicit_now):
    """Same actual bound run: fresh GETs/exact ten vars, unchanged historic POST.

    Root authenticates the original dispatch and immutable bound custody. This
    does not redispatch, rebind, refresh original observations or grant authority.
    """
    return _project(m, bound['preparation'], original_dispatch_contract, original_dispatch_capture,
                    fresh_captures, explicit_now=explicit_now, bound=bound)
