"""Pure post-send key custody projection for adoption/cleanup. No clocks, I/O, clients or authority.

Root authenticates source modules, native receipts, owner/session, durable intent,
actual guard and public mounted byte identity independently before any use.
"""
import copy
from datetime import datetime, timedelta, timezone
import hashlib
import json
import re

MODEL_SHA = 'f40c07b2204c3305b434836015c99f30af4dfc893a0ce0e4f0318c17ea844a19'
TEST_SHA = 'f06b2e173ff499070ae8718d65870534510a585339b8fc451462baaea6dc5985'
PINS={'modelSHA256':MODEL_SHA,'modelTestSHA256':TEST_SHA}
MAIN='20a50d3f9e11cdae453b0f764689073f2adc6c644e6ac2a08660be5b1b2227a2'
OLD_IDS=('10908b8d-fe54-40e3-9210-a3eefea709b6','990c479f-d673-4bc9-8bca-7b9b86f4b205','fa374267-a456-4e79-b5b1-6cdf356b7d0e','bfa44dc1-ee8a-43b8-aec7-d8a7fbb38db1')
MANIFEST_FIELDS={'schema','mode','account','region','endpoint','publicMainSHA256','preparationSHA256','ownerContextSHA256','intentSHA256','claimId','wireBodySHA256','wireBodyBytes','notBeforeMS','expiresAtMS','newKeyARN','createReceiptSHA256'}
ROW_FIELDS={'schema','manifestSHA256','manifest','slot','action','keyARN','guardAtForward','status','responseEndpoint','sentAtMS','receivedAtMS','requestBodyBytes','requestBodySHA256','rawResponse','rawResponseBytes','rawResponseSHA256','requestId','requestIdSource','requestIdCaptureState','JSONParsedByObserver','providerAPIsCalledByObserver','ownerAuthenticatedByObserver','publicSourceAuthenticatedByObserver','writeAuthorityGrantedByObserver'}
FLAGS=('JSONParsedByObserver','providerAPIsCalledByObserver','ownerAuthenticatedByObserver','publicSourceAuthenticatedByObserver','writeAuthorityGrantedByObserver')


def require(ok,label):
    if not ok:raise ValueError('next-key:'+label)


def exact(v,fields):require(type(v) is dict and set(v)==set(fields),'closed-fields')


def sha(v):return hashlib.sha256(v).hexdigest()


def canonical(v):return json.dumps(v,sort_keys=True,separators=(',',':'),ensure_ascii=False,allow_nan=False).encode()


def ms(now):
    delta=now-datetime(1970,1,1,tzinfo=timezone.utc)
    return delta.days*86400000+delta.seconds*1000+delta.microseconds//1000


def stamp(value):return (datetime(1970,1,1,tzinfo=timezone.utc)+timedelta(milliseconds=value)).isoformat(timespec='milliseconds').replace('+00:00','Z')


def intent_checked(m,prepared,intent_record,approved_body,explicit_now):
    m.clock(explicit_now);m.preparation_checked(prepared);m.owner_checked(prepared['records']['owner'],prepared['proposal'],explicit_now)
    exact(intent_record,{'preparationSHA256','claimId','intent'});intent=intent_record['intent']
    exact(intent,{'operation','account','region','actorARN','maximumForwards','request'})
    require(intent_record['preparationSHA256']==m.digest(prepared) and type(intent_record['claimId']) is str and re.fullmatch(r'[a-zA-Z0-9-]{8,100}',intent_record['claimId'])
        and intent['operation']=='CreateKey' and intent['account']==m.ACCOUNT and intent['region']==m.REGION and type(intent['maximumForwards']) is int and intent['maximumForwards']==1
        and intent['actorARN']==prepared['records']['owner']['ownerContext']['principalARN'],'exact-preparation-owner-intent')
    require(type(approved_body) is str and 2<=len(approved_body.encode())<=32768,'bounded-wire-body');wire=m.strict_json(approved_body.encode())
    exact(wire,{'Policy','Description','Origin','Tags','KeyUsage','KeySpec','MultiRegion'});require(type(wire['Policy']) is str,'wire-policy-string')
    parsed=copy.deepcopy(wire);parsed['Policy']=m.strict_json(wire['Policy'].encode())
    require(m.same(parsed,intent['request']),'complete-native-wire-versus-intent')
    return m.digest(intent_record)


