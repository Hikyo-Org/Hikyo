"""Real configured-model outer paths; only native/provider authority is synthetic.

Qualification requires an explicit HIKYO_158_ARCHIVE_ROOT containing the
exact pinned originals. Missing originals are not replaced or redacted.
"""
import copy
from datetime import timedelta
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import signal
import subprocess
import sqlite3
import sys
import tempfile
import time
import unittest
from unittest.mock import patch

import historical_archive
import package_adapters

HERE = Path(__file__).resolve().parent
FIELD = 'attemptTrustBindingSHA256'


def load(name, relative):
    sources=package_adapters.verify_sources(hashlib.sha256((HERE/'adapters/SOURCE-PINS.json').read_bytes()).hexdigest())
    row=sources['files'][relative]
    raw=package_adapters.read_pinned(HERE/relative,row['rawSHA256'],row['bytes'])
    spec = importlib.util.spec_from_file_location(name, HERE / relative)
    module = importlib.util.module_from_spec(spec)
    sys.modules[name] = module
    exec(compile(raw,str(HERE/relative),'exec',dont_inherit=True),module.__dict__)
    return module


class Pipeline:
    """Use the parent's real verified loader/collector/configured pipeline fixture."""
    def __init__(self, temporary, version=1):
        from pipeline_support import Pipeline as CorePipeline
        archive_root = os.environ.get('HIKYO_158_ARCHIVE_ROOT')
        if not archive_root:
            raise ValueError('qualification-unavailable:HIKYO_158_ARCHIVE_ROOT-required')
        index_raw = (HERE / 'HISTORICAL-ARCHIVE-PINS.json').read_bytes()
        archive = historical_archive.PinnedArchive(Path(archive_root), index_raw,
            reviewed_index_sha256=hashlib.sha256(index_raw).hexdigest())
        self.attach(CorePipeline(version, archive=archive), temporary)

    def attach(self, p, root):
        self.core = p
        self.root = Path(root).resolve()
        self.m, self.old, self.now = p.m, p.f, p.now
        self.binding, self.manifest, self.admission = p.binding, p.manifest, p.admission
        self.prepared, self.modules = p.prepared, dict(p.modules)
        node_path = os.environ.get('HIKYO_158_NODE_PATH')
        if not node_path:
            raise ValueError('qualification-unavailable:HIKYO_158_NODE_PATH-required-pinned-Node')
        self.node_path = Path(node_path).resolve(strict=True)
        self.a = package_adapters.load_adapters(self.m, admission=self.admission, attempt_binding=self.binding,
            source_manifest=self.manifest, implementation_manifest=p.modules['implementation'],
            implementation_manifest_sha256=hashlib.sha256((HERE/'adapters/SOURCE-PINS.json').read_bytes()).hexdigest(),
            runtime_root=self.root, native_store=self.root/'SYNTHETIC-native.sqlite',
            node_path=self.node_path, transport_root=materialize_transport(self.root))
        if hashlib.sha256(self.node_path.read_bytes()).hexdigest() != self.a.runtime_binding.NODE_SHA:
            raise ValueError('qualification-unavailable:actual-Node-pin-mismatch')
        for key,leaf in {'guardDeadline':'future_key_guard_deadline.source-only.py',
                         'keyNormalizer':'normalize_next_key_custody.source-only.py'}.items():
            self.modules[key]=p.load_core(leaf)
        self.sha = self.m.digest(self.binding)

        return self

    def close(self):
        self.old.doCleanups()


class AdapterTests(unittest.TestCase):
    def pipeline(self, version=1):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        fixture = Pipeline(temporary.name, version)
        self.addCleanup(fixture.close)
        return fixture

    def test_two_releases_same_adapter_sources_and_no_rebinding(self):
        rows = []
        for version in (1,2):
            f = self.pipeline(version)
            a = f.a
            a.context.prepared(f.m, f.prepared)
            a.context.forward(f.m, f.now, f.prepared)
            rows.append(a.context.binding_sha256)
            wrong = dict(f.prepared, attemptTrustBindingSHA256='0'*64)
            with self.assertRaisesRegex(ValueError, 'same-attempt-trust-binding'):
                a.context.prepared(f.m, wrong)
            other = load('adapter_other_model_'+str(version), 'core/next_fixture_model.source-only.py')
            with self.assertRaisesRegex(ValueError, 'same-configured-successor-model'):
                a.context.checked(other)
        self.assertNotEqual(*rows)

    def test_binding_flows_through_actual_custody_commands(self):
        f = self.pipeline()
        binding = self.source_binding(f)
        command = f.a.create_custody.invocation(f.m,binding,kind='create',identity=self.identity(),
            request_id='44444444-4444-4444-8444-444444444444',sequence=25)
        self.assertEqual(command[FIELD],f.sha)
        self.assertEqual(command['request']['arguments'][FIELD],f.sha)
        f.a.create_custody.Custody(binding).command_context_checked(f.m,command)
        wrong = copy.deepcopy(command); wrong[FIELD]='0'*64
        with self.assertRaises(ValueError):
            f.a.create_custody.Custody(binding).command_context_checked(f.m,wrong)
        wrong = dict(binding, attemptTrustBindingSHA256='0'*64)
        with self.assertRaises(ValueError):
            f.a.create_custody.source_binding_checked(f.m,wrong)

    @staticmethod
    def identity():
        url='https://eu-central-1.console.aws.amazon.com/kms/home?region=eu-central-1#/kms/keys/create'
        return {'targetId':'SYNTHETIC-TARGET','targetType':'page','targetURL':url,
                'browserContextId':'SYNTHETIC-CONTEXT','mainFrameId':'SYNTHETIC-FRAME',
                'loaderId':'SYNTHETIC-LOADER','frameURL':url.split('#')[0],
                'frameURLRelation':'target-url-without-fragment'}

    @staticmethod
    def source_binding(f):
        root=f.a.context.transport_root
        leaves={'sourceContract':'SOURCE-CONTRACT.json','transportSource':'source-manifest.json',
                'reviewManifest':'review-manifest.json','clientSource':'client.cjs'}
        refs={key:{'path':str(root/leaf),'rawSHA256':hashlib.sha256((root/leaf).read_bytes()).hexdigest()}
              for key,leaf in leaves.items()}
        return {'schema':'hikyo-158/persistent-chrome-create-source-binding/v1',FIELD:f.sha,**refs,
            'node':{'path':str(f.node_path),'rawSHA256':hashlib.sha256(f.node_path.read_bytes()).hexdigest()},
            'session':'hikyo-158-owner-complete','protocolRevision':'r2-actual-snippet-exchange-capacity',
            'ownerId':'33333333-3333-4333-8333-333333333333','runtimeDirectory':str(f.root/'SYNTHETIC-owner'),
            'scripts':{'create':{'id':'aws-create-readout-v1','rawSHA256':hashlib.sha256((HERE/'adapters/aws-create-readout-v1.js').read_bytes()).hexdigest()}},
            'runtimeReadinessGranted':False}


