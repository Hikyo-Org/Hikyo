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


def dev_proof():
    return dict(schema="hikyo.dev/source-validation/v1", repository="Hikyo-Org/Hikyo", repository_id=1,
                commit="b" * 40, ref="refs/heads/dev", workflow_path=".github/workflows/ci-dev.yml", event="push",
                run_id=17, run_attempt=2, workflow_id=3, authority_commit="a" * 40,
                configuration_sha256="c" * 64, proof_sha256="d" * 64)


class SourceIdentityTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.path = Path(self.temporary.name) / "source-validation.json"
        self.proof = dev_proof()
        self.environ = dict(GITHUB_REPOSITORY="Hikyo-Org/Hikyo", GITHUB_RUN_ID="42", GITHUB_RUN_ATTEMPT="1",
                            GITHUB_SHA="a" * 40, GITHUB_REF="refs/heads/main", APPLE_RELEASE_CHANNEL="nightly",
                            APPLE_RELEASE_SOURCE_MODE="dev", APPLE_RELEASE_COMMIT="b" * 40,
                            APPLE_RELEASE_SOURCE_VALIDATION=str(self.path))
        self.write_proof(self.proof)

    def write_proof(self, proof):
        raw = json.dumps(proof).encode()
        self.path.write_bytes(raw)
        self.environ["APPLE_RELEASE_SOURCE_VALIDATION_SHA256"] = hashlib.sha256(raw).hexdigest()

    def test_exact_dev_source_and_original_publisher_are_distinct(self):
        current = state.identity(self.environ)
        self.assertEqual(current["source_commit"], "b" * 40)
        self.assertEqual(current["source_ref"], "refs/heads/dev")
        self.assertEqual(current["publisher_commit"], "a" * 40)
        self.assertEqual(current["publisher_ref"], "refs/heads/main")
        self.assertEqual(current["source_validation"], self.proof)
        with patch.object(state, "api") as api:
            state.source_identity(self.environ)
        api.assert_not_called()  # Live CI/policy authentication is the trusted workflow's prior gate.

    def test_descriptor_digest_and_all_source_proof_authority_fields_are_bound(self):
        for key, value in (("schema", "other"), ("repository", "other/repo"), ("commit", "c" * 40),
                           ("ref", "refs/heads/main"), ("workflow_path", ".github/workflows/ci.yml"),
                           ("event", "pull_request"), ("authority_commit", "c" * 40), ("run_id", True),
                           ("run_attempt", 0), ("workflow_id", "3"), ("repository_id", -1), ("run_id", 2**53),
                           ("configuration_sha256", "invalid"), ("proof_sha256", "invalid"), ("extra", "ignored")):
            with self.subTest(field=key), self.assertRaises(ValueError):
                self.write_proof(dict(self.proof, **{key: value}))
                state.identity(self.environ)
        self.write_proof(self.proof)
        self.path.write_text("{}")
        with self.assertRaisesRegex(ValueError, "digest mismatch"):
            state.identity(self.environ)

    def test_source_validation_id_uses_the_signed_protocol_integer_bound(self):
        self.write_proof(dict(self.proof, run_id=2**53 - 1))
        self.assertEqual(state.identity(self.environ)["source_validation"]["run_id"], 2**53 - 1)

    def test_explicit_mode_and_trusted_main_publisher_are_required(self):
        for key, value in (("APPLE_RELEASE_SOURCE_MODE", "main"), ("APPLE_RELEASE_SOURCE_MODE", "unknown"),
                           ("APPLE_RELEASE_CHANNEL", "stable"), ("GITHUB_REF", "refs/heads/dev"),
                           ("GITHUB_SHA", "c" * 40), ("APPLE_RELEASE_COMMIT", "c" * 40),
                           ("APPLE_RELEASE_SOURCE_VALIDATION_SHA256", "")):
            with self.subTest(field=key), self.assertRaises(ValueError):
                state.identity(dict(self.environ, **{key: value}))
        self.path.unlink()
        self.path.symlink_to("missing-descriptor")
        with self.assertRaises(ValueError):
            state.identity(self.environ)

    def test_legacy_main_and_stable_identity_have_no_new_authority_fields(self):
        environ = {key: value for key, value in self.environ.items()
                   if key not in ("APPLE_RELEASE_SOURCE_MODE", "APPLE_RELEASE_SOURCE_VALIDATION", "APPLE_RELEASE_SOURCE_VALIDATION_SHA256")}
        environ["APPLE_RELEASE_COMMIT"] = environ["GITHUB_SHA"]
        current = state.identity(environ)
        self.assertFalse(set(state.PUBLISHER_FIELDS) & current.keys())
        current = state.identity(dict(environ, APPLE_RELEASE_CHANNEL="stable", GITHUB_REF="refs/tags/v1.2.3"))
        self.assertEqual(current["source_commit"], environ["GITHUB_SHA"])
        self.assertFalse(set(state.PUBLISHER_FIELDS) & current.keys())
        with self.assertRaises(ValueError):
            state.identity(dict(environ, APPLE_RELEASE_COMMIT="b" * 40))


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

    def use_dev_source(self):
        self.current.update(source_commit="b" * 40, source_ref="refs/heads/dev", publisher_commit="a" * 40,
                            publisher_ref="refs/heads/main", source_validation=dev_proof())
        for arch in ("arm64", "x86_64"):
            path = self.directory / f"{arch}-receipt.json"
            receipt = state.parse_json(path.read_bytes())
            receipt.update({key: self.current[key] for key in ("source_commit", "source_ref", *state.PUBLISHER_FIELDS)})
            path.write_text(json.dumps(receipt))
        state.snapshot(self.directory, self.destination, "finalized", True, self.current)
        self.set_archive(self.destination.read_bytes())

    def test_dev_checkpoint_restores_original_source_and_publisher(self):
        self.use_dev_source()
        record, files = self.read()
        self.assertEqual(record["schema"], state.DEV_SCHEMA)
        self.assertEqual(record["source_commit"], "b" * 40)
        self.assertEqual(record["publisher_commit"], self.run["head_sha"])
        with patch.object(state, "api", return_value=self.run), patch.object(state, "artifacts", return_value=[self.artifact]), patch.object(state, "read_checkpoint", return_value=(record, files)):
            state.restore(self.root / "dev-restored", dict(self.current, attempt="2"))
        restored = state.parse_json((self.root / "dev-restored/arm64-receipt.json").read_bytes())
        self.assertEqual(restored["source_validation"], dev_proof())
        self.assertEqual(self.recover(), ["repos/Hikyo-Org/Hikyo/actions/runs/42/rerun-failed-jobs"])
        with self.assertRaisesRegex(ValueError, "checkpoints refused"):
            self.recover(run=dict(self.run, head_sha="b" * 40))

    def test_dev_retry_upload_proofs_authenticate_original_publisher(self):
        self.use_dev_source()
        files = {path.name: path.read_bytes() for path in self.directory.iterdir()}
        for arch in ("arm64", "x86_64"):
            receipt = state.parse_json(files[f"{arch}-receipt.json"])
            receipt.update(phase="Prepared", original_run_attempt="1", submission_id=None)
            files[f"{arch}-receipt.json"] = json.dumps(receipt).encode()
        job = dict(name="apple-notarize", run_id=42, run_attempt=1, head_sha="a" * 40,
                   status="completed", conclusion="skipped", steps=[
                       dict(name=f"Submit {arch} once or reconcile its UUID", status="completed", conclusion="skipped")
                       for arch in ("arm64", "x86_64")])
        with patch.object(state, "api", return_value=[dict(jobs=[job])]):
            self.assertTrue(state.no_previous_upload(dict(self.current, attempt="2")))
            self.assertEqual(state.unstarted_architectures(files, dict(self.current, attempt="2")), ["arm64", "x86_64"])
        job["head_sha"] = "b" * 40
        with patch.object(state, "api", return_value=[dict(jobs=[job])]):
            with self.assertRaisesRegex(ValueError, "job identity mismatch"):
                state.no_previous_upload(dict(self.current, attempt="2"))
            with self.assertRaisesRegex(ValueError, "job identity mismatch"):
                state.unstarted_architectures(files, dict(self.current, attempt="2"))

    def test_snapshot_rejects_partial_or_changed_dev_binding(self):
        self.use_dev_source()
        path = self.directory / "arm64-receipt.json"
        original = state.parse_json(path.read_bytes())
        for key in state.PUBLISHER_FIELDS:
            receipt = deepcopy(original)
            receipt.pop(key)
            path.write_text(json.dumps(receipt))
            with self.subTest(field=key), self.assertRaises(ValueError):
                state.snapshot(self.directory, self.destination, "finalized", True, self.current)
        path.write_text(json.dumps(original))
        record, files = self.read()
        changed = deepcopy(self.current)
        changed["source_validation"]["run_attempt"] = 3
        with patch.object(state, "api", return_value=self.run), patch.object(state, "artifacts", return_value=[self.artifact]), patch.object(state, "read_checkpoint", return_value=(record, files)):
            with self.assertRaisesRegex(ValueError, "restored source mismatch"):
                state.restore(self.root / "changed-proof", dict(changed, attempt="2"))

    def test_dev_checkpoint_cannot_downgrade_or_change_retained_proof(self):
        self.use_dev_source()
        original, files = self.read()
        for mutation in ("legacy-schema", "missing-publisher", "publisher", "source", "unsafe-id", "proof-field", "receipt-proof", "receipt-proof-type"):
            record, retained = deepcopy(original), dict(files)
            if mutation == "legacy-schema": record["schema"] = state.SCHEMA
            if mutation == "missing-publisher": record.pop("publisher_commit")
            if mutation == "publisher": record["publisher_commit"] = "c" * 40
            if mutation == "source": record["source_commit"] = "c" * 40
            if mutation == "unsafe-id": record["source_validation"]["run_id"] = 2**53
            if mutation == "proof-field": record["source_validation"]["unexpected"] = "ignored"
            if mutation in ("receipt-proof", "receipt-proof-type"):
                receipt = state.parse_json(retained["arm64-receipt.json"])
                if mutation == "receipt-proof": receipt["source_validation"]["run_attempt"] = 3
                else: receipt["source_validation"]["repository_id"] = True
                retained["arm64-receipt.json"] = json.dumps(receipt).encode()
                record["files"]["arm64-receipt.json"] = hashlib.sha256(retained["arm64-receipt.json"]).hexdigest()
            retained["checkpoint.json"] = json.dumps(record).encode()
            buffer = io.BytesIO()
            with tarfile.open(fileobj=buffer, mode="w:") as archive:
                for name, data in retained.items():
                    entry = tarfile.TarInfo(name)
                    entry.size = len(data)
                    archive.addfile(entry, io.BytesIO(data))
            self.set_archive(buffer.getvalue())
            with self.subTest(mutation=mutation), self.assertRaises(ValueError):
                self.read()

    def test_legacy_receipts_cannot_import_dev_publisher_fields(self):
        path = self.directory / "arm64-receipt.json"
        receipt = state.parse_json(path.read_bytes())
        receipt["publisher_commit"] = "a" * 40
        path.write_text(json.dumps(receipt))
        with self.assertRaises(ValueError):
            state.snapshot(self.directory, self.destination, "finalized", True, self.current)

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
        with self.assertRaisesRegex(ValueError, "checkpoints refused"):
            self.recover(latest=dict(self.run, head_sha="b" * 40))

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
        mutations = ("none", "source", "image", "binary", "log", "native-missing", "publisher", "proof", "receipt-source", "native-publisher")
        cases = [(mode, mutation) for mode in ("main", "dev") for mutation in mutations] + [("dev", "source-ref")]
        for mode, mutation in cases:
            with self.subTest(mode=mode, mutation=mutation), tempfile.TemporaryDirectory() as directory:
                root = Path(directory)
                native = root / "native"
                native.mkdir()
                current = dict(source_commit="a" * 40, run_id="42")
                if mode == "dev":
                    current.update(source_commit="b" * 40, source_ref="refs/heads/dev", channel="nightly",
                                   repository="Hikyo-Org/Hikyo", publisher_commit="a" * 40,
                                   publisher_ref="refs/heads/main", source_validation=dev_proof())
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
                        source_commit=current["source_commit"], original_run_id="42", version="1.2.3", archive_filename=filename,
                        submission_id=submission, submitted_sha256="b" * 64, final_sha256=image_hash))
                    if mode == "dev":
                        evidence["receipts"][-1].update(schema="hikyo.dev/apple-notarization-receipt/v2",
                            **deepcopy({key: current[key] for key in ("source_ref", "channel", "repository", *state.PUBLISHER_FIELDS)}))
                    evidence["logs"].append(dict(jobId=submission, status="Accepted", archiveFilename=filename, sha256="b" * 64))
                    receipt = dict(schema="hikyo.dev/apple-install-receipt/v1", source_commit=current["source_commit"], run_id="42", architecture=arch,
                                   version="1.2.3", input_artifact_id="7", final_dmg_sha256=image_hash, installed_binary_sha256=binary_hash,
                                   gatekeeper=True, executable_notarization=True, quarantine_retained=True, native_version=True, native_help=True)
                    if mode == "dev":
                        receipt.update(publisher_commit=current["publisher_commit"], publisher_ref=current["publisher_ref"])
                    if arch == "arm64":
                        if mutation == "source": receipt["source_commit"] = "c" * 40
                        if mutation == "image": receipt["final_dmg_sha256"] = "c" * 64
                        if mutation == "binary": receipt["installed_binary_sha256"] = "c" * 64
                        if mutation == "log": evidence["logs"][0]["sha256"] = "c" * 64
                        if mutation == "native-missing": continue
                        if mutation == "publisher": evidence["receipts"][0]["publisher_commit"] = "c" * 40
                        if mutation == "proof": evidence["receipts"][0]["source_validation"] = dict(dev_proof(), run_attempt=3)
                        if mutation == "receipt-source": evidence["receipts"][0]["source_commit"] = "c" * 40
                        if mutation == "source-ref": evidence["receipts"][0]["source_ref"] = "refs/heads/main"
                        if mutation == "native-publisher": receipt["publisher_commit"] = "c" * 40
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
