"""Pure persistent-owner custody. No transport, file, profile, clock or DB discovery.

The exact genuine-event verifier is the pinned, unchanged adopted verifier.
Its result establishes native byte custody, never provider or owner authenticity.
Root must separately authenticate and release every original and source binding.
"""

from . import adapter_context
import copy
import hashlib
from pathlib import PurePosixPath
import re
import shlex

from . import transport_receipt
from .native_carrier import matches_native_carrier

from .ancestor_chrome_custody import THREAD, identity_checked, readout_script, verify_native_original


KINDS = ('owner', 'identity', 'iam', 'kms', 'setup', 'historical')
BINDING_KEYS = {'attemptTrustBindingSHA256', 'schema', 'sourceContract', 'transportSource', 'reviewManifest', 'clientSource',
                'node', 'session', 'ownerId', 'runtimeDirectory', 'scripts', 'runtimeReadinessGranted'}
CONTEXT_KEYS = {'attemptTrustBindingSHA256', 'schema', 'threadId', 'kind', 'identity', 'argv', 'innerCommand', 'carrierArgv', 'command',
                'sourceSHA256', 'reviewSHA256', 'sourceContractSHA256', 'scriptSHA256', 'scriptId',
                'request', 'sourceBindingSHA256', 'controlLeaseClaimed', 'exclusiveControlVerifiedByAdapter',
                'cliCaptureSuppressedBySource', 'coreEphemeralProtocolMetadataSuppressed'}
RESPONSE_KEYS = {'schema', 'ownerId', 'sourceSHA256', 'reviewSHA256', 'requestId', 'sequence',
                 'accepted', 'ok', 'nextSequence', 'state', 'ownerStillAlive', 'ownerClosed',
                 'result', 'error', 'responseSHA256', 'clientChannelClosed','cliCaptureSuppressed',
                 'coreEphemeralProtocolMetadataSuppressed','controlLeaseClaimed',
                 'exclusiveControlVerifiedByAdapter','ownerProcessExitVerified'}
STDOUT_ONLY = {'responseSHA256','clientChannelClosed','cliCaptureSuppressed',
              'coreEphemeralProtocolMetadataSuppressed','controlLeaseClaimed',
              'exclusiveControlVerifiedByAdapter','ownerProcessExitVerified'}


def raw_sha(raw):
    return hashlib.sha256(raw).hexdigest()


def uuid(value):
    return type(value) is str and re.fullmatch(r'[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12}', value) is not None


def source_binding_checked(m, binding):
    adapter_context.checked(m, binding)
    m.exact(binding, BINDING_KEYS)
    m.require(binding['schema'] == 'hikyo-158/persistent-chrome-prepare-source-binding/v1'
              and binding['session'] == 'hikyo-158-owner-complete'
              and uuid(binding['ownerId']) and binding['runtimeReadinessGranted'] is False,
              'persistent-explicit-new-source-binding-no-readiness-grant')
    for key in ('sourceContract', 'transportSource', 'reviewManifest', 'clientSource', 'node'):
        row = binding[key]
        m.exact(row, {'path', 'rawSHA256'})
        m.require(type(row['path']) is str and row['path'].startswith('/')
                  and str(PurePosixPath(row['path'])) == row['path']
                  and '..' not in PurePosixPath(row['path']).parts
                  and not any(c in row['path'] for c in ('\0', '\r', '\n'))
                  and m.sha(row['rawSHA256']), 'persistent-exact-reviewed-source-ref')
    m.require(type(binding['runtimeDirectory']) is str and binding['runtimeDirectory'].startswith('/')
              and str(PurePosixPath(binding['runtimeDirectory'])) == binding['runtimeDirectory']
              and '..' not in PurePosixPath(binding['runtimeDirectory']).parts
              and not any(c in binding['runtimeDirectory'] for c in ('\0', '\r', '\n')),
              'persistent-explicit-opaque-runtime-directory')
    # These are proposed/Root-reviewed script bindings, never inferred IDs/code.
    m.exact(binding['scripts'], KINDS)
    for kind, row in binding['scripts'].items():
        m.exact(row, {'id', 'rawSHA256'})
        m.require(type(row['id']) is str and re.fullmatch(r'[a-z][a-z0-9-]{0,63}', row['id'])
                  and m.sha(row['rawSHA256']), 'persistent-closed-reviewed-script-binding')
    m.require(len({row['id'] for row in binding['scripts'].values()}) == len(KINDS),
              'persistent-distinct-script-IDs')


