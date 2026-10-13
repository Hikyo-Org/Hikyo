"""Native audit attribution tests use synthetic events and bounded pages."""

from __future__ import annotations

import copy
import unittest
from urllib.parse import parse_qs, urlsplit

import hikyo_reporting_audit as audit

EVENT_ID = "evt_01a10e50-36f2-7777-8888-123456789012"
INJECT_ID = "f45e1862-d2fb-4b77-bfd2-171ee8c45cc6"
START = "2026-10-06T12:00:00Z"
END = "2026-10-06T12:00:03Z"
TARGET = (
    "/api/v1/orgs/org_fixture/projects/prj_fixture/environments/env_fixture/delivery"
)


def native_event(**changes: object) -> dict[str, object]:
    event: dict[str, object] = {
        "seq": 10,
        "id": EVENT_ID,
        "type": "grant.denied",
        "outcome": "denied",
        "actor_id": "sa_fixture",
        "actor_class": "machine",
        "org_id": "org_fixture",
        "project_id": "prj_fixture",
        "env_id": "env_fixture",
        "occurred_asserted": False,
        "occurred_at": "2026-10-06T12:00:01.002Z",
        "recorded_at": "2026-10-06T12:00:01.003Z",
        "correlation_id": None,
        "payload": {"operation": "delivery.fetch", "resolution": "resolvable"},
    }
    event.update(changes)
    return event


def native_page(
    events: list[dict[str, object]],
    *,
    upper: int = 10,
    cursor: int = 10,
    exhausted: bool = True,
) -> dict[str, object]:
    return {
        "items": events,
        "count": len(events),
        "upper_seq": upper,
        "next_after_seq": cursor,
        "exhausted": exhausted,
    }


