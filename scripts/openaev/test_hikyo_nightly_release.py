"""Release selection, count provenance and owned-section preservation boundaries."""

from __future__ import annotations

import json
import subprocess
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

import hikyo_nightly_release as release

SOURCE = "36c323c4" + "a" * 32
TAG = "v0.0.1-nightly.20261009.72.g36c323c4"
RUN = "7d5e9d0a-b6a0-403d-960b-9ef08b7cb51a"


def native_release(**changes: object) -> dict[str, object]:
    return {
        "id": 72,
        "tag_name": TAG,
        "published_at": "2026-10-09T08:30:37Z",
        "draft": False,
        "prerelease": True,
        "body": "Original release notes\n\nKeep this operator edit.",
        "updated_at": "2026-10-09T08:30:37Z",
    } | changes


def candidate() -> release.Candidate:
    return release.Candidate(72, TAG, SOURCE, "2026-10-09T08:30:37Z")


def suites(*statuses: str) -> tuple[release.SuiteResult, ...]:
    return (
        release.SuiteResult(
            "security",
            tuple(
                release.TestResult(
                    f"case-{index}", status, status in {"passed", "failed"}
                )
                for index, status in enumerate(statuses)
            ),
        ),
    )


class Client:
    def __init__(self) -> None:
        self.release = native_release()
        self.listing: list[dict[str, object]] = [self.release]
        self.source = SOURCE
        self.patches: list[dict[str, object]] = []
        self.reads = 0
        self.change_before_second = False
        self.change_after_write = False
        self.drop_section_after_write = False
        self.change_identity_after_write = False

    def request(self, path: str, body: dict[str, object] | None = None) -> object:
        if body is not None:
            self.patches.append(body)
            self.release |= body
            if self.change_after_write:
                self.release["body"] = (
                    str(self.release["body"]) + "\nA later human edit."
                )
            if self.drop_section_after_write:
                self.release["body"] = "Other writer removed owned section."
            if self.change_identity_after_write:
                self.release["tag_name"] = "v0.0.1-nightly.20261008.71.g36c323c4"
            return self.release
        if "/releases?" in path:
            return self.listing
        if "/git/ref/" in path:
            return {
                "ref": f"refs/tags/{TAG}",
                "object": {"sha": self.source, "type": "commit"},
            }
        if path.endswith("/releases/72"):
            self.reads += 1
            if self.change_before_second and self.reads == 2:
                self.release["body"] = "New prewrite operator edit."
            return dict(self.release)
        raise AssertionError("unexpected_path")


class SelectionTests(unittest.TestCase):
    def test_newest_published_nightly_uses_tag_commit_not_main(self) -> None:
        client = Client()
        client.listing = [
            native_release(
                id=100,
                tag_name="v1.0.0",
                published_at="2026-10-10T10:00:00Z",
                prerelease=False,
            ),
            native_release(id=73, draft=True, published_at="2026-10-11T10:00:00Z"),
            native_release(
                id=74, name="[REVOKED] Bad", published_at="2026-10-12T10:00:00Z"
            ),
            native_release(id=69, published_at="2026-10-06T08:34:59Z"),
            native_release(),
        ]
        self.assertEqual(release.discover_candidate(client), candidate())

    def test_latest_nightly_publication_ties_refuse_in_both_response_orders(self) -> None:
        rows = [native_release(), native_release(id=73, tag_name=TAG.replace(".72.", ".73."))]
        for listing in (rows, list(reversed(rows))):
            client = Client()
            client.listing = listing
            with self.subTest(order=[row["id"] for row in listing]), self.assertRaisesRegex(
                release.ReleaseError, "published_nightly_ambiguous"
            ):
                release.discover_candidate(client)

    def test_revocation_body_and_explicit_flag_excluded(self) -> None:
        for changes in ({"revoked": True}, {"body": "<!-- hikyo-release-revoked -->"}):
            client = Client()
            client.listing = [native_release(**changes)]
            with self.assertRaisesRegex(release.ReleaseError, "unavailable"):
                release.discover_candidate(client)

    def test_release_removed_or_source_ref_changed_rejected(self) -> None:
        for changes in (
            {"draft": True},
            {"tag_name": "v0.0.1-nightly.20261008.71.g36c323c4"},
            {"published_at": "2026-10-09T09:00:00Z"},
            {"id": True},
        ):
            client = Client()
            client.release |= changes
            with self.assertRaises(release.ReleaseError):
                release.validate_candidate(client, candidate())
        client = Client()
        client.source = "b" * 40
        with self.assertRaisesRegex(release.ReleaseError, "source_tag_mismatch"):
            release.validate_candidate(client, candidate())

    def test_annotated_tag_dereferenced_and_wrong_type_rejected(self) -> None:
        class Annotated(Client):
            def request(
                self, path: str, body: dict[str, object] | None = None
            ) -> object:
                if "/git/ref/" in path:
                    return {
                        "ref": f"refs/tags/{TAG}",
                        "object": {"sha": "b" * 40, "type": "tag"},
                    }
                if "/git/tags/" in path:
                    return {
                        "sha": "b" * 40,
                        "object": {"sha": SOURCE, "type": "commit"},
                    }
                return super().request(path, body)

        self.assertEqual(release.resolve_source(Annotated(), TAG), SOURCE)
        with (
            patch.object(
                Annotated,
                "request",
                return_value={
                    "ref": f"refs/tags/{TAG}",
                    "object": {"sha": SOURCE, "type": "tree"},
                },
            ),
            self.assertRaises(release.ReleaseError),
        ):
            release.resolve_source(Annotated(), TAG)

    def test_listing_limit_and_invalid_publication_fail_closed(self) -> None:
        client = Client()
        client.listing = [native_release() for _ in range(100)]
        with self.assertRaisesRegex(release.ReleaseError, "listing_limit"):
            release.discover_candidate(client)
        client.listing = [native_release(published_at="2026-10-09T08:30:37")]
        with self.assertRaisesRegex(release.ReleaseError, "publication_time_invalid"):
            release.discover_candidate(client)

    def test_invalid_candidate_identity_fails(self) -> None:
        for release_id, tag, sha in (
            (True, TAG, SOURCE),
            (72, "main", SOURCE),
            (72, TAG, "b" * 40),
            (72, TAG, "private-source"),
        ):
            with self.assertRaises(release.ReleaseError):
                release.Candidate(release_id, tag, sha, "2026-10-09T08:30:37Z")


