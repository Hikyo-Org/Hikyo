#!/usr/bin/env bash
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"
files=()
while IFS= read -r -d '' path; do
	# An unstaged deletion remains in the index, but has no imports to check.
	[[ -f "$path" ]] || continue
	# Generated imports belong to their pinned generator. For example,
	# controller-gen intentionally omits the metav1 alias goimports adds.
	if ! grep -q '^// Code generated .*DO NOT EDIT' "$path"; then
		files+=("$path")
	fi
done < <(git ls-files -z --cached --others --exclude-standard -- '*.go')

if ((${#files[@]} == 0)); then exit 0; fi
unformatted=$(go run golang.org/x/tools/cmd/goimports@v0.49.0 -l "${files[@]}")
if [[ -n "$unformatted" ]]; then
	printf 'Go import formatting differs in:\n%s\n' "$unformatted" >&2
	exit 1
fi
printf 'Go import formatting passed for %s handwritten files\n' "${#files[@]}"
