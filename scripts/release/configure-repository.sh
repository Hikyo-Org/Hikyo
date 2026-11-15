#!/bin/sh
set -eu

: "${GH_BIN:=gh}"

main_only=false
if [ "${1:-}" = --main-only ]; then
	main_only=true
	shift
fi

if [ "$#" -ne 1 ]; then
	printf 'usage: %s [--main-only] OWNER/REPO\n' "$0" >&2
	exit 2
fi

repository=$1
repo_root=$(CDPATH='' cd -- "$(dirname "$0")/../.." && pwd)

# The unattended stable release uses its own GitHub App. Its numeric id only
# exists after the owner creates it, so it comes from the environment and is
# injected into the stable policies. Unset turns unattended releases off again:
# the tag policy returns to admin-only and the app-only trust branch policy is
# deleted, so the human ceremony can push release/sync-trust-* branches. Export
# the id on every run; leaving it out is the rollback.
stable_app_id=${HIKYO_STABLE_RELEASE_APP_ID:-}
if [ -n "$stable_app_id" ]; then
	printf '%s\n' "$stable_app_id" | grep -Eq '^[1-9][0-9]*$' || {
		printf 'repository policy: HIKYO_STABLE_RELEASE_APP_ID must be a numeric GitHub App id\n' >&2
		exit 2
	}
fi

