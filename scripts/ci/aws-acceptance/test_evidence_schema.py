"""Exercise the actual consumer's closed evidence boundary without authority."""
import ast
import copy
import importlib.util
from pathlib import Path
from types import SimpleNamespace
import tempfile
import time
import unittest

import runner
from test_runner import approved_environment

DIRECTORY = Path(__file__).parent
CORE = DIRECTORY.parent / "release-admission" / "core"
spec = importlib.util.spec_from_file_location("hosted_result_proposal", CORE / "next_hosted_result.source-only.py")
consumer = importlib.util.module_from_spec(spec)
spec.loader.exec_module(consumer)
tree = ast.parse((CORE / "next_fixture_model.source-only.py").read_text())
functions = ast.Module(body=[node for node in tree.body if isinstance(node, ast.FunctionDef) and node.name in ("require", "exact")], type_ignores=[])
namespace = {}
exec(compile(functions, "next_fixture_model.source-only.py", "exec"), namespace)
model = SimpleNamespace(**namespace)


class EvidenceSchemaTests(unittest.TestCase):
    def setUp(self):
        directory = tempfile.TemporaryDirectory()
        self.addCleanup(directory.cleanup)
        approval = runner.Approval(approved_environment(time.time()), time.time())
        self.data = runner.Evidence(Path(directory.name) / "evidence.json", approval).data

    def test_v2_producer_is_accepted_by_actual_consumer_boundary(self):
        self.assertEqual(self.data["schema"], "hikyo.aws-acceptance/v2")
        consumer._evidence_shape(model, self.data)

    def test_legacy_v1_retains_exact_original_shape(self):
        data = {key: value for key, value in self.data.items() if key not in ("failure_assertions", "failure_assertions_truncated")}
        data["schema"] = "hikyo.aws-acceptance/v1"
        consumer._evidence_shape(model, data)
        data["failure_assertions"] = []
        with self.assertRaisesRegex(ValueError, "closed-fields"):
            consumer._evidence_shape(model, data)

    def test_unknown_schema_extra_key_or_missing_v2_field_is_rejected(self):
        variants = []
        value = copy.deepcopy(self.data); value["schema"] = "hikyo.aws-acceptance/v3"; variants.append(value)
        value = copy.deepcopy(self.data); value["untrusted"] = "synthetic-private"; variants.append(value)
        for key in ("failure_assertions", "failure_assertions_truncated"):
            value = copy.deepcopy(self.data); del value[key]; variants.append(value)
        for value in variants:
            with self.subTest(keys=list(value)), self.assertRaises(ValueError):
                consumer._evidence_shape(model, value)

    def test_every_pinned_identifier_and_known_test_is_accepted(self):
        for test in runner.TEST_NAMES:
            self.data["failure_assertions"] = [{"test": test, "assertion": "assertion-066"}]
            consumer._evidence_shape(model, self.data)
        for identifier in runner.FAILURE_ASSERTIONS.values():
            self.data["failure_assertions"] = [{"test": runner.SUITE, "assertion": identifier}]
            consumer._evidence_shape(model, self.data)

    def test_unknown_id_test_raw_text_entry_fields_and_types_are_rejected(self):
        variants = [
            {"test": runner.SUITE, "assertion": "assertion-000"},
            {"test": runner.SUITE, "assertion": "assertion-225"},
            {"test": "TestForeign", "assertion": "assertion-066"},
            {"test": runner.SUITE, "assertion": "synthetic-private-credential"},
            {"test": runner.SUITE, "assertion": "assertion-066", "raw": "synthetic-private"},
            {"test": runner.SUITE},
            {"test": [], "assertion": "assertion-066"},
            {"test": runner.SUITE, "assertion": []},
            "assertion-066",
        ]
        for row in variants:
            with self.subTest(row=row), self.assertRaises(ValueError):
                self.data["failure_assertions"] = [row]
                consumer._evidence_shape(model, self.data)

    def test_rows_cap_and_exact_boolean(self):
        row = {"test": runner.SUITE, "assertion": "assertion-066"}
        self.data["failure_assertions"] = [row] * 128
        for value in (False, True):
            self.data["failure_assertions_truncated"] = value
            consumer._evidence_shape(model, self.data)
        self.data["failure_assertions"] = [row] * 129
        with self.assertRaises(ValueError):
            consumer._evidence_shape(model, self.data)
        self.data["failure_assertions"] = []
        for value in (0, 1, None, "false", []):
            self.data["failure_assertions_truncated"] = value
            with self.subTest(value=value), self.assertRaises(ValueError):
                consumer._evidence_shape(model, self.data)


if __name__ == "__main__":
    unittest.main()
