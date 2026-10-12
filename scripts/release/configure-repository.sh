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

upsert_ruleset() {
	payload=$1
	name=$(jq -r '.name' "$payload")
	id=$($GH_BIN api "repos/$repository/rulesets" --jq ".[] | select(.name == \"$name\") | .id")
	if [ -z "$id" ] && [ "$name" = 'release tags require admin role' ]; then
		id=$($GH_BIN api "repos/$repository/rulesets" \
			--jq '.[] | select(.name == "release tags require maintainer role") | .id')
	elif [ -z "$id" ] && [ "$name" = 'nightly tags require Hikyo release app' ]; then
		id=$($GH_BIN api "repos/$repository/rulesets" \
			--jq '.[] | select(.name == "nightly tags require GitHub Actions") | .id')
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
	printf '[]\n' >"$main_tmp/list.json"
	page=1
	while :; do
		$GH_BIN api "repos/$repository/rulesets?per_page=100&page=$page" >"$main_tmp/page.json"
		page_count=$(jq -ser '
			if length != 1 or (.[0] | type) != "array" then error("invalid ruleset page") else .[0] end |
			if length > 100 or any(.[]; type != "object" or (.name | type) != "string" or
				(.id | type) != "number" or .id <= 0 or (.id | floor) != .id)
			then error("invalid ruleset entry") else length end' "$main_tmp/page.json")
		jq -s '.[0] + .[1]' "$main_tmp/list.json" "$main_tmp/page.json" >"$main_tmp/combined.json"
		mv "$main_tmp/combined.json" "$main_tmp/list.json"
		[ "$page_count" -eq 100 ] || break
		[ "$page" -lt 20 ] || {
			printf 'repository policy: incomplete ruleset discovery after 20 pages\n' >&2
			exit 1
		}
		page=$((page + 1))
	done
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
		if ! "$repo_root/scripts/release/check-main-policy.sh" "$main_tmp/before.json" 2>/dev/null; then
			"$repo_root/scripts/release/check-main-policy.sh" --legacy "$main_tmp/before.json"
		fi
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

upsert_ruleset "$repo_root/release/repository/release-tag-immutability.json"
upsert_ruleset "$repo_root/release/repository/release-tag-creation.json"
upsert_ruleset "$repo_root/release/repository/nightly-tag-creation.json"
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

rulesets=$($GH_BIN api "repos/$repository/rulesets")
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
printf '%s\n' "$creation" | jq -e '
	.bypass_actors == [{
		actor_id: 5,
		actor_type: "RepositoryRole",
		bypass_mode: "always"
	}] and
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
