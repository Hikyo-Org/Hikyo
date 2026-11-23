#!/bin/sh
# Fake gh and go: the desktop publisher's gates and its idempotent retry.
set -eu
script_dir=$(CDPATH='' cd -- "$(dirname "$0")" && pwd)
repo_root=$(CDPATH='' cd -- "$script_dir/../.." && pwd)
policy=$repo_root/release/repository
F=$(mktemp -d "${TMPDIR:-/tmp}/hikyo-publish-desktop-test.XXXXXX")
trap 'rm -rf "$F"' EXIT HUP INT TERM
fail() { printf 'publish desktop fixture failed: %s\n' "$*" >&2; exit 1; }
commit=1111111111111111111111111111111111111111
mkdir "$F/bin"

cat >"$F/bin/gh" <<'GH'
#!/bin/sh
set -eu
printf 'gh %s\n' "$*" >>"$F/log"
arg_after() { # value following flag $1
	flag=$1
	shift
	while [ "$#" -gt 0 ]; do [ "$1" != "$flag" ] || { printf '%s' "$2"; return; }; shift; done
}
case "$1 $2" in
	'release download')
		tag=$3 dir=$(arg_after --dir "$@") pattern=$(arg_after --pattern "$@")
		for file in "$F/assets/$tag"/*; do
			[ -e "$file" ] || continue
			[ -z "$pattern" ] || [ "$(basename "$file")" = "$pattern" ] || continue
			cp "$file" "$dir/"
		done
		exit 0 ;;
esac
[ "$1" = api ] || exit 1
shift
if [ "$1" = --method ] && [ "$2" = PATCH ]; then
	[ "$FIXTURE_MODE" != edit-fails ] || exit 1
	id=${3##*/}
	case " $* " in
		*' -F draft=false '*'-F prerelease=false '*'-f make_latest=false '*) ;;
		*) printf 'wrong publication fields: %s\n' "$*" >&2; exit 1 ;;
	esac
	jq --argjson id "$id" '(.[] | select(.id == $id)) |= (.draft = false | .immutable = true)' "$F/releases.json" >"$F/releases.new"
	[ -f "$F/tagref.json" ] || cp "$F/tagref.created" "$F/tagref.json"
	mv "$F/releases.new" "$F/releases.json"
	exit 0
fi
if [ "$1" = --paginate ]; then jq -c '[.]' "$F/releases.json"; exit 0; fi
case "$1" in
	repos/example/repo/immutable-releases) cat "$F/immutable.json" ;;
	repos/example/repo/releases/latest) cat "$F/latest.json" ;;
	repos/example/repo/releases/*)
		id=${1##*/}
		n=$(($(cat "$F/reads-$id" 2>/dev/null || echo 0) + 1))
		printf '%s' "$n" >"$F/reads-$id"
		if [ "$FIXTURE_MODE" = changing ] && [ "$n" -ge 2 ] && [ "$id" = 20 ]; then
			jq --argjson id "$id" '.[] | select(.id == $id) | .assets[0].digest = "sha256:changed"' "$F/releases.json"
		else
			jq --argjson id "$id" '.[] | select(.id == $id)' "$F/releases.json"
		fi ;;
	repos/example/repo/git/ref/tags/*)
		[ "$FIXTURE_MODE" != tag-error ] || { printf 'gh: Server Error (HTTP 502)\n' >&2; exit 1; }
		[ -f "$F/tagref.json" ] || { printf 'gh: Not Found (HTTP 404)\n' >&2; exit 1; }
		cat "$F/tagref.json" ;;
	repos/example/repo/rulesets\?*) cat "$F/rulesets.json" ;;
	repos/example/repo/rulesets/*) cat "$F/ruleset-${1##*/}.json" ;;
	*) printf 'unexpected gh api %s\n' "$*" >&2; exit 1 ;;
esac
GH
cat >"$F/bin/go" <<'GO'
#!/bin/sh
set -eu
printf 'go %s\n' "$*" >>"$F/log"
[ "$FIXTURE_MODE" != verify-fails ] || exit 1

GO
chmod +x "$F/bin/gh" "$F/bin/go"

