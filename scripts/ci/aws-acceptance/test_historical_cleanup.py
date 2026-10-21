"""Offline exact-resource, durable-intent and no-retransmission regressions."""

import json
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest import mock

import historical_cleanup as cleanup


def response(value):
    return subprocess.CompletedProcess([], 0, json.dumps(value).encode(), b"")


def not_found(prefix=b"", retry=b"", operation=b"DescribeSecret"):
    return subprocess.CompletedProcess([], 254, b"", prefix + b"An error occurred (ResourceNotFoundException)"
                                       + b" when calling the " + operation
                                       + b" operation" + retry + b": Secrets Manager cannot find the specified secret.\n")


def metadata(**changes):
    return {"ARN": cleanup.ARN, "Name": cleanup.NAME, "KmsKeyId": cleanup.KEY_ARN,
            "DeletedDate": "2026-10-26T09:40:57+00:00", **changes}


class HistoricalCleanupTests(unittest.TestCase):
    def setUp(self):
        self.directory = tempfile.TemporaryDirectory()
        self.addCleanup(self.directory.cleanup)
        self.intent = Path(self.directory.name) / "historical-cleanup-intent.json"
        self.clock = 1000.0
        self.states = []
        self.calls = []
        self.responses = []
        self.cancelled = False
        self.env = {"PATH": "/usr/bin", "AWS_ACCESS_KEY_ID": "synthetic-access",
                    "AWS_SECRET_ACCESS_KEY": "synthetic-private-canary", "AWS_MAX_ATTEMPTS": "99"}

    def wait(self, seconds):
        self.clock += seconds
        return self.cancelled

    def command(self, arguments, **options):
        operation = arguments[arguments.index("secretsmanager") + 1]
        self.calls.append(operation)
        self.assertEqual(arguments[arguments.index("--secret-id") + 1], cleanup.ARN)
        self.assertEqual(arguments[arguments.index("--region") + 1], "eu-central-1")
        self.assertEqual(options["env"]["AWS_MAX_ATTEMPTS"], "1")
        self.assertEqual(options["stderr"], subprocess.PIPE)
        self.assertFalse(options["check"])
        self.assertLessEqual(options["timeout"], cleanup.CLI_TIMEOUT)
        self.assertNotIn("synthetic-private-canary", " ".join(arguments))
        if operation == "delete-secret":
            self.assertIn("--force-delete-without-recovery", arguments)
            self.assertEqual(self.intent.stat().st_mode & 0o777, 0o600)
            intent = json.loads(self.intent.read_text())
            self.assertEqual(intent["arn"], cleanup.ARN)
            self.assertEqual(intent["run_id"], "1234")
            self.assertEqual(intent["maximum_delete_attempts"], 1)
            self.assertEqual(self.states[-1]["status"], "delete-intent-recorded")
            self.assertEqual(self.states[-1]["delete_attempts"], 1)
        result = self.responses.pop(0)
        if isinstance(result, Exception):
            raise result
        return result

    def helper(self, **options):
        return cleanup.Cleanup(self.env, 1100, self.intent, "1234", self.states.append,
                               lambda: self.cancelled, now=lambda: self.clock,
                               wait=self.wait, command=self.command, **options)

    def assert_held(self):
        self.assertFalse(self.states[-1]["verified_absent"])
        self.assertEqual(self.states[-1]["status"], "held-reconcile-before-new-suite")
        self.assertNotIn("synthetic-private-canary", json.dumps(self.states))

    def test_empty_request_skips_without_aws_or_intent(self):
        self.helper().execute("")
        self.assertEqual(self.calls, [])
        self.assertFalse(self.intent.exists())
        self.assertEqual(self.states[-1]["status"], "not-requested")

    def test_already_absent_accepts_only_known_exact_cli_formats_with_zero_deletes(self):
        for prefix in (b"", b"aws: [ERROR]: "):
            for retry in (b"", b" (reached max retries: 0)"):
                with self.subTest(prefix=prefix, retry=retry):
                    self.responses = [not_found(prefix, retry)]
                    self.helper().execute(cleanup.ARN)
                    self.assertEqual(self.states[-1]["status"], "already-absent")
                    self.assertTrue(self.states[-1]["verified_absent"])
                    self.assertEqual(self.states[-1]["delete_attempts"], 0)
                    self.assertFalse(self.intent.exists())
        self.assertEqual(self.calls, ["describe-secret"] * 4)

    def test_known_cli_error_blank_line_framing_is_accepted_without_delete(self):
        for leading, trailing in ((b"\n", b"\n"), (b"\r\n", b"\r\n"),
                                  (b"", b"\n"), (b"", b"\r\n")):
            for prefix in (b"", b"aws: [ERROR]: "):
                for retry in (b"", b" (reached max retries: 0)"):
                    with self.subTest(leading=leading, trailing=trailing, prefix=prefix, retry=retry):
                        original = not_found(prefix, retry)
                        self.responses = [subprocess.CompletedProcess([], 254, b"", leading + original.stderr[:-1] + trailing)]
                        self.helper().execute(cleanup.ARN)
                        self.assertEqual(self.states[-1]["status"], "already-absent")
                        self.assertTrue(self.states[-1]["verified_absent"])
                        self.assertEqual(self.states[-1]["delete_attempts"], 0)
                        self.assertFalse(self.intent.exists())
        self.assertNotIn("delete-secret", self.calls)

    def test_official_cli_2381_formatted_diagnostic_is_accepted_without_delete(self):
        # Captured offline from AWS CLI 2.38.1 ClientErrorHandler, with a
        # synthetic DescribeSecret ClientError, no AWS client/session/network.
        # Source: aws/aws-cli 2.38.1, awscli/errorformat.py:write_error.
        diagnostic = (b"\naws: [ERROR]: An error occurred (ResourceNotFoundException)"
                      b" when calling the DescribeSecret operation:"
                      b" Secrets Manager cannot find the specified secret.\n")
        for stderr in (diagnostic, diagnostic.replace(b"\n", b"\r\n")):
            with self.subTest(stderr=stderr):
                self.responses = [subprocess.CompletedProcess([], 254, b"", stderr)]
                self.helper().execute(cleanup.ARN)
                self.assertEqual(self.states[-1]["status"], "already-absent")
                self.assertTrue(self.states[-1]["verified_absent"])
                self.assertEqual(self.states[-1]["delete_attempts"], 0)
                self.assertFalse(self.intent.exists())
        self.assertNotIn("delete-secret", self.calls)

    def test_exact_scheduled_metadata_allows_one_durable_delete_then_fresh_absence(self):
        self.responses = [response(metadata()), response({"ARN": cleanup.ARN}),
                          response(metadata(KmsKeyId=cleanup.KEY_ID)), not_found()]
        with mock.patch.object(cleanup.os, "fsync", wraps=cleanup.os.fsync) as fsync:
            helper = self.helper()
            helper.execute(cleanup.ARN)
            self.assertGreaterEqual(fsync.call_count, 2)
        self.assertEqual(self.calls, ["describe-secret", "delete-secret", "describe-secret", "describe-secret"])
        self.assertEqual(self.states[-1]["status"], "verified-absent")
        self.assertEqual(self.states[-1]["delete_attempts"], 1)
        self.assertTrue(self.states[-1]["delete_response_exact_arn"])
        previous = list(self.states)
        with self.assertRaises(cleanup.Refused):
            helper.execute(cleanup.ARN)
        self.assertEqual(self.states, previous)
        self.assertEqual(self.calls.count("delete-secret"), 1)

    def test_arbitrary_arn_refuses_before_describe(self):
        for arn in (cleanup.ARN + "other", cleanup.NAME, cleanup.ARN.replace("099256073061", "111122223333")):
            with self.subTest(arn=arn), self.assertRaises(cleanup.Refused):
                self.helper().execute(arn)
            self.assert_held()
        self.assertEqual(self.calls, [])

    def test_foreign_or_unscheduled_metadata_never_reaches_delete(self):
        for changes in ({"ARN": cleanup.ARN + "other"}, {"Name": "foreign"},
                        {"KmsKeyId": "alias/foreign"}, {"KmsKeyId": cleanup.KEY_ID + "other"},
                        {"DeletedDate": None}, {"DeletedDate": False}, {"DeletedDate": float("nan")},
                        {"DeletedDate": "2026-10-26T09:40:57"}, {"DeletedDate": "invalid"}):
            with self.subTest(changes=changes):
                self.responses = [response(metadata(**changes))]
                with self.assertRaises(cleanup.Refused):
                    self.helper().execute(cleanup.ARN)
                self.assert_held()
                self.assertFalse(self.intent.exists())
        self.assertNotIn("delete-secret", self.calls)

    def test_malformed_duplicate_and_value_payloads_never_reach_delete(self):
        for raw in (b"not json", b"[]", b'{"ARN":"foreign","ARN":"' + cleanup.ARN.encode() + b'"}',
                    json.dumps(metadata(SecretString="synthetic-private-canary")).encode()):
            self.responses = [subprocess.CompletedProcess([], 0, raw, b"")]
            with self.assertRaises(cleanup.Refused):
                self.helper().execute(cleanup.ARN)
            self.assert_held()
        self.assertNotIn("delete-secret", self.calls)

    def test_error_classifier_refuses_other_codes_operations_exit_status_and_extra_text(self):
        original = not_found()
        cases = [subprocess.CompletedProcess([], 254, b"", b"\n\n" + original.stderr),
                 subprocess.CompletedProcess([], 254, b"", original.stderr + b"\n"),
                 subprocess.CompletedProcess([], 254, b"", b"\r" + original.stderr),
                 not_found(operation=b"DeleteSecret"), not_found(retry=b" (reached max retries: 1)"),
                 subprocess.CompletedProcess([], 254, b"", original.stderr.replace(b"ResourceNotFoundException)", b"ResourceNotFoundException) (reached max retries: 0)")),
                 subprocess.CompletedProcess([], 255, b"", original.stderr),
                 subprocess.CompletedProcess([], 254, b"unexpected", original.stderr),
                 subprocess.CompletedProcess([], 254, b"", original.stderr + b"extra\n"),
                 subprocess.CompletedProcess([], 254, b"", b"\n" + original.stderr + b"\nsynthetic-private-canary\n"),
                 subprocess.CompletedProcess([], 254, b"", b"synthetic-private-canary\n" + original.stderr),
                 subprocess.CompletedProcess([], 254, b"", b" \n" + original.stderr),
                 subprocess.CompletedProcess([], 254, b"", original.stderr + b"\t\n"),
                 subprocess.CompletedProcess([], 254, b"", original.stderr.replace(b"ResourceNotFoundException", b"AccessDeniedException")),
                 subprocess.CompletedProcess([], 254, b"", b'{"Code":"ResourceNotFoundException"}')]
        for result in cases:
            self.responses = [result]
            with self.assertRaises(cleanup.Refused):
                self.helper().execute(cleanup.ARN)
            self.assert_held()
        self.assertNotIn("delete-secret", self.calls)

    def test_ambiguous_delete_never_retransmits_or_polls_and_preserves_claim(self):
        for result in (subprocess.TimeoutExpired("aws", 10),
                       subprocess.CompletedProcess([], 254, b"", b"synthetic-private-canary"),
                       not_found(operation=b"DeleteSecret"),
                       response({"ARN": cleanup.ARN + "foreign"}),
                       subprocess.CompletedProcess([], 0, b"malformed", b"")):
            with self.subTest(result=type(result).__name__), tempfile.TemporaryDirectory() as directory:
                self.intent = Path(directory) / "historical-cleanup-intent.json"
                self.responses = [response(metadata()), result]
                self.calls = []
                with self.assertRaises(cleanup.Refused):
                    self.helper().execute(cleanup.ARN)
                self.assertEqual(self.calls, ["describe-secret", "delete-secret"])
                self.assertTrue(self.intent.exists())
                self.assertEqual(self.states[-1]["delete_attempts"], 1)
                self.assert_held()

    def test_verification_is_bounded_and_never_repeats_delete(self):
        self.responses = [response(metadata()), response({"ARN": cleanup.ARN})]
        self.responses.extend(response(metadata()) for _ in range(cleanup.ABSENCE_READS))
        with self.assertRaises(cleanup.Refused):
            self.helper().execute(cleanup.ARN)
        self.assertEqual(self.calls.count("delete-secret"), 1)
        self.assertEqual(self.calls.count("describe-secret"), cleanup.ABSENCE_READS + 1)
        self.assert_held()

    def test_deadline_cancellation_and_prior_intent_refuse_without_network(self):
        for condition in ("deadline", "cancel", "prior-intent"):
            with self.subTest(condition=condition):
                self.clock = 1100 if condition == "deadline" else 1000
                self.cancelled = condition == "cancel"
                if condition == "prior-intent":
                    self.intent.write_text("immutable prior claim")
                with self.assertRaises(cleanup.Refused):
                    self.helper().execute(cleanup.ARN)
                self.assertEqual(self.calls, [])
                self.assert_held()
        self.assertEqual(self.intent.read_text(), "immutable prior claim")

    def test_intent_or_evidence_failure_blocks_delete(self):
        self.responses = [response(metadata())]
        with mock.patch.object(cleanup.os, "fsync", side_effect=OSError("synthetic-private-canary")), self.assertRaises(cleanup.Refused):
            self.helper().execute(cleanup.ARN)
        self.assertEqual(self.calls, ["describe-secret"])
        self.assert_held()
        with tempfile.TemporaryDirectory() as directory:
            self.intent = Path(directory) / "historical-cleanup-intent.json"
            self.responses = [response(metadata())]
            self.calls = []
            helper = self.helper()
            def persist(state):
                self.states.append(state)
                if state["status"] == "delete-intent-recorded":
                    raise OSError("synthetic-private-canary")
            helper.persist = persist
            with self.assertRaises(cleanup.Refused):
                helper.execute(cleanup.ARN)
            self.assertEqual(self.calls, ["describe-secret"])
            self.assertTrue(self.intent.exists())
            self.assert_held()

    def test_deadline_after_delete_response_holds_without_verification_or_retry(self):
        self.responses = [response(metadata()), response({"ARN": cleanup.ARN})]
        helper = self.helper()
        def command(arguments, **options):
            result = self.command(arguments, **options)
            if "delete-secret" in arguments:
                self.clock = 1100
            return result
        helper.command = command
        with self.assertRaises(cleanup.Refused):
            helper.execute(cleanup.ARN)
        self.assertEqual(self.calls, ["describe-secret", "delete-secret"])
        self.assertEqual(self.states[-1]["delete_attempts"], 1)
        self.assert_held()


if __name__ == "__main__":
    unittest.main()