def manifest(m,prepared,intent_record,approved_body,mode,not_before_ms,expires_at_ms,new_key_arn=None,create_receipt_sha256=None):
    """Pure explicit source spec. Does not create or authenticate an intent."""
    require(mode in ('create','readback'),'mode');prefix=f'arn:aws:kms:{m.REGION}:{m.ACCOUNT}:key/'
    require((mode=='create' and new_key_arn is None and create_receipt_sha256 is None) or
        (mode=='readback' and m.sha(create_receipt_sha256) and type(new_key_arn) is str and re.fullmatch(re.escape(prefix)+r'[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12}',new_key_arn)
            and new_key_arn not in m.scheduled_key_closure(prepared['proposal'])),'new-key-source-reference')
    return {'schema':'hikyo-158/next-key-observer-manifest/v1','mode':mode,'account':m.ACCOUNT,'region':m.REGION,'endpoint':f'https://kms.{m.REGION}.amazonaws.com/','publicMainSHA256':MAIN,
        'preparationSHA256':m.digest(prepared),'ownerContextSHA256':m.digest(prepared['records']['owner']['ownerContext']),'intentSHA256':m.digest(intent_record),'claimId':intent_record['claimId'],
        'wireBodySHA256':sha(approved_body.encode()),'wireBodyBytes':len(approved_body.encode()),'notBeforeMS':not_before_ms,'expiresAtMS':expires_at_ms,'newKeyARN':new_key_arn,'createReceiptSHA256':create_receipt_sha256}


def read_wires(action,key):
    values=[key,key.rsplit('/',1)[1]];result=[]
    for value in values:
        variants=([{'KeyId':value,'PolicyName':'default'}] if action=='GetKeyPolicy' else [{'KeyId':value},{'KeyId':value,'Limit':100},{'Limit':100,'KeyId':value}] if action=='ListAliases' else [{'KeyId':value}])
        result.extend(json.dumps(v,separators=(',',':')).encode() for v in variants)
    return result


def row_checked(m,prepared,intent_record,body,row,mode,slot,action,key,now,new_arn=None,create_sha=None):
    exact(row,ROW_FIELDS);given=row['manifest'];exact(given,MANIFEST_FIELDS)
    expected=manifest(m,prepared,intent_record,body,mode,given['notBeforeMS'],given['expiresAtMS'],new_arn,create_sha)
    require(m.same(given,expected) and row['manifestSHA256']==sha(canonical(given)),'whole-exact-manifest')
    now_ms=ms(now);start,end=given['notBeforeMS'],given['expiresAtMS'];sent,received=row['sentAtMS'],row['receivedAtMS']
    require(type(start) is int and type(end) is int and 0<=now_ms-start<=300000 and now_ms<end<=start+300000
        and type(sent) is int and type(received) is int and start<=sent<=received<=now_ms and now_ms-sent<=300000 and received-sent<=20000,'original-native-clock')
    require(row['schema']=='hikyo-158/next-key-native-response/v1' and row['slot']==slot and row['action']==action and row['keyARN']==key
        and type(row['status']) is int and row['status']==200 and row['responseEndpoint']==given['endpoint'] and all(row[f] is False for f in FLAGS),'exact-response-provenance')
    wires=[body.encode()] if mode=='create' else read_wires(action,key)
    require(type(row['requestBodyBytes']) is int and any(len(w)==row['requestBodyBytes'] and sha(w)==row['requestBodySHA256'] for w in wires),'exact-request-body')
    raw=row['rawResponse'];require(type(raw) is str and 2<=len(raw.encode())<=32768 and type(row['rawResponseBytes']) is int and len(raw.encode())==row['rawResponseBytes']
        and sha(raw.encode())==row['rawResponseSHA256'] and not re.search(r'SecretString|SecretBinary|SecretAccessKey|AccessKeyId|SessionToken',raw),'bounded-safe-raw-source')
    if mode=='create':
        guard=row['guardAtForward'];exact(guard,{'schema','phase','claimConsumed','nativeSendInvocations','intentSHA256','wireBodySHA256','endpoint'})
        require(guard=={'schema':'hikyo-158/native-kms-xhr-write-status/v1','phase':'forwarded','claimConsumed':True,'nativeSendInvocations':1,
            'intentSHA256':given['intentSHA256'],'wireBodySHA256':given['wireBodySHA256'],'endpoint':given['endpoint']} and type(guard['nativeSendInvocations']) is int and guard['claimConsumed'] is True,'one-forward-guard-snapshot')
        require(type(row['requestId']) is str and re.fullmatch(r'[A-Za-z0-9-]{8,128}',row['requestId']) and row['requestIdSource']=='native-response-header:x-amzn-requestid' and row['requestIdCaptureState']=='captured','safe-native-request-id')
    else:require(row['guardAtForward'] is None and row['requestId'] is None and row['requestIdSource'] is None and row['requestIdCaptureState']=='not-requested','no-invented-read-authority')
    return m.strict_json(raw.encode())


