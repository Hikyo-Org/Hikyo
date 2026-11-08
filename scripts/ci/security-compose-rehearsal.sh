#!/bin/sh
# Real Compose replacements through the real securitydeployment.Executor, all
# inside one privileged docker:dind container, so cleanup is one container and
# one volume. Follows the Compose section of docs/operations/security-maintenance.md.
#
# Deliberately not real: the feed and coordinator driver (the harness calls the
# Executor with the fixture's exact identity), GitHub (a local server under a
# private CA answers at GitHub's names, since `stage` has no offline path), and
# the stop failure in step 5. Each image gains the CA, and compose.yaml gains
# extra_hosts. Release verification is the product's, under ephemeral fixture trust.
#
# Generate the fixture first (the `ui` tag needs internal/webui/dist to exist):
#   HIKYO_UNATTENDED_FIXTURE_OUTPUT=/abs/fixture HIKYO_UNATTENDED_FIXTURE_GOARCH=arm64 \
#     go test -count=1 -run '^TestWriteUnattendedContainerFixture$' ./scripts/ci/unattendedfixture
# Exits nonzero while a DEFECT line is printed.
#
# `security-compose-rehearsal.sh feed` instead proves the whole path with no bypass:
# real `feed` commands publish an advisory under ephemeral feed trust, and a real
# `security-maintenance run --once` as root reads it through the engine and driver,
# replaces the server and moves its own binary. It builds its own fixture (the `ui`
# tag needs internal/webui/dist; a placeholder is created and removed if absent).
# Substituted: GitHub, ghcr.io and hikyo.app answer from one local server under a
# private CA, and the policy has the nightly channel and a closed window.
# `security-compose-rehearsal.sh rollback` is the feed mode's setup with the automatic rollback to
# the fenced snapshot driven for real: a real `security-maintenance run --once` as root, real Compose,
# one fresh release A with data per class. The failure is forced from outside the product, by throttling
# the target at write-intent and then corrupting its key wrapper (post-write health failure), stopping its
# processes (a hang), killing its container (a failure after the final gate), corrupting the archive (a
# rollback that cannot finish), or killing the coordinator. Classes: 1 restore-required, 2 hang until the
# deadline, 3 refused before any write, 4m/4u coordinator killed then a later run under manual policy /
# with the feed unreachable, 5 third rollback then retry_exhausted, 6 the rollback fails, 7 past the final
# gate. Every assertion prints an `ASSERT ok:` line or a DEFECT. HIKYO_REHEARSAL_CLASSES="1 5" picks
# classes; HIKYO_REHEARSAL_KEEP=1 keeps the dind container and inputs, to rerun with
# `docker exec --env MODE=rollback --env CLASSES=... --env ARCH=... NAME-dind sh /work/THIS-SCRIPT inner`.
# `security-compose-rehearsal.sh native` runs the native runbook from nothing under real
# systemd 257 as PID 1 in a privileged container (no dind): a package-less /usr/bin/hikyo
# (the signed fixture binary stands in for the package), the runbook's user, directories,
# unit and configuration, then install-systemd.sh to enrolled, the real timer firing
# `run --once`, and a critical advisory carrying the installation from release A to B with
# its data. The container is restarted (a real reboot of its PID 1) after enrolling and
# again after a forced failure of the host's first update, whose backup export is killed
# before write-intent. Resources are named hikyo-1068-native-*. Not wired into CI.
# HIKYO_REHEARSAL_ENGINE picks the engine CLI that runs the dind container, which
# must be privileged and rootful, e.g. "podman --connection podman-machine-default-root".
# shellcheck disable=SC2016,SC2012 # rollback mode's check evaluates single-quoted expressions on purpose
set -eu

name=hikyo-1068-rehearsal
dind_image=docker:29-dind@sha256:9e79b879b177fbd72c34376a3dd3d91747da5db49e1f560f76aea59561471977

engine=${HIKYO_REHEARSAL_ENGINE:-docker}
if [ "${1:-}" = native ]; then
	nname=hikyo-1068-native
	systemd_base=debian:trixie-slim@sha256:d7e12182ce18b85b93007c1dedf31f2d29e01ccf3182cc4017c709b6259bc132
	cd "$(git rev-parse --show-toplevel)"
	work=$(mktemp -d "${TMPDIR:-/tmp}/$nname.XXXXXX")
	arch=$(go env GOARCH)
	placeholder=false
	[ -d internal/webui/dist ] || placeholder=true
	pulled=false
	$engine image inspect "$systemd_base" >/dev/null 2>&1 || pulled=true
	# shellcheck disable=SC2329 # invoked by the trap below
	cleanup() {
		if [ "$placeholder" = true ]; then rm -rf internal/webui/dist; fi
		# HIKYO_REHEARSAL_KEEP=1 keeps the container and inputs for inspection; remove them by name.
		if [ "${HIKYO_REHEARSAL_KEEP:-}" = 1 ]; then echo "kept: container $nname-systemd, image $nname-systemd:run, inputs $work" >&2; return; fi
		$engine rm --force --volumes "$nname-systemd" >/dev/null 2>&1 || true
		$engine image rm "$nname-systemd:run" >/dev/null 2>&1 || true
		if [ "$pulled" = true ]; then $engine image rm "$systemd_base" >/dev/null 2>&1 || true; fi
		rm -rf "$work"
	}
	trap cleanup EXIT
	trap 'exit 129' HUP
	trap 'exit 130' INT
	trap 'exit 143' TERM
	CGO_ENABLED=0 GOOS=linux GOARCH="$arch" go build -p 2 -trimpath -o "$work/harness" ./scripts/ci/securitycomposerehearsal
	if [ "$placeholder" = true ]; then mkdir internal/webui/dist && echo '<!doctype html>placeholder' >internal/webui/dist/index.html; fi
	mkdir "$work/ca"
	go run ./scripts/ci/securitycomposerehearsal ca "$work/ca"
	# A published release always carries its signed image, and the feed publisher refuses a target without one.
	HIKYO_UNATTENDED_FIXTURE_OUTPUT=$work/fixture HIKYO_UNATTENDED_FIXTURE_GOARCH=$arch \
		HIKYO_UNATTENDED_FIXTURE_IMAGE_REPOSITORY=ghcr.io/$nname/hikyo HIKYO_UNATTENDED_FIXTURE_CA=$work/ca/ca.crt \
		go test -p 2 -parallel 2 -count=1 -run '^TestWriteUnattendedContainerFixture$' ./scripts/ci/unattendedfixture
	cp -R install "$0" "$work/"
	cat >"$work/Dockerfile" <<DOCKERFILE