def materialize_transport(root, leaf="pinned-transport"):
    from adapters.build_transport import build
    archive = os.environ.get('HIKYO_158_ARCHIVE_ROOT')
    if not archive:
        raise ValueError('qualification-unavailable:HIKYO_158_ARCHIVE_ROOT-required')
    source = Path(archive) / 'aws-recovery-20261020/banked-series-approval-20261022'
    output = root/leaf
    build(source,output)
    return output


def compiler_check(root, data=None):
    node = os.environ.get('HIKYO_158_NODE_PATH')
    if not node:
        raise ValueError('qualification-unavailable:HIKYO_158_NODE_PATH-required-pinned-Node')
    node = Path(node).resolve(strict=True)
    if hashlib.sha256(node.read_bytes()).hexdigest() != 'a9bd0630891c2dcdee70de88270fee2cc0c4a9e76495039dd3b4f91c5e6b71df':
        raise ValueError('qualification-unavailable:actual-Node-pin-mismatch')
    command=[str(node),str(HERE/'adapters/test_native_stage.cjs'),str(materialize_transport(root,"compiler-transport"))]
    if data is not None:
        target=root/'SYNTHETIC-native-stage.json';target.write_bytes(json.dumps(data).encode());command.append(str(target))
    result=subprocess.run(command,check=True,capture_output=True,timeout=20)
    return json.loads(result.stdout)


class SourceTests(unittest.TestCase):
    def test_pinned_transport_uses_real_Node_closed_registry_compiler(self):
        with tempfile.TemporaryDirectory() as temporary:
            result=compiler_check(Path(temporary).resolve())
        self.assertTrue(result['actualNodeCompilerExecuted'])
        self.assertFalse(result['persistentOwnerStarted']);self.assertFalse(result['providerCalled'])
        self.assertEqual(result['compiled'],14)

    def test_real_source_pins_and_drift_refusal(self):
        path = HERE/'adapters/SOURCE-PINS.json'
        if not path.exists():
            raise ValueError('qualification-unavailable:adapter-source-map-unsealed')
        pin = hashlib.sha256(path.read_bytes()).hexdigest()
        checked = package_adapters.verify_sources(pin)
        self.assertEqual(checked['stagePins']['modelSHA256'],hashlib.sha256((HERE/'core/next_fixture_model.source-only.py').read_bytes()).hexdigest())
        with self.assertRaises(ValueError):
            package_adapters.verify_sources('0'*64)

    def test_all_generated_python_parses_without_loading(self):
        import ast
        for path in (HERE/'adapters').rglob('*.py'):
            ast.parse(path.read_text())

