#!/usr/bin/env python3
"""Disposable container ONLY: real product packets, Unix ACL and SSH rehearsal."""

import hashlib
import errno
import json
import os
from pathlib import Path
import shutil
import signal
import socket
import subprocess
import time


def command(arguments, *, data=None, ok=True):
    result = subprocess.run(arguments, input=data, capture_output=True, timeout=20, check=False)
    if (result.returncode == 0) != ok:
        health = result.stdout.decode()[:3000] if "/opt/hikyo-evidence/monitor.py" in arguments else ""
        raise AssertionError(f"command result {result.returncode}: {arguments[0:3]} {result.stderr.decode()[:500]} {health}")
    return result


def ssh_denials(ssh, packet):
    for request in ("id", "sh", "python3 -c 'print(1)'", "rm -rf /srv/hikyo-evidence"):
        command(ssh + [request], ok=False)
    subsystem = command(ssh[:-2] + ["-s", ssh[-1], "sftp"], ok=False)
    assert (subsystem.returncode == 64 or
            (subsystem.returncode == 255 and b"subsystem request failed" in subsystem.stderr)), "SFTP subsystem must be explicitly refused"
    tty = command(ssh[:-2] + ["-tt", ssh[-1]], data=packet, ok=False)
    assert tty.returncode == 255 and b"PTY allocation request failed" in tty.stderr, "PTY must be denied independently of command refusal"
    forwarding_probe = [argument for argument in ssh[:-2] if argument != "-oClearAllForwardings=yes"]
    forward = command(forwarding_probe + ["-N", "-oExitOnForwardFailure=yes", "-R", "0:127.0.0.1:2222", ssh[-1]], ok=False)
    assert b"remote port forwarding failed" in forward.stderr


def health_and_capacity(base, streams):
    state = base / "monitor"
    certificate, key = state / "test.crt", state / "test.key"
    command(["openssl", "req", "-x509", "-newkey", "ed25519", "-nodes", "-subj", "/CN=127.0.0.1",
             "-addext", "subjectAltName=IP:127.0.0.1", "-days", "1", "-keyout", str(key), "-out", str(certificate)])
    shutil.chown(key, user="hk-monitor", group="hk-monitor")
    shutil.chown(certificate, user="hk-monitor", group="hk-monitor")
    snapshot = state / "health.json"
    snapshot.write_text(json.dumps({"checked_ns": time.time_ns(), "healthy": True}))
    shutil.chown(snapshot, user="hk-monitor", group="hk-monitor")
    health_log = (state / "test-health.log").open("wb")
    server = subprocess.Popen(["runuser", "-u", "hk-monitor", "--", "python3", "-I", "-B",
                               "/opt/hikyo-evidence/health.py", str(snapshot), str(certificate), str(key),
                               "--bind", "127.0.0.1", "--port", "8449"],
                              stdout=health_log, stderr=health_log, start_new_session=True)
    curl = ["curl", "-q", "--cacert", str(certificate), "--silent", "--show-error", "--fail", "--max-time", "5", "https://127.0.0.1:8449/health"]
    try:
        for attempt in range(20):
            try:
                with socket.create_connection(("127.0.0.1", 8449), timeout=1):
                    break
            except OSError:
                if server.poll() is not None or attempt == 19:
                    health_log.flush()
                    raise RuntimeError("owned TLS health process did not start: " + (state / "test-health.log").read_text()[-2000:])
                time.sleep(0.1)
        assert command(curl).stdout == b"PASS\n"
        untrusted = command(["curl", "-q", "--silent", "--fail", "--max-time", "5", curl[-1]], ok=False)
        assert untrusted.returncode == 60, "health TLS must reject untrusted certificate"
        for checked in (time.time_ns() - 121_000_000_000, time.time_ns() + 60_000_000_000):
            snapshot.write_text(json.dumps({"checked_ns": checked, "healthy": True}))
            assert command(curl, ok=False).returncode == 22, "stopped/future detector must produce HTTP503"
    finally:
        if server.poll() is None:
            try:
                os.killpg(server.pid, signal.SIGTERM)
            except ProcessLookupError:
                pass  # The exclusively owned group exited between poll and kill.
        server.wait(timeout=10)
        health_log.close()
    assert command(curl, ok=False).returncode == 7, "stopped health service must be a transport failure"

    # Actual ENOSPC only on this disposable container's private 64MiB tmpfs.
    # The second receipt ID is synthetic; this is a storage-boundary control,
    # not another claimed product export.
    directory = Path("/dev/shm/hikyo-1037-capacity")
    directory.mkdir(mode=0o700)
    cfg = dict(streams["pg-tenant"], database=str(directory / "retained.sqlite"))
    config = directory / "config.json"
    config.write_text(json.dumps(cfg))
    cli = ["python3", "-I", "-B", "/opt/hikyo-evidence/receiver.py"]
    command(cli + ["init", str(config)])
    packet = Path("/packets/postgres-tenant/packet").read_bytes()
    original = json.loads(command(cli + ["receive", str(config)], data=packet).stdout)
    framing, body = packet.split(b"\n\n", 1)
    events, last = body.rstrip(b"\n").rsplit(b"\n", 1)
    terminal = json.loads(last)
    terminal["export_receipt"]["started_event_id"] = "synthetic-capacity-export"
    framing = framing.replace(original["batch_id"].encode(), b"synthetic-capacity-export")
    second = framing + b"\n\n" + events + b"\n" + json.dumps(terminal).encode() + b"\n"
    filler = directory / "filler"
    try:
        with filler.open("wb") as target:
            while True:
                target.write(b"x" * (512 * 1024))
    except OSError as error:
        assert error.errno == errno.ENOSPC
    command(cli + ["receive", str(config)], data=second, ok=False)
    retained = command(["sqlite3", "-readonly", cfg["database"], "SELECT digest FROM batches"])
    assert retained.stdout.strip().decode() == original["sha256"]
    filler.unlink()
    command(cli + ["receive", str(config)], data=second)
    shutil.rmtree(directory)
    return {"TLS_trust_stale_future_stopped_health": "PASS", "actual_owned_tmpfs_ENOSPC_retained_copy_retry": "PASS"}


