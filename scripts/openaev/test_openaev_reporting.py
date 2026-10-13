"""Reporting must refuse unproved outcomes and preserve native provenance."""

from __future__ import annotations

import copy
import unittest
from dataclasses import replace
from unittest.mock import patch

import openaev_reporting as reporting
import openaev_security

ASSET_ID = "01624f0f-8e78-4cb9-8b6d-a989972d57ed"
OWNER_ID = "60bc3ed3-f883-4720-8bc1-08608402584f"
INJECT_ID = "88e07408-9d6e-429e-9260-04a3a89d0baf"
STATUS_ID = "1689636b-13cf-40d0-8d84-9c06380cafef"
EVENT_ID = "b1234cbb-122a-4b98-98c9-e20b54818330"
AUDIT_EVENT_ID = "evt_" + EVENT_ID
PATH = "/api/v1/orgs/lab/projects/project/environments/staging/delivery"
CASE = "write-without-edit"


def asset() -> dict[str, object]:
    return {
        "asset_id": ASSET_ID,
        "asset_name": reporting.ASSET_NAME,
        "asset_url": reporting.HIKYO_ORIGIN,
        "asset_external_reference": reporting.ASSET_REFERENCE,
        "asset_category": "WEB_APPLICATION",
        "asset_subcategory": "WEB_API",
        "asset_agents": [],
    }


def payload(case: str = CASE) -> dict[str, object]:
    return openaev_security._payload(case, "GET", PATH, None, None)


def search_page(rows: list[dict[str, object]], number: int = 0) -> dict[str, object]:
    size = 100
    total = len(rows)
    content = copy.deepcopy(rows[number * size:(number + 1) * size])
    for row in content:
        row.pop("asset_url", None)
    pages = (total + size - 1) // size
    return {"content": content, "number": number, "size": size,
            "totalElements": total, "totalPages": pages,
            "numberOfElements": len(content), "last": number + 1 >= pages}


def other_asset(index: int) -> dict[str, object]:
    row = asset()
    row["asset_id"] = f"00000000-0000-4000-8000-{index:012d}"
    row["asset_external_reference"] = f"a-other-{index:04d}"
    return row


