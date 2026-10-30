"""Future-only pure CreateKey manifest recipe. No I/O, clock, guard or authority.

Root authenticates original intent/ticket/source/status captures independently.
The existing current claim cannot use or adopt this future source successor.
"""
import copy

PINS = {'modelSHA256': 'f40c07b2204c3305b434836015c99f30af4dfc893a0ce0e4f0318c17ea844a19', 'modelTestSHA256': 'f06b2e173ff499070ae8718d65870534510a585339b8fc451462baaea6dc5985', 'keyNormalizerSHA256': '4df1d41122616c47f44864d74ffaeaf974d72f72216745e284d538679045a2cf', 'guardSHA256': '6182330cb0a73fcc6e9f31dd5e5080b952e89735e40d0e9e3f961b5c9a145f26', 'observerSHA256': '5a68216b0e07b7fe207b1f361f2ab7192d3e28e2c13ae1adee8b20211e398700'}
FLAGS={'runtimeReady':False,'executionAuthorityGranted':False,'sourceAuthenticityEstablished':False,
    'claimCreated':False,'nativeActionPerformed':False}
MIN_PRECLAIM_MS=120000
MIN_NATIVE_STAGE_MS=60000
STEPS=['Root authenticates exact current operation and original source',
    'Compatible global lock and guard burn immediately adjacent to one forward',
    'Preserve complete original response custody before controller receipt/CAS',
    'Human deployment approval remains separate']


def preclaim_window(m,prepared,*,explicit_now,minimum_remaining_ms):
    """Must be called by a future reviewed driver BEFORE reservation, no claim here."""
    m.require(type(minimum_remaining_ms) is int and MIN_PRECLAIM_MS<=minimum_remaining_ms<=300000,
        'Root-preclaim-startup-buffer-at-least-120000ms')
    return m.first_key_forward_window(prepared,explicit_now=explicit_now,minimum_remaining_ms=minimum_remaining_ms)


def _reconstruct_window(m,prepared,window):
    m.require(type(window) is dict and 'observedAt' in window and 'minimumRemainingMS' in window,'whole-original-preclaim-window')
    expected=preclaim_window(m,prepared,explicit_now=m.instant(window['observedAt']),minimum_remaining_ms=window['minimumRemainingMS'])
    m.require(m.same(expected,window),'exact-original-preclaim-window-reconstruction')


def _intent(m,prepared,full,contract,now):
    m.exact(full,{'schema','operation','request','preparationSHA256','attemptTrustBindingSHA256','boundSHA256','stateSHA256','claimId','maximumRequests','armedAt','proofSHA256'})
    m.require(full['schema']=='hikyo-158/next-owner-intent/v1' and full['operation']=='CreateKey'
        and full['preparationSHA256']==m.digest(prepared) and full['boundSHA256'] is None
        and m.sha(full['stateSHA256']) and m.sha(full['proofSHA256']) and full['claimId']=='next-owner-CreateKey'
        and type(full['maximumRequests']) is int and full['maximumRequests']==1,'exact-future-controller-key-intent')
    request=full['request'];m.exact(request,{'Policy','Description','Origin','Tags','KeyUsage','KeySpec','MultiRegion'})
    m.require(request['Origin']=='AWS_KMS' and request['Tags']==[] and request['KeyUsage']=='ENCRYPT_DECRYPT'
        and request['KeySpec']=='SYMMETRIC_DEFAULT' and request['MultiRegion'] is False
        and type(request['Description']) is str and 1<=len(request['Description'])<=8192,'closed-future-key-request')
    policy=request['Policy'];m.exact(policy,{'Version','Id','Statement'})
    m.require(policy['Version']=='2012-10-17' and type(policy['Id']) is str and 1<=len(policy['Id'])<=128
        and policy['Statement']==[{'Sid':'Enable IAM User Permissions','Effect':'Allow',
            'Principal':{'AWS':f'arn:aws:iam::{m.ACCOUNT}:root'},'Action':'kms:*','Resource':'*'}],'unchanged-root-delegation-policy')
    m.exact(contract,{'operation','endpoint','request','intentSHA256','ticket','maximumForwards','automaticRetry',
        'runtimeReady','executionAuthorityGranted','requiredExternalSteps','attemptTrustBindingSHA256'})
    ticket=contract['ticket'];m.exact(ticket,{'intentSHA256','maximumRequests','issuedAt','attemptTrustBindingSHA256'})
    m.require(full['attemptTrustBindingSHA256']==contract['attemptTrustBindingSHA256']==ticket['attemptTrustBindingSHA256']
        ==prepared['attemptTrustBindingSHA256'],'same-native-forward-trust-binding')
    m.require(contract['operation']=='CreateKey' and contract['endpoint']==f'https://kms.{m.REGION}.amazonaws.com/'
        and m.same(contract['request'],request) and contract['intentSHA256']==m.digest(full)
        and type(contract['maximumForwards']) is int and contract['maximumForwards']==1
        and contract['automaticRetry'] is False and contract['runtimeReady'] is False and contract['executionAuthorityGranted'] is False
        and contract['requiredExternalSteps']==STEPS and ticket['intentSHA256']==m.digest(full)
        and type(ticket['maximumRequests']) is int and ticket['maximumRequests']==1
        and m.instant(full['armedAt'])<=m.instant(ticket['issuedAt'])<=now,'exact-original-key-forward-contract')
    m.fresh(full['armedAt'],now);m.fresh(ticket['issuedAt'],now)
    return {'preparationSHA256':full['preparationSHA256'],'claimId':full['claimId'],
        'intent':{'operation':'CreateKey','account':m.ACCOUNT,'region':m.REGION,
            'actorARN':prepared['records']['owner']['ownerContext']['principalARN'],'maximumForwards':1,'request':copy.deepcopy(request)}}