def prepare_through_adapters(p, root):
    """Real A plan/prepare/publication becomes this pipeline's preparation.

    Only public/native originals and owner originality are synthetic. Builders
    execute from the parent's exact pinned archive, without changing validators.
    Call before constructing Execution, so every downstream consumer shares it.
    """
    from pipeline_support import PairOriginals
    root = Path(root).resolve()
    root.mkdir(parents=True, exist_ok=True)
    f = Pipeline.__new__(Pipeline).attach(p, root)
    m, a = f.m, f.a
    modules = {'model': m, 'operator': p.modules['operator'],
        'inventory': p.load_core('normalize_next_inventory.source-only.py'),
        'setup': p.load_core('normalize_setup_whole_reads.source-only.py')}
    bundle = a.prepare_root.make_bundle(modules,
        {k: v for k, v in p.f.inputs.items() if k != 'proposalMarkdown'}, p.f.inputs['proposalMarkdown'],
        configuration_raw=m.canonical(p.config), configuration_raw_sha256=m.digest(p.config),
        originals_verifier=PairOriginals(p))
    expected = {'schema': 'hikyo-158/root-reviewed-source-pair/v1',
        'candidateSHA': p.binding['candidateHeadSHA'], 'mainSHA': p.binding['mainSHA']}
    binding = dict(AdapterTests.source_binding(f), schema='hikyo-158/persistent-chrome-prepare-source-binding/v1')
    del binding['protocolRevision']
    registry = m.strict_json((HERE/'adapters/prepare-registry.json').read_bytes())
    binding['scripts'] = {kind: {'id': 'aws-'+kind+'-readout-v1',
        'rawSHA256': next(row['sha256'] for row in registry['scripts'] if row['id']=='aws-'+kind+'-readout-v1')}
        for kind in a.persistent_custody.KINDS}
    connection = sqlite3.connect(':memory:')
    connection.execute('CREATE TABLE orchestration_events(sequence INTEGER,stream_id TEXT,event_type TEXT,payload_json TEXT)')
    try:
        def ref(leaf, value, *, raw=None):
            raw = m.canonical(value) if raw is None else raw
            path = root / leaf
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_bytes(raw)
            return {'path': str(path), 'rawSHA256': hashlib.sha256(raw).hexdigest()}
        sequence = 0
        def capture(leaf, kind, value, at):
            nonlocal sequence
            sequence += 1
            identity = AdapterTests.identity()
            url = ('https://us-east-1.console.aws.amazon.com/iam/home?region=eu-central-1#/home'
                   if kind in ('owner', 'iam', 'setup') else
                   'https://eu-central-1.console.aws.amazon.com/secretsmanager/secret?region='+m.REGION+'&name='+m.HISTORICAL
                   if kind in ('identity', 'historical') else identity['targetURL'])
            identity.update(targetURL=url, frameURL=url.split('#')[0],
                            frameURLRelation='target-url-without-fragment' if '#' in url else 'same-url')
            args = {FIELD: f.sha, 'expected': identity}
            if kind in ('iam', 'kms', 'setup'):
                args['manifest'] = value['manifest']
                value = {'manifest': value['manifest'], 'readRows': value['rows']}
            command = a.persistent_custody.invocation(m, binding, kind=kind, identity=identity,
                request_id=f'22222222-2222-4222-8222-{sequence:012d}', sequence=sequence, script_arguments=args)
            if kind=='owner':
                value={'schema':'hikyo-158/public-iam-owner-observation/v1',
                    **{k:value[k] for k in ('pageURL','accountHeaderText','capturedAt')}, 'dashboardHeadingText':'IAM Dashboard'}
            if kind in ('iam','kms','setup','historical'):
                wire=m.canonical(value).decode()
                value={'schema':'hikyo-158/public-json-chunks/v1','chunks':[wire[i:i+2048] for i in range(0,len(wire),2048)]}
            readout={'schema':'hikyo-158/chrome-public-readout/v1','kind':kind,'before':identity,'after':identity,
                'readAtMS':modules['inventory'].epoch_ms(at),'value':value,
                'controlLeaseClaimed':False,'exclusiveControlVerifiedByAdapter':False}
            response={'schema':'hikyo-158/persistent-owner-response/v1',
                **{k:command['request'][k] for k in ('ownerId','sourceSHA256','reviewSHA256','requestId','sequence')},
                'accepted':True,'ok':True,'nextSequence':sequence+1,'state':'active','ownerStillAlive':True,
                'ownerClosed':False,'result':readout,'error':None}
            response['responseSHA256']=a.transport_receipt.digest(response)
            response.update(clientChannelClosed=True,cliCaptureSuppressed=True,coreEphemeralProtocolMetadataSuppressed=False,
                controlLeaseClaimed=False,exclusiveControlVerifiedByAdapter=False,ownerProcessExitVerified=False)
            original={'type':'command_execution','threadId':a.persistent_custody.THREAD,'status':'completed','exitCode':0,
                'input':command['command'],'output':m.canonical(response).decode(),'startedAt':m.stamp(at),
                'completedAt':m.stamp(at),'updatedAt':m.stamp(at),'nativeItemRef':{'nativeId':'SYNTHETIC-A-'+str(sequence)}}
            raw=m.canonical(original)
            connection.execute('INSERT INTO orchestration_events VALUES(?,?,?,?)',
                (100+sequence,a.persistent_custody.THREAD,'turn-item.updated',raw.decode()));connection.commit()
            return {'context':ref('inputs/'+leaf+'-context.json',command),
                    'original':ref('inputs/'+leaf+'-original.json',original,raw=raw)}
        def sidecar(leaf, phase, captures, **extra):
            return ref('inputs/'+leaf, {FIELD:f.sha,'schema':'hikyo-158/persistent-chrome-first-prepare-custody-index/v1',
                'phase':phase,'sourceBindingSHA256':m.digest(binding),'expectedPair':expected,'captures':captures,**extra})
        folder=root/('banked-first-admission-'+p.now.strftime('%Y%m%d')+'-r1')
        plan_at=p.now-timedelta(seconds=1)
        owner=copy.deepcopy(p.f.owner['ownerContext']['sourceReceipt']);owner['capturedAt']=m.stamp(plan_at)
        plan_index={'ownerSource':ref('inputs/owner.json',owner),
            'metadataReference':ref('inputs/metadata.json',{'schema':'hikyo-158/root-final-metadata-capture-plan/v1',
                'account':m.ACCOUNT,'region':m.REGION,'ownerContextSHA256':'1'*64,'recordedAt':m.stamp(plan_at),
                'purpose':'SYNTHETIC actual A qualification','maximumMilliseconds':300000,
                'fixtureReservationPermitted':False,'writesPermitted':False}),
            'historicalReference':ref('inputs/history.json',{'schema':'hikyo-158/persistent-chrome-historical-read-preparation/v1',
                'account':m.ACCOUNT,'region':m.REGION,'observerSourceSHA256':a.persistent_admission.PINS['historicalObserver'][1],
                'gateSourceSHA256':a.persistent_admission.PINS['historicalGate'][1],'publicSourcePins':a.persistent_admission.SM_PINS,
                'readIntent':a.persistent_admission._read_intent(m),'captureTimeoutMS':5000,
                'purpose':'SYNTHETIC actual A qualification','fixtureAdmissionGranted':False,'fixtureReservation':False,'resourceMutation':False})}
        plan_captures={'ownerSource':capture('owner','owner',owner,plan_at), 'identity':capture('identity','identity',None,plan_at)}
        owner_derivation={'schema':'hikyo-158/root-reviewed-owner-projection-derivation/v1',
            'publicOriginalRawSHA256':plan_captures['ownerSource']['original']['rawSHA256'],'ownerProjectionSHA256':m.digest(owner),
            'genuineHumanOwnerOriginalRawSHA256':hashlib.sha256(b'SYNTHETIC external human owner original').hexdigest(),
            'principalARNOrigin':'Root-external-derivation-from-genuine-human-owner-verification'}
        plan_custody=sidecar('plan-custody.json','plan',plan_captures,ownerDerivation=owner_derivation)
        plan=a.chrome_first_prepare.run_plan(a.prepare_root,bundle,plan_index,m.canonical(plan_index),
            binding=binding,expected_pair=expected,custody_index_ref=plan_custody,folder=folder,
            connection=connection,sequence_min=0,sequence_max=50000,clock=lambda:p.now)
        fixture=p.archive.fixture_module('test_normalize_next_inventory.py')
        fixture.m,fixture.n,fixture.s=m,modules['inventory'],modules['setup']
        builder=fixture.InventoryBridgeTests();builder.f,builder.now=p.f,p.now
        builder.ms=modules['inventory'].epoch_ms(plan_at);builder.owner_sha=m.digest(plan['ownerContext'])
        builder.bound={'preparationSHA256':m.digest(plan['capturePlan']),'run':{'keyARN':p.f.key_arn},'records':{'key':p.f.key}}
        builder.inv,builder.custody=p.f.sealing['inventory'],p.f.sealing['custody'];builder.prepare_baseline()
        reads={}
        for key,manifest_key in (('iamBundle','iamManifest'),('kmsBundle','kmsManifest'),('setupBundle','setupManifest')):
            value=copy.deepcopy(getattr(builder,{'iamBundle':'iam','kmsBundle':'kms','setupBundle':'setup'}[key]))
            value['manifest']=copy.deepcopy(plan[manifest_key])
            for row in value['rows']:
                for field,given in value['manifest'].items():
                    if field in row and field!='schema':row[field]=given
                row['manifestSHA256']=m.digest(value['manifest'])
            reads[key]=value
        receipt=copy.deepcopy(p.f.historical['sourceReceipt'])
        ms=modules['inventory'].epoch_ms(p.now)
        receipt.update(sentAtMS=ms,receivedAtMS=ms,storedAtMS=ms,reviewedPreparationSHA256=plan['historicalPreparationSHA256'])
        recovery={'schema':'hikyo-158/exact-own-historical-metadata-reconciliation/v1','observedAt':m.stamp(p.now),
            'storageKey':plan['historicalStorageKey'],'rawReceipt':m.canonical(receipt).decode(),'storageReset':False,'providerRequest':False}
        captures={'historicalRecovery':capture('history','historical',receipt,p.now),
                  **{key:capture(key,{'iamBundle':'iam','kmsBundle':'kms','setupBundle':'setup'}[key],value,p.now)
                     for key,value in reads.items()}}
        def plan_ref(leaf):
            path=folder/leaf
            return {'path':str(path),'rawSHA256':hashlib.sha256(path.read_bytes()).hexdigest()}
        custody=sidecar('prepare-custody.json','prepare',captures,planCustodyIndex=plan_ref('plan-custody-index.original.json'))
        env=p.f.environment
        responses={'environment':{'id':m.ENV_ID,**env['configuration']},'variables':{'variables':[],'total_count':0},
            'branches':{'branch_policies':[{'id':1,'node_id':'SYNTHETIC','name':'main','type':'branch'}],'total_count':1},
            'oidcDefaults':env['OIDCDefaults']}
        environment={key:ref('inputs/environment-'+key+'.json',{'endpoint':a.persistent_admission.ENV_ENDPOINTS[key],
            'method':'GET','startedAt':m.stamp(plan_at),'finishedAt':m.stamp(p.now),'exitCode':0,
            'stdout':m.canonical(value).decode(),'stderr':''}) for key,value in responses.items()}
        index={'proposal':{'derive':'pinned-Root-inputs+whole-GitHub-sourceManifest'},
            'code':{'derive':'whole-GitHub-collection.code'},'ownerContext':plan_ref('owner-context.actual.json'),
            'capturePlan':plan_ref('metadata-capture-plan.actual.json'),'historicalPreparation':plan_ref('historical-read-preparation.actual.json'),
            'historicalRecovery':ref('inputs/history-recovery.json',recovery),'environmentCaptures':environment,
            'budget':ref('inputs/budget.json',p.f.budget),**{key:ref('inputs/'+key+'.json',value) for key,value in reads.items()}}
        key_ref=ref('inputs/key-request.json',p.f.key['intent']['request'])
        key_review=ref('inputs/key-review.json',{'schema':'hikyo-158/root-reviewed-create-key-request/v1',
            'proposalRawSHA256':a.prepare_root.PROPOSAL_SHA,'configurationRawSHA256':a.prepare_root.CONFIG_SHA,
            'requestRawSHA256':key_ref['rawSHA256']})
        # Whole collector originals have their own unchanged 24 MB bound;
        # only registered compact code enters the model's 2 MB packets.
        raw_collection=json.dumps(p.pair_collection,sort_keys=True,separators=(',',':'),
                                  ensure_ascii=True,allow_nan=False).encode()
        collection_ref=ref('inputs/whole-collection.actual.json',None,raw=raw_collection)
        wire=a.chrome_first_prepare.run_prepare(a.prepare_root,bundle,index,m.canonical(index),binding=binding,
            expected_pair=expected,custody_index_ref=custody,folder=folder,collection_ref=collection_ref,
            key_ref=key_ref,key_review_ref=key_review,connection=connection,sequence_min=0,sequence_max=50000,
            lease_start=p.f.kwargs['lease_start'],lease_until=p.f.kwargs['lease_until'],namespace=p.resource['namespace'],clock=lambda:p.now)
        prepared=m.strict_json((folder/'prepared.actual.json').read_bytes())
        records=modules['operator'].decode_tool_wire(m,wire,'phase-records')
        m.require(m.same(records,prepared['records']) and Path(collection_ref['path']).read_bytes()==raw_collection,
                  'actual-A-published-records-and-unchanged-whole-collector-original')
        p.prepared=p.f.prepared=prepared;p.f.proposal=prepared['proposal'];p.f.records=records
        p.f.owner=records['owner'];p.f.historical=records['historicalAbsence'];p.code=p.f.code=records['code']
        p.adapter_preparation={'prepared':prepared,'plan':plan,'folder':folder,'wire':wire,
            'wholeCollectionRaw':raw_collection,'sameModel':m,FIELD:f.sha}
        return p.adapter_preparation
    finally:
        connection.close()


