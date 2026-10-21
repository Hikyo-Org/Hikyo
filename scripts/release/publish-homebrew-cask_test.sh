#!/bin/sh
set -eu

fixture_dir=$(mktemp -d "${TMPDIR:-/tmp}/hikyo-homebrew-publish.XXXXXX")
trap 'rm -rf "$fixture_dir"' EXIT HUP INT TERM
bundle="$fixture_dir/bundle"
mkdir -p "$bundle"

printf 'arm64 archive\n' >"$bundle/hikyo_1.2.3_Darwin_arm64.dmg"
printf 'intel archive\n' >"$bundle/hikyo_1.2.3_Darwin_x86_64.dmg"
arm_sha=$(shasum -a 256 "$bundle/hikyo_1.2.3_Darwin_arm64.dmg" | awk '{print $1}')
intel_sha=$(shasum -a 256 "$bundle/hikyo_1.2.3_Darwin_x86_64.dmg" | awk '{print $1}')
jq -n --arg arm_sha "$arm_sha" --arg intel_sha "$intel_sha" '{
	schema: "hikyo.dev/release-manifest/v1", version: "1.2.3", tag: "v1.2.3",
	artifacts: [
		{name: "hikyo_1.2.3_Darwin_arm64.dmg", kind: "installer", sha256: $arm_sha},
		{name: "hikyo_1.2.3_Darwin_x86_64.dmg", kind: "installer", sha256: $intel_sha}
	]
}' >"$bundle/release-manifest.json"

cat >"$fixture_dir/gh" <<'EOF'
#!/bin/sh
printf '%s\n' "$*" >>"$GH_CALLS"
case "$1 $2" in
	'release view')
		printf '{"isDraft":%s,"isPrerelease":%s,"tagName":"%s"}\n' \
			"$GH_RELEASE_DRAFT" "$GH_RELEASE_PRERELEASE" "$GH_RELEASE_TAG"
		;;
	'api repos/Hikyo-Org/homebrew-tap/contents/Casks/hikyo.rb?ref=main') printf 'gh: Not Found (HTTP 404)\n' >&2; exit 1 ;;
	'api repos/Hikyo-Org/homebrew-tap/git/ref/heads/main')
		printf 'bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb\n'
		;;
	'api repos/Hikyo-Org/homebrew-tap/git/ref/heads/release/hikyo-1.2.3')
		[ "${GH_BRANCH_EXISTS:-false}" = true ] || { printf 'gh: Not Found (HTTP 404)\n' >&2; exit 1; }
		printf '{"object":{"sha":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}\n'
		;;
	'api repos/Hikyo-Org/homebrew-tap/contents/Casks/hikyo.rb?ref=release%2Fhikyo-1.2.3')
		if [ "${GH_BRANCH_EXISTS:-false}" = true ]; then
			jq -nc --arg content "${GH_BRANCH_CONTENT:-different}" '{sha:("a"*40),content:$content}'
		else printf 'gh: Not Found (HTTP 404)\n' >&2; exit 1; fi ;;
	'api repos/Hikyo-Org/homebrew-tap/compare/'*)
		if [ "${GH_UNRELATED:-false}" = true ]; then printf '{"files":[{"filename":"other-user-file"}]}\n'; else printf '{"files":[{"filename":"Casks/hikyo.rb"}]}\n'; fi ;;
	'api --method')
		case "$*" in
			*' PATCH repos/Hikyo-Org/homebrew-tap/git/refs/heads/release/hikyo-1.2.3 '* )
				printf '{"object":{"sha":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}}\n'
				;;
			*' POST repos/Hikyo-Org/homebrew-tap/git/refs '*)
				printf '{"ref":"refs/heads/release/hikyo-1.2.3"}\n'
				;;
			*' PUT repos/Hikyo-Org/homebrew-tap/contents/Casks/hikyo.rb '*)
				printf '{"commit":{"sha":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}\n'
				;;
			*) printf 'unexpected gh api mutation: %s\n' "$*" >&2; exit 2 ;;
		esac
		;;
	'pr list') printf '[]\n' ;;
	'pr create') printf 'https://github.com/Hikyo-Org/homebrew-tap/pull/1\n' ;;
	*) printf 'unexpected gh call: %s\n' "$*" >&2; exit 2 ;;
esac
EOF
chmod +x "$fixture_dir/gh"
export GH_BIN="$fixture_dir/gh"
export GH_CALLS="$fixture_dir/gh.calls"
: >"$GH_CALLS"

export GH_RELEASE_DRAFT=true GH_RELEASE_PRERELEASE=false GH_RELEASE_TAG=v1.2.3
if "$(dirname "$0")/publish-homebrew-cask.sh" \
	Hikyo-Org/Hikyo v1.2.3 "$bundle" Hikyo-Org/homebrew-tap \
	>"$fixture_dir/draft.out" 2>"$fixture_dir/draft.err"
