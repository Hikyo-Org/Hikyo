"""Read-only stage data checks; all native authority/authenticity remains Root-owned."""

from . import adapter_context
import copy
from datetime import timedelta
from pathlib import Path

from . import runner as h_runner
from . import stage_custody

HERE = Path(__file__).resolve().parent


def descriptor(*, m, kn, deadline, values, review, index_ref, ready_raw, claim, state,
               state_head, intent_record, ticket_record, locked_window, stage_registry, status_context,
               status_raw, connection, source_pins, phase, explicit_now, final_clock, monotonic_now):
    trust = adapter_context.checked(m, review, values['lockContext'], values['sourceBinding'], values['fullIntent'], values['forwardContract'], values['forwardContract']['ticket'], claim, state, intent_record, ticket_record, locked_window, status_context)
    trust.prepared(m, values['prepared'])
    m.require(phase in ('arm', 'finish'), 'closed-native-stage-preflight-phase')
    m.exact(values, h_runner.INPUTS)
    m.require(review['schema'] == 'hikyo-158/persistent-create-holder-review/v1'
        and review['maximumHoldMS'] == 55000 and m.same(review['inputIndex'], index_ref)
        and m.same(review['inputDigests'], {k: m.digest(v) for k, v in values.items()})
        and m.same(review['sourceBinding'], values['sourceBinding'])
        and m.same(review['lockContext'], values['lockContext']), 'same-whole-reviewed-H-index-and-context')
    m.require(ready_raw.endswith(b'\n') and ready_raw.count(b'\n') == 1 and len(ready_raw) <= 4096,
              'whole-retained-H-ready-frame')
    window = m.strict_json(ready_raw[:-1]); h_runner.window_checked(m, window, review, monotonic_now)
    m.require(m.same(window, locked_window), 'same-permanent-H-lock-once-window')
    context = values['lockContext']; prepared = values['prepared']; bundle = values['nativeManifestBundle']
    stage_custody.h_custody.Custody(values['sourceBinding']).command_context_checked(m, context['chromeCommand'])
    m.configuration_checked(); m.preparation_checked(prepared)
    m.require(m.same(source_pins, trust.stage_pins())
        and m.same(context['nativeSources'], deadline.PINS) and source_pins['modelSHA256'] == deadline.PINS['modelSHA256']
        and source_pins['modelTestSHA256'] == deadline.PINS['modelTestSHA256']
        and m.same(kn.PINS, {k: deadline.PINS[k] for k in ('modelSHA256', 'modelTestSHA256')}), 'same-frozen-configured-native-sources')
    m.exact(claim, {adapter_context.FIELD, 'schema', 'approvalSHA256', 'preparationSHA256', 'heldHistorySHA256', 'completedHistorySHA256',
        'configurationSHA256', 'sourcePins', 'historicalPins', 'usedNamespaces', 'namespace', 'retainedReservations',
        'newReservationsCharged', 'lifetimeReservations', 'maximumDispatches', 'automaticRetry', 'reservedAt', 'proofSHA256'})
    m.require(m.digest(claim) == context['claimSHA256'] == window['claimSHA256']
        and claim['schema'] == 'hikyo-158/next-exclusive-reservation/v1'
        and claim['preparationSHA256'] == m.digest(prepared) == context['preparationSHA256']
        and claim['configurationSHA256'] == m.digest(m.release_configuration_checked()) == context['configurationSHA256']
        and m.digest(claim['sourcePins']) == context['controllerSourcesSHA256'], 'same-original-claimed-preparation-and-sources')
    config = m.configuration_checked()
    m.require(claim['approvalSHA256'] == m.SOURCE_PINS['approval']
        and claim['heldHistorySHA256'] == m.SOURCE_PINS['heldHistory'] == m.digest(prepared['proposal']['inputs']['heldHistory'])
        and claim['completedHistorySHA256'] == m.SOURCE_PINS['completedAttempt'] == m.digest(prepared['proposal']['inputs']['completedAttempt'])
        and m.same(claim['historicalPins'], m.SOURCE_PINS)
        and m.same(claim['usedNamespaces'], prepared['proposal']['usedNamespaces'])
        and claim['namespace'] == prepared['namespace'] == config['namespace']
        and all(type(claim[k]) is int and claim[k] == v for k, v in (
            ('retainedReservations', m.RETAINED_RESERVATIONS), ('newReservationsCharged', m.NEW_RESERVATIONS),
            ('lifetimeReservations', m.LIFETIME_RESERVATIONS), ('maximumDispatches', 1)))
        and claim['automaticRetry'] is False, 'same-whole-configured-exclusive-claim-policies')
    m.exact(state, {adapter_context.FIELD, 'claimSHA256', 'phase', 'held', 'pending', 'completed', 'owner', 'roles', 'provider',
                   'keyARN', 'boundaryPolicyId', 'boundSHA256', 'sealedSHA256', 'dispatch'})
    m.require(state_head == context['stateSHA256'] == window['stateSHA256']
        and state['claimSHA256'] == m.digest(claim) and state['phase'] == 'reserved' and state['held'] is False
        and m.same(state['pending'], values['fullIntent']) and state['completed'] == []
        and state['owner'] == {} and state['roles'] == {}
        and all(state[k] is None for k in ('provider', 'keyARN', 'boundaryPolicyId', 'boundSHA256', 'sealedSHA256', 'dispatch'))
        and m.same(intent_record, values['fullIntent']) and m.same(ticket_record, values['forwardContract']['ticket'])
        and m.digest(intent_record) == context['intentSHA256'] == window['intentSHA256']
        and m.digest(ticket_record) == context['ticketSHA256'] == window['ticketSHA256']
        and m.digest(values['forwardContract']) == context['contractSHA256']
        and m.digest(bundle) == context['manifestBundleSHA256'] == window['manifestBundleSHA256'], 'current-pending-intent-burned-ticket-no-replay')
    checked = stage_custody.status_original(m, values['sourceBinding'], stage_registry, status_context, status_raw,
        connection=connection, sequence_min=review['sequenceMin'], sequence_max=review['sequenceMax'], explicit_now=explicit_now)
    m.require(callable(final_clock), 'explicit-current-clock-after-native-SQL')
    explicit_now = final_clock(); m.clock(explicit_now)
    m.require(m.instant(claim['reservedAt']) <= m.instant(values['fullIntent']['armedAt'])
        <= m.instant(values['forwardContract']['ticket']['issuedAt']) <= explicit_now,
        'same-original-reservation-intent-ticket-chronology')
    args = status_context['request']['arguments']
    shared = {k: bundle['observerManifest'][k] for k in ('notBeforeMS', 'expiresAtMS')}
    m.require(window['sendDeadlineMS'] == shared['expiresAtMS']
        and args['phase'] == phase and m.same(args['manifest'], bundle['observerManifest'])
        and m.same(args['window'], shared) and m.same(args['expected'], context['chromeCommand']['identity'])
        and m.instant(window['readyAt']) <= m.instant(checked['original']['startedAt'])
        <= m.instant(checked['original']['completedAt']) <= explicit_now, 'whole-current-status-after-H-ready')
    status = checked['value']
    # Exact unchanged G validator, on the SAME configured model/normalizer.
    m.admit_current_release(prepared, explicit_now)
    trust.forward(m, explicit_now, claim, state, intent_record, ticket_record)
    current = deadline.recheck(m, kn, prepared, values['fullIntent'], values['forwardContract'], bundle,
        status['guard'], status['observer'], phase=phase, explicit_now=explicit_now, minimum_remaining_ms=60000)
    now_ms = kn.ms(explicit_now)
    physical_start = kn.ms(m.instant(window['lockAcquiredAt']))
    m.require(m.instant(window['readyAt']) <= explicit_now
        <= m.instant(window['lockAcquiredAt']) + timedelta(milliseconds=55000)
        and bundle['guardManifest']['expiresAtMS'] == shared['expiresAtMS'], 'original-H-physical-and-shared-window-no-renewal')
    h_runner.window_checked(m, window, review, monotonic_now)
    before = min(now_ms + 2000, physical_start + 55000, shared['expiresAtMS'] - 60000)
    m.require(before > now_ms, 'adjacent-native-descriptor-window-exhausted')
    return {adapter_context.FIELD: trust.binding_sha256, 'schema': 'hikyo-158/native-create-stage-preflight/v1', 'phase': phase, 'issuedAtMS': now_ms,
        'effectBeforeMS': before, 'sharedWindow': shared, 'physicalWindow': {'notBeforeMS': physical_start, 'expiresAtMS': physical_start + 55000},
        'expectedIdentity': copy.deepcopy(args['expected']), 'sourcePins': copy.deepcopy(source_pins),
        'normalizedIntentSHA256': m.digest(bundle['normalizedIntent']),
        'holderContextSHA256': m.digest(review), 'readyRawSHA256': stage_custody.h_custody.raw_sha(ready_raw),
        'lockWindowSHA256': m.digest(window), 'holderIndexRawSHA256': index_ref['rawSHA256'],
        **{k: context[k] for k in ('preparationSHA256', 'claimSHA256', 'stateSHA256', 'intentSHA256', 'ticketSHA256', 'manifestBundleSHA256')},
        'statusOriginalRawSHA256': checked['originalRawSHA256'], 'statusOriginalSHA256': checked['originalSHA256'],
        'statusOriginalSequence': checked['originalSequence'], 'statusContextSHA256': checked['contextSHA256'],
        'preflightSHA256': m.digest(current), 'guardManifest': copy.deepcopy(bundle['guardManifest']),
        'observerManifest': copy.deepcopy(bundle['observerManifest']), 'expectedGuardStatus': copy.deepcopy(status['guard']),
        'expectedObserverStatus': copy.deepcopy(status['observer']), 'requiredRemainingMS': 60000, 'maximumActionAgeMS': 2000,
        'writeAuthorityGranted': False, 'externalAuthenticityVerified': False, 'runtimeReady': False}
