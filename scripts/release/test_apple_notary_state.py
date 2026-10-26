"""Synthetic custody/continuation failures, never Apple acceptance proof."""
from copy import deepcopy
from datetime import datetime, timezone
import hashlib
import io
import json
from pathlib import Path
import tarfile
import tempfile
import unittest
from unittest.mock import patch
import zipfile

import apple_notary_state as state


class CheckpointTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        self.current = dict(repository="Hikyo-Org/Hikyo", run_id="42", attempt="1",
                            source_commit="a" * 40, channel="nightly", source_ref="refs/heads/main")
        self.directory = self.root / "evidence"
        self.directory.mkdir()
        for arch in ("arm64", "x86_64"):
            image = ("synthetic " + arch).encode()
            receipt = dict(schema="hikyo.dev/apple-notarization-receipt/v2", repository=self.current["repository"],
                           original_run_id="42", source_commit="a" * 40, architecture=arch, channel="nightly",
                           source_ref="refs/heads/main", submitted_sha256=hashlib.sha256(image).hexdigest())
            (self.directory / f"{arch}-receipt.json").write_text(json.dumps(receipt))
            (self.directory / f"{arch}-submitted.dmg").write_bytes(image)
        self.destination = self.root / "checkpoint.tar"
        state.snapshot(self.directory, self.destination, "finalized", True, self.current)
        self.set_archive(self.destination.read_bytes())
        self.run = dict(path=state.WORKFLOW, head_branch="main", event="schedule", id=42,
                        repository=dict(full_name=self.current["repository"], id=1), head_repository=dict(id=1),
                        head_sha="a" * 40, run_attempt=1, status="completed", conclusion="failure",
                        created_at=datetime.now(timezone.utc).isoformat())

    def set_archive(self, raw_tar):
        buffer = io.BytesIO()
        with zipfile.ZipFile(buffer, "w") as archive:
            archive.writestr("checkpoint.tar", raw_tar)
        self.raw = buffer.getvalue()
        self.artifact = dict(id=7, name="apple-notary-state-42-1-finalized", expired=False,
                             size_in_bytes=len(self.raw), digest="sha256:" + hashlib.sha256(self.raw).hexdigest())

    def read(self):
        with patch.object(state, "api", return_value=self.raw):
            return state.read_checkpoint(self.artifact, self.current["repository"], "42")

    def test_roundtrip_and_restore_use_original_identity(self):
        record, files = self.read()
        self.assertTrue(record["recovery_pending"])
        self.assertEqual(set(files), {item.name for item in self.directory.iterdir()})
        current = dict(self.current, attempt="2")
        with patch.object(state, "api", return_value=self.run), patch.object(state, "artifacts", return_value=[self.artifact]), patch.object(state, "read_checkpoint", return_value=(record, files)):
            state.restore(self.root / "restored", current)
        self.assertEqual((self.root / "restored/arm64-receipt.json").read_bytes(), files["arm64-receipt.json"])

    def test_digest_source_and_run_mismatch_refused(self):
        self.artifact["digest"] = "sha256:" + "0" * 64
        with self.assertRaisesRegex(ValueError, "digest mismatch"):
            self.read()
        self.set_archive(self.destination.read_bytes())
        self.artifact["name"] = "apple-notary-state-43-1-finalized"
        with self.assertRaisesRegex(ValueError, "identity mismatch"):
            self.read()
        with patch.object(state, "api", return_value=dict(self.run, head_sha="b" * 40)):
            with self.assertRaisesRegex(ValueError, "source changed"):
                state.restore(self.root / "wrong-source", self.current)

    def test_retry_without_checkpoint_cannot_create_new_intent(self):
        with patch.object(state, "api", return_value=self.run), patch.object(state, "artifacts", return_value=[]):
            with self.assertRaisesRegex(ValueError, "do not resubmit"):
                state.restore(self.root / "retry", dict(self.current, attempt="2"))

    def test_latest_stage_never_falls_back_to_expired_or_duplicate(self):
        prepared = dict(self.artifact, id=6, name="apple-notary-state-42-1-prepared")
        self.assertEqual(state.select_checkpoint([self.artifact, prepared], "42")["id"], 7)
        with self.assertRaisesRegex(ValueError, "expired"):
            state.select_checkpoint([prepared, dict(self.artifact, expired=True)], "42")
        with self.assertRaisesRegex(ValueError, "duplicate"):
            state.select_checkpoint([self.artifact, self.artifact], "42")
        self.assertIsNone(state.select_checkpoint([self.artifact], "42", before_attempt=1))

    def test_paths_links_unknown_and_duplicate_members_refused(self):
        for name, kind in (("../receipt.json", tarfile.REGTYPE), ("arm64-receipt.json", tarfile.SYMTYPE),
                           ("notary.p8", tarfile.REGTYPE)):
            with self.subTest(name=name, kind=kind):
                buffer = io.BytesIO()
                with tarfile.open(fileobj=buffer, mode="w:") as archive:
                    entry = tarfile.TarInfo(name)
                    entry.type = kind
                    archive.addfile(entry)
                self.set_archive(buffer.getvalue())
                with self.assertRaisesRegex(ValueError, "unsafe"):
                    self.read()
        with self.assertRaises(ValueError):
            state.parse_json(b'{"status":"Accepted","status":"Invalid"}')
        with self.assertRaises(ValueError):
            state.parse_json(b'{"value":NaN}')

    def test_local_snapshot_rejects_unallowlisted_private_material(self):
        (self.directory / "notary.p8").write_text("synthetic credential")
        with self.assertRaisesRegex(ValueError, "unexpected checkpoint file"):
            state.snapshot(self.directory, self.destination, "submitted", True, self.current)

    def test_only_trusted_nightly_run_can_be_continued(self):
        for change in (dict(path=".github/workflows/other.yml"), dict(head_branch="feature"),
                       dict(event="pull_request"), dict(head_repository=dict(id=2))):
            with self.subTest(change=change), self.assertRaises(ValueError):
                state.trusted_run(dict(self.run, **change), self.current["repository"])

    def recover(self, *, pending=True, run=None, latest=None, ancestry="ahead"):
        record, files = self.read()
        record["recovery_pending"] = pending
        selected_run = run or self.run
        calls = []

        def api(endpoint, **kwargs):
            calls.append((endpoint, kwargs))
            if "/runs?" in endpoint:
                return [dict(workflow_runs=[selected_run])]
            if "/compare/" in endpoint:
                return dict(status=ancestry)
            if endpoint.endswith("/rerun-failed-jobs"):
                return b""
            return latest or selected_run

        with patch.object(state, "api", side_effect=api), patch.object(state, "artifacts", return_value=[self.artifact]), patch.object(state, "read_checkpoint", return_value=(record, files)):
            state.recover(self.current["repository"])
        return [endpoint for endpoint, kwargs in calls if kwargs.get("method") == "POST"]

    def test_pending_reruns_failed_jobs_only_and_rechecks_attempt(self):
        self.assertEqual(self.recover(), ["repos/Hikyo-Org/Hikyo/actions/runs/42/rerun-failed-jobs"])
        self.assertEqual(self.recover(pending=False), [])
        self.assertEqual(self.recover(latest=dict(self.run, run_attempt=2, status="in_progress")), [])
        self.assertEqual(self.recover(run=dict(self.run, conclusion="cancelled")), [])
        self.assertEqual(self.recover(run=dict(self.run, run_attempt=12)), [])
        self.assertEqual(self.recover(run=dict(self.run, created_at="2020-01-01T00:00:00Z")), [])
        with self.assertRaisesRegex(ValueError, "left main"):
            self.recover(ancestry="diverged")


