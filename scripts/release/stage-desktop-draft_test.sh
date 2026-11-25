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
case "$*" in
	'api --paginate --slurp repos/example/repo/releases?per_page=100') cat "$F/pages.json" ;;
	'api --paginate --slurp repos/example/repo/releases/88/assets?per_page=100')
		n=0
		printf '[['
		for f in "$F"/uploaded/*; do
			n=$((n + 1))
			[ "$n" -eq 1 ] || printf ','
			printf '{"id":%s,"name":"%s"}' "$((100 + n))" "$(basename "$f")"
		done
		printf ']]\n' ;;
	*'--method DELETE'*) ;;
	'api --method POST repos/example/repo/releases '*)
		[ -z "${FIXTURE_MODE:-}" ] || [ "$FIXTURE_MODE" != wrong-target ] || { printf '{"id":88,"tag_name":"desktop-v1.4.0","draft":true,"target_commitish":"main"}\n'; exit 0; }
		printf '{"id":88,"tag_name":"desktop-v1.4.0","draft":true,"target_commitish":"%s"}\n' "$GITHUB_SHA" ;;
	*'--method POST https://uploads.github.com/repos/example/repo/releases/88/assets?name='*)
		case " $* " in *' -H Content-Type: application/octet-stream '*) ;; *) printf 'upload lacks the octet-stream Content-Type\n' >&2; exit 1 ;; esac
		name=${*##*name=}
		name=${name%% *}
		mkdir -p "$F/uploaded"
		while [ "$#" -gt 0 ]; do [ "$1" != --input ] || cp "$2" "$F/uploaded/$name"; shift; done ;;
	'api repos/example/repo/releases/88') printf '{"id":88,"tag_name":"desktop-v1.4.0","draft":true}\n' ;;
	*'repos/example/repo/releases/assets/'*)
		case " $* " in *' -H Accept: application/octet-stream '*) ;; *) printf 'download lacks the octet-stream Accept\n' >&2; exit 1 ;; esac
		id=${*##*/}
		n=0
		for f in "$F"/uploaded/*; do n=$((n + 1)); [ "$((100 + n))" != "$id" ] || cat "$f"; done ;;
	*) printf 'unexpected gh %s\n' "$*" >&2; exit 1 ;;
esac
SH
chmod +x "$F/bin/"*
reset() {
	rm -rf "$F/dist" "$F/log" "$F/work" "$F/uploaded"
	mkdir -p "$F/dist" "$F/work"
	printf 'arm\n' >"$F/dist/Hikyo-Desktop-1.4.0-arm64.dmg"
	printf 'intel!\n' >"$F/dist/Hikyo-Desktop-1.4.0-x86_64.dmg"
	printf '[[]]\n' >"$F/pages.json"
	: >"$F/log"
	ARM=$(shasum -a 256 "$F/dist/Hikyo-Desktop-1.4.0-arm64.dmg" | cut -d' ' -f1)
	X86=$(shasum -a 256 "$F/dist/Hikyo-Desktop-1.4.0-x86_64.dmg" | cut -d' ' -f1)
}
stage() {
	env PATH="$F/bin:$PATH" F="$F" COSIGN_BIN="$F/bin/cosign" GH_BIN="$F/bin/gh" RUNNER_TEMP="$F/work" \
		GITHUB_ACTIONS=true GITHUB_REPOSITORY=example/repo GITHUB_SHA="$commit" \
		DESKTOP_SHA256_ARM64="${ARM_RECORDED:-$ARM}" DESKTOP_SHA256_X86_64="${X86_RECORDED:-$X86}" FIXTURE_MODE="${FIXTURE_MODE:-}" \
		GITHUB_REF="${REF:-refs/tags/v1.4.0}" GITHUB_WORKFLOW_REF="${WORKFLOW_REF:-example/repo/.github/workflows/release.yml@${REF:-refs/tags/v1.4.0}}" \
		"$script_dir/stage-desktop-draft.sh" release/trust "${DIR:-$F/dist}"
}
refused() {
	label=$1
	if stage >"$F/out" 2>&1; then fail "$label accepted"; fi
	if grep -E 'method POST' "$F/log" >/dev/null; then fail "$label reached release creation"; fi
}

reset
stage >"$F/out" || fail "valid staging refused: $(cat "$F/out")"
grep -F -- "cosign sign-blob --yes --use-signing-config=false --new-bundle-format=true --rekor-url=https://rekor.sigstore.dev --trusted-root=release/trust/stable-trusted-root.json --bundle" "$F/log" >/dev/null ||
	fail 'cosign flags differ from sign-stable-draft.sh'
grep -F -- "gh api --method POST repos/example/repo/releases -f tag_name=desktop-v1.4.0 -f target_commitish=$commit" "$F/log" >/dev/null || fail 'draft not created at the commit'
grep -F -- '-F draft=true' "$F/log" >/dev/null || fail 'created release is not a draft'
grep -F -- 'make_latest=false' "$F/log" >/dev/null || fail 'created release may become Latest'
if grep -E -- '--verify-tag|gh release' "$F/log" >/dev/null; then fail 'run must address the release by ID through the API, never by tag'; fi
grep -F 'releases/88/assets?name=Hikyo-Desktop-1.4.0-arm64.dmg' "$F/log" >/dev/null || fail 'upload not bound to the release ID'
grep -F 'releases/assets/101' "$F/log" >/dev/null || fail 'download not bound to asset IDs'
grep -F 'staged and verified' "$F/out" >/dev/null || fail 'no completion line'
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

reset; FIXTURE_MODE=wrong-target
if stage >"$F/out" 2>&1; then fail 'release with another target accepted'; fi
grep -F 'not the expected draft' "$F/out" >/dev/null || fail 'wrong target not reported'
if grep -F 'uploads.github.com' "$F/log" >/dev/null; then fail 'uploaded into an unexpected release'; fi
FIXTURE_MODE=
reset; ARM_RECORDED=$(printf '%064d' 0) refused 'arm64 image differs from its recorded digest'
reset; X86_RECORDED=nothex refused 'malformed recorded x86_64 digest'
reset; REF=refs/tags/v1.4.0-rc.1 refused 'prerelease tag'
reset; REF=refs/tags/v0.9.0 refused 'pre-1.0 tag'
reset; REF=refs/heads/main refused 'branch ref'
reset; WORKFLOW_REF=example/repo/.github/workflows/other.yml@refs/tags/v1.4.0 refused 'other workflow'
reset; rm "$F/dist/Hikyo-Desktop-1.4.0-x86_64.dmg"; refused 'missing image'
reset; printf 'x' >"$F/dist/extra.txt"; refused 'extra file'
reset; : >"$F/dist/Hikyo-Desktop-1.4.0-arm64.dmg"; refused 'empty image'
reset; ln -sf Hikyo-Desktop-1.4.0-x86_64.dmg "$F/dist/Hikyo-Desktop-1.4.0-arm64.dmg"; refused 'symlinked image'
printf 'stage desktop fixture: identity, exact asset set, draft replacement and signing flags hold\n'