# list_rulesets FILE: every ruleset of the repository, all pages, as one JSON array.
# A single unpaginated read returns only the first 30, so a policy on a later page
# would look absent and be created twice, or look deleted when it is not.
list_rulesets() {
	printf '[]\n' >"$1"
	page=1
	while :; do
		$GH_BIN api "repos/$repository/rulesets?per_page=100&page=$page" >"$1.page"
		page_count=$(jq -ser '
			if length != 1 or (.[0] | type) != "array" then error("invalid ruleset page") else .[0] end |
			if length > 100 or any(.[]; type != "object" or (.name | type) != "string" or
				(.id | type) != "number" or .id <= 0 or (.id | floor) != .id)
			then error("invalid ruleset entry") else length end' "$1.page")
		jq -s '.[0] + .[1]' "$1" "$1.page" >"$1.next"
		mv "$1.next" "$1"
		rm -f "$1.page"
		[ "$page_count" -eq 100 ] || break
		[ "$page" -lt 20 ] || {
			printf 'repository policy: incomplete ruleset discovery after 20 pages\n' >&2
			exit 1
		}
		page=$((page + 1))
	done
}
# ruleset_id LIST NAME: the id of the one ruleset with that name, or nothing; two is an error.
ruleset_id() {
	jq -er --arg name "$2" '[.[] | select(.name == $name) | .id] |
		if length > 1 then error("ambiguous ruleset name") else (.[0] // "") end' "$1"
}

upsert_ruleset() {
	payload=$1
	name=$(jq -r '.name' "$payload")
	list_rulesets "$policy_tmp/rulesets.json"
	id=$(ruleset_id "$policy_tmp/rulesets.json" "$name")
	if [ -z "$id" ] && [ "$name" = 'release tags require admin role' ]; then
		id=$(ruleset_id "$policy_tmp/rulesets.json" 'release tags require maintainer role')
	elif [ -z "$id" ] && [ "$name" = 'nightly tags require Hikyo release app' ]; then
		id=$(ruleset_id "$policy_tmp/rulesets.json" 'nightly tags require GitHub Actions')
	fi
	if [ -n "$id" ]; then
		$GH_BIN api --method PUT "repos/$repository/rulesets/$id" --input "$payload" >/dev/null
	else
		$GH_BIN api --method POST "repos/$repository/rulesets" --input "$payload" >/dev/null
	fi
}

configure_main() {
	policy="$repo_root/release/repository/main-ci-gate.json"
	"$repo_root/scripts/release/check-main-policy.sh" "$policy"
	main_tmp=$(mktemp -d "${TMPDIR:-/tmp}/hikyo-main-policy.XXXXXX")
	trap 'rm -rf "$main_tmp"' EXIT HUP INT TERM
	list_rulesets "$main_tmp/list.json"
	count=$(jq -ser 'if length != 1 or (.[0] | type) != "array" then error("invalid ruleset list") else .[0] end | [.[] | select(.name == "main requires PR and release CI")] | length' "$main_tmp/list.json")
	if [ "$count" = 0 ] && [ "$main_only" = false ]; then
		# Preserve initial repository setup; the narrow migration never creates
		# a second ruleset or changes any unrelated repository setting.
		cp "$policy" "$main_tmp/proposed.json"
		$GH_BIN api --method POST "repos/$repository/rulesets" --input "$main_tmp/proposed.json" >"$main_tmp/created.json"
		id=$(jq -er '.id | select(type == "number")' "$main_tmp/created.json")
	else
		id=$(jq -er '[.[] | select(.name == "main requires PR and release CI")] |
			if length == 1 then .[0].id | select(type == "number" and . > 0 and floor == .) else error("main ruleset must exist uniquely") end' "$main_tmp/list.json")
		$GH_BIN api "repos/$repository/rulesets/$id" >"$main_tmp/before.json"
		# Preserve all unrelated live parameters, rules, conditions and actor policy.
		# Refuse a weakened/redacted source rather than overwriting it with defaults.
		"$repo_root/scripts/release/check-main-policy.sh" "$main_tmp/before.json"
		jq --slurpfile desired "$policy" '
			(.rules[] | select(.type == "required_status_checks").parameters.required_status_checks[0]) as $existing |
			{name,target,enforcement,bypass_actors,conditions,rules} |
			.rules = ([.rules[] | select(.type != "required_status_checks" and .type != "merge_queue")] +
				[$desired[0].rules[] | select(.type == "required_status_checks" or .type == "merge_queue") |
					if .type == "required_status_checks" then
						.parameters.required_status_checks[0] = ($existing | .context = "ci-queue-required")
					else . end])
		' "$main_tmp/before.json" >"$main_tmp/proposed.json"
		"$repo_root/scripts/release/check-main-policy.sh" "$main_tmp/proposed.json"
		$GH_BIN api --method PUT "repos/$repository/rulesets/$id" --input "$main_tmp/proposed.json" >/dev/null
	fi
	"$repo_root/scripts/release/check-main-policy.sh" "$main_tmp/proposed.json"
	$GH_BIN api "repos/$repository/rulesets/$id" >"$main_tmp/after.json"
	"$repo_root/scripts/release/check-main-policy.sh" "$main_tmp/after.json"
	jq -e --slurpfile expected "$main_tmp/proposed.json" '
		# GitHub may represent an unspecified integration as absent or null.
		# Every concrete binding (including explicit zero) must remain exact.
		def writable: {name,target,enforcement,bypass_actors,conditions,rules} |
			(.rules[] | select(.type == "required_status_checks").parameters.required_status_checks[]) |=
				(if .integration_id == null then del(.integration_id) else . end) |
			.rules |= sort_by(.type);
		writable == ($expected[0] | writable)
	' "$main_tmp/after.json" >/dev/null || {
		printf 'repository policy: main readback differs from submitted protection\n' >&2
		exit 1
	}
	rm -rf "$main_tmp"
	trap - EXIT HUP INT TERM
	printf 'repository policy: mandatory queue main policy verified\n'
}

if [ "$main_only" = true ]; then
	configure_main
	exit 0
fi

policy_tmp=$(mktemp -d "${TMPDIR:-/tmp}/hikyo-stable-policy.XXXXXX")
trap 'rm -rf "$policy_tmp"' EXIT HUP INT TERM
stable_tmp=$policy_tmp

# The app may bypass the stable tag and trust-branch policies only once the stable
# line itself judges a tag by who pushed it. A stable release older than that
# change still trusts an unsigned tag from anyone able to create one, so granting
# the bypass first would let a stolen app key mint a tag the shipped workflow
# accepts. The marker is read from the highest stable tag's own commit.
require_actor_based_tag_authority() {
	list="$policy_tmp/stable-tags.json"
	$GH_BIN api --paginate --slurp "repos/$repository/git/matching-refs/tags/v" >"$list"
	tag=$(jq -er '
		def key: capture("^(?<a>[0-9]+)\\.(?<b>[0-9]+)\\.(?<c>[0-9]+)$") | [.a, .b, .c | tonumber];
		[.[][] | .ref | ltrimstr("refs/tags/") | ltrimstr("v") |
			select(test("^[0-9]+\\.[0-9]+\\.[0-9]+$"))] | sort_by(key) | last | select(. != null)' "$list") || {
		printf 'repository policy: no stable release tag exists, so the stable-release app cannot be granted tag access; release a stable version with the ceremony first\n' >&2
		exit 1
	}
	commit=$($GH_BIN api "repos/$repository/commits/v$tag" --jq '.sha')
	printf '%s\n' "$commit" | grep -Eq '^[0-9a-f]{40}$' || {
		printf 'repository policy: cannot resolve the commit of v%s\n' "$tag" >&2
		exit 1
	}
	$GH_BIN api "repos/$repository/contents/scripts/release/require-signed-tag.sh?ref=$commit" --jq '.content' >"$policy_tmp/tag-authority.b64"
	base64 -d <"$policy_tmp/tag-authority.b64" >"$policy_tmp/tag-authority.sh" 2>/dev/null || true
	if ! grep -F 'triggering_actor' "$policy_tmp/tag-authority.sh" >/dev/null || ! grep -F 'release_run_jq' "$policy_tmp/tag-authority.sh" >/dev/null; then
		printf 'repository policy: the latest stable tag v%s predates the actor-based tag authority; release a stable version that contains it with the ceremony first, then grant the stable-release app\n' "$tag" >&2
		exit 1
	fi
}
if [ -n "$stable_app_id" ]; then
	require_actor_based_tag_authority
fi

upsert_ruleset "$repo_root/release/repository/release-tag-immutability.json"
cp "$repo_root/release/repository/release-tag-creation.json" "$stable_tmp/tags.json"
if [ -n "$stable_app_id" ]; then
	# The app may create the annotated stable tag; humans keep the admin role.
	jq --argjson id "$stable_app_id" \
		'.bypass_actors += [{actor_id: $id, actor_type: "Integration", bypass_mode: "always"}]' \
		"$repo_root/release/repository/release-tag-creation.json" >"$stable_tmp/tags.json"
	# Repository admins keep their role bypass beside the app, so the ceremony can
	# still push release/sync-trust-* when the app is unavailable.
	jq --argjson id "$stable_app_id" \
		'.bypass_actors += [{actor_id: $id, actor_type: "Integration", bypass_mode: "always"}]' \
		"$repo_root/release/repository/stable-trust-branches.json" >"$stable_tmp/trust.json"
fi
upsert_ruleset "$stable_tmp/tags.json"
upsert_ruleset "$repo_root/release/repository/nightly-tag-creation.json"
upsert_ruleset "$repo_root/release/repository/nightly-promotion-branches.json"
trust_ruleset='stable trust sync branches require stable-release app'
if [ -n "$stable_app_id" ]; then
	upsert_ruleset "$stable_tmp/trust.json"
else
	# An app-only policy left behind would block the human ceremony's sync-trust push.
	list_rulesets "$policy_tmp/rulesets.json"
	trust_id=$(ruleset_id "$policy_tmp/rulesets.json" "$trust_ruleset")
	if [ -n "$trust_id" ]; then
		$GH_BIN api --method DELETE "repos/$repository/rulesets/$trust_id" >/dev/null
	fi
	list_rulesets "$policy_tmp/rulesets.json"
	remaining=$(ruleset_id "$policy_tmp/rulesets.json" "$trust_ruleset")
	if [ -n "$remaining" ]; then
		printf 'repository policy: the stable trust branch policy is still present after rollback\n' >&2
		exit 1
	fi
fi
rm -rf "$policy_tmp"
trap - EXIT HUP INT TERM
configure_main

$GH_BIN api --method PUT "repos/$repository/immutable-releases" >/dev/null
# CodeQL runs as GitHub's default setup (a repository setting, not workflow
# YAML): Actions, Go and TypeScript surfaces, weekly and on every PR. Keep the
# setting explicit here instead of duplicating it under .github/workflows.
$GH_BIN api --method PATCH "repos/$repository/code-scanning/default-setup" \
	-f state=configured -f query_suite=default \
	-f 'languages[]=actions' -f 'languages[]=go' -f 'languages[]=javascript-typescript' >/dev/null
allowed_actions=$($GH_BIN api "repos/$repository/actions/permissions" --jq '.allowed_actions')
$GH_BIN api --method PUT "repos/$repository/actions/permissions" \
	-F enabled=true -f allowed_actions="$allowed_actions" -F sha_pinning_required=true >/dev/null

verify_tmp=$(mktemp -d "${TMPDIR:-/tmp}/hikyo-policy-verify.XXXXXX")
trap 'rm -rf "$verify_tmp"' EXIT HUP INT TERM
list_rulesets "$verify_tmp/rulesets.json"
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
printf '%s\n' "$creation" | jq -e --arg app "$stable_app_id" '
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
$GH_BIN api "repos/$repository/immutable-releases" --jq '.enabled' | grep -x true >/dev/null
$GH_BIN api "repos/$repository/actions/permissions" --jq '.sha_pinning_required' | grep -x true >/dev/null
$GH_BIN api "repos/$repository/code-scanning/default-setup" | jq -e '
	.state == "configured" and
	(["actions", "go", "javascript-typescript"] - .languages) == []
' >/dev/null

probe_tag=v-ruleset-probe
probe_error=$(mktemp "${TMPDIR:-/tmp}/hikyo-probe-tag-lookup.XXXXXX")
trap 'rm -f "$probe_error"' EXIT HUP INT TERM
if ! $GH_BIN api "repos/$repository/git/ref/tags/$probe_tag" >/dev/null 2>"$probe_error"; then
	grep -F '(HTTP 404)' "$probe_error" >/dev/null || {
		printf 'repository policy: cannot inspect probe tag\n' >&2
		exit 1
	}
	main_sha=$($GH_BIN api "repos/$repository/git/ref/heads/main" --jq '.object.sha')
	$GH_BIN api --method POST "repos/$repository/git/refs" \
		-f ref="refs/tags/$probe_tag" -f sha="$main_sha" >/dev/null
fi
git -C "$repo_root" fetch --quiet origin main "refs/tags/$probe_tag:refs/tags/$probe_tag"
probe_commit=$(git -C "$repo_root" rev-parse "$probe_tag^{commit}")
replacement=$(git -C "$repo_root" rev-parse 'origin/main^{commit}')
if [ "$replacement" = "$probe_commit" ]; then
	replacement=$(git -C "$repo_root" rev-parse 'origin/main^{commit}^')
fi
(
	cd "$repo_root"
	HIKYO_ALLOW_IMMUTABLE_TAG_PROBE=YES \
		"$repo_root/scripts/release/probe-tag-move.sh" "$repository" "$probe_tag" "$replacement"
)

printf 'repository policy: PR/CI main gate, immutable releases, protected stable/nightly tags, live move probe, SHA-pinned actions, CodeQL default setup active\n'