class CountTests(unittest.TestCase):
    def test_counts_are_per_native_case_not_request_or_unit_test_totals(self) -> None:
        result = suites("passed", "failed", "skipped", "infra_error")
        self.assertEqual(
            release.summarise_results(result),
            {"security": {"passed": 1, "failed": 1, "skipped": 1, "infra_error": 1}},
        )
        self.assertEqual(release.outcome(result, ()), "INFRA ERROR")

    def test_skipped_empty_or_error_cannot_pass(self) -> None:
        for statuses, expected in (
            ((), "INCOMPLETE"),
            (("skipped",), "INCOMPLETE"),
            (("passed", "skipped"), "INCOMPLETE"),
            (("failed",), "FAIL"),
            (("passed",), "PASS"),
        ):
            self.assertEqual(release.outcome(suites(*statuses), ()), expected)
        self.assertEqual(
            release.outcome(suites("passed"), ("readiness-unavailable",)), "INFRA ERROR"
        )

    def test_pass_fail_require_verified_native_evidence(self) -> None:
        for status in ("passed", "failed"):
            with self.assertRaisesRegex(release.ReleaseError, "evidence_required"):
                release.TestResult("case", status, False)
        with self.assertRaises(release.ReleaseError):
            release.TestResult("case", "passed", json.loads("1"))

    def test_duplicate_cases_suites_or_public_html_inputs_rejected(self) -> None:
        row = release.TestResult("case", "passed", True)
        with self.assertRaises(release.ReleaseError):
            release.SuiteResult("security", (row, row))
        with self.assertRaises(release.ReleaseError):
            release.summarise_results((suites("passed")[0], suites("failed")[0]))
        for name in ("<script>", "private/token", "case|table", "case\nnewline"):
            with self.assertRaises(release.ReleaseError):
                release.TestResult(name, "passed", True)
        with self.assertRaises(release.ReleaseError):
            release.outcome(suites("passed"), ("https://private.invalid/path",))

    def test_cleanup_unverified_forces_infrastructure_badge(self) -> None:
        section = release.render_section(
            candidate(), RUN, suites("passed"), cleanup_verified=False
        )
        self.assertIn("INFRA%20ERROR", section)
        self.assertIn("UNVERIFIED, operator action required", section)
        self.assertIn("cleanup-unverified", section)
        self.assertNotIn("message=PASS", section)

    def test_badges_are_static_counts_and_url_encoded(self) -> None:
        section = release.render_section(
            candidate(),
            RUN,
            suites("passed", "failed", "skipped"),
            cleanup_verified=True,
        )
        self.assertIn(
            "img.shields.io/static/v1?label=OpenAEV%20lab&message=FAIL", section
        )
        self.assertIn("label=passed&message=1", section)
        self.assertIn("| security | 1 | 1 | 1 | 0 |", section)
        self.assertNotIn("192.168", section)


