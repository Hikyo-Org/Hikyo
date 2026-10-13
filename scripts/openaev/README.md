# OpenAEV after release publication

The independent `.github/workflows/openaev-release.yml` validates an already
published release on `ubuntu-24.04-arm`. It never gates release publication.
Published stable releases and prereleases trigger through `release: published`.
Nightlies trigger after their exact successful `nightly-release` workflow has
finished publishing and signing the container. An unchanged nightly has no new
candidate and is skipped.

The runtime job creates a fresh Kubernetes cluster, OpenAEV services, native
Hikyo target, TLS and synthetic credentials. It needs no home-network access,
private registry, production credentials or long-lived lab state. All container
images and the injector source/dependencies are pinned. Application namespaces
have enforced network policies and node-level isolation; independent positive
controls establish that forbidden targets are reachable before negative probes
prove the application cannot reach them.

The suite retains 24 authorization, 22 adversarial, 260 bounded-request, six
load-safety and one detection-delivery observations. Readiness, resource identity,
85% memory ceilings, 30-second sample freshness and the native audit/Prometheus/
Alertmanager/inbox chain are required. A response from the HTTP injector alone
is never a pass.

The trusted workflow checkout prepares signed stable, RC, core-version
prerelease and nightly evidence with Hikyo's native verification and offline
bundle APIs. Released code is the test target, never the source of the harness
or preparation tool. The runtime image must contain the exact authenticated
Linux ARM64 binary. Stable images also bind to their signed manifest digest.

Only a complete 313/313 PASS, verified isolation, native binary binding and
successful removal of all owned target and stack resources can reach the
separate success publisher. That job checks the exact immutable release identity
again and creates a completed `OpenAEV release validation` success check on its
source commit, with the native proof and validation run link. It never edits
release notes or assets. On failure or cancellation it creates no extra check,
comment, badge or report. Raw output, response bodies,
audit attribution and synthetic custody are never uploaded as public artifacts.

## Rehearsal and development

Manual workflow dispatch accepts one exact published `tag`. Every manual run is
read-only and cannot invoke release publication. Once the workflow exists on
the default branch:

```sh
gh workflow run openaev-release.yml --ref main -f tag=v0.0.1-nightly.20261012.75.g868703c7
```

The command starts a fresh hosted run; it does not publish another release.
Old stable tags created before the workflow existed need this manual replay.

Local checks use Python 3.11 or newer and the repository's pinned Go toolchain:

```sh
python3 -m venv /tmp/hikyo-openaev-checks
/tmp/hikyo-openaev-checks/bin/pip install --require-hashes --only-binary=:all: -r scripts/openaev/requirements-check.lock
PATH=/tmp/hikyo-openaev-checks/bin:$PATH scripts/openaev/check.sh
go test ./scripts/openaev/native-prepare
go test ./scripts/ci -run '^TestOpenAEV' -count=1
scripts/ci/run-go-tool.sh actionlint .github/workflows/openaev-release.yml
```

A complete local lab additionally needs native ARM64 Linux Docker, at least the
combined service memory caps plus host/control-plane overhead, `kubectl`, OpenSSL,
Go, curl and Docker Buildx with the OCI exporter. The stack entry points are
`stack/start.sh STATE IMAGE_DIGEST`, `STATE/environment.sh`, and
`stack/stop.sh STATE`. Use the hosted workflow for measured full-lab evidence.

The old Mac LaunchAgent `com.went.hikyo-openaev-nightly` was unloaded and disabled
on 2026-10-13 to stop its failure-note publisher. Its historical evidence and the
persistent Pi lab were not removed. This repository workflow becomes automatic
after merging it into the default branch and publishing a subsequent candidate.

Implementation provenance: the assertions and regression coverage were ported
from `operations/openaev-lab` in the author's isolated lab, with all home-network
and production-baseline dependencies replaced by fresh hosted identities and
native controls. The suite does not claim to verify unchanged Pi production
workloads, production capacity, CVEs or vendor certification.
