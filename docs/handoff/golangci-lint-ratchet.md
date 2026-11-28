# golangci-lint new-code ratchet (#1273)

## Result

These are the static-analysis findings CodeRabbit kept posting as "Supported by static analysis", now caught before the push and on the PR. Only lines changed since the merge base are checked.

- `scripts/ci/golangci-lint-new.sh <base>` runs golangci-lint, pinned in `scripts/ci/golangci-lint-version`.
  - It uses the rules in `.golangci.yml` with `--new-from-merge-base`.
  - It lints only the changed packages, or `./...` when the config, the pin, `go.mod` or `go.sum` changes.
- `.githooks/pre-push` runs that script whenever a push changes Go code or one of its inputs.
  - It lints a detached temporary worktree at the pushed commit, using that commit's script, pin and config. The hook checkout's script is used only when the commit has none.
  - It sets `GOFLAGS=-trimpath`, so each fresh path hits the build cache.
  - `HIKYO_PREPUSH_GOLANGCI=skip` skips the check for WIP pushes only.
- `ci-advisory-golangci` (`.github/workflows/ci-golangci.yml`) runs the same script on PRs. It is not a required context.
- The rule set matches the codes that fired in review:
  - errcheck, errorlint, noctx, wastedassign and unused;
  - staticcheck `SA*`, `QF1001` and `QF1003`;
  - gosec G115, G204 and G306, outside tests.

  Issue caps are 0, so findings are never truncated.

## Evidence

- On a14cb19da, the hook ran on its own push against `hikyo-upstream/dev` and reported 0 issues: 79 s cold and 9 s warm. That push changes `.golangci.yml`, so it lints `./...`. A Go-only push takes about 3 s warm.
- `scripts/git/pre-push_test.sh`, `scripts/ci/golangci-lint-new_test.sh` and `go test -count=1 -run 'Golangci|PrePush|Invariants|Storybook' ./scripts/ci` pass, and `shellcheck` is clean.
- The new fixture assertions were mutation-checked.
- Review: three fresh-context rounds. The last reported 0 findings.

## Remaining work

- Fix the baseline and gate the low-count rules repo-wide (wastedassign 18, unused 7 and some staticcheck SA hits on `main`).
  - `scripts/ci/pr-gate/queue_refresh.go:525` assigns `cause` and never reads it. This looks like a real bug but is unconfirmed.
- Adopt zizmor for workflows, with a reviewed ignore list for the deliberate privileged triggers. This needs owner security sign-off.
- Skipped on purpose:
  - Checkov CKV_OPENAPI_5, because the endpoints it flags are public by design;
  - LanguageTool;
  - the `coderabbit` CLI, an external service whose cost is unverified.

## Next steps

1. Install the hook per worktree with `scripts/git/install-hooks.sh`, which sets `core.hooksPath=.githooks`. An absolute `core.hooksPath` that points at one checkout makes every worktree run that checkout's hook.
2. Before each push, fetch `hikyo-upstream`. A stale base widens what counts as new code.
3. Reproduce a hook failure with the command it prints: check out the pushed commit detached, then run `scripts/ci/golangci-lint-new.sh <base>`.
