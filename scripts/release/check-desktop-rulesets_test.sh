#!/bin/sh
# The desktop rulesets mirror the v* ones, and the readback refuses anything weaker.
set -eu
script_dir=$(CDPATH='' cd -- "$(dirname "$0")" && pwd)
repo_root=$(CDPATH='' cd -- "$script_dir/../.." && pwd)
check=$script_dir/check-desktop-rulesets.sh
policy=$repo_root/release/repository
work=$(mktemp -d "${TMPDIR:-/tmp}/hikyo-desktop-rulesets.XXXXXX")
trap 'rm -rf "$work"' EXIT HUP INT TERM
fail() { printf 'desktop rulesets fixture failed: %s\n' "$*" >&2; exit 1; }

# Each file equals its v* sibling except the name and the ref pattern.
for pair in creation:release-tag-creation immutability:release-tag-immutability; do
	desktop=$policy/desktop-tag-${pair%%:*}.json sibling=$policy/${pair##*:}.json
	jq -n --slurpfile d "$desktop" --slurpfile s "$sibling" '
		def norm: del(.name) | .conditions.ref_name = {};
		($d[0] | norm) == ($s[0] | norm) and
		$d[0].conditions.ref_name == {include: ["refs/tags/desktop-v*"], exclude: []}' |
		grep -x true >/dev/null || fail "${pair%%:*} differs from its v* sibling beyond name and refs"
done
grep -F 'desktop-tag-creation.json' "$script_dir/configure-repository.sh" >/dev/null || fail 'configure-repository.sh does not upsert creation'
grep -F 'desktop-tag-immutability.json' "$script_dir/configure-repository.sh" >/dev/null || fail 'configure-repository.sh does not upsert immutability'
grep -F 'check-desktop-rulesets.sh' "$script_dir/configure-repository.sh" >/dev/null || fail 'configure-repository.sh does not read the rulesets back'

# Fake gh: the list comes from $work/list.json, each ruleset from $work/id-<id>.json.
cat >"$work/gh" <<'GH'
#!/bin/sh
case "$2" in
	repos/example/repo/rulesets\?*) cat "$FAKE_DIR/list.json" ;;
	repos/example/repo/rulesets/*) cat "$FAKE_DIR/id-${2##*/}.json" ;;
	*) exit 1 ;;
esac
GH
chmod +x "$work/gh"
GH_BIN=$work/gh FAKE_DIR=$work
export GH_BIN FAKE_DIR

reset() {
	jq -c '{id: 11} + .' "$policy/desktop-tag-creation.json" >"$work/id-11.json"
	jq -c '{id: 12} + .' "$policy/desktop-tag-immutability.json" >"$work/id-12.json"
	jq -s '[.[] | {id, name, enforcement}]' "$work/id-11.json" "$work/id-12.json" >"$work/list.json"
}
expect_ok() { reset; "$check" example/repo >/dev/null 2>&1 || fail "$1 refused"; }
expect_refused() {
	label=$1 file=$2 filter=$3
	reset
	jq -c "$filter" "$work/$file" >"$work/mutated" && mv "$work/mutated" "$work/$file"
	[ "$file" = list.json ] || jq -s '[.[] | {id, name, enforcement}]' "$work/id-11.json" "$work/id-12.json" >"$work/list.json"
	if "$check" example/repo >/dev/null 2>&1; then fail "$label accepted"; fi
}
expect_ok 'exact policy'
expect_refused 'missing creation' list.json 'map(select(.id != 11))'
expect_refused 'duplicate immutability' list.json '. + [.[1]]'
expect_refused 'disabled creation' id-11.json '.enforcement = "disabled"'
expect_refused 'evaluate-only immutability' id-12.json '.enforcement = "evaluate"'
expect_refused 'creation with extra bypass' id-11.json '.bypass_actors += [{actor_id: 1, actor_type: "RepositoryRole", bypass_mode: "always"}]'
expect_refused 'immutability with bypass' id-12.json '.bypass_actors = [{actor_id: 5, actor_type: "RepositoryRole", bypass_mode: "always"}]'
expect_refused 'immutability without deletion' id-12.json '.rules = [{type: "update"}]'
expect_refused 'creation on other refs' id-11.json '.conditions.ref_name.include = ["refs/tags/v*"]'
expect_refused 'immutability with exclude' id-12.json '.conditions.ref_name.exclude = ["refs/tags/desktop-v1*"]'
printf 'desktop rulesets fixture: policy files mirror v* rulesets and weaker live rulesets are refused\n'
