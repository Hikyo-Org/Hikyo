"""Pure same-configured-Root Chrome plan/prepare, plus explicit local publication.

No browser, provider, profile, process, credential, DB discovery or authority.
Runtime callers supply the current read-only native-event connection and exact
reviewed source binding. Source kind and hashes are not external authentication.
"""

from . import adapter_context
import copy
from pathlib import Path

from . import pair_collection
from . import persistent_admission as assembly
from .persistent_custody import Custody, source_binding_checked


PLAN_CAPTURES = {'ownerSource', 'identity'}
PREPARE_CAPTURES = {'iamBundle', 'kmsBundle', 'setupBundle', 'historicalRecovery'}
CAPTURE_KINDS = {'ownerSource': 'owner', 'identity': 'identity', 'iamBundle': 'iam',
                 'kmsBundle': 'kms', 'setupBundle': 'setup', 'historicalRecovery': 'historical'}


def _checked(root, bundle, binding, expected_pair):
    m = root.check_loaded(bundle)
    adapter_context.checked(m, binding)
    source_binding_checked(m, binding)
    pair_collection.expected_pair_checked(m, expected_pair)
    m.require(bundle[0]['model'] is m and assembly.ASSEMBLY_INPUTS == root.ASSEMBLY_INPUTS,
              'chrome-same-configured-Root-model-and-eleven-inputs')
    return m


def verify_captures(m, binding, captures, required, *, connection, sequence_min, sequence_max, explicit_now):
    m.exact(captures, required)
    custody = Custody(binding)
    results = {}
    for key, value in captures.items():
        m.exact(value, {'context', 'originalRaw'})
        m.require(value['context']['kind'] == CAPTURE_KINDS[key], 'chrome-exact-capture-kind')
        results[key] = custody.command_readout_checked(m, value['originalRaw'], value['context'],
            connection=connection, sequence_min=sequence_min, sequence_max=sequence_max, explicit_now=explicit_now)
    originals = [row['originalRawSHA256'] for row in results.values()]
    requests = [value['context']['request']['requestId'] for value in captures.values()]
    owner_sequences = [value['context']['request']['sequence'] for value in captures.values()]
    m.require(len(set(originals)) == len(originals) and len(set(requests)) == len(requests)
              and len(set(owner_sequences)) == len(owner_sequences), 'chrome-distinct-finite-command-originals')
    return results


def _owner_projection_checked(m,projection,captured,derivation):
    public=captured['readout']['value']
    m.exact(public,{'schema','pageURL','accountHeaderText','dashboardHeadingText','capturedAt'})
    m.exact(derivation,{'schema','publicOriginalRawSHA256','ownerProjectionSHA256',
        'genuineHumanOwnerOriginalRawSHA256','principalARNOrigin'})
    m.require(public['schema']=='hikyo-158/public-iam-owner-observation/v1'
        and public['dashboardHeadingText']=='IAM Dashboard'
        and all(public[k]==projection[k] for k in ('pageURL','accountHeaderText','capturedAt'))
        and projection['principalARN']==f'arn:aws:iam::{m.ACCOUNT}:root'
        and derivation['schema']=='hikyo-158/root-reviewed-owner-projection-derivation/v1'
        and derivation['publicOriginalRawSHA256']==captured['originalRawSHA256']
        and derivation['ownerProjectionSHA256']==m.digest(projection)
        and m.sha(derivation['genuineHumanOwnerOriginalRawSHA256'])
        and derivation['principalARNOrigin']=='Root-external-derivation-from-genuine-human-owner-verification',
        'chrome-observed-public-fields-and-explicit-external-Root-principal-derivation')


