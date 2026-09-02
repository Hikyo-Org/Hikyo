#!/bin/sh
set -eu

repo_root=$(git rev-parse --show-toplevel)
[ -x "$repo_root/.githooks/pre-push" ] || {
	printf 'hook installer: %s is missing or not executable\n' "$repo_root/.githooks/pre-push" >&2
	exit 1
}

git config core.hooksPath .githooks
printf 'Git hooks installed: core.hooksPath=.githooks\n'