def invocation(m, binding, *, kind, identity, request_id, sequence, script_arguments=None):
    """Exact finite client argv only. No profile field or runtime execution."""
    source_binding_checked(m, binding)
    identity_checked(m, identity)
    m.require(kind in KINDS and uuid(request_id) and type(sequence) is int and 1<=sequence<=1024,
              'persistent-exact-kind-request-ID-and-owner-sequence')
    if kind in ('identity', 'historical'):
        readout_script(m, identity, kind)  # Reuse original closed route checks only, never execute code.
    arguments = {'expected': copy.deepcopy(identity), adapter_context.FIELD: binding[adapter_context.FIELD]} if script_arguments is None else copy.deepcopy(script_arguments)
    m.exact(arguments, {adapter_context.FIELD, 'expected','manifest'} if kind in ('iam','kms','setup') else {adapter_context.FIELD, 'expected'})
    adapter_context.checked(m, arguments)
    m.require(m.same(arguments['expected'],identity), 'persistent-exact-script-identity-argument')
    if kind in ('iam','kms','setup'):
        m.require(type(arguments['manifest']) is dict, 'persistent-explicit-native-manifest-argument')
    script = binding['scripts'][kind]
    request = {'schema': 'hikyo-158/persistent-owner-request/v1', 'ownerId': binding['ownerId'],
               'sourceSHA256': binding['transportSource']['rawSHA256'],
               'reviewSHA256': binding['reviewManifest']['rawSHA256'], 'requestId': request_id,
               'sequence': sequence, 'operation': 'run-code', 'scriptId': script['id'],
               'arguments': arguments}
    argv = [binding['node']['path'], binding['clientSource']['path'], '--owner-id', binding['ownerId'],
            '--runtime-dir', binding['runtimeDirectory'], '--source-sha256', request['sourceSHA256'],
            '--review-sha256', request['reviewSHA256'], '--request-json', m.canonical(request).decode()]
    inner = shlex.join(argv)
    return {adapter_context.FIELD: binding[adapter_context.FIELD], 'schema': 'hikyo-158/persistent-chrome-prepare-command/v1', 'threadId': THREAD,
            'kind': kind, 'identity': copy.deepcopy(identity), 'argv': argv, 'innerCommand': inner,
            'carrierArgv': ['/bin/zsh', '-lc', inner], 'command': '/bin/zsh -lc ' + shlex.quote(inner),
            'sourceSHA256': request['sourceSHA256'], 'reviewSHA256': request['reviewSHA256'],
            'sourceContractSHA256': binding['sourceContract']['rawSHA256'], 'scriptSHA256': script['rawSHA256'],
            'scriptId': script['id'], 'request': request, 'sourceBindingSHA256': m.digest(binding),
            'controlLeaseClaimed': False, 'exclusiveControlVerifiedByAdapter': False,
            'cliCaptureSuppressedBySource': True, 'coreEphemeralProtocolMetadataSuppressed': False}


