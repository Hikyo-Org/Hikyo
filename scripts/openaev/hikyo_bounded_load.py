"""Paced lab requests, with explicit proofs and bounded cooperative callbacks.

This module performs no networking. Request callbacks must enforce the supplied
timeout, validate response bodies, and honor cancellation before another request.
Python cannot forcibly terminate a callback that violates that contract. Such a
callback fails the phase and appears as an undrained worker, never as a success.
"""

from __future__ import annotations

import math
import re
import threading
import time
from asyncio import CancelledError
from collections import Counter
from collections.abc import Callable
from concurrent.futures import Future, ThreadPoolExecutor, wait
from dataclasses import dataclass

MAX_CONCURRENCY = 8
MAX_RATE = 12.0
MAX_REQUESTS = 160
MAX_DURATION = 45.0
MAX_REQUEST_TIMEOUT = 3.0
HEALTH_INTERVAL = 1.0
_LABEL = re.compile(r"[a-z][a-z0-9-]{0,63}\Z")
_STATUSES = frozenset({200, 401, 404, 429})


def _number(value: float, maximum: float) -> bool:
    return type(value) in (int, float) and math.isfinite(value) and 0 < value <= maximum


@dataclass(frozen=True)
class RequestCase:
    name: str
    expected_statuses: frozenset[int]

    def __post_init__(self) -> None:
        if not isinstance(self.name, str) or not _LABEL.fullmatch(self.name):
            raise ValueError("invalid case label")
        if (
            type(self.expected_statuses) is not frozenset
            or not self.expected_statuses
            or any(type(status) is not int for status in self.expected_statuses)
            or not self.expected_statuses <= _STATUSES
        ):
            raise ValueError("invalid expected statuses")


@dataclass(frozen=True)
class PhaseConfig:
    name: str
    cases: tuple[RequestCase, ...]
    request_count: int
    concurrency: int = 4
    rate_per_second: float = 12.0
    max_duration_s: float = 45.0
    request_timeout_s: float = 3.0

    def __post_init__(self) -> None:
        if not isinstance(self.name, str) or not _LABEL.fullmatch(self.name):
            raise ValueError("invalid phase label")
        if (
            type(self.cases) is not tuple
            or not self.cases
            or any(not isinstance(case, RequestCase) for case in self.cases)
            or len({case.name for case in self.cases}) != len(self.cases)
        ):
            raise ValueError("invalid phase cases")
        for value, maximum in (
            (self.request_count, MAX_REQUESTS),
            (self.concurrency, MAX_CONCURRENCY),
        ):
            if type(value) is not int or not 0 < value <= maximum:
                raise ValueError("invalid integer phase bound")
        if self.request_count < len(self.cases):
            raise ValueError("phase must exercise every configured case")
        if (
            not _number(self.rate_per_second, MAX_RATE)
            or not _number(self.max_duration_s, MAX_DURATION)
            or not _number(self.request_timeout_s, MAX_REQUEST_TIMEOUT)
        ):
            raise ValueError("invalid numeric phase bound")


@dataclass(frozen=True)
class RequestContext:
    index: int
    case: RequestCase
    timeout_s: float
    deadline: float
    cancellation: threading.Event


@dataclass(frozen=True)
class ResponseObservation:
    status: int
    elapsed_ms: float
    response_bytes: int
    body_verified: bool
    secret_absent: bool | None = None
    native_retry_after: float | None = None


@dataclass(frozen=True)
class CompletedRequest:
    index: int
    case: str
    observation: ResponseObservation
    scheduling_lateness_ms: float


@dataclass(frozen=True)
class LatencySummary:
    count: int
    p50_ms: float | None
    p95_ms: float | None
    p99_ms: float | None
    max_ms: float | None


def latency_summary(values: tuple[float, ...]) -> LatencySummary:
    """Nearest-rank percentiles; no interpolation or empty-sample zeroes."""
    if any(
        type(value) not in (int, float) or not math.isfinite(value) or value < 0
        for value in values
    ):
        raise ValueError("invalid latency sample")
    ordered = sorted(values)
    if not ordered:
        return LatencySummary(0, None, None, None, None)
    return LatencySummary(
        len(ordered),
        ordered[math.ceil(0.50 * len(ordered)) - 1],
        ordered[math.ceil(0.95 * len(ordered)) - 1],
        ordered[math.ceil(0.99 * len(ordered)) - 1],
        ordered[-1],
    )


@dataclass(frozen=True)
class PhaseReport:
    name: str
    requested: int
    started: int
    completed: int
    cancelled_before_start: int
    undrained_workers: int
    wall_s: float
    service_window_s: float
    completion_rate_per_second: float
    service_throughput_per_second: float
    status_counts: tuple[tuple[int, int], ...]
    case_status_counts: tuple[tuple[str, int, int], ...]
    response_bytes: int
    latency: LatencySummary
    latency_by_status: tuple[tuple[int, LatencySummary], ...]
    scheduling_lateness: LatencySummary
    health_checks: int
    failure: str | None
    failure_index: int | None
    failure_case: str | None
    observations: tuple[CompletedRequest, ...]

    @property
    def succeeded(self) -> bool:
        return (
            self.failure is None
            and self.completed == self.requested
            and self.completed > 0
            and self.undrained_workers == 0
            and self.health_checks >= 2
        )


