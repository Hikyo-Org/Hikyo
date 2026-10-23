"""Chrome CreateKey-only copy of the finite physical lock holder, with no live defaults.

No import-time I/O. Root supplies configured modules/controller, exact original
context, two owned pipes and clocks. Only explicit calls access the existing
ledger/pipe. No core phase, provider, browser, loader or authority implementation.
The single-use lock marker survives every outcome. All ambiguity reconciles.
"""

from . import adapter_context
from contextlib import contextmanager
import copy
from datetime import timedelta
import math
import os
import re
import select
import stat

from .ancestor_chrome_custody import THREAD
from . import pipe_io


LOCK_ONCE = 'root-native-CreateKey-lock.once.json'
CONTEXT_KEYS = {'attemptTrustBindingSHA256', 'schema', 'threadId', 'chromeCommand', 'preparationSHA256', 'configurationSHA256',
                'claimSHA256', 'stateSHA256', 'intentSHA256', 'ticketSHA256',
                'contractSHA256', 'manifestBundleSHA256', 'controllerSourcesSHA256', 'nativeSources'}
FALSE_FLAGS = {'providerAuthorizationGranted': False, 'externalAuthenticityVerified': False,
               'automaticRetry': False, 'claimCreated': False}


def _monotonic(m, clock):
    value = clock()
    m.require(type(value) in (int, float) and math.isfinite(value) and value >= 0,
              'explicit-finite-monotonic-clock')
    return value


