#!/bin/sh
set -eu

repo=$(mktemp -d "${TMPDIR:-/tmp}/hikyo-signature-fixture.XXXXXX")
trap 'rm -rf "$repo"' EXIT HUP INT TERM
zero=0000000000000000000000000000000000000000

git -C "$repo" init -q
git -C "$repo" config user.name 'Fixture Author'
git -C "$repo" config user.email 'fixture@example.com'
git -C "$repo" config commit.gpgsign false

printf 'base\n' >"$repo/file"
git -C "$repo" add file
git -C "$repo" commit -q -m base
base=$(git -C "$repo" rev-parse HEAD)

ssh-keygen -q -t ed25519 -N '' -f "$repo/signing-key"
printf 'fixture@example.com %s\n' "$(cat "$repo/signing-key.pub")" >"$repo/allowed-signers"
git -C "$repo" config gpg.format ssh
git -C "$repo" config user.signingkey "$repo/signing-key"
git -C "$repo" config gpg.ssh.allowedSignersFile "$repo/allowed-signers"
git -C "$repo" config commit.gpgsign true

mkdir -p "$repo/.githooks" "$repo/scripts/ci" "$repo/scripts/git"
cp "$(dirname "$0")/check-commit-signatures.sh" "$repo/scripts/ci/"
cp "$(dirname "$0")/../../.githooks/pre-push" "$repo/.githooks/"
cp "$(dirname "$0")/../git/install-hooks.sh" "$repo/scripts/git/"
chmod +x "$repo/.githooks/pre-push" "$repo/scripts/ci/check-commit-signatures.sh" "$repo/scripts/git/install-hooks.sh"
(cd "$repo" && scripts/git/install-hooks.sh >/dev/null)
[ "$(git -C "$repo" config core.hooksPath)" = .githooks ] || {
	printf 'signature fixture failed: pre-push hook was not installed\n' >&2
	exit 1
}

printf 'signed\n' >>"$repo/file"
git -C "$repo" commit -q -am signed
signed=$(git -C "$repo" rev-parse HEAD)
"$(dirname "$0")/check-commit-signatures.sh" "$base" "$signed" "$repo"
git -C "$repo" update-ref refs/remotes/origin/main "$base"
printf 'refs/heads/signed %s refs/heads/signed %s\n' "$signed" "$zero" >"$repo/push-input"
git -C "$repo" hook run --to-stdin="$repo/push-input" pre-push -- origin unused

primary=$(git -C "$repo" branch --show-current)
git -C "$repo" switch -q -c merge-side "$base"
printf 'side\n' >"$repo/side"
git -C "$repo" add side
git -C "$repo" commit -q -m side
git -C "$repo" switch -q "$primary"
git -C "$repo" -c commit.gpgsign=false merge -q --no-ff --no-edit merge-side
unsigned_merge=$(git -C "$repo" rev-parse HEAD)
if "$(dirname "$0")/check-commit-signatures.sh" "$base" "$unsigned_merge" "$repo" >/dev/null 2>&1; then
	printf 'signature fixture failed: unsigned merge commit accepted\n' >&2
	exit 1
fi

git -C "$repo" switch -q -c unsigned-case "$signed"
printf 'unsigned\n' >>"$repo/file"
git -C "$repo" -c commit.gpgsign=false commit -q -am unsigned
unsigned=$(git -C "$repo" rev-parse HEAD)
if "$(dirname "$0")/check-commit-signatures.sh" "$signed" "$unsigned" "$repo" >/dev/null 2>&1; then
	printf 'signature fixture failed: unsigned commit accepted\n' >&2
	exit 1
fi
printf 'refs/heads/unsigned-case %s refs/heads/unsigned-case %s\n' "$unsigned" "$zero" >"$repo/push-input"
if git -C "$repo" hook run --to-stdin="$repo/push-input" pre-push -- origin unused >/dev/null 2>&1; then
	printf 'signature fixture failed: pre-push hook accepted an unsigned commit\n' >&2
	exit 1
fi

: >"$repo/allowed-signers"
if "$(dirname "$0")/check-commit-signatures.sh" "$base" "$signed" "$repo" >/dev/null 2>&1; then
	printf 'signature fixture failed: unverifiable signature accepted\n' >&2
	exit 1
fi

printf 'Signature fixture: checker and pre-push hook accept valid signatures and refuse invalid ones\n'
