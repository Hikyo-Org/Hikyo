#!/bin/sh
set -eu

fixture_dir=$(mktemp -d "${TMPDIR:-/tmp}/hikyo-homebrew-cask.XXXXXX")
trap 'rm -rf "$fixture_dir"' EXIT HUP INT TERM
bundle="$fixture_dir/bundle"
mkdir -p "$bundle"

printf 'arm64 archive\n' >"$bundle/hikyo_1.2.3_Darwin_arm64.dmg"
printf 'intel archive\n' >"$bundle/hikyo_1.2.3_Darwin_x86_64.dmg"
arm_sha=$(shasum -a 256 "$bundle/hikyo_1.2.3_Darwin_arm64.dmg" | awk '{print $1}')
intel_sha=$(shasum -a 256 "$bundle/hikyo_1.2.3_Darwin_x86_64.dmg" | awk '{print $1}')
jq -n --arg arm_sha "$arm_sha" --arg intel_sha "$intel_sha" '{
	schema: "hikyo.dev/release-manifest/v1",
	version: "1.2.3",
	tag: "v1.2.3",
	artifacts: [
		{name: "hikyo_1.2.3_Darwin_arm64.dmg", kind: "installer", sha256: $arm_sha},
		{name: "hikyo_1.2.3_Darwin_x86_64.dmg", kind: "installer", sha256: $intel_sha}
	]
}' >"$bundle/release-manifest.json"

output="$fixture_dir/hikyo.rb"
"$(dirname "$0")/render-homebrew-cask.sh" \
	"$bundle/release-manifest.json" "$bundle" "$output" Hikyo-Org/Hikyo

grep -Fx 'cask "hikyo" do' "$output" >/dev/null
grep -Fx '  arch arm: "arm64", intel: "x86_64"' "$output" >/dev/null
grep -Fx '  version "1.2.3"' "$output" >/dev/null
grep -Fx "  sha256 arm:   \"$arm_sha\"," "$output" >/dev/null
grep -Fx "         intel: \"$intel_sha\"" "$output" >/dev/null
grep -Fx '  url "https://github.com/Hikyo-Org/Hikyo/releases/download/v#{version}/hikyo_#{version}_Darwin_#{arch}.dmg"' "$output" >/dev/null
grep -Fx '  binary "hikyo"' "$output" >/dev/null

nightly_version=0.0.1-nightly.20261024.92.gaaaaaaaa
for arch in arm64 x86_64; do
	cp "$bundle/hikyo_1.2.3_Darwin_$arch.dmg" "$bundle/hikyo_${nightly_version}_Darwin_$arch.dmg"
done
jq --arg version "$nightly_version" '
  .schema = "hikyo.dev/nightly-manifest/v1" | .profile = "nightly/v1" |
  .version = $version | .tag = ("v" + $version) |
  (.artifacts[].name) |= sub("1.2.3"; $version)
' "$bundle/release-manifest.json" >"$bundle/nightly-manifest.json"
"$(dirname "$0")/render-homebrew-cask.sh" --nightly \
	"$bundle/nightly-manifest.json" "$bundle" "$fixture_dir/hikyo@nightly.rb" Hikyo-Org/Hikyo
grep -Fx 'cask "hikyo@nightly" do' "$fixture_dir/hikyo@nightly.rb" >/dev/null
grep -Fx '  conflicts_with cask: "hikyo"' "$fixture_dir/hikyo@nightly.rb" >/dev/null
grep -Fx "  version \"$nightly_version\"" "$fixture_dir/hikyo@nightly.rb" >/dev/null
# Channel confusion must never render a stable cask from nightly metadata.
for scenario in stable-as-nightly nightly-as-stable; do
	case "$scenario" in
		stable-as-nightly) set -- --nightly "$bundle/release-manifest.json" ;;
		nightly-as-stable) set -- "$bundle/nightly-manifest.json" ;;
	esac
	if "$(dirname "$0")/render-homebrew-cask.sh" "$@" "$bundle" "$fixture_dir/$scenario.rb" Hikyo-Org/Hikyo >"$fixture_dir/$scenario.out" 2>&1; then
		printf 'homebrew cask fixture: accepted channel confusion %s\n' "$scenario" >&2; exit 1
	fi
	[ ! -e "$fixture_dir/$scenario.rb" ]
done

cp "$bundle/release-manifest.json" "$fixture_dir/missing.json"
jq 'del(.artifacts[] | select(.name | contains("x86_64")))' \
	"$fixture_dir/missing.json" >"$fixture_dir/missing-one.json"
if "$(dirname "$0")/render-homebrew-cask.sh" \
	"$fixture_dir/missing-one.json" "$bundle" "$fixture_dir/missing.rb" Hikyo-Org/Hikyo \
	>"$fixture_dir/missing.out" 2>"$fixture_dir/missing.err"
then
	printf 'homebrew cask fixture: missing Intel archive unexpectedly accepted\n' >&2
	exit 1
fi
grep -F 'homebrew cask: expected one signed Darwin x86_64 disk image' \
	"$fixture_dir/missing.err" >/dev/null

# Integrity failures must never produce a new cask or strip quarantine.
for scenario in tampered symlink raw-archive; do
	manifest="$bundle/release-manifest.json"
	case "$scenario" in
		tampered) printf 'tampered\n' >>"$bundle/hikyo_1.2.3_Darwin_arm64.dmg" ;;
		symlink)
			printf 'arm64 archive\n' >"$fixture_dir/arm64.dmg"
			rm "$bundle/hikyo_1.2.3_Darwin_arm64.dmg"
			ln -s "$fixture_dir/arm64.dmg" "$bundle/hikyo_1.2.3_Darwin_arm64.dmg"
			;;
		raw-archive)
			jq '(.artifacts[].kind) = "binary"' "$manifest" >"$fixture_dir/raw.json"
			manifest="$fixture_dir/raw.json"
			;;
	esac
	if "$(dirname "$0")/render-homebrew-cask.sh" "$manifest" "$bundle" "$fixture_dir/$scenario.rb" Hikyo-Org/Hikyo >"$fixture_dir/$scenario.out" 2>"$fixture_dir/$scenario.err"; then
		printf 'homebrew cask fixture: accepted %s image\n' "$scenario" >&2; exit 1
	fi
	[ ! -e "$fixture_dir/$scenario.rb" ]
done
if grep -E 'quarantine|xattr|no-quarantine' "$output"; then
	printf 'homebrew cask fixture: cask bypasses Gatekeeper\n' >&2; exit 1
fi

printf 'homebrew cask fixture: verified DMGs rendered for both architectures; integrity failures refused\n'
