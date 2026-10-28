"""Verify the exact portable adapter implementation, then bind one model/tuple.

No archive/provider/loader discovery and no import-cache reuse between bindings.
"""
import hashlib
import importlib.abc
import importlib.util
import json
import os
from pathlib import Path
import stat
import sys
import uuid

HERE = Path(__file__).resolve().parent


def require(ok, label):
    if not ok:
        raise ValueError('adapter-package:' + label)


def read_pinned(path, pin, size=None, limit=2_000_000):
    path = Path(path)
    require(path.is_absolute() and not any(p.is_symlink() for p in (path, *path.parents)), 'plain-source')
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
    with os.fdopen(fd, 'rb') as stream:
        info = os.fstat(stream.fileno())
        require(stat.S_ISREG(info.st_mode) and info.st_nlink == 1 and 0 < info.st_size <= limit,
                'bounded-regular-source')
        raw = stream.read(limit + 1)
    require(0 < len(raw) <= limit and (size is None or len(raw) == size)
            and hashlib.sha256(raw).hexdigest() == pin, 'exact-source-bytes')
    return raw


def strict(raw):
    def pairs(items):
        result = {}
        for key, value in items:
            require(key not in result, 'duplicate-source-key')
            result[key] = value
        return result
    def bad(_):
        raise ValueError('adapter-package:nonfinite-source-json')
    return json.loads(raw, object_pairs_hook=pairs, parse_constant=bad)


def _verified_sources(expected, *, root=HERE):
    manifest = strict(read_pinned(root / 'adapters/SOURCE-PINS.json', expected))
    require(type(manifest) is dict and set(manifest) == {'schema', 'files', 'stagePins'}
            and manifest['schema'] == 'hikyo-1046/portable-adapter-sources/v1', 'closed-source-manifest')
    require(type(manifest['files']) is dict and 20 <= len(manifest['files']) <= 256, 'bounded-source-closure')
    required = {'package_adapters.py', 'adapters/__init__.py', 'adapters/adapter_context.py',
                'adapters/chrome_first_prepare.py', 'adapters/create_holder.py', 'adapters/runner.py',
                'adapters/preflight.py', 'adapters/paired_core.py', 'adapters/root_connection.py',
                'adapters/k_cleanup_custody.py', 'core/next_fixture_model.source-only.py',
                'core/test_next_fixture_model.py'}
    require(required <= manifest['files'].keys(), 'complete-real-adapter-and-model-source-closure')
    snapshots = {}
    for relative, row in manifest['files'].items():
        path = Path(relative)
        require(type(relative) is str and str(path) == relative and not path.is_absolute()
                and '..' not in path.parts, 'confined-source-map')
        require(type(row) is dict and set(row) == {'bytes', 'rawSHA256'}
                and type(row['bytes']) is int, 'closed-source-pin')
        snapshots[relative] = read_pinned(root / path, row['rawSHA256'], row['bytes'])
    return manifest, snapshots


def verify_sources(expected, *, root=HERE):
    return _verified_sources(expected, root=root)[0]


class _SnapshotLoader(importlib.abc.Loader):
    def __init__(self, path, raw):
        self.path, self.raw = path, raw

    def create_module(self, spec):
        return None

    def exec_module(self, module):
        # Compile the exact verified snapshot. SourceFileLoader may read a
        # matching unreviewed pyc even with -B; no bytecode cache is consulted.
        exec(compile(self.raw, str(self.path), 'exec', dont_inherit=True), module.__dict__)


class _SnapshotFinder(importlib.abc.MetaPathFinder):
    def __init__(self, name, root, snapshots):
        self.name, self.root, self.snapshots = name, root, dict(snapshots)

    def find_spec(self, fullname, path=None, target=None):
        if fullname != self.name and not fullname.startswith(self.name + '.'):
            return None
        relative = fullname[len(self.name):].lstrip('.').replace('.', '/')
        package = relative + '/__init__.py' if relative else '__init__.py'
        leaf = package if package in self.snapshots else relative + '.py'
        require(leaf in self.snapshots, 'undeclared-relative-module:' + fullname)
        location = self.root / leaf
        loader = _SnapshotLoader(location, self.snapshots[leaf])
        return importlib.util.spec_from_file_location(fullname, location, loader=loader,
            submodule_search_locations=[str(location.parent)] if leaf == package else None)


def source_package(name, root, source_pins, *, snapshots=None):
    """Load a confined package and every relative dependency from pinned bytes.

    The caller supplies independently checked pins. The production path reuses
    snapshots already read by the complete closure verifier, without a TOCTOU
    source reread. The finder stays installed for lazy relative dependencies.
    """
    root = Path(root)
    require(name.isidentifier() and name not in sys.modules and root.is_absolute(), 'fresh-source-package')
    selected = {}
    for leaf, row in source_pins.items():
        path = Path(leaf)
        require(not path.is_absolute() and '..' not in path.parts and path.as_posix() == leaf
                and path.suffix == '.py', 'confined-python-source')
        raw = (read_pinned(root / leaf, row['rawSHA256'], row['bytes']) if snapshots is None
               else snapshots[leaf])
        require(type(raw) is bytes and len(raw) == row['bytes']
                and hashlib.sha256(raw).hexdigest() == row['rawSHA256'], 'exact-python-snapshot')
        selected[leaf] = raw
    require('__init__.py' in selected, 'declared-package-initializer')
    finder = _SnapshotFinder(name, root, selected)
    sys.meta_path.insert(0, finder)
    try:
        from importlib import import_module
        module = import_module(name)
        module._verified_source_finder = finder
        return module
    except BaseException:
        sys.meta_path.remove(finder)
        for key in tuple(sys.modules):
            if key == name or key.startswith(name + '.'):
                del sys.modules[key]
        raise


