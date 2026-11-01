#!/bin/sh
# Exercise real Helm packaging/rendering with synthetic registry and OIDC edges.
set -eu
script_dir=$(CDPATH='' cd -- "$(dirname "$0")" && pwd)
work=$(mktemp -d "${TMPDIR:-/tmp}/hikyo-nightly-chart-test.XXXXXX")
trap 'rm -rf "$work"' EXIT HUP INT TERM
REAL_HELM=$(command -v helm)
export REAL_HELM
mkdir "$work/bin"
export FIXTURE="$work" REPOSITORY=Hikyo-Org/Hikyo
export COMMIT=1234567812345678123456781234567812345678
export VERSION=0.0.1-nightly.20261031.1.g12345678
export IMAGE_DIGEST=sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
export CHART_DIGEST=sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
export GITHUB_OUTPUT="$work/output" GITHUB_STEP_SUMMARY="$work/summary"
export GITHUB_REF=refs/heads/main GITHUB_SHA="$COMMIT"
cat >"$work/bin/git" <<'SH'
#!/bin/sh
set -eu
[ "$*" = 'rev-parse HEAD' ]
printf '%s\n' "${CHECKOUT_COMMIT:-$COMMIT}"
SH
cat >"$work/bin/cosign" <<'SH'
#!/bin/sh
set -eu
printf 'cosign %s\n' "$*" >>"$FIXTURE/log"
case "$1" in
verify)
	[ "$2" = --certificate-identity ] && [ "$3" = "https://github.com/$REPOSITORY/.github/workflows/nightly.yml@refs/heads/main" ]
	[ "$4" = --certificate-oidc-issuer ] && [ "$5" = https://token.actions.githubusercontent.com ]
	[ "$6" = --certificate-github-workflow-sha ] && [ "$7" = "$COMMIT" ]
	case "$8" in
	"ghcr.io/hikyo-org/hikyo@$IMAGE_DIGEST") [ "${FAIL_IMAGE:-false}" = false ] ;;
	"ghcr.io/hikyo-org/charts/hikyo@$CHART_DIGEST") [ "${SIGNED:-true}" = true ] || [ -f "$FIXTURE/signed" ] ;;
	*) exit 91 ;;
	esac
	;;
sign)
	[ "$2" = --yes ] && [ "$3" = --use-signing-config=false ] && [ "$4" = --rekor-url=https://rekor.sigstore.dev ]
	[ "$5" = "ghcr.io/hikyo-org/charts/hikyo@$CHART_DIGEST" ]
	[ "${FAIL_SIGN:-false}" = false ]
	touch "$FIXTURE/signed"
	;;
*) exit 92 ;;
esac
SH
cat >"$work/bin/helm" <<'SH'
#!/bin/sh
set -eu
printf 'helm %s\n' "$*" >>"$FIXTURE/log"
case "$1" in
lint | template) exec "$REAL_HELM" "$@" ;;
package)
	"$REAL_HELM" "$@"
	cp "$4/hikyo-$VERSION.tgz" "$FIXTURE/prepared.tgz"
	;;
push)
	[ "$3" = oci://ghcr.io/hikyo-org/charts ]
	cp "$2" "$FIXTURE/pushed.tgz"
	printf 'Pushed: ghcr.io/hikyo-org/charts/hikyo:%s\nDigest: %s\n' "$VERSION" "$CHART_DIGEST" >&2
	;;
pull)
	ref=$2
	shift 2
	destination= anonymous=false
	while [ "$#" -gt 0 ]; do
		case "$1" in
		--version) [ "$2" = "$VERSION" ]; shift 2 ;;
		--destination) destination=$2; shift 2 ;;
		--registry-config)
			jq -e '.auths == {}' "$2" >/dev/null
			jq -e '.auths == {}' "$DOCKER_CONFIG/config.json" >/dev/null
			anonymous=true; shift 2 ;;
		*) exit 93 ;;
		esac
	done
	if [ "$ref" = oci://ghcr.io/hikyo-org/charts/hikyo ] && [ "$anonymous" = false ]; then
		case "${LOOKUP:-missing-helm4}" in
		missing) printf 'Error: ghcr.io/hikyo-org/charts/hikyo:%s: not found\n' "$VERSION" >&2; exit 1 ;;
		missing-helm4) printf 'Error: failed to perform "FetchReference" on source: ghcr.io/hikyo-org/charts/hikyo:%s: not found\n' "$VERSION" >&2; exit 1 ;;
		unrelated-missing) printf 'Error: failed to perform "FetchReference" on source: ghcr.io/unrelated/chart:%s: not found\n' "$VERSION" >&2; exit 1 ;;
		denied) echo '403 Forbidden: manifest unknown' >&2; exit 1 ;;
		timeout) echo 'deadline exceeded' >&2; exit 1 ;;
		network) echo 'network not found' >&2; exit 1 ;;
		existing) ;;
		*) exit 94 ;;
		esac
	elif [ "$anonymous" = false ]; then
		[ "$ref" = "oci://ghcr.io/hikyo-org/charts/hikyo@$CHART_DIGEST" ]
	fi
	if [ "$anonymous" = true ] && [ "${PUBLIC:-true}" != true ]; then
		echo '403 Forbidden: private package' >&2; exit 1
	fi
	cp "$FIXTURE/prepared.tgz" "$destination/hikyo-$VERSION.tgz"
	if [ "${MISMATCH:-false}" = true ] || { [ "$anonymous" = true ] && [ "${PUBLIC_MISMATCH:-false}" = true ]; }; then
		printf 'conflicting chart bytes\n' >"$destination/hikyo-$VERSION.tgz"
	fi
	printf 'Pulled: %s\nDigest: %s\n' "$ref" "${READBACK_DIGEST:-$CHART_DIGEST}" >&2
	if [ "${DUPLICATE_DIGEST:-false}" = true ]; then printf 'Digest: %s\n' "$CHART_DIGEST" >&2; fi
	;;