FROM $systemd_base
RUN apt-get update && apt-get install -y --no-install-recommends systemd systemd-sysv dbus ca-certificates sudo jq curl sqlite3 openssl iproute2 netcat-openbsd procps \
	&& rm -rf /var/lib/apt/lists/*
ENV container=docker
STOPSIGNAL SIGRTMIN+3
CMD ["/bin/sh", "-c", "mount --make-rshared /run && exec /sbin/init"]
DOCKERFILE
	$engine build --quiet --tag "$nname-systemd:run" "$work" >/dev/null
	# Real systemd needs writable cgroups and mount namespaces; nothing from the host is bound but read-only inputs.
	$engine run --detach --privileged --cgroupns=private --systemd=always --name "$nname-systemd" \
		--tmpfs /run --tmpfs /run/lock --tmpfs /tmp \
		--volume "$work:/work:ro" --volume "$work/fixture:/fixture:ro" "$nname-systemd:run" >/dev/null
	attempt=0
	until $engine exec "$nname-systemd" systemctl show --property=Version >/dev/null 2>&1; do
		attempt=$((attempt + 1))
		[ "$attempt" -lt 60 ] || { echo 'systemd did not start' >&2; exit 1; }
		sleep 1
	done
	# Each stage is detached, so that losing this terminal does not kill a rehearsal that waits minutes
	# for timers. A stage ends in the container's tmpfs, which the restart between stages clears.
	shown=0
	follow() {
		$engine exec "$nname-systemd" tail -c "+$((shown + 1))" /run/native-flow.log >"$work/chunk" 2>/dev/null || true
		tee -a "$work/native.log" <"$work/chunk"
		shown=$((shown + $(wc -c <"$work/chunk")))
	}
	native_stage() {
		shown=0
		$engine exec --detach --env "ARCH=$arch" --env MODE=native --env "STAGE=$1" "$nname-systemd" \
			sh -c "sh /work/$(basename "$0") inner >/run/native-flow.log 2>&1; echo \$? >/run/native-flow.exit"
		until $engine exec "$nname-systemd" test -e /run/native-flow.exit; do
			follow
			sleep 5
		done
		follow
		return "$($engine exec "$nname-systemd" cat /run/native-flow.exit)"
	}
	native_reboot() {
		echo "== reboot: restarting the container, which stops and starts systemd as PID 1"
		$engine restart "$nname-systemd" >/dev/null
		attempt=0
		until $engine exec "$nname-systemd" systemctl show --property=Version >/dev/null 2>&1; do
			attempt=$((attempt + 1))
			[ "$attempt" -lt 60 ] || { echo 'systemd did not come back' >&2; exit 1; }
			sleep 1
		done
	}
	# Exit 2 is a stage that finished with defects; anything else nonzero stops the rehearsal.
	for stage in 1 2 3; do
		[ "$stage" = 1 ] || native_reboot
		code=0
		native_stage "$stage" || code=$?
		[ "$code" = 0 ] || [ "$code" = 2 ] || exit "$code"
	done
	defects=$(grep -c '^DEFECT: ' "$work/native.log" || true)
	echo "== native: finished with $defects defect(s)"
	[ "$defects" = 0 ]
	exit
fi

if [ "${1:-}" != inner ]; then
	mode=${1:-}
	cd "$(git rev-parse --show-toplevel)"
	work=$(mktemp -d "${TMPDIR:-/tmp}/$name.XXXXXX")
	if [ "$mode" = feed ] || [ "$mode" = rollback ]; then
		arch=$(go env GOARCH)
		fixture=$work/fixture
		placeholder=false
		[ -d internal/webui/dist ] || placeholder=true
	else
		: "${HIKYO_UNATTENDED_FIXTURE_OUTPUT:?generate signed fixtures first}"
		fixture=$HIKYO_UNATTENDED_FIXTURE_OUTPUT
		arch=$(cat "$fixture/architecture")
	fi
	pulled=false
	$engine image inspect "$dind_image" >/dev/null 2>&1 || pulled=true
	# shellcheck disable=SC2329 # invoked by the trap below
	cleanup() {
		# HIKYO_REHEARSAL_KEEP=1 keeps the dind container and inputs for a rerun of `inner`; remove them by name.
		if [ "${HIKYO_REHEARSAL_KEEP:-}" = 1 ]; then echo "kept: container $name-dind, volume $name-dind, inputs $work" >&2; return; fi
		$engine rm --force --volumes "$name-dind" >/dev/null 2>&1 || true
		$engine volume rm "$name-dind" >/dev/null 2>&1 || true
		if [ "$pulled" = true ]; then $engine image rm "$dind_image" >/dev/null 2>&1 || true; fi
		if [ "${placeholder:-false}" = true ]; then rm -rf internal/webui/dist; fi
		rm -rf "$work"
	}
	trap cleanup EXIT
	trap 'exit 129' HUP
	trap 'exit 130' INT
	trap 'exit 143' TERM
	CGO_ENABLED=0 GOOS=linux GOARCH="$arch" go build -p 2 -trimpath -o "$work/harness" ./scripts/ci/securitycomposerehearsal
	if [ "$mode" = feed ] || [ "$mode" = rollback ]; then
		if [ "$placeholder" = true ]; then mkdir internal/webui/dist && echo '<!doctype html>placeholder' >internal/webui/dist/index.html; fi
		mkdir "$work/ca"
		go run ./scripts/ci/securitycomposerehearsal ca "$work/ca"
		HIKYO_UNATTENDED_FIXTURE_OUTPUT=$fixture HIKYO_UNATTENDED_FIXTURE_GOARCH=$arch \
			HIKYO_UNATTENDED_FIXTURE_IMAGE_REPOSITORY=ghcr.io/$name/hikyo HIKYO_UNATTENDED_FIXTURE_CA=$work/ca/ca.crt \
			go test -p 2 -parallel 2 -count=1 -run '^TestWriteUnattendedContainerFixture$' ./scripts/ci/unattendedfixture
	fi
	cp -R install Dockerfile.release "$0" "$work/"
	$engine run --detach --privileged --name "$name-dind" --env DOCKER_TLS_CERTDIR= \
		--volume "$name-dind:/var/lib/docker" --volume "$work:/work:ro" \
		--volume "$fixture:/fixture:ro" "$dind_image" >/dev/null
	attempt=0
	until $engine exec "$name-dind" docker version >/dev/null 2>&1; do
		attempt=$((attempt + 1))
		[ "$attempt" -lt 60 ] || { echo 'dind did not start' >&2; exit 1; }
		sleep 2
	done
	$engine exec --env "ARCH=$arch" --env "MODE=$mode" --env "CLASSES=${HIKYO_REHEARSAL_CLASSES:-}" "$name-dind" sh "/work/$(basename "$0")" inner
	exit 0
fi

H=/work/harness
CFG=/etc/hikyo/security-maintenance.json
F=/fixture
D=/opt/rehearsal
STATE=/var/lib/hikyo-security-maintenance/deployment
JOURNAL=$STATE/deployment.json
DEFECTS=0
MODE=${MODE:-}
FEEDLIKE=false
case "$MODE" in feed | rollback) FEEDLIKE=true ;; esac
CHANNEL=stable WINDOW_START=0 WINDOW_MINUTES=0
log() { printf '%s\n' "== $*"; }
fail() { printf 'FAIL: %s\n' "$*"; exit 1; }
defect() { printf 'DEFECT: %s\n' "$*"; DEFECTS=$((DEFECTS + 1)); }
img() { awk -v l="$1" '$1==l{print $2}' "$D/digests"; }
dc() { docker compose --project-directory /etc/hikyo --project-name hikyo --file /etc/hikyo/compose.yaml --env-file /etc/hikyo/install.env "$@"; }
summary() { jq -c '{version:.identity.version,generation,phase,maintenance,ready,instance,image}'; }
github_start() { "$H" github "$F" "$D/ca" 11.0.0.2:443 ${FEED:+"$FEED" "$REPO"} >>"$D/github.log" 2>&1 & GITHUB=$!; until nc -z 11.0.0.2 443; do sleep 1; done; }
github_stop() { kill "$GITHUB"; wait "$GITHUB" 2>/dev/null || true; }
env_image() { sed -n 's/^HIKYO_SERVER_IMAGE=//p' /etc/hikyo/install.env; }
set_env_image() { sed -i "s|^HIKYO_SERVER_IMAGE=.*|HIKYO_SERVER_IMAGE=$1|" /etc/hikyo/install.env; }
container_id() { docker inspect --format '{{.Id}}' hikyo-server-1; }
custody() { sha256sum /srv/hikyo/root-key /srv/hikyo/upgrade/unattended/operator.pub | cut -c1-64 | tr '\n' ' '; }
version_of() { jq -r .Identity.version "$F/descriptor-$1.json"; }

# wait_ready LABEL: polls Observe until the fixture's LABEL is healthy, or returns 1.
wait_ready() {
	attempt=0
	while [ "$attempt" -lt 30 ]; do
		attempt=$((attempt + 1))
		if observed=$("$H" observe "$CFG" 2>/dev/null) && printf '%s' "$observed" | jq -e --arg v "$(version_of "$1")" '.identity.version == $v and .ready == true and .phase == "healthy" and .maintenance == false' >/dev/null; then
			printf '%s\n' "$observed" | summary
			return 0
		fi
		sleep 2
	done
	printf 'last observation: %s\n' "$("$H" observe "$CFG" 2>&1 | cut -c1-240)"
	docker logs --tail 2 hikyo-server-1 2>&1 | cut -c1-240
	return 1
}

# The runbook's steps 2 to 5 verbatim, apart from the marked substitutions.
runbook() {
	cd /work
	sudo install -d -m 0755 -o root -g root /etc/hikyo /srv/hikyo
	sudo install -d -m 0700 -o 65532 -g 65532 /srv/hikyo/data /srv/hikyo/upgrade
	sudo install -d -m 0755 -o root -g root /var/lib/hikyo-security-status
	openssl rand -hex 32 | sudo install -m 0400 -o 65532 -g 65532 /dev/stdin /srv/hikyo/root-key
	sudo install -m 0644 install/security-maintenance/compose.yaml /etc/hikyo/compose.yaml
	sudo install -m 0644 install/security-maintenance/compose.json.example /etc/hikyo/security-maintenance.json
	# Substitutions: repository, Docker path, GitHub names.
	jq --arg r "$REPO" --arg c "$CHANNEL" --argjson s "$WINDOW_START" --argjson d "$WINDOW_MINUTES" \
		'.deployment.image_repository=$r | .deployment.compose.docker="/usr/local/bin/docker" | .policy.channel=$c | .policy.window_start_minute=$s | .policy.window_duration_minutes=$d' "$CFG" >/tmp/config.json
	install -m 0644 /tmp/config.json "$CFG"
	sed -i 's|^    restart: unless-stopped$|&\n    extra_hosts: ["api.github.com:11.0.0.2", "github.com:11.0.0.2", "raw.githubusercontent.com:11.0.0.2"]|' /etc/hikyo/compose.yaml
	# The engine VM has 2 GiB, and a server verifying a release holds it several times over.
	[ "$FEEDLIKE" != true ] || sed -i 's|^      HIKYO_UPGRADE_UNATTENDED: "true"$|&\n      GOMEMLIMIT: 350MiB|' /etc/hikyo/compose.yaml
	sudo install -m 0600 -o root -g root /dev/null /etc/hikyo/install.env
	sudo tee /etc/hikyo/install.env >/dev/null <<ENV
HIKYO_SERVER_IMAGE=$(img a)
HIKYO_EXTERNAL_ORIGIN=https://hikyo.example.com
HIKYO_TRUSTED_PROXY_CIDRS=127.0.0.1/32
HIKYO_UPGRADE_INSTALLATION_DIR=/srv/hikyo/upgrade
HIKYO_SERVER_DATA_DIR=/srv/hikyo/data
HIKYO_SERVER_ROOT_KEY_FILE=/srv/hikyo/root-key
ENV
	# The driver's configuredSecurityEngine would create these; it is bypassed.
	[ "$FEEDLIKE" = true ] || install -d -m 0700 /var/lib/hikyo-security-maintenance "$STATE"
	sudo docker compose --project-directory /etc/hikyo --project-name hikyo \
		--file /etc/hikyo/compose.yaml --env-file /etc/hikyo/install.env up --detach >/dev/null 2>&1
	sudo docker compose --project-directory /etc/hikyo --project-name hikyo \
		--file /etc/hikyo/compose.yaml --env-file /etc/hikyo/install.env ps
}
fresh() {
	dc down >/dev/null 2>&1
	rm -rf /srv/hikyo /etc/hikyo /var/lib/hikyo-security-maintenance
	runbook
	wait_ready a || fail 'fresh source did not become healthy'
}

# The feed's real publisher commands under ephemeral trust. Shared by the feed and native modes.
feed_tools() {
	KEYS=$D/keys FEED=$D/feed PUBW=$D/publisher LAST='' TUFV=0 NEXT=0
	# GOMEMLIMIT: the engine VM has 2 GiB and these tools hold whole releases in memory.
	pub() { GOMEMLIMIT=400MiB "$F/release-a/hikyo" security-maintenance feed "$@"; }
	coord() { GOMEMLIMIT=400MiB "$COORD" security-maintenance "$@"; }
	iso() { date -u -d "$1" +%Y-%m-%dT%H:%M:%SZ; }
	# advisory ARGS: authors the next catalog from the last one. publish: signs and checks it.
	advisory() {
		NEXT=$((NEXT + 1))
		pub advisory --output "$PUBW/catalog-$NEXT.json" --cache "$PUBW/cache" ${LAST:+--previous-catalog "$LAST"} "$@" || return 1
		LAST=$PUBW/catalog-$NEXT.json
	}
	publish() {
		TUFV=$((TUFV + 1))
		pub publish --catalog "$LAST" --root "$FEED/metadata/1.root.json" --output "$FEED" --cache "$PUBW/cache" --version "$TUFV" \
			--targets-expires "$(iso '+30 days')" --snapshot-expires "$(iso '+20 days')" --timestamp-expires "$(iso '+10 days')" \
			--targets-key "$KEYS/targets.pem" --snapshot-key "$KEYS/snapshot.pem" --timestamp-key "$KEYS/timestamp.pem"
	}
}
feed_init() {
	log feed: ephemeral trust, real init-root, first empty catalog
	install -d -m 0700 "$KEYS" "$FEED" "$FEED/metadata" "$PUBW" "$PUBW/cache"
	for role in root targets snapshot timestamp; do
		openssl genpkey -algorithm ed25519 -out "$KEYS/$role.pem" 2>/dev/null
		openssl pkey -in "$KEYS/$role.pem" -pubout -out "$KEYS/$role.pub"
	done
	jq -n --arg expires "$(iso '+365 days')" --arg keys "$KEYS" '{version: 1, expires: $expires, signer_files: [$keys + "/root.pem"],
		roles: (["root", "targets", "snapshot", "timestamp"] | map({key: ., value: {public_key_files: [$keys + "/" + . + ".pub"], threshold: 1}}) | from_entries)}' >"$KEYS/root-config.json"
	pub init-root --config "$KEYS/root-config.json" --output "$FEED/metadata/1.root.json"
	github_start
	advisory
	publish
	curl -fsS https://hikyo.app/security-feed/metadata/timestamp.json | jq -c '.signed | {version, expires}'
}

# The image's own health check, since the executor's Observe needs the coordinator's state.
wait_healthy() {
	attempt=0
	until [ "$(docker inspect --format '{{.State.Health.Status}}' hikyo-server-1 2>/dev/null)" = healthy ]; do
		attempt=$((attempt + 1))
		[ "$attempt" -lt 90 ] || return 1
		sleep 2
	done
}
run_once() { code=0; coord run --once >"$D/run.json" 2>"$D/run.err" || code=$?; jq -c '{phase,reason,detail,selected:.selected.identity.version,running:.running.identity.version,applicable,coverage}' "$D/run.json" 2>&1 | cut -c1-1500; echo "run --once exit=$code"; return 0; }

# Whole path. No test seam and no direct executor call: the real feed commands publish
# a critical advisory under ephemeral feed keys, and the real root coordinator, a
# stamped release binary, finds it through the feed, verifies, prepares and replaces
# the server, and reports through the status file the server reads.
feed_flow() {
	feed_tools

	feed_init

	log feed: install release A with data, enroll through the real command
	WINDOW_START=$((($(date -u +%-H) * 60 + $(date -u +%-M) + 720) % 1440)) WINDOW_MINUTES=5 CHANNEL=nightly
	runbook
	wait_healthy || fail 'source did not become healthy'
	jq -c '{policy: (.policy | {channel, window_start_minute, window_duration_minutes, urgent_severity}), now_utc_minute: '"$(($(date -u +%-H) * 60 + $(date -u +%-M)))"'}' "$CFG"
	install -m 0644 "$FEED/metadata/1.root.json" /etc/hikyo/security-feed-root.json
	COORD=/usr/bin/hikyo
	install -m 0755 -o root -g root "$F/release-a/hikyo" "$COORD"
	coord check | jq -c '{phase,reason}'
	coord enroll | jq -c '{phase,enrollment:(.enrollment!=null)}'

	log data: first administrator, a TOTP factor and a signed-in session, all in the database
	seed_data
	api_status "$D/jar" | jq -c '{reported,phase:.status.phase,running:.status.running_version}'
	run_once
	[ "$(jq -r .phase "$D/run.json")" = current ] || fail 'empty catalog did not leave the installation current'

	log feed: a moderate advisory fixed by B waits, the maintenance window is closed
	# shellcheck disable=SC2153 # ARCH comes from the environment of the exec that runs this.
	advisory --id HIKYO-REHEARSAL-1 --summary 'moderate fixture flaw' --severity moderate --component hikyo --fixed-version "$(version_of b)" --platform "linux/$ARCH"
	publish
	run_once
	[ "$(jq -r .running.identity.version "$D/run.json")" = "$(version_of a)" ] || defect 'a moderate advisory moved the installation inside a closed window'
	container_a=$(container_id)

	log feed: a critical advisory fixed by B, the urgent bypass, no person involved
	advisory --id HIKYO-REHEARSAL-2 --summary 'critical fixture flaw' --severity critical --component hikyo --fixed-version "$(version_of b)" --platform "linux/$ARCH"
	publish
	run_once
	wait_healthy || fail 'server not healthy after the run'
	log observations after the update
	[ "$(jq -r .running.identity.version "$D/run.json")" = "$(version_of b)" ] || defect 'the critical advisory did not reach B'
	[ "$(docker inspect --format '{{.Config.Image}}' hikyo-server-1)" = "$(img b)" ] || defect 'server container does not run the B image'
	[ "$(container_id)" != "$container_a" ] || defect 'server container was not replaced'
	[ "$(env_image)" = "$(img b)" ] || defect 'install.env does not name B'
	curl -fsS http://127.0.0.1:8080/api/v1/meta | jq -c '{server_version,api_revision}'
	coord status | jq -c '{phase,stale,running:.running.identity.version,healthy:(.running.ready and .running.phase=="healthy"),applicable,coverage,covered_components,operation:(.operation!=null)}'
	jq -c '{phase,running_version,coverage,covered_components,protection_gap,known_until}' /var/lib/hikyo-security-status/status.json
	echo 'old session after the update:'
	curl -sS -H "$(cookie_header "$D/jar")" -o /dev/null -w 'whoami %{http_code}\n' http://127.0.0.1:8080/api/v1/auth/whoami
	mfa_login "$D/jar-after"
	jar=$D/jar-after
	echo "data after the update: orgs $(api GET /orgs | jq -c '[.items[].name]') keys $(api GET "/orgs/$org/projects/$project/keys" | jq -c '[.. | .name? // empty]')"
	api_status "$D/jar-after" | jq -c '{reported,phase:.status.phase,running:.status.running_version,coverage:.status.coverage,gap:.status.protection_gap}'
	[ "$(api_status "$D/jar-after" | jq -S -c .status)" = "$(jq -S -c 'del(.format)' /var/lib/hikyo-security-status/status.json)" ] || defect 'the API did not return the status file'
	echo "coordinator binary: $(sha256sum "$COORD" | cut -c1-16) release B $(sha256sum "$F/release-b/hikyo" | cut -c1-16)"
	echo 'requests the coordinator made through the local names:'
	grep -E 'hikyo.app/security-feed/metadata/timestamp.json|ghcr.io/v2/.*/manifests/sha256|api.github.com/repos/.*/tags/v1.1.0-nightly.2$' "$D/github.log" | sed 's/^[0-9/: ]* //' | sort | uniq -c | head

	log second run is a no-op
	container_b=$(container_id)
	generation=$(jq -r .running.generation "$D/run.json")
	run_once
	[ "$(container_id)" = "$container_b" ] && [ "$(jq -r .running.generation "$D/run.json")" = "$generation" ] || defect 'the second run changed the installation'

	log 'withdrawal: a target is published, then withdrawn before any run'
	advisory --id HIKYO-REHEARSAL-3 --summary 'later critical flaw' --severity critical --component hikyo --fixed-version "$(version_of c)" --platform "linux/$ARCH"
	publish
	coord check | jq -c '{phase,reason,selected:.selected.identity.version}'
	advisory --withdraw-target "$(version_of c)"
	publish
	coord check | jq -c '{phase,reason,selected:.selected.identity.version}'
	run_once
	[ "$(jq -r .selected.identity.version "$D/run.json")" != "$(version_of c)" ] || defect 'a withdrawn target was selected'

	log stale: a refreshed feed whose timestamp lapses
	pub refresh --root "$FEED/metadata/1.root.json" --output "$FEED" --snapshot-expires "$(iso '+50 seconds')" --timestamp-expires "$(iso '+50 seconds')" \
		--snapshot-key "$KEYS/snapshot.pem" --timestamp-key "$KEYS/timestamp.pem"
	run_once
	sleep 55
	run_once
	coord status | jq -c '{phase,stale,reason,running:.running.identity.version}'
	[ "$(container_id)" = "$container_b" ] || defect 'a stale feed changed the installation'
	du -sh /srv/hikyo/upgrade

	log 'first administrator only: no organisation or project, the update that used to strand the install'
	# The stale step let the feed's timestamp lapse, and its refresh took a version
	# of its own. Sign the same catalog again above it.
	TUFV=$((TUFV + 1))
	publish
	reinstall_source
	first_administrator first-admin
	container_a=$(container_id)
	run_once
	wait_healthy || fail 'server not healthy after the first update'
	[ "$(jq -r .running.identity.version "$D/run.json")" = "$(version_of b)" ] || defect 'a first-administrator installation did not take the critical update'
	[ "$(docker inspect --format '{{.Config.Image}}' hikyo-server-1)" = "$(img b)" ] || defect 'server container does not run the B image'
	[ "$(container_id)" != "$container_a" ] || defect 'server container was not replaced'
	docker inspect --format 'restarts={{.RestartCount}} status={{.State.Status}} health={{.State.Health.Status}}' hikyo-server-1
	coord status | jq -e '.operation == null' >/dev/null || defect 'the update left a pinned operation'
	[ ! -e /srv/hikyo/upgrade/unattended/preparation-abandoned.json ] || jq -c '{journal_aborted:.journal.aborted}' /srv/hikyo/upgrade/unattended/preparation-abandoned.json | cut -c1-300

	log 'forced drill failure: a tenant organisation holds a secret and nobody manages its identities'
	forced_source
	started=$(date +%s)
	run_once
	elapsed=$(($(date +%s) - started))
	coord status >"$D/status.json"
	echo "the attempt ended after ${elapsed}s, the coordinator's timeout is 1800s"
	wait_healthy || fail 'source did not come back by itself'
	[ "$(docker inspect --format '{{.Config.Image}}' hikyo-server-1)" = "$(img a)" ] || defect 'the source image was not put back'
	[ "$(env_image)" = "$(img a)" ] || defect 'install.env no longer names the source'
	[ "$elapsed" -lt 900 ] || defect 'the coordinator waited out its timeout'
	[ "$(jq -r .phase "$D/status.json")" = blocked ] && [ "$(jq -r .reason "$D/status.json")" = preapply_refused ] || defect 'the failed attempt was not recorded as refused before the database changed'
	[ "$(jq -r .operation "$D/status.json")" = null ] || defect 'the failed attempt left a pinned operation'
	[ "$(jq -r .retry_attempts "$D/status.json")" = 1 ] || defect 'the failed attempt did not start the retry backoff'
	[ "$(jq -r .retry_disruptive "$D/status.json")" = 1 ] || defect 'the failed attempt was not counted as one that reached the service'
	curl -fsS -o /dev/null http://127.0.0.1:8081/readyz || defect 'the source is not ready'
	docker exec hikyo-server-1 /usr/local/bin/hikyo security-maintenance inspect | jq -c '{version:.identity.version,generation,phase,maintenance,ready,aborted:(.aborted_before_mutation // false)}'
	jq -c '{phase,reason,detail,retry_attempts,retry_disruptive}' "$D/status.json" | cut -c1-400
	echo 'the unfinished journal and its recorded reason:'
	for file in operation preparation-abandoned; do
		[ ! -e "/srv/hikyo/upgrade/unattended/$file.json" ] || echo "$file.json"
	done
	if [ -s /srv/hikyo/upgrade/unattended/preparation-abandoned.json ]; then
		jq -c '{reason,journal_phase:.journal.phase,journal_aborted:.journal.aborted}' /srv/hikyo/upgrade/unattended/preparation-abandoned.json | cut -c1-300
	else
		defect 'the source restart was not recorded'
	fi
	echo "copies of the database left on the state volume: $(attempt_copies)"
	[ -z "$(attempt_copies)" ] || defect 'the failed attempt left its backup or restore scratch on the state volume'
	run_once
	coord status >"$D/status.json"
	[ "$(jq -r .reason "$D/status.json")" = retry_backoff ] || defect 'a second attempt was not held by the retry backoff'
	[ "$(sqlite3 /srv/hikyo/data/hikyo.db "SELECT count(*) FROM value_entries WHERE id='val_forced'")" = 1 ] || defect 'the failed attempt changed the data'

	log 'the same refusal repeats: attempts 2 and 3 each interrupt the service, then automatic attempts stop'
	for nth in 2 3; do
		rewind_backoff
		run_once
		wait_healthy || fail "source did not come back after attempt $nth"
		coord status >"$D/status.json"
		[ "$(jq -r .retry_disruptive "$D/status.json")" = "$nth" ] || defect "attempt $nth was not counted"
	done
	stopped_before=$(docker inspect --format '{{.State.StartedAt}}' hikyo-server-1)
	rewind_backoff
	run_once
	coord status >"$D/status.json"
	[ "$(jq -r .reason "$D/status.json")" = retry_exhausted ] || defect 'a fourth attempt was not refused'
	[ "$code" != 0 ] || defect 'the exhausted target did not fail the scheduled unit'
	[ "$(docker inspect --format '{{.State.StartedAt}}' hikyo-server-1)" = "$stopped_before" ] || defect 'an exhausted target interrupted the service again'
	jq -c '{phase,reason,action,detail}' "$D/status.json" | cut -c1-600
	jq -c '{phase,reason,protection_gap,action}' /var/lib/hikyo-security-status/status.json | cut -c1-400
	[ "$(jq -r .protection_gap /var/lib/hikyo-security-status/status.json)" = true ] || defect 'the exhausted target is not shown as a protection gap'
	echo 'the operator runs the action status names:'
	action=$(jq -r .action "$D/status.json" | sed 's|^hikyo security-maintenance ||')
	# shellcheck disable=SC2086 # the action is a command line
	coord $action | jq -c '{phase,retry:(.retry_target!=null),action}'
	coord status | jq -e '.retry_target == null and .retry_disruptive == null' >/dev/null || defect 'the operator action did not clear the cap'

	log 'coordinator killed while it waits: the next run puts the source back'
	forced_source
	coord run --once >"$D/run.json" 2>"$D/run.err" &
	coordinator=$!
	attempt=0
	until [ "$(docker inspect --format '{{.Config.Image}}' hikyo-server-1 2>/dev/null)" = "$(img b)" ]; do
		attempt=$((attempt + 1))
		[ "$attempt" -lt 1500 ] || fail 'the target never replaced the source'
		sleep 0.2
	done
	pkill -9 -f 'security-maintenance run --once' || true
	wait "$coordinator" || true
	echo 'coordinator killed with the target running'
	attempt=0
	until docker exec hikyo-server-1 /usr/local/bin/hikyo security-maintenance inspect 2>/dev/null | jq -e '.aborted_before_mutation == true' >/dev/null; do
		attempt=$((attempt + 1))
		[ "$attempt" -lt 150 ] || fail 'the target never reported that it stopped before writing'
		sleep 2
	done
	docker exec hikyo-server-1 /usr/local/bin/hikyo security-maintenance inspect | jq -c '{version:.identity.version,generation,phase,maintenance,ready,aborted:.aborted_before_mutation}'
	docker inspect --format 'target container: image={{.Config.Image}} status={{.State.Status}} restarts={{.RestartCount}}' hikyo-server-1
	if curl -fsS -o /dev/null http://127.0.0.1:8081/readyz; then defect 'the service answered with its coordinator dead'; else echo 'the service is down, as it would stay without a later run'; fi
	coord status | jq -c '{phase,reason,operation:(.operation!=null)}'
	run_once
	wait_healthy || fail 'the next run did not bring the source back'
	coord status >"$D/status.json"
	[ "$(docker inspect --format '{{.Config.Image}}' hikyo-server-1)" = "$(img a)" ] || defect 'the next run did not put the source image back'
	[ "$(env_image)" = "$(img a)" ] || defect 'install.env no longer names the source'
	[ "$(jq -r .operation "$D/status.json")" = null ] || defect 'the next run left the operation pinned'
	[ "$(jq -r .retry_disruptive "$D/status.json")" = 1 ] || defect 'the recovered attempt was not counted'
	# The source was put back before anything was written, so no rollback marker was set and the release removes the journal.
	[ ! -e "$JOURNAL" ] || defect "the deployment journal outlived a release before any write: $(jq -c . "$JOURNAL" 2>&1 | cut -c1-200)"
	curl -fsS -o /dev/null http://127.0.0.1:8081/readyz || defect 'the source is not ready after the next run'
	jq -c '{phase,reason,detail,retry_disruptive}' "$D/status.json" | cut -c1-400
	[ -z "$(attempt_copies)" ] || defect 'the recovered attempt left copies on the state volume'
}

