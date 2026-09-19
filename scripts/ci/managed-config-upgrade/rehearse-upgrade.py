#!/usr/bin/env python3
"""Local synthetic rehearsal; never a production updater or rollback tool."""

import argparse
import base64
import hashlib
import json
import os
import re
import secrets
import shutil
import subprocess
import sys
import time
import urllib.request
import uuid
from pathlib import Path

IDENTITY = (
    "https://github.com/Hikyo-Org/Hikyo/.github/workflows/nightly.yml@refs/heads/main"
)
ISSUER = "https://token.actions.githubusercontent.com"
IMAGE = re.compile(r"ghcr\.io/hikyo-org/hikyo@sha256:[0-9a-f]{64}\Z")
LABEL = "io.hikyo.dbugit.rehearsal"


def arguments(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "--source", required=True, help="Exact signed multiarch index reference"
    )
    parser.add_argument("--candidate", help="Exact signed multiarch index reference")
    parser.add_argument("--bootstrap-only", action="store_true")
    parser.add_argument(
        "--fixture", type=Path, help="Trusted executable: seed / verify stages"
    )
    parser.add_argument(
        "--output",
        type=Path,
        required=True,
        help="New private directory; must not exist",
    )
    parser.add_argument("--timeout", type=int, default=300)
    args = parser.parse_args(argv)
    if not IMAGE.fullmatch(args.source):
        parser.error("source must pin the official Hikyo sha256 index")
    if args.bootstrap_only:
        if args.candidate:
            parser.error("bootstrap-only cannot specify a candidate")
    elif not args.candidate or not IMAGE.fullmatch(args.candidate):
        parser.error("upgrade requires an official digest-pinned candidate")
    elif args.source == args.candidate:
        parser.error("same-image restart is not upgrade evidence")
    if not args.bootstrap_only and not args.fixture:
        parser.error("upgrade requires a trusted representative fixture")
    if args.fixture and (
        not args.fixture.is_file() or not os.access(args.fixture, os.X_OK)
    ):
        parser.error("fixture must be an executable file")
    if args.output.exists() or args.output.is_symlink():
        parser.error("output must not exist: never reuse earlier evidence")
    if not 1 <= args.timeout <= 1800:
        parser.error("timeout must be 1..1800 seconds")
    return args


