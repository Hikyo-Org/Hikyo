#!/bin/sh
set -eu

: "${GO_BIN:=go}"

[ "$#" -eq 2 ] || {
	printf 'usage: %s OWNER/REPO COMMIT\n' "$0" >&2
	exit 2
}

repository=$1
commit=$2
if ! printf '%s\n' "$repository" | grep -Eq '^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$'; then
	printf 'green main: invalid repository %s\n' "$repository" >&2
	exit 2
fi
if ! printf '%s\n' "$commit" | grep -Eq '^[0-9a-f]{40}$'; then
	printf 'green main: commit must be a full lowercase SHA-1\n' >&2
	exit 2
fi

repo_root=$(CDPATH='' cd -- "$(dirname "$0")/../.." && pwd)
# The verifier accepts complete exact landed merge-group proof as well as
# manual/legacy full-main proof. It authenticates the producer, latest attempt,
# approved execution definition and every native job; a same-name check or
# candidate-authored artifact cannot replace source execution evidence.
if ! "$GO_BIN" -C "$repo_root" run -p 2 ./scripts/ci/source-proof \
	--repository "$repository" --mode main --commit "$commit" --authority-commit "$commit" >/dev/null; then
	printf 'green main: exact commit %s needs complete authenticated source proof; run full ci-main.yml on the selected main commit when proof is unavailable\n' "$commit" >&2
	exit 1
fi

printf 'green main: exact commit %s passed CI\n' "$commit"
