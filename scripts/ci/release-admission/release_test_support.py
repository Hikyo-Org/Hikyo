"""Synthetic fixtures. Only the genuine-original/persistent-authority boundary is doubled.

Nothing here is suitable for production custody or a configured live bank.
"""
from copy import deepcopy
from dataclasses import replace
import hashlib

from bounded import canonical, digest, instant, require, stamp
from release_policy import (
    ACCEPTANCE_SERIES, CANDIDATE_CI_FILES, EVIDENCE_MODE, FROZEN, OWNER_ACTOR,
    POLICY_SHA256, REPOSITORY, REQUIRED_CI_JOBS, SCHEMAS, TRUSTED_FILES,
)
from release_records import (
    NativeEvent, Observation, Original, ReleaseAdmission, Verification,
)


def pin(text):
    return hashlib.sha256(text.encode()).hexdigest()


def git(text):
    return hashlib.sha1(text.encode()).hexdigest()


T0 = '2026-10-23T10:00:00Z'
T1 = '2026-10-23T10:01:00Z'
T2 = '2026-10-23T10:02:00Z'
T3 = '2026-10-23T10:03:00Z'
T4 = '2026-10-23T10:04:00Z'
T5 = '2026-10-23T10:05:00Z'
T6 = '2026-10-23T10:06:00Z'
T7 = '2026-10-23T10:07:00Z'
T8 = '2026-10-23T10:08:00Z'
T9 = '2026-10-23T10:09:00Z'


class SyntheticAuthority:
    """External boundary double with durable-state semantics for a single fixture.

    Approvals must be registered as whole records explicitly. Native bodies and
    their projections are cross-checked. CAS is atomic under a lock and consumes
    decision IDs across generations. Tests can retain/share this backend across
    fresh ReleaseAdmission instances to model restart and competing callers.
    """
    def __init__(self):
        import threading
        self.lock = threading.Lock()
        self.events = []
        self.heads = {}
        self.history = []
        self.used = set()
        self.verified = set()
        self.calls = []
        self.scope = 'synthetic-configured-root-stream'
        self.before_cas = None
        self.observation_transform = None
        self.receipt_transform = None

    def register(self, kind, record):
        self.verified.add((kind, canonical(record)))

    def add(self, kind, release, adoption, subject, custody, at):
        decision = {'kind': kind, 'releaseSHA256': release, 'adoptionSHA256': adoption,
                    'subjectSHA256': subject, 'custodySHA256': custody, 'occurredAt': at}
        self.events.append(NativeEvent(len(self.events) + 1, canonical({'nativeDecision': decision}), decision))
        return len(self.events)

    def add_unrelated(self):
        self.events.append(NativeEvent(len(self.events) + 1, canonical({'unrelated': len(self.events)}), None))

    def verify_record(self, kind, record, records, at):
        require((kind, canonical(record)) in self.verified, 'synthetic-unregistered-whole-original')
        self.calls.append((kind, digest(record), stamp(at)))
        seq = None
        if kind == 'releaseStatus':
            subject = record['rootAdoptionSHA256'] if record['sequence'] == 1 else digest(record)
            matching = [event.sequence for event in self.events if event.decision is not None
                        and event.decision['subjectSHA256'] == subject
                        and event.decision['custodySHA256'] == record['decisionCustodySHA256']]
            require(len(matching) == 1, 'synthetic-status-native-unique')
            seq = matching[0]
        receipt = Verification(kind, deepcopy(record), (Original(canonical({'wholeOriginal': record}), deepcopy(record)),), stamp(at), seq)
        return self.receipt_transform(receipt) if self.receipt_transform else receipt

    def observe(self, release_sha256, at, *, historical=False):
        events = []
        for event in self.events:
            decision = event.decision
            if historical and decision is not None and instant(decision['occurredAt']) > at:
                break
            require(event.raw == canonical({'nativeDecision': decision}) if decision is not None
                    else event.raw == canonical({'unrelated': event.sequence - 1}), 'synthetic-native-correspondence')
            events.append(deepcopy(event))
        if historical:
            candidates = [head for when, head in self.history
                          if when <= at and head.release_sha256 == release_sha256]
            head = candidates[-1] if candidates else None
        else:
            head = self.heads.get(release_sha256)
        observation = Observation(self.scope, stamp(at), len(events), tuple(events), head)
        return self.observation_transform(observation) if self.observation_transform else observation

    def compare_and_swap(self, expected, proposed, decision_sha256, observation):
        if self.before_cas:
            self.before_cas(self)
        with self.lock:
            require(observation.scope == self.scope and observation.terminal_sequence == len(self.events), 'synthetic-stream-moved')
            require(self.heads.get(proposed.release_sha256) == expected, 'synthetic-CAS-conflict')
            require(decision_sha256 not in self.used, 'synthetic-decision-replay')
            require(expected is None or proposed.watermark >= expected.watermark, 'synthetic-watermark-regression')
            self.used.add(decision_sha256)
            self.heads[proposed.release_sha256] = proposed
            when = max((instant(event.decision['occurredAt']) for event in self.events if event.decision), default=instant(T0))
            self.history.append((when, proposed))
            return proposed