def create_manifests(m,kn,source_pins,prepared,full_intent,forward_contract,approved_body,preclaim,*,
        explicit_now,minimum_remaining_ms,redirect_limitation_reviewed):
    """Pure replacement for R6's now+300000 recipe; never reserves or arms."""
    m.exact(source_pins,PINS);m.require(m.same(source_pins,PINS),'exact-reviewed-deadline-dependencies')
    m.require(redirect_limitation_reviewed is True,'explicit-existing-redirect-limitation-review')
    m.require(type(minimum_remaining_ms) is int and MIN_NATIVE_STAGE_MS<=minimum_remaining_ms<=300000,
        'Root-native-startup-buffer-at-least-60000ms')
    _reconstruct_window(m,prepared,preclaim)
    m.require(m.instant(preclaim['observedAt'])<=m.instant(full_intent['armedAt']),'preclaim-check-before-owner-intent')
    current=m.first_key_forward_window(prepared,explicit_now=explicit_now,minimum_remaining_ms=minimum_remaining_ms)
    normalized=_intent(m,prepared,full_intent,forward_contract,explicit_now)
    kn.intent_checked(m,prepared,normalized,approved_body,explicit_now)
    start=kn.ms(explicit_now);end=min(preclaim['expiresAtMS'],current['expiresAtMS'])
    # Both use the SAME conservative immutable source ceiling, not arm time+5m.
    manifest=kn.manifest(m,prepared,normalized,approved_body,'create',start,end)
    guard={'schema':'hikyo-158/native-kms-xhr-write-manifest/v1','endpoint':manifest['endpoint'],
        'wireBodySHA256':manifest['wireBodySHA256'],'wireBodyBytes':manifest['wireBodyBytes'],
        'intentSHA256':manifest['intentSHA256'],'publicMainSHA256':manifest['publicMainSHA256'],
        'expiresAtMS':end,'timeoutMS':20000,'redirectLimitationReviewed':True}
    return {'schema':'hikyo-158/future-key-deadline-manifests/v1',**FLAGS,'sourcePins':copy.deepcopy(source_pins),
        'preparationSHA256':m.digest(prepared),'preclaimWindow':copy.deepcopy(preclaim),'manifestWindow':current,
        'controllerIntentSHA256':m.digest(full_intent),'forwardContractSHA256':m.digest(forward_contract),
        'normalizedIntent':normalized,'approvedCreateBody':approved_body,'observerManifest':manifest,'guardManifest':guard}