class SectionTests(unittest.TestCase):
    def test_owned_marker_replacement_preserves_exact_rest(self) -> None:
        section = release.render_section(
            candidate(), RUN, suites("passed"), cleanup_verified=True
        )
        old = f"PRE\n{release.START}\nOld count\n{release.END}\nPOST"
        self.assertEqual(release.replace_section(old, section), f"PRE\n{section}\nPOST")
        for body in (
            release.START,
            release.END,
            release.END + release.START,
            release.START + release.START + release.END,
        ):
            with self.assertRaises(release.ReleaseError):
                release.replace_section(body, section)







class ArtifactTests(unittest.TestCase):
    def test_immutable_reports_private_and_offline_with_sanitized_json(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            directory = Path(temporary) / "report"
            paths = release.write_report(
                directory, candidate(), RUN, suites("passed"), cleanup_verified=True
            )
            document = json.loads(paths["results.json"].read_text())
            self.assertEqual(document["suite_counts"]["security"]["passed"], 1)
            self.assertEqual(document["schema_version"], 1)
            self.assertEqual(paths["results.json"].stat().st_mode & 0o777, 0o600)
            self.assertIn("&amp;message=PASS", paths["results.html"].read_text())
            self.assertNotIn("<script", paths["results.html"].read_text())
            self.assertNotIn("<img", paths["results.html"].read_text())
            with self.assertRaisesRegex(release.ReleaseError, "must_be_empty"):
                release.write_report(
                    directory, candidate(), RUN, suites("failed"), cleanup_verified=True
                )

    def test_symlink_report_destination_rejected_without_target_mutation(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            destination, link = (
                Path(temporary) / "destination",
                Path(temporary) / "link",
            )
            destination.mkdir()
            link.symlink_to(destination)
            with self.assertRaisesRegex(release.ReleaseError, "symlink"):
                release.write_report(
                    link, candidate(), RUN, suites("passed"), cleanup_verified=True
                )
            self.assertEqual(list(destination.iterdir()), [])

    def test_symlink_created_during_mkdir_is_rejected_without_target_writes(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            destination, directory = Path(temporary) / "destination", Path(temporary) / "report"
            destination.mkdir()
            original_mode = destination.stat().st_mode
            original_mkdir = Path.mkdir
            def replace_with_link(path, *args, **kwargs):
                if path == directory:
                    path.symlink_to(destination)
                return original_mkdir(path, *args, **kwargs)
            with patch.object(Path, "mkdir", replace_with_link):
                with self.assertRaisesRegex(release.ReleaseError, "symlink"):
                    release.write_report(directory, candidate(), RUN, suites("passed"), cleanup_verified=True)
            self.assertEqual(list(destination.iterdir()), [])
            self.assertEqual(destination.stat().st_mode, original_mode)

    def test_failed_cleanup_json_matches_nonpassing_badge(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            paths = release.write_report(
                Path(temporary) / "report",
                candidate(),
                RUN,
                suites("passed"),
                cleanup_verified=False,
            )
            value = json.loads(paths["results.json"].read_text())
            self.assertEqual(value["outcome"], "INFRA ERROR")
            self.assertFalse(value["cleanup_verified"])
            self.assertIn("cleanup-unverified", value["infra_errors"])


class GhTransportTests(unittest.TestCase):
    def test_hosted_suite_client_cannot_write_any_release(self) -> None:
        with self.assertRaisesRegex(release.ReleaseError, "github_read_only"):
            release.GhClient().request("repos/Hikyo-Org/Hikyo/releases/1", {"body": "should never be sent"})

    def test_timeout_or_auth_failure_never_exposes_stderr(self) -> None:
        for effect in (subprocess.TimeoutExpired("gh", 1), OSError("secret-token")):
            with (
                patch.object(release.subprocess, "run", side_effect=effect),
                self.assertRaisesRegex(
                    release.ReleaseError, "github_request_unavailable"
                ),
            ):
                release.GhClient().request("repos/Hikyo-Org/Hikyo/releases/72")
        with (
            patch.object(
                release.subprocess,
                "run",
                return_value=subprocess.CompletedProcess([], 1),
            ),
            self.assertRaisesRegex(release.ReleaseError, "github_request_refused"),
        ):
            release.GhClient().request("repos/Hikyo-Org/Hikyo/releases/72")

    def test_other_repository_and_invalid_timeout_rejected(self) -> None:
        with self.assertRaises(release.ReleaseError):
            release.GhClient().request("repos/Other/Project/releases/72")
        for timeout in (0, 61):
            with self.assertRaises(release.ReleaseError):
                release.GhClient(timeout=timeout)


if __name__ == "__main__":
    unittest.main()
