# #813: fork pull requests through trusted CI

## Problem

`trusted-ci` runs on `pull_request_target` and checks out the PR head. Since
`actions/checkout` v7 it refuses a fork's head there. So every fork PR failed
`validation / changes`, and no external contribution could merge (first seen on
#812). The refusal is correct. Under `pull_request_target`, fork code gets the
runtime token, which can write the base branch's Actions cache.

## Design

| Piece | Runs as | Decides |
| --- | --- | --- |
| `.github/workflows/ci-fork.yml` (`fork-ci`) | `pull_request`, fork PRs only. Read-only token, no secrets, PR-scoped cache | `vouch` job, then the full `ci.yml` validation graph. It is not a merge gate. |
| `ci-control.yml` `ci-required` | `pull_request_target`, base YAML. Never checks out PR code | Same-repo PR: unchanged (`validation` result). Fork PR: vouched author, then `scripts/ci/check-fork-validation.sh`. |
| `.github/VOUCHED.td` | Read by [vouch](https://github.com/mitchellh/vouch) `check-user` from the default branch through the API | Which fork authors get CI. Collaborators with write access and bots pass automatically. |

`check-fork-validation.sh` accepts a fork PR only when all of these hold:

1. The head is still the event's SHA.
2. The PR changes nothing under `.github/`, renames included. The fork run then
   executed base-identical workflow YAML. A truncated file list fails closed.
3. `fork-ci`'s latest run for that SHA completed, and its
   `validation / ci-required` job succeeded.

The script waits up to 90 minutes. The job timeout is 100.

Invariants pinned in `check-trusted-ci-scripts_test.sh`:

- `fork-ci` triggers on `pull_request` only.
- `fork-ci` has no job named `ci-required`, because a skipped job would satisfy
  the required check.
- No workflow sets `allow-unsafe-pr-checkout`.
- Both workflows run the vouch check without `allow-fail`.
- `ci-required` calls the fork gate.

`check-fork-validation_test.sh` drives the gate against a stub `gh`.

## No secrets on fork runs

GitHub withholds repository secrets from `pull_request` runs of fork PRs, and
the token is read-only. The YAML keeps it that way. `check-trusted-ci-scripts_test.sh`
fails in any of these cases:

- `ci-fork.yml` references `secrets.`, uses `secrets:` (including
  `secrets: inherit`), or mentions `id-token`;
- `ci.yml` references a secret or requests `id-token: write`;
- `ci-fork.yml` holds an issue or PR write permission.

Cache writes from a fork run are scoped to the PR ref, so they cannot seed
`main`. The only third-party code that sees a token on the fork path is the
vouch action, pinned by SHA, and that token is the read-only `GITHUB_TOKEN`.

## DCO failure comment

`dco-report.yml` (`pull_request_target`, `pull-requests: write`) explains a
missing sign-off on the PR. It checks out the base SHA and fetches the PR's
commits as git data only. Then it runs the base branch's `report-dco.sh`, which
calls `check-dco.sh` with the same Dependabot exemption as `ci.yml`. While
commits lack a sign-off, the script posts one marker comment and updates it on
later pushes. It deletes the comment once every commit is signed off.

The comment lists commits by SHA only. Subjects and author strings are
attacker-controlled and never reach the comment body. The gate stays in
`ci.yml`'s preflight; this workflow is not required. `report-dco_test.sh`
covers post, update, delete, and the no-subject rule. The fixture test pins
the single base-SHA checkout.

## Operating it

- **Vouch a contributor:** merge a change that adds `github:<login>` to
  `.github/VOUCHED.td`. A PR cannot vouch for itself, because both checks read
  the default branch. Then re-run the fork PR's `fork-ci` and `trusted-ci`.
- **First-time contributors:** the repository still requires maintainer
  approval for their first workflow run (`first_time_contributors`). If the
  approval comes more than 90 minutes after the push, `ci-required` fails.
  Re-run `trusted-ci` once `fork-ci` finishes.
- **A fork PR that touches `.github/`** fails closed by design. Land it from a
  branch in this repository.

## Known limits

- **Pre-existing, not introduced here: same-name check spoofing.** Any fork PR
  can add a workflow under `on: pull_request` with a job named `ci-required`,
  which publishes a green check run of that name on its head. The trusted gate
  fails such a PR (it touches `.github/`), but branch protection matches
  required checks by name, and both runs come from the GitHub Actions app. So
  the spoofed run may satisfy the rule. The same was already possible before
  this change. The mitigations are repository settings: fork-workflow approval
  for all outside contributors, or an org ruleset that requires `trusted-ci` by
  workflow file. The maintainer decides.

- The trusted gate is only exercised after merge, because `pull_request_target`
  runs `main`'s YAML. The first real fork PR (#812) is the end-to-end test.
- `fuzz-report.yml` listens to `trusted-ci` and `ci` runs. It does not report
  fuzz findings from `fork-ci` runs; the failure still shows in the PR checks.
- Changing only `VOUCHED.td` is an unclassified path, so the classifier falls
  back to the full plan.
- An org-level "require workflows to pass" ruleset could replace the hand-rolled
  gate. It needs org-admin configuration that this change does not assume.
