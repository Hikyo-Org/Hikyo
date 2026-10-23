"""Executable contracts for the actual unconfigured successor model."""
import importlib.util
from pathlib import Path
import sys
import unittest
import uuid


def actual_model():
    path = Path(__file__).with_name('next_fixture_model.source-only.py')
    name = '_successor_model_contract_' + uuid.uuid4().hex
    spec = importlib.util.spec_from_file_location(name, path)
    module = importlib.util.module_from_spec(spec)
    sys.modules[name] = module
    spec.loader.exec_module(module)
    return module


class ConfiguredModelContractTests(unittest.TestCase):
    def test_no_default_resource_or_release_authority(self):
        m = actual_model()
        self.addCleanup(sys.modules.pop, m.__name__)
        with self.assertRaisesRegex(ValueError, 'unconfigured'):
            m.configuration_checked()
        with self.assertRaisesRegex(ValueError, 'release-generation-unconfigured'):
            m.release_configuration_checked()
        self.assertIsNone(m.ATTEMPT_TRUST_BINDING)
        self.assertEqual(m.PAIR_COLLECTIONS, {})

    def test_json_limits_are_enforced_by_actual_model(self):
        m = actual_model()
        self.addCleanup(sys.modules.pop, m.__name__)
        with self.assertRaises(ValueError):
            m.strict_json(b'{"x":1,"x":2}')
        with self.assertRaises(ValueError):
            m.canonical({'payload': 'x' * 2_000_000})
        with self.assertRaises(ValueError):
            m.canonical([None] * 2049)
        self.assertEqual(m.strict_json(m.canonical({'finite': [1, False, None]})),
                         {'finite': [1, False, None]})
