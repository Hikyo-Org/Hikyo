#!/bin/sh
set -eu

if [ "$#" -ne 1 ]; then
	printf 'usage: %s IMAGE\n' "$0" >&2
	exit 2
fi

for command in docker curl grep sed tr; do
	command -v "$command" >/dev/null || {
		printf 'candidate image UI smoke: %s is required\n' "$command" >&2
		exit 2
	}
done

image=$1
work=$(mktemp -d "${TMPDIR:-/tmp}/hikyo-image-ui-smoke.XXXXXX")
container=
cleanup() {
	if [ -n "$container" ]; then
		docker rm --force "$container" >/dev/null 2>&1 || true
	fi
	rm -rf "$work"
}
trap cleanup EXIT HUP INT TERM

fail() {
	printf 'candidate image UI smoke: %s\n' "$1" >&2
	if [ -n "$container" ]; then
		docker logs "$container" >&2 || true
	fi
	exit 1
}

# Boot the candidate itself with an isolated, zero-authority fixture. The
# update channel is disabled so this packaging proof cannot make outbound
# release checks; only the host-bound HTTP port is exposed.
container=$(docker run --detach --rm \
	--read-only \
	--tmpfs /tmp:rw,noexec,nosuid,nodev,size=64m \
	--env HIKYO_DB=sqlite:/tmp/hikyo-release-smoke.db \
	--env HIKYO_STATE_DIR=/tmp/hikyo-release-smoke-state \
	--env HIKYO_TRUSTED_PROXY_CIDRS=127.0.0.1/32 \
	--env HIKYO_UPDATE_CHANNEL=off \
	--publish 127.0.0.1::8080 \
	"$image" server --dev --listen=0.0.0.0:8080 --operational-listen=0.0.0.0:8081)

published=$(docker port "$container" 8080/tcp) || fail 'public port was not published'
port=${published##*:}
case "$port" in
	'' | *[!0-9]*) fail "invalid published port: $published" ;;
esac
origin=http://127.0.0.1:$port
html=$work/index.html
html_headers=$work/index.headers

status=
attempt=0
while [ "$attempt" -lt 60 ]; do
	attempt=$((attempt + 1))
	if status=$(curl --silent --show-error --connect-timeout 1 --max-time 2 \
		--header 'Accept: text/html' --dump-header "$html_headers" \
		--output "$html" --write-out '%{http_code}' "$origin/" 2>/dev/null); then
		[ "$status" = 200 ] && break
	fi
	if [ "$(docker inspect --format '{{.State.Running}}' "$container" 2>/dev/null || true)" != true ]; then
		fail 'container exited before serving the SPA'
	fi
	sleep 0.5
done
[ "$status" = 200 ] || fail "GET / returned ${status:-no response}, want 200"

tr -d '\r' <"$html_headers" | grep -Eiq '^content-type: text/html(;|$)' ||
	fail 'GET / did not return text/html'
grep -F '<div id="root"></div>' "$html" >/dev/null ||
	fail 'GET / did not return the SPA HTML shell'

asset=$(grep -Eo 'src="/assets/[^"]+\.js"' "$html" |
	sed -n '1{s/^src="//;s/"$//;p;}')
[ -n "$asset" ] || fail 'SPA HTML did not reference a JavaScript asset'
printf '%s\n' "$asset" | grep -Eq '^/assets/[^/?#]+-[A-Za-z0-9_-]{8,}\.js$' ||
	fail "SPA JavaScript asset is not content hashed: $asset"

asset_headers=$work/asset.headers
asset_body=$work/asset.js
curl --fail --silent --show-error --dump-header "$asset_headers" \
	--output "$asset_body" "$origin$asset" || fail "GET $asset failed"
[ -s "$asset_body" ] || fail "GET $asset returned an empty body"
tr -d '\r' <"$asset_headers" | grep -Eiq '^content-type: text/javascript(;|$)' ||
	fail "GET $asset did not return text/javascript"

printf 'candidate image UI smoke: %s serves SPA HTML and %s as text/javascript\n' \
	"$image" "$asset"