reset() {
	FIXTURE_MODE=${1:-ok}
	rm -rf "$F/assets" "$F/log" "$F"/reads-*
	mkdir -p "$F/assets/v1.4.0" "$F/assets/desktop-v1.4.0"
	printf '{"source_commit":"%s"}\n' "$commit" >"$F/assets/v1.4.0/release-manifest.json"
	printf '{"source_commit":"%s"}\n' "$commit" >"$F/assets/desktop-v1.4.0/desktop-inventory.json"
	printf 'dmg\n' >"$F/assets/desktop-v1.4.0/Hikyo-Desktop-1.4.0-arm64.dmg"
	jq -n --arg commit "$commit" '[
		{id: 10, tag_name: "v1.4.0", draft: false, prerelease: false, immutable: true, target_commitish: $commit, assets: []},
		{id: 20, tag_name: "desktop-v1.4.0", draft: true, prerelease: false, immutable: false, target_commitish: $commit,
		 assets: [{id: 1, name: "a.dmg", size: 4, updated_at: "2026-11-21T00:00:00Z", digest: "sha256:aa"}]}
	]' >"$F/releases.json"
	printf '{"enabled":true}\n' >"$F/immutable.json"
	printf '{"tag_name":"v1.4.0"}\n' >"$F/latest.json"
	jq -n --arg commit "$commit" '{ref: "refs/tags/desktop-v1.4.0", object: {type: "commit", sha: $commit}}' >"$F/tagref.created"
	cp "$F/tagref.created" "$F/tagref.json"
	jq -c '{id: 11} + .' "$policy/desktop-tag-creation.json" >"$F/ruleset-11.json"
	jq -c '{id: 12} + .' "$policy/desktop-tag-immutability.json" >"$F/ruleset-12.json"
	jq -s '[.[] | {id, name, enforcement}]' "$F/ruleset-11.json" "$F/ruleset-12.json" >"$F/rulesets.json"
	: >"$F/log"
}
mutate() { # FILE JQ-FILTER
	jq -c "$2" "$F/$1" >"$F/mutated" && mv "$F/mutated" "$F/$1"
}
publish() {
	env GH_BIN="$F/bin/gh" PATH="$F/bin:$PATH" F="$F" FIXTURE_MODE="$FIXTURE_MODE" "$script_dir/publish-desktop-draft.sh" "$@"
}
edited() { grep -F 'gh api --method PATCH' "$F/log" >/dev/null; }
expect_refused() { # label, args...; refusal must happen before publication
	label=$1
	shift
	if publish "$@" >"$F/out" 2>&1; then fail "$label accepted"; fi
	if edited; then fail "$label published before refusing"; fi
}

reset
publish example/repo 1.4.0 release/trust >"$F/out" || fail "valid draft refused: $(cat "$F/out")"
grep -F 'gh api --method PATCH repos/example/repo/releases/20 -F draft=false -F prerelease=false -f make_latest=false' "$F/log" >/dev/null ||
	fail 'publication is not by release ID with draft=false prerelease=false make_latest=false'
if grep -F 'gh release edit' "$F/log" >/dev/null; then fail 'publication addressed the release by tag'; fi
[ "$(grep -c 'verify-desktop' "$F/log")" -eq 2 ] || fail 'draft and public bytes were not both verified'
grep -F -- "--version 1.4.0 --commit $commit" "$F/log" >/dev/null || fail 'verifier not bound to version and commit'
if grep -F 'releases/10 ' "$F/log" | grep -F PATCH >/dev/null; then fail 'CLI release was edited'; fi

# A rerun on an already public release verifies the bytes and publishes nothing.
publish example/repo 1.4.0 release/trust >"$F/out" || fail 'already-public release refused'
grep -F 'already public and immutable' "$F/out" >/dev/null || fail 'already-public path not reported'
[ "$(grep -c 'method PATCH' "$F/log")" -eq 1 ] || fail 'retry edited the public release'

reset
for version in 1.4.0-rc.1 0.9.0 1.4.0-nightly.20261101.1.gabcdef12 nonsense; do
	expect_refused "version $version" example/repo "$version" release/trust
done
expect_refused 'bad repository' not-a-repository 1.4.0 release/trust
[ ! -s "$F/log" ] || fail 'a refused version reached GitHub'

scenario() { # label, file, filter
	reset
	mutate "$2" "$3"
	if [ "$2" = ruleset-11.json ] || [ "$2" = ruleset-12.json ]; then
		jq -s '[.[] | {id, name, enforcement}]' "$F/ruleset-11.json" "$F/ruleset-12.json" >"$F/rulesets.json"
	fi
	expect_refused "$1" example/repo 1.4.0 release/trust
}
scenario 'immutable releases disabled' immutable.json '.enabled = false'
scenario 'creation ruleset disabled' ruleset-11.json '.enforcement = "disabled"'
scenario 'immutability ruleset weakened' ruleset-12.json '.rules = [{type: "update"}]'
scenario 'CLI release still a draft' releases.json '(.[0].draft) = true'
scenario 'CLI release not immutable' releases.json '(.[0].immutable) = false'
scenario 'CLI release a prerelease' releases.json '(.[0].prerelease) = true'
scenario 'desktop release absent' releases.json 'map(select(.id != 20))'
scenario 'duplicate desktop drafts' releases.json '. + [.[1] + {id: 21}]'
scenario 'desktop release marked prerelease' releases.json '(.[1].prerelease) = true'
scenario 'public desktop release not immutable' releases.json '(.[1].draft) = false'
scenario 'target commit differs' releases.json '(.[1].target_commitish) = "2222222222222222222222222222222222222222"'
scenario 'target is a branch' releases.json '(.[1].target_commitish) = "main"'
reset
printf '{"source_commit":"2222222222222222222222222222222222222222"}\n' >"$F/assets/desktop-v1.4.0/desktop-inventory.json"
expect_refused 'inventory commit differs from manifest' example/repo 1.4.0 release/trust
reset
printf '{"source_commit":"not-a-commit"}\n' >"$F/assets/v1.4.0/release-manifest.json"
expect_refused 'invalid manifest commit' example/repo 1.4.0 release/trust
reset verify-fails
expect_refused 'verifier rejects the draft' example/repo 1.4.0 release/trust
reset changing
expect_refused 'draft changed during verification' example/repo 1.4.0 release/trust
grep -F 'draft changed during verification' "$F/out" >/dev/null || fail 'change not reported'

