"""Private and attacker-controlled log fields cannot enter public diagnostics."""
from __future__ import annotations

import importlib.util
import json
import unittest
import subprocess
from unittest.mock import patch
from pathlib import Path

spec = importlib.util.spec_from_file_location("stack_diagnostics", Path(__file__).parent / "diagnostics.py")
if spec is None or spec.loader is None:
    raise RuntimeError("diagnostics_module_not_found")
diagnostics = importlib.util.module_from_spec(spec)
spec.loader.exec_module(diagnostics)


class SafeDiagnosticsTests(unittest.TestCase):
    def test_secrets_messages_unknown_classes_and_loggers_never_escape(self) -> None:
        private = "private-admin-token-0123456789"
        records = [
            {"logger_name": "io.openaev.health.DependencyProbeService",
             "message": "Health probe of rabbitmq failed " + private,
             "stack_trace": "java.net.ConnectException: " + private + "\ncom.evil." + private + ": failure"},
            {"logger_name": private, "message": "Application run failed " + private,
             "stack_trace": "java.net.ConnectException: failure"},
            {"logger_name": "org.springframework.boot.SpringApplication",
             "message": "Application run failed " + private,
             "stack_trace": "java.lang.IllegalArgumentException: " + private},
        ]
        result = diagnostics.log_summary("\n".join(json.dumps(record) for record in records).encode())
        self.assertNotIn(private, json.dumps(result))
        self.assertNotIn("com.evil", json.dumps(result))
        self.assertEqual(result["exception_counts"], {
            "java.lang.IllegalArgumentException": 1, "java.net.ConnectException": 1})
        self.assertEqual(result["signal_counts"], {"application_failed": 1, "health_dependency_failed": 1, "rabbitmq_health_failed": 1})

    def test_pinned_logback_structured_throwable_causes_are_reduced(self) -> None:
        record = {"loggerName": "org.springframework.boot.SpringApplication",
                  "message": "Application run failed private-token", "arguments": ["private-token"],
                  "throwable": {"className": "org.springframework.beans.factory.BeanCreationException",
                                "message": "private-token", "stepArray": [{"className": "private-token"}],
                                "cause": {"className": "java.net.UnknownHostException",
                                          "message": "private-token"},
                                "suppressed": [{"className": "evil.private-token"}]}}
        result = diagnostics.log_summary(json.dumps(record).encode())
        self.assertEqual(result["logger_counts"], {"org.springframework.boot.SpringApplication": 1})
        self.assertEqual(result["exception_counts"], {
            "java.net.UnknownHostException": 1,
            "org.springframework.beans.factory.BeanCreationException": 1})
        self.assertNotIn("private-token", json.dumps(result))

    def test_native_json_ignores_stderr_warnings(self) -> None:
        result = subprocess.CompletedProcess([], 0, stdout=b'{"items": []}', stderr=b'warning private-token')
        with patch.object(diagnostics.subprocess, "run", return_value=result):
            self.assertEqual(diagnostics.native_json(Path("kubeconfig"), ["get", "pods"]), {"items": []})
            self.assertIn(b"warning private-token", diagnostics.raw_command(Path("kubeconfig"), ["logs", "pod"]))

    def test_legacy_logger_and_schema_presence_are_closed_codes(self) -> None:
        record = {"logger": "io.openbas.App", "message": "Startup init private-token",
                  "private-token": "secret-value"}
        result = diagnostics.log_summary(json.dumps(record).encode())
        self.assertEqual(result["logger_counts"], {"io.openbas.App": 1})
        self.assertEqual(result["schema_key_counts"], {"logger": 1, "message": 1})
        self.assertEqual(result["signal_counts"], {"platform_initialization_started": 1})
        self.assertNotIn("private-token", json.dumps(result))
        self.assertNotIn("secret-value", json.dumps(result))

    def test_malformed_plaintext_and_unknown_reasons_do_not_escape(self) -> None:
        self.assertEqual(diagnostics.log_summary(b"private-token\nnot-json\n[123]\nnull")["json_records"], 0)
        self.assertEqual(diagnostics.safe_reason("private-token"), "other")
        self.assertEqual(diagnostics.safe_reason("ProgressDeadlineExceeded"), "ProgressDeadlineExceeded")


if __name__ == "__main__":
    unittest.main()
