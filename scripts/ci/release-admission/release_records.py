"""Closed immutable release records and lifecycle over an external authority.

No convenience store can authenticate an original or assert a current head.
The default external boundary refuses. This module has no provider operations.
"""
from copy import deepcopy
from dataclasses import dataclass
from datetime import datetime
from pathlib import PurePosixPath
import re
import time
from typing import Protocol

from bounded import canonical, clock, digest, exact, fresh, instant, require, sha
from release_policy import (
    ACCEPTANCE_SERIES, CANDIDATE_CI_FILES, FROZEN, HORIZON, EVIDENCE_MODE,
    OWNER_ACTOR, POLICY_SHA256, RECORD_FIELDS, REPOSITORY, REQUIRED_CI_JOBS,
    ROOT_HOLD_REASONS, SCHEMAS, TRUSTED_FILES,
)


@dataclass(frozen=True)
class Original:
    raw: bytes
    projection: dict


@dataclass(frozen=True)
class Verification:
    purpose: str
    subject: dict
    originals: tuple[Original, ...]
    verified_at: str
    decision_sequence: int | None = None


@dataclass(frozen=True)
class NativeEvent:
    sequence: int
    raw: bytes
    decision: dict | None


@dataclass(frozen=True)
class Head:
    release_sha256: str
    adoption_sha256: str
    status_sha256: str
    revision: int
    watermark: int


@dataclass(frozen=True)
class Observation:
    scope: str
    observed_at: str
    terminal_sequence: int
    events: tuple[NativeEvent, ...]
    head: Head | None


class Authority(Protocol):
    def verify_record(self, kind: str, record: dict, records: dict,
                      at: datetime) -> Verification: ...

    def observe(self, release_sha256: str, at: datetime, *,
                historical: bool = False) -> Observation: ...

    def compare_and_swap(self, expected: Head | None, proposed: Head,
                         decision_sha256: str, observation: Observation) -> Head: ...


class UnavailableAuthority:
    def verify_record(self, kind, record, records, at):
        raise ValueError('release-admission:external-original-authority-unavailable')

    def observe(self, release_sha256, at, *, historical=False):
        raise ValueError('release-admission:current-status-authority-unavailable')

    def compare_and_swap(self, expected, proposed, decision_sha256, observation):
        raise ValueError('release-admission:persistent-CAS-authority-unavailable')


_RESOURCE_FIELDS = frozenset({
    'approvalSHA256', 'directiveSHA256', 'contextSHA256', 'proposalRawSHA256',
    'proposalMessageSHA256', 'namespace', 'ledgerFolder', 'cleanupFolder',
})
_KINDS = {schema: kind for kind, schema in SCHEMAS.items()}
_NULLABLE_DIGESTS = frozenset({
    'previousReleaseSHA256', 'supersedesRevocationSHA256', 'supersedesAdoptionSHA256',
    'supersedesOwnerApprovalSHA256', 'recoverySHA256', 'previousStatusSHA256',
})
_STATIC_JSON_PATHS = frozenset({
    'BASELINE-SOURCE-PINS.json', 'HISTORICAL-ARCHIVE-PINS.json',
    'spec/CONTRACTS.json', 'spec/CONFORMANCE-CASES.json', 'spec/DESIGN-PINS.json',
    'adapters/native-pins.json', 'adapters/ORIGINS.json',
    'adapters/prepare-registry.json', 'adapters/create-registry.json',
    'adapters/U-GUARD-REGISTRY.json', 'adapters/TRANSPORT-CODE-PINS.json',
    'adapters/SOURCE-PINS.json', 'adapters/STAGE-SOURCE-PINS.json',
    'adapters/DESCRIPTOR-CONTRACT.json', 'adapters/stage-registry.json',
    'adapters/cleanup-registry.json', 'adapters/TRANSPORT-ORIGINS.json',
})
_EVENT_KINDS = frozenset({
    'owner-approval', 'root-adoption', 'root-hold', 'owner-withdrawal', 'root-recovery',
})


def _integer(value, label):
    require(type(value) is int and value > 0, label)


def _text(value, label):
    require(type(value) is str and 0 < len(value) <= 512, label)


def _hash_map(value, paths):
    exact(value, paths)
    require(all(sha(item) for item in value.values()), 'file-vector-sha256')


def resource_configuration_checked(value):
    exact(value, _RESOURCE_FIELDS)
    require(all(sha(value[key]) for key in _RESOURCE_FIELDS if key.endswith('SHA256')),
            'resource-configuration-digests')
    require(type(value['namespace']) is str and re.fullmatch(
        r'hikyo-e2e-[0-9]{8}c[0-9]{2}/', value['namespace']) is not None
        and value['namespace'] not in ('hikyo-e2e-20261021c03/', 'hikyo-e2e-20261021c04/'),
        'resource-namespace')
    require(all(type(value[key]) is str and re.fullmatch(r'[a-z][a-z0-9-]{4,80}', value[key])
                for key in ('ledgerFolder', 'cleanupFolder'))
            and value['ledgerFolder'] not in ('aws-next-fixture-state', 'aws-after-held-fixture-state')
            and value['cleanupFolder'] not in ('aws-after-held-fixture-partial-cleanup', value['ledgerFolder']),
            'resource-folders')
    return deepcopy(value)


