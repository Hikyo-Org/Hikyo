"""Terminal Go assertion projection, with real offline test2json regressions."""
import json
from pathlib import Path
import tempfile
import time
import unittest
from unittest.mock import patch

import runner
from test_runner import approved_environment


class FailureAssertionTests(unittest.TestCase):
    def setUp(self):
        directory = tempfile.TemporaryDirectory()
        self.addCleanup(directory.cleanup)
        approval = runner.Approval(approved_environment(time.time()), time.time())
        self.evidence = runner.Evidence(Path(directory.name) / "evidence.json", approval)
        self.test = runner.SUITE + "/lifecycle/sqlite"

    def emit(self, output="", test=None, action="output"):
        self.evidence.event({"Action": action, "Test": self.test if test is None else test, "Output": output})

    def line(self, message="durable AWS target did not converge"):
        return "    aws_real_durable_test.go:371: " + message + "\n"

    def end_failure(self, test=None):
        test = self.test if test is None else test
        self.emit("--- FAIL: " + test + " (0.00s)\n", test=test)
        self.emit(test=test, action="fail")

    def test_every_closed_literal_projects_identifier_only(self):
        for message, identifier in runner.FAILURE_ASSERTIONS.items():
            with self.subTest(identifier=identifier):
                self.evidence.data["failure_assertions"] = []
                self.emit(self.line(message))
                self.end_failure()
                self.assertEqual(self.evidence.data["failure_assertions"], [{"test": self.test, "assertion": identifier}])
                self.assertNotIn(message, self.evidence.path.read_text())

    def test_convergence_classes_extend_the_closed_vocabulary(self):
        self.assertEqual(len(runner.FAILURE_ASSERTIONS), 230)
        self.assertEqual(len(set(runner.FAILURE_ASSERTIONS.values())), 230)
        self.assertEqual(runner.FAILURE_ASSERTIONS["durable AWS target did not converge"], "assertion-066")
        self.assertEqual(set(runner.CONVERGENCE_ASSERTIONS.values()),
                         {f"assertion-{index:03d}" for index in range(225, 231)})
        for suffix in ("", "unknown", "auth synthetic-private-credential", "network\ncredential"):
            self.emit(self.line("durable AWS target did not converge: " + suffix))
            self.end_failure()
        self.assertEqual(self.evidence.data["failure_assertions"], [])

    def test_real_split_multiline_message_is_rejected(self):
        self.emit(self.line())
        self.emit("        synthetic-private-credential-never-publish\n")
        self.end_failure()
        self.assertEqual(self.evidence.data["failure_assertions"], [])
        self.assertNotIn("synthetic-private-credential-never-publish", self.evidence.path.read_text())

    def test_actual_go_test2json_witness_keeps_four_static_failures_only(self):
        events = Path(__file__).with_name("runtime-test2json.jsonl").read_text().splitlines()
        for line in events:
            self.evidence.event(json.loads(line))
        rows = self.evidence.data["failure_assertions"]
        self.assertEqual(len(rows), 4)
        self.assertEqual({row["assertion"] for row in rows}, {"assertion-066", "assertion-194"})
        self.assertNotIn(runner.SUITE + "/lifecycle/postgres", {row["test"] for row in rows})
        self.assertNotIn("synthetic-private-credential-never-publish", self.evidence.path.read_text())

    def test_unterminated_or_interrupted_candidate_is_not_published(self):
        self.emit(self.line())
        self.assertEqual(self.evidence.data["failure_assertions"], [])
        self.assertNotIn("assertion-066", self.evidence.path.read_text())
        self.assertFalse(self.evidence.finish(1, True))
        self.assertEqual(self.evidence.data["failure_assertions"], [])
        self.end_failure()
        self.assertEqual(self.evidence.data["failure_assertions"], [])

    def test_fail_action_without_exact_footer_is_not_published(self):
        self.emit(self.line())
        self.emit(action="fail")
        self.assertEqual(self.evidence.data["failure_assertions"], [])

    def test_wrong_footer_new_log_or_continuation_invalidates_candidate(self):
        for text in ("--- FAIL: TestForeign (0.00s)\n", "    other_test.go:1: other\n", "        " + self.line(), "\n", "unframed"):
            with self.subTest(text=text):
                self.emit(self.line())
                self.emit(text)
                self.end_failure()
                self.assertEqual(self.evidence.data["failure_assertions"], [])

    def test_interleaved_known_test_does_not_confuse_message_owner(self):
        other = runner.SUITE + "/lifecycle/postgres"
        self.emit(self.line())
        self.emit("=== RUN   " + other + "\n", test=other)
        self.end_failure()
        self.assertEqual(self.evidence.data["failure_assertions"], [{"test": self.test, "assertion": "assertion-066"}])

    def test_unknown_test_or_action_drops_pending_candidate(self):
        for action, test in (("output", "TestForeign"), ("pause", self.test)):
            with self.subTest(action=action, test=test):
                self.emit(self.line())
                self.emit(test=test, action=action)
                self.end_failure()
                self.assertEqual(self.evidence.data["failure_assertions"], [])

    def test_foreign_source_test_and_action_are_rejected(self):
        self.emit(self.line().replace("aws_real_durable_test.go", "aws_real_untrusted_test.go"))
        self.emit(self.line(), test="TestForeign")
        self.emit(self.line(), action="pass")
        self.end_failure()
        self.assertEqual(self.evidence.data["failure_assertions"], [])

    def test_dynamic_sensitive_suffix_and_multiline_payload_are_rejected(self):
        secret = "synthetic-private-credential-never-publish"
        for text in (self.line(secret), self.line("durable AWS target did not converge " + secret), self.line() + secret):
            self.emit(text)
            self.end_failure()
        self.assertEqual(self.evidence.data["failure_assertions"], [])
        self.assertNotIn(secret, self.evidence.path.read_text())

    def test_assertion_capture_does_not_turn_failed_suite_into_success(self):
        self.emit(self.line())
        self.end_failure()
        self.assertFalse(self.evidence.finish(1, True))
        self.assertEqual(self.evidence.data["status"], "failed-reconcile-before-rerun")
        self.assertEqual(self.evidence.data["tests"][self.test], "fail")

    def test_save_failure_propagates_without_publishing_pending_identifier(self):
        with patch.object(self.evidence, "save", side_effect=OSError("synthetic write failure")):
            with self.assertRaises(OSError):
                self.emit(self.line())
        self.assertNotIn("assertion-066", self.evidence.path.read_text())

    def test_cap_is_bounded_and_marks_only_additional_confirmed_entries(self):
        for _ in range(runner.MAX_DIAGNOSTICS):
            self.emit(self.line())
            self.end_failure()
        self.assertEqual(len(self.evidence.data["failure_assertions"]), runner.MAX_DIAGNOSTICS)
        self.assertFalse(self.evidence.data["failure_assertions_truncated"])
        self.emit(self.line())
        self.emit("        continuation\n")
        self.end_failure()
        self.assertFalse(self.evidence.data["failure_assertions_truncated"])
        self.emit(self.line())
        self.end_failure()
        self.assertTrue(self.evidence.data["failure_assertions_truncated"])
        self.assertEqual(len(self.evidence.data["failure_assertions"]), runner.MAX_DIAGNOSTICS)


if __name__ == "__main__":
    unittest.main()
