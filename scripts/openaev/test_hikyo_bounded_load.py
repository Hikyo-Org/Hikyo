"""Adversarial tests for pacing, proofs, bounds, cancellation and truthful totals."""

from __future__ import annotations

import math
import threading
import time
import unittest
from asyncio import CancelledError
from collections.abc import Callable
from dataclasses import asdict, replace
from itertools import pairwise

import hikyo_bounded_load as load


def observation(status: int = 404) -> load.ResponseObservation:
    return load.ResponseObservation(status, 1.0, 20, True, status != 200)


def config(
    count: int = 3,
    *,
    cases: tuple[load.RequestCase, ...] | None = None,
    concurrency: int = 4,
    max_duration_s: float = 3.0,
    request_timeout_s: float = 0.3,
) -> load.PhaseConfig:
    return load.PhaseConfig(
        "authorization-burst",
        cases
        if cases is not None
        else (load.RequestCase("withheld-secret", frozenset({404})),),
        count,
        concurrency=concurrency,
        max_duration_s=max_duration_s,
        request_timeout_s=request_timeout_s,
    )


class BoundedLoadTests(unittest.TestCase):
    def failed(
        self,
        request: Callable[[load.RequestContext], load.ResponseObservation],
        phase: load.PhaseConfig | None = None,
        guard: Callable[[], None] = lambda: None,
        cancel: threading.Event | None = None,
    ) -> load.PhaseReport:
        with self.assertRaises(load.PhaseFailure) as caught:
            load.run_phase(phase or config(3), request, guard, cancellation=cancel)
        self.assertFalse(caught.exception.report.succeeded)
        self.assertNotIn("secret-value", str(caught.exception))
        self.assertNotIn("secret-value", repr(asdict(caught.exception.report)))
        self.assertIsNone(caught.exception.__cause__)
        return caught.exception.report

    def test_hard_bounds_no_permissive_casts(self) -> None:
        factories: tuple[Callable[[], load.PhaseConfig], ...] = (
            lambda: config(concurrency=9),
            lambda: config(concurrency=True),
            lambda: config(161),
            lambda: config(0),
            lambda: replace(config(), rate_per_second=12.001),
            lambda: replace(config(), rate_per_second=float("nan")),
            lambda: replace(config(), rate_per_second=True),
            lambda: config(max_duration_s=45.01),
            lambda: config(request_timeout_s=3.01),
            lambda: config(cases=()),
            lambda: replace(config(), name="secret-value\nbody"),
        )
        for index, factory in enumerate(factories):
            with self.subTest(index=index), self.assertRaises(ValueError):
                factory()
        with self.assertRaises(ValueError):
            load.RequestCase("case", frozenset({True}))
        with self.assertRaises(ValueError):
            load.RequestCase("case", frozenset({500}))

    def test_nearest_rank_not_interpolated_and_empty_unknown(self) -> None:
        values = tuple(float(value) for value in range(1, 101))
        self.assertEqual(
            load.latency_summary(values),
            load.LatencySummary(100, 50.0, 95.0, 99.0, 100.0),
        )
        self.assertEqual(
            load.latency_summary((9.0, 1.0)), load.LatencySummary(2, 1.0, 9.0, 9.0, 9.0)
        )
        self.assertIsNone(load.latency_summary(()).p50_ms)
        self.assertEqual(load.latency_summary((0.0,)).p50_ms, 0.0)
        with self.assertRaises(ValueError):
            load.latency_summary((math.inf,))

    def test_mixed_cases_round_robin_separate_allow_denial_rate_limit(self) -> None:
        phase = config(
            6,
            cases=(
                load.RequestCase("allowed-delivery", frozenset({200, 429})),
                load.RequestCase("removed-grant", frozenset({404})),
                load.RequestCase("revoked-credential", frozenset({401})),
            ),
        )
        calls: list[load.RequestContext] = []

        def request(context: load.RequestContext) -> load.ResponseObservation:
            calls.append(context)
            status = (200, 404, 401, 429, 404, 401)[context.index]
            return replace(
                observation(status), native_retry_after=1.0 if status == 429 else None
            )

        report = load.run_phase(phase, request, lambda: None)
        self.assertTrue(report.succeeded)
        self.assertEqual(len(calls), 6)
        self.assertEqual(report.status_counts, ((200, 1), (401, 2), (404, 2), (429, 1)))
        self.assertEqual(report.completed, report.requested)
        self.assertEqual(report.response_bytes, 120)
        self.assertEqual(report.completion_rate_per_second, 6 / report.wall_s)
        self.assertEqual(
            report.service_throughput_per_second, 6 / report.service_window_s
        )
        self.assertEqual(report.health_checks, 2)

    def test_status_expectations_bound_to_case(self) -> None:
        report = self.failed(lambda _: observation(200))
        self.assertEqual(report.completed, 0)
        self.assertEqual(report.failure, "request_exception")
        self.assertEqual(report.status_counts, ())

    def test_missing_body_secret_timing_metadata_proof_fail(self) -> None:
        for changed in (
            replace(observation(), body_verified=False),
            replace(observation(), secret_absent=None),
            replace(observation(), elapsed_ms=math.nan),
            replace(observation(), response_bytes=-1),
            replace(observation(), native_retry_after=1.0),
        ):
            with self.subTest(observation=changed):

                def request(
                    _: load.RequestContext, result: load.ResponseObservation = changed
                ) -> load.ResponseObservation:
                    return result

                report = self.failed(request, config(1))
                self.assertEqual(report.completed, 0)

    def test_actual_starts_paced_concurrency_and_periodic_health(self) -> None:
        lock = threading.Lock()
        starts: list[float] = []
        health_times: list[float] = []
        active = 0
        max_active = 0

        def guard() -> None:
            health_times.append(time.monotonic())

        def request(context: load.RequestContext) -> load.ResponseObservation:
            nonlocal active, max_active
            with lock:
                active += 1
                max_active = max(max_active, active)
                starts.append(time.monotonic())
            began = time.monotonic()
            time.sleep(0.15)
            with lock:
                active -= 1
            self.assertLessEqual(context.timeout_s, 0.3)
            return replace(observation(), elapsed_ms=(time.monotonic() - began) * 1000)

        report = load.run_phase(config(14, concurrency=2), request, guard)
        self.assertEqual(report.completed, 14)
        self.assertEqual(max_active, 2)
        gaps = [b - a for a, b in pairwise(starts)]
        self.assertTrue(all(gap >= 1 / 12 - 0.005 for gap in gaps), gaps)
        self.assertGreaterEqual(len(health_times), 3)
        self.assertTrue(all(b - a <= 1.2 for a, b in pairwise(health_times)))
        self.assertGreater(report.scheduling_lateness.max_ms or 0, 0)

    def test_assertion_first_failure_stops_launches_retains_completed(self) -> None:
        calls: list[int] = []

        def request(context: load.RequestContext) -> load.ResponseObservation:
            calls.append(context.index)
            if context.index == 1:
                raise AssertionError("secret-value in leaked body")
            return observation()

        report = self.failed(request, config(10))
        self.assertEqual(calls, [0, 1])
        self.assertEqual(report.completed, 1)
        self.assertEqual(report.failure, "request_assertion")
        self.assertEqual(report.failure_index, 1)
        self.assertEqual(report.failure_case, "withheld-secret")
        self.assertEqual(report.undrained_workers, 0)
        self.assertEqual(report.health_checks, 2)

    def test_guard_failure_no_requests_before_preflight(self) -> None:
        calls: list[int] = []

        def request(context: load.RequestContext) -> load.ResponseObservation:
            calls.append(context.index)
            return observation()

        def guard() -> None:
            raise AssertionError("secret-value in resource metrics")

        report = self.failed(request, guard=guard)
        self.assertEqual(calls, [])
        self.assertEqual(report.failure, "health_assertion")
        self.assertEqual(report.started, 0)

    def test_final_guard_failure_prevents_pass(self) -> None:
        checks = 0

        def guard() -> None:
            nonlocal checks
            checks += 1
            if checks == 2:
                raise AssertionError("secret-value")

        report = self.failed(lambda _: observation(), config(1), guard)
        self.assertEqual(report.completed, 1)
        self.assertEqual(report.failure, "health_assertion")

    def test_periodic_guard_failure_stops_load_and_retains_completed(self) -> None:
        checks = 0

        def guard() -> None:
            nonlocal checks
            checks += 1
            if checks == 2:
                raise AssertionError("secret-value")

        report = self.failed(lambda _: observation(), config(30), guard)
        self.assertEqual(report.failure, "health_assertion")
        self.assertGreater(report.completed, 0)
        self.assertLess(report.completed, 30)
        self.assertEqual(report.undrained_workers, 0)

    def test_inflight_proved_response_retained_after_sibling_failure(self) -> None:
        first_started = threading.Event()

        def request(context: load.RequestContext) -> load.ResponseObservation:
            if context.index == 0:
                first_started.set()
                self.assertTrue(context.cancellation.wait(0.25))
                return observation()
            self.assertTrue(first_started.is_set())
            raise AssertionError("secret-value")

        report = self.failed(request, config(20, concurrency=2))
        self.assertEqual(report.started, 2)
        self.assertEqual(report.completed, 1)
        self.assertEqual(report.observations[0].index, 0)
        self.assertEqual(report.failure, "request_assertion")
        self.assertEqual(report.undrained_workers, 0)

    def test_async_callback_cancellation_is_failure(self) -> None:
        def request(_: load.RequestContext) -> load.ResponseObservation:
            raise CancelledError("secret-value")

        report = self.failed(request)
        self.assertEqual(report.failure, "request_cancelled")
        self.assertEqual(report.completed, 0)

    def test_callback_timeout_never_counts_success(self) -> None:
        def request(_: load.RequestContext) -> load.ResponseObservation:
            raise TimeoutError("secret-value")

        report = self.failed(request)
        self.assertEqual(report.failure, "request_timeout")
        self.assertEqual(report.completed, 0)

    def test_cancellation_drains_cooperative_inflight(self) -> None:
        cancellation = threading.Event()

        def request(context: load.RequestContext) -> load.ResponseObservation:
            cancellation.set()
            self.assertTrue(context.cancellation.wait(0.2))
            raise KeyboardInterrupt("secret-value")

        report = self.failed(request, cancel=cancellation)
        self.assertIn(report.failure, {"cancelled", "request_cancelled"})
        self.assertEqual(report.completed, 0)
        self.assertEqual(report.started, 1)
        self.assertEqual(report.undrained_workers, 0)

    def test_phase_deadline_preserves_partial_without_pass(self) -> None:
        report = self.failed(
            lambda _: observation(),
            config(20, max_duration_s=0.5, request_timeout_s=0.1),
        )
        self.assertEqual(report.failure, "phase_deadline")
        self.assertLess(report.completed, 20)
        self.assertGreater(report.completed, 0)
        self.assertLess(report.wall_s, 0.6)

    def test_uncooperative_callback_returns_bounded_failure_not_false_pass(
        self,
    ) -> None:
        release = threading.Event()

        def request(_: load.RequestContext) -> load.ResponseObservation:
            release.wait(2)
            return observation()

        try:
            report = self.failed(request, config(1, request_timeout_s=0.05))
            self.assertEqual(report.failure, "request_timeout")
            self.assertEqual(report.completed, 0)
            self.assertEqual(report.undrained_workers, 1)
            self.assertLess(report.wall_s, 0.3)
        finally:
            release.set()

    def test_early_cancellation_not_empty_pass(self) -> None:
        cancellation = threading.Event()
        cancellation.set()
        report = self.failed(lambda _: observation(), cancel=cancellation)
        self.assertEqual(report.started, 0)
        self.assertEqual(report.failure, "cancelled")


if __name__ == "__main__":
    unittest.main()
