"""Verify exact reviewed static bytes before loading fixed successor modules."""
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import stat
import sys
import threading


HERE = Path(__file__).resolve().parent
LOAD_SEQUENCE = 0
# Dependency order: policy/records use bounded; collector uses policy. The
# core's lazy imports also need workflow_contract already established. This
# registry belongs to the trusted loader, never to a helper-supplied marker.
HELPERS = ('bounded', 'release_policy', 'release_records', 'workflow_contract',
           'release_collector')
_HELPER_PROVENANCE = {}
_HELPER_LOCK = threading.RLock()
MODULES = {'model': 'core/next_fixture_model.source-only.py',
           'executor': 'core/next_fixture_execution.source-only.py',
           'ledger': 'core/next_fixture_ledger.source-only.py',
           'runtime': 'core/next_fixture_runtime_bridge.source-only.py',
           'operator': 'core/next_operator_orchestration_stdout.source-only.py',
           'result': 'core/next_hosted_result.source-only.py',
           'finalizer': 'core/next_owner_finalization.source-only.py',
           'configuration': 'core/source_prepared_entrypoint.source-only.py',
           'collector': 'release_collector.py'}


def require(condition, label):
    if not condition:
        raise ValueError('release-loader:' + label)


def read(path, maximum=2_000_000):
    require(path.is_absolute() and not any(p.is_symlink() for p in (path, *path.parents)),
            'plain-static-file')
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
    with os.fdopen(fd, 'rb') as stream:
        info = os.fstat(stream.fileno())
        require(stat.S_ISREG(info.st_mode) and info.st_nlink == 1 and 0 < info.st_size <= maximum,
                'bounded-regular-static-file')
        raw = stream.read(maximum + 1)
    require(len(raw) <= maximum, 'static-file-size')
    return raw


def decode(raw):
    def pairs(items):
        value = {}
        for key, child in items:
            require(key not in value, 'duplicate-map-key')
            value[key] = child
        return value
    def bad(_):
        raise ValueError('release-loader:nonfinite-map')
    return json.loads(raw, object_pairs_hook=pairs, parse_constant=bad)


def verify(*, reviewed_map_sha256, reviewed_manifest_raw_sha256, root=HERE):
    """Pins must come from independent source review, never runtime release data."""
    raw_map = read(root / 'EXECUTION-MAP.json')
    require(hashlib.sha256(raw_map).hexdigest() == reviewed_map_sha256, 'reviewed-map-pin')
    mapping = decode(raw_map)
    require(set(mapping) == {'schema', 'runtimeReady', 'sourceAdopted',
            'executionAuthorityGranted', 'actualConfiguration', 'implementationManifestRawSHA256',
            'implementationManifestSHA256', 'sourceFiles', 'modules'}, 'closed-static-map')
    require(mapping['schema'] == 'hikyo-1046/unconfigured-execution-map/v1'
            and mapping['actualConfiguration'] is None and mapping['runtimeReady'] is False
            and mapping['sourceAdopted'] is False and mapping['executionAuthorityGranted'] is False,
            'unconfigured-source-only')
    manifest_raw = read(root / 'IMPLEMENTATION-MANIFEST.json')
    require(hashlib.sha256(manifest_raw).hexdigest() == reviewed_manifest_raw_sha256
            == mapping['implementationManifestRawSHA256'], 'reviewed-manifest-raw-pin')
    manifest = decode(manifest_raw)
    require(set(manifest) == {'schema', 'files'}
            and manifest['schema'] == 'hikyo-158/release-implementation/v1', 'closed-code-manifest')
    semantic = json.dumps(manifest, sort_keys=True, separators=(',', ':'), ensure_ascii=True).encode()
    require(hashlib.sha256(semantic).hexdigest() == mapping['implementationManifestSHA256'],
            'distinct-semantic-manifest-pin')
    files = mapping['sourceFiles']
    require(type(files) is dict and 1 <= len(files) <= 2048
            and type(manifest['files']) is dict and set(manifest['files']) == set(files),
            'bounded-complete-static-closure')
    snapshots = {}
    for relative, pin in files.items():
        path = Path(relative)
        require(type(relative) is str and not path.is_absolute() and '..' not in path.parts
                and path.as_posix() == relative, 'confined-static-path')
        require(type(pin) is dict and set(pin) == {'bytes', 'rawSHA256'}
                and type(pin['bytes']) is int, 'closed-source-pin')
        raw = read(root / path)
        require(len(raw) == pin['bytes'] and hashlib.sha256(raw).hexdigest()
                == pin['rawSHA256'] == manifest['files'][relative], 'exact-source-bytes')
        snapshots[relative] = raw
    require(type(mapping['modules']) is dict and set(mapping['modules']) == set(MODULES),
            'fixed-module-roles')
    for role, relative in MODULES.items():
        require(mapping['modules'][role] == {'path': relative, **files[relative]}, 'fixed-import-source')
    return mapping, manifest, snapshots


