#!/bin/sh
set -eu

repo_root=$(CDPATH='' cd -- "$(dirname "$0")/../.." && pwd)
workflow_dir="$repo_root/.github/workflows"
repository_config="$repo_root/scripts/release/configure-repository.sh"

fail() {
	printf 'CodeQL default-setup fixture failed: %s\n' "$1" >&2
	exit 1
}

# Managed default setup is repository configuration, not workflow YAML. Any
# checked-in CodeQL action would create a second scan and fail at startup while
# default setup owns SARIF uploads.
for workflow in "$workflow_dir"/*.yml "$workflow_dir"/*.yaml; do
	[ -e "$workflow" ] || continue
	if grep -F 'github/codeql-action/' "$workflow" >/dev/null; then
		fail "advanced CodeQL workflow remains at ${workflow#"$repo_root/"}"
	fi
done

grep -F "repos/\$repository/code-scanning/default-setup" "$repository_config" >/dev/null ||
	fail 'repository configuration does not manage CodeQL default setup'
grep -F -- '-f state=configured -f query_suite=default' "$repository_config" >/dev/null ||
	fail 'repository configuration does not enable CodeQL default setup'
if grep -F 'state=not-configured' "$repository_config" >/dev/null; then
	fail 'repository configuration disables CodeQL default setup'
fi

printf 'CodeQL default-setup fixture: managed setup enabled; advanced workflows absent\n'
