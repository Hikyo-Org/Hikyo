#!/usr/bin/env bash
# Host-only operator commands inside an admitted unattended distroless container.
# Starts one signed release (SQLite, or singleton PostgreSQL over verified TLS),
# lets the server enroll and finish its first start, then runs the commands an
# operator runs with `docker exec` or `kubectl exec`: first administrator,
# credential establishment, diagnostics, encrypted backup and readable restore
# drill. No shell, cat or tar exists in the container. Values reach the operator
# through the terminal channel (a pseudo terminal, as kubectl exec -it) or an
# --output-file the command creates inside the container and later commands read
# in place. A separate helper container only prepares the empty volumes before the
# server starts and inspects results afterward; it never carries a secret in.
# Artifact acquisition is the preseeded, authenticated offline cache fixture that
# unattended-docker.sh uses. Every resource this script creates is tracked and
# removed on exit.
set -euo pipefail
engine=${1:-}
case "$engine" in sqlite | postgres) ;; *)
	echo "usage: $0 sqlite|postgres" >&2
	exit 2
	;;
esac
"$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)/configure-docker-mirror.sh"
cd "$(git rev-parse --show-toplevel)"
: "${HIKYO_UNATTENDED_FIXTURE_OUTPUT:?generate signed fixtures first}"
fixture=$HIKYO_UNATTENDED_FIXTURE_OUTPUT
arch=$(cat "$fixture/architecture")
work=$(mktemp -d "${TMPDIR:-/tmp}/hikyo-unattended-operator.XXXXXX")
suffix=$(basename "$work" | tr '[:upper:]' '[:lower:]')
data_volume=$suffix-data
state_volume=$suffix-state
network=$suffix-net
container=$suffix-server
postgres=$suffix-postgres
image=$suffix:a
helper_image=postgres:18@sha256:06cad38a5d9f5d24b4d83d86def30795d5e4b757fedbf5281172b576dedcd941
created_volumes=()
created_network=false
network_flags=()
cleanup() {
	docker rm --force "$container" "$postgres" >/dev/null 2>&1 || true
	for volume in ${created_volumes[@]+"${created_volumes[@]}"}; do docker volume rm "$volume" >/dev/null 2>&1 || true; done
	if [ "$created_network" = true ]; then docker network rm "$network" >/dev/null 2>&1 || true; fi
	docker image rm "$image" >/dev/null 2>&1 || true
	rm -rf "$work"
}
trap cleanup EXIT
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM
fail() {
	echo "unattended operator ($engine): $*" >&2
	if docker inspect "$container" >/dev/null 2>&1; then docker logs --tail 80 "$container" >&2 || true; fi
	exit 1
}
create_volume() {
	if docker volume inspect "$1" >/dev/null 2>&1; then fail "refusing an existing volume $1"; fi
	docker volume create "$1" >/dev/null
	created_volumes+=("$1")
}
create_volume "$data_volume"
create_volume "$state_volume"
helper() {
	docker run --rm --user 0:0 --entrypoint /bin/sh \
		--volume "$state_volume:/state" --volume "$data_volume:/data" \
		--volume "$fixture:/input:ro" "$helper_image" -ceu "$1"
}

mkdir -p "$work/image-root/$arch"
cp "$fixture/release-a/hikyo" "$work/image-root/$arch/hikyo"
docker build --platform "linux/$arch" --file Dockerfile.release --tag "$image" "$work" >/dev/null

helper 'mkdir -p /state/unattended/downloads /data/ops; cp /input/root.key /state/root.key; cp /input/descriptor-a.json /state/unattended/image.json
	chmod 700 /state /state/unattended /state/unattended/downloads /data /data/ops; chmod 600 /state/root.key /state/unattended/image.json
	chown -R 65532:65532 /state /data'

