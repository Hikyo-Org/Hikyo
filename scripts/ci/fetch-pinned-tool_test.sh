#!/usr/bin/env bash
# Fixture for fetch-pinned-tool.sh: a PATH-shimmed curl stands in for the network.
set -euo pipefail

script_dir=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT HUP INT TERM

fail() {
	printf 'fetch-pinned-tool fixture failed: %s\n' "$1" >&2
	exit 1
}

# Isolated copy of the helper so the fixture owns its pin files.
mkdir -p "$work/bin" "$work/ci/pinned-tools"
cp "$script_dir/fetch-pinned-tool.sh" "$work/ci/"
payload='pinned payload'
good=$(printf '%s' "$payload" | shasum -a 256 | cut -d' ' -f1)
printf 'url=https://example.invalid/tool\nsha256=%s\n' "$good" >"$work/ci/pinned-tools/tool.pin"

# The shim records its arguments and writes $FAKE_BODY to --output.
cat >"$work/bin/curl" <<'SHIM'
#!/bin/sh
set -eu
printf '%s\n' "$*" >>"$FIXTURE_DIR/curl.log"
[ "${FAKE_FAIL:-}" != true ] || exit 22
out=
while [ "$#" -gt 0 ]; do
	[ "$1" != --output ] || out=$2
	shift
done
printf '%s' "$FAKE_BODY" >"$out"
SHIM
chmod +x "$work/bin/curl"
export FIXTURE_DIR="$work" PATH="$work/bin:$PATH"
export PINNED_TOOL_CACHE="$work/cache"
helper="$work/ci/fetch-pinned-tool.sh"
cached="$work/cache/tool/$good"

# Network fetch: bounded exponential backoff flags, no fixed delay.
FAKE_BODY=$payload "$helper" tool "$work/out1" 2>/dev/null
[[ $(cat "$work/out1") == "$payload" ]] || fail 'output differs from payload'
[[ -f $cached ]] || fail 'verified download was not cached'
[[ $(wc -l <"$work/curl.log") -eq 1 ]] || fail 'expected exactly one curl call'
for flag in '--fail' '--location' "--proto =https" '--tlsv1.2' '--retry 8' '--retry-all-errors' \
	'--retry-max-time 300' '--connect-timeout 20' '--max-time 120' 'https://example.invalid/tool'; do
	grep -F -- "$flag" "$work/curl.log" >/dev/null || fail "curl call lacks $flag"
done
if grep -F -- '--retry-delay' "$work/curl.log" >/dev/null; then fail 'curl call must not set --retry-delay'; fi

# Cache hit: no network call, clear log line.
: >"$work/curl.log"
FAKE_FAIL=true "$helper" tool "$work/out2" 2>"$work/err"
grep -F 'verified cached tool' "$work/err" >/dev/null || fail 'cache hit not logged'
[[ ! -s $work/curl.log ]] || fail 'cache hit used the network'
[[ $(cat "$work/out2") == "$payload" ]] || fail 'cached output differs'

# Corrupted cache: discarded and fetched again.
printf 'tampered' >"$cached"
FAKE_BODY=$payload "$helper" tool "$work/out3" 2>/dev/null
[[ $(cat "$work/out3") == "$payload" ]] || fail 'corrupted cache was served'
[[ $(wc -l <"$work/curl.log") -eq 1 ]] || fail 'corrupted cache did not trigger a re-fetch'
[[ $(cat "$cached") == "$payload" ]] || fail 'corrupted cache was not replaced'

# Prefetch fills the cache and writes no output.
rm -rf "$work/cache"
FAKE_BODY=$payload "$helper" --prefetch tool 2>/dev/null
[[ -f $cached ]] || fail 'prefetch did not fill the cache'

# A mismatching network copy fails closed: no cache entry, no output, no temp file.
rm -rf "$work/cache"
if FAKE_BODY=wrong "$helper" tool "$work/out4" 2>/dev/null; then fail 'bad network checksum was accepted'; fi
[[ ! -e $work/out4 ]] || fail 'bad network copy reached the output'
[[ -z $(find "$work/cache" -type f) ]] || fail 'bad network copy was cached or left behind'

# Download failure is nonzero and writes nothing.
if FAKE_FAIL=true "$helper" tool "$work/out5" 2>/dev/null; then fail 'download failure was accepted'; fi
[[ ! -e $work/out5 ]] || fail 'failed download reached the output'

# Malformed pins and names are refused before any network call.
: >"$work/curl.log"
pin="$work/ci/pinned-tools/bad.pin"
refuse() {
	if FAKE_BODY=$payload "$helper" "${2:-bad}" "$work/outbad" 2>/dev/null; then fail "accepted $1"; fi
}
printf 'url=http://example.invalid/tool\nsha256=%s\n' "$good" >"$pin"; refuse 'http url'
printf 'url=https://example.invalid/tool\nsha256=%s\n' "$(printf %s "$good" | tr a-f A-F)" >"$pin"; refuse 'uppercase hex'
printf 'url=https://example.invalid/tool\nsha256=abc\n' >"$pin"; refuse 'short sha'
printf 'sha256=%s\nurl=https://example.invalid/tool\n' "$good" >"$pin"; refuse 'swapped keys'
printf 'url=https://example.invalid/tool\nsha256=%s\nextra=1\n' "$good" >"$pin"; refuse 'extra line'
printf 'url=https://example.invalid/tool\nsha256=%s\nextra=1' "$good" >"$pin"; refuse 'unterminated extra line'
printf 'url=https://example.invalid/tool\n' >"$pin"; refuse 'missing sha'
refuse 'missing pin' absent
refuse 'path traversal name' ../tool
[[ ! -s $work/curl.log ]] || fail 'a refused pin reached the network'
if "$helper" tool 2>/dev/null; then fail 'accepted missing output argument'; fi

# Without any SHA-256 tool it refuses clearly and keeps a good cached file.
FAKE_BODY=$payload "$helper" tool "$work/out6" 2>/dev/null
mkdir "$work/nosha"
for tool in bash dirname mkdir mktemp cp rm mv cat printf; do ln -s "$(command -v "$tool")" "$work/nosha/$tool"; done
ln -s "$work/bin/curl" "$work/nosha/curl"
if PATH="$work/nosha" "$helper" tool "$work/out7" 2>"$work/err"; then fail 'accepted a missing SHA-256 tool'; fi
grep -F 'no SHA-256 tool' "$work/err" >/dev/null || fail 'missing SHA-256 tool not reported'
[[ -f $cached ]] || fail 'missing SHA-256 tool deleted the cached file'

printf 'fetch-pinned-tool fixture: backoff flags, verified cache, re-fetch, fail-closed and pin refusal verified\n'
