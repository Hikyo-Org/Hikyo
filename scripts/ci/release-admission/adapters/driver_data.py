"""Closed public driver data. No authority, environment reads or runtime I/O."""

from . import adapter_context
import hashlib
import json
import re
from pathlib import Path
import shlex

HERE = Path(__file__).resolve().parent
N = HERE
LIMIT = 1_000_000  # Same entire-stdout bound as the pinned persistent client.
ENV = {'LANG':'C', 'LC_ALL':'C', 'TZ':'UTC', 'NO_COLOR':'1'}
FALSE = {'externalAuthenticityVerified':False, 'executionAuthorityGranted':False,
         'runtimeReady':False, 'controlLeaseClaimed':False, 'exclusiveControlVerifiedByAdapter':False,
         'providerOutcomeProven':False, 'parentNativeOriginalObservedByDriver':False}
REFS = {'holderIndex','holderDescriptor','ready','statusContext','statusOriginal'}


def require(ok, label):
    if not ok: raise ValueError('paired:' + label)


def exact(value, fields):
    require(type(value) is dict and set(value)==set(fields),'closed-fields')


def canonical(value):
    return json.dumps(value,sort_keys=True,separators=(',',':'),ensure_ascii=False,allow_nan=False).encode('utf-8')


def sha(raw): return hashlib.sha256(raw).hexdigest()


def strict(raw, limit=LIMIT):
    require(type(raw) is bytes and 0<len(raw)<=limit,'whole-bounded-json')
    def pairs(items):
        result={}
        for key,value in items:
            require(key not in result,'duplicate-json-key');result[key]=value
        return result
    def bad(_): raise ValueError('paired:noninteger-or-nonfinite-json')
    value=json.loads(raw.decode('utf-8'),object_pairs_hook=pairs,parse_float=bad,parse_constant=bad)
    canonical(value)  # Refuse malformed UTF-16 surrogates; literal astral Unicode is lossless.
    return value


def hash_checked(value):
    require(type(value) is str and re.fullmatch('[a-f0-9]{64}',value),'raw-pin')


def ref_checked(ref):
    exact(ref,{'path','rawSHA256'});hash_checked(ref['rawSHA256'])
    path=ref['path'];require(type(path) is str and len(path)<=2048 and path.startswith('/')
        and str(Path(path))==path and '..' not in Path(path).parts and not any(c in path for c in ('\0','\n','\r')),'explicit-normalized-path')
    path.encode('utf-8');return Path(path)


def plan_checked(plan):
    context=adapter_context.get()
    context.checked(context.model,plan,plan['action'])
    exact(plan,{adapter_context.FIELD,'schema','phase','nativeStageSourceSHA256','holderContextSHA256','python','inputs','action'})
    require(plan['schema']=='hikyo-158/native-paired-driver-plan/v1' and plan['phase'] in ('arm','finish')
        and plan['nativeStageSourceSHA256']==context.source_sha('adapters/preflight.py'),'explicit-phase-and-reviewed-N')
    hash_checked(plan['holderContextSHA256']);ref_checked(plan['python']);exact(plan['inputs'],REFS)
    paths=[]
    for ref in plan['inputs'].values():
        path=ref_checked(ref);require(path.is_relative_to(context.runtime_root),'public-reference-within-E');paths.append(path)
    require(len(paths)==len(set(paths)),'distinct-original-references')
    a=plan['action'];exact(a,{adapter_context.FIELD,'requestId','sequence','ownerId','sourceSHA256','reviewSHA256','holderSourceBindingSHA256','expectedIdentity','sharedWindow'})
    for key in ('requestId','ownerId'):
        require(type(a[key]) is str and re.fullmatch('[a-f0-9]{8}(?:-[a-f0-9]{4}){3}-[a-f0-9]{12}',a[key]),'explicit-UUID')
    require(type(a['sequence']) is int and 1<=a['sequence']<=1153,'explicit-positive-sequence')
    for key in ('sourceSHA256','reviewSHA256','holderSourceBindingSHA256'):hash_checked(a[key])
    exact(a['sharedWindow'],{'notBeforeMS','expiresAtMS'})
    require(all(type(v) is int and 0<=v<=9007199254740991 for v in a['sharedWindow'].values()),'explicit-original-window')
    exact(a['expectedIdentity'],{'targetId','targetType','targetURL','browserContextId','mainFrameId','loaderId','frameURL','frameURLRelation'})
    return plan


def carrier(plan, plan_ref, source_pin):
    plan_checked(plan);ref_checked(plan_ref);hash_checked(source_pin)
    argv=[plan['python']['path'],'-B',str(HERE/'root_driver.py'),plan['phase'],'--runtime',
          '--reviewed-source-sha256',source_pin,'--plan-file',plan_ref['path'],'--plan-sha256',plan_ref['rawSHA256']]
    inner=shlex.join(argv)
    return {'schema':'hikyo-158/native-paired-driver-carrier/v1','argv':argv,'innerCommand':inner,
            'carrierArgv':['/bin/zsh','-lc',inner],'command':'/bin/zsh -lc '+shlex.quote(inner)}


def shape(value, definition):
    """Narrow closed declarative result validator; same N/R2 known definitions.

    The pinned client already runs the actual Zod compiler. This independent
    consumer never changes that registry/schema or admits arbitrary JSON fields.
    """
    kind=definition['kind']
    if kind=='literal':require(type(value) is type(definition['value']) and value==definition['value'],'literal')
    elif kind=='enum':require(type(value) is str and value in definition['values'],'enum')
    elif kind=='integer':require(type(value) is int and definition['min']<=value<=definition['max'],'integer')
    elif kind=='boolean':require(type(value) is bool,'boolean')
    elif kind=='pattern':require(type(value) is str and len(value.encode('utf-16-le'))//2<=definition['maxLength']
        and re.fullmatch(definition['pattern'],value),'pattern')
    elif kind=='object':
        exact(value,definition['fields'])
        for key,d in definition['fields'].items():shape(value[key],d)
    elif kind=='array':
        require(type(value) is list and len(value)<=definition['maxItems'],'array')
        for item in value:shape(item,definition['items'])
    else:raise ValueError('paired:unsupported-closed-definition')


def shape_bound(d):
    kind=d['kind']
    if kind=='literal':return len(canonical(d['value']))
    if kind=='enum':return max(len(canonical(v)) for v in d['values'])
    if kind=='integer':return max(len(str(d['min'])),len(str(d['max'])))
    if kind=='boolean':return 5
    if kind=='pattern':return d['maxLength']*6+2
    if kind=='array':return 2+d['maxItems']*(shape_bound(d['items'])+1)
    if kind=='object':return 2+sum(len(canonical(k))+2+shape_bound(v) for k,v in d['fields'].items())
    raise ValueError('paired:unsupported-closed-definition')
