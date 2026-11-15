#!/bin/sh
# Fixtures for the two tag authorities: a GitHub-verified human tag and the
# unsigned annotated tag of the unattended stable-release app.
set -eu
script_dir=$(CDPATH='' cd -- "$(dirname "$0")" && pwd)
scratch=$(mktemp -d "${TMPDIR:-/tmp}/hikyo-require-signed-tag.XXXXXX")
trap 'rm -rf "$scratch"' EXIT HUP INT TERM
mkdir "$scratch/bin" "$scratch/trust"
commit=1111111111111111111111111111111111111111
printf '{"highest_release":"1.1.0"}\n' >"$scratch/trust/metadata.json"
export FIXTURE_COMMIT=$commit FIXTURE_MODE=app FIXTURE_SCRATCH="$scratch" HIKYO_TRUST_DIR="$scratch/trust"
cat >"$scratch/bin/gh" <<'SH'
#!/bin/sh
set -eu
mode=$FIXTURE_MODE
printf '%s\n' "$*" >>"$FIXTURE_SCRATCH/calls"
case "$*" in
  'api repos/owner/repo/git/ref/tags/v1.1.1')
    type=tag
    [ "$mode" != lightweight ] || type=commit
    jq -nc --arg type "$type" '{object:{type:$type,sha:("2"*40)}}' ;;
  'api repos/owner/repo/git/tags/'*)
    # The signature says nothing about the pusher: signed-bot and signed-human differ only in the run.
    verified=false
    [ "${FIXTURE_SIGNED:-}" != 1 ] || verified=true
    case "$mode" in user-unsigned) ;; human*|user-*) verified=true ;; esac
    reason=unsigned
    [ "$verified" != true ] || reason=valid
    [ "$mode" != user-bad-reason ] || reason=expired_key
    jq -nc --argjson verified "$verified" --arg reason "$reason" --arg commit "$FIXTURE_COMMIT" '{tag:"v1.1.1",object:{type:"commit",sha:$commit},verification:{verified:$verified,reason:$reason}}' ;;
  'api repos/owner/repo/actions/workflows/release.yml/runs?event=push&head_sha='*'&per_page=100')
    jq -nc --arg commit "$FIXTURE_COMMIT" '{workflow_runs:[
      {id:41,head_sha:$commit,head_branch:"v1.1.1",event:"push",run_number:1,run_attempt:1},
      {id:42,head_sha:$commit,head_branch:"v1.1.1",event:"push",run_number:2,run_attempt:1},
      {id:43,head_sha:$commit,head_branch:"v1.1.0",event:"push",run_number:9,run_attempt:1}]}' ;;
  'api repos/owner/repo/actions/runs/42')
    actor=777 triggering=777 atype=Bot ttype=Bot event=push branch=v1.1.1 path=.github/workflows/release.yml
    alogin=pusher tlogin=pusher
    case "$mode" in
      human*|user-*) actor=1 triggering=1 atype=User ttype=User ;;
      app-rerun-by-human) triggering=1 ttype=User ;;
      wrong-bot) actor=778 triggering=778 ;;
      bot-no-type) atype=User ttype=User ;;
      wrong-event) event=workflow_dispatch ;;
      wrong-branch) branch=v1.1.2 ;;
      wrong-workflow) path=.github/workflows/other.yml ;;
      prefix-workflow) path=.github/workflows/release.yml.evil ;;
    esac
    case "$mode" in
      # A person re-ran another person's push: two different logins, both Users.
      human-rerun) triggering=2 tlogin=other ;;
      user-actor-bot-type) atype=Bot ;;
      user-triggering-bot-type) ttype=Bot ;;
      user-actor-org-type) atype=Organization ;;
      user-triggering-org-type) ttype=Organization ;;
    esac
    jq -nc --argjson a "$actor" --argjson t "$triggering" --arg at "$atype" --arg tt "$ttype" --arg al "$alogin" --arg tl "$tlogin" --arg event "$event" --arg branch "$branch" --arg path "$path" --arg commit "$FIXTURE_COMMIT" \
      '{path:$path,event:$event,head_branch:$branch,head_sha:$commit,repository:{full_name:"owner/repo"},actor:{id:$a,type:$at,login:$al},triggering_actor:{id:$t,type:$tt,login:$tl}}' ;;
  'api repos/owner/repo/contents/scripts/release/require-signed-tag.sh?ref='*' --jq .content')
    # The tagged commit's own copy of this script: the actor-based rule names both markers.
    [ "$mode" != authority-read-fail ] || exit 1
    if [ "$mode" = no-authority ]; then printf 'set -eu\n# the older rule: the tag signature only\n' | base64
    elif [ "$mode" = authority-actor-only ]; then printf 'set -eu\n# triggering_actor decides\n' | base64
    elif [ "$mode" = authority-run-only ]; then printf 'set -eu\n# release_run_jq decides\n' | base64
    else printf 'set -eu\n# triggering_actor and release_run_jq decide\n' | base64; fi ;;
  'api repos/owner/repo/releases/latest')
    if [ "$mode" = stale-latest ]; then echo '{"tag_name":"v1.0.0"}'; elif [ "$mode" = latest-api-failure ]; then exit 1; else echo '{"tag_name":"v1.1.0"}'; fi ;;
  'api --paginate repos/owner/repo/git/matching-refs/tags/v --jq .[].ref')
    [ "$mode" != tags-api-failure ] || exit 1
    printf 'refs/tags/v-ruleset-probe\nrefs/tags/v1.0.0\nrefs/tags/v1.1.0\nrefs/tags/v1.1.1\nrefs/tags/v1.2.0-nightly.20261112.1.gabcdef12\n'
    [ "$mode" != higher-tag ] || printf 'refs/tags/v1.1.2\n' ;;
  *) printf 'unexpected gh invocation: %s\n' "$*" >&2; exit 2 ;;
