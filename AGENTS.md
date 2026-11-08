# Agent guide

Conventions for automated agents (and humans) working in this repository.

Do NOT use em-dash (—)

## Documentation format

Commit documentation in Markdown only. Do not commit new HTML documentation.
HTML companions are local, offline-only artifacts for the user to read and
validate; keep them outside Git and share local links when needed.

## Pull requests target dev

Once the `dev` branch exists, branch from it and open feature PRs with
`gh pr create --base dev`. GitHub and `gh` default to `main`, whose
`ci-queue-required` check then refuses feature PRs. Only same-repository `release/`
and `hotfix/` branches, Dependabot security updates and the CI definitions dev
admission refuses go to `main`. See the [branching guide](./docs/development/branching.md).

## Commits must be DCO signed-off

Every commit on a pull request **must** carry a Developer Certificate of Origin
(DCO) sign-off, and this is checked **before** the PR can merge: the
trusted admission gates (`scripts/ci/pr-gate/queue_dco.go`, running `scripts/ci/check-dco.sh`) refuse
a head when any non-merge commit in the range is missing one. Merge commits are not checked. They exempt only Dependabot's commits, on PRs
it opened and on canonical `release/*` or `main` promotion heads that carry them.

- Add both the signature and sign-off when you commit: `git commit -S -s`.
- The trailer must match the commit's author identity exactly:

  ```
  Signed-off-by: Your Name <your.email@example.com>
  ```

- Every non-merge commit in the PR is checked, not just the tip. If you amend, rebase, or
  add commits, each one still needs its own sign-off (`git rebase --signoff
  <base>` re-applies it across a range).
- Sign off as you go. A missing sign-off on an already-pushed commit can only be
  added by rewriting that commit, which then needs a force push, so it is far
  cheaper to sign off at commit time than to fix it after the DCO check has
  already gone red.

