#!/bin/sh
# Explicit operator activation after this trusted implementation lands on main.
# Main/tag/release policy and existing dev history are never rewritten here.
set -eu
: "${GH_BIN:=gh}"
[ "$#" -eq 1 ] || { printf 'usage: %s OWNER/REPO\n' "$0" >&2; exit 2; }
repository=$1
printf '%s\n' "$repository" | grep -Eq '^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$' || {
	printf 'dev activation: invalid repository\n' >&2; exit 2;
}
repo_root=$(CDPATH='' cd -- "$(dirname "$0")/../.." && pwd)
cd "$repo_root"
work=$(mktemp -d "${TMPDIR:-/tmp}/hikyo-dev-policy.XXXXXX")
trap 'rm -rf "$work"' EXIT HUP INT TERM
check="$repo_root/scripts/release/check-dev-policy.sh"
desired="$repo_root/release/repository/dev-ci-gate.json"
"$check" "$desired"

fail() { printf 'dev activation: %s\n' "$1" >&2; exit 1; }
read_ref() {
	branch=$1
	output=$2
	$GH_BIN api "repos/$repository/git/ref/heads/$branch" >"$output"
	jq -ser --arg ref "refs/heads/$branch" '
    if length != 1 then error("invalid ref stream") else .[0] end |
    select(.ref == $ref and .object.type == "commit") | .object.sha |
    select(type == "string" and test("^[0-9a-f]{40}$"))
  ' "$output"
}
same_policy() {
	jq -se --slurpfile expected "$1" '
    def writable: {name,target,enforcement,bypass_actors,conditions,rules} |
      (.rules[] | select(.type == "required_status_checks").parameters.required_status_checks[]) |=
        (if .integration_id == null then del(.integration_id) else . end) |
      .rules |= sort_by(.type);
    if length != 1 or ($expected | length) != 1 then error("invalid protection stream")
    else .[0] | writable == ($expected[0] | writable) end
  ' "$2" >/dev/null
}
read_policy() {
	$GH_BIN api "repos/$repository/rulesets/$id" >"$1"
	jq -se --argjson id "$id" 'length == 1 and (.[0] | .id == $id)' "$1" >/dev/null ||
		fail 'protection read returned a different owning ruleset'
}

$GH_BIN api "repos/$repository" >"$work/repository.json"
jq -se --arg repository "$repository" '
  length == 1 and (.[0] | .full_name == $repository and .default_branch == "main" and
    (.id | type == "number" and . > 0 and floor == .))
' "$work/repository.json" >/dev/null || fail 'main must remain the authenticated default branch'
main_sha=$(read_ref main "$work/main.json")
$GH_BIN api "repos/$repository/commits/$main_sha" >"$work/commit.json"
jq -se --arg sha "$main_sha" 'length == 1 and (.[0] | .sha == $sha and .commit.verification.verified == true)' \
	"$work/commit.json" >/dev/null || fail 'main commit is not cryptographically verified by GitHub'
git -c credential.helper= fetch --quiet --no-tags --no-recurse-submodules \
	"https://github.com/$repository.git" "$main_sha"
set -- .github/workflows/ci-dev-gate.yml .github/workflows/ci-dev.yml \
	.github/workflows/ci.yml .github/workflows/floor-bench.yml \
	.github/workflows/ci-feedback.yml scripts/ci/pr-gate scripts/ci/source-proof \
	scripts/ci/discover-dev-prs.sh scripts/ci/build-queue-publisher.sh \
	scripts/ci/check-fork-validation.sh scripts/ci/check-dco.sh \
	scripts/release/configure-dev.sh scripts/release/check-dev-policy.sh \
	scripts/release/require-green-main.sh \
	release/repository/dev-ci-gate.json
[ -z "$(git ls-files --others --exclude-standard -- "$@")" ] || fail 'untracked activation inputs are not trusted main code'
git diff --quiet "$main_sha" -- "$@" || fail 'activation inputs differ from installed trusted main'
# A file missing on both sides cannot prove the new implementation is installed.
for required in .github/workflows/ci-dev-gate.yml .github/workflows/ci-dev.yml scripts/ci/pr-gate/dev.go release/repository/dev-ci-gate.json; do
	git cat-file -e "$main_sha:$required" || fail 'new development workflow has not landed on main'
done
"$repo_root/scripts/release/require-green-main.sh" "$repository" "$main_sha"