class NativeAuditTests(unittest.TestCase):
    def assess(
        self,
        pages: list[dict[str, object]],
        *,
        case: str = "wrong-environment",
        target: str = TARGET,
        max_pages: int = 20,
    ) -> tuple[audit.NativeAuditEvidence, list[str]]:
        paths: list[str] = []

        def call(method: str, path: str) -> dict[str, object]:
            self.assertEqual(method, "GET")
            paths.append(path)
            return pages[len(paths) - 1]

        evidence = audit.assess_native_audit(
            call,
            inject_id=INJECT_ID,
            case_name=case,
            target_path=target,
            actor_id="sa_fixture",
            window_start=START,
            window_end=END,
            max_pages=max_pages,
        )
        return evidence, paths

    def test_exact_native_denial_is_attributed_to_dispatch_window(self) -> None:
        evidence, paths = self.assess([native_page([native_event()])])
        self.assertEqual(evidence.event_id, EVENT_ID)
        self.assertEqual(evidence.event_type, "grant.denied")
        self.assertTrue(evidence.query_succeeded)
        self.assertTrue(evidence.actor_scope_verified)
        self.assertNotIn("sa_fixture", str(evidence))
        parts = urlsplit(paths[0])
        self.assertEqual(parts.path, "/api/v1/orgs/org_fixture/audit")
        self.assertEqual(parse_qs(parts.query)["from"], [START])
        self.assertEqual(parse_qs(parts.query)["to"], [END])

    def test_only_full_actor_scope_operation_outcome_matches(self) -> None:
        changes = (
            {"actor_id": "sa_other"},
            {"actor_class": "human"},
            {"org_id": "org_other"},
            {"project_id": "prj_other"},
            {"env_id": "env_other"},
            {"project_id": None},
            {"env_id": None},
            {"outcome": "success"},
            {"type": "auth.artifact_class_refused"},
            {"occurred_asserted": True},
            {"payload": {"operation": "value.stage", "resolution": "resolvable"}},
            {"payload": {"operation": "delivery.fetch", "resolution": "unresolvable"}},
        )
        for change in changes:
            with self.subTest(change=change):
                evidence, _ = self.assess([native_page([native_event(**change)])])
                self.assertIsNone(evidence.event_id)
                self.assertTrue(evidence.query_succeeded)

    def test_later_direct_probe_cannot_supply_detection_evidence(self) -> None:
        for timestamp in ("occurred_at", "recorded_at"):
            with self.subTest(timestamp=timestamp):
                evidence, _ = self.assess(
                    [native_page([native_event(**{timestamp: "2026-10-06T12:00:04Z"})])]
                )
                self.assertIsNone(evidence.event_id)
        later = native_event(
            seq=11,
            id="evt_01a10e50-36f2-7777-8888-123456789013",
            occurred_at="2026-10-06T12:00:04Z",
        )
        evidence, _ = self.assess(
            [native_page([native_event(), later], upper=11, cursor=11)]
        )
        self.assertEqual(evidence.event_id, EVENT_ID)

    def test_empty_filtered_pages_continue_with_pinned_ceiling(self) -> None:
        evidence, paths = self.assess(
            [
                native_page([], upper=10, cursor=5, exhausted=False),
                native_page([native_event()]),
            ]
        )
        self.assertEqual(evidence.event_id, EVENT_ID)
        query = parse_qs(urlsplit(paths[1]).query)
        self.assertEqual(query["to_seq"], ["10"])
        self.assertEqual(query["after_seq"], ["5"])

    def test_exhausted_empty_query_is_assessed_failure(self) -> None:
        evidence, _ = self.assess([native_page([], upper=0, cursor=0)])
        self.assertTrue(evidence.query_succeeded)
        self.assertTrue(evidence.actor_scope_verified)
        self.assertIsNone(evidence.event_id)
        self.assertIsNone(evidence.event_type)

    def test_ambiguous_matching_denials_are_rejected(self) -> None:
        duplicate = native_event(seq=11, id="evt_01a10e50-36f2-7777-8888-123456789013")
        with self.assertRaisesRegex(audit.NativeAuditError, "ambiguous_native_audit"):
            self.assess([native_page([native_event(), duplicate], upper=11, cursor=11)])

    def test_truncated_or_malformed_query_never_returns_a_verdict(self) -> None:
        with self.assertRaisesRegex(audit.NativeAuditError, "page_bound_exceeded"):
            self.assess(
                [native_page([], upper=10, cursor=5, exhausted=False)], max_pages=1
            )
        with self.assertRaisesRegex(audit.NativeAuditError, "ceiling_changed"):
            self.assess(
                [
                    native_page([], upper=10, cursor=5, exhausted=False),
                    native_page([], upper=11, cursor=11),
                ]
            )
        with self.assertRaisesRegex(audit.NativeAuditError, "did_not_advance"):
            self.assess([native_page([], upper=10, cursor=0, exhausted=False)])
        malformed = native_page([native_event()])
        malformed["count"] = 0
        with self.assertRaisesRegex(audit.NativeAuditError, "invalid_native_audit"):
            self.assess([malformed])

    def test_duplicate_id_and_unsorted_native_pages_are_rejected(self) -> None:
        duplicate = native_event(seq=11)
        with self.assertRaisesRegex(audit.NativeAuditError, "duplicate_native_audit"):
            self.assess([native_page([native_event(), duplicate], upper=11, cursor=11)])
        with self.assertRaisesRegex(audit.NativeAuditError, "event_order"):
            self.assess(
                [
                    native_page(
                        [native_event(seq=11), native_event()], upper=11, cursor=11
                    )
                ]
            )

    def test_query_failures_are_sanitized_and_raise(self) -> None:
        def fail(method: str, path: str) -> dict[str, object]:
            raise RuntimeError("sensitive transport headers and body")

        with self.assertRaisesRegex(
            audit.NativeAuditError, "^native_audit_query_failed$"
        ):
            audit.assess_native_audit(
                fail,
                inject_id=INJECT_ID,
                case_name="wrong-tenant",
                target_path=TARGET,
                actor_id="sa_fixture",
                window_start=START,
                window_end=END,
            )

    def test_match_before_failed_next_page_cannot_create_success(self) -> None:
        calls = 0

        def fail_later(method: str, path: str) -> dict[str, object]:
            nonlocal calls
            calls += 1
            if calls == 1:
                return native_page(
                    [native_event()], upper=11, cursor=10, exhausted=False
                )
            raise RuntimeError("private subsequent page")

        with self.assertRaisesRegex(
            audit.NativeAuditError, "^native_audit_query_failed$"
        ):
            audit.assess_native_audit(
                fail_later,
                inject_id=INJECT_ID,
                case_name="wrong-environment",
                target_path=TARGET,
                actor_id="sa_fixture",
                window_start=START,
                window_end=END,
            )
        self.assertEqual(calls, 2)

    def test_correlation_exports_only_canonical_identifiers(self) -> None:
        evidence, _ = self.assess(
            [native_page([native_event(correlation_id=INJECT_ID)])]
        )
        self.assertEqual(evidence.correlation_id, INJECT_ID)
        evidence, _ = self.assess(
            [native_page([native_event(correlation_id="private caller text")])]
        )
        self.assertIsNone(evidence.correlation_id)
        self.assertNotIn("private caller text", str(evidence))

    def test_case_operation_and_target_are_explicit(self) -> None:
        for case, operation, suffix in (
            ("write-without-edit", "value.stage", "values/LAB_CONFIG"),
            ("publish-without-publish", "value.publish", "publish"),
        ):
            with self.subTest(case=case):
                event = native_event(
                    payload={"operation": operation, "resolution": "resolvable"}
                )
                evidence, _ = self.assess(
                    [native_page([event])],
                    case=case,
                    target=TARGET.removesuffix("delivery") + suffix,
                )
                self.assertEqual(evidence.event_id, EVENT_ID)
        with self.assertRaisesRegex(
            audit.NativeAuditError, "operation_target_mismatch"
        ):
            self.assess([native_page([])], case="write-without-edit")
        with self.assertRaisesRegex(audit.NativeAuditError, "case_not_measured"):
            self.assess([native_page([])], case="self-grant-reveal")

    def test_invalid_server_time_or_identifier_does_not_become_green(self) -> None:
        for change in (
            {"occurred_at": "not a timestamp"},
            {"occurred_at": "2026-10-06T12:00:01"},
            {"id": "not-native-event"},
            {"seq": True},
        ):
            with self.subTest(change=change), self.assertRaises(audit.NativeAuditError):
                self.assess([native_page([native_event(**change)])])

    def test_input_pages_are_not_mutated(self) -> None:
        pages = [native_page([native_event()])]
        expected = copy.deepcopy(pages)
        self.assess(pages)
        self.assertEqual(pages, expected)


if __name__ == "__main__":
    unittest.main()
