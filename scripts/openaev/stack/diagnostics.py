#!/usr/bin/env python3
"""Reduce startup evidence to fixed codes; never expose log messages or secret data."""
from __future__ import annotations

import argparse
import json
import re
import subprocess
from collections import Counter
from pathlib import Path

STAGES = frozenset({
    "preflight", "install-k3d", "build-injector", "create-cluster", "pull-pinned-images",
    "import-injector", "install-system-services", "create-private-custody", "start-isolation-controls",
    "enforce-node-fence", "apply-backend-workloads", "apply-openaev-workloads", "wait-statefulset-rabbitmq",
    "wait-statefulset-pgsql", "wait-statefulset-elasticsearch", "wait-statefulset-silo",
    "wait-deployment-openaev", "wait-deployment-http-injector", "verify-rabbitmq-remount",
    "verify-rabbitmq-remount-create", "verify-rabbitmq-remount-ready",
})

LOGGERS = frozenset({
    "io.openaev.App", "io.openaev.runner.InitAdminCommandLineRunner",
    "io.openaev.health.DependencyProbeService", "io.openaev.rest.health_check.HealthCheckApi",
    "io.openaev.service.RabbitmqService", "io.openaev.service.MinioService",
    "io.openaev.engine.facade.EngineService", "io.openaev.processor.MigrationProcessor",
    "io.openaev.processor.datapack.V20260101_Starter_pack", "io.openaev.service.connector_instances.NativeEncryptionService",
    "org.springframework.boot.SpringApplication", "org.springframework.boot.web.embedded.tomcat.TomcatWebServer",
    "org.springframework.amqp.rabbit.connection.CachingConnectionFactory",
    "org.springframework.amqp.rabbit.listener.SimpleMessageListenerContainer",
    "org.springframework.boot.web.servlet.context.ServletWebServerApplicationContext",
    "org.flywaydb.core.FlywayExecutor", "org.flywaydb.core.internal.command.DbValidate", "org.flywaydb.core.internal.command.DbMigrate",
    "com.zaxxer.hikari.HikariDataSource", "com.zaxxer.hikari.pool.HikariPool",
})
# Earlier OpenAEV images retain the fixed OpenBAS package names. Emitted names
# still come from this allowlist, never from arbitrary input logger fields.
LOGGERS = LOGGERS | frozenset(logger.replace("io.openaev.", "io.openbas.")
                             for logger in LOGGERS if logger.startswith("io.openaev."))
LOG_SCHEMA_KEYS = frozenset({"loggerName", "logger_name", "logger", "message", "throwable", "exception", "stack_trace"})

