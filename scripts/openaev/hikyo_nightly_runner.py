"""Run all native OpenAEV checks on an isolated hosted runner, without publishing."""
from __future__ import annotations

import argparse
import json
import math
import os
import signal
import ssl
import stat
import subprocess
import sys
import time
import urllib.error
import urllib.request
import uuid
from dataclasses import asdict
from pathlib import Path

import hikyo_detection_runtime as monitoring_runtime
import hikyo_lab_guard as health
import hikyo_nightly_candidate as preparation
import hikyo_nightly_lifecycle as lifecycle
import hikyo_nightly_release as release
from suite_identity import source_fingerprint

HERE = Path(__file__).resolve().parent
ROOT = HERE.parents[1]
CA = Path(os.environ.get("OPENAEV_CA_FILE", "/missing-runner-ca"))
KUBE = lifecycle.KUBE
LEASE_SECONDS = 7200


SECURITY = (
    "opt-in-off-presence",
    "read-config-secret-presence",
    "read-and-reveal",
    "config-only-projection",
    "conditional-current",
    "wrong-environment",
    "wrong-project",
    "wrong-tenant",
    "expired-token",
    "missing-token",
    "tampered-token",
    "workload-human-plaintext-reveal",
    "self-grant-reveal",
    "create-service-account",
    "mint-own-credential",
    "create-project",
    "write-without-edit",
    "publish-without-publish",
    "withdraw-machine-opt-in",
    "remove-reveal-grant",
    "remove-read-grant",
    "cursor-after-read-removal",
    "revoked-token",
    "cursor-after-token-revocation",
)

ADVERSARIAL = (
    "whack-cursor-sql",
    "whack-cursor-xss",
    "whack-cursor-traversal",
    "whack-cursor-unicode",
    "whack-cursor-boundary-128",
    "whack-cursor-boundary-129",
    "whack-cursor-8k",
    "whack-cursor-projection-binding",
    "whack-cursor-outside-scope",
    "whack-long-workload-token",
    "whack-tampered-workload-token",
    "whack-hostile-scalar-write",
    "whack-8k-scalar-write",
    "whack-oversized-scalar-write",
    "whack-structured-value",
    "whack-deep-json-value",
    "whack-value-mass-assignment",
    "whack-grant-mass-assignment",
    "whack-publish-mass-assignment",
    "whack-publish-bypass-attempt",
    "whack-after-read-removal",
    "whack-after-credential-revocation",
)

class RunnerError(RuntimeError):
    """Literal diagnostics, no subprocess output or response bodies."""

