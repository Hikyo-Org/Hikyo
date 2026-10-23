"""Pure, exact persistent stage commands and whole native status custody."""

from . import adapter_context
import copy
import shlex
from pathlib import Path

from . import create_custody as h_custody
from .ancestor_chrome_custody import THREAD, identity_checked, readout_script, verify_native_original
from . import transport_receipt
from .native_carrier import matches_native_carrier

HERE = Path(__file__).resolve().parent


def command(m, binding, registry, *, kind, arguments, request_id, sequence):
    adapter_context.checked(m, binding)
    h_custody.source_binding_checked(m, binding)
    m.require(m.same(registry, m.strict_json((HERE / 'stage-registry.json').read_bytes())),
              'same-source-owned-stage-registry')
    m.require(kind in ('install', 'inspect', 'status', 'arm', 'finish') and h_custody.uuid(request_id)
              and type(sequence) is int and 1 <= sequence <= 1153, 'closed-stage-command-request')
    keys = {adapter_context.FIELD, 'expected', 'window'} | ({'manifest', 'approvedBodyChunks'} if kind == 'install'
        else {'phase', 'manifest'} if kind in ('status', 'inspect') else {'phase', 'descriptor'})
    m.exact(arguments, keys)
    adapter_context.checked(m, arguments)
    if kind in ('arm', 'finish'):
        adapter_context.checked(m, arguments['descriptor'])
    identity_checked(m, arguments['expected']); readout_script(m, arguments['expected'], 'create')
    m.exact(arguments['window'], {'notBeforeMS', 'expiresAtMS'})
    m.require(all(type(v) is int and 0 <= v <= 9007199254740991 for v in arguments['window'].values()),
              'closed-original-window')
    if kind != 'install':
        m.require((kind == 'inspect' and arguments['phase'] == 'inspect') or
                  (arguments['phase'] in ('arm', 'finish') and (kind == 'status' or arguments['phase'] == kind)),
                  'exact-stage-phase')
    entry = next(row for row in registry['scripts'] if row['id'] == 'native-create-' + kind + '-v1')
    request = {'schema': 'hikyo-158/persistent-owner-request/v1', 'ownerId': binding['ownerId'],
        'sourceSHA256': binding['transportSource']['rawSHA256'], 'reviewSHA256': binding['reviewManifest']['rawSHA256'],
        'requestId': request_id, 'sequence': sequence, 'operation': 'run-code', 'scriptId': entry['id'],
        'arguments': copy.deepcopy(arguments)}
    argv = [binding['node']['path'], binding['clientSource']['path'], '--owner-id', binding['ownerId'],
        '--runtime-dir', binding['runtimeDirectory'], '--source-sha256', request['sourceSHA256'],
        '--review-sha256', request['reviewSHA256'], '--request-json', m.canonical(request).decode()]
    inner = shlex.join(argv)
    return {adapter_context.FIELD: binding[adapter_context.FIELD], 'schema': 'hikyo-158/native-create-stage-command/v1', 'kind': kind, 'threadId': THREAD,
        'protocolRevision': binding['protocolRevision'], 'holderSourceBindingSHA256': m.digest(binding),
        'scriptSHA256': entry['sha256'], 'argv': argv, 'innerCommand': inner,
        'carrierArgv': ['/bin/zsh', '-lc', inner], 'command': '/bin/zsh -lc ' + shlex.quote(inner), 'request': request}


def status_original(m, binding, registry, context, raw, *, connection, sequence_min, sequence_max, explicit_now):
    request = context['request']
    expected = command(m, binding, registry, kind='status', arguments=request['arguments'],
                       request_id=request['requestId'], sequence=request['sequence'])
    m.require(m.same(context, expected), 'whole-exact-reviewed-status-context')
    original, sequence = verify_native_original(connection, raw, m, sequence_min, sequence_max)
    m.require(matches_native_carrier(original['input'], expected_inner_command=context['innerCommand'], expected_carrier_argv=context['carrierArgv']) and original.get('threadId', THREAD) == THREAD
              and m.instant(original['updatedAt']) <= explicit_now, 'exact-status-native-carrier-and-time')
    m.fresh(original['completedAt'], explicit_now)
    response = m.strict_json(original['output'].encode()); m.exact(response, h_custody.RESPONSE_KEYS)
    m.require(response['schema'] == 'hikyo-158/persistent-owner-response/v1'
        and all(response[k] == request[k] for k in ('ownerId', 'sourceSHA256', 'reviewSHA256', 'requestId', 'sequence'))
        and type(response['sequence']) is int and type(response['nextSequence']) is int
        and response['nextSequence'] == request['sequence'] + 1 and response['nextSequence'] <= 1154
        and all(response[k] is True for k in ('accepted', 'ok', 'ownerStillAlive', 'clientChannelClosed', 'cliCaptureSuppressed'))
        and all(response[k] is False for k in ('ownerClosed', 'coreEphemeralProtocolMetadataSuppressed',
            'controlLeaseClaimed', 'exclusiveControlVerifiedByAdapter', 'ownerProcessExitVerified'))
        and response['state'] == 'active' and response['error'] is None
        and response['responseSHA256'] == transport_receipt.digest({k: v for k, v in response.items() if k not in h_custody.STDOUT_ONLY}),
        'whole-exact-status-response-no-authority')
    readout = response['result']
    m.exact(readout, {'schema', 'kind', 'phase', 'before', 'after', 'readAtMS', 'value',
                     'controlLeaseClaimed', 'exclusiveControlVerifiedByAdapter', 'writeAuthorityGranted', 'providerOutcomeProven'})
    args = request['arguments']; identity_checked(m, readout['before']); identity_checked(m, readout['after'])
    m.require(readout['schema'] == 'hikyo-158/native-create-stage-readout/v1' and readout['kind'] == 'status'
        and readout['phase'] == args['phase'] and m.same(readout['before'], args['expected'])
        and m.same(readout['after'], args['expected'])
        and type(readout['readAtMS']) is int
        and m.instant(original['startedAt']).timestamp()*1000 <= readout['readAtMS']
        <= m.instant(original['completedAt']).timestamp()*1000
        and all(readout[k] is False for k in ('controlLeaseClaimed', 'exclusiveControlVerifiedByAdapter',
                                             'writeAuthorityGranted', 'providerOutcomeProven')), 'exact-status-readout-cdp-window')
    m.exact(readout['value'], {'guard', 'observer'})
    return {adapter_context.FIELD: context[adapter_context.FIELD], 'original': original, 'value': readout['value'], 'originalSequence': sequence,
            'originalSHA256': m.digest(original), 'originalRawSHA256': h_custody.raw_sha(raw),
            'contextSHA256': m.digest(context)}