The `-s` flag only records that you agree to the DCO
(<https://developercertificate.org/>); it is not a cryptographic signature.

## Commits must be cryptographically signed

Every commit on a pull request must also carry a valid Git cryptographic
signature that GitHub reports as Verified. The DCO trailer above is required,
but it is not a substitute for this signature.

- For a new contributor or a signing/verification failure, read
  [the signing setup in CONTRIBUTING.md](./CONTRIBUTING.md#commit-signing-setup)
  for key configuration, local trust, and GitHub registration.
- Keep `commit.gpgsign=true` and commit normally with `git commit -S -s`. Never
  override or disable the configured signing behavior.
- Install the repository's fail-closed pre-push hook once in each clone:

  ```sh
  scripts/git/install-hooks.sh
  ```

  The hook checks every commit between `hikyo-upstream/main`, or `hikyo-upstream/dev`
  when that leaves fewer, and each branch being pushed. Keep the hook installed; do not bypass it with `--no-verify`.
- The hook also runs `scripts/ci/storybook-suite.sh` (about 4 minutes) when a push
  changes `web/`, `clients/ts/`, `.nvmrc`, the root `package.json`, that script or
  `docs/site/scripts/test-csp.mjs`; a pass is cached per exact input bytes and modes,
  and a cached pass skips the checkout checks. Otherwise push from a clean checkout
  holding the pushed inputs (no changes to them, no `.env*` files under `web/` or
  `clients/ts/`). Install its browser once with `pnpm --dir web run e2e:install`.
  For WIP pushes only, set `HIKYO_PREPUSH_STORYBOOK=skip`; CI still runs the suite.
  Signatures are never skippable.
- The hook also runs `scripts/ci/golangci-lint-new.sh <base>` when a push changes
  Go code. It runs golangci-lint (errcheck, errorlint, errchkjson, noctx,
  wastedassign, unused, staticcheck, and gosec G115/G204/G306 outside tests, from
  `.golangci.yml`) and flags only issues on lines changed since the merge base, so
  the existing baseline never blocks you. It lints a temporary worktree at the
  pushed commit, so any branch can be pushed and uncommitted files are ignored.
  Fetch `hikyo-upstream` first: a stale base widens "new code". Only the default
  build is linted (files for another OS, or tagged `ui`, `k8se2e` or `floor*`, are
  not), and type errors are always reported in full. Reproduce with
  `scripts/ci/golangci-lint-new.sh hikyo-upstream/main`. For WIP pushes only, set
  `HIKYO_PREPUSH_GOLANGCI=skip`; the advisory `ci-advisory-golangci` check still runs it.
- For `web/` changes, also run the affected flow specs before requesting review and
  list them in the PR Evidence:
  `pnpm --dir web run build && pnpm --dir web exec playwright test --project=desktop e2e/flows/<spec>.spec.ts`
  (add `--project=mobile` for mobile; `pnpm --dir web run e2e:install` once).
- Commit normally with signing enabled. If `git commit -S -s` itself fails to
  sign, stop and report the exact error. Never disable or bypass signing.
- Before every push, verify the complete pull-request range, using the base branch
  of your PR:

  ```sh
  scripts/ci/check-commit-signatures.sh hikyo-upstream/dev HEAD
  ```

  After pushing, confirm GitHub reports `verified: true` for the pushed commits.

## Contributor bootstrap

For first-time contributor setup or incomplete local tooling, use the repository
[hikyo-contributor-bootstrap skill](./.agents/skills/hikyo-contributor-bootstrap/SKILL.md).
Codex invokes it as `$hikyo-contributor-bootstrap`; Claude Code invokes it as
`/hikyo-contributor-bootstrap` through the linked `.claude/skills/` entry.
The skill covers signing, local validation, and an optional playground.

## Before pushing

- Use the existing shared feedback driver, `scripts/ci/fast-feedback.sh`, for
  local checks that match `ci-feedback`; use `--working-tree` for local edits.
  Follow [Local verification](CONTRIBUTING.md#local-verification) for pinned
  prerequisites and explicit base/head selection. Select checks from the complete
  PR diff; use `--plan` to inspect that selection before running them.
- Trace changed inputs through their consumers and existing fixtures. Cache-shaped
  declarations include test code; artifact changes affect inventory counts and
  deployed readers; workflow changes affect runner/cache policy; published guides
  affect site mappings and synthetic docs inputs. Run the affected owner tests and
  consumer checks from [the CI guide](docs/development/ci.md) and workflow source.
  Compilation and static analysis establish only the checks they execute.
- Propagate every required validation failure. Use `set -eu` for POSIX shell,
  `set -euo pipefail` for Bash, or explicitly collect every exit status. Await
  every child; an aggregate batch must return nonzero if any required check fails.
- After the final edit, merge or rebase, rerun affected checks and record the
  validated revision. Inspect applicable current-head feedback and invariant
  results; report pending, unavailable and skipped checks. Preserve the distinction
  between queue admission and complete full-suite proof.
- Diagnose CI from the run, attempt, tested head/base and first actionable failed
  step, including failed jobs in cancelled workflows. Distinguish test failures,
  stale-base refusals and publisher failures. A refreshed candidate needs fresh
  proof for its exact pair. Publisher changes also need cold-start and lagging-API
  coverage within the existing setup-inclusive deadline and publication reserve.
- Keep commit messages in the Conventional Commits style already used in the
  history (`feat(web): …`, `fix(app): …`, `test(web): …`).

## Reviewing changes

- Before requesting human review, have a fresh-context agent review the complete
  PR diff against its issue or decision record and this guide.
- Tag each finding Important (breaks or risks behaviour), Nit (style or
  preference) or Pre-existing (present before this PR, so not the author's
  regression). Cite a file and line as evidence. Start the report with the count,
  and say plainly when nothing needs fixing.
- Fix every confirmed finding, Nits included. Record evidence only for a finding
  you reject as wrong. Fix a Pre-existing finding at its root cause in this PR
  when it is in scope; otherwise open an issue for it.
- When a finding is one instance of a class, fix every instance and say how you
  found them.
- Converge. After the first full review, re-review only the new diff and do not
  repeat findings already fixed or rejected. After three review-and-fix rounds
  without a clean result, stop and reconsider the approach, or ask the
  maintainer, instead of starting a fourth round.
- When the same finding recurs across PRs, fix it at the source: the code first,
  then a check in `internal/lint` or `scripts/ci`, and this guide last.
- When a `Dev integration is red` issue closes, add its regression to the
  [review eval corpus](docs/development/review-evals.md).

## graphify

Use `rg`, `rg --files`, and focused source reads by default. Graphify is optional
for explicit requests or scoped relationship and impact exploration; an existing
graph does not make graph queries a prerequisite for codebase work.

- When the user invokes `/graphify`, follow the installed Graphify skill.
- Use `graphify query "<terms>"`, `graphify path "<A>" "<B>"`, or
  `graphify explain "<concept>"` when graph navigation helps the task. Respect
  truncation and verify important relationships against source.
- Check `built_at_commit` and account for uncommitted changes before relying on
  graph output. Treat it as navigation, with current source and runtime evidence
  governing correctness claims.
- Build or refresh a graph when requested or needed for an active graph task.
  Ordinary source reads and code edits do not require a graph rebuild. Follow
  [graphify-out/README.md](graphify-out/README.md) for setup and refresh details.
- Generated graphs are local; only `graphify-out/README.md` is committed. Read
  the wiki or `GRAPH_REPORT.md` when useful for the current investigation.
