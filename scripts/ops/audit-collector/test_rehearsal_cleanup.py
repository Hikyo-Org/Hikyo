"""Unknown daemon state must never be called verified cleanup."""

import importlib.util
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch


spec = importlib.util.spec_from_file_location("rehearse_local", Path(__file__).with_name("rehearse-local.py"))
runner = importlib.util.module_from_spec(spec)
spec.loader.exec_module(runner)


class CleanupTest(unittest.TestCase):
    def test_unignored_output_is_refused_before_directory_or_runtime_creation(self):
        with tempfile.TemporaryDirectory(prefix="hikyo-1037-output-preflight-") as scratch:
            root = Path(scratch)
            subprocess.run(["git", "init", "-q", str(root)], check=True)
            output = root / "unignored-evidence"
            with patch.object(sys, "argv", ["rehearse-local.py", str(output)]), \
                    patch.object(runner.subprocess, "check_output", return_value=str(root)), \
                    patch.object(runner.os, "umask"), \
                    patch.object(runner.uuid, "uuid4", side_effect=AssertionError("runtime creation reached")):
                with self.assertRaisesRegex(ValueError, "ignored"):
                    runner.main()
            self.assertFalse(output.exists())

    def test_portable_default_is_ignored_and_cannot_be_staged_normally(self):
        with tempfile.TemporaryDirectory(prefix="hikyo-1037-output-ignored-") as scratch:
            root = Path(scratch)
            subprocess.run(["git", "init", "-q", str(root)], check=True)
            policy = Path(__file__).resolve().parents[3] / ".gitignore"
            (root / ".gitignore").write_bytes(policy.read_bytes())
            output = root / ".local/reports/audit-1037-rehearsal"
            runner.validate_output(output)
            output.mkdir(parents=True)
            receipt = output / "ownership.json"
            receipt.write_text('{"synthetic": true}\n')
            subprocess.run(["git", "-C", str(root), "add", "-A"], check=True)
            staged = subprocess.check_output(["git", "-C", str(root), "diff", "--cached", "--name-only"], text=True)
            self.assertEqual(staged.splitlines(), [".gitignore"])
            subprocess.run(["git", "-C", str(root), "add", "-f", str(receipt)], check=True)
            with self.assertRaisesRegex(ValueError, "untracked"):
                runner.validate_output(output)

    def test_external_private_output_and_other_worktree_are_checked(self):
        with tempfile.TemporaryDirectory(prefix="hikyo-1037-output-external-") as scratch:
            root = Path(scratch)
            runner.validate_output(root / "new/private/evidence")
            repository = root / "other-repository"
            subprocess.run(["git", "init", "-q", str(repository)], check=True)
            with self.assertRaisesRegex(ValueError, "ignored"):
                runner.validate_output(repository / "new/private/evidence")

    def test_daemon_loss_is_unknown_for_all_resource_classes(self):
        for kind in ("container", "volume", "image"):
            with self.subTest(kind=kind), patch.object(runner.subprocess, "run", return_value=subprocess.CompletedProcess([], 1, "", "daemon unavailable")):
                with self.assertRaises(RuntimeError):
                    runner.confirmed_absent(kind, "owned")

    def test_successful_inventory_distinguishes_present_and_absent(self):
        for kind in ("container", "volume", "image"):
            with self.subTest(kind=kind), patch.object(runner.subprocess, "run", return_value=subprocess.CompletedProcess([], 0, "owned\nshared\n", "")):
                self.assertFalse(runner.confirmed_absent(kind, "owned"))
                self.assertTrue(runner.confirmed_absent(kind, "absent"))


if __name__ == "__main__":
    unittest.main()
