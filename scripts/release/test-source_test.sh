#!/usr/bin/env bash
set -euo pipefail
script_dir=$(CDPATH='' cd -- "$(dirname "$0")" && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
mkdir -p "$work/repo/scripts/release" "$work/repo/scripts/ci" "$work/bin"
cp "$script_dir/test-source.sh" "$work/repo/scripts/release/"
cat >"$work/bin/go" <<'SH'
#!/usr/bin/env bash
set -euo pipefail
[[ $GOMAXPROCS == 2 && $GOFLAGS == *-p=2* ]] || exit 90
case "$*" in
  'test -p 2 -list '*)
    [[ $CASE != list-failure ]] || exit 91
    [[ $CASE != empty-inventory ]] || exit 0
    printf '%s\n' TestA FuzzB ExampleC 'ok example/isolation 0.001s'
    ;;
  'run -p 2 ./scripts/ci/analysis-shards-go isolation-race '*)
    [[ $CASE != planner-failure ]] || exit 92
    while [[ $1 != --shard ]]; do shift; done
    shard=$2
    case $shard in 0) root=TestA ;; 1) root=FuzzB ;; 2) root=ExampleC ;; *) exit 93 ;; esac
    case $CASE in
      empty-shard) [[ $shard != 1 ]] || exit 0 ;;
      duplicate) [[ $shard != 1 ]] || root=TestA ;;
      missing) [[ $shard != 1 ]] || root=TestMissing ;;
      injection) [[ $shard != 1 ]] || root='FuzzB|TestOther' ;;
    esac
    printf '%s\n' "$root"
    ;;
  'test -json -p 2 -parallel 2 -count=1 -timeout=18m ./internal/isolation/ -run '*)
    case ${!#} in
      '^(TestA)$') shard=0 ;;
      '^(FuzzB)$') shard=1 ;;
      '^(ExampleC)$') shard=2 ;;
      *) exit 94 ;;
    esac
    printf 'isolation-%s\n' "$shard" >>"$TRACE"
    [[ $CASE != "isolation-$shard" ]] || exit 95
    ;;
  *) printf 'unexpected go command: %s\n' "$*" >&2; exit 96 ;;
esac
SH
cat >"$work/repo/scripts/ci/test-core-packages.sh" <<'SH'
#!/usr/bin/env bash
set -euo pipefail
[[ $GOMAXPROCS == 2 && $GOFLAGS == *-p=2* ]] || exit 97
printf 'core\n' >>"$TRACE"
[[ $CASE != core ]]
SH
cat >"$work/repo/scripts/release/test-fixtures.sh" <<'SH'
#!/usr/bin/env bash
set -euo pipefail
printf 'fixtures\n' >>"$TRACE"
[[ $CASE != fixtures ]]
SH
chmod +x "$work/bin/go" "$work/repo/scripts/ci/test-core-packages.sh" "$work/repo/scripts/release/"*.sh
export PATH="$work/bin:$PATH" TRACE="$work/trace"
printf '%s\n' core isolation-0 isolation-1 isolation-2 fixtures >"$work/expected"
for case_name in success core isolation-0 isolation-1 isolation-2 fixtures; do
  export CASE=$case_name RELEASE_SOURCE_TEST_OUTPUT="$work/$case_name"
  : >"$TRACE"
  result=0
  "$work/repo/scripts/release/test-source.sh" >"$work/log" 2>&1 || result=$?
  if [[ $case_name == success ]]; then [[ $result == 0 ]]; else [[ $result != 0 ]]; fi
  cmp "$work/expected" "$TRACE"
  [[ $(wc -l <"$RELEASE_SOURCE_TEST_OUTPUT/results.tsv") -eq 5 ]]
  if [[ $case_name != success ]]; then grep -F $'\tFAIL' "$RELEASE_SOURCE_TEST_OUTPUT/results.tsv" >/dev/null; fi
done
for case_name in list-failure empty-inventory planner-failure empty-shard duplicate missing injection; do
  export CASE=$case_name RELEASE_SOURCE_TEST_OUTPUT="$work/$case_name"
  : >"$TRACE"
  if "$work/repo/scripts/release/test-source.sh" >"$work/log" 2>&1; then
    echo "release test fixture: accepted invalid inventory $case_name" >&2
    exit 1
  fi
  [[ ! -s $TRACE ]]
done
export CASE=success RELEASE_SOURCE_TEST_OUTPUT="$work/success"
: >"$TRACE"
if "$work/repo/scripts/release/test-source.sh" >"$work/log" 2>&1; then
  echo 'release test fixture: overwrote an existing evidence directory' >&2
  exit 1
fi
[[ ! -s $TRACE ]]
printf 'release test fixture: complete test/fuzz/example coverage, bounds, failure propagation and inventory refusals passed\n'
