"Portable original A Root input/publication checks, no legacy loader."
import copy
from decimal import Decimal
import hashlib
import json
import math
import os
from pathlib import Path
import re
import stat
from . import adapter_context
E = None
NAMESPACE = None
PROPOSAL_SHA = None
CONFIG_SHA = None
_ORIGINALS = None

LIMIT = 2_000_000

ARCHIVE_LIMIT = 24_000_000

PLAN_FILES = {
    'ownerContext': 'owner-context.actual.json',
    'capturePlan': 'metadata-capture-plan.actual.json',
    'iamManifest': 'iam-manifest.actual.json',
    'kmsManifest': 'kms-manifest.actual.json',
    'setupManifest': 'setup-manifest.actual.json',
    'historicalPreparation': 'historical-read-preparation.actual.json',
    'historicalObserverManifest': 'historical-observer-manifest.actual.json',
    'historicalGateManifest': 'historical-gate-manifest.actual.json',
}

PLAN_INPUTS = {'ownerSource', 'metadataReference', 'historicalReference'}

ASSEMBLY_INPUTS = {
    'proposal', 'ownerContext', 'capturePlan', 'iamBundle', 'kmsBundle',
    'setupBundle', 'historicalPreparation', 'historicalRecovery',
    'environmentCaptures', 'code', 'budget',
}

ENV_KEYS = {'environment', 'variables', 'branches', 'oidcDefaults'}

def require(ok, label):
    if not ok:
        raise ValueError('root-first-prepare:' + label)

def raw_sha(raw):
    return hashlib.sha256(raw).hexdigest()

def plain_path(path, root=None):
    target = Path(path)
    require(target.is_absolute() and '..' not in target.parts, 'absolute-named-path')
    require(not any(p.is_symlink() for p in (target, *target.parents)), 'no-symlink-components')
    if root is not None:
        require(target.is_relative_to(root), 'named-file-within-artifact-root')
    return target

