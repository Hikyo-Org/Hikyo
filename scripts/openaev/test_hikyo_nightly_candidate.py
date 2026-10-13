"""Reject unbound candidate identities and unsafe public bundle transport."""

from __future__ import annotations

import hashlib
import importlib.util
import json
import tempfile
import unittest
from pathlib import Path
from unittest.mock import Mock, patch

import hikyo_nightly_candidate as native
from hikyo_nightly_release import Candidate

SHA = "36c323c417d1c29b20431a8c4bdb539882b9136c"
CANDIDATE = Candidate(
    407711634, "v0.0.1-nightly.20261009.72.g36c323c4", SHA, "2026-10-09T01:00:00Z"
)


class CandidateTests(unittest.TestCase):
    def setUp(self) -> None:
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name)
        self.cache = self.root / "cache"
        self.cache.mkdir()
        self.bundle = self.cache / "bundle"
        self.bundle.mkdir()
        (self.bundle / "index.json").write_text('{"native":true}')
        (self.cache / "binary").write_bytes(b"synthetic never executed executable")
        self.output: dict[str, object] = {
            "identity": {
                "profile": "nightly/v1",
                "version": CANDIDATE.tag[1:],
                "commit": SHA,
                "manifest_sha256": "a" * 64,
            },
            "bundle_directory": str(self.bundle),
            "binary_path": str(self.cache / "binary"),
            "binary_sha256": hashlib.sha256(
                (self.cache / "binary").read_bytes()
            ).hexdigest(),
            "artifacts": [
                {
                    "kind": "image",
                    "image": "ghcr.io/hikyo-org/hikyo",
                    "tag": CANDIDATE.tag[1:],
                    "digest": "sha256:" + "b" * 64,
                }
            ],
        }

    def test_native_output_binds_exact_candidate_and_canonical_tree(self) -> None:
        proof = native._verified_output(self.output, CANDIDATE, self.cache)
        rows = [
            {
                "path": "index.json",
                "size": 15,
                "sha256": hashlib.sha256(b'{"native":true}').hexdigest(),
            }
        ]
        rows[0]["size"] = len(b'{"native":true}')
        digest = hashlib.sha256(
            json.dumps(rows, sort_keys=True, separators=(",", ":")).encode()
        ).hexdigest()
        self.assertEqual(proof["bundle_tree_sha256"], digest)
        self.assertEqual(proof["expected_version"], CANDIDATE.tag[1:])

    def test_stable_and_signed_rc_use_native_stable_profile(self) -> None:
        for tag in ("v1.0.0", "v1.0.0-rc.1", "v1.0.0-foo-nightly.1"):
            candidate = Candidate(12, tag, SHA, "2026-10-13T01:00:00Z")
            output = {**self.output, "identity": {**native._object(self.output["identity"]),
                      "profile": "stable/v1", "version": tag[1:]}, "artifacts": [
                      {**native._object(self.output["artifacts"][0]), "tag": tag[1:]}]}
            proof = native._verified_output(output, candidate, self.cache)
            self.assertEqual(proof["native_verification"], "stable/v1")

    def test_signed_stable_image_inventory_binds_exact_repository_tag_and_digest(self) -> None:
        candidate = Candidate(12, "v1.0.0-rc.1", SHA, "2026-10-13T01:00:00Z")
        image = {"kind": "image", "image": "ghcr.io/hikyo-org/hikyo", "tag": "1.0.0-rc.1",
                 "digest": "sha256:" + "b" * 64}
        output = {**self.output, "identity": {**native._object(self.output["identity"]),
                  "profile": "stable/v1", "version": candidate.tag[1:]}, "artifacts": [image]}
        proof = native._verified_output(output, candidate, self.cache)
        self.assertEqual(proof["signed_image_ref"], "ghcr.io/hikyo-org/hikyo@sha256:" + "b" * 64)
        for artifacts in ([], [image, image], [dict(image, tag="2.0.0")],
                          [dict(image, image="ghcr.io/attacker/image")], [dict(image, digest="unsigned")]):
            with self.subTest(artifacts=artifacts), self.assertRaises(native.CandidateError):
                native._verified_output(dict(output, artifacts=artifacts), candidate, self.cache)
        nightly_output = dict(self.output, artifacts=[])
        self.assertIsNone(native._verified_output(nightly_output, CANDIDATE, self.cache)["signed_image_ref"])

    def test_mutable_tag_digest_cannot_replace_the_signed_stable_image(self) -> None:
        candidate = Candidate(12, "v1.0.0-rc.1", SHA, "2026-10-13T01:00:00Z")
        signed = "ghcr.io/hikyo-org/hikyo@sha256:" + "a" * 64
        redirected = "ghcr.io/hikyo-org/hikyo@sha256:" + "b" * 64
        with (patch.object(native, "validate_candidate", return_value={"immutable": True}),
              patch.object(native, "command", side_effect=[b"unix:///var/run/docker.sock", b"", b"{}"]),
              patch.dict(native.os.environ, {"DOCKER_HOST": ""}),
              patch.object(native, "_verified_output", return_value={"signed_image_ref": signed}),
              patch.object(native, "resolve_image", return_value=redirected),
              patch.object(native, "verify_image") as verify,
              patch.object(native, "verify_image_binary") as binary,
              self.assertRaisesRegex(native.CandidateError, "signed_manifest_digest_mismatch")):
            native.prepare_candidate(candidate, self.root / "fresh-preparation")
        verify.assert_not_called()
        binary.assert_not_called()

    def test_wrong_source_version_or_profile_is_rejected(self) -> None:
        for field, replacement in [
            ("commit", "c" * 40),
            ("version", "0.0.1-nightly.other"),
            ("profile", "stable/v1"),
        ]:
            data = {
                **self.output,
                "identity": {
                    **native._object(self.output["identity"]),
                    field: replacement,
                },
            }
            with self.assertRaisesRegex(native.CandidateError, "identity_mismatch"):
                native._verified_output(data, CANDIDATE, self.cache)

    def test_image_resolution_only_uses_exact_official_version(self) -> None:
        with patch.object(
            native,
            "command",
            return_value=json.dumps({"digest": "sha256:" + "b" * 64}).encode(),
        ) as invoke:
            self.assertEqual(
                native.resolve_image(CANDIDATE),
                "ghcr.io/hikyo-org/hikyo@sha256:" + "b" * 64,
            )
        self.assertIn(
            "ghcr.io/hikyo-org/hikyo:" + CANDIDATE.tag[1:], invoke.call_args.args[0]
        )
        with (
            patch.object(native, "command", return_value=b'{"digest":"unsigned"}'),
            self.assertRaises(native.CandidateError),
        ):
            native.resolve_image(CANDIDATE)

    def test_image_payload_is_not_executed_and_must_match_native_digest(self) -> None:
        calls: list[list[str]] = []

        def invoke(arguments: list[str], **_options: object) -> bytes:
            calls.append(arguments)
            if arguments[1] == "cp":
                Path(arguments[-1]).write_bytes(b"synthetic image executable")
            return b"container"

        with (
            patch.object(native, "command", side_effect=invoke),
            self.assertRaisesRegex(native.CandidateError, "binary_mismatch"),
        ):
            native.verify_image_binary("ghcr.io/hikyo-org/hikyo@sha256:" + "b" * 64, "0" * 64)
        self.assertEqual([call[1] for call in calls], ["create", "cp", "rm"])

    def test_native_binary_cannot_change_after_verification(self) -> None:
        (self.cache / "binary").write_bytes(b"changed")
        with self.assertRaisesRegex(native.CandidateError, "binary_changed"):
            native._verified_output(self.output, CANDIDATE, self.cache)

    def test_native_paths_never_escape_cache(self) -> None:
        for path in [
            "/tmp/bundle",
            "/cache/../operator.key",
            "https://other/bundle",
            None,
        ]:
            with self.assertRaisesRegex(native.CandidateError, "path_boundary"):
                native._host_path(path, self.cache)

    def test_bundle_rejects_symlinks_and_aggregate_overflow(self) -> None:
        (self.bundle / "linked").symlink_to(self.cache / "binary")
        with self.assertRaisesRegex(native.CandidateError, "bundle_member"):
            native.tree_inventory(self.bundle)
        (self.bundle / "linked").unlink()
        with (
            patch.object(native, "MAX_BUNDLE_BYTES", 4),
            self.assertRaisesRegex(native.CandidateError, "size_bound"),
        ):
            native.tree_inventory(self.bundle)
        with (
            patch.object(native, "MAX_FILES", 0),
            self.assertRaisesRegex(native.CandidateError, "inventory_bound"),
        ):
            native.tree_inventory(self.bundle)

    def test_tree_digest_uses_sorted_posix_relativepaths(self) -> None:
        nested = self.bundle / "a"
        nested.mkdir()
        (nested / "z.json").write_text("native data")
        rows = native.tree_inventory(self.bundle)
        self.assertEqual([row["path"] for row in rows], ["a/z.json", "index.json"])

    def test_signature_failure_prevents_image_pull(self) -> None:
        with (
            patch.object(
                native,
                "command",
                side_effect=native.CandidateError("signature_refused"),
            ) as invoke,
            self.assertRaises(native.CandidateError),
        ):
            native.verify_image(
                "ghcr.io/hikyo-org/hikyo@sha256:" + "b" * 64, self.root, "test", SHA
            )
        self.assertEqual(invoke.call_count, 1)

    def test_native_signed_index_requires_both_supported_platforms(self) -> None:
        raw = json.dumps(
            {
                "manifests": [
                    {"platform": {"os": "linux", "architecture": "arm64"}},
                    {"platform": {"os": "unknown", "architecture": "unknown"}},
                ]
            }
        ).encode()
        with (
            patch.object(native, "command", side_effect=[b"verified", raw]) as invoke,
            self.assertRaisesRegex(native.CandidateError, "platforms_missing"),
        ):
            native.verify_image(
                "ghcr.io/hikyo-org/hikyo@sha256:" + "b" * 64, self.root, "test", SHA
            )
        self.assertEqual(invoke.call_count, 2)
        self.assertIn(
            "--certificate-github-workflow-sha", invoke.call_args_list[0].args[0]
        )

    def test_untrusted_source_or_reused_state_never_runs_preparer(self) -> None:
        for directory, source in [(self.root, native.SOURCE_REPO),
                                  (self.root / "new", self.root / "untrusted-checkout")]:
            with (patch.object(native, "command") as invoke,
                  self.assertRaises(native.CandidateError)):
                native.prepare_candidate(CANDIDATE, directory, source_repo=source)
            invoke.assert_not_called()

    def test_pilot_default_and_explicit_candidate_version_guard(self) -> None:
        spec = importlib.util.spec_from_file_location(
            "nightly_pilot", Path(__file__).with_name("hikyo-pilot.py")
        )
        if spec is None or spec.loader is None:
            raise TypeError("pilot loader unavailable")
        pilot = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(pilot)
        arguments = Mock(
            state=self.root / "pilot",
            expected_version=CANDIDATE.tag[1:],
            origin="https://lab",
            ca=self.root / "ca",
            resume=False,
        )
        with (
            patch.object(pilot, "API") as api,
            patch.object(pilot, "host_command") as host,
        ):
            api.return_value.call.return_value = (
                200,
                {"server_version": "0.0.1-nightly.20261003.66.g5c3f969f"},
            )
            with self.assertRaisesRegex(SystemExit, "different Hikyo release"):
                pilot.bootstrap(arguments)
            host.assert_not_called()


if __name__ == "__main__":
    unittest.main()
