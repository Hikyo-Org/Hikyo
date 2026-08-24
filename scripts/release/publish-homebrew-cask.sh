#!/bin/sh
set -eu

: "${GH_BIN:=gh}"

script_dir=$(CDPATH='' cd -- "$(dirname "$0")" && pwd)
# shellcheck disable=SC1091
. "$script_dir/../lib/release.sh"

fail() {
	printf 'homebrew publish: %s\n' "$*" >&2
	exit 1
}

[ "$#" -eq 4 ] || fail 'usage: publish-homebrew-cask.sh SOURCE_REPOSITORY TAG BUNDLE TAP_REPOSITORY'
source_repository=$1
tag=$2
bundle=$3
tap_repository=$4

for repository in "$source_repository" "$tap_repository"; do
	case "$repository" in
		*[!A-Za-z0-9._/-]* | */*/* | /* | */ | '') fail "invalid repository $repository" ;;
	esac
done
case "$tag" in v*) version=${tag#v} ;; *) fail "invalid release tag $tag" ;; esac
is_semver "$version" || fail "invalid release tag $tag"
[ -d "$bundle" ] || fail "missing verified release bundle $bundle"

release_json=$(
	"$GH_BIN" release view "$tag" --repo "$source_repository" \
		--json isDraft,isPrerelease,tagName
) || fail "cannot inspect source release $tag"
jq -e --arg tag "$tag" '
	.tagName == $tag and
	(.isDraft | type == "boolean") and
	(.isPrerelease | type == "boolean")
' <<EOF >/dev/null || fail "invalid release state for $tag"
$release_json
EOF
[ "$(printf '%s\n' "$release_json" | jq -r '.isDraft')" = false ] \
	|| fail "release $tag is still a draft"
channel=$("$script_dir/release-channel.sh" "$tag") \
	|| fail "cannot classify release channel for $tag"
if [ "$channel" = prerelease ] || \
	[ "$(printf '%s\n' "$release_json" | jq -r '.isPrerelease')" = true ]; then
	printf 'homebrew publish: prerelease %s does not update stable tap\n' "$tag"
	exit 0
fi

manifest="$bundle/release-manifest.json"
[ -f "$manifest" ] || fail 'verified bundle has no release manifest'
[ "$(jq -r '.tag' "$manifest")" = "$tag" ] || fail 'bundle tag differs from public release'
scratch=$(mktemp -d "${TMPDIR:-/tmp}/hikyo-homebrew-publish.XXXXXX")
trap 'rm -rf "$scratch"' EXIT HUP INT TERM
cask="$scratch/hikyo.rb"
"$script_dir/render-homebrew-cask.sh" "$manifest" "$bundle" "$cask" "$source_repository" >/dev/null
cask_content=$(base64 <"$cask" | tr -d '\n')

contents_endpoint="repos/$tap_repository/contents/Casks/hikyo.rb"
current_sha=
if current_json=$("$GH_BIN" api "$contents_endpoint?ref=main" 2>/dev/null); then
	printf '%s\n' "$current_json" | jq -e '.sha | test("^[0-9a-f]{40}$")' >/dev/null \
		|| fail 'tap returned invalid cask identity'
	current_content=$(printf '%s\n' "$current_json" | jq -r '.content' | tr -d '\n')
	if [ "$current_content" = "$cask_content" ]; then
		printf 'homebrew publish: tap already contains Hikyo %s\n' "$version"
		exit 0
	fi
fi

branch="release/hikyo-$version"
branch_query=$(printf '%s' "$branch" | jq -sRr @uri)
base_sha=$("$GH_BIN" api "repos/$tap_repository/git/ref/heads/main" \
	--jq '.object.sha') || fail 'cannot resolve tap main'
case "$base_sha" in *[!0-9a-f]* | '') fail 'tap main returned invalid commit' ;; esac
[ "${#base_sha}" -eq 40 ] || fail 'tap main returned invalid commit'
if "$GH_BIN" api "repos/$tap_repository/git/ref/heads/$branch" >/dev/null 2>&1; then
	"$GH_BIN" api --method PATCH "repos/$tap_repository/git/refs/heads/$branch" \
		-f sha="$base_sha" -F force=true >/dev/null \
		|| fail "cannot refresh tap branch $branch from main"
else
	"$GH_BIN" api --method POST "repos/$tap_repository/git/refs" \
		-f ref="refs/heads/$branch" -f sha="$base_sha" >/dev/null \
		|| fail "cannot create tap branch $branch"
fi

branch_content=
if branch_json=$("$GH_BIN" api "$contents_endpoint?ref=$branch_query" 2>/dev/null); then
	current_sha=$(printf '%s\n' "$branch_json" | jq -er '.sha | select(test("^[0-9a-f]{40}$"))') \
		|| fail 'tap branch returned invalid cask identity'
	branch_content=$(printf '%s\n' "$branch_json" | jq -r '.content' | tr -d '\n')
fi
if [ "$branch_content" != "$cask_content" ]; then
	if [ -n "$current_sha" ]; then
		update_json=$(
			"$GH_BIN" api --method PUT "$contents_endpoint" \
				-f message="chore: update hikyo cask to $version" \
				-f content="$cask_content" -f sha="$current_sha" -f branch="$branch"
		) || fail 'tap update failed'
	else
		update_json=$(
			"$GH_BIN" api --method PUT "$contents_endpoint" \
				-f message="chore: add hikyo cask $version" \
				-f content="$cask_content" -f branch="$branch"
		) || fail 'tap creation failed'
	fi
	jq -e '.commit.sha | test("^[0-9a-f]{40}$")' <<EOF >/dev/null \
		|| fail 'tap update returned no commit'
$update_json
EOF
fi
pr_json=$("$GH_BIN" pr list --repo "$tap_repository" --head "$branch" --state all \
	--limit 1 --json state,url) || fail 'cannot inspect tap pull request'
pr_state=$(printf '%s\n' "$pr_json" | jq -r '.[0].state // empty')
pr_url=$(printf '%s\n' "$pr_json" | jq -r '.[0].url // empty')
case "$pr_state" in
	OPEN) ;;
	MERGED) fail "tap pull request was already merged but main differs: $pr_url" ;;
	CLOSED) fail "tap pull request was closed without merge: $pr_url" ;;
	'')
		pr_url=$("$GH_BIN" pr create --repo "$tap_repository" --base main --head "$branch" \
			--title="Update Hikyo cask to $version" \
			--body="Publishes the cask for signed Hikyo release $tag. Source: https://github.com/$source_repository/releases/tag/$tag") \
			|| fail 'cannot open tap pull request'
		;;
	*) fail 'tap returned invalid pull-request state' ;;
esac
case "$pr_url" in https://github.com/*/pull/[0-9]*) ;; *) fail 'tap returned invalid pull-request URL' ;; esac
printf 'homebrew publish: review and merge %s\n' "$pr_url"