class Custody:
    """Closed callback binding to the adopted exact current-stream SQL verifier."""
    def __init__(self, binding):
        self.binding = copy.deepcopy(binding)

    def command_context_checked(self, m, context):
        m.exact(context, CONTEXT_KEYS)
        request = context['request']
        m.exact(request, {'schema', 'ownerId', 'sourceSHA256', 'reviewSHA256', 'requestId',
                          'sequence', 'operation', 'scriptId', 'arguments'})
        expected = invocation(m, self.binding, kind=context['kind'], identity=context['identity'],
                              request_id=request['requestId'], sequence=request['sequence'],script_arguments=request['arguments'])
        m.require(m.same(context, expected), 'persistent-whole-exact-shell-command-context')

    def command_readout_checked(self, m, raw, command, *, connection, sequence_min, sequence_max, explicit_now):
        self.command_context_checked(m, command)
        m.require(type(raw) is bytes and 0 < len(raw) <= 1_999_999, 'persistent-whole-original-bound')
        # This callback is always constructed here. A caller cannot replace it
        # with a boolean/hash-only authority or a permissive fabricated original.
        def authenticate_original(whole_raw):
            return verify_native_original(connection, whole_raw, m, sequence_min, sequence_max)
        original, sequence = authenticate_original(raw)
        m.clock(explicit_now)
        m.fresh(original['completedAt'], explicit_now)
        m.require(matches_native_carrier(original['input'], expected_inner_command=command['innerCommand'], expected_carrier_argv=command['carrierArgv'])
                  and ('threadId' not in original or original['threadId'] == THREAD)
                  and m.instant(original['updatedAt']) <= explicit_now,
                  'persistent-exact-zsh-carrier-and-whole-command-chronology')
        response = m.strict_json(original['output'].encode())
        m.exact(response, RESPONSE_KEYS)
        request = command['request']
        m.require(response['schema'] == 'hikyo-158/persistent-owner-response/v1'
                  and all(response[k] == request[k] for k in
                          ('ownerId', 'sourceSHA256', 'reviewSHA256', 'requestId', 'sequence'))
                  and type(response['sequence']) is int and response['sequence'] > 0
                  and type(response['nextSequence']) is int and response['nextSequence'] == request['sequence'] + 1
                  and all(response[k] is True for k in ('accepted', 'ok', 'ownerStillAlive', 'clientChannelClosed'))
                  and response['ownerClosed'] is False and response['state'] == 'active'
                  and response['cliCaptureSuppressed'] is True
                  and all(response[k] is False for k in ('coreEphemeralProtocolMetadataSuppressed',
                      'controlLeaseClaimed','exclusiveControlVerifiedByAdapter','ownerProcessExitVerified'))
                  and response['error'] is None and m.sha(response['responseSHA256'])
                  and response['responseSHA256'] == transport_receipt.digest({k:v for k,v in response.items()
                      if k not in STDOUT_ONLY}),
                  'persistent-exact-accepted-finite-client-same-live-owner-no-replay')
        readout = response['result']
        m.exact(readout, {'schema', 'kind', 'before', 'after', 'readAtMS', 'value',
                          'controlLeaseClaimed', 'exclusiveControlVerifiedByAdapter'})
        identity_checked(m, readout['before']); identity_checked(m, readout['after'])
        m.require(readout['schema'] == 'hikyo-158/chrome-public-readout/v1' and readout['kind'] == command['kind']
                  and m.same(readout['before'], command['identity']) and m.same(readout['after'], command['identity'])
                  and type(readout['readAtMS']) is int and 0 <= readout['readAtMS'] <= 9007199254740991
                  and m.instant(original['startedAt']).timestamp()*1000 <= readout['readAtMS']
                  <= m.instant(original['completedAt']).timestamp()*1000
                  and readout['controlLeaseClaimed'] is False and readout['exclusiveControlVerifiedByAdapter'] is False,
                  'persistent-actual-target-frame-loader-around-public-readout')
        public_value = readout['value']
        if command['kind'] in ('historical','iam','kms','setup'):
            m.exact(public_value,{'schema','chunks'})
            m.require(public_value['schema']=='hikyo-158/public-json-chunks/v1'
                and type(public_value['chunks']) is list and 1<=len(public_value['chunks'])<=64
                and all(type(part) is str and 1<=len(part)<=2048 for part in public_value['chunks']),
                'persistent-explicit-bounded-public-payload-chunks')
            wire=''.join(public_value['chunks']).encode()
            m.require(len(wire)<=131072,'persistent-bounded-whole-public-payload')
            public_value=m.strict_json(wire)
        if command['kind'] in ('iam','kms','setup'):
            m.exact(public_value, {'manifest','readRows'})
            m.require(m.same(public_value['manifest'],request['arguments']['manifest']),
                      'persistent-exact-reviewed-readout-manifest')
        return {adapter_context.FIELD: command[adapter_context.FIELD], 'original': original, 'originalRawSHA256': raw_sha(raw), 'originalSHA256': m.digest(original),
                'originalSequence': sequence, 'commandContextSHA256': m.digest(command),
                'readout': readout, 'publicValue':public_value}
