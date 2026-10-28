"""Offline configured pipeline fixtures using the real sealed successor modules.

Only native originality, transport, clocks and temporary storage are synthetic.
The explicit pinned archive supplies unchanged historical originals and builders.
"""
from copy import deepcopy
from dataclasses import replace
from datetime import timedelta
import hashlib
import os
import tempfile
import types
import json
import sys
from pathlib import Path
from unittest.mock import patch

from load_successor import bootstrap_helpers, load_unconfigured

HERE = Path(__file__).resolve().parent


def raw_digest(raw):
    return hashlib.sha256(raw).hexdigest()


# Bootstrap before importing public helpers or fixtures that retain their
# classes/functions. Locally computed fixture pins prove source integrity only;
# operational Root review/adoption remains an external boundary.
bootstrap_helpers(
    reviewed_map_sha256=raw_digest((HERE / 'EXECUTION-MAP.json').read_bytes()),
    reviewed_manifest_raw_sha256=raw_digest((HERE / 'IMPLEMENTATION-MANIFEST.json').read_bytes()))

from bounded import canonical, digest, instant, require, stamp
import collector_test_support as providers
from historical_archive import PinnedArchive
from decision_journal import DecisionJournal
import release_collector as collector
from release_policy import (ACCEPTANCE_SERIES, CANDIDATE_CI_FILES, EVIDENCE_MODE,
                            FROZEN, OWNER_ACTOR, POLICY_SHA256, REPOSITORY,
                            REQUIRED_CI_JOBS, SCHEMAS)
from release_records import Observation, Original, ReleaseAdmission
from release_test_support import SyntheticAuthority


def archive_from_environment():
    root = os.environ.get('HIKYO_158_ARCHIVE_ROOT')
    require(bool(root), 'qualification-unavailable:HIKYO_158_ARCHIVE_ROOT-required')
    raw = (HERE / 'HISTORICAL-ARCHIVE-PINS.json').read_bytes()
    return PinnedArchive(Path(root), raw, reviewed_index_sha256=raw_digest(raw))


def sealed_modules():
    return load_unconfigured(
        reviewed_map_sha256=raw_digest((HERE / 'EXECUTION-MAP.json').read_bytes()),
        reviewed_manifest_raw_sha256=raw_digest((HERE / 'IMPLEMENTATION-MANIFEST.json').read_bytes()))


class PipelineAuthority(SyntheticAuthority):
    """Synthetic ORIGINALITY only; all release/pair/history predicates are real."""
    def __init__(self, pipeline):
        super().__init__()
        self.pipeline = pipeline
        self.main_advanced_at = None
        self.validated = []
        self.temporary = tempfile.TemporaryDirectory(prefix='synthetic-release-decisions-')
        folder = Path(self.temporary.name).resolve()
        self.genesis_original = {'syntheticRootJournalInitialization': self.scope,
                                 'implementation': digest(pipeline.modules['implementation'])}
        genesis = {'schema': 'hikyo-1046/decision-journal-genesis/v1', 'scope': self.scope,
                   'rootAuthorizationSHA256': digest(self.genesis_original)}
        (folder / 'genesis.once.json').write_bytes(canonical(genesis))
        self.journal = DecisionJournal(folder, scope=self.scope, genesis_sha256=digest(genesis))
        self.checkpoints = []

    def close(self):
        self.temporary.cleanup()

    def reopen_journal(self):
        self.journal = DecisionJournal(self.journal.folder, scope=self.scope,
                                      genesis_sha256=self.journal.genesis_sha256)

    def advance_main(self, at):
        """Synthetic external main observation with its actual action time.

        Earlier original observations remain available for reconstruction;
        a present observation cannot still attest the superseded main pair.
        """
        require(self.main_advanced_at is None and self.pipeline.now <= at,
                'one-explicit-synthetic-main-advance')
        self.main_advanced_at = instant(stamp(at))

    def observe(self, release_sha256, at, *, historical=False):
        # Native originals are the explicit synthetic boundary. Persistent heads
        # and globally consumed decisions come exclusively from real disk CAS.
        _, heads, journal_events = self.journal.snapshot()
        native = super().observe(release_sha256, at, historical=historical)
        head = heads.get(release_sha256)
        if historical:
            head = None
            for event in journal_events:
                proposed = self.journal._head(event['proposed'])
                if proposed.release_sha256 != release_sha256:
                    continue
                status = self.pipeline.records[proposed.status_sha256]
                if instant(status['recordedAt']) <= at:
                    head = proposed
        return Observation(native.scope, native.observed_at, native.terminal_sequence,
                           native.events, head)

    def compare_and_swap(self, expected, proposed, decision_sha256, observation):
        def authenticate_checkpoint(observed, journal_events):
            require(observed.scope == self.scope and observed.terminal_sequence == len(self.events)
                    and observed.events == tuple(self.events), 'synthetic-whole-native-checkpoint')
            # Validate complete native correspondence again while the journal's
            # actual exclusive lock is held. No boolean/cached head substitutes.
            for event in self.events:
                require(event.raw == canonical({'nativeDecision': event.decision})
                        if event.decision is not None else event.raw == canonical(
                            {'unrelated': event.sequence - 1}), 'checkpoint-original-correspondence')
            require(observed.head == expected, 'checkpoint-exact-durable-predecessor')
            self.checkpoints.append((observed, deepcopy(journal_events)))
            return deepcopy(observed)
        return self.journal.compare_and_swap(expected, proposed, decision_sha256, observation,
                                             authenticate_checkpoint)

    def verify_record(self, kind, record, records, at):
        p = self.pipeline
        originals = []
        if kind in ('releaseCandidate', 'mergeEvidence', 'attemptTrustBinding'):
            checked = collector.release_checked(p.m, p.release_collection)
            require(checked == p.release_collection['code'], 'actual-release-replay')
            p.m.affirmative_merge_checked(p.bootstrap['mergeDecision'],
                p.bootstrap['bootstrapPR'], p.bootstrap['observedAt'])
            originals.extend(Original(row['body'].encode(), row)
                             for row in p.release_collection['originals'])
            if kind == 'releaseCandidate':
                require(record == p.release, 'exact-collected-release-record')
            elif kind == 'mergeEvidence':
                require(record == p.merge, 'exact-collected-merge-record')
            else:
                require(self.main_advanced_at is None or at < self.main_advanced_at,
                        'synthetic-current-main-advanced')
                heads = collector.pair_checked(p.m, p.pair_collection, p.manifest,
                                               p.release_collection, at)
                require(heads == {'candidate': record['candidateHeadSHA'],
                                  'workflow': record['mainSHA']}, 'actual-binding-pair')
                originals.extend(Original(row['body'].encode(), row)
                                 for row in p.pair_collection['originals'])
        if kind == 'historicalHistory':
            require(record == p.history, 'literal-whole-historical-record')
            consumed = {**p.f.inputs, 'approval': p.f.inputs['consumedApproval']}
            p.m.held_history_checked(consumed)
            p.m.consumed_approval_checked(consumed)
            p.m.completed_history_checked(p.f.inputs)
            p.m.affirmative_merge_checked(p.bootstrap['mergeDecision'],
                p.bootstrap['bootstrapPR'], p.bootstrap['observedAt'])
        receipt = super().verify_record(kind, record, records, at)
        self.validated.append(kind)
        return replace(receipt, originals=tuple(originals)) if originals else receipt


