# Agent guide

Conventions for automated agents (and humans) working in this repository.

Do NOT use em-dash (—)

## Documentation format

Commit documentation in Markdown only. Do not commit new HTML documentation.
HTML companions are local, offline-only artifacts for the user to read and
validate; keep them outside Git and share local links when needed.

## Commits must be DCO signed-off

Every commit on a pull request **must** carry a Developer Certificate of Origin
(DCO) sign-off, and this is checked **before** the PR can merge: the
`validation / preflight` CI job's DCO step (`scripts/ci/check-dco.sh`) fails the build when any
commit in the range is missing one.

- Add both the signature and sign-off when you commit: `git commit -S -s`.
- The trailer must match the commit's author identity exactly:

  ```
  Signed-off-by: Your Name <your.email@example.com>
  ```

- Every commit in the PR is checked, not just the tip. If you amend, rebase, or
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

  The hook checks every commit between `hikyo-upstream/main` and each branch being
  pushed. Keep the hook installed; do not bypass it with `--no-verify`.
- Commit normally with signing enabled. If `git commit -S -s` itself fails to
  sign, stop and report the exact error. Never disable or bypass signing.
- Before every push, verify the complete pull-request range:

  ```sh
  scripts/ci/check-commit-signatures.sh hikyo-upstream/main HEAD
  ```

  After pushing, confirm GitHub reports `verified: true` for the pushed commits.

## Contributor bootstrap

For first-time contributor setup or incomplete local tooling, use the repository
[hikyo-contributor-bootstrap skill](./.agents/skills/hikyo-contributor-bootstrap/SKILL.md).
Codex invokes it as `$hikyo-contributor-bootstrap`; Claude Code invokes it as
`/hikyo-contributor-bootstrap` through the linked `.claude/skills/` entry.
The skill covers signing, local validation, and an optional playground.

## Before pushing

- Run the relevant checks for what you touched (for `web/`: `node --run
  typecheck` and `node --run test`).
- Keep commit messages in the Conventional Commits style already used in the
  history (`feat(web): …`, `fix(app): …`, `test(web): …`).

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
