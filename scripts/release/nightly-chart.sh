#!/bin/sh
# Publish a source-bound chart only after the canonical nightly image is verified.
set -eu
script_dir=$(CDPATH='' cd -- "$(dirname "$0")" && pwd)
# shellcheck disable=SC1091
. "$script_dir/../lib/release.sh"
[ "$#" -eq 0 ] || { printf 'usage: %s\n' "$0" >&2; exit 2; }
: "${REPOSITORY:?}" "${VERSION:?}" "${COMMIT:?}" "${IMAGE_DIGEST:?}" "${GITHUB_OUTPUT:?}"
fail() { printf 'nightly chart: %s\n' "$1" >&2; exit 1; }
[ "${GITHUB_REF:-}" = refs/heads/main ] && [ "${GITHUB_SHA:-}" = "$COMMIT" ] || fail 'require the exact main workflow source'
[ "$(git rev-parse HEAD)" = "$COMMIT" ] || fail 'checkout differs from the workflow source'
is_full_sha "$COMMIT"
is_digest "$IMAGE_DIGEST"
image=$(printf 'ghcr.io/%s' "$REPOSITORY" | tr '[:upper:]' '[:lower:]')
chart=$(printf 'ghcr.io/%s/charts/hikyo' "${REPOSITORY%/*}" | tr '[:upper:]' '[:lower:]')
identity="https://github.com/$REPOSITORY/.github/workflows/nightly.yml@refs/heads/main"
work=$(mktemp -d "${TMPDIR:-/tmp}/hikyo-nightly-chart.XXXXXX")
trap 'rm -rf "$work"' EXIT HUP INT TERM
verify_signature() {
	cosign verify --certificate-identity "$identity" \
		--certificate-oidc-issuer https://token.actions.githubusercontent.com \
		--certificate-github-workflow-sha "$COMMIT" "$1" >"$work/signatures.json"
}
digest_from_log() {
	[ "$(awk '$1 == "Digest:" {count++} END {print count+0}' "$1")" -eq 1 ] || fail 'registry returned an ambiguous digest'
	digest=$(awk '$1 == "Digest:" {print $2}' "$1")
	is_digest "$digest" || fail 'registry did not return exactly one immutable digest'
}
verify_signature "$image@$IMAGE_DIGEST"
python3 -B "$script_dir/nightly_chart.py" prepare --source chart/hikyo \
	--destination "$work/chart" --repository "$REPOSITORY" --version "$VERSION" \
	--commit "$COMMIT" --image-digest "$IMAGE_DIGEST"
helm lint "$work/chart" --set database.existingSecret=hikyo-database \
	--set rootKey.existingSecret=hikyo-root-key --set upgrade.existingClaim=hikyo-upgrade-public \
	--set upgrade.stateExistingClaim=hikyo-upgrade-state --set externalOrigin=https://hikyo.example.com \
	--set 'network.trustedProxyCIDRs={10.42.0.0/16}' >/dev/null
helm template fixture "$work/chart" --set database.existingSecret=hikyo-database \
	--set rootKey.existingSecret=hikyo-root-key --set upgrade.existingClaim=hikyo-upgrade-public \
	--set upgrade.stateExistingClaim=hikyo-upgrade-state --set externalOrigin=https://hikyo.example.com \
	--set 'network.trustedProxyCIDRs={10.42.0.0/16}' >"$work/rendered.yaml"
python3 -B "$script_dir/nightly_chart.py" check-rendered "$work/rendered.yaml" \
	--repository "$REPOSITORY" --image-digest "$IMAGE_DIGEST"
helm package "$work/chart" --destination "$work" >/dev/null
package="$work/hikyo-$VERSION.tgz"
mkdir "$work/existing" "$work/readback"
fresh=false
if helm pull "oci://$chart" --version "$VERSION" --destination "$work/existing" >"$work/lookup.log" 2>&1; then
	digest_from_log "$work/lookup.log"
	# Interrupted unsigned uploads are not granted fresh source authority.
	verify_signature "$chart@$digest"
	python3 -B "$script_dir/nightly_chart.py" compare "$package" "$work/existing/hikyo-$VERSION.tgz"
elif ! grep -Eiq 'unauthorized|denied|forbidden|timeout|deadline exceeded' "$work/lookup.log" &&
	{ grep -Eiq 'manifest unknown|MANIFEST_UNKNOWN|unexpected status.*404 Not Found' "$work/lookup.log" ||
		grep -Fx "Error: $chart:$VERSION: not found" "$work/lookup.log" >/dev/null ||
		grep -Fx "Error: failed to perform \"FetchReference\" on source: $chart:$VERSION: not found" "$work/lookup.log" >/dev/null; }; then
	helm push "$package" "oci://${chart%/*}" >"$work/push.log" 2>&1 || { cat "$work/push.log" >&2; fail 'chart push failed'; }
	digest_from_log "$work/push.log"
	fresh=true
else
	cat "$work/lookup.log" >&2
	fail 'cannot prove immutable chart is absent'
fi
chart_digest=$digest
helm pull "oci://$chart@$chart_digest" --destination "$work/readback" >"$work/readback.log" 2>&1
digest_from_log "$work/readback.log"
[ "$digest" = "$chart_digest" ] || fail 'immutable chart readback digest mismatch'
python3 -B "$script_dir/nightly_chart.py" compare "$package" "$work/readback/hikyo-$VERSION.tgz"
if [ "$fresh" = true ]; then
	cosign sign --yes --use-signing-config=false --rekor-url=https://rekor.sigstore.dev "$chart@$chart_digest"
	verify_signature "$chart@$chart_digest"
fi
if [ -n "${GITHUB_STEP_SUMMARY:-}" ]; then
	printf '### Verified nightly chart\n\nChart: %s:%s\n\nDigest: %s\n\nImage: %s@%s\n\nSource: %s\n\n' \
		"$chart" "$VERSION" "$chart_digest" "$image" "$IMAGE_DIGEST" "$COMMIT" >>"$GITHUB_STEP_SUMMARY"
fi
# Override both credential locations: an authenticated pull does not prove that
# an ordinary anonymous Helm client can retrieve the published package.
mkdir -p "$work/anonymous/docker" "$work/anonymous/download"
printf '{"auths":{}}\n' >"$work/anonymous/registry.json"
printf '{"auths":{}}\n' >"$work/anonymous/docker/config.json"
if ! DOCKER_CONFIG="$work/anonymous/docker" helm pull "oci://$chart" --version "$VERSION" \
	--registry-config "$work/anonymous/registry.json" --destination "$work/anonymous/download" >"$work/public.log" 2>&1; then
	cat "$work/public.log" >&2
	fail 'signed chart is not anonymously retrievable; check package visibility and rerun after repair'
fi
digest_from_log "$work/public.log"
[ "$digest" = "$chart_digest" ] || fail 'anonymous chart readback digest mismatch'
python3 -B "$script_dir/nightly_chart.py" compare "$package" "$work/anonymous/download/hikyo-$VERSION.tgz"
DOCKER_CONFIG="$work/anonymous/docker" verify_signature "$chart@$chart_digest"
DOCKER_CONFIG="$work/anonymous/docker" verify_signature "$image@$IMAGE_DIGEST"
printf 'chart=%s\ndigest=%s\n' "$chart" "$chart_digest" >>"$GITHUB_OUTPUT"
printf 'nightly chart: %s@%s verified and anonymously retrievable\n' "$chart" "$chart_digest"