@contextmanager
def _locked_window(*, controller, model, deadline, key_normalizer, context,
                   full_intent, forward_contract, bundle, guard_status,
                   observer_status, explicit_clock, monotonic_clock, maximum_hold_ms, custody):
    m, ledger = model, controller.ledger
    adapter_context.checked(m, context, full_intent, forward_contract, forward_contract['ticket'])
    config = m.configuration_checked()
    m.require(callable(explicit_clock) and callable(monotonic_clock)
              and controller.m is m and controller.now is explicit_clock
              and type(maximum_hold_ms) is int and 1 <= maximum_hold_ms <= 55000,
              'same-configured-controller-and-explicit-finite-clocks')
    m.exact(context, CONTEXT_KEYS)
    custody.command_context_checked(m, context['chromeCommand'])
    m.require(context['schema'] == 'hikyo-158/persistent-chrome-create-lock-context/v1'
              and context['threadId'] == THREAD == context['chromeCommand']['threadId']
              and context['chromeCommand']['kind'] == 'create'
              and all(m.sha(context[k]) for k in CONTEXT_KEYS - {'schema', 'threadId', 'chromeCommand', 'nativeSources'}),
              'closed-Root-reviewed-native-lock-context')
    prepared = controller.prepared
    adapter_context.get(m).prepared(m, prepared)
    m.exact(context['nativeSources'], deadline.PINS)
    m.require(m.same(context['nativeSources'], deadline.PINS)
              and m.same(key_normalizer.PINS, {k: deadline.PINS[k] for k in ('modelSHA256', 'modelTestSHA256')})
              and prepared['proposal']['sourceManifest']['modelSHA256'] == deadline.PINS['modelSHA256']
              and prepared['proposal']['sourceManifest']['testSHA256'] == deadline.PINS['modelTestSHA256']
              and controller.sources['modelSHA256'] == deadline.PINS['modelSHA256']
              and controller.sources['modelTestSHA256'] == deadline.PINS['modelTestSHA256']
              and context['controllerSourcesSHA256'] == m.digest(controller.sources)
              and context['preparationSHA256'] == m.digest(prepared)
              and context['configurationSHA256'] == m.digest(m.release_configuration_checked())
              and context['intentSHA256'] == m.digest(full_intent)
              and context['ticketSHA256'] == m.digest(forward_contract['ticket'])
              and context['contractSHA256'] == m.digest(forward_contract)
              and context['manifestBundleSHA256'] == m.digest(bundle), 'exact-current-source-and-original-context-crosslinks')
    lock_path = ledger.parent / '.provisioning.lock'
    m.require(ledger.parent.name == 'aws-revalidation-20261018b'
              and ledger.folder.name == config['ledgerFolder']
              and ledger.folder.parent == ledger.parent.parent
              and lock_path.is_file() and not lock_path.is_symlink(), 'same-existing-compatible-global-lock')
    with ledger.lock():
        # The underlying Ledger uses LOCK_NB. Include every validation/read
        # under the acquired lock in this budget, not only the pipe wait.
        start = _monotonic(m, monotonic_clock)
        acquired_at = explicit_clock()
        m.clock(acquired_at)
        stop = start + maximum_hold_ms / 1000
        head, state = ledger.snapshot()
        claim = ledger.claim_record()
        expected_claim_keys = {adapter_context.FIELD, 'schema', 'approvalSHA256', 'preparationSHA256', 'heldHistorySHA256',
                               'completedHistorySHA256', 'configurationSHA256', 'sourcePins', 'historicalPins',
                               'usedNamespaces', 'namespace', 'retainedReservations', 'newReservationsCharged',
                               'lifetimeReservations', 'maximumDispatches', 'automaticRetry', 'reservedAt', 'proofSHA256'}
        adapter_context.checked(m, claim)
        m.exact(claim, expected_claim_keys)
        m.require(context['stateSHA256'] == head and context['claimSHA256'] == m.digest(claim)
                  and claim['schema'] == 'hikyo-158/next-exclusive-reservation/v1'
                  and claim['approvalSHA256'] == m.SOURCE_PINS['approval']
                  and claim['preparationSHA256'] == m.digest(prepared)
                  and claim['configurationSHA256'] == m.digest(m.release_configuration_checked())
                  and claim['heldHistorySHA256'] == m.SOURCE_PINS['heldHistory'] == m.digest(prepared['proposal']['inputs']['heldHistory'])
                  and claim['completedHistorySHA256'] == m.SOURCE_PINS['completedAttempt'] == m.digest(prepared['proposal']['inputs']['completedAttempt'])
                  and m.same(claim['sourcePins'], controller.sources) and m.same(claim['historicalPins'], m.SOURCE_PINS)
                  and m.same(claim['usedNamespaces'], prepared['proposal']['usedNamespaces'])
                  and claim['namespace'] == prepared['namespace'] == config['namespace']
                  and all(type(claim[k]) is int and claim[k] == v for k, v in (
                      ('retainedReservations', m.RETAINED_RESERVATIONS), ('newReservationsCharged', m.NEW_RESERVATIONS),
                      ('lifetimeReservations', m.LIFETIME_RESERVATIONS), ('maximumDispatches', 1)))
                  and claim['automaticRetry'] is False, 'exact-configured-exclusive-claim')
        m.exact(state, {adapter_context.FIELD, 'claimSHA256', 'phase', 'held', 'pending', 'completed', 'owner', 'roles', 'provider',
                        'keyARN', 'boundaryPolicyId', 'boundSHA256', 'sealedSHA256', 'dispatch'})
        adapter_context.checked(m, state)
        m.require(state['claimSHA256'] == m.digest(claim) and state['phase'] == 'reserved'
                  and state['held'] is False and m.same(state['pending'], full_intent)
                  and state['completed'] == [] and state['owner'] == {} and state['roles'] == {}
                  and all(state[k] is None for k in ('provider', 'keyARN', 'boundaryPolicyId', 'boundSHA256', 'sealedSHA256', 'dispatch'))
                  and m.same(ledger.read_once('owner-CreateKey-intent.once.json'), full_intent)
                  and m.same(ledger.read_once('owner-CreateKey-transmit.once.json'), forward_contract['ticket']),
                  'exact-pending-key-only-intent-and-already-burned-ticket')
        for forbidden in ('held.once.json', 'owner-CreateKey-response.once.json', LOCK_ONCE):
            try:
                ledger.read_once(forbidden)
            except FileNotFoundError:
                continue
            raise ValueError('future-native-lock:held-response-or-lock-window-already-exists')
        now = explicit_clock()
        m.clock(now)
        m.admit_current_release(prepared, now)
        adapter_context.get(m).forward(m, now, context, claim, state, full_intent, forward_contract)
        checked = deadline.recheck(m, key_normalizer, prepared, full_intent, forward_contract,
                                   bundle, guard_status, observer_status, phase='arm', explicit_now=now,
                                   minimum_remaining_ms=60000)
        m.require(m.instant(claim['reservedAt']) <= m.instant(full_intent['armedAt'])
                  <= m.instant(forward_contract['ticket']['issuedAt']) <= now,
                  'original-reservation-intent-ticket-chronology')
        m.require(_monotonic(m, monotonic_clock) < stop, 'lock-validation-budget-exhausted-before-ready')
        record = {adapter_context.FIELD: context[adapter_context.FIELD], 'schema': 'hikyo-158/chrome-create-lock-window/v1', **FALSE_FLAGS,
                  'lockAcquiredAt': m.stamp(acquired_at), 'readyAt': m.stamp(now), 'maximumHoldMS': maximum_hold_ms,
                  'monotonicDeadline': stop, 'monotonicAcquired': start,
                  'sendDeadlineMS': bundle['guardManifest']['expiresAtMS'],
                  'contextSHA256': m.digest(context), 'preflightSHA256': m.digest(checked),
                  'claimSHA256': m.digest(claim), 'stateSHA256': head,
                  'intentSHA256': m.digest(full_intent), 'ticketSHA256': m.digest(forward_contract['ticket']),
                  'manifestBundleSHA256': m.digest(bundle), 'nativeSources': copy.deepcopy(context['nativeSources'])}
        ledger.once(LOCK_ONCE, record)  # Permanent single-use burn, including timeout/error.
        m.require(_monotonic(m, monotonic_clock) < stop, 'lock-burn-budget-exhausted-before-ready')
        yield record, stop


