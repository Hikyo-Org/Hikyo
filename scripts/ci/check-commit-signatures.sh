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
	|| { printf 'Signature check: invalid base %s\n' "$base" >&2; exit 2; }
git -C "$repo" rev-parse --verify "$head^{commit}" >/dev/null 2>&1 \
	|| { printf 'Signature check: invalid head %s\n' "$head" >&2; exit 2; }
base=$(git -C "$repo" merge-base "$base" "$head") \
	|| { printf 'Signature check: base and head have no merge base\n' >&2; exit 2; }

commits=$(git -C "$repo" rev-list --reverse "$base..$head")
[ -n "$commits" ] || { printf 'Signature check: empty commit range\n' >&2; exit 2; }

failed=0
for commit in $commits; do
	status=$(git -C "$repo" show -s --format='%G?' "$commit")
	if [ "$status" != G ]; then
		printf 'Signature check: %s is not locally verified (status %s)\n' "$commit" "$status" >&2
		failed=1
	fi
done

[ "$failed" -eq 0 ] || exit 1
printf 'Signature check: %s commits cryptographically verified\n' "$(printf '%s\n' "$commits" | wc -l | tr -d ' ')"
