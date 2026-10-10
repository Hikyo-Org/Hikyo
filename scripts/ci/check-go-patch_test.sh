#!/bin/sh
set -eu

script_dir=$(CDPATH='' cd -- "$(dirname "$0")" && pwd)
script="$script_dir/check-go-patch.sh"
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT HUP INT TERM
mkdir -p "$work/bin"
cat >"$work/bin/curl" <<'STUB'
#!/bin/sh
set -eu
printf '%s\n' "$@" >"$CURL_ARGS_FILE"
[ "${CURL_FAIL:-0}" = 0 ] || exit 22
cat "$RELEASE_FIXTURE"
STUB
chmod +x "$work/bin/curl"

fail() { printf 'Go patch fixture failed: %s\n' "$1" >&2; exit 1; }

run_case() {
	name=$1
	pin=$2
	metadata=$3
	expected_status=$4
	dir="$work/$name"
	mkdir -p "$dir"
	printf 'module example.test/fixture\n\ngo %s\n\nrequire example.test/dependency v1.2.3\n' "$pin" >"$dir/go.mod"
	cp "$dir/go.mod" "$dir/original.mod"
	printf '%s\n' "$metadata" >"$dir/releases.json"
	curl_fail=0
	[ "$name" != download_failure ] || curl_fail=1
	status=0
	(cd "$dir" && PATH="$work/bin:$PATH" RELEASE_FIXTURE="$dir/releases.json" CURL_ARGS_FILE="$dir/curl-args" CURL_FAIL="$curl_fail" \
		GO_PATCH_PROPOSAL_DIR="$dir/proposal" GITHUB_STEP_SUMMARY="$dir/summary" \
		sh "$script" >"$dir/stdout" 2>"$dir/stderr") || status=$?
	[ "$status" -eq "$expected_status" ] || fail "$name: exit $status, want $expected_status"
	cmp "$dir/go.mod" "$dir/original.mod" || fail "$name: checkout changed"
	grep -Fx 'https://go.dev/dl/?mode=json' "$dir/curl-args" >/dev/null || fail "$name: release source changed"
	for flag in --fail --retry --proto '=https' --tlsv1.2; do
		grep -Fx -- "$flag" "$dir/curl-args" >/dev/null || fail "$name: missing curl policy $flag"
	done
	if [ "$expected_status" -eq 1 ]; then
		[ -s "$dir/proposal/go.mod.patch" ] || fail "$name: no proposal patch"
		cp "$dir/go.mod" "$dir/apply.mod"
		patch "$dir/apply.mod" "$dir/proposal/go.mod.patch" >/dev/null || fail "$name: unusable patch"
		grep -Fx 'go 1.27.2' "$dir/apply.mod" >/dev/null || fail "$name: proposal chose incorrect patch"
		grep -Fx 'require example.test/dependency v1.2.3' "$dir/apply.mod" >/dev/null || fail "$name: proposal changed dependencies"
		grep -F 'standard-library vulnerabilities' "$dir/summary" >/dev/null || fail "$name: missing security context"
		# Markdown code delimiters below are literal text.
		# shellcheck disable=SC2016
		grep -F 'Replace `go 1.27.0` with `go 1.27.2`' "$dir/summary" >/dev/null || fail "$name: missing exact recommendation"
	else
		[ ! -e "$dir/proposal/go.mod.patch" ] || fail "$name: unexpected update proposal"
	fi
}

run_case current 1.27.2 '[{"version":"go1.27.2","stable":true}]' 0
run_case new_patch 1.27.0 '[{"version":"go1.27.1","stable":true},{"version":"go1.27.2","stable":true}]' 1
run_case newer_minor 1.27.2 '[{"version":"go1.28.1","stable":true},{"version":"go1.27.2","stable":true}]' 0
run_case release_candidate 1.27.2 '[{"version":"go1.27.3rc1","stable":false},{"version":"go1.27.2","stable":true}]' 0
run_case downgrade 1.27.3 '[{"version":"go1.27.2","stable":true}]' 2
run_case absent_line 1.27.2 '[{"version":"go1.28.1","stable":true}]' 2
run_case malformed_json 1.27.2 'not-json' 2
run_case malformed_shape 1.27.2 '{"version":"go1.27.2","stable":true}' 2
run_case malformed_stable 1.27.2 '[{"version":"go1.27.2","stable":"true"}]' 2
run_case malformed_version 1.27.2 '[{"version":"go1.27.2oops","stable":true}]' 2
run_case multiple_documents 1.27.2 '[{"version":"go1.27.2","stable":true}] [{"version":"go1.27.2","stable":true}]' 2
run_case no_stable 1.27.2 '[{"version":"go1.27.3rc1","stable":false}]' 2
run_case download_failure 1.27.2 '[{"version":"go1.27.2","stable":true}]' 2

# The workflow can upload the documented default RUNNER_TEMP artifact location.
status=0
(cd "$work/new_patch" && unset GO_PATCH_PROPOSAL_DIR && PATH="$work/bin:$PATH" \
	RELEASE_FIXTURE="$work/new_patch/releases.json" CURL_ARGS_FILE="$work/default-curl-args" \
	RUNNER_TEMP="$work/runner" GITHUB_STEP_SUMMARY="$work/default-summary" \
	sh "$script" >"$work/default-stdout" 2>"$work/default-stderr") || status=$?
[ "$status" -eq 1 ] || fail "default directory: exit $status, want 1"
[ -s "$work/runner/go-patch-proposal/go.mod.patch" ] || fail 'default directory: missing patch'
cmp "$work/new_patch/go.mod" "$work/new_patch/original.mod" || fail 'default directory: checkout changed'

printf 'Go patch proposal fixtures passed\n'