def _settled_original_checked(m, kn, prepared, full_intent, bundle, context, window, original_raw, now,
                              *, native_connection, sequence_min, sequence_max, custody):
    adapter_context.checked(m, context, window, full_intent)
    adapter_context.get(m).prepared(m, prepared)
    checked = custody.command_readout_checked(m, original_raw, context['chromeCommand'],
        connection=native_connection, sequence_min=sequence_min, sequence_max=sequence_max, explicit_now=now)
    original, readout = checked['original'], checked['readout']
    value = checked['publicValue']
    m.exact(value, {'create', 'guard'})
    row, guard = value['create'], value['guard']
    m.exact(row, kn.ROW_FIELDS)
    received = m.instant(kn.stamp(row['receivedAtMS']))
    sent = m.instant(kn.stamp(row['sentAtMS']))
    created = kn.row_checked(m, prepared, bundle['normalizedIntent'], bundle['approvedCreateBody'], row,
                             'create', 'create', 'CreateKey', None, received)
    m.require(m.same(row['manifest'], bundle['observerManifest']), 'exact-installed-original-observer-manifest')
    m.first_key_forward_window(prepared, explicit_now=sent, minimum_remaining_ms=1)
    m.exact(created, {'KeyMetadata'})
    meta = created['KeyMetadata']
    required = {'Arn', 'KeyId', 'AWSAccountId', 'KeyState', 'Enabled', 'Origin', 'KeyUsage',
                'KeySpec', 'MultiRegion', 'CreationDate', 'KeyManager'}
    optional = {'CustomerMasterKeySpec', 'EncryptionAlgorithms', 'Description', 'CurrentKeyMaterialId'}
    m.require(type(meta) is dict and required <= set(meta) <= required | optional,
              'closed-original-created-key-metadata')
    m.require(type(meta['KeyId']) is str and re.fullmatch(r'[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12}', meta['KeyId'])
              and meta['Arn'] == f'arn:aws:kms:{m.REGION}:{m.ACCOUNT}:key/' + meta['KeyId']
              and meta['Arn'] not in m.scheduled_key_closure(prepared['proposal'])
              and meta['AWSAccountId'] == m.ACCOUNT and meta['KeyState'] == 'Enabled' and meta['Enabled'] is True
              and meta['Origin'] == 'AWS_KMS' and meta['KeyUsage'] == 'ENCRYPT_DECRYPT'
              and meta['KeySpec'] == 'SYMMETRIC_DEFAULT' and meta['MultiRegion'] is False and meta['KeyManager'] == 'CUSTOMER'
              and type(meta['CreationDate']) in (int, float) and math.isfinite(meta['CreationDate'])
              and sent.timestamp() <= meta['CreationDate'] <= received.timestamp()
              and ('Description' not in meta or meta['Description'] == full_intent['request']['Description'])
              and ('CustomerMasterKeySpec' not in meta or meta['CustomerMasterKeySpec'] == 'SYMMETRIC_DEFAULT')
              and ('EncryptionAlgorithms' not in meta or meta['EncryptionAlgorithms'] == ['SYMMETRIC_DEFAULT']),
              'exact-original-new-key-response-identity-and-request')
    m.exact(guard, {'schema', 'phase', 'claimConsumed', 'nativeSendInvocations', 'blocked', 'activeReads',
                    'successfulReadProbes', 'lastReadTarget', 'responseStatus', 'reason', 'responseEndpointMatched',
                    'intentSHA256', 'wireBodySHA256', 'endpoint', 'durabilityVerifiedByPage', 'ownerIdentityVerifiedByPage',
                    'nativeSendCounterScope', 'loadedSourceBytesVerifiedByPage', 'redirectErrorSupported',
                    'physicalHTTPHopBoundEstablished', 'coversOtherRealmsOrCapturedMethods', 'cleanup'})
    m.require(guard['schema'] == 'hikyo-158/native-kms-xhr-write-status/v1' and guard['phase'] == 'settled'
              and guard['claimConsumed'] is True and type(guard['nativeSendInvocations']) is int and guard['nativeSendInvocations'] == 1
              and type(guard['activeReads']) is int and guard['activeReads'] == 0
              and type(guard['blocked']) is int and guard['blocked'] >= 0
              and type(guard['successfulReadProbes']) is int and guard['successfulReadProbes'] >= 1
              and guard['lastReadTarget'] in ('TrentService.ListKeys', 'TrentService.ListKeysForService',
                  'TrentService.ListAliases', 'TrentService.DescribeKey', 'TrentService.GetKeyPolicy',
                  'TrentService.GetKeyRotationStatus', 'TrentService.ListResourceTags')
              and type(guard['responseStatus']) is int and guard['responseStatus'] == 200
              and guard['reason'] is None and guard['responseEndpointMatched'] is True
              and guard['intentSHA256'] == bundle['guardManifest']['intentSHA256']
              and guard['wireBodySHA256'] == bundle['guardManifest']['wireBodySHA256']
              and guard['endpoint'] == bundle['guardManifest']['endpoint']
              and guard['nativeSendCounterScope'] == 'CreateKey-only-excludes-metadata-reads'
              and guard['cleanup'] == 'close-controlled-tab'
              and all(guard[k] is False for k in ('durabilityVerifiedByPage', 'ownerIdentityVerifiedByPage',
                  'loadedSourceBytesVerifiedByPage', 'redirectErrorSupported', 'physicalHTTPHopBoundEstablished', 'coversOtherRealmsOrCapturedMethods')),
              'whole-settled-exact-one-native-guard-custody')
    m.require(m.instant(window['readyAt']) <= sent <= received
              <= m.instant(kn.stamp(readout['readAtMS'])) <= m.instant(original['completedAt']) <= now
              and row['sentAtMS'] < window['sendDeadlineMS']
              and m.instant(window['readyAt']) <= m.instant(original['startedAt'])
              <= m.instant(original['completedAt']) <= m.instant(original['updatedAt']) <= now
              and now <= m.instant(window['lockAcquiredAt']) + timedelta(milliseconds=window['maximumHoldMS']),
              'native-send-and-whole-original-within-physical-window')
    return {adapter_context.FIELD: context[adapter_context.FIELD], 'keyARN': meta['Arn'], 'requestId': row['requestId'],
            'nativeOriginalSHA256': m.digest(original),
            'nativeOriginalRawSHA256': checked['originalRawSHA256'],
            'nativeOriginalSequence': checked['originalSequence'],
            'chromeCommandContextSHA256': checked['commandContextSHA256'],
            'nativeCreateSHA256': m.digest(row),
            'guardStatusSHA256': m.digest(guard),
            **{k: checked[k] for k in ('sourceBindingSHA256','transportSourceSHA256','transportReviewSHA256',
                                      'sourceContractSHA256','scriptSHA256','persistentRequestId','persistentSequence',
                                      'responseSHA256','readoutSHA256')}}


