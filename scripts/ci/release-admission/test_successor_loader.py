"""Reject source substitution and import redirection before executing code."""
import hashlib
import json
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import textwrap
import unittest

import load_successor
from seal_sources import seal


HELPERS = ('bounded', 'release_policy', 'release_records', 'workflow_contract',
           'release_collector')
PROBE_BOOTSTRAP = '''
import hashlib
import importlib
import importlib.util
import marshal
from pathlib import Path
import struct
import sys

root = Path(sys.argv[1])
sys.path.insert(0, str(root))
import load_successor
from seal_sources import seal
helpers = ('bounded', 'release_policy', 'release_records', 'workflow_contract',
           'release_collector')

def pins():
    # These test-computed pins prove integrity only, never review or adoption.
    return dict(root=root,
        reviewed_map_sha256=hashlib.sha256((root / 'EXECUTION-MAP.json').read_bytes()).hexdigest(),
        reviewed_manifest_raw_sha256=hashlib.sha256((root / 'IMPLEMENTATION-MANIFEST.json').read_bytes()).hexdigest())

def foreign_cache(name):
    path = root / (name + '.py')
    raw = path.read_bytes()
    info = path.stat()
    marker = root / (name + '.foreign-executed')
    payload = raw + ('\\nFOREIGN_CACHE_SENTINEL = "unreviewed bytecode"\\n'
        + 'with open(' + repr(str(marker)) + ', "w") as marker: marker.write("executed")\\n').encode()
    cached = Path(importlib.util.cache_from_source(str(path)))
    cached.parent.mkdir(exist_ok=True)
    cached.write_bytes(importlib.util.MAGIC_NUMBER + struct.pack('<III', 0,
        int(info.st_mtime) & 0xffffffff, info.st_size)
        + marshal.dumps(compile(payload, str(path), 'exec')))
    return raw, marker

def refuse(label):
    try:
        load_successor.load_unconfigured(**pins())
    except ValueError as error:
        assert label in str(error), str(error)
        print(str(error))
    else:
        raise AssertionError('verified loader accepted an unproven or stale helper')
'''


