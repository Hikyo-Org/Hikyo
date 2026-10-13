# OpenAEV validation core

This is the bottom layer of the post-release OpenAEV migration stack:

```text
core -> #904 assertion suites -> #903 hosted runner and success-only publisher
```

The core owns the bounded OpenAEV recording and reporting transports, adversarial case definitions, deadline-bound workload HTTP requests, load scheduling, native audit validation, native lab resource guards, and read-only release identity/count helpers. The HTTP pilot transport is included because recording imports it directly.

Core source and regression tests came from the existing assertion layer. The workload TLS context explicitly enforces its existing TLS 1.2 minimum. Review fixes make ordinary background collector crashes sticky failures, reject truncated HTTP bodies, preserve sanitized reporting diagnostics, require native node identity, and collect the correct idle or active pod boundary, with regression coverage. No adversarial requests or tests were removed. The later assertion layer retains the full 313 native checks.

The only workflow in this layer is the read-only harness checker. It does not launch a lab, trigger validation after publication, or write release notes. The later runner layer supplies those behaviors under the user contract: GitHub-hosted ARM64, after release or prerelease publication, nonblocking, and no release mutation on failure.

## Local validation

Install the pinned checker dependencies into a Python environment, then run:

```sh
python3 -m pip install -r scripts/openaev/requirements-check.lock
scripts/openaev/check.sh
```

Current local result: Ruff passed; Mypy checked nine application files; 138 Python regression tests passed. Actionlint and ShellCheck validate the checker workflow and script. These local regressions are separate from the full hosted 313-assertion exercise, which remains pending.

See [the core handoff](../../docs/handoff/core.md) for stack ownership and delivery evidence.
