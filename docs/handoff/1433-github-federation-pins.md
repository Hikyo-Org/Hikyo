# PR #1433: run-narrowing GitHub federation pins

PR: <https://github.com/Hikyo-Org/Hikyo/pull/1433> (base `dev`)

## Result

The November 2026 wfx-loot campaign used stolen GitHub accounts to push CI
payloads that dumped secrets and requested OIDC tokens. Hikyo cannot protect a
value after it reaches a compromised job, so this PR narrows which runs can
receive one (owner-approved option A) and documents keeping production secrets
out of CI (option D).

- GitHub Actions bindings now also require `ref_protected`,
  `runner_environment` and `job_workflow_ref`. The pin must be present; the
  value is the operator's choice. `job_workflow_sha` stays optional.
- Every required pin must be a JSON string. Creation refuses a number or
  boolean with `ErrBindingPinKind`. Stored bindings are not re-checked.
- The web GitHub preset used to pin `repository_id` and `repository_owner_id`
  as numbers, while GitHub emits strings. Those bindings never matched a real
  token. The preset now pins strings, and replacing a binding re-pins legacy
  number or boolean values as strings. The subject field starts empty, with a
  hint to copy it from a real token.
- `machine-identities.mdx` and `deployment-adapters.mdx` explain each pin and
  its limits, and say to keep production secrets out of CI. The ADR
  `machine-identities.md` carries a declared amendment banner linking #17.

## Evidence

- Claim types come from GitHub's OIDC documentation and the
  `github/actions-oidc-debugger` sample, not from a decoded live token.
- Local checks on the rebased tree:
  - `go vet`;
  - `go test -p 2` on the API, `oidcfed`, `oidctest`, `cli`, `server`,
    `service` and the `isolation` federation tests;
  - the full web unit suite, typecheck, lint, and the TypeScript client
    checks;
  - the full pre-push hook.
- Hosted CI: all 26 checks passed on `6d95429df`.
- Review: independent Standards and Spec reviews, with every finding fixed.
  CodeRabbit's two findings are fixed, and CodeRabbit confirmed both fixes.
  Cross-provider review was not requested and is not CLEAN.

## Remaining work

1. Capture before and after screenshots of the BindingDialog GitHub preset and
   add them to the PR description. The visual-evidence checklist item is open.
2. Clear CodeRabbit's stale changes-requested review from `a1d9aad8d`. Dismiss
   it, or request `@coderabbitai review` after its review limit resets.
3. Owner decision: reopen #17, as GOVERNANCE.md's amendment procedure requires.
4. Maintainer review and merge-queue validation. Local checks and queue
   admission do not establish merge proof.

## Compatibility

- **Breaking for creation:** scripts and IaC that create GitHub bindings must
  add the three new pins as strings.
- Bindings already created through the web UI with numeric ids never
  authenticated, so operators must recreate them as replacements.
- CLI-created bindings that already exist keep working.
- Rolling back restores the old pin set without touching stored bindings.