class PairOriginals(collector.ExternalOriginals):
    """Explicit test originality boundary, exact whole synthetic provider captures."""
    def __init__(self, pipeline):
        self.pipeline = pipeline

    def verify(self, collection):
        p = self.pipeline
        require(collection == p.pair_collection, 'whole-synthetic-originals')
        collector.pair_checked(p.m, collection, p.manifest, p.release_collection, p.now)
        return True


class Pipeline:
    def __init__(self, version=1, *, archive=None, modules=None, source_sized=False, withdrawal_delay=1):
        self.archive = archive or archive_from_environment()
        self.modules = modules or sealed_modules()
        self.m = self.modules['model']
        self.fixture_source = self.archive.fixture_module('test_next_fixture_model.py')
        self.f = self.fixture_source.NextFixtureModelTests()
        self.f.setUp()
        self.now, self.at = self.f.now, self.f.at
        self.records = {}
        self.authority = PipelineAuthority(self)
        self.bootstrap = {key: deepcopy(self.f.code[key])
                          for key in ('bootstrapPR', 'mergeDecision', 'observedAt')}
        self.history = {key: deepcopy(self.f.inputs[key]) for key in (
            'priorApproval', 'ownerDirective', 'approvalContext', 'heldHistory',
            'consumedApproval', 'completedAttempt')}
        self.resource = self.modules['configuration'].configuration_candidate(
            self.m, self.f.inputs, self.f.inputs['proposalMarkdown'])
        raw = canonical(self.resource)
        self.modules['configuration'].configure_externally_reviewed(self.m,
            {key: value for key, value in self.f.inputs.items() if key != 'proposalMarkdown'},
            self.f.inputs['proposalMarkdown'], raw, reviewed_raw_sha256=raw_digest(raw))
        # Only injected synthetic provider chronology changes. Historical input
        # records above remain byte-for-byte literal at their original times.
        changes = dict(NOW=self.now - timedelta(minutes=3),
                       MERGED_AT='2026-10-22T00:30:00Z',
                       RUN_CREATED='2026-10-22T00:29:00Z',
                       RUN_COMPLETED='2026-10-22T00:50:00Z')
        original_files = providers.file_bytes
        def files(v):
            if not source_sized:
                return original_files(v)
            root = HERE.parents[2]
            return {path: (root / path).read_text() + '\n' + (' ' * v) + '\n'
                    for path in REPOSITORY_FILES}
        with patch.multiple(providers, **changes), patch.object(providers, 'file_bytes', files):
            self.provider = providers.Fixture(version)
            self.provider.m = self.m
            for response in self.provider.responses.values():
                response['body'] = response['body'].replace(
                    b'2026-10-23T09:59:00Z', b'2026-10-22T00:29:00Z')
            self.provider.review['reviewedAt'] = '2026-10-22T00:56:00Z'
            self.provider.review['originals']['review-original']['observedAt'] = '2026-10-22T00:56:00Z'
            self.release_collection = self.provider.release()
        self._release_records(version)
        self._configure_records()
        self.provider.manifest = deepcopy(self.manifest)
        self.pair_collection = collector.Collector(self.m, self.provider.transport,
            lambda: self.now).collect_pair(self.provider.pair_selection, self.manifest,
                                           self.release_collection)
        self.authority.register('attemptTrustBinding', self.binding)
        self.withdrawal = self._withdrawal(self.now + timedelta(seconds=withdrawal_delay))
        self.put(self.withdrawal)
        self.admission = ReleaseAdmission(self.config, self.records,
            implementation=self.modules['implementation'], historical_history=self.history,
            authority=self.authority)
        self.admission.adopt_now(self.now)
        self.m.configure_release(self.admission, self.release_collection, self.binding,
                                 self.manifest, self.bootstrap)
        self.code = self.m.register_pair_collection(self.pair_collection, self.manifest,
                                                   PairOriginals(self))
        self._prepare()

    def close(self):
        self.f.doCleanups()
        self.authority.close()

    def load_core(self, name):
        import importlib.util
        import uuid
        relative = 'core/' + name
        raw = (HERE / relative).read_bytes()
        require(raw_digest(raw) == self.modules['implementation']['files'][relative],
                'actual-sealed-helper-source')
        key = '_pipeline_actual_' + uuid.uuid4().hex
        spec = importlib.util.spec_from_file_location(key, HERE / relative)
        module = importlib.util.module_from_spec(spec)
        sys.modules[key] = module
        exec(compile(raw, str(HERE / relative), 'exec'), module.__dict__)
        return module

    def put(self, record):
        key = digest(record)
        self.records[key] = deepcopy(record)
        from release_records import _KINDS
        kind = _KINDS.get(record.get('schema'))
        if kind:
            self.authority.register(kind, record)
        return key

    def _release_records(self, version):
        c = self.release_collection['code']
        e = c['episode']
        pr = {key: e['pullRequest'][key] for key in
              ('number', 'nodeId', 'headSHA', 'baseSHA', 'mergeSHA', 'mergedAt')}
        events = [row for page in e['timelinePages'] for row in page
                  if row.get('event') == 'merged']
        require(len(events) == 1, 'one-actual-merged-event')
        event = events[0]
        self.merge = {'schema': SCHEMAS['mergeEvidence'], 'mode': EVIDENCE_MODE,
            'repository': dict(REPOSITORY), 'pullRequest': dict(pr, state='merged'),
            'mergedEvent': {'restId': event['id'], 'nodeId': event['node_id'],
                'actor': dict(OWNER_ACTOR), 'createdAt': event['created_at'], 'commitSHA': event['commit_id']},
            'publicCommitSHA256': digest(e['publicCommit']),
            'queueRunEvidenceSHA256': digest(e['queueRun']),
            'sourceRangeEvidenceSHA256': digest(e['sourceRange']),
            'timelineEvidenceSHA256': digest({'rest': e['timelinePages'], 'graphql': e['graphqlPages']}),
            'captureIndexSHA256': collector.collection_digest(self.m, self.release_collection),
            'finalReadbackSHA256': digest(c['finalReadbacks'])}
        commit = e['publicCommit']
        self.release = {'schema': SCHEMAS['releaseCandidate'], 'policySHA256': POLICY_SHA256,
            'historicalHistorySHA256': digest(self.history), 'previousReleaseSHA256': None,
            'repository': dict(REPOSITORY),
            'releasePR': dict(pr, sourceRangeSHA256=self.merge['sourceRangeEvidenceSHA256']),
            'mergeCommit': {'sha': commit['sha'], 'treeSHA': commit['treeSHA'],
                            'orderedParents': commit['parents']},
            'trustedFileSHA256s': collector._pins(c['releaseSource']['files']),
            'workflowRun': {'id': e['queueRun']['id'], 'attempt': e['queueRun']['run_attempt'],
                'path': e['queueRun']['path'], 'event': e['queueRun']['event'],
                'name': e['queueRun']['name'], 'headSHA': e['queueRun']['head_sha'],
                'testedCommitSHA': e['queueRun']['testedCommit']['sha'],
                'jobsEvidenceSHA256': self.merge['queueRunEvidenceSHA256']},
            'changeReviewSHA256': digest(self.release_collection['changeReview']),
            'evidenceIndexSHA256': collector.collection_digest(self.m, self.release_collection),
            'collectedAt': self.release_collection['finishedAt'], 'evidenceMode': EVIDENCE_MODE,
            'mergeEvidenceSHA256': self.put(self.merge)}
        release_pin = self.put(self.release)
        self.owner_original = {'syntheticOwnerDecision': 'approve-trusted-release',
            'releaseSHA256': release_pin, 'resourceConfiguration': self.resource,
            'decidedAt': '2026-10-22T00:58:00Z', 'version': version}
        self.approval = {'schema': SCHEMAS['ownerReleaseApproval'], 'purpose': 'approve-trusted-release',
            'policySHA256': POLICY_SHA256, 'releaseSHA256': release_pin,
            'resourceConfigurationSHA256': digest(self.resource), 'acceptanceSeries': ACCEPTANCE_SERIES,
            'actor': dict(OWNER_ACTOR), 'originalDecisionCustodySHA256': digest(self.owner_original),
            'decidedAt': self.owner_original['decidedAt'], 'validUntil': '2026-10-22T22:00:00Z',
            'evidenceMode': EVIDENCE_MODE, 'supersedesRevocationSHA256': None,
            'supersedesAdoptionSHA256': None, 'supersedesOwnerApprovalSHA256': None}
        approval_pin = self.put(self.approval)
        self.authority.add('owner-approval', release_pin, None, approval_pin,
                           digest(self.owner_original), self.approval['decidedAt'])
        self.root_original = {'syntheticRootAdoption': release_pin,
            'implementation': self.modules['implementation'], 'ownerApprovalSHA256': approval_pin}
        self.adoption = {'schema': SCHEMAS['rootReleaseAdoption'], 'policySHA256': POLICY_SHA256,
            'implementationManifestSHA256': digest(self.modules['implementation']),
            'releaseSHA256': release_pin, 'ownerApprovalSHA256': approval_pin,
            'reviewSHA256': digest({'independentSyntheticReview': self.root_original}),
            'originalCustodyIndexSHA256': digest(self.root_original),
            'resourceConfigurationSHA256': digest(self.resource), 'adoptedAt': '2026-10-22T00:59:00Z',
            'adoptionKind': 'initial', 'supersedesAdoptionSHA256': None, 'recoverySHA256': None}
        adoption_pin = self.put(self.adoption)
        self.genesis = {'schema': SCHEMAS['releaseStatus'], 'rootAdoptionSHA256': adoption_pin,
            'releaseSHA256': release_pin, 'sequence': 1, 'previousStatusSHA256': None,
            'disposition': 'eligible', 'reason': 'release-adopted',
            'decisionCustodySHA256': digest({'syntheticGenesisOriginal': self.root_original}),
            'recordedAt': self.adoption['adoptedAt']}
        self.put(self.genesis)
        self.authority.add('root-adoption', release_pin, adoption_pin, adoption_pin,
                           self.genesis['decisionCustodySHA256'], self.genesis['recordedAt'])
        for kind, value in (('implementation', self.modules['implementation']),
                            ('historicalHistory', self.history), ('resourceConfiguration', self.resource)):
            self.authority.register(kind, value)

    def _configure_records(self):
        self.config = {'schema': SCHEMAS['releaseConfiguration'], 'resourceConfiguration': self.resource,
            'resourceConfigurationSHA256': digest(self.resource), 'policySHA256': POLICY_SHA256,
            'implementationManifestSHA256': digest(self.modules['implementation']),
            'releaseSHA256': digest(self.release), 'ownerApprovalSHA256': digest(self.approval),
            'rootAdoptionSHA256': digest(self.adoption), 'statusGenesisSHA256': digest(self.genesis),
            'historicalHistorySHA256': digest(self.history)}
        self.put(self.config)
        self.manifest = {'schema': SCHEMAS['sourceManifest'],
            'modelSHA256': self.modules['implementation']['files']['core/next_fixture_model.source-only.py'],
            'testSHA256': self.modules['implementation']['files']['core/test_next_fixture_model.py'],
            'frozenNextHelpers': dict(FROZEN),
            'candidateFileSHA256s': self.provider.manifest['candidateFileSHA256s'],
            'candidateCIFileSHA256s': {key: self.release['trustedFileSHA256s'][key] for key in CANDIDATE_CI_FILES},
            'workflowFileSHA256s': self.release['trustedFileSHA256s'],
            'requiredCIJobs': {key: dict(value) for key, value in REQUIRED_CI_JOBS.items()},
            'configurationSHA256': digest(self.config), **{key: self.config[key] for key in (
                'resourceConfigurationSHA256', 'policySHA256', 'releaseSHA256', 'ownerApprovalSHA256',
                'rootAdoptionSHA256', 'historicalHistorySHA256')}}
        self.put(self.manifest)
        main = self.provider.read_value(collector.REPO + '/pulls/' + str(self.provider.main_pr))
        self.binding = {'schema': SCHEMAS['attemptTrustBinding'], **{key: self.config[key] for key in (
            'policySHA256', 'implementationManifestSHA256', 'releaseSHA256', 'ownerApprovalSHA256',
            'rootAdoptionSHA256', 'statusGenesisSHA256', 'resourceConfigurationSHA256', 'historicalHistorySHA256')},
            'candidateHeadSHA': self.provider.candidate_sha, 'mainSHA': self.provider.main_sha,
            'candidateRun': {'id': self.provider.candidate_run, 'attempt': 1},
            'sourceManifestSHA256': digest(self.manifest),
            'currentMainRun': {'id': self.provider.main_run, 'attempt': 1,
                'mergedPRNumber': self.provider.main_pr, 'headSHA': main['head']['sha'],
                'baseSHA': main['base']['sha'], 'mergeSHA': main['merge_commit_sha']},
            'releaseConfigurationSHA256': digest(self.config)}
        self.put(self.binding)

    def _prepare(self):
        f, m = self.f, self.m
        f.candidate, f.workflow = self.provider.candidate_sha, self.provider.main_sha
        f.manifest, f.code = self.manifest, self.code
        f.records['code'] = self.code
        f.proposal = m.unbound_proposal(f.inputs, self.manifest)
        fixtures = self.archive.fixture_module('test_normalize_next_inventory.py')
        fixtures.m = m
        fixtures.n = self.load_core('normalize_next_inventory.source-only.py')
        fixtures.s = self.load_core('normalize_setup_whole_reads.source-only.py')
        builder = fixtures.InventoryBridgeTests()
        builder.f, builder.now = f, self.now
        builder.ms = fixtures.n.epoch_ms(self.now) - 1000
        read_plan = {'syntheticBaselineReadPlan': self.binding}
        builder.bound = {'preparationSHA256': digest(read_plan), 'run': {'keyARN': f.key_arn},
                         'records': {'key': f.key}}
        builder.inv, builder.custody = f.sealing['inventory'], f.sealing['custody']
        builder.owner_sha = digest(f.owner['ownerContext'])
        builder.prepare_baseline()
        normalized = fixtures.n.normalize_baseline(m, fixtures.s, fixtures.n.PINS, f.proposal,
            builder.iam, builder.kms, builder.setup, owner_context=f.owner['ownerContext'],
            environment=f.environment, reviewed_preparation_sha256=digest(read_plan), explicit_now=self.now)
        f.owner = normalized['owner']
        f.records['owner'] = deepcopy(f.owner)
        self.baseline_normalization = normalized
        f.prepared = self.modules['configuration'].prepare(m, f.inputs, f.inputs['proposalMarkdown'],
            self.manifest, f.records, {key: digest(value) for key, value in f.records.items()}, **f.kwargs)
        f.proposal = f.prepared['proposal']
        self.prepared = f.prepared

    def _withdrawal(self, at):
        status = {'schema': SCHEMAS['releaseStatus'], 'rootAdoptionSHA256': digest(self.adoption),
            'releaseSHA256': digest(self.release), 'sequence': 2, 'previousStatusSHA256': digest(self.genesis),
            'disposition': 'revoked', 'reason': 'owner-withdrawal',
            'decisionCustodySHA256': digest({'syntheticWithdrawalOriginal': digest(self.release), 'at': stamp(at)}),
            'recordedAt': stamp(at)}
        return status

    def withdraw(self, at):
        status = self.withdrawal
        require(status['recordedAt'] == stamp(at), 'preloaded-immutable-withdrawal')
        key = digest(status)
        self.authority.add('owner-withdrawal', digest(self.release), digest(self.adoption), key,
                           status['decisionCustodySHA256'], stamp(at))
        self.admission.transition_now(key, at)
        return status


