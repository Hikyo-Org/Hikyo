#!/bin/sh
set -eu

# WebUI parity registry: every `issue: N` disposition in api/parity.yaml must
# name an OPEN issue. A closed issue with the row still pointing at it means a
# surface was declared done without the registry flipping to `webui` (or the
# issue was closed without the surface landing); either way the inventory is
# lying and the build says so. The structural half of the registry (coverage,
# evidence, closed exception classes) is `go test ./api`; this is the one check
# that needs the network.
#
# Usage: check-parity-issues.sh [registry]
# Environment: GH_TOKEN (read), GH_REPO (owner/name; defaults to gh's view of
# the checkout).

script_dir=$(CDPATH='' cd -- "$(dirname "$0")" && pwd)
registry=${1:-$script_dir/../../api/parity.yaml}

if [ ! -f "$registry" ]; then
	printf 'parity issues: registry %s not found\n' "$registry" >&2
	exit 2
fi

# Comment lines are dropped first so prose never counts as a disposition.
issues=$(grep -v '^[[:space:]]*#' "$registry" | grep -oE 'issue:[[:space:]]*[0-9]+' | grep -oE '[0-9]+' | sort -un || true)

if [ -z "$issues" ]; then
	printf 'parity issues: no issue dispositions in %s\n' "$registry"
	exit 0
fi

repo=${GH_REPO:-}
if [ -z "$repo" ]; then
	if ! repo=$(gh repo view --json nameWithOwner --jq .nameWithOwner 2>/dev/null); then
		printf 'parity issues: set GH_REPO=owner/name; the checkout has no resolvable GitHub remote\n' >&2
		exit 2
	fi
fi

failed=0
for number in $issues; do
	if ! answer=$(gh api "repos/$repo/issues/$number" --jq '[.state, (.pull_request != null | tostring)] | join(" ")' 2>&1); then
		printf 'parity issues: cannot read %s#%s: %s\n' "$repo" "$number" "$answer" >&2
		printf 'parity issues: the token needs read access to issues; this check never falls back to guessing\n' >&2
		exit 1
	fi
	state=${answer%% *}
	is_pull=${answer##* }
	if [ "$is_pull" = "true" ]; then
		printf 'parity issues: #%s is a pull request, not an implementation issue\n' "$number" >&2
		failed=1
		continue
	fi
	if [ "$state" != "open" ]; then
		printf 'parity issues: #%s is %s but api/parity.yaml still lists operations under it; flip the rows to webui or reopen the issue\n' "$number" "$state" >&2
		failed=1
		continue
	fi
	printf 'parity issues: #%s open\n' "$number"
done

if [ "$failed" -ne 0 ]; then
	exit 1
fi
printf 'parity issues: every referenced issue is open\n'
