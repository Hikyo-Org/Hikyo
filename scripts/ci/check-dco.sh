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

# A single author identity may be exempted from the sign-off requirement via the
# DCO_EXEMPT_AUTHOR env var. This exists for Dependabot, which cannot produce a
# parseable Signed-off-by (its messages place the sign-off after the "---"
# metadata separator, which git interpret-trailers treats as the end of the
# trailer block) and for which the DCO — a human-contributor attestation — is
# meaningless. The git author header is attacker-controlled, so this is NOT a
# trust decision: the caller must set DCO_EXEMPT_AUTHOR only after verifying
# non-spoofable provenance (the GitHub PR actor), and leave it empty otherwise.
# Default-closed — an unset var exempts nobody.
exempt_author=${DCO_EXEMPT_AUTHOR:-}

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
	printf 'DCO check: %s commits signed, %s exempt (%s)\n' "$signed" "$exempt" "$exempt_author"
else
	printf 'DCO check: %s commits signed\n' "$signed"
fi
