"""Deterministic bounded derivation from explicitly supplied, pinned originals.

No imports of original launchers. No release/configuration data enters output.
"""
import argparse
import ast
import hashlib
import json
from pathlib import Path
import re

HERE = Path(__file__).resolve().parent
PACKAGES = {
    'A': 'chrome-first-prepare-custody-integration-source',
    'H': 'chrome-persistent-create-lock-runner-source',
    'N': 'chrome-native-create-stage-source',
    'D': 'chrome-native-paired-driver-source',
    'K': 'chrome-new-key-cleanup-source',
    'U': 'chrome-reviewed-aws-operator-source',
}
FILES = {
    'U': ['reviewed-scripts/kms-guard-install-unarmed-v1.js'],
    'A': ['root_first_prepare.source-only.py', 'chrome_first_prepare.py', 'persistent_admission.py', 'persistent_custody.py',
          'pair_collection.py', 'ancestor_chrome_custody.py', 'native_carrier.py',
          'transport_receipt.py', 'registry.fragment.json',
          'reviewed-scripts/aws-owner-readout-v1.js', 'reviewed-scripts/aws-identity-readout-v1.js',
          'reviewed-scripts/aws-historical-readout-v1.js', 'reviewed-scripts/aws-create-readout-v1.js',
          'reviewed-scripts/aws-iam-readout-v1.js', 'reviewed-scripts/aws-kms-readout-v1.js',
          'reviewed-scripts/aws-setup-readout-v1.js', 'reviewed-scripts/aws-navigate-v1.js'],
    'H': ['create_holder.py', 'runner.py', 'create_custody.py', 'local_files.py',
          'pipe_io.py', 'runtime_binding.py', 'aws-create-readout-v1.js', 'create-registry.json', 'TRANSPORT-CODE-PINS.json'],
    'N': ['preflight.py', 'root_preflight.py', 'stage_custody.py', 'stage-kernel.cjs',
          'guarded-document.cjs', 'shared-window.cjs', 'native-guard.source-only.js',
          'native-observer.source-only.js', 'build.cjs', 'U-GUARD-REGISTRY.json', 'vm-fixture.cjs'],
    'D': ['paired_core.py', 'driver_data.py', 'source_gate.py', 'paired_custody.py',
          'child_transport.py', 'driver_native_carrier.py'],
    'K': ['root_connection.py', 'k_cleanup_custody.py', 'k_native_carrier.py',
          'k_transport_receipt.py', 'binding.cjs', 'page-operation.cjs', 'public-dom.cjs',
          'operator.js.txt', 'native-pins.json', 'build.cjs', 'fixture.cjs', 'prepare-from-ticket.mjs',
          'dependencies/native_partial_kms_deletion_prepare.source-only.mjs',
          'dependencies/shared-window.cjs', 'dependencies/native_partial_kms_deletion_guard.source-only.js',
          'dependencies/native_next_key_custody_prearm_observer.source-only.js'],
}


def replace(source, old, new, count=1):
    if source.count(old) != count:
        raise ValueError('adapter-derive:changed-original-edit-anchor:' + old[:100])
    return source.replace(old, new)


def function_span(source, name):
    node = next(n for n in ast.parse(source).body if isinstance(n, (ast.FunctionDef, ast.AsyncFunctionDef)) and n.name == name)
    lines = source.splitlines(keepends=True)
    return ''.join(lines[node.lineno-1:node.end_lineno])


def imports(source):
    locals_ = {Path(f).stem for files in FILES.values() for f in files if f.endswith('.py')}
    for name in sorted(locals_, key=len, reverse=True):
        source = re.sub(r'^import ' + re.escape(name) + r'( as \w+)?$',
                        lambda m: 'from . import ' + name + (m.group(1) or ''), source, flags=re.M)
        source = re.sub(r'^from ' + re.escape(name) + r' import ', 'from .' + name + ' import ', source, flags=re.M)
    # Insert below the module docstring, preserving genuine __file__ locations.
    tree = ast.parse(source)
    line = tree.body[0].end_lineno if isinstance(tree.body[0], ast.Expr) and isinstance(tree.body[0].value, ast.Constant) and isinstance(tree.body[0].value.value, str) else 0
    rows = source.splitlines(keepends=True)
    rows.insert(line, '\nfrom . import adapter_context\n')
    return ''.join(rows)


