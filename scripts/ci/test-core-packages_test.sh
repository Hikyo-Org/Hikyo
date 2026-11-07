#!/usr/bin/env bash
set -euo pipefail
root=$(git rev-parse --show-toplevel)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
mkdir "$work/bin"
cat >"$work/bin/go" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
if [[ "$1" == run && "$2" == ./scripts/ci/prepare-lint-cache ]]; then
  printf 'prepared\n' >"$CORE_TEST_PREPARED"
  [[ "$CORE_TEST_FAIL" != prepare ]] || exit 95
elif [[ "$1" == list ]]; then
  case "$2" in
    ./internal/app) printf 'example/internal/app\n' ;;
    ./internal/lint) printf 'example/internal/lint\n' ;;
    ./internal/isolation) printf 'example/internal/isolation\n' ;;
    ./...) cat "$CORE_TEST_INVENTORY" ;;
    *) exit 90 ;;
  esac
elif [[ "$1" == test && "$2" == -count=1 ]]; then
  [[ -f "$CORE_TEST_PREPARED" ]] || exit 96
  shift 2
  printf '%s\n' "$@" >>"$CORE_TEST_EXECUTED"
  printf 'call\n' >>"$CORE_TEST_CALLS"
  if [[ "$*" == *internal/app* ]]; then
    [[ "$#" == 1 && $(wc -l <"$CORE_TEST_CALLS") -eq 1 ]] || exit 91
    [[ "$CORE_TEST_FAIL" != app ]] || exit 92
  elif [[ "$*" == *internal/lint* ]]; then
    [[ "$#" == 1 && $(wc -l <"$CORE_TEST_CALLS") -eq 2 ]] || exit 95
    [[ "$CORE_TEST_FAIL" != lint ]] || exit 96
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
export CORE_TEST_PREPARED="$work/prepared"
export CORE_TEST_FAIL=''
printf '%s\n' example/internal/service example/internal/app example/internal/lint example/internal/isolation example/cmd/hikyo >"$CORE_TEST_INVENTORY"
printf '%s\n' example/internal/service example/cmd/hikyo >"$work/expected-pool"
printf '%s\n' example/internal/app example/internal/lint >"$work/expected-serial"
# The frozen test_core step passes no mode; it must select the pool.
for mode in '' pool serial; do
  for failure in '' concurrent app lint; do
    rm -f "$CORE_TEST_PREPARED"
    : >"$CORE_TEST_EXECUTED"
    : >"$CORE_TEST_CALLS"
    export CORE_TEST_FAIL="$failure"
    result=0
    "$root/scripts/ci/test-core-packages.sh" ${mode:+"$mode"} >"$work/log" 2>&1 || result=$?
    expected="$work/expected-${mode:-pool}"
    # A failure injected into the other mode's packages never runs there.
    failing=''
    case "${mode:-pool}:$failure" in
      pool:concurrent | serial:app | serial:lint) failing=1 ;;
    esac
    if [[ -z "$failing" ]]; then [[ "$result" == 0 ]]; else [[ "$result" != 0 ]]; fi
    cmp "$expected" "$CORE_TEST_EXECUTED"
  done
done
if "$root/scripts/ci/test-core-packages.sh" bogus >"$work/log" 2>&1; then
  echo 'test core fixture: unknown mode accepted' >&2
  exit 1
fi
export CORE_TEST_FAIL=prepare
for mode in pool serial; do
  rm -f "$CORE_TEST_PREPARED"
  : >"$CORE_TEST_EXECUTED"
  if "$root/scripts/ci/test-core-packages.sh" "$mode" >"$work/log" 2>&1; then
    echo "test core fixture: failed lint preparation accepted in $mode" >&2
    exit 1
  fi
  [[ ! -s "$CORE_TEST_EXECUTED" ]]
done
export CORE_TEST_FAIL=''
for inventory in missing-app missing-lint missing-isolation duplicate empty-concurrent; do
  printf '%s\n' example/internal/service example/internal/app example/internal/lint example/internal/isolation >"$CORE_TEST_INVENTORY"
  case "$inventory" in
    missing-app) grep -Fvx example/internal/app "$CORE_TEST_INVENTORY" >"$work/next" ;;
    missing-lint) grep -Fvx example/internal/lint "$CORE_TEST_INVENTORY" >"$work/next" ;;
    missing-isolation) grep -Fvx example/internal/isolation "$CORE_TEST_INVENTORY" >"$work/next" ;;
    duplicate) cat "$CORE_TEST_INVENTORY" "$CORE_TEST_INVENTORY" >"$work/next" ;;
    empty-concurrent) grep -Fvx example/internal/service "$CORE_TEST_INVENTORY" >"$work/next" ;;
  esac
  mv "$work/next" "$CORE_TEST_INVENTORY"
  for mode in pool serial; do
    : >"$CORE_TEST_EXECUTED"
    if "$root/scripts/ci/test-core-packages.sh" "$mode" >"$work/log" 2>&1; then
      echo "test core fixture: invalid package inventory accepted in $mode" >&2
      exit 1
    fi
    [[ ! -s "$CORE_TEST_EXECUTED" ]]
    if [[ "$inventory" == empty-concurrent ]]; then
      grep -Fx 'test core: concurrent package inventory is empty' "$work/log" >/dev/null
    fi
  done
done
echo 'test core fixture: cache preparation, exact pool and serial coverage, app then lint ordering, failure propagation and inventory refusals passed'
