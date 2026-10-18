# Manual nightly release trigger

## Outcome

The publishing workflow lost its branch-selected manual trigger in security
remediation commit `c3dbfacd0`. It retained the 02:00 UTC schedule and the
default-branch-only `nightly-chain-restart` repository event.

`nightly-release-manual` now provides an input-free Actions button. Its main-only
job uses the built-in workflow token to send a normal `nightly-release`
repository dispatch. It checks out no code and references no release secret.
The existing publishing workflow consumes that event from the default branch.
It retains exact-source full CI, signing, startup and upgrade checks, concurrency,
and duplicate-source skipping. Only the explicit `nightly-chain-restart` event
can opt into building without a predecessor.

## Use after merge

Open Actions, select **nightly-release-manual**, choose **Run workflow**, and
select **main**, or run:

```sh
gh workflow run nightly-manual.yml --repo Hikyo-Org/Hikyo --ref main
```

Follow the separate **nightly-release** run for publication. The release source
is the default-branch commit at repository-dispatch time. Request acceptance is
not publication success. GitHub offers the manual button after the wrapper
exists on the default branch.

## Validation and delivery boundary

- The regression test rejects the original trigger configuration and accepts
  the new configuration. It executes the real request step with a fake GitHub
  CLI to verify its fixed payload, successful summary, and API failure handling.
- The new test is included in CI's existing workflow-policy test selection.
- Selected CI workflow-policy and security-release tests passed locally.
- The nightly release shell fixtures, ShellCheck, actionlint for all changed
  workflows, and whitespace checks passed locally.
- Commit, push, PR and merge are authorized. The local checks above do not
  establish remote CI or merge; use the delivery PR for that evidence. No
  nightly release was requested from the live repository during validation.

## Existing operator configuration

Read-only metadata on 2026-10-18 still showed no environment secret for the
nightly app key, no deployment branch policy, and a repository-scoped app key.
The existing [credential custody handoff](nightly-release-custody.md) describes
the required operator correction. No secret value was read and no repository
setting was changed. The manual request workflow does not repair or certify
that existing configuration.
