#!/usr/bin/env bash
# mvp-boundary O7 + ops-spec §13 / CI invariant 4: an instrumented boot+idle run
# of `hikyo server`, with NOTHING configured (no remotes, no recipients, no
# adapters, no IdPs), and with a configured mailer, originates ZERO outbound
# connections and still boots AND
# serves. strace records every outbound syscall (connect/sendto/sendmsg); the
# run fails if any targets a non-loopback address, or if the server cannot serve.
#
# Linux-only: it depends on strace and syscall tracing. Runs in CI, not on a
# developer's macOS box.
set -euo pipefail

command -v strace >/dev/null || { echo "no-egress: strace is required"; exit 2; }

work="$(mktemp -d)"
child=""
cleanup() {
	if [ -n "$child" ]; then
		kill -KILL "$child" 2>/dev/null || true
		wait "$child" 2>/dev/null || true
	fi
	rm -rf "$work"
}
trap cleanup EXIT

if [ -n "${HIKYO_NO_EGRESS_BIN:-}" ]; then
	bin="$HIKYO_NO_EGRESS_BIN"
	if [ ! -x "$bin" ]; then
		echo "no-egress: prebuilt binary is not executable: $bin"
		exit 2
	fi
else
	bin="$work/hikyo"
	go build -o "$bin" ./cmd/hikyo
fi

# Environment inputs are only the pre-adoption seed. Give each probe its own
# empty state so neither run silently reuses the other's adopted configuration.
# Clear inherited mail inputs to prove the absent case, including file tiers.
unset HIKYO_MAIL_ADDR HIKYO_MAIL_TLS HIKYO_MAIL_USER HIKYO_MAIL_PASSWORD \
	HIKYO_MAIL_PASSWORD_FILE HIKYO_MAIL_FROM HIKYO_MAIL_EHLO \
	HIKYO_MAIL_ALLOWED_CIDRS HIKYO_MAIL_CA_FILE

probe() {
local mode="$1"
port=47811
ops_port=47812
origin="http://127.0.0.1:${port}"
ops_origin="http://127.0.0.1:${ops_port}"
trace="$work/$mode-net.log"
# The per-syscall trace goes to strace's own -o file; strace's diagnostics AND
# the server's slog (which writes "boot complete" to stderr) share serverlog.
serverlog="$work/$mode-server.log"

export HIKYO_STATE_DIR="$work/$mode-state"
export HIKYO_DB="sqlite:$work/$mode-hikyo.db"
mkdir -p "$HIKYO_STATE_DIR"

# Fail early if the port is already answering — otherwise a stray listener could
# satisfy the health checks below while hikyo silently fails to bind.
if curl -sf "${origin}/api/v1/meta" >/dev/null 2>&1 || curl -sf "${ops_origin}/healthz" >/dev/null 2>&1; then
	echo "no-egress: public or operational port already answering before boot"
	exit 1
fi

# Trace TCP connect(2) AND UDP sendto/sendmsg(2): both are outbound paths, and a
# UDP send needs no connect(), so tracing connect alone would miss it.
# --kill-on-exit makes strace terminate every tracee when the tracer exits. Stop
# only strace's exact PID below: a negative process-group signal can also reach
# the GitHub runner when setsid forks before exec on a hosted runner. SIGKILL is
# intentional: strace defers SIGTERM while tracing, whereas PTRACE_O_EXITKILL
# guarantees that a killed tracer cannot leave the server behind.
strace --kill-on-exit -f -e trace=connect,sendto,sendmsg -o "$trace" \
	"$bin" server --dev --listen "127.0.0.1:${port}" --operational-listen "127.0.0.1:${ops_port}" >"$serverlog" 2>&1 &
child=$!

# Boots AND serves with outbound unavailable (CI invariant 4): both liveness
# (/healthz) and readiness (/readyz = DB reachable + keyring loaded + migrations
# current). A 200/503 split would mean the process is up but cannot serve.
ready=0
for _ in $(seq 1 60); do
	if curl -sf "${ops_origin}/healthz" >/dev/null 2>&1 && curl -sf "${ops_origin}/readyz" >/dev/null 2>&1; then
		ready=1
		break
	fi
	sleep 0.5
done
if [ "$ready" -ne 1 ]; then
	echo "no-egress: server never became healthy AND ready at ${ops_origin}"
	sed -n '1,120p' "$serverlog" >&2
	exit 1
fi
# Confirm it is OUR hikyo that bound the port, not a pre-existing listener.
if ! grep -q "boot complete" "$serverlog" || ! grep -q "addr=127.0.0.1:${port}" "$serverlog"; then
	echo "no-egress: could not confirm hikyo bound ${origin} (boot-complete log absent)"
	sed -n '1,120p' "$serverlog" >&2
	exit 1
fi

# Idle, so a background poller or lazy dialer would have to reach out here.
sleep 3
curl -sf "${ops_origin}/readyz" >/dev/null 2>&1 || true

kill -KILL "$child" 2>/dev/null || true
wait "$child" 2>/dev/null || true
child=""

# strace must actually have traced — a missing file or an attach error means we
# proved nothing about egress and must not report a green result.
if [ ! -f "$trace" ]; then
	echo "no-egress: strace produced no trace file; egress was not instrumented"
	sed -n '1,120p' "$serverlog" >&2 || true
	exit 1
fi
if grep -qiE 'strace: (ptrace|could not attach|test_ptrace|failed)' "$serverlog"; then
	echo "no-egress: strace reported an attach/trace error; instrumentation unreliable"
	sed -n '1,120p' "$serverlog" >&2
	exit 1
fi

# Any connect/sendto/sendmsg to an AF_INET/AF_INET6 address outside loopback
# (127/8, ::1) is egress. inet_addr("127.x.x.x") and "::1" are loopback; a
# connected-UDP sendto carries a NULL address and its connect() was already
# traced above.
egress="$(grep -E '(connect|sendto|sendmsg)\([0-9]+, .*sa_family=AF_INET6?' "$trace" \
	| grep -vE 'inet_addr\("127\.|"::1"|::ffff:127\.|sin6_addr=.*"::1"' || true)"
if [ -n "$egress" ]; then
	echo "no-egress: $mode boot+idle attempted outbound traffic:"
	echo "$egress"
	exit 1
fi

echo "no-egress: $mode OK - booted, served, ready, and originated 0 non-loopback connect/sendto/sendmsg"
}

probe unconfigured

# A fully configured relay with an explicit address-policy exception must still
# remain idle. A mistaken boot-time probe would generate a traced non-loopback
# connect, even though no relay is listening at this documentation-only address.
export HIKYO_MAIL_ADDR="198.51.100.1:465"
export HIKYO_MAIL_TLS="implicit"
export HIKYO_MAIL_FROM="Hikyo <hikyo@example.com>"
export HIKYO_MAIL_USER="no-egress-fixture"
export HIKYO_MAIL_PASSWORD="no-egress-fixture"
export HIKYO_MAIL_ALLOWED_CIDRS="198.51.100.1/32"
probe configured-mailer
