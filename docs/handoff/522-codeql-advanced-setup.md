# CodeQL: default setup → advanced setup (PR #522)

## Root cause

GitHub's default CodeQL setup installs its own Go toolchain (currently
1.26.6) and sets `GOTOOLCHAIN=local`, preventing autobuild from
downloading the version required by `go.mod`. After raising `go` to 1.27
in go.mod, autobuild fails: `go.mod requires go >= 1.27.0 (running go
1.26.6; GOTOOLCHAIN=local)`.

The overlay cache (ACTIONS cache keyed to `ubuntu-latest`, 5 days old)
picked up CodeQL CLI 2.26.3, which was stale relative to the 3.30.0
available at analysis time — a separate but related freshness problem.

## Decision

Replace default setup with an explicit advanced-setup workflow
(`.github/workflows/codeql-analysis.yml`).

| Aspect | Default setup | Advanced setup (chosen) |
|---|---|---|
| Go toolchain | Managed snapshot (1.26.6) | `setup-go` resolves `go.mod` |
| Languages | Repository setting | Workflow matrix |
| Bundle freshness | Overlay cache, opaque | Fresh `init` download |
| GOTOOLCHAIN=local | Forced by runner | Not set (setup-go pins exactly) |
| Revert cost | Flip one setting | Delete workflow + flip setting |

### Workflow choices

- **Languages**: actions, go, javascript-typescript (same set as before)
- **Cron**: Monday 06:00 UTC (`0 6 * * 1`) — avoids collision with
  nightly (daily 02:00) and race-isolation (Mon 04:00)
- **Permissions**: contents:read + security-events:write + actions:read
  (actions:read needed for the Actions language extractor)
- **Build mode**: autobuild for Go; `none` for actions and
  javascript-typescript
- **query_suite**: default (omitted; codeql-action default)
- **GOTOOLCHAIN**: not set globally; setup-go pins the exact version
  from go.mod, which is the deterministic semantics the repo expects
- **Checkout ref**: `${{ github.event.pull_request.head.sha || github.sha }}`
  (matches ci.yml convention)
- **concurrency**: per-language group, cancels non-push runs (matches
  ci.yml pattern)

### configure-repository.sh

The PATCH assertion flips from `state=configured` to a guarded
`state=not-configured`. The guard avoids a 202/validation-run on every
release cycle when the setting is already off.

## Gofmt saga

The ci.yml Format gate uses bare `gofmt`, which resolves to the
preinstalled `/usr/local/go/bin/gofmt` (go 1.26.2) on both CI runners
and local machines, shadowing the `setup-go`-installed toolchain at
`$(go env GOROOT)/bin/gofmt` (go 1.27.0). On this branch's CI the gate
was correct (setup-go prepends first), but on local machines the stale
1.26.2 was used to format config_test.go, producing 3 spurious lines
that failed the `validation / generated` check.

Fix: ci.yml Format step now uses `"$(go env GOROOT)/bin/gofmt"` so the
gate always matches the toolchain `go.mod` declares.

## Verification

After pushing, re-run the failed `Analyze (go)` job on PR #522. The
advanced workflow will run, and the upload SARIF step will succeed once
default setup is disabled via API.

```sh
gh api --method PATCH repos/Hikyo-Org/Hikyo/code-scanning/default-setup \
  -f state=not-configured
```

The async validation run (202) completes in ~1 minute. The advanced
workflow's SARIF upload will then succeed on re-run.

## Rollback

1. Delete `.github/workflows/codeql-analysis.yml`
2. Flip configure-repository.sh back to `state=configured` with the
   original PATCH block
3. Run `scripts/release/configure-repository.sh` to re-enable default
   setup
4. Revert ci.yml gofmt line (optional; the GOROOT fix is harmless with
   default setup)

## References

- PR #522: https://github.com/Hikyo-Org/Hikyo/pull/522
- codeql-action v4.37.8 tag: `db488dddef3bf6cb639b32c2e9a7c0a7ea8271d28`
- Default setup API: `PATCH /repos/{owner}/{repo}/code-scanning/default-setup`
  with `state: not-configured`
- configure-repository.sh line ~38–46 (updated in this commit)
