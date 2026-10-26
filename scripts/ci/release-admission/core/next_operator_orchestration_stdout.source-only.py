"""SOURCE ONLY. Narrow wiring of frozen controllers to Root capture releases.

No authentication implementation, default engine, provider/API request, clock,
backend launch or historical controller. Original Root tool decisions remain
external and mandatory. Tests use disposable ledgers and synthetic transports.
"""
import base64
import binascii
import copy
import gzip
import zlib
import hashlib
from pathlib import Path

PINS = {'model': 'f40c07b2204c3305b434836015c99f30af4dfc893a0ce0e4f0318c17ea844a19', 'executor': '57c31fce3d373d770930e5e01f4b1a0fb27c4128983c78eb18b0c08a4f27a85c', 'ledger': 'c93ebd62f4304b880d29eb24812ccbf4c567d803970db1b11f27ff834f55a297', 'runtime': '018cb2a29568c47bd9fbed644be60182c448ebd21b2a9b253b2a5c5184a70d16', 'result': '1b827401e65985ae7480ada9e25b503680490faa6f3cfb5cd9ae610cf25998c8', 'finalizer': '401565f523e4270573e44a23b13d6dea73e075722fa7259b48630467a4150c94'}


WIRE_SCHEMA='hikyo-158/complete-model-wire/v1'
WIRE_KINDS=frozenset(('bound','sealed','phase-records','hosted-result-input','finalizer-records'))
MAX_WIRE_BYTES=1_000_000
MAX_MODEL_BYTES=1_900_000


def encode_tool_wire(model,value,kind):
    """Producer for an actual tool's COMPLETE output, never capture rewriting.

    Root must authenticate this exact wire object as the original tool result.
    Encoding an old uncompressed capture here cannot make a new original.
    """
    model.require(kind in WIRE_KINDS,'closed-complete-model-wire-kind')
    raw=model.canonical(value);model.require(0<len(raw)<=MAX_MODEL_BYTES,'bounded-complete-tool-model')
    compressed=gzip.compress(raw,mtime=0)
    model.require(0<len(compressed)<=MAX_WIRE_BYTES,'bounded-compressed-model')
    return {'schema':WIRE_SCHEMA,'algorithm':'gzip+base64','kind':kind,'uncompressedBytes':len(raw),
        'uncompressedSHA256':hashlib.sha256(raw).hexdigest(),'data':base64.b64encode(compressed).decode('ascii')}


