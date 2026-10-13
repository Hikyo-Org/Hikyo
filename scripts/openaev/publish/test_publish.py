import copy
import io
import subprocess
from pathlib import Path
import unittest
from unittest.mock import MagicMock, patch
import publish_pass
import release_context


class PublicationTests(unittest.TestCase):
    def setUp(self):
        self.identity = {"repository": "Hikyo-Org/Hikyo", "release_id": 7,
                         "tag": "v1.2.3", "source_sha": "a" * 40,
                         "published_at": "2026-10-13T00:00:00Z"}
        self.result = dict(self.identity)
        del self.result["repository"]
        self.result.update(schema_version=1, status="PASS", image_ref="ghcr.io/hikyo-org/hikyo@sha256:" + "b" * 64,
                           suite_sha256=publish_pass.suite_fingerprint(), expected_assertions=313, passed_assertions=313,
                           failed_assertions=0, skipped_assertions=0, cleanup_verified=True,
                           isolation_verified=True, image_native_binary_verified=True,
                           native_verification="stable/v1")
        self.release = {"immutable": True, "draft": False, "tag_name": self.identity["tag"],
                        "published_at": self.identity["published_at"], "body": "Original notes\n"}
        self.run_url = "https://github.com/Hikyo-Org/Hikyo/actions/runs/42"

    def test_prerelease_containing_nightly_uses_stable_verification(self):
        tag = "v1.0.0-foo-nightly.1"
        script = Path(__file__).resolve().parents[2] / "release" / "release-channel.sh"
        channel = subprocess.run([str(script), tag], check=True, capture_output=True, text=True)
        self.assertEqual(channel.stdout.strip(), "prerelease")
        identity = dict(self.identity, tag=tag)
        result = dict(self.result, tag=tag)
        publish_pass.validate(result, identity)
        with self.assertRaises(ValueError):
            publish_pass.validate(dict(result, native_verification="nightly/v1"), identity)

    def test_every_partial_or_failed_outcome_has_zero_api_calls(self):
        corruptions = {"status": "FAIL", "failed_assertions": 1, "skipped_assertions": 1,
                       "passed_assertions": 312, "expected_assertions": 312,
                       "cleanup_verified": False, "isolation_verified": False,
                       "image_native_binary_verified": False, "schema_version": True,
                       "native_verification": "nightly/v1", "source_sha": "d" * 40,
                       "image_ref": "ghcr.io/hikyo-org/hikyo:nightly", "suite_sha256": ""}
        for key, value in corruptions.items():
            with self.subTest(key=key), patch.object(publish_pass, "api") as api:
                result = dict(self.result, **{key: value})
                with self.assertRaises(ValueError):
                    publish_pass.publish(result, self.identity, self.run_url)
                api.assert_not_called()

    def test_raw_or_unexpected_evidence_is_rejected(self):
        for key in self.result:
            result = dict(self.result)
            del result[key]
            with self.subTest(missing=key), self.assertRaises(ValueError):
                publish_pass.validate(result, self.identity)
        with self.assertRaises(ValueError):
            publish_pass.validate(dict(self.result, raw_evidence="sensitive"), self.identity)

    def test_pass_creates_only_a_success_check_and_is_idempotent(self):
        posted = []
        def api(path, payload=None):
            if payload is not None:
                posted.append((path, payload))
                return {}
            return copy.deepcopy(self.release)
        with patch.object(publish_pass, "api", side_effect=api), patch.object(publish_pass, "source_sha", return_value="a" * 40), patch.object(publish_pass, "existing_pass", side_effect=[False, True]):
            self.assertTrue(publish_pass.publish(self.result, self.identity, self.run_url))
            self.assertFalse(publish_pass.publish(self.result, self.identity, self.run_url))
        self.assertEqual(len(posted), 1)
        path, payload = posted[0]
        self.assertEqual(path, "repos/Hikyo-Org/Hikyo/check-runs")
        self.assertEqual(payload["status"], "completed")
        self.assertEqual(payload["conclusion"], "success")
        self.assertEqual(payload["head_sha"], self.identity["source_sha"])
        self.assertEqual(payload["external_id"], release_context.check_identity(7, "a" * 40))
        self.assertEqual(payload["details_url"], self.run_url)
        self.assertIn("all 313 assertions", payload["output"]["summary"])
        self.assertEqual(self.release["body"], "Original notes\n")

    def test_concurrent_human_note_edit_is_never_replaced(self):
        requests = []
        def api(path, payload=None):
            requests.append((path, payload))
            if payload is None:
                snapshot = copy.deepcopy(self.release)
                self.release["body"] = "New human notes during publication\n"
                return snapshot
            return {}
        with patch.object(publish_pass, "api", side_effect=api), patch.object(publish_pass, "source_sha", return_value="a" * 40), patch.object(publish_pass, "existing_pass", return_value=False):
            self.assertTrue(publish_pass.publish(self.result, self.identity, self.run_url))
        self.assertEqual(self.release["body"], "New human notes during publication\n")
        writes = [(path, body) for path, body in requests if body is not None]
        self.assertEqual(len(writes), 1)
        self.assertEqual(writes[0][0], "repos/Hikyo-Org/Hikyo/check-runs")
        self.assertNotIn("body", writes[0][1])

    def test_changed_release_and_tag_are_not_mutated(self):
        for field, value in {"immutable": False, "draft": True, "published_at": "later", "tag_name": "v2.0.0"}.items():
            with self.subTest(field=field), patch.object(publish_pass, "api", return_value=dict(self.release, **{field: value})) as api, patch.object(publish_pass, "source_sha", return_value="a" * 40):
                with self.assertRaises(ValueError):
                    publish_pass.publish(self.result, self.identity, self.run_url)
                self.assertEqual(api.call_count, 1)
        with patch.object(publish_pass, "api", return_value=self.release) as api, patch.object(publish_pass, "source_sha", return_value="d" * 40):
            with self.assertRaises(ValueError):
                publish_pass.publish(self.result, self.identity, self.run_url)
            self.assertEqual(api.call_count, 1)

    def test_nightly_published_event_waits_for_container_workflow(self):
        event = {"release": {"tag_name": "v0.0.1-nightly.20261012.75.g868703c7"}}
        with patch.object(release_context, "api") as api:
            self.assertIsNone(release_context.resolve("release", event, "Hikyo-Org/Hikyo"))
            api.assert_not_called()

    def test_untrusted_or_failed_upstream_is_ignored(self):
        run = {"conclusion": "success", "head_branch": "main", "path": ".github/workflows/nightly.yml",
               "head_repository": {"full_name": "Hikyo-Org/Hikyo"}}
        for field, value in {"conclusion": "failure", "head_branch": "feature", "path": ".github/workflows/evil.yml",
                             "head_repository": {"full_name": "attacker/repo"}}.items():
            with self.subTest(field=field), patch.object(release_context.subprocess, "run") as command:
                self.assertIsNone(release_context.resolve("workflow_run", {"workflow_run": dict(run, **{field: value})}, "Hikyo-Org/Hikyo"))
                command.assert_not_called()

    def test_automatic_duplicate_checks_owned_pass(self):
        release = dict(self.release, id=7)
        with patch.object(release_context, "api", return_value=release), patch.object(release_context, "source_sha", return_value="a" * 40), patch.object(release_context, "existing_pass", return_value=True) as check:
            self.assertIsNone(release_context.resolve("release", {"release": release}, "Hikyo-Org/Hikyo"))
            check.assert_called_once_with("Hikyo-Org/Hikyo", 7, "a" * 40)

    def test_manual_replay_does_not_skip_a_previous_success(self):
        release = dict(self.release, id=7)
        with patch.object(release_context, "api", return_value=release), patch.object(release_context, "source_sha", return_value="a" * 40), patch.object(release_context, "existing_pass", return_value=True) as check:
            context = release_context.resolve("workflow_dispatch", {}, "Hikyo-Org/Hikyo", release["tag_name"])
            self.assertEqual(context["release_id"], 7)
            check.assert_not_called()

    def test_nightly_selects_exact_completed_run_not_latest(self):
        run = {"conclusion": "success", "head_branch": "main", "path": ".github/workflows/nightly.yml",
               "head_repository": {"full_name": "Hikyo-Org/Hikyo"}, "run_number": 75, "head_sha": "a" * 40}
        wanted = dict(self.release, id=7, prerelease=True, tag_name="v0.0.1-nightly.20261012.75.gaaaaaaaa")
        newer = dict(wanted, id=8, tag_name="v0.0.1-nightly.20261013.76.gbbbbbbbb")
        with patch.object(release_context, "release_page", return_value=[newer, wanted]), patch.object(release_context, "api", return_value=wanted), patch.object(release_context, "source_sha", return_value="a" * 40), patch.object(release_context, "existing_pass", return_value=False):
            result = release_context.resolve("workflow_run", {"workflow_run": run}, "Hikyo-Org/Hikyo")
            self.assertEqual(result["tag"], wanted["tag_name"])
        with patch.object(release_context, "release_page", return_value=[]), patch.object(release_context, "api") as api:
            self.assertIsNone(release_context.resolve("workflow_run", {"workflow_run": run}, "Hikyo-Org/Hikyo"))
            api.assert_not_called()

    def test_reused_prior_nightly_tag_is_a_noop(self):
        run = {"conclusion": "success", "head_branch": "main", "path": ".github/workflows/nightly.yml",
               "head_repository": {"full_name": "Hikyo-Org/Hikyo"}, "run_number": 76, "head_sha": "a" * 40}
        prior = dict(self.release, id=7, prerelease=True, tag_name="v0.0.1-nightly.20261012.75.gaaaaaaaa")
        with patch.object(release_context, "release_page", return_value=[prior]), patch.object(release_context, "api") as api:
            self.assertIsNone(release_context.resolve("workflow_run", {"workflow_run": run}, "Hikyo-Org/Hikyo"))
            api.assert_not_called()

    def test_delayed_candidate_does_not_accumulate_pages_or_choose_latest(self):
        unrelated = dict(self.release, id=8, prerelease=True, tag_name="v0.0.1-nightly.20261013.76.gbbbbbbbb")
        wanted = dict(unrelated, id=7, tag_name="v0.0.1-nightly.20261012.75.gaaaaaaaa")
        pages = [[unrelated] * release_context.RELEASE_PAGE_SIZE, [wanted]]
        with patch.object(release_context, "release_page", side_effect=pages) as api:
            self.assertEqual(release_context.nightly_release("Hikyo-Org/Hikyo", 75), wanted)
            self.assertEqual(api.call_args_list[1].args[1], 2)

    def test_page_exhaustion_does_not_silently_skip_or_publish_candidate(self):
        wanted = dict(self.release, id=7, prerelease=True, tag_name="v0.0.1-nightly.20261012.75.gaaaaaaaa")
        unrelated = dict(wanted, id=8, tag_name="v0.0.1-nightly.20261013.76.gbbbbbbbb")
        for first in (wanted, unrelated):
            with self.subTest(found=first == wanted), patch.object(release_context, "MAX_RELEASE_PAGES", 2), patch.object(release_context, "release_page", side_effect=[[first] + [unrelated] * (release_context.RELEASE_PAGE_SIZE - 1), [unrelated] * release_context.RELEASE_PAGE_SIZE]) as api:
                with self.assertRaisesRegex(ValueError, "bounded lookup window"):
                    release_context.nightly_release("Hikyo-Org/Hikyo", 75)
                self.assertLessEqual(api.call_count, 2)

    def test_release_page_reads_bounded_bytes_and_kills_overflow_producer(self):
        process = MagicMock()
        process.stdout = MagicMock(wraps=io.BytesIO(b"x" * (release_context.MAX_RELEASE_PAGE_BYTES + 2)))
        process.poll.return_value = None
        with patch.object(release_context.subprocess, "Popen", return_value=process), patch.object(release_context.threading, "Timer"):
            with self.assertRaisesRegex(ValueError, "bounded response limit"):
                release_context.release_page("Hikyo-Org/Hikyo", 1)
        process.stdout.read.assert_called_once_with(release_context.MAX_RELEASE_PAGE_BYTES + 1)
        process.terminate.assert_called_once()

    def test_only_owned_exact_completed_success_is_idempotent(self):
        check = {"name": release_context.CHECK_NAME, "external_id": release_context.check_identity(7, "a" * 40),
                 "head_sha": "a" * 40, "status": "completed", "conclusion": "success",
                 "app": {"id": 15368, "slug": "github-actions"}}
        for key, value in {"name": "another check", "external_id": "different release", "head_sha": "b" * 40,
                           "status": "in_progress", "conclusion": "failure", "app": {"id": 42, "slug": "github-actions"}}.items():
            with self.subTest(key=key), patch.object(release_context, "api", return_value={"check_runs": [dict(check, **{key: value})]}):
                self.assertFalse(release_context.existing_pass("Hikyo-Org/Hikyo", 7, "a" * 40))
        with patch.object(release_context, "api", return_value={"check_runs": [check]}):
            self.assertTrue(release_context.existing_pass("Hikyo-Org/Hikyo", 7, "a" * 40))

    def test_check_lookup_is_bounded_and_rejects_exhaustion(self):
        other = {"name": "other", "app": {"id": 42}}
        owned = {"name": release_context.CHECK_NAME, "external_id": release_context.check_identity(7, "a" * 40),
                 "head_sha": "a" * 40, "status": "completed", "conclusion": "success",
                 "app": {"id": 15368, "slug": "github-actions"}}
        with patch.object(release_context, "api", side_effect=[{"check_runs": [other] * release_context.CHECK_PAGE_SIZE}, {"check_runs": [owned]}]) as api:
            self.assertTrue(release_context.existing_pass("Hikyo-Org/Hikyo", 7, "a" * 40))
            self.assertIn("page=2", api.call_args.args[0])
            self.assertIn("filter=all", api.call_args.args[0])
        with patch.object(release_context, "api", return_value={"check_runs": [other] * release_context.CHECK_PAGE_SIZE}) as api:
            with self.assertRaisesRegex(ValueError, "check history exceeds bounded"):
                release_context.existing_pass("Hikyo-Org/Hikyo", 7, "a" * 40)
            self.assertEqual(api.call_count, release_context.MAX_CHECK_PAGES)

    def test_api_does_not_create_pending_or_failed_checks(self):
        for status, conclusion in (("in_progress", None), ("completed", "failure")):
            with self.subTest(status=status), patch.object(release_context.subprocess, "Popen") as process:
                with self.assertRaisesRegex(ValueError, "completed success"):
                    release_context.api("repos/Hikyo-Org/Hikyo/check-runs", payload={"status": status, "conclusion": conclusion})
                process.assert_not_called()

    def test_api_posts_structured_success_proof_only(self):
        process = MagicMock()
        process.stdout = io.BytesIO(b'{"id":42}')
        process.wait.return_value = 0
        process.poll.return_value = 0
        payload = {"status": "completed", "conclusion": "success", "output": {"summary": "313/313"}}
        with patch.object(release_context.subprocess, "Popen", return_value=process) as command, patch.object(release_context.threading, "Timer"):
            self.assertEqual(release_context.api("repos/Hikyo-Org/Hikyo/check-runs", payload=payload), {"id": 42})
        self.assertEqual(command.call_args.args[0][-4:], ["--method", "POST", "--input", "-"])
        self.assertIn(b'"conclusion": "success"', process.stdin.write.call_args.args[0])

    def test_api_never_accepts_release_mutations(self):
        with patch.object(release_context.subprocess, "Popen") as process:
            with self.assertRaisesRegex(ValueError, "only create check runs"):
                release_context.api("repos/Hikyo-Org/Hikyo/releases/7", payload={"body": "replacement"})
            process.assert_not_called()

    def test_annotated_tags_are_peeled_without_executing_source(self):
        with patch.object(release_context, "api", side_effect=[{"object": {"type": "tag", "sha": "b" * 40}},
                                                              {"object": {"type": "commit", "sha": "a" * 40}}]):
            self.assertEqual(release_context.source_sha("Hikyo-Org/Hikyo", "v1.2.3"), "a" * 40)


if __name__ == "__main__":
    unittest.main()
