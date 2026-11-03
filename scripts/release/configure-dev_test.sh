#!/usr/bin/env bash
# Exercise activation with a read/write API double. Main proof is a separately
# tested trusted boundary; this fixture proves activation ordering and scope.
set -euo pipefail
script_dir=$(CDPATH='' cd -- "$(dirname "$0")" && pwd)
root=$(CDPATH='' cd -- "$script_dir/../.." && pwd)
work=$(mktemp -d "${TMPDIR:-/tmp}/hikyo-dev-activation-test.XXXXXX")
trap 'rm -rf "$work"' EXIT

for scenario in new existing resume-bootstrap resume-created later-page \
  default-dev unverified dirty untracked missing-workflow proof-failed \
  unprotected-history duplicate-owner main-moved initial-readback-drift \
  final-readback-drift created-ref-drift put-failed inherited-queue missing-finalized-ref; do
  fixture="$work/$scenario"
  mkdir -p "$fixture/scripts/release" "$fixture/release/repository" "$fixture/bin" "$fixture/state"
  cp "$script_dir/configure-dev.sh" "$script_dir/check-dev-policy.sh" "$fixture/scripts/release/"
  cp "$root/release/repository/dev-ci-gate.json" "$fixture/release/repository/"
  cat >"$fixture/scripts/release/require-green-main.sh" <<'PROOF'
#!/bin/sh
set -eu
[ "$#" -eq 2 ] && [ "$1" = owner/repo ] && [ "$2" = "$DEV_FIXTURE_MAIN" ]
printf 'proof\n' >>"$DEV_FIXTURE/events"
[ "$DEV_FIXTURE_SCENARIO" != proof-failed ]
touch "$DEV_FIXTURE/state/verified"
PROOF
  cat >"$fixture/bin/git" <<'GIT'
#!/bin/sh
set -eu
case "$1" in
  -c)
    [ "$2" = credential.helper= ] && [ "$3" = fetch ]
    [ "$7" = https://github.com/owner/repo.git ] && [ "$8" = "$DEV_FIXTURE_MAIN" ]
    ;;
  ls-files)
    if [ "$DEV_FIXTURE_SCENARIO" = untracked ]; then printf 'scripts/ci/pr-gate/dev.go\n'; fi
    ;;
  diff) [ "$DEV_FIXTURE_SCENARIO" != dirty ] ;;
  cat-file)
    if [ "$DEV_FIXTURE_SCENARIO" = missing-workflow ] && [ "$3" = "$DEV_FIXTURE_MAIN:.github/workflows/ci-dev.yml" ]; then exit 1; fi
    ;;
  *) printf 'unexpected git operation: %s\n' "$*" >&2; exit 1 ;;
esac
GIT
  cat >"$fixture/bin/gh" <<'GH'
