#!/bin/sh
set -eu

fixture_dir=$(mktemp -d "${TMPDIR:-/tmp}/hikyo-homebrew-cask.XXXXXX")
trap 'rm -rf "$fixture_dir"' EXIT HUP INT TERM
bundle="$fixture_dir/bundle"
mkdir -p "$bundle"

printf 'arm64 archive\n' >"$bundle/hikyo_1.2.3_Darwin_arm64.tar.gz"
printf 'intel archive\n' >"$bundle/hikyo_1.2.3_Darwin_x86_64.tar.gz"
arm_sha=$(shasum -a 256 "$bundle/hikyo_1.2.3_Darwin_arm64.tar.gz" | awk '{print $1}')
intel_sha=$(shasum -a 256 "$bundle/hikyo_1.2.3_Darwin_x86_64.tar.gz" | awk '{print $1}')
jq -n --arg arm_sha "$arm_sha" --arg intel_sha "$intel_sha" '{
	schema: "hikyo.dev/release-manifest/v1",
	version: "1.2.3",
	tag: "v1.2.3",
	artifacts: [
		{name: "hikyo_1.2.3_Darwin_arm64.tar.gz", kind: "binary", sha256: $arm_sha},
		{name: "hikyo_1.2.3_Darwin_x86_64.tar.gz", kind: "binary", sha256: $intel_sha}
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
grep -Fx '  url "https://github.com/Hikyo-Org/Hikyo/releases/download/v#{version}/hikyo_#{version}_Darwin_#{arch}.tar.gz"' "$output" >/dev/null
grep -Fx '  binary "hikyo"' "$output" >/dev/null

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
grep -F 'homebrew cask: expected one signed Darwin x86_64 archive' \
	"$fixture_dir/missing.err" >/dev/null

printf 'homebrew cask fixture: verified release archives rendered for both macOS architectures\n'