database=sqlite:/var/lib/hikyo/data/hikyo.db
environment=()
ca=/var/lib/hikyo/upgrade/ca.crt
drill_target=(--target-sqlite /var/lib/hikyo/data/ops/drill.db)
if [ "$engine" = postgres ]; then
	docker network create "$network" >/dev/null
	created_network=true
	network_flags=(--network "$network")
	mkdir -p "$work/tls"
	openssl req -x509 -newkey rsa:2048 -nodes -days 1 -keyout "$work/tls/ca.key" -out "$work/tls/ca.crt" -subj '/CN=Hikyo operator acceptance CA' >/dev/null 2>&1
	openssl req -newkey rsa:2048 -nodes -keyout "$work/tls/tls.key" -out "$work/tls/server.csr" -subj '/CN=postgres' >/dev/null 2>&1
	printf 'subjectAltName=DNS:postgres\nextendedKeyUsage=serverAuth\n' >"$work/tls/server.ext"
	openssl x509 -req -days 1 -sha256 -in "$work/tls/server.csr" -CA "$work/tls/ca.crt" -CAkey "$work/tls/ca.key" -CAcreateserial \
		-extfile "$work/tls/server.ext" -out "$work/tls/tls.crt" >/dev/null 2>&1
	tls_volume=$suffix-pgtls
	create_volume "$tls_volume"
	docker run --rm --user 0:0 --entrypoint /bin/sh --volume "$tls_volume:/tls" --volume "$work/tls:/in:ro" "$helper_image" -ceu \
		'cp /in/tls.crt /in/tls.key /tls/; chown 999:999 /tls/tls.crt /tls/tls.key; chmod 600 /tls/tls.key; chmod 644 /tls/tls.crt'
	docker run --rm --user 0:0 --entrypoint /bin/sh --volume "$state_volume:/state" --volume "$work/tls:/in:ro" "$helper_image" -ceu \
		'cp /in/ca.crt /state/ca.crt; chown 65532:65532 /state/ca.crt; chmod 644 /state/ca.crt'
	docker run --detach --name "$postgres" --network "$network" --network-alias postgres \
		--env POSTGRES_USER=hikyo --env POSTGRES_PASSWORD=hikyo --env POSTGRES_DB=hikyo \
		--volume "$tls_volume:/etc/postgres-tls:ro" "$helper_image" \
		-c ssl=on -c ssl_cert_file=/etc/postgres-tls/tls.crt -c ssl_key_file=/etc/postgres-tls/tls.key >/dev/null
	for ((attempt = 0; attempt < 60; attempt++)); do
		# -h 127.0.0.1 forces TCP: the bootstrap server listens on its socket only.
		if docker exec "$postgres" pg_isready -h 127.0.0.1 -U hikyo -d hikyo >/dev/null 2>&1; then break; fi
		sleep 1
	done
	docker exec "$postgres" pg_isready -h 127.0.0.1 -U hikyo -d hikyo >/dev/null || fail 'PostgreSQL did not become ready'
	docker exec "$postgres" createdb -U hikyo hikyo_scratch
	docker exec "$postgres" createdb -U hikyo hikyo_drill
	dsn() { printf 'postgres://hikyo:hikyo@postgres:5432/%s?sslmode=verify-full&sslrootcert=%s' "$1" "$ca"; }
	database=$(dsn hikyo)
	environment=(--env "HIKYO_UPGRADE_SCRATCH_POSTGRES_DSN=$(dsn hikyo_scratch)")
	helper "printf '%s' '$(dsn hikyo_drill)' >/data/ops/drill-dsn; chmod 600 /data/ops/drill-dsn; chown 65532:65532 /data/ops/drill-dsn"
	drill_target=(--target-postgres-dsn-file /var/lib/hikyo/data/ops/drill-dsn)
fi

