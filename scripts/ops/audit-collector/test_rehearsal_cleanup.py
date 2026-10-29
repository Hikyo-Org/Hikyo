"""Unknown daemon state must never be called verified cleanup."""

import importlib.util
from pathlib import Path
import subprocess
import unittest
from unittest.mock import patch


spec = importlib.util.spec_from_file_location("rehearse_local", Path(__file__).with_name("rehearse-local.py"))
runner = importlib.util.module_from_spec(spec)
spec.loader.exec_module(runner)


class CleanupTest(unittest.TestCase):
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
