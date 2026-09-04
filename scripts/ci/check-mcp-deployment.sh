#!/bin/sh
set -eu

root=$(CDPATH='' cd -- "$(dirname "$0")/../.." && pwd)
compose="$root/deploy/mcp/compose.yaml"
proxy="$root/deploy/mcp/nginx.conf"
tmp=$(mktemp -d "${TMPDIR:-/tmp}/hikyo-mcp-deployment.XXXXXX")
stage_volume="hikyo-mcp-deployment-$$_staged"

cleanup() {
	docker volume rm "$stage_volume" >/dev/null 2>&1 || true
	rm -rf "$tmp"
}
trap cleanup EXIT HUP INT TERM

fail() {
	printf 'MCP deployment check: %s\n' "$1" >&2
	exit 1
}

command -v docker >/dev/null 2>&1 || fail "docker is required"
docker compose version >/dev/null 2>&1 || fail "docker compose is required"
command -v python3 >/dev/null 2>&1 || fail "python3 is required"
python3 -c 'import yaml' >/dev/null 2>&1 || fail "python3 with PyYAML is required"
command -v openssl >/dev/null 2>&1 || fail "openssl is required"

touch "$tmp/root-key" "$tmp/tls-cert" "$tmp/tls-key"
chmod 600 "$tmp/root-key" "$tmp/tls-key"
chmod 644 "$tmp/tls-cert"
export HIKYO_IMAGE='ghcr.io/hikyo-org/hikyo@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa'
export HIKYO_DB='postgres://hikyo@database:5432/hikyo'
export HIKYO_EXTERNAL_ORIGIN='https://hikyo.example.com'
export HIKYO_COMPOSE_SUBNET='172.30.0.0/24'
export HIKYO_ROOT_KEY_FILE="$tmp/root-key"
export HIKYO_TLS_CERT_FILE="$tmp/tls-cert"
export HIKYO_TLS_KEY_FILE="$tmp/tls-key"

"$root/scripts/mcp-compose-preflight.sh" >/dev/null
if HIKYO_IMAGE='ghcr.io/hikyo-org/hikyo:latest' "$root/scripts/mcp-compose-preflight.sh" >/dev/null 2>&1; then
	fail "operator preflight accepted a mutable Hikyo image tag"
fi
if HIKYO_ROOT_KEY_FILE="$tmp/missing-root-key" "$root/scripts/mcp-compose-preflight.sh" >/dev/null 2>&1; then
	fail "operator preflight accepted a missing root-key file"
fi
if HIKYO_TLS_CERT_FILE="$tmp" "$root/scripts/mcp-compose-preflight.sh" >/dev/null 2>&1; then
	fail "operator preflight accepted a directory as the TLS certificate"
fi
chmod 640 "$tmp/root-key"
if "$root/scripts/mcp-compose-preflight.sh" >/dev/null 2>&1; then
	fail "operator preflight accepted a group-readable root-key file"
fi
chmod 600 "$tmp/root-key"
chmod 604 "$tmp/tls-key"
if "$root/scripts/mcp-compose-preflight.sh" >/dev/null 2>&1; then
	fail "operator preflight accepted a world-readable TLS key file"
fi
chmod 600 "$tmp/tls-key"
docker compose -f "$compose" config >"$tmp/rendered.yaml"

python3 - "$tmp/rendered.yaml" "$proxy" <<'PY'
import re
import sys
import yaml

rendered_path, proxy_path = sys.argv[1:]
with open(rendered_path, encoding="utf-8") as stream:
    rendered = yaml.safe_load(stream)
with open(proxy_path, encoding="utf-8") as stream:
    proxy = stream.read()

def fail(message):
    print(f"MCP deployment check: {message}", file=sys.stderr)
    raise SystemExit(1)

services = rendered.get("services", {})
if set(services) != {"hikyo", "proxy", "root-key-stage"}:
    fail(f"unexpected Compose services: {sorted(services)}")
hikyo = services["hikyo"]
proxy_service = services["proxy"]
root_key_stage = services["root-key-stage"]
for service_name, service in services.items():
    image = service.get("image", "")
    if not re.fullmatch(r"[^@]+@sha256:[0-9a-f]{64}", image):
        fail(f"{service_name} image is not digest-pinned")
if hikyo.get("command") != [
    "server",
    "--listen=0.0.0.0:8080",
    "--operational-listen=0.0.0.0:8081",
    "--root-key-file=/run/hikyo-root-key/root-key",
]:
    fail("Hikyo command drifted")
environment = hikyo.get("environment", {})
want_environment = {
    "HIKYO_DB": "postgres://hikyo@database:5432/hikyo",
    "HIKYO_EXTERNAL_ORIGIN": "https://hikyo.example.com",
    "HIKYO_MCP_ALLOWED_ORIGINS": "",
    "HIKYO_MCP_ENABLED": "true",
    "HIKYO_TRUSTED_PROXY_CIDRS": "172.30.0.0/24",
}
if environment != want_environment:
    fail(f"Hikyo environment drifted: {environment}")
