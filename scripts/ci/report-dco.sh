#!/bin/sh
# Explains a failed DCO check on the pull request itself. dco-report.yml runs
# this from the base branch under pull_request_target, over PR commits fetched
# as git data; nothing from the PR is executed. The gate itself stays in ci.yml's
# preflight; this only keeps one comment in sync with it: posted or updated
# while commits lack a sign-off, deleted once they all carry one.
#
# The comment names commits by SHA only. Commit subjects and author strings are
# attacker-controlled text and never reach the comment body.
set -eu

: "${GH_REPO:?}" "${PR_NUMBER:?}" "${BASE_SHA:?}" "${HEAD_SHA:?}" "${PR_AUTHOR:?}"
repo=${DCO_REPOSITORY:-.}
marker='<!-- hikyo-dco-report -->'
script_dir=$(CDPATH='' cd -- "$(dirname "$0")" && pwd)

if [ "$PR_AUTHOR" = "dependabot[bot]" ]; then
	export DCO_EXEMPT_AUTHOR='dependabot[bot] <49699333+dependabot[bot]@users.noreply.github.com>'
fi

status=0
output=$("$script_dir/check-dco.sh" "$BASE_SHA" "$HEAD_SHA" "$repo" 2>&1) || status=$?
printf '%s\n' "$output"
[ "$status" -le 1 ] || exit "$status"

existing=$(gh api --paginate "repos/$GH_REPO/issues/$PR_NUMBER/comments?per_page=100" \
	--jq ".[] | select(.user.login == \"github-actions[bot]\" and (.body | startswith(\"$marker\"))) | .id" | head -n 1)

if [ "$status" -eq 0 ]; then
	if [ -n "$existing" ]; then
		gh api -X DELETE "repos/$GH_REPO/issues/comments/$existing" >/dev/null
	fi
	exit 0
fi

# The backticks are Markdown, not a substitution.
# shellcheck disable=SC2016
unsigned=$(printf '%s\n' "$output" | sed -n 's/^DCO check: \([0-9a-f]\{40\}\) missing Signed-off-by: .*/- `\1`/p')
body=$(cat <<EOF
$marker
### DCO sign-off missing

Every commit in this pull request needs a \`Signed-off-by:\` trailer that matches its author exactly. See [CONTRIBUTING.md](https://github.com/$GH_REPO/blob/main/CONTRIBUTING.md#developer-certificate-of-origin). These commits have none:

$unsigned

To fix it, sign off every commit on the branch, without moving it, and force-push:

\`\`\`sh
git fetch https://github.com/$GH_REPO.git $BASE_SHA
git rebase --signoff "\$(git merge-base HEAD $BASE_SHA)"
git push --force-with-lease
\`\`\`

For a single commit, \`git commit --amend --signoff --no-edit\` followed by \`git push --force-with-lease\` also works. The sign-off certifies the [Developer Certificate of Origin 1.1](https://developercertificate.org/). It is not a cryptographic signature, which this repository also requires: keep commit signing on while you rebase.

This comment updates on every push and disappears once all commits are signed off.
EOF
)

if [ -n "$existing" ]; then
	gh api -X PATCH "repos/$GH_REPO/issues/comments/$existing" -f body="$body" >/dev/null
else
	gh api -X POST "repos/$GH_REPO/issues/$PR_NUMBER/comments" -f body="$body" >/dev/null
fi
exit 1