class HelperProvenanceTests(unittest.TestCase):
    """Real package imports in isolated interpreters; no archive is required."""
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix='successor-helper-test-')
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name).resolve() / 'package'
        shutil.copytree(Path(__file__).resolve().parent, self.root,
                        ignore=shutil.ignore_patterns('__pycache__'))
        # Seal only the disposable copy. Repository seals remain parent-owned.
        seal(self.root)

    def probe(self, source, role=''):
        result = subprocess.run([sys.executable, '-I', '-B', '-c',
            PROBE_BOOTSTRAP + '\n' + textwrap.dedent(source), str(self.root), role],
            capture_output=True, text=True, timeout=30, check=False)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        return result.stdout

    def test_ordinary_foreign_cache_preimports_refuse_for_all_five_helpers(self):
        for role in HELPERS:
            with self.subTest(helper=role):
                self.probe('''
                    role = sys.argv[2]
                    raw, marker = foreign_cache(role)
                    # In the reported bounded case, use the ordinary record API.
                    importlib.import_module('release_records' if role == 'bounded' else role)
                    helper = sys.modules[role]
                    assert helper.__file__ == str(root / (role + '.py'))
                    assert helper.FOREIGN_CACHE_SENTINEL == 'unreviewed bytecode'
                    assert marker.read_text() == 'executed'
                    assert (root / (role + '.py')).read_bytes() == raw
                    before = {name: sys.modules.get(name) for name in helpers}
                    load_successor.verify(**pins())
                    refuse('unproven-helper-cache:')
                    assert all(sys.modules.get(name) is value for name, value in before.items())
                    if role == 'bounded':
                        assert sys.modules['release_records'].canonical is helper.canonical
                ''', role)

    def test_unproven_late_helper_refuses_before_other_helpers_execute(self):
        self.probe('''
            import workflow_contract
            assert 'bounded' not in sys.modules
            refuse('unproven-helper-cache:workflow_contract')
            assert 'bounded' not in sys.modules
            assert 'release_policy' not in sys.modules
            assert 'release_records' not in sys.modules
            assert 'release_collector' not in sys.modules
        ''')

    def test_fresh_and_repeat_loads_compile_all_helpers_and_preserve_identities(self):
        self.probe('''
            markers = [foreign_cache(name)[1] for name in helpers]
            first = load_successor.load_unconfigured(**pins())
            established = {name: sys.modules[name] for name in helpers}
            from release_records import Observation, ReleaseAdmission, canonical
            from bounded import canonical as bounded_canonical
            assert canonical is bounded_canonical
            assert established['release_policy'].digest is established['bounded'].digest
            assert established['release_collector'].release_policy is established['release_policy']
            assert all(not marker.exists() for marker in markers)
            assert all(not hasattr(value, 'FOREIGN_CACHE_SENTINEL') for value in established.values())
            assert load_successor.bootstrap_helpers(**pins()) == established
            second = load_successor.load_unconfigured(**pins())
            assert all(sys.modules[name] is value for name, value in established.items())
            assert sys.modules['release_records'].Observation is Observation
            assert sys.modules['release_records'].ReleaseAdmission is ReleaseAdmission
            assert first['collector'] is second['collector'] is established['release_collector']
            assert first['model'] is not second['model']
            assert first['model'].FUTURE_CONFIGURATION is None
            first['model'].FUTURE_CONFIGURATION = {'synthetic': 'first model only'}
            first['model'].PAIR_COLLECTIONS['synthetic'] = 'first model only'
            assert second['model'].FUTURE_CONFIGURATION is None
            assert second['model'].PAIR_COLLECTIONS == {}
            assert all(not marker.exists() for marker in markers)
        ''')

    def test_new_sealed_helper_snapshots_refuse_old_objects_without_replacement(self):
        for role in HELPERS:
            with self.subTest(helper=role):
                self.probe('''
                    load_successor.load_unconfigured(**pins())
                    before = {name: sys.modules[name] for name in helpers}
                    from release_records import Observation, canonical
                    path = root / (sys.argv[2] + '.py')
                    path.write_bytes(path.read_bytes() + b'\\n# New test-only reviewed snapshot.\\n')
                    seal(root)
                    load_successor.verify(**pins())
                    refuse('stale-helper-snapshot:')
                    assert all(sys.modules[name] is value for name, value in before.items())
                    assert sys.modules['release_records'].Observation is Observation
                    assert sys.modules['release_records'].canonical is canonical
                ''', role)

    def test_same_path_reimport_objects_are_not_registered_helpers(self):
        for role in HELPERS:
            with self.subTest(helper=role):
                self.probe('''
                    load_successor.load_unconfigured(**pins())
                    role = sys.argv[2]
                    trusted = sys.modules[role]
                    spec = importlib.util.spec_from_file_location(role, root / (role + '.py'))
                    replacement = importlib.util.module_from_spec(spec)
                    sys.modules[role] = replacement
                    spec.loader.exec_module(replacement)
                    assert replacement.__file__ == trusted.__file__
                    assert replacement is not trusted
                    refuse('unproven-helper-cache:' + role)
                    assert sys.modules[role] is replacement
                ''', role)

    def test_removed_registered_helpers_are_not_silently_replaced(self):
        for role in HELPERS:
            with self.subTest(helper=role):
                self.probe('''
                    load_successor.load_unconfigured(**pins())
                    role = sys.argv[2]
                    retained_reference = sys.modules.pop(role)
                    refuse('registered-helper-missing:' + role)
                    assert role not in sys.modules
                    assert retained_reference.__name__ == role
                ''', role)

    def test_explicit_bootstrap_precedes_public_record_imports(self):
        self.probe('''
            established = load_successor.bootstrap_helpers(**pins())
            from release_records import Observation, canonical
            assert established['release_records'].Observation is Observation
            assert established['bounded'].canonical is canonical
            assert load_successor.bootstrap_helpers(**pins()) == established
            assert load_successor.load_unconfigured(**pins())['collector'] is established['release_collector']
        ''')

    def test_identical_helper_bytes_from_another_root_refuse(self):
        self.probe('''
            import shutil
            established = load_successor.bootstrap_helpers(**pins())
            other = root.with_name('other-package')
            shutil.copytree(root, other, ignore=shutil.ignore_patterns('__pycache__'))
            root = other
            load_successor.verify(**pins())
            refuse('stale-helper-snapshot:')
            assert all(sys.modules[name] is value for name, value in established.items())
        ''')

    def test_concurrent_loads_share_proven_helpers_and_separate_models(self):
        self.probe('''
            from concurrent.futures import ThreadPoolExecutor
            from threading import Barrier
            barrier = Barrier(2)
            def load():
                barrier.wait(timeout=10)
                return load_successor.load_unconfigured(**pins())
            with ThreadPoolExecutor(max_workers=2) as pool:
                pending = [pool.submit(load) for _ in range(2)]
                first, second = [future.result(timeout=15) for future in pending]
            assert first['collector'] is second['collector']
            for role in load_successor.MODULES:
                if role != 'collector':
                    assert first[role] is not second[role]
                    assert first[role].__name__ != second[role].__name__
            assert first['model'].PAIR_COLLECTIONS is not second['model'].PAIR_COLLECTIONS
            assert load_successor.bootstrap_helpers(**pins())['release_collector'] is first['collector']
        ''')

    def test_direct_qualification_imports_bootstrap_before_helper_consumers(self):
        for entrypoint in ('test_two_release_pipeline', 'test_adapter_context'):
            with self.subTest(entrypoint=entrypoint):
                self.probe('''
                    importlib.import_module(sys.argv[2])
                    import pipeline_support
                    established = load_successor.bootstrap_helpers(**pins())
                    assert pipeline_support.Observation is established['release_records'].Observation
                    assert pipeline_support.canonical is established['bounded'].canonical
                    assert pipeline_support.collector is established['release_collector']
                    assert pipeline_support.sealed_modules()['collector'] is established['release_collector']
                ''', entrypoint)


class LoaderTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name).resolve()
        for relative in load_successor.MODULES.values():
            path = self.root / relative
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text('raise RuntimeError("source must never execute during verification")\n')
        seal(self.root)

    def pins(self):
        return {'reviewed_map_sha256': hashlib.sha256((self.root / 'EXECUTION-MAP.json').read_bytes()).hexdigest(),
                'reviewed_manifest_raw_sha256': hashlib.sha256((self.root / 'IMPLEMENTATION-MANIFEST.json').read_bytes()).hexdigest(),
                'root': self.root}

    def test_complete_source_verification_does_not_execute_modules(self):
        mapping, manifest, snapshots = load_successor.verify(**self.pins())
        self.assertEqual(set(mapping['modules']), set(load_successor.MODULES))
        self.assertEqual(set(manifest['files']), set(snapshots))

    def test_changed_source_refuses_before_import(self):
        pins = self.pins()
        (self.root / load_successor.MODULES['model']).write_text('print("unreviewed import")\n')
        with self.assertRaisesRegex(ValueError, 'exact-source-bytes'):
            load_successor.load_unconfigured(**pins)

    def test_rehashed_map_cannot_redirect_fixed_model_role(self):
        path = self.root / 'EXECUTION-MAP.json'
        mapping = json.loads(path.read_text())
        mapping['modules']['model'] = mapping['modules']['executor']
        path.write_text(json.dumps(mapping))
        with self.assertRaisesRegex(ValueError, 'fixed-import-source'):
            load_successor.load_unconfigured(**self.pins())

    def test_runtime_configuration_cannot_be_inserted_in_static_map(self):
        path = self.root / 'EXECUTION-MAP.json'
        mapping = json.loads(path.read_text())
        mapping['actualConfiguration'] = {'approved': True}
        path.write_text(json.dumps(mapping))
        with self.assertRaisesRegex(ValueError, 'unconfigured-source-only'):
            load_successor.load_unconfigured(**self.pins())

    def test_raw_and_semantic_manifest_digests_are_distinct(self):
        pins = self.pins()
        mapping = json.loads((self.root / 'EXECUTION-MAP.json').read_text())
        self.assertNotEqual(mapping['implementationManifestSHA256'], pins['reviewed_manifest_raw_sha256'])
        pins['reviewed_manifest_raw_sha256'] = mapping['implementationManifestSHA256']
        with self.assertRaisesRegex(ValueError, 'manifest-raw-pin'):
            load_successor.verify(**pins)


if __name__ == '__main__':
    unittest.main()
