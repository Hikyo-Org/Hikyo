"""Closed K command derivation and receipt validation, never authentication.

Root independently reviews/releases the source/runtime binding and registry.
This module does not discover or authenticate them, execute a command, inspect
a profile, or grant runtime/provider authority. F runs only after native verify.
"""

from . import adapter_context
import copy
import hashlib
from pathlib import Path, PurePosixPath
import re
import shlex
import types

HERE = Path(__file__).resolve().parent
BASE = 'https://eu-central-1.console.aws.amazon.com/kms/home?region=eu-central-1'
OLD = ('10908b8d-fe54-40e3-9210-a3eefea709b6', '990c479f-d673-4bc9-8bca-7b9b86f4b205',
       'fa374267-a456-4e79-b5b1-6cdf356b7d0e', 'bfa44dc1-ee8a-43b8-aec7-d8a7fbb38db1')
IDENTITY = {'targetId', 'targetType', 'targetURL', 'browserContextId', 'mainFrameId',
            'loaderId', 'frameURL', 'frameURLRelation'}
RESPONSE_KEYS = {'schema', 'ownerId', 'sourceSHA256', 'reviewSHA256', 'requestId', 'sequence',
                 'accepted', 'ok', 'nextSequence', 'state', 'ownerStillAlive', 'ownerClosed',
                 'result', 'error', 'responseSHA256', 'clientChannelClosed', 'cliCaptureSuppressed',
                 'coreEphemeralProtocolMetadataSuppressed', 'controlLeaseClaimed',
                 'exclusiveControlVerifiedByAdapter', 'ownerProcessExitVerified'}
STDOUT_ONLY = {'responseSHA256', 'clientChannelClosed', 'cliCaptureSuppressed',
               'coreEphemeralProtocolMetadataSuppressed', 'controlLeaseClaimed',
               'exclusiveControlVerifiedByAdapter', 'ownerProcessExitVerified'}
SOURCE_PINS = {
    'sourceContract': 'a8dbe3b2736d741b7a84efcad5428869f8d37f8b17c60469bdb0e9d9e6219347',
    'transportSource': 'b265c26918e55612ca3a6eea171430ad04da86afa6674496e598ca9a6acd01f2',
    'clientSource': '9eb5c3fc3954c12cffa914022c00b418545dc39a67ab69db82c01500ae3fe5d7',
    'node': 'a9bd0630891c2dcdee70de88270fee2cc0c4a9e76495039dd3b4f91c5e6b71df',
}


NATIVE_PINS = {'next_fixture_iam_operations.source-only.mjs': '3ce2f92883d62c4cc9ba46b0d56def2a7a67fd5a063c514ecf58639b5bfcfa0e', 'native_next_fixture_iam_guard.source-only.js': '2f79843da8df75977874d1bfdd416f5b3124e875b041c1634ffcb489adae781f', 'setup_version_operations.source-only.mjs': 'dd64d90d4ba15066fe0716092fe692f3df9dad3838af247f54c73617f33e1ec8', 'native_setup_version_operations_guard.source-only.js': '577ed656f42e56072decf6dc58a0c5c35f4b3dff2062a930e022c1a35ec8944f', 'native_partial_kms_deletion_guard.source-only.js': 'aee32b7cca291edc42a33d78f2cf3831bc281d65a5b2dbd4a407e2ad06ac11be', 'native_partial_kms_deletion_prepare.source-only.mjs': 'bb806cc3090fdab4f46c4ea0acf72fdb3db324e89583bf03174976a8486411fb'}

def helper(leaf):
    # Two closed exact public source copies, unique module names, no discovery,
    # foreign sys.modules reuse, bytecode cache or production loader.
    pins = {'k_native_carrier.py': '006de410fcc4bc03374ac90adc91b9b14a7ba70b7642b5805d5ec7357153f4e1',
            'k_transport_receipt.py': '25fc50c0eeb820c0468f4e7ba04f7295b2012963c93491bc75237e24e79764cd'}
    path = HERE / leaf
    raw = path.read_bytes()
    if hashlib.sha256(raw).hexdigest() != pins[leaf]:
        raise ValueError('cleanup-custody:exact-public-helper-pin')
    import importlib
    return importlib.import_module('.' + Path(leaf).stem, __package__)


def uuid(value):
    return type(value) is str and re.fullmatch(r'[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12}', value) is not None


def wire_uuid(value):
    return type(value) is str and re.fullmatch(r'[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}', value) is not None


