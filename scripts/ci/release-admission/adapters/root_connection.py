"""SOURCE ONLY. Concrete existing G call connections, no authority implementation.

Import performs no I/O. Caller supplies the actual configured, constructed G
coordinator and Root's independently authenticated whole command originals.
No launcher, provider, SQLite, clock default, fake command_execution or fallback.
"""

from . import adapter_context
import copy
import json

OPERATION = 'ScheduleKeyDeletion'
NAMESPACE = 'hikyo-e2e-20261022c05/'
HOME = 'https://eu-central-1.console.aws.amazon.com/kms/home?region=eu-central-1#/kms/home'
OLD = ('10908b8d-fe54-40e3-9210-a3eefea709b6', '990c479f-d673-4bc9-8bca-7b9b86f4b205',
       'fa374267-a456-4e79-b5b1-6cdf356b7d0e', 'bfa44dc1-ee8a-43b8-aec7-d8a7fbb38db1')


def describe():
    return {'schema': 'hikyo-158/new-key-cleanup-root-connection/v1',
            'namespace': NAMESPACE, 'operation': OPERATION,
            'coordinator': 'G/partial_setup_cleanup_inventory.source-only.py:AbortCleanup',
            'calls': ['reconcile(proof, authority)', "arm('ScheduleKeyDeletion', proof, authority)",
                      "ticket('ScheduleKeyDeletion', proof, authority)",
                      "accept('ScheduleKeyDeletion', receipt_proof, authority)"],
            'browserOrder': ['inspect', 'set-seven', 'confirm', 'install', 'arm', 'schedule', 'status'],
            'freshReadbackOrder': ['Root verifies original schedule command custody', 'officialPage.goto(fixed HOME)',
                                  'new actual CDP identity', 'readback-install', 'navigate managedKeys/new/keyPolicy/keyRotation',
                                  'wait until actual original-window clock reaches new.DescribeKey.receivedAtMS + 30001',
                                  'navigate aliases once explicitly; require genuine ListAliases slot',
                                  'navigate managedKeys/old1..old4', 'readback-collect each of eight exact slots'],
            'freshReadbackCustody': ['Root independently releases trusted source/runtime binding and full registry bytes',
                'reviewed_command_context(model, trusted_binding, full_request, reviewed_registry_raw)',
                'unchanged prebound current-thread native-original verifier',
                'exact F carrier argv, complete JS R2 receipt, original document and cleanup binding',
                'retain whole raw original + exact request/context/receipt before any unload'],
            'executionAuthorityGranted': False, 'providerAPIsCalled': False, 'runtimeReady': False}


def reconcile(coordinator, proof, authority):
    _coordinator_binding(coordinator)
    return coordinator.reconcile(proof, authority)


def arm(coordinator, proof, authority):
    _coordinator_binding(coordinator)
    return coordinator.arm(OPERATION, proof, authority)


def ticket(coordinator, proof, authority):
    # No invented or overwritten ticket. G performs the fresh gate and once write.
    _coordinator_binding(coordinator)
    return coordinator.ticket(OPERATION, proof, authority)


def binding_for_ui(intent, ticket):
    """Lossless two named-field representation needed by actual R2 schemas.

    R2 definitions lack nullable/union and exclude dots in object field names.
    No caller-supplied source, selector, URI or general decoder is accepted.
    """
    def convert(value):
        result = copy.deepcopy(value)
        result['reviewedDocumentSHA256JSON'] = json.dumps(result.pop('reviewedDocumentSHA256'), separators=(',', ':'))
        result['nativeSourcePinsJSON'] = json.dumps(result.pop('nativeSourcePins'), sort_keys=True, separators=(',', ':'))
        return result
    result = {'intent': convert(intent), 'ticket': copy.deepcopy(ticket)}
    result['ticket']['requiredNativePreparation'] = convert(result['ticket']['requiredNativePreparation'])
    result['ticket']['nativeSourcePinsJSON'] = json.dumps(result['ticket'].pop('nativeSourcePins'), sort_keys=True, separators=(',', ':'))
    return result


