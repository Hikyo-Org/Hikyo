"""Explicit source-only configuration/preparation, never Root authentication.

No import I/O, clock, module discovery, provider, ledger or default approval.
Root must authenticate actual original decisions and the exact source/config
map independently before invoking this local data adapter. Hashes prove only
integrity; these functions cannot release execution authority.
"""
import copy
import hashlib

FLAGS={'runtimeReady':False,'executionAuthorityGranted':False,'sourceAuthenticityEstablished':False,
    'claimCreated':False,'providerActionPerformed':False}


def configuration_candidate(m,inputs,proposal_markdown):
    """Derive explicit candidate data only; no globals/authority are changed."""
    m.require(type(proposal_markdown) is bytes,'whole-actual-proposal-bytes-required')
    approval=inputs['approval'];context=inputs['futureApprovalContext']
    return {'approvalSHA256':m.digest(approval),'directiveSHA256':m.digest(inputs['futureDirective']),
        'contextSHA256':m.digest(context),'proposalRawSHA256':hashlib.sha256(proposal_markdown).hexdigest(),
        'proposalMessageSHA256':m.digest(context['proposalContext']),'namespace':approval['namespace'],
        'ledgerFolder':approval['ledgerFolder'],'cleanupFolder':approval['cleanupFolder']}


def configure_externally_reviewed(m,inputs,proposal_markdown,raw_configuration,*,reviewed_raw_sha256):
    """Explicit Root source-adoption step, not an authentication implementation.

    The supplied raw pin must come from Root's independently reviewed source
    map, not from a caller deriving a pin and treating it as approval. Actual
    decision/tool custody remains subject to the existing external Root gate.
    Failed validation restores the original unconfigured module state.
    """
    m.require(type(raw_configuration) is bytes and 2<=len(raw_configuration)<=16384
        and m.sha(reviewed_raw_sha256) and hashlib.sha256(raw_configuration).hexdigest()==reviewed_raw_sha256,
        'exact-explicit-Root-reviewed-configuration-bytes')
    supplied=m.strict_json(raw_configuration);expected=configuration_candidate(m,inputs,proposal_markdown)
    m.require(m.same(supplied,expected),'configuration-exact-original-decision-and-proposal')
    before=(copy.deepcopy(m.FUTURE_CONFIGURATION),copy.deepcopy(m.SOURCE_PINS),m.PROPOSAL_RAW_SHA)
    try:
        m.FUTURE_CONFIGURATION=copy.deepcopy(supplied);m.PROPOSAL_RAW_SHA=supplied['proposalRawSHA256']
        m.SOURCE_PINS.update(approval=supplied['approvalSHA256'],futureDirective=supplied['directiveSHA256'],
            futureApprovalContext=supplied['contextSHA256'])
        m.configuration_checked();m._future_inputs_checked(inputs)
    except BaseException:
        m.FUTURE_CONFIGURATION,m.SOURCE_PINS,m.PROPOSAL_RAW_SHA=before
        raise
    return {'schema':'hikyo-158/future-configured-source-data/v1',**FLAGS,
        'configurationSHA256':m.digest(supplied),'configurationRawSHA256':reviewed_raw_sha256,
        'approvalSHA256':supplied['approvalSHA256'],'bankedSeriesSHA256':m.digest(inputs['approval']['bankedSeries']),
        'firstAttemptSubset':copy.deepcopy(inputs['approval']['bankedSeries']['firstAttemptSubset']),'completedHistorySHA256':m.SOURCE_PINS['completedAttempt']}


def prepare(m,inputs,proposal_markdown,source_manifest,records,reviewed_pins,*,lease_start,lease_until,namespace,explicit_now):
    """Pure preparation only. Missing config or stale actual evidence refuses."""
    m.configuration_checked()
    m.require(namespace==m.FUTURE_CONFIGURATION['namespace'],'exact-actually-configured-future-namespace')
    proposal=m.unbound_proposal({**copy.deepcopy(inputs),'proposalMarkdown':proposal_markdown},source_manifest)
    return m.prepare(proposal,records,reviewed_pins,lease_start=lease_start,lease_until=lease_until,
        namespace=namespace,explicit_now=explicit_now)


if __name__=='__main__':
    raise SystemExit('SOURCE ONLY: no actual approval/configuration, Root authentication or execution driver is supplied')