def identity(m, value):
    m.exact(value, IDENTITY)
    m.require(all(type(v) is str and 0 < len(v) <= 512 for v in value.values())
              and value['targetType'] == 'page' and value['targetURL'].startswith(BASE + '#/'),
              'cleanup-exact-public-document-identity')
    m.require(all(re.fullmatch(r'[A-Za-z0-9._:-]{1,256}', value[k]) is not None
                  for k in ('targetId', 'browserContextId', 'mainFrameId', 'loaderId')),
              'cleanup-closed-CDP-identity-fields')
    expected = value['targetURL'] if value['frameURLRelation'] == 'same-url' else BASE
    m.require(value['frameURLRelation'] in ('same-url', 'target-url-without-fragment')
              and value['frameURL'] == expected, 'cleanup-exact-frame-URL-relation')


def native_binding(m, value):
    m.exact(value, {'intent', 'ticket'})
    def decode(given):
        result = copy.deepcopy(given)
        m.require(type(result['reviewedDocumentSHA256JSON']) is str
                  and type(result['nativeSourcePinsJSON']) is str, 'cleanup-wire-binding-text')
        result['reviewedDocumentSHA256'] = m.strict_json(result.pop('reviewedDocumentSHA256JSON').encode())
        result['nativeSourcePins'] = m.strict_json(result.pop('nativeSourcePinsJSON').encode())
        m.require(given['nativeSourcePinsJSON'] == m.canonical(result['nativeSourcePins']).decode()
                  and given['reviewedDocumentSHA256JSON'] == m.canonical(result['reviewedDocumentSHA256']).decode(),
                  'cleanup-exact-two-wire-data-representations')
        return result
    intent = decode(value['intent'])
    ticket = copy.deepcopy(value['ticket'])
    ticket['requiredNativePreparation'] = decode(ticket['requiredNativePreparation'])
    m.require(type(ticket['nativeSourcePinsJSON']) is str, 'cleanup-ticket-wire-pin-text')
    ticket['nativeSourcePins'] = m.strict_json(ticket.pop('nativeSourcePinsJSON').encode())
    m.require(value['ticket']['nativeSourcePinsJSON'] == m.canonical(ticket['nativeSourcePins']).decode(),
              'cleanup-exact-ticket-wire-pin-map')
    adapter_context.checked(m, intent, ticket, ticket['requiredNativePreparation'])
    m.exact(intent, {adapter_context.FIELD, 'schema', 'operation', 'request', 'maximumForwards', 'preparationSHA256',
        'originalClaimSHA256', 'originalHeadSHA256', 'armedAt', 'cleanupUntil', 'reconciliationSHA256',
        'keyCreateCustodySHA256', 'currentNewKeyMetadataSHA256', 'oldKeysClosureSHA256',
        'newBoundaryCustodySHA256', 'reviewedDocumentSHA256', 'ownerContextSHA256',
        'nativeSourcePins', 'executionAuthorization'})
    m.exact(ticket, {adapter_context.FIELD, 'schema', 'operation', 'request', 'intentSHA256', 'issuedAt', 'maximumForwards',
        'requiredNativePreparation', 'nativeSourcePins', 'executionAuthorization', 'nativeHelperInput'})
    for row in (intent, ticket):
        m.exact(row['request'], {'KeyId', 'PendingWindowInDays'})
        m.require(row['operation'] == 'ScheduleKeyDeletion' and row['executionAuthorization'] is False
                  and type(row['maximumForwards']) is int and row['maximumForwards'] == 1
                  and type(row['request']['PendingWindowInDays']) is int and row['request']['PendingWindowInDays'] == 7,
                  'cleanup-exact-one-seven-day-operation')
    m.require(intent['schema'] == 'hikyo-158/partial-cleanup-intent/v1'
              and ticket['schema'] == 'hikyo-158/partial-cleanup-native-contract/v1'
              and uuid(intent['request']['KeyId']) and intent['request']['KeyId'] not in OLD
              and m.same(intent['request'], ticket['request'])
              and ticket['intentSHA256'] == m.digest(intent)
              and m.same(ticket['requiredNativePreparation'], intent)
              and m.same(ticket['nativeSourcePins'], intent['nativeSourcePins'])
              and m.same(intent['nativeSourcePins'], NATIVE_PINS)
              and (intent['reviewedDocumentSHA256'] is None or m.sha(intent['reviewedDocumentSHA256'])), 'cleanup-full-intent-ticket-binding')
    m.require(all(m.sha(intent[key]) for key in ('preparationSHA256', 'originalClaimSHA256',
        'originalHeadSHA256', 'reconciliationSHA256', 'keyCreateCustodySHA256', 'currentNewKeyMetadataSHA256',
        'oldKeysClosureSHA256', 'newBoundaryCustodySHA256', 'ownerContextSHA256')),
        'cleanup-whole-required-custody-hashes')
    return intent, ticket


