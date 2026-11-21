#!/bin/sh
# Fake cosign, gh and go: inventory shape, exact signing identity and draft handling.
set -eu
script_dir=$(CDPATH='' cd -- "$(dirname "$0")" && pwd)
F=$(mktemp -d "${TMPDIR:-/tmp}/hikyo-stage-desktop-test.XXXXXX")
trap 'rm -rf "$F"' EXIT HUP INT TERM
fail() { printf 'stage desktop fixture failed: %s\n' "$*" >&2; exit 1; }
commit=1111111111111111111111111111111111111111
mkdir "$F/bin"
cat >"$F/bin/cosign" <<'SH'
#!/bin/sh
printf 'cosign %s\n' "$*" >>"$F/log"
while [ "$#" -gt 0 ]; do [ "$1" != --bundle ] || { printf 'bundle\n' >"$2"; exit 0; }; shift; done
exit 1
SH
cat >"$F/bin/go" <<'SH'
#!/bin/sh
printf 'go %s\n' "$*" >>"$F/log"
SH
cat >"$F/bin/gh" <<'SH'
#!/bin/sh
printf 'gh %s\n' "$*" >>"$F/log"
case "$1 $2" in
	'api --paginate') cat "$F/pages.json" ;;
	'api --method') ;;
	'release create'|'release upload') ;;
	'release download')
		while [ "$#" -gt 0 ]; do [ "$1" != --dir ] || cp "$F/dist/"* "$2/"; shift; done ;;
	*) exit 1 ;;
esac
SH
chmod +x "$F/bin/"*
reset() {
	rm -rf "$F/dist" "$F/log" "$F/work"
	mkdir -p "$F/dist" "$F/work"
	printf 'arm\n' >"$F/dist/Hikyo-Desktop-1.4.0-arm64.dmg"
	printf 'intel!\n' >"$F/dist/Hikyo-Desktop-1.4.0-x86_64.dmg"
	printf '[[]]\n' >"$F/pages.json"
	: >"$F/log"
}
stage() {
	env PATH="$F/bin:$PATH" F="$F" COSIGN_BIN="$F/bin/cosign" GH_BIN="$F/bin/gh" RUNNER_TEMP="$F/work" \
		GITHUB_ACTIONS=true GITHUB_REPOSITORY=example/repo GITHUB_SHA="$commit" \
		GITHUB_REF="${REF:-refs/tags/v1.4.0}" GITHUB_WORKFLOW_REF="${WORKFLOW_REF:-example/repo/.github/workflows/release.yml@${REF:-refs/tags/v1.4.0}}" \
		"$script_dir/stage-desktop-draft.sh" release/trust "${DIR:-$F/dist}"
}
refused() {
	label=$1
	if stage >"$F/out" 2>&1; then fail "$label accepted"; fi
	if grep -E 'gh release (create|upload)' "$F/log" >/dev/null; then fail "$label reached release creation"; fi
}

reset
stage >"$F/out" || fail "valid staging refused: $(cat "$F/out")"
grep -F -- "cosign sign-blob --yes --use-signing-config=false --new-bundle-format=true --rekor-url=https://rekor.sigstore.dev --trusted-root=release/trust/stable-trusted-root.json --bundle" "$F/log" >/dev/null ||
	fail 'cosign flags differ from sign-stable-draft.sh'
grep -F -- "gh release create desktop-v1.4.0 --draft --target $commit --latest=false" "$F/log" >/dev/null || fail 'draft not created with --draft --target --latest=false'
if grep -F -- '--verify-tag' "$F/log" >/dev/null; then fail 'run must not require an existing tag'; fi
[ "$(grep -c 'verify-desktop' "$F/log")" -eq 2 ] || fail 'inventory not verified before and after staging'
if grep -F 'gh api --method DELETE' "$F/log" >/dev/null; then fail 'deleted a release when none existed'; fi

# Rerun: this lane's own earlier draft is replaced, a public release is refused.
reset
printf '[[{"id":77,"tag_name":"desktop-v1.4.0","draft":true}]]\n' >"$F/pages.json"
stage >"$F/out" || fail 'rerun over an earlier draft refused'
grep -F 'gh api --method DELETE repos/example/repo/releases/77' "$F/log" >/dev/null || fail 'earlier draft not replaced'
reset
printf '[[{"id":78,"tag_name":"desktop-v1.4.0","draft":false}]]\n' >"$F/pages.json"
refused 'already public desktop release'
if grep -F 'gh api --method DELETE' "$F/log" >/dev/null; then fail 'deleted a public release'; fi
reset
printf '[[{"id":79,"tag_name":"v1.4.0","draft":true}]]\n' >"$F/pages.json"
stage >"$F/out" || fail 'the CLI draft blocked desktop staging'
if grep -F 'gh api --method DELETE' "$F/log" >/dev/null; then fail 'deleted the CLI draft'; fi

reset; REF=refs/tags/v1.4.0-rc.1 refused 'prerelease tag'
reset; REF=refs/tags/v0.9.0 refused 'pre-1.0 tag'
reset; REF=refs/heads/main refused 'branch ref'
reset; WORKFLOW_REF=example/repo/.github/workflows/other.yml@refs/tags/v1.4.0 refused 'other workflow'
reset; rm "$F/dist/Hikyo-Desktop-1.4.0-x86_64.dmg"; refused 'missing image'
reset; printf 'x' >"$F/dist/extra.txt"; refused 'extra file'
reset; : >"$F/dist/Hikyo-Desktop-1.4.0-arm64.dmg"; refused 'empty image'
reset; ln -sf Hikyo-Desktop-1.4.0-x86_64.dmg "$F/dist/Hikyo-Desktop-1.4.0-arm64.dmg"; refused 'symlinked image'
printf 'stage desktop fixture: identity, exact asset set, draft replacement and signing flags hold\n'
