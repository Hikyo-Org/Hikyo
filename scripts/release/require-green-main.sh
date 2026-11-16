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

# A failure here exits 4: the shell's own status 1 would read as "not green".
repo_root=$(CDPATH='' cd -- "$(dirname "$0")/../.." && pwd) || exit 4
# Exit 0: complete green source proof. Exit 1: no such proof. Exit 4: GitHub could
# not be read. Exit 2: bad usage.
# The verifier accepts complete exact landed merge-group proof as well as
# manual/legacy full-main proof. It authenticates the producer, latest attempt,
# approved execution definition and every native job; a same-name check or
# candidate-authored artifact cannot replace source execution evidence.
# "go run" hides the program's exit status, so the verifier reports its class
# through a file. The class starts as "lookup" (could not look), and only the
# verifier's own definite verdict changes it to "not-green": a build failure or a
# crash before the verdict is never read as "not green".
class=$(mktemp "${TMPDIR:-/tmp}/hikyo-green-class.XXXXXX") || exit 4
trap 'rm -f "$class"' EXIT HUP INT TERM
printf 'lookup\n' >"$class" || exit 4
if ! "$GO_BIN" -C "$repo_root" run -p 2 ./scripts/ci/source-proof \
	--repository "$repository" --mode main --commit "$commit" --authority-commit "$commit" --failure-class "$class" >/dev/null; then
	if [ "$(cat "$class")" = not-green ]; then
		printf 'green main: exact commit %s needs complete authenticated source proof; run full ci-main.yml on the selected main commit when proof is unavailable\n' "$commit" >&2
		exit 1
	fi
	printf 'green main: could not read GitHub or run the verifier for exact commit %s; nothing is decided\n' "$commit" >&2
	exit 4
fi

printf 'green main: exact commit %s passed CI\n' "$commit"
