"""Portable real source-package import regressions, no archive or adoption."""
import hashlib
import importlib
import importlib.util
import marshal
from pathlib import Path
import struct
import sys
import tempfile
import unittest
import uuid

from package_adapters import source_package


class SourceLoaderTests(unittest.TestCase):
    def package(self, sources):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        root = Path(temporary.name).resolve()
        pins = {}
        for leaf, raw in sources.items():
            path = root / leaf
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_bytes(raw)
            pins[leaf] = {'bytes': len(raw), 'rawSHA256': hashlib.sha256(raw).hexdigest()}
        return root, pins

    def foreign_cache(self, path, marker):
        cached = Path(importlib.util.cache_from_source(str(path)))
        cached.parent.mkdir(exist_ok=True)
        info = path.stat()
        payload = compile(f"with open({str(marker)!r}, 'w') as stream:\n    stream.write('FOREIGN_PYC_RAN')\n", str(path), 'exec')
        cached.write_bytes(importlib.util.MAGIC_NUMBER
            + struct.pack('<III', 0, int(info.st_mtime) & 0xffffffff, info.st_size)
            + marshal.dumps(payload))
        # Demonstrate that the exact matching-header payload is executable by
        # the ordinary loader, then test that the source-only loader ignores it.
        spec = importlib.util.spec_from_file_location('foreign_cache_control', path)
        spec.loader.exec_module(importlib.util.module_from_spec(spec))
        self.assertEqual(marker.read_text(), 'FOREIGN_PYC_RAN')
        marker.unlink()

    def load(self, root, pins):
        name = 'verified_adapter_test_' + uuid.uuid4().hex
        module = source_package(name, root, pins)
        def cleanup():
            sys.meta_path.remove(module._verified_source_finder)
            for key in tuple(sys.modules):
                if key == name or key.startswith(name + '.'):
                    del sys.modules[key]
        self.addCleanup(cleanup)
        return module

    def test_matching_header_foreign_initializer_never_executes(self):
        root, pins = self.package({'__init__.py': b"value = 'VERIFIED_SOURCE'\n"})
        marker = root / 'foreign-payload-ran'
        self.foreign_cache(root / '__init__.py', marker)
        module = self.load(root, pins)
        self.assertEqual(module.value, 'VERIFIED_SOURCE')
        self.assertFalse(marker.exists())
        self.assertEqual(module.__file__, str(root / '__init__.py'))
        self.assertEqual(hashlib.sha256((root / '__init__.py').read_bytes()).hexdigest(), pins['__init__.py']['rawSHA256'])

    def test_matching_header_foreign_relative_dependency_never_executes(self):
        root, pins = self.package({'__init__.py': b'from .dependency import value\n',
                                  'dependency.py': b"value = 'VERIFIED_DEPENDENCY'\n"})
        marker = root / 'foreign-payload-ran'
        self.foreign_cache(root / 'dependency.py', marker)
        module = self.load(root, pins)
        self.assertEqual(module.value, 'VERIFIED_DEPENDENCY')
        self.assertFalse(marker.exists())
        self.assertEqual(hashlib.sha256((root / 'dependency.py').read_bytes()).hexdigest(), pins['dependency.py']['rawSHA256'])

    def test_lazy_dependency_uses_verified_snapshot_and_undeclared_import_refuses(self):
        root, pins = self.package({'__init__.py': b'value = 1\n', 'lazy.py': b'value = 2\n'})
        module = self.load(root, pins)
        marker = root / 'foreign-payload-ran'
        self.foreign_cache(root / 'lazy.py', marker)
        (root / 'lazy.py').write_text('raise RuntimeError("SOURCE_CHANGED_AFTER_VERIFICATION")\n')
        self.assertEqual(importlib.import_module(module.__name__ + '.lazy').value, 2)
        self.assertFalse(marker.exists())
        (root / 'undeclared.py').write_text(f"open({str(marker)!r}, 'w').write('UNDECLARED')\n")
        with self.assertRaisesRegex(ValueError, 'undeclared-relative-module'):
            importlib.import_module(module.__name__ + '.undeclared')
        self.assertFalse(marker.exists())

    def test_changed_source_pin_refuses_before_initializer(self):
        root, pins = self.package({'__init__.py': b'value = 1\n'})
        (root / '__init__.py').write_text('raise RuntimeError("UNVERIFIED_SOURCE_RAN")\n')
        with self.assertRaisesRegex(ValueError, 'exact-source-bytes'):
            self.load(root, pins)


if __name__ == '__main__':
    unittest.main()