*) exit 95 ;;
esac
SH
chmod +x "$work/bin/"*
export PATH="$work/bin:$PATH"
reset_fixture() { rm -f "$work/log" "$work/output" "$work/signed" "$work/pushed.tgz"; }
refuse() {
	name=$1
	shift
	reset_fixture
	if env "$@" "$script_dir/nightly-chart.sh" >"$work/error" 2>&1; then
		printf 'nightly chart fixture accepted %s\n' "$name" >&2; exit 1
	fi
	[ ! -s "$work/output" ]
}
# The packaged nightly's optional policy must select this source, rather than
# the stable tag signer or an arbitrary main-branch image.
python3 -B "$script_dir/nightly_chart.py" prepare --source chart/hikyo \
	--destination "$work/admission-chart" --repository "$REPOSITORY" --version "$VERSION" \
	--commit "$COMMIT" --image-digest "$IMAGE_DIGEST"
render_admission() {
	"$REAL_HELM" template fixture "$work/admission-chart" \
	--set database.existingSecret=fixture --set rootKey.existingSecret=fixture-root \
	--set upgrade.existingClaim=fixture-public --set upgrade.stateExistingClaim=fixture-state \
	--set externalOrigin=https://hikyo.example.com --set 'network.trustedProxyCIDRs={10.42.0.0/16}' \
	--set releaseTrust.admissionPolicy.enabled=true
}
render_admission >"$work/admission.yaml"
python3 - "$work/admission.yaml" <<'PY'
import os
import sys
import yaml

policy = next(doc for doc in yaml.safe_load_all(open(sys.argv[1], encoding="utf-8"))
              if doc and doc.get("kind") == "ClusterPolicy")
keyless = policy["spec"]["rules"][0]["verifyImages"][0]["attestors"][0]["entries"][0]["keyless"]
assert keyless == {
    "issuer": "https://token.actions.githubusercontent.com",
    "subject": "https://github.com/Hikyo-Org/Hikyo/.github/workflows/nightly.yml@refs/heads/main",
    "additionalExtensions": {"githubWorkflowSha": os.environ["COMMIT"]},
    "rekor": {"url": "https://rekor.sigstore.dev"},
}
PY
python3 - "$work/admission-chart/Chart.yaml" <<'PY'
from pathlib import Path
import sys
import yaml

path = Path(sys.argv[1])
metadata = yaml.safe_load(path.read_text())
metadata["annotations"]["org.opencontainers.image.revision"] = "unknown"
path.write_text(yaml.safe_dump(metadata))
PY
if render_admission >"$work/refused.yaml" 2>"$work/refused.error"; then
	echo 'nightly admission accepted an unbound source revision' >&2; exit 1
fi
reset_fixture
SIGNED=false "$script_dir/nightly-chart.sh" >"$work/result" 2>&1
grep -Fx "digest=$CHART_DIGEST" "$work/output" >/dev/null
grep -F 'cosign sign ' "$work/log" >/dev/null
grep -F -- '--registry-config ' "$work/log" >/dev/null
reset_fixture
LOOKUP=missing SIGNED=false "$script_dir/nightly-chart.sh" >"$work/result" 2>&1
grep -Fx "digest=$CHART_DIGEST" "$work/output" >/dev/null
reset_fixture
LOOKUP=existing "$script_dir/nightly-chart.sh" >"$work/result" 2>&1
if grep -E 'cosign sign |helm push ' "$work/log" >/dev/null; then
	echo 'matching signed chart was overwritten or re-signed' >&2; exit 1
fi
for lookup in denied timeout network unrelated-missing; do
	refuse "$lookup lookup" LOOKUP="$lookup"
	if grep -E 'cosign sign |helm push ' "$work/log" >/dev/null; then exit 1; fi
done
refuse 'unsigned existing chart' LOOKUP=existing SIGNED=false
if grep -E 'cosign sign |helm push ' "$work/log" >/dev/null; then exit 1; fi
refuse 'conflicting signed chart' LOOKUP=existing MISMATCH=true
if grep -E 'cosign sign |helm push ' "$work/log" >/dev/null; then exit 1; fi
refuse 'wrong image signature' FAIL_IMAGE=true
if grep -F 'helm ' "$work/log" >/dev/null; then exit 1; fi
refuse 'changed checkout' CHECKOUT_COMMIT=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
refuse 'non-main workflow' GITHUB_REF=refs/heads/feature
refuse 'unrelated version' VERSION=0.0.1-nightly.20261031.1.g87654321
refuse 'duplicate registry digest' DUPLICATE_DIGEST=true
refuse 'changed readback digest' READBACK_DIGEST=sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc
refuse 'private package' PUBLIC=false
refuse 'different anonymous readback' PUBLIC_MISMATCH=true
refuse 'failed signing' FAIL_SIGN=true SIGNED=false
# An interrupted unsigned upload cannot be laundered by a rerun.
refuse 'unsigned upload on rerun' LOOKUP=existing SIGNED=false
printf 'Nightly chart: real Helm packages/rendering, immutable reuse, refusal paths and anonymous readback verified\n'