def read_pinned(path, pin, *, limit=LIMIT):
    target = plain_path(path, E)
    require(type(pin) is str and re.fullmatch('[0-9a-f]{64}', pin), 'explicit-raw-pin')
    fd = os.open(target, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
    with os.fdopen(fd, 'rb') as stream:
        require(stat.S_ISREG(os.fstat(stream.fileno()).st_mode), 'regular-public-file')
        raw = stream.read(limit + 1)
    require(0 < len(raw) <= limit and raw_sha(raw) == pin, 'bounded-raw-file-pin')
    return raw

def check_loaded(bundle):
    modules, mapping, inputs, proposal, pins = bundle
    m = modules['model']
    adapter_context.get(m)
    config = m.configuration_checked()
    require(config['namespace'] == NAMESPACE and m.PROPOSAL_RAW_SHA == PROPOSAL_SHA and
            raw_sha(proposal) == PROPOSAL_SHA, 'exact-configured-root-inputs')
    require(m.digest(inputs['approval']) == config['approvalSHA256'] and
            m.digest(inputs['futureDirective']) == config['directiveSHA256'] and
            m.digest(inputs['futureApprovalContext']) == config['contextSHA256'], 'root-input-configuration-match')
    require(modules['assembly'].ASSEMBLY_INPUTS == ASSEMBLY_INPUTS, 'exact-eleven-assembly-inputs')
    return m

def strict_archive(raw):
    """Archive envelope may exceed model cap. Never widen model.strict_json."""
    require(type(raw) is bytes and 0 < len(raw) <= ARCHIVE_LIMIT, 'whole-archive-bound')

    def pairs(items):
        value = {}
        for key, child in items:
            require(key not in value, 'duplicate-archive-key')
            value[key] = child
        return value

    def constant(_):
        raise ValueError('root-first-prepare:nonfinite-archive-value')

    value = json.loads(raw, object_pairs_hook=pairs, parse_constant=constant)

    def walk(node, depth=0):
        require(depth <= 40, 'archive-depth')
        if type(node) is dict:
            for child in node.values():
                walk(child, depth + 1)
        elif type(node) is list:
            require(len(node) <= 50000, 'archive-list-bound')
            for child in node:
                walk(child, depth + 1)
        elif type(node) is float:
            require(math.isfinite(node), 'finite-archive-number')
    walk(value)
    return value

def projected_rows(m, value):
    """Only the reviewed manifest/readRows -> manifest/rows repair, no synthesis."""
    if type(value) is dict and set(value) == {'manifest', 'readRows'}:
        result = {'manifest': copy.deepcopy(value['manifest']), 'rows': copy.deepcopy(value['readRows'])}
    else:
        m.exact(value, {'manifest', 'rows'})
        result = copy.deepcopy(value)
    require(type(result['rows']) is list, 'whole-native-row-list')
    m.canonical(result)
    return result

def checked_budget(m, budget, now):
    m.budget_checked(budget, now)  # Original G validator remains untouched.
    gross = sum((m.money(row['amountUSD']) for row in budget['grossCharges']), Decimal(0))
    reserve = sum((m.money(v) for v in budget['completionReserves'].values()), Decimal(0))
    billing = budget['bankedBilling']
    billable = m.money(billing['billableDisplayedUSD'])
    fees = m.money(billing['unreflectedTaxFeesReserveUSD'])
    # Apply the owner's combined conservative <=3 ceiling to both actual bases.
    require(gross + reserve + fees <= Decimal('3.00') and
            billable + reserve + fees <= Decimal('3.00') and
            gross + reserve + fees < Decimal('15.00') and
            billable + reserve + fees < Decimal('15.00'), 'combined-completion-budget')

def exact_key_request(m, request, review, request_raw_sha):
    m.exact(review, {'schema', 'proposalRawSHA256', 'configurationRawSHA256', 'requestRawSHA256'})
    require(review == {'schema': 'hikyo-158/root-reviewed-create-key-request/v1',
                      'proposalRawSHA256': PROPOSAL_SHA, 'configurationRawSHA256': CONFIG_SHA,
                      'requestRawSHA256': request_raw_sha}, 'exact-external-reviewed-key-request-binding')
    m.exact(request, {'Policy', 'Description', 'Origin', 'Tags', 'KeyUsage', 'KeySpec', 'MultiRegion'})
    require(request['Origin'] == 'AWS_KMS' and request['Tags'] == [] and
            request['KeyUsage'] == 'ENCRYPT_DECRYPT' and request['KeySpec'] == 'SYMMETRIC_DEFAULT' and
            request['MultiRegion'] is False and type(request['Description']) is str and
            1 <= len(request['Description']) <= 8192, 'reviewed-semantic-create-key-request')
    policy = request['Policy']
    m.exact(policy, {'Version', 'Id', 'Statement'})
    require(policy['Version'] == '2012-10-17' and type(policy['Id']) is str and 1 <= len(policy['Id']) <= 128 and
            m.same(policy['Statement'], [{'Sid': 'Enable IAM User Permissions', 'Effect': 'Allow',
                'Principal': {'AWS': 'arn:aws:iam::' + m.ACCOUNT + ':root'},
                'Action': 'kms:*', 'Resource': '*'}]), 'reviewed-root-delegation-policy')
    return copy.deepcopy(request)

def admission_folder(path, *, must_exist):
    folder = plain_path(path, E)
    require(folder.parent == E and re.fullmatch(r'banked-first-admission-[0-9]{8}-r[1-9][0-9]*', folder.name),
            'exact-new-admission-folder-name')
    require(folder.is_dir() if must_exist else not folder.exists(), 'exclusive-admission-folder')
    return folder

def write_new(path, raw):
    # O_EXCL also refuses dangling symlinks. No overwrite, cleanup or replay.
    target = plain_path(path, E)
    fd = os.open(target, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    with os.fdopen(fd, 'wb') as stream:
        stream.write(raw)

def input_ref(m, reference, originals, *, limit=LIMIT):
    m.exact(reference, {'path', 'rawSHA256'})
    raw = read_pinned(reference['path'], reference['rawSHA256'], limit=limit)
    key = str(Path(reference['path']).relative_to(E))
    require(key not in originals or originals[key] == raw, 'one-file-one-original')
    originals[key] = raw
    return m.strict_json(raw)

def original_archive_outputs(originals):
    # Whole original bytes, including whitespace and readRows, preserved once.
    return {'original-inputs/' + name: raw for name, raw in originals.items()}

def publish(folder, outputs):
    require(all(not (folder / name).exists() and not (folder / name).is_symlink() for name in outputs),
            'exclusive-all-output-leaves')
    for name, raw in outputs.items():
        parent = folder / name
        parent.parent.mkdir(parents=True, exist_ok=True)
        write_new(parent, raw)

def configured_unbound(bundle, source_manifest):
    """Pure integration seam. Same Root-configured model and original inputs."""
    m = check_loaded(bundle)
    _, mapping, root_inputs, proposal_bytes, _ = bundle
    m.source_manifest_checked(source_manifest)
    require(source_manifest['modelSHA256'] == mapping['modules']['model']['rawSHA256'] and
            source_manifest['testSHA256'] == mapping['modelTest']['rawSHA256'], 'same-reviewed-source-model')
    return m.unbound_proposal({**copy.deepcopy(root_inputs), 'proposalMarkdown': proposal_bytes}, source_manifest)


def make_bundle(*args, **kwargs):
    import sys
    from .prepare_root_bindings import bind
    return bind(sys.modules[__name__], *args, **kwargs)


def collection_checked(*args, **kwargs):
    import sys
    from .prepare_root_bindings import collection
    return collection(sys.modules[__name__], *args, **kwargs)