# Post-publication checks fail loudly once the release is public.
reset
mutate latest.json '.tag_name = "desktop-v1.4.0"'
if publish example/repo 1.4.0 release/trust >"$F/out" 2>&1; then fail 'moved latest accepted'; fi
grep -F 'desktop-v release became the latest' "$F/out" >/dev/null || fail 'desktop Latest not reported'
# Finishing an older version while a newer CLI release is Latest is fine.
reset
mutate latest.json '.tag_name = "v1.5.0"'
publish example/repo 1.4.0 release/trust >"$F/out" 2>&1 || fail "newer Latest refused: $(cat "$F/out")"
# A missing tag is normal before publication: publishing creates it from the target.
reset
rm "$F/tagref.json"
publish example/repo 1.4.0 release/trust >"$F/out" 2>&1 || fail "draft without its tag refused: $(cat "$F/out")"
# A tag that already exists elsewhere is refused before the release can go public.
for target in sha type; do
	reset
	case $target in
		sha) mutate tagref.json '.object.sha = "2222222222222222222222222222222222222222"' ;;
		type) mutate tagref.json '.object.type = "tag"' ;;
	esac
	expect_refused "existing tag with a different $target" example/repo 1.4.0 release/trust
	grep -F 'already exists and does not point at' "$F/out" >/dev/null || fail "conflicting tag ($target) not reported"
	if publish --check-only example/repo 1.4.0 release/trust >"$F/out" 2>&1; then fail "check-only accepted a conflicting tag ($target)"; fi
done
# Any tag lookup failure other than 404 refuses too.
reset tag-error
expect_refused 'unreadable tag ref' example/repo 1.4.0 release/trust
grep -F 'cannot read refs/tags/desktop-v1.4.0' "$F/out" >/dev/null || fail 'tag lookup failure not reported'
reset edit-fails
if publish example/repo 1.4.0 release/trust >"$F/out" 2>&1; then fail 'failed edit accepted'; fi
# An already public desktop release that became Latest is still refused on retry.
reset
mutate releases.json '(.[1].draft) = false | (.[1].immutable) = true'
mutate latest.json '.tag_name = "desktop-v1.4.0"'
if publish example/repo 1.4.0 release/trust >"$F/out" 2>&1; then fail 'already-public desktop Latest accepted'; fi
grep -F 'desktop-v release became the latest' "$F/out" >/dev/null || fail 'already-public desktop Latest not reported'
# An already public release whose tag moved is refused too.
reset
mutate releases.json '(.[1].draft) = false | (.[1].immutable) = true'
mutate tagref.json '.object.type = "tag"'
if publish example/repo 1.4.0 release/trust >"$F/out" 2>&1; then fail 'public release with a wrong tag accepted'; fi

# Check-only mode: verifies the staged draft against a still-draft v release and
# publishes, edits and downloads nothing public.
reset
mutate releases.json '(.[0].draft) = true | (.[0].immutable) = false'
publish --check-only example/repo 1.4.0 release/trust >"$F/out" 2>&1 || fail "check of a draft v release refused: $(cat "$F/out")"
edited && fail 'check-only mode published'
[ "$(grep -c 'verify-desktop' "$F/log")" -eq 1 ] || fail 'check-only did not verify the draft bytes'
for case in 'target_commitish differs' 'inventory commit differs' 'verifier fails' 'no desktop draft' 'two desktop releases' 'v release is a prerelease' 'rulesets missing'; do
	reset
	mutate releases.json '(.[0].draft) = true | (.[0].immutable) = false'
	case $case in
		'target_commitish differs') mutate releases.json '(.[1].target_commitish) = "2222222222222222222222222222222222222222"' ;;
		'inventory commit differs') printf '{"source_commit":"2222222222222222222222222222222222222222"}\n' >"$F/assets/desktop-v1.4.0/desktop-inventory.json" ;;
		'verifier fails') FIXTURE_MODE=verify-fails ;;
		'no desktop draft') mutate releases.json 'del(.[1])' ;;
		'two desktop releases') mutate releases.json '. + [.[1] + {id: 21}]' ;;
		'v release is a prerelease') mutate releases.json '(.[0].prerelease) = true' ;;
		'rulesets missing') mutate rulesets.json '[]' ;;
	esac
	if publish --check-only example/repo 1.4.0 release/trust >"$F/out" 2>&1; then fail "check-only accepted: $case"; fi
	edited && fail "check-only published while refusing: $case"
done
# A check still refuses prerelease versions.
if publish --check-only example/repo 1.4.0-rc.1 release/trust >"$F/out" 2>&1; then fail 'check-only accepted a prerelease version'; fi
printf 'publish desktop fixture: gates, idempotent retry and post-publication checks hold\n'