# Closed diagnostic codes only; subprocess output, exception text and custody
# never reach a public workflow log.
PUBLIC_ERROR_CODES = frozenset({
    'artifact_directory_has_foreign_files',
    'artifact_directory_requires_owner_only_permissions',
    'artifact_directory_symlink',
    'background_collection_failed',
    'background_collection_stale',
    'background_guard_already_finished',
    'background_guard_already_started',
    'background_guard_has_no_sample',
    'background_memory_provider_mismatch',
    'background_memory_samples_missing',
    'background_worker_did_not_stop',
    'bootstrap_instance_missing',
    'bound_volume_changed_since_journal',
    'bound_volume_missing',
    'bound_volume_ownership_mismatch',
    'bound_volume_uid_changed',
    'bounded_lab_command_failed',
    'bounded_lab_priority_required',
    'bounded_resource_wait_failed',
    'bounded_single_server_required',
    'bundle_archive_size_bound_exceeded',
    'bundle_changed_while_reading',
    'bundle_file_read_failed',
    'bundle_index_missing',
    'bundle_inventory_bound',
    'bundle_member_changed',
    'bundle_path_invalid',
    'bundle_regular_files_only',
    'bundle_size_bound',
    'bundle_size_bound_exceeded',
    'bundle_symlink_forbidden',
    'candidate_command_output_bound',
    'candidate_command_refused',
    'candidate_command_unavailable',
    'cluster_read_failed',
    'container_identity_missing',
    'container_metrics_identity_mismatch',
    'container_metrics_incomplete',
    'container_metrics_missing',
    'container_status_identity_missing',
    'deployment_disappeared',
    'docker_command_failed',
    'docker_command_unavailable',
    'docker_daemon_identity_invalid',
    'docker_environment_override_refused',
    'docker_native_unix_socket_required',
    'docker_response_too_large',
    'docker_socket_owner_invalid',
    'duplicate_case_evidence',
    'duplicate_container_metrics',
    'duplicate_load_observation',
    'duplicate_pod_identity',
    'duplicate_pod_metrics',
    'exercise_command_failed',
    'fixed_lab_pod_label_required',
    'fixed_lab_selector_required',
    'foreign_lab_child_owner',
    'foreign_lab_init_container',
    'foreign_lab_pod',
    'foreign_lab_replicaset',
    'foreign_lab_server_container',
    'foreign_lab_volume',
    'foreign_lab_volume_source',
    'foreign_or_existing_lab_resource',
    'fresh_candidate_state_required',
    'fresh_pass_attestation_required',
    'fresh_runner_state_required',
    'hikyo_not_removed_at_idle_boundary',
    'hosted_suite_not_complete_pass',
    'idle_cluster_baseline_changed',
    'image_native_binary_mismatch',
    'image_signed_manifest_digest_mismatch',
    'immutable_release_required',
    'invalid_background_guard_cadence',
    'invalid_bundle_directory',
    'invalid_bundle_input',
    'invalid_bundle_member',
    'invalid_burst_evidence',
    'invalid_candidate_field',
    'invalid_candidate_object',
    'invalid_case_evidence',
    'invalid_cluster_list',
    'invalid_cluster_object',
    'invalid_container_inspection',
    'invalid_evidence_object',
    'invalid_expected_lab_pod_count',
    'invalid_image_binary',
    'invalid_journal_volume_name',
    'invalid_lab_baseline',
    'invalid_lab_container_template',
    'invalid_lab_deployment_template',
    'invalid_lab_tunnel',
    'invalid_lifecycle_lease',
    'invalid_lifecycle_resource_journal',
    'invalid_load_evidence',
    'invalid_load_health',
    'invalid_load_observation',
    'invalid_load_phase',
    'invalid_load_phases',
    'invalid_memory_quantity',
    'invalid_memory_sample',
    'invalid_native_binary',
    'invalid_native_digest',
    'invalid_native_image_digest',
    'invalid_native_json',
    'invalid_native_object',
    'invalid_network_inspection',
    'invalid_node_name',
    'invalid_or_existing_lifecycle_run',
    'invalid_prepared_bundle_journal',
    'invalid_relay_container',
    'invalid_release_identity_fields',
    'invalid_resource_list',
    'invalid_resource_uid',
    'invalid_run_id',
    'invalid_runtime_endpoints_or_lease',
    'invalid_verified_candidate_identity',
    'isolated_single_node_runner_required',
    'lab_child_uid_changed',
    'lab_children_or_foreign_workloads_remain',
    'lab_container_security_boundary_invalid',
    'lab_container_security_missing',
    'lab_initializers_missing',
    'lab_memory_pressure',
    'lab_pod_host_or_identity_boundary_invalid',
    'lab_pod_identity_or_restarts_changed',
    'lab_pod_security_boundary_invalid',
    'lab_resources_not_released',
    'lab_target_not_idle',
    'lab_volumes_missing',
    'lifecycle_journal_owner_mismatch',
    'lifecycle_run_lease_expired',
    'load_case_mismatch',
    'load_completion_unverified',
    'load_count_mismatch',
    'load_response_unverified',
    'local_docker_required',
    'metrics_pod_identity_mismatch',
    'metrics_pod_identity_missing',
    'metrics_stale',
    'metrics_timestamp_invalid',
    'metrics_timestamp_missing',
    'monitor_image_preparation_failed',
    'namespace_has_no_ready_pods',
    'native_arm64_runner_required',
    'native_artifact_inventory_missing',
    'native_binary_changed',
    'native_bundle_transfer_digest_mismatch',
    'native_candidate_identity_mismatch',
    'native_container_boundary_failed',
    'native_internal_network_boundary_failed',
    'native_monitoring_readiness_timeout',
    'native_output_path_boundary',
    'native_read_query_rejected',
    'native_read_route_rejected',
    'native_resource_ownership_mismatch',
    'native_resource_uid_changed',
    'native_throttle_unverified',
    'native_throttling_recovery_unverified',
    'network_isolation_unverified',
    'nightly_cancelled',
    'nightly_deadline_reached',
    'openaev_baseline_changed',
    'owned_resource_cleanup_incomplete',
    'pod_identity_missing',
    'pod_metrics_incomplete',
    'pod_not_ready',
    'pod_terminating',
    'prepared_bundle_identity_missing',
    'prepared_run_required',
    'preparer_disappeared',
    'preparer_must_be_removed_before_server',
    'preparer_terminated',
    'private_lab_tunnel_unavailable',
    'private_platform_token_required',
    'relay_read_route_rejected',
    'release_image_digest_binding_mismatch',
    'release_input_binding_mismatch',
    'resource_outside_fixed_lab_allowlist',
    'results_candidate_binding_mismatch',
    'runner_cluster_identity_changed',
    'runner_cluster_identity_missing',
    'runner_stack_environment_required',
    'runtime_already_started',
    'runtime_create_intent_missing',
    'runtime_create_native_id_invalid',
    'runtime_journal_already_exists',
    'runtime_journal_directory_permissions',
    'runtime_journal_directory_symlink',
    'runtime_journal_permissions_or_size',
    'runtime_journal_required_before_create',
    'runtime_journal_resource_binding_invalid',
    'runtime_journal_resources_invalid',
    'runtime_journal_schema_invalid',
    'runtime_journal_unavailable',
    'runtime_native_id_changed',
    'runtime_native_identity_mismatch',
    'runtime_native_inspect_invalid',
    'runtime_object_invalid',
    'runtime_recovery_daemon_changed',
    'runtime_recovery_resource_remaining',
    'runtime_uuid_invalid',
    'signed_candidate_image_identity_mismatch',
    'signed_candidate_image_inventory_missing_or_ambiguous',
    'signed_index_platforms_missing',
    'signed_multiarch_index_required',
    'summary_node_identity_mismatch',
    'trusted_workflow_checkout_required',
    'unexpected_case_evidence',
    'unexpected_lab_pod_count',
    'unexpected_load_phase',
    'unexpected_local_path_volume_name',
    'unpinned_arm64_lab_required',
    'unsafe_cleanup_proof_path',
    'unsafe_evidence_file',
    'unsafe_evidence_permissions',
    'unsafe_lifecycle_journal',
    'unsafe_lifecycle_state',
    'unsafe_runner_directory',
    'unsupported_memory_quantity',
    'verified_bundle_tree_changed',
})


