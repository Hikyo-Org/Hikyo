# Agent guide

Conventions for automated agents (and humans) working in this repository.

Do NOT use em-dash (—)

## Commits must be DCO signed-off

Every commit on a pull request **must** carry a Developer Certificate of Origin
(DCO) sign-off, and this is checked **before** the PR can merge: the
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
  added by rewriting that commit, which then needs a force push, so it is far
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
- Install the repository's fail-closed pre-push hook once in each clone:

  ```sh
  scripts/git/install-hooks.sh
  ```

  The hook checks every commit between `origin/main` and each branch being
  pushed. Keep the hook installed; do not bypass it with `--no-verify`.
- Commit normally with signing enabled. If `git commit -s` itself fails to
  sign, stop and report the exact error. Never disable or bypass signing.
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

## graphify

This project has a knowledge graph at graphify-out/ with god nodes, community structure, and cross-file relationships.

When the user types `/graphify`, use the installed graphify skill or instructions before doing anything else.

Only `graphify-out/README.md` is committed; the graph is generated locally.

Rules:
- On a fresh clone or worktree `graphify-out/graph.json` is absent — run `graphify update .` once to materialize it (free, AST-only). Community names come out as mechanical hub names; run `graphify label` (LLM) only if you want prose names.
- For codebase questions, first run `graphify query "<question>"` when graphify-out/graph.json exists. Use `graphify path "<A>" "<B>"` for relationships and `graphify explain "<concept>"` for focused concepts. These return a scoped subgraph, usually much smaller than GRAPH_REPORT.md or raw grep output.
- Dirty graphify-out/ files are expected after hooks or incremental updates; dirty graph files are not a reason to skip graphify. Only skip graphify if the task is about stale or incorrect graph output, or the user explicitly says not to use it.
- If graphify-out/wiki/index.md exists, use it for broad navigation instead of raw source browsing.
- Read graphify-out/GRAPH_REPORT.md only for broad architecture review or when query/path/explain do not surface enough context.
- Check `built_at_commit` before treating graph relationships as current. The graph is navigational evidence; verify security-sensitive and runtime claims against source.
- After modifying application code, run `graphify update .` to refresh structural relationships (AST-only, no API cost). Documentation and image semantics need a skill-driven `/graphify --update` pass.
- See [graphify-out/README.md](graphify-out/README.md) for installation and why only the README is tracked in Git.
