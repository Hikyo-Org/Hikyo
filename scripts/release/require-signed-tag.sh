#!/bin/sh
# Requires an annotated tag on the exact commit and green main source proof.
#
# The authority is decided by WHO PUSHED THE TAG, read from the release-build
# run, never by how the tag object is signed. A GitHub-verified tag signature
# proves nothing about the pusher: a stolen app key can push a tag signed with
# any verified key.
#   1. A bot pushed (actor or triggering actor has type Bot, or either id is
#      STABLE_RELEASE_APP_BOT_ID): the unattended rules apply, whatever the
#      signature says. The run must be started and triggered by the configured
#      bot id; the version must be exactly the patch successor of the
#      authenticated latest stable release (the Go verifier decides); the latest
#      published release must be that stable release; no higher stable tag may
#      exist. A bot with an empty or different configured id is refused.
#   2. A person pushed (actor and triggering actor are both Users): the tag
#      object must be GitHub-verified with reason "valid". Only repository admins
#      can create v* tags at all (creation ruleset), so no admin read happens here.
# HIKYO_RELEASE_RUN_ID names the run; without it the latest push run of the tag
# is used (the local publication ceremony).
set -eu
: "${GH_BIN:=gh}" "${GO_BIN:=go}"
[ "$#" -eq 3 ] || { printf 'usage: %s REPOSITORY TAG COMMIT\n' "$0" >&2; exit 2; }
repository=$1 tag=$2 commit=$3
script_dir=$(CDPATH='' cd -- "$(dirname "$0")" && pwd)
repo_root=$(CDPATH='' cd -- "$script_dir/../.." && pwd)
# shellcheck disable=SC1091
. "$script_dir/../lib/release.sh"
is_full_sha "$commit" && is_semver "${tag#v}" && [ "$tag" != "${tag#v}" ] || exit 2
printf '%s\n' "$repository" | grep -Eq '^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$' || exit 2
trust=${HIKYO_TRUST_DIR:-$repo_root/release/trust}
refuse() { printf 'release: %s\n' "$*" >&2; exit 1; }
reference=$("$GH_BIN" api "repos/$repository/git/ref/tags/$tag") || refuse "cannot read the $tag ref"
object=$(printf '%s\n' "$reference" | jq -er '.object | select(.type == "tag") | .sha') || refuse 'annotated tag on exact commit required'
is_full_sha "$object" || refuse 'annotated tag on exact commit required'
signed_tag=$("$GH_BIN" api "repos/$repository/git/tags/$object") || refuse "cannot read the $tag object"
printf '%s\n' "$signed_tag" | jq -e --arg tag "$tag" --arg commit "$commit" '
  .tag == $tag and .object.type == "commit" and .object.sha == $commit
