# Issue 158: bounded AWS runner bootstrap

This main-target bootstrap imports the six reviewed frozen runner files without
changing their source bytes. It does not complete [issue 158](https://github.com/Hikyo-Org/Hikyo/issues/158)
or establish real AWS acceptance.

## Local capability

- Require the protected `AWS_ACCEPTANCE_RUN_PREFIX` value, with no fallback.
  Each run retains the 32-creation reservation ceiling; namespace changes never
  reset the cumulative operator ledger.
- Accept an empty optional `AWS_ACCEPTANCE_HISTORICAL_CLEANUP_ARN` or only the
  single hardcoded old synthetic ARN. Use the verified cleanup role alone,
  exact scheduled-deletion metadata, and one durable ForceDelete intent.
- Refuse ambiguous deletion without retransmission or polling. After an exact
  delete response, bound read-only absence checks. Hold before starting the
  new synthetic suite unless requested historical cleanup proves absence.
- Preserve separate historical-cleanup evidence. The runner does not enforce
  cross-host once-only admission or the global cumulative creation allowance.

## Validation

From the repository root, the scoped offline checks are:

```sh
PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover -s scripts/ci/aws-acceptance -p 'test_*.py' -v
actionlint .github/workflows/aws-acceptance.yml
git diff --check
```

All 37 offline Python tests passed in 24.054 seconds through the shared queue.
Actionlint 1.7.12, AST parsing of all four Python files and whitespace checks
passed. This scope has no configured Python lint or type checker. No TypeScript
checks apply. Offline
coverage includes the installed-Go compiler capability, exact metadata and CLI
error formats, durable intent, ambiguity, bounded polling, cancellation,
expiry, cleanup-role binding, and refusal before suite startup. These checks
provide no live-provider or hosted-CI proof.

## Explicit holds

- Numeric approval for up to three manual runs and 96 additional creations is
  **PENDING**, not granted. The two prior runs reserved 48 creations; a proposed
  lifetime total of 144 is not authorization. Reconcile the cumulative ledger
  and actual billing independently.
- No AWS, credential, browser, OIDC, protected-environment or effective-policy
  change, workflow dispatch, fixture creation, deletion or spending belongs to
  this PR. The recovered owner session remains read-only.
- Before any later live phase, obtain explicit resource and numeric authority,
  verify effective policies and deadlines, claim historical cleanup once in the
  operator ledger bound to the exact first pending run ID, and clear its optional
  flag for subsequent runs. Verify fresh namespace custody, exact candidate and
  workflow SHAs, and the exact pending protected job before human approval.
- Cross-provider formal review is unavailable and has not been called clean.
  No paid or Claude review is authorized. Root review, required hosted checks,
  and the final human merge gate remain open. Keep this bootstrap PR unmerged;
  do not enable auto-merge or enter a merge queue.

See [the runner guide](../../scripts/ci/aws-acceptance/README.md) for the full
operator contract. Local validation, merge, live execution, cleanup and billing
are separate evidence gates.