def configured_plan(root, bundle, owner_source, metadata_reference, historical_reference, *,
                    binding, expected_pair, owner_derivation, captures, connection, sequence_min, sequence_max, explicit_now):
    m = _checked(root, bundle, binding, expected_pair)
    checked = verify_captures(m, binding, captures, PLAN_CAPTURES, connection=connection,
        sequence_min=sequence_min, sequence_max=sequence_max, explicit_now=explicit_now)
    _owner_projection_checked(m,owner_source,checked['ownerSource'],owner_derivation)
    m.require(captures['ownerSource']['context']['identity']['targetURL'] == owner_source['pageURL']
              and checked['identity']['readout']['value'] is None,
              'chrome-whole-owner-projection-and-inert-historical-identity')
    m.require(checked['ownerSource']['originalSequence'] < checked['identity']['originalSequence']
              and captures['ownerSource']['context']['request']['sequence'] < captures['identity']['context']['request']['sequence']
              and m.instant(checked['ownerSource']['original']['completedAt'])
              <= m.instant(checked['identity']['original']['startedAt']), 'chrome-original-owner-before-preparation')
    return assembly.plan(bundle[0], owner_source, metadata_reference, historical_reference,
        chrome_command=captures['identity']['context'], original_raw=captures['identity']['originalRaw'],
        connection=connection, sequence_min=sequence_min, sequence_max=sequence_max, explicit_now=explicit_now,
        custody=Custody(binding))


def configured_prepare(root, bundle, inputs, *, binding, expected_pair, plan_captures, captures,
                       owner_derivation, connection, sequence_min, sequence_max, lease_start, lease_until, namespace, explicit_now):
    m = _checked(root, bundle, binding, expected_pair)
    adapter_context.get(m).forward(m, explicit_now, binding)
    m.exact(inputs, root.ASSEMBLY_INPUTS)
    m.require(inputs['code']['candidateCommit']['sha'] == expected_pair['candidateSHA']
              and inputs['code']['workflowCommit']['sha'] == expected_pair['mainSHA'],
              'chrome-pure-prepare-exact-reviewed-source-pair')
    root.require(m.same(inputs['proposal'], root.configured_unbound(bundle, inputs['proposal']['sourceManifest'])),
                 'same-configured-unbound-proposal')
    root.require(namespace == root.NAMESPACE, 'explicit-configured-c05-namespace')
    planned = verify_captures(m, binding, plan_captures, PLAN_CAPTURES, connection=connection,
        sequence_min=sequence_min, sequence_max=sequence_max, explicit_now=explicit_now)
    checked = verify_captures(m, binding, captures, PREPARE_CAPTURES, connection=connection,
        sequence_min=sequence_min, sequence_max=sequence_max, explicit_now=explicit_now)
    all_captures = {**plan_captures, **captures}
    all_checked = {**planned, **checked}
    m.require(len({v['context']['request']['requestId'] for v in all_captures.values()}) == len(all_captures)
              and len({v['context']['request']['sequence'] for v in all_captures.values()}) == len(all_captures)
              and len({v['originalSequence'] for v in all_checked.values()}) == len(all_checked),
              'chrome-no-cross-phase-original-or-request-reuse')
    ordered = sorted(all_captures, key=lambda key: all_captures[key]['context']['request']['sequence'])
    for before, after in zip(ordered, ordered[1:]):
        m.require(all_checked[before]['originalSequence'] < all_checked[after]['originalSequence']
                  and m.instant(all_checked[before]['original']['completedAt'])
                  <= m.instant(all_checked[after]['original']['startedAt']), 'chrome-owner-and-native-order-preserved')
    given = copy.deepcopy(inputs)
    _owner_projection_checked(m,given['ownerContext']['sourceReceipt'],planned['ownerSource'],owner_derivation)
    m.require(m.same(given['historicalPreparation']['chromeCommand'], plan_captures['identity']['context']),
              'chrome-original-owner-and-plan-command-not-replaced')
    for key in ('iamBundle', 'kmsBundle', 'setupBundle'):
        m.require(m.same(root.projected_rows(m,checked[key]['publicValue']),root.projected_rows(m,given[key])),
                  'chrome-byte-custody-for-whole-native-bundle')
    historical = checked['historicalRecovery']
    recovery = given['historicalRecovery']
    m.exact(recovery, {'schema', 'observedAt', 'storageKey', 'rawReceipt', 'storageReset', 'providerRequest'})
    m.require(recovery['schema'] == 'hikyo-158/exact-own-historical-metadata-reconciliation/v1'
              and recovery['observedAt'] == historical['original']['completedAt']
              and recovery['storageKey'] == 'hikyo158-historical-describe-metadata-' + m.digest(given['historicalPreparation'])
              and recovery['storageReset'] is False and recovery['providerRequest'] is False
              and type(recovery['rawReceipt']) is str
              and m.same(m.strict_json(recovery['rawReceipt'].encode()), historical['publicValue']),
              'chrome-exact-own-public-historical-recovery-projection')
    for key in ('iamBundle', 'setupBundle'):
        given[key] = root.projected_rows(m, given[key])
    root.checked_budget(m, given['budget'], explicit_now)
    owner = assembly.normalize_owner(bundle[0], given['proposal'], given['ownerContext'], given['capturePlan'],
        given['iamBundle'], given['kmsBundle'], given['setupBundle'], given['environmentCaptures'], explicit_now=explicit_now)
    history = assembly.historical_proof(bundle[0], given['historicalPreparation'], captures['historicalRecovery']['originalRaw'],
        chrome_command=captures['historicalRecovery']['context'], connection=connection,
        sequence_min=sequence_min, sequence_max=sequence_max, explicit_now=explicit_now,
        preparation_original_raw=plan_captures['identity']['originalRaw'], custody=Custody(binding))
    records = {'owner': owner['owner'], 'historicalAbsence': history, 'code': given['code'], 'budget': given['budget']}
    prepared = m.prepare(given['proposal'], records, {k:m.digest(v) for k,v in records.items()},
        lease_start=lease_start, lease_until=lease_until, namespace=namespace, explicit_now=explicit_now)
    adapter_context.get(m).prepared(m, prepared)
    return {adapter_context.FIELD: binding[adapter_context.FIELD], 'schema':'hikyo-158/inert-admission-assembly/v1', 'prepared':prepared,
            'normalizedOwner':owner, 'historicalAbsence':history,
            'inputSHA256s':{k:m.digest(v) for k,v in given.items()}, 'runtimeReady':False,
            'executionAuthorityGranted':False, 'providerAPIsCalled':False, 'fixtureReservationCreated':False}


