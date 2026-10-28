"""Owned narrow copy of the adopted collection validator. No I/O or defaults.

Only change to collection semantics: required reviewed expected candidate/main
pair replaces the two ancestor constants. Original core validation is retained.
"""

from . import adapter_context
import re


def expected_pair_checked(m, pair):
    m.exact(pair, {'schema', 'candidateSHA', 'mainSHA'})
    m.require(pair['schema']=='hikyo-158/root-reviewed-source-pair/v1'
        and all(type(pair[k]) is str and re.fullmatch(r'[0-9a-f]{40}',pair[k])
                for k in ('candidateSHA','mainSHA'))
        and pair['candidateSHA']!=pair['mainSHA'], 'explicit-required-reviewed-candidate-main-pair')


def collection_checked(root, bundle, raw, now, *, expected_pair):
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