run_server() {
	local name=$1
	shift
	docker run --detach --name "$name" --read-only --cap-drop ALL \
		${network_flags[@]+"${network_flags[@]}"} \
		--tmpfs /tmp:rw,nosuid,nodev,size=128m \
		--volume "$data_volume:/var/lib/hikyo/data" \
		--volume "$state_volume:/var/lib/hikyo/upgrade" \
		--volume "$fixture/public:/fixtures:ro" \
		--env "HIKYO_DB=$database" \
		--env HIKYO_STATE_DIR=/var/lib/hikyo/data \
		--env HIKYO_UPGRADE_STATE_DIR=/var/lib/hikyo/upgrade \
		--env HIKYO_UPGRADE_UNATTENDED=true \
		--env HIKYO_ROOT_KEY_FILE=/var/lib/hikyo/upgrade/root.key \
		--env HIKYO_EXTERNAL_ORIGIN=https://unattended.fixture.invalid \
		--env HIKYO_TRUSTED_PROXY_CIDRS=127.0.0.1/32 \
		--env HIKYO_UPDATE_CHANNEL=off \
		${environment[@]+"${environment[@]}"} \
		"$@" >/dev/null
}
run_server "$container" --publish 127.0.0.1::8080 --publish 127.0.0.1::8081 "$image" server \
	--listen=0.0.0.0:8080 --operational-listen=0.0.0.0:8081