# The source with a tenant secret the scratch restore cannot open, so every proof refuses.
forced_source() {
	reinstall_source
	first_administrator first-admin
	dc stop server >/dev/null 2>&1
	sqlite3 /srv/hikyo/data/hikyo.db "PRAGMA foreign_keys=ON;
INSERT INTO orgs (id,name,active,metadata,created_at) VALUES ('org_forced','Forced',TRUE,'{}','2026-09-05T00:00:00Z');
INSERT INTO projects (id,org_id,name,created_at) VALUES ('prj_forced','org_forced','Forced','2026-09-05T00:00:00Z');
INSERT INTO environments (id,org_id,project_id,name,note,created_at,display_order) VALUES ('env_forced','org_forced','prj_forced','forced','','2026-09-05T00:00:00Z',0);
INSERT INTO keys (id,org_id,project_id,name,folder_path,classification,description,deprecated,deprecation_note,declaration,required_mode,forbidden_mode,created_at) VALUES ('key_forced','org_forced','prj_forced','FORCED_SECRET','','secret','',FALSE,'','{}','none','none','2026-09-05T00:00:00Z');
INSERT INTO value_entries (id,org_id,project_id,environment_id,key_id,ciphertext,updated_at,updated_by) VALUES ('val_forced','org_forced','prj_forced','env_forced','key_forced',x'0102030405','2026-09-05T00:00:00Z',(SELECT id FROM principals LIMIT 1));"
	chown -R 65532:65532 /srv/hikyo/data
	dc up --detach >/dev/null 2>&1
	wait_healthy || fail 'source did not restart with the forced secret'
}
# Substituted: the retry backoff counts real time, so the rehearsal moves the deadline instead of the clock.
rewind_backoff() { jq '.retry_after = "2000-01-01T00:00:00Z"' /var/lib/hikyo-security-maintenance/status.json >"$D/status.rewound" && cat "$D/status.rewound" >/var/lib/hikyo-security-maintenance/status.json; }
attempt_copies() { find /srv/hikyo/upgrade/unattended -mindepth 1 -maxdepth 1 -type d \( -name 'backup-*' -o -name 'evidence-*' -o -name 'scratch-*' -o -name '.hikyo-upgrade-evidence-*' \) -exec basename {} \; | tr '\n' ' '; }