then
	printf 'homebrew publish fixture: unsigned draft unexpectedly accepted\n' >&2
	exit 1
fi
grep -F 'homebrew publish: release v1.2.3 is still a draft' "$fixture_dir/draft.err" >/dev/null
[ "$(wc -l <"$GH_CALLS" | tr -d ' ')" -eq 1 ] || {
	printf 'homebrew publish fixture: draft release reached tap mutation\n' >&2
	exit 1
}

: >"$GH_CALLS"
export GH_RELEASE_DRAFT=false GH_RELEASE_PRERELEASE=false
"$(dirname "$0")/publish-homebrew-cask.sh" \
	Hikyo-Org/Hikyo v1.2.3 "$bundle" Hikyo-Org/homebrew-tap >/dev/null
grep -F 'api repos/Hikyo-Org/homebrew-tap/contents/Casks/hikyo.rb?ref=main' "$GH_CALLS" >/dev/null
grep -F 'api --method POST repos/Hikyo-Org/homebrew-tap/git/refs' "$GH_CALLS" >/dev/null
grep -F 'api --method PUT repos/Hikyo-Org/homebrew-tap/contents/Casks/hikyo.rb' "$GH_CALLS" >/dev/null
grep -F 'contents/Casks/hikyo.rb?ref=release%2Fhikyo-1.2.3' "$GH_CALLS" >/dev/null
grep -F 'pr create --repo Hikyo-Org/homebrew-tap --base main --head release/hikyo-1.2.3' "$GH_CALLS" >/dev/null
if grep -F 'pr merge' "$GH_CALLS" >/dev/null; then
	printf 'homebrew publish fixture: publisher attempted to merge tap PR\n' >&2
	exit 1
fi

: >"$GH_CALLS"
export GH_BRANCH_EXISTS=true
"$(dirname "$0")/render-homebrew-cask.sh" "$bundle/release-manifest.json" "$bundle" "$fixture_dir/expected.rb" Hikyo-Org/Hikyo >/dev/null
GH_BRANCH_CONTENT=$(base64 <"$fixture_dir/expected.rb" | tr -d '\n')
export GH_BRANCH_CONTENT
"$(dirname "$0")/publish-homebrew-cask.sh" \
	Hikyo-Org/Hikyo v1.2.3 "$bundle" Hikyo-Org/homebrew-tap >/dev/null
if grep -E 'api --method (PATCH|PUT)|force=true' "$GH_CALLS" >/dev/null; then
	printf 'homebrew publish fixture: existing verified cask was overwritten\n' >&2; exit 1
fi
for scenario in unrelated different; do
	: >"$GH_CALLS"
	if [ "$scenario" = unrelated ]; then export GH_UNRELATED=true; else unset GH_BRANCH_CONTENT; fi
	if "$(dirname "$0")/publish-homebrew-cask.sh" Hikyo-Org/Hikyo v1.2.3 "$bundle" Hikyo-Org/homebrew-tap >"$fixture_dir/retry.out" 2>"$fixture_dir/retry.err"; then
		printf 'homebrew publish fixture: unsafe existing branch accepted\n' >&2; exit 1
	fi
	if grep -E 'api --method|pr create' "$GH_CALLS" >/dev/null; then
		printf 'homebrew publish fixture: unsafe existing branch mutated\n' >&2; exit 1
	fi
	unset GH_UNRELATED
done
unset GH_BRANCH_EXISTS

: >"$GH_CALLS"
export GH_RELEASE_DRAFT=false GH_RELEASE_PRERELEASE=true
"$(dirname "$0")/publish-homebrew-cask.sh" \
	Hikyo-Org/Hikyo v1.2.3 "$bundle" Hikyo-Org/homebrew-tap >/dev/null
[ "$(wc -l <"$GH_CALLS" | tr -d ' ')" -eq 1 ] || {
	printf 'homebrew publish fixture: prerelease reached tap mutation\n' >&2
	exit 1
}

: >"$GH_CALLS"
export GH_RELEASE_DRAFT=false GH_RELEASE_PRERELEASE=false GH_RELEASE_TAG=v0.9.0
"$(dirname "$0")/publish-homebrew-cask.sh" \
	Hikyo-Org/Hikyo v0.9.0 "$bundle" Hikyo-Org/homebrew-tap >/dev/null
[ "$(wc -l <"$GH_CALLS" | tr -d ' ')" -eq 1 ] || {
	printf 'homebrew publish fixture: policy prerelease reached tap mutation\n' >&2
	exit 1
}

printf 'homebrew publish fixture: only a published stable release updates tap\n'