public_port=
ops_port=
wait_ready() {
	local attempt
	public_port=$(docker port "$container" 8080/tcp | head -n1)
	public_port=${public_port##*:}
	ops_port=$(docker port "$container" 8081/tcp | head -n1)
	ops_port=${ops_port##*:}
	for ((attempt = 0; attempt < 120; attempt++)); do
		if curl --fail --silent --max-time 2 "http://127.0.0.1:$ops_port/readyz" >/dev/null; then return; fi
		[ "$(docker inspect --format '{{.State.Running}}' "$container")" = true ] || break
		sleep 1
	done
	fail "server did not become ready ($1)"
}
wait_ready 'first start'

op() { docker exec "$container" /usr/local/bin/hikyo "$@"; }
# Captured stderr of a command that must fail, with the exit status checked.
refused() {
	local want=$1 output
	shift
	if output=$("$@" 2>&1); then fail "expected a refusal containing '$want', but the command succeeded"; fi
	grep -Fq -- "$want" <<<"$output" || fail "refusal did not contain '$want': $output"
}
snapshot() {
	# Every file under the upgrade state (journal, image record, pin, enrollment, custody
	# vault and record), by content, owner and mode, except the download cache.
	helper "cd /state && find . -path ./unattended/downloads -prune -o -type f -print0 | sort -z | xargs -0 sha256sum; find . -path ./unattended/downloads -prune -o -printf '%m %u %p\\n' | sort"
}
control() {
	if [ "$engine" = sqlite ]; then
		docker run --rm --user 65532:65532 --entrypoint /tools/inspect --volume "$data_volume:/data" --volume "$work/tools:/tools:ro" "$helper_image" /data/hikyo.db
	else
		docker exec "$postgres" psql -U hikyo -d hikyo -Atc "SELECT json_build_object('instance',c.instance_id,'applied',c.applied_json::json,'generation',c.generation,'maintenance',(c.maintenance=1),'pending',p.operation_json::json) FROM upgrade_control c JOIN upgrade_pending p USING(singleton)"
	fi
}
if [ "$engine" = sqlite ]; then
	mkdir -p "$work/tools"
	CGO_ENABLED=0 GOOS=linux GOARCH="$arch" go build -trimpath -o "$work/tools/inspect" ./scripts/ci/unattendedfixture/inspect
fi
control_json() { control | jq -c .; }
before_state=$(snapshot)
before_control=$(control_json)
printf 'unattended operator (%s): server admitted; running operator commands\n' "$engine"

# tty_exec runs a command with a pseudo terminal, answers its one y/N prompt, and prints
# the terminal transcript. It is the operator typing "y" at kubectl exec -it.
tty_exec() {
	python3 -I - "$@" <<'PY'
import os, pty, select, subprocess, sys, time
master, slave = pty.openpty()
child = subprocess.Popen(["docker", "exec", "-it", *sys.argv[1:]], stdin=slave, stdout=slave, stderr=slave, close_fds=True)
os.close(slave)
seen, answered, deadline = b"", False, time.time() + 60
while time.time() < deadline:
    ready, _, _ = select.select([master], [], [], 1)
    if ready:
        try:
            chunk = os.read(master, 4096)
        except OSError:
            break
        if not chunk:
            break
        seen += chunk
        if not answered and b"[y/N]" in seen:
            os.write(master, b"y\n")
            answered = True
    elif child.poll() is not None:
        break
try:
    child.wait(timeout=10)
except subprocess.TimeoutExpired:
    child.kill()
    child.wait()
sys.stdout.write(seen.decode(errors="replace").replace("\r", ""))
sys.exit(child.returncode)
PY
}
# 1. First administrator over the terminal channel: the authority is displayed on the
# terminal once and never reaches stdout, a file or a log.
# Without a terminal or a destination, creation must refuse before it creates anything, so
# the terminal create below still succeeds.
if output=$(op admin create --username operator 2>&1); then fail 'admin create succeeded without a disclosure channel'; fi
case "$output" in *hik_*) fail 'admin create disclosed to a non-terminal stdout' ;; esac
created=$(tty_exec "$container" /usr/local/bin/hikyo admin create --username operator) || fail 'admin create on a terminal failed (transcript withheld: it may hold the authority)'
authority=$(printf '%s\n' "$created" | grep -Eo 'hik_[A-Za-z0-9_-]+' | head -n1)
principal=$(printf '%s\n' "$created" | sed -n 's/.*principal \([^)]*\)).*/\1/p' | head -n1)
[ -n "$authority" ] && [ -n "$principal" ] || fail 'admin create did not display an authority and principal on the terminal'
op admin grant --principal "$principal" --capability instance-config >/dev/null 2>&1 || fail 'admin grant failed'
refused 'conflict' op admin create --username second --output-file /var/lib/hikyo/data/ops/second
wait_ready 'after the first administrator'

# 2. Establish the credential and sign in through the public listener.
base=http://127.0.0.1:$public_port
password=operator-acceptance-password-long-enough
jar=$work/jar
code=$(curl -sS -o "$work/establish.json" -w '%{http_code}' -H 'Content-Type: application/json' \
	--data "$(jq -cn --arg a "$authority" --arg p "$password" '{authority:$a,password:$p}')" "$base/api/v1/auth/credential/establish")
case "$code" in 2??) ;; *) fail "credential establishment answered $code: $(head -c 300 "$work/establish.json")" ;; esac
cookie_header() { printf 'Cookie: __Host-hikyo=%s; __Host-hikyo-csrf=%s' "$(awk '$6=="__Host-hikyo"{print $7}' "$1")" "$(awk '$6=="__Host-hikyo-csrf"{print $7}' "$1")"; }
api() {
	curl -sS -X "$1" -H "$(cookie_header "$jar")" -H "X-Hikyo-CSRF: $(awk '$6=="__Host-hikyo-csrf"{print $7}' "$jar")" \
		-H 'Content-Type: application/json' ${3:+--data "$3"} "$base/api/v1$2"
}
laststep=0
totp() {
	python3 -I - "$(cat "$work/totp-uri")" <<'PY'
import base64, hashlib, hmac, struct, sys, time, urllib.parse
query = urllib.parse.parse_qs(urllib.parse.urlparse(sys.argv[1]).query)
secret = query["secret"][0].upper()
key = base64.b32decode(secret + "=" * (-len(secret) % 8))
period, digits = int(query.get("period", ["30"])[0]), int(query.get("digits", ["6"])[0])
digest = hmac.new(key, struct.pack(">Q", int(time.time()) // period), hashlib.sha1).digest()
offset = digest[-1] & 15
print(str((struct.unpack(">I", digest[offset:offset + 4])[0] & 0x7FFFFFFF) % 10**digits).zfill(digits))
PY
}
mfa_login() {
	rm -f "$jar"
	while [ $(($(date +%s) / 30)) -le "$laststep" ]; do sleep 1; done
	curl -sS -c "$jar" -o "$work/login.json" -H 'Content-Type: application/json' \
		--data "{\"username\":\"operator\",\"password\":\"$password\",\"artifact\":\"browser\"}" "$base/api/v1/auth/local/login"
	curl -sS -b "$jar" -c "$jar" -o /dev/null -H 'Content-Type: application/json' --data "{\"code\":\"$(totp)\"}" \
		"$base/api/v1/auth/login/challenge/$(jq -r .challenge_id "$work/login.json")/totp"
	laststep=$(($(date +%s) / 30))
}
curl -sS -c "$jar" -o /dev/null -H 'Content-Type: application/json' \
	--data "{\"username\":\"operator\",\"password\":\"$password\",\"artifact\":\"browser\"}" "$base/api/v1/auth/local/login"
csrf=$(awk '$6=="__Host-hikyo-csrf"{print $7}' "$jar")
[ -n "$csrf" ] || fail 'the established credential did not sign in'
curl -fsS -H "$(cookie_header "$jar")" -H "X-Hikyo-CSRF: $csrf" -H 'Content-Type: application/json' --data "{\"password\":\"$password\"}" \
	"$base/api/v1/auth/totp/enrol/start" | jq -r .otpauth_uri >"$work/totp-uri"
curl -fsS -o /dev/null -H "$(cookie_header "$jar")" -H "X-Hikyo-CSRF: $csrf" -H 'Content-Type: application/json' --data "{\"code\":\"$(totp)\"}" \
	"$base/api/v1/auth/totp/enrol/confirm"
laststep=$(($(date +%s) / 30))
mfa_login
org=$(api POST /orgs '{"name":"operator-org"}' | jq -er .id)
mfa_login
project=$(api POST "/orgs/$org/projects" '{"name":"operator-project"}' | jq -er .id)
env_id=$(api POST "/orgs/$org/projects/$project/environments" '{"name":"production"}' | jq -er .id)
api POST "/orgs/$org/projects/$project/keys" '{"name":"OPERATOR_SECRET","classification":"secret","declaration":{"rule":{"type":"string","allow_empty":true}}}' | jq -e .name >/dev/null
version=$(api PUT "/orgs/$org/projects/$project/environments/$env_id/values/OPERATOR_SECRET" '{"value":"readable-after-restore"}' | jq -er .version_id)
api POST "/orgs/$org/projects/$project/environments/$env_id/publish" "{\"version_ids\":[\"$version\"]}" | jq -e . >/dev/null || fail 'publishing the secret failed'
printf 'unattended operator (%s): first administrator established, one secret stored\n' "$engine"

# 3. Diagnostics: host-only status reads.
op admin config status >"$work/config-status.txt" 2>&1 || fail "admin config status: $(cat "$work/config-status.txt")"
op restore status >"$work/restore-status.txt" 2>&1 || fail "restore status: $(cat "$work/restore-status.txt")"

# 4. Backup identity over the file channel: the identity is created by the command in an owner-only file inside the
# container, and the drill below reads it in place.
case "$(op backup keygen 2>&1 || true)" in *AGE-SECRET-KEY-*) fail 'keygen disclosed to a non-terminal stdout' ;; esac
keygen=$(op backup keygen --output-file /var/lib/hikyo/data/ops/identity 2>&1) || fail "backup keygen: $keygen"
case "$keygen" in *AGE-SECRET-KEY-*) fail 'keygen printed the identity' ;; esac
recipient=$(printf '%s\n' "$keygen" | grep -Eo 'age1[a-z0-9]+' | head -n1)
[ -n "$recipient" ] || fail "backup keygen did not report a recipient: $keygen"
# shellcheck disable=SC2016 # the expression runs in the helper container
helper 'test "$(stat -c %a /data/ops/identity)" = 600 && test -s /data/ops/identity' || fail 'identity file is not an owner-only, non-empty file'