def _capture_refs(root, m, rows, required, originals):
    m.exact(rows, required)
    captures = {}
    for key, row in rows.items():
        m.exact(row, {'context', 'original'})
        context = root.input_ref(m, row['context'], originals)
        m.exact(row['original'], {'path', 'rawSHA256'})
        raw = root.read_pinned(row['original']['path'], row['original']['rawSHA256'])
        path = str(Path(row['original']['path']).relative_to(root.E))
        root.require(path not in originals or originals[path] == raw, 'one-file-one-original')
        originals[path] = raw
        captures[key] = {'context':context, 'originalRaw':raw}
    return captures


def _custody_index(root, m, reference, phase, binding, expected_pair, originals):
    value = root.input_ref(m, reference, originals)
    keys = {adapter_context.FIELD,'schema','phase','sourceBindingSHA256','expectedPair','captures'}
    adapter_context.checked(m, value)
    if phase == 'prepare': keys.add('planCustodyIndex')
    else: keys.add('ownerDerivation')
    m.exact(value, keys)
    m.require(value['schema']=='hikyo-158/persistent-chrome-first-prepare-custody-index/v1'
              and value['phase']==phase and value['sourceBindingSHA256']==m.digest(binding)
              and m.same(value['expectedPair'],expected_pair), 'chrome-exact-reviewed-custody-index-and-source-pair')
    return value