if hikyo.get("stop_grace_period") != "30s":
    fail("Hikyo stop_grace_period must remain 30s")
if root_key_stage.get("command") != [
    "sh", "-ceu", "install -m 0400 -o 65532 -g 65532 /run/secrets/hikyo-root-key /staged/root-key",
]:
    fail("root-key staging must install an owner-only file for the nonroot Hikyo process")
if hikyo.get("depends_on", {}).get("root-key-stage", {}).get("condition") != "service_completed_successfully":
    fail("Hikyo must wait for root-key staging")
if hikyo.get("secrets"):
    fail("Hikyo must not consume Compose's group-readable file-backed secret directly")
mounts = hikyo.get("volumes", [])
if len(mounts) != 1 or mounts[0].get("target") != "/run/hikyo-root-key" or not mounts[0].get("read_only"):
    fail(f"Hikyo staged root-key mount drifted: {mounts}")
if hikyo.get("ports") != [{"mode": "ingress", "target": 8081, "published": "8081", "protocol": "tcp", "host_ip": "127.0.0.1"}]:
    fail(f"operational port exposure drifted: {hikyo.get('ports')}")
if proxy_service.get("ports") != [{"mode": "ingress", "target": 443, "published": "443", "protocol": "tcp"}]:
    fail(f"public HTTPS exposure drifted: {proxy_service.get('ports')}")
network_configs = [network.get("ipam", {}).get("config", []) for network in rendered.get("networks", {}).values()]
if network_configs != [[{"subnet": "172.30.0.0/24"}]]:
    fail(f"trusted proxy network drifted: {network_configs}")

required = [
    r"(?m)^\s*resolver 127\.0\.0\.11 valid=10s ipv6=off;$",
    r"(?m)^\s*upstream hikyo_backend \{$",
    r"(?m)^\s*zone hikyo_backend 64k;$",
    r"(?m)^\s*server hikyo:8080 resolve;$",
    r"(?m)^\s*listen 443 ssl;$",
    r"(?m)^\s*location = /mcp \{$",
    r"(?m)^\s*client_max_body_size 256k;$",
    r"(?m)^\s*proxy_pass http://hikyo_backend;$",
    r"(?m)^\s*proxy_set_header Host \$host;$",
    r"(?m)^\s*proxy_set_header X-Forwarded-Host \$host;$",
    r"(?m)^\s*proxy_set_header X-Forwarded-Proto https;$",
    r"(?m)^\s*proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;$",
    r"(?m)^\s*proxy_set_header Authorization \$http_authorization;$",
    r"(?m)^\s*proxy_set_header Mcp-Protocol-Version \$http_mcp_protocol_version;$",
    r"(?m)^\s*proxy_set_header Mcp-Method \$http_mcp_method;$",
    r"(?m)^\s*proxy_set_header Mcp-Name \$http_mcp_name;$",
]
for pattern in required:
    if re.search(pattern, proxy) is None:
        fail(f"proxy contract missing pattern {pattern}")
if re.search(r"(?i)\b(ip_hash|sticky|session_affinity|proxy_cookie_path)\b", proxy):
    fail("proxy must not configure session stickiness")
if re.search(r"(?m)^\s*(limit_except|deny)\b", proxy):
    fail("proxy must pass methods through so Hikyo owns the exact 405 response")
if "proxy_pass http://hikyo_backend/" in proxy:
    fail("proxy_pass must preserve the exact /mcp path")
PY

stage_image=$(python3 -c 'import sys,yaml; print(yaml.safe_load(open(sys.argv[1], encoding="utf-8"))["services"]["root-key-stage"]["image"])' "$tmp/rendered.yaml")
proxy_image=$(python3 -c 'import sys,yaml; print(yaml.safe_load(open(sys.argv[1], encoding="utf-8"))["services"]["proxy"]["image"])' "$tmp/rendered.yaml")
docker volume create "$stage_volume" >/dev/null
docker run --rm \
	-v "$tmp/root-key:/run/secrets/hikyo-root-key:ro" \
	-v "$stage_volume:/staged" \
	"$stage_image" sh -ceu \
	'install -m 0400 -o 65532 -g 65532 /run/secrets/hikyo-root-key /staged/root-key; test "$(stat -c "%a %u %g" /staged/root-key)" = "400 65532 65532"; cmp /run/secrets/hikyo-root-key /staged/root-key' \
	>/dev/null

openssl req -x509 -newkey rsa:2048 -nodes -days 1 -subj '/CN=hikyo.example.com' \
	-keyout "$tmp/tls-key" -out "$tmp/tls-cert" >/dev/null 2>&1
chmod 600 "$tmp/tls-key"
docker run --rm --add-host hikyo:127.0.0.1 \
	-v "$proxy:/etc/nginx/nginx.conf:ro" \
	-v "$tmp/tls-cert:/run/secrets/tls-cert:ro" \
	-v "$tmp/tls-key:/run/secrets/tls-key:ro" \
	"$proxy_image" nginx -t >/dev/null

printf 'MCP deployment check: digest-pinned Compose, init, and exact HTTPS proxy contract passed\n'