esac
SH
cat >"$scratch/bin/go" <<'SH'
#!/bin/sh
set -eu
case "$*" in
  *'./scripts/release/stable preflight '*) exit 0 ;;
  *'./scripts/release/stable candidate --patch-only '*) [ "$FIXTURE_MODE" != not-patch ] || exit 1; exit 0 ;;
  *'./scripts/ci/source-proof '*) [ "$FIXTURE_MODE" != red-ci ] || exit 1; exit 0 ;;
  *) printf 'unexpected go invocation: %s\n' "$*" >&2; exit 2 ;;
esac
SH
chmod +x "$scratch/bin/gh" "$scratch/bin/go"
export GH_BIN="$scratch/bin/gh" GO_BIN="$scratch/bin/go"
run() { "$script_dir/require-signed-tag.sh" owner/repo v1.1.1 "$commit"; }
# The refusal must come from the intended gate, not from a fixture gap.
reason() {
  case $1 in
    lightweight) echo 'annotated tag on exact commit required' ;;
    app-rerun-by-human|wrong-bot|bot-no-type) echo 'started and triggered by the stable-release app' ;;
    wrong-event|wrong-branch|wrong-workflow|prefix-workflow) echo 'not the release-build push of this tag' ;;
    not-patch) echo 'patch successor' ;;
    stale-latest) echo 'latest published release is not' ;;
    higher-tag) echo 'higher stable tag exists' ;;
    red-ci) echo 'green' ;;
    latest-api-failure|tags-api-failure) echo 'cannot read: gh api' ;;
    no-authority | authority-actor-only | authority-run-only) echo 'predates the actor-based tag authority' ;;
    authority-read-fail) echo 'cannot read the tag authority of the tagged commit' ;;
    user-actor-bot-type|user-triggering-bot-type) echo 'started and triggered by the stable-release app' ;;
    user-actor-org-type|user-triggering-org-type) echo 'neither a person nor the stable-release app' ;;
    user-unsigned|user-bad-reason) echo 'GitHub-verified annotated tag on exact commit required' ;;
    app) echo 'no stable-release bot id is configured' ;;
    *) echo "$1" ;;
  esac
}
expect_refused() {
  FIXTURE_MODE=$1
  export FIXTURE_MODE
  if run >"$scratch/out" 2>&1; then printf 'require-signed-tag fixture: accepted %s\n' "$1" >&2; exit 1; fi
  want=${2:-$(reason "$1")}
  grep -F "$want" "$scratch/out" >/dev/null || { cat "$scratch/out" >&2; printf 'require-signed-tag fixture: %s was refused for the wrong reason, want: %s\n' "$1" "$want" >&2; exit 1; }
}
expect_accepted() {
  FIXTURE_MODE=$1
  export FIXTURE_MODE
  run >"$scratch/out" 2>&1 || { cat "$scratch/out" >&2; printf 'require-signed-tag fixture: refused %s\n' "$1" >&2; exit 1; }
}
export STABLE_RELEASE_APP_BOT_ID=777 HIKYO_RELEASE_RUN_ID=42 HIKYO_AUTHORITY_OUT="$scratch/authority"
expect_accepted app
[ "$(cat "$scratch/authority")" = app ] || { printf 'app authority not reported\n' >&2; exit 1; }
# The bot's tag is judged by the unattended rules even when GitHub verifies its signature.
(FIXTURE_SIGNED=1; export FIXTURE_SIGNED; expect_accepted app)
[ "$(cat "$scratch/authority")" = app ] || { printf 'a verified bot tag skipped the app rules\n' >&2; exit 1; }
expect_accepted human
[ "$(cat "$scratch/authority")" = human ] || { printf 'human authority not reported\n' >&2; exit 1; }
# A person's re-run of a person's push (two different User logins) is a person's tag.
expect_accepted human-rerun
[ "$(cat "$scratch/authority")" = human ] || { printf 'a human rerun lost the human authority\n' >&2; exit 1; }
# The admin role is never read from the collaborators API.
if grep -q 'collaborators' "$scratch/calls"; then printf 'the collaborators API was read\n' >&2; exit 1; fi
# Without HIKYO_RELEASE_RUN_ID (the local ceremony) the latest push run of the tag decides.
(unset HIKYO_RELEASE_RUN_ID; expect_accepted human; grep -q 'workflows/release.yml/runs?event=push&head_sha=' "$scratch/calls" || { printf 'the run was not looked up\n' >&2; exit 1; })
(unset HIKYO_RELEASE_RUN_ID; expect_accepted app; [ "$(cat "$scratch/authority")" = app ])
# A person's tag is judged by the signature, so an old commit's script is no obstacle to the ceremony.
(: >"$scratch/calls"; expect_accepted human; grep -q 'contents/scripts/release' "$scratch/calls" && { printf 'a human tag read the tagged commit script\n' >&2; exit 1; } || true)
# A human tag needs no app variable at all.
(unset STABLE_RELEASE_APP_BOT_ID; expect_accepted human)
for mode in lightweight app-rerun-by-human wrong-event wrong-branch wrong-workflow prefix-workflow not-patch stale-latest higher-tag red-ci \
  wrong-bot bot-no-type latest-api-failure tags-api-failure no-authority authority-actor-only authority-run-only authority-read-fail \
  user-actor-bot-type user-triggering-bot-type user-actor-org-type user-triggering-org-type user-bad-reason; do
  expect_refused "$mode"