def arguments_checked(m, arguments):
    m.exact(arguments, {'action', 'expected', 'binding', 'preparation', 'readbackManifest', 'choice', 'slot'})
    m.require(arguments['action'] in ('schedule', 'status') and arguments['choice'] == arguments['slot'] == 'none',
              'cleanup-only-schedule-or-capture-context')
    identity(m, arguments['expected'])
    intent, ticket = native_binding(m, arguments['binding'])
    key = intent['request']['KeyId']
    m.exact(arguments['preparation'], {'manifest', 'approvedBody'})
    manifest = arguments['preparation']['manifest']
    refs = ticket['nativeHelperInput']
    fields = {'newKeyARN', 'newKeyId', 'originalCreateCustodySHA256', 'reviewedCurrentWholeKeyMetadataSHA256',
        'oldKeysClosureSHA256', 'cleanupIntentSHA256', 'reviewedPreparationSHA256', 'cleanupReconciliationSHA256',
        'guardSourceSHA256', 'helperSourceSHA256', 'notBeforeMS', 'expiresAtMS', 'captureTimeoutMS'}
    m.exact(refs, fields)
    expected = {'newKeyARN': 'arn:aws:kms:eu-central-1:099256073061:key/' + key, 'newKeyId': key,
        'originalCreateCustodySHA256': intent['keyCreateCustodySHA256'],
        'reviewedCurrentWholeKeyMetadataSHA256': intent['currentNewKeyMetadataSHA256'],
        'oldKeysClosureSHA256': intent['oldKeysClosureSHA256'], 'cleanupIntentSHA256': m.digest(intent),
        'reviewedPreparationSHA256': intent['preparationSHA256'], 'cleanupReconciliationSHA256': intent['reconciliationSHA256'],
        'guardSourceSHA256': 'aee32b7cca291edc42a33d78f2cf3831bc281d65a5b2dbd4a407e2ad06ac11be',
        'helperSourceSHA256': 'bb806cc3090fdab4f46c4ea0acf72fdb3db324e89583bf03174976a8486411fb',
        'notBeforeMS': int(m.instant(ticket['issuedAt']).timestamp() * 1000),
        'expiresAtMS': min(int(m.instant(ticket['issuedAt']).timestamp() * 1000) + 300000,
                          int(m.instant(intent['cleanupUntil']).timestamp() * 1000)), 'captureTimeoutMS': 20000}
    m.require(m.same(refs, expected) and all(type(refs[k]) is int for k in ('notBeforeMS', 'expiresAtMS', 'captureTimeoutMS'))
              and 0 <= refs['notBeforeMS'] < refs['expiresAtMS'] <= int(m.instant('2026-10-26T12:22:56Z').timestamp() * 1000)
              and m.instant(intent['armedAt']) <= m.instant(ticket['issuedAt']),
              'cleanup-exact-original-helper-input')
    body = '{"KeyId":"' + key + '","PendingWindowInDays":7}'
    whole = {'schema': 'hikyo-158/partial-kms-deletion-manifest/v1', 'account': '099256073061',
        'region': 'eu-central-1', 'endpoint': 'https://kms.eu-central-1.amazonaws.com/',
        'publicMainSHA256': '20a50d3f9e11cdae453b0f764689073f2adc6c644e6ac2a08660be5b1b2227a2', **expected,
        'requestBodySHA256': hashlib.sha256(body.encode()).hexdigest(), 'requestBodyBytes': len(body.encode()),
        'maximumNativeScheduleForwards': 1, 'maximumNativeDescribeForwardsPerKey': 2}
    whole['helperPreparationSHA256'] = m.digest(whole)
    m.require(arguments['preparation']['approvedBody'] == body and m.same(manifest, whole),
              'cleanup-exact-reviewed-helper-preparation')
    readback = arguments['readbackManifest']
    m.exact(readback, {'schema', 'mode', 'account', 'region', 'endpoint', 'publicMainSHA256',
        'preparationSHA256', 'ownerContextSHA256', 'intentSHA256', 'claimId', 'wireBodySHA256',
        'wireBodyBytes', 'notBeforeMS', 'expiresAtMS', 'newKeyARN', 'createReceiptSHA256'})
    m.require(readback['schema'] == 'hikyo-158/next-key-observer-manifest/v1' and readback['mode'] == 'readback'
              and all(readback[k] == manifest[k] for k in ('account', 'region', 'endpoint', 'publicMainSHA256', 'notBeforeMS', 'expiresAtMS', 'newKeyARN'))
              and readback['preparationSHA256'] == intent['preparationSHA256']
              and type(readback['notBeforeMS']) is int and type(readback['expiresAtMS']) is int
              and all(m.sha(readback[k]) for k in ('ownerContextSHA256', 'intentSHA256', 'wireBodySHA256', 'createReceiptSHA256'))
              and type(readback['claimId']) is str and 0 < len(readback['claimId']) <= 256
              and type(readback['wireBodyBytes']) is int and 0 < readback['wireBodyBytes'] <= 32768,
              'cleanup-closed-original-readback-manifest')
    routes = [BASE + '#/kms/home', BASE + '#/kms/keys', BASE + '#/kms/keys/scheduleDeletion/' + key]
    routes += [BASE + '#/kms/keys/' + value + tab for value in (key, *OLD)
               for tab in ('', '/keyPolicy', '/keyRotation', '/aliases', '/cryptographicConfiguration')]
    m.require(arguments['expected']['targetURL'] in routes and (arguments['action'] != 'schedule'
              or arguments['expected']['targetURL'] == BASE + '#/kms/keys/scheduleDeletion/' + key),
              'cleanup-exact-request-document-route')
    return intent, ticket, routes