def decode_tool_wire(model,wire,kind):
    """Strict lossless decoding AFTER actual original-capture authentication."""
    m=model;m.exact(wire,{'schema','algorithm','kind','uncompressedBytes','uncompressedSHA256','data'})
    m.require(wire['schema']==WIRE_SCHEMA and wire['algorithm']=='gzip+base64' and kind in WIRE_KINDS and wire['kind']==kind
        and type(wire['uncompressedBytes']) is int and 0<wire['uncompressedBytes']<=MAX_MODEL_BYTES
        and m.sha(wire['uncompressedSHA256']) and type(wire['data']) is str and 0<len(wire['data'])<=((MAX_WIRE_BYTES+2)//3)*4,'closed-bounded-complete-model-wire')
    try:
        compressed=base64.b64decode(wire['data'],validate=True)
        m.require(base64.b64encode(compressed).decode('ascii')==wire['data'] and 0<len(compressed)<=MAX_WIRE_BYTES,'canonical-base64-model-wire')
        decoder=zlib.decompressobj(16+zlib.MAX_WBITS)
        raw=decoder.decompress(compressed,MAX_MODEL_BYTES+1)
        m.require(len(raw)<=MAX_MODEL_BYTES and not decoder.unconsumed_tail and decoder.eof and not decoder.unused_data,'single-complete-bounded-gzip-member')
    except (binascii.Error,zlib.error,ValueError,UnicodeError):
        raise ValueError('operator:invalid-complete-model-wire') from None
    m.require(len(raw)==wire['uncompressedBytes'] and hashlib.sha256(raw).hexdigest()==wire['uncompressedSHA256'],'whole-lossless-model-wire-custody')
    value=m.strict_json(raw);m.require(type(value) is dict,'whole-model-object')
    return value


STDOUT_OPERATION='RootCompleteModelWireStdout'
STDOUT_SELECTOR_SCHEMA='hikyo-158/complete-model-stdout-selector/v1'


def authenticated_stdout_wire(model,row,kind):
    """Pure strict projection AFTER Root's registry authenticated the raw row.

    Preserve the genuine outer command envelope and original stdout STRING in
    the registry. Never add a parsed field or replace an original tool result.
    This codec performs no authentication and grants no execution authority.
    """
    m=model
    m.require(row['source']=='public-source' and row['operation']==STDOUT_OPERATION,'closed-stdout-wire-source')
    selector=row['selector'];m.exact(selector,{'schema','kind'})
    m.require(selector['schema']==STDOUT_SELECTOR_SCHEMA and selector['kind'] in WIRE_KINDS,'closed-stdout-wire-selector')
    m.require(type(row['response']) is str,'original-stdout-string-required')
    wire=m.strict_json(row['response'].encode('utf-8'))
    decoded=decode_tool_wire(m,wire,selector['kind'])
    return decoded if selector['kind']==kind else None


def _original_value(model,response,kind,row=None):
    if row is not None and row.get('operation')==STDOUT_OPERATION:
        return authenticated_stdout_wire(model,row,kind)
    if type(response) is dict and response.get('schema')==WIRE_SCHEMA:
        return decode_tool_wire(model,response,kind) if response.get('kind')==kind else None
    return response


def _check_wire_rows(model,raw_evidence,expected,kind):
    for row in raw_evidence:
        response=row['response']
        if row.get('operation')==STDOUT_OPERATION:
            decoded=authenticated_stdout_wire(model,row,kind)
            model.require(decoded is not None and model.same(decoded,expected),'complete-original-stdout-wire-projection-matches-input')
        elif type(response) is dict and response.get('schema')==WIRE_SCHEMA:
            decoded=decode_tool_wire(model,response,kind)
            model.require(model.same(decoded,expected),'complete-original-wire-projection-matches-input')


def phase_records(model,bundle):
    """Closed whole-stdout record projection, only AFTER registry authentication."""
    records=bundle['records']
    if type(records) is dict and set(records)=={'stdoutEvidenceIndex'}:
        index=records['stdoutEvidenceIndex']
        model.require(type(index) is int and index==0 and len(bundle['rawEvidence'])==1,'closed-authenticated-phase-stdout-index')
        row=bundle['rawEvidence'][index]
        model.require(row['source']=='public-source' and row['operation']==STDOUT_OPERATION
            and row['selector']=={'schema':STDOUT_SELECTOR_SCHEMA,'kind':'phase-records'},'exact-single-phase-stdout-selector')
        records=_original_value(model,row['response'],'phase-records',row)
        model.require(type(records) is dict,'complete-authenticated-phase-records')
    return copy.deepcopy(records)


def refused_command(*args,**kwargs):
    raise RuntimeError('operator:an explicit Root-released engine is required')


def carrier(model,kind,payload,raw_evidence,observed_at):
    """Integrity projection for an existing Root registry, never authority."""
    model.require(type(kind) is str and type(payload) is dict and type(raw_evidence) is list and bool(raw_evidence),'complete-operator-carrier-input')
    return {'schema':'hikyo-158/next-external-proof-bundle/v1','kind':'operator-'+kind,'observedAt':observed_at,
        'records':copy.deepcopy(payload),'rawEvidence':copy.deepcopy(raw_evidence)}


class ReleasedCLIPhase:
    """Prepared closed phase; run requires an explicit engine, never a default."""
    def __init__(self,operator,registry,controller,cli,phase,proof):
        self.operator,self.registry,self.controller,self.cli=operator,registry,controller,cli
        self.phase,self.proof,self.used=phase,copy.deepcopy(proof),False
        self.scope=operator.b.cli_scope(operator.x,cli,phase);registry.require_transport_scope(self.scope)
    def run(self,engine):
        o=self.operator;o._pins();o.m.require(callable(engine) and not self.used,'explicit-once-phase-engine')
        self.used=True
        self.cli.command=o.b.MetadataCLITransport(o.x,self.cli,self.phase,self.registry,engine)
        return self.controller.provision(self.cli,self.proof) if self.phase=='provision' else self.controller.retire(self.cli,self.proof)


class Operator:
    def __init__(self,modules,ledger,prepared,execution_sources,finalization_sources,operator_sha256,explicit_clock):
        self.modules=dict(modules);self.m=modules['model'];self.x=modules['executor'];self.b=modules['runtime']
        self.h=modules['result'];self.n=modules['finalizer'];self.ledger,self.prepared=ledger,copy.deepcopy(prepared)
        self.execution_sources,self.finalization_sources=copy.deepcopy(execution_sources),copy.deepcopy(finalization_sources)
        self.operator_sha256,self.now=operator_sha256,explicit_clock;self.bound,self.sealed=None,None
        self.m.exact(modules,PINS);self.m.require(callable(explicit_clock) and self.m.sha(operator_sha256)
            and hashlib.sha256(Path(__file__).read_bytes()).hexdigest()==operator_sha256,'explicit-clock-and-reviewed-operator-pin')
        self._pins();self.m.preparation_checked(prepared);self.x.source_manifest_checked(self.m,execution_sources)
        self.m.exact(finalization_sources,{'execution','modelSHA256','executorSHA256','resultSHA256','finalizationSHA256','nativeGuardSHA256s'})
        self.m.require(execution_sources['executorSHA256']==PINS['executor'] and execution_sources['ledgerSHA256']==PINS['ledger']
            and execution_sources['executorTestSHA256']==self.n.EXECUTOR_TEST_SHA256
            and self.m.same(finalization_sources['execution'],execution_sources) and finalization_sources['finalizationSHA256']==PINS['finalizer'],
            'actual-reviewed-original-execution-and-finalization-source-map')
        self.m.require(finalization_sources['modelSHA256']==PINS['model'] and finalization_sources['executorSHA256']==PINS['executor']
            and finalization_sources['resultSHA256']==PINS['result'] and self.m.same(finalization_sources['nativeGuardSHA256s'],self.n.NATIVE_GUARD_PINS),
            'complete-reviewed-finalization-source-map-before-reserve')
        self.m.require(type(ledger) is modules['ledger'].Ledger and ledger.folder.name==self.m.configuration_checked()['ledgerFolder'],'original-concrete-configured-ledger')
    def _pins(self):
        for name,pin in PINS.items():self.b.source_pin(self.modules[name],pin)
    def _registry(self,authority):
        self._pins();return self.b.OriginalCaptureRegistry(self.m,authority,self.now)
    def _context(self,channel,inner):
        return {'schema':'hikyo-158/operator-capture-context/v1','channel':channel,'operatorSHA256':self.operator_sha256,
            'frozenModulePins':dict(PINS),'innerContext':copy.deepcopy(inner)}
    def describe_executor_context(self,kind,state_sha256):
        """Inert exact context for Root review, never a release decision."""
        allowed={'reserve','dispatch','dispatch-forward','bind','provision','seal','retire'}|{
            prefix+op for op in self.x.OWNER_OPERATIONS for prefix in ('owner-','owner-forward-','owner-result-')}
        self.m.require(kind in allowed and (state_sha256 is None if kind=='reserve' else self.m.sha(state_sha256)),'closed-executor-context')
        return {'phase':kind,'account':self.m.ACCOUNT,'region':self.m.REGION,'approvalSHA256':self.m.SOURCE_PINS['approval'],
            'attemptTrustBindingSHA256':self.prepared['attemptTrustBindingSHA256'],
            'preparationSHA256':self.m.digest(self.prepared),'boundSHA256':self.m.digest(self.bound) if self.bound is not None else None,
            'stateSHA256':state_sha256,'sourcePins':copy.deepcopy(self.execution_sources),
            'storagePaths':{'ledger':str(self.ledger.folder),'globalParent':str(self.ledger.parent),'globalLock':str(self.ledger.parent/'.provisioning.lock')}}
    def describe_finalizer_capture(self,proof,state_sha256):
        m=self.m;m.require(self.bound is not None and self.sealed is not None and m.sha(state_sha256),'bound-finalizer-capture-context')
        m.exact(proof,{'schema','kind','observedAt','records','rawEvidence'})
        allowed={'complete'}|{prefix+op for op in self.n.OPERATIONS for prefix in ('arm-','ticket-','receipt-')}
        m.require(proof['kind'] in allowed,'closed-finalizer-capture-kind')
        inner={'phase':proof['kind'],'account':m.ACCOUNT,'region':m.REGION,'approvalSHA256':m.SOURCE_PINS['approval'],
            'attemptTrustBindingSHA256':self.prepared['attemptTrustBindingSHA256'],
            'boundPlanSHA256':m.digest(self.bound),'sealedModelSHA256':m.digest(self.sealed),'stateSHA256':state_sha256,
            'sourcePins':copy.deepcopy(self.finalization_sources),'storagePaths':{'ledger':str(self.ledger.folder),'parent':str(self.ledger.parent),'lock':str(self.ledger.parent/'.provisioning.lock')}}
        payload={k:copy.deepcopy(v) for k,v in proof.items() if k!='rawEvidence'}
        return {'captureBundle':carrier(m,'final-owner',payload,proof['rawEvidence'],proof['observedAt']),'context':self._context('final-owner',inner)}
    def describe_result_capture(self,bundle,raw_evidence,*,archive_bytes=None,never_started=False):
        m=self.m;m.require(self.bound is not None and type(never_started) is bool,'bound-result-capture-context')
        if never_started:m.require(archive_bytes is None,'no-archive-never-started-capture');archive=b''
        else:m.require(type(archive_bytes) is bytes and self.sealed is not None,'whole-archive-result-capture');archive=archive_bytes
        inner={'modelSHA256':self.h.MODEL_SHA256,'modelTestSHA256':self.h.MODEL_TEST_SHA256,'executorSHA256':self.h.EXECUTOR_SHA256,
            'boundPlanSHA256':m.digest(self.bound),'sealedModelSHA256':m.digest(self.sealed) if self.sealed is not None else None,
            'account':m.ACCOUNT,'region':m.REGION,'run':copy.deepcopy(self.bound['run']),
            'sourceManifest':copy.deepcopy(self.bound['preparation']['proposal']['sourceManifest'])}
        if never_started:inner={'phase':'never-started-cancelled-only',**inner}
        payload={'validatorInput':bundle,'archiveCustody':{'sha256':hashlib.sha256(archive).hexdigest(),'bytes':len(archive)}}
        return {'captureBundle':carrier(m,'hosted-result',payload,raw_evidence,bundle['observedAt']),'context':self._context('hosted-result',inner)}
    def _controller(self,registry):
        o=self
        class CapturedExecutorAdapter(o.x.RootProofAdapter):
            def authenticate(self,bundle,context,explicit_now):
                authenticated=registry.authenticate(bundle,context,explicit_now)
                _check_wire_rows(o.m,authenticated['rawEvidence'],phase_records(o.m,authenticated),'phase-records')
                return authenticated
            def records_from_authenticated_bundle(self,bundle):
                return phase_records(o.m,bundle)
        controller=self.x.FixtureController(self.m,self.ledger,self.prepared,self.execution_sources,CapturedExecutorAdapter(),self.now)
        if self.bound is not None:
            self.m.bound_checked(self.bound,self.m.instant(self.bound['derivedAt']))
            with self.ledger.lock():
                _,state=self.ledger.snapshot();self.m.require(state['boundSHA256']==self.m.digest(self.bound),'same-original-bound-ledger')
            controller.bound=copy.deepcopy(self.bound)
        return controller
    def attach(self,authority,bound,sealed,raw_evidence):
        """Restore only independently Root-captured immutable model references.

        Does not call bind/seal again or modify original ledger/claims. Complete
        bound/sealed original tool projections must be in the reviewed evidence.
        """
        m=self.m;registry=self._registry(authority)
        m.require(m.same(bound['preparation'],self.prepared),'same-whole-original-operator-preparation')
        m.bound_checked(bound,m.instant(bound['derivedAt']))
        if sealed is not None:m.sealed_checked(sealed);m.require(m.same(sealed['boundPlan'],bound),'same-actual-sealed-bound')
        with self.ledger.lock():
            head,state=self.ledger.snapshot();claim=self.ledger.claim_record()
            m.require(claim['preparationSHA256']==m.digest(self.prepared) and m.same(claim['sourcePins'],self.execution_sources)
                and claim['approvalSHA256']==m.SOURCE_PINS['approval'] and m.same(claim['historicalPins'],m.SOURCE_PINS)
                and claim['heldHistorySHA256']==m.SOURCE_PINS['heldHistory'] and claim['completedHistorySHA256']==m.SOURCE_PINS['completedAttempt'] and claim['configurationSHA256']==m.digest(m.release_configuration_checked()) and claim['heldHistorySHA256']==m.digest(self.prepared['proposal']['inputs']['heldHistory'])
                and m.same(claim['usedNamespaces'],self.prepared['proposal']['usedNamespaces']) and claim['namespace']==self.prepared['namespace']
                and type(claim['retainedReservations']) is int and claim['retainedReservations']==176
                and type(claim['newReservationsCharged']) is int and claim['newReservationsCharged']==32
                and type(claim['lifetimeReservations']) is int and claim['lifetimeReservations']==208
                and type(claim['maximumDispatches']) is int and claim['maximumDispatches']==1 and claim['automaticRetry'] is False,'whole-original-attach-claim-custody')
            m.require(state['boundSHA256']==m.digest(bound)
                and (sealed is None or state['sealedSHA256']==m.digest(sealed)),'captured-models-match-original-state')
            payload={'preparationSHA256':m.digest(self.prepared),'boundPlanSHA256':m.digest(bound),'sealedModelSHA256':m.digest(sealed) if sealed is not None else None,'stateSHA256':head}
            bundle=carrier(m,'attach',payload,raw_evidence,m.stamp(self.now()))
            authenticated=registry.authenticate(bundle,self._context('attach',payload),self.now())
            m.require(any(m.same(_original_value(m,row.get('response'),'bound',row),bound) for row in authenticated['rawEvidence']),'whole-original-bound-capture')
            if sealed is not None:m.require(any(m.same(_original_value(m,row.get('response'),'sealed',row),sealed) for row in authenticated['rawEvidence']),'whole-original-sealed-capture')
        self.bound,self.sealed=copy.deepcopy(bound),copy.deepcopy(sealed)
        return copy.deepcopy(payload)
    def reserve(self,authority,proof):return self._controller(self._registry(authority)).reserve(proof)
    def arm_owner(self,operation,authority,proof):
        self.m.require(operation in self.x.OWNER_OPERATIONS,'closed-owner-operation')
        return self._controller(self._registry(authority)).arm_owner(operation,self._owner_request(operation),proof)
    def _owner_request(self,operation):
        # CreateKey needs its exact independently prepared native request; the
        # generic key builder is intentionally not supplied by this operator.
        self.m.require(operation!='CreateKey','use-arm_key-with-exact-reviewed-native-request')
        self.m.require(self.bound is not None,'bound-activation-only')
        if operation=='DeletePolicyVersion':return {'PolicyArn':self.m.SETUP,'VersionId':'v1'}
        if operation=='CreatePolicyVersion':return {'PolicyArn':self.m.SETUP,'PolicyDocument':self.bound['documents']['setup-user.managed-policy.json'],'SetAsDefault':True}
        return {'PolicyName':'hikyo-158-role-boundary','Path':'/','PolicyDocument':self.bound['documents']['setup-role-boundary.json']}
    def arm_key(self,authority,exact_request,proof):
        return self._controller(self._registry(authority)).arm_owner('CreateKey',exact_request,proof)
    def owner_contract(self,operation,authority,proof):
        self.m.require(operation in self.x.OWNER_OPERATIONS,'closed-owner-operation')
        controller=self._controller(self._registry(authority));controller.owner_forward_ticket(operation,proof)
        with self.ledger.lock():
            result=self.b.external_forward_contract(self.m,self.x,self.ledger,operation,self.now())
            self.ledger.once('operator-owner-'+operation+'-contract.once.json',result)
        return result
    def accept_owner(self,operation,authority,proof):
        self.m.require(operation in self.x.OWNER_OPERATIONS,'closed-owner-operation')
        return self._controller(self._registry(authority)).accept_owner_receipt(operation,proof)
    def arm_dispatch(self,authority,proof):return self._controller(self._registry(authority)).arm_dispatch(proof)
    def dispatch_contract(self,authority,proof):
        controller=self._controller(self._registry(authority));controller.dispatch_forward_ticket(proof)
        with self.ledger.lock():
            result=self.b.external_forward_contract(self.m,self.x,self.ledger,'GitHubDispatch',self.now())
            self.ledger.once('operator-github-dispatch-contract.once.json',result)
        return result
    def bind(self,authority,proof):
        result=self._controller(self._registry(authority)).bind(proof);self.bound=copy.deepcopy(result);return result
    def describe_cli_scope(self,phase,cli_inputs):
        """Inert exact argv/env scope for Root's later direct review/release."""
        self._pins();self.m.require(phase in ('provision','retire') and self.bound is not None,'closed-bound-cli-scope')
        self.m.exact(cli_inputs,{'loginRoot','baseEnvironment','thumbprints'})
        cli=self.x.OfficialFixtureCLI(self.m,self.ledger,self.bound,cli_inputs['loginRoot'],cli_inputs['baseEnvironment'],cli_inputs['thumbprints'],refused_command,self.now)
        return self.b.cli_scope(self.x,cli,phase)
    def prepare_cli_phase(self,phase,authority,proof,cli_inputs):
        m=self.m;m.require(phase in ('provision','retire') and self.bound is not None,'closed-bound-cli-phase')
        m.exact(cli_inputs,{'loginRoot','baseEnvironment','thumbprints'})
        registry=self._registry(authority);controller=self._controller(registry)
        cli=self.x.OfficialFixtureCLI(m,self.ledger,self.bound,cli_inputs['loginRoot'],cli_inputs['baseEnvironment'],cli_inputs['thumbprints'],refused_command,self.now)
        return ReleasedCLIPhase(self,registry,controller,cli,phase,proof)
    def seal(self,authority,proof):
        result=self._controller(self._registry(authority)).seal(proof);self.sealed=copy.deepcopy(result);return result
    def _result_adapter(self,registry,raw_evidence):
        o=self
        class CapturedHostedAdapter(o.h.RootHostedProofAdapter):
            def authenticate(self,bundle,context,archive_bytes,explicit_now):
                payload={'validatorInput':bundle,'archiveCustody':{'sha256':hashlib.sha256(archive_bytes).hexdigest(),'bytes':len(archive_bytes)}}
                capture=carrier(o.m,'hosted-result',payload,raw_evidence,bundle['observedAt'])
                authenticated=registry.authenticate(capture,o._context('hosted-result',context),explicit_now)
                _check_wire_rows(o.m,authenticated['rawEvidence'],authenticated['records']['validatorInput'],'hosted-result-input')
                return copy.deepcopy(authenticated['records']['validatorInput'])
        return CapturedHostedAdapter()
    def validate_result(self,authority,bundle,raw_evidence,*,archive_bytes=None,never_started=False):
        self.m.require(self.bound is not None and type(never_started) is bool,'bound-result-branch')
        registry=self._registry(authority);adapter=self._result_adapter(registry,raw_evidence)
        if never_started:
            self.m.require(archive_bytes is None,'never-started-has-no-archive')
            return self.h.validate_never_started(self.m,self.bound,self.sealed,bundle,adapter,explicit_now=self.now())
        self.m.require(type(archive_bytes) is bytes and self.sealed is not None,'explicit-original-hosted-archive')
        return self.h.validate(self.m,archive_bytes,self.bound,self.sealed,bundle,adapter,explicit_now=self.now())
    def _finalizer(self,registry):
        o=self
        class CapturedFinalOwnerAdapter(o.n.RootFinalizationAdapter):
            def authenticate(self,bundle,context,explicit_now):
                payload={k:copy.deepcopy(v) for k,v in bundle.items() if k!='rawEvidence'}
                capture=carrier(o.m,'final-owner',payload,bundle['rawEvidence'],bundle['observedAt'])
                authenticated=registry.authenticate(capture,o._context('final-owner',context),explicit_now)
                _check_wire_rows(o.m,authenticated['rawEvidence'],authenticated['records']['records'],'finalizer-records')
                return {**authenticated['records'],'rawEvidence':authenticated['rawEvidence']}
        o.m.require(o.bound is not None and o.sealed is not None,'genuine-sealed-final-owner-only')
        return o.n.OwnerFinalization(o.m,o.x,o.ledger,o.bound,o.sealed,o.finalization_sources,CapturedFinalOwnerAdapter(),o.now)
    def arm_final_owner(self,operation,authority,proof):
        self.m.require(operation in self.n.OPERATIONS,'closed-final-owner-operation')
        return self._finalizer(self._registry(authority)).arm(operation,proof)
    def final_owner_contract(self,operation,authority,proof):
        m=self.m;m.require(operation in self.n.OPERATIONS,'closed-final-owner-operation')
        finalizer=self._finalizer(self._registry(authority));ticket=finalizer.forward_ticket(operation,proof)
        with self.ledger.lock():
            intent=self.ledger.read_once('final-owner-'+operation+'-intent.once.json')
            result={'schema':'hikyo-158/operator-final-owner-contract/v1','operation':operation,'endpoint':'https://iam.amazonaws.com/',
                'request':intent['request'],'intentSHA256':m.digest(intent),'ticket':ticket,'requiredNativePreparation':intent['requiredNativePreparation'],
                'maximumForwards':1,'automaticRetry':False,'runtimeReady':False,'executionAuthorityGranted':False}
            self.ledger.once('operator-final-owner-'+operation+'-contract.once.json',result)
        return result
    def accept_final_owner(self,operation,authority,proof):
        self.m.require(operation in self.n.OPERATIONS,'closed-final-owner-operation')
        return self._finalizer(self._registry(authority)).accept_receipt(operation,proof)
    def complete_final_owner(self,authority,proof):return self._finalizer(self._registry(authority)).complete(proof)


if __name__=='__main__':
    raise SystemExit('SOURCE ONLY: Root original capture releases and explicit engines are required')
