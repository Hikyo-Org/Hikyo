#!/bin/sh
# Keeps desktop/ (a nested Go module: Wails, cgo, WebKit) apart from the root
# module, and keeps the feedback planner honest about what desktop/ builds. Run nightly by
# nightly-desktop.yml; no ci.yml job runs it.
#   1. No desktop-only dependency (Wails and its platform bindings) in the root
#      go.mod or go.sum, no go.work, and no root requirement on desktop/.
#   2. desktop/go.mod still points at the root module with `replace ... => ../`.
#   3. Every root package desktop/ compiles (go list -deps, darwin, default and
#      probe builds) selects the desktop-compile check in feedback-plan.sh.
# Where desktop/ does not exist yet (main before the shell lands) only check 1 runs.
# Go sources under desktop/ without desktop/go.mod fail: a deleted manifest must not
# let the module pass unbuilt.
# Usage: scripts/ci/check-desktop-deps.sh [REPO_ROOT]
set -eu

fail() {
	printf 'check-desktop-deps: %s\n' "$*" >&2
	exit 1
}

script_dir=$(CDPATH='' cd -- "$(dirname "$0")" && pwd) || fail 'cannot locate this script'
feedback_plan=${DESKTOP_DEPS_FEEDBACK_PLAN:-$script_dir/feedback-plan.sh}
root_module=github.com/Hikyo-Org/hikyo
if [ "$#" -gt 0 ]; then
	root=$1
else
	root=$(git rev-parse --show-toplevel) || fail 'not inside a git work tree'
fi
cd "$root" || fail "cannot enter $root"
[ -f go.mod ] && [ -f go.sum ] || fail 'the root go.mod or go.sum is missing'

# 1. Modules only desktop/ may use. Prefixes, so every version and /vN suffix matches.
for module in github.com/wailsapp/ github.com/ebitengine/purego github.com/go-ole/go-ole; do
	if grep -F -n -e "$module" go.mod go.sum >&2; then
		fail "the root module lists $module, a desktop-only dependency; run go mod tidy at the repository root and keep the import inside desktop/"
	fi
done
[ ! -e go.work ] || fail 'go.work joins desktop/ to the root module; remove it'
if grep -E -n "^[[:space:]]*(require[[:space:]]+)?${root_module}/desktop[[:space:]]|^[[:space:]]*replace[[:space:]].*desktop" go.mod >&2; then
	fail 'the root go.mod references the desktop module'
fi

# Everything below needs the module itself.
if [ ! -f desktop/go.mod ]; then
	if [ -d desktop ] && [ -n "$(find desktop -name '*.go' -print -quit)" ]; then
		fail 'desktop/ holds Go sources but desktop/go.mod is missing; restore the module manifest'
	fi
	printf 'check-desktop-deps: no desktop/ module here; the root module is clean\n'
	exit 0
fi

# 2. The one allowed coupling, from desktop/ to the root module.
# Inline `replace M => ../` and the grouped `replace ( M => ../ )` form both count.
awk -v mod="$root_module" '
	{ sub(/\/\/.*/, "") }
	/^[ \t]*replace[ \t]*\([ \t]*$/ { block = 1; next }
	block && /^[ \t]*\)/ { block = 0; next }
	{
		line = $0
		if (!block) {
			if (line !~ /^[ \t]*replace[ \t]/) next
			sub(/^[ \t]*replace[ \t]+/, "", line)
		}
		if (split(line, f, " ") == 3 && f[1] == mod && f[2] == "=>" && (f[3] == "../" || f[3] == "..")) ok = 1
	}
	END { exit !ok }' desktop/go.mod ||
	fail "desktop/go.mod must keep: replace $root_module => ../"

# 3. Every root package desktop/ compiles selects the desktop-compile feedback check.
# The default build compiles files the probe build excludes (//go:build !probe).
listing=$(cd desktop && GOOS=darwin CGO_ENABLED=1 "${GO:-go}" list -deps -test -f '{{.ImportPath}}' ./... &&
	GOOS=darwin CGO_ENABLED=1 "${GO:-go}" list -deps -test -tags probe -f '{{.ImportPath}}' ./...) ||
	fail 'go list failed inside desktop/'
packages=$(printf '%s\n' "$listing" | sed -n "s|^$root_module/||p" | grep -v -E -e '^desktop( .*)?$' -e '^desktop/' -e '^desktop_test \[.*/desktop\.test\]$' | sed 's/ \[.*$//; s/\.test$//' | sort -u) ||
	packages=''
[ -n "$packages" ] || fail 'desktop/ imports no root packages; the feedback planner list is stale'
changed=$(mktemp "${TMPDIR:-/tmp}/hikyo-desktop-deps.XXXXXX") || fail 'cannot create a scratch file'
trap 'rm -f "$changed"' EXIT HUP INT TERM
for package in $packages; do
	printf '%s/x.go\0' "$package" >"$changed"
	selected=$(bash -c '. "$1" && feedback_plan_build "$2" && printf "%s" "$desktop"' _ "$feedback_plan" "$changed") ||
		fail "feedback planner failed for $package"
	[ "$selected" = true ] ||
		fail "desktop/ imports $package, but a change there does not select the desktop-compile check; add it to feedback-plan.sh"
done
printf 'check-desktop-deps: desktop/ stays out of the root module; %s root packages select desktop-compile\n' "$(printf '%s\n' "$packages" | wc -l | tr -d ' ')"