class PhaseFailure(RuntimeError):
    def __init__(self, report: PhaseReport):
        self.report = report
        super().__init__(f"bounded phase failed: {report.failure}")


def _failure_kind(error: BaseException, prefix: str) -> str:
    if isinstance(error, AssertionError):
        return prefix + "_assertion"
    if isinstance(error, TimeoutError):
        return prefix + "_timeout"
    if isinstance(error, (KeyboardInterrupt, SystemExit, CancelledError)):
        return prefix + "_cancelled"
    return prefix + "_exception"


def _validate(observation: ResponseObservation, case: RequestCase) -> None:
    if not isinstance(observation, ResponseObservation):
        raise TypeError("invalid observation")
    if (
        type(observation.status) is not int
        or observation.status not in case.expected_statuses
    ):
        raise ValueError("unexpected status")
    if observation.body_verified is not True:
        raise ValueError("missing independent body proof")
    if observation.status != 200 and observation.secret_absent is not True:
        raise ValueError("missing withheld-secret proof")
    if (
        observation.secret_absent is not None
        and type(observation.secret_absent) is not bool
    ):
        raise ValueError("invalid secret proof")
    if not _number(observation.elapsed_ms, 3000.0):
        raise ValueError("invalid response timing")
    if (
        type(observation.response_bytes) is not int
        or not 0 <= observation.response_bytes <= 2 * 1024 * 1024
    ):
        raise ValueError("invalid response size")
    if observation.native_retry_after is not None and (
        observation.status != 429 or not _number(observation.native_retry_after, 3600.0)
    ):
        raise ValueError("invalid native retry metadata")


class _State:
    def __init__(self) -> None:
        self.lock = threading.Lock()
        self.stop = threading.Event()
        self.failure: str | None = None
        self.failure_index: int | None = None
        self.failure_case: str | None = None
        self.completed: list[CompletedRequest] = []
        self.active: dict[int, tuple[float, float]] = {}
        self.started = 0
        self.cancelled = 0
        self.first_start: float | None = None
        self.last_finish: float | None = None
        self.next_start = 0.0
        self.next_index = 0
        self.health_checks = 0

    def fail(
        self, reason: str, index: int | None = None, case: str | None = None
    ) -> None:
        with self.lock:
            if self.failure is None:
                self.failure = reason
                self.failure_index = index
                self.failure_case = case
            self.stop.set()


