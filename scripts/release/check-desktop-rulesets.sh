#!/bin/sh
# Read back both desktop tag rulesets from the live repository and refuse any
# that is absent, inactive or weaker than release/repository/desktop-tag-*.json.
set -eu
[ "$#" -eq 1 ] || { printf 'usage: %s OWNER/REPO\n' "$0" >&2; exit 2; }
repository=$1
: "${GH_BIN:=gh}"
fail() { printf 'desktop rulesets: %s\n' "$*" >&2; exit 1; }
rulesets=$("$GH_BIN" api "repos/$repository/rulesets?per_page=100") || fail 'cannot list rulesets'
fetch() {
	id=$(printf '%s\n' "$rulesets" | jq -er --arg name "$1" '
		[.[] | select(.name == $name and .enforcement == "active")] |
		if length == 1 then .[0].id | select(type == "number") else error("missing or duplicate") end') ||
		fail "active ruleset required exactly once: $1"
	"$GH_BIN" api "repos/$repository/rulesets/$id" || fail "cannot read ruleset $1"
}
creation=$(fetch 'desktop release tags require admin role')
printf '%s\n' "$creation" | jq -e '
	.target == "tag" and .enforcement == "active" and
	.bypass_actors == [{actor_id: 5, actor_type: "RepositoryRole", bypass_mode: "always"}] and
	([.rules[].type] == ["creation"]) and
	.conditions.ref_name.include == ["refs/tags/desktop-v*"] and
	.conditions.ref_name.exclude == []
' >/dev/null || fail 'creation ruleset is weaker than release/repository/desktop-tag-creation.json'
immutability=$(fetch 'desktop release tags are immutable')
printf '%s\n' "$immutability" | jq -e '
	.target == "tag" and .enforcement == "active" and
	(.bypass_actors | length) == 0 and
	([.rules[].type] | sort) == ["deletion", "update"] and
	.conditions.ref_name.include == ["refs/tags/desktop-v*"] and
	.conditions.ref_name.exclude == []
' >/dev/null || fail 'immutability ruleset is weaker than release/repository/desktop-tag-immutability.json'
printf 'desktop rulesets: creation and immutability rulesets active\n'