def public_error_code(error: BaseException) -> str:
    message = str(error)
    return message if message in PUBLIC_ERROR_CODES else "unexpected_failure"

def private_directory(path: Path) -> None:
    if path.is_symlink():
        raise RunnerError("unsafe_runner_directory")
    path.mkdir(mode=0o700, parents=True, exist_ok=True)
    native = path.stat()
    if native.st_uid != os.getuid() or stat.S_IMODE(native.st_mode) != 0o700:
        raise RunnerError("unsafe_runner_directory")

def read_json(path: Path, *, private: bool = True) -> dict[str, object]:
    if path.is_symlink() or not path.is_file() or path.stat().st_size > 8 << 20:
        raise RunnerError("unsafe_evidence_file")
    native = path.stat()
    if native.st_uid != os.getuid() or (
        private and stat.S_IMODE(native.st_mode) != 0o600
    ):
        raise RunnerError("unsafe_evidence_permissions")
    value = json.loads(path.read_bytes())
    if not isinstance(value, dict) or any(not isinstance(k, str) for k in value):
        raise RunnerError("invalid_evidence_object")
    return dict(value)

def save(path: Path, value: object) -> None:
    encoded = json.dumps(value, indent=2).encode()
    temporary = path.with_name(path.name + ".writing")
    fd = os.open(temporary, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    try:
        with os.fdopen(fd, "wb") as output:
            output.write(encoded)
            output.flush()
            os.fsync(output.fileno())
        os.replace(temporary, path)
    finally:
        temporary.unlink(missing_ok=True)

def run_command(arguments: list[str], logfile: Path, timeout: float) -> None:
    if timeout <= 0:
        raise RunnerError("nightly_deadline_reached")
    fd = os.open(logfile, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    with os.fdopen(fd, "wb") as output:
        process = subprocess.Popen(
            arguments,
            cwd=ROOT,
            stdin=subprocess.DEVNULL,
            stdout=output,
            stderr=subprocess.STDOUT,
            start_new_session=True,
        )
        try:
            status = process.wait(timeout=timeout)
        except BaseException:
            if process.poll() is None:
                os.killpg(process.pid, signal.SIGTERM)
            try:
                process.wait(timeout=10)
            except subprocess.TimeoutExpired:
                os.killpg(process.pid, signal.SIGKILL)
                process.wait(timeout=10)
            raise
    if status:
        raise RunnerError("exercise_command_failed")

class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(
        self, req: object, fp: object, code: int, msg: str, headers: object, newurl: str
    ) -> None:
        return None

def _status(url: str) -> int:
    opener = urllib.request.build_opener(
        urllib.request.ProxyHandler({}),
        NoRedirect(),
        urllib.request.HTTPSHandler(context=ssl.create_default_context(cafile=str(CA))),
    )
    try:
        with opener.open(url, timeout=3) as response:
            return response.status
    except (urllib.error.URLError, OSError):
        return 0

def tunnel(
    service: str, port: int, target: int, state: Path
) -> subprocess.Popen[bytes] | None:
    if (service, port, target) not in (
        ("openaev", 18080, 8080),
        ("hikyo", 18081, 8443),
    ):
        raise RunnerError("invalid_lab_tunnel")
    path = "/api/v1/meta" if service == "hikyo" else "/"
    url = f"https://127.0.0.1:{port}{path}"
    if _status(url) == 200:
        return None
    namespace = "hikyo-lab" if service == "hikyo" else "openaev-lab"
    fd = os.open(
        state / f"{service}-tunnel.log", os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600
    )
    with os.fdopen(fd, "wb") as output:
        process = subprocess.Popen(
            [
                *KUBE,
                "-n",
                namespace,
                "port-forward",
                "--address",
                "127.0.0.1",
                "service/" + service,
                f"{port}:{target}",
            ],
            stdin=subprocess.DEVNULL,
            stdout=output,
            stderr=subprocess.STDOUT,
            start_new_session=True,
        )
    for _ in range(20):
        if _status(url) == 200:
            return process
        if process.poll() is not None:
            break
        time.sleep(1)
    stop_tunnel(process)
    raise RunnerError("private_lab_tunnel_unavailable")

def stop_tunnel(process: subprocess.Popen[bytes] | None) -> None:
    if process is not None and process.poll() is None:
        process.terminate()
        try:
            process.wait(timeout=5)
        except subprocess.TimeoutExpired:
            process.kill()
            process.wait(timeout=5)

def unchanged_idle(before: dict[str, object], after: dict[str, object]) -> None:
    for field in ("environment", "lab_pods"):
        if before[field] != after[field]:
            raise RunnerError("idle_cluster_baseline_changed")

def active_baseline(before: dict[str, object], active: dict[str, object]) -> None:
    if before["environment"] != active["environment"]:
        raise RunnerError("runner_cluster_identity_changed")
    old, new = before["lab_pods"], active["lab_pods"]
    if not isinstance(old, dict) or not isinstance(new, dict):
        raise RunnerError("invalid_lab_baseline")
    if len(new) != len(old) + 1 or any(new.get(k) != v for k, v in old.items()):
        raise RunnerError("openaev_baseline_changed")

def atomic_results(
    name: str, planned: tuple[str, ...], report: dict[str, object] | None
) -> release.SuiteResult:
    rows = [] if report is None else report.get("cases", [])
    if not isinstance(rows, list):
        raise RunnerError("invalid_case_evidence")
    indexed: dict[str, dict[str, object]] = {}
    for row in rows:
        if not isinstance(row, dict) or row.get("case") not in planned:
            raise RunnerError("unexpected_case_evidence")
        case = row["case"]
        if not isinstance(case, str) or case in indexed:
            raise RunnerError("duplicate_case_evidence")
        indexed[case] = dict(row)
    tests = []
    for case in planned:
        row = indexed.get(case)
        if row is None:
            tests.append(release.TestResult(case, "skipped", False))
            continue
        measured = (
            type(row.get("direct_status")) is int
            and row.get("execution_recorded") is True
            and isinstance(row.get("inject_id"), str)
        )
        passed = (
            measured
            and row.get("passed") is True
            and row.get("domain_assertion_passed") is True
            and row.get("direct_status") == row.get("expected_status")
        )
        # An interrupted platform dispatch is infrastructure, not proof of a
        # failed security assertion. Only completed native evidence is counted.
        status = "passed" if passed else "failed" if measured else "infra_error"
        tests.append(release.TestResult(case, status, measured))
    return release.SuiteResult(name, tuple(tests))

PHASES = (
    (
        "mixed-authorization",
        40,
        (
            ("authorized", (200,)),
            ("tampered", (401,)),
            ("outside", (404,)),
            ("opaque-cursor", (200,)),
        ),
    ),
    ("native-principal-throttling", 100, (("authorized-burst", (200, 429)),)),
    ("denials-after-throttling", 40, (("tampered", (401,)), ("outside", (404,)))),
    (
        "post-read-grant-removal",
        40,
        (("removed-grant", (404,)), ("removed-grant-cursor", (404,))),
    ),
    (
        "post-credential-revocation",
        40,
        (("revoked", (401,)), ("revoked-cursor", (401,))),
    ),
)

def load_results(report: dict[str, object] | None) -> release.SuiteResult:
    """Count only independently validated per-request native observations."""
    phases = [] if report is None else report.get("load_phases", [])
    if not isinstance(phases, list):
        raise RunnerError("invalid_load_evidence")
    indexed: dict[str, dict[str, object]] = {}
    for phase in phases:
        if not isinstance(phase, dict) or not isinstance(phase.get("name"), str):
            raise RunnerError("invalid_load_phase")
        name = phase["name"]
        if name in indexed or name not in {row[0] for row in PHASES}:
            raise RunnerError("unexpected_load_phase")
        indexed[name] = dict(phase)
    tests: list[release.TestResult] = []
    for name, count, cycle in PHASES:
        phase = indexed.get(name)
        observations: dict[int, dict[str, object]] = {}
        if phase is not None:
            rows = phase.get("observations")
            if (
                not isinstance(rows, list)
                or phase.get("requested") != count
                or phase.get("completed") != len(rows)
            ):
                raise RunnerError("load_count_mismatch")
            for row in rows:
                if not isinstance(row, dict) or type(row.get("index")) is not int:
                    raise RunnerError("invalid_load_observation")
                index = row["index"]
                if index in observations or not 0 <= index < count:
                    raise RunnerError("duplicate_load_observation")
                case, statuses = cycle[index % len(cycle)]
                observation = row.get("observation")
                if not isinstance(observation, dict) or row.get("case") != case:
                    raise RunnerError("load_case_mismatch")
                elapsed, size = (
                    observation.get("elapsed_ms"),
                    observation.get("response_bytes"),
                )
                if (
                    type(observation.get("status")) is not int
                    or observation.get("status") not in statuses
                    or observation.get("body_verified") is not True
                    or observation.get("secret_absent") is not True
                    or type(elapsed) not in (int, float)
                    or not isinstance(elapsed, (int, float))
                    or not math.isfinite(elapsed)
                    or not 0 <= elapsed <= 3000
                    or type(size) is not int
                    or not 0 <= size <= 2 * 1024 * 1024
                ):
                    raise RunnerError("load_response_unverified")
                if observation["status"] == 429:
                    retry = observation.get("native_retry_after")
                    if (
                        type(retry) not in (int, float)
                        or not isinstance(retry, (int, float))
                        or not 0 < retry <= 30
                    ):
                        raise RunnerError("native_throttle_unverified")
                observations[index] = dict(observation)
            health_checks = phase.get("health_checks")
            if phase.get("failure") is None and (
                len(rows) != count
                or phase.get("undrained_workers") != 0
                or type(health_checks) is not int
                or not isinstance(health_checks, int)
                or health_checks < 2
            ):
                raise RunnerError("load_completion_unverified")
        for index in range(count):
            tests.append(
                release.TestResult(
                    name + "-" + str(index),
                    "passed" if index in observations else "skipped",
                    index in observations,
                )
            )
    if len(phases) == len(PHASES) and all(
        phase.get("failure") is None for phase in indexed.values()
    ):
        burst = indexed["native-principal-throttling"]["observations"]
        if not isinstance(burst, list):
            raise RunnerError("invalid_burst_evidence")
        burst_statuses = [row["observation"]["status"] for row in burst]
        recovery = None if report is None else report.get("throttling_recovery")
        if (
            not isinstance(recovery, dict)
            or burst_statuses.count(429) == 0
            or burst_statuses.count(200) == 0
            or recovery.get("observed_429") != burst_statuses.count(429)
            or recovery.get("same_principal_status") != 200
            or recovery.get("other_principal_control") != 200
        ):
            raise RunnerError("native_throttling_recovery_unverified")
    return release.SuiteResult("Bounded requests", tuple(tests))

def verify_isolation(path: Path) -> None:
    report = read_json(path)
    checks = report.get("checks")
    expected = {
        "lab-rabbitmq",
        "lab-hikyo",
        "forbidden-canary-service",
        "forbidden-canary-pod",
        "forbidden-canary-ingress",
        "kubernetes-api",
        "internet",
        "node-kubernetes-api",
        "runner-metadata-canary",
        "lab-cluster-dns",
        "forbidden-external-dns",
    }
    if (
        report.get("pass") is not True
        or report.get("forbidden_destination_control") != "all_connected"
        or not isinstance(checks, list)
        or report.get("dns_positive_control") != "cluster_resolved"
        or len(checks) != 11
        or any(
            not isinstance(row, dict) or row.get("pass") is not True for row in checks
        )
        or {row.get("target") for row in checks} != expected
    ):
        raise RunnerError("network_isolation_unverified")

def load_control_results(report: dict[str, object] | None) -> release.SuiteResult:
    """Keep resource safety and throttle recovery distinct from request counts."""
    raw_phases = [] if report is None else report.get("load_phases", [])
    if not isinstance(raw_phases, list):
        raise RunnerError("invalid_load_phases")
    phases: dict[str, dict[str, object]] = {}
    for row in raw_phases:
        if (
            not isinstance(row, dict)
            or not isinstance(row.get("name"), str)
            or row["name"] in phases
        ):
            raise RunnerError("invalid_load_phases")
        phases[row["name"]] = dict(row)
    evidence = [] if report is None else report.get("load_health", [])
    if not isinstance(evidence, list):
        raise RunnerError("invalid_load_health")
    indexed: dict[str, dict[str, object]] = {}
    for row in evidence:
        if (
            not isinstance(row, dict)
            or not isinstance(row.get("phase"), str)
            or row["phase"] in indexed
        ):
            raise RunnerError("invalid_load_health")
        indexed[row["phase"]] = dict(row)
    tests = []
    for name, _, _ in PHASES:
        phase, native = phases.get(name), indexed.get(name)
        if phase is None:
            tests.append(release.TestResult(name, "skipped", False))
            continue
        native_checks = None if native is None else native.get("native_checks")
        phase_checks = phase.get("health_checks")
        good = (
            native is not None
            and native.get("worker_stopped") is True
            and native.get("failure") is None
            and isinstance(native_checks, int)
            and type(native_checks) is int
            and native_checks >= 2
            and native.get("max_collection_age_s") == 15.0
            and native.get("max_native_memory_age_s") == 30.0
            and isinstance(native.get("started_at"), str)
            and isinstance(native.get("finished_at"), str)
            and phase.get("failure") is None
            and phase.get("undrained_workers") == 0
            and phase.get("completed") == phase.get("requested")
            and type(phase_checks) is int
            and isinstance(phase_checks, int)
            and phase_checks >= 2
        )
        tests.append(
            release.TestResult(name, "passed" if good else "infra_error", good)
        )
    recovery = None if report is None else report.get("throttling_recovery")
    good_recovery = (
        isinstance(recovery, dict)
        and recovery.get("same_principal_status") == 200
        and recovery.get("other_principal_control") == 200
        and type(recovery.get("observed_429")) is int
        and recovery["observed_429"] > 0
    )
    tests.append(
        release.TestResult(
            "native-throttle-recovery",
            "passed"
            if good_recovery
            else "skipped"
            if recovery is None
            else "infra_error",
            good_recovery,
        )
    )
    return release.SuiteResult("Load safety", tuple(tests))

def detection_result(report: dict[str, object] | None) -> release.SuiteResult:
    proof = {} if report is None else report.get("independent_detection", {})
    complete = (
        isinstance(proof, dict)
        and proof.get("scope_actor_window_verified") is True
        and proof.get("rule_fired") is True
        and proof.get("notification_delivered") is True
        and proof.get("collector_scope_only_verified") is True
        and isinstance(proof.get("inbox_receipt_sha256"), str)
        and len(str(proof["inbox_receipt_sha256"])) == 64
        and report is not None
        and report.get("passed") is True
        and report.get("monitor_runtime_closed") is True
    )
    status = "passed" if complete else "skipped" if report is None else "infra_error"
    return release.SuiteResult(
        "Detection delivery", (release.TestResult("audit-to-inbox", status, complete),)
    )

def reports(
    state: Path, instance: str, version: str
) -> tuple[release.SuiteResult, ...]:
    loaded: list[dict[str, object] | None] = []
    for path in (
        state / "security/results.json",
        state / "adversarial/adversarial-results.json",
        state / "detection/fixture/detection-results.json",
    ):
        report = read_json(path) if path.exists() else None
        if report is not None and (
            report.get("instance_identity") != instance
            or report.get("release") != version
        ):
            raise RunnerError("results_candidate_binding_mismatch")
        loaded.append(report)
    return (
        atomic_results("Authorization", SECURITY, loaded[0]),
        atomic_results("Adversarial", ADVERSARIAL, loaded[1]),
        load_results(loaded[1]),
        load_control_results(loaded[1]),
        detection_result(loaded[2]),
    )

def _signal(number: int, frame: object) -> None:
    raise RunnerError("nightly_cancelled")




def execute(state: Path, *, release_json: Path, tag: str, source_sha: str,
            image_ref: str) -> dict[str, object]:
    """Fresh run only. Public PASS exists only after verified owned cleanup."""
    attestation = state.parent / "validated-pass.json"
    if attestation.exists() or attestation.is_symlink():
        raise RunnerError("fresh_pass_attestation_required")
    if state.exists() or state.is_symlink():
        raise RunnerError("fresh_runner_state_required")
    private_directory(state)
    if not os.environ.get("KUBECONFIG") or not CA.is_file():
        raise RunnerError("runner_stack_environment_required")
    document = read_json(release_json, private=False)
    release_id, published = document.get("id"), document.get("published_at")
    if type(release_id) is not int or not isinstance(release_id, int) or not isinstance(published, str):
        raise RunnerError("invalid_release_identity_fields")
    candidate = release.Candidate(release_id, tag, source_sha, published)
    if document.get("tag_name") != tag or document.get("draft") is not False:
        raise RunnerError("release_input_binding_mismatch")
    run_id = str(uuid.uuid4())
    save(state / "candidate.json", asdict(candidate))
    fingerprint = source_fingerprint()
    deadline = time.monotonic() + LEASE_SECONDS
    target = lifecycle.EphemeralHikyo(run_id, state / "lifecycle")
    tunnels: list[subprocess.Popen[bytes] | None] = []
    errors: list[str] = []
    cleanup_verified = False
    isolation_verified = False
    instance = ""
    version = candidate.tag.removeprefix("v")
    prepared: dict[str, object] = {}
    suites: tuple[release.SuiteResult, ...] = (atomic_results("Authorization", SECURITY, None),
              atomic_results("Adversarial", ADVERSARIAL, None), load_results(None),
              load_control_results(None), detection_result(None))
    before: dict[str, object] | None = None
    platform_token = os.environ.get("OPENAEV_PLATFORM_TOKEN_FILE")
    if not platform_token:
        raise RunnerError("private_platform_token_required")
    platform = ["--platform-url", "https://127.0.0.1:18080",
                "--platform-token-file", platform_token]

    def command(script: str, arguments: list[str], name: str, cap: int = 1800) -> None:
        print("Hosted stage: " + name, flush=True)
        run_command([sys.executable, str(HERE / script), *arguments], state / (name + ".log"),
                    min(cap, deadline - time.monotonic()))

    try:
        print("Hosted stage: prepare-monitor-images", flush=True)
        monitoring_runtime.prepare_images()
        before = health.collect(expected_lab_pods=6)
        save(state / "idle-before.json", before)
        print("Hosted stage: authenticate-native-candidate", flush=True)
        prepared = preparation.prepare_candidate(candidate, state / "candidate")
        if prepared.get("image_ref") != image_ref:
            raise RunnerError("release_image_digest_binding_mismatch")
        # Candidate authentication uses a read-only token. Exercise code has no
        # GitHub credential, and release writes belong to a separate success job.
        os.environ.pop("GH_TOKEN", None)
        os.environ.pop("GITHUB_TOKEN", None)
        bundle, bundle_hash = prepared.get("bundle_directory"), prepared.get("bundle_tree_sha256")
        if not isinstance(bundle, str) or not isinstance(bundle_hash, str):
            raise RunnerError("prepared_bundle_identity_missing")
        unchanged_idle(before, health.collect(expected_lab_pods=6))
        print("Hosted stage: start-authenticated-target", flush=True)
        target.prepare(Path(bundle), bundle_hash, lease_seconds=LEASE_SECONDS)
        target.start(read_json(HERE / "hikyo-nightly-deployment.json", private=False), image_ref,
                     expected_version=version, source_commit=candidate.source_sha)
        active = health.collect()
        active_baseline(before, active)
        save(state / "active-before.json", active)
        tunnels.append(tunnel("hikyo", 18081, 8443, state))
        tunnels.append(tunnel("openaev", 18080, 8080, state))
        command("hikyo-pilot.py", ["bootstrap", "--state", str(state / "pilot"),
                                 "--ca", str(CA), "--expected-version", version], "bootstrap", 600)
        pilot = state / "pilot/pilot.json"
        instance_value = read_json(pilot).get("instance_identity")
        if not isinstance(instance_value, str):
            raise RunnerError("bootstrap_instance_missing")
        instance = instance_value
        common = ["--pilot-config", str(pilot), "--admin-file", str(state / "pilot/admin.json")]
        command("check-isolation.py", ["--output", str(state / "isolation-before.json")], "isolation-before", 180)
        verify_isolation(state / "isolation-before.json")
        for name in ("security", "adversarial"):
            command("security-suite.py", ["prepare", "--state", str(state / name), *common, *platform], name + "-prepare", 900)
            script = "security-suite.py" if name == "security" else "adversarial-suite.py"
            arguments = ["run", "--report-home"] if name == "security" else []
            command(script, [*arguments, "--state", str(state / name), *platform], name + "-run", 2400)
            health.LabGuard(active).verify(health.collect())
        try:
            command("hikyo_detection_provision.py", ["--state", str(state / "detection"), *common], "detection-prepare", 900)
            command("detection-exercise.py", ["--state", str(state / "detection/fixture"),
                                           "--collector-config-file", str(state / "detection/collector.json"), *platform], "detection-run", 900)
        finally:
            if (state / "detection/collector.json").exists():
                command("hikyo_detection_provision.py", ["--cleanup", "--state", str(state / "detection"), *common], "collector-cleanup", 300)
        command("check-isolation.py", ["--output", str(state / "isolation-after.json")], "isolation-after", 180)
        verify_isolation(state / "isolation-after.json")
        isolation_verified = True
        health.LabGuard(active).verify(health.collect())
        suites = reports(state, instance, version)
    except (RuntimeError, OSError, ValueError, TypeError, KeyError, subprocess.SubprocessError) as error:
        errors.append(public_error_code(error))
        if isinstance(error, health.GuardError):
            save(state / "guard-failure.json", {"code": str(error)})
        if instance:
            try:
                suites = reports(state, instance, version)
            except (RuntimeError, OSError, ValueError, TypeError, KeyError):
                errors.append("result-evidence-unverified")
    finally:
        for process in reversed(tunnels):
            stop_tunnel(process)
        runtime = state / "detection/fixture/stack"
        if (runtime / "runtime-owner-journal.json").exists():
            try:
                proof = monitoring_runtime.recover_runtime(runtime)
                save(state / "monitor-cleanup.json", proof)
                if proof.get("verified") is not True:
                    errors.append("monitor-cleanup-unverified")
            except (RuntimeError, OSError, ValueError, TypeError, KeyError):
                errors.append("monitor-cleanup-unverified")
        try:
            proof = target.cleanup()
            save(state / "cleanup.json", proof)
            cleanup_verified = proof.get("verified") is True
            if before is not None:
                after = health.collect(expected_lab_pods=6)
                unchanged_idle(before, after)
                save(state / "idle-after.json", after)
        except (RuntimeError, OSError, ValueError, TypeError, KeyError):
            cleanup_verified = False
    if not cleanup_verified:
        errors.append("cleanup-unverified")
    counts = release.summarise_results(suites)
    status = release.outcome(suites, tuple(dict.fromkeys(errors)))
    summary = {"run_id": run_id, "tag": tag, "source_sha": source_sha, "status": status,
               "cleanup_verified": cleanup_verified, "counts": counts,
               "error_codes": list(dict.fromkeys(errors))}
    save(state / "summary.json", summary)
    total_passed = sum(row["passed"] for row in counts.values())
    if status == "PASS" and total_passed == 313 and cleanup_verified and isolation_verified:
        save(attestation, {
            "schema_version": 1, "status": "PASS", "release_id": candidate.release_id,
            "tag": tag, "source_sha": source_sha, "published_at": candidate.published_at,
            "image_ref": image_ref, "suite_sha256": fingerprint, "expected_assertions": 313,
            "passed_assertions": 313, "failed_assertions": 0, "skipped_assertions": 0,
            "cleanup_verified": True, "isolation_verified": True,
            "image_native_binary_verified": prepared.get("image_native_binary_verified"),
            "native_verification": prepared.get("native_verification"),
        })
    else:
        raise RunnerError("hosted_suite_not_complete_pass")
    return summary


def main() -> None:
    os.umask(0o077)
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--release-json", type=Path, required=True)
    parser.add_argument("--state", type=Path, required=True)
    parser.add_argument("--tag", required=True)
    parser.add_argument("--source-sha", required=True)
    parser.add_argument("--image-ref", required=True)
    args = parser.parse_args()
    for number in (signal.SIGTERM, signal.SIGINT):
        signal.signal(number, _signal)
    try:
        result = execute(args.state, release_json=args.release_json, tag=args.tag,
                         source_sha=args.source_sha, image_ref=args.image_ref)
    except (RuntimeError, OSError, ValueError, TypeError, KeyError, subprocess.SubprocessError) as error:
        print("FAIL: hosted lab " + public_error_code(error) + "; no release publication")
        raise SystemExit(1) from None
    if result.get("status") != "PASS":
        raise SystemExit(1)
    print("PASS: 313 native assertions")


if __name__ == "__main__":
    main()
