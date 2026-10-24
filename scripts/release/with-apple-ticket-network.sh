#!/bin/sh
# Work around actions/runner-images#14924 without replacing the runner's rules.
set +x
set -eu
fail() { printf 'Apple ticket network: %s\n' "$*" >&2; exit 1; }
[ "$#" -gt 0 ] || fail 'usage: with-apple-ticket-network.sh COMMAND [ARGUMENT...]'
[ "$(uname -s)" = Darwin ] || fail 'macOS is required'
[ "${GITHUB_ACTIONS:-}" = true ] && [ "${RUNNER_ENVIRONMENT:-}" = github-hosted ] ||
  fail 'TCP fallback is restricted to disposable GitHub-hosted runners'

# The standard macOS ruleset evaluates this wildcard anchor. Refuse to install
# an ineffective child rule if a runner image changes the parent ruleset.
rules=$(sudo -n pfctl -sr) || fail 'cannot inspect packet-filter rules'
printf '%s\n' "$rules" | grep -Fx 'anchor "com.apple/*" all' >/dev/null ||
  fail 'missing standard macOS packet-filter anchor'
anchor=com.apple/hikyo-notary
token=
cleanup() {
  status=$?
  trap - EXIT HUP INT TERM
  if ! sudo -n pfctl -a "$anchor" -F rules; then status=1; fi
  if [ -n "$token" ]; then
    if ! sudo -n pfctl -X "$token"; then status=1; fi
  fi
  exit "$status"
}
trap cleanup EXIT
trap 'exit 1' HUP INT TERM
# Reject QUIC so Apple's ticket client uses HTTPS over TCP instead. Other
# rules, TCP traffic and the preexisting firewall enablement are preserved.
printf '%s\n' 'block return out quick proto udp from any to any port 443' |
  sudo -n pfctl -a "$anchor" -f -
enablement=$(sudo -n pfctl -E 2>&1) || fail 'cannot enable packet filter'
token=$(printf '%s\n' "$enablement" | sed -n 's/^Token : \([0-9][0-9]*\)$/\1/p')
case "$token" in '' | *[!0-9]*) fail 'packet filter returned no release token' ;; esac
"$@"