class FakeAPI:
    def __init__(self, detection: bool = False):
        self.calls: list[tuple[str, str, dict[str, object] | None]] = []
        self.assets: list[dict[str, object]] = [asset()]
        self.rows: list[dict[str, object]] = []
        for i, kind in enumerate(
            ["PREVENTION", "VULNERABILITY"] + (["DETECTION"] if detection else [])
        ):
            self.rows.append(
                {
                    "inject_expectation_id": f"88e07408-9d6e-429e-9260-04a3a89d0ba{i}",
                    "inject_expectation_type": kind,
                    "inject_expectation_asset": ASSET_ID,
                    "inject_expectation_expected_score": 100,
                    "inject_expectation_score": 0,
                    "inject_expectation_status": "FAILED",
                    "inject_expectation_results": [],
                }
            )
        self.title = "Hikyo isolated lab security: " + CASE
        self.target = reporting.HIKYO_ORIGIN + PATH
        self.persist = True
        self.post_read_override: dict[str, object] | None = None
        self.inject_id = INJECT_ID
        self.status: dict[str, object] = {
            "status_id": STATUS_ID,
            "status_name": "EXECUTED",
            "tracking_sent_date": "2026-10-06T09:00:00Z",
            "tracking_end_date": "2026-10-06T09:00:03Z",
            "status_main_traces": [
                {
                    "execution_action": "EXECUTION",
                    "execution_status": "INFO",
                    "execution_time": "2026-10-06T09:00:00Z",
                    "execution_message": reporting.DISPATCH_MESSAGE,
                },
                {
                    "execution_action": "COMPLETE",
                    "execution_status": "EXECUTED",
                    "execution_time": "2026-10-06T09:00:02.998Z",
                    "execution_message": "native completion",
                },
            ],
        }
        self.trace_override: dict[str, object] = {}
        self.home_rows: list[dict[str, object]] = [
            {"label": status, "value": 0, "data": []}
            for status in ("SUCCESS", "FAILED", "PENDING")
        ]

    def request(
        self, method: str, path: str, payload: dict[str, object] | None = None
    ) -> object:
        self.calls.append((method, path, copy.deepcopy(payload)))
        if path == "endpoints/search":
            if payload is None:
                raise AssertionError("search pagination body required")
            return search_page(self.assets, payload["page"])
        if path == "endpoints/" + ASSET_ID:
            full = copy.deepcopy(next(row for row in self.assets if row["asset_id"] == ASSET_ID))
            full.pop("asset_external_reference", None)
            return full
        if path == "endpoints/agentless":
            self.assets.append(asset())
            return asset()
        if "/target_results/" in path:
            if not path.endswith("/types/ASSETS"):
                raise AssertionError("wrong native target enum")
            rows = copy.deepcopy(self.rows)
            if self.post_read_override is not None and any(
                method == "PUT" for method, _, _ in self.calls
            ):
                rows[0].update(self.post_read_override)
            return rows
        if path.startswith("atomic-testings/"):
            return {
                "inject_id": self.inject_id,
                "inject_title": self.title,
                "inject_content": {"uri": self.target},
                "inject_expectations": [
                    {
                        "inject_expectation_id": r["inject_expectation_id"],
                        "inject_expectation_name": "native simple",
                    }
                    for r in self.rows
                ],
                "inject_status": {
                    key: value
                    for key, value in self.status.items()
                    if key != "status_main_traces"
                },
            }
        if path == "injects/status-all-traces?injectId=" + INJECT_ID:
            return copy.deepcopy(self.status | self.trace_override)
        if path.startswith("expectations/"):
            if payload is None:
                raise AssertionError("missing update payload")
            row = next(
                r for r in self.rows if r["inject_expectation_id"] == path.split("/")[1]
            )
            if self.persist:
                row["inject_expectation_score"] = payload["expectation_score"]
                row["inject_expectation_status"] = (
                    "SUCCESS" if payload["expectation_score"] == 100 else "FAILED"
                )
                row["inject_expectation_results"] = [
                    {
                        "sourceId": payload["source_id"],
                        "sourceType": payload["source_type"],
                        "sourceName": payload["source_name"],
                        "score": payload["expectation_score"],
                    }
                ]
            return copy.deepcopy(row)
        if path == "dashboards/adhoc/series":
            return copy.deepcopy(self.home_rows)
        raise AssertionError("unexpected API operation")


def reporter(api: FakeAPI) -> reporting.OpenAEVReporter:
    result = reporting.OpenAEVReporter.__new__(reporting.OpenAEVReporter)
    result.owner_id = OWNER_ID
    result._api = api
    result._asset_id = ASSET_ID
    return result


def outcome(case: str = CASE) -> reporting.VerifiedOutcome:
    return reporting.VerifiedOutcome(INJECT_ID, case, 404, 404, True, True, True)


def audit(event: str | None = AUDIT_EVENT_ID) -> reporting.AuditEvidence:
    return reporting.AuditEvidence(
        INJECT_ID,
        CASE,
        "2026-10-06T09:00:00Z",
        "2026-10-06T09:00:03Z",
        True,
        True,
        event,
    )


def monitoring() -> reporting.MonitoringEvidence:
    return reporting.MonitoringEvidence(
        audit(),
        "5f6882ba-2112-4c13-9981-fdf26a8dbd76",
        "prn_independent_collector",
        True,
        "2026-10-06T08:59:59Z",
        "2026-10-06T09:00:01Z",
        "2026-10-06T09:00:02Z",
        "2026-10-06T09:00:04Z",
        "2026-10-06T09:00:05Z",
        "2026-10-06T09:00:06Z",
        "2026-10-06T09:00:07Z",
        "abcdef0123456789",
        "a" * 64,
        True,
        True,
        True,
    )


