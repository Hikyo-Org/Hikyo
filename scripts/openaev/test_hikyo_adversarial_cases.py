"""Regression checks for bounded adversarial plans and disclosure assertions."""

from __future__ import annotations

import copy
import json
import unittest
from dataclasses import replace
from urllib.parse import parse_qs, urlsplit

from hikyo_adversarial_cases import (
    MAX_BODY_BYTES,
    MAX_CASE_BYTES,
    MAX_PATH_BYTES,
    AdversarialAssertionError,
    assert_response,
    build_cases,
)


def fixture() -> dict[str, object]:
    base = "/api/v1/orgs/org_lab/projects/prj_lab"
    return {
        "org_path": "/api/v1/orgs/org_lab",
        "base": base,
        "env": base + "/environments/env_lab",
        "outside_envs": [
            base + "/environments/env_outside",
            "/api/v1/orgs/org_lab/projects/prj_other/environments/env_other",
            "/api/v1/orgs/org_other/projects/prj_other/environments/env_other",
        ],
        "accounts": {"reader": {"principal": "sa_lab"}},
        "publish_version": "ver_pending",
        "config_value": "CONFIG_TEST_CANARY",
        "secret_value": "SECRET_TEST_CANARY",
    }


def presence() -> dict[str, object]:
    return {
        "current": False,
        "keys": [
            {
                "name": "LAB_CONFIG",
                "classification": "config",
                "presence": "set",
                "value": "CONFIG_TEST_CANARY",
            },
            {"name": "LAB_SECRET", "classification": "secret", "presence": "set"},
        ],
    }


class PlanTests(unittest.TestCase):
    def setUp(self) -> None:
        self.fixture = fixture()
        self.cases = build_cases(self.fixture, "valid_full_cursor")
        self.named = {case.name: case for case in self.cases}

    def test_escaped_fixture_canaries_are_rejected_at_build_and_assertion_boundaries(self) -> None:
        denied = next(case for case in self.cases if case.mode == "not_found")
        for field in ("config_value", "secret_value"):
            for canary in ('quote"canary', "slash\\canary", "line\ncanary", "canaryé"):
                altered = {**self.fixture, field: canary}
                response = {"error": {"code": "not_found"}, "detail": canary}
                for boundary in ("build", "direct"):
                    with self.subTest(field=field, canary=canary, boundary=boundary):
                        with self.assertRaisesRegex(AdversarialAssertionError, "invalid_fixture_canary"):
                            if boundary == "build":
                                build_cases(altered, "valid_full_cursor")
                            else:
                                assert_response(altered, denied, response)

    def test_exact_status_matrix_and_unique_names(self) -> None:
        outcomes = {
            case.name.removeprefix("whack-"): case.status for case in self.cases
        }
        self.assertEqual(
            outcomes,
            {
                "cursor-sql": 200,
                "cursor-xss": 200,
                "cursor-traversal": 200,
                "cursor-unicode": 200,
                "cursor-boundary-128": 200,
                "cursor-boundary-129": 400,
                "cursor-8k": 400,
                "cursor-projection-binding": 200,
                "cursor-outside-scope": 404,
                "long-workload-token": 401,
                "tampered-workload-token": 401,
                "hostile-scalar-write": 404,
                "8k-scalar-write": 404,
                "oversized-scalar-write": 400,
                "structured-value": 400,
                "deep-json-value": 400,
                "value-mass-assignment": 400,
                "grant-mass-assignment": 400,
                "publish-mass-assignment": 400,
                "publish-bypass-attempt": 404,
            },
        )
        self.assertEqual(len(self.named), 20)

    def test_payloads_are_finite_fixed_api_targets(self) -> None:
        for case in self.cases:
            with self.subTest(case=case.name):
                parsed = urlsplit(case.path)
                self.assertFalse(parsed.netloc or parsed.scheme or parsed.fragment)
                self.assertTrue(parsed.path.startswith("/api/v1/orgs/"))
                self.assertNotIn("..", parsed.path)
                body = json.dumps(case.body).encode() if case.body is not None else b""
                self.assertLessEqual(len(case.path.encode()), MAX_PATH_BYTES)
                self.assertLessEqual(len(body), MAX_BODY_BYTES)
                self.assertLessEqual(
                    len(body) + len(case.path.encode()), MAX_CASE_BYTES
                )
                if case.method != "GET":
                    self.assertIsNotNone(case.observer)
                if case.status == 400:
                    self.assertIsNone(case.detection_operation)

    def test_native_cursor_boundary_and_projection_binding(self) -> None:
        for suffix, length in (
            ("boundary-128", 128),
            ("boundary-129", 129),
            ("8k", 8192),
        ):
            query = parse_qs(urlsplit(self.named["whack-cursor-" + suffix].path).query)
            self.assertEqual(len(query["cursor"][0]), length)
        query = parse_qs(
            urlsplit(self.named["whack-cursor-projection-binding"].path).query
        )
        self.assertEqual(
            query, {"cursor": ["valid_full_cursor"], "projection": ["config-only"]}
        )

    def test_scalar_limits_and_valid_publish_bypass(self) -> None:
        for name, length in (
            ("8k-scalar-write", 8192),
            ("oversized-scalar-write", 65537),
        ):
            body = self.named["whack-" + name].body
            self.assertIsNotNone(body)
            assert body is not None
            self.assertEqual(len(str(body["value"])), length)
        case = self.named["whack-publish-bypass-attempt"]
        self.assertEqual(
            case.body,
            {
                "version_ids": ["ver_pending"],
                "bypass": {"reason": "synthetic-lab bypass attempt"},
            },
        )
        self.assertEqual(case.detection_operation, "value.publish")
        self.assertTrue(
            all(
                case.path.endswith("/values/LAB_CONFIG")
                for case in self.cases
                if case.detection_operation == "value.stage"
            )
        )

    def test_fixture_cannot_escape_scope(self) -> None:
        for field, value in (
            ("env", "/api/v1/orgs/foreign/projects/prj/environments/env"),
            ("publish_version", "../../prod"),
            ("outside_envs", [self.fixture["env"]] * 3),
        ):
            altered = copy.deepcopy(self.fixture)
            altered[field] = value
            with self.assertRaises(AdversarialAssertionError):
                build_cases(altered, "valid_cursor")

    def test_paths_reject_urls_duplicates_encoded_path_escape_and_controls(
        self,
    ) -> None:
        case = self.cases[0]
        for path in (
            "https://production.invalid/api/v1/orgs/org_lab",
            case.path + "&cursor=second",
            "/api/v1/orgs/org_lab/%2e%2e/delivery",
            case.path + "%0Aheader",
            case.path.replace("/delivery?", "/publish?"),
        ):
            with self.subTest(path=path), self.assertRaises(AdversarialAssertionError):
                replace(case, path=path)

    def test_excessive_body_and_unobserved_mutation_rejected(self) -> None:
        case = self.named["whack-hostile-scalar-write"]
        with self.assertRaises(AdversarialAssertionError):
            replace(case, body={"value": "X" * MAX_BODY_BYTES})
        with self.assertRaises(AdversarialAssertionError):
            replace(case, observer=None)


