#!/usr/bin/env bash
# Starts nightly-build.yml on the source's own branch, waits for it, and exports
# the authenticated result as this job's outputs. A build on dev cannot touch
# main-scoped caches; only this trusted side continues to signing and publication.
set -euo pipefail
fail() { printf 'nightly build: %s\n' "$*" >&2; exit 1; }
: "${GH_TOKEN:?}" "${GITHUB_REPOSITORY:?}" "${GITHUB_OUTPUT:?}" "${GITHUB_RUN_ID:?}" "${GITHUB_RUN_ATTEMPT:?}" "${GITHUB_RUN_NUMBER:?}"
: "${SOURCE:?}" "${SOURCE_MODE:?}" "${RECEIPT_ARTIFACT_ID:?}" "${ALLOW_MISSING_PREDECESSOR:?}" "${RUNNER_TEMP:?}"
GH_BIN=${GH_BIN:-gh}
repo=$GITHUB_REPOSITORY
workflow=.github/workflows/nightly-build.yml

case "$SOURCE_MODE" in main | dev) ;; *) fail 'invalid source mode' ;; esac
[[ "$SOURCE" =~ ^[0-9a-f]{40}$ ]] || fail 'invalid source commit'
[[ "$RECEIPT_ARTIFACT_ID" =~ ^[1-9][0-9]*$ ]] || fail 'invalid receipt artifact'

build_run=$($GH_BIN api --method POST "repos/$repo/actions/workflows/nightly-build.yml/dispatches" \
	-f "ref=$SOURCE_MODE" \
	-f "inputs[commit]=$SOURCE" \
	-f "inputs[publisher_run_id]=$GITHUB_RUN_ID" \
	-f "inputs[publisher_run_attempt]=$GITHUB_RUN_ATTEMPT" \
	-f "inputs[publisher_run_number]=$GITHUB_RUN_NUMBER" \
	-f "inputs[receipt_artifact_id]=$RECEIPT_ARTIFACT_ID" \
	-f "inputs[allow_missing_predecessor]=$ALLOW_MISSING_PREDECESSOR" \
	--jq '.workflow_run_id')
[[ "$build_run" =~ ^[1-9][0-9]*$ ]] || fail 'dispatch returned no run'
printf 'nightly build: run %s on %s for %s\n' "$build_run" "$SOURCE_MODE" "$SOURCE"

# The result is judged below from the authenticated run, not from this exit code.
$GH_BIN run watch "$build_run" --repo "$repo" --interval 30 >/dev/null || true

run=$($GH_BIN api "repos/$repo/actions/runs/$build_run")
jq -e --arg repo "$repo" --arg path "$workflow" --arg branch "$SOURCE_MODE" '
	.status == "completed" and .conclusion == "success" and .path == $path and
	.event == "workflow_dispatch" and .head_branch == $branch and
	.repository.full_name == $repo and .head_repository.full_name == $repo
' <<<"$run" >/dev/null || fail "run $build_run is not a successful build on $SOURCE_MODE"

# The definition that ran must be the one this trusted checkout approves.
head_sha=$(jq -er '.head_sha' <<<"$run")
ran=$($GH_BIN api "repos/$repo/contents/$workflow?ref=$head_sha" --jq '.sha')
[ "$ran" = "$(git hash-object "$workflow")" ] || fail 'build definition differs from approved main'

mkdir -p "$RUNNER_TEMP/nightly-build-identity"
$GH_BIN run download "$build_run" --repo "$repo" --name nightly-build-identity --dir "$RUNNER_TEMP/nightly-build-identity"
identity=$RUNNER_TEMP/nightly-build-identity/nightly-build-identity.json
[ -f "$identity" ] && [ ! -L "$identity" ] || fail 'missing regular build identity'

# Parse the known shape: dev code wrote this file, and it becomes job outputs.
jq -e --arg source "$SOURCE" '
	def sha256: test("^[0-9a-f]{64}$");
	type == "object" and (keys == ["artifact_id","commit","compatibility_sha256","non_darwin_sha256","skip","tag","tag_exists","version"]) and
	all(.[]; type == "string" and (test("[\\r\\n]") | not)) and .commit == $source and
	(.tag | test("^v[0-9A-Za-z.+-]+$")) and .tag == "v" + .version and
	(.skip == "true" or (
		.skip == "false" and (.tag_exists == "true" or .tag_exists == "false") and
		(.artifact_id | test("^[1-9][0-9]*$")) and (.non_darwin_sha256 | sha256) and (.compatibility_sha256 | sha256)
	))
' "$identity" >/dev/null || fail 'invalid build identity'

{
	jq -r 'to_entries[] | "\(.key)=\(.value)"' "$identity"
	printf 'build_run_id=%s\n' "$build_run"
} >>"$GITHUB_OUTPUT"