class HolderFixture:
    """Actual H holder/runner/N preflight inputs, synthetic public/native bytes."""
    def __init__(self, f, *, execution=None):
        self.f, self.m, self.a = f, f.m, f.a
        m, a = self.m, self.a
        package_adapters.verify_native_dependencies(a, f.modules)
        self.kn, self.deadline = f.modules['keyNormalizer'], f.modules['guardDeadline']
        self.binding = AdapterTests.source_binding(f)
        a.runtime_binding.runtime_binding_checked(m,self.binding)
        self.command = a.create_custody.invocation(m,self.binding,kind='create',identity=AdapterTests.identity(),
            request_id='44444444-4444-4444-8444-444444444444',sequence=25)
        if execution is None:
            from pipeline_support import Execution
            execution = Execution(f.core, base=f.root)
        self.execution = e = execution
        self.ledger = e.ledger
        self.clock = e.explicit_clock
        if not (self.ledger.folder / 'claim.once.json').exists():
            e.reserve()
        self.intent = e.controller.arm_owner('CreateKey', copy.deepcopy(f.old.key['intent']['request']),
            e.bundle('owner-CreateKey', f.old.records))
        self.ticket = e.controller.owner_forward_ticket('CreateKey', e.bundle('owner-forward-CreateKey', f.old.records))
        e.full_key_intent, e.key_ticket = self.intent, self.ticket
        sources = e.sources
        with self.ledger.lock():
            claim = self.ledger.claim_record()
            self.head, _ = self.ledger.snapshot()
        self.contract = {'operation':'CreateKey',FIELD:f.sha,'endpoint':f'https://kms.{m.REGION}.amazonaws.com/',
            'request':copy.deepcopy(self.intent['request']),'intentSHA256':m.digest(self.intent),'ticket':self.ticket,
            'maximumForwards':1,'automaticRetry':False,'runtimeReady':False,'executionAuthorityGranted':False,
            'requiredExternalSteps':copy.deepcopy(self.deadline.STEPS)}
        wire=copy.deepcopy(self.intent['request']);wire['Policy']=json.dumps(wire['Policy'],indent=4)
        self.body=json.dumps(wire,separators=(',',':'))
        preclaim=self.deadline.preclaim_window(m,f.prepared,explicit_now=f.now,minimum_remaining_ms=120000)
        self.bundle=self.deadline.create_manifests(m,self.kn,self.deadline.PINS,f.prepared,self.intent,self.contract,
            self.body,preclaim,explicit_now=f.now,minimum_remaining_ms=60000,redirect_limitation_reviewed=True)
        self.guard={'schema':'hikyo-158/native-kms-xhr-write-status/v1','phase':'observing','claimConsumed':False,
            'nativeSendInvocations':0,'blocked':0,'activeReads':0,'successfulReadProbes':1,'lastReadTarget':'TrentService.ListKeys',
            'responseStatus':None,'reason':None,'responseEndpointMatched':None,
            'intentSHA256':self.bundle['guardManifest']['intentSHA256'],
            'wireBodySHA256':self.bundle['guardManifest']['wireBodySHA256'],'endpoint':self.bundle['guardManifest']['endpoint'],
            'durabilityVerifiedByPage':False,'ownerIdentityVerifiedByPage':False,'nativeSendCounterScope':'CreateKey-only-excludes-metadata-reads',
            'loadedSourceBytesVerifiedByPage':False,'redirectErrorSupported':False,'physicalHTTPHopBoundEstablished':False,
            'coversOtherRealmsOrCapturedMethods':False,'cleanup':'close-controlled-tab'}
        self.observer={'schema':'hikyo-158/next-key-observer-status/v1','mode':'create','readySlots':[],
            'pendingCaptures':0,'error':None,'providerAPIsCalledByObserver':False,'ownerAuthenticatedByObserver':False,
            'publicSourceAuthenticatedByObserver':False,'writeAuthorityGrantedByObserver':False,
            'coversOtherRealmsOrCapturedMethods':False,'physicalHTTPHopBoundEstablished':False,
            'cleanup':'full-navigation-controlled-tab-preserves-session'}
        self.context={'schema':'hikyo-158/persistent-chrome-create-lock-context/v1',FIELD:f.sha,
            'threadId':a.create_custody.THREAD,'chromeCommand':self.command,'preparationSHA256':m.digest(f.prepared),
            'configurationSHA256':m.digest(m.release_configuration_checked()),'claimSHA256':m.digest(claim),
            'stateSHA256':self.head,'intentSHA256':m.digest(self.intent),'ticketSHA256':m.digest(self.ticket),
            'contractSHA256':m.digest(self.contract),'manifestBundleSHA256':m.digest(self.bundle),
            'controllerSourcesSHA256':m.digest(sources),'nativeSources':copy.deepcopy(self.deadline.PINS)}
        self.controller=a.runner.state_only_controller(f.modules['executor'],m,self.ledger,f.prepared,sources,self.clock)
        self.values={'prepared':f.prepared,'fullIntent':self.intent,'forwardContract':self.contract,
            'nativeManifestBundle':self.bundle,'guardStatus':self.guard,'observerStatus':self.observer,
            'lockContext':self.context,'sourceBinding':self.binding}
        self.review=a.runner.review_context(m,self.values,
            {'path':str(f.root/'SYNTHETIC-index.json'),'rawSHA256':hashlib.sha256(b'SYNTHETIC INDEX').hexdigest()},
            runtime_dir=f.root/'SYNTHETIC-holder',sequence_min=0,sequence_max=50000,
            source_manifest_sha=hashlib.sha256((HERE/'adapters/SOURCE-PINS.json').read_bytes()).hexdigest(),
            python_ref={'path':str(Path(sys.executable).resolve()),'rawSHA256':hashlib.sha256(Path(sys.executable).resolve().read_bytes()).hexdigest()})
        self.review_sha=m.digest(self.review)
        self.connection=sqlite3.connect(str(a.context.native_store),check_same_thread=False)
        self.connection.execute('CREATE TABLE orchestration_events(sequence INTEGER, stream_id TEXT, event_type TEXT, payload_json TEXT)')

    def original(self,command,value,*,stage=False,insert=True):
        m,a,f=self.m,self.a,self.f
        before=command['request']['arguments']['expected']
        if not stage:
            wire=json.dumps(value,ensure_ascii=False,separators=(',',':'))
            value={'schema':'hikyo-158/public-json-chunks/v1','chunks':[wire[i:i+1024] for i in range(0,len(wire),1024)]}
        readout={'schema':'hikyo-158/native-create-stage-readout/v1' if stage else 'hikyo-158/chrome-public-readout/v1',
            'kind':command['kind'],'before':before,'after':before,'readAtMS':self.kn.ms(f.now),'value':value,
            'controlLeaseClaimed':False,'exclusiveControlVerifiedByAdapter':False}
        if stage:
            readout.update(phase=command['request']['arguments']['phase'],writeAuthorityGranted=False,providerOutcomeProven=False)
        response={'schema':'hikyo-158/persistent-owner-response/v1',
            **{key:command['request'][key] for key in ('ownerId','sourceSHA256','reviewSHA256','requestId','sequence')},
            'accepted':True,'ok':True,'nextSequence':command['request']['sequence']+1,'state':'active',
            'ownerStillAlive':True,'ownerClosed':False,'result':readout,'error':None}
        response['responseSHA256']=a.create_custody.transport_receipt.digest(response)
        response.update(clientChannelClosed=True,cliCaptureSuppressed=True,coreEphemeralProtocolMetadataSuppressed=False,
            controlLeaseClaimed=False,exclusiveControlVerifiedByAdapter=False,ownerProcessExitVerified=False)
        original={'type':'command_execution','status':'completed','exitCode':0,'threadId':a.create_custody.THREAD,
            'input':command['command'],'output':m.canonical(response).decode(),'startedAt':m.stamp(f.now),
            'completedAt':m.stamp(f.now),'updatedAt':m.stamp(f.now),'nativeItemRef':{'nativeId':'SYNTHETIC-original-'+str(command['request']['sequence'])}}
        raw=m.canonical(original)
        if insert:
            self.connection.execute('INSERT INTO orchestration_events VALUES(?,?,?,?)',
                (1000+command['request']['sequence'],a.create_custody.THREAD,'turn-item.updated',raw.decode()))
            self.connection.commit()
        return raw

    def create_value(self):
        m,f,kn=self.m,self.f,self.kn
        meta=copy.deepcopy(f.old.key['createResponse']['KeyMetadata']);meta['CreationDate']=f.now.timestamp()
        raw=json.dumps({'KeyMetadata':meta},separators=(',',':'));given=self.bundle['observerManifest']
        row={'schema':'hikyo-158/next-key-native-response/v1','manifestSHA256':kn.sha(kn.canonical(given)),
            'manifest':copy.deepcopy(given),'slot':'create','action':'CreateKey','keyARN':None,
            'guardAtForward':{'schema':'hikyo-158/native-kms-xhr-write-status/v1','phase':'forwarded','claimConsumed':True,
                'nativeSendInvocations':1,'intentSHA256':given['intentSHA256'],'wireBodySHA256':given['wireBodySHA256'],'endpoint':given['endpoint']},
            'status':200,'responseEndpoint':given['endpoint'],'sentAtMS':kn.ms(f.now),'receivedAtMS':kn.ms(f.now),
            'requestBodyBytes':len(self.body.encode()),'requestBodySHA256':kn.sha(self.body.encode()),
            'rawResponse':raw,'rawResponseBytes':len(raw.encode()),'rawResponseSHA256':kn.sha(raw.encode()),
            'requestId':'SYNTHETIC-REQUEST-ID-0001','requestIdSource':'native-response-header:x-amzn-requestid',
            'requestIdCaptureState':'captured',**{flag:False for flag in kn.FLAGS}}
        guard=dict(self.guard,phase='settled',claimConsumed=True,nativeSendInvocations=1,responseStatus=200,responseEndpointMatched=True)
        return {'create':row,'guard':guard}

    def kwargs(self,window,phase='arm'):
        m,a,f=self.m,self.a,self.f
        arguments={FIELD:f.sha,'expected':self.command['identity'],
            'window':{key:self.bundle['observerManifest'][key] for key in ('notBeforeMS','expiresAtMS')},
            'phase':phase,'manifest':self.bundle['observerManifest']}
        registry=m.strict_json((HERE/'adapters/stage-registry.json').read_bytes())
        command=a.stage_custody.command(m,self.binding,registry,kind='status',arguments=arguments,
            request_id='88888888-8888-4888-8888-'+('000000000021' if phase=='arm' else '000000000023'),sequence=21 if phase=='arm' else 23)
        raw=self.original(command,{'guard':dict(self.guard,phase='observing' if phase=='arm' else 'armed'),'observer':self.observer},stage=True)
        records=a.root_preflight.readonly_records(m,f.modules['ledger'],self.ledger.folder,self.ledger.parent,f.root/'SYNTHETIC-no-runtime')
        return dict(m=m,kn=self.kn,deadline=self.deadline,values=self.values,review=self.review,
            index_ref=self.review['inputIndex'],ready_raw=m.canonical(window)+b'\n',**records,
            stage_registry=registry,status_context=command,status_raw=raw,connection=self.connection,
            source_pins=a.context.stage_pins(),phase=phase,explicit_now=f.now,final_clock=self.clock,monotonic_now=time.monotonic)