def custody(source):
    source = replace(source, "BINDING_KEYS = {'schema',", "BINDING_KEYS = {'attemptTrustBindingSHA256', 'schema',")
    source = replace(source, "CONTEXT_KEYS = {'schema',", "CONTEXT_KEYS = {'attemptTrustBindingSHA256', 'schema',")
    source = replace(source, '    m.exact(binding, BINDING_KEYS)',
                     '    adapter_context.checked(m, binding)\n    m.exact(binding, BINDING_KEYS)')
    # Keep the R2 request envelope unchanged. The outer command's exact bytes
    # contain the binding in script arguments, checked by the reviewed registry.
    source = replace(source, "arguments = {'expected': copy.deepcopy(identity)}", "arguments = {'expected': copy.deepcopy(identity), adapter_context.FIELD: binding[adapter_context.FIELD]}")
    if "m.exact(arguments, {'expected'})" in source:
        source = replace(source, "m.exact(arguments, {'expected'})", "m.exact(arguments, {'expected', adapter_context.FIELD})\n    adapter_context.checked(m, arguments)")
    else:
        source = replace(source, "m.exact(arguments, {'expected','manifest'} if kind in ('iam','kms','setup') else {'expected'})", "m.exact(arguments, {adapter_context.FIELD, 'expected','manifest'} if kind in ('iam','kms','setup') else {adapter_context.FIELD, 'expected'})\n    adapter_context.checked(m, arguments)")
    source = replace(source, "return {'schema': 'hikyo-158/persistent-chrome", "return {adapter_context.FIELD: binding[adapter_context.FIELD], 'schema': 'hikyo-158/persistent-chrome")
    source = replace(source, "return {'original': original,", "return {adapter_context.FIELD: command[adapter_context.FIELD], 'original': original,")
    return source


