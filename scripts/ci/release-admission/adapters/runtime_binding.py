"""Named source-only verification. Never opens a profile or imports JS code."""

from . import adapter_context
import json
from pathlib import Path
import re

from .create_custody import source_binding_checked
from .local_files import plain_path, read_pinned, require, sha

HERE = Path(__file__).resolve().parent
NODE_SHA = 'a9bd0630891c2dcdee70de88270fee2cc0c4a9e76495039dd3b4f91c5e6b71df'


def runtime_binding_checked(m, binding):
    """Exact source root plus one separately allowed executable outside E.

    The whole source contract stays pinned to our snapshot until separately
    reviewed updates. Root registry inclusion/reseal changes source/review raw
    hashes as explicit binding data, never an automatic latest-source choice.
    This is integrity only, not final independent review or runtime adoption.
    """
    context = adapter_context.checked(m, binding)
    source_binding_checked(m, binding)
    PERSISTENT, NODE = context.transport_root, context.node_path
    for key, leaf in (('sourceContract', 'SOURCE-CONTRACT.json'), ('transportSource', 'source-manifest.json'),
                      ('reviewManifest', 'review-manifest.json'), ('clientSource', 'client.cjs')):
        require(Path(binding[key]['path']) == PERSISTENT / leaf, 'exact-persistent-source-leaf')
    require(binding['node']['path'] == str(NODE) and binding['node']['rawSHA256'] == NODE_SHA,
            'only-reviewed-node-executable')
    # Hash executable source bytes only. No Node, Playwright, browser or CLI invocation.
    read_pinned(NODE, NODE_SHA, limit=256_000_000)
    def ref(key):
        row = binding[key]
        return read_pinned(row['path'], row['rawSHA256'])
    contract_raw = ref('sourceContract')
    require(sha(contract_raw) == 'a8dbe3b2736d741b7a84efcad5428869f8d37f8b17c60469bdb0e9d9e6219347', 'exact-snapshotted-contract-no-final-adoption-inference')
    contract = m.strict_json(contract_raw)
    require(contract['schema'] == 'hikyo-158/persistent-owner-source-contract/v1'
            and contract['session'] == binding['session'] and contract['ownerLifetimeMS'] == 7200000
            and contract['protocolRevision'] == binding['protocolRevision'] == 'r2-actual-snippet-exchange-capacity'
            and all(contract['limits'][k] == v for k, v in {'maxOrdinaryRecords': 1024, 'maxControlRecords': 128,
                'finalCloseReserve': 1, 'maxRequestSequence': 1153, 'maxAcceptedRecords': 1153}.items())
            and contract['truthfulFlags'] == {'cliCaptureSuppressed': True, 'coreEphemeralProtocolMetadataSuppressed': False,
                'controlLeaseClaimed': False, 'exclusiveControlVerifiedByAdapter': False, 'ownerProcessExitVerified': False}
            and contract['safeErrorCodes'] == ['invalid-input', 'source-pin', 'binding', 'socket-policy',
                'operation-failed', 'deadline', 'owner-unavailable', 'protocol', 'sequence', 'replay', 'busy',
                'held', 'uncertain', 'shutdown-failed', 'capacity'],
            'same-page-finite-owner-contract')
    source = m.strict_json(ref('transportSource')); m.exact(source, {'schema', 'files'})
    require(source['schema'] == 'hikyo-158/persistent-owner-source-manifest/v1'
            and type(source['files']) is dict and 1 <= len(source['files']) <= 512, 'closed-persistent-source-map')
    frozen = m.strict_json((HERE / 'TRANSPORT-CODE-PINS.json').read_bytes())
    for relative, row in frozen['files'].items():
        require(relative in source['files'] and m.same(source['files'][relative], row),
                'exact-r2-code-contract-package-pins-registry-only-extension')
    for relative, row in source['files'].items():
        m.exact(row, {'sha256', 'bytes'})
        require(type(relative) is str and re.fullmatch(r'[A-Za-z0-9_./-]+', relative)
                and not Path(relative).is_absolute() and '..' not in Path(relative).parts,
                'closed-source-relative-path')
        raw = read_pinned(PERSISTENT / relative, row['sha256'], limit=20_000_000)
        require(type(row['bytes']) is int and len(raw) == row['bytes'], 'exact-persistent-source-bytes')
    require(source['files']['client.cjs']['sha256'] == sha(ref('clientSource'))
            and source['files']['SOURCE-CONTRACT.json']['sha256'] == sha(contract_raw), 'client-contract-source-crosslinks')
    review_raw = ref('reviewManifest')
    require(source['files']['review-manifest.json']['sha256'] == sha(review_raw), 'same-frozen-review-source')
    review = m.strict_json(review_raw); m.exact(review, {'schema', 'scripts'})
    require(review['schema'] == 'hikyo-158/reviewed-owner-scripts/v1' and type(review['scripts']) is list,
            'closed-startup-script-manifest')
    ids = [r['id'] for r in review['scripts']]
    require(len(ids) == len(set(ids)) <= 64, 'unique-startup-script-IDs')
    expected = m.strict_json((HERE / 'create-registry.json').read_bytes())
    selected = binding['scripts']['create']
    require(selected['id'] == expected['id'] == 'aws-create-readout-v1' and selected['id'] in ids,
            'required-concrete-create-readout-at-startup')
    entry = review['scripts'][ids.index(selected['id'])]
    require(m.same(entry, expected) and entry['sha256'] == selected['rawSHA256'], 'exact-create-script-and-contract')
    require(source['files'][entry['file']]['sha256'] == entry['sha256'], 'create-script-source-crosslink')
    require(read_pinned(PERSISTENT / entry['file'], entry['sha256'], limit=65536)
            == (HERE / 'aws-create-readout-v1.js').read_bytes(), 'unchanged-reviewed-create-script')
    # Official package source integrity is checked without executing its loader.
    pins_raw = read_pinned(PERSISTENT / 'runtime-pins.cjs', source['files']['runtime-pins.cjs']['sha256'])
    prefix, suffix = b'module.exports = Object.freeze(', b');'
    start = pins_raw.index(prefix) + len(prefix)
    pins = m.strict_json(pins_raw[start:pins_raw.rindex(suffix)])
    require(pins['nodeSHA256'] == NODE_SHA, 'same-node-pin-as-transport')
    for relative, digest in pins['files'].items():
        require(type(relative) is str and not Path(relative).is_absolute()
                and '..' not in Path(relative).parts, 'closed-vendor-source-path')
        read_pinned(PERSISTENT / 'vendor/playwright-core' / relative, digest, limit=20_000_000)
    plain_path(binding['runtimeDirectory'])  # Opaque protocol metadata only; never open it.