def paired_action(h, window, phase, *, admission_clock_jump=False):
    """Actual N preflight/D driver/custody; only the external child is synthetic."""
    m,a,f=h.m,h.a,h.f
    kwargs=h.kwargs(window,phase)
    shared=kwargs['status_context']['request']['arguments']['window']
    sequence=kwargs['status_context']['request']['sequence']+1
    plan={'schema':'hikyo-158/native-paired-driver-plan/v1',FIELD:f.sha,'phase':phase,
        'nativeStageSourceSHA256':a.context.source_sha('adapters/preflight.py'),'holderContextSHA256':m.digest(h.review),
        'python':h.review['python'],'inputs':{key:{'path':str(f.root/('SYNTHETIC-'+phase+'-'+key)),
            'rawSHA256':hashlib.sha256((phase+key).encode()).hexdigest()} for key in a.driver_data.REFS},
        'action':{FIELD:f.sha,'requestId':'99999999-9999-4999-8999-'+f'{sequence:012d}','sequence':sequence,
            'ownerId':h.binding['ownerId'],'sourceSHA256':h.binding['transportSource']['rawSHA256'],
            'reviewSHA256':h.binding['reviewManifest']['rawSHA256'],'holderSourceBindingSHA256':m.digest(h.binding),
            'expectedIdentity':h.command['identity'],'sharedWindow':shared}}
    plan['inputs']['statusOriginal']['rawSHA256']=hashlib.sha256(kwargs['status_raw']).hexdigest()
    plan['inputs']['ready']['rawSHA256']=hashlib.sha256(kwargs['ready_raw']).hexdigest()
    plan['inputs']['holderIndex']=copy.deepcopy(h.review['inputIndex'])
    transport_calls=0
    def transport(argv,*,stop,before_spawn,env):
        nonlocal transport_calls
        transport_calls+=1
        before_spawn()
        request=m.strict_json(argv[-1].encode())
        command=a.stage_custody.command(m,h.binding,kwargs['stage_registry'],kind=phase,arguments=request['arguments'],
            request_id=request['requestId'],sequence=request['sequence'])
        value=({'guard':dict(h.guard,phase='armed'),'armInvokedOnce':True,'providerOutcomeProven':False}
               if phase=='arm' else {'clickDispatched':True,'providerOutcomeProven':False})
        raw=h.original(command,value,stage=True,insert=False)
        return {'stdout':m.strict_json(raw)['output'].encode()+b'\n','exitCode':0,'closed':True}
    source_pin=a.context.source_sha('adapters/paired_core.py')
    plan_ref={'path':str(f.root/('SYNTHETIC-'+phase+'-plan')),'rawSHA256':hashlib.sha256(m.canonical(plan)).hexdigest()}
    runtime=a.n_runtime.prepare(model=m,modules=f.modules,controller=h.controller,values=h.values,review=h.review,
        plan=plan,registry=kwargs['stage_registry'],status_context=kwargs['status_context'],status_raw=kwargs['status_raw'],
        ready_raw=kwargs['ready_raw'],index_ref=h.review['inputIndex'],clock=h.clock,monotonic=time.monotonic)
    def pre_submit():
        if not admission_clock_jump:
            return runtime['pre_submit']()
        original_observe=h.execution.p.authority.observe
        def observe(*args, **kw):
            result=original_observe(*args, **kw)
            jump={'descriptor':2001,'physical':55001}.get(admission_clock_jump)
            h.execution.clock=(f.now+timedelta(milliseconds=jump) if jump is not None
                               else m.instant(h.kn.stamp(shared['expiresAtMS']+1)))
            return result
        with patch.object(h.execution.p.authority,'observe',observe):
            return runtime['pre_submit']()
    raw,code=a.paired_core.Attempt().run(n=a.root_preflight,stage=a.stage_custody,m=m,plan=plan,plan_ref=plan_ref,
        source_pin=source_pin,values=h.values,registry=kwargs['stage_registry'],status_context=kwargs['status_context'],
        preflight=runtime['preflight'],pre_submit=pre_submit,
        verify=lambda:a.source_gate.verify(source_pin),transport=transport,wall_ms=lambda:h.kn.ms(h.clock()),
        monotonic=time.monotonic,physical_stop=window['monotonicDeadline'],client_outer_ms=5000)
    receipt=a.driver_data.strict(raw)
    if admission_clock_jump:
        m.require(code==2 and receipt['outcome']=='refused-before-launch' and receipt['launchAttempted'] is False,
                  'real-admission-clock-crossing-refuses-child-launch')
        m.require(transport_calls==0,'clock-crossing-never-invoked-child-transport')
        return {'receipt':receipt,'clock':h.clock(),'transportCalls':transport_calls}
    m.require(code==0 and receipt['outcome']=='completed-public-action','actual-paired-driver-completed-'+phase)
    carrier=a.driver_data.carrier(plan,plan_ref,source_pin)
    original={'type':'command_execution','status':'completed','exitCode':0,'threadId':a.create_custody.THREAD,
        'input':carrier['command'],'output':raw.decode(),'startedAt':m.stamp(f.now),'completedAt':m.stamp(f.now),
        'updatedAt':m.stamp(f.now),'nativeItemRef':{'nativeId':'SYNTHETIC-parent-driver-'+phase}}
    original_raw=m.canonical(original)
    h.connection.execute('INSERT INTO orchestration_events VALUES(?,?,?,?)',
        (1000+sequence,a.create_custody.THREAD,'turn-item.updated',original_raw.decode()))
    h.connection.commit()
    custody=a.paired_custody.verify_parent_original(m=m,n=a.root_preflight,stage=a.stage_custody,
        registry=kwargs['stage_registry'],binding=h.binding,plan=plan,plan_ref=plan_ref,source_pin=source_pin,
        raw=original_raw,connection=h.connection,sequence_min=0,sequence_max=50000,explicit_now=f.now)
    return {'receipt':receipt,'custody':custody,'originalRaw':original_raw}