def command_context(m, binding, request, reviewed_registry_raw):
    """Derive exact argv from Root-trusted data, never from stdout/native input."""
    adapter_context.checked(m, binding)
    m.exact(binding, {adapter_context.FIELD, 'schema', 'session', 'threadId', 'ownerId', 'runtimeDirectory',
        'sourceContract', 'transportSource', 'reviewManifest', 'clientSource', 'node', 'script', 'runtimeReadinessGranted'})
    m.require(binding['schema'] == 'hikyo-158/cleanup-reviewed-root-binding/v1'
              and binding['session'] == 'hikyo-158-owner-complete' and wire_uuid(binding['ownerId'])
              and wire_uuid(binding['threadId']) and binding['runtimeReadinessGranted'] is False,
              'cleanup-explicit-Root-reviewed-binding')
    def path(value):
        m.require(type(value) is str and 0 < len(value) <= 512 and value.startswith('/')
                  and str(PurePosixPath(value)) == value and '..' not in PurePosixPath(value).parts
                  and not any(c in value for c in ('\0', '\r', '\n')), 'cleanup-reviewed-absolute-path')
    for key in ('sourceContract', 'transportSource', 'reviewManifest', 'clientSource', 'node'):
        m.exact(binding[key], {'path', 'rawSHA256'})
        path(binding[key]['path'])
        m.require(m.sha(binding[key]['rawSHA256']) and (key in ('transportSource', 'reviewManifest') or key not in SOURCE_PINS
                  or binding[key]['rawSHA256'] == SOURCE_PINS[key]), 'cleanup-pinned-R2-source-and-node')
    path(binding['runtimeDirectory'])
    parent = PurePosixPath(binding['clientSource']['path']).parent
    for key, leaf in [('sourceContract', 'SOURCE-CONTRACT.json'), ('transportSource', 'source-manifest.json'),
                      ('reviewManifest', 'review-manifest.json'), ('clientSource', 'client.cjs')]:
        m.require(PurePosixPath(binding[key]['path']) == parent / leaf, 'cleanup-exact-R2-client-source-layout')
    m.exact(binding['script'], {'id', 'rawSHA256'})
    m.require(type(reviewed_registry_raw) is bytes and 0 < len(reviewed_registry_raw) <= 262144
              and hashlib.sha256(reviewed_registry_raw).hexdigest() == binding['reviewManifest']['rawSHA256'],
              'cleanup-exact-Root-reviewed-registry-bytes')
    registry = m.strict_json(reviewed_registry_raw)
    m.exact(registry, {'schema', 'scripts'})
    m.require(registry['schema'] == 'hikyo-158/reviewed-owner-scripts/v1' and type(registry['scripts']) is list
              and 1 <= len(registry['scripts']) <= 64, 'cleanup-bounded-reviewed-registry')
    scripts = [s for s in registry['scripts'] if s['id'] == 'new-key-cleanup-v1']
    m.require(len(scripts) == 1, 'cleanup-one-reviewed-registry-entry')
    script = scripts[0]
    m.exact(script, {'id', 'file', 'sha256', 'effect', 'arguments', 'result'})
    m.require(binding['script'] == {'id': 'new-key-cleanup-v1', 'rawSHA256': script['sha256']}
              and m.sha(script['sha256']) and script['file'] == 'reviewed-scripts/new-key-cleanup-v1.js'
              and script['effect'] == 'mutation', 'cleanup-Root-selected-reviewed-script')
    m.exact(request, {'schema', 'ownerId', 'sourceSHA256', 'reviewSHA256', 'requestId',
                      'sequence', 'operation', 'scriptId', 'arguments'})
    m.require(request['schema'] == 'hikyo-158/persistent-owner-request/v1' and request['operation'] == 'run-code'
              and request['scriptId'] == script['id'] and wire_uuid(request['requestId']) and wire_uuid(request['ownerId'])
              and request['ownerId'] == binding['ownerId'] and type(request['sequence']) is int
              and 1 <= request['sequence'] <= 1153
              and request['sourceSHA256'] == binding['transportSource']['rawSHA256']
              and request['reviewSHA256'] == binding['reviewManifest']['rawSHA256'], 'cleanup-full-required-R2-request')
    intent, ticket, _ = arguments_checked(m, request['arguments'])
    wire = helper('k_transport_receipt.py')
    argv = [binding['node']['path'], binding['clientSource']['path'], '--owner-id', binding['ownerId'],
        '--runtime-dir', binding['runtimeDirectory'], '--source-sha256', request['sourceSHA256'],
        '--review-sha256', request['reviewSHA256'], '--request-json', wire.canonical(request)]
    m.require(all(type(v) is str and len(v.encode()) <= 262144 and not any(c in v for c in ('\0', '\r', '\n'))
                  for v in argv), 'cleanup-finite-pinned-client-argv')
    inner = shlex.join(argv)
    return {adapter_context.FIELD: binding[adapter_context.FIELD], 'schema': 'hikyo-158/cleanup-reviewed-command-context/v1', 'threadId': binding['threadId'],
        'sourceBindingSHA256': wire.digest(binding), 'reviewedRegistryRawSHA256': binding['reviewManifest']['rawSHA256'],
        'scriptSHA256': script['sha256'], 'request': copy.deepcopy(request), 'argv': argv,
        'innerCommand': inner, 'carrierArgv': ['/bin/zsh', '-lc', inner],
        'command': '/bin/zsh -lc ' + shlex.quote(inner), 'expectedDocument': copy.deepcopy(request['arguments']['expected']),
        'intentSHA256': m.digest(intent), 'ticketSHA256': m.digest(ticket),
        'helperPreparationSHA256': request['arguments']['preparation']['manifest']['helperPreparationSHA256']}


