#!/bin/sh
# Reads the repository's rulesets back (all pages) and fails unless every stable
# and nightly release policy is exactly what configure-repository.sh installs.
#
# usage: verify-repository-rulesets.sh OWNER/REPO [STABLE_RELEASE_APP_ID]
#   An empty or absent app id asserts the rolled-back state: admin-only tag
#   creation and no trust-branch policy.
set -eu

: "${GH_BIN:=gh}"
[ "$#" -ge 1 ] && [ "$#" -le 2 ] || { printf 'usage: %s OWNER/REPO [STABLE_RELEASE_APP_ID]\n' "$0" >&2; exit 2; }
repository=$1
stable_app_id=${2:-}
if [ -n "$stable_app_id" ]; then
	printf '%s\n' "$stable_app_id" | grep -Eq '^[1-9][0-9]*$' || exit 2
fi
repo_root=$(CDPATH='' cd -- "$(dirname "$0")/../.." && pwd)
# shellcheck disable=SC1091
. "$repo_root/scripts/lib/release.sh"
trust_ruleset='stable trust sync branches require stable-release app'
verify_tmp=$(mktemp -d "${TMPDIR:-/tmp}/hikyo-policy-verify.XXXXXX")
trap 'rm -rf "$verify_tmp"' EXIT HUP INT TERM
github_rulesets "$repository" "$verify_tmp/rulesets.json" || exit 1
rulesets=$(cat "$verify_tmp/rulesets.json")
rm -rf "$verify_tmp"
trap - EXIT HUP INT TERM
printf '%s\n' "$rulesets" | jq -e '
	[.[] | select(.name == "release tags are immutable" and .enforcement == "active")] | length == 1
' >/dev/null
printf '%s\n' "$rulesets" | jq -e '
	[.[] | select(.name == "release tags require admin role" and .enforcement == "active")] | length == 1
' >/dev/null
printf '%s\n' "$rulesets" | jq -e '
	[.[] | select(.name == "nightly tags require Hikyo release app" and .enforcement == "active")] | length == 1
' >/dev/null
printf '%s\n' "$rulesets" | jq -e '
	[.[] | select(.name == "nightly promotion branches require Hikyo release app" and .enforcement == "active")] | length == 1
' >/dev/null
printf '%s\n' "$rulesets" | jq -e '
	[.[] | select(.name == "main requires PR and release CI" and .enforcement == "active")] | length == 1
' >/dev/null
immutability_id=$(printf '%s\n' "$rulesets" | jq -r '.[] | select(.name == "release tags are immutable") | .id')
immutability=$($GH_BIN api "repos/$repository/rulesets/$immutability_id")
printf '%s\n' "$immutability" | jq -e '
	(.bypass_actors | length) == 0 and
	([.rules[] | select(.type == "update")] | length) == 1 and
	([.rules[] | select(.type == "deletion")] | length) == 1 and
	.conditions.ref_name.include == ["refs/tags/v*"]
' >/dev/null
creation_id=$(printf '%s\n' "$rulesets" | jq -r '.[] | select(.name == "release tags require admin role") | .id')
creation=$($GH_BIN api "repos/$repository/rulesets/$creation_id")
# GitHub expresses "only repository admins may create this ref" as an admin-role
# bypass on a creation-block rule. Update/deletion remain in the separate
# zero-bypass immutability ruleset asserted above.
# The human release path trusts that only an admin can create a v* tag as a User, so
# this policy is what every "a person pushed this tag" decision rests on.
printf '%s\n' "$creation" | jq -e --arg app "$stable_app_id" '
	.target == "tag" and
	([.bypass_actors[] | select(.actor_type == "RepositoryRole")] == [{
		actor_id: 5,
		actor_type: "RepositoryRole",
		bypass_mode: "always"
	}]) and
	([.bypass_actors[] | select(.actor_type != "RepositoryRole")] ==
		(if $app == "" then [] else [{actor_id: ($app | tonumber), actor_type: "Integration", bypass_mode: "always"}] end)) and
	([.rules[] | select(.type == "creation")] | length) == 1 and
	.conditions.ref_name.include == ["refs/tags/v*"] and
	.conditions.ref_name.exclude == ["refs/tags/v*-nightly.*"]
' >/dev/null
nightly_id=$(printf '%s\n' "$rulesets" | jq -r '.[] | select(.name == "nightly tags require Hikyo release app") | .id')
nightly=$($GH_BIN api "repos/$repository/rulesets/$nightly_id")
printf '%s\n' "$nightly" | jq -e '
	.bypass_actors == [{
		actor_id: 4700019,
		actor_type: "Integration",
		bypass_mode: "always"
	}] and
	([.rules[] | select(.type == "creation")] | length) == 1 and
	.conditions.ref_name.include == ["refs/tags/v*-nightly.*"] and
	.conditions.ref_name.exclude == []
' >/dev/null
promotion_id=$(printf '%s\n' "$rulesets" | jq -r '.[] | select(.name == "nightly promotion branches require Hikyo release app") | .id')
promotion=$($GH_BIN api "repos/$repository/rulesets/$promotion_id")
printf '%s\n' "$promotion" | jq -e '
	.bypass_actors == [{
		actor_id: 4700019,
		actor_type: "Integration",
		bypass_mode: "always"
	}] and
	([.rules[].type] | sort) == ["creation", "update"] and
	.conditions.ref_name.include == ["refs/heads/release/nightly-*"] and
	.conditions.ref_name.exclude == []
' >/dev/null
if [ -n "$stable_app_id" ]; then
	printf '%s\n' "$rulesets" | jq -e '
		[.[] | select(.name == "stable trust sync branches require stable-release app" and .enforcement == "active")] | length == 1
	' >/dev/null
	trust_id=$(printf '%s\n' "$rulesets" | jq -r '.[] | select(.name == "stable trust sync branches require stable-release app") | .id')
	# Only the app and the repository admin role may touch these branches.
	$GH_BIN api "repos/$repository/rulesets/$trust_id" | jq -e --argjson app "$stable_app_id" '
		def by_actor: sort_by(.actor_type, .actor_id);
		(.bypass_actors | by_actor) == ([
			{actor_id: 5, actor_type: "RepositoryRole", bypass_mode: "always"},
			{actor_id: $app, actor_type: "Integration", bypass_mode: "always"}] | by_actor) and
		([.rules[].type] | sort) == ["creation", "update"] and
		.conditions.ref_name.include == ["refs/heads/release/sync-trust-*"] and
		.conditions.ref_name.exclude == []
	' >/dev/null
else
	printf '%s\n' "$rulesets" | jq -e --arg name "$trust_ruleset" '[.[] | select(.name == $name)] | length == 0' >/dev/null
fi
printf 'repository policy: release rulesets verified\n'
