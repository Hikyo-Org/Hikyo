"""One preflight + one internal child action. No native-original fabrication."""

from . import adapter_context
import base64
import copy

from . import driver_data as d


def response_checked(n,stage,m,raw,command,descriptor,registry,now_ms):
    d.require(type(raw) is bytes and raw.endswith(b'\n') and raw.count(b'\n')==1 and b'\r' not in raw,'whole-client-stdout-frame')
    response=d.strict(raw);m.exact(response,stage.h_custody.RESPONSE_KEYS)
    req=command['request']
    m.require(response['schema']=='hikyo-158/persistent-owner-response/v1'
        and all(response[k]==req[k] for k in ('ownerId','sourceSHA256','reviewSHA256','requestId','sequence'))
        and type(response['sequence']) is int and type(response['nextSequence']) is int
        and response['nextSequence']==req['sequence']+1<=1154
        and all(response[k] is True for k in ('accepted','ok','ownerStillAlive','clientChannelClosed','cliCaptureSuppressed'))
        and all(response[k] is False for k in ('ownerClosed','coreEphemeralProtocolMetadataSuppressed','controlLeaseClaimed','exclusiveControlVerifiedByAdapter','ownerProcessExitVerified'))
        and response['state']=='active' and response['error'] is None
        and response['responseSHA256']==stage.transport_receipt.digest({k:v for k,v in response.items() if k not in stage.h_custody.STDOUT_ONLY}),
        'exact-paired-client-response-binding-and-closure')
    row=next(r for r in registry['scripts'] if r['id']==req['scriptId'])
    value=response['result'];d.shape(value,row['result'])
    m.require(value['kind']==value['phase']==descriptor['phase']
        and m.same(value['before'],descriptor['expectedIdentity']) and m.same(value['after'],descriptor['expectedIdentity'])
        and descriptor['issuedAtMS']<=value['readAtMS']<descriptor['effectBeforeMS']
        and value['readAtMS']<=now_ms,'exact-action-public-CDP-time')
    if descriptor['phase']=='arm':
        m.require(m.same(value['value']['guard'],{**descriptor['expectedGuardStatus'],'phase':'armed'}),'exact-native-arm-status')
    return response


