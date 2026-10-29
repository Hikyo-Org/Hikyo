"""Local policy controls only; actual host/guest binding needs protected LXC proof."""

import importlib.util
import json
import os
from pathlib import Path
import stat
import subprocess
import sys
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import patch


spec = importlib.util.spec_from_file_location("host_mount", Path(__file__).with_name("host_mount.py"))
host = importlib.util.module_from_spec(spec)
spec.loader.exec_module(host)


class HostMountTest(unittest.TestCase):
    def setUp(self):
        self.value = {"version": 1, "guest": "audit-fixture", "container_id": 706,
                      "node": "fixture-node", "mount_source": "/dev/mapper/audit-fixture",
                      "luks_uuid": "a7937c26-06c6-4026-8f5b-3b5dc39b3c43",
                      "filesystem_uuid": "697d78bd-636e-4a34-a051-391db5a3b5ee",
                      "device_major": 253, "device_minor": 4, "verified_ns": 100_000_000_000}

    def validate(self, value):
        return host.validate(value, guest="audit-fixture", source="/dev/mapper/audit-fixture",
                             device=(253, 4), now=101_000_000_000,
                             expected_luks_uuid=self.value["luks_uuid"],
                             expected_filesystem_uuid=self.value["filesystem_uuid"])

    def test_exact_receipt_binds_guest_mount_and_current_window(self):
        self.assertEqual(self.validate(self.value), self.value)

    def test_wrong_or_unbounded_binding_refuses(self):
        for field, wrong in (("version", True), ("guest", "other"), ("container_id", False),
                             ("node", ""), ("mount_source", "/dev/mapper/other"),
                             ("device_major", 0), ("device_minor", 5), ("verified_ns", -300_000_000_000),
                             ("verified_ns", 102_000_000_000), ("luks_uuid", "not-a-uuid"),
                             ("filesystem_uuid", "not-a-uuid")):
            with self.subTest(field=field), self.assertRaises(ValueError):
                self.validate(dict(self.value, **{field: wrong}))
        for wrong in ([], {}, dict(self.value, extra=True)):
            with self.subTest(wrong=wrong), self.assertRaises(ValueError):
                self.validate(wrong)

    def test_duplicate_metadata_is_not_silently_accepted(self):
        with self.assertRaises(ValueError):
            host.object_pairs([("version", 1), ("version", 2)])

    def test_valid_but_wrong_storage_uuid_is_refused(self):
        for field in ("luks_uuid", "filesystem_uuid"):
            with self.subTest(field=field), self.assertRaises(ValueError):
                self.validate(dict(self.value, **{field: "b278922b-bdc8-45f2-ae23-38ca54336438"}))

    def verify(self, directory):
        return host.verify(directory, self.value["mount_source"], self.value["luks_uuid"], self.value["filesystem_uuid"])

    def test_file_guard_refuses_writable_mount_and_writable_or_foreign_file(self):
        with tempfile.TemporaryDirectory(prefix="audit-host-proof-") as directory:
            path = Path(directory).resolve() / "receipt.json"
            path.write_text(json.dumps(self.value))
            with patch.object(host, "PROOF", path), patch.object(host.os, "statvfs", return_value=SimpleNamespace(f_flag=0)):
                with self.assertRaisesRegex(ValueError, "fixed read-only bind mount"):
                    self.verify(directory)
            for owner, mode, size in ((99, stat.S_IFREG | 0o444, 100),
                                      (0, stat.S_IFREG | 0o644, 100),
                                      (0, stat.S_IFREG | 0o444, 4097)):
                with self.subTest(owner=owner, mode=mode, size=size), patch.object(host, "PROOF", path), \
                        patch.object(host.os, "statvfs", return_value=SimpleNamespace(f_flag=os.ST_RDONLY)), \
                        patch.object(host.os, "fstat", return_value=SimpleNamespace(st_uid=owner, st_mode=mode, st_size=size)):
                    with self.assertRaisesRegex(ValueError, "owner/type/mode/size refused"):
                        self.verify(directory)

    def test_cli_requires_explicit_exclusive_host_mode_and_both_expected_ids(self):
        script = Path(__file__).with_name("install-host.py")
        for flags, diagnostic in (
                (["--host-encryption-proof"], "both independently approved storage UUIDs"),
                (["--rehearsal", "--host-encryption-proof"], "modes are exclusive"),
                (["--expected-luks-uuid", self.value["luks_uuid"]], "storage UUID arguments require host proof mode")):
            with self.subTest(flags=flags):
                result = subprocess.run([sys.executable, "-I", "-B", str(script), "unused-manifest", "unused-keys", *flags],
                                        capture_output=True, timeout=10, check=False)
                self.assertNotEqual(result.returncode, 0)
                self.assertIn(diagnostic.encode(), result.stderr)

    def test_regular_bounded_file_read_uses_binding_policy(self):
        # Read-only mount and host ownership are mocked, not LXC/crypto proof.
        with tempfile.TemporaryDirectory(prefix="audit-host-proof-") as directory:
            path = Path(directory).resolve() / "receipt.json"
            device = Path(directory).stat().st_dev
            value = dict(self.value, device_major=os.major(device), device_minor=os.minor(device))
            path.write_text(json.dumps(value))
            info = SimpleNamespace(st_uid=0, st_mode=stat.S_IFREG | 0o444, st_size=path.stat().st_size)
            with patch.object(host, "PROOF", path), \
                    patch.object(host.os, "statvfs", return_value=SimpleNamespace(f_flag=os.ST_RDONLY)), \
                    patch.object(host.os, "fstat", return_value=info), \
                    patch.object(host.socket, "gethostname", return_value="audit-fixture"), \
                    patch.object(host.time, "time_ns", return_value=101_000_000_000):
                self.assertEqual(self.verify(directory), value)


if __name__ == "__main__":
    unittest.main()