' >/dev/null || refuse 'annotated tag on exact commit required'
api_to() { # file args...: a failed read is a refusal, never an empty answer
  api_out=$1
  shift
  "$GH_BIN" api "$@" >"$api_out" || refuse "cannot read: gh api $*"
}
scratch=$(mktemp -d "${TMPDIR:-/tmp}/hikyo-tag-authority.XXXXXX")
trap 'rm -rf "$scratch"' EXIT HUP INT TERM
run_id=${HIKYO_RELEASE_RUN_ID:-}
if [ -z "$run_id" ]; then
  api_to "$scratch/runs.json" "repos/$repository/actions/workflows/release.yml/runs?event=push&head_sha=$commit&per_page=100"
  run_id=$(jq -er --arg tag "$tag" --arg commit "$commit" '
    [.workflow_runs[] | select(.head_sha == $commit and .head_branch == $tag and .event == "push")] |
    sort_by(.run_number, .run_attempt) | last | .id | select(type == "number")
  ' "$scratch/runs.json") || refuse 'no release-build run of this tag'
fi
printf '%s\n' "$run_id" | grep -Eq '^[1-9][0-9]*$' || refuse 'invalid release-build run id'
api_to "$scratch/run.json" "repos/$repository/actions/runs/$run_id"
bot=${STABLE_RELEASE_APP_BOT_ID:-}
printf '%s\n' "$bot" | grep -Eq '^([1-9][0-9]*)?$' || refuse 'invalid stable-release bot id'
# release_run_jq comes from the shared release library.
# shellcheck disable=SC2154
jq -e --arg repo "$repository" --arg tag "$tag" --arg commit "$commit" "$release_run_jq"'
  release_run and
  .event == "push" and .head_branch == $tag and .head_sha == $commit and
  .repository.full_name == $repo and (.actor.id | type == "number") and (.triggering_actor.id | type == "number")
' "$scratch/run.json" >/dev/null || refuse 'the run is not the release-build push of this tag and commit'
# Any bot involvement means the unattended rules; a bot id match counts even if
# the type field were ever missing.
if jq -e --arg bot "$bot" '
  .actor.type == "Bot" or .triggering_actor.type == "Bot" or
  ($bot != "" and ((.actor.id | tostring) == $bot or (.triggering_actor.id | tostring) == $bot))
' "$scratch/run.json" >/dev/null; then
  authority=app
  [ -n "$bot" ] || refuse 'a bot pushed this tag but no stable-release bot id is configured'
  jq -e --arg bot "$bot" '
    .actor.type == "Bot" and .triggering_actor.type == "Bot" and
    (.actor.id | tostring) == $bot and (.triggering_actor.id | tostring) == $bot
  ' "$scratch/run.json" >/dev/null || refuse 'a bot tag needs a release-build run started and triggered by the stable-release app'
  # The tagged commit's own copy of this script must judge by the pusher too: a tag at an
  # older commit would run that commit's release workflow under the old signature rule.
  rc=0
  commit_has_actor_tag_authority "$repository" "$commit" || rc=$?
  case $rc in
  0) ;;
  1) refuse 'the tagged commit predates the actor-based tag authority' ;;
  *) refuse 'cannot read the tag authority of the tagged commit' ;;
  esac
  "$GO_BIN" -C "$repo_root" run ./scripts/release/stable preflight --trust "$trust" >/dev/null ||
    refuse 'stable trust does not authenticate'
  "$GO_BIN" -C "$repo_root" run ./scripts/release/stable candidate --patch-only \
    --trust "$trust" --version "${tag#v}" --commit "$commit" --out "$scratch/candidate.json" ||
    refuse 'the app may only publish the patch successor of the signed latest stable release'
  latest=$(jq -er '.highest_release | select(type == "string")' "$trust/metadata.json")
  api_to "$scratch/latest.json" "repos/$repository/releases/latest"
  [ "$(jq -er '.tag_name' "$scratch/latest.json")" = "v$latest" ] ||
    refuse "the latest published release is not v$latest"
  api_to "$scratch/tags.json" --paginate "repos/$repository/git/matching-refs/tags/v" --jq '.[].ref'
  # Nightly and non-semver tags never count. A pre-release of a higher triple does.
  jq -Rne --arg tag "$tag" '
    def triple: ltrimstr("v") | capture("^(?<a>[0-9]+)\\.(?<b>[0-9]+)\\.(?<c>[0-9]+)") | [.a,.b,.c | tonumber];
    ($tag | triple) as $new |
    [inputs | ltrimstr("refs/tags/") | select(test("^v[0-9]+\\.[0-9]+\\.[0-9]+") and (contains("-nightly.") | not)) | triple | select(. > $new)] | length == 0
  ' <"$scratch/tags.json" >/dev/null || refuse 'a higher stable tag exists'
else
  authority=human
  jq -e '.actor.type == "User" and .triggering_actor.type == "User"' "$scratch/run.json" >/dev/null ||
    refuse 'the tag pusher is neither a person nor the stable-release app'
  printf '%s\n' "$signed_tag" | jq -e '.verification.verified == true and .verification.reason == "valid"' >/dev/null ||
    refuse 'GitHub-verified annotated tag on exact commit required'
  # No collaborator-permission read: the creation ruleset (configure-repository.sh
  # asserts it) lets only repository admins create v* tags, so a person's push of
  # this tag already proves the admin role. The type and the verified tag are the
  # remaining checks here.
fi
"$script_dir/require-green-main.sh" "$repository" "$commit"
if [ -n "${HIKYO_AUTHORITY_OUT:-}" ]; then printf '%s\n' "$authority" >"$HIKYO_AUTHORITY_OUT"; fi
printf 'release: %s-authorized annotated tag and exact main CI verified\n' "$authority"
