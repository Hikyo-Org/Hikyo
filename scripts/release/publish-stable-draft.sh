#!/bin/sh
set -eu
[ "$#" -eq 4 ] || { printf 'usage: %s REPOSITORY TAG EXPECTED_MANIFEST_SHA256 TRUST\n' "$0" >&2; exit 2; }
repository=$1 tag=$2 expected=$3 trust=$4
: "${GH_BIN:=gh}"
trust_state=${HIKYO_RELEASE_TRUST_STATE:-"${XDG_STATE_HOME:-$HOME/Library/Application Support}/hikyo/release-trust.json"}
script_dir=$(CDPATH='' cd -- "$(dirname "$0")" && pwd)
# shellcheck disable=SC1091
. "$script_dir/../lib/release.sh"
is_semver "${tag#v}" && [ "$tag" != "${tag#v}" ] || exit 2
printf '%s\n' "$expected" | grep -Eq '^[0-9a-f]{64}$' || exit 2
printf '%s\n' "$repository" | grep -Eq '^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$' || exit 2
case "$tag" in *-nightly.*) printf 'publication: nightly tag refused\n' >&2; exit 2 ;; esac
scratch=$(mktemp -d "${TMPDIR:-/tmp}/hikyo-publish-stable.XXXXXX")
trap 'rm -rf "$scratch"' EXIT HUP INT TERM
mkdir "$scratch/draft" "$scratch/public"
read_release() (
  release_file=$1
  # The tag endpoint omits unpublished drafts. Resolve the exact tag across
  # every release page, then read its ID without weakening the identity checks.
  "$GH_BIN" api --paginate --slurp "repos/$repository/releases?per_page=100" >"$release_file.pages"
  release_id=$(jq -er --arg tag "$tag" '
    if type == "array" and all(.[]; type == "array") then
      if all(.[][];
        type == "object" and
        (.tag_name | type == "string" and length > 0) and
        (.id | type == "number" and . > 0 and . == floor)
      ) then
        [.[][] | select(.tag_name == $tag)] |
        if length == 1 then .[0].id
        else error("release tag must identify exactly one release") end
      else error("invalid release row") end
    else error("invalid release pages") end
  ' "$release_file.pages")
  "$GH_BIN" api "repos/$repository/releases/$release_id" >"$release_file"
  jq -e --arg tag "$tag" --argjson id "$release_id" '
    .id == $id and .tag_name == $tag
  ' "$release_file" >/dev/null || { printf 'publication: release lookup identity differs\n' >&2; exit 1; }
)
immutability=$("$GH_BIN" api "repos/$repository/immutable-releases")
printf '%s\n' "$immutability" | jq -e '.enabled == true' >/dev/null || { printf 'publication: immutable GitHub releases must be enabled\n' >&2; exit 1; }
read_release "$scratch/release.json"
jq -e --arg tag "$tag" '
  .tag_name == $tag and (.draft == true or (.draft == false and .immutable == true))
' "$scratch/release.json" >/dev/null || { printf 'publication: existing draft or immutable public release required\n' >&2; exit 1; }
already_public=$(jq -r '.draft == false' "$scratch/release.json")
"$GH_BIN" release download "$tag" --repo "$repository" --dir "$scratch/draft"
[ "$(sha256_file "$scratch/draft/release-manifest.json")" = "$expected" ] || { printf 'publication: reviewed manifest digest differs\n' >&2; exit 1; }
commit=$(jq -er '.source_commit' "$scratch/draft/release-manifest.json")
"$script_dir/require-signed-tag.sh" "$repository" "$tag" "$commit"
runs=$("$GH_BIN" api "repos/$repository/actions/workflows/release.yml/runs?event=push&head_sha=$commit&per_page=100")
printf '%s\n' "$runs" | \
  jq -e --arg tag "$tag" --arg commit "$commit" '
    [.workflow_runs[] | select(.head_sha == $commit and .head_branch == $tag)] |
    sort_by(.run_number,.run_attempt) | last | .status == "completed" and .conclusion == "success"
  ' >/dev/null || { printf 'publication: exact tag build must succeed\n' >&2; exit 1; }
go run ./scripts/release/stable verify --trust "$trust" --directory "$scratch/draft" \
  --version "${tag#v}" --commit "$commit" --state "$trust_state" --published --latest
"$script_dir/verify-stable-oci.sh" "$repository" "$scratch/draft/release-manifest.json"
# A retry after publication performs exactly the same authenticated byte and
# registry checks, then returns without editing the immutable public release.
if [ "$already_public" = true ]; then
  printf 'publication: %s already public and immutable; downloaded bytes verified\n' "$tag"
  exit 0
fi
# Re-fetch immediately before publication to reject asset replacement after review.
read_release "$scratch/recheck.json"
jq -S '[.id,.tag_name,.draft,(.assets | sort_by(.id) | map({id,name,size,updated_at,digest}))]' "$scratch/release.json" >"$scratch/before"
jq -S '[.id,.tag_name,.draft,(.assets | sort_by(.id) | map({id,name,size,updated_at,digest}))]' "$scratch/recheck.json" >"$scratch/after"
cmp -s "$scratch/before" "$scratch/after" || { printf 'publication: draft changed during verification\n' >&2; exit 1; }
if [ "$("$script_dir/release-channel.sh" "$tag")" = prerelease ]; then
  "$GH_BIN" release edit "$tag" --repo "$repository" --draft=false --prerelease=true --latest=false
else
  "$GH_BIN" release edit "$tag" --repo "$repository" --draft=false --prerelease=false --latest
fi
"$GH_BIN" release download "$tag" --repo "$repository" --dir "$scratch/public"
original_id=$(jq -er '.id' "$scratch/release.json")
"$GH_BIN" api "repos/$repository/releases/$original_id" >"$scratch/public-release.json"
jq -e --slurpfile original "$scratch/release.json" '
  .id == $original[0].id and .tag_name == $original[0].tag_name and
  .draft == false and .immutable == true
' "$scratch/public-release.json" >/dev/null || { printf 'publication: public release identity or immutability differs\n' >&2; exit 1; }
[ "$(sha256_file "$scratch/public/release-manifest.json")" = "$expected" ] || { printf 'publication: public manifest differs\n' >&2; exit 1; }
go run ./scripts/release/stable verify --trust "$trust" --directory "$scratch/public" \
  --version "${tag#v}" --commit "$commit" --state "$trust_state" --published --latest
"$script_dir/verify-stable-oci.sh" "$repository" "$scratch/public/release-manifest.json"
printf 'publication: %s public downloads verified\n' "$tag"