def execute_native_create(execution, *, admission_clock_jump=False):
    """Same Execution claim through real H process and D arm/finish, no re-arm.

    Returned native/provider bytes are synthetic external originals authenticated
    by the actual custody/parser paths. No persistent owner or provider runs.
    """
    wrapper=Pipeline.__new__(Pipeline).attach(execution.p,execution.base)
    h=HolderFixture(wrapper,execution=execution)
    execution.native_connection=h.connection
    m,a=h.m,h.a
    pid=os.fork()
    if pid==0:
        connection=sqlite3.connect('file:'+str(a.context.native_store)+'?mode=ro',uri=True)
        connection.execute('PRAGMA query_only=ON')
        try:
            a.runner.start(m=m,controller=h.controller,deadline=h.deadline,key_normalizer=h.kn,values=h.values,
                review=h.review,expected_context_sha=h.review_sha,connection=connection,clock=h.clock)
        except BaseException:
            os._exit(2)
        finally:
            connection.close()
        os._exit(0)
    reaped=False
    try:
        directory=Path(h.review['runtimeDirectory']);end=time.monotonic()+5
        while not (directory/'descriptor.json').exists():
            m.require(time.monotonic()<end,'finite-real-holder-setup');time.sleep(.01)
        ready=a.runner.ready(m,h.review,h.review_sha)
        window=m.strict_json(ready[:-1])
        a.root_preflight.physical_exclusion(h.ledger.parent)
        arm=paired_action(h,window,'arm',admission_clock_jump=admission_clock_jump)
        if admission_clock_jump:
            return {'refused':arm,'holderWindow':window,'holder':h,'sameExecution':execution}
        finish=paired_action(h,window,'finish')
        value=h.create_value()
        original=h.original(h.command,value)
        a.local_files.exclusive(directory/a.runner.ORIGINAL,original)
        submitted=a.runner.release(m=m,key_normalizer=h.kn,values=h.values,review=h.review,
            expected_context_sha=h.review_sha,original_raw_sha=hashlib.sha256(original).hexdigest(),
            connection=h.connection,wall_clock=h.clock)
        end=time.monotonic()+5
        while True:
            actual,status=os.waitpid(pid,os.WNOHANG)
            if actual:
                reaped=True;break
            m.require(time.monotonic()<end,'finite-real-holder-process-exit');time.sleep(.01)
        m.require(os.waitstatus_to_exitcode(status)==0,'successful-real-holder-process')
        final=m.strict_json((directory/'final.json').read_bytes())
        m.require(final['outcome']=='validated-release' and final[FIELD]==wrapper.sha
                  and final['result'][FIELD]==wrapper.sha and submitted[FIELD]==wrapper.sha,
                  'same-complete-holder-binding-and-native-settlement')
        execution.native_connection=h.connection
        return {'holder':h,'intent':h.intent,'ticket':h.ticket,'bundle':h.bundle,'approvedBody':h.body,
            'nativeCreate':value['create'],'nativeGuard':value['guard'],'nativeOriginalRaw':original,
            'arm':arm,'finish':finish,'holderFinal':final,'submitted':submitted,
            FIELD:wrapper.sha,'sameExecution':execution,'sameLedger':h.ledger}
    finally:
        if not reaped:
            try:
                if os.waitpid(pid,os.WNOHANG)[0]==0:
                    os.kill(pid,signal.SIGTERM);os.waitpid(pid,0)
            except ChildProcessError:
                pass


