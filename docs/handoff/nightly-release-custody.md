# Nightly release credential custody

Publication uses the `nightly-release` environment. A workflow branch/ref check
is not a secret boundary: a branch writer can modify its workflow. The app
private key must therefore be an environment secret protected by a main-only
deployment branch policy, not a repository secret.

## Configuration verified on 2026-10-21

The earlier 2026-10-01 audit found the app private key at repository scope and
no protected publication environment. While configuring stable and nightly
Apple distribution, the maintainer's existing local app key was authenticated
against GitHub's app endpoint. The returned app was `hikyo-nightly-release`
with the expected `contents: write` permission. No key material was printed.

The `nightly-release` environment now uses custom deployment branch policies
with exactly `main` of type `branch` and no tag rule. The existing private key
was installed as `NIGHTLY_RELEASE_APP_PRIVATE_KEY` in that environment. After
verifying its metadata there, the repository-scoped secret was removed and its
absence verified. The app client-ID variable was preserved.

This closes the documented GitHub credential-location and branch-policy gate.
It does not prove that the previous repository-scoped credential was never
used by a branch workflow. The key was relocated without rotation; historical
exposure assessment and any resulting rotation remain operator decisions.

## Repeatable operator configuration

These steps describe the verified configuration and can be used when restoring
or replacing credentials. Keep private material out of chat and Git.

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
