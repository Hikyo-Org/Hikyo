#!/bin/sh
# Never changes the host firewall: all privileged tools are disposable fixtures.
set -eu
script_dir=$(CDPATH='' cd -- "$(dirname "$0")" && pwd)
fixture=$(mktemp -d "${TMPDIR:-/tmp}/hikyo-ticket-network.XXXXXX")
trap 'rm -rf "$fixture"' EXIT HUP INT TERM
mkdir "$fixture/bin"
export TICKET_FIXTURE_LOG="$fixture/calls"
cat >"$fixture/bin/uname" <<'EOF'
#!/bin/sh
printf 'Darwin\n'
EOF
cat >"$fixture/bin/sudo" <<'EOF'
#!/bin/sh
[ "$1" = -n ] || exit 1
shift
exec "$@"
EOF
cat >"$fixture/bin/pfctl" <<'EOF'
#!/bin/sh
set -eu
printf 'pfctl %s\n' "$*" >>"$TICKET_FIXTURE_LOG"
case "$*" in
  '-sr')
    [ "${TICKET_FIXTURE_MODE:-}" != missing-anchor ] || exit 0
    printf 'anchor "com.apple/*" all\n'
    [ "${TICKET_FIXTURE_MODE:-}" != rules-read-failure ]
    ;;
  '-a com.apple/hikyo-notary -f -')
    IFS= read -r rule
    [ "$rule" = 'block return out quick proto udp from any to any port 443' ]
    [ "${TICKET_FIXTURE_MODE:-}" != load-failure ]
    ;;
  '-E')
    [ "${TICKET_FIXTURE_MODE:-}" != enable-failure ] || exit 1
    printf 'pf enabled\nToken : 123456\n' >&2
    ;;
  '-a com.apple/hikyo-notary -F rules') [ "${TICKET_FIXTURE_CLEANUP_FAIL:-false}" = false ] ;;
  '-X 123456') [ "${TICKET_FIXTURE_MODE:-}" != release-failure ] ;;
  *) printf 'unexpected firewall operation\n' >&2; exit 1 ;;
esac
EOF
cat >"$fixture/bin/payload" <<'EOF'
#!/bin/sh
printf 'payload %s\n' "$*" >>"$TICKET_FIXTURE_LOG"
exit "${TICKET_FIXTURE_PAYLOAD_STATUS:-0}"
EOF
chmod +x "$fixture/bin/"*
export PATH="$fixture/bin:$PATH" GITHUB_ACTIONS=true RUNNER_ENVIRONMENT=github-hosted
for scenario in success payload-failure cleanup-failure local-host missing-anchor rules-read-failure load-failure enable-failure release-failure; do
  : >"$TICKET_FIXTURE_LOG"
  export TICKET_FIXTURE_PAYLOAD_STATUS=0 TICKET_FIXTURE_CLEANUP_FAIL=false
  export TICKET_FIXTURE_MODE="$scenario"
  RUNNER_ENVIRONMENT=github-hosted
  expected=0
  case "$scenario" in
    payload-failure) TICKET_FIXTURE_PAYLOAD_STATUS=42; expected=42 ;;
    cleanup-failure) TICKET_FIXTURE_CLEANUP_FAIL=true; expected=1 ;;
    local-host) RUNNER_ENVIRONMENT=self-hosted; expected=1 ;;
    missing-anchor | rules-read-failure | load-failure | enable-failure | release-failure) expected=1 ;;
  esac
  actual=0
  sh "$script_dir/with-apple-ticket-network.sh" payload 'argument with spaces' >"$fixture/out" 2>"$fixture/err" || actual=$?
  [ "$actual" -eq "$expected" ] || { cat "$fixture/err" >&2; exit 1; }
  if [ "$scenario" = local-host ]; then
    [ ! -s "$TICKET_FIXTURE_LOG" ]
    continue
  fi
  case "$scenario" in
    missing-anchor | rules-read-failure | load-failure | enable-failure)
      if grep -F payload "$TICKET_FIXTURE_LOG"; then exit 1; fi
      case "$scenario" in
        load-failure | enable-failure) grep -Fx 'pfctl -a com.apple/hikyo-notary -F rules' "$TICKET_FIXTURE_LOG" >/dev/null ;;
      esac
      continue
      ;;
  esac
  grep -Fx 'payload argument with spaces' "$TICKET_FIXTURE_LOG" >/dev/null
  grep -Fx 'pfctl -a com.apple/hikyo-notary -F rules' "$TICKET_FIXTURE_LOG" >/dev/null
  grep -Fx 'pfctl -X 123456' "$TICKET_FIXTURE_LOG" >/dev/null
done
printf 'Apple ticket network fixture: scoped TCP fallback, cleanup and exit status passed\n'
