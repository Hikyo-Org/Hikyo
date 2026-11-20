#!/bin/sh
# Driven checks of the renderer policy and downloads (ADR D10, D11) against a
# stand-in instance. macOS with a GUI session only. It builds a probe build,
# runs it for a few seconds and asserts on the log.
#
#   sh probe/run.sh          # from desktop/
set -eu
seeded=""
cd "$(dirname "$0")/.."
work=$(mktemp -d)
trap 'kill $server 2>/dev/null || true; pkill -f "$work/probe" 2>/dev/null || true; [ -z "${seeded:-}" ] || for h in $seeded; do security delete-generic-password -s io.hikyo.desktop -a "$h" >/dev/null 2>&1 || true; done; rm -rf "$work"' EXIT
go build -tags probe -o "$work/probe" . 2>/dev/null
python3 probe/instance.py 2>"$work/server.log" &
server=$!
sleep 1
run() { # script, extra env...
	script=$1
	shift
	env HIKYO_DESKTOP_HOME="$work/home" HIKYO_DESKTOP_ALLOW_LOOPBACK_HTTP=1 HIKYO_DESKTOP_PROBE_SEED=http://127.0.0.1:18765 \
		HIKYO_DESKTOP_PROBE_SAVE_DIR="$work/save" HIKYO_DESKTOP_PROBE_JS="$script" "$@" \
		"$work/probe" >"$work/app.log" 2>&1 &
	pid=$!
	sleep 12
	kill $pid 2>/dev/null || true
	wait $pid 2>/dev/null || true
	seeded="${seeded:-} $(sed -n 's/.*probe-js \[\(c-[0-9a-f]*\)\].*/\1/p' "$work/app.log" | sort -u | tr '\n' ' ')"
}
mkdir -p "$work/save"

run "$PWD/probe/wails-inert.js"
for want in '"runtime":403' '"flags":403' '"external":false' '"handlers":null'; do
	grep -q "probe-js \[c-.*$want" "$work/app.log" || { echo "FAIL wails-inert: missing $want"; cat "$work/app.log"; exit 1; }
done
! grep -q '"invoke":"called"' "$work/app.log" || { echo "FAIL wails-inert: _wails.invoke reached native code"; exit 1; }
grep -q '"path":"/second"' "$work/app.log" || { echo "FAIL wails-inert: no check after navigation"; exit 1; }
echo "ok wails-inert: _wails is present but inert; no message handler, /wails/ is 403"

run "$PWD/probe/downloads.js"
for f in via-route.csv bytes.csv audit-x.csv; do
	[ -f "$work/save/$f" ] || { echo "FAIL downloads: $f not saved"; cat "$work/app.log"; exit 1; }
done
[ "$(grep -c 'GET /api/v1/export?nav=1' "$work/server.log")" = 1 ] || { echo "FAIL downloads: the navigation made more than one request"; exit 1; }
echo "ok downloads: route path, route bytes, and a main-frame /api/ navigation each saved once"
