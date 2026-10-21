#!/bin/sh
set -eu

script_dir=$(CDPATH='' cd -- "$(dirname "$0")" && pwd)
fixture_dir=$(mktemp -d "${TMPDIR:-/tmp}/hikyo-image-root-fixture.XXXXXX")
trap 'rm -rf "$fixture_dir"' EXIT HUP INT TERM
commit=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
version=0.0.0-snapshot-aaaaaaaa

fixture() {
	case_dir=$fixture_dir/$1
	dist=$case_dir/dist
	image_root=$case_dir/image-root
	config=$case_dir/goreleaser.yaml
	mkdir -p "$dist/hikyo_linux_amd64_v1" "$dist/hikyo_linux_arm64_v8.0"
	printf 'amd64 canonical binary\n' >"$dist/hikyo_linux_amd64_v1/hikyo"
	printf 'arm64 canonical binary\n' >"$dist/hikyo_linux_arm64_v8.0/hikyo"
	for arch in amd64 arm64; do
		payload=$case_dir/$arch
		mkdir "$payload"
		printf 'license fixture\n' >"$payload/LICENSE"
		printf '%s canonical binary\n' "$arch" >"$payload/hikyo"
		chmod 755 "$payload/hikyo"
		name_arch=$arch
		[ "$arch" != amd64 ] || name_arch=x86_64
		COPYFILE_DISABLE=1 tar -czf "$dist/hikyo_${version}_Linux_$name_arch.tar.gz" -C "$payload" LICENSE hikyo
	done
	# Only the archive carries executable permissions. The OCI root must inherit
	# those permissions rather than the mode of the loose GoReleaser fixture.
	chmod 600 "$dist/hikyo_linux_amd64_v1/hikyo" "$dist/hikyo_linux_arm64_v8.0/hikyo"
	printf 'builds:\n  - id: hikyo\n' >"$config"
	jq -n --arg commit "$commit" --arg version "$version" \
		'{commit: $commit, version: $version}' >"$dist/metadata.json"
	jq -n \
		--arg amd64 "$dist/hikyo_linux_amd64_v1/hikyo" \
		--arg arm64 "$dist/hikyo_linux_arm64_v8.0/hikyo" \
		'[
			{name:"hikyo", path:$amd64, goos:"linux", goarch:"amd64", type:"Binary", extra:{ID:"hikyo"}},
			{name:"hikyo", path:$arm64, goos:"linux", goarch:"arm64", type:"Binary", extra:{ID:"hikyo"}}
		]' >"$dist/artifacts.json"
}

refuse() {
	if "$script_dir/prepare-image-root.sh" "$dist" "$image_root" "$commit" "$config" \
		>"$case_dir/refused.out" 2>"$case_dir/refused.err"; then
		printf 'prepare image root fixture: %s unexpectedly accepted\n' "$1" >&2
		exit 1
	fi
	if [ -e "$image_root" ] || [ -e "$dist/binary-provenance.json" ]; then
		printf 'prepare image root fixture: %s wrote OCI inputs or provenance before refusal\n' "$1" >&2
		exit 1
	fi
}

fixture accepted
"$script_dir/prepare-image-root.sh" "$dist" "$image_root" "$commit" "$config" >/dev/null
cmp "$dist/hikyo_linux_amd64_v1/hikyo" "$image_root/amd64/hikyo"
cmp "$dist/hikyo_linux_arm64_v8.0/hikyo" "$image_root/arm64/hikyo"
[ -x "$image_root/amd64/hikyo" ] && [ -x "$image_root/arm64/hikyo" ]
config_sha=$(sha256sum "$config" 2>/dev/null | awk '{print $1}' || shasum -a 256 "$config" | awk '{print $1}')
amd64_sha=$(shasum -a 256 "$case_dir/amd64/hikyo" | awk '{print $1}')
arm64_sha=$(shasum -a 256 "$case_dir/arm64/hikyo" | awk '{print $1}')
jq -e \
	--arg commit "$commit" \
	--arg version "$version" \
	--arg config_sha "$config_sha" \
	--arg amd64_sha "$amd64_sha" --arg arm64_sha "$arm64_sha" '
	.schema == "hikyo.dev/release-binaries/v1" and
	.source_commit == $commit and
	.version == $version and
	.producer == {
		name: "goreleaser",
		build_id: "hikyo",
		config: ".goreleaser.yaml",
		config_sha256: $config_sha
	} and
	([.packages[] | select(
		.goos == "linux" and
		((.goarch == "amd64" and .archive_input.sha256 == $amd64_sha) or
		 (.goarch == "arm64" and .archive_input.sha256 == $arm64_sha)) and
		.archive_input.sha256 == .oci_input.sha256
	)] | length) == 2