def receipt_checked(m, response, context, ui_result):
    m.exact(response, RESPONSE_KEYS)
    request = context['request']
    m.require(response['schema'] == 'hikyo-158/persistent-owner-response/v1'
              and all(response[k] == request[k] for k in ('ownerId', 'sourceSHA256', 'reviewSHA256', 'requestId', 'sequence'))
              and type(response['sequence']) is int and type(response['nextSequence']) is int
              and response['nextSequence'] == request['sequence'] + 1
              and all(response[k] is True for k in ('accepted', 'ok', 'ownerStillAlive', 'clientChannelClosed', 'cliCaptureSuppressed'))
              and all(response[k] is False for k in ('ownerClosed', 'coreEphemeralProtocolMetadataSuppressed',
                  'controlLeaseClaimed', 'exclusiveControlVerifiedByAdapter', 'ownerProcessExitVerified'))
              and response['state'] == 'active' and response['error'] is None
              and m.sha(response['responseSHA256']) and m.same(response['result'], ui_result),
              'cleanup-complete-live-R2-receipt')
    wire = helper('k_transport_receipt.py')
    m.require(response['responseSHA256'] == wire.digest({k: v for k, v in response.items() if k not in STDOUT_ONLY}),
              'cleanup-exact-JS-wire-response-digest')
    return wire
