# Agent guide

Conventions for automated agents (and humans) working in this repository.

## Commits must be DCO signed-off

Every commit on a pull request **must** carry a Developer Certificate of Origin
(DCO) sign-off, and this is checked **before** the PR can merge — the
`validation / dco` CI job (`scripts/ci/check-dco.sh`) fails the build when any
commit in the range is missing one.

- Add the sign-off when you commit: `git commit -s` (or `git commit --signoff`).
- The trailer must match the commit's author identity exactly:

  ```
  Signed-off-by: Your Name <your.email@example.com>
  ```

- Every commit in the PR is checked, not just the tip. If you amend, rebase, or
  add commits, each one still needs its own sign-off (`git rebase --signoff
  <base>` re-applies it across a range).
- Sign off as you go. A missing sign-off on an already-pushed commit can only be
  added by rewriting that commit, which then needs a force push — so it is far
  cheaper to sign off at commit time than to fix it after the DCO check has
  already gone red.

The `-s` flag only records that you agree to the DCO
(<https://developercertificate.org/>); it is not a cryptographic signature.

## Before pushing

- Run the relevant checks for what you touched (for `web/`: `node --run
  typecheck` and `node --run test`).
- Keep commit messages in the Conventional Commits style already used in the
  history (`feat(web): …`, `fix(app): …`, `test(web): …`).
