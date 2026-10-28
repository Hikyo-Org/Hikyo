"""NEW parent-native-original custody. Never relabel internal child stdout."""

from . import adapter_context
import base64
from .driver_native_carrier import matches_native_carrier

from . import driver_data as d
from .paired_core import response_checked


KEYS={adapter_context.FIELD,'schema','phase','driverSourceSHA256','nativeStageSourceSHA256','planReference','driverCarrier',
      'preflightDescriptor','descriptorSHA256','child','completedAtMS','outcome','launchAttempted','automaticRetry',*d.FALSE}
CHILD={'evidenceKind','argv','request','requestSHA256','scriptSHA256','environment','exitCode',
       'stdoutRawSHA256','stdoutBytes','stdoutBase64','clientResponseSHA256'}


def verify_parent_original(*,m,n,stage,registry,binding,plan,plan_ref,source_pin,raw,connection,
                           sequence_min,sequence_max,explicit_now):
    adapter_context.checked(m,binding,plan,plan['action'])
    original,sequence=stage.verify_native_original(connection,raw,m,sequence_min,sequence_max)
    expected=d.carrier(plan,plan_ref,source_pin)
    m.require(matches_native_carrier(original['input'], expected_inner_command=expected['innerCommand'], expected_carrier_argv=expected['carrierArgv']) and original.get('threadId',stage.THREAD)==stage.THREAD
        and m.instant(original['updatedAt'])<=explicit_now,'exact-parent-driver-native-carrier')
    m.fresh(original['completedAt'],explicit_now)
    receipt=d.strict(original['output'].encode('utf-8'));d.exact(receipt,KEYS);adapter_context.checked(m,receipt)
    m.require(receipt['schema']=='hikyo-158/native-paired-driver-result/v1'
        and receipt['phase']==plan['phase'] and receipt['driverSourceSHA256']==source_pin
        and receipt['nativeStageSourceSHA256']==adapter_context.get(m).source_sha('adapters/preflight.py') and m.same(receipt['planReference'],plan_ref)
        and m.same(receipt['driverCarrier'],expected) and receipt['outcome']=='completed-public-action'
        and receipt['launchAttempted'] is True and receipt['automaticRetry'] is False
        and all(receipt[k] is False for k in d.FALSE),'closed-paired-parent-output-no-authority')
    desc=receipt['preflightDescriptor'];child=receipt['child'];d.exact(child,CHILD)
    adapter_context.checked(m,desc)
    a=plan['action']
    m.require(a['ownerId']==binding['ownerId'] and a['sourceSHA256']==binding['transportSource']['rawSHA256']
        and a['reviewSHA256']==binding['reviewManifest']['rawSHA256']
        and a['holderSourceBindingSHA256']==m.digest(binding),'same-explicit-plan-owner-source-review-binding')
    m.require(receipt['descriptorSHA256']==m.digest(desc) and desc['phase']==plan['phase']
        and desc['holderContextSHA256']==plan['holderContextSHA256']
        and m.same(desc['expectedIdentity'],plan['action']['expectedIdentity'])
        and m.same(desc['sharedWindow'],plan['action']['sharedWindow'])
        and desc['statusOriginalRawSHA256']==plan['inputs']['statusOriginal']['rawSHA256']
        and desc['readyRawSHA256']==plan['inputs']['ready']['rawSHA256']
        and desc['holderIndexRawSHA256']==plan['inputs']['holderIndex']['rawSHA256']
        and desc['effectBeforeMS']==min(desc['issuedAtMS']+2000,desc['physicalWindow']['expiresAtMS'],desc['sharedWindow']['expiresAtMS']-60000)
        and desc['physicalWindow']['expiresAtMS']==desc['physicalWindow']['notBeforeMS']+55000,'whole-paired-preflight-binding')
    cmd=stage.command(m,binding,registry,kind=plan['phase'],arguments={adapter_context.FIELD:plan[adapter_context.FIELD],'expected':plan['action']['expectedIdentity'],
        'window':plan['action']['sharedWindow'],'phase':plan['phase'],'descriptor':desc},
        request_id=plan['action']['requestId'],sequence=plan['action']['sequence'])
    row=next(r for r in registry['scripts'] if r['id']==cmd['request']['scriptId'])
    d.shape(cmd['request']['arguments'],row['arguments'])
    m.require(child['evidenceKind']=='internal-child-stdio-not-native-original' and m.same(child['argv'],cmd['argv'])
        and m.same(child['request'],cmd['request']) and child['requestSHA256']==m.digest(cmd['request'])
        and child['scriptSHA256']==cmd['scriptSHA256'] and child['environment']==d.ENV
        and type(child['exitCode']) is int and child['exitCode']==0,'exact-child-argv-request-environment')
    stdout=base64.b64decode(child['stdoutBase64'],validate=True)
    m.require(len(stdout)==child['stdoutBytes'] and d.sha(stdout)==child['stdoutRawSHA256'],'whole-child-stdout-bytes')
    response=response_checked(n,stage,m,stdout,cmd,desc,registry,receipt['completedAtMS'])
    m.require(child['clientResponseSHA256']==response['responseSHA256']
        and m.instant(original['startedAt']).timestamp()*1000<=desc['issuedAtMS']
        <=response['result']['readAtMS']<=receipt['completedAtMS']<desc['effectBeforeMS']
        and receipt['completedAtMS']<=m.instant(original['completedAt']).timestamp()*1000
        <desc['physicalWindow']['expiresAtMS'],'whole-parent-and-child-original-window')
    return {adapter_context.FIELD:plan[adapter_context.FIELD],'schema':'hikyo-158/native-paired-parent-custody/v1','nativeOriginalRawSHA256':d.sha(raw),
        'nativeOriginalSHA256':m.digest(original),'nativeOriginalSequence':sequence,'driverCarrierSHA256':m.digest(expected),
        'descriptorSHA256':m.digest(desc),'childStdoutRawSHA256':d.sha(stdout),
        'requestSHA256':m.digest(cmd['request']),'clientResponseSHA256':response['responseSHA256'],**d.FALSE}
