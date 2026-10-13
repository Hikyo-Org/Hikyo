"""Focused transport tests without cluster or platform mutations."""

from __future__ import annotations

import json
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

import openaev_security as recorder

PATH = "/api/v1/orgs/lab/projects/project/environments/staging/delivery"
TOKEN = "hik_1_wl_disposable-secret"
INJECT_ID = "85db69c9-6ef9-48d7-9ab8-ec3c6f027c69"


class RecorderTests(unittest.TestCase):
    def test_targets_reject_origin_escape_and_redirect_parameters(self):
        rejected = [
            "https://production.example" + PATH,
            "//production.example" + PATH,
            PATH + "#fragment",
            PATH.replace("/staging/", "/../"),
            PATH.replace("/staging/", "/%2e%2e/"),
            PATH.replace("/staging/", "/%252e%252e/"),
            PATH.replace("/staging/", "//"),
            PATH + "?redirect=https%3A%2F%2Fproduction.example",
            PATH + "?cursor=a&cursor=b",
            PATH + "?cursor=%0d%0aAuthorization",
            PATH + "?projection=everything",
            PATH.replace("/delivery", "/grants") + "?cursor=abc",
        ]
        for path in rejected:
            with (
                self.subTest(path=path),
                self.assertRaises(recorder.OpenAEVRecordingError),
            ):
                recorder._target(path)
        for query in ("?cursor=a%2Fb%3D", "?projection=config-only"):
            self.assertEqual(
                recorder._target(PATH + query), recorder.HIKYO_ORIGIN + PATH + query
            )

    def test_payload_matches_real_contracts_and_raw_json(self):
        for method in ("POST", "PUT"):
            payload = recorder._payload(
                "denied-mutation", method, PATH, TOKEN, {"value": "fake"}
            )
            self.assertEqual(
                payload["inject_injector_contract"], recorder.CONTRACTS[method]
            )
            content = payload["inject_content"]
            self.assertIsInstance(content, dict)
            if not isinstance(content, dict):
                self.fail("invalid content")
            self.assertEqual(json.loads(content["body"]), {"value": "fake"})
            self.assertIn(
                {"key": "Content-Type", "value": "application/json"}, content["headers"]
            )
        no_body = recorder._payload("no-body", "POST", PATH, TOKEN, None)
        self.assertEqual(
            no_body["inject_injector_contract"], recorder.FORM_POST_CONTRACT
        )
        content = no_body["inject_content"]
        self.assertIsInstance(content, dict)
        if isinstance(content, dict):
            self.assertNotIn("body", content)
            self.assertEqual(content["parts"], [])
            self.assertNotIn(
                {"key": "Content-Type", "value": "application/json"},
                content["headers"],
            )
        get = recorder._payload("anonymous", "GET", PATH, None, None)["inject_content"]
        self.assertIsInstance(get, dict)
        if isinstance(get, dict):
            self.assertNotIn("body", get)
            self.assertEqual(
                get["headers"], [{"key": "Accept", "value": "application/json"}]
            )

    def test_only_supported_workload_requests(self):
        invalid = [
            ("DELETE", TOKEN, None),
            ("GET", "hik_1_sess_secret", None),
            ("GET", "hik_1_cli_secret", None),
            ("GET", TOKEN, {}),
        ]
        for method, token, body in invalid:
            with (
                self.subTest(method=method, token=token),
                self.assertRaises(recorder.OpenAEVRecordingError),
            ):
                recorder._payload("test", method, PATH, token, body)

    def test_reporting_cannot_retarget_or_replace_workload_authority(self):
        asset_id = "bfa84df0-8450-4b1d-b507-727702d1ab56"
        for field, value in (
            ("uri", "https://production.example/api/v1/orgs/lab"),
            ("headers", [{"key": "Authorization", "value": "human-secret"}]),
        ):
            instance, _ = self._recorder()

            def malicious_config(
                name, payload, target_field=field, changed_value=value
            ):
                payload["inject_assets"] = [asset_id]
                payload["inject_content"][target_field] = changed_value
                return payload

            instance._reporting_config = malicious_config
            with (
                self.subTest(field=field),
                self.assertRaisesRegex(
                    recorder.OpenAEVRecordingError, "reporting_changed_security_request"
                ),
            ):
                instance.record_case(
                    "test", "GET", PATH, TOKEN, None, lambda body: None
                )

    def test_positive_control_expectations_are_normalized_before_launch(self):
        asset_id = "bfa84df0-8450-4b1d-b507-727702d1ab56"
        instance, transport = self._recorder()

        def configure(name, payload):
            payload["inject_assets"] = [asset_id]
            payload["inject_content"]["expectations"] = []
            return payload

        instance._reporting_config = configure
        wanted = configure("test", recorder._payload("test", "GET", PATH, TOKEN, None))[
            "inject_content"
        ]
        if not isinstance(wanted, dict):
            self.fail("configured injection content must be an object")
        configured_content: dict[str, object] = {}
        for key, item in wanted.items():
            if not isinstance(key, str):
                self.fail("configured injection content keys must be strings")
            configured_content[key] = item
        response = {"error": {"code": "not_found"}}
        base = self._responses(response)
        transport.request.side_effect = (
            base[:2]
            + [
                {},
                {"inject_content": wanted},
            ]
            + base[2:]
        )
        instance.record_case("test", "GET", PATH, TOKEN, None, lambda body: None)
        calls = [
            (call.args[0], call.args[1]) for call in transport.request.call_args_list
        ]
        self.assertLess(
            calls.index(("PUT", "atomic-testings/" + INJECT_ID)),
            calls.index(("POST", "atomic-testings/" + INJECT_ID + "/launch")),
        )
        instance, transport = self._recorder()
        instance._reporting_config = configure
        transport.request.side_effect = base[:2] + [
            {},
            {"inject_content": {"expectations": [{"expectation_type": "PREVENTION"}]}},
        ]
        with self.assertRaisesRegex(
            recorder.OpenAEVRecordingError, "reporting_configuration_not_persisted"
        ):
            instance.record_case("test", "GET", PATH, TOKEN, None, lambda body: None)
        self.assertFalse(
            any("/launch" in call.args[1] for call in transport.request.call_args_list)
        )

        for field, value in (
            ("uri", "https://production.example/api/v1/orgs/lab"),
            ("headers", [{"key": "Authorization", "value": "different-authority"}]),
        ):
            instance, transport = self._recorder()
            instance._reporting_config = configure
            persisted = dict(configured_content)
            persisted[field] = value
            transport.request.side_effect = base[:2] + [
                {},
                {"inject_content": persisted},
            ]
            with (
                self.subTest(field=field),
                self.assertRaisesRegex(
                    recorder.OpenAEVRecordingError,
                    "reporting_configuration_not_persisted",
                ),
            ):
                instance.record_case(
                    "test", "GET", PATH, TOKEN, None, lambda body: None
                )
            self.assertFalse(
                any(
                    "/launch" in call.args[1]
                    for call in transport.request.call_args_list
                )
            )

    def _recorder(self):
        with tempfile.TemporaryDirectory() as directory:
            token_file = Path(directory) / "platform.token"
            token_file.write_text("synthetic-platform-token")
            token_file.chmod(0o600)
            with patch.object(recorder._pilot, "PlatformAPI") as transport:
                instance = recorder.OpenAEVRecorder(
                    "https://127.0.0.1:18080", token_file, Path(directory) / "ca.crt"
                )
                return instance, transport.return_value

    @staticmethod
    def _responses(body):
        return [
            {"injector_contract_id": recorder.CONTRACTS["GET"]},
            {"inject_id": INJECT_ID},
            {},
            {
                "status_main_traces": [
                    {
                        "execution_action": "COMPLETE",
                        "execution_status": "EXECUTED",
                        "execution_message": json.dumps(body),
                    }
                ]
            },
        ]

    def test_real_recorded_body_is_asserted_and_metadata_is_sanitized(self):
        instance, transport = self._recorder()
        transport.request.side_effect = self._responses(
            {"error": {"code": "not_found"}}
        )

        def assert_body(body):
            self.assertEqual(body, {"error": {"code": "not_found"}})

        result = instance.record_case(
            "scope-denied", "GET", PATH + "?cursor=opaque", TOKEN, None, assert_body
        )
        self.assertEqual(result["inject_id"], INJECT_ID)
        self.assertTrue(result["execution_recorded"])
        self.assertTrue(result["domain_assertion_passed"])
        self.assertFalse(result["http_status_recorded"])
        rendered = json.dumps(result)
        for secret in (TOKEN, "opaque", "not_found", PATH):
            self.assertNotIn(secret, rendered)
        self.assertNotIn("passed", result)
        self.assertEqual(
            transport.request.call_args_list[2].args[:2],
            ("POST", "atomic-testings/" + INJECT_ID + "/launch"),
        )

    def test_generic_execution_success_cannot_pass_wrong_domain_body(self):
        instance, transport = self._recorder()
        transport.request.side_effect = self._responses({"error": {"code": "wrong"}})

        def assert_body(body):
            raise AssertionError("sensitive callback payload " + TOKEN + str(body))

        with self.assertRaises(recorder.OpenAEVRecordingError) as caught:
            instance.record_case("wrong-body", "GET", PATH, TOKEN, None, assert_body)
        self.assertEqual(str(caught.exception), "recorded_response_assertion_failed")
        self.assertEqual(caught.exception.inject_id, INJECT_ID)
        self.assertIsNone(caught.exception.__cause__)
        self.assertTrue(caught.exception.__suppress_context__)

    def test_invalid_json_and_execution_error_preserve_inject_id(self):
        for execution_status, message, code in (
            ("EXECUTED", "not json", "invalid_recorded_json_response"),
            ("ERROR", TOKEN, "injector_execution_failed"),
        ):
            instance, transport = self._recorder()
            responses = self._responses({})
            responses[-1] = {
                "status_main_traces": [
                    {
                        "execution_action": "COMPLETE",
                        "execution_status": execution_status,
                        "execution_message": message,
                    }
                ]
            }
            transport.request.side_effect = responses
            with (
                self.subTest(status=execution_status),
                self.assertRaises(recorder.OpenAEVRecordingError) as caught,
            ):
                instance.record_case("failed", "GET", PATH, TOKEN, None, lambda _: None)
            self.assertEqual(str(caught.exception), code)
            self.assertEqual(caught.exception.inject_id, INJECT_ID)

    def test_domain_runtime_error_is_sanitized_with_inject_id(self):
        class DomainError(RuntimeError):
            pass

        instance, transport = self._recorder()
        transport.request.side_effect = self._responses({"value": "synthetic-canary"})

        def assert_body(body):
            raise DomainError(TOKEN + " synthetic-canary " + str(body))

        with self.assertRaises(recorder.OpenAEVRecordingError) as caught:
            instance.record_case("custom-error", "GET", PATH, TOKEN, None, assert_body)
        self.assertEqual(str(caught.exception), "recorded_response_assertion_failed")
        self.assertEqual(caught.exception.inject_id, INJECT_ID)
        self.assertIsNone(caught.exception.__cause__)
        self.assertTrue(caught.exception.__suppress_context__)

    def test_no_body_post_checks_form_contract_then_json_post_checks_raw_contract(self):
        instance, transport = self._recorder()
        first = self._responses({})
        first[0] = {"injector_contract_id": recorder.FORM_POST_CONTRACT}
        second = self._responses({})
        second[0] = {"injector_contract_id": recorder.CONTRACTS["POST"]}
        transport.request.side_effect = first + second
        instance.record_case("no-body", "POST", PATH, TOKEN, None, lambda _: None)
        instance.record_case("json-body", "POST", PATH, TOKEN, {}, lambda _: None)
        calls = transport.request.call_args_list
        self.assertEqual(
            calls[0].args, ("GET", "injector_contracts/" + recorder.FORM_POST_CONTRACT)
        )
        self.assertEqual(
            calls[4].args, ("GET", "injector_contracts/" + recorder.CONTRACTS["POST"])
        )
        self.assertEqual(calls[1].args[2]["inject_content"]["parts"], [])
        self.assertNotIn("body", calls[1].args[2]["inject_content"])
        self.assertEqual(calls[5].args[2]["inject_content"]["body"], "{}")

    def test_resume_verifies_exact_predispatch_failure_only(self):
        name = "workload-human-plaintext-reveal"
        expected = (
            "The inject is not ready to be executed (injectId="
            + INJECT_ID
            + ", title=Hikyo isolated lab security: "
            + name
            + ", missing mandatory fields: body:MANDATORY_CONTENT)"
        )
        status = {
            "status_name": "ERROR",
            "status_main_traces": [
                {
                    "execution_action": "COMPLETE",
                    "execution_status": "ERROR",
                    "execution_message": expected,
                }
            ],
        }
        instance, transport = self._recorder()
        transport.request.return_value = status
        proof = instance.verify_missing_body_failure(INJECT_ID, name)
        self.assertTrue(proof["failure_verified"])
        self.assertEqual(proof["inject_id"], INJECT_ID)
        for malformed in (
            {**status, "status_name": "EXECUTED"},
            {**status, "status_main_traces": []},
            {**status, "status_main_traces": status["status_main_traces"] * 2},
            {
                **status,
                "status_main_traces": [
                    {
                        "execution_action": "COMPLETE",
                        "execution_status": "ERROR",
                        "execution_message": expected.replace("body:", "parts:"),
                    }
                ],
            },
        ):
            transport.request.return_value = malformed
            with self.assertRaises(recorder.OpenAEVRecordingError) as caught:
                instance.verify_missing_body_failure(INJECT_ID, name)
            self.assertEqual(str(caught.exception), "resume_failure_not_verified")
            self.assertEqual(caught.exception.inject_id, INJECT_ID)
        with self.assertRaises(recorder.OpenAEVRecordingError):
            instance.verify_missing_body_failure("urn:uuid:" + INJECT_ID, name)

    def test_bounded_timeout_and_invalid_created_identifier(self):
        instance, transport = self._recorder()
        transport.request.side_effect = self._responses({})[:3]
        with (
            patch.object(recorder.time, "monotonic", side_effect=[0, 300]),
            self.assertRaises(recorder.OpenAEVRecordingError) as caught,
        ):
            instance.record_case("timeout", "GET", PATH, TOKEN, None, lambda _: None)
        self.assertEqual(str(caught.exception), "execution_timeout_300_seconds")
        self.assertEqual(caught.exception.inject_id, INJECT_ID)
        self.assertEqual(transport.request.call_count, 3)
        instance, transport = self._recorder()
        transport.request.side_effect = [
            {"injector_contract_id": recorder.CONTRACTS["GET"]},
            {"inject_id": "unsafe/path"},
        ]
        with self.assertRaises(recorder.OpenAEVRecordingError) as caught:
            instance.record_case("bad-id", "GET", PATH, TOKEN, None, lambda _: None)
        self.assertEqual(str(caught.exception), "invalid_created_inject_id")
        self.assertIsNone(caught.exception.inject_id)
        self.assertEqual(transport.request.call_count, 2)


if __name__ == "__main__":
    unittest.main()
