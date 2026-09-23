#!/usr/bin/env bash
set -euo pipefail
root=$(git rev-parse --show-toplevel)
runner="$root/scripts/ci/test-race-packages.sh"
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
mkdir "$work/bin"
cat >"$work/bin/go" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
if [[ "$1" == list ]]; then
  case "$2" in
    ./internal/service) printf 'example/internal/service\n' ;;
    ./internal/app) printf 'example/internal/app\n' ;;
    ./internal/isolation) printf 'example/internal/isolation\n' ;;
    ./internal/store) printf 'example/internal/store\n' ;;
    ./internal/store/upgrade) printf 'example/internal/store/upgrade\n' ;;
    ./internal/upgradegate) printf 'example/internal/upgradegate\n' ;;
    ./...) cat "$RACE_TEST_INVENTORY" ;;
    *) exit 90 ;;
  esac
elif [[ "$1" == test ]]; then
  shift
  for flag in -race -p 2 -timeout=20m -vet=off -count=1; do
    if [[ "${1:-}" != "$flag" ]]; then
      echo 'race fixture: detector, parallelism, timeout, vet or count flags changed' >&2
      exit 91
    fi
    shift
  done
  [[ "$#" -gt 0 ]] || exit 92
  printf '%s\n' "$*" >>"$RACE_TEST_EXECUTED"
  printf 'call\n' >>"$RACE_TEST_CALLS"
  package=${!#}
  suite=${package##*/}
  case "$package" in
    */internal/app | */internal/service)
      if [[ "$suite" == app ]]; then
        expected_filter=$RACE_TEST_APP_FILTER
        expected_call=$RACE_TEST_APP_CALL
      else
        expected_filter=$RACE_TEST_SERVICE_FILTER
        expected_call=$RACE_TEST_SERVICE_CALL
      fi
      if [[ "$#" != 3 || "$1" != -run || "$2" != "$expected_filter" || $(wc -l <"$RACE_TEST_CALLS") -ne "$expected_call" ]]; then
        echo 'race fixture: filtered suite shares a batch, runs out of order, or lost its filter' >&2
        exit 93
      fi
      ;;
    */internal/store)
      if [[ "$#" != 3 || "$1" != -run || "$2" != "$RACE_TEST_STORE_FILTER" ]]; then
        echo 'race fixture: pooled split suite shares a batch or lost its filter' >&2
        exit 93
      fi
      ;;
    *)
      if [[ "$#" != 1 ]]; then
        echo 'race fixture: peer package shares a batch or carries a filter' >&2
        exit 93
      fi
      ;;
  esac
  [[ "$RACE_TEST_FAIL" != "$suite" ]] || exit 94
else
  exit 96
fi
EOF
chmod +x "$work/bin/go"
export PATH="$work/bin:$PATH"
export RUNNER_TEMP="$work"
export RACE_TEST_INVENTORY="$work/inventory"
export RACE_TEST_EXECUTED="$work/executed"
export RACE_TEST_CALLS="$work/calls"
export RACE_TEST_FAIL=''
export RACE_TEST_APP_CALL=2
export RACE_TEST_SERVICE_CALL=3
export RACE_TEST_APP_FILTER='^(TestBoot|TestRestore_Drill|FuzzApp|Example)$'
export RACE_TEST_SERVICE_FILTER='^(TestService|FuzzService|Example_service)$'
export RACE_TEST_STORE_FILTER='^(TestStore|FuzzStore)$'
app_line=$(printf 'example/internal/app\t%s' "$RACE_TEST_APP_FILTER")
service_line=$(printf 'example/internal/service\t%s' "$RACE_TEST_SERVICE_FILTER")
store_line=$(printf 'example/internal/store\t%s' "$RACE_TEST_STORE_FILTER")
inventory() {
  printf '%s\n' example/internal/service example/internal/app example/internal/isolation example/internal/store \
    example/internal/store/upgrade example/internal/upgradegate example/cmd/hikyo example/cmd/tool >"$RACE_TEST_INVENTORY"
}
inventory
for scope in mixed app-only service-only filtered-only peers-only; do
  : >"$work/expected-sequential"
  case "$scope" in
    mixed)
      printf '%s\n' "$app_line" "$store_line" "$service_line" example/cmd/hikyo example/cmd/tool >"$work/shard"
      printf '%s\n' "-run $RACE_TEST_STORE_FILTER example/internal/store" example/cmd/hikyo example/cmd/tool >"$work/expected-pool"
      printf '%s\n' "-run $RACE_TEST_APP_FILTER example/internal/app" "-run $RACE_TEST_SERVICE_FILTER example/internal/service" >"$work/expected-sequential"
      export RACE_TEST_APP_CALL=4 RACE_TEST_SERVICE_CALL=5
      ;;
    app-only)
      printf '%s\n' "$app_line" >"$work/shard"
      : >"$work/expected-pool"
      printf '%s\n' "-run $RACE_TEST_APP_FILTER example/internal/app" >"$work/expected-sequential"
      export RACE_TEST_APP_CALL=1
      ;;
    service-only)
      printf '%s\n' "$service_line" >"$work/shard"
      : >"$work/expected-pool"
      printf '%s\n' "-run $RACE_TEST_SERVICE_FILTER example/internal/service" >"$work/expected-sequential"
      export RACE_TEST_SERVICE_CALL=1
      ;;
    filtered-only)
      printf '%s\n' "$service_line" "$app_line" >"$work/shard"
      : >"$work/expected-pool"
      printf '%s\n' "-run $RACE_TEST_SERVICE_FILTER example/internal/service" "-run $RACE_TEST_APP_FILTER example/internal/app" >"$work/expected-sequential"
      export RACE_TEST_APP_CALL=2 RACE_TEST_SERVICE_CALL=1
      ;;
    peers-only)
      printf '%s\n' example/cmd/hikyo "$store_line" >"$work/shard"
      printf '%s\n' "-run $RACE_TEST_STORE_FILTER example/internal/store" example/cmd/hikyo >"$work/expected-pool"
      ;;
  esac
  sort "$work/expected-pool" -o "$work/expected-pool"
  pool_calls=$(wc -l <"$work/expected-pool")
  for failure in '' hikyo store app service; do
    : >"$RACE_TEST_EXECUTED"
    : >"$RACE_TEST_CALLS"
    export RACE_TEST_FAIL="$failure"
    result=0
    "$runner" "$work/shard" >"$work/log" 2>&1 || result=$?
    expected_failure=false
    if [[ -n "$failure" ]] && grep -q "/$failure\$" "$work/expected-pool" "$work/expected-sequential"; then
      expected_failure=true
    fi
    if { [[ "$expected_failure" == true && "$result" == 0 ]]; } ||
      { [[ "$expected_failure" == false && "$result" != 0 ]]; }; then
      cat "$work/log" >&2
      echo "race fixture: incorrect failure propagation for $scope/$failure" >&2
      exit 1
    fi
    # Pool entries run concurrently in any order; app and service follow.
    awk -v calls="$pool_calls" 'NR <= calls' "$RACE_TEST_EXECUTED" | sort | cmp "$work/expected-pool" -
    awk -v calls="$pool_calls" 'NR > calls' "$RACE_TEST_EXECUTED" | cmp "$work/expected-sequential" -
  done