REPOSITORY_FILES = tuple(sorted(__import__('release_policy').TRUSTED_FILES))


class Execution:
    """Real controller and ledger. Only originality/provider transports doubled."""
    def __init__(self, pipeline, *, base=None, operator_leaf="next_operator_orchestration_stdout.source-only.py"):
        self.p = pipeline
        self.f, self.m = pipeline.f, pipeline.m
        self.x, self.l = pipeline.modules['executor'], pipeline.modules['ledger']
        self.clock = pipeline.now
        self.temporary = tempfile.TemporaryDirectory(prefix='hikyo-two-release-') if base is None else None
        self.base = Path(self.temporary.name if self.temporary else base).resolve()
        self.parent = self.base / 'aws-revalidation-20261018b'
        self.folder = self.base / self.m.configuration_checked()['ledgerFolder']
        self.parent.mkdir(exist_ok=True)
        self.folder.mkdir(exist_ok=True)
        self.ledger = self.l.Ledger(self.folder, self.parent,
            reviewed_folder=self.m.configuration_checked()['ledgerFolder'])
        self.registered = set()
        implementation = pipeline.modules['implementation']['files']
        self.sources = {'modelSHA256': implementation['core/next_fixture_model.source-only.py'],
            'modelTestSHA256': implementation['core/test_next_fixture_model.py'],
            'executorSHA256': implementation['core/next_fixture_execution.source-only.py'],
            'executorTestSHA256': implementation['test_two_release_pipeline.py'],
            'ledgerSHA256': implementation['core/next_fixture_ledger.source-only.py'],
            'frozenNextHelpers': dict(FROZEN)}
        self.explicit_clock = lambda: self.clock
        self.operator_module = pipeline.load_core(operator_leaf)
        n = pipeline.modules['finalizer']
        self.final_sources = {'execution': self.sources, 'modelSHA256': n.MODEL_SHA256,
            'executorSHA256': n.EXECUTOR_SHA256, 'resultSHA256': n.RESULT_SHA256,
            'finalizationSHA256': implementation['core/next_owner_finalization.source-only.py'],
            'nativeGuardSHA256s': dict(n.NATIVE_GUARD_PINS)}
        roles = {key: pipeline.modules[key] for key in ('model','executor','ledger','runtime','result','finalizer')}
        self.operator = self.operator_module.Operator(roles, self.ledger, self.f.prepared,
            self.sources, self.final_sources, implementation['core/'+operator_leaf], self.explicit_clock)
        # Root originality alone is synthetic. The immutable snapshot consumer,
        # whole-original projections and all semantic validators remain actual.
        runtime = pipeline.modules['runtime']
        class SyntheticRootOriginals(runtime.RootCaptureAuthority):
            def __init__(self, snapshot):
                self.snapshot = deepcopy(snapshot)
            def release_root_reviewed_snapshot(self):
                return deepcopy(self.snapshot)
        self.root_original_type = SyntheticRootOriginals
        self.controller = None
        self.capture_sequence = 0
        self.historical = deepcopy(self.f.historical)

    def close(self):
        if hasattr(self, 'native_connection'):
            self.native_connection.close()
        if self.temporary:
            self.temporary.cleanup()

    def root_capture(self, bundle, context, *, scopes=()):
        self.capture_sequence += 1
        at = stamp(self.clock)
        originals = [{'source': row['source'], 'operation': row['operation'],
            'selector': deepcopy(row['selector']), 'receivedAt': at,
            'toolName': 'synthetic-offline-original-transport',
            'toolCallId': 'synthetic-original-'+str(self.capture_sequence)+'-'+str(index),
            'toolResult': {'response': deepcopy(row['response'])}, 'responsePath': ['response']}
            for index,row in enumerate(bundle['rawEvidence'])]
        snapshot = {'schema':'hikyo-158/root-original-capture-release/v1','releasedAt':at,
            'bridgeSHA256':self.p.modules['implementation']['files']['core/next_fixture_runtime_bridge.source-only.py'],
            'captures':[{'captureId':'synthetic-capture-'+str(self.capture_sequence),
                'capturedAt':at,'context':deepcopy(context),'bundle':deepcopy(bundle),'originals':originals}],
            'transportScopes':deepcopy(list(scopes))}
        self.root_snapshot_sizes = getattr(self,'root_snapshot_sizes',[])+[len(self.m.canonical(snapshot))]
        return self.root_original_type(snapshot)

    def head(self):
        with self.ledger.lock():
            return self.ledger.snapshot()[0]

    def raw_original(self, records, kind):
        # The synthetic external tool emits its complete lossless wire as its
        # original output. No prior original capture is rewritten or rehashed.
        wire = self.operator_module.encode_tool_wire(self.m, records, kind)
        if kind=='phase-records' and hasattr(self.operator_module,'STDOUT_OPERATION'):
            response = self.m.canonical(wire).decode()
            operation = self.operator_module.STDOUT_OPERATION
            selector = {'schema':self.operator_module.STDOUT_SELECTOR_SCHEMA,'kind':kind}
        else:
            response,operation,selector=wire,'synthetic-complete-model-output',{'kind':kind}
        return [{'source':'public-source','operation':operation,'selector':selector,
            'response':response,'responseSHA256':digest(response),'observedAt':stamp(self.clock)}]

    def bundle(self, kind, records, *, scopes=()):
        value = {'schema':'hikyo-158/next-external-proof-bundle/v1','kind':kind,
            'observedAt':stamp(self.clock),'records':{'stdoutEvidenceIndex':0},
            'rawEvidence':self.raw_original(records,'phase-records')}
        context = self.operator.describe_executor_context(kind, None if kind=='reserve' else self.head())
        self.capture_authority = self.root_capture(value,context,scopes=scopes)
        registry = self.operator._registry(self.capture_authority)
        actual = self.operator._controller(registry)
        if self.controller is None:
            self.controller = actual
        else:
            self.controller.adapter = actual.adapter
        self.registered.add(canonical(value))
        return value

    def reserve(self):
        proof = self.bundle('reserve',self.f.records)
        return self.operator.reserve(self.capture_authority,proof)

    def arm_key(self):
        """Arm and burn exactly one real controller ticket; never accept a key."""
        f, c = self.f, self.controller
        self.full_key_intent = c.arm_owner('CreateKey', f.key['intent']['request'],
            self.bundle('owner-CreateKey', f.records))
        self.key_ticket = c.owner_forward_ticket('CreateKey',
            self.bundle('owner-forward-CreateKey', f.records))
        return deepcopy(self.full_key_intent), deepcopy(self.key_ticket)

    def capture_key_from_normalized(self, intent, ticket, normalized):
        """Accept real kn.normalize output after H/N/D verified its native row.

        Does not reserve, arm, issue a ticket or perform a native forward.
        The caller retains/authenticates complete native custody at its external
        original boundary. Core semantic checks and the actual durable receipt
        path execute here on the same controller and model instance.
        """
        m, f, c = self.m, self.f, self.controller
        m.require(normalized['schema']=='hikyo-158/next-key-native-normalized-post-send/v1',
                  'actual-normalized-key-custody-shape')
        provenance=normalized['provenance']
        m.require(provenance['modelSHA256']==self.sources['modelSHA256']
                  and provenance['modelTestSHA256']==self.sources['modelTestSHA256'],
                  'native-key-actual-model-source-pins')
        with self.ledger.lock():
            _, state=c._state()
            m.require(m.same(self.ledger.read_once('owner-CreateKey-intent.once.json'),intent)
                      and m.same(self.ledger.read_once('owner-CreateKey-transmit.once.json'),ticket)
                      and m.same(state['pending'],intent)
                      and intent['attemptTrustBindingSHA256']==ticket['attemptTrustBindingSHA256']
                      ==f.prepared['attemptTrustBindingSHA256'], 'same-original-native-key-intent-ticket')
        key=deepcopy(normalized['key'])
        result={'intentSHA256':digest(intent),'maximumForwards':1,'forwardsObserved':1,
            'sentAt':key['sentAt'],'receivedAt':key['receivedAt'],
            'response':key['createResponse'],'keyProof':key}
        c.accept_owner_receipt('CreateKey',self.bundle('owner-result-CreateKey',result))
        f.key=key
        self.normalized_key=deepcopy(normalized)
        self.full_key_intent,self.key_ticket=deepcopy(intent),deepcopy(ticket)
        return key

    def capture_key(self, *, native=False):
        if native:
            from test_adapter_context import execute_native_create
            return self.capture_key_from_native(execute_native_create(self))
        intent,ticket=self.arm_key()
        key=deepcopy(self.f.key)
        key.update(preparationSHA256=digest(self.f.prepared),claimId=intent['claimId'])
        key['intentSHA256']=digest({name:key[name] for name in ('preparationSHA256','claimId','intent')})
        result={'intentSHA256':digest(intent),'maximumForwards':1,'forwardsObserved':1,
            'sentAt':key['sentAt'],'receivedAt':key['receivedAt'],'response':key['createResponse'],'keyProof':key}
        self.controller.accept_owner_receipt('CreateKey',self.bundle('owner-result-CreateKey',result))
        self.f.key=key
        return key

    def bind(self):
        f, c, m = self.f, self.controller, self.m
        self.dispatch = c.arm_dispatch(self.bundle('dispatch', f.records))
        c.dispatch_forward_ticket(self.bundle('dispatch-forward', f.records))
        records = deepcopy(f.binding)
        records.update(key=f.key, code=f.code, owner=deepcopy(f.records['owner']),
            budget=deepcopy(f.records['budget']), historicalAbsence=deepcopy(f.historical),
            firstMutationEvidence=m.first_mutation_evidence(f.prepared, f.key))
        # Synthetic current job and variables are derived from the actual pair.
        records['job']['run']['head_sha'] = f.workflow
        records['job']['run']['inputs']['reviewed_sha'] = f.candidate
        proof = self.bundle('bind', {'bindingRecords': records, 'reviewedStart': f.at,
            'historicalAbsence': f.historical, 'dispatchCustody': {'intentSHA256': digest(self.dispatch),
                'forwardsObserved': 1, 'sentAt': f.at, 'receivedAt': f.at,
                'runId': records['job']['run']['id']}})
        f.bound = self.operator.bind(self.capture_authority,proof)
        c.bound = deepcopy(f.bound)
        return f.bound

    def refresh_synthetic_inventory(self):
        f, m = self.f, self.m
        documents = f.bound['documents']
        f.sealing['owner']['setupPolicy']['versions']['v6']['document'] = deepcopy(documents['setup-user.managed-policy.json'])
        inv, custody = f.sealing['inventory'], f.sealing['custody']
        inv['boundary']['versions']['v1']['document'] = deepcopy(documents['setup-role-boundary.json'])
        custody['boundary']['documentSHA256'] = digest(documents['setup-role-boundary.json'])
        for name, role in inv['roles'].items():
            role['AssumeRolePolicyDocument'] = deepcopy(documents[name + '.trust.json'])
            role['inlinePolicies']['hikyo-158-fixture'] = deepcopy(documents[name + '.policy.json'])
        custody['boundPlanSHA256'] = digest(f.bound)
        expected = m.expected_creation_operations(f.bound, inv)
        for sequence, (row, wanted) in enumerate(zip(custody['operations'], expected), 1):
            row.update(boundPlanSHA256=digest(f.bound), request=deepcopy(wanted['request']))
            row['claimId'] = ('next-owner-' + row['operation'] if sequence <= 3
                              else 'next-provision-' + str(sequence - 3))
            row['intentSHA256'] = digest({key: row[key] for key in (
                'sequence', 'boundPlanSHA256', 'claimId', 'maximumForwards',
                'operation', 'request', 'actorARN')})
        f.sealing['job'] = deepcopy(f.job)
        f.sealing['job']['run']['head_sha'] = f.workflow
        f.sealing['job']['run']['inputs']['reviewed_sha'] = f.candidate
        f.sealing['job']['environment']['variables'] = m.environment_variables(f.bound['run'])

    def inventory_builder(self):
        fixture = self.p.archive.fixture_module('test_normalize_next_inventory.py')
        # Rebind only the builder's dependencies to the ACTUAL successor
        # implementations. Every normalization and checker remains real.
        fixture.m = self.m
        fixture.n = self.load_core('normalize_next_inventory.source-only.py')
        fixture.s = self.load_core('normalize_setup_whole_reads.source-only.py')
        builder = fixture.InventoryBridgeTests()
        builder.f, builder.bound = self.f, self.f.bound
        builder.now, builder.ms = self.clock, fixture.n.epoch_ms(self.clock) - 1000
        builder.custody, builder.inv = self.f.sealing['custody'], self.f.sealing['inventory']
        builder.owner_sha = digest(self.f.bound['records']['owner']['ownerContext'])
        return fixture, builder

    def load_core(self, name):
        return self.p.load_core(name)

    def active(self, stage='active', boundary=True):
        f = self.f
        policy = deepcopy(f.owner['setupPolicy'])
        if stage == 'deleted':
            del policy['versions']['v1']
        elif stage == 'active':
            policy = deepcopy(f.sealing['owner']['setupPolicy'])
        return {'code': deepcopy(f.code), 'budget': deepcopy(f.budget),
            'historicalAbsence': deepcopy(f.historical),
            'ownerContext': deepcopy(f.owner['ownerContext']),
            'ownerStage': {'observedAt': f.at, 'setupUser': deepcopy(f.owner['setupUser']), 'setupPolicy': policy},
            'oldKeys': deepcopy(f.old_keys), 'newKey': deepcopy(f.key['readback']),
            'boundary': {'observedAt': f.at,
                         'value': deepcopy(f.sealing['inventory']['boundary']) if boundary else None}}

    def provision_and_seal(self, *, after_write=None):
        f, m, c = self.f, self.m, self.controller
        self.refresh_synthetic_inventory()
        for operation, stage, request, response in [
            ('DeletePolicyVersion', 'original', {'PolicyArn': m.SETUP, 'VersionId': 'v1'}, {}),
            ('CreatePolicyVersion', 'deleted', {'PolicyArn': m.SETUP,
                'PolicyDocument': f.bound['documents']['setup-user.managed-policy.json'], 'SetAsDefault': True},
                {'VersionId': 'v6', 'IsDefaultVersion': True}),
            ('CreatePolicy', 'active', {'PolicyName': 'hikyo-158-role-boundary', 'Path': '/',
                'PolicyDocument': f.bound['documents']['setup-role-boundary.json']},
                {'Arn': m.BOUNDARY, 'PolicyId': f.sealing['inventory']['boundary']['policyId'], 'DefaultVersionId': 'v1'})]:
            records = self.active(stage, boundary=False)
            intent = c.arm_owner(operation, request, self.bundle('owner-' + operation, records))
            c.owner_forward_ticket(operation, self.bundle('owner-forward-' + operation, records))
            c.accept_owner_receipt(operation, self.bundle('owner-result-' + operation,
                {'intentSHA256': digest(intent), 'maximumForwards': 1, 'forwardsObserved': 1,
                 'sentAt': f.at, 'receivedAt': f.at, 'response': response, 'keyProof': None}))
        fixture = self.p.archive.fixture_module('test_next_fixture_execution.py')
        fixture.m = m
        fixture.x = self.x
        self.runner = fixture.SyntheticRunner(f)
        seen_writes = 0
        self.provider_timeouts=[]
        def provider_transport(args, *, env, timeout):
            nonlocal seen_writes
            if args[2] in ("create-open-id-connect-provider","create-role","put-role-policy"):
                self.provider_timeouts.append(timeout)
            result = self.runner(args, env=env, timeout=timeout)
            if len(self.runner.writes) != seen_writes:
                seen_writes = len(self.runner.writes)
                if after_write is not None:
                    after_write(self, seen_writes)
            return result
        self.cli_inputs = {'loginRoot':str(self.base / 'private-operator/aws-local-login'),
            'baseEnvironment':{'PATH':'/opt/homebrew/bin:/usr/bin:/bin','LANG':'C','TMPDIR':str(self.base)},
            'thumbprints':['f'*40]}
        scope = self.operator.describe_cli_scope('provision',self.cli_inputs)
        proof = self.bundle('provision',self.active(),scopes=[scope])
        phase = self.operator.prepare_cli_phase('provision',self.capture_authority,proof,self.cli_inputs)
        self.cli = phase.cli
        def synthetic_provider_engine(args, *, env, timeout):
            return {**provider_transport(args,env=env,timeout=timeout),'timedOut':False,'overflow':False}
        self.provider_engine = synthetic_provider_engine
        phase.run(synthetic_provider_engine)
        # Real normalization from synthetic raw XML/JSON native reads.
        self.clock += timedelta(seconds=1)
        fixture, builder = self.inventory_builder()
        builder.iam = builder.bundle('iam', 'inventory')
        builder.kms = builder.bundle('kms', 'inventory')
        builder.setup = builder.setup_bundle('activated')
        normalized = fixture.n.normalize_inventory(m, fixture.s, fixture.n.PINS,
            f.bound, f.sealing['custody'], builder.iam, builder.kms, builder.setup,
            owner_context_sha256=builder.owner_sha, explicit_now=self.clock)
        sealing = deepcopy(f.sealing)
        sealing.update(owner=normalized['owner'],inventory=normalized['inventory'],historicalAbsence=deepcopy(f.historical))
        self.sealing_records=sealing
        proof=self.bundle('seal',{'sealingRecords':sealing,'historicalAbsence':f.historical})
        self.sealed=self.operator.seal(self.capture_authority,proof)
        self.normalized = normalized
        return self.sealed

    def _hosted_result_builder(self):
        fixture = self.p.archive.fixture_module('test_next_hosted_result.py')
        builder = fixture.HostedResultTests()
        builder.setUp()
        h, m, f = self.p.modules['result'], self.m, self.f
        old_candidate, old_workflow = builder.f.candidate, builder.f.workflow
        old_at = stamp(builder.now)
        old_completed = builder.completed
        old_started = stamp(builder.f.now + timedelta(minutes=1))
        self.clock = self.p.now + timedelta(seconds=4)
        completed = stamp(self.p.now + timedelta(seconds=2))
        started = stamp(self.p.now + timedelta(seconds=1))
        def current(value):
            if type(value) is dict:
                return {key: current(child) for key, child in value.items()}
            if type(value) is list:
                return [current(child) for child in value]
            if type(value) is str:
                return {old_candidate: f.candidate, old_workflow: f.workflow,
                        old_at: stamp(self.clock), old_completed: completed,
                        old_started: started}.get(value, value)
            return value
        builder.bundle = current(builder.bundle)
        builder.data = current(builder.data)
        builder.bundle.update(boundPlanSHA256=digest(f.bound),
            sealedModelSHA256=digest(self.sealed) if hasattr(self, 'sealed') else None)
        builder.bundle['artifactPages'][0]['items'][0]['name'] = (
            'aws-158-' + f.bound['run']['runId'] + '-' + f.candidate)
        builder.now = self.clock
        builder.completed = completed
        builder.bundle['absenceRecords'] = [builder.absence(builder.arn)]
        builder.bundle['historicalAbsence'] = self.p.fixture_source.historical_fixture(
            digest(f.owner['ownerContext']), self.clock)
        builder.repack()
        return builder

    def validate_result_bundle(self, bundle, *, archive_bytes=None, never_started=False):
        raw = self.raw_original(bundle,'hosted-result-input')
        capture = self.operator.describe_result_capture(bundle,raw,
            archive_bytes=archive_bytes,never_started=never_started)
        authority = self.root_capture(capture['captureBundle'],capture['context'])
        return self.operator.validate_result(authority,bundle,raw,
            archive_bytes=archive_bytes,never_started=never_started)

    def hosted_result(self, *, failed=False):
        builder = self._hosted_result_builder()
        if failed:
            # The unchanged historical builder supplies complete failed-create
            # custody; only its synthetic configured run and clock are updated.
            builder.bound = deepcopy(self.f.bound)
            builder.failed()
            proof = builder.bundle['failedCreationCustody']
            for intent, outcome in zip(proof['intents'], proof['outcomes']):
                intent['sentAt'] = stamp(self.p.now + timedelta(seconds=1))
                outcome.update(intentSHA256=digest(intent),
                    receivedAt=stamp(self.p.now + timedelta(seconds=2)))
        self.result = self.validate_result_bundle(builder.bundle,archive_bytes=builder.archive)
        self.hosted_builder = builder
        return self.result

    def never_started_bundle(self):
        """Synthetic cancelled originals from the actual controller's custody.

        No original disk record or production validator is altered. Each test
        variant is captured afresh by the real Operator/OriginalCaptureRegistry.
        """
        builder = self._hosted_result_builder()
        bundle = {key: deepcopy(builder.bundle[key]) for key in (
            'observedAt', 'boundPlanSHA256', 'sealedModelSHA256', 'run',
            'jobPages', 'artifactPages', 'historicalAbsence')}
        bundle['schema'] = 'hikyo-158/never-started-cancelled-bundle/v1'
        bundle['run']['conclusion'] = 'cancelled'
        for key in ('jobPages', 'artifactPages'):
            bundle[key][0].update(total_count=0,items=[])
        bundle['approvalHistory'] = {
            'endpoint': self.p.modules['result'].API + self.f.bound['run']['runId'] + '/approvals',
            'observedAt': stamp(self.clock), 'response': []}
        with self.ledger.lock():
            claim = self.ledger.claim_record()
            events = [self.m.strict_json(path.read_bytes())
                      for path in sorted(self.folder.glob('event-*.once.json'))]
            head = self.ledger.snapshot()[0]
        bundle['originalCustody'] = {'claim': claim, 'events': events, 'headSHA256': head}
        return bundle

    def cleanup_code(self):
        return {'schema': 'hikyo-158/immutable-dispatch-code-readback/v1',
            'observedAt': stamp(self.clock), 'boundPlanSHA256': digest(self.f.bound),
            **{key: deepcopy(self.f.code[key]) for key in (
                'candidateCommit', 'workflowCommit', 'candidateFiles', 'workflowFiles', 'absentFiles')}}

    def retire(self):
        fixture = self.p.archive.fixture_module('test_next_fixture_execution.py')
        fixture.m = self.m
        fixture.x = self.x
        records = fixture.ExecutionTests.retirement(self)
        records['secretAbsence'] = self.result['secretAbsenceProof']
        scope=self.operator.describe_cli_scope('retire',self.cli_inputs)
        proof=self.bundle('retire',records,scopes=[scope])
        phase=self.operator.prepare_cli_phase('retire',self.capture_authority,proof,self.cli_inputs)
        phase.run(self.provider_engine)
        self.cli=phase.cli
        return records

    def finalize(self):
        fixture = self.p.archive.fixture_module('test_next_owner_finalization.py')
        builder = fixture.FinalOwnerTests()
        try:
            builder.setUp()
            records = deepcopy(builder.records)
        finally:
            builder.doCleanups()
        n, m, f = self.p.modules['finalizer'], self.m, self.f
        records.update(hostedResult=deepcopy(self.result),code=self.cleanup_code(),
            ownerContext=deepcopy(f.owner['ownerContext']),setupUser=deepcopy(f.owner['setupUser']),
            historicalAbsence=self.p.fixture_source.historical_fixture(
                digest(f.owner['ownerContext']), self.clock),
            setupPolicy=deepcopy(f.sealing['owner']['setupPolicy']))
        records['retirement']['observedAt'] = stamp(self.clock)
        with self.ledger.lock():
            schedule = self.ledger.read_once('retire-18-response.once.json')['outcome']['value']
        records['retirement']['newKey'].update(DeletionDate=schedule['DeletionDate'])
        records['boundary']['value'] = deepcopy(f.sealing['inventory']['boundary'])
        records['boundary']['observedAt'] = stamp(self.clock)
        records['ownerMFA']['observedAt'] = stamp(self.clock)
        self.finalizer_records = records
        for operation in ('DeletePolicy', 'SetDefaultPolicyVersion'):
            proof=self.final_bundle('arm-'+operation,records)
            intent=self.operator.arm_final_owner(operation,self.capture_authority,proof)
            proof=self.final_bundle('ticket-'+operation,records)
            ticket=self.operator.final_owner_contract(operation,self.capture_authority,proof)['ticket']
            native = {'syntheticNativePreparation': intent['requiredNativePreparation'],
                      'ticket': ticket}
            receipt = {'schema': 'hikyo-158/final-owner-response/v1', 'operation': operation,
                'intentSHA256': digest(intent), 'ticketSHA256': digest(ticket),
                'request': intent['request'], 'sentAt': stamp(self.clock), 'receivedAt': stamp(self.clock),
                'forwardsObserved': 1, 'httpStatus': 200, 'requestId': 'synthetic-final-owner-response',
                'response': {}, 'sourceReceipt': {'source': 'synthetic native forward custody',
                    'requiredPreparationSHA256': digest(intent['requiredNativePreparation']),
                    'nativePreparationSHA256': digest(native),
                    'nativeGuardSourceSHA256': n.NATIVE_GUARD_PINS[
                        'native_next_fixture_iam_guard.source-only.js' if operation == 'DeletePolicy'
                        else 'native_setup_version_operations_guard.source-only.js']}}
            proof=self.final_bundle('receipt-'+operation,receipt)
            self.operator.accept_final_owner(operation,self.capture_authority,proof)
            if operation == 'DeletePolicy':
                records['boundary'].update(value={'operation': 'GetPolicy', 'arn': m.BOUNDARY,
                                                  'errorCode': 'NoSuchEntity'}, attachments=None)
            else:
                policy = records['setupPolicy']
                policy['defaultVersionId'] = 'v5'
                policy['versions']['v6']['isDefault'] = False
                policy['versions']['v5']['isDefault'] = True
        restored = deepcopy(self.sealed['records']['owner'])
        restored.update(schema='hikyo-158/next-restored-owner/v1', observedAt=stamp(self.clock),
                        setupPolicy=deepcopy(records['setupPolicy']))
        proof=self.final_bundle('complete',{'readback':records,'restoredOwner':restored})
        self.closure=self.operator.complete_final_owner(self.capture_authority,proof)
        return self.closure

    def final_bundle(self, kind, records):
        bundle={'schema':'hikyo-158/next-owner-finalization-proof/v1','kind':kind,
            'observedAt':stamp(self.clock),'records':deepcopy(records),
            'rawEvidence':self.raw_original(records,'finalizer-records')}
        capture=self.operator.describe_finalizer_capture(bundle,self.head())
        self.capture_authority=self.root_capture(capture['captureBundle'],capture['context'])
        return bundle

    def capture_key_from_native(self, capture):
        """Accept the reusable real H-process/N/D/custody capture on this attempt."""
        p,m=self.p,self.m
        require(capture['sameExecution'] is self and capture['sameLedger'] is self.ledger
                and capture['attemptTrustBindingSHA256']==digest(p.binding),
                'same-real-native-attempt-controller-and-ledger')
        h=capture['holder']
        require(h.m is m and h.execution is self, 'same-real-native-model-identity')
        self.native_holder=h
        # The H ready descriptor is retained by the actual runner and included
        # in both real N descriptors. Keep those exact checked outputs.
        self.paired_receipt=capture['finish']['receipt']
        self.holder_release=capture['holderFinal']['result']
        self.native_capture=capture
        self.adapters=h.a
        with self.ledger.lock():
            self.holder_window=self.ledger.read_once(h.a.create_holder.LOCK_ONCE)
        # Complete real eight-row readback normalization after the actual
        # holder accepted the original CreateKey receipt.
        kn = h.kn
        create = capture['nativeCreate']
        key_arn = self.f.key_arn
        reads = []
        new = self.f.key['readback']
        slots = {'new.DescribeKey': ('DescribeKey', key_arn, {'KeyMetadata': m.strict_json(create['rawResponse'].encode())['KeyMetadata']}),
            'new.GetKeyPolicy': ('GetKeyPolicy', key_arn, {'Policy': json.dumps(new['policy']), 'PolicyName': 'default'}),
            'new.GetKeyRotationStatus': ('GetKeyRotationStatus', key_arn, {'KeyId': key_arn, 'KeyRotationEnabled': False}),
            'new.ListAliases': ('ListAliases', key_arn, {'Aliases': [], 'Truncated': False})}
        for index, key_id in enumerate(kn.OLD_IDS, 1):
            arn = f'arn:aws:kms:{m.REGION}:{m.ACCOUNT}:key/' + key_id
            slots['old' + str(index) + '.DescribeKey'] = ('DescribeKey', arn,
                {'KeyMetadata': m.scheduled_key_closure(p.prepared['proposal'])[arn]['metadata']})
        manifest = kn.manifest(m, p.prepared, h.bundle['normalizedIntent'], h.body, 'readback',
            kn.ms(p.now) - 1000, kn.ms(p.now) + 120000, key_arn, digest(create))
        for slot, (action, arn, response) in slots.items():
            raw = json.dumps(response, separators=(',', ':'))
            wire = kn.read_wires(action, arn)[0]
            reads.append({'schema': 'hikyo-158/next-key-native-response/v1',
                'manifestSHA256': kn.sha(kn.canonical(manifest)), 'manifest': manifest,
                'slot': slot, 'action': action, 'keyARN': arn, 'guardAtForward': None,
                'status': 200, 'responseEndpoint': manifest['endpoint'],
                'sentAtMS': kn.ms(p.now), 'receivedAtMS': kn.ms(p.now),
                'requestBodyBytes': len(wire), 'requestBodySHA256': raw_digest(wire),
                'rawResponse': raw, 'rawResponseBytes': len(raw.encode()),
                'rawResponseSHA256': raw_digest(raw.encode()), 'requestId': None,
                'requestIdSource': None, 'requestIdCaptureState': 'not-requested',
                **{flag: False for flag in kn.FLAGS}})
        self.normalized_key = kn.normalize(m, kn.PINS, p.prepared, h.bundle['normalizedIntent'],
            h.body, create, reads, explicit_now=p.now)
        return self.capture_key_from_normalized(capture['intent'], capture['ticket'], self.normalized_key)

    def abort(self, *, bound=False):
        """Actual AbortCleanup over this attempt's original durable controller."""
        fixture = self.p.archive.fixture_module('test_partial_setup_cleanup.py')
        m, p = self.m, self.p
        modules = {'model': m, 'executor': self.x, 'ledger': self.l,
            'runtime': p.modules['runtime'], 'finalizer': p.modules['finalizer'],
            'operator': self.load_core('next_operator_orchestration.source-only.py'),
            'stdout': p.modules['operator'],
            'metadata': self.load_core('partial_metadata_bridge.source-only.py'),
            'keyFacts': self.load_core('normalize_key_origin_cleanup_only.source-only.py')}
        cleanup_module = self.load_core('partial_setup_cleanup_inventory.source-only.py')
        fixture.m, fixture.x = m, self.x
        fixture.b, fixture.n = modules['runtime'], modules['finalizer']
        fixture.o, fixture.stdout = modules['operator'], modules['stdout']
        fixture.metadata = modules['metadata']
        fixture.nt.m = m
        fixture.nt.n = self.load_core('normalize_next_inventory.source-only.py')
        fixture.nt.s = self.load_core('normalize_setup_whole_reads.source-only.py')
        class Originals(modules['runtime'].RootCaptureAuthority):
            def __init__(self, snapshot):
                self.snapshot = deepcopy(snapshot)
            def release_root_reviewed_snapshot(self):
                return deepcopy(self.snapshot)
        fixture.SyntheticRoot = Originals
        builder = fixture.AbortTests()
        builder.e, builder.key = self, deepcopy(self.f.key)
        builder.folder = self.base / m.configuration_checked()['cleanupFolder']
        builder.folder.mkdir()
        builder.native = dict(modules['finalizer'].NATIVE_GUARD_PINS)
        for name in ('native_partial_kms_deletion_prepare.source-only.mjs',
                     'native_partial_kms_deletion_guard.source-only.js'):
            builder.native[name] = p.modules['implementation']['files']['core/native/' + name]
        builder.cleanup = cleanup_module.AbortCleanup(modules, self.ledger, p.prepared,
            self.sources, builder.native, builder.folder, self.explicit_clock, archive=fixture.ARCHIVE)
        builder.sizes = []
        def inventory_for(adapter, owner, present=()):
            wire = fixture.nt.InventoryBridgeTests()
            wire.f = self.f
            wire.bound = {'preparationSHA256': digest(self.f.prepared),
                          'run': {'keyARN': self.f.key_arn}, 'records': {'key': self.f.key}}
            wire.custody, wire.inv = self.f.sealing['custody'], self.f.sealing['inventory']
            wire.owner_sha = digest(owner)
            wire.ms = fixture.nt.n.epoch_ms(self.clock) - 1000
            raw = wire.bundle('iam', 'baseline')
            roles = next(row for row in raw['rows'] if row['slot'] == 'roles.ListRoles')
            providers = next(row for row in raw['rows'] if row['slot'] == 'provider.ListOpenIDConnectProviders')
            fixture.nt.reseal(roles, fixture.nt.envelope('ListRoles', {'Roles': [], 'IsTruncated': 'false'}))
            for row in (roles, providers):
                row['sentAtMS'] = row['receivedAtMS'] = fixture.nt.n.epoch_ms(self.clock)
            return modules['metadata'].inventory_from_lists(m,
                {'manifest': raw['manifest'], 'roles': roles, 'providers': providers},
                preparation_sha256=digest(p.prepared), owner_context_sha256=digest(owner),
                explicit_now=self.clock, not_before=self.f.now)
        builder.inventory_for = types.MethodType(inventory_for, builder)
        require(hasattr(self,'adapters'),'actual-configured-K-adapter-required')
        root=self.adapters.root_connection
        self.cleanup_adapter_calls=[]
        original_call=builder.call
        def through_root(adapter,method,kind,records,*args):
            if method in ('reconcile','arm','ticket','accept') and (not args or args==('ScheduleKeyDeletion',)):
                records=deepcopy(records)
                if method=='accept':records['attemptTrustBindingSHA256']=digest(p.binding)
                proof,authority=adapter.proof(kind,records)
                self.cleanup_adapter_calls.append(method)
                return getattr(root,method)(adapter.cleanup,proof,authority)
            return original_call(method,kind,records,*args)
        builder.call=types.MethodType(through_root,builder)
        before = {path.name: path.read_bytes() for path in self.folder.glob('*.once.json')}
        builder.activate()
        if bound:
            builder.stage_v6 = builder.stage_delete_v1 = builder.stage_boundary = False
            records = builder.stage_records()
        else:
            records = builder.records()
        # Real K successor checks exact original binding at every cleanup seam.
        builder.call('reconcile','abort-reconcile',records)
        builder.schedule(records)
        result = builder.call('complete', 'abort-complete', records)
        for name, raw in before.items():
            require((self.folder / name).read_bytes() == raw, 'abort-original-bytes-unchanged')
        self.abort_builder, self.abort_closure = builder, result
        return result