# 5. Encrypted backup, then the readable restore drill into an empty scratch target.
op backup export --out /var/lib/hikyo/data/ops/backup --recipient "$recipient" >"$work/export.txt" 2>&1 || fail "backup export: $(cat "$work/export.txt")"
archive=$(helper 'ls /data/ops/backup' | head -n1)
[ -n "$archive" ] || fail 'backup export wrote no archive'
helper "head -c 21 /data/ops/backup/$archive" | grep -Fxq 'age-encryption.org/v1' || fail 'backup archive is not an age-encrypted file'
archive=/var/lib/hikyo/data/ops/backup/$archive
op restore drill --from "$archive" --identity-file /var/lib/hikyo/data/ops/identity --root-key-file /var/lib/hikyo/upgrade/root.key \
	--principal "$principal" --project "$org/$project" "${drill_target[@]}" -o json >"$work/drill.out" 2>"$work/drill.err" || fail "restore drill: $(cat "$work/drill.err")"
# The report is the one JSON object on stderr that carries a verdict.
grep -F '"values_readable"' "$work/drill.err" | jq -e '.ok == true and .values_readable == true and .credential == true' >/dev/null ||
	fail "restore drill did not prove readable values: $(cat "$work/drill.err")"
printf 'unattended operator (%s): diagnostics, encrypted backup and readable restore drill passed\n' "$engine"

