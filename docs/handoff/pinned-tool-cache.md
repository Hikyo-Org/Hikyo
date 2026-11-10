# Pinned tool cache and backoff

## Problem

GitHub Actions jobs failed when GitHub release downloads returned transient 504s.
Confirmed failing URLs came from these runs: 42579206519, 42583876110, 42589120553
(that run also had an unrelated `BootstrapBoundaries` test failure, since fixed on
main), 42466934891 and 42466676752.

- `github.com/sigstore/cosign/releases`, inside `sigstore/cosign-installer`, which
  retries only three times with 1, 2 and 4 second waits.
- `github.com/koalaman/shellcheck/releases` in the `ci.yml` lint job.
- `github.com/kubernetes-sigs/kind/releases` `kind-linux-amd64` (`k8s-e2e`) and
  `kind-linux-arm64` (floor bench measurement).

The existing steps used `--retry 4 --retry-delay 2 --retry-all-errors`. A fixed
`--retry-delay` disables curl's exponential doubling, so the whole window was
about eight seconds. The outage lasted over a minute, and the Actions cache
service itself also returned 504s. The `ci-feedback.yml` ShellCheck download had
no retry at all, and `chart-kind.sh` fetched the Calico manifest without one.

## Design

1. `scripts/ci/pinned-tools/<name>.pin` is the single source of truth for each
   artifact: `url=` and `sha256=` lines only.
2. `scripts/ci/fetch-pinned-tool.sh <name> <output>` verifies a cached copy,
   otherwise downloads with `--retry 8 --retry-all-errors --retry-max-time 300
   --connect-timeout 20 --max-time 120` and no `--retry-delay`. The checksum is
   always verified, whether the file came from cache or network. A corrupt cache
   entry is discarded and re-fetched; a mismatching download fails closed.
3. `.github/workflows/ci-tool-cache.yml` (`cache-seed-tools`) is the only writer,
   trusted main only, daily and on dispatch, modeled on `ci-race-cache.yml`.
4. Readers restore the entry restore-only (same path and key string, no
   `restore-keys`, no `fail-on-cache-miss`) and then call the helper.
5. Backoff only, no cache: `floor-bench.yml` and `release-acceptance.yml`
   (their policy forbids caches; `kind-linux-arm64`, `helm-linux-arm64`), and the
   privileged Cosign and Helm installs in `release.yml`, `nightly.yml`,
   `security-feed.yml` and `security-release.yml`. The privileged jobs run after a
   checkout of trusted code (a tag or main). `floor-bench.yml` checks out the
   candidate revision under validation, so it runs that revision's helper
   read-only and without secrets; it already runs that revision's scripts.
6. Cosign v3.1.3 is installed to `$HOME/.cosign/cosign` (the action's default
   directory), replacing `sigstore/cosign-installer`. The pinned checksum equals
   the release's `cosign_checksums.txt`, and `cosign verify-blob` passed for both
   the keyless and KMS bundles, the checks the installer performed.
7. Decision: `azure/setup-helm` (v4.2.3) is replaced everywhere by the helper
   (`helm-linux-amd64`, `helm-linux-arm64`), because it downloads from
   get.helm.sh without a checksum and without retry or cache. The extracted
   binary goes to `$RUNNER_TEMP/helm` on `GITHUB_PATH`. Nightly keeps its
   "Verify Helm before it receives the publication token" step. The seeded
   `helm-linux-amd64` entry also serves `supply-chain-checks`.

Frozen inventories were handled by the repository's established pattern: a new
normalizer (`normalizePinnedToolReuse`) first requires the exact new steps and
then restores the frozen step content for hashing; baselines were not regenerated.
`TestPinnedToolReuseDrift` asserts restore path and key parity with the seed and
that each restore precedes its consumer. The normalizer and the security-release
test also require the pin files to equal the frozen url and sha256, so a changed
pin fails the tests.

## Remaining work and post-merge evidence

1. Merge the PR.
2. Run `gh workflow run ci-tool-cache.yml --ref main` and confirm each matrix job
   logs `Cache saved with key: pinned-tool-v1-Linux-...`.
3. `gh cache list --key pinned-tool-v1- --ref refs/heads/main` must show 6 entries.
4. Re-run a reader (a `ci-main` dispatch, or the tooling lane of a PR's
   `ci-feedback`) and quote `Cache restored from key: pinned-tool-v1-...`
   followed by `fetch-pinned-tool: verified cached <tool>`. A first run before the
   seed exists is a miss and downloads with backoff.
5. A main-to-dev backmerge is needed per `docs/development/branching.md`.

Out of scope, still unprotected against release outages:
- `install/upgrade-nightly.sh` downloads Cosign for end users with its own retry
  policy.
- `scripts/ci/check_gitops_kind.py` has a manual runbook download.

The `ci-feedback.yml` restore keeps its one-minute step bound, so a hung cache
service fails that lane; the download itself stays inside `feedback-budget.sh`.
