import io
from pathlib import Path
import tarfile
import tempfile
import unittest
from unittest.mock import patch

import nightly_inputs


class NightlyInputsTests(unittest.TestCase):
    def setUp(self):
        self.scratch = tempfile.TemporaryDirectory()
        self.addCleanup(self.scratch.cleanup)
        self.root = Path(self.scratch.name)
        self.destination = self.root / "publisher"
        self.destination.mkdir()
        (self.destination / "trusted.py").write_text("trusted publisher\n")

    def archive(self, extra=()):
        path = self.root / "input.tar.gz"
        with tarfile.open(path, "w:gz") as archive:
            for name in ("dist/metadata.json", "nightly-context/source-validation.json"):
                info = tarfile.TarInfo(name)
                data = b"{}\n"
                info.size = len(data)
                archive.addfile(info, io.BytesIO(data))
            for info, data in extra:
                archive.addfile(info, io.BytesIO(data) if data is not None else None)
        return path

    def assert_refused(self, archive):
        with self.assertRaises((ValueError, OSError, tarfile.TarError, EOFError)):
            nightly_inputs.restore(archive, self.destination)
        self.assertEqual((self.destination / "trusted.py").read_text(), "trusted publisher\n")
        self.assertEqual(sorted(p.name for p in self.destination.iterdir()), ["trusted.py"])

    def test_restores_payload_and_keeps_trusted_tools(self):
        binary = tarfile.TarInfo("dist/hikyo_linux_arm64/hikyo")
        binary.mode = 0o4755
        binary.size = 3
        nightly_inputs.restore(self.archive([(binary, b"bin")]), self.destination)
        result = self.destination / binary.name
        self.assertEqual(result.read_bytes(), b"bin")
        self.assertEqual(result.stat().st_mode & 0o7777, 0o755)
        self.assertEqual((self.destination / "trusted.py").read_text(), "trusted publisher\n")

    def test_rejects_paths_outside_payload_roots_before_writing(self):
        for name in ("../trusted.py", "/tmp/nightly-escape", "scripts/release/tool.sh", "dist/../trusted.py", "dist\\trusted.py", "./dist/extra", "dist//extra"):
            with self.subTest(name=name):
                info = tarfile.TarInfo(name)
                info.size = 1
                self.assert_refused(self.archive([(info, b"x")]))

    def test_rejects_links_devices_and_duplicate_files(self):
        for kind in (tarfile.SYMTYPE, tarfile.LNKTYPE, tarfile.CHRTYPE, tarfile.FIFOTYPE):
            with self.subTest(kind=kind):
                info = tarfile.TarInfo("dist/evil")
                info.type = kind
                info.linkname = "../trusted.py"
                self.assert_refused(self.archive([(info, None)]))
        duplicate = tarfile.TarInfo("dist/metadata.json")
        duplicate.size = 1
        self.assert_refused(self.archive([(duplicate, b"x")]))

    def test_requires_complete_metadata(self):
        path = self.root / "empty.tar.gz"
        with tarfile.open(path, "w:gz"):
            pass
        self.assert_refused(path)

    def test_enforces_size_and_entry_bounds_before_writing(self):
        path = self.archive()
        with patch.object(nightly_inputs, "MAX_BYTES", 1):
            self.assert_refused(path)
        with patch.object(nightly_inputs, "MAX_ENTRIES", 1):
            self.assert_refused(path)

    def test_refuses_existing_or_linked_destination(self):
        for linked in (False, True):
            with self.subTest(linked=linked):
                target = self.destination / "dist"
                if linked:
                    target.symlink_to(self.root, target_is_directory=True)
                else:
                    target.mkdir()
                with self.assertRaises(ValueError):
                    nightly_inputs.restore(self.archive(), self.destination)
                self.assertEqual((self.destination / "trusted.py").read_text(), "trusted publisher\n")
                if linked:
                    target.unlink()
                else:
                    target.rmdir()


if __name__ == "__main__":
    unittest.main()