def derive(package, leaf, source):
    if package == 'U' and leaf.endswith('.js'):
        return re.sub(r'(?m)^[ \t]+$', '', source)
    if leaf.endswith('.py'):
        source = imports(source)
        source = source.replace('m.digest(m.configuration_checked())', 'm.digest(m.release_configuration_checked())')
        source = source.replace('m.digest(config)', 'm.digest(m.release_configuration_checked())')
    if leaf in ('persistent_custody.py', 'create_custody.py'):
        return custody(source)
    if leaf == 'root_first_prepare.source-only.py':
        tree = ast.parse(source)
        keep_constants = {'LIMIT','ARCHIVE_LIMIT','PLAN_FILES','PLAN_INPUTS','ASSEMBLY_INPUTS','ENV_KEYS'}
        keep_functions = {'require','raw_sha','plain_path','read_pinned','check_loaded','strict_archive',
                          'projected_rows','checked_budget','exact_key_request','admission_folder',
                          'write_new','input_ref','original_archive_outputs','publish','configured_unbound'}
        chunks = []
        for node in tree.body:
            if isinstance(node, ast.Assign) and any(isinstance(t, ast.Name) and t.id in keep_constants for t in node.targets):
                chunks.append(ast.get_source_segment(source,node))
            elif isinstance(node, ast.FunctionDef) and node.name in keep_functions:
                chunks.append(ast.get_source_segment(source,node))
        source = '\n\n'.join(chunks) + '\n'
        source = '"Portable original A Root input/publication checks, no legacy loader."\nimport copy\nfrom decimal import Decimal\nimport hashlib\nimport json\nimport math\nimport os\nfrom pathlib import Path\nimport re\nimport stat\nfrom . import adapter_context\nE = None\nNAMESPACE = None\nPROPOSAL_SHA = None\nCONFIG_SHA = None\n_ORIGINALS = None\n\n' + source
        source = replace(source, "    config = m.configuration_checked()", "    adapter_context.get(m)\n    config = m.configuration_checked()")
        source += '\n\ndef make_bundle(*args, **kwargs):\n    import sys\n    from .prepare_root_bindings import bind\n    return bind(sys.modules[__name__], *args, **kwargs)\n\n\ndef collection_checked(*args, **kwargs):\n    import sys\n    from .prepare_root_bindings import collection\n    return collection(sys.modules[__name__], *args, **kwargs)\n'
        return source
    if leaf == 'chrome_first_prepare.py':
        source = replace(source, 'archive=pair_collection.collection_checked(root,bundle,raw_archive,now,expected_pair=expected_pair)', 'view=pair_collection.collection_checked(root,bundle,raw_archive,now,expected_pair=expected_pair)')
        source = replace(source, "source_manifest=archive['sourceManifest']", "source_manifest=view['sourceManifest']")
        source = replace(source, "code=archive['code']", "code=view['code']")
        source = replace(source, '    m = root.check_loaded(bundle)', '    m = root.check_loaded(bundle)\n    adapter_context.checked(m, binding)', count=1)
        source = replace(source, "    m.exact(inputs, root.ASSEMBLY_INPUTS)", "    adapter_context.get(m).forward(m, explicit_now, binding)\n    m.exact(inputs, root.ASSEMBLY_INPUTS)")
        source = replace(source, "    return {'schema':'hikyo-158/inert-admission-assembly/v1',", "    adapter_context.get(m).prepared(m, prepared)\n    return {adapter_context.FIELD: binding[adapter_context.FIELD], 'schema':'hikyo-158/inert-admission-assembly/v1',")
        source = replace(source, "keys = {'schema','phase','sourceBindingSHA256','expectedPair','captures'}", "keys = {adapter_context.FIELD,'schema','phase','sourceBindingSHA256','expectedPair','captures'}\n    adapter_context.checked(m, value)")
        return source
    if leaf == 'persistent_admission.py':
        # Only the obsolete model member changes. The six actual helper pins
        # and public source pins remain the originals.
        start = source.index('PINS = ')
        end = source.index('\nSM_PINS = ', start)
        pins = ast.literal_eval(source[start+len('PINS = '):end])
        pins.pop('model')
        source = source[:start] + 'PINS = ' + repr(pins) + source[end:]
        return source
    if leaf == 'pair_collection.py':
        # The collector owns its closed successor v3 schema. Do not reinterpret
        # an old v2 archive as reusable-release evidence.
        start = source.index('def collection_checked(')
        source = source[:start] + '''def collection_checked(root, bundle, raw, now, *, expected_pair):
    m = root.check_loaded(bundle)
    context = adapter_context.get(m)
    expected_pair_checked(m, expected_pair)
    m.require(expected_pair['candidateSHA'] == context.binding['candidateHeadSHA']
              and expected_pair['mainSHA'] == context.binding['mainSHA'],
              'same-reviewed-attempt-candidate-main-pair')
    # Parent supplies the actual successor collector revalidation, never a v2
    # field-set bypass or a summary masquerading as collection originals.
    view = root.collection_checked(bundle, raw, now, expected_pair=expected_pair)
    m.exact(view, {adapter_context.FIELD, 'schema', 'sourceManifest', 'code'})
    context.checked(m, view)
    m.require(view['schema'] == 'hikyo-1046/configured-adapter-pair-view/v1'
              and m.same(view['sourceManifest'], context.manifest), 'same-attempt-collected-manifest')
    m.ci_checked(view['code'], context.manifest, now)
    context.forward(m, now)
    return view
'''
        return source
    if leaf == 'create_holder.py':
        source = replace(source, "CONTEXT_KEYS = {'schema',", "CONTEXT_KEYS = {'attemptTrustBindingSHA256', 'schema',")
        source = replace(source, '    config = m.configuration_checked()', "    adapter_context.checked(m, context, full_intent, forward_contract, forward_contract['ticket'])\n    config = m.configuration_checked()")
        source = replace(source, '    m.preparation_checked(prepared)', '    adapter_context.get(m).prepared(m, prepared)')
        source = replace(source, "expected_claim_keys = {'schema',", "expected_claim_keys = {adapter_context.FIELD, 'schema',")
        source = replace(source, '        m.exact(claim, expected_claim_keys)', '        adapter_context.checked(m, claim)\n        m.exact(claim, expected_claim_keys)')
        source = replace(source, "m.exact(state, {'claimSHA256',", "m.exact(state, {adapter_context.FIELD, 'claimSHA256',")
        source = replace(source, "        m.require(state['claimSHA256']", "        adapter_context.checked(m, state)\n        m.require(state['claimSHA256']")
        source = replace(source, '        checked = deadline.recheck(', '        m.admit_current_release(prepared, now)\n        adapter_context.get(m).forward(m, now, context, claim, state, full_intent, forward_contract)\n        checked = deadline.recheck(')
        source = replace(source, "record = {'schema':", "record = {adapter_context.FIELD: context[adapter_context.FIELD], 'schema':")
        source = replace(source, "    checked = custody.command_readout_checked", "    adapter_context.checked(m, context, window, full_intent)\n    adapter_context.get(m).prepared(m, prepared)\n    checked = custody.command_readout_checked")
        source = replace(source, "    return {'keyARN':", "    return {adapter_context.FIELD: context[adapter_context.FIELD], 'keyARN':")
        source = replace(source, "result = {'schema':", "result = {adapter_context.FIELD: context[adapter_context.FIELD], 'schema':")
        return source
    if leaf == 'runner.py':
        source = replace(source, '    m.exact(values, INPUTS)', "    context = adapter_context.checked(m, values['lockContext'], values['sourceBinding'], values['fullIntent'], values['forwardContract'], values['forwardContract']['ticket'])\n    context.prepared(m, values['prepared'])\n    m.exact(values, INPUTS)")
        source = replace(source, "return {'schema': 'hikyo-158/persistent-create-holder-review/v1',", "return {adapter_context.FIELD: context.binding_sha256, 'schema': 'hikyo-158/persistent-create-holder-review/v1',")
        source, count = re.subn(r"'nativeStore': '[^']+',", "'nativeStore': str(context.native_store),", source)
        if count != 1:
            raise ValueError('adapter-derive:one-explicit-native-store')
        source = replace(source, "m.exact(window, {'schema',", "adapter_context.checked(m, window, review, review['lockContext'])\n    m.exact(window, {adapter_context.FIELD, 'schema',")
        for name in ('initialize', 'endpoints'):
            signature = function_span(source, name).splitlines()[0]
            source = replace(source, signature, signature + '\n    adapter_context.checked(m, review, review[\'lockContext\'])')
        source = replace(source, "output = {'schema':", "output = {adapter_context.FIELD: review[adapter_context.FIELD], 'schema':")
        source = replace(source, "failure = {'schema':", "failure = {adapter_context.FIELD: review[adapter_context.FIELD], 'schema':")
        source = replace(source, "descriptor = {'schema':", "descriptor = {adapter_context.FIELD: review[adapter_context.FIELD], 'schema':")
        source = replace(source, "m.exact(descriptor, {'schema',", "adapter_context.checked(m, descriptor)\n    m.exact(descriptor, {adapter_context.FIELD, 'schema',")
        source = replace(source, "submitted = {'schema':", "submitted = {adapter_context.FIELD: review[adapter_context.FIELD], 'schema':")
        return source
    if leaf == 'runtime_binding.py':
        start = source.index('HERE = ')
        end = source.index('def runtime_binding_checked(', start)
        source = source[:start] + "HERE = Path(__file__).resolve().parent\nNODE_SHA = 'a9bd0630891c2dcdee70de88270fee2cc0c4a9e76495039dd3b4f91c5e6b71df'\n\n\n" + source[end:]
        source = replace(source, '    source_binding_checked(m, binding)', '    context = adapter_context.checked(m, binding)\n    source_binding_checked(m, binding)\n    PERSISTENT, NODE = context.transport_root, context.node_path')
        source = replace(source, "    snapshot = (HERE / 'persistent-SOURCE-CONTRACT.snapshot.json').read_bytes()\n    require(contract_raw == snapshot, 'exact-snapshotted-contract-no-final-adoption-inference')", "    require(sha(contract_raw) == 'a8dbe3b2736d741b7a84efcad5428869f8d37f8b17c60469bdb0e9d9e6219347', 'exact-snapshotted-contract-no-final-adoption-inference')")
        return source
    if leaf == 'preflight.py':
        source = replace(source, "    m.require(phase in", "    trust = adapter_context.checked(m, review, values['lockContext'], values['sourceBinding'], values['fullIntent'], values['forwardContract'], values['forwardContract']['ticket'], claim, state, intent_record, ticket_record, locked_window, status_context)\n    trust.prepared(m, values['prepared'])\n    m.require(phase in")
        source = replace(source, "m.strict_json((HERE / 'STAGE-SOURCE-PINS.json').read_bytes())", 'trust.stage_pins()')
        source = replace(source, "m.exact(claim, {'schema',", "m.exact(claim, {adapter_context.FIELD, 'schema',")
        source = replace(source, "m.exact(state, {'claimSHA256',", "m.exact(state, {adapter_context.FIELD, 'claimSHA256',")
        source = replace(source, '    current = deadline.recheck(', '    m.admit_current_release(prepared, explicit_now)\n    trust.forward(m, explicit_now, claim, state, intent_record, ticket_record)\n    current = deadline.recheck(')
        source = replace(source, "return {'schema': 'hikyo-158/native-create-stage-preflight/v1',", "return {adapter_context.FIELD: trust.binding_sha256, 'schema': 'hikyo-158/native-create-stage-preflight/v1',")
        return source
    if leaf == 'stage_custody.py':
        source = replace(source, "'review-manifest.fragment.json'", "'stage-registry.json'")
        source = replace(source, '    h_custody.source_binding_checked(m, binding)', '    adapter_context.checked(m, binding)\n    h_custody.source_binding_checked(m, binding)')
        source = replace(source, "keys = {'expected', 'window'}", "keys = {adapter_context.FIELD, 'expected', 'window'}")
        source = replace(source, '    m.exact(arguments, keys)', '    m.exact(arguments, keys)\n    adapter_context.checked(m, arguments)\n    if kind in (\'arm\', \'finish\'):\n        adapter_context.checked(m, arguments[\'descriptor\'])')
        source = replace(source, "return {'schema': 'hikyo-158/native-create-stage-command/v1',", "return {adapter_context.FIELD: binding[adapter_context.FIELD], 'schema': 'hikyo-158/native-create-stage-command/v1',")
        source = replace(source, "    return {'original': original,", "    return {adapter_context.FIELD: context[adapter_context.FIELD], 'original': original,")
        return source
    if leaf == 'root_preflight.py':
        # Preserve the actual lock probe and readonly journal/claim reader.
        # Runtime discovery and old fixed G-loader CLI are replaced by the
        # explicit configured parent API in n_runtime.py.
        start = source.index('def physical_exclusion(')
        end = source.index('\n\ndef main(', start)
        source = '"""Original N physical lock/journal/registry checks, explicit parent wiring."""\nfrom . import adapter_context\nimport fcntl\nimport os\nfrom pathlib import Path\nimport stat\nfrom . import local_files as files\nfrom . import runtime_binding\nHERE = Path(__file__).resolve().parent\n\n\n' + source[start:end] + '\n'
        source = replace(source, '    return {\'claim\': claim,', "    adapter_context.checked(m, claim, state)\n    return {'claim': claim,")
        source = replace(source, "runtime_binding.PERSISTENT / row['file']", "adapter_context.get(m).transport_root / row['file']")
        return source
    if leaf == 'driver_data.py':
        source = re.sub(r"^B = HERE.parent\nE = B.parent\nN = B / .*\nN_SHA = .*\nN_REGISTRY_SHA = .*\n", 'N = HERE\n', source, flags=re.M)
        source = replace(source, "exact(plan,{'schema',", "context=adapter_context.get()\n    context.checked(context.model,plan,plan['action'])\n    exact(plan,{adapter_context.FIELD,'schema',")
        source = source.replace('==N_SHA', "==context.source_sha('adapters/preflight.py')")
        source = replace(source, "path.is_relative_to(E)", 'path.is_relative_to(context.runtime_root)')
        source = replace(source, "exact(a,{'requestId',", "exact(a,{adapter_context.FIELD,'requestId',")
        # Carrier is an exact public command description. The configured parent
        # supplies the executable wrapper; no hidden old runtime loader survives.
        return source
    if leaf == 'paired_core.py':
        source = replace(source, "'nativeStageSourceSHA256':d.N_SHA", "'nativeStageSourceSHA256':adapter_context.get(m).source_sha('adapters/preflight.py')")
        source = replace(source, "receipt={'schema':", "receipt={adapter_context.FIELD:adapter_context.get(m).binding_sha256,'schema':")
        source = replace(source, '            verify();d.plan_checked(plan)', "            adapter_context.checked(m,plan,values['sourceBinding'],values['lockContext'],status_context)\n            adapter_context.get(m).prepared(m,values['prepared'])\n            verify();d.plan_checked(plan)")
        source = replace(source, '            receipt[\'preflightDescriptor\']=descriptor;', "            adapter_context.checked(m,descriptor)\n            receipt['preflightDescriptor']=descriptor;")
        source = replace(source, "'expected':a['expectedIdentity'],'window':a['sharedWindow']", "adapter_context.FIELD:plan[adapter_context.FIELD],'expected':a['expectedIdentity'],'window':a['sharedWindow']")
        return source
    if leaf == 'paired_custody.py':
        source = replace(source, "KEYS={'schema',", "KEYS={adapter_context.FIELD,'schema',")
        source = replace(source, "    original,sequence=", "    adapter_context.checked(m,binding,plan,plan['action'])\n    original,sequence=")
        source = replace(source, 'd.exact(receipt,KEYS)', 'd.exact(receipt,KEYS);adapter_context.checked(m,receipt)')
        source = source.replace('==d.N_SHA', "==adapter_context.get(m).source_sha('adapters/preflight.py')")
        source = replace(source, "    a=plan['action']", "    adapter_context.checked(m,desc)\n    a=plan['action']")
        source = replace(source, "arguments={'expected':", "arguments={adapter_context.FIELD:plan[adapter_context.FIELD],'expected':")
        source = replace(source, "return {'schema':'hikyo-158/native-paired-parent-custody/v1'", "return {adapter_context.FIELD:plan[adapter_context.FIELD],'schema':'hikyo-158/native-paired-parent-custody/v1'")
        return source
    if leaf == 'child_transport.py':
        source, count = re.subn(r"argv\[0\]=='[^']+'", "argv[0]==str(adapter_context.get().node_path)", source)
        if count != 1:
            raise ValueError('adapter-derive:one-reviewed-node-argument')
        source = replace(source, "str(d.B/'chrome-capture-suppressed-persistent-owner-source/client.cjs')", "str(adapter_context.get().transport_root/'client.cjs')")
        source = replace(source, "cwd=str(d.B/'chrome-capture-suppressed-persistent-owner-source')", "cwd=str(adapter_context.get().transport_root)")
        return source
    if leaf == 'source_gate.py':
        start = source.index('def verify(')
        source = source[:start] + '''def verify(pin):
    context = adapter_context.get()
    manifest = context.source_check()
    d.require(pin == context.source_sha('adapters/paired_core.py'), 'exact-reviewed-paired-driver-source')
    return manifest


def imports():
    from . import root_preflight, stage_custody
    adapter_context.get().source_check()
    return root_preflight, stage_custody
'''
        return source
    if leaf == 'root_connection.py':
        for name, call in [('reconcile','reconcile(proof, authority)'),('arm','arm(OPERATION, proof, authority)'),('ticket','ticket(OPERATION, proof, authority)'),('accept','accept(OPERATION, receipt_proof, authority)')]:
            source = replace(source, '    return coordinator.' + call,
                             '    _coordinator_binding(coordinator)\n    return coordinator.' + call)
        start = source.index('def _custody():')
        end = source.index('\n\ndef reviewed_command_context', start)
        source = source[:start] + '''def _custody():
    from . import k_cleanup_custody
    return k_cleanup_custody


def _coordinator_binding(coordinator):
    m = coordinator.m
    context = adapter_context.get(m)
    context.prepared(m, coordinator.prepared)
    # Reconstruction uses original action time and original reserved binding;
    # current release eligibility is deliberately not a cleanup prerequisite.
    with coordinator.original.lock():
        claim = coordinator.original.claim_record()
        context.checked(m, claim)
        action_at = m.instant(claim['reservedAt'])
        context.cleanup(m, action_at, coordinator.now(), claim, coordinator.prepared)
''' + source[end:]
        source = replace(source, "    source = copy.deepcopy(original_guard_row)", "    adapter_context.checked(model, intent, ticket)\n    source = copy.deepcopy(original_guard_row)")
        source = replace(source, "return {'schema': 'hikyo-158/partial-cleanup-response/v1'", "return {adapter_context.FIELD: intent[adapter_context.FIELD], 'schema': 'hikyo-158/partial-cleanup-response/v1'")
        source = replace(source, "    inputs = ticket['nativeHelperInput']", "    adapter_context.get(model).prepared(model, prepared)\n    adapter_context.checked(model, original_create_intent, ticket)\n    inputs = ticket['nativeHelperInput']")
        source = replace(source, "    inputs = copy.deepcopy(unchanged_full_inputs)", "    adapter_context.get(model).prepared(model, prepared)\n    inputs = copy.deepcopy(unchanged_full_inputs)")
        source = replace(source, "return {'schema': 'hikyo-158/new-key-cleanup-fresh-readback-plan/v1'", "return {adapter_context.FIELD: expected[adapter_context.FIELD], 'schema': 'hikyo-158/new-key-cleanup-fresh-readback-plan/v1'")
        return source
    if leaf == 'k_cleanup_custody.py':
        start = source.index('    module = types.ModuleType(')
        end = source.index('    return module', start) + len('    return module')
        source = source[:start] + "    import importlib\n    return importlib.import_module('.' + Path(leaf).stem, __package__)" + source[end:]
        source = replace(source, "'source-contract.json'", "'SOURCE-CONTRACT.json'")
        source = replace(source, "m.exact(intent, {'schema',", "adapter_context.checked(m, intent, ticket, ticket['requiredNativePreparation'])\n    m.exact(intent, {adapter_context.FIELD, 'schema',")
        source = replace(source, "m.exact(ticket, {'schema',", "m.exact(ticket, {adapter_context.FIELD, 'schema',")
        source = replace(source, "m.exact(binding, {'schema',", "adapter_context.checked(m, binding)\n    m.exact(binding, {adapter_context.FIELD, 'schema',")
        source = replace(source, "return {'schema': 'hikyo-158/cleanup-reviewed-command-context/v1'", "return {adapter_context.FIELD: binding[adapter_context.FIELD], 'schema': 'hikyo-158/cleanup-reviewed-command-context/v1'")
        # Runtime source/review manifests include the new reviewed scripts;
        # actual source reads verify the unchanged transport code separately.
        source = replace(source, "        m.require(m.sha(binding[key]['rawSHA256']) and (key not in SOURCE_PINS", "        m.require(m.sha(binding[key]['rawSHA256']) and (key in ('transportSource', 'reviewManifest') or key not in SOURCE_PINS")
        return source
    if package == 'N' and leaf == 'vm-fixture.cjs':
        source = replace(source, "const owner=path.join(__dirname,'../chrome-capture-suppressed-persistent-owner-source');", "if(!process.argv[2]||!path.isAbsolute(process.argv[2]))throw Error('explicit-pinned-test-transport-root-required');\nconst owner=process.argv[2];")
        source = replace(source, "'review-manifest.fragment.json'", "'stage-registry.json'")
        source = replace(source, "path.join(__dirname,'../chrome-reviewed-aws-operator-source',entry.file)", "path.join(__dirname,entry.file)")
        source = replace(source, 'const input=compile(z,entry.arguments).parse(args);', "const input=compile(z,entry.arguments).parse(kind==='guard'?args:{attemptTrustBindingSHA256:f.input.attemptTrustBindingSHA256,...args});")
        source = replace(source, "'TrentService.ListAliases'", "'TrentService.ListKeys'")
        return source
    if package == 'K' and leaf == 'fixture.cjs':
        source = replace(source, "const intent={schema:", "const intent={attemptTrustBindingSHA256:options.attemptTrustBindingSHA256,schema:")
        source = replace(source, "const ticket={schema:", "const ticket={attemptTrustBindingSHA256:options.attemptTrustBindingSHA256,schema:")
        return source
    if leaf == 'stage-kernel.cjs':
        source = replace(source, "d.schema!=='hikyo-158/native-create-stage-preflight/v1'", "typeof d.attemptTrustBindingSHA256!=='string'||!/^[a-f0-9]{64}$/u.test(d.attemptTrustBindingSHA256)||d.schema!=='hikyo-158/native-create-stage-preflight/v1'")
        # Bind the installed page capability to the first validated descriptor;
        # second phase cannot substitute a release/attempt while keeping windows.
        source = replace(source, 'function createNativeStage(globalObject, window, manifest, pins, checkSharedWindow)', 'function createNativeStage(globalObject, window, manifest, pins, checkSharedWindow, attemptTrustBindingSHA256)')
        source = replace(source, 'let held=false,installed=false,armAttempted=false,armed=false,finishAttempted=false;', 'let held=false,installed=false,armAttempted=false,armed=false,finishAttempted=false;const attemptBinding=attemptTrustBindingSHA256;')
        source = replace(source, '  function descriptor(d,phase,expected) {', "  function descriptor(d,phase,expected) {\n    if(attemptBinding!==null&&attemptBinding!==d.attemptTrustBindingSHA256)fail();\n    if(typeof d.attemptTrustBindingSHA256!=='string'||!/^[a-f0-9]{64}$/u.test(d.attemptTrustBindingSHA256))fail();")
        return source
    if package == 'N' and leaf == 'build.cjs':
        source = replace(source, ' ${body}\n', '${body}\n')
        start = source.index('const sourcePins=')
        end = source.index('\nconst ident=', start)
        source = source[:start] + "const sourcePins=JSON.parse(fs.readFileSync(path.join(root,'STAGE-SOURCE-PINS.json'),'utf8'));" + source[end:]
        source = replace(source, "const descriptor=obj({schema:", "const descriptor=obj({attemptTrustBindingSHA256:hash,schema:")
        source = replace(source, "arguments:obj({expected:ident,window,...args})", "arguments:obj({attemptTrustBindingSHA256:hash,expected:ident,window,...args})") if "arguments:obj({expected:ident,window,...args})" in source else source
        source = replace(source, ' try {', " try {\n if(typeof input.attemptTrustBindingSHA256!=='string'||!/^[a-f0-9]{64}$/u.test(input.attemptTrustBindingSHA256)||input.descriptor&&input.descriptor.attemptTrustBindingSHA256!==input.attemptTrustBindingSHA256)fail();")
        source = replace(source, 'async ({window,manifest,chunks})=>', 'async ({window,manifest,chunks,attemptTrustBindingSHA256})=>')
        source = replace(source, '${JSON.stringify(sourcePins)},checkSharedWindow);', '${JSON.stringify(sourcePins)},checkSharedWindow,attemptTrustBindingSHA256);')
        source = replace(source, 'chunks:input.approvedBodyChunks}', 'chunks:input.approvedBodyChunks,attemptTrustBindingSHA256:input.attemptTrustBindingSHA256}')
        source = replace(source, "'review-manifest.fragment.json'", "'stage-registry.json'")
        return source
    if package == 'K' and leaf == 'binding.cjs':
        source = replace(source, "exact(i,['schema',", "exact(i,['attemptTrustBindingSHA256','schema',")
        source = replace(source, "exact(t,['schema',", "exact(t,['attemptTrustBindingSHA256','schema',")
        source = replace(source, " if(i.schema!==", " if(typeof i.attemptTrustBindingSHA256!=='string'||!/^[a-f0-9]{64}$/u.test(i.attemptTrustBindingSHA256)||i.attemptTrustBindingSHA256!==t.attemptTrustBindingSHA256||i.schema!==")
        return source
    if package == 'K' and leaf == 'build.cjs':
        source = replace(source, "const intentFields={schema:", "const intentFields={attemptTrustBindingSHA256:hash,schema:")
        source = replace(source, "ticket=obj({schema:", "ticket=obj({attemptTrustBindingSHA256:hash,schema:")
        source = replace(source, "'operator.js.txt'", "'operator-template.js'")
        source = replace(source, "'review-manifest.fragment.json'", "'cleanup-registry.json'")
        return source
    if package == 'A' and leaf == 'registry.fragment.json':
        record = json.loads(source)
        for row in record['scripts']:
            row['arguments']['fields']['attemptTrustBindingSHA256'] = {'kind':'pattern','pattern':'^[a-f0-9]{64}$','maxLength':64}
        return json.dumps(record, indent=2) + '\n'
    if leaf == 'create-registry.json':
        record = json.loads(source)
        record['arguments']['fields']['attemptTrustBindingSHA256'] = {'kind':'pattern','pattern':'^[a-f0-9]{64}$','maxLength':64}
        return json.dumps(record, indent=2) + '\n'
    return source