done
# A GitHub-verified tag pushed by the bot must still meet every app gate: the
# signature must never choose the authority.
for mode in not-patch higher-tag stale-latest red-ci app-rerun-by-human wrong-bot latest-api-failure tags-api-failure no-authority authority-actor-only authority-run-only authority-read-fail; do
  (FIXTURE_SIGNED=1; export FIXTURE_SIGNED; expect_refused "$mode")
done
# A person's unsigned tag is refused, and a bot with no configured id is never a person.
expect_refused user-unsigned
(unset STABLE_RELEASE_APP_BOT_ID; expect_refused app)
(unset STABLE_RELEASE_APP_BOT_ID; FIXTURE_SIGNED=1; export FIXTURE_SIGNED; expect_refused app)
# Missing or malformed app configuration cannot widen the human rule.
(export STABLE_RELEASE_APP_BOT_ID=; expect_refused app)
(STABLE_RELEASE_APP_BOT_ID=0x1; export STABLE_RELEASE_APP_BOT_ID; expect_refused app 'invalid stable-release bot id')
(STABLE_RELEASE_APP_BOT_ID=778; export STABLE_RELEASE_APP_BOT_ID; expect_refused app 'started and triggered by the stable-release app')
printf 'require-signed-tag fixture: human and app authorities passed; every refusal case held\n'
