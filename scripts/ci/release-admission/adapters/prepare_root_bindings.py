"""Explicit new-model wiring for the original A Root input/publication helpers."""
import copy
from pathlib import Path

from . import adapter_context
from . import persistent_admission


def bind(root, modules, inputs, proposal_raw, *, configuration_raw, configuration_raw_sha256,
         originals_verifier):
    m = modules['model']
    context = adapter_context.get(m)
    m.require(type(configuration_raw) is bytes and root.raw_sha(configuration_raw) == configuration_raw_sha256
              and m.same(m.strict_json(configuration_raw), m.release_configuration_checked()),
              'exact-explicit-release-configuration-original-bytes')
    m.require(type(proposal_raw) is bytes and root.raw_sha(proposal_raw) == m.PROPOSAL_RAW_SHA,
              'same-original-resource-proposal-bytes')
    m._future_inputs_checked(inputs)
    source = context.source_check()
    copied = dict(modules)
    for value in copied.values():
        if hasattr(value, '__file__'):
            path = Path(value.__file__).resolve()
            relative = path.relative_to(Path(__file__).resolve().parent.parent).as_posix()
            m.require(relative in source['files']
                      and root.raw_sha(path.read_bytes()) == source['files'][relative]['rawSHA256'],
                      'actual-Root-bundle-module-source-bytes')
    copied['assembly'] = persistent_admission
    model = source['files']['core/next_fixture_model.source-only.py']
    test = source['files']['core/test_next_fixture_model.py']
    mapping = {'modules': {'model': copy.deepcopy(model)}, 'modelTest': copy.deepcopy(test)}
    root.E, root.NAMESPACE = context.runtime_root, m.configuration_checked()['namespace']
    root.PROPOSAL_SHA, root.CONFIG_SHA = m.PROPOSAL_RAW_SHA, configuration_raw_sha256
    root._ORIGINALS = originals_verifier
    bundle = (copied, mapping, copy.deepcopy(inputs), proposal_raw, copy.deepcopy(source))
    root.check_loaded(bundle)
    return bundle


def collection(root, bundle, raw, now, *, expected_pair):
    m = root.check_loaded(bundle)
    context = adapter_context.get(m)
    m.require(expected_pair['candidateSHA'] == context.binding['candidateHeadSHA']
              and expected_pair['mainSHA'] == context.binding['mainSHA'], 'same-fixed-Root-attempt-pair')
    archive = root.strict_archive(raw)
    # The real configured model invokes the actual successor collector and the
    # parent's whole-native-original verifier. No v2 archive is reinterpreted.
    code = m.register_pair_collection(archive, context.manifest, root._ORIGINALS)
    context.forward(m, now)
    # The whole collector envelope is retained unchanged by the real model
    # registry and publication archive. Only this bounded A view enters prepare.
    view = {adapter_context.FIELD: context.binding_sha256,
            'schema': 'hikyo-1046/configured-adapter-pair-view/v1',
            'sourceManifest': copy.deepcopy(context.manifest), 'code': code}
    m.strict_json(m.canonical(view))
    return view