done
for invalid in empty duplicate isolation unknown option whitespace blank missing-file no-arg extra-arg inventory-duplicate inventory-missing-app inventory-missing-isolation inventory-missing-store app-unfiltered app-unanchored app-injection service-unfiltered service-unanchored store-unfiltered store-unanchored duplicate-service inventory-missing-service peer-filter trailing-tab; do
  inventory
  printf '%s\n' "$app_line" "$service_line" >"$work/shard"
  args=("$work/shard")
  case "$invalid" in
    empty) : >"$work/shard" ;;
    duplicate) printf '%s\n' "$app_line" >>"$work/shard" ;;
    app-unfiltered) printf '%s\n' example/internal/app example/internal/service >"$work/shard" ;;
    app-unanchored) printf 'example/internal/app\tTestBoot|TestOther\nexample/internal/service\n' >"$work/shard" ;;
    app-injection) printf 'example/internal/app\t^(TestBoot)$ -count=0\nexample/internal/service\n' >"$work/shard" ;;
    service-unfiltered) printf '%s\n' example/internal/service >"$work/shard" ;;
    service-unanchored) printf 'example/internal/service\tTestService\n' >"$work/shard" ;;
    store-unfiltered) printf '%s\n' example/internal/store >>"$work/shard" ;;
    store-unanchored) printf 'example/internal/store\tTestStore\n' >>"$work/shard" ;;
    duplicate-service) printf '%s\n' "$service_line" "$(printf 'example/internal/service\t^(TestOther)$')" >"$work/shard" ;;
    inventory-missing-service) sed -i.bak '/internal\/service$/d' "$RACE_TEST_INVENTORY" ;;
    inventory-missing-store) sed -i.bak '/internal\/store$/d' "$RACE_TEST_INVENTORY" ;;
    trailing-tab) printf '%s\t\n' "$app_line" >"$work/shard" ;;
    peer-filter) printf 'example/cmd/hikyo\t^(TestOnly)$\n' >"$work/shard" ;;
    isolation) printf '%s\n' example/internal/isolation >>"$work/shard" ;;
    unknown) printf '%s\n' example/not-in-plan >>"$work/shard" ;;
    option) printf '%s\n' -run=Nothing >>"$work/shard" ;;
    whitespace) printf '%s\n' 'example/internal/service example/cmd/hikyo' >>"$work/shard" ;;
    blank) printf '\n' >>"$work/shard" ;;
    missing-file) args=("$work/missing") ;;
    no-arg) args=() ;;
    extra-arg) args+=("extra") ;;
    inventory-duplicate) printf '%s\n' example/internal/app >>"$RACE_TEST_INVENTORY" ;;
    inventory-missing-isolation) sed -i.bak '/internal\/isolation$/d' "$RACE_TEST_INVENTORY" ;;
    inventory-missing-app) sed -i.bak '/internal\/app$/d' "$RACE_TEST_INVENTORY" ;;
  esac
  : >"$RACE_TEST_EXECUTED"
  if "$runner" "${args[@]}" >"$work/log" 2>&1; then
    echo "race fixture: invalid $invalid inventory accepted" >&2
    exit 1
  fi
  [[ ! -s "$RACE_TEST_EXECUTED" ]]
done
echo 'race fixture: exact shard coverage, one-per-entry pool, sequential app/service ordering, unchanged flags, failure propagation and inventory refusals passed'