def main():
    os.umask(0o077)
    if not Path("/.dockerenv").exists() or not Path("/packets").is_dir():
        raise ValueError("requires exclusively owned disposable container and product packet mount")
    base = Path("/srv/hikyo-evidence")
    base.mkdir(mode=0o700)
    keys = Path("/run/hikyo-rehearsal-keys")
    keys.mkdir(mode=0o700)
    streams = {}
    names = {"postgres-tenant": "pg-tenant", "postgres-instance": "pg-instance",
             "sqlite-tenant": "sqlite-tenant", "sqlite-instance": "sqlite-instance"}
    for original, name in names.items():
        streams[name] = json.loads((Path("/packets") / original / "stream.json").read_bytes())
        command(["ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-f", str(keys / name)])
    manifest = keys / "streams.json"
    manifest.write_text(json.dumps(streams))
    command(["python3", "-I", "-B", "/assets/install-host.py", str(manifest), str(keys), "--rehearsal"])
    # A second install must refuse without resetting existing custody.
    command(["python3", "-I", "-B", "/assets/install-host.py", str(manifest), str(keys), "--rehearsal"], ok=False)
    log = Path("/run/hikyo-rehearsal-sshd.log").open("wb")
    daemon = subprocess.Popen(["/usr/sbin/sshd", "-D", "-e", "-f", "/etc/hikyo-evidence/sshd_config"], stdout=log, stderr=log)
    try:
        for attempt in range(20):
            try:
                with socket.create_connection(("127.0.0.1", 2222), timeout=1):
                    break
            except OSError:
                if daemon.poll() is not None or attempt == 19:
                    raise RuntimeError("owned sshd did not start")
                time.sleep(0.1)
        summary = {"evidence_class": "LOCAL separate Unix users and loopback OpenSSH, not protected-host acceptance",
                   "streams": {}, "sshd_pid": daemon.pid}
        transport_probes = []
        for original, name in names.items():
            poller, submit = "hk-p-" + name, "hk-s-" + name
            spool = base / ("spool-" + name)
            shutil.copyfile(keys / name, spool / "submit-key")
            (spool / "submit-key").chmod(0o600)
            shutil.chown(spool / "submit-key", user=poller, group=poller)
            public = Path("/etc/hikyo-evidence/ssh_host_ed25519_key.pub").read_text().strip()
            known = spool / "known_hosts"
            known.write_text("[127.0.0.1]:2222 " + public + "\n")
            known.chmod(0o644)
            ssh = ["runuser", "-u", poller, "--", "ssh", "-F", "/dev/null", "-p", "2222",
                   "-i", str(spool / "submit-key"), "-oBatchMode=yes", "-oIdentitiesOnly=yes",
                   "-oStrictHostKeyChecking=yes", "-oUserKnownHostsFile=" + str(known),
                   "-oClearAllForwardings=yes", "-T", submit + "@127.0.0.1"]
            packet = (Path("/packets") / original / "packet").read_bytes()
            first = command(ssh, data=packet).stdout
            second = command(ssh, data=packet).stdout
            assert first == second, "lost-ack replay changed destination receipt"
            ack = json.loads(first)
            assert ack["sha256"] == hashlib.sha256(packet).hexdigest()
            # Defer protocol fault probes until the healthy initial inventory
            # is observed. A refused PTY can close a receive session with empty
            # stdin; that receiver refusal must remain HOLD, never be erased.
            transport_probes.append((ssh, packet))
            database = base / name / "retained.sqlite"
            for user in (poller, submit):
                prefix = ["runuser", "-u", user, "--"]
                for arguments in (["cat", str(database)], ["rm", str(database)],
                                  ["sqlite3", str(database), "UPDATE batches SET digest='changed'"],
                                  ["chmod", "777", str(database)],
                                  ["sh", "-c", "echo changed >> /etc/hikyo-evidence/monitor.json"],
                                  ["sh", "-c", "echo changed >> /opt/hikyo-evidence/receiver.py"],
                                  ["sudo", "-n", "id"]):
                    command(prefix + arguments, ok=False)
            for user in ("hk-reader", "hk-monitor"):
                command(["runuser", "-u", user, "--", "sqlite3", "-readonly", str(database), "SELECT COUNT(*) FROM batches"])
                command(["runuser", "-u", user, "--", "sqlite3", str(database), "DELETE FROM batches"], ok=False)
            # Independent reader compares exact bytes in a separate OS identity,
            # after the product rehearsal already removed the producer event.
            read = command(["runuser", "-u", "hk-reader", "--", "python3", "-I", "-B", "-c",
                            "import sqlite3,sys;d=sqlite3.connect('file:'+sys.argv[1]+'?mode=ro',uri=True);sys.stdout.buffer.write(d.execute('SELECT packet FROM batches').fetchone()[0])", str(database)])
            assert read.stdout == packet
            summary["streams"][name] = {**ack, "ssh_fixed_command": "PASS", "poller_read_write_delete_acl_denied": "PASS",
                                         "independent_read": "PASS", "reader_monitor_write_denied": "PASS"}
        # Root represents the independent deletion owner. Demonstrate exactly
        # that permission only on a fresh disposable deletion-probe file.
        probe = base / "deletion-owner-probe"
        probe.write_bytes(b"synthetic deletion probe")
        command(["runuser", "-u", "hk-reader", "--", "rm", str(probe)], ok=False)
        probe.unlink()
        summary["deletion_owner"] = "root only, synthetic probe removed"
        # Synthetic attempt assertions establish the initial local OS-monitor
        # baseline; these are not claimed source HTTP/authentication receipts.
        for name in names.values():
            status = base / ("spool-" + name) / "poll-status.json"
            status.write_text(json.dumps({"attempt_ns": time.time_ns(), "outcome": "success", "failure_count": 0}))
        monitor = ["runuser", "-u", "hk-monitor", "--", "python3", "-I", "-B", "/opt/hikyo-evidence/monitor.py", "check",
                   "/etc/hikyo-evidence/monitor.json", "/opt/hikyo-evidence/receiver.py",
                   str(base / "monitor/state.json"), str(base / "monitor/health.json")]
        assert json.loads(command(monitor).stdout)["healthy"]
        for ssh, packet in transport_probes:
            ssh_denials(ssh, packet)
        # Missing root-owned poll preflight configuration must be recorded by
        # the real poller UID before any source request, preserving monitor ACL
        # across atomic status replacement.
        command(["runuser", "-u", "hk-p-pg-tenant", "--", "python3", "-I", "-B", "/opt/hikyo-evidence/poll-once.py", "pg-tenant"], ok=False)
        monitored = json.loads(command(monitor, ok=False).stdout)
        assert not monitored["healthy"]
        assert "source-attempt-history-HOLD" in monitored["streams"]["pg-tenant"]["reasons"]
        summary["OS_monitor_and_durable_preflight_failure"] = "PASS, initial attempt assertions synthetic"
        summary["boundary_controls"] = health_and_capacity(base, streams)
        summary["remote_mutations"] = []
        print(json.dumps(summary, sort_keys=True))
    finally:
        daemon.terminate()
        daemon.wait(timeout=10)
        log.close()


if __name__ == "__main__":
    main()