def normalize(m,source_pins,prepared,intent_record,approved_create_body,create_row,read_rows,*,explicit_now):
    m.exact(source_pins,PINS);require(m.same(source_pins,PINS),'reviewed-model-pins')
    # The historical forward is immutable. Validate its original authority at
    # its actual send, and its complete settled native receipt at actual receive.
    # Never use the later readback clock to renew or expire that historical send.
    m.clock(explicit_now);exact(create_row,ROW_FIELDS)
    require(type(create_row['sentAtMS']) is int and type(create_row['receivedAtMS']) is int
        and 0<=create_row['sentAtMS']<=create_row['receivedAtMS']<=ms(explicit_now),'immutable-create-before-current-readback')
    first_mutation=m.instant(stamp(create_row['sentAtMS']))
    actual_received=m.instant(stamp(create_row['receivedAtMS']))
    intent_sha=intent_checked(m,prepared,intent_record,approved_create_body,first_mutation)
    created=row_checked(m,prepared,intent_record,approved_create_body,create_row,'create','create','CreateKey',None,actual_received)
    m.ci_checked(prepared['records']['code'],prepared['proposal']['sourceManifest'],first_mutation)
    m.owner_checked(prepared['records']['owner'],prepared['proposal'],first_mutation)
    m.budget_checked(prepared['records']['budget'],first_mutation)
    m.historical_absence_checked(prepared['records']['historicalAbsence'],first_mutation,m.digest(prepared['records']['owner']['ownerContext']))
    require(m.instant(prepared['lease']['start'])<=first_mutation<=explicit_now<m.instant(prepared['lease']['until']),'key-within-reviewed-lease')
    exact(created,{'KeyMetadata'});metadata=created['KeyMetadata'];require(type(metadata) is dict and type(metadata.get('Arn')) is str,'whole-create-key-metadata');key=metadata['Arn'];creation_sha=m.digest(create_row)
    require(key not in m.scheduled_key_closure(prepared['proposal']),'not-any-old-key')
    if 'Description' in metadata:require(metadata['Description']==intent_record['intent']['request']['Description'],'exact-created-description')
    if 'CustomerMasterKeySpec' in metadata:require(metadata['CustomerMasterKeySpec']=='SYMMETRIC_DEFAULT','symmetric-customer-key-spec')
    if 'EncryptionAlgorithms' in metadata:require(metadata['EncryptionAlgorithms']==['SYMMETRIC_DEFAULT'],'symmetric-encryption-algorithm')
    if 'CurrentKeyMaterialId' in metadata:require(m.sha(metadata['CurrentKeyMaterialId']),'safe-key-material-id')
    prefix=f'arn:aws:kms:{m.REGION}:{m.ACCOUNT}:key/'
    required={**{'new.'+action:(action,key) for action in ('DescribeKey','GetKeyPolicy','GetKeyRotationStatus','ListAliases')},
        **{'old'+str(i)+'.DescribeKey':('DescribeKey',prefix+key_id) for i,key_id in enumerate(OLD_IDS,1)}}
    require(type(read_rows) is list and len(read_rows)==8,'eight-complete-key-reads');values={};pins={};times=[create_row['sentAtMS'],create_row['receivedAtMS']]
    for row in read_rows:
        require(type(row) is dict and type(row.get('slot')) is str and row['slot'] in required and row['slot'] not in values,'unique-native-key-read-slot');slot=row['slot'];action,arn=required[slot]
        values[slot]=row_checked(m,prepared,intent_record,approved_create_body,row,'readback',slot,action,arn,explicit_now,key,creation_sha)
        require(create_row['receivedAtMS']<=row['sentAtMS'],'readback-after-actual-create-response');times.extend([row['sentAtMS'],row['receivedAtMS']]);pins[slot]=m.digest(row)
    new=values['new.DescribeKey'];exact(new,{'KeyMetadata'});require(m.same(new['KeyMetadata'],metadata),'whole-create-readback-metadata-match')
    policy=values['new.GetKeyPolicy'];exact(policy,{'Policy','PolicyName'});require(policy['PolicyName']=='default' and type(policy['Policy']) is str,'default-key-policy')
    policy=m.strict_json(policy['Policy'].encode());require(m.same(policy,intent_record['intent']['request']['Policy']),'whole-root-delegation-policy')
    rotation=values['new.GetKeyRotationStatus'];exact(rotation,{'KeyId','KeyRotationEnabled'});require(rotation=={'KeyId':key,'KeyRotationEnabled':False} and type(rotation['KeyRotationEnabled']) is bool,'rotation-disabled')
    aliases=values['new.ListAliases'];require(type(aliases) is dict and {'Aliases','Truncated'}<=set(aliases)<={'Aliases','Truncated','AliasCount'} and aliases['Aliases']==[] and aliases['Truncated'] is False
        and ('AliasCount' not in aliases or type(aliases['AliasCount']) is int and aliases['AliasCount']==0),'complete-zero-aliases')
    old={}
    for i,key_id in enumerate(OLD_IDS,1):
        row=values['old'+str(i)+'.DescribeKey'];exact(row,{'KeyMetadata'});arn=prefix+key_id
        require(m.same(row['KeyMetadata'],m.scheduled_key_closure(prepared['proposal'])[arn]['metadata']),'all-four-old-full-schedules-unchanged');old[arn]=row['KeyMetadata']
    require(m.instant(prepared['derivedAt'])<=m.instant(stamp(create_row['sentAtMS'])),'create-after-preparation')
    proof={'schema':'hikyo-158/next-created-key-custody/v1','observedAt':stamp(max(times)),'intent':copy.deepcopy(intent_record['intent']),
        'createResponse':{'KeyMetadata':metadata,'RequestId':create_row['requestId']},'readback':{'metadata':new['KeyMetadata'],'policy':policy,'rotationEnabled':False,'aliases':[],'Truncated':False},
        'preparationSHA256':m.digest(prepared),'sentAt':stamp(create_row['sentAtMS']),'receivedAt':stamp(create_row['receivedAtMS']),'claimId':intent_record['claimId'],'intentSHA256':intent_sha,'forwardsObserved':1}
    require(m.key_checked(proof,prepared['proposal'],explicit_now)==key,'model-key-proof')
    return {'schema':'hikyo-158/next-key-native-normalized-post-send/v1','key':proof,'oldKeys':old,
        'provenance':{'modelSHA256':MODEL_SHA,'modelTestSHA256':TEST_SHA,'createReceiptSHA256':creation_sha,'readReceiptSHA256s':pins,'earliestActualSentAtMS':min(times),'latestActualReceivedAtMS':max(times),
            'requestIdSource':'native-response-header:x-amzn-requestid'},'runtimeReady':False,'externalAuthenticityVerifiedByNormalizer':False,'providerAPIsCalledByNormalizer':False,'writeAuthorityGrantedByNormalizer':False}
