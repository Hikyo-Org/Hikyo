#!/bin/sh
set -eu

script_dir=$(CDPATH='' cd -- "$(dirname "$0")" && pwd)
# shellcheck disable=SC1091
. "$script_dir/../lib/release.sh"

if [ "$#" -ne 4 ]; then
	printf 'usage: %s DIST IMAGE_ROOT EXPECTED_COMMIT GORELEASER_CONFIG\n' "$0" >&2
	exit 2
fi

dist=$1
image_root=$2
expected_commit=$3
config=$4
artifacts=$dist/artifacts.json
metadata=$dist/metadata.json
provenance=$dist/binary-provenance.json

is_full_sha "$expected_commit" || {
	printf 'prepare image root: expected commit must be a full SHA\n' >&2
	exit 2
}
[ -f "$artifacts" ] || { printf 'prepare image root: missing %s\n' "$artifacts" >&2; exit 2; }
[ -f "$metadata" ] || { printf 'prepare image root: missing %s\n' "$metadata" >&2; exit 2; }
[ -f "$config" ] || { printf 'prepare image root: missing %s\n' "$config" >&2; exit 2; }

version=$(jq -er '.version | select(type == "string" and length > 0)' "$metadata") || {
	printf 'prepare image root: metadata has no version\n' >&2
	exit 1
}
is_semver "$version" || {
	printf 'prepare image root: invalid archive version\n' >&2
	exit 1
}
commit=$(jq -er '.commit | select(type == "string")' "$metadata") || {
	printf 'prepare image root: metadata has no commit\n' >&2
	exit 1
}
[ "$commit" = "$expected_commit" ] || {
	printf 'prepare image root: GoReleaser commit %s does not match candidate %s\n' \
		"$commit" "$expected_commit" >&2
	exit 1
}

dist_dir=$(CDPATH='' cd -P -- "$dist" && pwd)
config_sha=$(sha256_file "$config")
scratch=$(mktemp -d "${TMPDIR:-/tmp}/hikyo-image-root.XXXXXX")
trap 'rm -rf "$scratch"' EXIT HUP INT TERM
: >"$scratch/packages.jsonl"

for arch in amd64 arm64; do
	source=$(jq -er --arg arch "$arch" '
		[.[] | select(
			.type == "Binary" and
			.goos == "linux" and
			.goarch == $arch and
			.extra.ID == "hikyo"
		)] |
		if length == 1 then .[0].path
		else error("expected exactly one GoReleaser binary for linux/" + $arch)
		end
	' "$artifacts") || {
		printf 'prepare image root: expected exactly one GoReleaser binary for linux/%s\n' "$arch" >&2
		exit 1
	}
	[ -f "$source" ] && [ ! -L "$source" ] || {
		printf 'prepare image root: invalid canonical binary %s\n' "$source" >&2
		exit 1
	}
	source_dir=$(CDPATH='' cd -P -- "$(dirname "$source")" && pwd)
	case "$source_dir/" in
		"$dist_dir"/*) ;;
		*) printf 'prepare image root: binary escapes dist: %s\n' "$source" >&2; exit 1 ;;
	esac

	name_arch=$arch
	[ "$arch" != amd64 ] || name_arch=x86_64
	archive=$dist/hikyo_${version}_Linux_$name_arch.tar.gz
	[ -f "$archive" ] && [ ! -L "$archive" ] || {
		printf 'prepare image root: missing regular linux/%s archive\n' "$arch" >&2
		exit 1
	}
	payload=$scratch/$arch
	mkdir "$payload"
	# Refuse traversal, duplicate members, links and special files before any
	# extraction. Canonical GoReleaser archives contain these two regular files.
	tar -tzf "$archive" >"$scratch/entries"
	LC_ALL=C sort "$scratch/entries" >"$scratch/sorted-entries"
	printf 'LICENSE\nhikyo\n' >"$scratch/expected-entries"
	cmp -s "$scratch/sorted-entries" "$scratch/expected-entries" || {
		printf 'prepare image root: unexpected linux/%s archive members\n' "$arch" >&2
		exit 1
	}
	LC_ALL=C tar -tvzf "$archive" >"$scratch/member-types"
	awk 'substr($1, 1, 1) != "-" { exit 1 } END { if (NR != 2) exit 1 }' "$scratch/member-types" || {
		printf 'prepare image root: linux/%s archive members must be regular files\n' "$arch" >&2
		exit 1
	}
	tar -xzf "$archive" -C "$payload"
	for member in LICENSE hikyo; do
		[ -f "$payload/$member" ] && [ ! -L "$payload/$member" ] || {
			printf 'prepare image root: invalid linux/%s archive member %s\n' "$arch" "$member" >&2
			exit 1
		}
	done
	[ -x "$payload/hikyo" ] || {
		printf 'prepare image root: linux/%s archive binary is not executable\n' "$arch" >&2
		exit 1
	}
	cmp "$payload/hikyo" "$source" >/dev/null || {
		printf 'prepare image root: linux/%s archive binary differs from GoReleaser output\n' "$arch" >&2
		exit 1
	}
done

# Every architecture must pass before any OCI input or provenance is written.
# Copy from the archive itself, including its executable permissions.
for arch in amd64 arm64; do
	canonical=$scratch/$arch/hikyo
	target_dir=$image_root/$arch
	target=$target_dir/hikyo
	mkdir -p "$target_dir"
	cp -p "$canonical" "$target"
	cmp "$canonical" "$target" >/dev/null || {
		printf 'prepare image root: copied linux/%s binary differs from archive\n' "$arch" >&2
		exit 1
	}
	archive_sha=$(sha256_file "$canonical")
	oci_sha=$(sha256_file "$target")
	[ "$archive_sha" = "$oci_sha" ] || {
		printf 'prepare image root: linux/%s archive and OCI input hashes differ\n' "$arch" >&2
		exit 1
	}
	printf 'prepare image root: linux/%s sha256=%s archive_input=oci_input\n' \
		"$arch" "$archive_sha"

	jq -nc \
		--arg goarch "$arch" \
		--arg archive_sha "$archive_sha" \
		--arg oci_path "image-root/$arch/hikyo" \
		--arg oci_sha "$oci_sha" '
		{
			goos: "linux",
			goarch: $goarch,
			archive_input: {build_id: "hikyo", sha256: $archive_sha},
			oci_input: {path: $oci_path, sha256: $oci_sha}
		}' >>"$scratch/packages.jsonl"
done

jq -s \
	--arg commit "$commit" \
	--arg version "$version" \
	--arg config_sha "$config_sha" '
	{
		schema: "hikyo.dev/release-binaries/v1",
		source_commit: $commit,
		version: $version,
		producer: {
			name: "goreleaser",
			build_id: "hikyo",
			config: ".goreleaser.yaml",
			config_sha256: $config_sha
		},
		packages: .
	}' "$scratch/packages.jsonl" >"$provenance"

printf 'prepare image root: canonical amd64/arm64 binaries copied; provenance %s\n' "$provenance"