# A fresh release A with its own state and coordinator enrollment, for another scenario.
reinstall_source() {
	dc down >/dev/null 2>&1 || true # nothing to remove on the first call
	rm -rf /srv/hikyo /etc/hikyo /var/lib/hikyo-security-maintenance /var/lib/hikyo-security-status
	runbook >/dev/null
	wait_healthy || fail 'fresh source did not become healthy'
	install -m 0644 "$FEED/metadata/1.root.json" /etc/hikyo/security-feed-root.json
	coord enroll | jq -c '{phase,enrollment:(.enrollment!=null)}'
}

mfa_login() {
	rm -f "$1"
	curl -sS -c "$1" -o "$D/login.json" -H 'Content-Type: application/json' --data "{\"username\":\"rehearsal-admin\",\"password\":\"$PASSWORD\",\"artifact\":\"browser\"}" http://127.0.0.1:8080/api/v1/auth/local/login
	wait_step
	curl -sS -b "$1" -c "$1" -o /dev/null -H 'Content-Type: application/json' --data "{\"code\":\"$("$H" totp "$(cat "$D/totp-uri")")\"}" \
		"http://127.0.0.1:8080/api/v1/auth/login/challenge/$(jq -r .challenge_id "$D/login.json")/totp"
	LASTSTEP=$(($(date +%s) / 30))
}
# TOTP codes are single-use per 30 second step.
wait_step() { while [ $(($(date +%s) / 30)) -le "${LASTSTEP:-0}" ]; do sleep 1; done; }
cookie_header() { printf 'Cookie: __Host-hikyo=%s; __Host-hikyo-csrf=%s' "$(awk '$6=="__Host-hikyo"{print $7}' "$1")" "$(awk '$6=="__Host-hikyo-csrf"{print $7}' "$1")"; }
api_status() { curl -sS -H "$(cookie_header "$1")" http://127.0.0.1:8080/api/v1/instance/security-maintenance; }

# The server's operator commands need the unattended runtime's own bundle and operator key.
hx() {
	bundle=$(basename "$(find /srv/hikyo/upgrade/unattended/downloads -maxdepth 1 -name 'bundle-*' | head -1)")
	docker exec -e "HIKYO_UPGRADE_BUNDLE=/var/lib/hikyo-upgrade/unattended/downloads/$bundle" -e HIKYO_UPGRADE_OPERATOR_PUBLIC_KEY=/var/lib/hikyo-upgrade/unattended/operator.pub hikyo-server-1 /usr/local/bin/hikyo "$@"
}
# The first administrator, as `hikyo admin create` leaves it: an instance grant, no organisation.
first_administrator() {
	created=$(hx admin create --username "$1" --dangerously-print 2>&1)
	principal=$(printf '%s' "$created" | sed -n 's/.*principal \([^)]*\)).*/\1/p')
	hx admin grant --principal "$principal" --capability instance-config >/dev/null 2>&1
	# The server reports itself not ready for a moment after the new administrator.
	attempt=0
	until [ "$(docker exec hikyo-server-1 /usr/local/bin/hikyo security-maintenance inspect 2>/dev/null | jq -r .ready)" = true ]; do
		attempt=$((attempt + 1))
		[ "$attempt" -lt 30 ] || fail 'the server did not become ready after the first administrator'
		sleep 2
	done
}
seed_data() {
	PASSWORD=rehearsal-password-long-enough
	first_administrator rehearsal-admin
	sleep 5
	curl -fsS -o /dev/null -H 'Content-Type: application/json' --data "$(jq -cn --arg a "$(printf '%s\n' "$created" | grep '^hik_')" --arg p "$PASSWORD" '{authority:$a,password:$p}')" http://127.0.0.1:8080/api/v1/auth/credential/establish
	jar=$D/jar
	curl -sS -c "$jar" -o /dev/null -H 'Content-Type: application/json' --data "{\"username\":\"rehearsal-admin\",\"password\":\"$PASSWORD\",\"artifact\":\"browser\"}" http://127.0.0.1:8080/api/v1/auth/local/login
	csrf=$(awk '$6=="__Host-hikyo-csrf"{print $7}' "$jar")
	curl -fsS -H "$(cookie_header "$jar")" -H "X-Hikyo-CSRF: $csrf" -H 'Content-Type: application/json' --data "{\"password\":\"$PASSWORD\"}" \
		http://127.0.0.1:8080/api/v1/auth/totp/enrol/start | jq -r .otpauth_uri >"$D/totp-uri"
	curl -fsS -o /dev/null -H "$(cookie_header "$jar")" -H "X-Hikyo-CSRF: $csrf" -H 'Content-Type: application/json' --data "{\"code\":\"$("$H" totp "$(cat "$D/totp-uri")")\"}" http://127.0.0.1:8080/api/v1/auth/totp/enrol/confirm
	LASTSTEP=$(($(date +%s) / 30))
	mfa_login "$jar"
	# An organisation, a project and a principal who manages identities there give the
	# scratch restore drill a credential lifecycle to run. Creating the org ends the session.
	org=$(api POST /orgs '{"name":"rehearsal-org"}' | jq -er .id)
	mfa_login "$jar"
	project=$(api POST "/orgs/$org/projects" '{"name":"rehearsal-project"}' | jq -er .id)
	api POST "/orgs/$org/projects/$project/keys" '{"name":"REHEARSAL_KEY","classification":"config","declaration":{"rule":{"type":"string","allow_empty":true}}}' | jq -c '{name}'
}
# api METHOD PATH [JSON] with the browser session in $jar.
api() {
	curl -sS -X "$1" -H "$(cookie_header "${jar:-$D/jar}")" -H "X-Hikyo-CSRF: $(awk '$6=="__Host-hikyo-csrf"{print $7}' "${jar:-$D/jar}")" \
		-H 'Content-Type: application/json' ${3:+--data "$3"} "http://127.0.0.1:8080/api/v1$2"
}

# The native runbook from nothing: docs/operations/security-maintenance.md, "Native".
native_runbook() {
	cd /work
	# Substitution: the signed package is the fixture's signed release A binary.
	install -m 0755 -o root -g root "$F/release-a/hikyo" /usr/bin/hikyo
	sudo useradd --system --user-group --home-dir /var/lib/hikyo --shell /usr/sbin/nologin hikyo
	sudo install -d -m 0755 -o root -g root /etc/hikyo
	sudo install -d -m 0750 -o hikyo -g hikyo /var/lib/hikyo
	sudo install -d -m 0755 -o root -g root /var/lib/hikyo-security-status
	openssl rand -hex 32 | sudo install -m 0400 -o root -g root /dev/stdin /etc/hikyo/root.key
	sudo tee /etc/hikyo/hikyo.env >/dev/null <<ENV
HIKYO_DB=sqlite:/var/lib/hikyo/hikyo.db
HIKYO_EXTERNAL_ORIGIN=https://hikyo.example.com
HIKYO_TRUSTED_PROXY_CIDRS=127.0.0.1/32
HIKYO_SECURITY_MAINTENANCE_STATUS_FILE=/var/lib/hikyo-security-status/status.json
ENV
	sudo chmod 0600 /etc/hikyo/hikyo.env
	sudo install -m 0644 install/security-maintenance/hikyo.service /etc/systemd/system/hikyo.service
	sudo systemctl daemon-reload
	sudo install -m 0644 install/security-maintenance/native.json.example /etc/hikyo/security-maintenance.json
	# Substitutions: the nightly channel of the fixture releases, a closed window the critical bypass opens, the feed root.
	jq --arg c "$CHANNEL" --argjson s "$WINDOW_START" --argjson d "$WINDOW_MINUTES" '.policy.channel=$c | .policy.window_start_minute=$s | .policy.window_duration_minutes=$d | .timeout_seconds=60' "$CFG" >/tmp/config.json
	install -m 0644 /tmp/config.json "$CFG"
	install -m 0644 "$FEED/metadata/1.root.json" /etc/hikyo/security-feed-root.json
	sudo sh install/security-maintenance/install-systemd.sh
}
native_state() { sha256sum /etc/hikyo/upgrade-keys/operator.age /var/lib/hikyo-upgrade/operator.pub /var/lib/hikyo-upgrader/runtime.env 2>&1 | cut -c1-16 | tr '\n' ' '; }
native_wait_ready() {
	attempt=0
	until curl -fsS -o /dev/null http://127.0.0.1:8081/readyz 2>/dev/null; do
		attempt=$((attempt + 1))
		[ "$attempt" -lt "${1:-90}" ] || return 1
		sleep 2
	done
}
# Waits for the timer to start the unit by itself: the unit's invocation id changes.
native_wait_timer() {
	invocation_before=$(systemctl show hikyo-security-maintenance.service --property=InvocationID --value)
	attempt=0
	while [ "$attempt" -lt 200 ]; do
		attempt=$((attempt + 1))
		now=$(systemctl show hikyo-security-maintenance.service --property=InvocationID --value)
		state=$(systemctl show hikyo-security-maintenance.service --property=ActiveState --value)
		if [ "$now" != "$invocation_before" ] && [ "$state" != activating ] && [ "$state" != active ]; then return 0; fi
		sleep 3
	done
	return 1
}
# The network names and the fake GitHub are not durable: a reboot recreates the container's network.
native_network() {
	ip addr show lo | grep -q 11.0.0.2 || ip addr add 11.0.0.2/32 dev lo
	grep -q api.github.com /etc/hosts || printf '11.0.0.2 api.github.com github.com raw.githubusercontent.com ghcr.io hikyo.app\n' >>/etc/hosts
	mkdir -p "$D/ca"
	[ -e "$D/ca/ca.crt" ] || cp /work/ca/ca.crt /work/ca/ca.key "$D/ca/"
	install -D -m 0644 "$D/ca/ca.crt" /usr/local/share/ca-certificates/hikyo-1068.crt
	update-ca-certificates >/dev/null 2>&1
	REPO=''
	feed_tools
}
# `check` takes the lock the timer's run holds and then prints an empty status, so it is retried.
native_check() {
	attempt=0
	while [ "$attempt" -lt 40 ]; do
		attempt=$((attempt + 1))
		checked=$(coord check 2>/dev/null || true)
		if printf '%s' "$checked" | jq -e '.format != ""' >/dev/null 2>&1; then printf '%s\n' "$checked"; return 0; fi
		sleep 3
	done
	return 1
}
# The installation as the service itself sees it, after a reboot or a failure.
native_running_source() {
	native_wait_ready || fail "the service is not ready ($1)"
	systemctl is-active hikyo.service
	[ "$(systemctl is-enabled hikyo.service)" = enabled ] || defect "hikyo.service is not enabled ($1)"
	systemctl is-active hikyo-security-maintenance.timer
	[ "$(systemctl is-enabled hikyo-security-maintenance.timer)" = enabled ] || defect "the timer is not enabled ($1)"
	[ "$(native_check | jq -r .running.identity.version)" = "$(version_of a)" ] || defect "the service does not run release A, or cannot be observed ($1)"
	[ "$(sudo -u hikyo sqlite3 /var/lib/hikyo/hikyo.db "SELECT name FROM orgs WHERE id='org_native'")" = 'Native marker' ] || defect "the marker row is gone ($1)"
}
native_bundle() { sed -n 's/^HIKYO_UPGRADE_BUNDLE=//p' /var/lib/hikyo-upgrader/runtime.env; }
native_stage_enroll() {
	log setup: local names for GitHub and hikyo.app under the private CA
	native_network
	feed_init
	WINDOW_START=$((($(date -u +%-H) * 60 + $(date -u +%-M) + 720) % 1440)) WINDOW_MINUTES=5 CHANNEL=nightly

	log native: the runbook from nothing through install-systemd.sh
	native_runbook
	native_wait_ready || fail 'the enrolled service is not ready'
	systemctl is-active hikyo.service
	[ "$(systemctl is-enabled hikyo.service)" = enabled ] || defect 'install-systemd.sh left hikyo.service not enabled, so a reboot would leave it down'
	systemctl is-enabled hikyo-security-maintenance.timer
	coord status | jq -c '{phase,reason,enrolled:(.enrollment!=null),running:.running.identity.version,healthy:(.running.ready and .running.phase=="healthy")}'
	echo 'file modes and owners:'
	stat -c '%a %U:%G %n' /etc/hikyo/upgrade-keys /etc/hikyo/upgrade-keys/operator.age /var/lib/hikyo-upgrade/operator.pub /var/lib/hikyo-upgrader/runtime.env /var/lib/hikyo-installation /var/lib/hikyo/hikyo.db
	cat /var/lib/hikyo-upgrader/runtime.env
	native_state >"$D/native-state"

	log native: data in the running source
	sudo -u hikyo sqlite3 -cmd '.timeout 5000' /var/lib/hikyo/hikyo.db "INSERT INTO orgs (id,name,active,metadata,created_at) VALUES ('org_native','Native marker',TRUE,'{}','2026-09-05T00:00:00Z');"
	sudo -u hikyo sqlite3 /var/lib/hikyo/hikyo.db "SELECT name FROM orgs WHERE id='org_native'"

	log native: enrolling again changes nothing
	sh /work/install/security-maintenance/install-systemd.sh | tail -1
	[ "$(native_state)" = "$(cat "$D/native-state")" ] || defect 'a second enrollment changed the vault, pin or runtime environment'
	native_wait_ready 30 || defect 'a second enrollment left the service not ready'
}
native_stage_first_update() {
	log setup: the network again after the reboot
	native_network
	github_start
	log native: after the reboot the service is back, enabled, and the vault and pin are unchanged
	native_running_source 'after the first reboot'
	[ "$(native_state)" = "$(cat "$D/native-state")" ] || defect 'the reboot changed the vault, pin or runtime environment'

	log native: the real timer resumes after the reboot, fires run --once and finds the installation current
	native_wait_timer || fail 'the timer did not run the coordinator after the reboot'
	systemctl show hikyo-security-maintenance.service --property=Result,ExecMainStatus
	coord status | jq -c '{phase,stale,reason,running:.running.identity.version}'
	[ "$(coord status | jq -r .phase)" = current ] || defect 'the timer run did not leave the installation current'

	log native: the first update of a host installed from nothing fails before write-intent
	# This host has never completed an upgrade, so there is no journal from an earlier run.
	[ ! -e /var/lib/hikyo-upgrader/operation.json ] || defect 'a journal exists before the first update'
	# Shell counters die with the stage that made them; stage 1 left one catalog and one publication.
	LAST=$PUBW/catalog-1.json NEXT=1 TUFV=1
	advisory --id HIKYO-REHEARSAL-N1 --summary 'critical fixture flaw' --severity critical --component hikyo --fixed-version "$(version_of b)" --platform "linux/$ARCH"
	publish
	# The export is the first step after the service stops, and its death is a real failure of the proof.
	( until pkill -KILL -f 'backup upgrade-expor[t]'; do sleep 0.01; done ) &
	killer=$!
	native_wait_timer || fail 'the timer did not run the coordinator after the advisory'
	kill "$killer" 2>/dev/null || true
	systemctl show hikyo-security-maintenance.service --property=Result,ExecMainStatus
	coord status | jq -c '{phase,reason,detail,running:.running.identity.version,applicable,operation:(.operation!=null),retry:.retry_attempts}'
	ls /var/lib/hikyo-upgrader /var/lib/hikyo-upgrade
	ls /var/lib/hikyo-upgrader/runtime-backup-*.log >/dev/null 2>&1 || fail 'the export was not killed: the update was not forced to fail'
	if [ -s /var/lib/hikyo-upgrader/preparation-abandoned.json ]; then
		jq -c '{reason}' /var/lib/hikyo-upgrader/preparation-abandoned.json
	else
		defect 'the source restart was not recorded'
	fi
	native_running_source 'after the failed first update'
	[ -d "$(native_bundle)" ] || defect "the bundle the source boots from, $(native_bundle), was removed by the failed first update"
	[ ! -e /var/lib/hikyo-upgrader/operation.json ] || defect 'the failed first update left its journal'
	[ "$(coord status | jq -r .operation)" = null ] || defect 'the failed first update left the operation pinned'
}
# The first administrator and a signed-in session, in the running native service's database.
native_seed_session() {
	PASSWORD=rehearsal-password-long-enough
	install -m 0400 -o hikyo -g hikyo /etc/hikyo/root.key /run/hikyo-1068-root.key
	# shellcheck disable=SC2046 # each env file holds KEY=VALUE lines without spaces, one word per variable
	nx() { sudo -u hikyo env $(cat /etc/hikyo/hikyo.env) $(cat /var/lib/hikyo-upgrader/runtime.env) HIKYO_ROOT_KEY_FILE=/run/hikyo-1068-root.key /usr/bin/hikyo "$@"; }
	created=$(nx admin create --username rehearsal-admin --dangerously-print 2>&1) || fail "admin create: $created"
	principal=$(printf '%s' "$created" | sed -n 's/.*principal \([^)]*\)).*/\1/p')
	nx admin grant --principal "$principal" --capability instance-config >/dev/null 2>&1 || fail 'admin grant failed'
	native_wait_ready 30 || fail 'the service is not ready after the first administrator'
	sleep 5
	curl -fsS -o /dev/null -H 'Content-Type: application/json' --data "$(jq -cn --arg a "$(printf '%s\n' "$created" | grep '^hik_')" --arg p "$PASSWORD" '{authority:$a,password:$p}')" http://127.0.0.1:8080/api/v1/auth/credential/establish
	jar=$D/jar
	curl -sS -c "$jar" -o /dev/null -H 'Content-Type: application/json' --data "{\"username\":\"rehearsal-admin\",\"password\":\"$PASSWORD\",\"artifact\":\"browser\"}" http://127.0.0.1:8080/api/v1/auth/local/login
	csrf=$(awk '$6=="__Host-hikyo-csrf"{print $7}' "$jar")
	curl -fsS -H "$(cookie_header "$jar")" -H "X-Hikyo-CSRF: $csrf" -H 'Content-Type: application/json' --data "{\"password\":\"$PASSWORD\"}" \
		http://127.0.0.1:8080/api/v1/auth/totp/enrol/start | jq -r .otpauth_uri >"$D/totp-uri"
	curl -fsS -o /dev/null -H "$(cookie_header "$jar")" -H "X-Hikyo-CSRF: $csrf" -H 'Content-Type: application/json' --data "{\"code\":\"$("$H" totp "$(cat "$D/totp-uri")")\"}" http://127.0.0.1:8080/api/v1/auth/totp/enrol/confirm
	LASTSTEP=$(($(date +%s) / 30))
	mfa_login "$jar"
	# The scratch recovery proof needs a principal who manages identities in an organisation. Creating the org ends the session.
	org=$(api POST /orgs '{"name":"rehearsal-org"}' | jq -er .id) || fail 'the organisation was not created'
	mfa_login "$jar"
	api POST "/orgs/$org/projects" '{"name":"rehearsal-project"}' | jq -er .id >/dev/null || fail 'the project was not created'
	[ "$(curl -sS -H "$(cookie_header "$jar")" -o /dev/null -w '%{http_code}' http://127.0.0.1:8080/api/v1/auth/whoami)" = 200 ] || fail 'the seeded session does not work'
}
native_identity() { sudo -u hikyo sqlite3 /var/lib/hikyo/hikyo.db "SELECT identity FROM instance_identity; SELECT credential_epoch||'/'||restore_epoch FROM auth_instance_state; SELECT incarnation||'/'||restore_epoch||'/'||generation FROM upgrade_control;" | tr '\n' ' '; }
# Makes the target fail by killing its server whenever the installed binary is not the source's (fail),
# or kills the coordinator the moment the target binary is installed (hold). Nothing here is the
# coordinator's: it is only the failure the host would otherwise have to produce.
native_force() {
	while :; do
		# The target must fail before its own runtime gate marks the database healthy: past that gate
		# the product refuses a rollback by design. Once the schema is written, the service account
		# loses the database, so the target cannot open it however fast it boots.
		if [ ! -e /run/hikyo-1068-armed ] && [ "$(jq -r .phase /var/lib/hikyo-upgrader/operation.json 2>/dev/null)" = schema-applied ]; then
			chown root:root /var/lib/hikyo/hikyo.db /var/lib/hikyo/hikyo.db-wal /var/lib/hikyo/hikyo.db-shm 2>/dev/null || true
			: >/run/hikyo-1068-armed
		fi
		if [ "$(sha256sum /usr/bin/hikyo | cut -c1-64)" != "$2" ]; then
			if [ ! -e /run/hikyo-1068-held ]; then
				jq -r .phase /var/lib/hikyo-upgrader/operation.json >/run/hikyo-1068-phase 2>&1
				if [ -s /var/lib/hikyo-upgrader/operation-source.json ]; then echo present >/run/hikyo-1068-record; else echo absent >/run/hikyo-1068-record; fi
			fi
			if [ "$1" = hold ]; then
				systemctl kill --signal=SIGKILL hikyo-security-maintenance.service
				: >/run/hikyo-1068-held
				return 0
			fi
			: >/run/hikyo-1068-held
			pkill -KILL -f '^/usr/bin/hikyo serve[r]' || true
		fi
		sleep 0.1
	done
}
# What one timer run did, so a run that attempted nothing (backoff, a refusal) is visible.
native_round_report() {
	systemctl show hikyo-security-maintenance.service --property=Result,ExecMainStatus | tr '\n' ' '
	coord status | jq -c '{phase,reason,retry_attempts,retry_disruptive,retry_after,last_attempt,operation:(.operation!=null),detail}'
	journalctl -u hikyo-security-maintenance.service -n 6 --no-pager -o cat 2>/dev/null | cut -c1-300
}
native_assert_rolled_back() {
	src=$1 label=$2 disruptive_before=$3
	[ "$(sha256sum /usr/bin/hikyo | cut -c1-64)" = "$src" ] || defect "the source binary is not back in /usr/bin/hikyo ($label)"
	native_running_source "$label"
	[ "$(native_identity)" = "$(cat "$D/native-identity")" ] || defect "instance, incarnation, credential or restore epoch changed ($label): $(native_identity) != $(cat "$D/native-identity")"
	[ "$(sudo -u hikyo sqlite3 /var/lib/hikyo/hikyo.db 'SELECT count(*) FROM orgs')" = "$(cat "$D/native-orgs")" ] || defect "data written before the update changed ($label)"
	[ "$(curl -sS -H "$(cookie_header "$D/jar")" -o /dev/null -w '%{http_code}' http://127.0.0.1:8080/api/v1/auth/whoami)" = 200 ] || defect "the session from before the update stopped working ($label)"
	[ "$(coord status | jq -r .operation)" = null ] || defect "the operation is still pinned ($label)"
	[ "$(coord status | jq -r '(.detail // "") + (.retry_cause // "")' | grep -c 'the earlier release is serving again and the security fix is not applied')" -ge 1 ] || defect "the status does not say the earlier release serves and the fix is not applied ($label)"
	[ "$(coord status | jq -r '.retry_disruptive // 0')" -gt "$disruptive_before" ] || defect "the rollback did not count toward the disruptive attempts ($label)"
	# Kept for a coordinator that dies before it records the rollback; the next preparation removes it.
	[ "$(jq -r .rolled_back /var/lib/hikyo-upgrader/operation-source.json 2>/dev/null)" = true ] || defect "no source record says the attempt rolled back ($label)"
	[ ! -e /var/lib/hikyo-upgrader/maintenance ] || defect "the maintenance fence is still set ($label)"
	coord status | jq -c '{phase,reason,retry_attempts,retry_disruptive,detail}'
	ls /var/lib/hikyo-upgrader
}
# Puts the host where a crash in the middle of the restore leaves it: the journal at rolling-back with the
# SQLite file already gone (the restore removes it before it writes the snapshot back). The next run must
# resume the rollback from the journal alone, so the stage genuinely has no database to read.
native_absent_database() {
	systemctl stop hikyo.service
	jq '.phase = "rolling-back" | .rollback_database = "/var/lib/hikyo/hikyo.db"' /var/lib/hikyo-upgrader/operation.json >"$D/operation.rolling-back"
	install -m 0600 -o root -g root "$D/operation.rolling-back" /var/lib/hikyo-upgrader/operation.json
	rm -f /var/lib/hikyo/hikyo.db /var/lib/hikyo/hikyo.db-wal /var/lib/hikyo/hikyo.db-shm
	[ "$(jq -r .phase /var/lib/hikyo-upgrader/operation.json)" = rolling-back ] || defect 'the journal is not at rolling-back'
	[ ! -e /var/lib/hikyo/hikyo.db ] || defect 'the database file is still present'
}
native_stage_forced_failure() {
	log native: a session and data in the running source before any forced failure
	native_seed_session
	native_identity >"$D/native-identity"
	# shellcheck disable=SC2024 # only the read needs the service account; the result belongs to this run
	sudo -u hikyo sqlite3 /var/lib/hikyo/hikyo.db 'SELECT count(*) FROM orgs' >"$D/native-orgs"
	src=$(sha256sum /usr/bin/hikyo | cut -c1-64)
	cat "$D/native-identity"
	before=$(coord status | jq -r '.retry_disruptive // 0')

	log native: the target fails after the schema write, and the timer run rolls it back by itself
	rm -f /run/hikyo-1068-held /run/hikyo-1068-armed /run/hikyo-1068-phase /run/hikyo-1068-record
	native_force fail "$src" &
	killer=$!
	round=0
	until [ -e /run/hikyo-1068-held ] && [ "$(systemctl show hikyo-security-maintenance.service --property=ActiveState --value)" != active ]; do
		round=$((round + 1))
		[ "$round" -le 6 ] || break
		native_wait_timer || fail 'the timer did not run the coordinator'
		native_round_report
	done
	kill "$killer" 2>/dev/null || true
	wait "$killer" 2>/dev/null || true
	[ -e /run/hikyo-1068-held ] || fail 'the target binary was never installed: the failure was not forced'
	echo "journal phase when the target binary was installed: $(cat /run/hikyo-1068-phase)"
	[ "$(cat /run/hikyo-1068-record)" = present ] || defect 'the update wrote no source record before the target binary was installed, so no rollback is possible'
	case "$(cat /run/hikyo-1068-phase)" in preparing | proved | null | '') defect "the failure was not after the schema write: $(cat /run/hikyo-1068-phase)" ;; esac
	systemctl show hikyo-security-maintenance.service --property=Result,ExecMainStatus
	# Read before the assertions: they run `coord check`, which rewrites the status the run left.
	reported=$(coord status | jq -r .reason)
	native_assert_rolled_back "$src" 'forced failure rolled back in the same run' "$before"
	[ "$reported" = operation_rolled_back ] || defect "a post-write rollback is not reported as one: $reported"
	before=$(coord status | jq -r '.retry_disruptive // 0')

	log native: the coordinator dies after the target binary is installed, and a later timer run rolls back after the deadline
	rm -f /run/hikyo-1068-held /run/hikyo-1068-armed /run/hikyo-1068-phase /run/hikyo-1068-record
	native_force hold "$src" &
	killer=$!
	round=0
	until [ -e /run/hikyo-1068-held ]; do
		round=$((round + 1))
		[ "$round" -le 6 ] || break
		native_wait_timer || fail 'the timer did not run the coordinator'
		native_round_report
	done
	kill "$killer" 2>/dev/null || true
	wait "$killer" 2>/dev/null || true
	[ -e /run/hikyo-1068-held ] || fail 'the target binary was never installed: the coordinator was not killed'
	echo "coordinator killed; journal phase: $(cat /run/hikyo-1068-phase)"
	coord status | jq -c '{phase,reason,operation:(.operation!=null)}'
	[ "$(coord status | jq -r '.operation != null')" = true ] || defect 'the killed coordinator left no pinned operation'
	log native: the same crash with the SQLite file absent at rolling-back, which the next run must still roll back
	native_absent_database
	round=0
	until [ "$(coord status | jq -r .operation)" = null ]; do
		round=$((round + 1))
		[ "$round" -le 8 ] || break
		native_wait_timer || fail 'the timer did not run the coordinator'
		coord status | jq -c '{phase,reason,operation:(.operation!=null)}'
	done
	native_assert_rolled_back "$src" 'later run rolled back after the deadline' "$before"

	log native: three disruptive attempts at one target stop the automatic retries, and reconcile reopens them
	[ "$(coord status | jq -r .reason)" = retry_exhausted ] || defect "the third disruptive attempt did not exhaust the retries: $(coord status | jq -r .reason)"
	coord status | jq -r '.action'
	coord reconcile --confirm "$(coord status | jq -r .action | awk '{print $NF}')" | jq -c '{phase,reason}'
	rm -f /run/hikyo-1068-root.key
}
native_stage_second_update() {
	log setup: the network again after the second reboot
	native_network
	github_start
	log native: the restored source is bootable after a further restart
	native_running_source 'after the second reboot'
	[ -d "$(native_bundle)" ] || defect "the bundle the source boots from, $(native_bundle), is gone after the restart"
	[ "$(native_check | jq -r .reason)" != installation_unavailable ] || defect 'the restored source cannot be observed: its bundle or cache is missing'

	native_stage_forced_failure

	log native: the timer retries after its backoff and carries the installation to B
	# Not "attempt": the helpers below count with it.
	round=0
	until [ "$(coord status | jq -r .running.identity.version)" = "$(version_of b)" ]; do
		round=$((round + 1))
		[ "$round" -le 4 ] || break
		native_wait_timer || fail 'the timer did not run the coordinator'
		systemctl show hikyo-security-maintenance.service --property=Result,ExecMainStatus
		coord status | jq -c '{phase,reason,detail,running:.running.identity.version,operation:(.operation!=null)}'
	done
	native_wait_ready 60 || defect 'the service is not ready after the update'
	[ "$(coord status | jq -r .running.identity.version)" = "$(version_of b)" ] || defect 'the critical advisory did not reach B'
	[ "$(sha256sum /usr/bin/hikyo | cut -c1-64)" = "$(sha256sum "$F/release-b/hikyo" | cut -c1-64)" ] || defect '/usr/bin/hikyo is not release B'
	[ "$(sudo -u hikyo sqlite3 /var/lib/hikyo/hikyo.db "SELECT name FROM orgs WHERE id='org_native'")" = 'Native marker' ] || defect 'the marker row did not survive the update'
	curl -fsS http://127.0.0.1:8080/api/v1/meta | jq -c '{server_version,api_revision}'
	jq -c '{phase,running_version,coverage,protection_gap,known_until}' /var/lib/hikyo-security-status/status.json
	ls /var/lib/hikyo-upgrade
}
native_flow() {
	COORD=/usr/bin/hikyo
	case "${STAGE:-1}" in
	1) native_stage_enroll ;;
	2) native_stage_first_update ;;
	3) native_stage_second_update ;;
	esac
	log "stage ${STAGE:-1} finished with $DEFECTS defect(s)"
	[ "$DEFECTS" -eq 0 ] || exit 2
}