printf '[]\n' >"$work/list.json"
page=1
while :; do
	$GH_BIN api "repos/$repository/rulesets?per_page=100&page=$page" >"$work/page.json"
	count=$(jq -ser '
    if length != 1 or (.[0] | type) != "array" then error("invalid ruleset page") else .[0] end |
    if length > 100 or any(.[]; type != "object" or (.name | type) != "string" or
      (.id | type) != "number" or .id <= 0 or .id != (.id | floor))
    then error("invalid ruleset identity") else length end
  ' "$work/page.json")
	jq -s '.[0] + .[1]' "$work/list.json" "$work/page.json" >"$work/combined.json"
	mv "$work/combined.json" "$work/list.json"
	[ "$count" -eq 100 ] || break
	[ "$page" -lt 20 ] || fail 'incomplete ruleset discovery'
	page=$((page + 1))
done
jq -e '(map(.id) | unique | length) == length' "$work/list.json" >/dev/null || fail 'duplicate ruleset identities'
id=$(jq -er '[.[] | select(.name == "dev requires trusted fast feedback")] |
  if length == 0 then "missing" elif length == 1 then .[0].id else error("ambiguous dev ruleset") end' "$work/list.json")
missing=false
if $GH_BIN api "repos/$repository/git/ref/heads/dev" >"$work/dev.json" 2>"$work/dev.error"; then
	dev_sha=$(jq -ser 'if length != 1 then error("invalid dev ref stream") else .[0] end |
    select(.ref == "refs/heads/dev" and .object.type == "commit") | .object.sha |
    select(type == "string" and test("^[0-9a-f]{40}$"))' "$work/dev.json")
elif grep -F '(HTTP 404)' "$work/dev.error" >/dev/null; then
	missing=true
	dev_sha=$main_sha
else
	cat "$work/dev.error" >&2
	fail 'cannot authenticate current dev ref'
fi

if [ "$id" = missing ]; then
	[ "$dev_sha" = "$main_sha" ] || fail 'refusing to adopt unprotected dev history that differs from main'
	cp "$desired" "$work/final.json"
	if [ "$missing" = true ]; then
		# Protect the name before creating it. The only temporary exception is
		# initial status-check enforcement; PR admission rejects this state.
		jq '(.rules[] | select(.type == "required_status_checks").parameters.do_not_enforce_on_create) = true' \
			"$desired" >"$work/proposed.json"
		"$check" --bootstrap "$work/proposed.json"
	else
		cp "$desired" "$work/proposed.json"
	fi
	[ "$(read_ref main "$work/main-final.json")" = "$main_sha" ] || fail 'main moved before protection installation'
	$GH_BIN api --method POST "repos/$repository/rulesets" --input "$work/proposed.json" >"$work/created.json"
	id=$(jq -ser 'if length != 1 then error("invalid creation response") else .[0].id end |
    select(type == "number" and . > 0 and floor == .)' "$work/created.json")
	read_policy "$work/installed.json"
	same_policy "$work/proposed.json" "$work/installed.json" || fail 'initial dev protection readback differs'
else
	read_policy "$work/before.json"
	bootstrap_state=false
	if ! "$check" "$work/before.json"; then
		"$check" --bootstrap "$work/before.json"
		bootstrap_state=true
		[ "$dev_sha" = "$main_sha" ] || fail 'bootstrap recovery requires unchanged verified main history'
	fi
	[ "$missing" = false ] || [ "$bootstrap_state" = true ] ||
		fail 'existing finalized dev protection lost its branch; explicit recovery is required'
	# Retain stricter review parameters, concrete App binding and unrelated
	# rules. Only the bootstrap creation flag is finalized.
	jq '{name,target,enforcement,bypass_actors,conditions,rules} |
    (.rules[] | select(.type == "required_status_checks").parameters.do_not_enforce_on_create) = false' \
		"$work/before.json" >"$work/final.json"
fi

if [ "$missing" = true ]; then
	[ "$(read_ref main "$work/main-create.json")" = "$main_sha" ] || fail 'main moved before dev creation'
	jq -n --arg sha "$main_sha" '{ref:"refs/heads/dev",sha:$sha}' >"$work/ref.json"
	$GH_BIN api --method POST "repos/$repository/git/refs" --input "$work/ref.json" >"$work/created-ref.json"
	[ "$(read_ref dev "$work/dev-created.json")" = "$main_sha" ] || fail 'created dev ref differs from verified main'
fi

"$check" "$work/final.json"
$GH_BIN api --method PUT "repos/$repository/rulesets/$id" --input "$work/final.json" >/dev/null
read_policy "$work/after.json"
"$check" "$work/after.json"
same_policy "$work/final.json" "$work/after.json" || fail 'final dev protection differs from submitted policy'
[ "$(read_ref dev "$work/dev-after.json")" = "$dev_sha" ] || fail 'dev moved during activation; no history was overwritten'
$GH_BIN api "repos/$repository/rules/branches/dev" >"$work/effective.json"
# Inherited checks cannot silently introduce a full queue or mask this context.
jq -n --slurpfile policy "$work/after.json" --slurpfile effective "$work/effective.json" '
  if ($effective | length) != 1 or ($effective[0] | type) != "array" then error("invalid effective protections")
  else $policy[0] | .rules = $effective[0] end
' >"$work/effective-policy.json"
"$check" "$work/effective-policy.json"
printf 'dev activation: exact trusted workflow, verified initial source and effective fast-only protection confirmed\n'
