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
case "$*" in
  'api repos/owner/repo/git/ref/tags/v1.1.1')
    type=tag
    [ "$mode" != lightweight ] || type=commit
    jq -nc --arg type "$type" '{object:{type:$type,sha:("2"*40)}}' ;;
  'api repos/owner/repo/git/tags/'*)
    verified=false
    case "$mode" in human) verified=true ;; esac
    jq -nc --argjson verified "$verified" --arg commit "$FIXTURE_COMMIT" '{tag:"v1.1.1",object:{type:"commit",sha:$commit},verification:{verified:$verified,reason:(if $verified then "valid" else "unsigned" end)}}' ;;
  'api repos/owner/repo/actions/runs/42')
    actor=777 triggering=777 event=push branch=v1.1.1 path=.github/workflows/release.yml
    case "$mode" in
      human-rerun) triggering=1 ;;
      human-actor) actor=1 triggering=1 ;;
      wrong-event) event=workflow_dispatch ;;
      wrong-branch) branch=v1.1.2 ;;
      wrong-workflow) path=.github/workflows/other.yml ;;
    esac
    jq -nc --argjson a "$actor" --argjson t "$triggering" --arg event "$event" --arg branch "$branch" --arg path "$path" --arg commit "$FIXTURE_COMMIT" \
      '{path:$path,event:$event,head_branch:$branch,head_sha:$commit,repository:{full_name:"owner/repo"},actor:{id:$a},triggering_actor:{id:$t}}' ;;
  'api repos/owner/repo/releases/latest --jq .tag_name')
    if [ "$mode" = stale-latest ]; then echo v1.0.0; else echo v1.1.0; fi ;;
  'api --paginate repos/owner/repo/git/matching-refs/tags/v --jq .[].ref')
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
expect_refused() {
  FIXTURE_MODE=$1
  export FIXTURE_MODE
  if run >"$scratch/out" 2>&1; then printf 'require-signed-tag fixture: accepted %s\n' "$1" >&2; exit 1; fi
}
export STABLE_RELEASE_APP_BOT_ID=777 HIKYO_RELEASE_RUN_ID=42 HIKYO_AUTHORITY_OUT="$scratch/authority"
FIXTURE_MODE=app run >/dev/null
[ "$(cat "$scratch/authority")" = app ] || { printf 'app authority not reported\n' >&2; exit 1; }
FIXTURE_MODE=human run >/dev/null
[ "$(cat "$scratch/authority")" = human ] || { printf 'human authority not reported\n' >&2; exit 1; }
# A human tag needs neither the app variables nor a run.
(unset STABLE_RELEASE_APP_BOT_ID HIKYO_RELEASE_RUN_ID; FIXTURE_MODE=human run >/dev/null)
for mode in lightweight human-rerun human-actor wrong-event wrong-branch wrong-workflow not-patch stale-latest higher-tag red-ci; do
  expect_refused "$mode"
done
# Missing or malformed app configuration falls back to the human-only rule.
for blank in STABLE_RELEASE_APP_BOT_ID HIKYO_RELEASE_RUN_ID; do
  (export "$blank="; expect_refused app)
done
(STABLE_RELEASE_APP_BOT_ID=0x1; export STABLE_RELEASE_APP_BOT_ID; expect_refused app)
# Another bot id must not satisfy the check.
(STABLE_RELEASE_APP_BOT_ID=778; export STABLE_RELEASE_APP_BOT_ID; expect_refused app)
printf 'require-signed-tag fixture: human and app authorities passed; every refusal case held\n'
