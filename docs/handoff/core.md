# OpenAEV core layer handoff

The authorized migration moves OpenAEV validation to a GitHub-hosted ARM64 runner after release or prerelease publication. Validation is nonblocking. Failure must never append release notes, badges, or result artifacts.

## Stack and ownership

```text
Core PR, base main
  -> #904 assertion suites
  -> #903 hosted runner, native preparation, isolated stack, success-only publisher
```

This core layer imports nine application Python files and eight matching regression modules. It owns bounded recording/reporting transports, adversarial definitions and HTTP transport, load scheduling, native audit checks, native memory/readiness guards, and read-only release identity/count helpers. The HTTP pilot transport is a required import dependency. Only the harness checker workflow is added; this layer adds no live post-release workflow.

Source came from the existing assertion layer. The workload transport explicitly sets its existing TLS 1.2 minimum. Review fixes restrict platform origins to the loopback tunnel and two exact lab DNS endpoints, make ordinary background collector crashes sticky sanitized failures even after a later healthy native read, reject truncated HTTP bodies even when the delivered prefix is complete JSON, retain sanitized reporting schema and size errors while hiding raw transport details, require a nonempty native node UID, and collect six pods at idle and seven during execution. 23 additional regression tests reproduce these failures against the original core head and verify the corrections. Existing adversarial request coverage and all retained core tests remain intact. The middle suite layer retains all 313 native assertions.

## Evidence

Fresh base: `fc103b2a6f91f7ca3f13b03aa0715ee01a966846` from origin/main.

Local check: `scripts/openaev/check.sh` passed Ruff, Mypy for nine application files, and 154 Python regression tests. These unit tests are not the 313 native assertions. The workflow and shell checker receive Actionlint and ShellCheck validation. Structural graph refresh is local navigation data and is excluded from the commit.

The complete hosted exercise remains pending. Earlier local isolated-network and independent monitoring probes are separate evidence and do not establish complete hosted PASS. The runner layer owns the release-bound attestation, cleanup gate, and success-only publication. No publication workflow is activated by this core layer.

## Reproduce

```sh
python3 -m venv /tmp/hikyo-openaev-core-checks
/tmp/hikyo-openaev-core-checks/bin/python -m pip install --require-hashes --only-binary=:all: -r scripts/openaev/requirements-check.lock
PATH="/tmp/hikyo-openaev-core-checks/bin:$PATH" scripts/openaev/check.sh
```

Expected result: Ruff passes, Mypy reports nine source files, and 154 tests pass.

## Next layer

Rebase the assertion suite PR onto the final core commit without duplicating core files, then rebase the runner PR onto the suite commit. Run the complete checker and hosted rehearsal on the final combined tree. Keep the release publisher unavailable for failed, incomplete, canceled, or cleanup-unverified runs.

The reporting asset search traverses all bounded native Spring pages before creating a target and refuses inconsistent or incomplete inventory. Checker dependencies include official PyPI wheel hashes with required hash checking and binary-only installation. The unused standalone HTTP pilot executable now fails closed; bounded import helpers remain available to the hosted suite.

Canaries must use an unescaped ASCII alphabet, and every lab pod must bind to
the verified ARM64 node. Latest-nightly timestamp ties and report-directory
symlinks introduced during creation fail closed. Protocol and redirect errors
are sanitized, with active inject identity preserved and no raw error chains.
