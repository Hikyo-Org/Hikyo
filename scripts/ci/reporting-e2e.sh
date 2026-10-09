#!/usr/bin/env bash
# Delivery-target condition reporting from a real operator to a real browser
# (#791). Provisions an ephemeral kind cluster, builds the SPA, and runs the
# Go test that starts the server and the operator and drives Playwright.
#
# The cluster is created fresh and deleted in a trap. It never adopts or
# deletes a pre-existing cluster of the same name: a parallel session might
# own it.
#
# Screenshots, the operator log and the captured report bodies land in
# HIKYO_REPORTING_E2E_ARTIFACTS (default web/test-results/reporting-e2e).
# Chromium must already be installed: `pnpm --dir web run e2e:install`.
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"

CLUSTER=hikyo-reporting-e2e
# The node image scripts/ci/k8s-e2e.sh pins, for the same reason.
NODE_IMAGE="mirror.gcr.io/kindest/node:v1.36.1@sha256:3489c7674813ba5d8b1a9977baea8a6e553784dab7b84759d1014dbd78f7ebd5"

for tool in docker kind go pnpm; do
	command -v "$tool" >/dev/null || { echo "reporting-e2e: missing $tool" >&2; exit 1; }
done
if kind get clusters 2>/dev/null | grep -qx "$CLUSTER"; then
	echo "reporting-e2e: a kind cluster named '$CLUSTER' already exists; refusing to reuse or delete it" >&2
	exit 1
fi

kubeconfig="$(mktemp -t hikyo-reporting-e2e-kubeconfig.XXXXXX)"
created=false
cleanup() {
	if [ "$created" = true ]; then
		kind delete cluster --name "$CLUSTER" >/dev/null 2>&1 || true
	fi
	rm -f "$kubeconfig"
}
trap cleanup EXIT HUP INT TERM

echo "reporting-e2e: creating kind cluster '$CLUSTER' ($NODE_IMAGE)"
kind create cluster --name "$CLUSTER" --image "$NODE_IMAGE" --kubeconfig "$kubeconfig" --wait 120s
created=true

echo "reporting-e2e: building the SPA the server serves"
./scripts/ci/build-spa.sh

export HIKYO_K8S_E2E_KUBECONFIG="$kubeconfig"
export HIKYO_REPORTING_E2E_ARTIFACTS="${HIKYO_REPORTING_E2E_ARTIFACTS:-$PWD/web/test-results/reporting-e2e}"
echo "reporting-e2e: running the operator, server and browser suite"
go test -count=1 -tags k8se2e -run '^TestK8sReportingBrowser$' ./internal/isolation/ -timeout 20m -v
echo "reporting-e2e: passed; evidence in $HIKYO_REPORTING_E2E_ARTIFACTS"
