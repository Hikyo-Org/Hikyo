# PR #1049: align generated client and web Zod versions

PR: https://github.com/Hikyo-Org/Hikyo/pull/1049

The grouped update raised the web package to Zod 4.6.6 while leaving the
generated TypeScript client's package on 4.4.3. Web code imports those generated
schemas and passes them to its own Zod APIs. Comparing schema types across the
two versions made the web typecheck stall locally; both hosted feedback attempts
received runner shutdown signals during that check.

Update the client package and its frozen lockfile to Zod 4.6.6 as well. With
that single dependency change, the local web typecheck completed in about ten
seconds. The client version regression test rejects the original mismatch.
Keep client and web versions aligned on future dependency updates.

Validation uses the complete PR diff:

```sh
scripts/ci/fast-feedback.sh --working-tree --plan
VITEST_MAX_WORKERS=2 scripts/ci/fast-feedback.sh --working-tree --lane=frontend
pnpm --dir clients/ts run generate
git diff --exit-code -- clients/ts/src/generated
```

No compiler options, test coverage, CI deadlines, or dependency upgrades are
removed. Hosted exact-head feedback and current-base merge-group validation
remain required before merge. Local checks alone do not establish merge proof.