class ReportingTests(unittest.TestCase):
    def test_reporting_schema_errors_retain_safe_diagnostic_codes(self):
        for method_name, response, code in (
            ("_request", [], "invalid_reporting_object"),
            ("_array_request", {}, "invalid_reporting_array"),
            ("_array_request", [42], "invalid_reporting_array"),
        ):
            api = FakeAPI()
            with self.subTest(method=method_name, response=response), patch.object(
                api, "request", return_value=response
            ):
                with self.assertRaisesRegex(reporting.ReportingError, "^" + code + "$"):
                    getattr(reporter(api), method_name)("GET", "me")

    def test_reporting_transport_preserves_already_sanitized_error(self):
        for method_name in ("_request", "_array_request"):
            api = FakeAPI()
            safe = reporting.ReportingError("reporting_response_too_large")
            with self.subTest(method=method_name), patch.object(api, "request", side_effect=safe):
                with self.assertRaises(reporting.ReportingError) as caught:
                    getattr(reporter(api), method_name)("GET", "me")
                self.assertIs(caught.exception, safe)

    def test_reporting_raw_transport_errors_never_disclose_details(self):
        secret = "raw-network-body-secret"
        for method_name, code in (
            ("_request", "reporting_platform_request_failed"),
            ("_array_request", "reporting_platform_array_request_failed"),
        ):
            for exception_type in (OSError, ValueError, TypeError):
                api = FakeAPI()
                with self.subTest(method=method_name, exception=exception_type), patch.object(
                    api, "request", side_effect=exception_type(secret)
                ):
                    with self.assertRaises(reporting.ReportingError) as caught:
                        getattr(reporter(api), method_name)("GET", "me")
                    self.assertEqual(str(caught.exception), code)
                    self.assertNotIn(secret, str(caught.exception))
                    self.assertTrue(caught.exception.__suppress_context__)

    def test_new_monitoring_score_has_full_chain_source_and_receipt(self):
        api = FakeAPI(detection=True)
        native_result = reporter(api).publish_verdict(
            INJECT_ID, CASE, outcome(), monitoring_evidence=monitoring()
        )
        retained = reporting._objects(
            native_result["native_expectation_readback"], "invalid_test_readback"
        )
        self.assertEqual(
            {row["inject_expectation_type"] for row in retained},
            {"PREVENTION", "VULNERABILITY", "DETECTION"},
        )
        self.assertEqual(
            retained,
            [
                {
                    key: row[key]
                    for key in (
                        "inject_expectation_id",
                        "inject_expectation_type",
                        "inject_expectation_score",
                        "inject_expectation_status",
                        "inject_expectation_results",
                    )
                }
                for row in api.rows
            ],
        )
        self.assertEqual(
            reporting._objects(
                retained[-1]["inject_expectation_results"], "invalid_test_sources"
            )[0]["sourceName"],
            monitoring().source_name(),
        )
        source = reporting._objects(
            api.rows[-1]["inject_expectation_results"], "invalid_test_source"
        )
        self.assertEqual(source[0]["sourceName"], monitoring().source_name())
        self.assertEqual(api.rows[-1]["inject_expectation_score"], 100)
        historical = FakeAPI(detection=True)
        reporter(historical).publish_verdict(
            INJECT_ID, CASE, outcome(), audit_evidence=audit()
        )
        self.assertEqual(
            reporting._objects(
                historical.rows[-1]["inject_expectation_results"], "invalid_test_source"
            )[0]["sourceName"],
            reporting.AUDIT_SOURCE + " " + AUDIT_EVENT_ID,
        )

    def test_incomplete_or_mismatched_monitoring_chain_never_writes_verdict(self):
        proofs = (
            replace(monitoring(), collector_scope_only_verified=False),
            replace(monitoring(), scope_actor_window_verified=False),
            replace(monitoring(), rule_fired=False),
            replace(monitoring(), notification_delivered=False),
            replace(monitoring(), inbox_receipt_sha256="unknown"),
            replace(monitoring(), alertmanager_fingerprint="unknown"),
            replace(monitoring(), native_audit=audit(None)),
            replace(monitoring(), baseline_at="2026-10-06T09:00:01Z"),
            replace(monitoring(), native_occurred_at="2026-10-06T08:59:58Z"),
            replace(monitoring(), native_recorded_at="2026-10-06T08:59:58Z"),
            replace(monitoring(), inbox_received_at="2026-10-06T09:20:00Z"),
            replace(monitoring(), collector_polled_at="2026-10-06T09:00:10Z"),
            replace(monitoring(), native_audit=replace(audit(), inject_id=EVENT_ID)),
        )
        for proof in proofs:
            api = FakeAPI(detection=True)
            with self.subTest(proof=proof), self.assertRaises(reporting.ReportingError):
                reporter(api).publish_verdict(
                    INJECT_ID, CASE, outcome(), monitoring_evidence=proof
                )
            self.assertFalse(any(method == "PUT" for method, _, _ in api.calls))

    def test_monitoring_and_audit_sources_cannot_be_combined(self):
        api = FakeAPI(detection=True)
        with self.assertRaisesRegex(
            reporting.ReportingError, "duplicate_detection_evidence_sources"
        ):
            reporter(api).publish_verdict(
                INJECT_ID,
                CASE,
                outcome(),
                audit_evidence=audit(),
                monitoring_evidence=monitoring(),
            )
        self.assertFalse(any(method == "PUT" for method, _, _ in api.calls))

    def test_independent_expectation_description_is_opt_in(self):
        configured = reporting.configure_payload(
            payload(), CASE, ASSET_ID, detection_assessed=True, monitoring_assessed=True
        )
        content = configured["inject_content"]
        self.assertIsInstance(content, dict)
        if isinstance(content, dict):
            expectations = content["expectations"]
            self.assertTrue(
                any(
                    "Prometheus" in row["expectation_description"]
                    for row in expectations
                )
            )
        with self.assertRaises(reporting.ReportingError):
            reporting.configure_payload(
                payload(), CASE, ASSET_ID, monitoring_assessed=True
            )

    def test_positive_controls_have_no_attack_metrics(self):
        for case in reporting.POSITIVE_CASES:
            original = payload(case)
            configured = reporting.configure_payload(original, case, ASSET_ID)
            content = configured["inject_content"]
            self.assertIsInstance(content, dict)
            if isinstance(content, dict):
                self.assertEqual(content["expectations"], [])
            self.assertNotEqual(original, configured)
            self.assertEqual(original["inject_assets"], [])

    def test_detection_only_for_assessed_authenticated_denials(self):
        configured = reporting.configure_payload(
            payload(), CASE, ASSET_ID, detection_assessed=True
        )
        content = configured["inject_content"]
        if not isinstance(content, dict):
            self.fail("invalid content")
        expectations = content["expectations"]
        if not isinstance(expectations, list):
            self.fail("invalid expectations")
        kinds = [r["expectation_type"] for r in expectations]
        self.assertCountEqual(kinds, ["PREVENTION", "VULNERABILITY", "DETECTION"])
        for case in ("missing-token", "read-and-reveal", "opt-in-off-presence"):
            with self.subTest(case=case), self.assertRaises(reporting.ReportingError):
                reporting.configure_payload(
                    payload(case), case, ASSET_ID, detection_assessed=True
                )

    def test_config_rejects_foreign_origin_traversal_unknown_case(self):
        for uri in (
            "https://production.example/api/v1/orgs/lab",
            reporting.HIKYO_ORIGIN + "/api/v1/orgs/lab/../secrets",
            reporting.HIKYO_ORIGIN + PATH + "?redirect=production",
        ):
            request = payload()
            request["inject_content"] = {"uri": uri}
            with (
                self.subTest(uri=uri),
                self.assertRaises(
                    (reporting.ReportingError, openaev_security.OpenAEVRecordingError)
                ),
            ):
                reporting.configure_payload(request, CASE, ASSET_ID)
        with self.assertRaises(reporting.ReportingError):
            reporting.configure_payload(payload(), "new-unverified-case", ASSET_ID)

    def test_asset_reused_only_when_native_identity_matches(self):
        api = FakeAPI()
        result = reporter(api)
        self.assertEqual(result.ensure_asset(), ASSET_ID)
        self.assertFalse(any(path == "endpoints/agentless" for _, path, _ in api.calls))
        for key, value in (
            ("asset_url", "https://production.example"),
            ("asset_category", "HOST"),
            ("asset_agents", [{"agent_id": "live"}]),
        ):
            api.assets = [asset()]
            api.assets[0][key] = value
            with self.subTest(key=key), self.assertRaises(reporting.ReportingError):
                result.ensure_asset()
        api.assets = [asset(), asset()]
        with self.assertRaises(reporting.ReportingError):
            result.ensure_asset()

    def test_asset_on_later_native_page_is_reused_without_creation(self):
        api = FakeAPI()
        api.assets = [other_asset(index) for index in range(100)] + [asset()]
        self.assertEqual(reporter(api).ensure_asset(), ASSET_ID)
        pages = [body["page"] for _, path, body in api.calls if path == "endpoints/search"]
        self.assertEqual(pages, [0, 1])
        self.assertFalse(any(path == "endpoints/agentless" for _, path, _ in api.calls))
        self.assertEqual(len(api.assets), 101)

    def test_duplicate_external_reference_on_later_page_is_refused(self):
        api = FakeAPI()
        duplicate = asset()
        duplicate["asset_id"] = "aa624f0f-8e78-4cb9-8b6d-a989972d57ed"
        api.assets = [other_asset(index) for index in range(99)] + [asset(), duplicate]
        with self.assertRaisesRegex(reporting.ReportingError, "duplicate_disposable_reporting_assets"):
            reporter(api).ensure_asset()
        self.assertFalse(any(path == "endpoints/agentless" for _, path, _ in api.calls))

    def test_incomplete_or_invalid_native_asset_pages_never_create(self):
        original = search_page([asset()])
        for field, value in (
            ("number", None), ("number", True), ("number", 1), ("size", 99),
            ("totalPages", 2), ("totalElements", 2), ("numberOfElements", 0),
            ("last", False), ("last", 1),
        ):
            api = FakeAPI()
            page = {**original, field: value}
            with self.subTest(field=field, value=value), patch.object(api, "request", return_value=page) as request:
                with self.assertRaisesRegex(reporting.ReportingError, "invalid_asset_search_pagination"):
                    reporter(api).ensure_asset()
                self.assertEqual(request.call_count, 1)

    def test_changed_totals_or_repeated_asset_ids_refuse_complete_search(self):
        for changed in ("totals", "repeated"):
            api = FakeAPI()
            api.assets = [other_asset(index) for index in range(101)]
            original_request = api.request
            def changed_second(method, path, payload=None):
                page = original_request(method, path, payload)
                if path == "endpoints/search" and payload["page"] == 1:
                    if changed == "totals":
                        page = search_page(api.assets + [other_asset(101)], 1)
                    else:
                        page["content"] = [other_asset(0)]
                return page
            with self.subTest(changed=changed), patch.object(api, "request", side_effect=changed_second):
                code = "asset_search_inventory_changed" if changed == "totals" else "asset_search_inventory_repeated"
                with self.assertRaisesRegex(reporting.ReportingError, code):
                    reporter(api).ensure_asset()
            self.assertFalse(any(path == "endpoints/agentless" for _, path, _ in api.calls))

    def test_asset_search_inventory_bound_is_refused_before_mutation(self):
        api = FakeAPI()
        rows = [other_asset(index) for index in range(2001)]
        with patch.object(api, "request", return_value=search_page(rows)) as request:
            with self.assertRaisesRegex(reporting.ReportingError, "asset_search_page_limit"):
                reporter(api).ensure_asset()
            self.assertEqual(request.call_count, 1)

    def test_failed_later_page_never_creates_or_claims_complete_search(self):
        api = FakeAPI()
        api.assets = [other_asset(index) for index in range(101)]
        original_request = api.request
        def fail_second(method, path, payload=None):
            if path == "endpoints/search" and payload["page"] == 1:
                raise OSError("raw-private-network-detail")
            return original_request(method, path, payload)
        with patch.object(api, "request", side_effect=fail_second):
            with self.assertRaisesRegex(reporting.ReportingError, "^reporting_platform_request_failed$"):
                reporter(api).ensure_asset()
        self.assertFalse(any(path == "endpoints/agentless" for _, path, _ in api.calls))

    def test_empty_inventory_creates_only_disposable_agentless_target(self):
        api = FakeAPI()
        api.assets = []
        self.assertEqual(reporter(api).ensure_asset(), ASSET_ID)
        create = next(
            values for _, path, values in api.calls if path == "endpoints/agentless"
        )
        self.assertIsNotNone(create)
        if create:
            self.assertEqual(create["asset_url"], reporting.HIKYO_ORIGIN)
            self.assertFalse(create["asset_internet_facing"])

    def test_generic_executed_status_or_partial_assertions_never_pass(self):
        changes = [
            replace(outcome(), recorded_body_asserted=False),
            replace(outcome(), direct_body_asserted=False),
            replace(outcome(), state_asserted=False),
            replace(outcome(), actual_status=200),
            replace(outcome(), expected_status=200),
            replace(outcome(), inject_id=EVENT_ID),
            replace(outcome(), case_name="self-grant-reveal"),
        ]
        for modified in changes:
            api = FakeAPI()
            with (
                self.subTest(proof=modified),
                self.assertRaises(reporting.ReportingError),
            ):
                reporter(api).publish_verdict(INJECT_ID, CASE, modified)
            self.assertFalse(any(method == "PUT" for method, _, _ in api.calls))

    def test_failed_preflight_never_partially_updates_foreign_rows(self):
        api = FakeAPI()
        api.rows[-1]["inject_expectation_asset"] = EVENT_ID
        with self.assertRaises(reporting.ReportingError):
            reporter(api).publish_verdict(INJECT_ID, CASE, outcome())
        self.assertFalse(any(method == "PUT" for method, _, _ in api.calls))
        api = FakeAPI()
        api.target = "https://production.example" + PATH
        with self.assertRaises(reporting.ReportingError):
            reporter(api).publish_verdict(INJECT_ID, CASE, outcome())
        self.assertFalse(any(method == "PUT" for method, _, _ in api.calls))

    def test_native_verdicts_have_honest_owner_provenance_and_idempotent_sources(self):
        api = FakeAPI()
        result = reporter(api)
        for _ in range(2):
            result.publish_verdict(INJECT_ID, CASE, outcome())
        for row in api.rows:
            self.assertEqual(row["inject_expectation_score"], 100)
            self.assertEqual(row["inject_expectation_status"], "SUCCESS")
            sources = row["inject_expectation_results"]
            self.assertIsInstance(sources, list)
            if isinstance(sources, list):
                self.assertEqual(len(sources), 1)
                self.assertEqual(sources[0]["sourceType"], "user")
                self.assertEqual(sources[0]["sourceId"], OWNER_ID)
                self.assertEqual(sources[0]["sourceName"], reporting.ASSERTION_SOURCE)
        self.assertFalse(
            any(
                "relaunch" in path or method == "DELETE"
                for method, path, _ in api.calls
            )
        )

    def test_postwrite_identity_and_target_cannot_change_under_green_scores(self):
        changes: list[dict[str, object]] = [
            {"inject_expectation_type": "DETECTION"},
            {"inject_expectation_asset": EVENT_ID},
            {"inject_expectation_expected_score": 50},
            {"inject_expectation_id": EVENT_ID},
        ]
        for change in changes:
            api = FakeAPI()
            api.post_read_override = change
            with (
                self.subTest(change=change),
                self.assertRaises(reporting.ReportingError),
            ):
                reporter(api).publish_verdict(INJECT_ID, CASE, outcome())

    def test_successful_submission_is_not_persistence_proof(self):
        api = FakeAPI()
        api.persist = False
        with self.assertRaisesRegex(
            reporting.ReportingError, "native_verdict_persistence_mismatch"
        ):
            reporter(api).publish_verdict(INJECT_ID, CASE, outcome())

    def test_detection_requires_matching_bounded_native_assessment(self):
        api = FakeAPI(detection=True)
        result = reporter(api)
        with self.assertRaises(reporting.ReportingError):
            result.publish_verdict(INJECT_ID, CASE, outcome())
        for proof in (
            replace(audit(), query_succeeded=False),
            replace(audit(), actor_scope_verified=False),
            replace(audit(), inject_id=EVENT_ID),
            replace(audit(), window_end="2026-10-06T09:20:00Z"),
            replace(audit(), window_end="2026-10-06T09:10:00Z"),
            replace(
                audit(),
                window_start="2026-10-06T09:01:00Z",
                window_end="2026-10-06T09:04:00Z",
            ),
            replace(audit(), window_start="2026-10-06T09:00:00"),
        ):
            with self.subTest(proof=proof), self.assertRaises(reporting.ReportingError):
                result.publish_verdict(INJECT_ID, CASE, outcome(), audit_evidence=proof)
        self.assertFalse(any(method == "PUT" for method, _, _ in api.calls))
        result.publish_verdict(INJECT_ID, CASE, outcome(), audit_evidence=audit())
        self.assertEqual(api.rows[-1]["inject_expectation_status"], "SUCCESS")

    def test_assessed_absence_is_failed_detection_unknown_not_assessed(self):
        api = FakeAPI(detection=True)
        reporter(api).publish_verdict(
            INJECT_ID, CASE, outcome(), audit_evidence=audit(None)
        )
        self.assertEqual(api.rows[-1]["inject_expectation_score"], 0)
        self.assertEqual(api.rows[-1]["inject_expectation_status"], "FAILED")
        self.assertEqual(api.rows[0]["inject_expectation_status"], "SUCCESS")

    def test_execution_window_uses_native_dispatch_and_finish_times(self):
        api = FakeAPI()
        self.assertEqual(
            reporter(api).execution_window(INJECT_ID),
            ("2026-10-06T09:00:00Z", "2026-10-06T09:00:03Z"),
        )

    def test_execution_window_keeps_exact_trace_when_send_metadata_rounds_forward(self):
        api = FakeAPI()
        api.status["tracking_sent_date"] = "2026-10-09T15:25:01Z"
        api.status["tracking_end_date"] = "2026-10-09T15:25:01.826415Z"
        traces = reporting._objects(api.status["status_main_traces"], "test_traces")
        traces[0]["execution_time"] = "2026-10-09T15:25:00.612518Z"
        traces[1]["execution_time"] = "2026-10-09T15:25:01.824911Z"
        api.status["status_main_traces"] = traces
        start, end = reporter(api).execution_window(INJECT_ID)
        self.assertEqual(start, "2026-10-09T15:25:00.612518Z")
        self.assertEqual(end, "2026-10-09T15:25:01.826415Z")
        # Actual occurrence and commit are inside the precise dispatch window.
        self.assertLess(start, "2026-10-09T15:25:00.838995Z")
        self.assertGreater(end, "2026-10-09T15:25:00.839925Z")
        self.assertEqual(api.status["tracking_sent_date"], "2026-10-09T15:25:01Z")

    def test_execution_window_rejects_missing_ambiguous_and_foreign_traces(self):
        template = reporting._objects(
            FakeAPI().status["status_main_traces"], "test_traces"
        )
        variants: list[object] = [
            None,
            [],
            [template[1]],
            [template[0]],
            [template[0], template[0], template[1]],
            [template[0], template[1], template[1]],
            [{**template[0], "execution_agent": {"agent_id": OWNER_ID}}, template[1]],
            [{**template[0], "execution_context_identifiers": [ASSET_ID]}, template[1]],
            [{**template[0], "execution_message": "foreign operation"}, template[1]],
            [{**template[0], "execution_action": "START"}, template[1]],
            [{**template[0], "execution_status": "EXECUTED"}, template[1]],
            [template[0], {**template[1], "execution_status": "ERROR"}],
        ]
        for traces in variants:
            with self.subTest(traces=traces):
                api = FakeAPI()
                api.status["status_main_traces"] = traces
                with self.assertRaises(reporting.ReportingError):
                    reporter(api).execution_window(INJECT_ID)

    def test_execution_window_requires_same_native_inject_status_and_clocks(self):
        for field, value in (
            ("status_id", OWNER_ID),
            ("status_id", None),
            ("status_name", "PENDING"),
            ("tracking_sent_date", "2026-10-06T09:00:01Z"),
            ("tracking_end_date", "2026-10-06T09:00:04Z"),
        ):
            with self.subTest(field=field):
                api = FakeAPI()
                api.trace_override[field] = value
                with self.assertRaises(reporting.ReportingError):
                    reporter(api).execution_window(INJECT_ID)
        api = FakeAPI()
        api.inject_id = OWNER_ID
        with self.assertRaises(reporting.ReportingError):
            reporter(api).execution_window(INJECT_ID)

    def test_execution_window_rejects_malformed_unordered_or_excessive_precise_times(
        self,
    ):
        for field, value in (
            ("dispatch", "not-a-date"),
            ("dispatch", "2026-10-06T09:00:00"),
            ("dispatch", None),
            ("dispatch", "2026-10-06T09:00:04Z"),
            ("complete", "2026-10-06T08:59:59Z"),
            ("complete", "2026-10-06T09:00:04Z"),
            ("complete", "2026-10-06T09:00:02"),
            ("tracking_sent_date", "not-a-date"),
            ("tracking_end_date", "2026-10-06T09:00:03"),
            ("tracking_end_date", "2026-10-06T09:05:00.000001Z"),
        ):
            with self.subTest(field=field, value=value):
                api = FakeAPI()
                traces = reporting._objects(
                    api.status["status_main_traces"], "test_traces"
                )
                if field in {"dispatch", "complete"}:
                    traces[0 if field == "dispatch" else 1]["execution_time"] = value
                    api.status["status_main_traces"] = traces
                else:
                    api.status[field] = value
                with self.assertRaises(reporting.ReportingError):
                    reporter(api).execution_window(INJECT_ID)

    def test_publication_requires_precise_trace_window_without_rounded_fallback(self):
        api = FakeAPI(detection=True)
        traces = reporting._objects(api.status["status_main_traces"], "test_traces")
        traces[0]["execution_time"] = "2026-10-06T09:00:00.612518Z"
        api.status["status_main_traces"] = traces
        api.status["tracking_sent_date"] = "2026-10-06T09:00:01Z"
        rounded = replace(audit(), window_start="2026-10-06T09:00:01Z")
        with self.assertRaisesRegex(
            reporting.ReportingError, "native_audit_execution_window_mismatch"
        ):
            reporter(api).publish_verdict(
                INJECT_ID, CASE, outcome(), audit_evidence=rounded
            )
        self.assertFalse(any(method == "PUT" for method, _, _ in api.calls))
        precise = replace(audit(), window_start="2026-10-06T09:00:00.612518Z")
        result = reporter(api).publish_verdict(
            INJECT_ID, CASE, outcome(), audit_evidence=precise
        )
        self.assertEqual(
            result["expectations"],
            [
                {
                    "id": row["inject_expectation_id"],
                    "type": row["inject_expectation_type"],
                    "score": 100,
                    "status": "SUCCESS",
                }
                for row in api.rows
            ],
        )

    def test_home_buckets_use_native_enum_keys_not_display_labels(self):
        api = FakeAPI()
        api.home_rows[0]["data"] = [
            {"key": "prevention", "label": "prevention", "value": 21},
            {"key": "vulnerability", "label": "vulnerability", "value": 21},
        ]
        api.home_rows[1]["data"] = [
            {"key": "DETECTION", "label": "Detection", "value": 2}
        ]
        counts = reporter(api).home_metrics()
        self.assertEqual(
            counts["PREVENTION"], {"SUCCESS": 21, "FAILED": 0, "PENDING": 0}
        )
        self.assertEqual(counts["DETECTION"], {"SUCCESS": 0, "FAILED": 2, "PENDING": 0})
        api.home_rows[0]["data"] = [
            {"key": "PREVENTION", "label": "Prevention", "value": -1}
        ]
        with self.assertRaises(reporting.ReportingError):
            reporter(api).home_metrics()
        api.home_rows[0]["data"] = []
        api.home_rows.pop()
        with self.assertRaises(reporting.ReportingError):
            reporter(api).home_metrics()

    def test_home_query_uses_native_expectation_index_and_all_three_types(self):
        api = FakeAPI()
        reporter(api).home_metrics()
        _, path, values = api.calls[-1]
        self.assertEqual(path, "dashboards/adhoc/series")
        self.assertIsNotNone(values)
        if values is None:
            self.fail("missing Home query")
        config = values["widget_config"]
        if not isinstance(config, dict):
            self.fail("invalid config")
        series = config["series"]
        if not isinstance(series, list):
            self.fail("invalid series")
        self.assertEqual(len(series), 3)
        self.assertCountEqual(
            [r["name"] for r in series], ["SUCCESS", "FAILED", "PENDING"]
        )
        self.assertEqual(config["field"], "inject_expectation_type")
        self.assertEqual(
            reporter(api).home_metrics(),
            {
                kind: {status: 0 for status in ("SUCCESS", "FAILED", "PENDING")}
                for kind in reporting.TYPES
            },
        )


if __name__ == "__main__":
    unittest.main()
