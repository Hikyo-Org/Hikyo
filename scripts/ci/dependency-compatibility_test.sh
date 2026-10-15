#!/usr/bin/env bash
# A local dependency removes an API while the consuming source stays unchanged.
set -euo pipefail
script_dir=$(CDPATH='' cd -- "$(dirname "$0")" && pwd)
work=$(mktemp -d "${TMPDIR:-/tmp}/hikyo-dependency-compatibility.XXXXXX")
trap 'rm -rf "$work"' EXIT
mkdir -p "$work/repo" "$work/before" "$work/after"
for version in before after; do
	printf 'module example.invalid/dependency\n\ngo 1.27.2\n' >"$work/$version/go.mod"
done
printf 'package dependency\n\nconst Value = 1\n' >"$work/before/api.go"
printf 'package dependency\n\nconst Replacement = 1\n' >"$work/after/api.go"
cd "$work/repo"
git init -q
printf 'package fixture\n' >fixture.go
select_dependency() {
	printf 'module example.invalid/consumer\n\ngo 1.27.2\n\nrequire example.invalid/dependency v0.0.0\nreplace example.invalid/dependency => ../%s\n' "$1" >go.mod
}
run_compatibility() { bash "$script_dir/dependency-compatibility.sh" >"$work/output" 2>&1; }
refuse() {
	if run_compatibility; then
		printf 'dependency compatibility unexpectedly accepted %s\n' "$1" >&2
		exit 1
	fi
	grep -F "$1" "$work/output" >/dev/null || { cat "$work/output" >&2; exit 1; }
	if grep -F 'default-tag production and test consumers passed' "$work/output"; then exit 1; fi
}
for consumer in production.go internal_test.go external_test.go; do
	package=fixture
	[ "$consumer" != external_test.go ] || package=fixture_test
	printf 'package %s\n\nimport "example.invalid/dependency"\n\nvar Consumer = dependency.Value\n' "$package" >"$consumer"
	select_dependency before
	run_compatibility
	# Only the module selection changes; the same consumer must now fail.
	select_dependency after
	refuse 'undefined: dependency.Value'
	rm "$consumer"
done
select_dependency before
# Platform-suffixed files are selected by Go's target rules, not by ./... alone.
# A broken active consumer fails; the same source for another OS is excluded,
# and a successful result must state exactly which target was checked.
target_os=$(go env GOOS)
target_arch=$(go env GOARCH)
other_os=windows
[ "$target_os" != windows ] || other_os=linux
printf 'package fixture\n\nvar PlatformConsumer = missingPlatformAPI\n' >"platform_$target_os.go"
refuse 'undefined: missingPlatformAPI'
mv "platform_$target_os.go" "platform_$other_os.go"
printf '//go:build compatibility_nondefault\n\npackage fixture\n\nvar TaggedConsumer = missingTaggedAPI\n' >tagged.go
GOFLAGS='-mod=readonly -tags compatibility_nondefault' run_compatibility
grep -F "scope target=$target_os/$target_arch CGO_ENABLED=" "$work/output" >/dev/null
grep -F "default-tag production and test consumers passed for $target_os/$target_arch" "$work/output" >/dev/null
if grep -F 'all production and test consumers passed' "$work/output"; then exit 1; fi
rm "platform_$other_os.go" tagged.go
cat >no_execute_test.go <<'EOF'
package fixture

import "testing"

func init() { panic("compatibility executed package init") }
func TestMustNotExecute(t *testing.T) { t.Fatal("compatibility executed tests") }
EOF
run_compatibility
rm no_execute_test.go
cat >vet_test.go <<'EOF'
package fixture

import "fmt"

func vetConsumer() { fmt.Printf("%d", "not an integer") }
EOF
refuse 'fmt.Printf format %d has arg "not an integer" of wrong type string'
printf 'dependency compatibility fixtures: unchanged production/internal/external test consumers, explicit target scope, no execution and vet passed\n'