def run_plan(root, bundle, index, index_raw, *, binding, expected_pair, custody_index_ref,
             folder, connection, sequence_min, sequence_max, clock):
    m = _checked(root,bundle,binding,expected_pair)
    root.require(m.same(index,m.strict_json(index_raw)), 'whole-original-plan-index')
    m.exact(index,root.PLAN_INPUTS)
    originals = {}
    values = {k:root.input_ref(m,v,originals) for k,v in index.items()}
    custody_index = _custody_index(root,m,custody_index_ref,'plan',binding,expected_pair,originals)
    captures = _capture_refs(root,m,custody_index['captures'],PLAN_CAPTURES,originals)
    now=clock();m.clock(now)
    result=configured_plan(root,bundle,values['ownerSource'],values['metadataReference'],values['historicalReference'],
        binding=binding,expected_pair=expected_pair,owner_derivation=custody_index['ownerDerivation'],captures=captures,connection=connection,
        sequence_min=sequence_min,sequence_max=sequence_max,explicit_now=now)
    folder=root.admission_folder(folder,must_exist=False)
    root.require(folder.name.split('-')[-2]==now.strftime('%Y%m%d'),'runtime-plan-folder-date')
    outputs={name:m.canonical(result[key]) for key,name in root.PLAN_FILES.items()}
    outputs.update({'complete-inert-plan.actual.json':m.canonical(result),'owner-source.actual.json':m.canonical(values['ownerSource']),
        'plan-input-index.original.json':index_raw,'plan-source-pins.actual.json':m.canonical(bundle[4]),
        'expected-pair.reviewed.json':m.canonical(expected_pair),'source-binding.reviewed.json':m.canonical(binding),
        'plan-custody-index.original.json':root.read_pinned(custody_index_ref['path'],custody_index_ref['rawSHA256'])})
    outputs.update(root.original_archive_outputs(originals))
    folder.mkdir(exist_ok=False);root.publish(folder,outputs)
    return result


