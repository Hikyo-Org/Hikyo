"""Exercise the actual YAML topology used for unstarted-job inference."""
from pathlib import Path
import unittest

import bounded
from workflow_contract import single_sandbox_checked


class WorkflowTests(unittest.TestCase):
    def setUp(self):
        self.body = (Path(__file__).resolve().parents[3] / '.github/workflows/aws-acceptance.yml').read_text()

    def test_two_approved_byte_variants_preserve_one_sandbox(self):
        first = single_sandbox_checked(bounded, self.body)
        second = single_sandbox_checked(bounded, '# reviewed comment change\n' + self.body)
        self.assertEqual(first, second)

    def test_extra_job_duplicate_key_matrix_and_name_override_refuse(self):
        variants = [self.body + '\n  other:\n    runs-on: ubuntu-latest\n',
                    self.body + '\njobs:\n  sandbox: {}\n',
                    self.body.replace('  sandbox:\n', '  sandbox:\n    strategy: {matrix: {version: [one, two]}}\n'),
                    self.body.replace('  sandbox:\n', '  sandbox:\n    name: something-else\n')]
        for value in variants:
            with self.subTest(value=value[-120:]), self.assertRaises(ValueError):
                single_sandbox_checked(bounded, value)

    def test_yaml_aliases_and_changed_environment_refuse(self):
        for value in ('x: &x [*x]\n' + self.body,
                      self.body.replace('environment: hikyo-158-aws-sandbox', 'environment: other')):
            with self.assertRaises(ValueError):
                single_sandbox_checked(bounded, value)


if __name__ == '__main__':
    unittest.main()