def hold_create_key_window(*, controller, model, deadline, key_normalizer, context,
                           full_intent, forward_contract, bundle, guard_status, observer_status,
                           release_fd, ready_fd, explicit_clock, monotonic_clock, maximum_hold_ms,
                           native_connection, sequence_min, sequence_max, custody):
    """One finite pipe handshake; no callbacks, provider actions or phase calls.

    Root sends exactly one complete original JSON object after independent
    SQL/native authentication and retaining the whole file. Missing/non200/
    ambiguous outcomes throw, unlock and reconcile only; the once marker stays.
    """
    m = model
    m.require(type(release_fd) is int and release_fd >= 0 and type(ready_fd) is int
              and ready_fd >= 0 and release_fd != ready_fd, 'explicit-distinct-Root-owned-pipes')
    m.require(all(stat.S_ISFIFO(os.fstat(fd).st_mode) and not os.get_blocking(fd)
                  for fd in (release_fd, ready_fd)), 'explicit-nonblocking-owned-pipes-not-files-or-terminals')
    with _locked_window(controller=controller, model=m, deadline=deadline, key_normalizer=key_normalizer,
                        context=context, full_intent=full_intent, forward_contract=forward_contract,
                        bundle=bundle, guard_status=guard_status, observer_status=observer_status,
                        explicit_clock=explicit_clock, monotonic_clock=monotonic_clock,
                        maximum_hold_ms=maximum_hold_ms, custody=custody) as (window, stop):
        ready = m.canonical(window) + b'\n'
        m.require(len(ready) <= 4096, 'bounded-public-ready-descriptor')
        pipe_io.write_all(ready_fd, ready, stop=stop, clock=monotonic_clock)
        raw = pipe_io.read_frame(release_fd, limit=2_000_000, stop=stop, clock=monotonic_clock)
        m.require(_monotonic(m, monotonic_clock) <= stop, 'single-whole-original-within-finite-window')
        original_raw = raw[:-1]  # Strip only the pipe delimiter, never reserialize native bytes.
        now = explicit_clock()
        m.clock(now)
        checked = _settled_original_checked(m, key_normalizer, controller.prepared, full_intent,
                                            bundle, context, window, original_raw, now,
                                            native_connection=native_connection,
                                            sequence_min=sequence_min, sequence_max=sequence_max, custody=custody)
        result = {adapter_context.FIELD: context[adapter_context.FIELD], 'schema': 'hikyo-158/chrome-create-lock-released/v1', **FALSE_FLAGS,
                  'lockHeld': False, 'settledCustodyValidatedForRelease': True,
                  'lockWindowSHA256': m.digest(window), **checked}
        m.require(_monotonic(m, monotonic_clock) <= stop,
                  'settled-validation-timeout-reconcile-only')
    return result