def load_adapters(model, *, admission, attempt_binding, source_manifest,
                  implementation_manifest_sha256, implementation_manifest,
                  runtime_root, native_store, node_path,
                  transport_root):
    verify = lambda: verify_sources(implementation_manifest_sha256)
    manifest, snapshots = _verified_sources(implementation_manifest_sha256)
    model.require(model.digest(implementation_manifest) == admission.configuration['implementationManifestSHA256'],
                  'same-adopted-static-implementation-manifest')
    implementation_files = implementation_manifest['files']
    model.require(all(implementation_files.get(relative) == row['rawSHA256']
                      for relative, row in manifest['files'].items())
                  and implementation_files.get('adapters/SOURCE-PINS.json') == implementation_manifest_sha256,
                  'adopted-implementation-includes-complete-real-adapter-closure')
    model.require(Path(model.__file__).resolve() == HERE / 'core/next_fixture_model.source-only.py',
                  'actual-configured-model-source-location')
    model.require(source_manifest['modelSHA256'] == manifest['stagePins']['modelSHA256']
                  and source_manifest['testSHA256'] == manifest['stagePins']['modelTestSHA256'],
                  'actual-successor-model-and-test-source-pins')
    name = '_hikyo_release_adapters_' + uuid.uuid4().hex
    pins = {leaf.removeprefix('adapters/'): row for leaf, row in manifest['files'].items()
            if leaf.startswith('adapters/') and leaf.endswith('.py')}
    python_snapshots = {leaf.removeprefix('adapters/'): raw for leaf, raw in snapshots.items()
                        if leaf.startswith('adapters/') and leaf.endswith('.py')}
    module = source_package(name, HERE / 'adapters', pins, snapshots=python_snapshots)
    try:
        from importlib import import_module
        context_module = import_module(name + '.adapter_context')
        context = context_module.Context(model, admission, attempt_binding, source_manifest,
            source_check=verify, runtime_root=runtime_root, native_store=native_store,
            node_path=node_path, transport_root=transport_root)
        context_module.ACTIVE = context
        module.context = context
        for leaf in ('prepare_root', 'chrome_first_prepare', 'persistent_custody', 'persistent_admission',
                     'create_holder', 'create_custody', 'runner', 'runtime_binding',
                     'preflight', 'root_preflight', 'stage_custody', 'driver_data',
                     'paired_core', 'paired_custody', 'child_transport', 'n_runtime', 'root_driver',
                     'root_connection', 'k_cleanup_custody'):
            setattr(module, leaf, import_module(name + '.' + leaf))
        return module
    except BaseException:
        sys.meta_path.remove(module._verified_source_finder)
        for key in tuple(sys.modules):
            if key == name or key.startswith(name + '.'):
                del sys.modules[key]
        raise


def verify_native_dependencies(adapters, modules):
    """Verify parent-repinned native guards against actual static source bytes.

    This one-time source migration accepts no pin values from release data,
    replaces no model/functions, and never changes any module's file identity.
    """
    context = adapters.context
    context.checked(modules['model'])
    sources = context.source_check()
    paths = {'keyNormalizer': 'core/normalize_next_key_custody.source-only.py',
             'guardDeadline': 'core/future_key_guard_deadline.source-only.py',
             'executor': 'core/next_fixture_execution.source-only.py',
             'ledger': 'core/next_fixture_ledger.source-only.py'}
    for role, relative in paths.items():
        module = modules[role]
        require(Path(module.__file__).resolve() == HERE / relative, 'actual-fixed-native-helper-module')
        row = sources['files'][relative]
        read_pinned(HERE / relative, row['rawSHA256'], row['bytes'])
    model_pin = sources['files']['core/next_fixture_model.source-only.py']['rawSHA256']
    test_pin = sources['files']['core/test_next_fixture_model.py']['rawSHA256']
    normalizer, deadline, executor = (modules[k] for k in ('keyNormalizer', 'guardDeadline', 'executor'))
    require(set(normalizer.PINS) == {'modelSHA256', 'modelTestSHA256'}
            and set(deadline.PINS) == {'modelSHA256', 'modelTestSHA256', 'keyNormalizerSHA256',
                                       'guardSHA256', 'observerSHA256'}, 'fixed-native-pin-role-sets')
    expected_model = {'modelSHA256': model_pin, 'modelTestSHA256': test_pin}
    expected_deadline = {'modelSHA256': model_pin, 'modelTestSHA256': test_pin,
        'keyNormalizerSHA256': sources['files'][paths['keyNormalizer']]['rawSHA256'],
        'guardSHA256': sources['stagePins']['guardSHA256'],
        'observerSHA256': sources['stagePins']['observerSHA256']}
    require(normalizer.PINS == expected_model and deadline.PINS == expected_deadline
            and normalizer.MODEL_SHA == executor.MODEL_SHA256 == model_pin
            and normalizer.TEST_SHA == executor.MODEL_TEST_SHA256 == test_pin,
            'actual-parent-repinned-native-source-guards')
    return modules
