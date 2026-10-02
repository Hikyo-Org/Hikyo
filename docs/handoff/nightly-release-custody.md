# Nightly release credential custody

Publication uses the `nightly-release` environment. A workflow branch/ref check
is not a secret boundary: a branch writer can modify its workflow. The app
private key must therefore be an environment secret protected by a main-only
deployment branch policy, not a repository secret.

## Read-only configuration evidence

Checked on 2026-10-01 at 13:20 UTC using repository environment and Actions
secret metadata APIs. The repository lists only `copilot` and `github-pages`
environments; `nightly-release` is absent. The repository-scoped secret
`NIGHTLY_RELEASE_APP_PRIVATE_KEY` is still present (metadata created and last
updated 2026-08-24). No secret value was read and no external setting changed.
This is an unresolved operator gate, not a clean security result. Recheck before
publication because external settings can change independently of this branch.

## Required operator configuration

No credential or repository-setting mutation is authorized or performed by the
remediation agent. Until the operator completes these steps, the existing
repository-scoped key remains exposed to branch-selected workflows. Merely
adding `environment: nightly-release` does not fix that exposure; a missing
environment can be created without protection by GitHub, and secret lookup can
fall back to the repository secret.

1. Open [repository environments](https://github.com/Hikyo-Org/Hikyo/settings/environments)
   and create `nightly-release`. Set deployment branches and tags to selected
   branches and tags, with exactly one branch rule `main` and no tag rule.
2. Add `NIGHTLY_RELEASE_APP_PRIVATE_KEY` to that environment's secrets using the
   existing operator-controlled credential. Do not paste the key into chat or
   commit it. Keep the existing app client-ID variable unchanged.
3. Open [repository Actions secrets](https://github.com/Hikyo-Org/Hikyo/settings/secrets/actions)
   and remove the repository-scoped `NIGHTLY_RELEASE_APP_PRIVATE_KEY` after
   confirming the environment secret is present. Assess prior exposure and
   rotate the app key if appropriate; rotation is a separate operator action.
4. Verify the configuration with the read-only commands below before enabling
   publication. The branch response must contain only `main` of type `branch`,
   the environment response must use custom branch policies, the environment
   secret list must include the key name, and the repository list must not.

```sh
gh api repos/Hikyo-Org/Hikyo/environments/nightly-release
gh api repos/Hikyo-Org/Hikyo/environments/nightly-release/deployment-branch-policies
gh api repos/Hikyo-Org/Hikyo/environments/nightly-release/secrets --jq '.secrets[].name'
gh api repos/Hikyo-Org/Hikyo/actions/secrets --jq '.secrets[].name'
```

These APIs list metadata, not secret values. Local workflow checks verify the
environment binding only; they cannot certify externally configured custody.