class ImmutableEvidenceTests(unittest.TestCase):
    def test_exact_installed_and_submitted_bytes_required_before_notes_change(self):
        for mutation in ("none", "source", "image", "binary", "log", "native-missing"):
            with self.subTest(mutation=mutation), tempfile.TemporaryDirectory() as directory:
                root = Path(directory)
                native = root / "native"
                native.mkdir()
                current = dict(source_commit="a" * 40, run_id="42")
                evidence = dict(schema="hikyo.dev/apple-release-evidence/v1", receipts=[], logs=[])
                for number, arch in enumerate(("arm64", "x86_64")):
                    image = ("synthetic image " + arch).encode()
                    binary = ("synthetic binary " + arch).encode()
                    filename = f"hikyo_1.2.3_Darwin_{arch}.dmg"
                    (root / filename).write_bytes(image)
                    image_hash, binary_hash = hashlib.sha256(image).hexdigest(), hashlib.sha256(binary).hexdigest()
                    with tarfile.open(root / f"hikyo_1.2.3_Darwin_{arch}.tar.gz", "w:gz") as archive:
                        entry = tarfile.TarInfo("hikyo")
                        entry.size = len(binary)
                        archive.addfile(entry, io.BytesIO(binary))
                    submission = f"00000000-1111-2222-3333-{number:012d}"
                    evidence["receipts"].append(dict(architecture=arch, status="Accepted", diagnostics=dict(availability="available"),
                        source_commit="a" * 40, original_run_id="42", version="1.2.3", archive_filename=filename,
                        submission_id=submission, submitted_sha256="b" * 64, final_sha256=image_hash))
                    evidence["logs"].append(dict(jobId=submission, status="Accepted", archiveFilename=filename, sha256="b" * 64))
                    receipt = dict(schema="hikyo.dev/apple-install-receipt/v1", source_commit="a" * 40, run_id="42", architecture=arch,
                                   version="1.2.3", input_artifact_id="7", final_dmg_sha256=image_hash, installed_binary_sha256=binary_hash,
                                   gatekeeper=True, quarantine_retained=True, native_version=True, native_help=True)
                    if arch == "arm64":
                        if mutation == "source": receipt["source_commit"] = "c" * 40
                        if mutation == "image": receipt["final_dmg_sha256"] = "c" * 64
                        if mutation == "binary": receipt["installed_binary_sha256"] = "c" * 64
                        if mutation == "log": evidence["logs"][0]["sha256"] = "c" * 64
                        if mutation == "native-missing": continue
                    (native / f"install-{arch}.json").write_text(json.dumps(receipt))
                (root / "apple-notarization-evidence.json").write_text(json.dumps(evidence))
                notes = root / "notes.txt"
                notes.write_text("original notes")
                if mutation == "none":
                    state.append_evidence(root, native, notes, current, "1.2.3", "7")
                    self.assertIn('"quarantine_retained": true', notes.read_text())
                else:
                    with self.assertRaises(ValueError):
                        state.append_evidence(root, native, notes, current, "1.2.3", "7")
                    self.assertEqual(notes.read_text(), "original notes")

    def test_missing_native_pair_cannot_be_bound_into_notes(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / "apple-notarization-evidence.json").write_text(json.dumps(dict(
                schema="hikyo.dev/apple-release-evidence/v1", receipts=[{}, {}], logs=[{}, {}])))
            notes = root / "notes.txt"
            notes.write_text("original notes")
            with self.assertRaisesRegex(ValueError, "both native"):
                state.append_evidence(root, root / "missing", notes, {}, "1.2.3", "7")
            self.assertEqual(notes.read_text(), "original notes")


if __name__ == "__main__":
    unittest.main()
