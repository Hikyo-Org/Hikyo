#!/bin/sh
set -eu

if [ "$#" -lt 2 ] || [ "$#" -gt 3 ]; then
	printf 'usage: %s BASE HEAD [REPOSITORY]\n' "$0" >&2
	exit 2
fi

base=$1
head=$2
repo=${3:-.}

git -C "$repo" rev-parse --verify "$base^{commit}" >/dev/null 2>&1 \
	|| { printf 'DCO check: invalid base %s\n' "$base" >&2; exit 2; }
git -C "$repo" rev-parse --verify "$head^{commit}" >/dev/null 2>&1 \
	|| { printf 'DCO check: invalid head %s\n' "$head" >&2; exit 2; }
base=$(git -C "$repo" merge-base "$base" "$head") \
	|| { printf 'DCO check: base and head have no merge base\n' >&2; exit 2; }

commits=$(git -C "$repo" rev-list --no-merges --reverse "$base..$head")
[ -n "$commits" ] || { printf 'DCO check: empty commit range\n' >&2; exit 2; }

# Dependabot cannot produce a parseable Signed-off-by (its messages place the
# sign-off after the "---" metadata separator, which git interpret-trailers
# treats as the end of the trailer block), and the DCO, a human-contributor
# attestation, is meaningless for it. The git author header is attacker-
# controlled, so two exemptions exist, both default-closed:
#   - DCO_EXEMPT_AUTHOR: one exact identity, set by a caller only after checking
#     the non-spoofable GitHub PR actor (the PR was opened by Dependabot).
#   - DCO_MAIN_SHA / DCO_DEV_SHA: trusted tips of the base repository's main and
#     dev, resolved by the caller from the canonical remote, never from PR
#     input. A Dependabot-authored commit that is already an ancestor of either
#     entered that branch through a gated PR, and Dependabot's commits only
#     enter dev through PRs Dependabot opened, so a promotion or backmerge may
#     carry it.
# That premise rests on the dev and main rulesets: they require PRs and status
# checks, block non-fast-forward pushes and deletions, and have no bypass
# actors. Adding a bypass actor or loosening either ruleset breaks it.
# An unset variable exempts nobody; a set one that does not resolve to a commit
# is an error, never a silent loss of the check.
dependabot='dependabot[bot] <49699333+dependabot[bot]@users.noreply.github.com>'
exempt_author=${DCO_EXEMPT_AUTHOR:-}
trusted_tips=
for name in DCO_MAIN_SHA DCO_DEV_SHA; do
	value=$(printenv "$name" || true)
	[ -n "$value" ] || continue
	tip=$(git -C "$repo" rev-parse --verify "$value^{commit}" 2>/dev/null) \
		|| { printf 'DCO check: invalid trusted %s %s\n' "$name" "$value" >&2; exit 2; }
	trusted_tips="$trusted_tips $tip"
done

# Prints nothing and succeeds only for a commit reachable from a trusted tip.
on_trusted_branch() {
	for tip in $trusted_tips; do
		git -C "$repo" merge-base --is-ancestor "$1" "$tip" 2>/dev/null && return 0
	done
	return 1
}

failed=0
signed=0
exempt=0
for commit in $commits; do
	# show -s can still inspect blobs for rename detection. Admission metadata
	# is blob-filtered; author and trailer checks need commit objects only.
	identity=$(git -C "$repo" log -1 --no-patch --format='%an <%ae>' "$commit")
	if [ -n "$exempt_author" ] && [ "$identity" = "$exempt_author" ]; then
		exempt=$((exempt + 1))
		continue
	fi
	if [ "$identity" = "$dependabot" ] && on_trusted_branch "$commit"; then
		exempt=$((exempt + 1))
		continue
	fi
	trailers=$(git -C "$repo" log -1 --no-patch --format='%B' "$commit" | git -C "$repo" interpret-trailers --parse)
	if ! printf '%s\n' "$trailers" | grep -F -i -x "Signed-off-by: $identity" >/dev/null; then
		printf 'DCO check: %s missing Signed-off-by: %s\n' "$commit" "$identity" >&2
		failed=1
	else
		signed=$((signed + 1))
	fi
done

[ "$failed" -eq 0 ] || exit 1
if [ "$exempt" -gt 0 ]; then
	printf 'DCO check: %s commits signed, %s Dependabot commits exempt\n' "$signed" "$exempt"
else
	printf 'DCO check: %s commits signed\n' "$signed"
fi