def run_prepare(root, bundle, index, index_raw, *, binding, expected_pair, custody_index_ref,
                folder, collection_ref, key_ref, key_review_ref, connection, sequence_min, sequence_max,
                lease_start, lease_until, namespace, clock):
    m=_checked(root,bundle,binding,expected_pair)
    modules,_,_,_,sources=bundle
    folder=root.admission_folder(folder,must_exist=True)
    root.write_new(folder/'prepare-attempt.once.json',m.canonical({
        'schema':'hikyo-158/local-preparation-attempt/v1','inputIndexRawSHA256':root.raw_sha(index_raw),
        'reservationCreated':False,'resourceActionPerformed':False}))
    root.require(m.same(index,m.strict_json(index_raw)),'whole-original-prepare-index')
    m.exact(index,root.ASSEMBLY_INPUTS)
    root.require(index['proposal']=={'derive':'pinned-Root-inputs+whole-GitHub-sourceManifest'}
        and index['code']=={'derive':'whole-GitHub-collection.code'},'exact-proposal-code-derivation')
    originals={}
    m.exact(collection_ref,{'path','rawSHA256'})
    archive_path=root.plain_path(collection_ref['path'],root.E)
    root.require(archive_path.name=='whole-collection.actual.json','named-whole-github-collection')
    raw_archive=root.read_pinned(archive_path,collection_ref['rawSHA256'],limit=root.ARCHIVE_LIMIT)
    originals[str(archive_path.relative_to(root.E))]=raw_archive
    given={k:root.input_ref(m,v,originals) for k,v in index.items() if k not in ('proposal','code','environmentCaptures')}
    m.exact(index['environmentCaptures'],root.ENV_KEYS)
    given['environmentCaptures']={k:root.input_ref(m,v,originals) for k,v in index['environmentCaptures'].items()}
    for key,leaf in (('ownerContext','owner-context.actual.json'),('capturePlan','metadata-capture-plan.actual.json'),
                     ('historicalPreparation','historical-read-preparation.actual.json')):
        root.require(Path(index[key]['path'])==folder/leaf,'exact-own-plan-input-leaf')
    m.require(m.same(root.input_ref(m,{'path':str(folder/'expected-pair.reviewed.json'),
                  'rawSHA256':root.raw_sha(m.canonical(expected_pair))},originals),expected_pair)
              and m.same(root.input_ref(m,{'path':str(folder/'source-binding.reviewed.json'),
                  'rawSHA256':root.raw_sha(m.canonical(binding))},originals),binding),'chrome-same-plan-source-binding-and-pair')
    custody_index=_custody_index(root,m,custody_index_ref,'prepare',binding,expected_pair,originals)
    root.require(Path(custody_index['planCustodyIndex']['path'])==folder/'plan-custody-index.original.json',
                 'chrome-exact-own-plan-custody-index')
    plan_custody_index=_custody_index(root,m,custody_index['planCustodyIndex'],'plan',binding,expected_pair,originals)
    plan_captures=_capture_refs(root,m,plan_custody_index['captures'],PLAN_CAPTURES,originals)
    captures=_capture_refs(root,m,custody_index['captures'],PREPARE_CAPTURES,originals)
    request=root.input_ref(m,key_ref,originals);review=root.input_ref(m,key_review_ref,originals)
    now=clock();m.clock(now)
    view=pair_collection.collection_checked(root,bundle,raw_archive,now,expected_pair=expected_pair)
    source_manifest=view['sourceManifest'];proposal=root.configured_unbound(bundle,source_manifest)
    root.checked_budget(m,given['budget'],now)
    request=root.exact_key_request(m,request,review,key_ref['rawSHA256'])
    given.update(proposal=proposal,code=view['code']);m.exact(given,root.ASSEMBLY_INPUTS)
    root.require(namespace==root.NAMESPACE,'explicit-configured-c05-namespace')
    result=configured_prepare(root,bundle,given,binding=binding,expected_pair=expected_pair,
        plan_captures=plan_captures,captures=captures,owner_derivation=plan_custody_index['ownerDerivation'],connection=connection,
        sequence_min=sequence_min,sequence_max=sequence_max,lease_start=lease_start,
        lease_until=lease_until,namespace=namespace,explicit_now=now)
    prepared=result['prepared'];m.preparation_checked(prepared)
    root.require(m.same(prepared['proposal'],proposal),'same-configured-unbound-proposal')
    records=prepared['records'];wire=modules['operator'].encode_tool_wire(m,records,'phase-records')
    outputs={'admission-assembly.actual.json':m.canonical(result),'prepared.actual.json':m.canonical(prepared),
        'baseline-records.actual.json':m.canonical(records),'intended-semantic-request.actual.json':m.canonical(request),
        'prepare-input-index.original.json':index_raw,
        'prepare-custody-index.original.json':root.read_pinned(custody_index_ref['path'],custody_index_ref['rawSHA256']),
        'preparation-source-pins.actual.json':m.canonical({'sources':sources,'configurationRawSHA256':root.CONFIG_SHA,
            'proposalRawSHA256':root.PROPOSAL_SHA,'collection':collection_ref,'sourceManifest':source_manifest,
            'expectedPair':expected_pair,'sourceBindingSHA256':m.digest(binding),
            'custodyIndexRawSHA256':custody_index_ref['rawSHA256'],'inputIndexRawSHA256':root.raw_sha(index_raw),
            'originalInputs':{name:root.raw_sha(raw) for name,raw in originals.items()}})}
    for name,raw in root.original_archive_outputs(originals).items():
        if (folder/name).exists():
            root.require(root.read_pinned(folder/name,root.raw_sha(raw),limit=root.ARCHIVE_LIMIT)==raw,
                         'unchanged-already-archived-plan-original')
        else:outputs[name]=raw
    end=clock();m.clock(end)
    root.require(now<=end<m.instant(lease_until),'finite-monotonic-preparation-clock')
    m.ci_checked(records['code'],source_manifest,end);m.owner_checked(records['owner'],proposal,end)
    root.checked_budget(m,records['budget'],end)
    m.historical_absence_checked(records['historicalAbsence'],end,m.digest(given['ownerContext']))
    root.publish(folder,outputs)
    return wire