def _bootstrap_helpers(root, snapshots):
    # Concurrent callers must not observe an in-progress helper as established
    # or start its dependent before its verified execution completes.
    with _HELPER_LOCK:
        return _bootstrap_helpers_locked(root, snapshots)


def _bootstrap_helpers_locked(root, snapshots):
    # Bind the entire helper closure, not just each helper's own bytes: a
    # retained record function may reference an earlier bounded/policy object.
    closure = tuple((name, hashlib.sha256(snapshots[name + '.py']).hexdigest())
                    for name in HELPERS)
    # Check every existing entry before executing any helper. Ordinary imports
    # from this very path, including matching-header pyc, are still unproven.
    # Never replace/remove them silently: callers may hold their old classes.
    for name in HELPERS:
        proven = _HELPER_PROVENANCE.get(name)
        if name in sys.modules:
            require(proven is not None and sys.modules[name] is proven[0],
                    'unproven-helper-cache:' + name)
            require(proven[1:] == (root, closure), 'stale-helper-snapshot:' + name)
        else:
            require(proven is None, 'registered-helper-missing:' + name)
    for name in HELPERS:
        relative = name + '.py'
        if name not in sys.modules:
            spec = importlib.util.spec_from_file_location(name, root / relative)
            value = importlib.util.module_from_spec(spec)
            sys.modules[name] = value
            try:
                exec(compile(snapshots[relative], str(root / relative), 'exec',
                             dont_inherit=True), value.__dict__)
            except BaseException:
                del sys.modules[name]
                raise
            _HELPER_PROVENANCE[name] = (value, root, closure)
    return {name: sys.modules[name] for name in HELPERS}


def bootstrap_helpers(*, reviewed_map_sha256, reviewed_manifest_raw_sha256, root=HERE):
    """Establish shared identities before importing any public helper API.

    Root must trust this loader/bootstrap and supply independently reviewed
    pins. This process-local provenance records only execution of exact helper
    snapshots, not approval, authenticity, source adoption or runtime authority.
    Unproven imports or changed helper snapshots require a fresh interpreter;
    clearing caches or reloading helpers cannot preserve retained identities.
    """
    _, _, snapshots = verify(reviewed_map_sha256=reviewed_map_sha256,
        reviewed_manifest_raw_sha256=reviewed_manifest_raw_sha256, root=root)
    return _bootstrap_helpers(root, snapshots)


def load_unconfigured(*, reviewed_map_sha256, reviewed_manifest_raw_sha256, root=HERE):
    mapping, manifest, snapshots = verify(reviewed_map_sha256=reviewed_map_sha256,
        reviewed_manifest_raw_sha256=reviewed_manifest_raw_sha256, root=root)
    _bootstrap_helpers(root, snapshots)
    global LOAD_SEQUENCE
    with _HELPER_LOCK:
        LOAD_SEQUENCE += 1
        sequence = LOAD_SEQUENCE
    result = {}
    for role, relative in MODULES.items():
        if role == 'collector':
            result[role] = sys.modules['release_collector']
            continue
        name = 'hikyo_1046_' + role + '_' + str(sequence)
        spec = importlib.util.spec_from_file_location(name, root / relative)
        module = importlib.util.module_from_spec(spec)
        sys.modules[name] = module
        try:
            exec(compile(snapshots[relative], str(root / relative), 'exec'), module.__dict__)
        except BaseException:
            del sys.modules[name]
            raise
        result[role] = module
    result['implementation'] = manifest
    result['executionMap'] = mapping
    return result
