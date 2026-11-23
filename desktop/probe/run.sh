#!/bin/sh
# Driven checks of the renderer policy and downloads (ADR D10, D11) against a
# stand-in instance. macOS with a GUI session only. It builds a probe build,
# runs it for a few seconds and asserts on the log.
#
#   sh probe/run.sh          # from desktop/
set -eu
seeded=""
server=""
cd "$(dirname "$0")/.."
work=$(mktemp -d)
cleanup() {
	[ -z "$server" ] || kill "$server" 2>/dev/null || true
	pkill -f "$work/probe" 2>/dev/null || true
	for h in $seeded; do
		security delete-generic-password -s app.hikyo.desktop -a "$h" >/dev/null 2>&1 || true
	done
	rm -rf "$work"
}
trap cleanup EXIT
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
	# The host ids come from the seed record the app wrote, not from the
	# renderer log, so an item seeded before the first page ran is still cleaned.
	seeded="${seeded:-} $(cat "$work/home/seeded" 2>/dev/null | tr '\n' ' ')"
}
mkdir -p "$work/save"

run "$PWD/probe/wails-inert.js"
# Every page (the root and /second) must report every expected value.
python3 -I - "$work/app.log" <<'PY' || { echo "FAIL wails-inert"; cat "$work/app.log"; exit 1; }
import json, re, sys
want = {"control": 200, "runtimeBlocked": True, "flagsBlocked": True, "external": False, "handlers": None}
seen = {}
for line in open(sys.argv[1]):
    m = re.search(r"probe-js \[c-[0-9a-f]+\]: (\{.*\})", line)
    if m:
        rec = json.loads(m.group(1))
        if "path" in rec:
            seen[rec["path"]] = rec
for path in ("/", "/second"):
    rec = seen.get(path)
    if rec is None:
        sys.exit("no record for " + path)
    for key, value in want.items():
        if rec.get(key, "missing") != value:
            sys.exit("%s: %s is %r, want %r" % (path, key, rec.get(key, "missing"), value))
PY
! grep -q '"invoke":"called"' "$work/app.log" || { echo "FAIL wails-inert: _wails.invoke reached native code"; exit 1; }
grep -q '"path":"/second"' "$work/app.log" || { echo "FAIL wails-inert: no check after navigation"; exit 1; }
echo "ok wails-inert: _wails is present but inert; no message handler, /wails/ is 403"

run "$PWD/probe/downloads.js"
for f in via-route.csv bytes.csv audit-x.csv; do
	[ -f "$work/save/$f" ] || { echo "FAIL downloads: $f not saved"; cat "$work/app.log"; exit 1; }
done
# Bytes, not only names: a truncated or wrong file must fail.
[ "$(cat "$work/save/bytes.csv")" = "hi" ] || { echo "FAIL downloads: bytes.csv holds the wrong bytes"; exit 1; }
for f in via-route.csv audit-x.csv; do
	[ "$(cat "$work/save/$f")" = "$(printf 'a,b\n1,2')" ] || { echo "FAIL downloads: $f holds the wrong bytes"; exit 1; }
done
[ "$(grep -c 'GET /api/v1/export?nav=1' "$work/server.log")" = 1 ] || { echo "FAIL downloads: the navigation made more than one request"; exit 1; }
echo "ok downloads: route path, route bytes, and a main-frame /api/ navigation each saved once"