class RealPathTests(unittest.TestCase):
    pipeline = AdapterTests.pipeline
    def holder(self):
        fixture=HolderFixture(self.pipeline())
        self.addCleanup(fixture.connection.close)
        return fixture

    def test_actual_holder_process_ready_release_preserves_lock_and_binding(self):
        h=self.holder();m,a=h.m,h.a
        raw=h.original(h.command,h.create_value())
        pid=os.fork()
        if pid==0:
            connection=sqlite3.connect('file:'+str(a.context.native_store)+'?mode=ro',uri=True)
            connection.execute('PRAGMA query_only=ON')
            try:
                a.runner.start(m=m,controller=h.controller,deadline=h.deadline,key_normalizer=h.kn,values=h.values,
                    review=h.review,expected_context_sha=h.review_sha,connection=connection,clock=h.clock)
            except BaseException:
                os._exit(2)
            finally:
                connection.close()
            os._exit(0)
        def stop():
            try:
                if os.waitpid(pid,os.WNOHANG)[0]==0:
                    os.kill(pid,signal.SIGTERM);os.waitpid(pid,0)
            except ChildProcessError:
                pass
        self.addCleanup(stop)
        directory=Path(h.review['runtimeDirectory']);end=time.monotonic()+5
        while not (directory/'descriptor.json').exists():
            self.assertLess(time.monotonic(),end,'finite owned holder setup')
            time.sleep(.01)
        ready=a.runner.ready(m,h.review,h.review_sha)
        window=m.strict_json(ready[:-1]);self.assertEqual(window[FIELD],h.f.sha)
        a.local_files.exclusive(directory/a.runner.ORIGINAL,raw)
        result=a.runner.release(m=m,key_normalizer=h.kn,values=h.values,review=h.review,expected_context_sha=h.review_sha,
            original_raw_sha=hashlib.sha256(raw).hexdigest(),connection=h.connection,wall_clock=h.clock)
        self.assertEqual(result[FIELD],h.f.sha)
        end=time.monotonic()+5
        while True:
            actual,status=os.waitpid(pid,os.WNOHANG)
            if actual:break
            self.assertLess(time.monotonic(),end,'finite owned holder exit');time.sleep(.01)
        self.assertEqual(os.waitstatus_to_exitcode(status),0)
        final=m.strict_json((directory/'final.json').read_bytes())
        self.assertEqual(final['outcome'],'validated-release');self.assertEqual(final['result'][FIELD],h.f.sha)
        self.assertTrue((h.ledger.folder/a.create_holder.LOCK_ONCE).exists())
        with self.assertRaises(ValueError):a.runner.ready(m,h.review,h.review_sha)

    def test_actual_native_preflight_closed_descriptor_and_paired_driver(self):
        from pipeline_support import Execution
        f=self.pipeline();e=Execution(f.core);self.addCleanup(e.close)
        capture=execute_native_create(e);h=capture['holder'];m=h.m
        descriptor=capture['arm']['receipt']['preflightDescriptor']
        self.assertEqual(descriptor[FIELD],f.sha)
        self.assertEqual(descriptor['maximumActionAgeMS'],2000)
        self.assertEqual(descriptor['requiredRemainingMS'],60000)
        self.assertEqual(capture['finish']['receipt']['outcome'],'completed-public-action')
        self.assertIs(capture['sameExecution'],e);self.assertIs(capture['sameLedger'],e.ledger)
        self.assertEqual(capture['holderFinal']['outcome'],'validated-release')
        result=compiler_check(e.base,{'fixture':{FIELD:f.sha,'syntheticOnly':True,'now':h.kn.ms(f.now),
            'expected':h.command['identity'],'window':{k:h.bundle['observerManifest'][k] for k in ('notBeforeMS','expiresAtMS')},
            'manifest':h.bundle['observerManifest'],'guardManifest':h.bundle['guardManifest'],
            'approvedBodyChunks':[h.body[i:i+1024] for i in range(0,len(h.body),1024)],
            'rawProviderResponse':h.create_value()['create']['rawResponse']},'arm':descriptor})
        self.assertEqual(result['nativeCases'],3);self.assertEqual(result['compiled'],14)

    def test_real_n_runtime_post_admission_clock_crossing_refuses_child_launch(self):
        from pipeline_support import Execution
        for crossing in ('descriptor','physical','shared'):
            with self.subTest(window=crossing):
                f=self.pipeline();e=Execution(f.core);self.addCleanup(e.close)
                result=execute_native_create(e,admission_clock_jump=crossing)
                receipt=result['refused']['receipt'];h=result['holder']
                self.assertEqual(receipt['outcome'],'refused-before-launch')
                self.assertFalse(receipt['launchAttempted'])
                self.assertEqual(result['refused']['transportCalls'],0)
                self.assertIsNone(receipt['child']['exitCode'])
                self.assertIsNone(receipt['child']['stdoutRawSHA256'])
                self.assertTrue((e.ledger.folder/h.a.create_holder.LOCK_ONCE).exists())
                self.assertTrue((e.ledger.folder/'owner-CreateKey-transmit.once.json').exists())
                self.assertFalse((e.ledger.folder/'owner-CreateKey-response.once.json').exists())
                self.assertGreater(h.kn.ms(e.clock),receipt['preflightDescriptor']['effectBeforeMS'])


class PrepareTests(unittest.TestCase):
    pipeline=AdapterTests.pipeline

    def test_actual_A_plan_prepare_publication_for_both_releases(self):
        for version in (1,2):
            with self.subTest(release=version):
                f=self.pipeline(version)
                p=f.core
                original=p.m.canonical(p.pair_collection)
                result=prepare_through_adapters(p,f.root/'actual-A')
                self.assertIs(result['sameModel'],p.m)
                self.assertEqual(result['prepared'],p.prepared)
                self.assertEqual(p.prepared,p.f.prepared)
                self.assertEqual(p.prepared['records'],p.f.records)
                self.assertEqual(p.m.canonical(p.pair_collection),original)
                self.assertEqual(result['wholeCollectionRaw'],original)
                self.assertEqual(p.f.records['code']['schema'],'hikyo-158/registered-pair-code/v1')
                self.assertEqual(p.f.historical,p.f.records['historicalAbsence'])
                self.assertEqual(p.f.owner,p.f.records['owner'])
                self.assertEqual((result['folder']/'prepared.actual.json').read_bytes(),p.m.canonical(p.prepared))


if __name__ == '__main__':
    unittest.main()