def implementation_checked(value):
    exact(value, {'schema', 'files'})
    require(value['schema'] == 'hikyo-158/release-implementation/v1', 'implementation-schema')
    files = value['files']
    require(type(files) is dict and 2 <= len(files) <= 2048, 'static-implementation-files')
    require({'core/next_fixture_model.source-only.py', 'core/test_next_fixture_model.py'} <= set(files),
            'implementation-model-and-test')
    for name, pin in files.items():
        require(type(name) is str and sha(pin), 'static-file-pin')
        path = PurePosixPath(name)
        require(re.fullmatch(r'[A-Za-z0-9_./-]+', name) is not None
                and not path.is_absolute() and str(path) == name and '..' not in path.parts
                and (path.suffix in {'.py', '.js', '.mjs', '.sh', '.go', '.md'}
                     or name in _STATIC_JSON_PATHS
                     or (path.suffix == '.cjs' and path.parts[0] in {'adapters', 'core'}))
                and not any(part in {'runtime', 'configuration', 'approvals', 'adoptions',
                                     'status', 'evidence', 'records', 'claims'} for part in path.parts),
                'static-implementation-no-runtime-data')
    canonical(value)
    return deepcopy(value)


def record_checked(record):
    """Check the entire fixed contract, including exact scalar/nested types."""
    require(type(record) is dict, 'record-object')
    canonical(record)
    require(type(record.get('schema')) is str, 'record-schema-string')
    kind = _KINDS.get(record['schema'])
    require(kind is not None, 'unknown-record-schema')
    exact(record, RECORD_FIELDS[kind])
    for key, value in record.items():
        if key.endswith('SHA256'):
            require((key in _NULLABLE_DIGESTS and value is None) or sha(value), 'record-digest-' + key)
        if key.endswith('At') or key == 'validUntil':
            instant(value)
    if 'policySHA256' in record:
        require(record['policySHA256'] == POLICY_SHA256, 'fixed-policy')
    if 'evidenceMode' in record:
        require(record['evidenceMode'] == EVIDENCE_MODE, 'explicit-post-merge-mode')
    if kind == 'releaseCandidate':
        require(record['repository'] == REPOSITORY, 'fixed-repository')
        _repository(record['repository'])
        _pr(record['releasePR'], source_range=True)
        commit = record['mergeCommit']
        exact(commit, {'sha', 'treeSHA', 'orderedParents'})
        require(sha(commit['sha'], 40) and sha(commit['treeSHA'], 40)
                and type(commit['orderedParents']) is list and len(commit['orderedParents']) == 2
                and all(sha(item, 40) for item in commit['orderedParents']), 'merge-commit')
        pr = record['releasePR']
        require(commit['sha'] == pr['mergeSHA']
                and commit['orderedParents'] == [pr['baseSHA'], pr['headSHA']], 'merge-identity-parents')
        _hash_map(record['trustedFileSHA256s'], TRUSTED_FILES)
        run = record['workflowRun']
        exact(run, {'id', 'attempt', 'path', 'event', 'name', 'headSHA', 'testedCommitSHA', 'jobsEvidenceSHA256'})
        _integer(run['id'], 'run-id')
        _integer(run['attempt'], 'run-attempt')
        require(run['path'] == '.github/workflows/ci-merge-group.yml'
                and run['event'] == 'merge_group' and run['name'] == 'trusted-merge-ci'
                and run['headSHA'] == pr['mergeSHA'] and run['testedCommitSHA'] == pr['mergeSHA']
                and sha(run['jobsEvidenceSHA256']), 'exact-release-queue-run')
        require(instant(pr['mergedAt']) <= instant(record['collectedAt']), 'merge-before-collection')
    elif kind == 'ownerReleaseApproval':
        require(record['purpose'] == 'approve-trusted-release'
                and record['acceptanceSeries'] == ACCEPTANCE_SERIES, 'owner-approval-scope')
        _actor(record['actor'])
        require(instant(record['decidedAt']) < instant(record['validUntil']) <= instant(HORIZON),
                'approval-explicit-unchanged-horizon')
        supersedes = [record[key] for key in ('supersedesRevocationSHA256', 'supersedesAdoptionSHA256',
                                              'supersedesOwnerApprovalSHA256')]
        require(all(item is None for item in supersedes) or all(sha(item) for item in supersedes),
                'complete-reauthorization-references')
    elif kind == 'rootReleaseAdoption':
        require(record['adoptionKind'] in ('initial', 'root-safety-recovery', 'owner-reauthorization'),
                'adoption-kind')
        if record['adoptionKind'] == 'initial':
            require(record['supersedesAdoptionSHA256'] is None and record['recoverySHA256'] is None,
                    'initial-adoption-no-predecessor')
        else:
            require(sha(record['supersedesAdoptionSHA256']) and sha(record['recoverySHA256']),
                    'successor-adoption-predecessor')
    elif kind == 'releaseStatus':
        _integer(record['sequence'], 'status-sequence')
        require(type(record['disposition']) is str and record['disposition'] in ('eligible', 'held', 'revoked'), 'status-disposition')
        reasons = {'eligible': {'release-adopted'}, 'held': ROOT_HOLD_REASONS,
                   'revoked': {'owner-withdrawal'}}
        require(type(record['reason']) is str and record['reason'] in reasons[record['disposition']], 'closed-status-reason')
        require((record['sequence'] == 1) == (record['previousStatusSHA256'] is None), 'status-genesis')
        require((record['sequence'] == 1) == (record['disposition'] == 'eligible'), 'eligible-genesis-only')
    elif kind == 'rootSafetyRecovery':
        require(type(record['resolvedReason']) is str and record['resolvedReason'] in ROOT_HOLD_REASONS, 'closed-recovery-reason')
    elif kind == 'mergeEvidence':
        require(record['mode'] == EVIDENCE_MODE, 'merge-evidence-mode')
        _repository(record['repository'])
        _pr(record['pullRequest'], state=True)
        event = record['mergedEvent']
        exact(event, {'restId', 'nodeId', 'actor', 'createdAt', 'commitSHA'})
        _integer(event['restId'], 'merged-event-id')
        _text(event['nodeId'], 'merged-event-node')
        _actor(event['actor'])
        instant(event['createdAt'])
        require(event['commitSHA'] == record['pullRequest']['mergeSHA']
                and event['createdAt'] == record['pullRequest']['mergedAt'], 'merge-event-correspondence')
    elif kind == 'releaseConfiguration':
        resource_configuration_checked(record['resourceConfiguration'])
        require(digest(record['resourceConfiguration']) == record['resourceConfigurationSHA256'],
                'resource-configuration-integrity')
    elif kind == 'sourceManifest':
        require(record['frozenNextHelpers'] == FROZEN, 'frozen-seven-helpers')
        _hash_map(record['frozenNextHelpers'], FROZEN)
        _hash_map(record['candidateFileSHA256s'], {'go.mod'})
        _hash_map(record['candidateCIFileSHA256s'], CANDIDATE_CI_FILES)
        _hash_map(record['workflowFileSHA256s'], TRUSTED_FILES)
        exact(record['requiredCIJobs'], {'candidate', 'workflow'})
        for lane, jobs in record['requiredCIJobs'].items():
            exact(jobs, REQUIRED_CI_JOBS[lane])
            require(jobs == REQUIRED_CI_JOBS[lane], 'fixed-complete-ci-jobs')
    elif kind == 'attemptTrustBinding':
        require(sha(record['candidateHeadSHA'], 40) and sha(record['mainSHA'], 40), 'pair-git-shas')
        exact(record['candidateRun'], {'id', 'attempt'})
        run = record['currentMainRun']
        exact(run, {'id', 'attempt', 'mergedPRNumber', 'headSHA', 'baseSHA', 'mergeSHA'})
        for item in (record['candidateRun'], run):
            _integer(item['id'], 'pair-run-id')
            _integer(item['attempt'], 'pair-run-attempt')
        _integer(run['mergedPRNumber'], 'main-merged-pr')
        require(all(sha(run[key], 40) for key in ('headSHA', 'baseSHA', 'mergeSHA'))
                and run['mergeSHA'] == record['mainSHA'], 'main-run-exact-sha')
    return deepcopy(record)