def recheck(m,kn,prepared,full_intent,forward_contract,bundle,guard_status,observer_status,*,
        phase,explicit_now,minimum_remaining_ms):
    """Call adjacent to EACH native arm/Finish; output is not action authority."""
    m.require(phase in ('arm','finish'),'closed-native-preflight-phase')
    m.require(type(minimum_remaining_ms) is int and MIN_NATIVE_STAGE_MS<=minimum_remaining_ms<=300000,
        'Root-native-startup-buffer-at-least-60000ms')
    m.exact(bundle,{'schema',*FLAGS,'sourcePins','preparationSHA256','preclaimWindow','manifestWindow',
        'controllerIntentSHA256','forwardContractSHA256','normalizedIntent','approvedCreateBody','observerManifest','guardManifest'})
    built=bundle['manifestWindow']
    expected=create_manifests(m,kn,bundle['sourcePins'],prepared,full_intent,forward_contract,bundle['approvedCreateBody'],bundle['preclaimWindow'],
        explicit_now=m.instant(built['observedAt']),minimum_remaining_ms=built['minimumRemainingMS'],redirect_limitation_reviewed=True)
    m.require(m.same(expected,bundle),'whole-original-installed-manifests-no-renewal')
    m.require(m.instant(built['observedAt'])<=explicit_now,'native-preflight-after-installation')
    current=m.first_key_forward_window(prepared,explicit_now=explicit_now,minimum_remaining_ms=minimum_remaining_ms)
    _intent(m,prepared,full_intent,forward_contract,explicit_now)
    m.require(bundle['guardManifest']['expiresAtMS']==bundle['observerManifest']['expiresAtMS']
        and bundle['guardManifest']['expiresAtMS']<=current['expiresAtMS'],'installed-guard-before-all-source-deadlines')
    m.exact(guard_status,{'schema','phase','claimConsumed','nativeSendInvocations','blocked','activeReads','successfulReadProbes','lastReadTarget',
        'responseStatus','reason','responseEndpointMatched','intentSHA256','wireBodySHA256','endpoint','durabilityVerifiedByPage',
        'ownerIdentityVerifiedByPage','nativeSendCounterScope','loadedSourceBytesVerifiedByPage','redirectErrorSupported',
        'physicalHTTPHopBoundEstablished','coversOtherRealmsOrCapturedMethods','cleanup'})
    m.require(guard_status['schema']=='hikyo-158/native-kms-xhr-write-status/v1' and guard_status['phase']==('observing' if phase=='arm' else 'armed')
        and guard_status['claimConsumed'] is False and type(guard_status['nativeSendInvocations']) is int and guard_status['nativeSendInvocations']==0
        and type(guard_status['activeReads']) is int and guard_status['activeReads']==0
        and type(guard_status['blocked']) is int and guard_status['blocked']>=0
        and type(guard_status['successfulReadProbes']) is int and guard_status['successfulReadProbes']>=1
        and guard_status['lastReadTarget'] in ('TrentService.ListKeys','TrentService.ListKeysForService','TrentService.ListAliases',
            'TrentService.DescribeKey','TrentService.GetKeyPolicy','TrentService.GetKeyRotationStatus','TrentService.ListResourceTags')
        and guard_status['responseStatus'] is None and guard_status['reason'] is None and guard_status['responseEndpointMatched'] is None
        and guard_status['intentSHA256']==bundle['guardManifest']['intentSHA256']
        and guard_status['wireBodySHA256']==bundle['guardManifest']['wireBodySHA256'] and guard_status['endpoint']==bundle['guardManifest']['endpoint']
        and all(guard_status[k] is False for k in ('durabilityVerifiedByPage','ownerIdentityVerifiedByPage','loadedSourceBytesVerifiedByPage',
            'redirectErrorSupported','physicalHTTPHopBoundEstablished','coversOtherRealmsOrCapturedMethods'))
        and guard_status['nativeSendCounterScope']=='CreateKey-only-excludes-metadata-reads' and guard_status['cleanup']=='close-controlled-tab',
        'healthy-exact-native-guard-before-forward')
    m.exact(observer_status,{'schema','mode','readySlots','pendingCaptures','error','providerAPIsCalledByObserver','ownerAuthenticatedByObserver',
        'publicSourceAuthenticatedByObserver','writeAuthorityGrantedByObserver','coversOtherRealmsOrCapturedMethods','physicalHTTPHopBoundEstablished','cleanup'})
    m.require(observer_status['schema']=='hikyo-158/next-key-observer-status/v1' and observer_status['mode']=='create'
        and observer_status['readySlots']==[] and type(observer_status['pendingCaptures']) is int and observer_status['pendingCaptures']==0
        and observer_status['error'] is None and all(observer_status[k] is False for k in ('providerAPIsCalledByObserver','ownerAuthenticatedByObserver',
            'publicSourceAuthenticatedByObserver','writeAuthorityGrantedByObserver','coversOtherRealmsOrCapturedMethods','physicalHTTPHopBoundEstablished'))
        and observer_status['cleanup']=='full-navigation-controlled-tab-preserves-session','healthy-original-create-observer')
    return {'schema':'hikyo-158/future-native-key-preflight/v1',**FLAGS,'phase':phase,'observedAt':m.stamp(explicit_now),
        'manifestBundleSHA256':m.digest(bundle),'actualNowWindow':current,
        'guardStatusSHA256':m.digest(guard_status),'observerStatusSHA256':m.digest(observer_status)}