# The automatic rollback to the fenced snapshot, against real Compose, a real root
# coordinator and the real feed (rollback mode). Each class starts from a fresh
# release A with data, forces one failure, and asserts the result against the running
# service, not only the journal. Faults come from outside: the target's CPU is
# throttled at write-intent so that its phases last seconds, then its processes are
# stopped, its key wrapper corrupted, or its container killed.
UNATT=/srv/hikyo/upgrade/unattended
DB=/srv/hikyo/data/hikyo.db
PUBSTATUS=/var/lib/hikyo-security-status/status.json
ok() { printf 'ASSERT ok: %s\n' "$*"; }
# check DESCRIPTION EXPRESSION: EXPRESSION is evaluated by sh, so that it reads current state.
check() { if eval "$2"; then ok "$1"; else defect "ASSERT FAILED: $1"; fi; }
# poll TENTHS COMMAND...: runs COMMAND every 100 ms until it succeeds.
poll() { left=$1; shift; while [ "$left" -gt 0 ]; do "$@" && return 0; left=$((left - 1)); sleep 0.1; done; return 1; }
inspect_source() { docker exec hikyo-server-1 /usr/local/bin/hikyo security-maintenance inspect 2>/dev/null; }
# ctl_row: "generation maintenance pending-phase" from the live control row.
ctl_row() { sqlite3 -readonly -cmd '.timeout 3000' "$DB" "SELECT c.generation||' '||c.maintenance||' '||ifnull(json_extract(p.operation_json,'\$.phase'),'-') FROM upgrade_control c LEFT JOIN upgrade_pending p ON p.singleton=c.singleton" 2>/dev/null; }
journal_phase() { jq -r '.phase // "none"' "$UNATT/operation.json" 2>/dev/null || echo none; }
journal_is() { grep -Eq "\"phase\": *\"$1\"" "$UNATT/operation.json" 2>/dev/null; }
row_phase_is() { case "$(ctl_row)" in *" $1") return 0 ;; esac; return 1; }
# past_write: the target has written the control row and has not finished.
past_write() { case "$(ctl_row)" in *" prepared" | *" schema-write-started" | *" schema-applied") return 0 ;; esac; return 1; }
# past_final: the control row is healthy, outside maintenance, at a generation above the source's.
# shellcheck disable=SC2046 # the row is three words
past_final() { set -- $(ctl_row); [ "${1:-0}" -gt "$B_GEN" ] && [ "${2:-1}" = 0 ] && [ "${3:-}" = healthy ]; }
# rollback_journals lists the JSON records that name a rollback; the staged binaries carry the words too.
rollback_journals() { find "$UNATT" -name '*.json' -exec grep -lE 'rolling-back|rolled back to the fenced snapshot' {} +; }
org_rows() { sqlite3 -readonly "$DB" "SELECT count(*) FROM orgs WHERE name='rehearsal-org'"; }
forced_rows() { sqlite3 -readonly "$DB" "SELECT count(*) FROM value_entries WHERE id='val_forced'"; }
hostpath() { printf '%s' "$1" | sed 's|^/var/lib/hikyo-upgrade|/srv/hikyo/upgrade|'; }
cfg_set() { jq "$@" "$CFG" >"$D/cfg.json" && install -m 0644 "$D/cfg.json" "$CFG"; }
throttle() { docker update --cpus 0.05 hikyo-server-1 >/dev/null; }
unthrottle() { docker update --cpus 0 hikyo-server-1 >/dev/null 2>&1 || true; }
# freeze stops every process of the target, from outside its PID namespace: a real hang.
freeze() { signal_target STOP; }
thaw() { signal_target CONT; }
# A process that exited since docker top listed it is not an error.
signal_target() { for p in $(docker top hikyo-server-1 | awk 'NR==1{for(i=1;i<=NF;i++)if($i=="PID")c=i} NR>1{print $c}'); do kill -"$1" "$p" 2>/dev/null || true; done; }
# freeze_readable LABEL: freezes the target where the control row stays readable. A freeze inside SQLite's lock
# window leaves the row unreadable, which is the separate case the operations guide lists; so it thaws and
# freezes again, at most 20 times. If the lock is never free it records a defect, thaws the target and returns 1,
# and the class is not scored. shortcut: the hung-inside-the-lock case itself is not rehearsed, add a class for it
# when its manual exit (stop the container, then run --once) needs proof.
freeze_readable() {
	tries=0
	freeze
	until [ -n "$(ctl_row)" ]; do
		tries=$((tries + 1))
		[ "$tries" -lt 20 ] || { defect "$1: every freeze held the database lock"; thaw; return 1; }
		thaw
		sleep 0.2
		freeze
	done
	[ "$tries" = 0 ] || echo "$1: refroze $tries time(s) to leave the database lock free"
}
start_run() { coord run --once >"$D/run.json" 2>"$D/run.err" & COORDPID=$!; }
finish_run() { code=0; wait "$COORDPID" || code=$?; jq -c '{phase,reason,detail,running:.running.identity.version}' "$D/run.json" 2>&1 | cut -c1-500; echo "run --once exit=$code"; }
kill_coordinator() { pkill -9 -f 'security-maintenance run --once' || true; wait "$COORDPID" 2>/dev/null || true; }
whoami_code() { curl -sS -H "$(cookie_header "$D/jar")" -o /dev/null -w '%{http_code}' http://127.0.0.1:8080/api/v1/auth/whoami; }
# tamper_keys corrupts the instance key wrapper after the fenced snapshot, so that the
# candidate's own health check refuses it. The snapshot holds the intact wrapper.
tamper_keys() {
	tries=0
	until sqlite3 -cmd '.timeout 2000' "$DB" "UPDATE tier3_keys SET blob=zeroblob(length(blob)) WHERE purpose='instance'" 2>/dev/null; do
		tries=$((tries + 1))
		[ "$tries" -lt 50 ] || return 1
		sleep 0.1
	done
	chown -R 65532:65532 /srv/hikyo/data
}
# tamper_after_write waits until the target has written the control row (a tamper before
# the write makes the target refuse before any restore is needed), then corrupts the wrapper.
tamper_after_write() {
	poll 1800 past_write || defect 'the target never wrote the control row'
	tamper_keys || fail 'could not corrupt the instance key wrapper'
	echo "instance key wrapper corrupted; control row=$(ctl_row) $(date -u +%T)"
}
tampered_keys() { sqlite3 -readonly -cmd '.timeout 3000' "$DB" "SELECT count(*) FROM tier3_keys WHERE blob=zeroblob(length(blob))"; }

rb_fresh() {
	log "$1: fresh release A with an organisation, a project, a key, a TOTP factor and a signed-in session"
	reinstall_source
	seed_data
	cfg_set --argjson t "${2:-900}" '.deployment.timeout_seconds=$t'
	# Baseline, read from the running service.
	b=$(inspect_source)
	B_INSTANCE=$(printf '%s' "$b" | jq -r .instance) B_INCARNATION=$(printf '%s' "$b" | jq -r .incarnation)
	B_EPOCH=$(printf '%s' "$b" | jq -r .restore_epoch) B_GEN=$(printf '%s' "$b" | jq -r .generation)
	B_INODE=$(stat -c %i "$DB")
	echo "baseline: instance=$B_INSTANCE incarnation=$(printf %.12s "$B_INCARNATION") restore_epoch=$B_EPOCH generation=$B_GEN inode=$B_INODE whoami=$(whoami_code) image=$(docker inspect --format '{{.Config.Image}}' hikyo-server-1 | cut -c1-40)"
	[ "$(whoami_code)" = 200 ] || fail 'the seeded session does not authenticate before the update'
}
# rb_at_write_intent: waits for the target to have exported and proved its snapshot.
rb_at_write_intent() {
	poll 3000 journal_is write-intent || { kill_coordinator; fail 'the target never reached write-intent'; }
	throttle
	EVID=$(hostpath "$(jq -r .runtime.EvidenceDirectory "$UNATT/operation.json")") CT=$(hostpath "$(jq -r .runtime.CiphertextPath "$UNATT/operation.json")")
	echo "write-intent: journal=$(journal_phase) control row=$(ctl_row) evidence: $(ls "$EVID" | tr '\n' ' ')"
}

# rb_assert_restored LABEL [DISRUPTIVE]: the source is serving again with everything it had.
rb_assert_restored() {
	label=$1 want=${2:-1}
	wait_healthy || { defect "$label: the source did not become healthy by itself"; return 0; }
	coord status >"$D/status.json"
	now=$(inspect_source)
	log "$label: assertions against the running service"
	check "$label: the source image digest is running" '[ "$(docker inspect --format "{{.Config.Image}}" hikyo-server-1)" = "$(img a)" ]'
	check "$label: install.env names the source" '[ "$(env_image)" = "$(img a)" ]'
	check "$label: the API reports the source release" '[ "$(curl -fsS http://127.0.0.1:8080/api/v1/meta | jq -r .server_version)" = "$(version_of a)" ]'
	check "$label: the service is ready" 'curl -fsS -o /dev/null http://127.0.0.1:8081/readyz'
	check "$label: same instance id" '[ "$(printf %s "$now" | jq -r .instance)" = "$B_INSTANCE" ]'
	check "$label: same recovery incarnation" '[ "$(printf %s "$now" | jq -r .incarnation)" = "$B_INCARNATION" ]'
	check "$label: restore epoch unchanged" '[ "$(printf %s "$now" | jq -r .restore_epoch)" = "$B_EPOCH" ]'
	check "$label: generation is the source's again" '[ "$(printf %s "$now" | jq -r .generation)" = "$B_GEN" ]'
	check "$label: healthy and outside maintenance" 'printf %s "$now" | jq -e ".phase == \"healthy\" and .maintenance == false" >/dev/null'
	check "$label: the database file was replaced by the restore" '[ "$(stat -c %i "$DB")" != "$B_INODE" ]'
	check "$label: no corrupted key wrapper is left" '[ "$(tampered_keys)" = 0 ]'
	check "$label: the old session still authenticates" '[ "$(whoami_code)" = 200 ]'
	check "$label: the organisation survived" 'api GET /orgs | jq -e "[.items[].name] | index(\"rehearsal-org\") != null" >/dev/null'
	check "$label: the project and its key survived" 'api GET "/orgs/$org/projects/$project/keys" | jq -e "[.. | .name? // empty] | index(\"REHEARSAL_KEY\") != null" >/dev/null'
	check "$label: the target's attestation is gone" '[ -z "$(find /srv/hikyo/upgrade -name "attestation*")" ]'
	check "$label: the enrollment is still valid" 'jq -e ".enrollment != null and .reason != \"recovery_incarnation_changed\" and .reason != \"explicit_enrollment_required\"" "$D/status.json" >/dev/null'
	check "$label: the deployment journal says the attempt rolled back, and no operation is pinned" 'jq -e ".phase == \"rolled-back\"" "$JOURNAL" >/dev/null && jq -e ".operation == null" "$D/status.json" >/dev/null'
	check "$label: the failed attempt counted as disruptive ($(jq -r .retry_disruptive "$D/status.json"))" "[ \"\$(jq -r .retry_disruptive \"\$D/status.json\")\" = $want ]"
	check "$label: status says the earlier release is serving and the fix is not applied" 'jq -r .detail "$D/status.json" | grep -q "the earlier release is serving again and the security fix is not applied"'
	jq -c '{phase,reason,detail,retry_attempts,retry_disruptive}' "$D/status.json" | cut -c1-500
	echo "public status: $(jq -c '{phase,reason,protection_gap}' $PUBSTATUS)"
	echo "status API: $(api_status "$D/jar" | jq -c '{reported,phase:.status.phase,reason:.status.reason}')"
	run_once
	check "$label: the coordinator runs again (held by the backoff, not refused for its enrollment)" '[ "$(jq -r .reason "$D/run.json")" = retry_backoff ]'
	mfa_login "$D/jar-after"
	check "$label: a fresh sign-in with password and TOTP works and the service accepts the write" '[ "$(curl -sS -b "$D/jar-after" -o /dev/null -w "%{http_code}" -H "$(cookie_header "$D/jar-after")" http://127.0.0.1:8080/api/v1/auth/whoami)" = 200 ]'
	ls /srv/hikyo/upgrade/unattended | tr '\n' ' '; echo
	[ -z "$(attempt_copies)" ] || echo "left on the state volume: $(attempt_copies)"
}

rb_c1() {
	rb_fresh 'class 1: post-write health failure, restore-required'
	start_run
	rb_at_write_intent
	tamper_after_write
	unthrottle
	if poll 1800 row_phase_is restore-required; then echo "control row reached restore-required: $(ctl_row)"; else defect 'class 1: the target never reached restore-required'; fi
	finish_run
	check 'c1: the run reported the rollback as one' '[ "$(jq -r .reason "$D/run.json")" = operation_rolled_back ]'
	rb_assert_restored c1
}

rb_c2() {
	rb_fresh 'class 2: the target hangs during migration, the deadline triggers the rollback' 240
	start_run
	rb_at_write_intent
	poll 1800 past_write || defect 'class 2: the target never left the fenced state'
	freeze_readable c2 || { finish_run; return 0; }
	echo "target frozen: control row=$(ctl_row) journal=$(journal_phase) $(docker inspect --format 'running={{.State.Running}} health={{.State.Health.Status}}' hikyo-server-1)"
	finish_run
	rb_assert_restored c2
}

# rb_c3: the existing no-restore path, with the evidence that no restore ran.
rb_c3() {
	log 'class 3: the target is refused before any write: no restore, the source restarts'
	forced_source
	B_INODE=$(stat -c %i "$DB") B_GEN=$(inspect_source | jq -r .generation)
	run_once
	wait_healthy || fail 'class 3: the source did not come back'
	coord status >"$D/status.json"
	check 'c3: the source image is running' '[ "$(docker inspect --format "{{.Config.Image}}" hikyo-server-1)" = "$(img a)" ]'
	check 'c3: no restore ran: the database file is the same inode' '[ "$(stat -c %i "$DB")" = "$B_INODE" ]'
	check 'c3: generation is the source generation' '[ "$(inspect_source | jq -r .generation)" = "$B_GEN" ]'
	check 'c3: recorded as refused before the database changed' '[ "$(jq -r .reason "$D/status.json")" = preapply_refused ]'
	check 'c3: no journal says rolling-back or rolled back' '[ -z "$(rollback_journals)" ]'
	check 'c3: no pinned operation' '[ "$(jq -r .operation "$D/status.json")" = null ]'
	check 'c3: the data is unchanged' '[ "$(forced_rows)" = 1 ]'
	jq -c '{phase,reason,detail,retry_disruptive}' "$D/status.json" | cut -c1-400
}

# rb_c4 KIND: the coordinator dies with the target hung. KIND manual: the policy is manual when the
# later run happens. KIND unreachable: the feed is down. Both run after the deadline.
rb_c4() {
	rb_fresh "class 4 ($1): coordinator killed mid-operation, a later run completes the rollback" 150
	start_run
	rb_at_write_intent
	poll 1800 past_write || defect 'class 4: the target never left the fenced state'
	freeze_readable "c4 ($1)" || { kill_coordinator; return 0; }
	kill_coordinator
	echo "coordinator killed, target frozen: control row=$(ctl_row) journal=$(journal_phase)"
	coord status | jq -c '{phase,reason,operation:(.operation!=null)}'
	case "$1" in
	manual) cfg_set '.policy.mode="manual"' ;;
	unreachable) github_stop ;;
	esac
	run_once
	check "c4 ($1): a run before the deadline does not touch the pinned target" '[ "$(docker inspect --format "{{.Config.Image}}" hikyo-server-1)" = "$(img b)" ]'
	echo 'waiting out the deadline'
	sleep 150
	run_once
	[ "$1" != unreachable ] || github_start
	cfg_set '.policy.mode="automatic"'
	rb_assert_restored "c4-$1"
}

