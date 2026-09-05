#!/usr/bin/env bash
set -euo pipefail
root=$(git rev-parse --show-toplevel)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
mkdir "$work/bin"
cat >"$work/bin/go" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
if [[ "$1" == list ]]; then
  case "$2" in
    ./internal/app) printf 'example/internal/app\n' ;;
    ./internal/isolation) printf 'example/internal/isolation\n' ;;
    ./...) cat "$CORE_TEST_INVENTORY" ;;
    *) exit 90 ;;
  esac
elif [[ "$1" == test && "$2" == -count=1 ]]; then
  shift 2
  printf '%s\n' "$@" >>"$CORE_TEST_EXECUTED"
  printf 'call\n' >>"$CORE_TEST_CALLS"
  if [[ "$*" == *internal/app* ]]; then
    [[ "$#" == 1 && $(wc -l <"$CORE_TEST_CALLS") -eq 2 ]] || exit 91
    [[ "$CORE_TEST_FAIL" != app ]] || exit 92
  else
    [[ "$CORE_TEST_FAIL" != concurrent ]] || exit 93
  fi
else
  exit 94
fi
EOF
chmod +x "$work/bin/go"
export PATH="$work/bin:$PATH"
export RUNNER_TEMP="$work"
export CORE_TEST_INVENTORY="$work/inventory"
export CORE_TEST_EXECUTED="$work/executed"
export CORE_TEST_CALLS="$work/calls"
export CORE_TEST_FAIL=''
printf '%s\n' example/internal/service example/internal/app example/internal/isolation example/cmd/hikyo >"$CORE_TEST_INVENTORY"
printf '%s\n' example/internal/service example/cmd/hikyo example/internal/app >"$work/expected"
for failure in '' concurrent app; do
  : >"$CORE_TEST_EXECUTED"
  : >"$CORE_TEST_CALLS"
  export CORE_TEST_FAIL="$failure"
  result=0
  "$root/scripts/ci/test-core-packages.sh" >"$work/log" 2>&1 || result=$?
  if [[ "$failure" == '' ]]; then [[ "$result" == 0 ]]; else [[ "$result" != 0 ]]; fi
  cmp "$work/expected" "$CORE_TEST_EXECUTED"
done
for inventory in missing-app missing-isolation duplicate empty-concurrent; do
  printf '%s\n' example/internal/service example/internal/app example/internal/isolation >"$CORE_TEST_INVENTORY"
  case "$inventory" in
    missing-app) grep -Fvx example/internal/app "$CORE_TEST_INVENTORY" >"$work/next" ;;
    missing-isolation) grep -Fvx example/internal/isolation "$CORE_TEST_INVENTORY" >"$work/next" ;;
    duplicate) cat "$CORE_TEST_INVENTORY" "$CORE_TEST_INVENTORY" >"$work/next" ;;
    empty-concurrent) grep -Fvx example/internal/service "$CORE_TEST_INVENTORY" >"$work/next" ;;
  esac
  mv "$work/next" "$CORE_TEST_INVENTORY"
  : >"$CORE_TEST_EXECUTED"
  if "$root/scripts/ci/test-core-packages.sh" >"$work/log" 2>&1; then
    echo 'test core fixture: invalid package inventory accepted' >&2
    exit 1
  fi
  [[ ! -s "$CORE_TEST_EXECUTED" ]]
  if [[ "$inventory" == empty-concurrent ]]; then
    grep -Fx 'test core: concurrent package inventory is empty' "$work/log" >/dev/null
  fi
done
echo 'test core fixture: exact coverage, app ordering, failure propagation and inventory refusals passed'