def _actor(value):
    exact(value, {'login', 'databaseId'})
    _integer(value['databaseId'], 'owner-database-id')
    require(value == OWNER_ACTOR, 'fixed-owner-actor')


def _repository(value):
    exact(value, {'owner', 'name', 'repositoryId', 'ownerId', 'branch'})
    _integer(value['repositoryId'], 'repository-id')
    _integer(value['ownerId'], 'repository-owner-id')
    require(value == REPOSITORY, 'fixed-repository')


def _pr(value, *, source_range=False, state=False):
    fields = {'number', 'nodeId', 'headSHA', 'baseSHA', 'mergeSHA', 'mergedAt'}
    if source_range:
        fields.add('sourceRangeSHA256')
    if state:
        fields.add('state')
    exact(value, fields)
    _integer(value['number'], 'pr-number')
    _text(value['nodeId'], 'pr-node-id')
    require(all(sha(value[key], 40) for key in ('headSHA', 'baseSHA', 'mergeSHA')), 'pr-git-shas')
    instant(value['mergedAt'])
    if source_range:
        require(sha(value['sourceRangeSHA256']), 'source-range-digest')
    if state:
        require(value['state'] == 'merged', 'public-pr-merged')


class ReleaseAdmission:
    def __init__(self, configuration, records, *, implementation, historical_history,
                 authority: Authority | None = None):
        self._configuration = record_checked(configuration)
        require(configuration['schema'] == SCHEMAS['releaseConfiguration'], 'configuration-schema')
        require(type(records) is dict and len(records) <= 2048, 'bounded-content-records')
        self._records = {}
        for pin, record in records.items():
            require(sha(pin) and digest(record) == pin, 'content-addressed-record-integrity')
            self._records[pin] = record_checked(record)
        self._implementation = implementation_checked(implementation)
        canonical(historical_history)
        self._history = deepcopy(historical_history)
        self._authority = authority if authority is not None else UnavailableAuthority()
        self._check_configuration()

    @property
    def configuration(self):
        return deepcopy(self._configuration)

    @property
    def resource_configuration(self):
        return deepcopy(self._configuration['resourceConfiguration'])

    @property
    def release_vector(self):
        return deepcopy(self._get(self._configuration['releaseSHA256'], 'releaseCandidate')['trustedFileSHA256s'])

    def _get(self, pin, kind):
        require(sha(pin) and pin in self._records, 'missing-record-' + kind)
        record = self._records[pin]
        require(record['schema'] == SCHEMAS[kind], 'record-role-' + kind)
        return record

    def _check_configuration(self):
        config = self._configuration
        require(config['implementationManifestSHA256'] == digest(self._implementation)
                and config['historicalHistorySHA256'] == digest(self._history), 'immutable-implementation-history')
        release = self._get(config['releaseSHA256'], 'releaseCandidate')
        approval = self._get(config['ownerApprovalSHA256'], 'ownerReleaseApproval')
        adoption = self._get(config['rootAdoptionSHA256'], 'rootReleaseAdoption')
        genesis = self._get(config['statusGenesisSHA256'], 'releaseStatus')
        for record in (release, approval, adoption):
            for key in ('policySHA256', 'historicalHistorySHA256', 'resourceConfigurationSHA256',
                        'releaseSHA256', 'ownerApprovalSHA256', 'implementationManifestSHA256'):
                if key in record:
                    require(record[key] == config[key], 'configuration-constituent-' + key)
        require(genesis['sequence'] == 1 and genesis['rootAdoptionSHA256'] == config['rootAdoptionSHA256']
                and genesis['releaseSHA256'] == config['releaseSHA256'], 'configured-genesis')
        require(instant(release['collectedAt']) < instant(approval['decidedAt'])
                <= instant(adoption['adoptedAt']) <= instant(genesis['recordedAt']), 'release-authority-chronology')
        evidence = self._get(release['mergeEvidenceSHA256'], 'mergeEvidence')
        public = evidence['pullRequest']
        pr = release['releasePR']
        require(all(public[key] == pr[key] for key in public if key != 'state')
                and evidence['repository'] == release['repository']
                and evidence['sourceRangeEvidenceSHA256'] == pr['sourceRangeSHA256']
                and evidence['queueRunEvidenceSHA256'] == release['workflowRun']['jobsEvidenceSHA256'],
                'whole-merge-record-correspondence')
        self._check_graph()
        self._status_chain(config['statusGenesisSHA256'])

    def _check_graph(self):
        """Resolve typed backward references; external evidence remains authenticated leaves."""
        edges = {
            'releaseCandidate': {'previousReleaseSHA256': 'releaseCandidate', 'mergeEvidenceSHA256': 'mergeEvidence'},
            'ownerReleaseApproval': {'releaseSHA256': 'releaseCandidate', 'supersedesRevocationSHA256': 'releaseStatus',
                                     'supersedesAdoptionSHA256': 'rootReleaseAdoption',
                                     'supersedesOwnerApprovalSHA256': 'ownerReleaseApproval'},
            'rootReleaseAdoption': {'releaseSHA256': 'releaseCandidate', 'ownerApprovalSHA256': 'ownerReleaseApproval',
                                    'supersedesAdoptionSHA256': 'rootReleaseAdoption'},
            'releaseStatus': {'rootAdoptionSHA256': 'rootReleaseAdoption', 'releaseSHA256': 'releaseCandidate',
                              'previousStatusSHA256': 'releaseStatus'},
            'rootSafetyRecovery': {'releaseSHA256': 'releaseCandidate', 'priorAdoptionSHA256': 'rootReleaseAdoption',
                                   'priorHeldStatusSHA256': 'releaseStatus'},
            'releaseConfiguration': {'releaseSHA256': 'releaseCandidate', 'ownerApprovalSHA256': 'ownerReleaseApproval',
                                     'rootAdoptionSHA256': 'rootReleaseAdoption', 'statusGenesisSHA256': 'releaseStatus'},
            'sourceManifest': {'configurationSHA256': 'releaseConfiguration',
                               'releaseSHA256': 'releaseCandidate', 'ownerApprovalSHA256': 'ownerReleaseApproval',
                               'rootAdoptionSHA256': 'rootReleaseAdoption'},
            'attemptTrustBinding': {'releaseSHA256': 'releaseCandidate', 'ownerApprovalSHA256': 'ownerReleaseApproval',
                                   'rootAdoptionSHA256': 'rootReleaseAdoption', 'statusGenesisSHA256': 'releaseStatus',
                                   'sourceManifestSHA256': 'sourceManifest', 'releaseConfigurationSHA256': 'releaseConfiguration'},
        }
        active, done = set(), set()

        def visit(pin, depth=0):
            require(depth <= 200, 'record-lineage-bound')
            require(pin not in active, 'record-digest-cycle')
            if pin in done:
                return
            active.add(pin)
            record = self._records[pin]
            kind = _KINDS[record['schema']]
            refs = dict(edges.get(kind, {}))
            if kind == 'rootReleaseAdoption' and record['adoptionKind'] != 'initial':
                refs['recoverySHA256'] = ('rootSafetyRecovery' if record['adoptionKind'] == 'root-safety-recovery'
                                          else 'ownerReleaseApproval')
            for key, target_kind in refs.items():
                target = record[key]
                if target is not None:
                    self._get(target, target_kind)
                    visit(target, depth + 1)
            active.remove(pin)
            done.add(pin)
        for pin in self._records:
            visit(pin)

    def configuration_checked(self):
        self._check_configuration()
        return self.configuration

    def source_manifest_checked(self, manifest):
        record_checked(manifest)
        require(manifest['schema'] == SCHEMAS['sourceManifest'], 'manifest-schema')
        config = self._configuration
        for key in ('policySHA256', 'resourceConfigurationSHA256', 'releaseSHA256', 'ownerApprovalSHA256',
                    'rootAdoptionSHA256', 'historicalHistorySHA256'):
            require(manifest[key] == config[key], 'manifest-constituent-' + key)
        require(manifest['configurationSHA256'] == digest(config), 'composite-before-manifest')
        files = self._implementation['files']
        require(manifest['modelSHA256'] == files['core/next_fixture_model.source-only.py']
                and manifest['testSHA256'] == files['core/test_next_fixture_model.py'], 'manifest-static-code-pins')
        require(manifest['workflowFileSHA256s'] == self.release_vector
                and manifest['candidateCIFileSHA256s'] == {key: self.release_vector[key] for key in CANDIDATE_CI_FILES},
                'manifest-approved-source-vector')
        return deepcopy(manifest)

    def attempt_binding_checked(self, binding, manifest):
        record_checked(binding)
        require(binding['schema'] == SCHEMAS['attemptTrustBinding'], 'binding-schema')
        self.source_manifest_checked(manifest)
        for key in ('policySHA256', 'implementationManifestSHA256', 'releaseSHA256', 'ownerApprovalSHA256',
                    'rootAdoptionSHA256', 'statusGenesisSHA256', 'resourceConfigurationSHA256', 'historicalHistorySHA256'):
            require(binding[key] == self._configuration[key], 'binding-constituent-' + key)
        require(binding['releaseConfigurationSHA256'] == digest(self._configuration)
                and binding['sourceManifestSHA256'] == digest(manifest), 'binding-digest-order')
        return deepcopy(binding)

    def verify_external(self, kind, record, at):
        clock(at)
        started = time.monotonic()
        result = self._authority.verify_record(kind, deepcopy(record), deepcopy(self._records), at)
        require(time.monotonic() - started <= 5, 'external-verifier-five-seconds')
        require(type(result) is Verification and result.purpose == kind
                and canonical(result.subject) == canonical(record), 'whole-verified-subject')
        fresh(result.verified_at, at)
        require(type(result.originals) is tuple and 0 < len(result.originals) <= 2048, 'whole-originals-required')
        total = 0
        for original in result.originals:
            require(type(original) is Original and type(original.raw) is bytes
                    and 0 < len(original.raw) <= 2_000_000 and type(original.projection) is dict,
                    'whole-original-shape')
            canonical(original.projection)
            total += len(original.raw)
        require(total <= 24_000_000, 'original-aggregate-bound')
        if result.decision_sequence is not None:
            _integer(result.decision_sequence, 'verified-decision-sequence')
            require(result.decision_sequence <= 50_000, 'decision-stream-bound')
        return deepcopy(result)

    def _observe(self, at, historical=False):
        clock(at)
        started = time.monotonic()
        observation = self._authority.observe(self._configuration['releaseSHA256'], at, historical=historical)
        require(type(observation) is Observation, 'authoritative-stream-observation')
        _text(observation.scope, 'immutable-root-stream-scope')
        fresh(observation.observed_at, at)
        require(type(observation.terminal_sequence) is int and 0 <= observation.terminal_sequence <= 50_000,
                'stream-50000-bound')
        require(type(observation.events) is tuple and len(observation.events) == observation.terminal_sequence,
                'complete-terminal-native-stream')
        decisions = []
        for sequence, event in enumerate(observation.events, 1):
            require(type(event) is NativeEvent and type(event.sequence) is int and event.sequence == sequence,
                    'native-stream-gap-replay')
            require(type(event.raw) is bytes and 0 < len(event.raw) <= 2_000_000, 'whole-native-event')
            if event.decision is not None:
                decision = event.decision
                exact(decision, {'kind', 'releaseSHA256', 'adoptionSHA256', 'subjectSHA256', 'custodySHA256', 'occurredAt'})
                require(type(decision['kind']) is str and decision['kind'] in _EVENT_KINDS and sha(decision['releaseSHA256'])
                        and (decision['adoptionSHA256'] is None or sha(decision['adoptionSHA256']))
                        and sha(decision['subjectSHA256']) and sha(decision['custodySHA256']), 'native-decision-projection')
                require(instant(decision['occurredAt']) <= at, 'future-stream-decision')
                decisions.append((sequence, decision))
            require(time.monotonic() - started <= 5, 'stream-five-second-verifier')
        head = observation.head
        if head is not None:
            require(type(head) is Head and head.release_sha256 == self._configuration['releaseSHA256']
                    and sha(head.adoption_sha256) and sha(head.status_sha256), 'per-release-current-head')
            _integer(head.revision, 'persistent-head-revision')
            require(type(head.watermark) is int and 0 <= head.watermark <= observation.terminal_sequence,
                    'persistent-monotonic-watermark')
            self._status_chain(head.status_sha256, head.adoption_sha256)
        return observation, decisions

    def _status_chain(self, pin, adoption_pin=None):
        chain, seen = [], set()
        while pin is not None:
            require(pin not in seen and len(chain) < 2048, 'bounded-status-chain')
            seen.add(pin)
            status = self._get(pin, 'releaseStatus')
            if adoption_pin is None:
                adoption_pin = status['rootAdoptionSHA256']
            require(status['rootAdoptionSHA256'] == adoption_pin
                    and status['releaseSHA256'] == self._configuration['releaseSHA256'], 'status-cross-adoption')
            chain.append((pin, status))
            pin = status['previousStatusSHA256']
        chain.reverse()
        adoption = self._get(adoption_pin, 'rootReleaseAdoption')
        require(adoption['releaseSHA256'] == self._configuration['releaseSHA256'], 'status-adoption-release')
        require(chain and chain[0][1]['sequence'] == 1
                and instant(adoption['adoptedAt']) <= instant(chain[0][1]['recordedAt']), 'status-adoption-genesis')
        for index, (_, status) in enumerate(chain, 1):
            require(status['sequence'] == index, 'contiguous-status-sequence')
            if index > 1:
                previous = chain[index - 2][1]
                allowed = {'eligible': {'held', 'revoked'}, 'held': {'revoked'}, 'revoked': set()}
                require(status['disposition'] in allowed[previous['disposition']]
                        and instant(previous['recordedAt']) <= instant(status['recordedAt']), 'sticky-status-transition')
        return chain

    def _event(self, decisions, kind, subject, custody, adoption=None):
        matches = [(sequence, decision) for sequence, decision in decisions
                   if decision['kind'] == kind and decision['subjectSHA256'] == subject
                   and decision['custodySHA256'] == custody
                   and decision['releaseSHA256'] == self._configuration['releaseSHA256']
                   and decision['adoptionSHA256'] == adoption]
        require(len(matches) == 1, 'unique-authenticated-decision-' + kind)
        return matches[0]

    def _authenticate_chain(self, chain, decisions, at):
        last_sequence = 0
        for pin, status in chain:
            kind = {'eligible': 'root-adoption', 'held': 'root-hold', 'revoked': 'owner-withdrawal'}[status['disposition']]
            subject = status['rootAdoptionSHA256'] if kind == 'root-adoption' else pin
            sequence, decision = self._event(decisions, kind, subject, status['decisionCustodySHA256'],
                                              status['rootAdoptionSHA256'])
            require(sequence > last_sequence and instant(decision['occurredAt']) <= instant(status['recordedAt']) <= at,
                    'status-original-order-time')
            receipt = self.verify_external('releaseStatus', status, at)
            require(receipt.decision_sequence == sequence, 'status-native-sequence-correspondence')
            last_sequence = sequence
        return last_sequence

    def _base_external(self, at):
        config = self._configuration
        for kind, record in (
            ('implementation', self._implementation), ('historicalHistory', self._history),
            ('resourceConfiguration', config['resourceConfiguration']),
            ('releaseCandidate', self._get(config['releaseSHA256'], 'releaseCandidate')),
            ('mergeEvidence', self._get(self._get(config['releaseSHA256'], 'releaseCandidate')['mergeEvidenceSHA256'], 'mergeEvidence')),
            ('ownerReleaseApproval', self._get(config['ownerApprovalSHA256'], 'ownerReleaseApproval')),
            ('rootReleaseAdoption', self._get(config['rootAdoptionSHA256'], 'rootReleaseAdoption')),
        ):
            self.verify_external(kind, record, at)

    def _eligible(self, observation, decisions, at):
        head = observation.head
        require(head is not None and head.adoption_sha256 == self._configuration['rootAdoptionSHA256'],
                'current-configured-adoption')
        chain = self._status_chain(head.status_sha256, head.adoption_sha256)
        require(chain[0][0] == self._configuration['statusGenesisSHA256'], 'exact-configured-status-genesis')
        applied = self._authenticate_chain(chain, decisions, at)
        require(applied <= head.watermark, 'status-head-before-persistent-watermark')
        require(chain[-1][1]['disposition'] == 'eligible', 'current-release-ineligible')
        approval = self._get(self._configuration['ownerApprovalSHA256'], 'ownerReleaseApproval')
        require(instant(approval['decidedAt']) <= at < instant(approval['validUntil']), 'standing-owner-approval-unexpired')
        owner_sequence, owner_event = self._event(decisions, 'owner-approval', self._configuration['ownerApprovalSHA256'],
                                                  approval['originalDecisionCustodySHA256'])
        require(instant(owner_event['occurredAt']) == instant(approval['decidedAt']), 'actual-owner-original-time')
        self._owner_grant_current(decisions, owner_sequence)
        adoption_sequence, adoption_event = self._event(decisions, 'root-adoption', head.adoption_sha256,
                                                        chain[0][1]['decisionCustodySHA256'], head.adoption_sha256)
        adoption = self._get(head.adoption_sha256, 'rootReleaseAdoption')
        require(owner_sequence < adoption_sequence and instant(adoption_event['occurredAt']) == instant(adoption['adoptedAt']),
                'actual-owner-adoption-order')
        for sequence, decision in decisions:
            if decision['releaseSHA256'] == head.release_sha256:
                require(not (decision['kind'] == 'owner-withdrawal' and sequence > owner_sequence),
                        'withdrawal-after-standing-approval')
                require(not (sequence > adoption_sequence and decision['kind'] in ('owner-approval', 'root-adoption', 'root-recovery')),
                        'newer-incompatible-release-decision')
        covered = {pin for pin, _ in chain} | {head.adoption_sha256, self._configuration['ownerApprovalSHA256']}
        for _, decision in decisions:
            if decision['releaseSHA256'] == head.release_sha256:
                require(decision['kind'] not in ('root-hold', 'owner-withdrawal')
                        or decision['subjectSHA256'] in covered
                        or decision['adoptionSHA256'] != head.adoption_sha256, 'unapplied-hold-or-withdrawal')
                if decision['kind'] == 'root-adoption' and decision['adoptionSHA256'] == head.adoption_sha256:
                    require(decision['subjectSHA256'] == head.adoption_sha256, 'conflicting-adoption-subject')
        return head

    def _owner_grant_current(self, decisions, owner_sequence):
        # A later adoption cannot reset the boundary of the selected grant.
        # Explicit reauthorization selects its genuinely later approval instead.
        require(not any(sequence > owner_sequence and decision['kind'] == 'owner-approval'
                        and decision['releaseSHA256'] == self._configuration['releaseSHA256']
                        for sequence, decision in decisions), 'newer-incompatible-release-decision')

    def adopt_now(self, now):
        clock(now)
        self._base_external(now)
        config = self._configuration
        adoption_pin = config['rootAdoptionSHA256']
        adoption = self._get(adoption_pin, 'rootReleaseAdoption')
        approval = self._get(config['ownerApprovalSHA256'], 'ownerReleaseApproval')
        genesis = self._get(config['statusGenesisSHA256'], 'releaseStatus')
        require(instant(adoption['adoptedAt']) <= now < instant(approval['validUntil']), 'adoption-time-unexpired')
        observation, decisions = self._observe(now)
        adoption_sequence, adoption_event = self._event(decisions, 'root-adoption', adoption_pin,
                                                        genesis['decisionCustodySHA256'], adoption_pin)
        require(instant(adoption_event['occurredAt']) == instant(adoption['adoptedAt']), 'actual-adoption-time')
        owner_sequence, owner_event = self._event(decisions, 'owner-approval', config['ownerApprovalSHA256'],
                                                  approval['originalDecisionCustodySHA256'])
        require(owner_sequence < adoption_sequence and instant(owner_event['occurredAt']) == instant(approval['decidedAt']),
                'owner-before-adoption-original-chronology')
        self._owner_grant_current(decisions, owner_sequence)
        self._authenticate_chain([(config['statusGenesisSHA256'], genesis)], decisions, now)
        head = observation.head
        decision_pin = genesis['decisionCustodySHA256']
        if adoption['adoptionKind'] == 'initial':
            require(head is None, 'no-duplicate-initial-adoption')
            require(approval['supersedesRevocationSHA256'] is None, 'initial-not-reauthorization')
            require(not any(decision['releaseSHA256'] == config['releaseSHA256']
                            and decision['kind'] in ('root-hold', 'owner-withdrawal')
                            for _, decision in decisions), 'initial-cannot-hide-prior-hold')
        else:
            require(head is not None and adoption['supersedesAdoptionSHA256'] == head.adoption_sha256,
                    'exact-current-superseded-adoption')
            old = self._get(head.adoption_sha256, 'rootReleaseAdoption')
            chain = self._status_chain(head.status_sha256, head.adoption_sha256)
            prior_sequence = self._authenticate_chain(chain, decisions, now)
            require(prior_sequence <= head.watermark and prior_sequence < adoption_sequence,
                    'superseded-authoritative-order')
            for key in ('policySHA256', 'implementationManifestSHA256', 'releaseSHA256', 'resourceConfigurationSHA256'):
                require(old[key] == adoption[key], 'unchanged-recovery-subject-' + key)
            if adoption['adoptionKind'] == 'root-safety-recovery':
                require(chain[-1][1]['disposition'] == 'held', 'root-recovery-only-held-no-withdrawal')
                require(old['ownerApprovalSHA256'] == config['ownerApprovalSHA256'], 'standing-grant-unchanged')
                recovery = self._get(adoption['recoverySHA256'], 'rootSafetyRecovery')
                require(recovery['releaseSHA256'] == config['releaseSHA256']
                        and recovery['priorAdoptionSHA256'] == head.adoption_sha256
                        and recovery['priorHeldStatusSHA256'] == head.status_sha256
                        and recovery['resolvedReason'] == chain[-1][1]['reason'], 'exact-root-hold-remediation')
                require(instant(chain[-1][1]['recordedAt']) < instant(recovery['decidedAt'])
                        <= instant(adoption['adoptedAt']), 'recovery-actual-chronology')
                recovery_sequence, recovery_event = self._event(
                    decisions, 'root-recovery', adoption['recoverySHA256'], recovery['rootDecisionCustodySHA256'],
                    head.adoption_sha256)
                require(prior_sequence < recovery_sequence < adoption_sequence
                        and instant(recovery_event['occurredAt']) == instant(recovery['decidedAt']), 'new-recovery-decision-order')
                require(not any(decision['releaseSHA256'] == config['releaseSHA256']
                                and decision['kind'] == 'owner-withdrawal'
                                and sequence > owner_sequence for sequence, decision in decisions),
                        'root-recovery-never-overrides-withdrawal')
                self.verify_external('rootSafetyRecovery', recovery, now)
                decision_pin = recovery['rootDecisionCustodySHA256']
            else:
                require(chain[-1][1]['disposition'] == 'revoked', 'reauthorization-actual-withdrawal')
                require(approval['supersedesRevocationSHA256'] == head.status_sha256
                        and approval['supersedesAdoptionSHA256'] == head.adoption_sha256
                        and approval['supersedesOwnerApprovalSHA256'] == old['ownerApprovalSHA256']
                        and adoption['recoverySHA256'] == config['ownerApprovalSHA256'], 'explicit-actual-withdrawal-reauthorization')
                require(prior_sequence < owner_sequence < adoption_sequence
                        and instant(chain[-1][1]['recordedAt']) < instant(approval['decidedAt']), 'genuinely-later-owner-reauthorization')
                # The independent adoption review must include remediation of every prior hold.
                self.verify_external('reauthorizationRemediation', {'adoption': adoption, 'priorStatuses': [row for _, row in chain]}, now)
                decision_pin = approval['originalDecisionCustodySHA256']
        # No later incompatible native decision may be hidden by the proposed genesis.
        for sequence, decision in decisions:
            if decision['releaseSHA256'] == config['releaseSHA256']:
                require(not (sequence > adoption_sequence and decision['kind'] in (
                    'owner-approval', 'root-adoption', 'root-hold', 'owner-withdrawal', 'root-recovery')),
                    'incompatible-decision-after-adoption')
                require(not (decision['kind'] in ('root-hold', 'owner-withdrawal')
                             and decision['adoptionSHA256'] == adoption_pin), 'new-adoption-already-held')
        proposed = Head(config['releaseSHA256'], adoption_pin, config['statusGenesisSHA256'],
                        1 if head is None else head.revision + 1, observation.terminal_sequence)
        return self._cas(head, proposed, decision_pin, observation)

    def _cas(self, expected, proposed, decision, observation):
        result = self._authority.compare_and_swap(expected, proposed, decision, observation)
        require(type(result) is Head and result == proposed, 'durable-atomic-cas-result')
        return result

    def transition_now(self, status_sha256, now):
        clock(now)
        status = self._get(status_sha256, 'releaseStatus')
        observation, decisions = self._observe(now)
        head = observation.head
        require(head is not None and head.adoption_sha256 == self._configuration['rootAdoptionSHA256']
                and status['rootAdoptionSHA256'] == head.adoption_sha256
                and status['previousStatusSHA256'] == head.status_sha256, 'transition-current-exact-predecessor')
        chain = self._status_chain(status_sha256, head.adoption_sha256)
        self._authenticate_chain(chain, decisions, now)
        require(len(chain) >= 2 and status['sequence'] == chain[-2][1]['sequence'] + 1, 'append-only-transition')
        kind = 'root-hold' if status['disposition'] == 'held' else 'owner-withdrawal'
        sequence, _ = self._event(decisions, kind, status_sha256, status['decisionCustodySHA256'], head.adoption_sha256)
        prior_sequence = self._authenticate_chain(chain[:-1], decisions, now)
        require(sequence > prior_sequence and prior_sequence <= head.watermark, 'new-unconsumed-transition-decision')
        require(not any(decision['releaseSHA256'] == head.release_sha256
                        and other_sequence > sequence and decision['kind'] == 'root-adoption'
                        for other_sequence, decision in decisions), 'later-transition-must-not-be-hidden')
        proposed = Head(head.release_sha256, head.adoption_sha256, status_sha256,
                        head.revision + 1, observation.terminal_sequence)
        return self._cas(head, proposed, status['decisionCustodySHA256'], observation)

    def admit_now(self, binding, manifest, now, *, reserved_binding=None):
        self.attempt_binding_checked(binding, manifest)
        if reserved_binding is not None:
            require(canonical(reserved_binding) == canonical(binding), 'reserved-attempt-never-rebinds')
        self._base_external(now)
        self.verify_external('attemptTrustBinding', binding, now)
        observation, decisions = self._observe(now)
        head = self._eligible(observation, decisions, now)
        return self._summary(binding, 'admission-now', head)

    def reconstruct_original_for_cleanup(self, binding, manifest, action_at, now):
        clock(action_at)
        clock(now)
        require(action_at <= now, 'cleanup-real-original-action-time')
        self.attempt_binding_checked(binding, manifest)
        self._base_external(action_at)
        self.verify_external('attemptTrustBinding', binding, action_at)
        observation, decisions = self._observe(action_at, historical=True)
        head = self._eligible(observation, decisions, action_at)
        return self._summary(binding, 'cleanup-only', head)

    @staticmethod
    def _summary(binding, mode, head):
        return {'mode': mode, 'attemptTrustBindingSHA256': digest(binding),
                'statusSHA256': head.status_sha256, 'watermark': head.watermark,
                'runtimeReady': False, 'externalAuthenticityVerifiedByModel': False,
                'executionAuthorityGranted': False}