# rb_c5: three rollbacks at one target, then no attempt.
rb_c5() {
	rb_fresh 'class 5: the third rollback at one target reaches retry_exhausted'
	for n in 1 2 3; do
		[ "$n" = 1 ] || rewind_backoff
		start_run
		rb_at_write_intent
		tamper_after_write
		unthrottle
		poll 1800 row_phase_is restore-required && echo "attempt $n: control row reached restore-required"
		finish_run
		wait_healthy || fail "class 5: the source did not come back after attempt $n"
		coord status >"$D/status.json"
		check "c5: attempt $n counted as disruptive" '[ "$(jq -r .retry_disruptive "$D/status.json")" = "$n" ]'
		check "c5: attempt $n restored the snapshot (no corrupted key)" '[ "$(tampered_keys)" = 0 ]'
	done
	started=$(docker inspect --format '{{.State.StartedAt}}' hikyo-server-1) inode=$(stat -c %i "$DB")
	rewind_backoff
	run_once
	coord status >"$D/status.json"
	check 'c5: the fourth run is refused as retry_exhausted' '[ "$(jq -r .reason "$D/status.json")" = retry_exhausted ]'
	check 'c5: the scheduled unit would fail' '[ "$code" != 0 ]'
	check 'c5: the source was not interrupted again' '[ "$(docker inspect --format "{{.State.StartedAt}}" hikyo-server-1)" = "$started" ]'
	check 'c5: no further attempt touched the database' '[ "$(stat -c %i "$DB")" = "$inode" ]'
	check 'c5: no target container exists' '[ "$(docker ps -a --format "{{.Image}}" | grep -c "$(img b)")" = 0 ]'
	check 'c5: the source serves' 'curl -fsS -o /dev/null http://127.0.0.1:8081/readyz'
	check 'c5: the public status shows a protection gap with the exact action' '[ "$(jq -r .protection_gap $PUBSTATUS)" = true ] && jq -r .action $PUBSTATUS | grep -q "^hikyo security-maintenance"'
	jq -c '{phase,reason,action,detail}' "$D/status.json" | cut -c1-500
}