class Attempt:
    def __init__(self):self.used=False

    def run(self,*,n,stage,m,plan,plan_ref,source_pin,values,registry,status_context,preflight,pre_submit,verify,
            transport,wall_ms,monotonic,physical_stop,client_outer_ms):
        d.require(not self.used,'local-attempt-already-used');self.used=True
        descriptor=None;command=None;child=None;attempted=False;closed_response=None
        carrier=d.carrier(plan,plan_ref,source_pin)
        receipt={adapter_context.FIELD:adapter_context.get(m).binding_sha256,'schema':'hikyo-158/native-paired-driver-result/v1','phase':plan['phase'],
            'driverSourceSHA256':source_pin,'nativeStageSourceSHA256':adapter_context.get(m).source_sha('adapters/preflight.py'),'planReference':copy.deepcopy(plan_ref),
            'driverCarrier':carrier,'preflightDescriptor':None,'descriptorSHA256':None,'child':None,
            'completedAtMS':None,'outcome':'refused-before-launch','launchAttempted':False,'automaticRetry':False,**d.FALSE}
        try:
            adapter_context.checked(m,plan,values['sourceBinding'],values['lockContext'],status_context)
            adapter_context.get(m).prepared(m,values['prepared'])
            verify();d.plan_checked(plan)
            binding=values['sourceBinding'];a=plan['action']
            m.require(a['ownerId']==binding['ownerId'] and a['sourceSHA256']==binding['transportSource']['rawSHA256']
                and a['reviewSHA256']==binding['reviewManifest']['rawSHA256']
                and a['holderSourceBindingSHA256']==m.digest(binding),'explicit-plan-source-binding')
            m.require(status_context['request']['arguments']['phase']==plan['phase']
                and a['sequence']==status_context['request']['sequence']+1
                and a['requestId']!=status_context['request']['requestId'],'explicit-original-status-next-sequence')
            descriptor=preflight()  # EXACTLY one real N preflight, never a fresh-status generator.
            adapter_context.checked(m,descriptor)
            receipt['preflightDescriptor']=descriptor;receipt['descriptorSHA256']=m.digest(descriptor)
            m.require(descriptor['phase']==plan['phase'] and descriptor['holderContextSHA256']==plan['holderContextSHA256']
                and m.same(a['expectedIdentity'],descriptor['expectedIdentity'])
                and m.same(a['sharedWindow'],descriptor['sharedWindow'])
                and descriptor['statusContextSHA256']==m.digest(status_context)
                and descriptor['statusOriginalRawSHA256']==plan['inputs']['statusOriginal']['rawSHA256']
                and descriptor['readyRawSHA256']==plan['inputs']['ready']['rawSHA256']
                and descriptor['holderIndexRawSHA256']==plan['inputs']['holderIndex']['rawSHA256'], 'same-phase-plan-and-real-descriptor')
            def adjacent():
                now=wall_ms()
                m.require(type(now) is int and descriptor['issuedAtMS']<=now<descriptor['effectBeforeMS']
                    and descriptor['physicalWindow']['notBeforeMS']<=now<descriptor['physicalWindow']['expiresAtMS']
                    and descriptor['sharedWindow']['notBeforeMS']<=now<=descriptor['sharedWindow']['expiresAtMS']-60000
                    and monotonic()<physical_stop,'original-adjacent-physical-and-shared-window')
            adjacent()
            command=stage.command(m,binding,registry,kind=plan['phase'],arguments={
                adapter_context.FIELD:plan[adapter_context.FIELD],'expected':a['expectedIdentity'],'window':a['sharedWindow'],'phase':plan['phase'],'descriptor':descriptor},
                request_id=a['requestId'],sequence=a['sequence'])
            row=next(r for r in registry['scripts'] if r['id']==command['request']['scriptId'])
            d.shape(command['request']['arguments'],row['arguments'])
            receipt['child']={'evidenceKind':'internal-child-stdio-not-native-original','argv':command['argv'],
                'request':command['request'],'requestSHA256':m.digest(command['request']),'scriptSHA256':command['scriptSHA256'],
                'environment':dict(d.ENV),'exitCode':None,'stdoutRawSHA256':None,'stdoutBytes':None,
                'stdoutBase64':None,'clientResponseSHA256':None}
            # Reserve the full closed result's conservative JSON/base64 size
            # BEFORE launch; output capacity is not an after-effect surprise.
            envelope_overhead=4096
            maximum=d.shape_bound(row['result'])+envelope_overhead
            d.require(len(d.canonical(receipt))+4*((maximum+2)//3)+1024<=d.LIMIT,'whole-parent-output-capacity')
            pre_submit();adjacent()
            stop=min(physical_stop,monotonic()+client_outer_ms/1000)
            # The transport calls this after its own setup and immediately before
            # Popen. Expiry between command building and sending cannot launch.
            def before_spawn():
                nonlocal attempted
                pre_submit();adjacent();attempted=True
            child=transport(command['argv'],stop=stop,before_spawn=before_spawn,env=dict(d.ENV))
            d.require(attempted,'child-must-follow-pre-spawn-gate');d.exact(child,{'stdout','exitCode','closed'})
            d.require(type(child['stdout']) is bytes and len(child['stdout'])<=d.LIMIT,'bounded-whole-child-bytes')
            receipt['child'].update(exitCode=child['exitCode'],stdoutRawSHA256=d.sha(child['stdout']),stdoutBytes=len(child['stdout']))
            d.require(child['closed'] is True and type(child['exitCode']) is int and child['exitCode']==0,'successful-closed-child')
            closed_response=response_checked(n,stage,m,child['stdout'],command,descriptor,registry,wall_ms())
            adjacent();pre_submit();adjacent()
            receipt['child'].update(stdoutBase64=base64.b64encode(child['stdout']).decode('ascii'),
                clientResponseSHA256=closed_response['responseSHA256'])
            receipt['outcome']='completed-public-action'
        except (ValueError,RuntimeError,OSError,KeyError,TypeError,UnicodeError,StopIteration):
            receipt['outcome']='uncertain-reconcile-only' if attempted else 'refused-before-launch'
            # Unknown/malformed stdout and all stderr stay out of public output.
        receipt['launchAttempted']=attempted
        receipt['completedAtMS']=wall_ms()
        if receipt['outcome']=='completed-public-action' and (receipt['completedAtMS']>=descriptor['effectBeforeMS'] or monotonic()>=physical_stop):
            receipt['outcome']='uncertain-reconcile-only'
        raw=d.canonical(receipt)+b'\n'
        d.require(len(raw)<=d.LIMIT,'whole-final-output-bound')
        return raw,0 if receipt['outcome']=='completed-public-action' else 2