class ReleaseFixture:
    def __init__(self, label='one', authority=None):
        self.authority = authority if authority is not None else SyntheticAuthority()
        self.records = {}
        self.implementation = {'schema': 'hikyo-158/release-implementation/v1', 'files': {
            'core/next_fixture_model.source-only.py': pin('same-static-model'),
            'core/test_next_fixture_model.py': pin('same-static-test'),
        }}
        self.history = {'schema': 'synthetic-whole-literal-history', 'original1017': 'unchanged', 'c03': 144, 'c04': 176}
        self.resource = {key: pin('same-bank-' + key) for key in ('approvalSHA256', 'directiveSHA256', 'contextSHA256', 'proposalRawSHA256', 'proposalMessageSHA256')}
        self.resource.update(namespace='hikyo-e2e-20261023c05/', ledgerFolder='synthetic-future-ledger', cleanupFolder='synthetic-future-cleanup')
        self.vector = {path: pin(label + path) for path in TRUSTED_FILES}
        pr = {'number': 1046 if label == 'one' else 1047, 'nodeId': 'PR_' + label,
              'headSHA': git(label + 'head'), 'baseSHA': git(label + 'base'),
              'mergeSHA': git(label + 'merge'), 'mergedAt': T0}
        self.merge = {'schema': SCHEMAS['mergeEvidence'], 'mode': EVIDENCE_MODE, 'repository': dict(REPOSITORY),
                      'pullRequest': dict(pr, state='merged'), 'mergedEvent': {'restId': 100,
                      'nodeId': 'ME_' + label, 'actor': dict(OWNER_ACTOR), 'createdAt': T0, 'commitSHA': pr['mergeSHA']}}
        for key in ('publicCommitSHA256', 'queueRunEvidenceSHA256', 'sourceRangeEvidenceSHA256',
                    'timelineEvidenceSHA256', 'captureIndexSHA256', 'finalReadbackSHA256'):
            self.merge[key] = pin(label + key)
        self.release = {'schema': SCHEMAS['releaseCandidate'], 'policySHA256': POLICY_SHA256,
                        'historicalHistorySHA256': digest(self.history), 'previousReleaseSHA256': None,
                        'repository': dict(REPOSITORY), 'releasePR': dict(pr, sourceRangeSHA256=self.merge['sourceRangeEvidenceSHA256']),
                        'mergeCommit': {'sha': pr['mergeSHA'], 'treeSHA': git(label + 'tree'), 'orderedParents': [pr['baseSHA'], pr['headSHA']]},
                        'trustedFileSHA256s': self.vector,
                        'workflowRun': {'id': 100, 'attempt': 1, 'path': '.github/workflows/ci-merge-group.yml',
                                        'event': 'merge_group', 'name': 'trusted-merge-ci', 'headSHA': pr['mergeSHA'],
                                        'testedCommitSHA': pr['mergeSHA'], 'jobsEvidenceSHA256': self.merge['queueRunEvidenceSHA256']},
                        'changeReviewSHA256': pin(label + 'review'), 'evidenceIndexSHA256': pin(label + 'index'),
                        'collectedAt': T1, 'evidenceMode': EVIDENCE_MODE, 'mergeEvidenceSHA256': self.put(self.merge)}
        self.release_pin = self.put(self.release)
        self.approval = {'schema': SCHEMAS['ownerReleaseApproval'], 'purpose': 'approve-trusted-release',
                         'policySHA256': POLICY_SHA256, 'releaseSHA256': self.release_pin,
                         'resourceConfigurationSHA256': digest(self.resource), 'acceptanceSeries': ACCEPTANCE_SERIES,
                         'actor': dict(OWNER_ACTOR), 'originalDecisionCustodySHA256': pin(label + 'owner-original'),
                         'decidedAt': T2, 'validUntil': '2026-10-24T10:00:00Z', 'evidenceMode': EVIDENCE_MODE,
                         'supersedesRevocationSHA256': None, 'supersedesAdoptionSHA256': None, 'supersedesOwnerApprovalSHA256': None}
        self.authority.add('owner-approval', self.release_pin, None, self.put(self.approval), self.approval['originalDecisionCustodySHA256'], T2)
        self.adoption = self.new_adoption('initial', None, None, T3)
        self.genesis = self.new_genesis(self.adoption, T3)
        self.configure(self.adoption, self.genesis)
        for kind, record in [('implementation', self.implementation), ('historicalHistory', self.history), ('resourceConfiguration', self.resource)]:
            self.authority.register(kind, record)

    def put(self, record):
        pin_value = digest(record)
        self.records[pin_value] = deepcopy(record)
        from release_records import _KINDS
        if record.get('schema') in _KINDS:
            self.authority.register(_KINDS[record['schema']], record)
        return pin_value

    def new_adoption(self, kind, prior, recovery, at):
        adoption = {'schema': SCHEMAS['rootReleaseAdoption'], 'policySHA256': POLICY_SHA256,
                    'implementationManifestSHA256': digest(self.implementation), 'releaseSHA256': self.release_pin,
                    'ownerApprovalSHA256': self.put(self.approval), 'reviewSHA256': pin('independent-review-' + at),
                    'originalCustodyIndexSHA256': pin('root-custody-' + self.release_pin + at), 'resourceConfigurationSHA256': digest(self.resource),
                    'adoptedAt': at, 'adoptionKind': kind, 'supersedesAdoptionSHA256': prior, 'recoverySHA256': recovery}
        self.put(adoption)
        return adoption

    def new_genesis(self, adoption, at):
        adoption_pin = self.put(adoption)
        genesis = {'schema': SCHEMAS['releaseStatus'], 'rootAdoptionSHA256': adoption_pin, 'releaseSHA256': self.release_pin,
                   'sequence': 1, 'previousStatusSHA256': None, 'disposition': 'eligible', 'reason': 'release-adopted',
                   'decisionCustodySHA256': pin('genesis-original-' + adoption_pin + at), 'recordedAt': at}
        self.put(genesis)
        self.authority.add('root-adoption', self.release_pin, adoption_pin, adoption_pin, genesis['decisionCustodySHA256'], at)
        return genesis

    def configure(self, adoption, genesis):
        self.adoption, self.genesis = adoption, genesis
        self.config = {'schema': SCHEMAS['releaseConfiguration'], 'resourceConfiguration': self.resource,
                       'resourceConfigurationSHA256': digest(self.resource), 'policySHA256': POLICY_SHA256,
                       'implementationManifestSHA256': digest(self.implementation), 'releaseSHA256': self.release_pin,
                       'ownerApprovalSHA256': self.put(self.approval), 'rootAdoptionSHA256': self.put(adoption),
                       'statusGenesisSHA256': self.put(genesis), 'historicalHistorySHA256': digest(self.history)}
        self.manifest = {'schema': SCHEMAS['sourceManifest'], 'modelSHA256': self.implementation['files']['core/next_fixture_model.source-only.py'],
                         'testSHA256': self.implementation['files']['core/test_next_fixture_model.py'], 'frozenNextHelpers': dict(FROZEN),
                         'candidateFileSHA256s': {'go.mod': pin('candidate-go.mod')}, 'candidateCIFileSHA256s': {key: self.vector[key] for key in CANDIDATE_CI_FILES},
                         'workflowFileSHA256s': self.vector, 'requiredCIJobs': {key: dict(value) for key, value in REQUIRED_CI_JOBS.items()},
                         'configurationSHA256': digest(self.config), **{key: self.config[key] for key in (
                             'resourceConfigurationSHA256', 'policySHA256', 'releaseSHA256', 'ownerApprovalSHA256', 'rootAdoptionSHA256', 'historicalHistorySHA256')}}
        self.binding = {'schema': SCHEMAS['attemptTrustBinding'], **{key: self.config[key] for key in (
            'policySHA256', 'implementationManifestSHA256', 'releaseSHA256', 'ownerApprovalSHA256', 'rootAdoptionSHA256', 'statusGenesisSHA256', 'resourceConfigurationSHA256', 'historicalHistorySHA256')},
            'candidateHeadSHA': git('candidate-pair'), 'mainSHA': self.release['releasePR']['mergeSHA'], 'candidateRun': {'id': 200, 'attempt': 1},
            'sourceManifestSHA256': digest(self.manifest), 'currentMainRun': {'id': 100, 'attempt': 1, 'mergedPRNumber': self.release['releasePR']['number'],
                **{key: self.release['releasePR'][key] for key in ('headSHA', 'baseSHA', 'mergeSHA')}}, 'releaseConfigurationSHA256': digest(self.config)}
        self.authority.register('attemptTrustBinding', self.binding)

    def admission(self, **overrides):
        values = dict(configuration=self.config, records=self.records, implementation=self.implementation,
                      historical_history=self.history, authority=self.authority)
        values.update(overrides)
        return ReleaseAdmission(**values)

    def status(self, disposition, at, previous=None):
        if previous is None:
            head = self.authority.heads[self.release_pin]
            previous = self.records[head.status_sha256]
        status = {'schema': SCHEMAS['releaseStatus'], 'rootAdoptionSHA256': digest(self.adoption), 'releaseSHA256': self.release_pin,
                  'sequence': previous['sequence'] + 1, 'previousStatusSHA256': digest(previous), 'disposition': disposition,
                  'reason': 'confirmed-source-compromise' if disposition == 'held' else 'owner-withdrawal',
                  'decisionCustodySHA256': pin(disposition + '-original-' + at), 'recordedAt': at}
        status_pin = self.put(status)
        self.authority.add('root-hold' if disposition == 'held' else 'owner-withdrawal', self.release_pin, digest(self.adoption), status_pin, status['decisionCustodySHA256'], at)
        return status

    def recovery(self, hold, at=T5, adoption_at=T6):
        old_pin = digest(self.adoption)
        recovery = {'schema': SCHEMAS['rootSafetyRecovery'], 'releaseSHA256': self.release_pin,
                    'priorAdoptionSHA256': old_pin, 'priorHeldStatusSHA256': digest(hold), 'resolvedReason': hold['reason'],
                    'remediationEvidenceSHA256': pin('remediation-' + at), 'independentReviewSHA256': pin('remediation-review-' + at),
                    'rootDecisionCustodySHA256': pin('root-recovery-original-' + old_pin + at), 'ownerStreamObservationSHA256': pin('complete-stream-' + at), 'decidedAt': at}
        recovery_pin = self.put(recovery)
        self.authority.add('root-recovery', self.release_pin, old_pin, recovery_pin, recovery['rootDecisionCustodySHA256'], at)
        adoption = self.new_adoption('root-safety-recovery', old_pin, recovery_pin, adoption_at)
        self.configure(adoption, self.new_genesis(adoption, adoption_at))
        return recovery

    def reauthorize(self, withdrawal, at=T7, adoption_at=T8, approval_changes=None):
        old = self.adoption
        self.approval = dict(self.approval, originalDecisionCustodySHA256=pin('later-owner-' + at), decidedAt=at,
                             supersedesRevocationSHA256=digest(withdrawal), supersedesAdoptionSHA256=digest(old),
                             supersedesOwnerApprovalSHA256=old['ownerApprovalSHA256'])
        if approval_changes is not None:
            self.approval.update(approval_changes)
        approval_pin = self.put(self.approval)
        self.authority.add('owner-approval', self.release_pin, None, approval_pin, self.approval['originalDecisionCustodySHA256'], at)
        adoption = self.new_adoption('owner-reauthorization', digest(old), approval_pin, adoption_at)
        self.configure(adoption, self.new_genesis(adoption, adoption_at))
        chain = self.admission()._status_chain(digest(withdrawal), digest(old))
        self.authority.register('reauthorizationRemediation', {'adoption': adoption, 'priorStatuses': [row for _, row in chain]})