def run_phase(
    config: PhaseConfig,
    request: Callable[[RequestContext], ResponseObservation],
    health_guard: Callable[[], None],
    *,
    cancellation: threading.Event | None = None,
) -> PhaseReport:
    """Run exactly the configured requests, or fail with retained safe evidence.

    Cases are selected round-robin. Health checks are distinct from workload
    requests and counted separately. Successful 429 responses represent native
    rate-limit refusal, not a successful authorized delivery. All timings and
    rates describe this phase's aggregate workload, never individual endpoints.
    """
    if (
        not isinstance(config, PhaseConfig)
        or not callable(request)
        or not callable(health_guard)
    ):
        raise TypeError("invalid runner arguments")
    state = _State()
    began = time.monotonic()
    deadline = began + config.max_duration_s
    health_timeout = min(1.0, config.max_duration_s / 4.0)
    executor = ThreadPoolExecutor(max_workers=config.concurrency + 1)
    pending: list[Future[None]] = []
    health_futures: list[Future[None]] = []
    health_due = began

    def health() -> None:
        try:
            health_guard()
        except BaseException as error:
            state.fail(_failure_kind(error, "health"))
            raise
        else:
            with state.lock:
                state.health_checks += 1

    def check_health() -> None:
        nonlocal health_due
        health_started = time.monotonic()
        if health_started >= deadline:
            state.fail("phase_deadline")
            return
        future = executor.submit(health)
        health_futures.append(future)
        wait(
            (future,),
            timeout=max(0.0, min(health_timeout, deadline - time.monotonic())),
        )
        if not future.done():
            state.fail("health_timeout")
        health_due = health_started + HEALTH_INTERVAL
        if time.monotonic() >= deadline:
            state.fail("phase_deadline")

    def worker(index: int) -> None:
        case = config.cases[index % len(config.cases)]
        while True:
            with state.lock:
                now = time.monotonic()
                if state.stop.is_set() or (
                    cancellation is not None and cancellation.is_set()
                ):
                    state.cancelled += 1
                    return
                wait = state.next_start - now
                if index != state.next_index:
                    wait = max(wait, 0.005)
                if wait <= 0:
                    timeout = min(
                        config.request_timeout_s, deadline - now - health_timeout
                    )
                    if timeout <= 0:
                        state.cancelled += 1
                        return
                    state.next_start = now + 1.0 / config.rate_per_second
                    state.next_index += 1
                    state.started += 1
                    state.first_start = state.first_start or now
                    state.active[index] = (now, timeout)
                    lateness = max(
                        0.0, (now - began - index / config.rate_per_second) * 1000.0
                    )
                    context = RequestContext(index, case, timeout, deadline, state.stop)
                    break
            state.stop.wait(min(wait, 0.01))
        try:
            observation = request(context)
            _validate(observation, case)
            finished = time.monotonic()
            if finished - now > timeout:
                state.fail("request_timeout", index, case.name)
            elif finished >= deadline:
                state.fail("phase_deadline", index, case.name)
            else:
                with state.lock:
                    state.completed.append(
                        CompletedRequest(index, case.name, observation, lateness)
                    )
                    state.last_finish = finished
        except BaseException as error:
            state.fail(_failure_kind(error, "request"), index, case.name)
            raise
        finally:
            with state.lock:
                state.active.pop(index, None)

    try:
        if cancellation is not None and cancellation.is_set():
            state.fail("cancelled")
        else:
            check_health()
        submitted = 0
        while not state.stop.is_set():
            now = time.monotonic()
            if cancellation is not None and cancellation.is_set():
                state.fail("cancelled")
                break
            if now >= deadline - health_timeout:
                state.fail("phase_deadline")
                break
            with state.lock:
                timed_out = any(
                    now - start > timeout for start, timeout in state.active.values()
                )
            if timed_out:
                state.fail("request_timeout")
                break
            pending = [future for future in pending if not future.done()]
            if submitted == config.request_count and not pending:
                break
            if now >= health_due:
                check_health()
                continue
            if submitted < config.request_count and len(pending) < config.concurrency:
                with state.lock:
                    if not state.stop.is_set():
                        pending.append(executor.submit(worker, submitted))
                        submitted += 1
            state.stop.wait(0.005)
        if state.stop.is_set():
            for future in pending:
                if future.cancel():
                    with state.lock:
                        state.cancelled += 1
        drain_deadline = min(
            deadline - health_timeout, time.monotonic() + config.request_timeout_s
        )
        while (
            any(not future.done() for future in pending)
            and time.monotonic() < drain_deadline
        ):
            if time.monotonic() >= health_due:
                check_health()
            time.sleep(0.005)
        if any(not future.done() for future in pending):
            state.fail("undrained_workers")
        # A final guard is required even after request failure when time permits.
        check_health()
    except (KeyboardInterrupt, SystemExit, CancelledError) as error:
        state.fail(_failure_kind(error, "runner"))
    finally:
        state.stop.set()
        executor.shutdown(wait=False, cancel_futures=True)

    ended = time.monotonic()
    with state.lock:
        observations = tuple(sorted(state.completed, key=lambda row: row.index))
        counts = Counter(row.observation.status for row in observations)
        case_counts = Counter(
            (row.case, row.observation.status) for row in observations
        )
        window = (
            state.last_finish - state.first_start
            if state.last_finish is not None and state.first_start is not None
            else 0.0
        )
        undrained = sum(not future.done() for future in pending + health_futures)
        if state.failure is None and (
            len(observations) != config.request_count or undrained
        ):
            state.failure = "incomplete_phase"
        wall = ended - began
        report = PhaseReport(
            name=config.name,
            requested=config.request_count,
            started=state.started,
            completed=len(observations),
            cancelled_before_start=state.cancelled,
            undrained_workers=undrained,
            wall_s=wall,
            service_window_s=window,
            completion_rate_per_second=len(observations) / wall,
            service_throughput_per_second=len(observations) / window
            if window > 0
            else 0.0,
            status_counts=tuple(sorted(counts.items())),
            case_status_counts=tuple(
                (case, status, count)
                for (case, status), count in sorted(case_counts.items())
            ),
            response_bytes=sum(row.observation.response_bytes for row in observations),
            latency=latency_summary(
                tuple(row.observation.elapsed_ms for row in observations)
            ),
            latency_by_status=tuple(
                (
                    status,
                    latency_summary(
                        tuple(
                            row.observation.elapsed_ms
                            for row in observations
                            if row.observation.status == status
                        )
                    ),
                )
                for status in sorted(counts)
            ),
            scheduling_lateness=latency_summary(
                tuple(row.scheduling_lateness_ms for row in observations)
            ),
            health_checks=state.health_checks,
            failure=state.failure,
            failure_index=state.failure_index,
            failure_case=state.failure_case,
            observations=observations,
        )
    if not report.succeeded:
        raise PhaseFailure(report) from None
    return report
