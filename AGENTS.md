# Agent guide

Conventions for automated agents (and humans) working in this repository.

Do NOT use em-dash (—)

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

## Commits must be cryptographically signed

Every commit on a pull request must also carry a valid Git cryptographic
signature that GitHub reports as Verified. The DCO trailer above is required,
but it is not a substitute for this signature.

- Keep `commit.gpgsign=true` and commit normally with `git commit -s`. Never
  override or disable the configured signing behavior.
- After a reboot, restart, or GPG-agent timeout, GPG may be installed and
  configured but still locked. Before the first commit, verify non-interactive
  signing works:

  ```sh
  printf test | gpg --batch --pinentry-mode loopback \
    --local-user 30CC8A404B41D6AE2B11596FEA4208DC5ABEB135 --sign >/dev/null
  ```

- If that command fails with `No pinentry`, `can't get input`, or another
  locked-agent error, stop before committing, rebasing, or pushing. Ask the
  user to unlock GPG. The user can trigger the pinentry prompt with:

  ```sh
  printf unlock | gpg \
    --local-user 30CC8A404B41D6AE2B11596FEA4208DC5ABEB135 --sign >/dev/null
  ```

  Re-run the non-interactive check after the user unlocks GPG. Do not bypass
  signing to keep working.
- Before every push, verify the complete pull-request range:

  ```sh
  scripts/ci/check-commit-signatures.sh origin/main HEAD
  ```

  After pushing, confirm GitHub reports `verified: true` for the pushed commits.

## Before pushing

- Run the relevant checks for what you touched (for `web/`: `node --run
  typecheck` and `node --run test`).
- Keep commit messages in the Conventional Commits style already used in the
  history (`feat(web): …`, `fix(app): …`, `test(web): …`).
