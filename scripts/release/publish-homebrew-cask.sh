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

nightly=false
cask_name=hikyo
if [ "${1:-}" = --nightly ]; then
	shift
	nightly=true
	cask_name=hikyo@nightly
fi
[ "$#" -eq 4 ] || fail 'usage: publish-homebrew-cask.sh [--nightly] SOURCE_REPOSITORY TAG BUNDLE TAP_REPOSITORY'
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
if [ "$nightly" = true ]; then
	[ "$(printf '%s\n' "$release_json" | jq -r '.isPrerelease')" = true ] || fail 'nightly must be a public prerelease'
	printf '%s\n' "$version" | grep -Eq '^[0-9]+\.[0-9]+\.[0-9]+-nightly\.[0-9]{8}\.[1-9][0-9]*\.g[0-9a-f]{8}$' || fail 'invalid nightly version'
	# Authentication precedes every tap mutation, including branch creation. Use
	# the existing recovery-authorized nightly verifier, never release-local keys.
	repo_root=$(CDPATH='' cd -- "$script_dir/../.." && pwd)
	bundle=$(CDPATH='' cd -- "$bundle" && pwd)
	(cd "$repo_root" && go run ./scripts/release/nightly verify \
		--trust "${HIKYO_HOMEBREW_TRUST_DIR:-$repo_root/release/trust}" \
		--directory "$bundle") >/dev/null || fail 'nightly bundle authentication failed'
elif [ "$channel" = prerelease ] || \
	[ "$(printf '%s\n' "$release_json" | jq -r '.isPrerelease')" = true ]; then
	printf 'homebrew publish: prerelease %s does not update stable tap\n' "$tag"
	exit 0
fi

manifest="$bundle/release-manifest.json"
[ -f "$manifest" ] || fail 'verified bundle has no release manifest'
[ "$(jq -r '.tag' "$manifest")" = "$tag" ] || fail 'bundle tag differs from public release'
scratch=$(mktemp -d "${TMPDIR:-/tmp}/hikyo-homebrew-publish.XXXXXX")
trap 'rm -rf "$scratch"' EXIT HUP INT TERM
cask="$scratch/$cask_name.rb"
set --
[ "$nightly" = false ] || set -- --nightly
"$script_dir/render-homebrew-cask.sh" "$@" "$manifest" "$bundle" "$cask" "$source_repository" >/dev/null
cask_content=$(base64 <"$cask" | tr -d '\n')

if [ "$nightly" = true ]; then
	# Authenticate the local inventory first, then prove that each public URL
	# actually serves those bytes. A retained valid bundle is not publication proof.
	mkdir "$scratch/public"
	for architecture in arm64 x86_64; do
		image="hikyo_${version}_Darwin_${architecture}.dmg"
		expected_sha=$(jq -r --arg name "$image" '.artifacts[] | select(.name == $name) | .sha256' "$manifest")
		"$GH_BIN" release download "$tag" --repo "$source_repository" \
			--dir "$scratch/public" --pattern "$image" \
			|| fail "cannot download public disk image $image"
		[ -f "$scratch/public/$image" ] && [ ! -L "$scratch/public/$image" ] \
			|| fail "missing regular public disk image $image"
		[ "$(sha256_file "$scratch/public/$image")" = "$expected_sha" ] \
			|| fail "public disk image hash mismatch: $image"
	done
fi

contents_endpoint="repos/$tap_repository/contents/Casks/$cask_name.rb"
current_sha=
if current_json=$("$GH_BIN" api "$contents_endpoint?ref=main" 2>"$scratch/main-error"); then
	printf '%s\n' "$current_json" | jq -e '.sha | test("^[0-9a-f]{40}$")' >/dev/null \
		|| fail 'tap returned invalid cask identity'
	current_content=$(printf '%s\n' "$current_json" | jq -r '.content' | tr -d '\n')
	if [ "$current_content" = "$cask_content" ]; then
		printf 'homebrew publish: tap already contains Hikyo %s\n' "$version"
		exit 0
	fi
else
	grep -F '(HTTP 404)' "$scratch/main-error" >/dev/null || fail 'cannot inspect current tap cask'
fi

branch="release/$cask_name-$version"
branch_query=$(printf '%s' "$branch" | jq -sRr @uri)
base_sha=$("$GH_BIN" api "repos/$tap_repository/git/ref/heads/main" \
	--jq '.object.sha') || fail 'cannot resolve tap main'
case "$base_sha" in *[!0-9a-f]* | '') fail 'tap main returned invalid commit' ;; esac
[ "${#base_sha}" -eq 40 ] || fail 'tap main returned invalid commit'
existing_branch=false
if branch_ref=$("$GH_BIN" api "repos/$tap_repository/git/ref/heads/$branch" 2>"$scratch/branch-error"); then
	existing_branch=true
	branch_head=$(printf '%s\n' "$branch_ref" | jq -er '.object.sha | select(test("^[0-9a-f]{40}$"))') || fail 'tap branch returned invalid commit'
	comparison=$("$GH_BIN" api "repos/$tap_repository/compare/$base_sha...$branch_head") || fail 'cannot inspect existing tap branch changes'
	printf '%s\n' "$comparison" | jq -e --arg path "Casks/$cask_name.rb" '
		(.files | type == "array") and all(.files[]; .filename == $path and (.previous_filename // $path) == $path)
	' >/dev/null || fail 'existing tap branch contains unrelated changes; no changes were overwritten'
else
	grep -F '(HTTP 404)' "$scratch/branch-error" >/dev/null || fail 'cannot determine whether tap branch exists'
	"$GH_BIN" api --method POST "repos/$tap_repository/git/refs" \
		-f ref="refs/heads/$branch" -f sha="$base_sha" >/dev/null \
		|| fail "cannot create tap branch $branch"
fi

branch_content=
if branch_json=$("$GH_BIN" api "$contents_endpoint?ref=$branch_query" 2>"$scratch/content-error"); then
	current_sha=$(printf '%s\n' "$branch_json" | jq -er '.sha | select(test("^[0-9a-f]{40}$"))') \
		|| fail 'tap branch returned invalid cask identity'
	branch_content=$(printf '%s\n' "$branch_json" | jq -r '.content' | tr -d '\n')
else
	grep -F '(HTTP 404)' "$scratch/content-error" >/dev/null || fail 'cannot inspect tap branch cask'
fi
if [ "$branch_content" != "$cask_content" ]; then
	if [ "$existing_branch" = true ] && [ "$branch_head" != "$base_sha" ]; then
		fail 'existing tap branch differs from verified cask; no changes were overwritten'
	fi
	if [ -n "$current_sha" ]; then
		update_json=$(
			"$GH_BIN" api --method PUT "$contents_endpoint" \
				-f message="chore: update $cask_name cask to $version" \
				-f content="$cask_content" -f sha="$current_sha" -f branch="$branch"
		) || fail 'tap update failed'
	else
		update_json=$(
			"$GH_BIN" api --method PUT "$contents_endpoint" \
				-f message="chore: add $cask_name cask $version" \
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
			--title="Update $cask_name cask to $version" \
			--body="Publishes the cask for signed Hikyo release $tag. Source: https://github.com/$source_repository/releases/tag/$tag") \
			|| fail 'cannot open tap pull request'
		;;
	*) fail 'tap returned invalid pull-request state' ;;
esac
case "$pr_url" in https://github.com/*/pull/[0-9]*) ;; *) fail 'tap returned invalid pull-request URL' ;; esac
printf 'homebrew publish: review and merge %s\n' "$pr_url"