def main(argv=None):
    parser = argparse.ArgumentParser()
    parser.add_argument('--source-root', required=True, type=Path)
    parser.add_argument('--output', type=Path, default=HERE)
    args = parser.parse_args(argv)
    if not args.source_root.is_absolute():
        raise ValueError('adapter-derive:explicit-absolute-source-root')
    index = json.loads((HERE / 'ORIGINS.json').read_bytes())
    originals = {}
    for relative, row in index['files'].items():
        path = args.source_root / relative
        if any(p.is_symlink() for p in (path, *path.parents)):
            raise ValueError('adapter-derive:plain-pinned-original')
        raw = path.read_bytes()
        if len(raw) != row['bytes'] or hashlib.sha256(raw).hexdigest() != row['rawSHA256']:
            raise ValueError('adapter-derive:exact-original-pin:' + relative)
        originals[relative] = raw
    args.output.mkdir(parents=True, exist_ok=True)
    for package, files in FILES.items():
        for leaf in files:
            relative = PACKAGES[package] + '/' + leaf
            result = derive(package, leaf, originals[relative].decode())
            name = {'N/build.cjs':'stage_build.cjs','K/build.cjs':'cleanup_build.cjs','A/registry.fragment.json':'prepare-registry.json','K/operator.js.txt':'operator-template.js','K/fixture.cjs':'cleanup-fixture.cjs','A/root_first_prepare.source-only.py':'prepare_root.py'}.get(package+'/'+leaf,leaf)
            if '/Users/' in result or '\u2014' in result:
                raise ValueError('adapter-derive:nonportable-generated-source:' + relative)
            if leaf.endswith('.py') and result.count('adapter_context') == 1:
                result = result.replace('\nfrom . import adapter_context\n', '')
            if leaf.endswith('.py'):
                result = result.rstrip() + '\n'
            target = args.output / name
            target.parent.mkdir(parents=True, exist_ok=True)
            target.write_text(result)


if __name__ == '__main__':
    main()