' "$dist/binary-provenance.json" >/dev/null

for mutation in loose-amd64 loose-arm64 changed-archive symlink-member hardlink-member duplicate-member missing-member missing-license extra-member nonexecutable missing-archive symlink-archive duplicate-architecture escaped-binary; do
	fixture "$mutation"
	archive=$dist/hikyo_${version}_Linux_x86_64.tar.gz
	payload=$case_dir/amd64
	case "$mutation" in
		loose-amd64) printf 'modified loose amd64 binary\n' >"$dist/hikyo_linux_amd64_v1/hikyo" ;;
		loose-arm64) printf 'modified loose arm64 binary\n' >"$dist/hikyo_linux_arm64_v8.0/hikyo" ;;
		changed-archive)
			printf 'modified archive binary\n' >"$payload/hikyo"
			COPYFILE_DISABLE=1 tar -czf "$archive" -C "$payload" LICENSE hikyo
			;;
		symlink-member)
			rm "$payload/hikyo"
			ln -s LICENSE "$payload/hikyo"
			COPYFILE_DISABLE=1 tar -czf "$archive" -C "$payload" LICENSE hikyo
			;;
		hardlink-member)
			# Equal bytes and executable mode isolate the forbidden hardlink type.
			cp "$dist/hikyo_linux_amd64_v1/hikyo" "$payload/LICENSE"
			chmod 755 "$payload/LICENSE"
			rm "$payload/hikyo"
			ln "$payload/LICENSE" "$payload/hikyo"
			COPYFILE_DISABLE=1 tar -czf "$archive" -C "$payload" LICENSE hikyo
			;;
		duplicate-member) COPYFILE_DISABLE=1 tar -czf "$archive" -C "$payload" LICENSE hikyo hikyo ;;
		missing-member) COPYFILE_DISABLE=1 tar -czf "$archive" -C "$payload" LICENSE ;;
		missing-license) COPYFILE_DISABLE=1 tar -czf "$archive" -C "$payload" hikyo ;;
		extra-member)
			printf 'unexpected member\n' >"$payload/extra"
			COPYFILE_DISABLE=1 tar -czf "$archive" -C "$payload" LICENSE hikyo extra
			;;
		nonexecutable)
			chmod 600 "$payload/hikyo"
			COPYFILE_DISABLE=1 tar -czf "$archive" -C "$payload" LICENSE hikyo
			;;
		missing-archive) rm "$archive" ;;
		symlink-archive)
			mv "$archive" "$case_dir/archive.tar.gz"
			ln -s "$case_dir/archive.tar.gz" "$archive"
			;;
		duplicate-architecture)
			jq '.[1] = .[0] | .[1].path = (.[0].path + ".duplicate")' \
				"$dist/artifacts.json" >"$case_dir/duplicate.json"
			cp "$dist/hikyo_linux_amd64_v1/hikyo" "$dist/hikyo_linux_amd64_v1/hikyo.duplicate"
			cp "$case_dir/duplicate.json" "$dist/artifacts.json"
			;;
		escaped-binary)
			cp "$payload/hikyo" "$case_dir/hikyo"
			jq --arg path "$case_dir/hikyo" '.[0].path = $path' "$dist/artifacts.json" >"$case_dir/escaped.json"
			cp "$case_dir/escaped.json" "$dist/artifacts.json"
			;;
	esac
	refuse "$mutation"
	if [ "$mutation" = duplicate-architecture ]; then
		grep -F 'expected exactly one GoReleaser binary for linux/amd64' "$case_dir/refused.err" >/dev/null
	fi
done

printf 'prepare image root fixture: canonical archive bytes, executable permissions and provenance verified; 14 altered or malformed inputs refused before OCI writes\n'