def public_detail(model, result):
    """Only AFTER caller Root authenticates the WHOLE original command envelope."""
    model.require(type(result) is dict and result.get('schema') == 'hikyo-158/new-key-cleanup-ui/v1', 'exact-cleanup-ui-output')
    model.require(all(result.get(k) is False for k in ('controlLeaseClaimed', 'exclusiveControlVerifiedByAdapter',
                  'writeAuthorityGranted', 'providerOutcomeProven', 'externalAuthenticityVerified')), 'no-ui-authority')
    parts = result.get('publicCaptureParts')
    try:
        model.require(type(parts) is list and 1 <= len(parts) <= 64 and all(type(s) is str and 1 <= len(s.encode('utf-16-le')) // 2 <= 2048 for s in parts), 'closed-public-capture-parts')
        raw = ''.join(parts).encode('utf-8')
    except UnicodeError:
        raise ValueError('cleanup-invalid-public-unicode') from None
    model.require(len(raw) <= 120000, 'bounded-single-original-public-capture')
    value = model.strict_json(raw)
    def scalar_strings(item, depth=0):
        model.require(depth <= 64, 'bounded-public-JSON-depth')
        if type(item) is str:
            item.encode('utf-8')
        elif type(item) is list:
            for child in item:
                scalar_strings(child, depth + 1)
        elif type(item) is dict:
            for key, child in item.items():
                key.encode('utf-8')
                scalar_strings(child, depth + 1)
    try:
        scalar_strings(value)
        # Whole public metadata JSON only. Preserve the original raw string and
        # bytes/hash, including legitimate escaped supplementary characters.
        if type(value) is dict:
            for key in ('row', 'schedule'):
                if type(value.get(key)) is dict and type(value[key].get('rawResponse')) is str:
                    scalar_strings(model.strict_json(value[key]['rawResponse'].encode()))
    except UnicodeError:
        raise ValueError('cleanup-invalid-public-unicode') from None
    return value


def schedule_record(model, intent, ticket, original_guard_row):
    """Exact G.accept record projection, not a proof/registry release."""
    adapter_context.checked(model, intent, ticket)
    source = copy.deepcopy(original_guard_row)
    model.require(source.get('schema') == 'hikyo-158/partial-kms-native-response/v1'
                  and source.get('slot') == 'schedule' and source.get('operation') == OPERATION,
                  'whole-original-schedule-required')
    response = model.strict_json(source['rawResponse'].encode())
    return {adapter_context.FIELD: intent[adapter_context.FIELD], 'schema': 'hikyo-158/partial-cleanup-response/v1', 'operation': OPERATION,
            'intentSHA256': model.digest(intent), 'ticketSHA256': model.digest(ticket), 'request': copy.deepcopy(intent['request']),
            'sentAt': _stamp_ms(source['sentAtMS']), 'receivedAt': _stamp_ms(source['receivedAtMS']),
            'forwardsObserved': source['forwardsObserved'], 'httpStatus': source['httpStatus'], 'requestId': source['requestId'],
            'response': response, 'sourceReceipt': source}


def _stamp_ms(value):
    from datetime import datetime, timedelta, timezone
    if type(value) is not int:
        raise ValueError('original integer milliseconds required')
    return (datetime(1970, 1, 1, tzinfo=timezone.utc) + timedelta(milliseconds=value)).isoformat(timespec='milliseconds').replace('+00:00', 'Z')


def accept(coordinator, receipt_proof, authority):
    # Root constructs the existing abort-receipt-ScheduleKeyDeletion bundle only
    # after authenticating its CURRENT WHOLE ROOT COMMAND, not child stdout.
    _coordinator_binding(coordinator)
    return coordinator.accept(OPERATION, receipt_proof, authority)


def readback_manifest(model, key_normalizer, prepared, original_create_intent, approved_create_body,
                      original_native_create, ticket):
    adapter_context.get(model).prepared(model, prepared)
    adapter_context.checked(model, original_create_intent, ticket)
    inputs = ticket['nativeHelperInput']
    return key_normalizer.manifest(model, prepared, original_create_intent, approved_create_body,
                                   'readback', inputs['notBeforeMS'], inputs['expiresAtMS'],
                                   inputs['newKeyARN'], model.digest(original_native_create))


def normalize_current_metadata(model, metadata_bridge, inventory_normalizer, setup_decoder, key_normalizer,
                               prepared, unchanged_full_inputs, captured_read_rows, *, explicit_now):
    adapter_context.get(model).prepared(model, prepared)
    inputs = copy.deepcopy(unchanged_full_inputs)
    model.require(type(captured_read_rows) is list and len(captured_read_rows) == 8, 'eight-genuine-key-originals')
    inputs['kms'] = copy.deepcopy(captured_read_rows)
    return metadata_bridge.normalize_partial_metadata(model, inventory_normalizer, setup_decoder, key_normalizer,
                                                     prepared, inputs, explicit_now=explicit_now)


def _custody():
    from . import k_cleanup_custody
    return k_cleanup_custody


def _coordinator_binding(coordinator):
    m = coordinator.m
    context = adapter_context.get(m)
    context.prepared(m, coordinator.prepared)
    # Reconstruction uses original action time and original reserved binding;
    # current release eligibility is deliberately not a cleanup prerequisite.
    with coordinator.original.lock():
        claim = coordinator.original.claim_record()
        context.checked(m, claim)
        action_at = m.instant(claim['reservedAt'])
        context.cleanup(m, action_at, coordinator.now(), claim, coordinator.prepared)


def reviewed_command_context(model, trusted_source_binding, client_request, reviewed_registry_raw):
    """Root supplies independently reviewed source/runtime binding and bytes.

    Pure derivation, not a source/auth release. No arbitrary command input.
    """
    try:
        return _custody().command_context(model, trusted_source_binding, client_request, reviewed_registry_raw)
    except (KeyError, TypeError, UnicodeError, OverflowError):
        raise ValueError('cleanup-reviewed-context-held') from None


def fresh_readback_plan(model, raw_original_command, ui_result, reviewed_context, verify_native_original,
                        *, trusted_source_binding, reviewed_registry_raw, explicit_now):
    """Whole current original -> exact reviewed argv/receipt -> inert unload plan.

    Root must first authenticate/release its source/runtime binding and registry.
    The unchanged prebound native verifier must enforce the current thread and
    bounded native sequence/context. This adapter implements no authentication.
    """
    try:
        return _fresh_readback_plan(model, raw_original_command, ui_result, reviewed_context, verify_native_original,
            trusted_source_binding=trusted_source_binding, reviewed_registry_raw=reviewed_registry_raw,
            explicit_now=explicit_now)
    except (KeyError, TypeError, UnicodeError, OverflowError):
        raise ValueError('cleanup-current-command-held') from None


def _fresh_readback_plan(model, raw_original_command, ui_result, reviewed_context, verify_native_original,
                         *, trusted_source_binding, reviewed_registry_raw, explicit_now):
    import hashlib
    c = _custody()
    model.require(type(reviewed_context) is dict and type(raw_original_command) is bytes
                  and 0 < len(raw_original_command) <= 1_999_999, 'whole-current-native-context-and-bytes')
    expected = c.command_context(model, trusted_source_binding, reviewed_context['request'], reviewed_registry_raw)
    model.require(model.same(reviewed_context, expected), 'independently-derived-whole-reviewed-command-context')
    # Never rewrite original bytes/input before the existing current-stream gate.
    verified = verify_native_original(raw_original_command)
    model.require(type(verified) is tuple and len(verified) == 2
                  and type(verified[1]) is int and verified[1] > 0, 'existing-Root-verifier-result')
    original, sequence = verified
    model.require(type(original) is dict and model.same(original, model.strict_json(raw_original_command)),
                  'unchanged-whole-current-native-original')
    model.require({'type', 'status', 'exitCode', 'input', 'output', 'startedAt', 'completedAt', 'updatedAt'} <= set(original)
                  and original['type'] == 'command_execution' and original['status'] == 'completed'
                  and type(original['exitCode']) is int and original['exitCode'] == 0
                  and type(original['input']) is str and type(original['output']) is str
                  and len(original['output'].encode()) <= 1000000,
                  'whole-completed-current-root-command')
    model.clock(explicit_now)
    model.fresh(original['completedAt'], explicit_now)
    model.require(model.instant(original['startedAt']) <= model.instant(original['completedAt'])
                  <= model.instant(original['updatedAt']) <= explicit_now
                  and ('threadId' not in original or original['threadId'] == expected['threadId']),
                  'current-command-chronology-and-prebound-thread')
    # Qualified F matcher is representation-only, and runs AFTER both context
    # derivation and actual native verification. It authenticates/executes nothing.
    matcher = c.helper('k_native_carrier.py')
    model.require(matcher.matches_native_carrier(original['input'],
                  expected_inner_command=expected['innerCommand'], expected_carrier_argv=expected['carrierArgv']),
                  'exact-reviewed-R2-client-carrier-argv')
    response = model.strict_json(original['output'].encode())
    wire = c.receipt_checked(model, response, expected, ui_result)
    model.exact(ui_result, {'schema', 'action', 'outcome', 'partialEffect', 'beforeJSON', 'afterJSON',
        'publicCaptureParts', 'controlLeaseClaimed', 'exclusiveControlVerifiedByAdapter', 'writeAuthorityGranted',
        'providerOutcomeProven', 'externalAuthenticityVerified'})
    args = expected['request']['arguments']
    model.require(ui_result['action'] == args['action'] and ui_result['outcome'] in ('captured', 'held')
                  and all(type(ui_result[k]) is str and len(ui_result[k].encode('utf-16-le')) // 2 <= 2048
                          for k in ('beforeJSON', 'afterJSON'))
                  and ui_result['partialEffect'] in ('native-schedule-may-have-forwarded',
                      'observer-state-may-have-changed', 'effect-uncertain-reconcile-before-any-replay')
                  and len(wire.canonical(ui_result).encode()) <= 131072,
                  'exact-bounded-cleanup-command-result')
    before = model.strict_json(ui_result['beforeJSON'].encode())
    after = model.strict_json(ui_result['afterJSON'].encode())
    c.identity(model, before)
    c.identity(model, after)
    intent, ticket, routes = c.arguments_checked(model, args)
    model.require(model.same(before, expected['expectedDocument']) and after['targetURL'] in routes
                  and all(after[key] == before[key] for key in ('targetId', 'targetType', 'browserContextId', 'mainFrameId', 'loaderId')),
                  'exact-original-document-around-cleanup-capture')
    detail = public_detail(model, ui_result)
    model.require(type(detail) is dict and type(detail.get('schedule')) is dict,
                  'retain-whole-schedule-before-unload')
    source = detail['schedule']
    model.exact(source, {'schema', 'slot', 'operation', 'request', 'httpStatus', 'responseEndpoint',
        'sentAtMS', 'receivedAtMS', 'rawResponse', 'rawResponseBytes', 'rawResponseSHA256',
        'requestBodyBytes', 'requestBodySHA256', 'requestId', 'requestIdSource', 'requestIdCaptureState',
        'forwardsObserved', 'guardSourceSHA256', 'helperPreparationSHA256', 'manifestSHA256', 'manifest',
        'JSONParsedByGuard', 'authenticationReadByGuard', 'providerAPIsInitiatedByGuard',
        'externalAuthenticityVerifiedByGuard', 'writeAuthorityGrantedByGuard'})
    manifest = args['preparation']['manifest']
    model.require(source['schema'] == 'hikyo-158/partial-kms-native-response/v1' and source['slot'] == 'schedule'
                  and source['operation'] == 'ScheduleKeyDeletion' and model.same(source['request'], intent['request'])
                  and model.same(source['manifest'], manifest) and source['manifestSHA256'] == model.digest(manifest)
                  and source['helperPreparationSHA256'] == expected['helperPreparationSHA256']
                  and source['guardSourceSHA256'] == ticket['nativeHelperInput']['guardSourceSHA256']
                  and source['requestBodyBytes'] == manifest['requestBodyBytes']
                  and source['requestBodySHA256'] == manifest['requestBodySHA256']
                  and source['responseEndpoint'] == manifest['endpoint']
                  and type(source['forwardsObserved']) is int and source['forwardsObserved'] == 1
                  and type(source['httpStatus']) is int and 100 <= source['httpStatus'] <= 599
                  and all(source[k] is False for k in ('JSONParsedByGuard', 'authenticationReadByGuard',
                      'providerAPIsInitiatedByGuard', 'externalAuthenticityVerifiedByGuard', 'writeAuthorityGrantedByGuard')),
                  'exact-schedule-intent-ticket-preparation-and-source')
    model.require(type(source['sentAtMS']) is int and type(source['receivedAtMS']) is int
                  and manifest['notBeforeMS'] <= source['sentAtMS'] <= source['receivedAtMS'] < manifest['expiresAtMS']
                  and source['receivedAtMS'] - source['sentAtMS'] <= manifest['captureTimeoutMS']
                  and source['sentAtMS'] - manifest['notBeforeMS'] <= 30000
                  and source['receivedAtMS'] <= int(model.instant(original['completedAt']).timestamp() * 1000)
                  and int(explicit_now.timestamp() * 1000) < manifest['expiresAtMS']
                  and (args['action'] != 'schedule' or int(model.instant(original['startedAt']).timestamp() * 1000) <= source['sentAtMS']),
                  'original-cleanup-capture-clock-without-window-renewal')
    model.require(type(source['rawResponse']) is str and type(source['rawResponseBytes']) is int
                  and source['rawResponseBytes'] == len(source['rawResponse'].encode())
                  and 2 <= source['rawResponseBytes'] <= 32768
                  and source['rawResponseSHA256'] == hashlib.sha256(source['rawResponse'].encode()).hexdigest()
                  and source['requestIdSource'] == 'native-response-header:x-amzn-requestid'
                  and (source['requestId'] is None and source['requestIdCaptureState'] == 'unavailable'
                       or type(source['requestId']) is str and 8 <= len(source['requestId']) <= 128
                       and source['requestIdCaptureState'] == 'captured'), 'whole-unmodified-schedule-response-and-header-state')
    return {adapter_context.FIELD: expected[adapter_context.FIELD], 'schema': 'hikyo-158/new-key-cleanup-fresh-readback-plan/v1', 'url': HOME,
        'api': 'officialPage.goto(exact URL); acquire NEW actual CDP identity; run readback-install',
        'retainedWholeCommandRaw': raw_original_command,
        'retainedWholeCommandSHA256': hashlib.sha256(raw_original_command).hexdigest(),
        'verifiedNativeSequence': sequence, 'retainedScheduleSHA256': model.digest(source),
        'retainedReviewedContext': copy.deepcopy(expected), 'retainedExactRequest': copy.deepcopy(expected['request']),
        'retainedR2Receipt': copy.deepcopy(response), 'retainedResponseSHA256': response['responseSHA256'],
        'sameDocumentPreserved': False, 'executionAuthorityGranted': False}


if __name__ == '__main__':
    print(json.dumps(describe(), sort_keys=True, separators=(',', ':')))
