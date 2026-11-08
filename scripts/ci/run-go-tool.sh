#!/bin/sh
set -eu

if [ "$#" -lt 1 ]; then
	printf 'usage: %s actionlint|govulncheck|golangci-lint [arguments...]\n' "$0" >&2
	exit 2
fi

tool=$1
shift

case "$tool" in
actionlint)
	module=github.com/rhysd/actionlint
	package=$module/cmd/actionlint
	;;
govulncheck)
	module=golang.org/x/vuln
	package=$module/cmd/govulncheck
	;;
golangci-lint)
	# Pinned outside go-tool-modules.txt: that file keys every Go cache and is
	# required into one module, where MVS would move x/tools past its pin.
	module=github.com/golangci/golangci-lint/v2
	package=$module/cmd/golangci-lint
	;;
*)
	printf 'unknown Go CI tool: %s\n' "$tool" >&2
	exit 2
	;;
esac

script_dir=$(CDPATH='' cd -- "$(dirname "$0")" && pwd)
manifest=$script_dir/go-tool-modules.txt
if [ "$tool" = golangci-lint ]; then
	manifest=$script_dir/golangci-lint-version
	version=$(tr -d '\r' <"$manifest")
else
	version=$(awk -F '@' -v module="$module" '$1 == module { print $2 }' "$manifest")
fi

if [ -z "$version" ] || [ "$(printf '%s\n' "$version" | wc -l | tr -d ' ')" -ne 1 ]; then
	printf 'expected exactly one version for %s in %s\n' "$module" "$manifest" >&2
	exit 1
fi

if [ "$tool" = actionlint ]; then
	# Validate the supported concurrency.queue key before invoking the pinned
	# actionlint parser, which has not implemented it yet. No checks are ignored.
	exec go run "$script_dir/workflow-lint" "$version" "$@"
fi

exec go run "$package@$version" "$@"
