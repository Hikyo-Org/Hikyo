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
        def api(endpoint, **_kwargs):
            return [dict(jobs=[])] if "/attempts/" in endpoint else self.run

        with patch.object(state, "api", side_effect=api), patch.object(state, "artifacts", return_value=[]):
            with self.assertRaisesRegex(ValueError, "do not resubmit"):
                state.restore(self.root / "retry", dict(self.current, attempt="2"))

    def test_retry_can_create_first_intents_after_proven_skipped_notarization(self):
        job = dict(name="apple-notarize", run_id=42, run_attempt=1, head_sha="a" * 40,
                   status="completed", conclusion="skipped", steps=[])

        def api(endpoint, **_kwargs):
            return [dict(jobs=[job])] if "/attempts/" in endpoint else self.run

        with patch.object(state, "api", side_effect=api), patch.object(state, "artifacts", return_value=[]):
            state.restore(self.root / "first-intents", dict(self.current, attempt="2"))
        self.assertEqual(list((self.root / "first-intents").iterdir()), [])
        job.update(conclusion="failure", steps=[dict(name=f"Submit {arch} once or reconcile its UUID", status="completed",
                                                    conclusion="failure" if arch == "arm64" else "skipped")
                                                for arch in ("arm64", "x86_64")])
        with patch.object(state, "api", side_effect=api):
            self.assertFalse(state.no_previous_upload(dict(self.current, attempt="2")))

    def test_interrupted_wait_receipt_overrides_missing_outputs(self):
        path = self.directory / "arm64-receipt.json"
        receipt = json.loads(path.read_text())
        receipt.update(phase="Submitted", status="In Progress", submission_id="aaaaaaaa-1111-2222-3333-444444444444")
        path.write_text(json.dumps(receipt))
        state.snapshot(self.directory, self.destination, "finalized", False, self.current)
        self.set_archive(self.destination.read_bytes())
        record, _files = self.read()
        self.assertTrue(record["recovery_pending"])

    def test_accepted_unavailable_logs_resume_after_outputs_are_lost(self):
        for availability, blocked, expected in (("unavailable", False, True), ("unavailable", True, False), ("invalid", False, False)):
            with self.subTest(availability=availability, blocked=blocked):
                for number, arch in enumerate(("arm64", "x86_64")):
                    path = self.directory / f"{arch}-receipt.json"
                    receipt = json.loads(path.read_text())
                    receipt.update(phase="Submitted", status="Accepted", diagnostics=dict(availability=availability),
                                   submission_id=f"00000000-1111-2222-3333-{number:012d}",
                                   finalization_state="Blocked" if blocked else "NotStarted")
                    path.write_text(json.dumps(receipt))
                state.snapshot(self.directory, self.destination, "finalized", False, self.current)
                self.set_archive(self.destination.read_bytes())
                record, files = self.read()
                self.assertEqual(record["recovery_pending"], expected)
                with patch.object(state, "artifacts", return_value=[self.artifact]), patch.object(state, "read_checkpoint", return_value=(record, files)), patch.object(state, "api", side_effect=[dict(status="ahead"), self.run, b""]) as api:
                    self.assertEqual(state.recover_one(self.current["repository"], self.run), expected)
                    self.assertEqual(any(call.kwargs.get("method") == "POST" for call in api.call_args_list), expected)

    def test_per_architecture_checkpoint_survives_before_next_upload(self):
        path = self.directory / "arm64-receipt.json"
        receipt = json.loads(path.read_text())
        receipt.update(phase="Submitted", status="Unknown", submission_id="aaaaaaaa-1111-2222-3333-444444444444")
        path.write_text(json.dumps(receipt))
        state.snapshot(self.directory, self.destination, "submitted-arm64", True, self.current)
        self.set_archive(self.destination.read_bytes())
        self.artifact["name"] = "apple-notary-state-42-1-submitted-arm64"
        record, files = self.read()
        self.assertEqual(record["stage"], "submitted-arm64")
        self.assertEqual(state.parse_json(files["arm64-receipt.json"])["submission_id"], receipt["submission_id"])

    def test_only_all_skipped_upload_steps_allow_an_old_prepared_intent(self):
        files = {path.name: path.read_bytes() for path in self.directory.iterdir()}
        for arch in ("arm64", "x86_64"):
            receipt = state.parse_json(files[f"{arch}-receipt.json"])
            receipt.update(phase="Prepared", original_run_attempt="1", submission_id=None)
            files[f"{arch}-receipt.json"] = json.dumps(receipt).encode()
        calls = []

        def previous_jobs(endpoint, **_kwargs):
            attempt = int(endpoint.split("/attempts/")[1].split("/")[0])
            calls.append(attempt)
            steps = [dict(name=f"Submit {arch} once or reconcile its UUID", status="completed", conclusion="skipped")
                     for arch in ("arm64", "x86_64")]
            if attempt == 1:
                steps[0]["conclusion"] = "failure"
            return [dict(jobs=[dict(name="apple-notarize", run_id=42, run_attempt=attempt, head_sha="a" * 40, steps=steps)])]

        with patch.object(state, "api", side_effect=previous_jobs):
            self.assertEqual(state.unstarted_architectures(files, dict(self.current, attempt="3")), ["x86_64"])
        self.assertEqual(calls, [1, 2])
        with patch.object(state, "api", return_value=[dict(jobs=[])]):
            self.assertEqual(state.unstarted_architectures(files, dict(self.current, attempt="2")), [])

    def test_interrupted_finalization_resumes_but_blocked_trust_does_not(self):
        for disposition, expected in (("InProgress", True), ("Pending", True), ("Blocked", False), ("Complete", False)):
            with self.subTest(disposition=disposition):
                for arch in ("arm64", "x86_64"):
                    path = self.directory / f"{arch}-receipt.json"
                    receipt = json.loads(path.read_text())
                    receipt.update(phase="Submitted", status="Accepted", finalization_state=disposition,
                                   diagnostics=dict(availability="available"), submission_id="aaaaaaaa-1111-2222-3333-444444444444")
                    path.write_text(json.dumps(receipt))
                state.snapshot(self.directory, self.destination, "finalized", False, self.current)
                self.set_archive(self.destination.read_bytes())
                record, _files = self.read()
                self.assertEqual(record["recovery_pending"], expected)

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

    def test_same_invocation_receipt_is_not_an_actions_handoff(self):
        path = self.directory / "arm64-receipt.json"
        receipt = json.loads(path.read_text())
        receipt["input_binding"] = "same-invocation"
        path.write_text(json.dumps(receipt))
        with self.assertRaisesRegex(ValueError, "unbound"):
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
        with self.assertRaisesRegex(ValueError, "checkpoints refused"):
            self.recover(ancestry="diverged")

    def test_stale_checkpoint_attempt_is_filtered_before_download(self):
        with patch.object(state, "artifacts", return_value=[self.artifact]), patch.object(state, "read_checkpoint") as read:
            self.assertFalse(state.recover_one(self.current["repository"], dict(self.run, run_attempt=2)))
        read.assert_not_called()

    def test_ineligible_historical_events_skip_without_artifact_access(self):
        for change in (dict(conclusion="success"), dict(run_attempt=12),
                       dict(created_at="2020-01-01T00:00:00Z")):
            with self.subTest(change=change), patch.object(state, "artifacts") as artifacts, patch.object(state, "api") as api:
                historical = dict(self.run, event="workflow_dispatch", **change)
                self.assertFalse(state.recover_one(self.current["repository"], historical))
                artifacts.assert_not_called()
                api.assert_not_called()

    def test_eligible_untrusted_run_is_refused_before_artifact_access(self):
        for change in (dict(event="workflow_dispatch"), dict(head_repository=dict(id=2))):
            with self.subTest(change=change), patch.object(state, "artifacts") as artifacts, patch.object(state, "api") as api:
                with self.assertRaisesRegex(ValueError, "untrusted workflow run"):
                    state.recover_one(self.current["repository"], dict(self.run, **change))
                artifacts.assert_not_called()
                api.assert_not_called()

    def test_invalid_run_does_not_starve_an_independent_candidate_and_failure_propagates(self):
        with patch.object(state, "api", return_value=[dict(workflow_runs=[self.run, dict(self.run, id=43)])]), patch.object(state, "recover_one", side_effect=[ValueError("synthetic malformed checkpoint"), True]) as recover:
            with self.assertRaisesRegex(ValueError, "checkpoints refused"):
                state.recover(self.current["repository"])
        self.assertEqual([call.args[1]["id"] for call in recover.call_args_list], [42, 43])


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
                                   gatekeeper=True, executable_notarization=True, quarantine_retained=True, native_version=True, native_help=True)
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
