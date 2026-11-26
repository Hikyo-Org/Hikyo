#!/bin/sh
set -eu

repo=$(mktemp -d "${TMPDIR:-/tmp}/hikyo-dco-fixture.XXXXXX")
trap 'rm -rf "$repo"' EXIT HUP INT TERM

git -C "$repo" init -q
git -C "$repo" config user.name 'Fixture Author'
git -C "$repo" config user.email 'fixture@example.com'

printf 'base\n' >"$repo/file"
git -C "$repo" add file
git -C "$repo" commit -q -s -m base
base=$(git -C "$repo" rev-parse HEAD)

printf 'signed\n' >>"$repo/file"
git -C "$repo" commit -q -s -am signed
signed=$(git -C "$repo" rev-parse HEAD)
"$(dirname "$0")/check-dco.sh" "$base" "$signed" "$repo"

git -C "$repo" branch topic "$signed"
git -C "$repo" switch -q --detach "$base"
printf 'advanced main\n' >"$repo/main-only"
git -C "$repo" add main-only
git -C "$repo" commit -q -s -m 'advance main'
advanced_base=$(git -C "$repo" rev-parse HEAD)
"$(dirname "$0")/check-dco.sh" "$advanced_base" "$signed" "$repo" >/dev/null
git -C "$repo" switch -q topic

git -C "$repo" merge -q --no-ff --no-edit "$advanced_base"
merged=$(git -C "$repo" rev-parse HEAD)
"$(dirname "$0")/check-dco.sh" "$advanced_base" "$merged" "$repo" >/dev/null

printf 'unsigned\n' >>"$repo/file"
git -C "$repo" commit -q -am unsigned
unsigned=$(git -C "$repo" rev-parse HEAD)
if "$(dirname "$0")/check-dco.sh" "$signed" "$unsigned" "$repo" >/dev/null 2>&1; then
	printf 'DCO fixture failed: unsigned commit accepted\n' >&2
	exit 1
fi

# Build a commit carrying Dependabot's exact author identity and no parseable
# sign-off (a real dependabot message, whose sign-off sits after the "---"
# metadata separator that git interpret-trailers cannot see).
git -C "$repo" switch -q --detach "$signed"
printf 'bumped\n' >>"$repo/file"
git -C "$repo" commit -q -am 'chore(deps): bump something

---
updated-dependencies:
- dependency-name: something
...

Signed-off-by: dependabot[bot] <support@github.com>' \
	--author='dependabot[bot] <49699333+dependabot[bot]@users.noreply.github.com>'
dependabot=$(git -C "$repo" rev-parse HEAD)

# Default-closed: with DCO_EXEMPT_AUTHOR unset, that identity is NOT exempt. A
# forged git author header alone buys nothing — this is the attack the exemption
# must not enable.
if "$(dirname "$0")/check-dco.sh" "$signed" "$dependabot" "$repo" >/dev/null 2>&1; then
	printf 'DCO fixture failed: forged dependabot author exempt with no opt-in\n' >&2
	exit 1
fi

# Opt-in: only when the trusted caller sets DCO_EXEMPT_AUTHOR (gated in CI on the
# non-spoofable GitHub PR actor) is that exact identity exempt.
DCO_EXEMPT_AUTHOR='dependabot[bot] <49699333+dependabot[bot]@users.noreply.github.com>' \
	"$(dirname "$0")/check-dco.sh" "$signed" "$dependabot" "$repo" >/dev/null

# The opt-in is scoped to the exact identity: a look-alike author (same name,
# different email) is still refused even with the exemption active.
printf 'spoof\n' >>"$repo/file"
git -C "$repo" commit -q -am 'chore: lookalike' \
	--author='dependabot[bot] <attacker@example.com>'
lookalike=$(git -C "$repo" rev-parse HEAD)
if DCO_EXEMPT_AUTHOR='dependabot[bot] <49699333+dependabot[bot]@users.noreply.github.com>' \
	"$(dirname "$0")/check-dco.sh" "$dependabot" "$lookalike" "$repo" >/dev/null 2>&1; then
	printf 'DCO fixture failed: dependabot look-alike accepted\n' >&2
	exit 1
fi

# Main-ancestry exemption: DCO_MAIN_SHA is the trusted tip of main. A Dependabot
# author is exempt only for commits already reachable from it, so the same
# forged commit that is refused while off main (as on a release/ branch) passes
# once main contains it.
check="$(dirname "$0")/check-dco.sh"
refuses() {
	if "$@" >/dev/null 2>&1; then
		printf 'DCO fixture failed: %s\n' "$label" >&2
		exit 1
	fi
}

git -C "$repo" branch release/1.1.0 "$dependabot"
label='forged dependabot author off main exempt'
DCO_MAIN_SHA=$signed refuses "$check" "$signed" "$dependabot" "$repo"
label='forged dependabot author on a release branch, not on main, exempt'
DCO_MAIN_SHA=$base refuses "$check" "$signed" release/1.1.0 "$repo"

# A backmerge into dev: main's Dependabot commit plus a signed-off merge commit.
git -C "$repo" switch -q --detach "$advanced_base"
git -C "$repo" merge -q --no-ff --signoff -m 'chore: backmerge main into dev' "$dependabot"
backmerge=$(git -C "$repo" rev-parse HEAD)
DCO_MAIN_SHA=$dependabot "$check" "$advanced_base" "$backmerge" "$repo" >/dev/null
label='backmerge carrying a Dependabot commit passed without main'
refuses "$check" "$advanced_base" "$backmerge" "$repo"
DCO_MAIN_SHA=$advanced_base refuses "$check" "$advanced_base" "$backmerge" "$repo"

label='unsigned human commit exempted by main ancestry'
DCO_MAIN_SHA=$unsigned refuses "$check" "$signed" "$unsigned" "$repo"
label='look-alike author exempted by main ancestry'
DCO_MAIN_SHA=$lookalike refuses "$check" "$dependabot" "$lookalike" "$repo"

label='unresolvable main did not fail closed'
DCO_MAIN_SHA=0000000000000000000000000000000000000001 refuses "$check" "$advanced_base" "$backmerge" "$repo"
DCO_MAIN_SHA=not-a-ref refuses "$check" "$advanced_base" "$backmerge" "$repo"

printf 'DCO fixture: signed/behind-base/merge accepted; unsigned, forged-without-opt-in, and look-alike refused; exact identity accepted only on opt-in; Dependabot commits exempt only when on the trusted main\n'