# 6. Repeated calls ran no second updater and did not change the selected release.
[ "$(snapshot)" = "$before_state" ] || fail "operator commands changed the unattended state files"$'\n'"$(snapshot)"$'\n'"$before_state"
[ "$(control_json)" = "$before_control" ] || fail "operator commands changed the installation: $(control_json) != $before_control"
processes=$(docker top "$container" | grep -c '/usr/local/bin/hikyo' || true)
[ "$processes" = 1 ] || fail "expected the server to be the only hikyo process, found $processes"
curl --fail --silent --max-time 2 "http://127.0.0.1:$ops_port/readyz" >/dev/null || fail 'the server stopped being ready'

# 7. Refusals leave state untouched and the server serving.
refused 'HIKYO_UPGRADE_BUNDLE belongs to manual installations' docker exec --env HIKYO_UPGRADE_BUNDLE=/tmp "$container" /usr/local/bin/hikyo admin config status
refused 'HIKYO_UPGRADE_OPERATOR_PUBLIC_KEY belongs to manual installations' docker exec --env HIKYO_UPGRADE_OPERATOR_PUBLIC_KEY=/tmp/x "$container" /usr/local/bin/hikyo backup keygen
helper 'cp /state/unattended/operator.pub /state/operator.pub.keep; printf replaced >/state/unattended/operator.pub'
refused 'operator pin differs from enrolled custody' op admin config status
helper 'cp /state/operator.pub.keep /state/unattended/operator.pub; rm /state/operator.pub.keep; chown 65532:65532 /state/unattended/operator.pub'
op admin config status >/dev/null 2>&1 || fail 'the restored operator pin was still refused'
[ "$(snapshot)" = "$before_state" ] || fail 'a refusal changed the unattended state files'
curl --fail --silent --max-time 2 "http://127.0.0.1:$ops_port/readyz" >/dev/null || fail 'the server stopped being ready'
# Never enrolled: the same image, an empty installation, no server start.
fresh_state=$suffix-fresh-state
fresh_data=$suffix-fresh-data
create_volume "$fresh_state"
create_volume "$fresh_data"
docker run --rm --user 0:0 --entrypoint /bin/sh --volume "$fresh_state:/state" --volume "$fresh_data:/data" --volume "$fixture:/input:ro" "$helper_image" -ceu \
	'cp /input/root.key /state/root.key; chmod 700 /state /data; chmod 600 /state/root.key; chown -R 65532:65532 /state /data'
refused 'operator commands never enroll' docker run --rm --read-only --cap-drop ALL --tmpfs /tmp \
	--volume "$fresh_data:/var/lib/hikyo/data" --volume "$fresh_state:/var/lib/hikyo/upgrade" \
	--env HIKYO_DB=sqlite:/var/lib/hikyo/data/hikyo.db --env HIKYO_UPGRADE_STATE_DIR=/var/lib/hikyo/upgrade \
	--env HIKYO_UPGRADE_UNATTENDED=true --env HIKYO_ROOT_KEY_FILE=/var/lib/hikyo/upgrade/root.key \
	"$image" admin create --username nobody --output-file /var/lib/hikyo/data/authority
[ -z "$(docker run --rm --entrypoint /bin/sh --volume "$fresh_data:/data:ro" --volume "$fresh_state:/state:ro" "$helper_image" -c 'ls /data /state | grep -v "^root.key$" | grep -v "^/"')" ] || fail 'the refused command created state on a fresh installation'

printf 'unattended operator (%s): operator commands passed and refusals changed nothing\n' "$engine"