EXCEPTIONS = frozenset({
    "java.net.ConnectException", "java.net.SocketTimeoutException", "java.net.UnknownHostException",
    "java.lang.IllegalArgumentException", "java.lang.IllegalStateException", "java.lang.OutOfMemoryError",
    "java.io.FileNotFoundException", "java.io.IOException", "java.security.UnrecoverableKeyException",
    "javax.net.ssl.SSLException", "javax.net.ssl.SSLHandshakeException",
    "org.springframework.beans.factory.BeanCreationException",
    "org.springframework.beans.factory.UnsatisfiedDependencyException",
    "org.springframework.amqp.AmqpConnectException", "org.springframework.amqp.AmqpAuthenticationException",
    "org.springframework.amqp.rabbit.support.ConsumerCancelledException",
    "org.postgresql.util.PSQLException", "org.flywaydb.core.api.FlywayException",
    "org.flywaydb.core.internal.exception.FlywayMigrateException",
    "io.minio.errors.ErrorResponseException", "io.minio.errors.InvalidResponseException",
})
REASONS = frozenset({
    "Running", "Pending", "Succeeded", "Failed", "Unknown", "Completed", "Error", "OOMKilled",
    "CrashLoopBackOff", "ImagePullBackOff", "ErrImagePull", "ErrImageNeverPull", "ContainerCreating",
    "PodInitializing", "CreateContainerConfigError", "CreateContainerError", "FailedScheduling",
    "FailedMount", "FailedAttachVolume", "FailedCreate", "Unhealthy", "BackOff", "Pulled", "Pulling",
    "Created", "Started", "Scheduled", "Killing", "SuccessfulCreate", "SuccessfulDelete",
    "ProgressDeadlineExceeded", "MinimumReplicasUnavailable", "MinimumReplicasAvailable",
    "ReplicaSetUpdated", "NewReplicaSetAvailable", "ContainersNotReady", "ContainersNotInitialized",
    "PodCompleted", "PodFailed", "PodReadyToStartContainers", "Ready", "Initialized", "PodScheduled",
})
# Messages are inspected only for fixed phrases and are never included in output.
SIGNALS = {
    "application_started": "Started App in ", "platform_initialization_started": "Startup init",
    "platform_instance_initialized": "Startup of the platform - Platform Instance ID:", "application_failed": "Application run failed",
    "tls_listener_started": "Tomcat started on port", "database_pool_started": "Start completed",
    "database_migration_completed": "Successfully applied", "database_migration_failed": "Migration failed",
    "admin_email_missing": "'openaev.admin.email' cannot be null",
    "admin_email_invalid": "'openaev.admin.email' should be a valid email address",
    "admin_token_invalid": "'openaev.admin.token' should be a valid UUID",
    "health_dependency_failed": "Health probe of ",
    "postgresql_health_failed": "Health probe of postgresql failed",
    "rabbitmq_health_failed": "Health probe of rabbitmq failed",
    "s3_health_failed": "Health probe of s3 failed", "engine_health_failed": "Health probe of engine failed", "health_endpoint_unhealthy": "Health check failure",
    "rabbitmq_connection_attempt": "Attempting to connect to: ",
    "rabbitmq_connection_created": "Created new connection: ",
    "rabbitmq_connection_failed": "Failed to connect to RabbitMQ",
    "keystore_load_failed": "Unable to load key store", "tls_key_unrecoverable": "Cannot recover key",
    "permission_denied": "Permission denied", "readonly_filesystem": "Read-only file system",
}


def object_value(value: object) -> dict[str, object]:
    if not isinstance(value, dict) or any(not isinstance(key, str) for key in value):
        return {}
    return dict(value)


def objects(value: object) -> list[dict[str, object]]:
    return [object_value(item) for item in value] if isinstance(value, list) else []


def safe_reason(value: object) -> str:
    return value if isinstance(value, str) and value in REASONS else "other"


def log_summary(raw: bytes) -> dict[str, object]:
    """Untrusted messages, unknown loggers, arguments and exception text never escape."""
    loggers: Counter[str] = Counter()
    exceptions: Counter[str] = Counter()
    signals: Counter[str] = Counter()
    schema_keys: Counter[str] = Counter()
    unknown_loggers = 0
    parsed = 0
    for line in raw.decode(errors="replace").splitlines():
        try:
            record = object_value(json.loads(line))
        except (ValueError, UnicodeError):
            continue
        if not record:
            continue
        parsed += 1
        schema_keys.update(key for key in LOG_SCHEMA_KEYS if key in record)
        logger = record.get("loggerName", record.get("logger_name", record.get("logger")))
        if not isinstance(logger, str) or logger not in LOGGERS:
            unknown_loggers += 1
            continue
        loggers[logger] += 1
        message = record.get("message")
        if isinstance(message, str):
            for code, phrase in SIGNALS.items():
                if phrase in message:
                    signals[code] += 1
        # Pinned OpenAEV uses Logback JsonEncoder, whose throwable is a
        # structured object. Only its exact class name and cause/suppressed
        # class names are inspected; messages and frame contents are ignored.
        pending = [object_value(record.get("throwable"))]
        for _ in range(64):
            if not pending:
                break
            throwable = pending.pop()
            name = throwable.get("className")
            if isinstance(name, str) and name in EXCEPTIONS:
                exceptions[name] += 1
            cause = object_value(throwable.get("cause"))
            if cause:
                pending.append(cause)
            pending.extend(objects(throwable.get("suppressed"))[:16])
        trace = record.get("stack_trace", record.get("exception"))
        if isinstance(trace, str):
            for name in EXCEPTIONS:
                if re.search(r"(?<![A-Za-z0-9_.$])" + re.escape(name) + r"(?=[:\s]|$)", trace):
                    exceptions[name] += 1
    return {"json_records": parsed, "unknown_logger_records": unknown_loggers,
            "schema_key_counts": dict(sorted(schema_keys.items())), "logger_counts": dict(sorted(loggers.items())),
            "exception_counts": dict(sorted(exceptions.items())), "signal_counts": dict(sorted(signals.items()))}


