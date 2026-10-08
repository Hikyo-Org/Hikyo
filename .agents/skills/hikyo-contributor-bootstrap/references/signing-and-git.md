# Git identity, signing, and contributor access

## Read the current guide first

Use `CONTRIBUTING.md`'s signing setup when present. Older checkouts may describe
DCO alone. `git commit -s` adds a trailer; `git commit -S -s` also requests a
cryptographic signature. Neither configures a key. Keep `commit.gpgsign=true`.

Inspect effective settings and their configuration origins without printing key
contents: `user.name`, `user.email`, `commit.gpgsign`, `gpg.format`,
`user.signingkey`, `gpg.ssh.allowedSignersFile`, and `core.hooksPath`. Confirm the
contributor's intended identity rather than copying the maintainer's. DCO must
match the author; GitHub needs the appropriate account email and registered
signing key. Ask once for an identity missing from configuration/instructions.

Keep a valid SSH, OpenPGP, or S/MIME setup. If none exists, recommend SSH and
explain any human passphrase/registration steps. Use Git 2.34+ and an SSH
implementation supporting SSH signing. Do not overwrite keys or request a
private key in chat.

Official setup references:

- [SSH key creation and agent setup](https://docs.github.com/en/authentication/connecting-to-github-with-ssh/generating-a-new-ssh-key-and-adding-it-to-the-ssh-agent)
- [Git signing-key configuration](https://docs.github.com/en/authentication/managing-commit-signature-verification/telling-git-about-your-signing-key)
- [Register an SSH Signing Key](https://docs.github.com/en/authentication/connecting-to-github-with-ssh/adding-a-new-ssh-key-to-your-github-account)
- [Generate a GPG key](https://docs.github.com/en/authentication/managing-commit-signature-verification/generating-a-new-gpg-key)
- [Register a GPG key](https://docs.github.com/en/authentication/managing-commit-signature-verification/adding-a-gpg-key-to-your-github-account)
- [Local SSH allowed-signers configuration](https://git-scm.com/docs/git-config#Documentation/git-config.txt-gpgsshallowedSignersFile)
- [GitHub signature verification](https://docs.github.com/en/authentication/managing-commit-signature-verification/about-commit-signature-verification)

## SSH setup fallback for older guides

Run from the checkout root in Bash or Zsh. Substitute the contributor's identity
and chosen key path. The public-key path requires its private counterpart in
the running SSH agent:

```sh
git config --local user.name "Your Name"
git config --local user.email "your-verified-email@example.com"
hikyo_signing_key="$HOME/.ssh/id_ed25519"
ssh-add "$hikyo_signing_key"
git config --local gpg.format ssh
git config --local user.signingkey "$hikyo_signing_key.pub"
git config --local commit.gpgsign true
```

Register the public key as a **Signing Key** on GitHub. Authentication-key
registration alone is insufficient. Retain an existing allowed-signers file if
configured. Otherwise, continuing in the same shell:

```sh
hikyo_allowed_signers="$(git rev-parse --absolute-git-dir)/hikyo-allowed-signers"
printf '%s %s\n' "$(git config user.email)" "$(cat "$hikyo_signing_key.pub")" >> "$hikyo_allowed_signers"
git config --local gpg.ssh.allowedSignersFile "$hikyo_allowed_signers"
```

This file stays outside tracked source. The checker accepts Git status `G`
only; a signature may exist while local verification fails. GPG status `U`
means valid signature with unknown local trust and fails this checker. Verify
key identity before trusting keys, including other contributors in the branch.
Let a real `git commit -S -s` use configured signing normally; retain its exact
error if it fails. Do not bypass signing/hooks or perform GPG unlock probes.

## Clone topology and hooks

Inspect `git remote -v` and `git branch -vv`. The pre-push hook currently
hardcodes `refs/remotes/origin/main` regardless of push destination. Use canonical
Hikyo main as the comparison base even for forks. One explicit way to populate
it without changing remote URLs is:

```sh
git fetch https://github.com/Hikyo-Org/Hikyo.git refs/heads/main:refs/remotes/origin/main
scripts/git/install-hooks.sh
```

Explain that this refreshes local `origin/main` from canonical Hikyo even if
`origin` is a fork. Do not force a rejected divergent update; investigate
topology. Recheck source if a future hook supports another base. Preserve other
hooks: the installer sets `core.hooksPath=.githooks`, so inspect an existing
arrangement first. For linked worktrees, recheck effective identity, trust file
paths, hooks, and ignored dependencies instead of assuming all state carries.

After real contribution commits exist:

```sh
git log --format='%h %G? %s' origin/main..HEAD
scripts/ci/check-dco.sh origin/main HEAD
scripts/ci/check-commit-signatures.sh origin/main HEAD
```

Both checkers reject an empty range. On freshly cloned main, report verification
pending the first contribution, not a key failure or reason to invent a commit.
Signatures include merge commits; DCO currently checks non-merge commits.
Rewriting commits requires new signatures and DCO across the complete rewritten
range. Do not rewrite pushed history without authorization.

## Fork contribution gates

GitHub signing-key registration is separate from Git push authentication. Inspect
the push URL and preserve a working credential helper/SSH setup. For HTTPS,
`gh auth status` can check an existing GitHub CLI session; if missing, let the
contributor complete `gh auth login` and configure Git credentials through the
supported helper. For SSH, use the authentication-key registration and agent
steps in the official SSH guides above. Never print tokens, private keys, or
credential-helper output. A successful public clone proves neither authenticated
GitHub access nor permission to push. Install Git/GitHub CLI only if needed,
using their official installers for the host platform.

Read the current fork section of `CONTRIBUTING.md`. Hikyo uses vouch: fork CI
requires the author's entry in `.github/VOUCHED.td` and maintainer approval of
fork workflow runs. Do not self-vouch. Fork PR changes under `.github/` cannot
pass the current trust gate; a maintainer must land them from a canonical branch.
Continue local testing while human gates are pending. Set the writable push
destination deliberately; an outside contributor cannot assume permission to
push to canonical `origin`.
Creating a fork is an external mutation; do it only if the user has authorized
that action. Existing fork access and local setup can be checked independently.