class AssertionTests(unittest.TestCase):
    def setUp(self) -> None:
        self.fixture = fixture()
        self.named = {case.name: case for case in build_cases(self.fixture, "cursor")}
        self.case = self.named["whack-cursor-sql"]

    def test_presence_and_config_projection_pass(self) -> None:
        assert_response(self.fixture, self.case, presence())
        projected = presence()
        keys = projected["keys"]
        assert isinstance(keys, list)
        projected["keys"] = keys[:1]
        assert_response(
            self.fixture, self.named["whack-cursor-projection-binding"], projected
        )

    def test_current_true_and_secret_anywhere_fail(self) -> None:
        for extra in ({"current": True}, {"note": "SECRET_TEST_CANARY"}):
            response = presence() | extra
            with self.assertRaises(AdversarialAssertionError):
                assert_response(self.fixture, self.case, response)

    def test_duplicate_wrong_classification_and_unexpected_key_fail(self) -> None:
        for mutation in ("duplicate", "classification", "unexpected"):
            response = presence()
            keys = response["keys"]
            assert isinstance(keys, list)
            if mutation == "duplicate":
                keys.append(keys[0])
            elif mutation == "classification":
                keys[0]["classification"] = "secret"
            else:
                keys.append({"name": "OTHER_SECRET"})
            with self.assertRaises(AdversarialAssertionError):
                assert_response(self.fixture, self.case, response)

    def test_denials_require_exact_domain_and_no_canary_or_material(self) -> None:
        for name in (
            "whack-cursor-8k",
            "whack-long-workload-token",
            "whack-cursor-outside-scope",
        ):
            case = self.named[name]
            assert_response(self.fixture, case, {"error": {"code": case.mode}})
            refusals: tuple[dict[str, object], ...] = (
                {"error": {"code": "other"}},
                {"error": {"code": case.mode}, "keys": []},
                {"error": {"code": case.mode}, "message": "CONFIG_TEST_CANARY"},
            )
            for response in refusals:
                with (
                    self.subTest(name=name),
                    self.assertRaises(AdversarialAssertionError),
                ):
                    assert_response(self.fixture, case, response)

    def test_projection_no_secret_name_even_with_injected_assertion(self) -> None:
        case = self.named["whack-cursor-projection-binding"]
        with self.assertRaises(AdversarialAssertionError):
            assert_response(
                self.fixture, case, presence(), lambda _fixture, _mode, _body: None
            )

    def test_shared_delivery_assertion_receives_modes_and_retains_outer_guards(
        self,
    ) -> None:
        modes: list[str] = []

        def capture(
            _fixture: dict[str, object], mode: str, _response: dict[str, object]
        ) -> None:
            modes.append(mode)

        assert_response(self.fixture, self.case, presence(), capture)
        denied = self.named["whack-cursor-outside-scope"]
        assert_response(self.fixture, denied, {"error": {"code": "not_found"}}, capture)
        invalid = self.named["whack-cursor-8k"]
        assert_response(
            self.fixture, invalid, {"error": {"code": "bad_request"}}, capture
        )
        self.assertEqual(modes, ["presence", "not_found"])


if __name__ == "__main__":
    unittest.main()
