#!/bin/sh
# Requires an annotated tag on the exact commit and green main source proof.
#
# Two authorities may have created the tag:
#   1. A human: GitHub must report the tag object as verified.
#   2. The unattended stable-release app, which creates an annotated tag object
#      GitHub does not sign. That is accepted only when the release-build run
#      HIKYO_RELEASE_RUN_ID was started and triggered by the app's bot user
#      STABLE_RELEASE_APP_BOT_ID, the version is exactly the patch successor of
#      the authenticated latest stable release (the Go verifier decides), the
#      latest published release is that stable release, and no higher stable
#      tag exists. Empty or malformed inputs fail closed to the human rule.
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
reference=$("$GH_BIN" api "repos/$repository/git/ref/tags/$tag")
object=$(printf '%s\n' "$reference" | jq -er '.object | select(.type == "tag") | .sha')
is_full_sha "$object" || exit 1
signed_tag=$("$GH_BIN" api "repos/$repository/git/tags/$object")
printf '%s\n' "$signed_tag" | jq -e --arg tag "$tag" --arg commit "$commit" '
  .tag == $tag and .object.type == "commit" and .object.sha == $commit
' >/dev/null || refuse 'annotated tag on exact commit required'
authority=human
if ! printf '%s\n' "$signed_tag" | jq -e '.verification.verified == true and .verification.reason == "valid"' >/dev/null; then
  authority=app
  bot=${STABLE_RELEASE_APP_BOT_ID:-} run_id=${HIKYO_RELEASE_RUN_ID:-}
  printf '%s\n%s\n' "$bot" "$run_id" | grep -Evq '^[1-9][0-9]*$' &&
    refuse 'GitHub-verified annotated tag on exact commit required'
  "$GH_BIN" api "repos/$repository/actions/runs/$run_id" | jq -e \
    --arg repo "$repository" --arg tag "$tag" --arg commit "$commit" --arg bot "$bot" '
    (.path == ".github/workflows/release.yml" or (.path | startswith(".github/workflows/release.yml@"))) and
    .event == "push" and .head_branch == $tag and .head_sha == $commit and
    .repository.full_name == $repo and
    (.actor.id | tostring) == $bot and (.triggering_actor.id | tostring) == $bot
  ' >/dev/null || refuse 'unsigned tag needs a release-build run started and triggered by the stable-release app'
  scratch=$(mktemp -d "${TMPDIR:-/tmp}/hikyo-app-tag.XXXXXX")
  trap 'rm -rf "$scratch"' EXIT HUP INT TERM
  "$GO_BIN" -C "$repo_root" run ./scripts/release/stable preflight --trust "$trust" >/dev/null ||
    refuse 'stable trust does not authenticate'
  "$GO_BIN" -C "$repo_root" run ./scripts/release/stable candidate --patch-only \
    --trust "$trust" --version "${tag#v}" --commit "$commit" --out "$scratch/candidate.json" ||
    refuse 'the app may only publish the patch successor of the signed latest stable release'
  latest=$(jq -er '.highest_release | select(type == "string")' "$trust/metadata.json")
  [ "$("$GH_BIN" api "repos/$repository/releases/latest" --jq .tag_name)" = "v$latest" ] ||
    refuse "the latest published release is not v$latest"
  "$GH_BIN" api --paginate "repos/$repository/git/matching-refs/tags/v" --jq '.[].ref' >"$scratch/tags" ||
    refuse 'cannot list release tags'
  # Nightly and non-semver tags never count. A pre-release of a higher triple does.
  jq -Rne --arg tag "$tag" '
    def triple: ltrimstr("v") | capture("^(?<a>[0-9]+)\\.(?<b>[0-9]+)\\.(?<c>[0-9]+)") | [.a,.b,.c | tonumber];
    ($tag | triple) as $new |
    [inputs | ltrimstr("refs/tags/") | select(test("^v[0-9]+\\.[0-9]+\\.[0-9]+") and (contains("-nightly.") | not)) | triple | select(. > $new)] | length == 0
  ' <"$scratch/tags" >/dev/null || refuse 'a higher stable tag exists'
fi
"$script_dir/require-green-main.sh" "$repository" "$commit"
if [ -n "${HIKYO_AUTHORITY_OUT:-}" ]; then printf '%s\n' "$authority" >"$HIKYO_AUTHORITY_OUT"; fi
printf 'release: %s-authorized annotated tag and exact main CI verified\n' "$authority"
