#!/usr/bin/env bash
# Run only the immutable base's copy, alongside its classifier and registry.
# This first stage narrows docs/site changes only. Backend impact is advisory.
set -euo pipefail

tools_dir=$(CDPATH='' cd -- "$(dirname "$0")" && pwd)
classifier="$tools_dir/classify-changed-paths.sh"
registry=${CI_JOB_REGISTRY:-$tools_dir/ci-job-registry.json}
full=$(CI_JOB_REGISTRY="$registry" bash "$classifier" --all)
emit() {
	jq -cn --arg mode "$1" --arg reason "$2" --argjson plan "$3" \
		'{mode: $mode, reason: $reason, plan: $plan}'
}
fallback() { emit full "$1" "$full"; exit 0; }

[[ ${BASE_SHA:-} =~ ^[0-9a-f]{40}$ && ${SOURCE_REVISION:-} =~ ^[0-9a-f]{40}$ ]] ||
	fallback 'Missing immutable base or candidate identity.'
[[ ${GITHUB_REPOSITORY:-} =~ ^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$ ]] ||
	fallback 'Missing repository identity.'
[[ ${GITHUB_REF:-} =~ ^refs/heads/gh-readonly-queue/main/pr-([1-9][0-9]*)-([0-9a-f]{40})$ ]] ||
	fallback 'Unsupported queue topology.'
pr=${BASH_REMATCH[1]}
[[ ${BASH_REMATCH[2]} = "$BASE_SHA" ]] || fallback 'Queue branch base differs from the event base.'
parents=$(git show -s --format=%P "$SOURCE_REVISION")
[[ $parents =~ ^$BASE_SHA\ ([0-9a-f]{40})$ ]] || fallback 'Queue candidate is not a single PR on its event base.'
pr_head=${BASH_REMATCH[1]}

paths_file=$(mktemp)
trap 'rm -f "$paths_file"' EXIT
git diff --no-renames --name-only -z "$BASE_SHA" "$SOURCE_REVISION" >"$paths_file"
paths=()
while IFS= read -r -d '' path; do
	[[ $path != *$'\n'* && $path != *$'\r'* ]] || fallback 'Unsupported path encoding.'
	# File extensions alone are not an ownership boundary. Images or fixtures
	# outside the documentation tree can be embedded in the application.
	case "$path" in
		*/package.json | */*lock* | */pnpm-workspace.yaml | *.go | *.sql)
			fallback 'Dependencies or backend inputs require full validation.' ;;
		README.md | CONTRIBUTING.md | GOVERNANCE.md | SECURITY.md | SUPPORT.md | TRADEMARK.md | \
			docs/site/* | docs/assets/* | docs/*.md | docs/*.mdx | docs/*.txt | docs/*.html) ;;
		*) fallback 'Changes extend beyond the docs and landing-page boundary.' ;;
	esac
	entry=$(git ls-tree "$SOURCE_REVISION" -- "$path")
	[[ -z $entry || $entry = '100644 blob '* || $entry = '100755 blob '* ]] ||
		fallback 'Special file modes require full validation.'
	paths+=("$path")
done <"$paths_file"
[[ ${#paths[@]} -gt 0 ]] || fallback 'Empty change set requires full validation.'
plan=$(printf '%s\n' "${paths[@]}" | CI_JOB_REGISTRY="$registry" bash "$classifier" --files)
jq -e '.docs == true and all(to_entries[]; .value == (.key == "docs"))' <<<"$plan" >/dev/null ||
	fallback 'The trusted classifier selected shared or non-docs domains.'

# Authorship comes from GitHub, never the rerun actor or git author headers.
# API failure, a changed PR head, force-full label or ambiguous metadata all
# retain the full suite. The required gate independently repeats this plan.
if ! metadata=$(gh api "repos/$GITHUB_REPOSITORY/pulls/$pr"); then
	fallback 'PR metadata is unavailable; full validation retained.'
fi
if ! jq -e --argjson number "$pr" --arg repo "$GITHUB_REPOSITORY" --arg head "$pr_head" '
	.number == $number and .state == "open" and
	.base.ref == "main" and .base.repo.full_name == $repo and
	.head.sha == $head and (.head.repo.id | type == "number" and . > 0) and
	(.user.id | type == "number" and . > 0) and .user.type == "User" and
	(.user.login | type == "string" and length > 0) and .user.login != "dependabot[bot]" and
	(.labels | type == "array") and
	all(.labels[]; (.name | type == "string") and .name != "ci:full")
' <<<"$metadata" >/dev/null; then
	fallback 'Bot, full-suite request, changed PR identity or uncertain metadata.'
fi
emit docs 'Only docs and landing-page paths changed; site verification remains required.' "$plan"