#!/bin/sh
set -eu
[ "$1" = api ]
shift
method=GET input=
if [ "$1" = --method ]; then method=$2; shift 2; fi
route=$1
shift
if [ "$#" -gt 0 ]; then [ "$1" = --input ]; input=$2; shift 2; fi
[ "$#" -eq 0 ]
printf '%s %s\n' "$method" "$route" >>"$DEV_FIXTURE/events"
case "$method:$route" in
  GET:repos/owner/repo)
    branch=main
    [ "$DEV_FIXTURE_SCENARIO" != default-dev ] || branch=dev
    jq -n --arg branch "$branch" '{id:10,full_name:"owner/repo",default_branch:$branch}'
    ;;
  GET:repos/owner/repo/git/ref/heads/main)
    sha=$DEV_FIXTURE_MAIN
    if [ "$DEV_FIXTURE_SCENARIO" = main-moved ] && [ "$(grep -c 'GET repos/owner/repo/git/ref/heads/main' "$DEV_FIXTURE/events")" -gt 1 ]; then sha=$DEV_FIXTURE_OTHER; fi
    jq -n --arg sha "$sha" '{ref:"refs/heads/main",object:{type:"commit",sha:$sha}}'
    ;;
  GET:repos/owner/repo/commits/*)
    verified=true
    [ "$DEV_FIXTURE_SCENARIO" != unverified ] || verified=false
    jq -n --arg sha "$DEV_FIXTURE_MAIN" --argjson verified "$verified" '{sha:$sha,commit:{verification:{verified:$verified}}}'
    ;;
  GET:repos/owner/repo/rulesets\?per_page=100\&page=1)
    if [ "$DEV_FIXTURE_SCENARIO" = later-page ]; then
      jq -n '[range(1001;1101) | {id:.,name:("unrelated-"+tostring)}]'
    elif [ "$DEV_FIXTURE_SCENARIO" = duplicate-owner ]; then
      printf '[{"id":31,"name":"dev requires trusted fast feedback"},{"id":32,"name":"dev requires trusted fast feedback"}]\n'
    elif [ -f "$DEV_FIXTURE/state/policy.json" ]; then
      printf '[{"id":31,"name":"dev requires trusted fast feedback"}]\n'
    else printf '[]\n'; fi
    ;;
  GET:repos/owner/repo/rulesets\?per_page=100\&page=2)
    [ "$DEV_FIXTURE_SCENARIO" = later-page ]
    printf '[{"id":31,"name":"dev requires trusted fast feedback"}]\n'
    ;;
  GET:repos/owner/repo/rulesets/31)
    if [ "$DEV_FIXTURE_SCENARIO" = initial-readback-drift ] ||
       { [ "$DEV_FIXTURE_SCENARIO" = final-readback-drift ] && [ -f "$DEV_FIXTURE/state/put" ]; }; then
      jq '.bypass_actors=[{actor_id:1,actor_type:"RepositoryRole",bypass_mode:"always"}]' "$DEV_FIXTURE/state/policy.json"
    else cat "$DEV_FIXTURE/state/policy.json"; fi
    ;;
  GET:repos/owner/repo/git/ref/heads/dev)
    if [ -f "$DEV_FIXTURE/state/ref.json" ]; then cat "$DEV_FIXTURE/state/ref.json"
    else printf 'gh: not found (HTTP 404)\n' >&2; exit 1; fi
    ;;
  GET:repos/owner/repo/rules/branches/dev)
    if [ "$DEV_FIXTURE_SCENARIO" = inherited-queue ]; then
      jq '.rules + [{type:"merge_queue",parameters:{}}]' "$DEV_FIXTURE/state/policy.json"
    else jq '.rules' "$DEV_FIXTURE/state/policy.json"; fi
    ;;
  POST:repos/owner/repo/rulesets)
    [ -f "$DEV_FIXTURE/state/verified" ]
    [ ! -f "$DEV_FIXTURE/state/policy.json" ]
    jq '.id=31' "$input" >"$DEV_FIXTURE/state/policy.json"
    cp "$input" "$DEV_FIXTURE/state/initial.json"
    cat "$DEV_FIXTURE/state/policy.json"
    ;;
  POST:repos/owner/repo/git/refs)
    [ -f "$DEV_FIXTURE/state/verified" ] && [ -f "$DEV_FIXTURE/state/policy.json" ]
    [ ! -f "$DEV_FIXTURE/state/ref.json" ]
    jq -e '.ref == "refs/heads/dev" and .sha == env.DEV_FIXTURE_MAIN' "$input" >/dev/null
    sha=$DEV_FIXTURE_MAIN
    [ "$DEV_FIXTURE_SCENARIO" != created-ref-drift ] || sha=$DEV_FIXTURE_OTHER
    jq -n --arg sha "$sha" '{ref:"refs/heads/dev",object:{type:"commit",sha:$sha}}' >"$DEV_FIXTURE/state/ref.json"
    cat "$DEV_FIXTURE/state/ref.json"
    ;;
  PUT:repos/owner/repo/rulesets/31)
    [ -f "$DEV_FIXTURE/state/verified" ]
    [ "$DEV_FIXTURE_SCENARIO" != put-failed ] || exit 1
    jq '.id=31' "$input" >"$DEV_FIXTURE/state/policy.json"
    touch "$DEV_FIXTURE/state/put"
    cat "$DEV_FIXTURE/state/policy.json"
    ;;
  *) printf 'unexpected or unauthorized API route: %s %s\n' "$method" "$route" >&2; exit 1 ;;
esac
GH
  chmod +x "$fixture/scripts/release/"*.sh "$fixture/bin/"*
  export DEV_FIXTURE="$fixture" DEV_FIXTURE_SCENARIO="$scenario"
  export DEV_FIXTURE_MAIN=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
  export DEV_FIXTURE_OTHER=bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
  if [[ "$scenario" =~ ^(existing|resume-bootstrap|resume-created|later-page|missing-finalized-ref)$ ]]; then
    jq '.id=31 | .rules += [{type:"commit_message_pattern",parameters:{name:"Preserve",operator:"contains",pattern:"sensitive",negate:true}}] |
      (.rules[] | select(.type=="pull_request").parameters.required_approving_review_count)=2 |
      (.rules[] | select(.type=="required_status_checks").parameters.required_status_checks[0].integration_id)=15368' \
      "$fixture/release/repository/dev-ci-gate.json" >"$fixture/state/policy.json"
    if [[ "$scenario" = resume-* ]]; then
      jq '(.rules[] | select(.type=="required_status_checks").parameters.do_not_enforce_on_create)=true' \
        "$fixture/state/policy.json" >"$fixture/state/bootstrap.json"
      mv "$fixture/state/bootstrap.json" "$fixture/state/policy.json"
    fi
    if [[ "$scenario" != resume-bootstrap && "$scenario" != missing-finalized-ref ]]; then
      jq -n --arg sha "$DEV_FIXTURE_MAIN" '{ref:"refs/heads/dev",object:{type:"commit",sha:$sha}}' >"$fixture/state/ref.json"
    fi
  fi
  if [ "$scenario" = unprotected-history ]; then
    jq -n --arg sha "$DEV_FIXTURE_OTHER" '{ref:"refs/heads/dev",object:{type:"commit",sha:$sha}}' >"$fixture/state/ref.json"
  fi
  status=0
  PATH="$fixture/bin:$PATH" GH_BIN="$fixture/bin/gh" \
    "$fixture/scripts/release/configure-dev.sh" owner/repo >"$fixture/out" 2>"$fixture/error" || status=$?
  case "$scenario" in
    new | existing | resume-bootstrap | resume-created | later-page)
      if [ "$status" -ne 0 ]; then cat "$fixture/error" >&2; printf 'activation fixture failed: %s\n' "$scenario" >&2; exit 1; fi
      "$script_dir/check-dev-policy.sh" "$fixture/state/policy.json"
      jq -e '.object.sha == env.DEV_FIXTURE_MAIN' "$fixture/state/ref.json" >/dev/null
      if [ "$scenario" = new ]; then
        "$script_dir/check-dev-policy.sh" --bootstrap "$fixture/state/initial.json"
        [ "$(grep -c '^POST\|^PUT' "$fixture/events")" -eq 3 ]
      else
        jq -e 'any(.rules[]; .type=="commit_message_pattern") and
          (.rules[]|select(.type=="pull_request").parameters.required_approving_review_count)==2 and
          (.rules[]|select(.type=="required_status_checks").parameters.required_status_checks[0].integration_id)==15368' \
          "$fixture/state/policy.json" >/dev/null
      fi
      ;;
    *) [ "$status" -ne 0 ] || { printf 'activation fixture accepted unsafe state: %s\n' "$scenario" >&2; exit 1; } ;;
  esac
  case "$scenario" in
    default-dev | unverified | dirty | untracked | missing-workflow | proof-failed | unprotected-history | duplicate-owner | main-moved | missing-finalized-ref)
      if grep -E '^POST |^PUT ' "$fixture/events" >/dev/null; then printf 'activation mutated before verified prerequisites: %s\n' "$scenario" >&2; exit 1; fi
      ;;
  esac
done
printf 'dev activation fixtures: trusted installation, exact proof, protected bootstrap, preservation and failed readback refusals passed\n'