def raw_command(kubeconfig: Path, arguments: list[str], *, include_stderr: bool = True) -> bytes:
    try:
        result = subprocess.run(["kubectl", "--kubeconfig", str(kubeconfig)] + arguments,
                                capture_output=True, check=False, timeout=15)
        return result.stdout + (result.stderr if include_stderr else b"")
    except (OSError, subprocess.SubprocessError):
        return b""


def native_json(kubeconfig: Path, arguments: list[str]) -> dict[str, object]:
    try:
        return object_value(json.loads(raw_command(kubeconfig, arguments + ["-o", "json"], include_stderr=False)))
    except (ValueError, UnicodeError):
        return {}


def collect(state: Path, stage: str) -> dict[str, object]:
    stage = stage if stage in STAGES else "unknown"
    kubeconfig = state / "kubeconfig"
    if not kubeconfig.is_file():
        return {"code": "stack_startup_diagnostics", "stage": stage, "native_status": "not_created"}
    document = native_json(kubeconfig, ["get", "pods", "-n", "openaev-lab"])
    pod_summary: dict[str, object] = {}
    for pod in objects(document.get("items")):
        labels = object_value(object_value(pod.get("metadata")).get("labels"))
        app = labels.get("app")
        if app not in ("openaev", "rabbitmq", "pgsql", "elasticsearch", "silo", "http-injector"):
            continue
        status = object_value(pod.get("status"))
        containers = []
        for container in objects(status.get("containerStatuses")) + objects(status.get("initContainerStatuses")):
            native_state = object_value(container.get("state"))
            reasons = [safe_reason(object_value(value).get("reason"))
                       for name, value in native_state.items() if name in ("waiting", "terminated")]
            containers.append({"ready": container.get("ready") is True,
                               "restarts": container.get("restartCount") if isinstance(container.get("restartCount"), int) else 0,
                               "state": "running" if "running" in native_state else "stopped",
                               "reasons": reasons})
        pod_summary[str(app)] = {"phase": safe_reason(status.get("phase")), "containers": containers}
    deployment = native_json(kubeconfig, ["get", "deployment/openaev", "-n", "openaev-lab"])
    deployment_reasons = [safe_reason(condition.get("reason")) for condition in
                          objects(object_value(deployment.get("status")).get("conditions"))]
    events = native_json(kubeconfig, ["get", "events", "-n", "openaev-lab"])
    event_reasons = Counter(safe_reason(event.get("reason")) for event in objects(events.get("items")))
    logs = raw_command(kubeconfig, ["logs", "deployment/openaev", "-n", "openaev-lab", "--tail=5000"])
    previous_logs = raw_command(kubeconfig, ["logs", "deployment/openaev", "-n", "openaev-lab", "--previous", "--tail=5000"])
    # Run the exact health probe. Its key remains a pod environment expansion;
    # capture stderr privately and emit only the parsed HTTP status number.
    probe = raw_command(kubeconfig, ["exec", "deployment/openaev", "-n", "openaev-lab", "--", "sh", "-c",
        'wget --no-check-certificate -T 5 -S -O /dev/null "https://127.0.0.1:8080/api/health?health_access_key=$OPENAEV_HEALTHCHECK_KEY"'])
    codes = re.findall(rb"HTTP/[0-9.]+\s+([1-5][0-9]{2})\b", probe)
    return {"code": "stack_startup_diagnostics", "stage": stage, "pods": pod_summary,
            "deployment_reasons": deployment_reasons, "event_reasons": dict(sorted(event_reasons.items())),
            "health_http_code": int(codes[-1]) if codes else None, "openaev_logs": log_summary(logs), "openaev_previous_logs": log_summary(previous_logs)}


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("state", type=Path)
    parser.add_argument("--stage", default="unknown")
    args = parser.parse_args()
    print(json.dumps(collect(args.state, args.stage), sort_keys=True))
