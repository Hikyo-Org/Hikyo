import io
import tempfile
import unittest
from pathlib import Path
import tarfile

import yaml

from nightly_chart import check_rendered, compare, package_files, prepare


class NightlyChartTest(unittest.TestCase):
    def setUp(self):
        self.scratch = tempfile.TemporaryDirectory()
        self.addCleanup(self.scratch.cleanup)
        self.work = Path(self.scratch.name)
        self.source = self.work / "source"
        self.source.mkdir()
        (self.source / "Chart.yaml").write_text("name: hikyo\ntype: application\nversion: 0.0.0\n")
        (self.source / "values.yaml").write_text(
            "image: {}\nupdates: {channel: stable}\nreleaseTrust: {admissionPolicy: {mode: keyed}}\n"
        )
        self.commit = "12345678" * 5
        self.version = "0.0.1-nightly.20261031.1.g12345678"
        self.digest = "sha256:" + "a" * 64

    def test_preparation_binds_source_image_version_and_nightly_channel(self):
        destination = self.work / "chart"
        prepare(self.source, destination, "Hikyo-Org/Hikyo", self.version, self.commit, self.digest)
        chart = yaml.safe_load((destination / "Chart.yaml").read_text())
        self.assertEqual(chart["version"], self.version)
        self.assertEqual(chart["appVersion"], self.version)
        self.assertEqual(chart["annotations"]["org.opencontainers.image.revision"], self.commit)
        values = yaml.safe_load((destination / "values.yaml").read_text())
        self.assertEqual(values["image"], {"repository": "ghcr.io/hikyo-org/hikyo", "digest": self.digest})
        self.assertEqual(values["updates"]["channel"], "nightly")
        self.assertEqual(values["releaseTrust"]["admissionPolicy"]["mode"], "keyless")
        self.assertNotIn("annotations", yaml.safe_load((self.source / "Chart.yaml").read_text()))

    def test_mismatched_source_version_and_invalid_inputs_are_refused(self):
        cases = [
            ("../escape", self.version, self.commit, self.digest),
            ("Hikyo-Org/Hikyo", "1.0.0", self.commit, self.digest),
            ("Hikyo-Org/Hikyo", self.version, "b" * 40, self.digest),
            ("Hikyo-Org/Hikyo", self.version, self.commit, "latest"),
        ]
        for number, args in enumerate(cases):
            with self.subTest(args=args), self.assertRaises(ValueError):
                prepare(self.source, self.work / str(number), *args)

    def test_source_links_cannot_copy_files_from_outside_the_chart(self):
        outside = self.work / "outside"
        outside.write_text("must not be packaged")
        (self.source / "leak").symlink_to(outside)
        with self.assertRaises(ValueError):
            prepare(self.source, self.work / "chart", "Hikyo-Org/Hikyo",
                    self.version, self.commit, self.digest)
        self.assertFalse((self.work / "chart").exists())

    def archive(self, name, entries, timestamp=0):
        path = self.work / name
        with tarfile.open(path, "w:gz") as archive:
            for filename, data, kind in entries:
                item = tarfile.TarInfo(filename)
                item.mtime = timestamp
                if kind == "link":
                    item.type = tarfile.SYMTYPE
                    item.linkname = "/outside"
                    archive.addfile(item)
                else:
                    item.size = len(data)
                    archive.addfile(item, io.BytesIO(data))
        return path

    def test_same_files_can_have_different_package_timestamps(self):
        entries = [("hikyo/Chart.yaml", b"metadata", "file"), ("hikyo/values.yaml", b"values", "file")]
        compare(self.archive("one.tgz", entries), self.archive("two.tgz", entries, timestamp=200))

    def test_every_changed_source_byte_is_refused(self):
        entries = [("hikyo/Chart.yaml", b"metadata", "file"), ("hikyo/values.yaml", b"values", "file")]
        expected = self.archive("expected.tgz", entries)
        entries[1] = ("hikyo/values.yaml", b"different image", "file")
        with self.assertRaises(ValueError):
            compare(expected, self.archive("changed.tgz", entries))

    def test_archive_links_traversal_duplicates_and_missing_metadata_are_refused(self):
        for number, entries in enumerate([
            [("hikyo/Chart.yaml", b"", "link")],
            [("hikyo/../outside", b"x", "file")],
            [("/outside", b"x", "file")],
            [("hikyo/Chart.yaml", b"one", "file"), ("hikyo/Chart.yaml", b"two", "file")],
            [("hikyo/Chart.yaml", b"one", "file")],
        ]):
            with self.subTest(entries=entries), self.assertRaises(ValueError):
                package_files(self.archive(str(number) + ".tgz", entries))

    def test_rendered_mixed_images_and_missing_cli_root_key_are_refused(self):
        server = {"name": "server", "image": "ghcr.io/hikyo-org/hikyo@" + self.digest,
                  "env": [{"name": "HIKYO_ROOT_KEY_FILE", "value": "/run/hikyo-root-key/root-key"}]}
        document = {"kind": "Deployment", "spec": {"template": {"spec": {"containers": [server]}}}}
        path = self.work / "rendered.yaml"
        path.write_text(yaml.safe_dump(document))
        check_rendered(path, "Hikyo-Org/Hikyo", self.digest)
        server["image"] = "ghcr.io/hikyo-org/hikyo:latest"
        path.write_text(yaml.safe_dump(document))
        with self.assertRaises(ValueError):
            check_rendered(path, "Hikyo-Org/Hikyo", self.digest)
        server["image"] = "ghcr.io/hikyo-org/hikyo@" + self.digest
        server["env"] = []
        path.write_text(yaml.safe_dump(document))
        with self.assertRaises(ValueError):
            check_rendered(path, "Hikyo-Org/Hikyo", self.digest)


if __name__ == "__main__":
    unittest.main()