class Rehearsal:
    def __init__(self, args):
        self.args = args
        self.output = args.output.absolute()
        self.output.mkdir(mode=0o700)
        self.run_id = "hikyo-rehearsal-" + uuid.uuid4().hex[:16]
        self.resources = []
        self.docker = ["docker"]
        self.report = {
            "run_id": self.run_id,
            "mode": "bootstrap" if args.bootstrap_only else "upgrade",
            "source": args.source,
            "candidate": args.candidate,
            "status": "incomplete",
            "scope": "synthetic local Docker PostgreSQL; not production or Kubernetes proof",
            "signature_identity": IDENTITY,
            "signature_issuer": ISSUER,
            "stages": [],
            "resources": self.resources,
        }
        self.save()

    def save(self):
        target = self.output / "report.json"
        temporary = self.output / "report.tmp"
        temporary.write_text(json.dumps(self.report, indent=2) + "\n")
        temporary.replace(target)

    def call(self, command, *, stdin=None, timeout=300):
        # Never expose subprocess output or command arguments on failure.
        result = subprocess.run(
            command, input=stdin, capture_output=True, timeout=timeout, check=False
        )
        with (self.output / "diagnostics.log").open("ab") as stream:
            stream.write(result.stdout + result.stderr)
        if result.returncode:
            raise RuntimeError("command failed; private diagnostics retained")
        return result.stdout.decode()

    def stage(self, name):
        self.report["stages"].append(name)
        self.save()
        print(name, flush=True)

    def own(self, kind, name, command):
        self.call(self.docker + command)
        self.resources.append({"kind": kind, "name": name})
        self.save()

    def verify(self, reference, name):
        result = self.call(
            [
                "cosign",
                "verify",
                "--certificate-identity",
                IDENTITY,
                "--certificate-oidc-issuer",
                ISSUER,
                reference,
            ]
        )
        (self.output / (name + "-signature.json")).write_text(result)
        manifest = json.loads(
            self.call(
                self.docker + ["buildx", "imagetools", "inspect", "--raw", reference]
            )
        )
        if not any(
            item.get("platform", {}).get("architecture") == "arm64"
            and item.get("platform", {}).get("os") == "linux"
            for item in manifest.get("manifests", [])
        ):
            raise RuntimeError("signed index lacks linux/arm64 manifest")
        (self.output / (name + "-index.json")).write_text(
            json.dumps(manifest, indent=2)
        )
        self.call(self.docker + ["pull", "--platform", "linux/arm64", reference])
        self.stage(name + "-signature-and-arm64-verified")

    def wait_ready(self, url):
        deadline = time.monotonic() + self.args.timeout
        # Ignore proxy environment for this loopback-only check.
        opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
        while time.monotonic() < deadline:
            state = json.loads(
                self.call(
                    self.docker
                    + ["inspect", "--format", "{{json .State}}", self.server],
                    timeout=5,
                )
            )
            if state.get("Running") is not True:
                self.report["server_failure"] = {
                    "container": self.server,
                    "status": state.get("Status"),
                    "exit_code": state.get("ExitCode"),
                    "oom_killed": state.get("OOMKilled"),
                }
                self.save()
                raise RuntimeError("server stopped before readiness")
            try:
                with opener.open(url, timeout=2) as response:
                    if response.status == 200:
                        return
            except (OSError, urllib.error.URLError):
                pass
            time.sleep(1)
        raise RuntimeError("readiness deadline exceeded")

    def fixture(self, stage):
        if not self.args.fixture:
            return
        if (
            hashlib.sha256(self.args.fixture.read_bytes()).hexdigest()
            != self.report["fixture_sha256"]
        ):
            raise RuntimeError("fixture changed during rehearsal")
        # Do not inherit production HIKYO credentials, proxy variables, or CLI config.
        environment = {
            "PATH": os.environ.get("PATH", "/usr/bin:/bin"),
            "HOME": str(self.output / "fixture-home"),
            "DOCKER_HOST": self.report["docker_endpoint"],
            "HIKYO_REHEARSAL_CONTAINER": self.server,
            "HIKYO_REHEARSAL_URL": self.url,
            "HIKYO_REHEARSAL_TLS_SPKI_PIN": self.report["tls_spki_pin"],
            "HIKYO_REHEARSAL_HELPER_IMAGE": self.report["postgres"],
            "HIKYO_REHEARSAL_OUTPUT_DIR": str(self.output),
            "HIKYO_REHEARSAL_SOURCE_IMAGE": self.args.source,
            "HIKYO_REHEARSAL_CANDIDATE_IMAGE": self.args.candidate or "",
            "HIKYO_REHEARSAL_RUN_ID": self.run_id,
        }
        with (self.output / ("fixture-" + stage + ".log")).open("wb") as log:
            result = subprocess.run(
                [str(self.args.fixture.resolve()), stage],
                env=environment,
                cwd=self.output,
                stdout=log,
                stderr=log,
                timeout=self.args.timeout,
                check=False,
            )
        if result.returncode:
            raise RuntimeError("representative fixture failed: " + stage)
        self.stage("fixture-" + stage + "-passed")

    def prepare_tls(self):
        tls = self.output / "tls"
        tls.mkdir(mode=0o700)
        self.call(
            [
                "openssl",
                "req",
                "-x509",
                "-newkey",
                "rsa:2048",
                "-nodes",
                "-days",
                "2",
                "-subj",
                "/CN=localhost",
                "-addext",
                "subjectAltName=IP:127.0.0.1,DNS:localhost",
                "-keyout",
                str(tls / "server.key"),
                "-out",
                str(tls / "server.crt"),
            ]
        )
        public = self.call(
            [
                "openssl",
                "x509",
                "-in",
                str(tls / "server.crt"),
                "-pubkey",
                "-noout",
            ]
        )
        # DER is binary, so keep it out of call()'s text decoder.
        result = subprocess.run(
            ["openssl", "pkey", "-pubin", "-outform", "DER"],
            input=public.encode(),
            capture_output=True,
            check=True,
            timeout=10,
        )
        self.report["tls_spki_pin"] = base64.b64encode(
            hashlib.sha256(result.stdout).digest()
        ).decode()
        self.save()

    def boot(self, reference, name):
        self.server = self.run_id + "-" + name
        self.own(
            "container",
            self.server,
            [
                "run",
                "-d",
                "--name",
                self.server,
                "--label",
                LABEL + "=" + self.run_id,
                "--platform",
                "linux/arm64",
                "--network",
                "container:" + self.pg,
                "--user",
                "65532:65532",
                "--read-only",
                "--tmpfs",
                "/tmp:rw,nosuid,nodev,size=64m,mode=1777",
                "--cap-drop",
                "ALL",
                "--security-opt",
                "no-new-privileges",
                "--env-file",
                str(self.output / "server.env"),
                "-v",
                self.state + ":/var/lib/hikyo",
                reference,
                "server",
                "--listen=0.0.0.0:8080",
                "--operational-listen=0.0.0.0:8081",
            ],
        )
        self.wait_ready(self.ready_url)
        self.stage(name + "-ready")

    def run(self):
        for tool in ("docker", "cosign", "openssl"):
            if not shutil.which(tool):
                raise RuntimeError("required tool missing: " + tool)
        if os.environ.get("DOCKER_HOST"):
            endpoint = os.environ["DOCKER_HOST"]
        else:
            endpoint = self.call(
                [
                    "docker",
                    "context",
                    "inspect",
                    "--format",
                    "{{.Endpoints.docker.Host}}",
                ]
            ).strip()
        if not endpoint.startswith("unix:///"):
            raise RuntimeError("only a local Unix Docker socket is permitted")
        self.docker = ["docker", "--host", endpoint]
        self.report["docker_endpoint"] = endpoint
        self.save()
        self.verify(self.args.source, "source")
        if self.args.candidate:
            self.verify(self.args.candidate, "candidate")
        postgres = "postgres:18@sha256:06cad38a5d9f5d24b4d83d86def30795d5e4b757fedbf5281172b576dedcd941"
        self.report["postgres"] = postgres
        self.call(self.docker + ["pull", "--platform", "linux/arm64", postgres])
        network = self.run_id + "-net"
        self.pg = self.run_id + "-postgres"
        self.state = self.run_id + "-state"
        pgdata = self.run_id + "-pgdata"
        label = LABEL + "=" + self.run_id
        self.own("network", network, ["network", "create", "--label", label, network])
        for volume in (self.state, pgdata):
            self.own("volume", volume, ["volume", "create", "--label", label, volume])
        self.prepare_tls()
        password = secrets.token_hex(32)
        (self.output / "pg.env").write_text(
            "POSTGRES_PASSWORD=" + password + "\nPOSTGRES_DB=hikyo\n"
        )
        self.own(
            "container",
            self.pg,
            [
                "run",
                "-d",
                "--name",
                self.pg,
                "--label",
                label,
                "--platform",
                "linux/arm64",
                "--network",
                network,
                "-p",
                "127.0.0.1::8080",
                "-p",
                "127.0.0.1::8081",
                "--env-file",
                str(self.output / "pg.env"),
                "-v",
                pgdata + ":/var/lib/postgresql",
                postgres,
            ],
        )
        deadline = time.monotonic() + self.args.timeout
        while True:
            try:
                # Probe TCP, not the socket. On fresh pgdata the entrypoint runs
                # a temporary init-phase server that listens on the Unix socket
                # ONLY, then shuts it down before the final server binds TCP.
                # A socket probe passes against that temp server and races the
                # following createdb into its shutdown; TCP is bound only by the
                # final server, so -h forces the wait to the real readiness.
                self.call(
                    self.docker
                    + [
                        "exec",
                        self.pg,
                        "pg_isready",
                        "-U",
                        "postgres",
                        "-h",
                        "127.0.0.1",
                    ],
                    timeout=5,
                )
                break
            except RuntimeError:
                if time.monotonic() > deadline:
                    raise RuntimeError(
                        "PostgreSQL readiness deadline exceeded"
                    ) from None
                time.sleep(1)
        self.call(
            self.docker + ["exec", self.pg, "createdb", "-U", "postgres", "scratch"]
        )
        # Pinned PostgreSQL helper only sees this run's fresh state volume.
        self.call(
            self.docker
            + [
                "run",
                "--rm",
                "--network",
                "none",
                "--platform",
                "linux/arm64",
                "-i",
                "-v",
                self.state + ":/state",
                "-v",
                str(self.output / "tls") + ":/tls:ro",
                "--entrypoint",
                "sh",
                postgres,
                "-ec",
                (
                    "umask 077; cat > /state/root-key; cp /tls/server.crt /tls/server.key /state/; mkdir -p /state/upgrade/operator-custody; "
                    "chown -R 65532:65532 /state; chmod 700 /state /state/upgrade/operator-custody; "
                    "chown 0:65532 /state/upgrade; chmod 3770 /state/upgrade"
                ),
            ],
            stdin=secrets.token_hex(32).encode(),
        )
        bindings = json.loads(
            self.call(
                self.docker
                + ["inspect", "--format", "{{json .NetworkSettings.Ports}}", self.pg]
            )
        )
        self.url = "https://127.0.0.1:" + bindings["8080/tcp"][0]["HostPort"]
        self.ready_url = (
            "http://127.0.0.1:" + bindings["8081/tcp"][0]["HostPort"] + "/readyz"
        )
        (self.output / "server.env").write_text(
            "HIKYO_DB=postgres://postgres:"
            + password
            + "@127.0.0.1:5432/hikyo?sslmode=disable\n"
            "HIKYO_UPGRADE_SCRATCH_POSTGRES_DSN=postgres://postgres:"
            + password
            + "@127.0.0.1:5432/scratch?sslmode=disable\n"
            "HIKYO_UPGRADE_UNATTENDED=true\nHIKYO_UPGRADE_STATE_DIR=/var/lib/hikyo/upgrade/operator-custody\n"
            "HIKYO_ROOT_KEY_FILE=/var/lib/hikyo/root-key\n"
            "HIKYO_TLS_CERT_FILE=/var/lib/hikyo/server.crt\n"
            "HIKYO_TLS_KEY_FILE=/var/lib/hikyo/server.key\n"
            "HIKYO_EXTERNAL_ORIGIN="
            + self.url
            + "\nHIKYO_UPDATE_CHANNEL=off\nHIKYO_MCP_ENABLED=true\n"
        )
        (self.output / "fixture-home").mkdir(mode=0o700)
        if self.args.fixture:
            self.report["fixture_sha256"] = hashlib.sha256(
                self.args.fixture.read_bytes()
            ).hexdigest()
        self.boot(self.args.source, "source")
        self.fixture("seed")
        if self.args.bootstrap_only:
            self.call(self.docker + ["stop", self.server])
            self.boot(self.args.source, "source-restart")
            self.fixture("verify")
            self.report["status"] = "bootstrap-only-no-upgrade-proof"
        else:
            self.call(self.docker + ["stop", self.server])
            self.stage("source-stopped")
            self.boot(self.args.candidate, "candidate")
            self.fixture("verify")
            self.report["status"] = "synthetic-upgrade-passed"
        self.save()

    def finish(self):
        for resource in self.resources:
            if resource["kind"] == "container":
                with (self.output / (resource["name"] + ".log")).open("wb") as log:
                    try:
                        subprocess.run(
                            self.docker + ["logs", resource["name"]],
                            stdout=log,
                            stderr=log,
                            timeout=15,
                            check=False,
                        )
                    except (OSError, subprocess.TimeoutExpired):
                        log.write(
                            b"\nDiagnostic collection unavailable; report status is unchanged.\n"
                        )
        print("Private report and diagnostics: " + str(self.output))
        print(
            "Resources retained; cleanup after inspection with cleanup-rehearsal.py --report "
            + str(self.output / "report.json")
        )


def main():
    os.umask(0o077)
    args = arguments()
    run = Rehearsal(args)
    try:
        run.run()
    except (RuntimeError, OSError, ValueError, subprocess.TimeoutExpired) as error:
        run.report["status"] = "failed"
        run.save()
        print(
            "Rehearsal failed ("
            + type(error).__name__
            + "); inspect private diagnostics.",
            file=sys.stderr,
        )
        return 1
    finally:
        run.finish()
    return 0


if __name__ == "__main__":
    sys.exit(main())
