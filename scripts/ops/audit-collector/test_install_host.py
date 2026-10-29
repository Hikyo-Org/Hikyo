"""Installation preflight must preserve the per-stream SSH boundary."""

import base64
import importlib.util
import json
from pathlib import Path
import struct
import tempfile
import unittest
from unittest.mock import patch


spec = importlib.util.spec_from_file_location("install_host", Path(__file__).with_name("install-host.py"))
installer = importlib.util.module_from_spec(spec)
spec.loader.exec_module(installer)


class InstallPreflightTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="hikyo-1037-install-preflight-")
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.base = self.root / "evidence"
        self.base.mkdir()
        self.code = self.root / "code"
        self.config = self.root / "config"
        self.keys = self.root / "keys"
        self.keys.mkdir()
        streams = {}
        for number, name in enumerate(sorted(installer.NAMES)):
            streams[name] = {"source_id": "local-1037-synthetic", "trail": name.split("-")[1],
                             "scope": {"fixture": name}, "database": "retained.sqlite",
                             "max_bytes": 16777216, "max_rows": 20000}
            # Public-key wire bytes only, no private key or credential material.
            blob = struct.pack(">I", 11) + b"ssh-ed25519" + struct.pack(">I", 32) + bytes([number]) * 32
            (self.keys / (name + ".pub")).write_text("ssh-ed25519 " + base64.b64encode(blob).decode() + " fixture\n")
        self.manifest = self.root / "manifest.json"
        self.manifest.write_text(json.dumps(streams))

    def preflight(self):
        with patch.multiple(installer, BASE=self.base, CODE=self.code, CONFIG=self.config), \
                patch.object(installer.os, "geteuid", return_value=0), \
                patch.object(installer.subprocess, "check_output", return_value="/dev/mapper/synthetic\n"), \
                patch.object(installer.pwd, "getpwnam", side_effect=KeyError), \
                patch.object(installer.shutil, "which", return_value=None), \
                patch.object(installer, "run") as commands:
            try:
                installer.install(self.manifest, self.keys, False)
            finally:
                commands.assert_called_once_with("cryptsetup", "status", "/dev/mapper/synthetic")
                self.assertFalse(self.code.exists())
                self.assertFalse(self.config.exists())
                self.assertEqual(list(self.base.iterdir()), [])

    def test_duplicate_key_with_different_comment_is_refused_before_mutation(self):
        first, second = sorted(installer.NAMES)[:2]
        key = (self.keys / (first + ".pub")).read_text().split()
        (self.keys / (second + ".pub")).write_text(" ".join(key[:2]) + " different-comment\n")
        with self.assertRaisesRegex(ValueError, "distinct"):
            self.preflight()

    def test_distinct_keys_pass_key_checks(self):
        with self.assertRaisesRegex(ValueError, "missing prerequisite"):
            self.preflight()


if __name__ == "__main__":
    unittest.main()