# rb_c6: the one-shot rollback cannot read its archive. Nothing may serve a wrong database.
rb_c6() {
	rb_fresh 'class 6: the rollback itself fails, the archive is corrupted' 150
	start_run
	rb_at_write_intent
	# The archive is damaged only after the target has written: before that the target itself
	# refuses (its own receipt check), and nothing needs rolling back.
	tamper_after_write
	orig=$(dd if="$CT" bs=1 skip=64 count=1 2>/dev/null | od -An -tx1 | tr -d ' ')
	printf '\377' | dd of="$CT" bs=1 seek=64 conv=notrunc 2>/dev/null
	echo "archive byte 64: $orig -> ff"
	unthrottle
	finish_run
	coord status >"$D/status.json"
	jq -c '{phase,reason,action,operation:(.operation!=null)}' "$D/status.json" | cut -c1-500
	echo "source container: $(docker inspect --format 'status={{.State.Status}} restarting={{.State.Restarting}} restarts={{.RestartCount}} health={{.State.Health.Status}} image={{.Config.Image}}' hikyo-server-1 | cut -c1-200)"
	docker logs --tail 3 hikyo-server-1 2>&1 | jq -r '.err // .msg' 2>/dev/null | cut -c1-240
	echo "control row: $(ctl_row)  journal: $(journal_phase)"
	check 'c6: nothing serves: the API does not answer' '! curl -fsS -o /dev/null --max-time 5 http://127.0.0.1:8080/api/v1/meta'
	check 'c6: the readiness probe is down' '! curl -fsS -o /dev/null --max-time 5 http://127.0.0.1:8081/readyz'
	check 'c6: the operation stays pinned' 'jq -e ".operation != null" "$D/status.json" >/dev/null'
	check 'c6: the status asks for a person with an exact command' 'jq -e ".action != null and (.action | test(\"^hikyo security-maintenance\"))" "$D/status.json" >/dev/null'
	check 'c6: the public status shows a protection gap' '[ "$(jq -r .protection_gap $PUBSTATUS)" = true ]'
	check 'c6: the database is not silently the old one: it still holds the target generation' '[ "$(ctl_row | cut -d" " -f1)" -gt "$B_GEN" ]'
	check 'c6: the corrupted database was not replaced' '[ "$(stat -c %i "$DB")" = "$B_INODE" ] && [ "$(tampered_keys)" != 0 ]'
	jq -c '{phase,reason,detail}' "$D/status.json" | cut -c1-500
	log 'c6: the archive is readable again and a later run, after the deadline, completes the rollback'
	printf '%b' "\\0$(printf '%o' "0x$orig")" | dd of="$CT" bs=1 seek=64 conv=notrunc 2>/dev/null
	sleep 150
	run_once
	rb_assert_restored c6-later
}

