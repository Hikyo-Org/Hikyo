#!/bin/sh
# Shell variables below are literal fixture text.
# shellcheck disable=SC2016
set -eu

script_dir=$(CDPATH='' cd -- "$(dirname "$0")" && pwd)
probe="$script_dir/no-egress.sh"

fail() {
	printf 'no-egress lifecycle fixture failed: %s\n' "$1" >&2
	exit 1
}

grep -F -- 'strace --kill-on-exit -f' "$probe" >/dev/null ||
	fail 'strace does not own tracee cleanup when the tracer exits'
grep -F -- 'kill -KILL "$child"' "$probe" >/dev/null ||
	fail 'the probe does not stop the tracer by its exact PID'
if grep -Eq 'kill[[:space:]][^#]*"?-\$\{?[[:alnum:]_]+' "$probe"; then
	fail 'the probe can signal an inherited process group'
fi
if grep -Eq '^[[:space:]]*pgid=' "$probe"; then
	fail 'the probe still derives a process group for teardown'
fi

printf 'no-egress lifecycle fixture: tracer-owned cleanup cannot signal the runner process group\n'

# Exercise both boots without Linux strace or a real network listener. The
# strace stand-in records effective seed inputs and emits a deterministic trace;
# the curl stand-in only becomes ready after that exact child starts.
fixture=$(mktemp -d)
trap 'rm -rf "$fixture"' EXIT HUP INT TERM
mkdir "$fixture/bin"
cat >"$fixture/bin/strace" <<'FIXTURE'
#!/bin/sh
set -eu
while [ "$#" -gt 0 ]; do
	if [ "$1" = -o ]; then trace=$2; break; fi
	shift
done
printf '%s|%s|%s|%s\n' "${HIKYO_MAIL_ADDR-}" "${HIKYO_MAIL_TLS-}" "${HIKYO_MAIL_PASSWORD_FILE-}" "$HIKYO_STATE_DIR" >>"$FIXTURE_RECORD"
: >"$trace"
if [ "${FIXTURE_EGRESS-}" = yes ] && [ -n "${HIKYO_MAIL_ADDR-}" ]; then
	printf 'connect(5, {sa_family=AF_INET, sin_port=htons(465), sin_addr=inet_addr("198.51.100.1")}, 16) = -1\n' >"$trace"
fi
printf 'boot complete addr=127.0.0.1:47811\n'
: >"$HIKYO_STATE_DIR/listening"
exec /bin/sleep 60
FIXTURE
cat >"$fixture/bin/curl" <<'FIXTURE'
#!/bin/sh
test -f "$HIKYO_STATE_DIR/listening"
FIXTURE
cat >"$fixture/bin/sleep" <<'FIXTURE'
#!/bin/sh
exec /bin/sleep 0.02
FIXTURE
cat >"$fixture/bin/hikyo" <<'FIXTURE'
#!/bin/sh
exit 0
FIXTURE
chmod +x "$fixture/bin/strace" "$fixture/bin/curl" "$fixture/bin/sleep" "$fixture/bin/hikyo"
export FIXTURE_RECORD="$fixture/record"
# Inherited partial configuration must not contaminate the absent probe.
if ! PATH="$fixture/bin:$PATH" HIKYO_NO_EGRESS_BIN="$fixture/bin/hikyo" \
	HIKYO_MAIL_PASSWORD_FILE=/do-not-open bash "$probe" >"$fixture/output" 2>&1; then
	cat "$fixture/output" >&2
	fail 'both deterministic boot probes did not succeed'
fi
[ "$(wc -l <"$FIXTURE_RECORD" | tr -d ' ')" = 2 ] || fail 'expected exactly two instrumented boots'
sed -n '1p' "$FIXTURE_RECORD" | grep -F '|||/' >/dev/null || fail 'absent probe retained inherited mail settings'
sed -n '2p' "$FIXTURE_RECORD" | grep -F '198.51.100.1:465|implicit||/' >/dev/null || fail 'second probe lacks a complete mail seed'
first_state=$(sed -n '1s/.*|//p' "$FIXTURE_RECORD")
second_state=$(sed -n '2s/.*|//p' "$FIXTURE_RECORD")
[ "$first_state" != "$second_state" ] || fail 'probes share adopted runtime state'
if PATH="$fixture/bin:$PATH" HIKYO_NO_EGRESS_BIN="$fixture/bin/hikyo" \
	FIXTURE_EGRESS=yes bash "$probe" >"$fixture/refusal" 2>&1; then
	fail 'configured mailer egress was accepted'
fi
grep -F 'configured-mailer boot+idle attempted outbound traffic' "$fixture/refusal" >/dev/null ||
	fail 'configured probe did not report its outbound refusal'
printf 'no-egress lifecycle fixture: absent and configured boots isolated; configured egress refused\n'