# rb_c7: the target is past the final gate and then fails. The spec forbids an automatic rollback.
rb_c7() {
	rb_fresh 'class 7: the target passes the final gate and then dies' 150
	start_run
	rb_at_write_intent
	unthrottle
	poll 3000 past_final || defect 'class 7: the target never passed the final gate'
	kill_coordinator
	docker update --restart=no hikyo-server-1 >/dev/null
	docker kill hikyo-server-1 >/dev/null
	row=$(ctl_row)
	echo "target past the final gate and killed (restart disabled): control row=$row $(docker inspect --format 'status={{.State.Status}}' hikyo-server-1)"
	inode=$(stat -c %i "$DB")
	sleep 150
	run_once
	coord status >"$D/status.json"
	jq -c '{phase,reason,action,operation:(.operation!=null),detail}' "$D/status.json" | cut -c1-600
	echo "containers: $(docker ps -a --format '{{.Names}}={{.Status}}' | tr '\n' ' ')"
	echo "image: $(docker inspect --format '{{.Config.Image}}' hikyo-server-1 2>&1 | cut -c1-60)  control row now: $(ctl_row)"
	check 'c7: not rolled back: the database file is untouched' "[ \"\$(stat -c %i \"\$DB\")\" = $inode ]"
	check 'c7: the control row still says the target is healthy' '[ "$(ctl_row)" = "$row" ]'
	check 'c7: the target data is intact' '[ "$(org_rows)" = 1 ]'
	check 'c7: the operation stays pinned' 'jq -e ".operation != null" "$D/status.json" >/dev/null'
	check 'c7: no journal says the snapshot was restored' '[ -z "$(rollback_journals)" ]'
}

rollback_flow() {
	feed_tools
	rm -rf "$KEYS" "$FEED" "$PUBW" # a rerun in a kept container starts a new feed
	feed_init
	WINDOW_START=$((($(date -u +%-H) * 60 + $(date -u +%-M) + 720) % 1440)) WINDOW_MINUTES=5 CHANNEL=nightly
	COORD=/usr/bin/hikyo
	install -m 0755 -o root -g root "$F/release-a/hikyo" "$COORD"
	advisory --id HIKYO-REHEARSAL-ROLLBACK --summary 'critical fixture flaw' --severity critical --component hikyo --fixed-version "$(version_of b)" --platform "linux/$ARCH"
	publish
	for class in ${CLASSES:-1 2 3 4m 4u 5 6 7}; do
		case "$class" in
		1) rb_c1 ;;
		2) rb_c2 ;;
		3) rb_c3 ;;
		4m) rb_c4 manual ;;
		4u) rb_c4 unreachable ;;
		5) rb_c5 ;;
		6) rb_c6 ;;
		7) rb_c7 ;;
		*) fail "unknown class $class" ;;
		esac
	done
}

if [ "$MODE" = native ]; then
	native_flow
	exit
fi

if [ "$FEEDLIKE" = true ]; then
	log setup: tools, local names for GitHub, ghcr.io and hikyo.app under the private CA
	apk add --no-cache sudo coreutils jq iproute2 openssl curl sqlite >/dev/null
	# Rerunnable in a kept container: leftovers of an earlier run are replaced.
	pkill -f '/work/harness github' 2>/dev/null || true
	rm -rf /tmp/hikyo-1068-registry-* # the fake registry's layer staging, memory backed on some hosts
	ip addr show lo | grep -q 11.0.0.2 || ip addr add 11.0.0.2/32 dev lo
	grep -q ghcr.io /etc/hosts || printf '11.0.0.2 api.github.com github.com raw.githubusercontent.com ghcr.io hikyo.app\n' >>/etc/hosts
	mkdir -p "$D"
	: >"$D/digests"
	mkdir -p "$D/ca"
	cp /work/ca/ca.crt /work/ca/ca.key "$D/ca/"
	install -D -m 0644 "$D/ca/ca.crt" /usr/local/share/ca-certificates/hikyo-1068.crt
	update-ca-certificates >/dev/null 2>&1
	install -D -m 0644 "$D/ca/ca.crt" /etc/docker/certs.d/ghcr.io/ca.crt
	REPO=ghcr.io/hikyo-1068-rehearsal/hikyo
	for label in a b c; do
		# The signed image artifact names the index, not the platform manifest the layout lists.
		printf '%s %s@%s\n' "$label" "$REPO" "$(jq -er '.artifacts[] | select(.kind == "image") | .digest' "$F/public/release-$label/release-manifest.json")" >>"$D/digests"
	done
	FEED=$D/feed
	if [ "$MODE" = rollback ]; then rollback_flow; else feed_flow; fi
	log "finished with $DEFECTS defect(s)"
	[ "$DEFECTS" -eq 0 ]
	exit
fi

# PublicDialer refuses private addresses, so the fake GitHub takes a public one on lo.
log setup: tools, public-looking address for the fake GitHub, registry, images
apk add --no-cache sudo coreutils jq iproute2 >/dev/null
ip addr add 11.0.0.2/32 dev lo
# shellcheck disable=SC2153 # exported into this inner phase by the outer one
mkdir -p "$D/ca" "$D/ctx/image-root/$ARCH"
"$H" ca "$D/ca"
docker run --detach --name hikyo-1068-rehearsal-registry --publish 127.0.0.1:5000:5000 registry:3@sha256:d4032d4cc323c1731c707931ecada07a53076eac38e3b61ae57533f170f2c6c3 >/dev/null 2>&1
sleep 2
REPO=localhost:5000/hikyo-1068-rehearsal/hikyo
cp "$D/ca/ca.crt" "$D/ctx/ca.crt"
{ cat /work/Dockerfile.release; echo 'COPY ca.crt /etc/ssl/certs/ca-certificates.crt'; } >"$D/ctx/Dockerfile"
for label in a b c; do
	cp "$F/release-$label/hikyo" "$D/ctx/image-root/$ARCH/hikyo"
	docker build --quiet --platform "linux/$ARCH" --tag "$REPO:$label" "$D/ctx" >/dev/null
	docker push --quiet "$REPO:$label" >/dev/null
	printf '%s %s\n' "$label" "$(docker image inspect --format '{{json .RepoDigests}}' "$REPO:$label" | jq -er '.[0]')" | tee -a "$D/digests"
	docker image rm "$REPO:$label" >/dev/null # So that every later pull is real.
done
rm -rf "$D/ctx"
github_start

log 1 runbook, Observe source
runbook
wait_ready a || fail 'source did not become healthy'
instance=$(printf '%s' "$observed" | jq -r .instance)
first_custody=$(custody)
[ "$(printf '%s' "$observed" | jq -r .image)" = "$(img a)" ] || fail 'source image differs from digest'

log 2 Prepare while the release source is down
github_stop
if output=$("$H" prepare "$CFG" "$F/descriptor-b.json" "$(img b)" 2>&1); then
	echo 'Prepare succeeded offline'
else
	printf '%s\n' "$output" | tail -1 | cut -c1-200
fi
github_start
wait_ready a >/dev/null || fail 'failed Prepare disturbed the source'

log 3 Prepare B, Apply with eligibility, poll Observe
"$H" prepare "$CFG" "$F/descriptor-b.json" "$(img b)" | jq -c '{staged:.staged_target.version,route:.route_sha256,requires_maintenance,phase,ready}'
[ "$(jq -r .phase "$JOURNAL")" = prepared ] || fail 'journal not prepared'
wait_ready a >/dev/null || fail 'Prepare disturbed the source'
"$H" apply "$CFG" "$F/descriptor-b.json" "$(img b)"
[ "$(jq -r .phase "$JOURNAL")" = applying ] || fail 'journal not applying after Apply'
wait_ready b || fail 'target did not become healthy'
[ "$(printf '%s' "$observed" | jq -r .generation)" -gt 1 ] || fail 'generation did not advance'
[ "$(printf '%s' "$observed" | jq -r .image)" = "$(img b)" ] || fail 'target image differs from digest'
[ "$(printf '%s' "$observed" | jq -r .instance)" = "$instance" ] || fail 'instance changed'
[ "$(env_image)" = "$(img b)" ] || fail 'environment file not updated'
[ "$(stat -c %a /etc/hikyo/install.env)" = 600 ] || fail 'environment file mode changed'
[ "$(jq -r .phase "$JOURNAL")" = healthy ] || fail 'journal not healthy'

log 4 plain up neither reverts nor recreates, data and root key survive
before=$(container_id)
dc up --detach >/dev/null 2>&1
[ "$(container_id)" = "$before" ] || defect 'plain up recreated the target container'
wait_ready b || fail 'target unhealthy after plain up'
[ "$(custody)" = "$first_custody" ] || fail 'root key or operator pin changed'
docker restart hikyo-server-1 >/dev/null
wait_ready b >/dev/null || fail 'target did not survive a restart'

log 5 stop failure pins the journal, source keeps serving, Reconcile releases
"$H" prepare "$CFG" "$F/descriptor-c.json" "$(img c)" >/dev/null
"$H" apply "$CFG" "$F/descriptor-c.json" "$(img c)" fail-stop 2>&1 | cut -c1-120 || true
[ "$(jq -r .phase "$JOURNAL")" = applying ] || fail 'failed stop did not pin applying'
wait_ready b >/dev/null || fail 'source stopped serving after failed stop'
if "$H" prepare "$CFG" "$F/descriptor-c.json" "$(img c)" >/dev/null 2>&1; then fail 'Prepare ignored the pin'; fi
"$H" reconcile "$CFG"
[ ! -e "$JOURNAL" ] || fail 'Reconcile kept the pin'

log 6 kill -9 the target during boot
"$H" prepare "$CFG" "$F/descriptor-c.json" "$(img c)" >/dev/null
"$H" apply "$CFG" "$F/descriptor-c.json" "$(img c)"
kill -9 "$(docker inspect --format '{{.State.Pid}}' hikyo-server-1)"
wait_ready c || fail 'target did not recover from kill'
docker inspect --format 'restarts={{.RestartCount}}' hikyo-server-1
[ "$(env_image)" = "$(img c)" ] || fail 'environment file not updated after kill'
[ "$(custody)" = "$first_custody" ] || fail 'root key or operator pin changed after kill'

log 7 older image refuses and leaves the database alone
current=$(env_image)
dc stop server >/dev/null 2>&1
database=$(sha256sum /srv/hikyo/data/hikyo.db /srv/hikyo/data/hikyo.db-wal)
set_env_image "$(img a)"
dc up --detach >/dev/null 2>&1
sleep 20
docker inspect --format 'older image: status={{.State.Status}} restarts={{.RestartCount}}' hikyo-server-1
docker logs --tail 1 hikyo-server-1 2>&1 | jq -r .err
[ "$(docker inspect --format '{{.State.Running}}' hikyo-server-1)" != true ] || [ "$(docker inspect --format '{{.State.Restarting}}' hikyo-server-1)" = true ] || defect 'older image kept running'
dc stop server >/dev/null 2>&1
[ "$(sha256sum /srv/hikyo/data/hikyo.db /srv/hikyo/data/hikyo.db-wal)" = "$database" ] || defect 'older image changed the database'
set_env_image "$current"
dc up --detach >/dev/null 2>&1
wait_ready c || fail 'target did not return after refusal'

log 8 source restart before and after a clean Prepare
fresh
docker restart hikyo-server-1 >/dev/null
wait_ready a >/dev/null || fail 'source does not survive a restart before Prepare'
"$H" prepare "$CFG" "$F/descriptor-b.json" "$(img b)" >/dev/null
docker restart hikyo-server-1 >/dev/null
if wait_ready a >/dev/null; then
	echo 'source survives a restart after Prepare'
	# Also for a source that itself arrived through staging, whose own release sits
	# in its stage cache while staging the next target overwrites the stage record.
	"$H" apply "$CFG" "$F/descriptor-b.json" "$(img b)"
	wait_ready b || fail 'B did not become ready'
	"$H" prepare "$CFG" "$F/descriptor-c.json" "$(img c)" >/dev/null
	docker restart hikyo-server-1 >/dev/null
	if wait_ready b >/dev/null; then
		echo 'staged source survives a restart after the next Prepare'
	else
		defect 'after the next Prepare a staged source no longer restarts'
	fi
else
	defect 'after Prepare alone the source no longer restarts'
	docker logs --tail 1 hikyo-server-1 2>&1 | jq -r .err
	echo 'documented recovery: select the staged target in install.env'
	set_env_image "$(jq -r .target.image "$JOURNAL")"
	dc up --detach >/dev/null 2>&1
	wait_ready b || fail 'documented recovery failed'
	"$H" prepare "$CFG" "$F/descriptor-c.json" "$(img c)" >/dev/null
	"$H" apply "$CFG" "$F/descriptor-c.json" "$(img c)"
	wait_ready c || fail 'no further update after recovery'
fi

log 9 coordinator dies between stop and up
fresh
"$H" prepare "$CFG" "$F/descriptor-b.json" "$(img b)" >/dev/null
"$H" apply "$CFG" "$F/descriptor-b.json" "$(img b)" kill-after-stop || echo "coordinator exit=$?"
[ "$(jq -r .phase "$JOURNAL")" = applying ] || fail 'journal not applying'
docker inspect --format 'server status={{.State.Status}}' hikyo-server-1
if "$H" observe "$CFG" >/dev/null 2>&1; then fail 'Observe accepted a stopped workload'; fi
if "$H" reconcile "$CFG" >/dev/null 2>&1; then fail 'Reconcile accepted a stopped workload'; fi
set_env_image "$(jq -r .target.image "$JOURNAL")"
dc up --detach >/dev/null 2>&1
wait_ready b || fail 'documented recovery failed'
[ "$(jq -r .phase "$JOURNAL")" = healthy ] || fail 'Observe did not complete the journal'
dc up --detach >/dev/null 2>&1

du -sh /srv/hikyo/upgrade
log "finished with $DEFECTS defect(s)"
[ "$DEFECTS" -eq 0 ]
